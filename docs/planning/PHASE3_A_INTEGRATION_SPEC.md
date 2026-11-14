# Phase 3 A 首批：安全回归与跨成员契约

日期：2026-11-14（Asia/Shanghai）。分支 `codex/p3-a-integration`，基线为最新远程 main `e9d2659fd5f88eaf63e33f3cb0c5d3064f1f4c35`（含 PR #17 Phase 2 结项及 PR #18 文档契约）。原工作区 `codex/phase3-demo-contracts` 干净并保持原样；本轮在 `.cache/worktrees/p3-a-integration` 独立开发。

授权：必要本地开发、Commit、普通 Push、持续使用同一草稿 PR；不合 main、不调用真实模型、不公网部署。本轮只完成 T13-Lite 技术回归、[T14-STYLE Spec](T14_STYLE_SPEC.md)、T15-Core 接口样例和 Demo 最小方案，不实施 SSE、T14 功能、P1 或 Phase 4。CodeGraph 已优先调用，但索引提示多文件变化且未命中当前后端链路，已改按实际文件核对，未重建索引。DSH 桥工具可用；本轮选择 Codex 内联完成，未新建或复用 DSH 会话、未改变模型路由。

## T13-Lite 累计验收矩阵

本轮业务、迁移、依赖、运行与部署配置均不修改，因此直接复用仍有效的 [Phase 2 最终记录](../acceptance/PHASE2_FINAL_ACCEPTANCE.md) 和 T03/T04/T05/T07/T09/T10 证据。新回归使用 Go 1.27.1 / Windows，`AI_MODE=disabled`，清除两个数据库集成开关；适配器测试只连接进程内合成 HTTP 供应商，绝无真实出网。

| 风险 | 本轮实际测试或代码核对 | 累计证据与结论 |
| --- | --- | --- |
| 认证/Session | auth 的 Me、登录失败一致性、退出撤销、Secure Cookie、Argon2id 及 Session 用例 | 本轮通过；真实 MySQL Session 与重启沿用 Phase 2/AC01/AC07 |
| 所有权/越权 | debate service 的 retry/summary/detail 分支；实际 repository 按 owner + debate/turn 约束 | Phase 2 `TestT05InputAuthOwnershipAndTopics`、`TestT09ExplicitRetryVersionConflictAndOwnership`、`TestT10SummaryCreationReplayAndOwnership`、AC08；本轮 CI 继续执行同一隔离集成集 |
| CSRF/Origin | `TestPOSTRequiresExactOrigin`、`TestPOSTRequiresExactlyOneOriginHeader`、`TestLogoutRejectsDuplicateCSRFHeader`；业务路由共用 RequireOrigin/Auth/CSRF | 本轮通过；Phase 2 `TestAuthOriginAndCSRFEnforcementOverHTTP` 复用 |
| 请求体/字符/分页 | `TestRegisterBodyLimitWithUnknownContentLength`、`TestInputBoundaries`、非法 UTF-8/空白/超长输入 | 本轮通过；Phase 2 2000/2001 码点、32 KiB、特殊字符存储证据及 CI 集成复用 |
| 认证限流/生成额度/并发 | `TestRateLimitResponsesCarryRetryAfter`、`TestAdmissionSlotsQuotaAndRollback`、`TestReviewInvalidHTTPRequestsAreRateLimited` | 本轮通过；沿用 6/分钟、20/日、每用户 1/进程 2，无重建或放宽 |
| 超时/取消/失败恢复 | HTTP/模型预算、`TestGenerationTimeoutPreservesLedgerFailure`、T09/T10 失败重试/版本/租约、取消与 panic 释放 | 本轮通过；Phase 2 真 MySQL 提交不明、迟到结果和崩溃恢复复用 |
| 日志脱敏 | 平台受控 panic/路由日志与 `TestOpenAIGeneratorLogsWhitelistedFieldsOnly`、用量未知测试 | 本轮通过；T07 Nginx 413/日志证据复用，Linux CI 再验；无凭据、正文、原始供应商错误 |
| 两种赛制边界 | `TestRoundsForEachMode`、`TestStageThirdRoundDiverges`、`TestValidateRequestUsesModeRounds`、`TestParseSummaryRoundNoFollowsMode` | 本轮通过；Phase 2 `TestStandard4FourRoundsEndToEnd` 与 `TestQuick3HistoryStillCompletesAtThree`：4/5 与 3/4 边界，8/6 消息，总结轮号；CI 继续验证 |
| 费用保护/真实入口关闭 | trial 账本排他预留、fail closed、监控/停用、无自动重试，以及 server 默认关闭真实调用用例 | 本轮通过；Linux 特定结果测试在 Windows 跳过，须看 PR CI；真实 Demo 限制见下节 |

