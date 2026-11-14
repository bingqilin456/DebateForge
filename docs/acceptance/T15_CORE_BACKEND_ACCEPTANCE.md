# T15-Core 后端技术验证与 B 交接

日期：2026-11-15（Asia/Shanghai）。分支 `codex/p3-a-integration`，A1/A2 基线 `66dd979132b6838ee732d499f7e1c75cf9f02fe9`，同一草稿 PR #19。本轮成员 A 明确授权本地开发、测试、Commit、普通 Push、更新草稿 PR；未合 main、未调用业务真实模型、未部署公网，未改 B 前端、依赖、迁移或 Phase 2/A2 功能。

## 前置与实现

已核对 [A1 T13-Lite 矩阵](../planning/PHASE3_A_INTEGRATION_SPEC.md#t13-lite-累计验收矩阵) 的技术通过证据及基线四项 CI。原负责人确认状态保留，本轮明确授权继续 T15，不把历史技术结论改成人工验收。[实施计划](../planning/T15_CORE_BACKEND_PLAN.md) 与冻结 API/OpenAPI 同步；事件载荷、JSON retry 和版本来源保持冻结契约。

Codex 负责规划、共享认领/HTTP/数据库关键路径、隔离集成与代理验证和最终审查；DSH 在同一 BridgeTask 完成 AI 适配/包装及审查修正。DSH 继承 `trae/deepseek-v4.1-flash/high`，早先选模失败未发送任务，可能影响其全局默认，已说明；未改认证或权限。DSH 调用仅为用户明确要求的开发协作，不是应用业务模型调用。

- 新端点复用普通 submit 的 `ValidateSubmit → Store.Claim → Gate → runTurnGeneration → Complete/Fail`，没有新认领方法、迁移或第二套幂等。90 秒租约、版本保护、6/分钟、20/日、每用户 1/全局 2 保持原实现。
- Session/Origin/CSRF/分钟限流、Accept/路径/正文与认领/生成额度均在首帧前。generating 同键返回 202 JSON；completed 重放 start/done，不调用模型；其他事件前失败保持普通错误 JSON。
- start 只有 turn_id/request_id；delta 只是临时文字；最终 Complete 成功才 done。提交结果不明只返回受控 error 并要求查询，不能宣称已失败落库。单一响应循环独占 Writer，工作协程传递事件；写/Flush 失败及 HTTP 断线取消上游，清理失败仍受原版本/状态保护。
- 锁定 Eino v0.7.13 + openai v0.1.13 实际 `Stream/Recv/Close`。观察层随读记录白名单事实，不整流缓存；缺尾帧、拒答、非法 UTF-8/角色/结束原因/工具调用、超限均受控失败，未知 usage 不记零。Fake/disabled/trial 显式适配，普通和流式共用 trial 预留及 settle；保留固定模型、两消息、输入/输出限额、窗口、停用、无自动重试等闸门。
- 真实 trial 多轮仍受既有两消息白名单限制，完整真实 Demo 未交付；取消只尽快停止上游，不能保证已发生费用消失，失败/未知预留保留。
- 模型 45 秒、请求 60 秒、租约 90 秒、心跳 15 秒。修正 Nginx 普通 `/api/` 前缀原有 `^~` 屏蔽 SSE 正则的问题，实际模板的 SSE location 已验证命中。

## 实际验证

环境：Windows、Go 1.27.1、GCC race；MySQL 8.4.13 固定容器 `siyige-t02-mysql-test-1`，`127.0.0.1:13307/siyige_test` / `siyige_test_app`。身份白名单由测试启动校验；仅启动既有测试容器，00001–00013 已 applied，无新迁移/播种或卷重建。凭据只传忽略目录文件路径，不入库。集成测试使用合成数据，并沿用既有固定身份校验及按确切测试用户清理；HTTP 探针创建随机合成账号和会话并退出登录，记录保留在测试库。

目标分支最终检查（均已实际执行）：

| 命令/检查 | 结果 |
| --- | --- |
| 清除两个集成开关、`AI_MODE=disabled`；`go test ./... -count=1`、`go vet ./...`、`go build ./...` | 全部通过（8 个包） |
| `go test -race ./internal/ai ./internal/debate ./internal/platform ./cmd/server -count=1` | 全部通过 |
| 显式固定测试库；`go test -race ./tests/integration -count=1` | 全量通过，28.961s，含 A2/Phase 2 及 T15 |
| Linux `golang:1.27.1`、断外网、只读代码/模块缓存、`go test -v -p 1 -race ./internal/ai -run 'TestTrialStream|TestTrialTransport|TestOpenAIStream|TestSSEObservationBody' -count=1` | 全部通过，含三条新增账本持久化用例，无跳过；race 1.496s |
| OpenAPI 3.1.1、YAML 重复键、17 个 implemented 操作 | 通过；使用已有本地校验库，未新增产品依赖 |
| 既有 CI 仓库静态/敏感内容门禁、51 份 Markdown 相对链接、46 处本轮文档锚点、触碰 Go 文件 gofmt、`git diff --check` | 全部通过 |

Linux 容器的前两次测试未执行到用例：默认 `/dev/shm` 空间不足导致构建失败；扩容后又因其 noexec 挂载不能执行测试二进制。最终改为 `--tmpfs /testtmp:rw,exec,size=1g` 与 `TMPDIR=/testtmp`；不将环境失败算作业务通过。该最终运行断开容器外网，仅使用合成供应商和本地测试文件。

| 专项 | 已执行结果 |
| --- | --- |
| T15 MySQL `go test -race ./tests/integration -run TestT15 -count=1` | 通过，含最新过期回收与事件前校验用例 |
| 并发与无双写 | 同键普通/流式竞争及 202/完成重放；仅一次模型、1 轮/2 消息/1 次计轮 |
| 断线及恢复 | 真实 HTTP delta 后断开，上游取消，详情定位原键；JSON retry 用查询版本；旧版本 409 不调用模型 |
| done 丢失 | 真实事务成功，下游 Writer 注入丢弃 done；详情 completed，同键重放不再生成 |
| 数据库失败 | Claim 提交确认丢失保持 JSON 无 start；Complete 确认丢失 error 无 done、库中 completed；确定回滚 error 无 done、仅一条原文 |
| 旧尝试迟到 | 验证 90 秒租约，强制到期并由独立 Service 回收/retry；旧流不能覆盖新版本或发送 done |
| 事件前错误 | 401、403、缺 Accept/坏 JSON 400、超体积 413 均为 JSON 且无认领副作用；原限流及额度单测复用 |
| 字节解析 | `python infra/sse/probe.py --self-test` 通过；逐字节 UTF-8/半字符/半行/半事件、LF/CRLF、多 data、心跳、EOF 半事件不发布 |
| 直连 / Vite / Nginx slow | 三条实际 HTTP 链路通过，目标分支构建首 delta 0.024/0.021/0.024 秒、末 delta 0.625/0.621/0.626 秒；delta 拼接与正式查询一致；Nginx 路由标记证明命中模板 SSE location |
| Nginx failure | 实测 start/delta/error，无 done；查询 failed，仅一条原文 |
| Nginx heartbeat | 实测 start/delta/heartbeat/后续 delta/done；首 delta 0.051 秒、末 delta 16.452 秒 |
| Vite timeout | 实际等待 45 秒，三次 keepalive 后 model_timeout error，无 done；查询 failed |
| Vite disconnect | 首 delta 后关闭连接，查询 failed、attempt_version=1 |
| Nginx lost-done 演练 | 客户端丢弃最终事件后只查询 completed；真实下游丢失另由 MySQL 专项验证 |

本地 Nginx 首次脚本启动发现缺少实际模板依赖的 `api_413` log_format，补齐后 `nginx -t` 和逐块探针通过；不将首次失败隐藏或当配置存在即验收。

Git/CI：适用本地检查完成后提交并普通推送到同一草稿 PR #19；最终 HEAD 与四项 CI 结论在 PR Checks/交接中记录，不把 A1/A2 旧 CI 当作本次 T15 通过。

## 可复用联调与故障

启动、停止、完整命令见 [infra/sse/README](../../infra/sse/README.md)，入口均为本机回环：

| 入口 | 用途 |
| --- | --- |
| `http://127.0.0.1:8080` | 后端直连；POST 时 Origin 仍使用 5173 |
| `http://127.0.0.1:5173` | 既有 Vite `/api/` 原路径代理，前端源码未改 |
| `http://127.0.0.1:18080` | 本地 Nginx API 代理；不是 HTTPS/公网验收 |

`start-local.ps1 -Scenario slow|failure|timeout|heartbeat` 仅显式 dev/test + Fake；分别为 6 码点/200ms 分块、首块后失败、45 秒超时、16 秒停顿触发心跳。`probe.py --scenario disconnect|lost-done` 演练断线/查询。探针逐字节读取，`--require-incremental` 断言实际时间间隔，`--nginx` 断言模板路由，`--require-heartbeat` 断言心跳已收到。

## 验收边界与给 B 的消息

后端技术交付待负责人确认。后端以目标分支构建运行 slow Fake；Nginx 使用目标 worktree 的模板/配置。原临时代理已停止并更名为 `siyige-t15-core-nginx` 保留，当前监听 18080 的是 `siyige-t15-local-nginx`，避免把源临时工作区冒充最终版本。B 的生产 UTF-8 解析器、真实浏览器交互、完整 AC12、HTTPS/公网代理、真实供应商 Stream/计费及 T15-DEMO-REAL/T14-EVAL 未验证；Fake 和合成供应商测试不能替代这些项目。Phase 3 未结项，P1 未扩大。迁移工具和前端未变，沿用既有有效证据与适用 CI，不做无关重跑。

> B：T15-Core 后端已在 `codex/p3-a-integration` / 草稿 PR #19 提供。冻结 start/delta/done/error 不变，start 不含版本。可用 5173 的 `/api/v1/debates/{id}/turns/stream` fetch POST，携带 Cookie、CSRF、Origin、Accept:text/event-stream；先看 HTTP/Content-Type，202 走 JSON，200 才解析 SSE。断线/error/EOF 无 done 按原 client_request_id 查详情；completed 用正式消息，generating 有限查询，failed 才手动 JSON retry 并携带查询到的 expected_attempt_version；409 只刷新查询，不自动换版本重发。Fake 场景与三个代理验证命令见 infra/sse/README.md。请在你的分支完成生产解析器和浏览器 AC12，保留字节拆块、半事件、心跳、202、断线和提交成功但 done 丢失的联调证据；A 未修改你的前端。
