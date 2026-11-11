-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE messages (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    turn_id BIGINT UNSIGNED NOT NULL,
    role VARCHAR(9) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    content TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_messages_turn_role (turn_id, role),
    CONSTRAINT fk_messages_turn FOREIGN KEY (turn_id) REFERENCES debate_turns (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT ck_messages_role CHECK (role IN ('user', 'assistant'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
