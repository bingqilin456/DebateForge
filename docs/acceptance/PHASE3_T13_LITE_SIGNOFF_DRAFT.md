# T13-Lite 安全及故障回归签收草案（待负责人确认）

日期：2026-11-17（Asia/Shanghai）。分支 `codex/research-and-demo-launcher`，HEAD `ee87ddb08b36311d4f63e53df4863c3df686c7bd`。本文是**草案**，只整理已有证据与其适用边界，等待负责人确认；不构成任务通过或 Phase 3 结项的结论。

本轮只做只读核对与文档起草：没有重跑后端测试、没有启动服务、没有连接数据库、没有调用真实模型。原始证据、ADR 与门槛数值均未改动。

## 1. 本文的结论边界

先说明本文能说什么、不能说什么：

- 本文登记的是**已执行过的本地累计回归**，来源是 [T13-Lite 累计验收矩阵](../planning/PHASE3_A_INTEGRATION_SPEC.md#t13-lite-累计验收矩阵) 与其中引用的 Phase 2、T03/T04/T05/T07/T09/T10 记录。这些是历史证据，本轮未重跑。
- 「无新增 P0/P1 缺陷」只对**已实际执行的回归项**成立。**被显式跳过的平台专项与本轮未执行项不在该结论内**：它们既不算通过，也不算发现缺陷。该结论同样不覆盖真实模型链路的已知失败，见第 4 节。
- 本文不声称 T13-Lite 已通过、不声称前后端全链路复验、不改变 TASKS 的任务状态。任务状态只由 [TASKS](../planning/TASKS.md) 的「当前状态」栏维护，改动需负责人确认。

## 2. 历史已复验项（引用，未重跑）

下表摘自 [首批矩阵](../planning/PHASE3_A_INTEGRATION_SPEC.md#t13-lite-累计验收矩阵)，原记录的「本轮通过」指该轮实际执行，不是本次执行：

| 风险 | 该轮实际验证 | 累计证据 |
| --- | --- | --- |
| 认证/Session | auth 包的 Me、登录失败一致性、退出撤销、Secure Cookie、Argon2id | 通过；真实 MySQL Session 与重启沿用 Phase 2 |
| 所有权/越权 | debate service 的 retry/summary/detail 分支与按 owner 约束的 repository | Phase 2 四组集成用例与 AC08 |
| CSRF/Origin | `TestPOSTRequiresExactOrigin`、`TestPOSTRequiresExactlyOneOriginHeader`、`TestLogoutRejectsDuplicateCSRFHeader` | 通过；业务路由共用 RequireOrigin/Auth/CSRF |
| 请求体/字符/分页 | 请求体上限、`TestInputBoundaries`、非法 UTF-8 与超长输入 | 通过；Phase 2 的 2000/2001 码点与 32 KiB 证据复用 |
| 认证限流/生成额度/并发 | 限流响应头、`TestAdmissionSlotsQuotaAndRollback`、非法请求限流 | 通过；6 次/分钟、20 次/日、每用户 1、进程 2 未放宽 |
| 超时/取消/失败恢复 | HTTP 与模型预算、`TestGenerationTimeoutPreservesLedgerFailure`、T09/T10 失败重试与版本 | 通过；Phase 2 真库提交不明、迟到结果、崩溃恢复复用 |
| 日志脱敏 | 受控 panic/路由日志、`TestOpenAIGeneratorLogsWhitelistedFieldsOnly` | 通过；无凭据、正文与原始供应商错误 |
| 两种赛制边界 | `TestRoundsForEachMode`、`TestStageThirdRoundDiverges`、`TestValidateRequestUsesModeRounds`、`TestParseSummaryRoundNoFollowsMode` | 通过；4/5 与 3/4 边界、8/6 条消息、总结轮号 |
| 费用保护/真实入口关闭 | 账本排他预留、fail closed、监控与停用、无自动重试，服务端默认关闭真实调用 | 通过；Linux 专用结果测试在 Windows 跳过 |

## 3. 平台与执行边界

这一节是本次核对的重点，避免把历史结论读成「全平台已实测」：

- 那轮回归在 **Windows + Go 1.27.1** 上执行，`AI_MODE=disabled`，并清除了两个数据库集成开关；适配器测试只连进程内合成 HTTP 供应商，没有真实出网。
- **Linux 专用用例在 Windows 上显式跳过**：`backend/internal/ai/demo_test.go`（持久账本只支持 Linux）、`backend/internal/ai/trial_test.go` 与 `trial_stream_test.go`（目录持久同步按部署目标在 Linux 验证）。`backend/internal/platform/t07_nginx_test.go` 还需要 `SIYIGE_TEST_NGINX_PROXY_CONF` 指向隔离 Linux 容器中的 `proxy.conf`，否则跳过。
- 因此 **Windows 本地不能记为 Linux 持久化、目录同步或 Nginx 代理链路的实测**。这些项若需确认，只能在 Linux 容器内另行执行，本轮未执行。
- 被跳过的这些用例**不计入第 1 节「无新增 P0/P1 缺陷」的范围**：它们没有被执行，既不能说通过，也不能说发现了缺陷。
- 前端与迁移工具当时未改动，复用 Phase 2 证据。

## 4. 四项 CI 与真实模型失败的位置

**四项 CI 是历史证据，当前 HEAD 未核实、未执行。** 记录中出现的四项门禁（仓库规范与静态、后端与迁移工具无收费检查、隔离 MySQL 集成、前端类型检查与构建）分别对应 [.github/workflows/ci.yml](../../.github/workflows/ci.yml) 中的 `repo-guard`、`backend-unit`、`backend-mysql-integration`、`frontend`。相关表述来自 134 基线的交接与 PR #26 的 run 43818298236，来源为 Codex 核对而非本轮自行触发，具体登记见 [B2/B3 签收记录](PHASE3_B2_B3_SIGNOFF.md)第 4 节。本轮没有触发 CI，也没有读取当前 HEAD 的运行结果，所以**不能把历史四项结果写成 `ee87ddb` 的 CI 状态**。

真实模型链路上已登记的失败不因 T13 的无收费回归而消失，本文不覆盖：

- 会话 25 的总结生成失败（`model_unavailable`，attempt v3），`data` 流派因此无合格样本。
- 两次模型输出未通过严格结构校验的失败（策略 1 次、总结 1 次），模型均已正常计费。
- 账本停用语义的修复已通过 build/vet/test/race，但**其运行时效果尚未在真实调用中验证**——那次执行用的仍是旧镜像。

以上均见[真实模型执行记录](P3_REAL_MODEL_EXECUTION_2026-11-17.md)第 5、8、9 节。

## 5. 本轮未执行项

| 项目 | 状态 |
| --- | --- |
| 后端 `go test`/`vet`/`build` | 未执行（复用历史结果） |
| Linux 容器内的持久账本、目录同步与 Nginx 代理专项 | 未执行 |
| 隔离 MySQL 集成测试 | 未执行（未连接数据库） |
| 前端 typecheck/test/build 与浏览器回归 | 未执行 |
| 四项 CI 触发或读取 | 未执行 |
| 任何服务启停、真实模型调用 | 未执行 |
| Commit / Push / PR / 合并 / 部署 | 未执行：起草阶段未执行，本轮三项分提交由 Codex 核对后进行 |

## 6. 结论（待负责人确认）

建议的确认口径是「**已实际执行的本地累计回归通过，这些项上无新增 P0/P1 缺陷**」，并同时接受三条边界：仅限所引用的已执行回归项，跳过与未执行项不在结论内；Linux/Nginx 专项与四项 CI 属历史证据、当前 HEAD 未核实；真实模型已知失败不受本文影响。

若负责人认为该口径不足，可要求补跑 Linux 专项或在新 HEAD 上重跑回归，再据实更新本草案——那需要新的执行授权，本文不预写结果。

## 7. 负责人确认记录（2026-11-17）

负责人在本次会话的 Q1 中明确答复「确认草案口径」。确认范围为：本文准确反映已有累计回归，「无新增 P0/P1 缺陷」仅限已执行项；当前受核对基线 `ee87ddb` 的 CI 未核实，Linux/Nginx 跳过项及真实模型已知失败继续保留。

本次只确认文档口径，不将 T13-Lite 改为「已通过」，不代替任务正式验收或 Phase 3 结项。前文「待负责人确认」保留为起草时状态；本节为其后续答复记录，未新增任何测试或 CI 结果。
