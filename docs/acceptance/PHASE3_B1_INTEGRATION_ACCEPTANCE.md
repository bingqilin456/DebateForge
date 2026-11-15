# Phase 3 B1 指定提交整合记录

日期：2026-11-15（Asia/Shanghai）。成员 A；分支 `codex/p3-a-integration`。本轮仅完成指定 B1 合入及受影响验证，待负责人确认，不代表完整 A4/B3 或 Phase 3 验收通过。

## 基线与合入

- 开始时集成 worktree 干净，HEAD 为 `72d2e3d20a62396fb245282e18c736e145d6cd25`；根工作区属于另一分支，本轮不修改该工作区。
- fetch 核对远端 `feat/t14-style-frontend` 头为指定 `a1e613b6c04243e6599316fa580a9242aa2d8060`，相对共同基线只有该 B 提交。
- 无冲突合入，合并提交 `df4e713d1e665e75a43e8ba39f388be3a1c7b6a4`，两个父提交依次为上述 A/B SHA。B 的 18 个文件只涉及前端和 [B 原始验收记录](T14_STYLE_FRONTEND_ACCEPTANCE.md)；没有覆盖 A 后续后端、契约、SSE、Demo 费用保护及运行配置。
- 后续提交仅更新文档；最终交付 SHA 由本轮最终回复及 PR #19 说明固定，普通 Push 到本分支。

## 契约接线

核对 [API](../api/API.md)、[T14 Spec](../planning/T14_STYLE_SPEC.md)、后端 handler/strategy DTO 与前端调用及组件：两个创建入口均传可选 `ai_style`，省略仍由后端默认 logical；仅展示已保存流派。策略 GET/POST 路径一致，首次 `{}`，显式失败重试携带 `retry:true` 与查询所得 `expected_attempt_version`；成功 201/200、生成中 202、409 只查询恢复。进入、刷新和重登不自动生成，已有 turn 不允许首次生成/重试，策略等待不阻断发言。内容以文本转义展示。

沿用 `phase3-contract-2026-11-14`、17 个 implemented 操作、strategy-v1/schema 1、summary-v1/schema 1；本轮没有修改正式契约或后端装配。B2 SSE 生产解析器没有随本提交交付。

## 实际验证

环境：Windows，Node 24.11.1/npm 11.6.2，现有锁文件依赖；普通回归设置 `AI_MODE=disabled`，清除 `SIYIGE_INTEGRATION`、`SIYIGE_MIGRATION_TEST`，浏览器检查也清除 `SIYIGE_BROWSER_LIVE`。

| 检查 | 实际结果 |
| --- | --- |
| `npm run typecheck` | 通过 |
| `npm run test` | 15 文件 / 255 测试通过 |
| `npm run build` | 通过，665 modules |
| `npx playwright test --config ../.cache/p3-b1-mock.config.mjs style.spec.ts views.spec.ts` | Edge 浏览器 Mock 11/11 通过，31.0 秒 |
| 合并范围、`git diff --check`、文档相对链接 | 通过；后端、infra、deploy、正式 API 和 CI 配置相对 A 基线无变更 |

浏览器使用忽略目录中的临时配置，独立 Vite `127.0.0.1:15173`、单 worker、不复用现有服务。所有 `/api/v1/**` 被测试响应拦截；没有连接 Go/MySQL。覆盖失败查询新版本、409 不自动重发、二次确认、防双击、文本转义、202 GET 轮询与首轮可用，以及辩题/历史分页、空态、异常恢复、创建结果未知及 401。Playwright 完成后自动结束本窗口的 Vite，未重启或重置共享 8080/5173/18080 等服务或数据库，未修改另一窗口正在开发的 Fake 装配。

复用 [B 原始记录](T14_STYLE_FRONTEND_ACCEPTANCE.md) 中四流派持久化、历史/刷新/重登、非法输入及真实首轮冲突的 Fake 证据；这是 B 在其基线上的既有实测，本轮没有重新执行 live 用例。A 后端和费用保护无变更，复用 [A 准备记录](T15_DEMO_PREP_ACCEPTANCE.md)、[T14 后端](T14_STYLE_BACKEND_ACCEPTANCE.md)、[T15 后端](T15_CORE_BACKEND_ACCEPTANCE.md) 及 A 基线 CI run `43623252820`，未重复后端全量或数据库回归；该 CI 不是新整合 SHA 的 CI。

## 剩余门槛与授权执行

- B1 已交付并合入，但真实持久 `failed → 查询最新版本 → 显式 retry 成功`、真实 `generating/202` 等待两项故障联调待补。Mock 通过不替代这两项，等待另一窗口的可控 Fake 装配与后续联调。
- B2 未交付：生产 UTF-8 增量 SSE 解析、断线查询恢复及显式重试交互缺失；完整 AC12 未通过。
- 完整 A4/B3 未通过：尚缺同一整合版本的完整 Fake 浏览器 AC12/AC13/AC16、体验材料与评测准备/人工结论。真实体验、10 场真实评测未执行，Phase 3 未结项。
- 本轮授权本地开发、合入指定 B SHA、Commit、普通 Push、更新 PR #19；未合 main、未真实调用、未部署，未操作数据库。
- 实查 PR #19 已于 **2026-11-15 01:58:56（北京时间；UTC 2026-11-14 17:58:56）**合并，merge SHA 为 `9fa19eb6c0fbd3a8d1ab3ce002a700c6235e9afc`。尝试恢复草稿被 GitHub 拒绝，已合并 PR 无法恢复草稿；本轮仅更新其说明，不新建 PR、不再次合 main。后续新增提交无法进入该 PR 的新审查/CI，须后续明确新 PR 安排。

后续固定基线交接已合入策略 Fake 指定 `4c5ffbbb8bfb797243b6bb84d7fa28312e78e917`；固定 test 目标的失败/延迟 HTTP 复核和 B 启动入口见 [装配记录末节](P3_A_STRATEGY_FAKE_ACCEPTANCE.md#整合后固定-test-目标复核)。上文“等待另一窗口装配”是本记录原轮次状态，B1 浏览器补测及 B2/B3、完整 A4 仍未通过。
