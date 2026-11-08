# T02 后端数据库连接与 Readiness 验收记录

本文保留数据库接入时的历史证据，旧健康路径不作为当前调用地址。当前接口为 `GET /api/health/live`、`GET /api/health/ready`，依据 [ADR-012](../decisions/DECISIONS.md#adr-012-健康端点统一为当前-gin-路径)；本轮测试迁移及配置修复结果见 [配置与 API 契约修复记录](T02_CONFIG_FIX_ACCEPTANCE.md)。数据库安全校验、秘密管理和 Readiness 语义沿用本文实现。

实施跨 2026 年 11 月 7–8 日（Asia/Shanghai）。状态：当前子任务完成技术验证，待项目负责人最终验收及学习讲解；完整 T02 保持进行中。

## 范围与工作区

用户明确要求继续 T02，从最靠前的未完成工作做一个可独立验证闭环后停止。本轮复用已启动的隔离 MySQL，接入 GORM、配置、连接池、Readiness 和测试入口保护。版本化迁移、业务表、Goose、5 条种子与重启持久化为下一闭环，未进入 T03–T18。

开始时分支 `feat/t02-database`、HEAD `fe047c1`，存在 5 个已修改文档（AGENTS、ARCHITECTURE、DECISIONS、PLAN、TASKS）和 3 个未跟踪环境文件（compose.yaml、init-accounts.sh、mysql-client.sh），暂存区为空。原始字节/哈希快照仅保存在忽略的 `.cache/t02-backend/baseline.json`。5 份文档保留在途内容并增量说明最新授权/状态，仅 T02 表格状态文字更新；3 个环境文件不修改、不删除、不提交。两项已提交 P1 不重复处理。

未执行 commit、push、merge、部署、迁移、种子、业务 DDL/DML、容器停止/重启/重建、账户变更或数据删除。真实 MySQL 验证只连接隔离测试实例，开发实例只通过 Compose ps 检查状态。

## 输入、输出与失败路径

| 配置 | 行为 |
| --- | --- |
| HTTP_ADDR | 沿用回环 IP/有效端口限制，默认 127.0.0.1:8080 |
| APP_ENV | 未设置为 dev，只允许 dev/test；不支持生产配置 |
| DB_ADDR | 按环境默认 127.0.0.1:13306 或 :13307，仅允许回环 IP/有效端口 |
| DB_NAME / DB_USER | 按环境默认 siyige_dev/siyige_dev_app 或 siyige_test/siyige_test_app；只能为当前环境对应应用账户 |
| DB_PASSWORD_FILE | 必填绝对路径；文件最多 4096 字节，去除末尾 CR/LF 后非空；不把密码放入环境变量、DSN 或命令参数 |
| SIYIGE_INTEGRATION | 未设置时集成测试跳过；显式为 1 才启用，其他非空值拒绝 |

配置非法或秘密文件缺失/不可读/为空/超长：启动失败，错误字符串不含秘密路径、内容或原始驱动错误。配置有效但 MySQL 不可达：HTTP 进程仍运行，live=200，ready=503。GORM 和驱动不输出原始日志；保留 cause 供 errors.Is/As 判断取消等错误。

健康端点保持 [API](../api/API.md#就绪检查与-pb-错误) 冻结的路径和 JSON：`GET /health/live` → `200 {"status":"alive"}`；`GET /health/ready` → `200 {"status":"ready"}` 或 `503 {"status":"not_ready"}`。均无认证、无模型调用、包含 X-Request-ID 和 Cache-Control: no-store。ready 从请求 Context 派生最多 3 秒预算，只 Ping，不表示表结构、种子或业务可用。

## 核心实现与文件

- `backend/internal/platform/config.go`：HTTP/数据库配置校验，`ValidateTestTarget` 强制 APP_ENV=test、127.0.0.1:13307、siyige_test、siyige_test_app；不依赖 DSN 字符串匹配。
- `backend/internal/platform/database.go`：结构化驱动配置，秘密文件读取，utf8mb4、UTC session/解析/时钟，GORM 与 database/sql 连接池。打开上限 10、空闲 5、寿命 3 分钟、空闲寿命 1 分钟；连接/读/写超时 3 秒，业务后续仍须派生有界 Context。
- `backend/internal/platform/health.go`、`router.go`：注入最小 PingContext 接口；live 不碰数据库，ready 固定 JSON，数据库错误不回传。
- `backend/cmd/server/main.go`：只组装池，不在启动时联网、迁移或建表；HTTP 退出后关闭池，保留 PB HTTP 超时参数。
- `backend/internal/platform/*_test.go`、`backend/cmd/server/main_test.go`：配置/目标/秘密、取消/超时、健康/脱敏、真实 HTTP 入口与关闭测试。
- `backend/tests/integration/database_test.go`：TestMain 在读秘密/联网前校验固定目标；连接后核对 DATABASE()、CURRENT_USER()。仅 SELECT/Ping，不做清库等操作。
- `backend/go.mod`、`go.sum` 锁定新增依赖；README/API/5 份已有在途文档增量同步；[实施计划](../planning/2026-11-08-t02-database-connection.md) 保留步骤和执行记录。

## 实际验证

本机 Go 1.27.1 Windows amd64、PowerShell 7.6.9。只读 Compose ps 确认两个 mysql:8.4.13 容器健康且仅映射各自回环端口。Go 缓存使用项目 `.cache/go-mod`、`.cache/go-build`。以下 Go 命令在 backend 目录执行。

1. TDD RED：新增测试后 `go test ./internal/platform` 失败，明确缺失 DatabaseConfig、OpenDatabase、DatabasePinger 和路由依赖注入；再实现对应能力。首次编译发现 HTTP 与数据库端口变量重名，改为 dbPort 后复验。
2. `go test ./...` 通过。覆盖配置/非法值、测试目标白名单、秘密异常与错误脱敏、取消和连接池关闭、3 秒真实 MySQL 握手停滞、健康/方法/错误契约、HTTP 入口在 DB 拒绝连接时仍运行及取消后退出；默认真实 MySQL 集成项跳过。
3. 显式测试环境下 `go test -count=1 -v ./tests/integration` 两项通过：TestMySQLConnectionAndSettings、TestHTTPReadinessTracksRealDatabase。GORM 实际查询验证版本 8.4.13、当前库/账户、UTC datetime、session +00:00、utf8mb4/utf8mb4_0900_ai_ci 和中文/Emoji。
4. 真实 HTTP 验证：连接可用 ready=200；只关闭本测试创建的池后 ready=503、live=200；创建新的连接和 HTTP 测试服务后 ready=200。不将该证据描述成已做容器故障/重启验证。
5. 用 `go test -c` 生成忽略目录内的集成测试程序，秘密路径故意指向不存在文件。逐项设置 APP_ENV=dev、DB_ADDR=127.0.0.1:13306、DB_ADDR=localhost:13307、DB_NAME=siyige_dev、DB_USER=siyige_test_migrator、DB_USER=root、SIYIGE_INTEGRATION=true；7 项均退出 1，返回配置/目标拒绝，未到秘密读取/数据库连接路径。
6. `go vet ./...`、`go build ./...`、`go mod verify` 通过；`gofmt -l cmd internal tests` 无输出。

运行命令见 [README](../../README.md#测试与构建)。集成测试需要显式 APP_ENV=test、SIYIGE_INTEGRATION=1 及测试应用秘密文件，任意配置不符均失败。

### 环境限制与处理

受限沙箱内 Docker API 和回环网络被阻断，最初单元/集成连接出现假故障；在获准的本机执行环境复验通过。沙箱内 goproxy.cn DNS 不可用；官方 proxy.golang.org 在本机也连接超时，最终使用本机原有 goproxy.cn 下载，Go 模块校验通过。未更改全局 Go 代理。

GORM 的 go mod tidy 会下载其上游测试引用的 SQLite 模块；本项目未导入或运行 SQLite，真实持久化验证使用 MySQL。首次网页抓取驱动许可证失败后，从实际下载的 v1.6.0 模块 `License` 核对为 MIT。

### 依赖依据

GORM v1.31.2 与 GORM MySQL v1.6.0 均为冻结的成熟方案、MIT；go-sql-driver/mysql v1.8.1 为驱动声明依赖、MPL-2.0。本轮直接使用其 Config/NewConnector 避免拼接秘密 DSN。替代方案是手写 database/sql 映射；当前沿用 ADR-004，未另换技术栈。直接及传递版本以 go.mod/go.sum 为准。

已查官方 [GORM 连接文档](https://gorm.io/docs/connecting_to_the_database.html)、[GORM 配置](https://gorm.io/docs/gorm_config.html)、[固定 MySQL 驱动源码](https://github.com/go-gorm/mysql/tree/v1.6.0)、[go-sql-driver 配置/Context](https://github.com/go-sql-driver/mysql/tree/v1.8.1)，并核对下载模块的源码与许可证。

新增传递依赖为 GORM/驱动既有依赖图的一部分，版本和许可证已从下载模块核对；不另引入替代实现：

| 依赖 | 用途与许可证 | 替代方案 |
| --- | --- | --- |
| filippo.io/edwards25519 v1.1.0 | 成熟 Go Edwards25519 实现，驱动认证支持依赖；BSD-3-Clause | 改写驱动认证路径会增加维护风险，不采用 |
| jinzhu/inflection v1.0.0 | GORM 成熟表名词形转换工具；MIT | 自写命名转换会偏离 GORM 默认，不采用 |
| jinzhu/now v1.1.5 | GORM 既有日期工具依赖；MIT | 替换 ORM 时间工具无本任务收益，不采用 |

## 审查与人工门槛

Codex 独立只读审查完成，未发现阻塞性的安全或正确性问题；核对了延迟连接、3 秒 Context、live 独立性、入口/实际身份双重目标保护、错误脱敏与退出清理。审查未重复运行测试，使用本记录的实际运行证据。无延期小问题。恢复测试的证据边界已按审查意见明确。

最终文件检查确认原 5 份在途文档的内容按顺序保留（仅 T02 表格状态更新），3 份未跟踪环境文件与开始时字节完全一致；78 个本地文件链接有效，25 个待审查文件未包含本地六份真实秘密的值，`git diff --check` 通过。Git 有既有行尾转换提示，不是内容检查失败。暂存区为空、分支及 HEAD 未变。

尚未执行前端回归、Nginx/公网验证、业务迁移、种子、模型测试、真实容器断开/重启、同一 HTTP 服务随真实 DB 故障自动恢复或业务表持久化验证；不据此认定完整 T02 完成。

项目负责人验收与学习讲解尚未发生，不代记通过。下一项建议为 T02 版本化初始迁移及重复执行保护，再按独立可验收范围加入 5 条中文辩题种子。当前停止，等待人工验收。
