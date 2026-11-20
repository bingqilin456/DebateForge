#!/usr/bin/env bash
# T16A 恢复：只恢复到固定独立空目标项目；先验证密钥与完整副本，再导入。
# 拒绝源目标重合、非空目标、损坏密文与错误密钥；不触碰源库、不删除卷。
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=./t16a-lib.sh
. "$script_dir/t16a-lib.sh"

usage() {
    cat <<'EOF'
用法：t16a-restore.sh --artifact <密文绝对路径> --identity-file <私钥绝对路径>
                      [--project t16a-backup-restore] [--confirm-empty-target] [--help]

把备份密文恢复进固定独立目标项目 t16a-backup-restore。恢复前依次校验：
密文摘要、age 解密、gzip 完整性、目标库为空、目标与源不重合。

  --artifact          <label>.sql.gz.age 的绝对路径
  --identity-file     age 私钥文件（绝对路径，只允许恢复端持有）
  --project           只接受 t16a-backup-restore（默认值；其他值一律拒绝）
  --confirm-empty-target  显式确认“目标库必须为空”的二次确认
  --help              显示本帮助

拒绝条件（任一命中即停止，目标库不被写入）：
  - 项目不是 t16a-backup-restore，或与清单记录的源项目相同（源目标重合）
  - 密文摘要缺失或不匹配、age 解密失败、gzip 校验失败（损坏或错误密钥）
  - 私钥文件与密文位于同一目录（要求密钥与密文分离）
  - 目标库非空，或未显式给出 --confirm-empty-target

恢复成功后输出 RTO 边界与关键表行数；不做生产恢复、不删除卷。
EOF
}

die_usage() {
    printf '[t16a][error] %s\n' "$*" >&2
    printf '使用 --help 查看用法。\n' >&2
    exit 2
}

artifact="" identity_file="" project="$T16A_RESTORE_PROJECT" confirm_empty=0
while [[ $# -gt 0 ]]; do
    case "$1" in
        --help|-h) usage; exit 0 ;;
        --artifact) [[ $# -ge 2 ]] || die_usage "--artifact 缺少值"; artifact="$2"; shift 2 ;;
        --identity-file) [[ $# -ge 2 ]] || die_usage "--identity-file 缺少值"; identity_file="$2"; shift 2 ;;
        --project) [[ $# -ge 2 ]] || die_usage "--project 缺少值"; project="$2"; shift 2 ;;
        --confirm-empty-target) confirm_empty=1; shift ;;
        *) die_usage "未知参数：$1" ;;
    esac
done

# 项目名先校验：非白名单（含 iso 源项目）直接拒绝，避免任何写入发生。
assert_project "$project" "$T16A_RESTORE_PROJECT"
[[ -n "$artifact" ]] || die_usage "必须提供 --artifact"
[[ -n "$identity_file" ]] || die_usage "必须提供 --identity-file"
require_abs_path "$artifact"
require_abs_path "$identity_file"
[[ -f "$artifact" ]] || die "密文不存在：$artifact"
[[ "$artifact" == *.sql.gz.age ]] || die "拒绝：--artifact 必须是 .sql.gz.age 密文"
[[ -f "$identity_file" ]] || die "私钥文件不存在：$identity_file"

artifact_dir="$(cd -- "$(dirname -- "$artifact")" && pwd)"
identity_dir="$(cd -- "$(dirname -- "$identity_file")" && pwd)"
[[ "$artifact_dir" != "$identity_dir" ]] || die "拒绝：私钥与密文位于同一目录，要求密钥与密文分离"

# age-keygen 默认在私钥前写 "# created:" 与 "# public key:" 注释行；
# 只取第一条 AGE-SECRET-KEY-1 行，避免把注释拼进私钥而导致误判。
key_content="$(grep -m1 '^AGE-SECRET-KEY-1' "$identity_file" | tr -d '\r\n' || true)"
case "$key_content" in
    AGE-SECRET-KEY-1*) : ;;
    *) die "拒绝：--identity-file 不是 age 私钥（应以 AGE-SECRET-KEY-1 开头）" ;;
esac
if command -v stat >/dev/null 2>&1; then
    key_mode="$(stat -c '%a' "$identity_file" 2>/dev/null || true)"
    case "$key_mode" in
        ''|600|400) : ;;
        *) warn "私钥文件权限为 $key_mode，建议收紧到 0600" ;;
    esac
fi

# 摘要文件与备份脚本的发布名一致：<label>.sha256（而不是 <label>.sql.gz.sha256）。
sha_file="${artifact%.sql.gz.age}.sha256"
manifest_file="${artifact%.sql.gz.age}.manifest.json"
[[ -f "$sha_file" ]] || die "缺少摘要文件：$sha_file"
[[ -f "$manifest_file" ]] || die "缺少清单文件：$manifest_file"

# 1) 摘要校验：先判断“副本是否完整”，此时尚未解密、未连接数据库。
expected_sha="$(awk '{print $1; exit}' "$sha_file")"
[[ "$expected_sha" =~ ^[0-9a-f]{64}$ ]] || die "拒绝：摘要文件格式非法"
actual_sha="$(sha256_file "$artifact")"
if [[ "$actual_sha" != "$expected_sha" ]]; then
    die "拒绝：密文摘要不匹配（副本损坏或被篡改），未写入目标库"
