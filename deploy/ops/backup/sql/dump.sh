#!/usr/bin/env bash
# 容器内一致性快照：InnoDB 事务级一致视图，输出 gzip 压缩的 SQL 到标准输出。
# 只读；--single-transaction 不锁表，--skip-lock-tables 避免额外锁。
set -euo pipefail
umask 077
client_config="$(mktemp /tmp/t16a-dump.XXXXXX)"
trap 'rm -f -- "$client_config"' EXIT
# 不能把 database= 写进 [client]：mysqldump 不认这个选项，会直接报
# “unknown variable 'database=...'”。目标库以最后一个位置参数传入。
{
    printf '[client]\nuser=%s\npassword=%s\n' "$MYSQL_MIGRATION_USER" "$(cat /run/secrets/migrator_password)"
    printf 'host=127.0.0.1\n'
} > "$client_config"

# 不导出 --events：迁移账户按最小权限只拿到表级 DML/DDL，没有 EVENT 权限，
# mysqldump 一读事件就会 Access denied；当前 Schema 也没有任何事件。
# 若将来引入存储过程等对象，须先单独评审权限再补对应导出参数。
# 加 --skip-add-locks：默认会在每个表的 INSERT 外包裹 LOCK TABLES ... WRITE，
# 而恢复端同样只拿最小权限（无 LOCK TABLES），导入会直接 1044 失败。
# 备份已用 --single-transaction 取得一致快照，加锁语句没有额外价值。
mysqldump --defaults-extra-file="$client_config" \
    --single-transaction \
    --quick \
    --skip-lock-tables \
    --skip-add-locks \
    --no-tablespaces \
    --skip-column-statistics \
    --set-gtid-purged=OFF \
    --hex-blob \
    --default-character-set=utf8mb4 \
    --routines \
    "$MYSQL_DATABASE" | gzip -9
