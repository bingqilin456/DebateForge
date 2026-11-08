# T02 dotenv 配置优化验收记录

本文保留首次实现的历史证据，包括当时的 `godotenv.Load()`、旧健康路径及测试结果。当前加载已改为 `godotenv.Read()` 配合 `os.LookupEnv()`；健康路径以 [ADR-012](../decisions/DECISIONS.md#adr-012-健康端点统一为当前-gin-路径) 为准。本轮修复与实际复验结果见 [配置与 API 契约修复记录](T02_CONFIG_FIX_ACCEPTANCE.md)，不沿用下方历史结果代替本轮验收。

日期：2026 年 11 月 8 日（Asia/Shanghai）。状态：本次配置优化已完成实施与技术自查，待独立审查及项目负责人最终验收；完整 T02 仍进行中，不推进迁移、种子或后续任务。

## 范围与实现

本次在已有 `feat/t02-database` 分支上增量修改，保留开始时全部未提交工作。修改前的文件哈希、相关文件副本与 Git 状态保存在忽略目录 `.cache/t02-dotenv/`，用于区分本次变更和已有 T02 改动。

| 文件 | 本次改动 |
| --- | --- |
| `backend/cmd/server/main.go` | 在现有 `LoadConfig()` 前调用 `godotenv.Load()`；仅忽略文件不存在错误，其他加载错误返回不含文件原文的受控提示 |
| `backend/cmd/server/dotenv_test.go` | 测试加载顺序、全部配置的系统变量优先级、显式空值、缺少文件、原有环境限制、解析失败脱敏及文件读取失败 |
| `backend/cmd/server/main_test.go` | 增加临时工作目录，隔离开发者 `.env`，保留原有真实 HTTP 生命周期与数据库不可达测试 |
| `backend/go.mod`、`backend/go.sum` | 只增加直接依赖 `github.com/joho/godotenv v1.5.1` 及其两条校验和，未升级其他依赖 |
| `backend/.env.example` | 提供六项开发配置；密码文件路径使用待替换占位符，无口令或个人路径 |
| `README.md` | 更新首次配置、日常启动、优先级、CI、切换集成测试环境及依赖说明 |
| 本验收记录 | 记录范围、验证与待验收项 |
| `backend/.env`（本地忽略） | 按现有开发目标生成，`DB_PASSWORD_FILE` 引用已有 `dev-app.txt` 的绝对路径，不复制密码内容 |

沿用现有 `.gitignore`：`.env` 被忽略，`**/.env.example` 允许跟踪，无需修改规则。原有配置校验、数据库秘密读取、连接池、Readiness、集成测试 `TestMain` 与防误连代码均未修改。

依赖选型依据：[godotenv v1.5.1 官方说明](https://github.com/joho/godotenv/tree/v1.5.1)及该版本源码、许可证。用途是替代重复的手动环境变量设置；使用成熟的 Go dotenv 库，MIT 许可证。不采用 `Overload()` 或自动导入 `autoload`；不自写 dotenv 解析器。

## 输入、优先级与失败路径

- 输入仅为启动时当前工作目录中的 `.env`，日常命令在 `backend/` 执行。不搜索父目录，不加载 `.env.test`，不按 APP_ENV 自动选择文件。
- 已存在的进程环境变量（包括显式空字符串）优先于 `.env`；两处均未提供时才由原有 `LoadConfig()` 使用默认值。显式空值仍会被原有配置校验拒绝。
- 缺少 `.env` 时继续加载原有配置；不豁免必填密码文件、回环地址、环境与账户匹配等校验。CI 和集成测试继续显式配置环境，不自动读取开发 `.env`。
- 文件存在但不可读或格式错误时启动失败，日志仅提示检查权限与格式，避免 godotenv 原始解析错误泄露文件内容。
- `.env` 中不保存实际密码、DSN 或模型密钥。`DB_PASSWORD_FILE` 仍须为绝对路径，口令读取限制与脱敏行为沿用原有实现。

## 实际验证

本机 Go 1.27.1 Windows amd64。Go 模块和构建缓存沿用项目 `.cache/go-mod`、`.cache/go-build`，未修改全局 Go 配置。以下命令在 `backend/` 执行。

| 验证 | 实际结果 |
| --- | --- |
| TDD RED：新增测试后运行 `go test -count=1 ./cmd/server` | 加载文件、保留数据库环境限制、格式错误与读取错误用例按预期失败，原因是启动入口尚未加载 `.env` |
| `go test -count=1 ./...`（本机执行环境） | 全部通过；默认 MySQL 集成测试跳过，不连接真实数据库 |
| `TestRunDotEnvConfiguration` 的 7 个子用例 | 全部通过；覆盖加载顺序、六项系统变量优先级、空值不覆盖、文件缺失、APP_ENV 限制、格式错误脱敏及读取失败 |
| 原有 HTTP 生命周期测试 | 在无 `.env` 的临时工作目录成功启动；live=200，数据库不可达 ready=503，取消后关闭监听 |
| `go vet ./...`、`go build ./...` | 均退出 0 |
| `go mod verify` | 退出 0，输出 `all modules verified` |
| `gofmt -l cmd internal tests` | 无输出 |
| `go run ./cmd/server`（本机执行环境） | 清除启动子进程六项配置环境变量后，仅靠本地 `.env` 成功监听 `127.0.0.1:8080` |
| 实际 `GET /health/live` | HTTP 200，`{"status":"alive"}`，`Cache-Control: no-store`，存在 X-Request-ID |
| 实际 `GET /health/ready` | HTTP 200，`{"status":"ready"}`，`Cache-Control: no-store`，存在 X-Request-ID；仅 Ping 现有开发库 |
| 冒烟进程退出 | 发送 Ctrl+C 后记录“HTTP 服务已停止”；`go run` 包装进程因中断返回退出码 1 |
| 最终文件检查 | `git diff --check` 通过；所有范围外既有文件 SHA-256 与开始时一致；仅新增预期的 3 个可跟踪文件；本地 `.env` 忽略且只含六项配置，秘密路径为已有绝对路径；示例无个人路径或密码字段，本次文档本地链接有效 |

受限沙箱内原有 HTTP 生命周期测试出现无法访问临时回环 HTTP 服务的失败；单独重跑同样失败。在允许回环访问的本机执行环境运行全套后通过，与既有 T02 验收记录中的沙箱限制一致。未将沙箱内该次失败记为测试通过。

## 技术自查与验收边界

实现只在服务启动入口修改进程环境；通用 `LoadConfig()` 及测试入口保持原行为。检查了加载错误的脱敏、显式空值优先级、缺少文件时必填项仍受校验，以及默认测试不会读取本地开发 `.env`。没有执行容器停止、重启、重建、账户变更、数据写入、迁移或种子；未执行 commit、push 或部署。

最终分支仍为 `feat/t02-database`、HEAD 仍为 `fe047c1`，暂存区为空。Git 提示既有 LF/CRLF 转换设置，不影响差异内容检查。

注意事项：启动目录必须为 `backend/`；换机器时必须从示例重新配置本机秘密路径；终端遗留变量会优先于 `.env`，切换环境应同步全部 DB 配置或清除覆盖值。实际冒烟只验证当前健康数据库；没有为本次优化切换真实 MySQL 状态。数据库不可达及失败脱敏由原有自动化测试回归覆盖。

- [ ] 独立审查本次增量及环境变量加载边界。
- [ ] 项目负责人最终人工验收。

本次完成后停止，未将完整 T02 或上述待验收项标为完成。
