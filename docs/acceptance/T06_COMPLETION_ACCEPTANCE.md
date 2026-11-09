# T06 正式入口、首轮联调与技术验收

2026-11-09，**项目负责人确认 T06 人工验收通过，T06 已完成。** 此前的技术验证、全新 Codex 独立审查及定向复审均通过，T06-R01（P3）已由独立复审确认关闭。

## 授权、基线与范围

本轮负责人授权完成 T06 剩余开发与阶段技术验证，与 PR #5 审查隔离。基线 `0211dc68b5d3573b847cf098d77fb3b52a5d1754`，分支 `feat/t06-completion`，Worktree 为原仓库 `.cache/worktrees/t06-completion`。创建前目标分支不存在、路径不存在、主工作区干净。第一次创建被 `.git` 沙箱只读保护拒绝，按本轮明确授权通过受审权限重执行成功；无其他 Git 历史写入。

实现范围：`frontend/index.html`、`frontend/src/App.vue` 与 App DOM 测试；新增 `src/api/debate.ts(+test)`、`src/debate/firstTurn.ts(+test)`、`src/components/debate/FirstTurnPanel.vue`；必要状态/计划/交接文档。复用 B 的 client/auth/forms，不改后端、迁移、正式 API/OpenAPI、已有健康测试、依赖清单或锁文件。

正式入口恢复登录后展示真实辩题、立场选择、创建、首轮发言与结果。会话 hash 支持刷新和本人详情重读。所有内容通过 Vue 文本插值展示；UTC 时间按浏览器本地时区展示。无 localStorage/sessionStorage 凭据、前端 Key 或读取 Session Cookie 的代码。

同步提交锁防双击；生成使用 70 秒请求超时；结果不明先 GET，generating 每 2 秒查询、最多 120 秒，停止后保留显式查询。仅当 GET 明确无轮次才允许用户明确重发同一原文/UUID；不自动生成、不换键。失败轮保留服务端原文，当前 T06 不提供 T09 的失败重试。创建会话无幂等保证，故传输失败提示可能已创建；再次按钮明确为“另建新会话”。

## 运行环境与资源归属

- 既有 `siyige-t02-mysql-test-1`：回环 13307；只读 SQL 实测数据库 `siyige_test`、账户 `siyige_test_app@%`、MySQL `8.4.13`、5 条启用辩题、迁移版本 10。未重启、建库、迁移、播种或修改容器/卷。开发库与其他项目未操作。
- 用户 GoLand 旧后端占用 8080，保留不动。本轮从独立 Worktree 编译既有 main 到 `.cache/t06/server.exe`，服务端使用 `APP_ENV=test`、`DB_ADDR=127.0.0.1:13307`、`DB_NAME=siyige_test`、`DB_USER=siyige_test_app`、`DB_PASSWORD_FILE` 指向原仓库 `.cache/t02-mysql/secrets/test-app.txt`（仅应用读取，不输出内容）。
- 本轮后端回环 18080，前端回环 15173；`AUTH_ORIGIN=http://127.0.0.1:15173`。`.cache/t06/vite.mjs` 通过 Vite `createServer` 临时覆盖端口和 `/api/`→18080，保留同源 Cookie/Origin/CSRF 机制，不改正式 `vite.config.ts`。正式默认仍为 5173/8080。
- 初始自有后端 PID 38164、Vite PID 53252，均由本轮隐藏启动，PID/日志保存在 `.cache/t06/`；后续启停/最终状态在下方记录。只核对并操作自己拥有的 PID/绝对程序路径。
- `AI_MODE=fake` 为真实 HTTP 联调的确定性回声实现，零供应商请求；`disabled` 可用于验证真实入口 `503 model_unavailable`。502/504/长生成通过已有后端集成或前端受控测试验证，不称作浏览器真实模型结果。
- agent-browser 与可用 Playwright CLI 未在当前环境找到；离线 npx 也未命中缓存。使用 CUA 实际 Edge 验证注册，再用独立 IAB 浏览器登录/首轮，避免与用户 Edge 的本地 Cookie 混用。DOM 测试与实际浏览器分别登记。
- 无新增依赖。`npm ci --offline --cache <原仓库>/.npm --ignore-scripts` 安装锁定依赖到本 Worktree；Go 依赖从现有 `C:/Users/ZhuanZ1/go/pkg/mod` 读取。原仓库早期 Go cache 缺少 Eino/UUID，首次 GOPROXY=off 构建失败，切到完整现有模块缓存后成功，不改依赖。

