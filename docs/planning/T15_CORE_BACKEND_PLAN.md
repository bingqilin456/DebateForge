# T15-Core 后端实施计划

日期：2026-11-14。分支：`codex/p3-a-integration`。范围与执行方式由成员 A 本轮明确授权：Codex 规划、监督和验收，DSH 分段实现；允许本地开发、测试、Commit、普通 Push、更新草稿 PR #19。

依据：[冻结契约](PHASE3_A_INTEGRATION_SPEC.md#t15-core-冻结契约与可复制样例)、[API](../api/API.md)、[OpenAPI](../api/openapi.yaml)。A1 T13-Lite 技术回归通过；原记录的负责人验收状态如实保留，本轮获准继续 T15。保留 A2 策略与 Phase 2 成果，不修改 B 前端，不实施 P1、真实模型调用或公网部署。

## 设计与边界

- AI 增量接口使用 `Stream(context.Context, Request, func(string) error) (Result, error)`，回调只输出临时文本，最终 Result 必须通过与普通生成一致的校验。实际适配器调用锁定版本 Eino `BaseChatModel.Stream`，关闭 reader，传播取消；Fake、disabled 和试运行包装显式实现同一能力。保留窗口、超时、账本、出网白名单和费用上限。
- 普通和流式 submit 共享 ValidateSubmit、Store.Claim、Gate 和最终 Complete/Fail 路径；不增加数据库表、认领方法、第二次写入或幂等键。JSON retry 保持原路径与 expected_attempt_version 语义。
- Handler 完成身份、CSRF、限流、参数及认领后才开始 SSE。generating 同键重放返回 202 JSON，completed 重放 start/done；新生成 start/delta 后仅在 Complete 提交成功时 done。错误使用冻结信封，数据库结果不明只提示查询。
- HTTP 请求 60 秒、模型 45 秒、租约 90 秒；15 秒心跳；写失败取消上游。最终提交与取消竞争以数据库结果为准，禁止迟到尝试覆盖新版本。

## 分段交付与验证

1. DSH：AI Stream 接口、Eino/响应观察、Fake/disabled/预算与 trial 包装及相关单元测试。先验证失败测试，再实现；只读锁文件与实际 SDK，禁止依赖升级。Codex 独立审查。
2. Codex（实际执行）：共享 submit/runGeneration 增量入口、SSE handler、request budget；覆盖事件前错误、202/重放、心跳、取消/超时/提交失败/提交不明/旧版本。Codex 独立核对认领、事务及费用保护。
3. Codex（实际执行）：隔离 MySQL 并发与无双写专项、可复用直连/Vite/Nginx 验证入口；UTF-8 字节拆分、半事件、丢失 done 后查询恢复。仅使用固定测试目标和 Fake；代理须实测才能登记通过。
4. 适用检查：`go test ./...`、`go vet ./...`、`go build ./...`、相关 race 专项、隔离 MySQL 集成；受影响迁移工具单独检查。记录环境/命令/结果与未验证项，维护 API/OpenAPI、TASKS、验收与 B 交接。
5. 核对变更范围后提交、普通推送、更新同一草稿 PR 并检查 CI。不自动合并。负责人最终验收与完整 AC12 前端验收独立保留。

## 审查重点

- 下游断线与最终事务提交竞争；已提交而 done 丢失后从详情恢复。
- 普通与流式同键竞争，失败版本冲突，旧尝试迟到；消息与计轮只能一次。
- 供应商无尾帧、非法 UTF-8、空回复/拒答/截断、未知 usage；不得把未知用量记零或正式保存临时片段。
- 写出前错误与写出后错误边界；不缓存整流伪造流式，不旁路试运行账本。
- 本地代理存在配置与实际逐块透传分开验收；B 的生产解析器及浏览器 AC12 不凭后端测试宣告通过。
