# T16-B 最小健康与备份检查

本目录是 T16 工作线 B 的交付。B1 交付最小检查工具、unittest 和本说明；B2 在本机隔离范围内补做**可观察路径**验证（应用/数据库不可用、备份失败或过期、恢复信号），并修正一处会让测试随执行日期翻转的夹具缺陷。

范围到此为止：**不含容器编排、证书续期/握手、应用回退和告警投递**。仓库内没有告警发送器，也没有证书续期钩子；B2 不声称这些已验证。

状态：本地工具与合成夹具自测通过（32 项）；**未在目标部署或现场环境执行，未做真实备份运行，未对真实容器做故障注入，也没有任何生产资源操作**。`deploy/ops/` 其余目录不由本步创建。

## 文件

| 文件 | 作用 |
|---|---|
| `health_probe.py` | 只读检查工具：`health` 子命令核对 `GET /api/health/live`、`GET /api/health/ready`；`backup` 子命令核对备份存在性与新鲜度。 |
| `test_health_probe.py` | 32 个 unittest，用本地临时端口和临时文件做合成夹具，不联网、不用 Docker。 |
| `README.md` | 本说明。 |

只用 Python 标准库（3.11+，实测 3.14.3），无第三方依赖，不需要安装宿主软件。

## 合同依据

- 健康端点见根 [AGENTS.md](../../../AGENTS.md)：`GET /api/health/live`、`GET /api/health/ready`。
- 实现见 `backend/internal/platform/health.go`：`live` 只报进程存活，返回 `200 {"status":"alive"}`；`ready` 在 3 秒预算内 ping 数据库，成功 `200 {"status":"ready"}`，失败 `503 {"status":"not_ready"}`，两者都带 `Cache-Control: no-store`。
- Nginx 只把这两个路径精确转发到 Go（`deploy/nginx/site.conf.template`），其他 `/health` 路径 404。
- 因此**判定必须同时满足状态码、`application/json`、`status` 取值三项**。HTML 200（SPA 兜底、代理错误页）一律判失败，不能当健康。

## 用法

健康检查（`--base-url` 只接受回环地址；不接受代理配置，不跟随重定向）：

```bash
python deploy/ops/health/health_probe.py health --base-url http://127.0.0.1:8080
```

输出每项一行 JSON，最后一行 `summary`。退出码：`0` 全部通过，`1` 有检查失败（应用/DB 故障），`2` 参数或目标不合法。故障演练核对 503 合同时加 `--expect-ready not_ready`。

备份检查：

```bash
python deploy/ops/health/health_probe.py backup --path /path/to/backup.tar.gz --max-age-hours 24
```

默认用备份文件 mtime 作为最后存档时间。若备份线另有状态文件，用 `--status-file` 指定，工具消费其 `ok` 与 `last_success_utc`（RFC3339、必须带时区，统一换算 UTC）：

```json
{"ok": true, "last_success_utc": "2026-11-20T00:30:00Z"}
```

`ok` 不是 `true`（上次运行失败）、字段缺失、时间不是合法带时区 RFC3339、时间在未来、超过 `--max-age-hours`，均判失败。备份文件为 0 字节、路径是目录、`stat` 期间被删除或不可访问，同样判失败而不抛异常。A 工作线已交付 `deploy/ops/backup/` 的备份与恢复工具，但其输出**尚未生成这种状态文件**；本步只用可独立注入的本地夹具验证判定逻辑，不依赖 A 的实际产物。两者对接时需按 A 的真实输出对齐字段。

`--timeout` 与 `--max-age-hours` 必须是大于 0 的有限数值；`nan`、`inf`、`-inf` 等值会被显式拒绝（退出码 2），不会因为 `nan` 参与比较恒为假而被当成合法值。

输出交换约定：每行 JSON 的 `detail` 只写检查结论与已知类别。不原样回显服务端可控内容（未知 `status` 取值只报实际类型，未知 `Content-Type` 归为「非白名单类型」，白名单类型如 `text/html` 才按类别显示）与调用方文件名/路径；`ok` 字段异常时只报其类型。这样证据可以贴进报告而不带入业务或主机信息。

## 自测

```bash
python -B -m unittest discover -s deploy/ops/health -p 'test_*.py'
```

实测结果：`python -B -m unittest discover -s deploy/ops/health -p 'test_*.py'` → `Ran 32 tests ... OK`，退出码 0（`-B` 不写 `__pycache__`，仓库保持干净）。覆盖：

