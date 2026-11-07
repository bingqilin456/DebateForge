#!/usr/bin/env bash
set -eo pipefail

app_password="$(cat /run/secrets/app_password)"
migrator_password="$(cat /run/secrets/migrator_password)"
# 本地生成器只产生十六进制口令，拒绝把任意文本拼进初始化 SQL。
[[ "$app_password" =~ ^[a-f0-9]{64}$ && "$migrator_password" =~ ^[a-f0-9]{64}$ ]]
[[ "$MYSQL_DATABASE" =~ ^siyige_(dev|test)$ ]]
[[ "$MYSQL_APP_USER" == "${MYSQL_DATABASE}_app" ]]
[[ "$MYSQL_MIGRATION_USER" == "${MYSQL_DATABASE}_migrator" ]]
# MySQL 的库级 GRANT 把下划线视为通配符，必须显式转义以限定精确库名。
grant_database="${MYSQL_DATABASE//_/\\_}"

# 不依赖父进程的 SOCKET 变量，兼容 Windows 挂载后的执行/source 差异。
bash /opt/siyige/mysql-client.sh root <<SQL
CREATE USER '${MYSQL_APP_USER}'@'%' IDENTIFIED BY '${app_password}';
GRANT SELECT, INSERT, UPDATE, DELETE ON \`${grant_database}\`.* TO '${MYSQL_APP_USER}'@'%';
CREATE USER '${MYSQL_MIGRATION_USER}'@'%' IDENTIFIED BY '${migrator_password}';
GRANT ALL PRIVILEGES ON \`${grant_database}\`.* TO '${MYSQL_MIGRATION_USER}'@'%';
SQL
unset app_password migrator_password grant_database
