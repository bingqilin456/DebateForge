# 思弈阁 Lite / DebateForge

**T06 已完成（2026-11-09）。** 项目负责人确认人工验收通过。正式入口支持注册、登录、登录恢复与退出，以及预设辩题、立场选择和首轮辩论。真实 T03/T05 接口使用隔离 MySQL 与 Fake 模型完成浏览器联调，技术证据与本地运行步骤见 [T06 验收记录](docs/acceptance/T06_COMPLETION_ACCEPTANCE.md)。T07-COST-01 继续阻塞公网真实 AI 入口。下方集成状态和 PB 健康页操作是历史记录。

**当前集成状态（2026-11-09）：T02–T05 累积成果、文档重组与 T06-B 在 `feat/integration-t05-t06-b` 集成，准备面向 `develop` 的 PR。** T02 已验收、T03 有条件验收及 P2/P3 延期保留、T04 已结项、T05 无收费范围已结项；真实模型链路未验收，T07-COST-01 仍阻塞公网真实入口。T06-B 的统一 client、认证 API/内存状态、独立表单和本地模拟预览已交付，见 [交接](docs/acceptance/T06_B_HANDOFF.md)；尚未接入 App.vue 或完成真实 T03/T05 浏览器联调，T06 整体进行中、未验收。本轮仅 Git 集成与必要验证，PR 创建后停止，等待全新会话独立只读审查及负责人决定；下方旧状态均保留当时语境。

**T05 无收费范围正式结项（2026-11-08）：项目负责人确认三关人工验收全部通过，批准 T05 无收费实现范围结项并授权 Git Commit。** 四接口、累计无收费验证与新会话独立复审 PASS，T05-R01/R02/R03 三项 P2 均已独立确认关闭；已接受限制及后续任务见 [T05 验收](docs/acceptance/T05_FIRST_TURN_ACCEPTANCE.md)。本轮仅登记验收、核对提交范围，并从 `codex/docs-reorganization` 当前 HEAD 创建 `feat/t05-first-turn` 后提交；不修改已验收业务代码、不重跑完整测试或独立审查，无需 DSH。禁止收费调用、Push、Merge、Rebase、Reset、PR 或部署，不进入 T06。真实模型链路未验收，仍须另行授权；T07-COST-01 继续阻塞公网真实入口。单进程并发未来评估提升至 4–8，当前仍为 2。T03 已接受的 P2/P3 延期及 T04 已结项事实保留；下方旧授权与状态保留当时语境，以本段及最新验收记录为准。

**T03 当前增量（2026 年 11 月 8 日）：用户另行授权完整认证实现，现已通过 Codex 集成技术验证，待独立审查与项目负责人人工验收。** 注册、登录、退出、登录状态恢复及认证保护已实现；见 [T03 验收记录](docs/acceptance/T03_AUTH_ACCEPTANCE.md)。不进入 T04–T06，不 commit/push/merge/deploy。下方 T03 不启动及业务未实现等内容为历史说明，不覆盖本轮增量。

**当前状态（2026 年 11 月 8 日）：T02 已完成，最终独立技术审查和项目负责人人工验收通过。** M01/M02 已关闭，M03 及已接受的验证/恢复边界继续延期，见 [最终验收记录](docs/acceptance/T02_SEEDS_PERSISTENCE_ACCEPTANCE.md)。本轮仅授权提交并推送到 `origin/feat/t02-database`，不进入 T03；Phase 1 尚未完成。下文早期“进行中/待验收”文字保留当时状态，以本段为当前依据。

面向中文辩论初学者的 AI 文字辩论训练项目，双人团队按阶段目标交付可访问、可维护、可测试、可答辩的 MVP，各阶段不限定自然日数或固定投入时长。

**Phase 0（工程初始化与基线验收）已结项，PB 已完成。T01 已于 2026 年 11 月 7 日经项目负责人验收结项，仅完成 Phase 1 首批 8 个 API 的契约设计，不代表业务代码已实现。** Phase 0 结项记录见 [INIT_ACCEPTANCE](docs/acceptance/INIT_ACCEPTANCE.md)。T02 进行中，已有隔离 MySQL 环境，本轮新增后端连接、Readiness 和测试目标保护，技术验证后待人工验收；业务迁移/种子未实施，完整 T02 未完成，T03–T18 尚未开始。前端仍为本地联通页面。阶段计划及暂停恢复规则见 [PLAN](docs/planning/PLAN.md)。

