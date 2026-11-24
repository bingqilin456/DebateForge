-- +goose Up
CREATE TABLE neon_dojo_attempts (
    user_id BIGINT UNSIGNED NOT NULL,
    question_id VARCHAR(40) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    question_json JSON NULL,
    last_choice TINYINT UNSIGNED NULL,
    rewarded_at DATETIME(6) NULL,
    elo_after INT NULL,
    PRIMARY KEY (user_id, question_id),
    INDEX idx_neon_dojo_reward (user_id, rewarded_at),
    CONSTRAINT fk_neon_dojo_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT ck_neon_dojo_choice CHECK (last_choice BETWEEN 0 AND 3),
    CONSTRAINT ck_neon_dojo_reward CHECK ((rewarded_at IS NULL) = (elo_after IS NULL))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
