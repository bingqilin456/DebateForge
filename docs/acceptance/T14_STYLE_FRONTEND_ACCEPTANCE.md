# T14-STYLE 前端与 Fake 联调记录

日期：2026-11-15（Asia/Shanghai）。本记录仅覆盖 Phase 3 B 第一批 T14-STYLE，不代表 AC16 全部完成或 Phase 3 结项，待负责人确认。

## 基线、环境与边界

- 原仓库：`D:\OneDrive\Desktop\思弈阁\DebateForge`。
- 分支：`feat/t14-style-frontend`；HEAD：`66dd979132b6838ee732d499f7e1c75cf9f02fe9`（A 的 T14 后端交付）。未改用 main、Phase 2 或 T15 基线。
- 按当前 [AGENTS](../../AGENTS.md) 导航读取 [T14 规格](../planning/T14_STYLE_SPEC.md)、[Phase 3 交接](../planning/PHASE3_A_INTEGRATION_SPEC.md)、[API](../api/API.md)、[OpenAPI](../api/openapi.yaml)、[后端验收记录](T14_STYLE_BACKEND_ACCEPTANCE.md) 及 AC16，复用现有认证、辩论与总结代码。无 `.codegraph/`，按实际文件定位。
- Git 更新/分支创建由负责人在自己的 PowerShell 完成；Agent 复核分支与 HEAD，仅保留本轮工作区改动，未操作只读 `.git`。
- 负责人在固定 `127.0.0.1:13307/siyige_test` 用现有工具完成前向迁移：00012、00013 从 pending 变为 applied，`applied=2`，已有数据保留。本轮未重复迁移、未直写数据库、未改 SQL。
- 负责人使用现有密码文件重建并启动 `.cache/t14-style-backend.exe`，配置 `APP_ENV=test`、`AI_MODE=fake`、固定测试库、`AUTH_ORIGIN=http://127.0.0.1:5173`。Agent 核对监听 8080 的进程路径及 live/ready 200；未读取受限的密码目录或绕过权限。
- 负责人完成锁文件依赖同步后，复用 Vite `127.0.0.1:5173`。未添加依赖或改 package/lock；Playwright 复用本机 Edge，无浏览器下载。
- 未改顶层入口/路由、共享 client/认证、后端、正式契约、架构或 TASKS；未实施 SSE、真实模型调用/质量评测、提交、推送、PR、合并或部署。

## 修改与接线

| 文件（仓库相对路径） | 本批职责 |
| --- | --- |
| `frontend/src/types/api.ts` | 复用 AIStyle，最小扩展创建参数与严格策略状态/请求类型 |
| `frontend/src/api/debate.ts` | 可选 ai_style；固定策略 GET/POST；严格响应校验及现有 70 秒生成预算 |
| `frontend/src/debate/styles.ts` | 四种流派的中文标签与 logical 默认值 |
| `frontend/src/debate/firstTurn.ts` | 原创建状态机透传选定流派，保留旧调用方式 |
| `frontend/src/components/debate/FirstTurnPanel.vue` | 主视图创建选择、后端保存值展示、嵌入策略卡 |
| `frontend/src/views/TopicsView.vue` | 辩题列表创建入口提交流派，不改变 Props/Emits |
| `frontend/src/views/HistoryView.vue` | 展示历史保存流派，不改变分页或详情交接 |
| `frontend/src/components/debate/StrategyPanel.vue` | 独立可跳过策略卡；只读恢复、两步重试、防重与迟到隔离 |
| `frontend/src/api/strategy.test.ts`、`frontend/src/StyleCreation.test.ts`、`frontend/src/components/debate/StrategyPanel.test.ts` | 新增 69 项 API/组件交互检查 |
| `frontend/src/App.test.ts`、`frontend/src/views/TopicsView.test.ts`、`frontend/e2e/views.spec.ts` | 只更新受影响的默认流派请求断言/策略只读 fixture |
| `frontend/e2e/style.spec.ts`、`frontend/e2e/style-live.spec.ts` | 分离浏览器 Mock 故障恢复与真实 Fake 联调 |
| `frontend/playwright.config.ts` | 可选复用已有浏览器；未配置时维持原 CI Chromium |
| 本文件 | 实际浏览器验收结果、限制与 A 交接 |

