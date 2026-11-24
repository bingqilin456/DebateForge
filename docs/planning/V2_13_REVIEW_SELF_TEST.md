# V2-13 / V2-10 / V2-11 复盘切片自测记录

- 日期：2026-11-19（Asia/Shanghai）
- 工作树：`C:\Users\ZhuanZ1\.codex\worktrees\v2-review-13-10-11`（分支 `codex/v2-practice-review`，基线 `ccf4416`）
- 授权范围：本地实现、Fake/Mock 检查。**未执行** Commit、Push、Merge、PR、部署、数据库写入或真实模型调用。

## 1. 交付范围

按 [并行设计 04-review](v2-parallel/04-review.md) 串行实施四轮练习可运行切片：

| 任务 | 切片内容 | 状态 |
| --- | --- | --- |
| V2-13 赛后复盘 | 完赛原文独立可读；复盘独立持久化、显式触发、失败重试、刷新恢复；比赛完成与复盘状态分离 | 本地可运行（Fake/内存/前端 Mock） |
| V2-10 四维训练参考 | 论证结构/回应针对性/证据使用/概念与范围；1–4 或观察不足；服务端复核原文依据，无依据不赋档 | 本地可运行（Fake/内存/前端 Mock） |
| V2-11 单场雷达联动 | 四维定型后接雷达；缺项留空不按 0；文字卡与原文定位；批注/重练/争点/PDF 未稳定时并列待整合 | 本地可运行（Fake/内存/前端 Mock） |

不在本轮范围：V2-14 PDF、局部重练、争点卡联动、自动触发策略、训练赛阶段读入、真实模型质量验证。

## 2. 契约选择

1. **复盘与总结彻底分离**：新表 `debate_reviews`、新接口 `GET/POST /api/v1/debates/{id}/review`、新 DTO `ReviewStatus`。不扩展 `summary_status`，不读取也不输出旧四维 `schema_version=2`（0–100）。
2. **复用既有基础设施但不复用状态机**：复盘中复用 `attempt_version`、90 秒租约、受控错误码、Gate 配额、`Failure`/`FailureDetails` 信封、`commit-unknown` 语义与过期回收模式；但自有表、自有状态机、自有版本。
3. **整份原子写入**：首版 `completed` 一次性保存完整复盘；`failed` 只存错误码。因此重试是重生成整份复盘。**不做伪造的局部恢复**：若未来改分段保存，须显式记录已落库段落并只补失败部分。
4. **保留已成功内容**：复盘只写 `debate_reviews`，从不触碰 `debate_summaries`、`turn_annotations`、`debate_turns`。测试直接断言复盘失败/重试前后，既有总结与批注原样可读。
5. **无依据不赋档**：模型输出先过严格 JSON 结构校验，再由 debate 层用本场权威原文逐字复核引用。核对不到（外场/改写）→ 该维降级为“观察不足”并写原因，绝不补低档或 0 分；跨消息歧义 → 保留档级但标“待核对”，坐标置空。
6. **功能默认关闭**：`V2_REVIEW_FAKE=1` + `AI_MODE=fake` + `APP_ENV=test` + 生成器实现 `ai.ReviewGenerator`。关闭时写入口 400、读入口 `not_started`、不调用模型，且租约回收不扫描新表。
7. **迁移为新增正向 SQL，未执行**：`00016_create_debate_reviews.sql`。编号需在整合时按远程分支复核重排。

## 3. 修改文件

后端：

- `backend/internal/ai/review.go`（新增）：四维与复盘独立契约、严格 JSON 解析、Prompt 构建、真实/Fake 生成、`ScriptReviews`/`FailReviews`。
- `backend/internal/ai/budget.go`：`Fake` 增加复盘记录、脚本与失败注入字段（不影响其它能力）。
- `backend/internal/debate/review.go`（新增）：DTO、严格输入、认领/生成/失败编排、引用复核与降级、HTTP 路由与 Handler。
- `backend/internal/debate/review_repository.go`（新增）：`GetReview`/`ClaimReview`/`CompleteReview`/`FailReview`/租约恢复。
- `backend/internal/debate/model.go`：`Store` 增加复盘方法。
- `backend/internal/debate/service.go`：复盘开关字段；`RecoverExpiredLeases` 仅在启用时扫描新表。
- `backend/internal/debate/handler.go`、`repository.go`：注册路由、暴露启用时的复盘回收。
- `backend/cmd/server/review_fake_local.go`（新增）、`main.go`：默认关闭的启动装配。
- `backend/migrations/00016_create_debate_reviews.sql`（新增，未执行）、`backend/migrations/README.md`。

