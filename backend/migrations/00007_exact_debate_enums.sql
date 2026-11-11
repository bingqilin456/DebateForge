-- +goose NO TRANSACTION
-- +goose Up
-- 保留原约束；按字节比较避免 ascii_bin 的 PAD SPACE 接受尾随空格。
-- ADD CHECK 会校验已有行；遇到异常只停止，不擅自 trim 或改写业务数据。
ALTER TABLE debates
    ADD CONSTRAINT ck_debates_stance_exact CHECK (CAST(user_stance AS BINARY) IN (_binary'pro', _binary'con')),
    ADD CONSTRAINT ck_debates_status_exact CHECK (CAST(status AS BINARY) IN (_binary'active', _binary'completed'));
