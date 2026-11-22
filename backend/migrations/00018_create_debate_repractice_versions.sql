-- +goose NO TRANSACTION
-- +goose Up
-- V2-08 局部重练：每次改写都是一条独立版本，绑定本场原始 message 与码点区间。
-- 与正式发言（messages）、会话状态、复盘（debate_reviews）完全隔离：
-- 本表只读引用 messages，不反向写回任何正式记录。
CREATE TABLE debate_repractice_versions (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    debate_id BIGINT UNSIGNED NOT NULL,
    client_request_id VARCHAR(128) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    message_id BIGINT UNSIGNED NOT NULL,
    start_cp INT UNSIGNED NOT NULL,
    end_cp INT UNSIGNED NOT NULL,
    quote TEXT NOT NULL,
    goal TEXT NOT NULL,
    rewrite TEXT NOT NULL,
    status VARCHAR(16) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    attempt_version BIGINT UNSIGNED NOT NULL DEFAULT 1,
    improved TINYINT(1) NULL,
    change_text TEXT NULL,
    reason_text TEXT NULL,
    new_problem TEXT NULL,
    error_code VARCHAR(64) CHARACTER SET ascii COLLATE ascii_bin NULL,
    restored_from BIGINT UNSIGNED NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    -- 同一会话内同一请求编号只对应一个版本：重复提交按幂等重放处理。
    UNIQUE KEY uq_debate_repractice_versions_request (debate_id, client_request_id),
    -- 历史版本按写入顺序回看，不重排。
    KEY ix_debate_repractice_versions_debate (debate_id, id),
    CONSTRAINT fk_debate_repractice_versions_debate FOREIGN KEY (debate_id) REFERENCES debates (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT fk_debate_repractice_versions_message FOREIGN KEY (message_id) REFERENCES messages (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT,
    CONSTRAINT ck_debate_repractice_versions_status CHECK (status IN ('generating', 'completed', 'failed')),
    CONSTRAINT ck_debate_repractice_versions_status_exact CHECK (CAST(status AS BINARY) IN (_binary'generating', _binary'completed', _binary'failed')),
    CONSTRAINT ck_debate_repractice_versions_attempt_version CHECK (attempt_version >= 1),
    -- 选段必须是 Unicode 码点半开区间 [start_cp, end_cp)，空区间不接受。
    CONSTRAINT ck_debate_repractice_versions_range CHECK (end_cp > start_cp)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_ai_ci;