给 A：两个创建入口均已接入，无需修改 App。`StrategyPanel` 仅依赖现有 `auth`、`detail` 和可选 `disabled`；从保存的 `detail.debate.ai_style` 展示，不保存第二份会话流派。已有 `created`、`select-debate`、轮次/重试/总结行为保持。

策略行为：

- 进入/刷新/换会话只 GET `/api/v1/debates/{id}/strategy`，不自动生成。仅显式点击 POST `{}`。
- 201/200/202 由现有 client 处理；generating 仅做 2 秒间隔、最多 120 秒的 GET，预算结束转手动查询。GET 沿用 5 秒超时，POST 70 秒。
- failed 先点击“查询最新重试版本”，重新 GET 并核对当前无任何 turn；再点“确认重试策略（vN）”才 POST `{retry:true,expected_attempt_version:N}`，不自行加版本。
- 409 或结果不明只 GET 恢复，绝不自动更换版本重发。再次重试必须重新查询、重新确认。已有任意状态的 turn 均禁首次生成/失败重试；发送前重读详情，最终竞争由后端锁裁定。
- 策略等待不锁住发言入口；可直接跳过。按认证上下文与会话标识作废迟到结果，离开后停止轮询；内容作为文本显示，不渲染不可信 HTML。

## 已执行验证

先有失败测试，再实现；新增边界回归也先确认失败后修正。最终源码检查后仅新增本记录，没有继续修改源码。

| 范围/命令（在 frontend 中执行） | 实际结果 |
| --- | --- |
| `npm run typecheck` | 通过 |
| `npm run test` | 15 文件、255 测试全部通过（含新增 69 项） |
| `npm run build` | 通过，665 modules，无新增依赖 |
| `npx playwright test e2e/style.spec.ts e2e/views.spec.ts` | 11 项浏览器 Mock 通过；最后两处恢复/校验小修后另重跑 style.spec.ts 的 2 项，仍通过 |
| `SIYIGE_BROWSER_LIVE=1` + `npx playwright test e2e/style-live.spec.ts` | 真实 Vite → Go → MySQL → Fake，1 个组合场景通过，约 1 分钟；不是 Mock 响应 |
| 最后小修后的真实只读复验 | 复用同一合成账户，重新登录、四流派历史、打开 data 已存策略、刷新、退出通过；策略 POST=0，无再次模型生成 |
| 截图查看 | 已查看策略成功/真实冲突页面；另有四流派历史截图。保存在忽略目录 `.cache/t14-style-browser/`，不含密码、Cookie 或 token |

普通前端检查清除 `SIYIGE_INTEGRATION`、`SIYIGE_MIGRATION_TEST`，设置 `AI_MODE=disabled`；不会改变已运行的 Fake 后端进程。未重做未受影响的 PB/T01、真实完整四轮总结/旧分页端到端验收或后端全量检查。

### 真实 Fake 浏览器逐项结果

- 主视图默认 logical、辩题入口另外三流派，创建返回对应保存值、standard_4/4 轮；主视图还验证 data 选择。两入口的四选项完整组合由组件测试覆盖。
- 双击创建各仅一次 POST；刷新后仍 not_started，不自动生成。双击显式生成各仅一次 POST `{}`，4 次返回真实 201/completed、schema 1、strategy-v1 和对应 ai_style。
- 四种流派的详情、刷新、历史列表、历史打开、退出重登后再次打开均一致，已存策略不重复生成。
- 真实 API 的 null、空串、错误大小写、未知字符串、数字、布尔、数组共七种非法 ai_style 均 400 invalid_request，前后本人会话总数不变；省略字段真实创建为 logical。
- 无策略的会话直接发送首轮成功；出现 turn 后不再提供首次生成/失败重试。
- 真实并发冲突：只暂缓浏览器原 POST 的发送，让另一个真实首轮请求先提交，再放行原 POST。后端真实返回 409 round_conflict；前端 GET 恢复、无自动重发、隐藏生成入口；策略仍 not_started。这里未使用 route.fulfill 伪造响应。
- 按真实 6 次/分钟共享 Gate 主动等窗口，未绕过限流。只创建合成测试账户/会话/发言，全部保留，不做清库或 teardown 删除。