本轮执行：`go test -v -p 1 -count=1 ./internal/auth ./internal/ai ./internal/debate ./internal/platform ./cmd/server` 返回 0。后端 `go vet ./...`、`go build ./...` 均返回 0。Windows 的 Nginx/部分 Linux 专用测试跳过，不能记成本地实跑；本轮不重启数据库或重复浏览器累计验收，前端与迁移工具未改，复用 Phase 2 证据及 PR CI。

T13 技术结论以本地回归、有效历史证据与当前 PR 适用 CI 共同确认；项目负责人确认前任务状态为待人工验收。现有公网费用风险 T07-COST-01 保留，不把 T13 的无收费通过当成真实模式或公网准入。

本轮契约验证：沿用本地忽略目录中的 `openapi-spec-validator` / PyYAML / jsonschema，未新增产品依赖；OpenAPI 3.1.1 验证、YAML 重复键、353 个内部引用、48 个 Schema 结构检查通过；53 项样例/边界断言涵盖四策略状态、合法/非法重试输入、四流派及非法值、Unicode 长度、SSE 各帧和末尾空帧、202 JSON 与恢复详情。变更 Markdown 的本地链接/锚点及 `git diff --check` 通过。发现并修正了 SSE 样例末尾空帧丢失、详情样例缺赛制字段、错误恢复标题锚点和 PLAN 链接；业务代码未发现本轮必须修复的缺陷。

适用 CI 使用既有四项门禁（仓库静态、后端/迁移无收费、隔离 MySQL、前端含 Chromium）。当前提交的执行状态以本分支持续草稿 PR Checks 为准，不把等待中的检查预记为通过。最终交接提供实际提交 SHA 和 PR 地址；本轮未合 main、未运行真实模型、未部署或改动数据库。

## T15-Core 冻结契约与可复制样例

2026-11-15 实施增量：本轮已按成员 A 新授权完成后端与本地链路技术验证，见 [专项记录](../acceptance/T15_CORE_BACKEND_ACCEPTANCE.md)。以下保留 A1 定稿时的历史语境，四事件与恢复契约不变。

