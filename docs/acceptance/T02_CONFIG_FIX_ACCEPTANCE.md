# T02 配置与 API 契约修复验收记录

日期：2026 年 11 月 8 日（Asia/Shanghai）。状态：本轮修复与自动化验证完成；项目负责人随后明确取消小范围独立复验，并授权在 `feat/t02-database` 整理提交和推送。完整 T02 仍进行中，迁移、种子及后续任务未实施。

## 授权与基线

本轮依用户明确要求，以 [ADR-012](../decisions/DECISIONS.md#adr-012-健康端点统一为当前-gin-路径) 为准使用 `GET /api/health/live`、`GET /api/health/ready`，不恢复历史路径，也不增加兼容别名。业务契约保持 T01 设计。

开始时分支为 `feat/t02-database`、HEAD 为 `fe047c1`，暂存区为空，存在数据库接入、dotenv、API/OpenAPI 和前端的未提交修改。本轮开始时，Gin 路由、API/OpenAPI、前端默认请求和页面提示，以及后端 HTTP、Readiness、集成测试路径已使用新契约；这些在途改动保留并验证，不记为本轮新增实现。后端路由测试同时保留旧路径返回 404 的反例。

本轮开始前的可跟踪文件副本、SHA-256 和 Git 状态保存在忽略目录 `.cache/t02-config-fix-4da90352290f489bb46897f1462264c6/`。不复制或读取真实秘密文件，不修改本地 `backend/.env`。

## 本轮增量

| 文件 | 修改与约束 |
| --- | --- |
| `backend/cmd/server/main.go` | 启动时先 `godotenv.Read()`，逐项用 `os.LookupEnv()` 保留已有环境变量，再补充缺失值；检查 `os.Setenv()` 错误，统一受控脱敏 |
| `backend/cmd/server/dotenv_test.go` | 增加 Windows 六项变量大小写混用与空值、文件空值、解析失败无部分写入、NUL 非法值及既有安全校验的回归用例 |
| `backend/internal/platform/config_test.go` | 测试夹具显式设置合法 HTTP 地址，避免数据库配置测试继承终端中的非法或空 HTTP_ADDR |
| `frontend/vite.config.ts` | 只保留原路径 `/api/` 代理，移除历史 `/health/` 规则；不增加 CORS、rewrite 或新代理目标 |
| `README.md`、`AGENTS.md`、`docs/architecture/ARCHITECTURE.md` | 同步当前代理说明；README 明确 Windows 优先级与加载失败行为 |
| `docs/api/API.md`、`docs/api/openapi.yaml` | 同步代理说明；OpenAPI 路径、操作、DTO 和状态码不变 |
| `docs/acceptance/T02_DOTENV_ACCEPTANCE.md`、`docs/acceptance/T02_DATABASE_CONNECTION_ACCEPTANCE.md` | 增加历史证据说明及当前修复记录指引，不改写历史实测结论 |
| 本记录 | 保存本轮实际证据与待验收边界 |

不新增或升级依赖。`go.mod`、`go.sum`、数据库配置及目标保护生产代码、秘密读取、连接池、Readiness Handler、集成测试安全入口和 MySQL 基础设施保持开始时内容。

## 输入、优先级及失败路径

仅启动入口读取当前工作目录的 `.env`，不搜索父目录或自动选择 `.env.test`；`LoadConfig()` 与集成测试入口不加载文件。优先级为：**已有环境变量（包括显式空值） > .env > 原有默认值**。Windows 的键名匹配交给 `os.LookupEnv()`，遵循系统不区分大小写的规则；其他系统沿用各自规则。

缺少文件时继续原有配置校验；读取或解析失败时不应用解析结果，返回受控提示。环境变量包含 NUL 等导致 `os.Setenv()` 拒绝设置时启动失败，错误不携带原始文件内容、键名或值。显式空值继续由原有配置校验拒绝，不静默退回默认值。

`DB_PASSWORD_FILE` 仍须为绝对路径，口令仍由独立秘密文件读取；APP_ENV、回环地址、库名/应用账户和测试固定目标保护保持原规则。Readiness 的 Ping、3 秒请求预算、200/503 JSON、取消传递、live 不访问数据库及退出清理保持原行为。

## 实际验证

Go 1.27.1 Windows amd64、Node.js 24.11.1、npm 11.6.2。Go 使用既有 `.cache/go-mod`、`.cache/go-build`，`GOTOOLCHAIN=local`、`GOPROXY=off`，未修改全局配置。默认完整测试显式清除 `SIYIGE_INTEGRATION`；不连接真实 MySQL。完整前后端测试在允许临时回环通信的本机执行环境运行。

| 验证 | 实际结果 |
| --- | --- |
| TDD RED：`go test -mod=readonly -count=1 -v ./cmd/server -run '^TestRunDotEnvConfiguration$'` | 原实现复现 Windows 大小写变量、六项小写空值被覆盖及非法变量设置失败未处理；退出 1 |
| 配置测试隔离 RED：继承非法 HTTP_ADDR，运行 `TestLoadDatabaseConfig` | 原夹具受终端变量影响，退出 1 |
| 上述配置测试 GREEN | dotenv 26 个子用例全部通过；继承非法 HTTP_ADDR 的数据库默认配置测试通过，均退出 0 |
| 后端 `go test -mod=readonly -count=1 -json ./...` | 3 个包通过；83 个 pass 事件（含父测试和子测试），2 个真实数据库集成项按默认入口跳过，0 失败 |
| 新健康契约回归 | 真实临时 HTTP 入口 live=200、数据库拒绝连接时 ready=503、取消后关闭监听；内存路由验证成功/失败 JSON、头、超时、取消、405 和旧路径 404 |
| `go vet -mod=readonly ./...`、`go build -mod=readonly ./...` | 均退出 0 |
| `go mod verify`、`gofmt -l cmd internal tests` | 模块全部通过校验；格式检查无输出 |
| 前端 `npm run typecheck`、`npm run test`、`npm run build` | 均退出 0；10 个测试通过，包括默认新路径的真实 HTTP 请求、失败重试及超时；生产构建成功 |
| OpenAPI 一致性检查 | OpenAPI 3.1.1 验证通过；200 个本地引用、28 个组件 Schema、21 个文档示例校验通过；API.md 与 OpenAPI 的 10 个操作一致（2 个健康、8 个规划业务），Gin、前端默认路径和 Vite 代理静态对齐 |
| 契约保留检查 | OpenAPI paths/components 与本轮开始时结构相同；API.md 原 JSON 示例未变，无健康旧路径条目 |
| 最终文件检查 | `git diff --check` 通过；57 个本地文档链接有效；13 个重点保护文件哈希与开始时一致，包括依赖锁定、数据库/秘密/健康生产代码、集成测试入口及前端在途请求实现 |

OpenAPI 检查复用已存在的 `.cache/api-docs/python-deps`，未安装依赖；脚本为本轮忽略目录下的 `validate_contract.py`，在仓库根目录执行。RED/GREEN、完整测试和静态检查日志保存在同一忽略目录。

## 未验证内容与验收边界

未连接真实 MySQL、切换数据库状态、读取真实秘密、执行迁移或种子、操作容器或部署。真实数据库恢复与当前已有账户的连通性不由本轮单元测试证明；已有数据库接入证据保留在历史记录。未新增浏览器人工验收记录，Vite 代理配置以静态一致性检查及前后端测试分别验证，不宣称执行了浏览器全链路验收。

截至本次修复结束时未执行 commit、push、merge 或分支切换，不将完整 T02 或人工验收标记完成。后续负责人已另行明确授权提交和推送当前分支；该授权不包含 PR、合并或后续开发。

本轮修改开始时已有的 11 个文件并新增本记录；其他在途内容保留。期间出现的其他新增学习文档不归属本轮交付，未读取或修改。

- 小范围独立复验：项目负责人于 2026 年 11 月 8 日明确决定不再执行，不补记为已通过。
- [ ] 项目负责人最终人工验收。
