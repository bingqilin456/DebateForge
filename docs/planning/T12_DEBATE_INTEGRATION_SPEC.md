# T12 核心辩论页面联调 Spec

- **任务编号与负责人**：T12 / A 主责（前端核心状态机、三轮与重试、自动总结与结构化面板、AC01–AC08 联调），B 参与样式与验收
- **关联需求与契约**：PRD `F02–F06, AC01–AC08` · ARCHITECTURE [会话与轮次状态机](../architecture/ARCHITECTURE.md#会话与轮次状态机) / [AI 上下文与总结](../architecture/ARCHITECTURE.md#ai-上下文与总结) · API [业务接口与恢复规则](../api/API.md#phase-1-业务接口)
- **前置依赖与授权边界**：T08、T09、T10、T11 后端接口已就绪；本轮授权完成 `frontend/` 联调与本地 `AI_MODE=fake` 下 AC01–AC08 验收，不调用真实收费模型，不自动 commit/push。

## 1. 目标与非目标（Scope & Non-Goals）
- **本任务交付**：
  1. 在 `frontend/src/api/client.ts` 与 `frontend/src/api/debate.ts` 完整接入 1–3 轮提交、失败轮显式重试、总结生成/查询/显式重试及本人历史分页查询与运行时契约校验。
  2. 在 `frontend/src/debate/firstTurn.ts` 与 `frontend/src/components/debate/FirstTurnPanel.vue` 实现三轮连续推进、`generating` 期间禁重发、第 3 轮完成后自动请求一次总结（及刷新 `completed` 且 `summary_status=not_started` 会话时自动补发一次）、失败轮与失败总结携带所见 `expected_attempt_version` 的显式重试。
  3. 新增 `frontend/src/components/debate/SummaryPanel.vue` 安全渲染结构化总结（`strengths`、`issues`、`suggestions`），纯文本转义展示，不直接渲染原始 JSON 或不可信 HTML。
- **明确不做**：
  - 不修改后端业务契约或数据库迁移；不启用真实收费模型；不实施 T15 可选 SSE 流式渲染。

## 2. 契约与数据增量（Contract & Schema Delta）
- **API 变更/晋升**：复用既有已冻结接口（`GET /api/v1/topics`、`GET /api/v1/debates`、`POST /api/v1/debates`、`GET /api/v1/debates/{id}`、`POST /api/v1/debates/{id}/turns`、`POST /api/v1/debates/{id}/turns/{turn_id}/retry`、`POST /api/v1/debates/{id}/summary`、`GET /api/v1/debates/{id}/summary`），无后端契约变更。
- **数据库/迁移增量**：无。

## 3. 状态流转与关键不变量（State & Invariants）
- **三轮顺序与等待锁**：仅当 `debate.status === 'active'`、`completed_rounds < 3` 且不存在 `generating` 或 `failed` 轮次时允许提交下一轮 `round_no = completed_rounds + 1`；请求在途或轮询 `generating` 期间禁用提交与重试按钮。
- **自动总结单次触发守卫**：第 3 轮完成（`debate.status === 'completed'`）且 `summary_status.status === 'not_started'` 时自动调用一次 `POST /api/v1/debates/{id}/summary`（`{"retry":false}`）；刷新已 `completed` 且 `not_started` 的会话同样自动补发一次；若总结状态为 `failed`，仅展示手动重试按钮，绝不自动循环调用。
- **显式重试版本约束**：轮次重试与总结重试必须携带界面当前所见的 `expected_attempt_version`；收到 `409 attempt_version_conflict` 或其他错误后，仅重新查询服务端状态并展示最新结果与版本号，严禁前端自动替换版本号重发。
- **刷新恢复与纯文本安全**：刷新页面或恢复会话先通过 `GET /api/v1/debates/{id}` 查询服务端真实状态；所有用户发言、AI 回复与总结条目均通过 Vue 文本插值转义渲染。

## 4. 实施子切片与文件范围（Slices & Files）
- **Slice 1（API 客户端与契约校验）**：扩展 `SummaryStatus`、`SummaryContent`、`DebateListResult` 类型与校验器，新增 `list`、`retryTurn`、`summary`、`requestSummary` 及 1–3 轮 `send` — 涉及文件：`frontend/src/api/client.ts`、`frontend/src/api/debate.ts`、`frontend/src/api/debate.test.ts`
- **Slice 2（三轮状态机、重试与 SummaryPanel 组件）**：实现三轮连续状态流转、单次自动总结守卫、409 版本冲突只查不重发及 `SummaryPanel.vue` — 涉及文件：`frontend/src/debate/firstTurn.ts`、`frontend/src/debate/firstTurn.test.ts`、`frontend/src/components/debate/SummaryPanel.vue`、`frontend/src/components/debate/FirstTurnPanel.vue`、`frontend/src/App.vue`、`frontend/src/App.test.ts`

## 5. Spec 验收矩阵（Verification Matrix）
| 场景编号 | 输入 / 前置状态 | 预期响应 / 状态变化 | 验证层级（Unit / MySQL / FE / Browser） |
| --- | --- | --- | --- |
| S01 | 连续提交第 1、2、3 轮有效发言，期间重复点击发送 | 依次发送 `round_no=1,2,3`，`generating` 期间禁重发；第 3 轮完成后自动请求一次总结并展示 | FE Unit + Fake 联调（AC03, AC06） |
| S02 | 刷新 `completed` 会话：`summary_status` 为 `not_started` vs `failed` | `not_started` 自动补发一次总结；`failed` 不自动请求，仅显示手动重试按钮 | FE Unit + Fake 联调（AC06, AC07） |
| S03 | 轮次 `failed`（`attempt_version=1`）点击重试；模拟返回 `409 attempt_version_conflict` | 请求携带 `expected_attempt_version=1`；遇 `409` 只重新查询详情并更新展示为新版本，不自动换版本重发 | FE Unit + Fake 联调（AC04, AC05） |
| S04 | 总结 `failed`（`attempt_version=1`）点击重试；模拟返回 `409 attempt_version_conflict` | 请求携带 `retry=true, expected_attempt_version=1`；遇 `409` 只查询最新状态，不自动重发 | FE Unit + Fake 联调（AC06） |
| S05 | 总结或消息含 `<script>` / `<img onerror>` 等字符串 | `SummaryPanel` 与消息列表均转义为纯文本展示，不生成危险 DOM 节点，不直接渲染原始 JSON | FE Unit（AC06, AC10） |
| S06 | 本地隔离 MySQL + Fake 模式下执行 AC01–AC08 全链路 | 注册/登录/退出、5 题选立场、三轮+第 4 轮拒绝、失败重试与 409、幂等与租约恢复、总结与重试、历史分页重读、跨用户 404 全部通过 | 本地真实前后端 + 隔离 MySQL（AC01–AC08） |

## 6. 交付检查清单（Definition of Done）
- [x] Spec 验收矩阵全部用例覆盖并通过
- [x] 前端 `npm run typecheck`、`npm run test`、`npm run build` 全部通过
- [x] 本地隔离 MySQL + `AI_MODE=fake` 完成 AC01–AC08 全链路验证
- [x] 专项验证证据写入 `docs/acceptance/T12_DEBATE_INTEGRATION_ACCEPTANCE.md`，任务状态更新为 `待人工验收`
