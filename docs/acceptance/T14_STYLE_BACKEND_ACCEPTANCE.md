# T14-STYLE 后端专项验收与 B 交接

日期：2026-11-14（Asia/Shanghai）。成员 A；分支 `codex/p3-a-integration`，沿用草稿 PR #19。A1 基线 `f7febe7`，包括原定稿契约与 T13 记录，未从 main 重建、未丢弃 A1。开始时目标工作树干净，`git fetch origin` 后本地/远程为 0 ahead / 0 behind。当前工作树没有 `.codegraph/`，未创建索引，直接按实际文件检查。

本轮授权已执行：本地开发、无收费测试、固定隔离 MySQL 的新增迁移和测试夹具、Commit、普通 Push、更新现有草稿 PR。未修改 B 前端、未合 main、未重写共享历史、未启动 SSE/P1、未调用真实模型、未部署公网。原主工作区及其他工作树保留。代码由 Codex 内联实现，未委派其他 Agent。

## 后端交付与契约一致性

- 创建 `POST /api/v1/debates` 接收可选 `ai_style`。精确接受 `logical/aggressive/empathic/data`，仅省略默认 logical；null、空串、大小写不符、未知值及非字符串均 400 invalid_request，校验前不 INSERT、不获取生成 Permit、不调用模型。新建仍 standard_4，初始轮数 0。没有流派修改接口。
- 保存既有 debates.ai_style；详情、历史与轮次 Prompt 读取保存值，轮次请求夹带流派不会覆盖。策略使用辩题快照、双方立场、保存流派与 mode，帮助用户自身立场，风险侧体现对手流派。历史 quick_3 不改。
- `GET/POST /api/v1/debates/{id}/strategy` 按 [A1 Spec](../planning/T14_STYLE_SPEC.md) 与 [API](../api/API.md#14-getpost-apiv1debatesidstrategyt14-style) 实现，输入、状态码、JSON 字段均无破坏性变更。GET 只读；POST 必须 Session、精确 Origin 与 CSRF。不存在/越权统一 404。
- 单会话一份成功策略。未开始 GET 为 not_started/null；首次 `{}`/retry=false 成功 201；重放 completed=200、generating=202、failed+retry=false=200；仅失败且显式 retry=true 与查询版本匹配才递增，成功 200，旧版本 409 attempt_version_conflict。未知/重复/大小写变体键及错误类型严格拒绝。
- 首次与失败重试在同一会话锁下检查没有任何 turn，与首轮认领串行。已有 turn 则 409 round_conflict；已完成/在途重放始终允许。策略不推进轮次、不写 messages、不进入总结，允许跳过策略直接开始辩论。
- 与轮次/总结共享同一个 Gate（6/分钟、20/UTC 日、单用户 1/进程 2）。仅新认领计每日额度；失败计次，确定回滚不计，提交不明保守计次。重放不调模型，仍受分钟防刷。
- POST 使用 60 秒请求预算、45 秒模型预算；GET 为 5 秒；短事务 3 秒、租约 90 秒。启动和既有 30 秒调度扫描策略；取消使用独立清理 Context 保存失败。完成/失败写回均校验 status/version；提交不明返回 503、不声称已失败、不附 details，客户端先 GET。
- schema 1 为 thesis/arguments/risks 三键，文本各 1–300 Unicode 码点且非空白，数组各 1–3 项，JSON ≤8 KiB。严格拒绝额外/缺失/重复字段、错误类型、非法 UTF-8、尾随内容及非法长度；不自动修复 JSON 或重试。
- 策略独立 `strategy-v1`，轮次 `debate-v1`、总结 `summary-v1` 保留。保存 schema/Prompt/model/用量元数据；未知用量 NULL。成功不可因升级重生成。策略 GET 返回保存版本。
- Fake 支持结构化成功与脚本失败；OpenAI 策略适配器只在合成 Transport 测试使用。服务端 disabled 或现有真实试运行包装器缺少策略能力时受控不可用；没有绕过其出网费用账本，也未新增收费装配。完整本地真实 Demo/费用账本仍属 T15-DEMO-REAL。

## 迁移与范围

新增 `00013_create_debate_strategies.sql`，唯一 debate_id、RESTRICT 外键、精确状态与正版本约束、租约索引和 UTC 元数据。没有改动 00001–00012；未新增依赖。原 P1 预留 00013 与本任务冲突，已在 PLAN/TASKS 同步下一候选为 00014，实施前仍须核对远程。

只操作 `127.0.0.1:13307/siyige_test`：迁移账户 `siyige_test_migrator`，业务测试账户 `siyige_test_app`，入口校验实际库/账户。现有 12 版本新增 `applied=1`，重跑 `applied=0`；共 0–13 版本、八张业务表、七条外键。未操作开发/生产库，保留卷和迁移故障证据表；业务夹具仅清理本测试创建用户的数据。CI 空库期望改为 applied=13。

变更范围：backend 的 debate/ai/platform、隔离集成测试、迁移及迁移测试；CI 仅同步迁移版本数；API/OpenAPI、架构、任务、计划、Spec 和导航同步。frontend 没有修改。

## 实际验证结果

环境：Windows / Go 1.27.1 / MySQL 8.4.13；Fake 或进程内合成 Transport，无真实模型调用。普通回归 AI_MODE=disabled 并清除两项集成开关；数据库测试单独显式启用固定测试目标。

| 执行项 | 实际结果 |
| --- | --- |
| 后端 `go test -count=1 -p 1 ./...` | 通过；未启用数据库时集成测试按设计跳过 |
| 后端 `go test -race -count=1 -p 1 ./...` | 通过；Windows 不适用的 Linux/Nginx 测试仍跳过，不冒充本地通过 |
| 后端 `go vet ./...` / `go build ./...` | 通过 |
| 隔离 MySQL `go test -race -count=1 -p 1 ./tests/integration` | 累计完整回归通过，包含新增 T14 和既有 Phase 2/赛制/认证/总结 |
| 迁移独立模块 `go test -count=1 -v ./...`（显式集成开关） | 通过，含版本重跑、约束与错误恢复；新增策略字段/唯一键/外键/精确枚举/版本约束专项再次通过 |
| 迁移独立模块 `go vet ./...` / `go build ./...` | 通过 |
| OpenAPI/Schema/样例/本地链接/gofmt/diff 检查 | OpenAPI 3.1.1、YAML 重复键、353 引用、48 Schema、16 implemented/1 planned、53 样例边界断言、305 本地链接/锚点通过；100 个暂存 Go 文件的 Git LF 内容均符合 gofmt，git diff --cached --check 通过 |
| PR #19 适用 CI | 推送后检查；未完成时不预记为通过，最终状态见 PR Checks |
| 本地前端/浏览器、SSE、真实模型质量/费用验收 | 未执行；前端未改，真实模型未授权 |

测试先观察到 null 创建返回 201 和策略 GET 404，再实现并转绿。后续测试暴露并修复：结构错误时已知用量丢失、策略 POST 遗漏 60 秒预算、Fake 规范 JSON 序列化后大小越界（修复后 ai 包 race/vet 通过）；旧赛制测试中“忽略 ai_style”的历史断言随新契约更新，standard_4 断言保留。累计 MySQL 回归修正该旧断言后通过；没有隐瞒失败或把重跑前结果当作成功。

## Fake 场景与复现

`backend/tests/integration/style_strategy_test.go` 用真实 Session/CSRF/HTTP/Repository/MySQL，只有模型边界 Fake：

| 场景 | 用例 |
| --- | --- |
| 四流派/默认/非法输入无副作用、保存/详情/历史/轮次 Prompt | TestT14CreateStyleStrictAndPersisted |
| GET 未开始、严格策略输入、首次/重放/保存版本 | TestT14StrategyHTTPContract |
| 超时、非法结构、两次失败、显式恢复、旧版本冲突与保存快照 | TestT14StrategyFailureRetryVersionAndSnapshot |
| 未登录/Origin/CSRF/越权/不存在/32 KiB | TestT14StrategyOwnershipAndWriteProtection |
| 在途 GET/重复 POST/合法重试重放/共享并发槽 | TestT14StrategyConcurrentReplayAndSharedGate |
| 跳过策略、已存在成功或失败轮次时拒绝新策略 | TestT14StrategyAfterAnyTurnAndSkip |
| 启动租约回收、显式恢复、迟到成功和失败不覆盖 | TestT14StrategyLeaseRecoveryAndLateOutput |
| 认领确定回滚、最终写入失败、槽位释放 | TestT14StrategyClaimRollbackAndCompletionFailure |
| 无策略能力受控不可用、HTTP 取消后查询及恢复 | TestT14StrategyCancellationAndDisabled |
| 每日额度/分钟限流与轮次共享、失败计次、重放豁免 | TestT14StrategyDailyQuotaSharedWithTurnsAndReplayExemption、TestT14StrategyMinuteLimitCountsInvalidInput |
| 认领/最终提交已成功但确认丢失、不二次生成 | TestT14StrategyCommitUncertainty |
| 同会话策略与首轮竞争会话锁 | TestT14StrategyAndFirstTurnSerializeOnDebate |

`internal/ai/strategy_test.go` 验证 Unicode 300/301、1–3 项、8 KiB、重复/额外字段、输出类型、双方立场与四流派数据边界，以及合成供应商成功/503/非法结构/截断仅一次请求。`internal/debate/strategy_test.go` 验证落库前再次校验仍保留已知用量。平台预算测试覆盖 POST 60 秒、GET 5 秒与较短父 Context。

从 backend 执行普通单测：

```powershell
$env:AI_MODE = 'disabled'
Remove-Item Env:SIYIGE_INTEGRATION, Env:SIYIGE_MIGRATION_TEST -ErrorAction SilentlyContinue
go test -count=1 ./internal/ai ./internal/debate ./internal/platform
```

数据库专项（密码文件取自己既有隔离环境的文件，绝不粘贴口令）：

```powershell
$env:APP_ENV = 'test'
$env:DB_ADDR = '127.0.0.1:13307'
$env:DB_NAME = 'siyige_test'
$env:DB_USER = 'siyige_test_app'
$env:DB_PASSWORD_FILE = '<测试应用账户密码文件绝对路径>'
$env:AI_MODE = 'disabled'
$env:SIYIGE_INTEGRATION = '1'
Remove-Item Env:SIYIGE_MIGRATION_TEST -ErrorAction SilentlyContinue
go test -race -count=1 -run TestT14 ./tests/integration
Remove-Item Env:SIYIGE_INTEGRATION -ErrorAction SilentlyContinue
```

## B 联调启动与交接消息

使用本分支完整代码；不要仅拷贝 Handler，避免遗漏 00013、Store、Gate、预算与恢复。现有本地隔离 MySQL 保持启动，先在 `backend/tools/migrate` 执行（只允许自己的隔离测试库）：

```powershell
go run . -env test -password-file '<测试迁移账户密码文件绝对路径>' -dir ../../migrations up
```

后端从 `backend` 启动，已执行的 HTTP 测试使用同一装配路径；本轮没有另启长期服务占用 B 端口：

```powershell
$env:APP_ENV = 'test'
$env:DB_ADDR = '127.0.0.1:13307'
$env:DB_NAME = 'siyige_test'
$env:DB_USER = 'siyige_test_app'
$env:DB_PASSWORD_FILE = '<测试应用账户密码文件绝对路径>'
$env:AUTH_ORIGIN = 'http://127.0.0.1:5173'
$env:HTTP_ADDR = '127.0.0.1:8080'
$env:AI_MODE = 'fake'
Remove-Item Env:SIYIGE_INTEGRATION, Env:SIYIGE_MIGRATION_TEST, Env:T07_FAKE_SCENARIO -ErrorAction SilentlyContinue
go run ./cmd/server
```

B 在自己的前端工作树沿用 `npm ci`、`npm run dev`，Vite 原路径代理 `/api/`。使用测试账号登录，从认证响应取得 Cookie/CSRF。服务 Ctrl+C 停止，保留数据库卷。默认 Fake 成功；失败/延迟/取消由上述测试脚本注入，未增加新的生产配置开关。

可直接发送给 B（发送由成员 A 决定，本轮未代发）：

> T14-STYLE 后端已在 codex/p3-a-integration / 草稿 PR #19 交付，保留 A1 契约，完整版本取本次交接 SHA。请先在本地隔离测试库应用 00013，再用 AI_MODE=fake 启动后端。创建请求可选 ai_style，仅接受 logical/aggressive/empathic/data，省略 logical，null/空串/未知/大小写不符/非字符串为 400；新会话仍 standard_4，创建零模型调用。创建后保存流派不再修改。策略 GET/POST 为 /api/v1/debates/{id}/strategy：进入/刷新/重登只 GET，点击才 POST {}；成功 201，completed 重放 200，generating 202，failed 普通重放 200。失败显式重试必须 retry:true + 最新 expected_attempt_version，旧版 409 时只 GET，不自动换版本重发。仅无任何 turn 可首次生成/重试，策略可跳过，不计轮次。正文只展示 thesis/arguments/risks 三字段并转义；completed 返回 schema_version、prompt_version、ai_style。请求未知字段拒绝 400；POST 仍带 Origin/Session/CSRF。后端 Fake/隔离 MySQL/race 已通过，失败与恢复场景见本专项记录。SSE、P1、真实模型和 AC16 浏览器整体验收未包含，请 B 留下四流派、刷新重登及失败恢复的前端证据。

## 验收结论与遗留

A 后端本地技术验收通过，待负责人确认及 B 前端 AC16 联调；不宣布完整 T14-STYLE 或 Phase 3 结项。没有新的已知后端阻塞；真实策略的专用费用准入装配、真实模型质量评测、SSE 及真实 Demo 仍属后续独立授权任务。既有 T07-COST-01 未关闭，Fake 不作为真实费用/质量或公网准入证据。最终提交与适用 CI 状态以本轮交接及 PR #19 Checks 为准。
