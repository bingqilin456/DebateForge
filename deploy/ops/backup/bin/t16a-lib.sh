#!/usr/bin/env bash
# T16A 备份恢复共享库：固定资源名、通用断言与输出格式；不含任何密钥材料。
# 由 t16a-backup.sh / t16a-restore.sh source，不单独执行。
set -euo pipefail

# 固定资源标识。任何脚本都不得把项目名、库名、卷名从外部改成任意值。
T16A_SOURCE_PROJECT="t16a-backup-source"
T16A_RESTORE_PROJECT="t16a-backup-restore"
T16A_SOURCE_DB="siyige_t16a_source"
T16A_RESTORE_DB="siyige_t16a_restore"
T16A_MYSQL_IMAGE="mysql:8.4.13"
T16A_TOOL_IMAGE="${T16A_TOOL_IMAGE:-t16a-backup-tool:v1.2.1}"

log()  { printf '[t16a] %s\n' "$*" >&2; }
warn() { printf '[t16a][warn] %s\n' "$*" >&2; }
die()  { printf '[t16a][error] %s\n' "$*" >&2; exit "${T16A_EXIT_CODE:-1}"; }

now_utc() { date -u +%Y-%m-%dT%H:%M:%SZ; }

sha256_file() {
    sha256sum -- "$1" | awk '{print $1}'
}

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "缺少命令：$1"
}

# 只接受绝对路径，避免因工作目录变化把产物写到未核对的位置。
require_abs_path() {
    case "$1" in
        /*) : ;;
        *) die "路径必须是绝对路径：$1" ;;
    esac
}

# 项目名白名单：这是防误操作的第一道闸，不做“差不多”匹配。
assert_project() {
    local actual="$1" expected="$2"
    [[ "$actual" == "$expected" ]] || die "项目名被拒绝：只允许 $expected，实际为 $actual"
}

# 固定 Compose 项目与文件，忽略调用方环境里的 COMPOSE_* 覆盖。
compose_for() {
    local project="$1"; shift
    env -u COMPOSE_PROJECT_NAME -u COMPOSE_FILE -u COMPOSE_PROFILES \
        docker compose -p "$project" -f "$T16A_COMPOSE_FILE" "$@"
}

# 本机专用演练要求容器不发布宿主端口；发现端口即停止，不改动现场。
assert_no_published_ports() {
    local project="$1" ports
    ports="$(docker ps --filter "label=com.docker.compose.project=$project" --format '{{.Ports}}' 2>/dev/null | sed '/^[[:space:]]*$/d')"
    [[ -z "$ports" ]] || die "项目 $project 存在已发布宿主端口，拒绝继续：$ports"
}

assert_service_running() {
    local project="$1" service="$2" id
    id="$(compose_for "$project" ps -q "$service" 2>/dev/null || true)"
    [[ -n "$id" ]] || die "项目 $project 的服务 $service 未运行；先启动固定隔离环境"
}

# 执行容器内 SQL 客户端；--execute 的 SQL 由本目录脚本固定，不接受外部输入。
sql_exec() {
    local project="$1" role="$2" sql="$3"
    compose_for "$project" exec -T mysql bash /opt/t16a/sql/client.sh "$role" --execute="$sql"
}

# 校验实际容器确属目标项目，避免同名服务其实来自其他环境。
assert_container_project() {
    local project="$1" service="$2" actual
    actual="$(docker inspect --format '{{index .Config.Labels "com.docker.compose.project"}}' "$(compose_for "$project" ps -q "$service")" 2>/dev/null || true)"
    [[ "$actual" == "$project" ]] || die "容器项目标签不符：期望 $project，实际 ${actual:-空}"
}

table_count() {
    local project="$1"
    sql_exec "$project" migrator "SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=DATABASE() AND table_type='BASE TABLE'"
}