2026 年 11 月 8 日当前进展：六表初始迁移后，新增 00007–00009 精确枚举 CHECK、00010 五条事务性辩题种子、历史保护回归及测试 MySQL 服务重启验证，技术闭环已通过，见 [种子与持久化记录](docs/acceptance/T02_SEEDS_PERSISTENCE_ACCEPTANCE.md)。完整 T02 仍进行中，等待独立审查、辩题人工审阅、负责人验收及学习讲解；本轮不提交、推送或合并。上段及下方“种子未实施”等早期接入描述保留为历史状态。初始迁移的有条件审查及历史提交授权见 [迁移记录](docs/acceptance/T02_MIGRATIONS_ACCEPTANCE.md)。

## 环境

首轮实际环境为 Windows amd64、Go **1.27.1**、Node.js **24.11.1**、npm **11.6.2**。先通过 `go version`、`node --version`、`npm --version` 核对版本。PB 应用仍无需 MySQL、Docker、模型 Key 或环境文件。

T02 本地数据库环境独立使用 PowerShell **7.2+**（`pwsh`）、Docker Desktop Linux Engine、Compose 和官方 MySQL `8.4.13`；Windows PowerShell 5.1 不受支持。开发库为 `127.0.0.1:13306/siyige_dev`，测试库为 `127.0.0.1:13307/siyige_test`；容器、卷、账户、口令和网络分别隔离。启动、连接及验证命令见 [MySQL 环境说明](infra/mysql/README.md)，实际证据见 [T02 环境验收记录](docs/acceptance/T02_MYSQL_ENV_ACCEPTANCE.md)。本轮后端已接入 GORM/MySQL 和 `/api/health/ready`，技术验证后待人工验收；版本化迁移和种子仍未实施，完整 T02 未完成，见 [后端接入记录](docs/acceptance/T02_DATABASE_CONNECTION_ACCEPTANCE.md)。上文 PB 应用不依赖数据库的描述保留为历史基线，当前启动需要秘密文件。

后端为独立模块 `github.com/bingqilin456/DebateForge/backend`。前端只使用 npm，保留 package-lock.json；不要混用其他包管理器或锁文件。

版本化迁移使用独立工具 module `backend/tools/migrate/`，锁定 Goose v3.29.0，通过显式 up/status 执行；服务启动不建表或播种。当前共十个 SQL 文件：六表、三个枚举修复、五条种子版本，固定目标保护及失败恢复见 [迁移说明](backend/migrations/README.md)。只在测试库验证，开发库未迁移。工具需在其自身目录运行 test/vet/build，服务目录的 `go test ./...` 不会覆盖独立 module。根目录可显式运行 `pwsh -NoProfile -File infra/mysql/verify-t02-persistence.ps1` 重启测试 MySQL 并核对持久化，详见该验收记录。

## 获取与启动

以下示例为 PowerShell。迁移子任务已于 2026 年 11 月 8 日获负责人有条件验收及提交、推送授权；原实施阶段未提交的说明保留在历史验收记录中。已经位于本地工作区时跳过克隆，直接打开两个终端。

```powershell
git clone https://github.com/bingqilin456/DebateForge.git
Set-Location DebateForge
```

终端 A，从仓库根目录启动后端：

```powershell
Set-Location backend
# 仅首次复制；已有 .env 时保留自己的配置。
if (!(Test-Path -LiteralPath '.env')) { Copy-Item .env.example .env }
# 首次编辑 .env，将 DB_PASSWORD_FILE 占位符替换为实际秘密文件绝对路径。
# 可用 (Resolve-Path '../.cache/t02-mysql/secrets/dev-app.txt').Path 查询路径。
go mod download
go run ./cmd/server
```

先按 MySQL 环境说明准备并启动已有开发库；秘密文件路径为绝对路径，不把口令放进 `.env`、环境变量或命令参数。`.env.example` 提供 APP_ENV、HTTP_ADDR、AUTH_ORIGIN、DB_ADDR、DB_NAME、DB_USER、DB_PASSWORD_FILE；Windows 路径建议使用正斜杠并保留单引号。首次配置后，每次只需在 `backend/` 执行 `go run ./cmd/server`。本地 `.env` 已被 Git 忽略，`.env.example` 可提交且不含真实秘密或个人路径。

