-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE debate_turns
    ADD CONSTRAINT ck_debate_turns_status_exact CHECK (CAST(status AS BINARY) IN (_binary'generating', _binary'completed', _binary'failed'));
