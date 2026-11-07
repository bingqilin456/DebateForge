# Project Bootstrap 执行与验收记录

技术验证记录日期：2026 年 11 月 1 日（Asia/Shanghai）。实现、技术验证及独立代码审查已完成。最新状态见 [人工验收与成员讲解互审登记](#人工验收与成员讲解互审登记)。完整 T02、T06、Phase 1 均未完成。

## 已批准规格与边界

用户已批准聊天中的初始化方案，并指定模块路径 `github.com/bingqilin456/DebateForge/backend`、npm、Go 1.27.1 / Node 24.11.1 / npm 11.6.2、两个回环地址及 `/health/live` 契约。实现以本记录、[API](API.md) 和 [ARCHITECTURE](ARCHITECTURE.md) 为准。本任务是 T01、T02、T06 的前置拆分任务，完整 T02、T06 和 W1 不标完成。

## 实施计划

- [x] 固定范围与 API 输入、输出、权限和失败契约。
- [x] 后端：先编写配置与 HTTP 契约测试，观察缺失实现导致失败，再实现最小配置、路由、日志、恢复及进程入口。
- [x] 前端：先编写真实本地 HTTP 下的客户端测试，观察缺失实现导致失败，再实现请求校验与健康页面。
- [x] 累计验证：后端 test / vet / build，前端 typecheck / test / build，实际启动服务并验证浏览器及代理的失败恢复。
- [x] 完成独立代码审查、运行文档、版本及依赖说明，记录结果后停止等待人工审查。

后端文件位于 `backend/cmd/server`、`backend/internal/platform` 和 Go 模块文件；前端文件位于 `frontend/src` 及 Vite / TypeScript / npm 配置。现有文档仅同步本次范围与证据，PRD 不变。

## 重点验证

1. 空值、非法端口和非回环监听配置不能悄悄变成公网监听。
2. `/health/ready` 与未知 API 不返回 HTML 200，错误和日志不泄露内部异常或请求内容。
3. JSON 的 HTTP 失败、HTML 200、非法 JSON 和错误结构不能判为成功。
4. 请求头或响应体等待超过总预算时必须失败，允许手动恢复。
5. 真实浏览器经 Vite 请求 Go；停止后端时显示失败，恢复后手动重试成功；375px 页面无横向溢出。

## 执行裁定

- Ruling：用户已经批准具体方案并要求开始实施，直接顺序执行，不重复索取计划批准；计划与结果集中在本文件，避免新增无关流程文件。
- Ruling：仓库无提交且所有原文件未跟踪；保留用户当前 `main` 工作区，不运行依赖 Git commit 的流程助手、不新建 worktree、不暂存、提交、推送或合并。以文件清单、实际检查和本记录留证；无提交基线时普通 git diff 不能代表完整改动。
- Pre-flight：前端消费后端固定的 `GET /health/live` 和 `status=alive`；代理保留路径。未发现接口冲突。
- Ruling：agent-browser CLI 未安装，使用已有浏览器工具完成真实页面验收，不为验收给项目增加 Playwright 或其他依赖。UI 技能的 Vue 关键词检索及一次重试无匹配，采用其通用可访问性与反馈原则；设计系统给出的营销段落不适合本页，未采用，也未写入额外设计目录。

## 实际环境、依赖及验证结果

### 环境与依赖

Windows amd64；实际命令输出 `go version go1.27.1 windows/amd64`、`node --version` 为 `v24.11.1`、`npm --version` 为 `11.6.2`。Gin 为 1.12.0；`npm ls --depth=0` 与 README 依赖表一致，未报告缺失或不兼容的直接依赖。直接依赖许可证已核对随包 package.json 和 Gin LICENSE。

`go mod tidy` 下载依赖并生成 go.sum；`npm install` 生成 package-lock.json，随后实际执行 `npm ci`，退出 0、安装 87 个包。为遵守沙箱写入边界，本次 Go 构建/模块缓存及 npm 缓存均放在根目录 `.cache/`；这是验收环境设置，正常开发可使用工具默认缓存。首次受限网络查询失败后，通过批准的网络执行获取官方依赖；未关闭证书或校验和验证。

### 测试过程及命令

所有命令均已实际执行，下面是结果摘要，不是预期值。

| 工作目录 | 命令/操作 | 实际结果 |
| --- | --- | --- |
| backend | 先写测试后 `go test ./...` | 退出 1，因 LoadConfig / NewRouter 尚未实现而编译失败；实现后退出 0 |
| frontend | 首次 `npm run test` | 退出 1，因 health 模块尚不存在而未收集测试；补齐函数占位后 9 项因行为未实现而失败 |
| frontend | 实现后 `npm run typecheck` | 退出 0，vue-tsc 项目检查通过 |
| frontend | 实现后 `npm run test` | 退出 0，1 个文件、9 项全部通过；使用真实临时回环 HTTP，不 Mock fetch |
| frontend | `npm run build` | 退出 0，Vite 8.3.5 构建成功，637 模块；JS 92.35 kB、CSS 398.97 kB（未 gzip） |
| frontend | `npm ci` 后再次运行 typecheck / test / build | 三项均退出 0，9 项测试全部通过，构建产物大小一致 |
| backend | `gofmt -w cmd internal` | 实际完成格式化 |
| backend | `go test ./...` | 退出 0，platform 通过；入口包无单测 |
| backend | `go vet ./...` | 退出 0，无诊断 |
| backend | `go build ./...` | 退出 0 |
| backend | `go build -o bin/bootstrap-server.exe ./cmd/server` | 退出 0，生成仅供本地验收的忽略产物 |
| backend | `go mod download` | 退出 0，README 的依赖下载命令已实际核实 |

后端测试覆盖默认地址、合法 IPv4/IPv6 回环、空值、非回环地址、非法/越界端口；HTTP 200/404/405、固定健康 JSON、禁止缓存、request_id 与日志脱敏、panic 受控 500。未连接数据库或模型，也没有执行 MySQL 集成、业务 E2E、生产部署或负载测试。

### 真实服务与浏览器验收

1. 实际启动构建的 Go 可执行文件及 `npm run dev`；只绑定 8080 / 5173 的回环地址。
2. Invoke-WebRequest 直连 `http://127.0.0.1:8080/health/live`：200、`application/json; charset=utf-8`、`{"status":"alive"}`。
3. 同样请求 `http://127.0.0.1:5173/health/live`：200、相同 JSON；X-Request-ID 由 Go 生成并经过代理保留。
4. 经 5173 请求 `/health/ready`、`/api/v1/missing`：均为 JSON 404、`not_found`，未落入 SPA HTML。未出现 Access-Control-Allow-Origin。
5. 真实浏览器首次进入页面显示“本地服务已连通”。Ctrl+C 停止 Go 后日志出现“HTTP 服务已停止”；终端包装器返回中断状态 1，不将其写为应用检查失败或正常退出码 0。
6. 后端停止后，页面点击“重新检查”显示“连接检查失败”“服务返回 HTTP 502”，按钮变为“重试连接”。
7. 按 README 实际执行 `go run ./cmd/server` 恢复后端；点击“重试连接”，页面恢复“本地服务已连通”，按钮恢复“重新检查”。没有伪造响应或自动循环重试。
8. 设置 375×812 视口检查页面，DOM 可用宽度 360px、scrollWidth 360px，无横向溢出；截图可见按钮和提示，检查后已重置临时视口。

截图保存在本机忽略目录：`.cache/bootstrap-failure.jpg`、`.cache/bootstrap-recovered.jpg`、`.cache/bootstrap-mobile.jpg`。这些是本次验收证据，不要求纳入版本管理。桌面大屏、其他浏览器与 Linux 尚未专项验证。

验收结束时已对本次 Go 和 Vite 终端发送 Ctrl+C，Go 再次输出“HTTP 服务已停止”；随后真实请求 8080 与 5173 均无法连接，确认没有继续提供本次开发服务。截图记录的是验收当时的状态。

### 文件与文档检查

新增后端模块/入口/platform/测试，前端配置/锁文件/入口/健康页面/API 客户端/类型/测试，以及 API 和本验收记录；更新 AGENTS、README、ARCHITECTURE、PLAN、TASKS、DECISIONS。PRD、协作教程、.gitignore 和原始参考资料不在修改范围。

当前相对 Markdown 链接文件目标检查无缺失。常见模型 Key、私钥和手机号模式扫描未发现匹配（不代表穷尽所有秘密类型）。.gitignore、PRD 和协作教程的 SHA-256 与实施前一致；.cache、node_modules、dist、bin 被忽略，go.mod/go.sum/package-lock.json 未误忽略。T01–T18 没有被勾选完成。

所有原始文件均未跟踪且无 commit，因此不以空 git diff 宣称没有变化；交付以完整文件清单与受保护文档哈希检查为准。未执行 Git 暂存、提交、推送、合并或生产操作。

新增 24 个源文件/配置/文档，修改 6 份现有文档；下表路径相对于仓库根目录：

| 变化 | 文件 | 原因 |
| --- | --- | --- |
| 新增 2 | backend/go.mod、backend/go.sum | 独立模块与固定依赖、完整性校验 |
| 新增 1 | backend/cmd/server/main.go | 配置组装、HTTP 服务、信号退出 |
| 新增 5 | backend/internal/platform/config.go、config_test.go、router.go、health.go、router_test.go | 回环配置校验、健康与错误契约、受控日志和测试 |
| 新增 7 | frontend/package.json、package-lock.json、index.html、vite.config.ts、tsconfig.json、tsconfig.app.json、tsconfig.node.json | 精确依赖树、入口、开发代理、类型与测试/构建命令 |
| 新增 7 | frontend/src/main.ts、env.d.ts、App.vue、api/client.ts、api/health.ts、api/health.test.ts、types/api.ts | Vue 入口、四种交互反馈（检查中/成功/失败/手动重试）、真实请求及契约类型/测试 |
| 新增 2 | docs/API.md、docs/INIT_ACCEPTANCE.md | 固定本轮契约并保留实际验收证据 |
| 修改 6 | AGENTS.md、README.md、docs/ARCHITECTURE.md、docs/PLAN.md、docs/TASKS.md、docs/DECISIONS.md | 同步当前授权、运行说明、阶段与 ADR；不改变 PRD 产品范围 |

### 未完成事项与影响

- 本次技术验证结束时，PB 人工验收、成员讲解与互审尚未登记。后续结论见下方登记。
- T01 的完整业务 DTO、模型/数据库版本和资源确认，T02 的 DB/迁移/ready，T06 的登录与首轮界面均未完成。
- 尚未执行干净克隆验证，因为本次禁止 commit/push；README 明确克隆只有在未来获准推送这些文件后才能取得本次改动。本地 `npm ci` 和 `go run` 已实际验证。
- 前端使用 Arco 完整样式表，构建 CSS 约 399 kB、gzip 约 50 kB；当前本地骨架可接受，按需样式优化留待真实页面需要时评估。

## 独立审查结论

使用独立上下文审查了后端和前端全部源文件、测试、配置、锁文件，以及 API/README/阶段文档；未发现当前 PB 范围内需要修复的确定性缺陷，无延期的小缺陷清单。此时 CodeGraph 已由现有索引服务索引当前源码，审查先查询 CodeGraph；没有自行初始化索引。

审查者没有修改文件或重新运行构建测试；其独立本地 HTTP 请求受到沙箱套接字权限限制，运行结果以主执行者经批准执行的命令和浏览器证据为准。未验证 Linux、干净克隆、长耗时在途请求排空及多浏览器；不将这些写成已通过。技能引用的 code-reviewer.md 未在本机技能目录找到，使用覆盖当前用户规格和失败路径的自包含审查说明替代；未绕过实际独立审查。

## 人工验收与成员讲解互审登记

结项登记日期：2026 年 11 月 6 日（Asia/Shanghai）。讲解与互审内容由用户在项目对话中确认，实际发生日期和工时未单独提供。

| 项目 | 状态 | 依据 |
| --- | --- | --- |
| PB 人工验收 | 已完成 | 用户在项目对话中确认人工验收已完成。实际验收日期未提供。 |
| A 讲解 | 已完成 | A 讲解了 `GET /health/live` 的请求链路：浏览器发起请求，经 Vite 代理转发到 Go 服务，前端根据响应更新页面。 |
| B 互审 | 已完成 | B 检查了后端停止时页面显示失败提示，以及后端恢复后手动重试成功的结果。 |

**结项结论：PB 已完成，Phase 0 已结项。** 技术验证沿用本记录的既有证据，本次核对结项文档，未重跑测试。Phase 1 从 T01 开始，须明确该任务的实施授权。
