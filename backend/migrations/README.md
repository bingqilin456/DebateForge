# T02 版本化数据库迁移

当前共十个版本：六张表、三份精确枚举 CHECK 修复、一份五条辩题种子。未实现业务 Repository/Handler，总结表留 T10。逻辑实体和 API 依据 [ARCHITECTURE](../../docs/architecture/ARCHITECTURE.md#数据实体与关系)、[API](../../docs/api/API.md)；最新证据见 [种子与持久化记录](../../docs/acceptance/T02_SEEDS_PERSISTENCE_ACCEPTANCE.md)，初始六表历史见 [迁移验收记录](../../docs/acceptance/T02_MIGRATIONS_ACCEPTANCE.md)。

2026 年 11 月 8 日最终独立技术审查及项目负责人人工验收通过，完整 T02 已结项。00007–00010、测试及重启持久化验证已验收，M01/M02 关闭；M03 受控诊断及已接受的验证/恢复边界继续延期，详见 [风险登记](../../docs/planning/TASKS.md#t02-迁移延期问题登记)。原六份 SQL 保持不变；负责人仅授权本轮提交/推送，不创建 PR、不合并或推进后续开发。

## 入口与执行顺序

独立 CLI 位于 `backend/tools/migrate/`，使用 Goose **v3.29.0 Provider**，与标准 Goose 共用 `goose_db_version` 格式。工具有独立 `go.mod/go.sum`，不进入服务依赖图，也不在服务启动时执行。T07 增加显式容器部署目标，执行前须取得目标库授权，见 [T07 部署清单](../../deploy/README.md#2-显式受审迁移)。

仅接受 `up`、`status`。必须显式传 `-env dev|test|deploy`、迁移账户秘密文件的绝对路径；地址、库名和账户按环境固定，不接受 DSN/root/应用账户。入口不加载 `.env`，不采用 `APP_ENV`、`DB_*`、`GOOSE_*` 的目标或口令。`-dir` 只接收已审阅的版本化 SQL 目录。

| 环境 | 固定地址/库 | 固定账户 | 本地秘密文件 |
| --- | --- | --- | --- |
| test | `127.0.0.1:13307/siyige_test` | `siyige_test_migrator@%` | `.cache/t02-mysql/secrets/test-migrator.txt` |
| dev | `127.0.0.1:13306/siyige_dev` | `siyige_dev_migrator@%` | `.cache/t02-mysql/secrets/dev-migrator.txt` |
| deploy | `mysql:3306/siyige_deploy` | `siyige_deploy_migrator@%` | 容器的 `/run/secrets/migrator_password` |

dev/test 迁移账户权限见 [现有隔离环境](../../infra/mysql/README.md)，deploy 权限见 [部署初始化脚本](../../deploy/init-accounts.sh)。部署应用账户在迁移完成后显式取得业务表权限，范围见 [授权 SQL](../../deploy/grant-app.sql)。先校验配置再读取秘密；口令只在进程内组装连接，不进入命令参数、环境变量、输出或仓库。连接后在 **同一个 SQL 会话** 验证实际数据库、`CURRENT_USER()` 及 MySQL `8.4.13`，再执行 `GET_LOCK('siyige:migrate:<库名>', 0)`，冲突立即失败。每个版本依次完成历史完整性检查、Goose 元数据初始化、DDL 和成功登记，最后解锁；不同进程可在版本边界交替执行，但同一版本不会并行执行。

工具逐版本调用 `ApplyVersion(..., true)`，已执行版本作为空操作跳过。未调用会在加锁前初始化元数据的 `Up/HasPending` 路径。历史只接受完整的前向版本前缀；缺失本地已执行文件、历史间隙、重复记录和非前向记录均拒绝。未知数据库版本不能被低版本工具静默忽略。

`status` 显示版本号、pending/applied、文件名和 UTC 成功时间。**首次 status 也会由 Goose 创建版本表和版本 0**，不是纯只读操作，但不会建业务表。`up` 输出本次实际执行数；初始六表历史首次为 `applied=6`，本轮从该基线升级为 `applied=4`，当前版本重跑为 `applied=0`。CLI 总预算 60 秒，建连 3 秒，单次读写 10 秒，解锁 3 秒；超时不等于 MySQL DDL 一定未发生。

## 使用

保持现有测试容器运行。从仓库根目录用 PowerShell 执行：

```powershell
$migrationSecret = (Resolve-Path '.cache/t02-mysql/secrets/test-migrator.txt').Path
Push-Location backend/tools/migrate
go mod download
go run . -env test -password-file $migrationSecret -dir ../../migrations status
go run . -env test -password-file $migrationSecret -dir ../../migrations up
go run . -env test -password-file $migrationSecret -dir ../../migrations status
Pop-Location
```

每条命令确认退出码为 0 再继续；失败先按下文核对，不连续重试掩盖问题。也可在工具目录构建 `go build -o ../../../.cache/t02-mysql/migrate.exe .` 后调用二进制。编译后的默认 SQL 目录仍相对当前工作目录，换目录执行时必须提供正确的 `-dir`。

开发库需要明确使用 `-env dev` 和 `dev-migrator.txt`，不得把测试命令改成生产地址。本轮只对测试库执行迁移，未改变开发库。

## 版本与物理结构

冻结设计规定了字段、关系和约束；以下把未指定的物理类型具体化，没有增加实体、HTTP 字段或状态。所有业务主键和外键为 `BIGINT UNSIGNED`，主键自增，时间 `DATETIME(6)`；连接设置 UTC，默认创建时间 `CURRENT_TIMESTAMP(6)`。表均为 InnoDB、utf8mb4/utf8mb4_0900_ai_ci。外键一律 RESTRICT，不级联删除已有会话或消息。

| 版本/表 | 主要字段与约束 |
| --- | --- |
| 00001 users | `username VARCHAR(32)` ASCII、不区分大小写唯一；应用在 T03 trim/小写归一化。`password_hash VARCHAR(255)` 保存 Argon2id 编码，`created_at`。 |
| 00002 auth_sessions | `user_id` 外键；SHA-256 `token_hash BINARY(32)` 保存原始摘要字节并唯一；`csrf_token VARCHAR(128)` ASCII；`expires_at` 索引、`created_at`。不存 Session 原值。 |
| 00003 topics | `title/description TEXT`、`enabled BOOLEAN` 默认 true 并 CHECK 0/1、`created_at`。初始版本不含种子，00010 插入五题。 |
| 00004 debates | 用户/辩题外键；标题/描述快照 TEXT；pro/con、active/completed 使用区分大小写的 ASCII 字符串和 CHECK；完成轮数 0–3；创建/更新时间；索引 `(user_id,created_at,id)`。 |
| 00005 debate_turns | 会话外键；轮号 1–3；规范 UUID `client_request_id CHAR(36)`；两组唯一键 `(debate_id,round_no)`、`(debate_id,client_request_id)`；三种状态 CHECK；版本 `BIGINT UNSIGNED` 默认 1 且 >=1；可空租约、受控错误/模型/Prompt 元数据及创建时间；索引 `(status,lease_expires_at)`。 |
| 00006 messages | 轮次外键；user/assistant CHECK；`(turn_id,role)` 唯一；`content TEXT`、创建时间。 |
| 00011 debate_summaries | 会话唯一外键；三种状态及字节精确 CHECK；可空 `schema_version` 与 `content_json TEXT`；版本 `BIGINT UNSIGNED` 默认 1 且 >=1；可空租约、受控错误/模型/Prompt 元数据及创建/更新时间；索引 `(status,lease_expires_at)`。 |

生成元数据：`error_code/prompt_version VARCHAR(64)` ASCII，`model_name VARCHAR(255)` utf8mb4，token 用量和耗时为可空 `INT UNSIGNED`。未知用量为 NULL，不能记成 0。2000 码点、UUID 规范化、用户名规则、状态转换、所有权及密码哈希仍由后续业务任务实现；数据库检查不能替代这些规则。`updated_at` 由后续业务显式维护，无隐式 ON UPDATE。文本使用 TEXT，避免自行引入短标题上限；后续新增输入边界须同步契约。

## 新增版本与种子

| 版本 | 增量 |
| --- | --- |
| 00007 | debates 的 pro/con 和 active/completed 字节精确 CHECK |
| 00008 | debate_turns 的 generating/completed/failed 字节精确 CHECK |
| 00009 | messages 的 user/assistant 字节精确 CHECK |
| 00010 | topics 固定 ID 1–5，完整中文标题/双边立场描述，enabled=true |

00007–00009 每份仅一条 ALTER，保留原约束、列类型和状态集合；校验已有行，异常时失败且不改数据。CAST AS BINARY 可拒绝字段宽度内的尾随空格；MySQL 对超长尾随空格先截断，`assistant ` 存为规范 `assistant`，不能宣称 CHECK 拒绝所有原始请求，后续业务入口必须精确校验。

00010 的幂等由历史和会话锁保证：重复 **up** 不再 INSERT，不是可绕过 Goose 手工重放的 SQL。DML 与版本登记在同一事务内；固定 ID 被占用时整笔失败，不覆盖数据或登记成功，无 IGNORE/REPLACE/UPSERT。包含五条题目的闭环已获负责人人工验收，内容见最新验收记录；未来修改须新增迁移。

本轮从已有 1–6 的测试库实际新增 `applied=4`，重跑 `applied=0`；当前空库预期 `applied=10`，本轮未新建空库重测。可在根目录运行 `pwsh -NoProfile -File infra/mysql/verify-t02-persistence.ps1`，仅重启测试 MySQL 并核对版本、六表和五条种子；详见最新验收记录。

新增集成测试覆盖异常历史在 up/status 下拒绝、合法前缀继续、既有异常枚举阻止 ALTER 和种子冲突回滚。独立探针表保留且每次运行增加，不改正式历史，不清理旧数据。默认仍跳过真实数据库测试。

## 失败、重跑与恢复

MySQL 8.4 InnoDB 支持单条原子 DDL，但 **DDL 与 Goose 的版本 INSERT 不构成一个原子事务**。因此每表一个文件，显式 `NO TRANSACTION`，不宣称六表整体回滚。一个版本失败时停止，之前成功的版本保留；成功记录含版本和时间。未成功的版本不登记，错误只输出失败版本、MySQL 数字错误码及受控提示，不输出原始 SQL/DSN/驱动消息。

迁移没有 Down、DROP、TRUNCATE、`IF NOT EXISTS` 或自动修复入口。已执行文件保持不可变，后续用新版本前向升级；Goose 原生版本表不存 SQL 校验和，审查时仍须核对源文件与实际结构。

1. 锁冲突：等待当前执行者结束，再查看 status；不会排队或自动重调。
2. DDL 尚未发生的连接/权限/语法失败：先查状态、实际表结构并修复原因；确认目标正确后重跑，已成功版本跳过。
3. DDL 已完成但登记未完成（断线/进程终止等）：版本可能 pending 而表已存在。重跑会因表已存在失败，**不会用 IF NOT EXISTS 冒充成功**。只读核对 `SHOW CREATE TABLE <表>`、`SELECT version_id,is_applied,tstamp FROM goose_db_version ORDER BY id`、迁移源与保留数据；停止写入，提交审查后的恢复方案。补记成功版本或其他数据修复必须另获批准，本工具不提供 force/mark-applied，不自动处理。
4. 历史损坏或本地缺少已执行文件：恢复正确的文件/记录来源并独立审查，不能删除版本行、删表或重建卷重来。

## 测试

工具是独立 module，**服务目录的 `go test ./...` 不会涵盖它**。在工具目录执行：

```powershell
go test -count=1 -v ./...
go vet ./...
go build -o ../../../.cache/t02-mysql/migrate.exe .
go mod verify
gofmt -l .
```

默认单测不会读取真实秘密或连接 MySQL。显式写入集成测试也从工具目录执行：

```powershell
$env:SIYIGE_MIGRATION_TEST = '1'
$env:SIYIGE_MIGRATION_PASSWORD_FILE = (Resolve-Path '../../../.cache/t02-mysql/secrets/test-migrator.txt').Path
go test -count=1 -v .
Remove-Item Env:SIYIGE_MIGRATION_TEST, Env:SIYIGE_MIGRATION_PASSWORD_FILE
```

测试固定 `127.0.0.1:13307/siyige_test`，不能通过服务的开发环境变量重定向。业务约束夹具在事务中回滚，但自增序列可能留空洞。失败恢复夹具使用独立随机 `t02_failure_versions_<后缀>` 与 `t02_failure_probe_<后缀>` 表，模拟部分 DDL、停止/拒绝静默重跑、审查后的修复与再次重跑；**证据表保留且每次该测试会增加一对**。恢复时仅修改内存测试夹具，不修改已发布正式迁移，不增删既有业务数据；后续清理证据表需单独授权。并发用两条真实连接验证会话锁，释放后可以继续；另验证已执行信号与解锁错误合并时必须失败。可在显式集成环境下执行 `go test -race -count=1 -v .`，需可用 C 编译器。

## 依赖依据

| 依赖 | 用途、成熟度 | 许可证 | 替代方案 |
| --- | --- | --- | --- |
| Goose v3.29.0 | 项目已选定的成熟版本化 SQL 工具；Provider API 用于独立 CLI | MIT | 标准 Goose CLI 也能迁移，但本项目需在同一会话执行目标校验和锁保护；手工登记易漏记，不另建版本引擎。 |
| go-sql-driver/mysql v1.10.1 | Goose module 要求的驱动版本，仅工具 module 使用；服务仍为 v1.8.1 | MPL-2.0 | 不替换服务驱动，也不引入其他数据库驱动运行时。 |

Goose 根库运行时只引入其所需的少量通用包。`go mod tidy` 还会下载 Goose 自身测试使用的 SQLite 等元数据，本工具未导入 SQLite，不把它用于项目测试。完整锁定见工具 `go.mod/go.sum`。

官方依据：[Goose v3.29.0](https://github.com/pressly/goose/tree/v3.29.0)、[Provider 与选项](https://github.com/pressly/goose/blob/v3.29.0/provider_options.go)、[SQL 注解](https://pressly.github.io/goose/documentation/annotations/)、[MySQL 原子 DDL 与隐式提交](https://dev.mysql.com/doc/refman/8.4/en/atomic-ddl.html)、[MySQL 命名锁](https://dev.mysql.com/doc/refman/8.4/en/locking-functions.html)、[驱动 Charset 选项](https://github.com/go-sql-driver/mysql/blob/v1.10.1/dsn.go)。