服务启动先用 `godotenv.Read()` 解析**当前工作目录**的 `.env`，逐项通过 `os.LookupEnv()` 判断是否补充，再执行原有 `LoadConfig()`。优先级为：**已有进程环境变量（含显式空值） > `.env` > 原有默认值**；Windows 按系统规则识别大小写不同的同名变量。只加载这一份文件，不自动读取 `.env.test` 或父目录。缺少 `.env` 时继续使用环境变量和默认值，CI 无需创建环境文件，但必填的 `DB_PASSWORD_FILE` 及原有校验仍生效。文件不可读、解析失败或 `os.Setenv()` 拒绝非法变量时启动失败，错误不回显文件内容。`LoadConfig()` 和集成测试入口不自行加载 `.env`，默认测试流程不读取本地开发配置。

首次配置优化的历史证据见 [dotenv 验收记录](docs/acceptance/T02_DOTENV_ACCEPTANCE.md)；ADR-012 路径统一、Windows 兼容修复及本轮验证见 [配置与 API 契约修复记录](docs/acceptance/T02_CONFIG_FIX_ACCEPTANCE.md)。负责人已取消小范围独立复验并授权提交、推送当前分支；完整 T02 未结项。

`APP_ENV` 默认 dev，也可显式为 test；未设置 DB 地址/库/账户时按该环境选取现有固定默认值。切换环境必须同时调整全部 DB 配置，避免 dev 的 `.env` 与 test 的系统变量混用。仅支持本地回环地址和对应应用账户，禁止 root/迁移账户；显式空值、非法配置、缺失/不可读秘密文件会启动失败。配置合法但 DB 停机时服务仍启动，ready 返回 503，数据库恢复后后续检查重新 Ping。当前不接受生产配置或任意 DSN，不自动迁移、建表或写种子。

默认监听 `127.0.0.1:8080`。`HTTP_ADDR` 未设置时使用默认值；显式空值、非回环地址、无效端口会启动失败。如果当前终端已有旧配置，可显式设置 `$env:HTTP_ADDR = '127.0.0.1:8080'` 后再运行。按 Ctrl+C 优雅退出，最多等待 5 秒完成在途请求。日志为 JSON，时间使用 UTC；不记录查询参数、请求正文或内部 panic 详情。

终端 B，从仓库根目录启动前端：

```powershell
Set-Location frontend
npm ci
npm run dev
```

前端固定回环监听，5173 被占用时会失败，不悄悄切换端口。Vite 把 `/api/` 原样代理到 `http://127.0.0.1:8080`，历史 `/health/` 不再代理。如果主动改变后端端口，还需同步修改 frontend/vite.config.ts 的 target；本阶段没有额外的代理目标环境变量。
页面自动检查一次，展示检查中、成功或失败；失败后点击“重试连接”，不会自动循环重试。成功仅表示 Go HTTP 进程存活。

## 本地联通验证

保持两个服务运行，在第三个终端执行：

```powershell
Invoke-RestMethod http://127.0.0.1:8080/api/health/live
Invoke-RestMethod http://127.0.0.1:5173/api/health/live
```

两者均应返回 `status: alive`，真实响应为 HTTP 200 和 JSON `{"status":"alive"}`。页面请求使用相对路径，不直接跨域访问 8080，不启用宽泛 CORS。

在终端 A 按 Ctrl+C 停止后端，保持前端运行，在页面点击“重新检查”：应显示失败，并出现“重试连接”。重新运行后端，点击“重试连接”：应恢复成功。客户端总超时 3 秒，校验 HTTP 状态、JSON Content-Type 和响应结构，HTML 200 不能判为健康。

`GET /api/health/ready` 在数据库连通时返回 HTTP 200 和 `{"status":"ready"}`，不可达或 3 秒预算内未响应则为 HTTP 503 和 `{"status":"not_ready"}`。健康响应不缓存；live 不访问 DB 或模型。ready 不证明业务表/种子就绪；当前已实现的业务为四条认证接口，见 API。前端开发服务器仅用于本地开发，不是生产部署方式。

## T03 认证配置

`AUTH_ORIGIN` 默认 `http://127.0.0.1:5173`，表示浏览器页面来源；所有认证 POST 必须携带该精确 Origin，退出另需当前 Session 的 `X-CSRF-Token`。注册不自动登录，登录和 me 返回 CSRF，Session 原值只通过 HttpOnly Cookie 传输。HTTP 开发 Cookie 为 `siyige_session_dev`；HTTPS 策略使用独立的 `__Host-siyige_session` 并开启 Secure；不使用 localStorage/JWT。

当前 APP_ENV 仍只允许 dev/test，开发库未迁移；T03 实际验收使用固定隔离测试库，命令见下节。开发库显式迁移须按相应授权执行，不在本轮自动运行。配置、安全边界和独立审查状态见 [T03 记录](docs/acceptance/T03_AUTH_ACCEPTANCE.md)。T06-B 已提供独立认证表单与本地模拟预览，见 [交接](docs/acceptance/T06_B_HANDOFF.md)；正式入口与首轮面板尚未接入。

