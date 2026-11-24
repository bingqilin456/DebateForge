-- +goose Up
ALTER TABLE topics ADD COLUMN neon_category VARCHAR(16) CHARACTER SET ascii NOT NULL DEFAULT 'society';
UPDATE topics SET neon_category = CASE id WHEN 1 THEN 'campus' WHEN 3 THEN 'campus' WHEN 5 THEN 'philosophy' ELSE 'society' END WHERE id BETWEEN 1 AND 5;
INSERT INTO topics (title, description, enabled, neon_category) VALUES
    ('大学生使用生成式 AI 辅助学习是利大于弊还是弊大于利',
    '正方主张利大于弊，反方主张弊大于利。请讨论自主学习、知识理解与技术依赖。', TRUE, 'technology');

CREATE TABLE neon_topics (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT UNSIGNED NOT NULL,
    title VARCHAR(200) NOT NULL,
    background TEXT NOT NULL,
    category VARCHAR(16) CHARACTER SET ascii NOT NULL,
    is_public BOOLEAN NOT NULL DEFAULT FALSE,
    INDEX idx_neon_topics_owner (user_id),
    INDEX idx_neon_topics_public (is_public, category),
    CONSTRAINT fk_neon_topics_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

CREATE TABLE neon_opponents (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id BIGINT UNSIGNED NOT NULL,
    name VARCHAR(40) NOT NULL,
    tag VARCHAR(30) NOT NULL,
    difficulty VARCHAR(12) CHARACTER SET ascii NOT NULL,
    persona TEXT NOT NULL,
    is_public BOOLEAN NOT NULL DEFAULT FALSE,
    INDEX idx_neon_opponents_owner (user_id),
    INDEX idx_neon_opponents_public (is_public),
    CONSTRAINT fk_neon_opponents_user FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;

ALTER TABLE debates
    ADD COLUMN neon_topic_id BIGINT UNSIGNED NULL,
    ADD COLUMN neon_opponent_id BIGINT UNSIGNED NULL,
    ADD COLUMN opponent_name_snapshot VARCHAR(40) NOT NULL DEFAULT '',
    ADD COLUMN opponent_tag_snapshot VARCHAR(30) NOT NULL DEFAULT '',
    ADD COLUMN opponent_persona_snapshot TEXT NULL,
    ADD INDEX idx_debates_neon_topic (neon_topic_id, user_id, completed_rounds),
    ADD INDEX idx_debates_neon_opponent (neon_opponent_id, user_id, completed_rounds);
