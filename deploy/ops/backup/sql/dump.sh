#!/usr/bin/env bash
# 容器内一致性快照：InnoDB 事务级一致视图，输出 gzip 压缩的 SQL 到标准输出。
# 只读；--single-transaction 不锁表，--skip-lock-tables 避免额外锁。
set -euo pipefail
umask 077
client_config="$(mktemp /tmp/t16a-dump.XXXXXX)"
trap 'rm -f -- "$client_config"' EXIT
{
    printf '[client]\nuser=%s\npassword=%s\n' "$MYSQL_MIGRATION_USER" "$(cat /run/secrets/migrator_password)"
    printf 'host=127.0.0.1\ndatabase=%s\n' "$MYSQL_DATABASE"
} > "$client_config"

mysqldump --defaults-extra-file="$client_config" \
    --single-transaction \
    --quick \
    --skip-lock-tables \
    --no-tablespaces \
    --skip-column-statistics \
    --set-gtid-purged=OFF \
    --hex-blob \
    --default-character-set=utf8mb4 \
    --routines \
    --events \
    "$MYSQL_DATABASE" | gzip -9
