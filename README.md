# 思弈阁 Lite / DebateForge

面向中文辩论初学者的 AI 文字辩论训练项目，双人团队按阶段目标交付可访问、可维护、可测试、可答辩的 MVP，各阶段不限定自然日数或固定投入时长。

**Phase 0（工程初始化与基线验收）已结项，PB 已完成。T01 已于 2026 年 11 月 7 日经项目负责人验收结项，仅完成 Phase 1 首批 8 个 API 的契约设计，不代表业务代码已实现。** 应用运行交付仍为 Go/Gin 存活接口及 Vue 本地联通页面，技术验证和 Phase 0 结项记录见 [INIT_ACCEPTANCE](docs/INIT_ACCEPTANCE.md)。T02 进行中，首个获准子任务为隔离 MySQL 本地环境，技术验证后待人工验收，完整 T02 未完成；T03–T18 尚未开始。阶段计划及暂停恢复规则见 [PLAN](docs/PLAN.md)。

## 环境

首轮实际环境为 Windows amd64、Go **1.27.1**、Node.js **24.11.1**、npm **11.6.2**。先通过 `go version`、`node --version`、`npm --version` 核对版本。PB 应用仍无需 MySQL、Docker、模型 Key 或环境文件。

T02 本地数据库环境独立使用 PowerShell **7.2+**（`pwsh`）、Docker Desktop Linux Engine、Compose 和官方 MySQL `8.4.13`；Windows PowerShell 5.1 不受支持。开发库为 `127.0.0.1:13306/siyige_dev`，测试库为 `127.0.0.1:13307/siyige_test`；容器、卷、账户、口令和网络分别隔离。启动、连接及验证命令见 [MySQL 环境说明](infra/mysql/README.md)，实际证据见 [T02 环境验收记录](docs/T02_MYSQL_ENV_ACCEPTANCE.md)。当前后端尚未连接数据库，`/health/ready` 仍未实现。

后端为独立模块 `github.com/bingqilin456/DebateForge/backend`。前端只使用 npm，保留 package-lock.json；不要混用其他包管理器或锁文件。

## 获取与启动

以下示例为 PowerShell。克隆步骤只在本次文件经人工审查并另行获准提交、推送到远程后才能取得这些改动；本任务没有执行提交或推送。已经位于本地工作区时跳过克隆，直接打开两个终端。

```powershell
git clone https://github.com/bingqilin456/DebateForge.git
Set-Location DebateForge
```

终端 A，从仓库根目录启动后端：

```powershell
Set-Location backend
go mod download
go run ./cmd/server
```

默认监听 `127.0.0.1:8080`。`HTTP_ADDR` 未设置时使用默认值；显式空值、非回环地址、无效端口会启动失败。如果当前终端已有旧配置，可显式设置 `$env:HTTP_ADDR = '127.0.0.1:8080'` 后再运行。按 Ctrl+C 优雅退出，最多等待 5 秒完成在途请求。日志为 JSON，时间使用 UTC；不记录查询参数、请求正文或内部 panic 详情。

终端 B，从仓库根目录启动前端：

```powershell
Set-Location frontend
npm ci
npm run dev
```

前端固定回环监听，5173 被占用时会失败，不悄悄切换端口。Vite 把 `/health/` 和 `/api/` 原样代理到 `http://127.0.0.1:8080`。如果主动改变后端端口，还需同步修改 frontend/vite.config.ts 的两个 target；本阶段没有额外的代理目标环境变量。
页面自动检查一次，展示检查中、成功或失败；失败后点击“重试连接”，不会自动循环重试。成功仅表示 Go HTTP 进程存活。

## 本地联通验证

保持两个服务运行，在第三个终端执行：

```powershell
Invoke-RestMethod http://127.0.0.1:8080/health/live
Invoke-RestMethod http://127.0.0.1:5173/health/live
```

两者均应返回 `status: alive`，真实响应为 HTTP 200 和 JSON `{"status":"alive"}`。页面请求使用相对路径，不直接跨域访问 8080，不启用宽泛 CORS。

