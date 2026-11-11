# T10 结构化总结与重试技术验证与独立审查记录

- **任务编号**：T10 结构化总结与重试（Phase 2）
- **基线提交**：`origin/main`（`9502dc0`，含 PR #11 提交 `522089b`、`423489d` 与 PR #12 提交 `345bebc`）
- **审查日期**：2026-11-12
- **主责与审查范围**：成员 A 实现；独立会话 C 执行只读代码审查、迁移与 Schema 校验核对、API/OpenAPI 契约核对与自动化回归验证
- **关联需求与契约**：[PRD](../product/PRD.md)（F05、F06；AC06、AC07、AC08） · [ARCHITECTURE](../architecture/ARCHITECTURE.md#ai-上下文与总结) · [API](../api/API.md) · [OpenAPI](../api/openapi.yaml)

---

## 1. 交付文件范围核对

本次审查核对 PR #11（`522089b`、`423489d`）与 PR #12（`345bebc`）在 `origin/main` 合入的文件范围：

- **版本化数据库迁移**：
  - `backend/migrations/00011_create_debate_summaries.sql`（新增 `debate_summaries` 表，未修改已冻结的 `00001–00010` 迁移文件）
  - `backend/migrations/README.md`、`backend/tools/migrate/integration_test.go`、`backend/tools/migrate/history_integration_test.go`、`.github/workflows/ci.yml`（同步初始迁移版本数为 11，补充 `debate_summaries` 表约束与精确枚举测试）
- **AI 总结 Prompt 与 Schema 校验层**：
  - `backend/internal/ai/summary.go`、`backend/internal/ai/budget.go`、`backend/internal/ai/summary_test.go`（实现 `SummaryGenerator` 接口、`SummaryPromptVersion = "summary-v1"`、`SummarySchemaVersion = 1`、6 条有序消息输入校验、`BuildSummaryPrompt`、`ParseSummary` / `ValidateSummary` 严格 JSON Schema 校验器，以及 `OpenAIAdapter` 与 `Fake` 实现）
- **Debate 总结业务与持久化层**：
  - `backend/internal/debate/model.go`、`backend/internal/debate/handler.go`、`backend/internal/debate/service.go`、`backend/internal/debate/repository.go`、`backend/internal/platform/business.go`（实现 `POST /api/v1/debates/:id/summary`、`GET /api/v1/debates/:id/summary`、详情 `summary_status` 真实投影、总结短事务认领、显式重试版本控制及 90 秒过期租约回收复用）
- **测试与接口文档**：
  - `backend/internal/debate/service_test.go`、`backend/internal/debate/review_regression_test.go`、`backend/internal/platform/business_test.go`、`backend/tests/integration/debate_test.go`、`backend/tests/integration/debate_failures_test.go`
  - `docs/api/API.md`、`docs/api/openapi.yaml`

---

## 2. 独立技术审查结论（PASS）

对 T10 数据库迁移、AI 结构化总结校验及总结状态机的逐项审查结论如下，**未发现 P0/P1/P2 缺陷**：

| 审查维度 | 代码位置 | 核对结果 |
| --- | --- | --- |
| **`00011` 迁移与字节精确枚举约束** | `00011_create_debate_summaries.sql:1-28` | 采用 `-- +goose NO TRANSACTION` 单表原子 DDL；`debate_id` 具备唯一索引 `uq_debate_summaries_debate` 与外键 `ON DELETE RESTRICT ON UPDATE RESTRICT`；建立复合索引 `ix_debate_summaries_status_lease (status, lease_expires_at)`；同时施加 `status IN ('generating', 'completed', 'failed')` 与 `CAST(status AS BINARY) IN (_binary'generating', _binary'completed', _binary'failed')` 字节级精确 CHECK（延续 T02-M01 修复规范，拒绝尾随空格与大小写变体），并约束 `attempt_version >= 1`。 |
| **独立总结 Prompt 与防注入边界** | `internal/ai/summary.go:93-231` | `normalizeSummaryMessages` 严格要求 3 轮已完成辩论（共 6 条 `user`/`assistant` 交替且非空、合法 UTF-8、单条 ≤ 2000 码点的正式消息）。`BuildSummaryPrompt` 固定组装为 `system`（训练反馈者规则 + 辩题快照 + 双方立场 + JSON Schema + 防注入说明）→ 6 条历史消息 → 固定 `user` 总结触发指令；用户历史发言绝不拼入 `system` 提示词。 |
| **`schema_version=1` 严格 JSON 校验（AC06）** | `internal/ai/summary.go:248-541` | `ParseSummary` / `decodeStrictJSONObject` 校验合法 UTF-8、总字节数 ≤ 16 KiB（`MaxSummaryJSONBytes`），顶层仅允许且必须包含 `strengths`、`issues`、`suggestions` 三个键，拒绝未知字段、重复键、大小写变体、Markdown 代码块包裹或对象外尾随内容。三类数组均要求 1–3 项；`round_no` 必须为十进制整数 `1..3`（拒绝浮点数 `1.0`、字符串 `"1"` 或越界值 `0`/`4`）；`point`、`reason`、`action`、`example` 均须为 1–300 个 Unicode 码点的非空字符串。校验失败返回 `CodeModelError`（映射为 `502 model_error`）并落库 `failed`，不保存无效内容，也不自动发起隐式修复调用。 |
| **总结认领、重放与 `retry=false` 保护（AC06）** | `repository.go:415-475`、`service.go:256-299` | `ClaimSummary` 在短事务内锁定会话行，强制校验 `row.Status == "completed" && row.CompletedRounds == 3` 及 3 轮 6 条正式消息完整，未完成辩论返回 `409 round_conflict`。首次请求（`not_started`）传 `retry=true` 返回 `400 invalid_request`，传 `retry=false` 认领 `attempt_version=1` 并在生成成功后返回 `201`。已完成总结再次 POST 返回 `200` 重放；生成中返回 `202`；**已失败总结在 `retry=false` 时直接返回 `200` 与当前 `failed` 状态，不重新调用模型、不消耗生成额度**。 |
| **总结显式重试与 `expected_attempt_version`（AC06）** | `model.go:303-312`、`repository.go:476-538`、`service.go:285-290` | `retry=true` 时必填正整数 `expected_attempt_version`（缺失或非法为 `400 invalid_request`）。当总结处于 `failed` 但 `existing.AttemptVersion != *in.ExpectedAttemptVersion` 时返回 `409 attempt_version_conflict`（附 `status: "failed"`、最新 `attempt_version` 与 `query_url: "/api/v1/debates/{id}/summary"`），**不占用并发槽、不扣减每日额度、不调用模型**。版本匹配时原子更新为 `generating`、`attempt_version + 1`、清空旧错误与元数据，重试成功返回 `200`。 |
| **写回版本校验、租约回收与共享限额** | `repository.go:539-612, 644-665`、`handler.go:32` | `CompleteSummary` 与 `FailSummary` 均校验 `status = 'generating' AND attempt_version = ?`；取消或超时在独立 3 秒上下文中尽力落库失败并保留已知 Token 用量与模型元数据。`RecoverExpiredLeases` 同步扫描 `debate_summaries` 中超过 90 秒租约的 `generating` 行并标记为 `failed/model_timeout`。总结接口与轮次接口共享同一 `Gate`（6 次/分、20 次/日、1/用户、2/进程）及 60 秒请求预算。 |
| **所有权与跨用户隔离（AC08）** | `repository.go:136-161, 417` | `GET /api/v1/debates/:id`、`GET /api/v1/debates/:id/summary` 与 `POST /api/v1/debates/:id/summary` 均通过 `owned(tx, uid, id, ...)` 校验会话所有权；会话不存在或属于其他用户统一返回 `404 not_found`。 |

---

## 3. 执行的验证命令与结果

### 3.1 迁移工具与 AI/Debate 单元测试（含 `-race`）

在当前工作树（Windows 11，Go 1.27.1，`AI_MODE=disabled`）执行：

```bash
cd backend && env -u SIYIGE_INTEGRATION -u SIYIGE_MIGRATION_TEST AI_MODE=disabled go test -race -p 1 -count=1 ./internal/ai ./internal/debate ./internal/platform
cd backend/tools/migrate && env -u SIYIGE_INTEGRATION -u SIYIGE_MIGRATION_TEST go vet ./... && env -u SIYIGE_INTEGRATION -u SIYIGE_MIGRATION_TEST go test -count=1 ./... && env -u SIYIGE_INTEGRATION -u SIYIGE_MIGRATION_TEST go build ./...
```

- `backend/internal/ai`（含 `summary_test.go`）：**通过**（使用 6 条匿名固定消息覆盖 `BuildSummaryPrompt`、正反立场翻转、防注入规则、`ParseSummary` 三类数组上下界、非法 `round_no`、空白/超 300 码点字段、未知/重复键、超 16 KiB JSON、`OpenAIAdapter` 与 `Fake` 的 `GenerateSummary` 完整分支）
- `backend/internal/debate`（含 `TestT10SummaryGenerationReplayRetryConflictAndLeaseRecovery`）：**通过**
- `backend/tools/migrate`：`go vet`、`go test`、`go build` **全部通过**

### 3.2 隔离 MySQL 8.4.13 迁移与集成测试（GitHub Actions CI 实测留证）

- 本机当前未运行 Docker Desktop，`origin/main`（`9502dc0`）在 GitHub Actions CI（Run ID `43018694901`，Job ID `130516671803`）的官方 `mysql:8.4.13` 容器上完成实测并通过：
  - 迁移 CLI 首次执行输出 `applied=11`，重复执行输出 `applied=0`，`status` 确认 11 个版本全部 `applied`；
  - `SIYIGE_MIGRATION_TEST=1` 迁移集成测试通过（验证 `debate_summaries` 外键、唯一键、`attempt_version >= 1` 及 `status` 字节精确枚举 CHECK 拒绝大小写与尾随空格）；
  - `TestT10SummaryCreationReplayAndOwnership`（未完成辩论请求总结 `409 round_conflict`、`not_started` 传 `retry=true` 返回 `400`、首次生成 `201`、已完成重放 `200` 零模型调用、详情与总结查询一致、跨用户 `GET`/`POST` 总结统一 `404`）；
  - `TestT10SummaryRetryVersionConflictPendingAndLeaseRecovery`（生成中重放 `202`、90 秒租约过期扫描回收为 `failed`、迟到旧尝试写回返回 `503` 不覆盖、`retry=false` 遇 `failed` 返回 `200` 不重调模型、非法 Schema JSON 转为 `502 model_error` 并升至版本 2、旧版本重试返回 `409 attempt_version_conflict` 且零模型调用、使用版本 2 重试成功返回 `200`）。

---

## 4. 未执行项与遗留边界

- 本次验证全程使用 `AI_MODE=disabled` 或 `Fake` 桩，**未发起任何真实收费模型调用**；真实模型下的总结生成质量评估归属 Phase 3（T14）。
- 任务状态保持 `待人工验收`，等待项目负责人最终验收确认。
