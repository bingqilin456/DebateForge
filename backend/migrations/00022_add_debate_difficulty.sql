-- +goose Up
ALTER TABLE debates ADD COLUMN difficulty VARCHAR(12) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'beginner',
    ADD COLUMN practice BOOLEAN NOT NULL DEFAULT FALSE,
    ADD CONSTRAINT ck_debates_difficulty CHECK (difficulty IN ('beginner', 'varsity', 'national'));