后端测试：

- `backend/internal/ai/review_v2_10_test.go`：四维固定顺序、档级 1–4/0/5/字符串、无依据不赋档、观察不足规则、旧总结形状拒绝、Prompt 隔离、四轮完整性、Fake 确定性。
- `backend/internal/debate/review_v2_13_test.go`：失败独立于完赛、刷新恢复、重试成功、成功内容与批注不被覆盖、并发点击只生成一次、已完成幂等。
- `backend/internal/debate/review_v2_10_test.go`：权威原文定位、外场/改写降级、跨消息歧义待核对、部分/全部观察不足、旧总结语义不变、未完成会话拒绝、功能关闭拒绝。
- `backend/internal/debate/review_helpers_test.go`：内存替身复盘方法与四轮夹具。
- `backend/internal/debate/review_http_test.go`：读取无模型、CSRF 403、非法正文 400、幂等 200、未完成 409、跨用户 404。
- `backend/internal/debate/service_test.go`：内存替身增加 `reviews` 与回收分支（不改既有断言）。
- `backend/cmd/server/review_fake_local_test.go`：开关白名单、生成器能力要求、关闭时不扫描新表。

前端：

- `frontend/src/types/api.ts`：`ReviewQuote`/`ReviewDimension`/`ReviewContent`/`ReviewStatus`/`ReviewRequest`。
- `frontend/src/shared/limits.ts`：复盘独立常量。
- `frontend/src/api/review.ts`（新增）：GET/POST 客户端与严格响应校验（四维顺序、观察不足、旧总结形状拒绝）。
- `frontend/src/debate/review.ts`（新增）：只读 GET + 显式触发状态机；有限轮询；409 只重读不自动换版本重发。
- `frontend/src/components/debate/ReviewPanel.vue`（新增）：复盘状态块、四维卡片、雷达（缺项留空）、原文定位按钮（键盘可达）、争点/优点/改进、待整合标注。
- `frontend/src/components/debate/FirstTurnPanel.vue`：复盘面板接入、消息 `data-message-id`/`data-message-content`、码点定位。

前端测试：

- `frontend/src/api/review.test.ts`、`frontend/src/debate/review.test.ts`、`frontend/src/components/debate/ReviewPanel.test.ts`。
- `frontend/e2e/review-mock.spec.ts` + `frontend/e2e/review-mock.playwright.config.ts`（独占 5174 端口，不占共享 5173）+ `package.json` 的 `test:e2e:review`。

文档：

- `docs/api/API.md`：新增 §16 复盘契约；边界段补充 V2-13 默认关闭说明。
- `backend/migrations/README.md`：00016 结构、未执行与并行编号待整合说明。
- 本文。

## 4. 测试命令与结果

后端（`backend/` 下）：

| 命令 | 结果 |
| --- | --- |
| `go vet ./...` | 通过（无输出） |
| `go build ./...` | 通过 |
| `go test ./... -count=1` | 通过：cmd/server、ai、auth、debate、platform、topic、tests/integration 全部 ok |
| `go test -race ./internal/debate/ -run 'TestReview\|TestT09\|TestAnnotation' -count=1` | 通过 |
| `gofmt -l internal/ai/review.go internal/debate/review.go internal/debate/review_repository.go cmd/server/review_fake_local.go` | 无输出（新增文件已格式化；仓库既有文件因 CRLF 在 `gofmt -l` 中整体列为未格式化，属基线现象，未批量改写） |

前端（`frontend/` 下）：

| 命令 | 结果 |
| --- | --- |
| `npm ci` | 通过（135 packages） |
| `npm run typecheck` | 通过 |
| `npm run test` | 通过：26 个文件、440 个用例（含新增 32 个复盘用例） |
| `npm run build` | 通过 |
| `npm run test:e2e:review` | 通过：3/3（独立 5174 端口 + 用例内隔离 Mock，无后端/数据库/真实模型） |

## 5. 未执行项

- 未执行任何数据库 SQL（含 `00016`），未连接隔离库，未设置 `SIYIGE_INTEGRATION`/`SIYIGE_MIGRATION_TEST`。
- 未调用真实模型；Fake 结果只验证结构与恢复，不验证输出质量。
- 未执行 `npm run test:e2e`（既有 `views/style/stream`）与 live/b2:live 用例；本轮改动未触及这些路径，按影响范围未复跑。
- 未做 Commit/Push/Merge/PR/部署。
- 未做真实样例人工标注与一致率评估（Q49 要求另行授权）。

