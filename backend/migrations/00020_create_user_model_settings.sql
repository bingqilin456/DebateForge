-- +goose Up
CREATE TABLE user_model_settings (
    user_id BIGINT UNSIGNED NOT NULL,
    ciphertext VARBINARY(284) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    PRIMARY KEY (user_id),
    CONSTRAINT fk_user_model_settings_user FOREIGN KEY (user_id) REFERENCES users (id)
        ON DELETE CASCADE ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE user_model_settings;