## 实际自动化验证

| 验证 | 实际结果 |
| --- | --- |
| API/首轮状态测试先行 | 首次两个 suite 因目标模块不存在失败；实现后初始 8 项通过 |
| App DOM 测试先行 | 原 PB 入口的 3 项流程断言失败；接线后转绿 |
| 浏览器发现的登录 pending 竞态 | App 新增用例先失败；修复只读初始化后通过 |
| 恢复认证丢会话 hash | 新增详情请求断言失败；恢复前读取当前 hash 后通过 |
| 最近定向回归 | App 5 + 状态 8，共 13 项通过 |
| 首次完整前端测试（沙箱） | 134 项中 123 通过、11 项真实回环 HTTP 因网络限制失败；未改业务或测试绕过 |
| 同一完整测试在允许本地回环环境重跑 | 10 文件、134/134 通过；后续新增回归用例仍需最终统一验证 |
| `npm run typecheck`、`npm run build` | 已通过；最终快照另登记 |
| `go build -o ../.cache/t06/server.exe ./cmd/server` | 成功；使用未修改的真实 main |
| `go test ./...`、`go vet ./...` | 通过；此命令默认跳过 MySQL 集成，不冒充真实数据库验证 |
| 显式真实 MySQL 集成 | `SIYIGE_INTEGRATION=1` + 固定 test 配置，`go test -count=1 -v ./tests/integration` 全部通过（约 13.98 秒）；覆盖认证/Origin/CSRF/过期/存储失败，topics/ownership、首轮并发/幂等/额度/取消/版本写回/提交不确定和 >5 秒 Fake。用例按自身确切夹具清理，不清表 |