## 6. 确定性缺陷修复（2026-11-19 第二轮）

在本切片既有实现上修两处确定性缺陷，不新增能力、不改契约外形、不实现第二批：

1. **关闭时 GET /review 仍查新表（已修）**：原实现无条件走 `Repository.GetReview`，旧环境未执行
   `00016` 时该表不存在，读取被映射成 `503 dependency_unavailable`，与“关闭即不访问新表”的既有
   开关契约冲突。现 `Service.GetReview` 在功能关闭时只调用既有轻量只读接口
   `store.Detail(ctx, uid, id)` 做会话归属校验：非法 ID、不存在与越权仍为 `404`（不会对任意 ID
   一律回 `not_started`），校验通过后投影 `not_started`，全程不触碰复盘存储、不调用模型、不改其它功能。
2. **POST /review 同步等待生成却走 5s 普通预算（已修）**：生成在请求内同步等待一次模型调用
   （上限 `ai.DefaultModelTimeout`=45s），普通 5s 预算会先切断。现把已精确匹配的 POST 路由
   `/api/v1/debates/:id/review` 加入既有生成预算判断（`ai.DefaultRequestBudget`=60s）；GET 读取仍为
   5s。判断依旧依赖 Gin 已匹配的 `c.FullPath()`，客户端无法用伪造后缀换取长预算。

新增回归：

- `backend/internal/debate/review_http_test.go`：`TestReviewDisabledReadProjectsNotStartedWithoutTouchingReviewTable`
  用探针让 `GetReview` 返回“表不存在”的依赖失败，证明关闭时（1）本场投影 `not_started`、
  （2）不存在与跨用户仍为 404、（3）`GetReview` 调用次数为 0。
- `backend/internal/platform/business_test.go`：预算表补 `POST/GET/POST+短父期限` 三行复盘路径。
- `backend/internal/platform/business_review_budget_test.go`（新增）：`TestBusinessRouterReviewBudgetThroughRealMiddleware`
  走 `NewBusinessRouter` 装配的真实业务路由与真实认证中间件，在认证读取 Session 时记录请求期限，
  断言 POST 拿到约 60s、GET 保持约 5s，并顺带确认关闭时 `GetReview` 仍为 0 次。

两处修复均先写回归并确认失败（关闭分支原为 `503`；POST 原为 `remaining≈5s`），再改实现转绿。

### 本轮命令与结果（`backend/` 下，已清空 `SIYIGE_INTEGRATION`/`SIYIGE_MIGRATION_TEST`，`AI_MODE=disabled`）

| 命令 | 结果 |
| --- | --- |
| `go test ./internal/debate ./internal/platform -count=1` | 通过（两包均 ok） |
| `go test -race ./internal/debate -count=1` | 通过 |
| `go test -race ./internal/platform -count=1` | 通过 |
| `go vet ./internal/debate ./internal/platform` | 通过（无输出） |
| `go build ./...` | 通过 |
| `gofmt -l`（本轮新增/改写的 `review.go`、两个测试文件） | 无输出 |

`gofmt -l` 仍会列出 `internal/platform/business.go`、`business_test.go`：这两个文件在本轮之前就是
CRLF 检出（未改动的 `router.go`、`annotation.go` 同样被列出），属基线现象，未批量改写行尾。

未执行：数据库/迁移、真实模型、Commit、Push、Merge、PR、部署，未触碰其它工作区。

## 7. 同切片追加修正（2026-11-19 第三轮）

同一复盘切片的两项补强，仍不新增能力、不改其它功能：

1. **空库全量迁移断言纳入 00016**（`backend/tools/migrate/integration_test.go`，未运行数据库）：
   版本历史条数 16→17（Goose 初始 0 + 十六个版本）、逐版本成功记录 `BETWEEN 1 AND 16` 且计 16、
   `status` 输出 16 条 applied、存储引擎/字符集与自增主键核对扩到十张业务表（新增
   `debate_reviews`）、外键总数 9→10。
2. **`debate_reviews` 结构与 FK 断言**：新增夹具断言会话唯一（重复 `debate_id` 为 1062）、
   非法外键为 1452、`attempt_version` 与 `status` 的 CHECK 为 3819、未完成时
   `schema_version`/`content_json`/`error_code`/用量均为 NULL、`status` 字节精确
   （`debate_reviews` 加入 PAD SPACE 反例表）；并直接核对约束名
   `fk_debate_reviews_debate` 的删除/更新规则为 `RESTRICT`、引用表为 `debates`，
   以及写入复盘行不改变既有总结行数。`rowCounts` 同步扩到十张表。
   **本轮未连接数据库、未执行 `up/status`，以上断言的实际运行留待授权隔离库。**
   `backend/migrations/README.md` 的 00016 小节同步改为“已补充断言、未运行”，避免保留“未新增断言”的
   过时描述；该小节属复盘切片文档，未改动其它迁移条目。
