# V2 页面设计模块化重构验收记录

日期：2026-11-19。**状态：Writing 与定向自测完成；负责人签收待确认。** 本记录只登记本轮实际执行过的检查，不构成 V2 开发授权，也不改变任何切片或 Phase 的既有结论。

## 1. 本轮范围与授权

- 负责人要求：按本轮确认的页面构图重新组织页面设计文档，允许模块化写作以便并发执行。
- 允许写入：`docs/product/PRD_V2_UI_DESIGN.md`（改写为总览与索引）、新建 `docs/product/ui/`（模块目录、演示图与源文件）、新建本记录。
- 联动修改（仅链接与效力说明，不改产品结论）：`docs/product/PRD.md` 两处指向旧锚点的链接、`docs/README.md` 三处入口、`docs/planning/TASKS.md` 一处交付入口、`docs/planning/V2_04_ANNOTATION_ANCHOR_DESIGN.md` 一处页面链接。
- 未执行：代码、迁移、部署、真实模型调用、数据库操作、Commit、Push、PR。

## 2. 变更文件清单

| 文件 | 类型 | 说明 |
| --- | --- | --- |
| `docs/product/PRD_V2_UI_DESIGN.md` | 改写 | 707 行单文件改为总览：定位与标记、六张演示图、P1–P11 清单（含实现状态列）、模块导航、现状基线、维护方式 |
| `docs/product/ui/README.md` | 新建 | 模块目录、标记约定、已交付切片基线、写作模板 |
| `docs/product/ui/01-global.md` | 新建 | 视图与路由、跨页事实源、桌面与移动组织 |
| `docs/product/ui/02-entry.md` | 新建 | 开始练习 P2–P5 |
| `docs/product/ui/03-workspace.md` | 新建 | 辩论工作台 P6、P6a–P6e |
| `docs/product/ui/04-review.md` | 新建 | 完赛复盘 P7、P7a–P7c 与 P8 |
| `docs/product/ui/05-history.md` | 新建 | 历史 P9 |
| `docs/product/ui/06-settings.md` | 新建 | 设置 P10、P11 |
| `docs/product/ui/07-common-states.md` | 新建 | 登录、共通失败态、退出 |
| `docs/product/ui/08-traceability.md` | 新建 | 追踪矩阵、未决项、未执行项、维护 |
| `docs/product/ui/mockup/index.html` | 新建 | 六屏静态线框源文件 |
| `docs/product/ui/mockup/shoot.js` | 新建 | Edge 无头渲染脚本 |
| `docs/product/ui/mockup/README.md` | 新建 | 演示图效力与重新渲染方法 |
| `docs/product/ui/mockup/shots/*.png` | 新建 | 六张演示图 |
| `docs/product/PRD.md` | 修改 | 两处旧锚点改为模块路径；入口说明补“已实现”标记 |
| `docs/README.md` | 修改 | 三处入口指向总览＋模块目录 |
| `docs/planning/TASKS.md` | 修改 | 交付入口补模块目录与重构说明 |
| `docs/planning/V2_04_ANNOTATION_ANCHOR_DESIGN.md` | 修改 | 页面 P6c 链接改指工作台模块 |
| `docs/acceptance/V2_UI_DESIGN_MODULARIZATION_ACCEPTANCE.md` | 新建 | 本记录 |

## 3. 结构与内容检查（脚本）

脚本位于 `.cache/ui-rewrite/`（Git 忽略目录，不作为交付文件；命令记录见 §4）。