fi
log "密文摘要校验通过：$actual_sha"

manifest_field() {
    sed -n "s/.*\"$1\"[[:space:]]*:[[:space:]]*\"\([^\"]*\)\".*/\1/p" "$manifest_file" | head -n1
}
manifest_source_project="$(manifest_field source_project)"
manifest_source_db="$(manifest_field source_db)"
[[ -n "$manifest_source_project" ]] || die "拒绝：清单缺少 source_project"
[[ "$manifest_source_project" != "$project" ]] || die "拒绝：源目标重合（清单源项目与目标项目相同）"
[[ "$(manifest_field artifact)" == "$(basename -- "$artifact")" ]] || die "拒绝：清单记录的文件名与密文不一致"

# 2) 目标环境：固定项目、固定库、无宿主端口、库必须为空。
require_cmd docker
T16A_COMPOSE_FILE="${T16A_COMPOSE_FILE:-$script_dir/../compose.yaml}"
[[ -f "$T16A_COMPOSE_FILE" ]] || die "找不到 compose 文件：$T16A_COMPOSE_FILE"
assert_service_running "$project" mysql
assert_container_project "$project" mysql
assert_no_published_ports "$project"
target_db="$(sql_exec "$project" migrator 'SELECT DATABASE()' | head -n1)"
[[ "$target_db" == "$T16A_RESTORE_DB" ]] || die "拒绝：目标库为 $target_db，期望 $T16A_RESTORE_DB"
[[ "$(manifest_field source_db)" != "$target_db" ]] || die "拒绝：源库与目标库相同"

existing_tables="$(table_count "$project" | head -n1)"
[[ "$existing_tables" =~ ^[0-9]+$ ]] || die "无法读取目标库表数，停止"
if [[ "$existing_tables" != "0" ]]; then
    die "拒绝：目标库非空（$existing_tables 张表），恢复只允许空目标"
fi
[[ "$confirm_empty" == "1" ]] || die "拒绝：缺少 --confirm-empty-target，未执行恢复"

# 3) 先验证完整副本：解密到目标端私有暂存，失败则目标库保持原样。
restore_started_at="$(now_utc)"
staging="$(mktemp -d "${TMPDIR:-/tmp}/t16a-restore.XXXXXX")"
cleanup() { rm -rf -- "$staging"; }
trap cleanup EXIT
chmod 700 "$staging" 2>/dev/null || true
payload="$staging/payload.sql.gz"
if command -v age >/dev/null 2>&1; then
    age --decrypt --identity "$identity_file" --output "$payload" "$artifact" || die "拒绝：解密失败（密钥错误或密文损坏），未写入目标库"
else
    warn "主机缺少 age，改用工具镜像 $T16A_TOOL_IMAGE（密文与私钥分别只读挂载）"
    docker run --rm \
        -v "$artifact_dir:/artifact:ro" \
        -v "$identity_dir:/keys:ro" \
        -v "$staging:/work:rw" \
        "$T16A_TOOL_IMAGE" \
        age --decrypt --identity "/keys/$(basename -- "$identity_file")" \
            --output /work/payload.sql.gz "/artifact/$(basename -- "$artifact")" \
        || die "拒绝：解密失败（密钥错误或密文损坏），未写入目标库"
fi
[[ -s "$payload" ]] || die "拒绝：解密结果为空，未写入目标库"
gzip -t "$payload" 2>/dev/null || die "拒绝：解密后 gzip 校验失败（副本不完整），未写入目标库"
# 明文仍留在暂存目录，导入后由 trap 删除。
import_started_at="$(now_utc)"

# 4) 导入：复用固定容器内客户端，失败即停止并保留目标库现状。
compose_for "$project" exec -T mysql bash /opt/t16a/sql/import.sh < "$payload" || die "导入失败；目标库可能处于部分写入状态，须人工核对"
import_finished_at="$(now_utc)"

restored_tables="$(table_count "$project" | head -n1)"
[[ "$restored_tables" =~ ^[0-9]+$ ]] || die "恢复后无法读取表数"
[[ "$restored_tables" -gt 0 ]] || die "恢复后表数为 0，判定失败"
row_counts="$(sql_exec "$project" migrator "SELECT 'users', COUNT(*) FROM users UNION ALL SELECT 'debates', COUNT(*) FROM debates UNION ALL SELECT 'messages', COUNT(*) FROM messages" 2>/dev/null || echo 'row_counts=unavailable')"
finished_at="$(now_utc)"

printf 'RESTORE_OK\n'
printf 'project=%s\n' "$project"
printf 'target_db=%s\n' "$target_db"
printf 'artifact=%s\n' "$(basename -- "$artifact")"
printf 'artifact_sha256=%s\n' "$actual_sha"
printf 'manifest_source_project=%s\n' "$manifest_source_project"
printf 'tables=%s\n' "$restored_tables"
printf 'row_counts=|%s\n' "$(printf '%s' "$row_counts" | tr '\n' ';')"
printf 'restore_started_at=%s\n' "$restore_started_at"
printf 'import_started_at=%s\n' "$import_started_at"
printf 'import_finished_at=%s\n' "$import_finished_at"
printf 'restore_finished_at=%s\n' "$finished_at"
