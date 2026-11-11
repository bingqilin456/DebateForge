-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE debates (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id BIGINT UNSIGNED NOT NULL,
    topic_id BIGINT UNSIGNED NOT NULL,
    topic_title_snapshot TEXT NOT NULL,
    topic_description_snapshot TEXT NOT NULL,
    user_stance VARCHAR(3) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT 'active',
    completed_rounds TINYINT UNSIGNED NOT NULL DEFAULT 0,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY ix_debates_user_created_id (user_id, created_at, id),
    KEY ix_debates_topic_id (topic_id),
    CONSTRAINT fk_debates_user FOREIGN KEY (user_id) REFERENCES users (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT fk_debates_topic FOREIGN KEY (topic_id) REFERENCES topics (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT ck_debates_stance CHECK (user_stance IN ('pro', 'con')),
    CONSTRAINT ck_debates_status CHECK (status IN ('active', 'completed')),
    CONSTRAINT ck_debates_completed_rounds CHECK (completed_rounds BETWEEN 0 AND 3)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
