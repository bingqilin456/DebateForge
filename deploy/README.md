# T07 单机部署准备

本配置用于后续获准的 Linux 单机隔离实跑和首次受控发布。默认只有 Nginx 通过宿主机回环 80/443 可达。Go 位于专用代理网与数据库网，MySQL 和迁移容器只在数据库网。两个后端网络均为内部网络。应用只允许 `disabled` 或 `fake`，配置不提供供应商密钥或模型出网。先获准在隔离环境使用 Fake 验证部署与 HTTPS。真实 DeepSeek 调用和公网开放分别审查并授权。真实模型公网试运行须先通过 [T07-COST-01](../docs/planning/TASKS.md#t07-cost-01-供应商硬预算缺失) 的费用控制验收及独立审查，审查网络与模型配置，取得负责人单独授权。

Compose、Nginx、MySQL 沿用 [ADR-008](../docs/decisions/DECISIONS.md#adr-008-单机-compose-部署与基本运维)。Go 与 Node 构建镜像沿用项目工具链版本。Go 运行镜像使用固定版本的 Alpine 和自带 BusyBox 提供最小运行环境及健康探针；也可在后续评估较大的 Debian slim。官方镜像许可随内容分发，Nginx 为 BSD-2-Clause，MySQL Community 为 GPLv2，Go 为 BSD-3-Clause，Node.js 为 MIT，Alpine 基础组件按包授权（含 BusyBox GPLv2）。无新增应用依赖。 [Docker 官方镜像](https://hub.docker.com/search?image_filter=official) · [Alpine 组件许可](https://pkgs.alpinelinux.org/)

## 1. 获准后准备隔离环境

在独立 Linux 主机或 VM 的仓库根目录使用 Bash。准备 Docker Engine ≥28 和 Compose v2 或以上插件。以下权限以 rootful Docker、未启用 UID remapping 为前提。确认回环 80/443 可用、`172.30.70.0/28` 与宿主机/现有 Docker 网段无重叠、Docker 未给内部网络配置外部直路由、没有其他容器加入两个内部网络。动态分配范围 `172.30.70.8/29` 避开 Nginx 固定地址 `172.30.70.2`。两套 T07 项目应在不同主机运行，避免网段冲突。首次部署使用全新隔离数据库与独立命名卷，禁止复用 T02 开发或测试数据库、凭据及卷。 [T02 dev/test](../infra/mysql/README.md) · [Docker 端口绑定](https://docs.docker.com/engine/network/port-publishing/) · [Docker 地址分配](https://docs.docker.com/reference/cli/docker/network/connect/#network-implications-of-stopping-pausing-or-restarting-containers)

1. 将 `deploy/.env.example` 复制成受限的 `deploy/.env`（`chmod 0600`）。设置 `T07_RELEASE` 为本次已审查的不可复用标识（例如提交 SHA），`T07_SITE_HOST` 为证书 SAN 中的小写 DNS 名，`T07_AI_MODE=fake` 仅用于获准的无收费联调。Compose 变量以当前 Shell 覆盖 env 文件，先排除旧的 `T07_*` 覆盖。
2. 在私有目录（如 `deploy/secrets/db`，宿主机权限 `0700`）准备三个独立的 64 位小写十六进制随机口令文件 `root.txt`、`app.txt`、`migrator.txt`。新环境可用 `(umask 077; set -C; openssl rand -hex 32 > "$file")` 分别创建，`file` 指向每个尚不存在的文件。私有目录内这三个文件可设 `0444`，便于 Docker 通过单文件 secrets 挂载给各自的非 root 用户。宿主机依靠私有父目录限制读取，不能把这些文件移到可遍历目录。现有卷需使用当前有效凭据；换文件不会自动更改数据库账户。 [Compose 文件 Secrets](https://docs.docker.com/compose/how-tos/use-secrets/)
3. 将 `T07_TLS_DIR` 置于宿主机 `0700 root:root` 私有父目录下，例如 `deploy/secrets/tls` 的父目录 `deploy/secrets`。只挂载 TLS 子目录，子目录用 `0750 root:101`，`fullchain.pem` 和 `privkey.pem` 用 `0640 root:101`，容器 UID/GID 101 只读访问。验收需确认容器能读取证书，宿主机非授权账号即使在 GID 101 也无法穿过私有父目录。隔离验收使用测试 CA 签发的证书，在验收浏览器中信任该 CA，curl 显式使用 CA 文件。用验收机器 hosts 或 curl `--resolve` 将同一域名指向回环。TLS、浏览器 Origin 和 Compose 的 `https://<域名>` 必须一致。

普通用户密码当前仍按 T03 注册与登录契约校验 12–128 个 Unicode 码点且最多 512 个 UTF-8 字节，保持原文。普通用户产品政策的差异归 T03-AUTH-01。数据库账户和服务端密钥继续使用各自的高强度随机生成与凭据管理规则，API Key 仅在另获真实调用授权时按供应商规则管理。

以下命令使用明确的隔离项目名。更换目标要重新核对 Docker context、项目名、卷、凭据路径、域名与发布标识。使用 `config --quiet` 校验变量，避免将包含宿主机路径的完整配置发到日志。

```bash
set -e
t07=(docker compose --env-file deploy/.env -f deploy/compose.yaml -p siyige-t07-lab)
"${t07[@]}" --profile migration config --quiet
"${t07[@]}" --profile migration build edge app migrate
"${t07[@]}" up -d --wait --wait-timeout 200 mysql
```

## 2. 显式受审迁移

在目标 MySQL 健康后、启动应用前执行。已有环境先停止写入并取得经核对的备份，初次空库登记为空库。核对迁移镜像与发布标识、SQL 00001–00010、应用授权 SQL、目标项目/卷及数据库账号。首次 `status` 可能创建 Goose 版本表和版本 0，属于数据库写入。Docker 初始化只在新卷创建数据库与账户，表和种子由迁移工具显式写入。迁移成功后，管理员在目标 MySQL 容器内以 socket root 显式执行逐表授权，Go 只持有应用账户的秘密。迁移和授权须覆盖在本次目标库的写入授权中。

```bash
"${t07[@]}" run --rm --no-deps migrate status
"${t07[@]}" run --rm --no-deps migrate up
"${t07[@]}" run --rm --no-deps migrate status
"${t07[@]}" exec -T mysql bash -c 'exec bash /opt/siyige/mysql-client.sh root < /opt/siyige/grant-app.sql'
"${t07[@]}" exec -T mysql bash /opt/siyige/mysql-client.sh app --execute='SELECT COUNT(*) FROM topics'
```

部署目标固定为内部 `mysql:3306/siyige_deploy` 与 `siyige_deploy_migrator`，本机 `dev/test` 目标仍各自固定。CLI 在同一会话核对实际库名、账户、MySQL 版本并取锁，沿用历史检查与前向迁移。全新库预期 10 个版本全部 applied、5 条题目；隔离首次验证时再次显式执行 `up` 应为 `applied=0`。再用 app 客户端执行 `UPDATE goose_db_version SET is_applied=is_applied WHERE 1=0`，预期被权限拒绝（1142），即使误获授权也不会更新行。管理员核对 `SHOW GRANTS FOR 'siyige_deploy_app'@'%'` 仅列业务表权限。初始化、迁移或授权失败即暂停发布，保留目标卷和证据，按现有恢复手册核对结构、历史和锁。 [迁移与失败处理](../backend/migrations/README.md#失败重跑与恢复)

## 3. 首次受控发布与回退

先通过隔离清单并取得对应目标的发布授权。在目标上保留经审批且可回退的镜像版本，记录镜像 ID 或 registry digest，保持同一 Compose 项目名和命名卷。初次空库按上节处理。升级已有站点时先停止接入及写入，验证备份完整性和可用性后，再执行单独获准的迁移。确认旧应用与目标 Schema 兼容后才将旧镜像视为可回退版本。完成迁移后执行：

```bash
"${t07[@]}" up -d --no-deps --no-build --pull never --wait --wait-timeout 90 app edge
"${t07[@]}" exec -T edge nginx -t
"${t07[@]}" ps
```

随后执行健康检查与首轮冒烟。容器 app 健康只看 live，Nginx 容器健康只看自身，MySQL 用迁移账户执行只读 SELECT 1，使应用授权前也能探测。必须经 HTTPS 单独验证 ready、迁移状态和业务。发布失败后，先取得负责人对相应操作的单独授权，再进入维护窗口。按获批范围切换应用镜像，或在无可用上一版时停止服务。开始前停止外部接入，保持同一 Docker context、Compose 项目和命名卷，冻结镜像重打标和其他发布操作。确认上一版兼容目标 Schema。从批准记录设置 `previous`（发布标识）、`approved_app_id`、`approved_edge_id`（两个本地镜像完整的 `sha256:…` ID）。若批准记录使用 registry digest，先核对它与本地镜像的对应关系并记录完整本地 ID。在修改发布配置前逐个核对标签实际指向的 ID：

```bash
set -e
: "${previous:?set approved release}" "${approved_app_id:?set approved app image ID}" "${approved_edge_id:?set approved edge image ID}"
verify_rollback_tags() {
  local app_id edge_id
  app_id="$(docker image inspect --format '{{.Id}}' "siyige-app:$previous")" &&
    edge_id="$(docker image inspect --format '{{.Id}}' "siyige-edge:$previous")" &&
    test "$app_id" = "$approved_app_id" &&
    test "$edge_id" = "$approved_edge_id"
}
verify_rollback_tags
```

核对通过后，将本项目实际使用的 `deploy/.env` 中 `T07_RELEASE` 持久修改为 `$previous`，文件保持 0600。在同一维护会话执行以下命令。任一检查失败即暂停，保持外部接入关闭并保留证据。

```bash
unset T07_RELEASE
rollback_images="$("${t07[@]}" config --images app edge)"
printf '%s\n' "$rollback_images" | grep -Fx "siyige-app:$previous"
printf '%s\n' "$rollback_images" | grep -Fx "siyige-edge:$previous"
verify_rollback_tags
"${t07[@]}" up -d --no-deps --no-build --pull never --wait --wait-timeout 90 app edge

verify_rollback_containers() {
  local app_container edge_container app_id edge_id
  app_container="$("${t07[@]}" ps -q app)" &&
    edge_container="$("${t07[@]}" ps -q edge)" &&
    test -n "$app_container" && test -n "$edge_container" &&
    app_id="$(docker inspect --format '{{.Image}}' "$app_container")" &&
    edge_id="$(docker inspect --format '{{.Image}}' "$edge_container")" &&
    test "$app_id" = "$approved_app_id" &&
    test "$edge_id" = "$approved_edge_id"
}
verify_rollback_containers
```

首发无上一版或 Schema 不兼容时，执行 `"${t07[@]}" stop edge app` 并保留卷和证据，由负责人决定后续恢复。切换且核对运行镜像后，重跑健康和本人详情验证。再执行一次相同的常规 `up`，重跑 `verify_rollback_containers`。验证通过并获准后恢复接入。回退操作只切换应用镜像，原卷保留，迁移与数据库恢复走各自的授权和手册。服务器秘密文件、TLS 证书、加密异机备份及独立库恢复演练纳入后续运维计划。

## 4. HTTPS 续期检查

隔离证书按测试 CA 重新签发。正式证书由主机的自动化 ACME 客户端以 DNS-01 续期，服务进程只读取证书文件；DNS 操作、证书签发和续期验证各按获准范围执行。自动续期的部署钩子将新证书链及私钥以同目录临时文件写入并分别原子替换，保留原权限与可用副本；两个文件都就绪后，使用固定的 Compose 项目和配置执行：

```bash
"${t07[@]}" exec -T edge nginx -t &&
"${t07[@]}" exec -T edge nginx -s reload
```

在续期演练中确认钩子执行、reload 成功，独立连接看到的新证书 SAN、链、指纹、有效期与预期一致。配置或证书校验失败时先恢复成对文件，保留现有进程并检查到期告警。HTTP 仅固定跳转到配置域名的 HTTPS，ACME 凭据不挂进服务。 [Nginx HTTPS 配置](https://nginx.org/en/docs/http/configuring_https_servers.html)

## 5. 隔离验收清单

以下探针在前面配置的隔离主机上执行。将 `site`、`ca_file` 设为实际域名与测试 CA 绝对路径。响应或截图证据须移除 Cookie、CSRF、口令和业务私密内容。

```bash
tls=(curl --silent --show-error --noproxy '*' --cacert "$ca_file" --resolve "$site:443:127.0.0.1")
probe="$(mktemp)"
trap 'rm -f -- "$probe"' EXIT
health() {
  local meta
  meta="$("${tls[@]}" -o "$probe" -w '%{http_code} %{content_type}' "https://$site/api/health/$1")" || return 1
  [[ "$meta" == "$2 application/json"* ]] && jq -e --arg value "$3" '.status == $value' "$probe"
}

# 正常时，经 Nginx 检查状态码、Content-Type 与 JSON
health live 200 alive
health ready 200 ready
for path in /api /api/unknown /api/health/unknown /health /health/live /api/../ /health/%2e%2e/; do
  code="$("${tls[@]}" --path-as-is -o /dev/null -w '%{http_code}' "https://$site$path")"
  [[ "$code" == 404 ]]
done

# 只在隔离项目故障演练中执行，恢复后等待 ready 返回 200
"${t07[@]}" stop mysql
health live 200 alive
health ready 503 not_ready
"${t07[@]}" start mysql
# 等待 MySQL 健康，再执行 health ready 200 ready

"${t07[@]}" stop app
for path in /api/health/live /api/health/ready /api/v1/topics; do
  if code="$("${tls[@]}" --max-time 8 -o /dev/null -w '%{http_code}' "https://$site$path")"; then
    [[ "$code" == 502 || "$code" == 503 || "$code" == 504 ]]
  fi
done
"${t07[@]}" start app
# 等待恢复后重复正常探针。失败中止时，核对并恢复本轮停止的服务。
```

- 检查 `curl --noproxy '*' --resolve "$site:80:127.0.0.1" -sSI "http://$site/api/health/live"` 返回 308 及同域 HTTPS Location。核对 HTTPS 证书链、SAN、到期日及浏览器无警告、无 Mixed Content。页面为 Vue，缺失静态资源返回 404，任何 API/健康请求都没有 SPA HTML 200。代理生成的故障响应可为非 2xx HTML，上游返回的 JSON 和状态须透传。
- **AC09 首轮子集。** 通过 HTTPS 完成注册、登录、退出和 `me` 恢复，选预设题与立场，创建会话、提交首轮并读取两条纯文本消息。保留 `__Host-siyige_session` 的 Secure、HttpOnly、SameSite=Lax、Path=/、无 Domain 检查。异 Origin、缺失或错误 CSRF 的写请求应 403，他人详情应 404。确认 Nginx 重写 `X-Real-IP`，从不同测试来源验 IP 额度独立，并用伪造 `X-Real-IP`/`X-Forwarded-For` 证明无法切换额度；登录用户名限流另验。
- 向注册入口发送 32 KiB 和 32 KiB + 1 字节的请求，分别以固定长度和 HTTP/1.1 分块发送，每次显式设置单个 `Origin: https://$site` 和 `Content-Type: application/json`。等于上限的请求交给 Go 校验；超限均须返回 JSON 413 `request_too_large`，有 `Cache-Control: no-store` 且 `X-Request-ID` 和正文一致。用空 JSON 对象后补空格，避免创建测试账号。超限探针带一个虚构的入站 `X-Request-ID`，确认响应使用 Nginx 生成的 ID。用 `"${t07[@]}" logs --since 2m --no-color edge` 查找同一 `request_id=<响应 ID>`，每个本地超限请求有一条含 413 的日志。放入虚构查询、Cookie、Authorization 和请求体标记，核对日志无这些标记。另在单独获准的临时隔离探针配置中把边缘上限调高、保持 Go 上限 32 KiB，保留同源 Origin 和 JSON Content-Type，用同样的超限请求触发 Go 413。核对上游状态、JSON、Content-Type 与 `X-Request-ID` 原样透传，且未进入本地 413 日志格式。另验发言 2000 码点、每用户生成 6 次/分钟与 20 次/日、单用户并发 1 与单进程全局并发 2，失败尝试也计数。
- 用独立获准的隔离 Fake 夹具验证超时、失败后不计完成轮及不重复落库，禁止靠真实模型刷限额。核对模型 45 秒、首轮请求 60 秒、Go socket 65 秒、Nginx 读取间隔 68 秒、浏览器 70 秒。Fake 冒烟覆盖代理和应用链路，真实 AI 回复、费用控制及公网 HTTPS 证据后续单列。
- **AC07 重启子集。** 记下首轮会话 ID、完成轮数与消息数，刷新后再退出重登，按原会话 hash 读取本人详情。依次重启 `app`、`mysql`，每次先等 ready 恢复，再确认同一会话仍为 1 轮、2 条消息且没有重新生成。核对 Compose 项目命名卷未更换。另对 Go 做一次受控重建，验证 Nginx 能重新解析 Go 容器地址。

完整三轮、总结和历史由后续业务阶段验收，独立备份恢复演练归 T16。实际授权、静态证据和待执行项见 [T07 验收记录](../docs/acceptance/T07_DEPLOYMENT_PREPARATION_ACCEPTANCE.md)。
