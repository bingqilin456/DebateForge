# T09 三轮状态机与失败恢复（含 T11 后端历史分页）技术验证与独立审查记录

- **任务编号**：T09 三轮状态机与失败恢复（含 T11 后端本人历史分页 API `GET /api/v1/debates`）
- **基线提交**：`origin/main`（`9502dc0`，含 PR #10 提交 `59689bb`）
- **审查日期**：2026-11-12
- **主责与审查范围**：成员 A 实现；独立会话 C 执行只读代码审查、API/OpenAPI 契约核对与自动化回归验证
- **关联需求与契约**：[PRD](../product/PRD.md)（F03、F04、F06；AC03、AC04、AC05、AC07、AC08） · [ARCHITECTURE](../architecture/ARCHITECTURE.md#会话与轮次状态机) · [API](../api/API.md) · [OpenAPI](../api/openapi.yaml)

---

## 1. 交付文件范围核对

本次审查核对 PR #10（`59689bb`）在 `origin/main` 合入的后端与契约文件：

- **业务模型与参数校验**：`backend/internal/debate/model.go`（定义 `LeaseDuration = 90s`、`LeaseScanInterval = 30s`、分页常量 `DefaultPage=1` / `DefaultPageSize=20` / `MaxPageSize=50`、`RetryInput`、`DebateList`、`ValidateRetry`、`ParsePagination` 及 `ErrAttemptVersionConflict`）
- **HTTP 路由与协议边界**：`backend/internal/debate/handler.go`、`backend/internal/platform/business.go`（注册 `GET /api/v1/debates` 与 `POST /api/v1/debates/:id/turns/:turn_id/retry`；为轮次重试挂载 60 秒生成请求预算、`RequireOrigin`、`RequireAuth`、`RequireCSRF` 与 `limitGeneration`；启动期触发 `StartLeaseRecovery`）
- **服务层状态机与租约调度**：`backend/internal/debate/service.go`（实现 1–3 轮有序历史拼装、`Retry` / `retry`、`StartLeaseRecovery` / `StopLeaseRecovery` / `waitStartupScan` 屏障、迟到写回拦截及失败落库保护）
- **持久化短事务与租约回收**：`backend/internal/debate/repository.go`（`Claim` 放开 1–3 轮并校验前置轮次完整、`Complete` 在第 3 轮完成时将 `debates.status` 置为 `completed`、`ClaimRetry` 原子校验 `status='failed'` 与 `attempt_version = expected_attempt_version` 并递增版本、`RecoverExpiredLeases` 回收过期 `generating` 记录、`List` 只读事务按 `created_at DESC, id DESC` 分页）
- **单元测试与隔离 MySQL 集成测试**：`backend/internal/debate/service_test.go`、`backend/internal/debate/limits_test.go`、`backend/internal/debate/review_regression_test.go`、`backend/tests/integration/debate_test.go`、`backend/tests/integration/debate_failures_test.go`
- **接口契约文档**：`docs/api/API.md`、`docs/api/openapi.yaml`

---

## 2. 独立技术审查结论（PASS）

对 T09 与 T11 后端实现的逐项契约与安全不变量核对如下，**未发现 P0/P1/P2 缺陷**：

| 审查维度 | 代码位置 | 核对结果 |
| --- | --- | --- |
| **三轮顺序与第 4 轮拒绝（AC03）** | `repository.go:180-194`、`service.go:381-392` | `Claim` 在 `FOR UPDATE` 锁定会话后校验 `row.Status == "active"`、`1 <= in.RoundNo <= 3` 且 `in.RoundNo == row.CompletedRounds + 1`，并逐轮核对 `1..in.RoundNo-1` 均处于 `completed` 且含 `user`+`assistant` 两条正式消息。第 4 轮、跳轮、或在存在 `generating`/`failed` 轮次时提交下一轮均返回 `409 round_conflict`。第 3 轮完整回复落库时在同一短事务内将 `debates.status` 更新为 `completed`。 |
| **短事务外调用模型与并发准入** | `repository.go:195-212, 261-294`、`service.go:185-253` | 新轮认领（`Claim`）与失败重试认领（`ClaimRetry`）均仅在确认需要调用模型时非阻塞调用 `admit()`（`gate.Acquire(uid)`：单用户并发 1、单进程并发 2、每日 20 次尝试），认领提交后释放数据库锁，在事务外通过 `runGeneration` 调用 Eino `Generator`（45 秒超时）。`defer permit.Finish(consumed)` 覆盖回滚、取消、panic 与正常完成，确定回滚不扣减每日额度。 |
| **显式重试与 `expected_attempt_version`（AC04）** | `model.go:297-302`、`repository.go:218-300`、`service.go:217-253` | `ValidateRetry` 强制 `expected_attempt_version >= 1`（缺失、0、负数、非整数或 `MaxUint64` 均返回 `400 invalid_request`）。`ClaimRetry` 校验 `turn_id` 归属当前用户与当前 `debate_id`（不匹配统一返回 `404 not_found`）。遇到 `completed` 返回 `200` 重放，遇到 `generating` 返回 `202`；遇到 `failed` 但 `turn.AttemptVersion != expectedVersion` 时返回 `409 attempt_version_conflict` 并携带最新 `FailureDetails{turn_id, status, attempt_version, query_url}`，**不获取并发槽、不消耗每日生成额度、不调用模型**。版本匹配时以 `WHERE id = ? AND debate_id = ? AND status = 'failed' AND attempt_version = ?` 原子更新为 `generating`、`attempt_version + 1` 并复用已保存 `user` 原文。 |
| **迟到旧尝试写回拦截（AC04/AC05）** | `repository.go:301-311, 350, 390` | `Complete` 与 `Fail` 均先锁定会话与轮次并通过 `currentTurn` 及 `WHERE id = ? AND status = 'generating' AND attempt_version = ?` 校验当前尝试版本。一旦租约被回收或已被新版本重试接管，迟到的旧模型结果写回返回 `ErrRoundConflict` 并转为受控 `503 dependency_unavailable`，绝不覆盖新尝试或追加重复 `assistant` 消息。 |
| **崩溃残留租约恢复（AC05）** | `service.go:44-109`、`repository.go:615-643` | 租约时长固定 90 秒（高出 60 秒请求预算与 45 秒模型超时），`StartLeaseRecovery` 在路由注册时立即执行一次启动扫描（业务读写入口通过 `waitStartupScan` 等待启动扫描完成），随后每 30 秒扫描 `status = 'generating' AND lease_expires_at <= now` 的记录，按 `id + status + attempt_version + lease_expires_at` 条件更新为 `failed`（`error_code = 'model_timeout'`），保证服务崩溃重启后不会永久卡在 `generating`。 |
| **T11 本人历史分页与跨用户隔离（AC07/AC08）** | `model.go:313-337`、`repository.go:98-120` | `GET /api/v1/debates` 强制 `RequireAuth()`；`ParsePagination` 默认 `page=1, page_size=20`，限制 `1 <= page <= 10^9`、`1 <= page_size <= 50` 且拒绝重复同名查询参数，非法参数返回 `400 invalid_request`。`Repository.List` 在 3 秒只读事务内严格按 `WHERE user_id = ?` 统计 `total` 并按 `created_at DESC, id DESC` 分页，空结果返回 `"items": []`。 |

---

## 3. 路由与 API/OpenAPI 契约一致性核对

核对后端路由注册（`platform/router.go`、`platform/business.go`、`auth/handler.go`、`topic/handler.go`、`debate/handler.go`）与 [API.md](../api/API.md)、[openapi.yaml](../api/openapi.yaml)：

- 后端实际注册的 **14 个 HTTP 操作（12 条路径）** 与 `API.md` 接口表、`openapi.yaml` 的 `paths` 完全一致（含 `GET /api/v1/debates`、`POST /api/v1/debates/{id}/turns`、`POST /api/v1/debates/{id}/turns/{turn_id}/retry`）。
- 请求参数、认证与 Origin/CSRF 中间件要求、成功状态码（`200`/`201`/`202`）、错误码枚举（含新增 `attempt_version_conflict`）及 `TurnFailureDetails` 结构完全对齐。
- `API.md` 中 21 个 JSON 示例块全部通过严格 JSON 语法解析；`openapi.yaml`（版本 `t09-t10-t11-2026-11-11`）全部 14 个操作均标记为 `x-implementation-status: implemented`。

---

## 4. 执行的验证命令与结果

### 4.1 本机无收费单元测试、静态检查与竞态检测（`-race`）

在当前工作树（Windows 11，Go 1.27.1，`AI_MODE=disabled`，清除 `SIYIGE_INTEGRATION` 与 `SIYIGE_MIGRATION_TEST`）执行：

```bash
cd backend && env -u SIYIGE_INTEGRATION -u SIYIGE_MIGRATION_TEST AI_MODE=disabled go vet ./...
cd backend && env -u SIYIGE_INTEGRATION -u SIYIGE_MIGRATION_TEST AI_MODE=disabled go test -race -p 1 -count=1 ./...
cd backend && env -u SIYIGE_INTEGRATION -u SIYIGE_MIGRATION_TEST AI_MODE=disabled go build ./...
```

- `go vet ./...`：**通过（0 警告）**
- `go test -race -p 1 -count=1 ./...`：**全部 8 个包通过**（含 `TestT09ThreeRoundsExplicitRetryLeaseAndT11Pagination`、`TestHTTPRetryAndPaginationRoutes`、`TestReviewCancelDuringCompletion` 等并发与状态机单测）
- `go build ./...`：**通过**

### 4.2 隔离 MySQL 8.4.13 集成测试验证（GitHub Actions CI 实测留证）

- 本机当前未启动 Docker Desktop Linux Engine（`//./pipe/dockerDesktopLinuxEngine` 不可达），因此本机未直接运行 `SIYIGE_INTEGRATION=1` 数据库写测试。
- 核对 `origin/main` 最新合入提交 `9502dc0` 的 GitHub Actions CI 运行记录（Run ID `43018694901`），其中 `隔离 MySQL 8.4.13 迁移与集成测试` Job（Job ID `130516671803`）在官方 `mysql:8.4.13` 隔离容器（`127.0.0.1:13307/siyige_test`）上实际执行并通过：
  - `TestT09ThreeRoundsAndFourthRoundRejected`（三轮连续推进、历史上下文递增、第 3 轮完成转 `completed`、6 条正式消息落库、第 4 轮 `409 round_conflict` 拒绝）
  - `TestT09ExplicitRetryVersionConflictAndOwnership`（第 2 轮超时落库 `attempt_version=1`、非法期望版本 `400`、跨会话 `turn_id` `404`、首次重试再失败升至 `attempt_version=2`、重发旧版本 `expected_attempt_version=1` 返回 `409 attempt_version_conflict` 且零模型调用、使用版本 2 重试成功返回 `200` 且重放幂等）
  - `TestT09LeaseRecoveryOnStartupAndTicker`（过期 `generating` 轮次被 `RecoverExpiredLeases` 标记为 `failed/model_timeout`、迟到旧尝试写回被拦截返回 `503`、恢复后可显式重试成功）
  - `TestT11HistoryPaginationAndIsolation`（未登录 `401`、空列表 `items=[]`、`(created_at DESC, id DESC)` 排序与多页切片、非法分页参数 `400`、两用户历史列表严格隔离）

---

## 5. 未执行项与遗留边界

- 本次独立审查全程在 `AI_MODE=disabled` 下执行，**未发起任何真实收费模型调用**。
- 本机因 Docker Engine 未启动，未在本机复跑 `SIYIGE_INTEGRATION=1`，隔离 MySQL 8.4.13 实测结果由 `origin/main` GitHub Actions CI（Run `43018694901`）及 T12 联调验收记录覆盖。
- 单进程内存限流器（`Gate`）的每日生成额度与分钟防刷在进程重启后重置，跨实例或持久化费用账本边界保持按 [T07-COST-01](../planning/TASKS.md#t07-cost-01-供应商硬预算缺失) 管理。
- 任务状态保持 `待人工验收`，等待项目负责人最终确认。
