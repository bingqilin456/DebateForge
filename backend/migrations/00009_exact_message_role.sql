-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE messages
    ADD CONSTRAINT ck_messages_role_exact CHECK (CAST(role AS BINARY) IN (_binary'user', _binary'assistant'));
