-- +goose NO TRANSACTION
-- +goose Up
-- V2-04 原文批注：为正式轮次增加“批注资格”标志，并新增独立的批注批次表。
--
-- 两条语句都必须前向执行，不改既有行：
--   1. annotation_eligible 默认 false。旧行保持原状，因此旧会话不会凭空获得批注资格，
--      读取接口对它们返回空列表。
--   2. turn_annotations 以触发 turn_id 唯一，一行代表一次限定范围的批注批次。
--      状态、尝试版本、租约与受控错误码沿用总结/策略表的既有形状，便于复用恢复路径。
--
-- 批注资格只表示“这一轮在提交时被本次切片允许生成批注”，
-- 不是正式轮次状态机的一部分：它不参与 generating/completed/failed 的判断，
-- 也不改变 debates.completed_rounds 与 debates.status。
ALTER TABLE debate_turns
    ADD COLUMN annotation_eligible BOOLEAN NOT NULL DEFAULT false AFTER error_code,
    ADD CONSTRAINT ck_debate_turns_annotation_eligible CHECK (annotation_eligible IN (0, 1));

CREATE TABLE turn_annotations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    debate_id BIGINT UNSIGNED NOT NULL,
    turn_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    content_json TEXT NULL,
    attempt_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    lease_expires_at DATETIME(6) NULL,
    error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    model_name VARCHAR(255) NULL,
    prompt_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    input_tokens INT UNSIGNED NULL,
    output_tokens INT UNSIGNED NULL,
    latency_ms INT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_turn_annotations_turn (turn_id),
    KEY ix_turn_annotations_debate (debate_id, turn_id),
    KEY ix_turn_annotations_status_lease (status, lease_expires_at),
    CONSTRAINT fk_turn_annotations_debate FOREIGN KEY (debate_id) REFERENCES debates (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT fk_turn_annotations_turn FOREIGN KEY (turn_id) REFERENCES debate_turns (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT ck_turn_annotations_status CHECK (status IN ('generating', 'completed', 'failed')),
    CONSTRAINT ck_turn_annotations_status_exact CHECK (CAST(status AS BINARY) IN (_binary'generating', _binary'completed', _binary'failed')),
    CONSTRAINT ck_turn_annotations_attempt_version CHECK (attempt_version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
