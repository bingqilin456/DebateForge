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
# Git Bash/MSYS 会把 /opt/t16a/... 这类容器内绝对路径改写成 Windows 路径，导致
# exec 找不到容器内脚本；这里只对 compose 调用关闭该改写，并把 compose 文件路径
# 换成 docker CLI 能识别的 Windows 形式（非 MSYS 环境 cygpath 不存在，行为不变）。
compose_for() {
    local project="$1"; shift
    local compose_file="$T16A_COMPOSE_FILE"
    if command -v cygpath >/dev/null 2>&1; then
        compose_file="$(cygpath -w -- "$compose_file" 2>/dev/null || printf '%s' "$compose_file")"
    fi
    MSYS_NO_PATHCONV=1 env -u COMPOSE_PROJECT_NAME -u COMPOSE_FILE -u COMPOSE_PROFILES \
        docker compose -p "$project" -f "$compose_file" "$@"
}

# 本机专用演练要求容器不发布宿主端口；发现端口即停止，不改动现场。
# 只看 HostConfig.PortBindings：docker ps 的 {{.Ports}} 对仅 EXPOSE、未发布的端口也会
# 打印 "3306/tcp"，据此判空会把标准 mysql 镜像误判成“已发布宿主端口”而永远无法备份。
# normalize_published_ports 只做空白归一，便于在无 Docker 的静态测试里直接验证判定口径。
normalize_published_ports() {
    tr -s '[:space:]' ' ' | sed -e 's/^ //' -e 's/ $//'
}

# 查询失败必须 fail-closed：查不到结果等于“无法证明未发布宿主端口”，此时若当成空结果
# 继续执行，就把隔离保证建立在错误状态上（例如 Docker 不可用时备份照跑）。因此 ps 或
# inspect 非零退出时一律拒绝，不再用 `|| true` 把失败吞成空输出。
assert_no_published_ports() {
    local project="$1" names name bound raw=""
    if ! names="$(docker ps --filter "label=com.docker.compose.project=$project" --format '{{.Names}}' 2>/dev/null)"; then
        die "无法查询项目 $project 的容器（docker ps 失败）；不能确认宿主端口未发布，拒绝继续"
    fi
    [[ -n "$names" ]] || return 0
    while IFS= read -r name; do
        [[ -n "$name" ]] || continue
        if ! bound="$(docker inspect --format '{{range $port, $bindings := .HostConfig.PortBindings}}{{if $bindings}}{{$port}} {{end}}{{end}}' "$name" 2>/dev/null)"; then
            die "无法读取容器 $name 的端口绑定（docker inspect 失败）；不能确认宿主端口未发布，拒绝继续"
        fi
        raw="$raw $bound"
    done <<< "$names"
    raw="$(printf '%s' "$raw" | normalize_published_ports)"
    [[ -z "$raw" ]] || die "项目 $project 存在已发布宿主端口，拒绝继续：$raw"
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

# 暂存目录名必须带进程号：同一个 label 的并发备份若共用暂存目录，会互相删除明文或错发产物。
# label 已由调用方按白名单校验，这里只拼接目录名，不接受额外路径片段。
staging_dir() {
    printf '%s/.staging.%s.%s\n' "$1" "$2" "$$"
}
