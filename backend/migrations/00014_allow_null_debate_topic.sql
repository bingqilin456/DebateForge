-- +goose NO TRANSACTION
-- +goose Up
-- V2-03 私有自定义辩题：把 debates.topic_id 放宽为可空。
-- 非空表示预设题（仍指向 topics 中的公共题目），NULL 表示本场使用用户自定义辩题，
-- 题目与背景只存在于本行的 topic_title_snapshot / topic_description_snapshot 中。
--
-- 只放宽可空性，不改列类型、不删除索引、不动既有外键，也不回填任何数据：
--   * 索引 ix_debates_topic_id 保留，MySQL 二级索引本身允许 NULL。
--   * 外键 fk_debates_topic 保留；MySQL 外键列允许 NULL，且 NULL 不参与引用检查，
--     因此“没有公共题目”与“引用一个不存在的题目”在数据库层仍是两种不同状态。
--   * 既有行全部保持原值，历史会话的预设语义与只读行为不变。
--
-- 兼容性与上线顺序（见 docs/planning/V2_03_PRIVATE_TOPIC_DESIGN.md）：
--   本迁移必须先于新程序上线。含自定义记录的库不能直接恢复到要求
--   topic_id NOT NULL 的旧 Schema 或旧程序；旧程序读取 NULL 行会得到无效题目。
ALTER TABLE debates
    MODIFY COLUMN topic_id BIGINT UNSIGNED NULL;
