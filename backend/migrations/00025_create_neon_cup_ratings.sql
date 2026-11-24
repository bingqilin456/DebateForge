-- +goose Up
ALTER TABLE debates ADD COLUMN cup_stage VARCHAR(8) CHARACTER SET ascii COLLATE ascii_bin NOT NULL DEFAULT '',
    ADD CONSTRAINT ck_debate_neon_stage CHECK (cup_stage IN ('', 'prelim', 'quarter', 'semi', 'final'));

CREATE TABLE neon_cup_players (
    user_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    elo INT NOT NULL DEFAULT 1420,
    unlocked_index INT NOT NULL DEFAULT 0,
    games INT NOT NULL DEFAULT 0,
    wins INT NOT NULL DEFAULT 0,
    CONSTRAINT fk_neon_cup_player FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT ck_neon_cup_player CHECK (elo >= 1000 AND unlocked_index BETWEEN 0 AND 3 AND wins BETWEEN 0 AND games),
    INDEX idx_neon_cup_elo (elo)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE neon_elo_entries (
    debate_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    user_id BIGINT UNSIGNED NOT NULL,
    elo_before INT NOT NULL,
    elo_after INT NOT NULL,
    delta INT NOT NULL,
    outcome VARCHAR(4) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    unlocked_index INT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    CONSTRAINT fk_neon_elo_debate FOREIGN KEY (debate_id) REFERENCES debates(id) ON DELETE CASCADE,
    CONSTRAINT fk_neon_elo_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
    CONSTRAINT ck_neon_elo_entry CHECK (elo_after = elo_before + delta AND outcome IN ('win', 'loss', 'draw')),
    INDEX idx_neon_elo_history (user_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