## 测试与构建

在 backend 目录执行：

```powershell
go test ./...
go vet ./...
go build ./...
go mod verify
gofmt -l cmd internal tests
```

test/vet/build/verify 应退出 0，gofmt 应无输出。默认测试不连接真实 MySQL 或模型，覆盖配置/目标保护、健康 JSON、超时/取消、404/405、request_id、脱敏、真实本地 HTTP 入口与退出。连接测试用临时回环端口模拟拒绝连接和握手停滞；集成测试默认跳过。

在 backend 目录显式启用真实隔离 MySQL 集成测试（需现有测试容器运行）：

```powershell
$env:SIYIGE_INTEGRATION = '1'
$env:APP_ENV = 'test'
$env:DB_ADDR = '127.0.0.1:13307'
$env:DB_NAME = 'siyige_test'
$env:DB_USER = 'siyige_test_app'
$env:DB_PASSWORD_FILE = (Resolve-Path '../.cache/t02-mysql/secrets/test-app.txt').Path
go test -count=1 -v ./tests/integration
```

测试入口在读取秘密和联网前校验固定测试目标，并在连接后核对实际库/账户，任何不符均失败。T03 新增真实 HTTP/Argon2id/GORM 认证测试，会写本轮随机用户和 Session、验证过期/恢复/并发并按确切 ID 清理；不迁移、重置数据或操作容器。旧连接与健康用例仍只读。默认单测前清除 `SIYIGE_INTEGRATION`，避免沿用显式集成模式。切回开发时可新开终端，或清除当前终端的覆盖值后再从 `backend/` 启动，让本地 `.env` 生效：

```powershell
'SIYIGE_INTEGRATION', 'APP_ENV', 'HTTP_ADDR', 'DB_ADDR', 'DB_NAME', 'DB_USER', 'DB_PASSWORD_FILE' |
    ForEach-Object { Remove-Item -LiteralPath "Env:$_" -ErrorAction SilentlyContinue }
go run ./cmd/server
```

在 frontend 目录执行：

```powershell
npm run typecheck
npm run test
npm run build
```

分别执行 vue-tsc 类型检查、Vitest 单次测试及 Vite 构建。客户端测试启动临时回环 HTTP 服务，覆盖成功、HTTP 失败、HTML/JSON/结构错误、请求头和响应体超时、网络不可达及失败后显式重试。构建产物在 dist，未执行部署。

## 当前依赖

以下均来自官方稳定发布包，精确组合经过本任务实际检查后才记为验证通过。后端 go.mod 固定模块版本，go.sum 校验下载内容；前端 package-lock.json 固定依赖树。不会把“满足 peerDependencies”当作测试结果。

| 直接依赖                           | 用途、选择依据                          | 许可证        | 替代方案与取舍                  |
| ------------------------------ | -------------------------------- | ---------- | ------------------------ |
| Gin 1.12.0                     | HTTP 路由，沿用 A 已有经验及 ADR-001       | MIT        | net/http 依赖少；当前沿用已批准 Gin |
| GORM 1.31.2 / driver/mysql 1.6.0 | 冻结的成熟数据访问方案，当前验证连接，后续用于 CRUD/短事务 | MIT | database/sql 需手工维护映射；本轮沿用 ADR-004 |
| go-sql-driver/mysql 1.8.1 | GORM 驱动声明的成熟 MySQL 连接器，提供连接参数与 Context 支持 | MPL-2.0 | 直接拼接 DSN 易误配；使用结构化 Config 和秘密文件 |
| godotenv 1.5.1 | 成熟的 Go dotenv 库，仅在服务启动时补充本地环境变量，保留已有环境优先级 | MIT | 手动设置系统变量步骤多；自写解析器增加格式与转义维护成本 |
| golang.org/x/crypto 0.48.0 | 已锁定的 Go 官方扩展库，T03 直接使用 Argon2id；仅调整直接依赖标记，未升级版本 | BSD-3-Clause | 第三方封装增加依赖；不自写密码算法 |
| Vue 3.5.44                     | 已批准的 Composition API 与单文件组件      | MIT        | 原生 DOM；不另换框架             |
| Arco Design 2.59.0             | 已批准的成熟 Vue 组件库，复用按钮、提示和标签        | MIT        | 原生 HTML/CSS；暂不另建组件体系     |
| Vite 8.3.5 / plugin-vue 6.0.10 | 官方 Vue 插件、开发代理与构建                | MIT        | 手工 Rollup 配置增加维护成本       |
| TypeScript 5.9.3               | 成熟 5.x 稳定版本，暂不升级新主版本             | Apache-2.0 | JavaScript 缺少当前要求的静态类型检查 |
| vue-tsc 3.3.14                 | Vue 官方推荐的 SFC 类型检查工具             | MIT        | tsc 不能独立完整检查 Vue SFC     |
| Vitest 5.0.5                   | 与 Vite 集成的稳定单测工具；使用真实 HTTP 验证客户端 | MIT        | node:test 需另安排 TS 测试执行方式 |
| @types/node 24.10.1            | Node 24 API 类型，供配置与测试使用          | MIT        | 不应以 any 替代类型             |


