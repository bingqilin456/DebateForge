#!/usr/bin/env bash
# T16A 备份：从固定源项目取一致性快照，用 age 公钥加密，校验后原子发布密文。
# 只读源库；不写目标库、不删除卷、不发布端口、不接触其他项目。
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./t16a-lib.sh
. "$script_dir/t16a-lib.sh"

usage() {
    cat <<'EOF'
用法：t16a-backup.sh --artifacts-dir <绝对路径> --recipient-file <绝对路径>
                     [--label <名称>] [--help]

从固定 Compose 项目 t16a-backup-source 的 mysql 服务生成 InnoDB 一致性快照，
以独立 age recipient（只含公钥）加密，校验摘要后原子发布密文。

  --artifacts-dir    密文与清单发布目录，自动创建且权限 0700（绝对路径）
  --recipient-file  age 公钥文件，内容形如 age1...（绝对路径；发现私钥即拒绝）
  --label            产物文件名前缀，默认 t16a-<UTC 时间戳>
  --help             显示本帮助

成功后才出现的产物：
  <label>.sql.gz.age     密文，长期保留的唯一数据副本
  <label>.sha256         密文 SHA-256 校验文件
  <label>.manifest.json  脱敏元数据（项目、库名、时间、表数、版本）

失败时清理暂存目录，不发布任何半成品；明文载荷不会离开暂存目录。
EOF
}

die_usage() {
    printf '[t16a][error] %s\n' "$*" >&2
    printf '使用 --help 查看用法。\n' >&2
    exit 2
}

artifacts_dir="" recipient_file="" label=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --help|-h) usage; exit 0 ;;
        --artifacts-dir) [[ $# -ge 2 ]] || die_usage "--artifacts-dir 缺少值"; artifacts_dir="$2"; shift 2 ;;
        --recipient-file) [[ $# -ge 2 ]] || die_usage "--recipient-file 缺少值"; recipient_file="$2"; shift 2 ;;
        --label) [[ $# -ge 2 ]] || die_usage "--label 缺少值"; label="$2"; shift 2 ;;
        *) die_usage "未知参数：$1" ;;
    esac
done

[[ -n "$artifacts_dir" ]] || die_usage "必须提供 --artifacts-dir"
[[ -n "$recipient_file" ]] || die_usage "必须提供 --recipient-file"
require_abs_path "$artifacts_dir"
require_abs_path "$recipient_file"
# 先校验输入与密钥形态，再要求 docker；参数拒绝路径不依赖 Docker 可用性。
[[ -f "$recipient_file" ]] || die "公钥文件不存在：$recipient_file"

recipient_content="$(tr -d '\r\n' < "$recipient_file")"
case "$recipient_content" in
    AGE-SECRET-KEY-*) die "拒绝：--recipient-file 含私钥，加密只用公钥 recipient" ;;
    age1*) : ;;
    *) die "拒绝：recipient 不是 age 公钥（应以 age1 开头）" ;;
esac
[[ "$recipient_content" != *[[:space:]]* ]] || die "拒绝：recipient 含空白字符"

if [[ -z "$label" ]]; then
    label="t16a-$(date -u +%Y%m%dT%H%M%SZ)"
fi
[[ "$label" =~ ^[a-z0-9][a-z0-9._-]{0,63}$ ]] || die "拒绝：--label 只允许小写字母、数字、点、下划线、连字符（≤64）"

for suffix in sql.gz.age sha256 manifest.json; do
    [[ ! -e "$artifacts_dir/$label.$suffix" ]] || die "拒绝：产物已存在，不覆盖：$artifacts_dir/$label.$suffix"
done

require_cmd docker
T16A_COMPOSE_FILE="${T16A_COMPOSE_FILE:-$script_dir/../compose.yaml}"
[[ -f "$T16A_COMPOSE_FILE" ]] || die "找不到 compose 文件：$T16A_COMPOSE_FILE"

# 源项目固定；启动前先核对容器标签与端口，避免连到别的环境。
assert_service_running "$T16A_SOURCE_PROJECT" mysql
assert_container_project "$T16A_SOURCE_PROJECT" mysql
assert_no_published_ports "$T16A_SOURCE_PROJECT"

mkdir -p -- "$artifacts_dir"
chmod 700 "$artifacts_dir" 2>/dev/null || warn "无法把 $artifacts_dir 权限收紧到 0700（非 POSIX 文件系统）"
staging="$artifacts_dir/.staging.$label.$"
[[ ! -e "$staging" ]] || die "拒绝：暂存目录已存在，不清理或覆盖：$staging"
mkdir -- "$staging"
chmod 700 "$staging" 2>/dev/null || true
cleanup() {
    # 失败不提交产物：暂存目录（含明文载荷）无条件删除。
    rm -rf -- "$staging"
}
trap cleanup EXIT

export T16A_DB="$T16A_SOURCE_DB"
export T16A_APP_USER="${T16A_SOURCE_DB}_app"
export T16A_MIGRATOR_USER="${T16A_SOURCE_DB}_migrator"
export T16A_SECRETS_DIR="${T16A_SECRETS_DIR:-}"

