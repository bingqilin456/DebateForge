# backend/ 局部规则

本文件是 `backend/` 的局部规则，只补充本目录特有的实现与检查约定；项目边界、安全红线、全局契约不变量、授权与验收规则仍以根 [AGENTS.md](../AGENTS.md) 为准。进入本目录前先读根 AGENTS，再读本文件。

## 分层与职责

- Gin + Eino + GORM + MySQL 模块化单体，按 `auth`、`topic`、`debate`、`ai`、`platform` 划分；只创建当前任务需要的文件。
- Handler 负责协议与参数校验，**Service 编排规则、状态机与事务边界**，Repository 负责持久化及事务内的一致性校验（如轮次状态、`attempt_version`、归属条件）。禁止 Handler 直接拼装 Prompt 或执行 SQL；模型调用在短事务外完成。
- `ai` 不持有数据库访问权限；只为模型替换与测试建立小接口，不预先抽象成万能 CRUD 框架。
- 向下传递 `context.Context`；错误保留上下文并在 HTTP 边界映射业务码，不回传堆栈、DSN 或供应商原始响应。不随意增加抽象层或 goroutine。

## 必须保持的不变量

- `debates.mode` 是轮数上限的唯一事实来源：轮次上限必须从会话行读取 `mode`，**不得硬编码轮数或绕过 `mode` 判断**；V1 生产创建仍默认 `standard_4`（按根规则，`quick_3` 仅历史兼容与隔离测试）。
- 普通发送与流式提交共用同一认领内核（`client_request_id` 幂等、`attempt_version`、90 秒租约、并发配额）；**不另建第二套独立认领/幂等内核**。
- 正式完成结果与 `done` 只在最终事务提交成功后返回或发送；提交失败只报错并按状态查询恢复。合法的 `202`、幂等重放与状态查询响应不受此限制。
- 不持有数据库事务或行锁等待模型响应，也不在锁内排队等并发槽；所有退出路径都要释放已取得的并发槽。
- 已执行的 SQL 迁移不可修改，修正一律新增前向迁移；服务启动不自动迁移或播种。

## 检查命令（在 `backend/` 下执行）

```powershell
go mod verify
go vet ./...
go test ./...
go build ./...
gofmt -l cmd internal tests
```

- 涉及并发、限额、状态争抢、租约扫描或版本竞争的改动，显式加跑 `go test -race ./...`；不涉及则不必全量复跑。
- 涉及迁移工具的改动，在**独立模块** `backend/tools/migrate/` 单独执行 `go mod verify`、`go vet ./...`、`go test ./...`、`go build ./...`；服务目录的 `go test ./...` 不覆盖该模块。
- 隔离 MySQL 集成测试仅在显式设置 `SIYIGE_INTEGRATION=1` 且 `APP_ENV=test` 时运行，默认跳过。固定目标为 `127.0.0.1:13307/siyige_test`、账户 `siyige_test_app`；入口在读取秘密和联网前校验固定目标，连接后核对实际库与账户，任何不符即失败。只使用该隔离库，不操作共享或 demo 库。
- 普通无收费回归前清除 `SIYIGE_INTEGRATION`、`SIYIGE_MIGRATION_TEST`，并设置 `AI_MODE=disabled`；需要模型响应的无收费联调用 `AI_MODE=fake`。真实模型只在另行授权的受控调用中使用。

## 迁移与集成测试

- 迁移编号、固定目标保护与失败恢复详见 [迁移说明](migrations/README.md)；备份、恢复与部署须先在隔离环境验证。
- 迁移工具是独立 module，有自己的依赖与锁文件，不把 Goose 引入服务 module。
- 业务夹具只清理自己创建的匿名记录；迁移失败/边界证据表等按既有说明保留，不自行扩大清理范围。

## 授权

本目录的读写仍受根 [AGENTS 的操作授权边界](../AGENTS.md#操作授权边界) 约束：提交、推送、迁移、数据库写入与真实模型调用均须当轮明确授权。
