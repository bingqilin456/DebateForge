-- +goose NO TRANSACTION
-- +goose Up
-- 前向扩展：放宽轮次上限以支持 4 轮标准赛制，并为会话增加赛制模式与辩手流派。
-- 语句顺序有意为之：先做纯放宽（不会因既有数据失败），再做加列。
-- 任一语句失败时停止且不登记版本；已成功的放宽不回滚，重跑前先核对实际结构。
-- 本迁移不包含任何数据回填：既有 quick_3 历史会话保持原 completed_rounds，只读语义不变。

-- 1. 放宽 debates.completed_rounds 上限 3 -> 6。
-- 上限放到 6 而不是 4：新增赛制模式时无需再次修改 CHECK；按模式的精确上限由业务代码校验。
ALTER TABLE debates
    DROP CHECK ck_debates_completed_rounds,
    ADD CONSTRAINT ck_debates_completed_rounds CHECK (completed_rounds BETWEEN 0 AND 6);

-- 2. 放宽 debate_turns.round_no 上限 3 -> 6，理由同上。
ALTER TABLE debate_turns
    DROP CHECK ck_debate_turns_round,
    ADD CONSTRAINT ck_debate_turns_round CHECK (round_no BETWEEN 1 AND 6);

-- 3. 新增赛制模式列。两步默认值：
--    先以 quick_3 作为默认，使既有行被填充为 quick_3（历史三轮语义不变）；
--    随后把列默认改为 standard_4，使新插入行使用 4 轮标准赛制。
--    两步均为元数据操作，不改写既有行数据。
ALTER TABLE debates
    ADD COLUMN mode VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'quick_3' AFTER user_stance,
    ADD CONSTRAINT ck_debates_mode CHECK (mode IN ('quick_3', 'standard_4')),
    ADD CONSTRAINT ck_debates_mode_exact CHECK (CAST(mode AS BINARY) IN (_binary'quick_3', _binary'standard_4'));

ALTER TABLE debates
    ALTER COLUMN mode SET DEFAULT 'standard_4';

-- 4. 新增辩手流派列。既有行填充为 logical，保持默认流派行为确定。
ALTER TABLE debates
    ADD COLUMN ai_style VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'logical' AFTER mode,
    ADD CONSTRAINT ck_debates_ai_style CHECK (ai_style IN ('logical', 'aggressive', 'empathic', 'data')),
    ADD CONSTRAINT ck_debates_ai_style_exact CHECK (CAST(ai_style AS BINARY) IN (_binary'logical', _binary'aggressive', _binary'empathic', _binary'data'));
