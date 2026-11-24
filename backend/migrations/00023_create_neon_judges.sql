-- +goose Up
CREATE TABLE neon_judges (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(80) NOT NULL,
    `role` VARCHAR(500) NOT NULL,
    `public` BOOLEAN NOT NULL DEFAULT FALSE,
    INDEX ix_neon_judges_owner (user_id, id),
    INDEX ix_neon_judges_visibility (`public`, id),
    CONSTRAINT fk_neon_judges_user FOREIGN KEY (user_id) REFERENCES users (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