该端点尚未注册；以下是未来实现的测试 fixture，不是本轮实跑流式结果。正式规范见 [API](../api/API.md#13-post-apiv1debatesidturnsstreamp0) 与 [OpenAPI](../api/openapi.yaml)。B 可据此实现解析器和状态测试；A 负责唯一认领内核及 Eino 适配。请求使用 fetch POST、Cookie 和 CSRF，不能用只能 GET 的 EventSource 发送本接口。

| 标识 | 来源/生命周期 | 用途 |
| --- | --- | --- |
| request_id | 服务端每次 HTTP 请求生成，与 X-Request-ID/信封/start 一致 | 日志和请求关联；新 GET/重试有新值；不是幂等键或版本，GET 不支持按它查轮次 |
| client_request_id | 浏览器发送前生成并保留的 UUID，会话内唯一 | 普通/流式共享的业务幂等键；响应未知时在详情 turns 中定位；不能换键重发 |
| turn_id | 首次认领生成的持久轮次 ID | 跨尝试稳定，定位已存原文及既有 retry 路径 |
| attempt_version | 数据库轮次的递增正整数 | 首次 1，显式失败重试递增；只从本人详情查询取得最新值，作为 expected_attempt_version；不是 round_no |

请求（Accept 为请求头；原 API 的 HEAD 用词已修正）：

```http
POST /api/v1/debates/123/turns/stream
Accept: text/event-stream
Content-Type: application/json
Origin: http://127.0.0.1:5173
X-CSRF-Token: <当前登录取得的值>

{"round_no":1,"content":"开放式考核有利于考查应用能力。","client_request_id":"550e8400-e29b-41d4-a716-446655440000"}
```

新认领：HTTP 200，`Content-Type: text/event-stream`、`Cache-Control: no-cache`、`X-Accel-Buffering: no`、`X-Request-ID: req-stream-1`。四事件中的成功序列如下（空行是帧边界）：

```text
event: start
data: {"turn_id":"456","request_id":"req-stream-1"}

event: delta
data: {"text":"评价公平"}

event: delta
data: {"text":"仍需统一标准。"}

event: done
data: {"turn":{"id":"456","round_no":1,"client_request_id":"550e8400-e29b-41d4-a716-446655440000","status":"completed","attempt_version":1,"error_code":null,"messages":[{"id":"901","role":"user","content":"开放式考核有利于考查应用能力。","created_at":"2026-11-14T02:00:00Z"},{"id":"902","role":"assistant","content":"评价公平仍需统一标准。","created_at":"2026-11-14T02:00:05Z"}]},"completed_rounds":1,"status":"active"}

```

流内失败的替代结尾（可以接在 start/若干 delta 后；无 done，发完关闭）：

```text
event: error
data: {"error":{"code":"model_timeout","message":"回复生成超时，请查看会话状态"},"request_id":"req-stream-1"}

```

只有最终事务提交成功才发 done；delta 不落为正式消息。数据库提交不明也只能 error + 查询，不得假报 failed 已落库。completed 的同键重放走 HTTP 200 SSE `start` → `done`（无模型、无 delta）；start 仍关联这次重放 HTTP，done 返回已存 Turn 和当前会话状态快照。每 15 秒可发送 `: keepalive` 注释，不能将注释当事件。

仍在生成的同键重放：HTTP **202 application/json**，不是 SSE，也不能当 error：

```json
{"data":{"turn":{"id":"456","round_no":1,"client_request_id":"550e8400-e29b-41d4-a716-446655440000","status":"generating","attempt_version":1,"error_code":null,"messages":[{"id":"901","role":"user","content":"开放式考核有利于考查应用能力。","created_at":"2026-11-14T02:00:00Z"}]},"completed_rounds":0,"query_url":"/api/v1/debates/123"},"request_id":"req-duplicate"}
```

事件前失败：普通 HTTP JSON；示例为 403。400/401/404/409/413/429/500/503 同信封，429 带 Retry-After。不得先写 SSE 头再改 HTTP 错误。

```json
{"error":{"code":"csrf_failed","message":"请求校验失败"},"request_id":"req-rejected"}
```

恢复只使用既有 GET `/api/v1/debates/123`；示例是 error/断线后查到失败。未收到 start 时按本地原 client_request_id 匹配 turns：

```json
{"data":{"debate":{"id":"123","topic":{"id":"7","title":"大学课程是否应采用开放式考核","description":"讨论学习效果与评价公平"},"user_stance":"pro","ai_stance":"con","mode":"standard_4","ai_style":"logical","total_rounds":4,"status":"active","completed_rounds":0,"created_at":"2026-11-14T02:00:00Z","updated_at":"2026-11-14T02:00:05Z"},"turns":[{"id":"456","round_no":1,"client_request_id":"550e8400-e29b-41d4-a716-446655440000","status":"failed","attempt_version":1,"error_code":"model_timeout","messages":[{"id":"901","role":"user","content":"开放式考核有利于考查应用能力。","created_at":"2026-11-14T02:00:00Z"}]}],"summary_status":{"status":"not_started","attempt_version":null}},"request_id":"req-recover"}
```

- completed：丢弃临时片段，展示正式消息；generating 或暂未找到原键：每 2 秒查询、最多 120 秒，之后提示稍后核实；401 清登录状态，404 受控结束恢复。查询失败不当作生成失败。
- failed：用户看见版本后手动 `POST /api/v1/debates/123/turns/456/retry`，正文 `{"expected_attempt_version":1}`；沿用普通 JSON 200/202/错误及查询。无 stream/retry 新端点，不重发 stream，不改原文或新建键。
- 409 attempt_version_conflict：只重新查询，不自动将新版本放入请求重试。流式没有 Last-Event-ID 续传、事件重放或本地推测版本。
- 解析必须先看 HTTP 状态与 Content-Type；202 即使 response.ok 为 true 也走 JSON。200 只接受 text/event-stream。使用增量 UTF-8 解码，缓存半字符/半行/半事件，兼容 LF/CRLF、多 data 行和注释；EOF 无 done 走查询，不能视为成功。迟到的旧会话响应作废。

| T15 最小专项 | A | B |
| --- | --- | --- |
| 同键普通/流式同时发；失败后旧版本重试；迟到结果 | 同一认领/事务/配额，无重复消息或计轮 | 只保留一轮，冲突只查 |
| UTF-8 字节拆分、半事件、CRLF、注释、错误帧 | Fake 可控分块，输出完整性校验 | 字节级解析测试，无乱码/残片 |
| 202、事件前 4xx/429/503、已完成重放 | 正确 Content-Type 与信封，不先发头 | HTTP 与 SSE 分支分别处理 |
| start 前断线、delta 后断线、提交后 done 丢失 | 取消与租约恢复，提交结果可查 | 原键查找、有限轮询、显式 JSON retry |
| 请求预算与代理 | 流式路由纳入 60 秒预算，45 秒模型/90 秒租约；现有路由名单尚未含 stream | 前端 70 秒预算；Nginx 禁缓冲/分块实测后才验 AC12 |

T15-Core 启动条件：T13 技术通过并经负责人确认、该契约与 API/OpenAPI 同步后具备实施条件；不依赖 T14 策略或真实调用。当前尚无 SSE 功能/AC12 通过证据，本轮不开始其实现。

## 本地完整 Demo 与 10 场评测的最小方案

2026-11-15 实施增量：成员 A 已获本轮新授权实施下列受控多次调用方案，费用包装、独立本地 demo 环境和 Fake 验证见 [准备 Spec](T15_DEMO_PREP_SPEC.md)、[验收及等待项](../acceptance/T15_DEMO_PREP_ACCEPTANCE.md)。以下保留 A1 方案形成时的限制，实际新路径见 [运行说明](../../infra/demo/README.md)。B1/B2 负责人确认尚未完成，真实调用仍未授权；不把本次无收费准备记成完整 Demo 已交付。

实际代码依据：`backend/cmd/server/generator.go`、`backend/internal/ai/trial.go`、`trial_ledger.go`、`summary.go`、`adapter.go`、`backend/internal/platform/business.go` 以及锁定 go.mod/go.sum。以下仅为下一切片方案，绝不通过放宽已有试运行闸门、删除账本或换目录来补调用次数。

| 当前限制 | 对 Phase 3 的影响 | 最小实施增量（后续） |
| --- | --- | --- |
| deepseek 要 APP_ENV=deploy、HTTPS Origin、内部 mysql:3306/siyige_deploy 账户、绝对 Key 文件与 Linux；TCP 强制 trial-relay:8443，TLS 仍验证供应商 | Windows 直接 go run 不能启用真实；现有 deploy 配置不能冒充本地 Demo | 用本机 Docker Desktop/WSL2 Linux 单实例 Go + 内部 relay；后续新增专用 APP_ENV=demo 与 AI_MODE=local-demo，Compose project=siyige-p3-demo，仅回环暴露页面/API，固定内部 mysql:3306/siyige_demo/siyige_demo_app，独立数据卷与迁移入口目标保护；不放宽 dev/test/deploy 校验或执行公网发布步骤 |
| 每个 isolated/public 阶段仅一枚 O_EXCL reserved 记录；固定两账号、2 小时窗口，isolated 只放首账号 | 不能完成一场多调用，更不能 10 场；public 槽不可借用 | 保留 T07 原路径；另做明确 local-demo 模式的有限多调用账本，复用文件原子持久预留与停用规则，不挪用旧授权或清零旧账本 |
| trialTransport 严格要求 stream=false、两条消息、1024 输出、请求 JSON 最多 15360 bytes | SSE 被拒；完整四轮/总结历史可能超过请求上限；窗口 1M 配置并不能绕过出网闸 | 同一受控 transport 为实际锁版 Stream/GenerateSummary/策略协议定义白名单；按完整序列化请求保守估算输入，显式配置输出和全站预算，超限受控失败，不裁剪历史 |
| trialGenerator 只实现 Generator.Generate；未实现 SummaryGenerator，且当前 Generator 无 Stream | service 的类型断言使真实总结不可用；没有可用流式路径 | 后续包装器显式提供 Generate/Stream/GenerateSummary/策略并共用同一费用认领与结果记录；不是直接返回裸 OpenAIAdapter 绕过保护 |
| responseObserver 当前按完整 JSON 检查供应商响应 | 直接接 Stream 可能被观察层拒绝或缓冲至结束 | 同步支持 SSE 的流式观察、最终 usage/finish_reason 与校验，遇取消/缺尾帧用量记未知，不缓存全流才转发 |
| 进程 Gate 20 次/用户/日、6/分钟，全局 2；重启丢计数 | 同账号一天不能做 10 场；不能靠重启绕限额 | 保持 Gate；10 场含策略至少 60 次成功调用，安排两个批准体验账号各 3 场/UTC 日以内（18 次），分至少两个 UTC 日完成；分钟满等待，不自动重发 |

建议先完成 T14-STYLE 与 T15-Core 的 Fake 实现，再单独实施 local-demo 账本/装配，最后获真实调用授权后验收。预算方案为**参数清单**，本轮没有批准值或真实价格结论：模型/供应商与能力核实、批准账号、UTC 起止窗口、最大调用数、总输入/输出 Token、单次上限、金额目标与独立余额范围、紧急停止路径。60 次只是假设每场一策略+四轮+总结且全部成功的下界，不是重试额度；每个失败/超时/结果未知都消耗预留，未批准补额就停止并登记未完成。

local-demo 费用实现只做必要控制：单实例持久账本；出网前互斥并原子预留 calls/input/output/按冻结价格保守估算金额；跨重启重建累计且拒绝账本损坏、配置被改、预算不足、窗口不足覆盖在途上限；持久 stop 与人工紧急停用；开始/结束检查窗口与授权身份；不自动释放失败/未知预留；无 SDK 自动重试、重定向、fallback 或自动充值。完成时记实际用量（未知不写 0），保留最坏费用预留，不能当作供应商绝对封顶。真实 Stream 中途停止仍可能计费。充值余额兜底与 T07-COST-01 风险不变，禁止把本地 Demo 准入扩大为公网。

Fake 必须先验：最后一个预算槽并发竞争、额度触顶、停用后重启、账本写失败/损坏、超时/取消/提交不明、SSE 无 usage 尾帧、窗口到期、不重试；验收前不配置真实 Key。运行说明须给出启动/Fake与真实切换/停止步骤、回环地址、隔离库和密钥/账本挂载位置，密钥只在后端，不能发布一个不受保护的直连端点。

评测按同一提交、模型和 Prompt 版本覆盖 ≥3 题、双方立场、四流派；建议 10 行计划为题1正逻辑/反进攻、题2正共情/反数据、题3正进攻/反逻辑、题1正数据/反共情、题2正逻辑、题3反数据。每场人工逐轮检查回应、立场、事实依据与建议，记录匿名会话/请求标识、Token/费用与失败。至少一场让负责人在浏览器自主输入，可同时计入 Demo 和 10 场，不重复计费/计场；额外自由体验需独立额度。T14-EVAL、T15-DEMO-REAL 均保持未执行，Phase 3 未结项。

## B 可立即开始与交接边界

1. T14：四流派选择控件和严格请求类型、策略卡四状态展示、显式生成/重试、GET 恢复和隔离响应测试；创建结果未知沿用先查历史，不自动重建。
2. T15：按上述 fixture 写 UTF-8 增量解析、202/普通错误分支、断线查询恢复及版本冲突测试；这些可以先于服务端实现准备，不声称真实 SSE 已打通。本轮 A 不实施 SSE。
3. 评测：准备 10 场匿名人工检查表和 Demo 操作说明提纲，真实执行等待独立授权及受测功能就绪。

禁止 B 自行变更：事件名/载荷、幂等键及重试路径、版本来源、固定 standard_4 与历史 quick_3 语义、四流派枚举、策略触发与共享额度、服务端模型/费用准入。A 负责 handler/service/repository/AI/迁移及认领核心；B 不并行改这些共享核心。需要改契约时先与 A 对齐并同批更新 API/OpenAPI/Spec。B 应在自己的功能分支工作，以本交接提交为基线；A 后续会话持续使用本分支与同一草稿 PR。
