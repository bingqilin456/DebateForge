#!/usr/bin/env bash
# 容器内导入：从标准输入读取 gzip 的 SQL 并写入当前数据库。
# 由 t16a-restore.sh 在空目标库上调用；脚本自身不判断库是否为空。
set -euo pipefail
umask 077
client_config="$(mktemp /tmp/t16a-import.XXXXXX)"
trap 'rm -f -- "$client_config"' EXIT
{
    printf '[client]\nuser=%s\npassword=%s\n' "$MYSQL_MIGRATION_USER" "$(cat /run/secrets/migrator_password)"
    printf 'host=127.0.0.1\ndatabase=%s\n' "$MYSQL_DATABASE"
} > "$client_config"

gzip -dc | mysql --defaults-extra-file="$client_config" --connect-timeout=5 --batch "$MYSQL_DATABASE"
