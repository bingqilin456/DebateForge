#!/usr/bin/env bash
# 仅在新数据卷初始化时创建本项目账户。口令只从 secrets 读取并限制格式，
# 避免任意文本进入 SQL。库名/账户白名单固定为 T16A 隔离演练范围。
(
set -euo pipefail

app_password="$(cat /run/secrets/app_password)"
migrator_password="$(cat /run/secrets/migrator_password)"
[[ "$app_password" =~ ^[a-f0-9]{64}$ && "$migrator_password" =~ ^[a-f0-9]{64}$ ]]

case "$MYSQL_DATABASE" in
    siyige_t16a_source|siyige_t16a_restore) : ;;
    *) echo "拒绝：数据库名不在 T16A 白名单：$MYSQL_DATABASE" >&2; exit 1 ;;
esac
[[ "$MYSQL_APP_USER" == "${MYSQL_DATABASE}_app" ]]
[[ "$MYSQL_MIGRATION_USER" == "${MYSQL_DATABASE}_migrator" ]]

# 库名下划线转义，避免库级授权误匹配其他 schema。
bash /opt/t16a/sql/client.sh root <<SQL
CREATE USER '$MYSQL_APP_USER'@'%' IDENTIFIED BY '$app_password';
CREATE USER '$MYSQL_MIGRATION_USER'@'%' IDENTIFIED BY '$migrator_password';
GRANT SELECT, INSERT, CREATE, ALTER, INDEX, REFERENCES, DROP ON \`${MYSQL_DATABASE//_/\\_}\`.* TO '$MYSQL_MIGRATION_USER'@'%';
GRANT SELECT, INSERT, UPDATE, DELETE ON \`${MYSQL_DATABASE//_/\\_}\`.* TO '$MYSQL_APP_USER'@'%';
SQL
)
