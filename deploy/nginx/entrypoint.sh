#!/bin/sh
set -eu

# 只把单个 DNS 名注入模板，避免将外部环境当作 Nginx 指令。
invalid_host() {
    printf '%s\n' 'SITE_HOST 必须为小写 DNS 名' >&2
    exit 1
}
case "${SITE_HOST:-}" in
    ''|*[!a-z0-9.-]*) invalid_host ;;
esac
[ "${#SITE_HOST}" -le 253 ] &&
    printf '%s\n' "$SITE_HOST" | grep -Eq '^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?(\.[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?)+$' ||
    invalid_host
envsubst '${SITE_HOST}' < /etc/nginx/siyige/site.conf.template > /tmp/site.conf
exec "$@"