- live/ready 正常路径通过，detail 固定为 `200 application/json status=alive|ready`。
- HTML 200、`text/plain`、空体、半截 JSON、`status` 取值错误、500、超长响应体，逐个判失败并给出原因。
- ready 的 503 `not_ready` 合同可正向核对（DB 故障区分点）；代理 502 HTML 与代理透传的 JSON 503 可区分，前者判应用/代理不可用，后者仍按结构化合同核对。
- 连接失败返回失败结果而不抛异常；非回环目标、带凭据 URL、非 http(s) 被拒绝。
- 回显约束：注入标记的未知 `status` 取值、未知 `Content-Type`、状态文件 `ok` 取值、带标记的备份/状态文件名，均不出现在任何 `detail` 或整行 JSON 中。
- 参数边界：`nan`/`inf`/`-inf`/`0`/负数 的 `--timeout` 与 `--max-age-hours` 一律拒绝（退出码 2）。
- 备份：文件缺失、0 字节、路径是目录、`stat` 抛 `FileNotFoundError`/`PermissionError`、mtime 过期、状态文件过期、`ok=false`、状态文件缺失/非 JSON/顶层非对象/缺字段/无时区/非法时间、未来时间，均判失败；新鲜文件通过。
- CLI 退出码 0 / 1 / 2。
- **B2 新增的故障→恢复可观察路径**（5 项）：应用不可用（代理 HTML 502）失败、恢复后同一目标通过；数据库不可用（ready 503 `not_ready`）失败、合同核对通过、恢复后 200 通过；应用进程未监听时连接失败、恢复监听后通过；备份 `ok=false` 失败、状态翻转为成功时通过；备份归档过期失败、新归档落盘后通过。

测试只用 `127.0.0.1` 临时端口和 `TemporaryDirectory`，不写仓库内文件，不需要固定端口。

## B2 改动

1. **修正夹具的时间依赖（真实缺陷）**：`BackupCheckTest` 用固定 `now = 2026-11-20T09:30Z`，但新建归档文件的 mtime 是执行时的墙钟时间。一旦执行时间晚于该固定值，`judge_backup` 会按「最后成功时间晚于当前时间，数据可疑」判失败，`test_fresh_archive_by_mtime_passes` 与 `test_archive_and_status_file_names_are_not_echoed` 随之失败。
   - 实测证据：在本次改动前的基线（HEAD `09da166`）原样执行同一命令，结果为 `Ran 27 tests ... FAILED (failures=2)`，失败原因正是「晚于当前时间」。
   - 修复：`setUp` 显式把夹具 mtime 设为固定 `now` 之前 1 小时，并加断言保证夹具 mtime 早于 `now`；另建归档的用例也显式设置 mtime，CLI 成功用例使用同一个固定时钟。
   - 这不是产品代码缺陷，判定逻辑本身正确（未来时间的确应判可疑）；错在夹具没有固定它依赖的时间输入。
2. **新增 5 项故障/恢复可观察路径测试**：覆盖 `T16_PLAN` B 线要求的「应用不可用、数据库不可用、备份失败或过期」以及对应的恢复信号。这些测试证明**探针能把故障态与恢复态判成两个稳定、可区分的信号**，不证明目标环境的告警或恢复流程已就绪。

## 未执行与缺口（如实登记）

- **告警投递未实现、不可验证**：仓库内没有告警发送器。`deploy/ops/health/`、`deploy/` 下检索不到 SMTP、webhook 或通知发送代码；`backend/internal/ai/trial_ledger.go` 里的 `alert io.Writer` 只输出试运行账本事件到日志/终端，是费用试运行的账本告警通道，不是运维监控的投递实现。因此「故障告警」与「恢复通知」**无法验证**，本工具只输出退出码与 JSON 结论，需由外部采集器决定是否告警。未伪造任何投递成功结果。
- **证书续期与 TLS 握手未验证**：本工具只支持 `http`/非 TLS 语义的目标判定，没有 `--cacert`、证书链/SAN/到期检查，也没有 `tls` 子命令。仓库内同样没有 ACME 客户端或续期钩子；`deploy/README.md` 第 4 节描述的手写钩子流程（原子替换证书 + `nginx -t` + `nginx -s reload`）未实现为本仓库脚本。即使后续用自签测试 CA 做完回环握手，那也只能算**探针能力验证**，不能称正式续期通过。本轮未做任何 TLS 握手。
- **无真实容器故障注入**：B2 全部判定证据来自合成 HTTP 服务与合成归档文件。未启动任何 Compose 项目，未停止真实容器，未验证 Nginx 透传、超时与 `internal` 网络行为。（本轮只用 `docker run --rm --network none` 做过一次性只读能力探测，容器随即销毁，不参与任何结论。）
- **应用回退**在 A2-R 已完成本地同卷验证。目标环境仍待核对回退镜像、数据卷及 Schema 兼容。
- 未在真实 Compose 项目、真实容器或目标主机上运行；未连接任何生产数据库、证书或模型。
- 备份状态文件格式仍是本地约定；A 工作线已交付备份/恢复工具，但其输出尚未生成该状态文件，对接时须按其真实输出对齐字段。
- 单测、CLI 冒烟与目标环境待办见 [T16 验证记录](../../../docs/acceptance/T16_OPERATIONS_VALIDATION.md)。
