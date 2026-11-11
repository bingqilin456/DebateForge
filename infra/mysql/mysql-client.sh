#!/usr/bin/env bash
set -eo pipefail

# 用短寿命 option file 避免口令进入命令行、Compose 配置或健康日志。
role="${1:?expected app, migrator or root}"
shift
host=127.0.0.1
case "$role" in
    app) user="$MYSQL_APP_USER" ;;
    migrator) user="$MYSQL_MIGRATION_USER" ;;
    root) user=root; host=localhost ;;
    *) exit 2 ;;
esac
umask 077
client_config="$(mktemp /tmp/siyige-client.XXXXXX)"
trap 'rm -f -- "$client_config"' EXIT
{
    printf '[client]\nuser=%s\npassword=%s\n' "$user" "$(cat "/run/secrets/${role}_password")"
    printf 'host=%s\ndatabase=%s\n' "$host" "$MYSQL_DATABASE"
} > "$client_config"
mysql --defaults-extra-file="$client_config" --connect-timeout=5 --batch --skip-column-names "$@"
