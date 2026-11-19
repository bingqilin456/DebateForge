# Phase 3 B 侧收尾：T14-EVAL 三场检查表

开始于 2026-11-17，按负责人要求于 2026-11-18 简化收尾流程，2026-11-19 核对 GitHub 主分支（Asia/Shanghai）。本地仍在 `codex/p3-b-review`，HEAD 为 `a4151cf289f87589b0fc5a76299eb81c4cde5eb5`。

## T14-EVAL：三行表

[既有真实执行记录](P3_REAL_MODEL_EXECUTION_2026-11-17.md)有完成状态、流派和用量，**没有完整四轮原文与总结正文**；本地验收证据和已有导出中也未找到三场正文。以下为 AI 整理的待确认表，不以 completed 状态或 Fake 文本替代实际阅读。

| 会话／流派 | 回应用户观点 | 保持相反立场 | 明显失实或无依据断言 | 总结建议具体且对应实际发言 | 总体结论 |
| --- | --- | --- | --- | --- | --- |
| 22／logical | 无法判断 | 无法判断 | 无法判断 | 无法判断 | 缺四轮及总结原文，待 B 阅读确认 |
| 23／aggressive | 无法判断 | 无法判断 | 无法判断 | 无法判断 | 缺四轮及总结原文，待 B 阅读确认 |
| 24／empathic | 无法判断 | 无法判断 | 无法判断 | 无法判断 | 缺四轮及总结原文，待 B 阅读确认 |

**B 统一确认：待原文齐备并实际阅读后一次确认，当前未确认，AI 未代签。** 问题仅按“第 N 轮／立论、驳立论、自由辩或结辩＋一句话”记在对应单元格，不要求逐轮签字、打分或互审。

会话 **25** 的总结失败样例按原执行记录保留，**data 流派无合格样本**；不补跑、不删除失败记录、不把会话 **26** 自动替换进原三场。按现行 [ADR-029](../decisions/DECISIONS.md#adr-029-phase-3-真实质量评测门槛由-5-场降为-3-场完整场次) 执行，不回退旧10场门槛。真实样本来源是 `siyige_demo`，不是本轮 `siyige_test` 的 Fake 同号会话。

## 已有证据直接复用

- **T13-Lite、AC17**：GitHub 主分支已包含两项正式签收。[T13-Lite](https://github.com/bingqilin456/DebateForge/blob/c8457f1ebe3e9d50dbff63bde8e3be8f7d14df0b/docs/acceptance/PHASE3_T13_LITE_ACCEPTANCE.md)覆盖无收费安全及故障累计回归。[AC17](https://github.com/bingqilin456/DebateForge/blob/c8457f1ebe3e9d50dbff63bde8e3be8f7d14df0b/docs/acceptance/PHASE3_AC17_BROWSER_ACCEPTANCE_2026-11-17.md)覆盖固定 `c8cd103` 的会话 26，含 Nginx 代理下的真实四轮 SSE、总结和刷新重登。
- **AC12／AC16**：直接引用 [B2/B3 正式签收](PHASE3_B2_B3_SIGNOFF.md)、[B2 整合](PHASE3_B2_INTEGRATION_ACCEPTANCE.md)、[B3 浏览器证据](PHASE3_B3_BROWSER_ACCEPTANCE.md)。原签收受测SHA为 `134b845e5f9352d83c711d763471d1452ba336c0`；本轮只复用既有解析、幂等/版本/查询恢复、流派持久化与失败恢复等已验收行为，不把受测SHA换成最新main，也不把Fake当真实质量评测。当前本地改动只有验收脚本和材料，无产品代码、接口或依赖变更。
- **a4151cf 已有补验**：前轮已执行 **5/5 真实 Fake 浏览器项**，链路为 Edge→Vite→Go→固定MySQL→Fake，**不含Nginx**：策略持久failed后查询并显式retry、策略延迟与重复POST 202、流式202仅查询恢复、丢done恢复、四流派/默认/七种非法值/刷新重登历史/409恢复。重试次数0，跳过0；原始时间为2026-11-17T15:58:11Z起，89.95s。见[逐项收据](evidence/p3-a4151cf-review/direct-live.json)、[检查摘要](evidence/p3-a4151cf-review/checks.json)及[四流派历史](evidence/p3-a4151cf-review/four-styles-history.png)、[策略显式重试](evidence/p3-a4151cf-review/strategy-explicit-retry.png)、[丢done查询恢复](evidence/p3-a4151cf-review/lost-done-query.png)。这些合成样本不计入T14-EVAL。
- 前轮前端typecheck、18文件/305项test、build（668 modules）和23项浏览器Mock通过；Go策略5个顶层测试通过。Windows跳过的Linux账本断言未算通过，新账本真实调用效果未补验。原始检查与限制见上述摘要；本轮仅整理文档/已有证据，不重复未受影响检查。

- **当前主分支的复用范围（2026-11-19）**：GitHub `main` 为 `c8457f1ebe3e9d50dbff63bde8e3be8f7d14df0b`，[四项 CI 均通过](https://github.com/bingqilin456/DebateForge/actions/runs/44093180482)。[与 `a4151cf` 的比较](https://github.com/bingqilin456/DebateForge/compare/a4151cf289f87589b0fc5a76299eb81c4cde5eb5...c8457f1ebe3e9d50dbff63bde8e3be8f7d14df0b)只有协作规则、文档和验收材料变化。AC16 可复用原签收及 `a4151cf` 对流派创建、持久化和策略恢复的补验。AC12 可复用原签收的分块、解析及 Nginx 恢复证据，结合 `a4151cf` 的直连故障补验和会话 26 的真实代理流式证据。两项新版 Fake/Nginx 场景仍记录为未执行，受测版本各自保留。

## 剩余材料与独立观察

**T14-EVAL唯一材料阻塞**：需提供既有 `siyige_demo` 会话22/23/24的脱敏详情（题目、双方立场、流派、四轮user/assistant原文、总结strengths/issues/suggestions），由B实际阅读后统一确认。不需启动模型或调用任何生成/重试接口；不要导出Cookie、CSRF、口令、密钥、DSN或私人信息。

前轮本机代理曾出现间歇502：六次ready探测为200/200/502/200/200/200，502未到Go；两项新基线Nginx场景没有执行，不能记为7/7通过。原归属检查已在正确解码后通过，未删除容器或放宽守卫。此项保留为独立本地环境观察，根因未定；按本轮收口要求不重开全量B3，也不撤销已有签收。若负责人确认它属于交付环境受影响且缺证据的项目，再只补对应代理验证。

2026-11-19 的检查限于 GitHub 状态、版本差异和已有收据。T14-EVAL 需补齐三场人工判断并由 B 一次确认，然后由项目负责人完成 Phase 3 阶段审查和最终签收。
