# 02 争点卡与双方原文定位

范围为 V2-06、V2-07，对应 [PRD §6](../../product/PRD.md#6-特色二争点卡与回应定位)和[工作台](../../product/ui/03-workspace.md)。目标覆盖 AC-V2-18、19，V2-05 异议与 AC-V2-20 以独立切片交付。

## 设计与边界

- 卡片保留稳定身份，记录标题、提出位置、相关回应和仍待澄清的问题。默认展示最多 5 张。回应是否切中问题的解释带双方原句引用，保留用户自行核对的空间。
- 复用 [V2-04 锚点设计](../V2_04_ANNOTATION_ANCHOR_DESIGN.md)来解析同场已保存的原文。精确命中才定位；重复、过期或失效的锚点显示“待核对”，卡片仍可读。越权按统一 404，用户与模型文本纯文本渲染。
- 优先用四轮完赛夹具完成赛后整理链路。训练赛再接 01 的已保存事件，依次覆盖驳立论完成、每个完整自由辩回合完成、结辩完成。相同源位置去重。分析慢、失败或限流时保留已有卡片与辩论推进。
- 人工改名、合并、移除优先。推荐对同场编辑做版本检查，并为移除留下足够的拒绝旧结果信息。晚到分析只补可合并的相关回应或待澄清内容，人工编辑冲突须可见。具体存储在 TE-09、TE-11 的评估里定型。
- 桌面接工作台栏位，窄屏用独立区块。四轮场中保持批注入口，争点卡在赛后集中展示。刷新后读取保存的卡片和人工编辑。

## 执行与验收

1. 用 V2-04 锚点准备准确、组合字符、重复和失效四类夹具。先验证原文定位与降级。
2. 在独立争点模块实现版本检查与更新合并。用 Fake 验证重复触发、晚到与人工改名、合并或移除竞争，以及补失败时保留已成功卡片。训练赛触发和公共工作台接线在 01 契约稳定后整合。
3. 用前端 Mock 验证 5 卡上限、双方定位、待核对降级、人工编辑后刷新、越权和失败提示。检查分析并发归属与正式发言隔离方案 TE-12。

## 交接记录（2026-11-19，V2-06/V2-07 独立切片）

本段只记录独立模块已实现与已自测的范围。**未做阶段签收**，共享装配、公共页面与生产持久化仍属整合项。

### 已实现（独立模块，未改共享装配）

后端 `backend/internal/debate/`：

| 文件 | 职责 |
| --- | --- |
| `issue_card.go` | 契约常量、DTO、归属/资格判定、锚点复核、人工编辑校验、`IssueCardService`（读取/整理/编辑） |
| `issue_card_store.go` | `IssueCardStore` 持久化边界接口（读取、认领、完成、失败、编辑） |
| `issue_card_memory.go` | 并发安全内存 Fake：单锁临界区等价于短事务，含 CAS 与晚到合并规则 |
| `issue_card_fake.go` | `FakeIssueCardAnalyzer`（可注入 candidates/error/gate）与默认确定性候选生成 |
| `issue_card_handler.go` | 独立 Gin handler 与 `RegisterIssueCardRoutes`（不改 `platform`/`handler.go`） |

前端：

| 文件 | 职责 |
| --- | --- |
| `frontend/src/types/issueCard.ts` | 契约类型（`IssueCardList`/`IssueCardItem`/锚点/编辑请求） |
| `frontend/src/api/issueCard.ts` | 请求封装（复用 `src/api/client.ts`，严格响应校验，无自动重试） |
| `frontend/src/debate/issueCard.ts` | 纯计算：引用回读（复用 V2-04 `verifiedRange`）、5 卡投影、状态与确认文案 |
| `frontend/src/components/debate/IssueCardPanel.vue` | 卡片面板：展示、整理、改名/合并/移除、冲突弱提示 |
| `frontend/src/components/debate/IssueCardCitationText.vue` | 单条双方引用，可定位/待核对两种形态 |
| `frontend/src/preview/issueCard/*` + `frontend/issue-card-preview.html` | 隔离 Mock harness（独立端口 5273，注入式内存持久化边界） |
| `frontend/e2e/issue-card*.ts` | 隔离浏览器 Mock 验收（独立配置与端口，不占用 5173） |

### API 与存储语义（供整合复用）

- `GET /api/v1/debates/{id}/issue-cards`：Session 即可；返回 `state`（`not_ready`/`unsupported`/`ready`）、`display_cap`、`analysis`、`items`。无模型调用、不写库。
- `POST /api/v1/debates/{id}/issue-cards/analyze`：要求 Origin + CSRF；正文 `{retry?, expected_attempt_version?}`。首次整理 201，重复触发读回既有结果 200，分析槽忙 202（不排队）。
- `POST /api/v1/debates/{id}/issue-cards/{card_id}/edit`：要求 Origin + CSRF；正文 `{expected_version, action, title?|target_card_id?}`，`action ∈ rename|merge|remove`，成功 200。
- 错误映射（由 `issueCardHTTPError` 局部包装，不改共享 `serviceError`）：版本过期 `409 issue_card_version_conflict`；未完赛 `409 issue_card_not_ready`；历史 `quick_3` `409 issue_card_mode_unsupported`；非本场/越权/不存在统一 `404 not_found`；格式错误 `400 invalid_request`。
- 存储事务/CAS 条件：认领与写回都要求 `status=generating` 且 `attempt_version` 一致，旧响应不得覆盖新尝试；人工编辑要求卡片仍为 `active` 且 `version == expected_version`；合并把被合并卡置为 `merged` 并记录目标卡，已 `removed` 的卡不因再次分析复活，`merged` 来源只补到目标卡。
- 写回错误语义（`CompleteIssueCardAnalysis` 必须区分，生产实现不得混淆）：**确定未提交**返回 `ErrIssueCardVersionConflict` 或 `errors.Join(ErrIssueCardNotCommitted, cause)`，服务层据此按同一 `attempt_version` 落 `failed` + `dependency_unavailable`，返回 503，用户可带该版本显式重试且已有卡片不变；**提交结果不明**返回未标记的普通错误，服务层返回 503 且 `CommitUnknown`，不写失败、不自动重跑，由读取状态恢复。分析器自身的失败（`model_*`）仍按原路径落 `failed`。
- 失败清理 context：脱离请求取消（`WithoutCancel`）但固定 3 秒期限，避免用户断开后分析永久停在 `generating`；清理写回仍走同 `attempt_version` 的 CAS，成功状态（`completed`）不会被覆盖。
- 稳定身份：卡片以“提出位置的消息 + 逐字原文”为 `SourceKey` 归并，重复整理与晚到分析都不新建同源卡、不改卡 ID。

### 正式挂载点（整合负责人落笔）

1. `backend/internal/platform/business.go`：在 `NewBusinessRouter` 里构造 `debate.NewIssueCardService(store, analyzer)` 与 `NewIssueCardHandler(...)`，调用 `RegisterIssueCardRoutes(group, a)`。
2. 同文件 `requestBudget`：把 `POST /api/v1/debates/:id/issue-cards/analyze` 加入长预算路径（与其它生成入口同预算）。
3. 生产 `IssueCardStore` 实现需要新增迁移（争点卡表 + 分析记录表）与实体表，按 TE-09/TE-11 定型；**本切片未新建迁移号、未建表、未连库**。
4. 前端正式挂载：`FirstTurnPanel.vue` 右栏（桌面）/主卡片下方（窄屏）挂 `IssueCardPanel`，`contents` 由 `detail.turns[].messages` 派生；赛后集中展示，四轮场中保留批注入口。

### 依赖关系

- **01 训练赛**：场中触发（驳立论完成、每个完整自由辩回合、结辩后）需要 01 提供已提交阶段事件；本切片只做赛后集中整理，未接场中触发。
- **TE-09**：表结构（独立表 vs 复用）未定型，本切片只给 Store 接口与内存 Fake。
- **TE-11**：合并策略已在内存 Fake 中定为“版本 CAS + 软状态（removed/merged）+ 字段级追加”，生产表需保留 `status`、`merged_into`、`source_key`、`version`。
- **TE-12**：分析使用 `IssueCardService` 自己的单槽（容量 1、不排队），不占用正式发言 Gate；真实供应商接入仍需评估分池与限额。
- 正式发言额度：整理与编辑都不经过 `Gate`，与正式发言/总结配额独立。

### 已执行验证（2026-11-19）

- `go test ./...`、`go vet ./...`、`go build ./...`、`gofmt -l cmd internal tests`：全部通过（新增文件 gofmt 无输出）。
- `go test -race ./internal/debate/ -run TestIssueCard`：通过。
- 新增 Go 测试 18 个（含真实交错：认领后阻住分析 → 改名/合并/移除 → 放行 → 校验人工优先；HTTP 守卫含 409 冲突、404 越权/不存在、400 非法体、隔离状态）。
- `npm run typecheck`、`npm run test`（471 条，含新增 63 条争点卡测试）、`npm run build`：通过。
- 隔离浏览器 Mock：`npx playwright test --config e2e/issue-card.playwright.config.ts` → 10/10 通过（独立端口 5273，内存夹具 + sessionStorage 持久化边界）。

### 可恢复性补丁验证（2026-11-19 第二批，仅后端）

补丁范围：`issue_card.go`（写回错误分流 + 有界清理）、`issue_card_store.go`（`ErrIssueCardNotCommitted` 与契约注释）、`issue_card_recovery_test.go`（新增 Fake Store 回归）。前端 slice 未改。

- 回归用例 4 条（Fake Store 注入，先红后绿）：
  - `TestIssueCardCompleteDeterminateFailureMarksFailedAndRetriesWithSameVersion`：分析合法但写回确定未提交 → `failed` + `dependency_unavailable`，同 `attempt_version` 可显式重试且卡 ID/版本不变（修复前永久 `generating`）。
  - `TestIssueCardCompleteUnknownCommitStaysGenerating`：提交不明 → `CommitUnknown`，不写失败、不自动重跑、保留在途状态。
  - `TestIssueCardFailureCleanupUsesBoundedDetachedContext`：请求取消后仍落一次失败，清理 context 有期限且不沿用取消（修复前无 deadline）。
  - `TestIssueCardFailureWriteNeverOverwritesCommittedSuccess`：误标未提交但实际已提交时，失败清理被 CAS 拒绝，`completed` 与卡片内容不被覆盖。
- 命令与结果：`go test ./internal/debate -run TestIssueCard -count=1` 通过；`go test -race ./internal/debate -run TestIssueCard -count=1` 通过；`go test ./internal/debate -count=1` 通过；`go vet ./internal/debate`、`go build ./...` 无输出。
- 环境：未设置 `SIYIGE_INTEGRATION`/`SIYIGE_MIGRATION_TEST`，`AI_MODE=disabled`；未连数据库、未跑迁移、未调用真实模型。
- 未做：未新增数据库/迁移、未改共享路由与页面、未改前端、未 Commit/Push/PR/merge；`errors.Join(ErrIssueCardNotCommitted, ...)` 的标记目前只有 Fake 测试覆盖，生产 `IssueCardStore` 实现仍需按该契约区分“确定未提交”与“提交不明”（属整合批次）。

### 未验证 / 未执行

- 未接真实模型、未连数据库、未执行迁移、未部署、未 Commit/Push/PR/merge。
- 未做共享 router/server 装配，因此**真实 HTTP 路由当前未对外暴露**；上述 HTTP 断言全部在独立测试 router 内完成。
- 未验证场中触发、真实供应商质量、跨设备并发与生产持久化；未跑 live 链路浏览器用例。
