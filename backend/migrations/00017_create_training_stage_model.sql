-- +goose NO TRANSACTION
-- +goose Up
-- V2-02 训练赛：服务端权威阶段进度 + 按发言步骤建模的正式步骤与 AI 认领。
--
-- 为什么必须新增前向迁移而不是复用 V1 结构：
--   * V1 的环节由 (赛制, 轮号) 推导且不落库，前提是「一轮 = 用户一条 + AI 一条」；
--     训练赛存在 AI 先发言与跨阶段连续同方，轮号不再能反推阶段（PRD V2 §3.2）。
--   * 因此训练赛把「阶段 + 阶段内步数」持久化到 debates，并用独立步骤表记录
--     每条正式发言；V1 行保持 NULL/0，V1 语义与推导规则完全不变。
--   * 迁移只放宽枚举与边界、只新增列和表，不回填、不改写任何既有数据。
-- 本迁移必须先于新程序上线：旧程序不认识 training_v2，会在读取时受控失败。

-- 1. 放宽 debates.mode 枚举，加入训练赛取值。
--    仍保留 quick_3 / standard_4 两个 V1 取值与字节精确校验，
--    避免 PAD SPACE 或其他大小写变体混入。
ALTER TABLE debates
    DROP CHECK ck_debates_mode,
    DROP CHECK ck_debates_mode_exact,
    ADD CONSTRAINT ck_debates_mode CHECK (mode IN ('quick_3', 'standard_4', 'training_v2')),
    ADD CONSTRAINT ck_debates_mode_exact CHECK (CAST(mode AS BINARY) IN (_binary'quick_3', _binary'standard_4', _binary'training_v2'));

-- 2. 放宽 debates.completed_rounds 上限 6 -> 10。
--    训练赛的自由辩最多十回合，复用该列记录「已完成自由辩回合数」；
--    数据库只做粗边界 0–10，V1 的精确上限（quick_3 为 3、standard_4 为 4）
--    仍由业务代码按 mode 校验，不把数据库变成第二个事实源。
ALTER TABLE debates
    DROP CHECK ck_debates_completed_rounds,
    ADD CONSTRAINT ck_debates_completed_rounds CHECK (completed_rounds BETWEEN 0 AND 10);

-- 3. 新增训练赛权威阶段进度列。
--    V1 行 training_stage 保持 NULL，表示「本会话不使用训练赛阶段模型」；
--    training_stage_step 默认 0，对 V1 行无意义且不参与任何 V1 推导。
ALTER TABLE debates
    ADD COLUMN training_stage VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NULL AFTER ai_style,
    ADD COLUMN training_stage_step TINYINT UNSIGNED NOT NULL DEFAULT 0 AFTER training_stage,
    ADD CONSTRAINT ck_debates_training_stage CHECK (training_stage IS NULL OR training_stage IN ('opening', 'rebuttal', 'free_debate', 'closing')),
    ADD CONSTRAINT ck_debates_training_stage_exact CHECK (training_stage IS NULL OR CAST(training_stage AS BINARY) IN (_binary'opening', _binary'rebuttal', _binary'free_debate', _binary'closing')),
    ADD CONSTRAINT ck_debates_training_stage_step CHECK (training_stage_step BETWEEN 0 AND 2);

-- 4. 正式发言步骤表：每个已提交步骤一行，失败步骤不落该表、不占编号。
--    step_no 是该会话内连续的全局发言步骤序号（1–26）；
--    同一会话内 client_request_id 与 step_no 都唯一，作为幂等的最终保护。
CREATE TABLE debate_steps (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    debate_id BIGINT UNSIGNED NOT NULL,
    step_no TINYINT UNSIGNED NOT NULL,
    stage VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    stage_step TINYINT UNSIGNED NOT NULL,
    free_debate_round TINYINT UNSIGNED NOT NULL DEFAULT 0,
    speaker VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    client_request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    content TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_debate_steps_no (debate_id, step_no),
    UNIQUE KEY uq_debate_steps_request (debate_id, client_request_id),
    CONSTRAINT fk_debate_steps_debate FOREIGN KEY (debate_id) REFERENCES debates (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT ck_debate_steps_no CHECK (step_no BETWEEN 1 AND 26),
    CONSTRAINT ck_debate_steps_stage CHECK (stage IN ('opening', 'rebuttal', 'free_debate', 'closing')),
    CONSTRAINT ck_debate_steps_stage_exact CHECK (CAST(stage AS BINARY) IN (_binary'opening', _binary'rebuttal', _binary'free_debate', _binary'closing')),
    CONSTRAINT ck_debate_steps_stage_step CHECK (stage_step BETWEEN 1 AND 2),
    CONSTRAINT ck_debate_steps_free_round CHECK (free_debate_round BETWEEN 0 AND 10),
    CONSTRAINT ck_debate_steps_speaker CHECK (speaker IN ('user', 'assistant')),
    CONSTRAINT ck_debate_steps_speaker_exact CHECK (CAST(speaker AS BINARY) IN (_binary'user', _binary'assistant'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 5. AI 步骤认领表：复用 V1 轮次相同的认领/幂等/版本/租约语义。
--    同一 client_request_id 在会话内唯一（幂等最终保护）；step_no 只建普通索引，
--    因为自由辩失败回复被明确放弃后，该 attempt 会在 claims 中保留为 abandoned，
--    随后结辩的正式步骤可复用同一逻辑 step_no 新建认领行——放弃记录必须可见，
--    但不能占住号码阻止阶段推进。同一时刻每个 step_no 至多一条活动认领，
--    由服务端在会话行锁内保证。
--    status 额外允许 abandoned：自由辩失败回复被用户明确放弃后记录仍保留可见
--    （PRD V2 §3.6 失败记录须可见）。
CREATE TABLE debate_step_claims (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    debate_id BIGINT UNSIGNED NOT NULL,
    step_no TINYINT UNSIGNED NOT NULL,
    client_request_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    attempt_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    lease_expires_at DATETIME(6) NULL,
    error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    model_name VARCHAR(255) NULL,
    prompt_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    input_tokens INT UNSIGNED NULL,
    output_tokens INT UNSIGNED NULL,
    latency_ms INT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_debate_step_claims_request (debate_id, client_request_id),
    KEY ix_debate_step_claims_step (debate_id, step_no, id),
    KEY ix_debate_step_claims_status_lease (status, lease_expires_at),
    CONSTRAINT fk_debate_step_claims_debate FOREIGN KEY (debate_id) REFERENCES debates (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT ck_debate_step_claims_step CHECK (step_no BETWEEN 1 AND 26),
    CONSTRAINT ck_debate_step_claims_status CHECK (status IN ('generating', 'completed', 'failed', 'abandoned')),
    CONSTRAINT ck_debate_step_claims_status_exact CHECK (CAST(status AS BINARY) IN (_binary'generating', _binary'completed', _binary'failed', _binary'abandoned')),
    CONSTRAINT ck_debate_step_claims_attempt_version CHECK (attempt_version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