| # | 检查 | 方法 | 结果 |
| --- | --- | --- | --- |
| 1 | 模块文件齐全 | 逐个 `Test-Path` | 8 个模块＋README 全部存在 |
| 2 | 五类标记使用 | 正则匹配 `[已确认]`/`[已实现]`/`[补充草案]`/`[设计建议]`/`[待决]` | 10 份文件均有标记，共 506 处 |
| 3 | 未出现未经支撑的签收式结论 | 正则否定匹配 | 0 处 |
| 4 | 总览链接全部模块 | 字符串包含 | 8/8 |
| 5 | 总览引用六张演示图 | 字符串包含 | 6/6 |
| 6 | 演示图文件存在 | `Test-Path` | 6/6 |
| 7 | 演示图效力声明 | mockup README 含“线框示意” | 存在 |
| 8 | 关键语义保真 | 关键短语断言（见 §5） | 全部保留 |
| 9 | 敏感字面量 | 5 类正则（Key、私钥、口令、DSN、手机号/学号） | 0 处 |
| 10 | 无 BOM、LF 行尾、无冲突标记 | 字节与正则检查 | 10/10 通过 |
| 11 | 表格后留空行 | 逐行检查 | 0 处违规 |

合计 **109 项通过、0 项失败**。

## 4. 已执行的验证命令

| 检查 | 命令 | 实际结果 |
| --- | --- | --- |
| 本地文件链接与锚点 | `pwsh -NoProfile -File .cache/ui-rewrite/check-links.ps1 -FileList <14 份文档>` | 文件链接 **551** 处、缺失 **0**；锚点 **249** 处、失效 **0** |
| 结构与内容不变量 | `pwsh -NoProfile -File .cache/ui-rewrite/check-content.ps1 -Root <worktree 根>` | **109 通过 / 0 失败** |
| 空白与行尾 | `git diff --check` | 退出 0，无空白错误（仅 CRLF 转换提示） |
| 变更范围 | `git status --porcelain=v1`、`git diff --stat` | 5 份文件被修改、1 个新目录；无代码、迁移、依赖、部署改动 |
| 演示图渲染 | `node docs/product/ui/mockup/shoot.js <worktree 根>`（`NODE_PATH` 指向 `frontend/node_modules`，本机 Edge 无头模式） | 6 张全部生成，逐屏目视核对 |

## 5. 内容保真核对

重构前单文件 707 行，逐节迁入模块后核对下列原语未被改写或丢失：

| 原章节 | 迁入 | 核对结果 |
| --- | --- | --- |
| §0 定位与标记、§1 信息架构、§2 页面清单 | 总览 + 01-global | 保留，并补“实现状态”列 |
| §3 登录与恢复 | 07-common-states | 保留（401 保留 `#debate=`、地址无效进入口页不自动建会话） |
| §4 训练入口 | 02-entry | 保留（四步、12/200、0/1000、本场不可切换、确认创建不调模型、返回入口另开一场） |
| §5 工作台 | 03-workspace | 保留（两类合法连续同方 `2n+4→2n+5`、步骤 4→5；0 回合不可跳过；10 回合上限；停止等待不计费；失败说明按已保存记录） |
| §6 完赛复盘 | 04-review | 保留（观察不足不落 1 档、不画成 0、≠复盘缺失；PDF 零新增调用；移动端保留雷达图） |
| §7 历史 | 05-history | 保留（复盘失败不得显示为“未完成”；旧 V1 按原赛制恢复） |
| §8 设置与额度 | 06-settings | 保留（仅 SUP-01 为用户确认；保存与测试分离；不虚构供应商余额） |
| §9 共通状态、§10 退出 | 07-common-states | 保留（429 三类须区分；不编造重置时点；不暗示自动保存草稿） |
| §11 追踪、§12 未决 | 08-traceability | 保留并补实现状态列 |

本轮新增（来自已签收切片与外部分支确认）的内容：

- `[已实现]` 标记与“实现状态”列，依据 V2-03（2026-11-18）与 V2-04（2026-11-19）两次签收。
- 批注按轮分组的批次口径（一轮 0–3 条、按轮补做最多一次）、多行换行标记、练习者视角（教练视角）。
- 场中开关的已实现语义：同时隐藏正文标记与批注面板。

## 6. 发现并修正的问题

