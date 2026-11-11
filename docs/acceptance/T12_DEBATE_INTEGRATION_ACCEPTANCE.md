# T12 核心辩论页面联调验收记录

- **任务编号**：T12 核心辩论页面联调（Phase 2）
- **日期**：2026-11-11
- **主责**：成员 A（辩论页三轮状态机、显式重试、自动请求总结、`SummaryPanel` 结构化安全渲染与 AC01–AC08 联调）
- **对应 Spec**：[T12_DEBATE_INTEGRATION_SPEC](../planning/T12_DEBATE_INTEGRATION_SPEC.md)

## 1. 交付文件范围

- 前端 API 客户端与契约校验：
  - `frontend/src/api/client.ts`（补充 `409 attempt_version_conflict` 受控错误提示映射）
  - `frontend/src/api/debate.ts`（接入 1–3 轮发言、失败轮显式重试 `retryTurn`、总结查询 `summary` 与生成/重试 `requestSummary`、本人历史分页 `list` 及结构化总结 Schema 校验器）
  - `frontend/src/api/debate.test.ts`（新增多轮发言、轮次重试与 409 版本冲突、总结 Schema 校验、历史分页边界测试）
- 前端辩论状态机与组件：
  - `frontend/src/debate/firstTurn.ts`（扩展为 1–3 轮连续推进、生成中禁重发、第 3 轮完成后自动请求一次总结、刷新 `completed` 且 `not_started` 会话自动补发一次、`failed` 仅显示手动重试且绝不循环调用、轮次与总结重试携带所见 `expected_attempt_version` 并在 409 后只重新查询展示不自动重发）
  - `frontend/src/debate/firstTurn.test.ts`（覆盖三轮连续提交、第 4 轮拒绝、轮次与总结失败显式重试、409 版本冲突只查不重发、刷新恢复与单次自动总结守卫）
  - `frontend/src/components/debate/SummaryPanel.vue`（固定结构化三栏组件，安全展示 `strengths`、`issues`、`suggestions`，纯文本转义渲染，不直接渲染原始 JSON 或不可信 HTML）
  - `frontend/src/components/debate/FirstTurnPanel.vue`（整合三轮发言表单、轮次失败重试按钮、`SummaryPanel` 挂载及本人历史分页列表与会话继续入口）
  - `frontend/src/App.vue`、`frontend/src/App.test.ts`（更新范围提示并新增三轮发言 + 失败重试 + 总结手动重试 + XSS 转义端到端组件测试）
- 任务文档：
  - `docs/planning/T12_DEBATE_INTEGRATION_SPEC.md`
  - `docs/acceptance/T12_DEBATE_INTEGRATION_ACCEPTANCE.md`
  - `docs/planning/TASKS.md`、`docs/README.md`

## 2. 执行的验证命令与结果

### 2.1 前端类型检查、单元测试与生产构建

在 `frontend/` 下执行：

```bash
npm run typecheck
npm run test
npm run build
```

- `npm run typecheck`（`vue-tsc -b`）：**通过（0 错误）**
- `npm run test`（`vitest run`）：**10 个测试文件、153 个测试用例全部通过**
- `npm run build`（`vite build`）：**通过**（产物 `dist/index.html`、`dist/assets/index-*.css`、`dist/assets/index-*.js` 正常生成）

### 2.2 隔离 MySQL 集成测试与本地 Fake 模式 AC01–AC08 全链路验证

在隔离测试数据库 `siyige-t02-mysql-test-1`（`127.0.0.1:13307`，`siyige_test`）及 `AI_MODE=fake`（全程零真实模型收费调用）下执行后端集成测试与经由 Vite 代理（`/api/` -> `127.0.0.1:8080`）的端到端链路核验：

| 验收项 | 验证内容 | 实际结果 |
| --- | --- | --- |
| **AC01** | 注册新用户、重复注册 `409 username_conflict`、错误密码 `401 invalid_credentials`、退出 `204` 后访问 `/api/v1/auth/me` 返回 `401 unauthenticated`、重新登录恢复 `200` | **通过** |
| **AC02** | 查询 `/api/v1/topics` 返回 5 条预设中文辩题；分别以 `pro`/`con` 创建会话返回相反 `ai_stance`；非法立场返回 `400 invalid_request`，不存在辩题返回 `404 not_found` | **通过** |
| **AC03** | 连续提交第 1、2、3 轮发言均返回 `201`，`completed_rounds` 依次为 `1 → 2 → 3`，第 3 轮完成后 `debate.status` 置为 `completed` 并落库 6 条正式消息；继续提交第 4 轮返回 `409 round_conflict`，前端界面隐藏第 4 轮输入框 | **通过** |
| **AC04** | 第 2 轮模拟超时/失败返回 `504/502`，完成轮数保持 1，保留用户原文与失败版本；携带 `expected_attempt_version` 显式重试成功后完成轮数变为 2；重试再次失败后重发旧版本返回 `409 attempt_version_conflict`，前端只重新查询展示最新状态与版本号，不自动替换版本号重发 | **通过** |
| **AC05** | 同幂等键重发返回 `200` 复用已完成结果，`generating` 期间返回 `202` 并按 2 秒间隔轮询状态；过期 `generating` 租约（90 秒）由后台恢复扫描转为 `failed`，迟到旧版本结果不覆盖新尝试 | **通过** |
| **AC06** | 第 3 轮完成后自动触发一次 `POST /api/v1/debates/{id}/summary`（`{"retry":false}`），返回 `201` 与 `schema_version=1` 的 `strengths`/`issues`/`suggestions` 三栏结构；未完成会话请求总结被拒（`409 round_conflict`）；总结失败时辩论记录完整保留，仅显示手动重试按钮且绝不循环请求；`SummaryPanel` 对 `<script>`/`<img onerror>` 等字符串按纯文本转义展示 | **通过** |
| **AC07** | 刷新页面先调用 `GET /api/v1/debates/{id}` 读取已落库的会话、6 条消息与总结；本人历史分页接口 `GET /api/v1/debates?page=1&page_size=10` 正常返回，未完成会话可从历史进入并继续下一轮 | **通过** |
| **AC08** | 用户乙查询本人历史列表不包含用户甲数据；用户乙访问用户甲的会话详情、提交轮次、重试轮次、生成/查询总结接口均统一返回 `404 not_found` | **通过** |

## 3. 未执行项与遗留边界

- 本轮全部验证均在 `AI_MODE=disabled` 或 `AI_MODE=fake` 下完成，**未发起任何真实收费模型调用**。
- 公网 HTTPS 部署回归（AC09）与备份恢复演练（AC10 运维部分）按计划归属 Phase 4（T16/T17）。
- 按本轮指令未自动执行 `git commit` 或 `git push`，待项目负责人人工验收确认。
