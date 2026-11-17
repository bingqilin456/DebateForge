# 思弈阁 / DebateForge

**思弈阁**是基于 **Go + Eino + MySQL** 与 **Vue 3 + TypeScript + Arco Design** 的专业中文 AI 辩论训练与对抗演练系统。为缺少练习对手的中文辩论学习者提供赛制化的文字对抗训练与结构化复盘反馈，由双人团队按阶段目标交付可访问、可维护、可测试、可答辩的工程实践项目。

当前进度、验证边界、负责人确认与阶段结项统一见 [TASKS](docs/planning/TASKS.md)。SSE 前后端已实现并有 Fake 整合验证；实现与技术验证不等于完整验收，后续门槛以 TASKS 为准。分工按[一功能一负责人](docs/planning/PLAN.md#现行功能分工原则)端到端交付，任务归属以 TASKS 为准。

## 产品版本

| 版本 | 状态 | 入口 |
| --- | --- | --- |
| **产品 V2**（PRD V2） | **需求已确认；未实现、未验收、未获开发阶段授权** | [PRD V2](docs/product/PRD.md)、[需求确认记录](docs/product/PRD_V2_DECISIONS.md)、[技术评估](docs/planning/PRD_V2_TECHNICAL_ASSESSMENT.md) |
| V1（冻结基线） | 只读快照，不再更新；既有 AC/F 编号与历史证据继续引用 | [PRD V1 冻结基线](docs/product/PRD_V1_BASELINE.md) |
| 当前 V1 代码与 HTTP 契约 | 本轮不改写；已实现接口承诺不变 | [API](docs/api/API.md)、[OpenAPI](docs/api/openapi.yaml) |
| Phase 3 验收基线 | 本轮不改写；任务状态、负责人、估算与证据保持原样 | [TASKS](docs/planning/TASKS.md#phase-3赛制流式破局亮点与质量攻坚) |

「不改写」是本轮文档重构的范围限定，**不是永久禁止**：V1 行为与基线后续如经授权变更，须走正常的需求确认、ADR 与任务流程并保留历史证据。

产品 V2 只把新能力登记为待分配/未开始，不构成开发授权，也不改变 V1 的 `standard_4`/`quick_3` 语义与既有流派数据。V2 暂缓流派不等于取消 Phase 3 的 AC16 门槛。命名边界：产品 V2 ≠ `phase3-summary-v2`（总结 `schema_version=2` 的 0–100 四维，T14-REPORT，未开始）≠ `v2-training-reference`（产品 V2 的 1–4 档）；详见 [ADR-027](docs/decisions/DECISIONS.md#adr-027-v2-需求基线与版本边界)。

## 核心能力

以下描述**当前 V1 已实现或已定稿**的能力；产品 V2 的规格见 [PRD V2](docs/product/PRD.md)，其能力尚未实现。

Phase 3 的 P0 产品目标包含用户可在本地浏览器亲自输入、完成真实模型四轮 SSE 对辩、总结及历史重读的 Demo，见 [PRD V1 冻结基线](docs/product/PRD_V1_BASELINE.md#phase-3-真实模型可体验-demop0-交付目标尚未实现)。真实体验须单独明确模型、参与人、窗口与次数/Token/费用上限；公网 ECS/HTTPS 与生产运维归 Phase 4。进度及未验收项见 [TASKS](docs/planning/TASKS.md#phase-3赛制流式破局亮点与质量攻坚)。

| 能力 | 说明 |
| --- | --- |
| 赛制化对辩 | 默认四轮标准赛（立论/驳立论/自由辩/结辩）；三轮快速赛仅历史兼容与隔离测试 |
| AI 陪练 | Eino 驱动，服务端构建 Prompt，创建前自主选择四种辩手流派，显式生成赛前策略 |
| 结构化复盘 | 三栏总结（优点/问题/建议），按赛制校验轮次引用 |
| 可靠性与恢复 | `attempt_version` 版本控制、90 秒租约回收、幂等重放、显式重试 |
| 历史与权限 | 本人历史分页、会话详情、跨用户 404 隔离 |
| SSE 流式对辩 | 增量预览、断线后查询恢复，复用普通发送的认领内核 |

以上能力的实现、验证和验收状态见 TASKS；产品 V2 新增的原文批注、争点卡、局部重练、训练赛可变回合与四维 1–4 档等规格见 [PRD V2](docs/product/PRD.md)，均未实现。逻辑谬误透视、`phase3-summary-v2` 四维战报和双 AI 演示属 V1 基线下 Phase 3 的增强需求，见 [PRD V1 冻结基线](docs/product/PRD_V1_BASELINE.md)。

## 技术栈

- **后端**：Go 1.27.1、Gin、Eino（CloudWeGo）、GORM、MySQL 8.4.13，模块化单体，按 `auth`/`topic`/`debate`/`ai` 划分。
- **前端**：Vue 3、TypeScript、Arco Design、Vite，自研轻量 API client 与运行时契约校验。
- **数据库**：版本化 SQL 迁移（Goose），前向升级、不修改已执行文件。
- **部署**：Linux + Docker Compose + Nginx，单机形态，TLS 终止于 Nginx。
- **门禁**：GitHub Actions 覆盖静态规范、前后端单测与构建、隔离 MySQL 集成测试。

需求以 [PRD V2](docs/product/PRD.md) 为准（V1 冻结基线见 [PRD V1](docs/product/PRD_V1_BASELINE.md)），技术契约见 [ARCHITECTURE](docs/architecture/ARCHITECTURE.md)，接口见 [API](docs/api/API.md) 与 [OpenAPI](docs/api/openapi.yaml)，长期决策见 [DECISIONS](docs/decisions/DECISIONS.md)。产品 V2 需求已确认但未实现，当前可运行行为与契约仍以 V1 为准。

以下各节用于环境准备、启动与检查；历史实测和授权保留在对应验收记录中，不构成新的操作授权。当前进度仅由 [TASKS](docs/planning/TASKS.md) 维护。

## 环境

工程基线使用 Windows amd64、Go **1.27.1**、Node.js **24.11.1**、npm **11.6.2**。先通过 `go version`、`node --version`、`npm --version` 核对版本。当前后端启动需要 MySQL 及数据库秘密文件；本地无收费验证使用 `disabled` 或按说明装配 Fake，无需模型 Key。

本地数据库环境使用 PowerShell **7.2+**（`pwsh`）、Docker Desktop Linux Engine、Compose 和官方 MySQL `8.4.13`；Windows PowerShell 5.1 不受支持。开发库为 `127.0.0.1:13306/siyige_dev`，测试库为 `127.0.0.1:13307/siyige_test`；容器、卷、账户、口令和网络分别隔离。准备步骤见 [MySQL 环境说明](infra/mysql/README.md)，历史实测见 [T02 环境验收记录](docs/acceptance/T02_MYSQL_ENV_ACCEPTANCE.md)。

后端为独立模块 `github.com/bingqilin456/DebateForge/backend`。前端只使用 npm，保留 package-lock.json；不要混用其他包管理器或锁文件。

版本化迁移使用独立工具 module `backend/tools/migrate/`，锁定 Goose v3.29.0，通过显式 up/status 执行；服务启动不建表或播种。版本清单、固定目标保护及失败恢复见 [迁移说明](backend/migrations/README.md)。工具需在其自身目录运行 test/vet/build，服务目录的 `go test ./...` 不覆盖独立 module。迁移、持久化脚本和重启操作需先明确目标与授权，不因命令列在文档中自动执行。

## 获取与启动

以下示例为 PowerShell。已经位于本地工作区时跳过克隆，直接打开两个终端。数据库写入、真实模型调用及部署须符合本轮明确授权，历史验收中的提交/推送授权不延续到当前任务。

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

首次配置优化及路径修复的历史证据见 [dotenv 验收记录](docs/acceptance/T02_DOTENV_ACCEPTANCE.md) 与 [配置及 API 契约修复记录](docs/acceptance/T02_CONFIG_FIX_ACCEPTANCE.md)；后续任务结论统一见 TASKS。

本节为默认开发环境启动方法；固定 test/Fake、独立 Demo 与部署环境应分别按对应运行说明配置。切换环境须同时核对全部 DB 配置及秘密文件，避免 dev 的 `.env` 与 test 的系统变量混用；不能使用 root/迁移账户运行应用。显式空值、非法配置、缺失/不可读秘密文件会启动失败。配置合法但 DB 停机时服务仍启动，ready 返回 503，数据库恢复后后续检查重新 Ping。服务不自动迁移、建表或写种子。

默认监听 `127.0.0.1:8080`。`HTTP_ADDR` 未设置时使用默认值；显式空值、非回环地址、无效端口会启动失败。如果当前终端已有旧配置，可显式设置 `$env:HTTP_ADDR = '127.0.0.1:8080'` 后再运行。按 Ctrl+C 优雅退出，最多等待 5 秒完成在途请求。日志为 JSON，时间使用 UTC；不记录查询参数、请求正文或内部 panic 详情。

终端 B，从仓库根目录启动前端：

```powershell
Set-Location frontend
npm ci
npm run dev
```

前端固定回环监听，5173 被占用时会失败，不悄悄切换端口。Vite 把 `/api/` 原样代理到 `http://127.0.0.1:8080`，历史 `/health/` 不再代理。如果主动改变后端端口，还需同步修改 frontend/vite.config.ts 的 target；本阶段没有额外的代理目标环境变量。
当前页面入口为注册/登录，登录后可选题、辩论和查历史。固定测试库的 SSE/Fake 启动、浏览器操作与停止步骤见 [B2 整合交接](docs/acceptance/PHASE3_B2_INTEGRATION_ACCEPTANCE.md#给-b3-的启动与使用)；本地 Demo 独立环境见 [infra/demo](infra/demo/README.md)，两者目标库不同，按各自说明核对。

## 本地联通验证

保持两个服务运行，在第三个终端执行：

```powershell
Invoke-RestMethod http://127.0.0.1:8080/api/health/live
Invoke-RestMethod http://127.0.0.1:5173/api/health/live
```

两者均应返回 `status: alive`，真实响应为 HTTP 200 和 JSON `{"status":"alive"}`。页面请求使用相对路径，不直接跨域访问 8080，不启用宽泛 CORS。

可在终端 A 按 Ctrl+C 停止后端，再次执行上述请求应连接失败；重新启动后应恢复。该检查只证明 HTTP 进程存活，不替代登录、辩论、总结或历史链路验收。

`GET /api/health/ready` 在数据库连通时返回 HTTP 200 和 `{"status":"ready"}`，不可达或 3 秒预算内未响应则为 HTTP 503 和 `{"status":"not_ready"}`。健康响应不缓存；live 不访问 DB 或模型。ready 不证明业务表/种子或模型可用；业务接口契约见 [API](docs/api/API.md)。前端开发服务器仅用于本地开发，不是生产部署方式。

## T03 认证配置

`AUTH_ORIGIN` 默认 `http://127.0.0.1:5173`，表示浏览器页面来源；所有认证 POST 必须携带该精确 Origin，退出另需当前 Session 的 `X-CSRF-Token`。注册不自动登录，登录和 me 返回 CSRF，Session 原值只通过 HttpOnly Cookie 传输。HTTP 开发 Cookie 为 `siyige_session_dev`；HTTPS 策略使用独立的 `__Host-siyige_session` 并开启 Secure；不使用 localStorage/JWT。

当前 `APP_ENV` 支持范围以 [配置示例](backend/.env.example) 与配置校验为准。历史 T03 验收使用固定隔离测试库，命令见下节；开发库的实际迁移状态须在获准操作时核对。认证配置及历史验收见 [T03 记录](docs/acceptance/T03_AUTH_ACCEPTANCE.md)，正式页面和后续联调进度见 TASKS。

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

以下列出部分直接依赖及原选型说明。完整依赖与精确版本以 `backend/go.mod`、迁移工具独立 `go.mod` 和 `frontend/package-lock.json` 为准；实际验证结果查对应任务记录，不以安装成功或满足 peerDependencies 代替。

| 直接依赖                           | 用途、选择依据                          | 许可证        | 替代方案与取舍                  |
| ------------------------------ | -------------------------------- | ---------- | ------------------------ |
| Gin 1.12.0                     | HTTP 路由，沿用 A 已有经验及 ADR-001       | MIT        | net/http 依赖少；当前沿用已批准 Gin |
| GORM 1.31.2 / driver/mysql 1.6.0 | 连接、业务持久化与短事务 | MIT | database/sql 需手工维护映射；本轮沿用 ADR-004 |
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


依据：[Gin 官方入门](https://gin-gonic.com/en/docs/quickstart/)、[Vite 官方文档](https://vite.dev/guide/)、[Vue TypeScript 指南](https://vuejs.org/guide/typescript/overview.html)、[Arco 官方仓库](https://github.com/arco-design/arco-design-vue)、[Vitest 官方指南](https://vitest.dev/guide/)、[GORM 连接](https://gorm.io/docs/connecting_to_the_database.html)、[MySQL Go 驱动](https://github.com/go-sql-driver/mysql/tree/v1.8.1)、[godotenv 官方说明](https://github.com/joho/godotenv/tree/v1.5.1)。许可证与版本同时核对本地已安装包的元数据；传递依赖版本以模块/锁文件为准。服务使用 Eino，前端使用 Playwright 开发依赖进行浏览器测试；当前页面切换由 App 的 hash 状态处理，未引入 Vue Router、Pinia 或 Axios。Goose 仅在独立迁移工具 module 使用，依赖说明见迁移 README。Go mod tidy 会下载 GORM 自身测试引用的 SQLite 模块元数据；本项目未导入 SQLite，也不以其替代 MySQL 集成测试。

## 文档与下一步

| 文档 | 用途 |
| --- | --- |
| [文档导航](docs/README.md) | 推荐阅读顺序、分类入口、状态差异及本次路径迁移对照 |
| [AGENTS.md](AGENTS.md) | 协作规范、功能负责人分工、操作授权规则、安全及验收边界 |
| [PRD V2](docs/product/PRD.md) | 当前产品规格：V2 完整需求、流程、边界与 AC-V2（**未实现**） |
| [PRD V1 冻结基线](docs/product/PRD_V1_BASELINE.md) | V1 只读快照：F01–F14、AC01–AC17 与历史验收语境 |
| [PRD V2 需求确认记录](docs/product/PRD_V2_DECISIONS.md) | Q1–Q51、Q22a、Q52 的逐项确认事实源 |
| [V2 技术评估](docs/planning/PRD_V2_TECHNICAL_ASSESSMENT.md) | 相对成本、风险、差异化、依赖顺序与待评估项 |
| [ARCHITECTURE](docs/architecture/ARCHITECTURE.md) | 模块职责、技术实现与目标设计边界 |
| [API](docs/api/API.md) | 健康、认证、辩论、总结、策略与流式接口契约 |
| [OpenAPI 3.1](docs/api/openapi.yaml) | 机器可读接口契约、示例及各操作实现标记；验收状态查 TASKS |
| [T03 认证记录](docs/acceptance/T03_AUTH_ACCEPTANCE.md) | Codex/DSH 分工、技术验证、复核命令、问题与独立审查/人工验收状态 |
| [PLAN](docs/planning/PLAN.md) / [TASKS](docs/planning/TASKS.md) | 阶段依赖与门槛（含[现行功能分工原则](docs/planning/PLAN.md#现行功能分工原则)） / 当前任务进度、风险、功能负责人、验证与负责人确认 |
| [DECISIONS](docs/decisions/DECISIONS.md) | 选型理由、授权增量、未落实假设 |
| [T05 / T06-B 集成](docs/acceptance/T05_T06_B_GIT_INTEGRATION.md) | Git 基线、合并关系、冲突处理与本轮验证边界 |
| [T06-B 交接](docs/acceptance/T06_B_HANDOFF.md) | 统一 client、认证状态/独立表单、本地预览与 A 接线；不代表 T06 整体验收 |
| [INIT_ACCEPTANCE](docs/acceptance/INIT_ACCEPTANCE.md) | 实际命令、结果、失败恢复和人工门槛 |

A 为功能负责人提供技术支持与风险把关，B 负责的功能同样须端到端完整交付。自 2026-11-16 起任务按[一功能一负责人](docs/planning/PLAN.md#现行功能分工原则)端到端交付：单一负责人统筹该功能的前端、后端、测试与必要文档，A 保留认证、持久化、生成认领、费用与部署的技术支持与风险把关，但不是只准 A 改，也不要求每个小改动强制互审；答辩按实际负责功能讲解，见 [ADR-026](docs/decisions/DECISIONS.md#adr-026-功能负责人端到端交付)。自 T01 起取消强制 A/B 互审和正式契约讲解登记，保留 Codex 技术审查、必要的自动化测试、相关高风险专项验证与项目负责人最终验收，见 [ADR-011](docs/decisions/DECISIONS.md#adr-011-简化验收流程与-t01-结项)。AI 生成和测试不能替代成员理解。references 仅供本地参考，不纳入版本管理。

数据库、认证、AI 辩论、生产部署及 Git 提交、推送、合并均需对应授权。

