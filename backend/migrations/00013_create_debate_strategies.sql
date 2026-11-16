-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE debate_strategies (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    debate_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    schema_version INT UNSIGNED NULL,
    content_json TEXT NULL,
    attempt_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    lease_expires_at DATETIME(6) NULL,
    error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    model_name VARCHAR(255) NULL,
    prompt_version VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    input_tokens INT UNSIGNED NULL,
    output_tokens INT UNSIGNED NULL,
    latency_ms INT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_debate_strategies_debate (debate_id),
    KEY ix_debate_strategies_status_lease (status, lease_expires_at),
    CONSTRAINT fk_debate_strategies_debate FOREIGN KEY (debate_id) REFERENCES debates (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT ck_debate_strategies_status CHECK (status IN ('generating', 'completed', 'failed')),
    CONSTRAINT ck_debate_strategies_status_exact CHECK (CAST(status AS BINARY) IN (_binary'generating', _binary'completed', _binary'failed')),
    CONSTRAINT ck_debate_strategies_attempt_version CHECK (attempt_version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
