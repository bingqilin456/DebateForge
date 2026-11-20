# T16-B1 最小健康与备份检查

本目录是 T16 工作线 B 的第一步，只交付一个最小检查工具、一份 unittest 和这份说明。范围到此为止：**不含容器编排、证书续期/握手、应用回退和告警投递**，这些由后续独立步骤处理，本步不得声称已验证。

状态：本地工具与合成夹具自测通过；**未在目标部署或现场环境执行，未做真实备份运行，也没有任何生产资源操作**。`deploy/ops/` 其余目录不存在，本步不创建。

## 文件

| 文件 | 作用 |
|---|---|
| `health_probe.py` | 只读检查工具：`health` 子命令核对 `GET /api/health/live`、`GET /api/health/ready`；`backup` 子命令核对备份存在性与新鲜度。 |
| `test_health_probe.py` | 27 个 unittest，用本地临时端口和临时文件做合成夹具，不联网、不用 Docker。 |
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

`ok` 不是 `true`（上次运行失败）、字段缺失、时间不是合法带时区 RFC3339、时间在未来、超过 `--max-age-hours`，均判失败。备份文件为 0 字节、路径是目录、`stat` 期间被删除或不可访问，同样判失败而不抛异常。A 工作线的真实备份实现尚未完成，本步只用这种**可独立注入的本地夹具**验证判定逻辑，不依赖 A。

`--timeout` 与 `--max-age-hours` 必须是大于 0 的有限数值；`nan`、`inf`、`-inf` 等值会被显式拒绝（退出码 2），不会因为 `nan` 参与比较恒为假而被当成合法值。

输出交换约定：每行 JSON 的 `detail` 只写检查结论与已知类别。不原样回显服务端可控内容（未知 `status` 取值只报实际类型，未知 `Content-Type` 归为「非白名单类型」，白名单类型如 `text/html` 才按类别显示）与调用方文件名/路径；`ok` 字段异常时只报其类型。这样证据可以贴进报告而不带入业务或主机信息。

## 自测

```bash
cd deploy/ops/health
python -m unittest test_health_probe -v
```

实测结果：`python -B -m unittest test_health_probe` → `Ran 27 tests ... OK`，退出码 0（`-B` 不写 `__pycache__`，仓库保持干净）。覆盖：

- live/ready 正常路径通过，detail 固定为 `200 application/json status=alive|ready`。
- HTML 200、`text/plain`、空体、半截 JSON、`status` 取值错误、500、超长响应体，逐个判失败并给出原因。
- ready 的 503 `not_ready` 合同可正向核对（DB 故障区分点）；代理 502 HTML 与代理透传的 JSON 503 可区分，前者判应用/代理不可用，后者仍按结构化合同核对。
- 连接失败返回失败结果而不抛异常；非回环目标、带凭据 URL、非 http(s) 被拒绝。
- 回显约束：注入标记的未知 `status` 取值、未知 `Content-Type`、状态文件 `ok` 取值、带标记的备份/状态文件名，均不出现在任何 `detail` 或整行 JSON 中。
- 参数边界：`nan`/`inf`/`-inf`/`0`/负数 的 `--timeout` 与 `--max-age-hours` 一律拒绝（退出码 2）。
- 备份：文件缺失、0 字节、路径是目录、`stat` 抛 `FileNotFoundError`/`PermissionError`、mtime 过期、状态文件过期、`ok=false`、状态文件缺失/非 JSON/顶层非对象/缺字段/无时区/非法时间、未来时间，均判失败；新鲜文件通过。
- CLI 退出码 0 / 1 / 2。

测试只用 `127.0.0.1` 临时端口和 `TemporaryDirectory`，不写仓库内文件，不需要固定端口。

另有一次真实 CLI 冒烟（子进程调用工具本体，服务端为临时合成的回环 JSON 服务，不是产品应用）：

| 场景 | 命令要点 | 退出码 | 输出要点 |
|---|---|---|---|
| 正常 | `health --base-url http://127.0.0.1:<临时端口>` | 0 | `200 application/json status=alive` / `status=ready`，`summary ok` |
| 非回环 | `health --base-url http://192.0.2.10:8080` | 2 | `仅允许回环目标，拒绝 192.0.2.10` |
| 备份缺失 | `backup --path <不存在> --max-age-hours 24` | 1 | `备份文件不存在` |
| 备份过期 | 归档 mtime 置为 50 小时前 | 1 | `备份过期：……50.00 小时前，超过上限 24 小时` |
| 备份新鲜 | 归档刚写入 | 0 | `备份未过期：……0.00 小时前` |
| 备份为空 | 0 字节文件 | 1 | `备份文件为空（0 字节），不算有效备份` |
| 非法超时 | `--timeout=nan` | 2 | `--timeout 必须为大于 0 的有限数值` |
| 非法年龄 | `--max-age-hours=inf` | 2 | `--max-age-hours 必须为大于 0 的有限数值` |

## 未执行与缺口

- **传输层未验证**：以上全部是 `127.0.0.1` 上的合成 JSON 服务，未经过 Nginx，未验证代理透传、超时、缓冲或 HTTP/2 行为。
- **TLS 未验证**：未对 `https://` 目标做任何实际握手；`--cacert`/测试 CA 尚无支持，证书链、SAN、到期日与续期均未检查。
- **应用回退未验证**：未核对回退镜像标识、同卷保持、Schema 兼容硬停止，本工具没有回退路径。
- 未在真实 Compose 项目、真实容器或目标主机上运行；应用不可用、数据库不可用两类**故障注入演练尚未执行**，当前只有合成夹具证据。
- 未验证告警交付失败与恢复通知（后续步骤）。
- 备份状态文件格式是本地约定，A 工作线落地后需与其实际输出对齐。
- 未提交、未推送；未创建任何容器、卷、网络或固定端口监听。本步唯一的 Docker 副作用是本会话此前（B1 开始前）构建的本地镜像标签 `t16b-app:1d974c9`，无容器引用，未删除。