| 问题 | 处理 |
| --- | --- |
| 初版工作台演示图把 AI 画在步骤 1（用户持正方时步骤 1 应为用户立论），与 PRD §3.2 相反 | 重排步骤并重渲染；工作台模块对应说明改为“已按 PRD 次序修正” |
| 初版演示图在四轮练习里显示“自由辩 1/10 回合”与“进入结辩”按钮，但 1–10 回合与结辩按钮只属训练赛 | 阶段轨改为“自由辩”，说明改为“四轮练习固定一次往返”，去掉结辩按钮提示 |
| 初版窄屏图出现“争点 4”分段，与“四轮练习争点卡赛后集中整理”冲突 | 窄屏分段改为“发言 / 批注”，并同步 01-global 与总览的描述 |
| 批注条目徽标合计超出单轮 0–3 条上限 | 调整为每轮 3 条，并在面板标注“同一轮合计 0–3 条，示例取上限” |
| `01-global.md` 与 `PRD_V2_UI_DESIGN.md` 各有一处锚点指向不存在的标题 | 分别改为 `03-workspace.md#3-整体布局`、`06-settings.md#7-待决与不做` |
| `mockup/README.md` 的相对路径少一级 | 改为 `../../PRD_V2_UI_DESIGN.md` |

## 7. 未执行项

1. 页面代码、组件、样式与视觉稿；本轮只有静态 HTML 线框，没有改任何产品前端代码。
2. 前端 `typecheck`/`test`/`build`、后端 `go test`/`vet`/`build`、浏览器 E2E、CI。
3. 真实渲染器中的锚点点击验证：锚点为脚本级 slug 比对，未在 GitHub/编辑器渲染后点验。
4. 演示图未与真实组件比对：线框自绘，不代表现有 `AnnotationPanel.vue` 等组件的实际像素。
5. 数据库、真实模型调用、部署与费用实测。
6. 负责人签收尚未发生；本文不得记为已确认。

## 8. 遗留与风险

| 问题 | 影响 | 说明 |
| --- | --- | --- |
| 移动端批注呈现措辞未统一 | 已关闭 | 负责人 2026-11-19 决定采用**单列堆叠**、不做覆盖正文的抽屉；PRD §5.3 与 AC-V2-14 措辞已同步为“底部区块／单列堆叠”，模块的 `[待决]` 已收为 `[已确认]` |
| 旧 V1 会话的批注面板口径不一致 | 中 | 文档倾向“默认不提供 V2 专属入口”，实现是渲染面板并显示空态；已在 05-history 标出不一致 |
| 429 三类在接口上不可区分 | 已修复（同批提交） | 服务端原对分钟防刷与日额度都返回 `rate_limited`。已拆为 `rate_limited`（分钟防刷）、`generation_busy`（并发占用）、`daily_quota_exhausted`（每日尝试用尽），并同步前端文案、单测、`API.md` 与 `openapi.yaml`。验证见 §10 |
| 页面文档仍含大量 `[设计建议]` | 低 | 演示图与线框全部为设计建议，不构成已确认行为 |
| 每轮批注演示数据为虚构样例 | 低 | 已在线框与 mockup 说明中标注 |

## 9. 结论边界

- 本记录只登记“文档已按页面重构为模块并完成定向自测”这一事实，**不是负责人签收**，不代替技术复核，也不构成 V2 开发授权。
- 页面内容只做重组与对齐已签收切片，没有新增产品需求；`[设计建议]` 与 `[待决]` 项仍需负责人逐项答复。
- 后续如需确认演示图构图、模块划分或 `[设计建议]` 项，须由负责人答复并另登记确认来源与日期。

## 10. 同批修复：429 三类业务码（2026-11-19）

负责人要求“按建议补上”，同批修改代码，使界面能区分三种 429 恢复动作。

### 10.1 改动

