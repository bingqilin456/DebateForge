-- +goose Up
CREATE TABLE training_repractice_states (
    debate_id BIGINT UNSIGNED NOT NULL,
    payload JSON NOT NULL,
    PRIMARY KEY (debate_id),
    CONSTRAINT fk_training_repractice_debate FOREIGN KEY (debate_id) REFERENCES debates (id)
        ON DELETE CASCADE ON UPDATE RESTRICT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

-- +goose Down
DROP TABLE training_repractice_states;