### Mock 专项与未执行项

已通过的 Mock/API/组件分支：201/200/202、202 GET 轮询、failed 后查询最新版本并二次确认、502/503 恢复、attempt_version_conflict 只查不重发、较慢详情恢复仍继续轮询、迟到跨会话/重新登录隔离、损坏响应/Unicode/长度/额外键和文本转义。

**真实后端的持久 failed → 查询新版本 → 显式 retry 成功，以及真实 generating/202 等待仍未执行。** 原因是此基线的默认 Fake 同步成功，失败/延迟只在 A 已交付的脚本测试装配中注入，没有可用于当前 test 运行进程的开关。未改后端、未切换 deploy、未篡改策略表制造失败，也未用浏览器 Mock 冒充真实失败联调。

给 A 的待联调项仅此：提供保持固定隔离库/测试环境的可控 Fake 失败/延迟装配后，补真实 failed/版本冲突/202 场景。现有前端与测试可直接接，不需重新准备文档。沿用 A 已有 Prompt/数据库专项证据；本轮不声称重新验证后端 Prompt 或真实流派质量。AC16/Phase 3 不写为整体验收通过。

## 打开与复验入口

现有两个服务保持运行时，浏览器打开 `http://127.0.0.1:5173/`，在“辩论主视图”或“辩题列表”选择流派即可。本页使用真实本地认证/持久化 + Fake AI，不是先前 T06 的内存模拟认证预览；请只使用合成测试账号和发言。

下列为本轮已用的复验方式，不要求负责人现在再执行：

```powershell
# frontend 目录，复用已有开发服务；默认 CI 不需要设置浏览器路径
$env:SIYIGE_BROWSER_EXECUTABLE = 'C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe'
npx.cmd playwright test e2e/style.spec.ts
# 仅在确认上述固定隔离库 + Fake 后端时启用；会保留新增合成测试数据
$env:SIYIGE_BROWSER_LIVE = '1'
npx.cmd playwright test e2e/style-live.spec.ts
```

真实用例关闭 trace，避免把 Session/CSRF 留在网络录制中；不需重装依赖、下载浏览器、重跑迁移或重建后端。

## 负责人请求代查的补充结果（2026-11-15）

负责人提出“你可以帮我检查一下吗”后，Agent 复用同一测试账户和正在运行的 Vite/Go 服务，通过真实 Edge 浏览器复查，并查看桌面、移动端和历史截图；不是负责人亲自操作或最终验收签字。

- 测试账号登录、退出及再次登录成功；两个创建入口都有四种流派，默认 logical，各选项切换正常。仅选择，不提交创建。
- 四种已有流派的历史记录、详情和策略三段内容正确；分别刷新后仍一致，策略 attempt_version 均为 1；没有创建后流派/赛制修改入口。
- 已生成策略不显示重复生成按钮；重新登录后从历史打开 data 策略仍可读取。发言输入和按钮可用性正常，本次不发送新发言。
- 390px 移动端无横向溢出；浏览器未发生未捕获脚本错误。业务写请求和策略 POST 均为 0，前后会话总数仍为 6；未调用模型、未新增会话或发言。
- 本次不重复首次生成/双击防重/跳过策略发言，复用上文已通过的真实 Fake 联调证据；真实 failed/202 的未执行项保持不变。
- 截图保留在忽略目录 `.cache/t14-style-browser/requested-check-2026-11-14T22-01-06-319Z/`。仅补记本记录，没有修改业务代码，未重跑不受影响的完整检查，未暂存、提交或推送。