Go 1.27.1 下 sonic 提示不支持当前版本并回退 encoding/json，这是已有依赖的兼容提示；本轮未修改依赖，没有把回退记作性能验证。官方用法核对：[Vue 响应式](https://vuejs.org/api/reactivity-core.html)、[生命周期](https://vuejs.org/api/composition-api-lifecycle.html)、[History replaceState](https://developer.mozilla.org/en-US/docs/Web/API/History/replaceState)。UI 查阅 ui-ux-pro-max 的 Vue 生命周期、组件测试与可访问性建议，未为简单表单新增验证库。

## 实际浏览器联调（持续补充）

真实链路：浏览器→本轮 Vite→本轮 T03/T05 main→既有隔离 MySQL；无 fetch mock 或网络拦截；生成仅 Fake。

| 已执行用例 | 结果 |
| --- | --- |
| Edge 正式入口切注册、真实注册 | HTTP 201；显示“注册成功，请登录”，密码清空，仍未登录 |
| IAB 错误密码 | 真实 401 invalid_credentials，中文受控提示、密码清空 |
| IAB 双击登录 | 一次实际成功登录；首次发现辩题初始化竞态，已补测试修复 |
| 刷新登录恢复与五条真实辩题 | me 200，topics 200，5 条来自真实表；页面可选择立场 |
| 选择第 3 题与反方、双击创建 | 一次 POST 201；会话 163，用户反方/AI 正方，0 轮，无模型调用 |
| 填写含 HTML 标签的首轮并双击发送 | 一次 POST 201；1 轮、2 条消息；标签按纯文本展示，没有 article 内 b/img 节点 |
| 刷新 `#debate=163` | me/detail 200，仍为 1 轮、2 条消息，不触发生成 |
| 手机视口检查 | IAB 实际 DOM clientWidth 与 scrollWidth 均 375，无横向溢出；请求 390/1280 的 viewport override 未改变实测宽度，不能把它算作已验证的目标尺寸或桌面首轮截图 |
| 开始新的练习 | 返回选题，旧消息清理；后续补正方与异常状态 |

浏览器工具原始 DOM/截图见本聊天工具记录；后端脱敏请求日志、临时启动覆盖在本 Worktree `.cache/t06/`，忽略不提交。最终会登记独立本机证据和快照摘要。

## 独立审查与风险边界

全新 Codex 独立只读审查聊天：`01a1bc48-5b45-74e3-8596-e5e838b0143f`（本地）。初审读取全部 15 个变更文件及相关 B 链路/契约，开始与结束核对 `.cache/t06/review-freeze.json`：基线/分支一致、清单差异 0、SHA-256 不符 0；无新增 P0/P1/P2。审查没有运行测试、数据库或浏览器，不把实现者证据称为审查者实测。DSH 仅只读环境调查，未承担审查。

| 编号 | 发现与修复 | 验证/状态 |
| --- | --- | --- |
| T06-R01 / P3 | JS `trim()` 漏掉 U+0085，后端按 `unicode.IsSpace` + BOM 判全空白并 400；前端先绑定原文/UUID 后锁定无法修改。改为共用 `hasNonWhitespace`，匹配 Unicode White_Space + BOM；在绑定原文前拒绝，面板同步禁用；不改后端和一般传输结果不明时的原文/UUID 保护 | 先补 4 类空白状态用例与实际 DOM 断言，修前 3 项失败；修后 25/25 定向通过。同一独立 Codex 聊天定向复审确认关闭 |
| 初审测试覆盖观察 | 原“202”状态用例其实从 open 恢复开始，且 API 直接响应守卫用例不足 | 修正名称，新增 submit 返回 generating→2 秒查询→120 秒停止/不重发的链路；真实 client 接受 HTTP 202 DTO，以及 UUID、原文、消息结构损坏和其它会话 ID 的拒绝用例；均通过 |

修后验证仅运行受影响的 3 个文件（App、debate API、firstTurn 状态），25/25，通过 `typecheck` 与生产构建。初审前完整前端为 138/138（`.cache/t06/frontend-tests.json`），修后定向为 `.cache/t06/review-regression-tests.json`，不虚称最终完整套件已重跑或 148 项已运行。后端未变化，不机械重复后端/数据库完整验证。

空白实现核对 [Go IsSpace](https://pkg.go.dev/unicode#IsSpace) 与 [JavaScript Unicode property escape](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Regular_expressions/Unicode_character_class_escape)，并按本仓库后端额外拒绝 BOM。修后快照 `.cache/t06/review-freeze-v2.json` 已由同一独立审查聊天定向复验：15 文件/哈希相符，相对初审恰有 8 个变化路径；T06-R01 关闭，无新增 P0/P1/P2 或剩余 P3。复审确认普通传输不确定时的原文/UUID 保护未削弱，并确认补测覆盖所述分支。复审不运行测试或写文件，保存结果是实现者执行证据。

PR #5 的并行审查消息：仅针对基线 0211dc6，有条件 PASS，无新增 P0/P1/P2；OpenAPI 来源旧路径和旧“所有业务接口 404”说明为 P3。本轮不修改 PR #5、不跨分支修复该 P3。T03 已接受 P2/P3、T02-M03、T05 限制保持；T07-COST-01 继续未关闭，阻塞公网真实模型入口。真实供应商调用、费用结算、Nginx/HTTPS 部署不在本轮验证。

## 技术交付时提供的人工验收步骤

1. 在确认 AI_MODE=fake 和固定测试库后，打开本轮 15173 入口（或按默认配置启动 5173/8080，不能混用旧后端）。注册测试账号，确认注册不自动登录；错误密码有提示，成功后显示真实辩题。
2. 分别选择正/反立场，创建会话，检查 AI 相反立场；填首轮、等待时按钮禁用，双击不重复；完成后恰有两条纯文本消息与 1 轮，暂无下一轮发送入口。
3. 刷新当前 hash 地址，仍能看到已保存内容。点击“开始新的练习”清旧内容；无需存储任何凭据。
4. 体验服务失败提示、显式恢复/查询；退出后旧内容消失，重新登录后按本人会话地址恢复。跨用户地址不能展示他人内容。
5. 核对手机端文字、键盘焦点、错误提示是否清楚，并由负责人明确给出 T06 人工验收结论。本轮技术通过也不代替此结论。

本轮无 commit/push/merge/rebase/reset、部署、收费调用或阶段结项。

## 最终浏览器与本机证据补充

- 正方会话 164：实际创建、首轮成功，用户正方 / AI 反方；返回 1 轮，2 条消息。
- 另开 IAB 页面退出吊销同一 Session 后，旧面板“查询状态”收到真实 401，旧用户名/消息/地址清理。重新登录后真实辩题自动读取；初次出现的残留失效提示已补失败测试并修复，再次浏览器验证提示不再残留。
- 仅重启本轮自有 18080 为 `AI_MODE=disabled`（PID 59076），创建会话 165 并发送，真实 503 model_unavailable 后 GET 200，0 完成轮、仅用户原文；刷新仍失败，不自动生成或提供 T09 重试。
- 第二测试用户访问 165，实际 404，面板没有他人消息。只停止本轮 59076 后刷新：保留“登录状态尚未确认”与显式恢复按钮，不伪装未登录；恢复自有 Fake 服务后手动恢复/退出/重新登录均正常。
- Edge 桌面登录页实际宽度/scrollWidth 均 1473，查看截图无横向溢出；IAB 首轮实际宽度均 375，查看截图有两条纯文本消息、时间及状态。认证后桌面首轮布局未通过有效视口覆盖复验，列为人工补验项；未宣称浏览器 console/error 已全面采集。
- 数据库只读计数核对：测试用户的会话 163 为 `con/1轮/1turn/2messages/completed`，164 为 `pro/1/1/2/completed`，165 为 `pro/0/1/1/failed`。两次双击没有多建会话或多写轮次。测试用户 `t06_ui_20261109`、`t06_other_20261109` 及 3 场会话保留供复查，不清空表；口令、Cookie、CSRF 不入证据。
- 本机证据：`.cache/t06/frontend-tests.json`、`review-regression-tests.json`、`verification-summary.json`、`review-freeze*.json`，`backend.log` / `backend-disabled.log` / `backend-fake.log`、`vite.log` 及对应 error 日志；浏览器原始 DOM/截图在本聊天 CUA 工具记录中，未捏造本机截图文件。所有缓存、日志、编译产物被现有规则忽略。
- 最终运行状态：本轮 Fake 后端 PID 33768 / 回环 18080，Vite PID 53252 / 回环 15173，直接与代理 `/api/health/ready` 均实测 `ready`。IAB 仅保留已退出登录的验收入口；本轮额外页面已关闭、临时 viewport override 已 reset。用户原 8080 进程和 MySQL 均保持，不再启用 disabled 模式。

现在可直接打开 `http://127.0.0.1:15173/` 人工验收。若本轮服务后来停止，在本 Worktree 根目录运行本机忽略脚本 `& ./.cache/t06/restart-backend.ps1 -Mode fake`；前端在 frontend 目录执行 `node ../.cache/t06/vite.mjs`（先确认 15173 未被其它服务占用；已有服务就复用，不重复启动）。这些脚本是本轮本机验证辅助，不是仓库交付或生产部署配置；新机器按前述明确环境变量和 Vite 临时 createServer 覆盖重建，不复制秘密或复用用户旧后端。

既有验证边界。70 秒浏览器请求超时和 120 秒轮询未另做整段浏览器等待，相关证据来自既有真实 HTTP client 超时测试、后端集成与前端 Fake timer。真实模型、费用、公网、HTTPS 和 Nginx 仍未验证。

## 技术交付时的 Git 状态

主工作区 `feat/integration-t05-t06-b` 保持干净，HEAD 仍为 `0211dc68b5d3573b847cf098d77fb3b52a5d1754`。本轮分支 `feat/t06-completion` HEAD 同一基线，15 个文件未提交（8 个前端源文件/测试/HTML，7 个必要文档），无清单外变更。初审、复审分别核对完整冻结快照；复审后的增量仅为本记录及计划的结果登记，前端实现/测试与 v2 审查快照保持一致。没有暂存、commit、push、merge、rebase、reset、PR 操作、部署或收费供应商请求。

## 负责人最终人工验收

2026-11-09，项目负责人在本次项目对话中明确确认「思弈阁 T06 人工验收已通过」，并授权登记结论、在 `feat/t06-completion` 创建一个本地 Git commit。T06 标记为已完成。本次登记采用负责人确认的整体结论，逐项人工操作和截图无新增记录。提交前复用已有测试及独立复审证据，核对冻结快照、提交范围、文档链接、敏感文件和 Git 差异格式。

T07-COST-01 继续阻塞公网真实 AI 入口。既有验证边界及 T02/T03/T05 延期记录仍适用。