在终端 A 按 Ctrl+C 停止后端，保持前端运行，在页面点击“重新检查”：应显示失败，并出现“重试连接”。重新运行后端，点击“重试连接”：应恢复成功。客户端总超时 3 秒，校验 HTTP 状态、JSON Content-Type 和响应结构，HTML 200 不能判为健康。

`/health/ready` 尚未注册，应返回受控 JSON 404；没有业务 API。前端开发服务器仅用于本地开发，不是生产部署方式。

## 测试与构建

在 backend 目录执行：

```powershell
go test ./...
go vet ./...
go build ./...
gofmt -l cmd internal
```

前三项应退出 0，最后一项应无输出。测试覆盖监听配置、健康契约、404/405、request_id、日志脱敏与 panic 的受控响应；不连接数据库或模型。

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
| Vue 3.5.44                     | 已批准的 Composition API 与单文件组件      | MIT        | 原生 DOM；不另换框架             |
| Arco Design 2.59.0             | 已批准的成熟 Vue 组件库，复用按钮、提示和标签        | MIT        | 原生 HTML/CSS；暂不另建组件体系     |
| Vite 8.3.5 / plugin-vue 6.0.10 | 官方 Vue 插件、开发代理与构建                | MIT        | 手工 Rollup 配置增加维护成本       |
| TypeScript 5.9.3               | 成熟 5.x 稳定版本，暂不升级新主版本             | Apache-2.0 | JavaScript 缺少当前要求的静态类型检查 |
| vue-tsc 3.3.14                 | Vue 官方推荐的 SFC 类型检查工具             | MIT        | tsc 不能独立完整检查 Vue SFC     |
| Vitest 5.0.5                   | 与 Vite 集成的稳定单测工具；使用真实 HTTP 验证客户端 | MIT        | node:test 需另安排 TS 测试执行方式 |
| @types/node 24.10.1            | Node 24 API 类型，供配置与测试使用          | MIT        | 不应以 any 替代类型             |


依据：[Gin 官方入门](https://gin-gonic.com/en/docs/quickstart/)、[Vite 官方文档](https://vite.dev/guide/)、[Vue TypeScript 指南](https://vuejs.org/guide/typescript/overview.html)、[Arco 官方仓库](https://github.com/arco-design/arco-design-vue)、[Vitest 官方指南](https://vitest.dev/guide/)。许可证与版本同时核对本地已安装包的元数据；传递依赖版本以模块/锁文件为准。暂不引入 Vue Router、Pinia、Axios、dotenv、数据库包、Eino 或浏览器测试依赖。

## 文档与下一步

| 文档 | 用途 |
| --- | --- |
| [AGENTS.md](AGENTS.md) | 协作规范、当前授权、安全及验收边界 |
| [PRD](docs/PRD.md) | 完整 MVP 产品范围与验收标准 |
| [ARCHITECTURE](docs/ARCHITECTURE.md) | 架构与后续业务设计，区分已实现和草案 |
| [API](docs/API.md) | 健康检查与 Phase 1 业务接口契约，业务尚未实现 |
| [PLAN](docs/PLAN.md) / [TASKS](docs/TASKS.md) | 阶段依赖与独立任务，PB 与 T01 已完成，T02 仅环境子任务进行中 |
| [DECISIONS](docs/DECISIONS.md) | 选型理由、授权增量、未落实假设 |
| [INIT_ACCEPTANCE](docs/INIT_ACCEPTANCE.md) | 实际命令、结果、失败恢复和人工门槛 |

A 负责后端与集成，B 负责明确的前端与测试任务，后续独立完成并理解辩题与历史页面。自 T01 起取消强制 A/B 互审和正式契约讲解登记，保留 Codex 技术审查、必要的自动化测试、相关高风险专项验证与项目负责人最终验收，见 [ADR-011](docs/DECISIONS.md#adr-011-简化验收流程与-t01-结项)。AI 生成和测试不能替代成员理解。references 仅供本地参考，不纳入版本管理。

数据库、认证、AI 辩论、生产部署及 Git 提交、推送、合并均需对应授权。

