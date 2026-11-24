-- 迁移成功后由管理员显式执行。按业务表授权，隔离 Goose 历史。
GRANT SELECT, INSERT ON `siyige_deploy`.`users` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, DELETE ON `siyige_deploy`.`auth_sessions` TO 'siyige_deploy_app'@'%';
GRANT SELECT ON `siyige_deploy`.`topics` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`debates` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`debate_turns` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT ON `siyige_deploy`.`messages` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`debate_summaries` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`debate_strategies` TO 'siyige_deploy_app'@'%';

-- Neon 公网 Fake 版使用的批注、训练、裁决和社区表。
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`turn_annotations` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`debate_reviews` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT ON `siyige_deploy`.`debate_steps` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`debate_step_claims` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`training_annotations` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`training_issue_states` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`training_repractice_states` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`neon_judges` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`neon_cup_players` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT ON `siyige_deploy`.`neon_elo_entries` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`neon_verdicts` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`neon_topics` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`neon_opponents` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`neon_public_reports` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`neon_dojo_attempts` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`neon_profiles` TO 'siyige_deploy_app'@'%';
