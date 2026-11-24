-- +goose Up
ALTER TABLE user_model_settings
    ADD COLUMN provider VARCHAR(16) NOT NULL DEFAULT 'deepseek',
    ADD COLUMN base_url VARCHAR(512) NOT NULL DEFAULT 'https://api.deepseek.com',
    ADD COLUMN model VARCHAR(128) NOT NULL DEFAULT 'deepseek-flash',
    ADD COLUMN alias VARCHAR(128) NOT NULL DEFAULT '',
    ADD COLUMN verified BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN used BOOLEAN NOT NULL DEFAULT FALSE;
