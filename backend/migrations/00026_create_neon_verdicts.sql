-- +goose Up
CREATE TABLE neon_verdicts (
    debate_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    result JSON NOT NULL,
    CONSTRAINT fk_neon_verdict_debate FOREIGN KEY (debate_id) REFERENCES debates(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
