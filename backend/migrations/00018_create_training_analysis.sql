-- +goose NO TRANSACTION
-- +goose Up
-- 按已提交 AI 步骤保存批注。四轮的 turn_id 外键保留原义。
CREATE TABLE training_annotations (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    debate_id BIGINT UNSIGNED NOT NULL,
    turn_id BIGINT UNSIGNED NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
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
    UNIQUE KEY uq_training_annotations_turn (turn_id),
    KEY ix_training_annotations_debate (debate_id, turn_id),
    KEY ix_training_annotations_status_lease (status, lease_expires_at),
    CONSTRAINT fk_training_annotations_debate FOREIGN KEY (debate_id) REFERENCES debates (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT fk_training_annotations_turn FOREIGN KEY (turn_id) REFERENCES debate_steps (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT ck_training_annotations_status CHECK (status IN ('generating', 'completed', 'failed')),
    CONSTRAINT ck_training_annotations_status_exact CHECK (CAST(status AS BINARY) IN (_binary'generating', _binary'completed', _binary'failed')),
    CONSTRAINT ck_training_annotations_attempt_version CHECK (attempt_version >= 1)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- 争点状态、去重位置与人工编辑一起提交，避免分析覆盖较晚的修改。
CREATE TABLE training_issue_states (
    debate_id BIGINT UNSIGNED NOT NULL,
    payload JSON NOT NULL,
    PRIMARY KEY (debate_id),
    CONSTRAINT fk_training_issue_debate FOREIGN KEY (debate_id) REFERENCES debates (id)
        ON DELETE CASCADE ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE training_issue_states;
DROP TABLE training_annotations;
