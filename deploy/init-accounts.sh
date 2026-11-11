#!/usr/bin/env bash
# 官方入口可能 source 只读绑定的普通文件，子 Shell 隔离选项和变量。
(
set -euo pipefail

# 官方镜像仅在新卷初始化账户。校验后才允许将口令传入 SQL 标准输入。
app_password="$(cat /run/secrets/app_password)"
migrator_password="$(cat /run/secrets/migrator_password)"
[[ "$app_password" =~ ^[a-f0-9]{64}$ && "$migrator_password" =~ ^[a-f0-9]{64}$ ]]
[[ "$MYSQL_DATABASE" == siyige_deploy && "$MYSQL_APP_USER" == siyige_deploy_app ]]
[[ "$MYSQL_MIGRATION_USER" == siyige_deploy_migrator ]]

# 库名下划线需转义，避免库级授权匹配其他 schema。
bash /opt/siyige/mysql-client.sh root <<SQL
CREATE USER 'siyige_deploy_app'@'%' IDENTIFIED BY '${app_password}';
CREATE USER 'siyige_deploy_migrator'@'%' IDENTIFIED BY '${migrator_password}';
GRANT SELECT, INSERT, CREATE, ALTER, INDEX, REFERENCES ON \`siyige\_deploy\`.* TO 'siyige_deploy_migrator'@'%';
SQL
)
