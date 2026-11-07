# T02 后端数据库连接实施计划

> 执行方式：本会话内按 TDD 实施一个闭环，遵守用户禁止 commit、push、merge、删除数据和重建容器的约束。完成技术验证后停止，等待人工验收。

**目标：** 连接已有隔离 MySQL，为进程注入 GORM 和有界连接池，实现冻结的 Readiness 契约及集成测试目标保护。

**架构：** 保持 platform 的配置/数据库/健康职责，由 cmd/server 组装和关闭资源。数据库池延迟连接，故障不阻止 live；ready 使用请求 Context 和最多 3 秒的 PingContext。没有自动迁移、业务查询或模型调用。

**技术栈：** Go 1.27.1、现有 Gin、GORM v1.31.2、gorm.io/driver/mysql v1.6.0、该驱动声明的 go-sql-driver/mysql v1.8.1。

**依据：** [ARCHITECTURE](../../ARCHITECTURE.md)、[API](../../API.md#就绪检查与-pb-错误)、[TASKS](../../TASKS.md)。现有冻结设计与本轮明确开发授权作为实施依据，不新增架构或接口决策。

## 全局约束

- 保留开始前 5 份文档的已有修改、3 个未跟踪环境文件；原始内容与哈希快照仅存于 Git 忽略的 `.cache/t02-backend/baseline.json`。
- 只使用已有回环 MySQL 环境及应用账户，秘密只从文件读取，不接受任意 DSN 参数。
- 集成测试必须显式启用；连接前校验 APP_ENV=test、127.0.0.1:13307、siyige_test、siyige_test_app。
- UTC、utf8mb4、单次数据库预算 3 秒；日志与错误不包含密码、DSN、原始 SQL/驱动错误。
- 版本化迁移、种子、表结构、Goose、T03 及后续任务均不在本闭环内。

## 审查重点

- 缺失/空/不可读秘密文件必须受控失败，不能在报错时暴露路径或内容。
- 数据库拒绝连接、握手停滞、请求取消时 ready=503，live=200，健康响应不缓存。
- 开发、迁移、root、非白名单地址必须在集成测试读取秘密及联网前被拒绝。
- GORM 与底层驱动不自动打印敏感错误，不使用启动 Ping/AutoMigrate。
- 退出时关闭 HTTP 和数据库资源；ready 可在依赖恢复后自行恢复。

## 单个开发闭环

**文件：** 修改 `backend/internal/platform/{config,router,health}.go`、入口和相关测试；新增 `database.go`、数据库测试、`backend/tests/integration/database_test.go`；新增验收记录，增量同步 README/API/任务状态，保留历史记录。

**接口：** `DatabaseConfig.ValidateTestTarget() error` 在联网前校验；`OpenDatabase(DatabaseConfig) (*Database, error)` 只组装连接池；Database 提供 `ORM *gorm.DB`、`PingContext(context.Context) error` 和 `Close() error`。`NewRouter(logger, checker)` 接受最小 PingContext 接口。

- [x] 编写配置、目标保护和健康行为测试，运行并记录 RED。
- [x] 实现必要配置、依赖和连接生命周期，测试 GREEN。
- [x] 编写真实 MySQL 只读集成测试，验证 GORM、UTC/字符集、健康和新建连接恢复；用失效 TCP 连接验证短超时，不操作容器或数据。
- [x] 验证 server 真实 HTTP 入口、go test/vet/build、格式与变更范围。
- [x] Codex 技术审查，登记实际结果和未执行项；T02 保持进行中，当前子任务待人工验收。

## 执行记录

- 初始分支 feat/t02-database，HEAD fe047c1，暂存区为空。两个 mysql:8.4.13 容器健康；只读 Docker 检查需沙箱提升后成功。
- 本轮明确授权替代此前“仅环境子任务”的历史限制；在文档中以增量说明登记，不覆盖原始在途修改。
- Ruling: 依用户指令在现有 feat/t02-database 原位实施，保留在途文件；不建分支/工作树，不提交，不删除计划工作目录。用户明确要求直接推进既有冻结设计，技能中的重复审批/提交模板不构成本轮额外门槛。
- RED→GREEN：新增能力缺失导致编译失败；实现后修正端口变量重名；go test ./... 在允许回环连接的环境通过。真实测试库的两项只读集成测试通过；7 项入口防误连验证通过。
- go vet/build、模块校验和 gofmt 通过；原始在途文档内容保留（仅 T02 表格状态增量修改），3 份环境文件字节不变，暂存区为空、HEAD 未变。本地文档 78 个文件链接及 25 个待审查文件的秘密值泄漏检查通过。
- Final review: 独立只读审查完成，未发现阻塞性安全/正确性问题；无待修复或延期的小问题。恢复证据仅为关闭池后创建新池及新 HTTP 测试服务，未验证同一服务自动恢复或真实容器停启，已明确记录。
- 完整命令、失败恢复、证据及未执行项见 [接入验收记录](../../T02_DATABASE_CONNECTION_ACCEPTANCE.md)。人工验收未发生，当前停止。
