# T02 版本化迁移 Implementation Plan

> **For agentic workers:** 本轮由当前会话按步骤执行，用户要求完成后等待独立审查；不创建提交、推送、合并或部署。

**Goal:** 在固定隔离测试库验证六张初始表、Goose 版本记录、重复执行保护与失败处置。

**Architecture:** 复用 ADR-004 的 SQL 迁移与独立 Goose v3.29.0 入口。迁移工具使用独立 Go module，避免迁移依赖升级服务端已锁定依赖；仅提供 up/status，以迁移账户连接固定本地目标，服务不自动迁移。每表一个非事务 DDL 版本，不提供删除表的 Down。

**Tech Stack:** Go 1.27.1、Goose v3.29.0、MySQL 8.4.13。

**Spec:** [ARCHITECTURE](../../ARCHITECTURE.md#数据实体与关系)、[ADR-004](../../DECISIONS.md#adr-004-mysql-与-gorm-并使用版本化迁移) 及本轮用户授权。逻辑实体、字段、关系不变；物理类型选择记入迁移 README。

## Global Constraints

- 六表为 users/auth_sessions/topics/debates/debate_turns/messages；总结表留 T10，五条种子留下一授权子任务。
- 仅固定 dev/test 回环目标，先校验配置，再读秘密，连接后核对数据库、账户和版本。
- 不使用 AutoMigrate，不删除数据库、不重建容器、不清空既有数据。
- 独立审查、负责人验收及学习讲解仍待用户；完整 T02 不结项。

## Review Focus

- 错误环境、应用账户及危险命令必须在读秘密和联网前拒绝。
- MySQL DDL 隐式提交：失败不可伪称整体回滚，成功 DDL 与版本登记之间存在故障窗口。
- 并发入口必须在同一数据库锁上互斥，锁连接和执行连接保持同一会话。
- 默认测试不可连接 MySQL；显式写入测试也只操作固定测试库，保留故障探针以免清理既有数据。
- 原始 SQL/DSN/秘密不得通过迁移错误或日志输出。

## Task 1：显式入口与目标保护

Files: `backend/tools/migrate/{go.mod,main.go,config.go,config_test.go}`。

- [x] 先写并运行配置与 CLI 拒绝路径测试（RED）。
- [x] 实现 `parseConfig([]string) (config,error)` 和 `config.validate() error`；只支持显式环境、秘密文件绝对路径及 up/status。
- [x] 验证配置测试通过（GREEN）。

## Task 2：SQL、版本与失败保护

Files: `backend/migrations/*.sql`、`backend/tools/migrate/{runner.go,runner_test.go,integration_test.go}`。

- [x] 先写测试：真实六表约束、重复执行前后版本记录与数据相同、错误 SQL 不登记、部分 DDL 不静默跳过、恢复后可继续、锁冲突不执行（RED）。
- [x] 实现六个 SQL 文件及 Goose Provider，使用 MySQL 会话锁、受控错误和有界超时（GREEN）。
- [x] 故障夹具使用独立随机版本表/探针表，保留检查；业务约束夹具在事务中回滚，不删除既有数据。

## Task 3：验证与交付

Files: 迁移 README、T02 迁移验收记录、AGENTS/README/PLAN/TASKS/ARCHITECTURE/DECISIONS 的当前状态增量。

- [x] 执行工具及服务端完整 test/vet/build/模块校验；显式执行隔离 MySQL 迁移集成测试。
- [x] 记录真实命令、结果、限制及未执行项；自查范围、差异、链接与秘密。
- [x] 停止在当前技术闭环；待独立审查、人工验收与学习讲解。

## 执行记录

- 起始分支 `feat/t02-database`，工作区干净。
- CodeGraph 索引可用但配置源已变化；依其输出读取当前实际文件，不重建索引。
- 沙箱默认无法访问 Docker named pipe；获准的只读提升检查确认两套 T02 容器健康，未操作其他容器。
- 取舍：使用 Goose Provider 构建独立 CLI，与标准 Goose 共用版本格式；独立 module 隔离新工具依赖，不改变服务依赖或 HTTP 行为。
- 技术自查补充：改用 ApplyVersion 避免加锁前初始化；校验历史前缀并处理已执行信号与清理错误合并。相关 RED/修复/最终 GREEN 见 [验收记录](../../T02_MIGRATIONS_ACCEPTANCE.md)。
- 工具最终含 MySQL 的 race 全量 8 测试、默认 test、vet/build/verify 均通过；服务回归及现有两个只读集成用例通过。独立审查与人工门槛保持待办。

## 验收与提交授权增量

2026 年 11 月 8 日负责人接受有条件独立审查、验证边界及两个 P2/一个 P3 延期，授权本轮登记并核对后提交及推送。上文“等待独立审查”和“不创建提交”等为实施阶段历史边界。当前问题及关闭条件见 [TASKS](../../TASKS.md#t02-迁移延期问题登记)；学习讲解尚未登记，完整 T02 仍进行中。不创建 PR、不合并、不推进延期修复、种子或其他开发。