| 文件 | 改动 |
| --- | --- |
| `backend/internal/debate/limits.go` | `retry` 按业务码选择 `Cause`；每日尝试用尽改用 `daily_quota_exhausted`；业务码提为常量 |
| `backend/internal/debate/model.go` | 新增 `ErrDailyQuotaExhausted` |
| `backend/internal/debate/limits_test.go` | 日额度断言改用新错误，并校验失败对象的 `Code`/`Status`/`RetryAfter` |
| `backend/tests/integration/debate_failures_test.go` | 第 21 次生成断言 `daily_quota_exhausted` |
| `backend/tests/integration/style_strategy_test.go` | 额度用尽两处断言改 `daily_quota_exhausted` |
| `frontend/src/api/client.ts` | 新增 `daily_quota_exhausted` 受控文案 |
| `frontend/src/api/client.test.ts` | 新增 3 条用例，按业务码断言提示可区分 |
| `docs/api/API.md` | 错误表与 4 个生成端点列表补第三类码 |
| `docs/api/openapi.yaml` | `GenerationLimited` 枚举与 `ErrorEnvelope.code` 枚举补第三类码 |

### 10.2 行为对照

| 原因 | 业务码 | 触发 | 恢复动作 |
| --- | --- | --- | --- |
| 分钟防刷（每分钟 6 次） | `rate_limited` | `Gate.Request` | 按 `Retry-After` 秒数重试 |
| 并发占用（单用户 1、全局 2） | `generation_busy` | `Gate.Acquire` | 等当前生成结束 |
| 每日尝试用尽（20 次） | `daily_quota_exhausted` | `Gate.Acquire` | 当日不再重试 |

三类均保持 `429` + `Retry-After`，仅业务码不同；既有只判断 `429` 的客户端不受影响。

### 10.3 已执行验证

| 检查 | 命令 | 结果 |
| --- | --- | --- |
| 后端全量单测 | `go test ./...`（`backend/`） | 7 个包全部 ok |
| 竞态专项 | `go test -race ./internal/debate -count=1` | ok |
| 后端静态检查 | `go vet ./...`、`go build ./...` | 退出 0 |
| 格式 | `gofmt -l`（对本轮编辑的 `limits.go`、`limits_test.go`） | 无输出；`model.go` 的既有对齐问题在 HEAD 即存在，本轮未引入、未改动 |
| 前端单测 | `vitest run`（`frontend/`） | 23 个文件、**404 项**全部通过 |
| 前端类型检查与构建 | `vue-tsc -b`、`vite build` | 均通过 |

### 10.4 本批未执行

- 隔离 MySQL 集成测试实跑：`SIYIGE_INTEGRATION` 未设置，集成测试按既有约定跳过；上述两个集成测试文件的改动**未在真实库上执行**，只做了编译与包级测试覆盖。
- 浏览器端到端验证与 CI。
- 部署与真实模型调用。

## 11. 两笔提交复核（2026-11-19）

复核基线为 `9c3113b..0a8c5c4`。保留 429 的三类业务码、HTTP 状态和服务端返回的等待秒数。前端单独处理日额度提示，避免按本地日期推测何时恢复。分钟限流和并发占用继续显示服务端给出的等待秒数。

同步共通状态的业务码、批注与异议的实现范围、两种赛制的发言顺序，以及移动端批注在主卡片下方的布局。重新生成静态线框，核对了受影响的开始练习、工作台、历史和窄屏截图。15 份 Markdown 的 296 个本地链接及其中 171 个标题锚点通过脚本检查。后端在禁用产品模型、跳过 MySQL 集成实跑的条件下，全量 `go test -race ./...` 通过。

DSH 前端验证通过，覆盖 23 个测试文件和 408 个用例，类型检查与生产构建成功。两个继承的 Go 格式问题已按 gofmt 修正，辩论包竞态测试通过。

后续工作见 [V2 并行设计](../planning/v2-parallel/README.md)。各设计稿的验收用例属于实施计划，GitHub CI、产品浏览器端到端联调和真实模型本轮均未运行。
