# T05 首轮业务实施计划

2026-11-08（Asia/Shanghai）。负责人已明确授权实施至可验收交付或明确阻塞，指定 Codex 统筹、DSH 实现、新 Codex 会话独立只读审查。仅 T05，不收费调用、不提交或部署。

## 基线与契约

- 分支 `codex/docs-reorganization`，HEAD `b1c255b12ad89078478c816d1ad125ec9ee7c69e`，开始时工作区及暂存区干净。
- T03 认证记录末尾：独立审查有条件通过，无未解决 P0/P1，人工验收完成，已接受 P2/P3 延期仍保留。T04 记录末尾：已正式结项；一次真实调用授权已消耗。T07-COST-01 继续阻塞公网真实入口。
- 输入、输出、权限和 HTTP 错误以 API.md 的 Phase 1 字段、四条 T05 接口、错误码表及 openapi.yaml 为准。PRD F02/F03/F04/F06、AC02/AC03 首轮子集/AC05 重放子集/AC07 重读子集/AC08 提供验收依据。
- ARCHITECTURE.md 的轮次处理顺序、安全与资源控制和 ADR-007/008/016：短事务认领，事务外 Generate，状态与 attempt_version 写回；单实例内存并发 1/用户、2/进程，6 次/分钟 HTTP 防刷、20 次/日成功认领的新尝试。重放与冲突不消耗每日生成额度；内存额度重启丢失，不是计费账本。429 含 Retry-After，不扩展为多副本保证。
- 32 KiB、1–2000 Unicode 码点、拒绝全空白并保存原文；请求 60s、模型 45s、数据库操作 3s；普通接口短预算。租约保留 90s，不实现回收。

## 实现与验证任务

1. **业务分层（DSH 串行独占写入）**：新增 `backend/internal/topic/`、`backend/internal/debate/` 的 model/dto/repository/service/handler 和相邻测试。复用 auth.RequireAuth/RequireOrigin/RequireCSRF、ai.Generator，不修改既有迁移、认证实现和 AI 实现。先写失败测试并记录，再实现。
   - `GET topics` 真实 enabled 查询与 ID 升序；`POST debates` 事务校验题目、保存快照、反转立场，零模型调用。
   - 首轮认领在同一事务锁定本人会话、先查 UUID 幂等，再判断轮次；非阻塞准入与每日额度预留，提交后消费，回滚释放。提交结果不明保守处理，不生成或补发。
   - user 消息随认领提交；成功写回唯一 assistant、turn completed、完成轮数 1、debate active；失败保留原文、不增轮。取消独立有界上下文尽力标记失败，状态与版本一致才可写回。
   - 同键不同内容/轮号 409 idempotency_conflict，已完成 200、生成中 202、已失败 409 turn_failed；异键已有轮次 409 round_conflict。本人详情读取一致快照、消息 user/assistant 排序、attempt_version 与固定 summary_status。
   - 测试包括错误映射、边界码点/空白/请求体、准入额度/并发槽释放、版本保护、模型错误/超时/取消、数据库及提交失败；不得只断言 Mock 返回值。
2. **装配与 HTTP 期限（Codex 或同一 DSH 后续）**：`platform/router.go`、`cmd/server/main.go` 及必要测试装配四接口。HTTP 入口先设预算，普通请求保留短期限，生成可超过 5s；无收费默认装配不得自动读取模型凭据发出真实请求。正式 Generator 接口可注入 T04 实现，真实调用另待授权。
3. **隔离 MySQL + 真实 HTTP（串行）**：新增 `backend/tests/integration/debate_test.go`，复用固定测试目标前后校验。真实注册/登录/CSRF、创建、>5s Fake 成功 JSON、重放/冲突/并发、他人及不存在统一 404、2000/2001 码点、失败/取消/数据库失败、重建服务后详情可读。只清理本次确切夹具，不动既有记录，不改容器/卷。
4. **统一验证与文档**：Go 定向测试后运行必要 test/race/vet/build、gofmt、diff 检查；显式区分集成跳过/实际通过，不重复无变化全量验证。同步 AGENTS/TASKS/API/OpenAPI/架构必要的当前状态，新增 T05 验收记录，保留历史证据。新 API 契约如有缺口先等待负责人确认。
5. **独立审查**：实现和统一验证后新建独立 Codex 会话只读审查；修复后再新建会话复审。P0/P1 关闭，P2 修复或负责人接受，P3 登记；工具不可用则报告阻塞。主会话自检及 DSH 自测不冒充独立审查。

## 明确不做

T06 及后续、第二三轮、显式重试、租约扫描、总结、历史列表、SSE、前端、真实模型调用、秘密文件修改、已执行迁移修改、commit/push/merge/rebase/reset/PR/部署。技术通过不代替人工验收，不自动结项。

## 官方用法核验

实施前核对 [GORM Transaction](https://gorm.io/docs/transactions.html)、[GORM Locking](https://gorm.io/docs/advanced_query.html#Locking)、[Go ResponseController](https://pkg.go.dev/net/http#ResponseController.SetWriteDeadline)。API 用法以锁定依赖和本机 Go 1.27.1 源码复核；不新增产品依赖。


## 执行收尾

Codex 裁定的执行调整：DSH 首回合没有代码落盘，后续缩小为 topic 切片并交付；Codex 直接完成 debate、装配、集成测试与文档，未增加会话或并行写。代价是主会话实现与整合职责集中，按用户要求通过两次全新独立 Codex 会话初审/复审验证。

无收费实现、实际隔离 MySQL 累计 race、vet/build/格式及契约检查已通过；初审三项 P2 修复后，新会话独立复审 PASS 并逐项确认关闭，无新 P0/P1/P2/P3。2026-11-08 项目负责人确认三关人工验收全部通过，正式批准 T05 无收费实现范围结项并授权从当前 HEAD 创建 `feat/t05-first-turn` 后 Git Commit。本轮不使用 DSH、不改业务代码、不重跑完整测试或独立审查，不 Push/Merge/Rebase/Reset/部署或收费调用。真实模型链路未验收，须另行授权；单进程并发未来在 T13 评估提升至 4–8，当前维持 2。前文为实施阶段的历史授权，最终状态与已接受限制见 [验收记录](../acceptance/T05_FIRST_TURN_ACCEPTANCE.md#最终人工验收与无收费范围结项)，不进入 T06。