依据：[Gin 官方入门](https://gin-gonic.com/en/docs/quickstart/)、[Vite 官方文档](https://vite.dev/guide/)、[Vue TypeScript 指南](https://vuejs.org/guide/typescript/overview.html)、[Arco 官方仓库](https://github.com/arco-design/arco-design-vue)、[Vitest 官方指南](https://vitest.dev/guide/)、[GORM 连接](https://gorm.io/docs/connecting_to_the_database.html)、[MySQL Go 驱动](https://github.com/go-sql-driver/mysql/tree/v1.8.1)、[godotenv 官方说明](https://github.com/joho/godotenv/tree/v1.5.1)。许可证与版本同时核对本地已安装包的元数据；传递依赖版本以模块/锁文件为准。服务及前端暂不引入 Vue Router、Pinia、Axios、Eino 或浏览器测试依赖；Goose 仅在独立迁移工具 module 使用，依赖说明见迁移 README。Go mod tidy 会下载 GORM 自身测试引用的 SQLite 模块元数据；本项目未导入 SQLite，也不以其替代 MySQL 集成测试。

## 文档与下一步

| 文档 | 用途 |
| --- | --- |
| [文档导航](docs/README.md) | 推荐阅读顺序、分类入口、状态差异及本次路径迁移对照 |
| [AGENTS.md](AGENTS.md) | 协作规范、当前授权、安全及验收边界 |
| [PRD](docs/product/PRD.md) | 完整 MVP 产品范围与验收标准 |
| [ARCHITECTURE](docs/architecture/ARCHITECTURE.md) | 架构与后续业务设计，区分已实现和草案 |
| [API](docs/api/API.md) | 健康检查与 Phase 1 业务接口契约；4 个认证接口和 4 个 T05 业务接口已实现 |
| [OpenAPI 3.1](docs/api/openapi.yaml) | 10 个已实现接口；T05 无收费范围独立复审 PASS、人工验收通过并结项，本地与 HTTPS Cookie 名已明确 |
| [T03 认证记录](docs/acceptance/T03_AUTH_ACCEPTANCE.md) | Codex/DSH 分工、技术验证、复核命令、问题与独立审查/人工验收状态 |
| [PLAN](docs/planning/PLAN.md) / [TASKS](docs/planning/TASKS.md) | 阶段依赖与独立任务，PB 与 T01 已完成，T02 已完成最终独立审查及人工验收，M03 等已接受风险继续延期 |
| [DECISIONS](docs/decisions/DECISIONS.md) | 选型理由、授权增量、未落实假设 |
| [T05 / T06-B 集成](docs/acceptance/T05_T06_B_GIT_INTEGRATION.md) | Git 基线、合并关系、冲突处理与本轮验证边界 |
| [T06-B 交接](docs/acceptance/T06_B_HANDOFF.md) | 统一 client、认证状态/独立表单、本地预览与 A 接线；不代表 T06 整体验收 |
| [INIT_ACCEPTANCE](docs/acceptance/INIT_ACCEPTANCE.md) | 实际命令、结果、失败恢复和人工门槛 |

A 负责后端与集成，B 负责明确的前端与测试任务，后续独立完成并理解辩题与历史页面。自 T01 起取消强制 A/B 互审和正式契约讲解登记，保留 Codex 技术审查、必要的自动化测试、相关高风险专项验证与项目负责人最终验收，见 [ADR-011](docs/decisions/DECISIONS.md#adr-011-简化验收流程与-t01-结项)。AI 生成和测试不能替代成员理解。references 仅供本地参考，不纳入版本管理。

数据库、认证、AI 辩论、生产部署及 Git 提交、推送、合并均需对应授权。

