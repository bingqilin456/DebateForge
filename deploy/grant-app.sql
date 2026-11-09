-- 迁移成功后由管理员显式执行。按业务表授权，隔离 Goose 历史。
GRANT SELECT, INSERT ON `siyige_deploy`.`users` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, DELETE ON `siyige_deploy`.`auth_sessions` TO 'siyige_deploy_app'@'%';
GRANT SELECT ON `siyige_deploy`.`topics` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`debates` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT, UPDATE ON `siyige_deploy`.`debate_turns` TO 'siyige_deploy_app'@'%';
GRANT SELECT, INSERT ON `siyige_deploy`.`messages` TO 'siyige_deploy_app'@'%';
