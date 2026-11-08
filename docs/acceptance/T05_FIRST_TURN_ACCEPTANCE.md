# T05 首轮业务与消息持久化验收记录

**最终状态（2026-11-08）：项目负责人确认三关人工验收全部通过，正式批准 T05 无收费实现范围结项并授权 Git Commit；新会话独立复审 PASS，三项 P2 均已独立确认关闭。** 真实模型链路仍未验收，后续调用须另行授权，已接受限制及本轮提交边界见文末。以下保留实施、审查及人工验收前的历史记录，以文末最终结论为准。

实施起点（2026-11-08，Asia/Shanghai）。基线 `codex/docs-reorganization` / `b1c255b12ad89078478c816d1ad125ec9ee7c69e`，开始时工作区及暂存区干净。本轮仅实施 T05，**不自动结项、不进入 T06**。负责人已授权无收费实现与固定隔离测试库验证，未授权真实模型调用、Git 提交/推送/合并/PR 或部署。

## 上游与冻结契约核对

- T03 [记录末尾](T03_AUTH_ACCEPTANCE.md#独立审查与人工验收结果)：有条件独立审查通过，无未解决 P0/P1，人工验收已完成；原 P2 测试 Session 清理风险及 P3 延期继续保留，没有将它们记为修复。
- T04 [最终验收](T04_AI_ACCEPTANCE.md#最终人工验收与结项)：已正式结项，原一次真实调用授权已消耗，不沿用到本轮。供应商硬预算缺失继续按 T07-COST-01 阻塞公网真实入口。
- 数值与错误规则依据 [API](../api/API.md#phase-1-业务接口)、[OpenAPI](../api/openapi.yaml)、[PRD](../product/PRD.md)、[架构轮次与安全控制](../architecture/ARCHITECTURE.md)、ADR-007/008/016。32 KiB、2000 码点、6/min、20/day、1/用户、2/进程、60/45/3 秒均已有来源；没有新增业务字段或调整 HTTP 状态码。
- 全局并发指**单个 Go 进程内所有用户合计 2 个模型调用**，不支持跨实例。分钟 HTTP 防刷也作用于重放；每日额度只计新认领，模型失败计入。进程内额度重启丢失，不是可靠计费账本。

## 交付与文件范围

- 新增 `backend/internal/topic/`：真实 MySQL 启用辩题查询、ID 升序、空 `[]`，Handler/Service/Repository 分层。
- 新增 `backend/internal/debate/`：建会话和辩题快照、首轮短事务认领及用户原文、规范 UUID 幂等、事务外 T04 Generator、完成/失败版本校验、唯一正式消息、本人详情及 attempt_version。
- `platform/business.go`、`router.go` 和 `cmd/server/`：装配四接口；HTTP 入口先于认证/正文施加生成 60s、普通 5s Context 和写期限，服务模型 45s，数据库整个短事务 3s；socket WriteTimeout 由 5s 改为 65s 兜底，真实生成路由仍受入口 60s 限制。
- `AI_MODE` 默认空/disabled，真实入口受控不可用；显式 fake 使用 T04 Fake。显式 deepseek 模式装配既有 Eino Adapter，要求 `AI_MAX_OUTPUT_TOKENS`，固定 DeepSeek 地址/模型和已核实窗口配置。本轮只构造适配器作无网络验证，**未开启或调用真实模式**。后续真实调用必须另行授权；`.env.example` 只增加非秘密说明，未修改 `.env` 或凭据。
- `backend/tests/integration/debate*.go`：固定测试目标前后验证、真实注册登录/Session/CSRF、真实 HTTP 与 MySQL、并发/取消/额度/故障测试。新 T05 夹具按本次已登记 user ID 清理其 Session、Debate、Turn、Message，不批量清空既有数据；不会改容器或卷。
- 必要回归修正仅涉及 `auth/handler_test.go`：独立复现固定 Service 时钟与真实墙钟混用导致的 Cookie Expires 失败，改为比较注入时钟的 24h。**未修改认证产品源码**，也未宣称原 T03 P2 清理问题已修复。
- 同步 AGENTS、README、API/OpenAPI、架构、决策、PLAN/TASKS 的当前状态，保留历史证据；新增 [计划](../planning/T05_FIRST_TURN_PLAN.md) 与 [DSH 进度](../planning/T05_DSH_PROGRESS.md)。无新产品依赖，原迁移/Go 锁定依赖/前端/infra 未修改。

## 协作记录

使用 codex-dsh-orchestrator；决策 `new-session`。仅使用 1 个 DSH 会话 `session-101b72ff-3318-4bbb-b237-24d4ef1f5b15`，BridgeTask `dsh_6a49b0bc74ba`，保持 `trae/deepseek-v4.1-flash / extra_high`，未选择/修改默认路由，`providerCacheEvidence=not_exposed`。

首回合完成只读探索但无源码落盘，Bridge 未暴露最终消息，不记为交付。Codex 同任务缩小派工后，DSH 完成 topic 源码/测试并写进度；其定向测试/race/vet/build 结果为 Worker 自测。Codex 核对源码后接管；回合 `turn_completed`、无待处理交互，已释放独占 claim。之后 Codex 完成 debate、装配、测试及文档。没有并行写工作区或并行共享数据库写测试。

## 实际验证

环境：Go 1.27.1、Node 24.11.1、npm 11.6.2；固定 MySQL 8.4.13 / `127.0.0.1:13307` / `siyige_test` / `siyige_test_app`，连接后验证 CURRENT_USER 与数据库名；两个项目 MySQL 容器只读状态均健康。

| 检查 | 结果与边界 |
| --- | --- |
| 首次输入/准入测试 | 实现前因 ParseID、ValidateSubmit、NewGate 等不存在编译失败；实现后通过。覆盖 BIGINT 上界、前导零/非法 ID、2000/2001 四字节码点、空白、并发 1/2、回滚返还、20/day 和 6/min。混合 BOM 空白后补并修正；该单项首次运行曾与文件变更交叠出现编译失败，未冒充已观察到断言 RED。 |
| 首次 Service 测试 | 实现前缺少 NewService 编译失败；实现后通过。模型在认领事务外、45 秒期限、重放零调用、错误映射、取消/模型 panic 释放槽、父期限到期后独立有界落失败。 |
| 首次整合测试 | 编译因 NewBusinessRouter、NewRepository、NewHandler、HTTPWriteTimeout 未实现失败，补齐后执行真实依赖测试通过。 |
| T05 首批真实 HTTP/MySQL | 5.5s Fake 成功 JSON、真实认证、持久化两条正式消息、完成轮数 1/active、重建 HTTP 服务后读取；UUID 大小写规范、同键异文/异轮冲突、第二轮拒绝、2000/2001、Session/CSRF、跨用户与不存在统一 404、真实种子排序通过。Fake 不代表真实模型验收。 |
| 生成中和并发 | 模型在途期间详情可读；同键 202、异键占同轮 409；同用户第二会话 429、两个真实用户占满进程槽后第三用户 429，均带 Retry-After；拒绝的请求没有新增 Turn。 |
| 失败、取消、数据库 | 模型超时分类 504，失败重放 409，用户原文留存且无 assistant；主动取消真实 HTTP 后可查询 generation_cancelled。关闭仅本夹具的连接池，接口受控 503；没有停止数据库容器。 |
| 每日额度 | 20 个新认领模型失败均计额，第 21 次 429、无新 Turn；已失败重放仍 409 turn_failed，未再调用模型。分钟/日期使用注入时钟，未等待真实一天。 |
| 提交结果不明 | 测试包装真实 sql.Tx：MySQL 已 Commit 后人为丢失认领或完成提交确认。均返回 503 dependency_unavailable、无已确认失败 details；GET 读到 generating/completed，重放 202/200，调用次数不增加。确定消息写入失败则整个认领回滚，后续能认领。 |
| 版本保护 | 模型在途时仅修改本轮夹具 attempt_version，再返回旧结果；接口受控错误，无 assistant 写入。 |
| HTTP 预算与入口 | 真实 >5s 请求已通过；单测生成 60s/普通 5s/更短父期限，模型 45s；真实 main 注册保护后的 T05 GET 返回 401；默认不因已有 Key 启用收费模型。未让真实模型运行 45/60 秒。 |
| 累计 race + 实际集成 | `go test -race ./... -count=1` **退出 0**；SIYIGE_INTEGRATION=1、APP_ENV=test 及固定白名单，8 包通过，integration 17.052s。包含真实认证及 T05 MySQL 测试，未跳过此轮集成。 |
| vet / build / 格式 | `go vet ./...`、`go build ./...`、`gofmt -l cmd internal tests` 通过，格式列表为空。 |
| 依赖与差异 | `go mod verify` 通过。`git diff --check` 通过；迁移、go.mod/go.sum、frontend、infra 与 HEAD 无差异。尚须最终文档检查及独立审查后复核。 |

累计 race 的复现环境与命令（不展示口令）：

```powershell
$env:SIYIGE_INTEGRATION = '1'
$env:APP_ENV = 'test'
$env:DB_ADDR = '127.0.0.1:13307'
$env:DB_NAME = 'siyige_test'
$env:DB_USER = 'siyige_test_app'
$env:DB_PASSWORD_FILE = (Resolve-Path '.cache/t02-mysql/secrets/test-app.txt').Path
Set-Location backend
go test -race ./... -count=1
```

失败/环境记录：最初普通沙箱的 Docker pipe 和回环测试受限；在获准本机依赖访问的环境复验通过，没有修改业务代码规避。一次 PowerShell 把通配符直接传给 gofmt 导致格式命令失败，改用两个确切文件路径后完成。一次编译与新增 unicode import 的文件变更交叠导致编译输入不一致，结束旧进程后定向测试和最终累计 race 已通过；不计为业务失败或 RED 证据。Sonic 对 Go 1.27 的既有提示回退 encoding/json，未改依赖或冒充优化路径兼容。

## 已知边界、审查与人工验收

- 独立 Codex 只读审查：**待新建会话执行**；主会话技术验证和 DSH 自测均不代替独立审查。
- 每日计数和分钟防刷为单进程内存；UTC 日界/固定一分钟窗口，最多 20000 用户条目，只回收过期条目，不淘汰活跃额度。提交确认丢失时保守预留该新认领额度，避免低估；不宣称进程重启或跨实例可靠计费。
- 数据库无法确认写回或进程硬崩溃可能遗留 generating；90s 租约与版本字段保留，回收及显式重试属 T09，不在本轮启动。
- 真实模型验收：**未执行、待另行授权**；T04 的历史真调用不能替代 T05 真链路。供应商硬预算继续为 P0 公网阻塞；本轮没有收费请求。
- 项目负责人人工验收：**待执行**；前端、Nginx/HTTPS、公网、容器重启等未在本轮实施或验证。不自动结项、不进入下一任务。
- Git：分支与 HEAD 未变，所有本轮文件留在未提交工作区，暂存区为空。没有 commit/push/merge/rebase/reset/PR/部署。


## 独立初审及三项 P2 修复

独立 Codex 新会话 `01a1bbae-7b92-78b3-a30b-de6aa229f2c0`，在基线 HEAD 和当时 37 个待审文件上只读审查，最终 **CONDITIONAL**，未发现新的 P0/P1/P3，发现 3 项 P2。源码在审查期间 SHA-256 保持一致。审查者核对实际变更及新增文件，独立运行本机累计 race、vet/build/mod verify/格式/差异检查通过；其 race 显式关闭 MySQL 集成，未将跳过记为数据库验收。审查者以工作区外 overlay 补测复现问题，未修改项目、写数据库、操作容器或调用真实模型。

| 编号 | 独立发现 | Codex 修复与实际证据 | 状态 |
| --- | --- | --- | --- |
| T05-R01 / P2 | 模型成功后、最终 Complete 开始前或执行中取消，确定未提交时未独立落失败，会遗留 generating。 | 将确定回滚的最终写回失败纳入独立 3 秒失败落库；取消保存 generation_cancelled，仍校验状态/版本。CommitUnknown 继续 503、无 details、不把可能成功的结果改成失败。新增单测覆盖确定取消与未知提交分支，真实 MySQL/HTTP 在 assistant 写入回调处取消验证原文/失败/无 assistant。 | 已修复，独立复审确认关闭 |
| T05-R02 / P2 | 路径/JSON/空白/长度等非法输入在分钟计数前返回，可绕过 HTTP 防刷。 | 认证、Origin/CSRF 通过后，在路径及正文解析前计数；Handler 与 Service 分开内部已计数入口，防止有效请求重复计数。空白、坏 JSON、非法 ID、超大正文的第 7 次均 429 且带 Retry-After。每日计额和模型槽仍只对新认领准入。 | 已修复，独立复审确认关闭 |
| T05-R03 / P2 | 使用 Context 错误覆盖 ai.Error，丢失已知 Usage/Metadata。 | 分类保留 Error 或成功 Result 的已知用量与元数据，校验失败也保留。覆盖取消错误、成功后取消、超时及已知 0 Token；真实 MySQL 取消写回后读取 input=100/output=10、模型名、Prompt 版本、latency=123，均保持。 | 已修复，独立复审确认关闭 |

先仅添加 `internal/debate/review_regression_test.go`，实际运行 `go test ./internal/debate -run '^TestReview' -count=1` **失败**：确定取消未落失败、用量丢失，以及四类非法 HTTP 的第 7 次仍 400/413。修复 service/handler 后同一命令 **通过**（0.506s）。追加已知 0 Token 超时及真实最终写回取消场景后，显式固定隔离测试库再次运行 `go test -race ./... -count=1` **退出 0**，8 包通过，integration **22.720s**；该重跑因核心修复及新增用例而必要，不是无变化重复全量验证。

本节仅登记已实施和实际技术验证，**不自行关闭三项 P2**。下一步按负责人要求另建全新独立 Codex 会话复审，不能沿用原审查上下文或使用 DSH 替代。真实模型与人工验收仍未执行。


## 新会话独立复审与当前交付结论

全新 Codex 复审会话 `01a1bbbd-811f-71b0-9be6-58fd869f7e0e`，独立于初审和实现上下文，对修复后 38 个待审文件进行只读复审。复审前后 SHA-256 与 `.cache/t05/review-fix-freeze.json` 全部一致，暂存区为空。**最终结论 PASS：T05-R01/R02/R03 三项 P2 均独立确认关闭，未发现新的 P0/P1/P2/P3。**

复审实际执行本机 `go test -race ./... -count=1` 退出 0、8 包通过；明确清空 SIYIGE_INTEGRATION，MySQL 集成跳过，不将主会话的数据库实测冒充独立数据库复验。工作区外 overlay 补测通过，覆盖独立清理预算、未知提交不改写、槽位释放、有效请求只计一次和部分未知用量。vet/build/mod verify/格式/差异检查通过。首轮沙箱回环测试失败，本机复跑通过。复审者未修改项目或秘密文件，未写数据库、操作容器、调用真实模型或 t04-smoke 入口（累计测试只运行其纯单元测试包）。

主会话修复后实际固定 MySQL 累计 race 的结果仍为 8 包通过 / integration 22.720s；修复后 vet、build 各自退出 0，gofmt 列表为空。收尾在不改产品代码的前提下更新本记录及当前状态；OpenAPI 规范、冻结 components 与各操作 requestBody/responses/parameters/security 相对 HEAD 保持一致，153 个本地链接目标、38 文件冻结哈希及空白检查通过。收尾文档更新后再核对差异、链接和代码哈希，不无变化重跑业务测试。

**当前可交付无收费技术验收。真实模型验收未执行、待另行授权；项目负责人最终人工验收未完成，T05 不结项。** 没有进入 T06，没有 Git 提交/推送/合并/PR 或部署；T07-COST-01 仍为 P0 公网开放阻塞。内存额度重启丢失、单进程并发作用范围及未实现的租约回收继续按既定边界保留。

最终收尾检查通过：本轮共 38 个变更文件，27 个 Go/非秘密配置文件与独立复审冻结哈希一致，收尾只更新 11 份文档中的必要状态与证据。154 个本地链接目标存在；OpenAPI 规范及冻结请求/响应/参数/安全定义一致；空白检查通过。HEAD 仍为 `b1c255b12ad89078478c816d1ad125ec9ee7c69e`，暂存区为空，全部变更未提交；迁移、依赖锁、前端与 infra 未变。

### 项目负责人人工验收事项

1. 在固定隔离测试环境、显式 AI_MODE=fake 下，通过 API 客户端注册/登录，核对启用辩题列表、创建后的快照与双方相反立场；创建时不生成。
2. 发送首轮并刷新详情，核对一组正式消息、completed_rounds=1、active、attempt_version=1；同键重放不多写，另一账号详情/发送同为 404。
3. 核对 CSRF/输入错误、生成中查询和失败原文提示，审阅上述实际自动化证据及已知限制。没有前端页面，不把本项当作 T06 浏览器验收。
4. 真实 DeepSeek 首轮联调另行明确授权；不得沿用 T04 一次性授权。供应商硬预算缺口不因本轮 Fake 或技术审查成功关闭，也不因此允许公网入口。

人工验收结论：**2026-11-08 项目负责人在本次对话中明确确认前三关人工验收全部通过，正式批准 T05 无收费实现范围结项。** 第 4 项真实 DeepSeek 链路仍未执行，留待另行授权；本次登记依据负责人明确确认，不补造操作截图或测试结果。


## 最终人工验收与无收费范围结项

2026-11-08（Asia/Shanghai），项目负责人明确声明“T05 三关人工验收已全部通过”，批准无收费实现范围正式结项并授权 Git Commit。人工验收通过覆盖上述前三项：Fake 环境的辩题/创建、首轮持久化/重放/所有权、错误与失败状态及既有证据审阅；不将其记录为真实模型或 T06 浏览器验收。

- 独立复审：沿用全新 Codex 会话 `01a1bbbd-811f-71b0-9be6-58fd869f7e0e` 的 **PASS**；T05-R01/R02/R03 均独立确认关闭，无新 P0/P1/P2/P3。本轮未重复独立审查。
- 自动化证据：沿用修复后真实隔离 MySQL 累计 race（8 包通过，integration 22.720s）及 vet/build/格式等既有结果。本轮仅检查文档、提交范围、敏感信息、差异和冻结文件哈希，不重跑完整测试、不修改已验收业务代码。
- 结项范围：四个 T05 接口、首轮业务与消息持久化、无收费 Fake 真实 HTTP/MySQL 验证及相应保护已完成；T05 无收费实现范围正式结项，Phase 1 与后续任务不因此自动完成或启动。

负责人已接受并保留以下限制：

| 限制 | 后续安排与当前影响 |
| --- | --- |
| 真实模型首轮链路未验收 | 后续真实联调须另行明确授权；T04 一次真实调用授权已消耗，不能复用。本轮结项不将 Fake 结果视为真实模型结果。 |
| 供应商硬预算缺失 | T07-COST-01 继续保持 P0 公网真实入口阻塞，按既定关闭条件验证。 |
| 额度与并发仅单进程生效，内存计数重启丢失 | 保留单实例 MVP 边界，不作为跨实例协调或可靠计费账本；后续按 T07 部署门槛及 T13/T17 验证计划处理。 |
| 单进程模型并发当前为 2 | 登记 T05-OPT-01，由 A 在 T13 参数调优时评估提升至 4–8；结合延迟、资源及费用保护验证决定，本轮不调整参数或容量承诺。 |
| 硬崩溃或无法确认写回可能残留 generating；无显式重试、第 2/3 轮 | 90 秒租约与版本字段保留，租约回收、显式重试及后续轮次继续由 T09 交付。 |
| 前端、代理/HTTPS、公网及重启链路未在本轮验收 | 按 T06/T12、T07、T17 等既定任务计划处理；AI 人工质量评估由 T14 交付。T03 原已接受 P2/P3、T02-M03 等延期不因本轮结项关闭。 |

本次仅授权规范化本地功能分支及一次提交：原分支 `codex/docs-reorganization`，基线 HEAD `b1c255b12ad89078478c816d1ad125ec9ee7c69e`；核对本地/远端跟踪分支、工作树与历史后，目标为从该 HEAD 新建 `feat/t05-first-turn`，保留全部未提交修改，原分支引用保持不变。目标分支若已存在或出现历史/归属冲突即停止，不覆盖。只暂存已确认的 38 个 T05 文件（含 27 个与独立复审冻结哈希一致的 Go/非秘密配置文件及 11 份必要文档），排除秘密、临时文件与无关改动。提交信息为 `feat: 完成 T05 首轮辩论业务与消息持久化`；实际 Commit Hash 与最终工作区状态由提交后核验报告给出。

本轮无需 DSH；不执行 Push、Merge、Rebase、Reset、PR、部署或真实收费调用，不进入 T06。以上授权覆盖前文实施时“禁止提交/待人工验收/不结项”的历史边界，其余限制继续有效。
