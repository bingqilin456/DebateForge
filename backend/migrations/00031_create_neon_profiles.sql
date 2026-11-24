-- +goose Up
CREATE TABLE neon_profiles (
    user_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    nickname VARCHAR(24) NOT NULL,
    avatar_kind VARCHAR(5) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    avatar_seal VARCHAR(1) NOT NULL DEFAULT '',
    avatar_image TEXT NOT NULL,
    CONSTRAINT fk_neon_profile_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT ck_neon_profile_avatar CHECK (
      (avatar_kind = 'seal' AND CHAR_LENGTH(avatar_seal) = 1 AND avatar_image = '') OR
      (avatar_kind = 'image' AND avatar_seal = '' AND CHAR_LENGTH(avatar_image) BETWEEN 24 AND 26000)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
