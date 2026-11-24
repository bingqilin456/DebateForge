-- +goose Up
CREATE TABLE neon_public_reports (
    debate_id BIGINT UNSIGNED NOT NULL PRIMARY KEY,
    owner_id BIGINT UNSIGNED NOT NULL,
    topic_key VARCHAR(32) CHARACTER SET ascii COLLATE ascii_bin NULL,
    share_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
    title TEXT NOT NULL,
    report JSON NULL,
    published_at DATETIME(6) NULL,
    UNIQUE KEY ux_neon_report_share (share_id),
    KEY ix_neon_report_owner_time (owner_id, published_at, debate_id),
    KEY ix_neon_report_topic_time (topic_key, published_at, debate_id),
    CONSTRAINT fk_neon_report_debate FOREIGN KEY (debate_id) REFERENCES debates (id) ON DELETE CASCADE,
    CONSTRAINT ck_neon_report_publication CHECK (
        (share_id IS NULL AND report IS NULL AND published_at IS NULL)
        OR (share_id IS NOT NULL AND report IS NOT NULL AND published_at IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
