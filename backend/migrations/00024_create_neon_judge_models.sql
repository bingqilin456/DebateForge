-- +goose Up
CREATE TABLE neon_judge_models (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT UNSIGNED NOT NULL,
    ciphertext VARBINARY(284) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    provider VARCHAR(16) NOT NULL,
    base_url VARCHAR(512) NOT NULL,
    model VARCHAR(128) NOT NULL,
    alias VARCHAR(128) NOT NULL,
    INDEX ix_neon_models_owner (user_id, id),
    CONSTRAINT fk_neon_models_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