3. **关闭时读取新增可选 `available:false`**：`GET /review` 在功能关闭时返回
   `{"status":"not_started","attempt_version":null,"available":false}`；启用时省略该字段，
   旧响应与旧 Mock 的 `not_started` 形状不变。前端据此隐藏整个复盘区、不暴露生成入口，
   并让状态机 `canGenerate/canRetry` 与 `start()` 一律拒绝。

新增/更新回归：

- `backend/internal/debate/review_http_test.go`：关闭分支断言 `available:false`；
  `TestReviewHTTPBoundary` 断言启用时**不输出** `available`。
- `frontend/src/api/review.test.ts`：`isReviewStatus` 接受 `available` 省略/`true`/`false`，
  拒绝非布尔值、`null` 以及把 `available` 挂到非 `not_started` 状态。
- `frontend/src/debate/review.test.ts`：`available:false` 时 `unavailable=true`、
  `canGenerate=false` 且 `generate()` 不产生 POST；省略 `available` 的旧响应仍按可用处理。
- `frontend/src/components/debate/FirstTurnPanelReview.test.ts`（新增）：走真实
  `FirstTurnPanel`，`available:false` 时断言无 `[data-review-panel]`、无生成按钮、
  无复盘提示文案，且完赛原文与总结区不受影响；省略 `available` 时完整面板照常渲染。
  去掉面板的 `!reviewUnavailable` 条件后该用例失败，恢复后通过。

### 本轮命令与结果

`backend/` 下（清空 `SIYIGE_INTEGRATION`/`SIYIGE_MIGRATION_TEST`，`AI_MODE=disabled`）：

| 命令 | 结果 |
| --- | --- |
| `go test ./internal/debate ./internal/platform ./cmd/server -count=1` | 通过 |
| `go vet ./internal/debate ./internal/platform` | 通过（无输出） |
| `go build ./...` | 通过 |

`backend/tools/migrate/` 下（`SIYIGE_MIGRATION_TEST` 未设置，数据库用例默认跳过）：

| 命令 | 结果 |
| --- | --- |
| `go vet ./...` | 通过 |
| `go test ./... -count=1` | 通过（集成用例因开关未设置而跳过） |
| `go build ./...` | 通过 |

`frontend/` 下：

| 命令 | 结果 |
| --- | --- |
| `npm run typecheck` | 通过 |
| `npm run test` | 通过：27 个文件、445 个用例 |

未执行：任何数据库 SQL（含 00016 的 `up/status`）、真实模型、Commit、Push、Merge、PR、部署。
`gofmt -l` 在 `backend/tools/migrate` 与本轮前一样列出该模块全部文件（基线 CRLF 检出），
未批量改写行尾；新增前端文件与后端改动文件按既有行尾习惯写入。

## 8. 待整合事项

1. **迁移编号**：`00016` 可能与并行分支冲突；整合时按最终顺序重排，再在隔离库执行并更新空库预期计数。
2. **PDF（V2-14）**：当前页脚为禁用的“待整合”按钮；后续应复用已保存复盘，零新增模型调用。
3. **局部重练（V2-08）**：维度卡的“去重练”在未接入时显示“待整合”；接入后替换为真实入口。
4. **争点卡（V2-06/07）**：主要争点暂由复盘结果给出；接入争点卡后改为读取已保存卡片与定位。
5. **训练赛阶段读入（V2-01/02）**：当前以 `debates.status=completed` + `completed_rounds` 判定完赛；若 01 引入训练赛阶段模型，需改为消费其权威完赛信号。
6. **自动生成策略**：按用户指令保持“用户主动发起”，不自动触发；PRD 中该点仍为待决。
7. **真实模型质量**：需要单独授权、样例与判定表后才能评估四维判定一致性。
8. **OpenAPI 登记**：`docs/api/openapi.yaml` 未登记复盘两个路径，留待整合负责人统一落笔，避免并行争写机器规范。
9. **批注跳转**：复盘维度卡目前只做原文定位；"跳到对应批注"要等批注锚点与复盘引用共用稳定读法后再接。