log "开始一致性快照：project=$T16A_SOURCE_PROJECT db=$T16A_SOURCE_DB"
dump_started_at="$(now_utc)"
# 一致性由容器内 mysqldump --single-transaction 保证；快照时间以主机 UTC 边界记录。
if ! compose_for "$T16A_SOURCE_PROJECT" exec -T mysql bash /opt/t16a/sql/dump.sh > "$staging/payload.sql.gz"; then
    die "快照失败；未发布任何产物"
fi
dump_finished_at="$(now_utc)"
[[ -s "$staging/payload.sql.gz" ]] || die "快照为空；未发布任何产物"
gzip -t "$staging/payload.sql.gz" 2>/dev/null || die "快照 gzip 校验失败；未发布任何产物"

# 不重复解压整份快照：gzip -t 已能发现截断；mysqldump 非零退出在上一步即终止。
mysql_version="$(sql_exec "$T16A_SOURCE_PROJECT" migrator 'SELECT VERSION()' | head -n1)"
tables="$(table_count "$T16A_SOURCE_PROJECT" | head -n1)"
[[ "$tables" =~ ^[0-9]+$ ]] || die "无法读取源库表数；未发布任何产物"

cipher="$staging/payload.sql.gz.age"
if command -v age >/dev/null 2>&1; then
    age --encrypt --recipient "$recipient_content" --output "$cipher" "$staging/payload.sql.gz"
else
    warn "主机缺少 age，改用工具镜像 $T16A_TOOL_IMAGE（目录只挂暂存子目录）"
    docker run --rm -i -v "$staging:/work:rw" "$T16A_TOOL_IMAGE" \
        age --encrypt --recipient "$recipient_content" --output /work/payload.sql.gz.age /work/payload.sql.gz
fi
[[ -s "$cipher" ]] || die "密文为空；未发布任何产物"
# 只做格式与摘要校验：没有私钥时无法也不应解密验证内容。
header="$(head -c 21 "$cipher" | tr -d '\0')"
[[ "$header" == "age-encryption.org/v1" ]] || die "密文头部不符合 age v1 格式；未发布任何产物"
cipher_sha256="$(sha256_file "$cipher")"
cipher_size="$(wc -c < "$cipher" | tr -d ' ')"

# 用同一暂存目录完成写入与摘要，最后才逐个原子改名发布。
printf '%s  %s\n' "$cipher_sha256" "$label.sql.gz.age" > "$staging/payload.sha256"
{
    printf '{\n'
    printf '  "schema": "t16a-backup/v1",\n'
    printf '  "artifact": "%s",\n' "$label.sql.gz.age"
    printf '  "sha256": "%s",\n' "$cipher_sha256"
    printf '  "size_bytes": %s,\n' "$cipher_size"
    printf '  "source_project": "%s",\n' "$T16A_SOURCE_PROJECT"
    printf '  "source_db": "%s",\n' "$T16A_SOURCE_DB"
    printf '  "mysql_version": "%s",\n' "$mysql_version"
    printf '  "table_count": %s,\n' "$tables"
    printf '  "consistency": "mysqldump --single-transaction --quick",\n'
    printf '  "encryption": "age v1.2.1 (X25519 recipient)",\n'
    printf '  "dump_started_at": "%s",\n' "$dump_started_at"
    printf '  "dump_finished_at": "%s",\n' "$dump_finished_at"
    printf '  "payload_gzip_valid": true,\n'
    printf '  "created_at": "%s"\n' "$(now_utc)"
    printf '}\n'
} > "$staging/payload.manifest.json"

# 清理明文再发布；任何一步失败都删除已放出的最终文件。
rm -f -- "$staging/payload.sql.gz"
published=()
publish_failed() {
    local f
    for f in ${published[@]+"${published[@]}"}; do rm -f -- "$artifacts_dir/$f"; done
    die "发布失败；已删除不完全产物"
}
mv -- "$cipher" "$artifacts_dir/$label.sql.gz.age" || publish_failed
published+=("$label.sql.gz.age")
mv -- "$staging/payload.sha256" "$artifacts_dir/$label.sha256" || publish_failed
published+=("$label.sha256")
mv -- "$staging/payload.manifest.json" "$artifacts_dir/$label.manifest.json" || publish_failed
published+=("$label.manifest.json")

printf 'BACKUP_OK\n'
printf 'artifact=%s\n' "$artifacts_dir/$label.sql.gz.age"
printf 'sha256=%s\n' "$cipher_sha256"
printf 'size_bytes=%s\n' "$cipher_size"
printf 'source_project=%s\n' "$T16A_SOURCE_PROJECT"
printf 'source_db=%s\n' "$T16A_SOURCE_DB"
printf 'table_count=%s\n' "$tables"
printf 'dump_started_at=%s\n' "$dump_started_at"
printf 'dump_finished_at=%s\n' "$dump_finished_at"
