# T14-STYLE 最小任务 Spec

日期：2026-11-14。负责人 A：接口、校验、Prompt、存储；B：流派选择、策略卡展示与交互测试。本文为 A1 定稿契约；A2 后端已实现，OpenAPI 已晋升为 `implemented`，专项证据及交接见 [后端验收记录](../acceptance/T14_STYLE_BACKEND_ACCEPTANCE.md)。依据 [PRD AC16](../product/PRD.md)、[ADR-023](../decisions/DECISIONS.md#adr-023-phase-3-优先级流派选择与查询恢复契约)，A1 仅交付 Spec；A2 延续同分支实现后端，不修改 B 前端。完整 AC16 仍待 B 联调和负责人验收。

## 输入、触发与边界

- 创建请求 `POST /api/v1/debates` 增补可选 `ai_style`，只接受 `logical/aggressive/empathic/data`；仅省略默认 `logical`，null、空串、非字符串、大小写不符和未知值均为 400，不写会话、不调用模型。`mode` 仍由服务端固定 `standard_4`；历史 `quick_3` 与既有流派不改。创建后不提供修改流派接口。
- 创建成功后展示「生成赛前策略」按钮，由用户点击才 POST；进入详情、刷新、重登、历史页及创建成功的回调均只 GET，不隐式生成。策略可跳过，不能成为第一轮的强制前置。
- 策略帮助**用户所持立场**破题；保存的 `ai_style` 描述陪练对手的风格和可能施压方式，不把策略写成 AI 相反立场的开场白。只使用辩题快照、双方立场、保存的流派及 mode，不读取其他用户数据，不执行工具或声称已检索事实。
- 首次生成与失败重试只允许尚未存在任何 turn 的会话（包括 generating/failed turn 也算已开始）；否则 409 `round_conflict`。已完成策略始终可重读/重放，已有 generating 返回 202。会话锁下检查并认领，与第一轮认领串行，避免检查后并发插入 turn 的竞态。策略生成在途允许用户开始辩论，策略不推进或阻塞轮次；模型并发槽仍可能使发送暂时返回 429。

## HTTP 契约

路径为 `/api/v1/debates/{id}/strategy`，单会话最多一份成功策略。无独立策略 ID、无流式策略、无新 retry 路径。接口信封沿用 `data/request_id` 与 `error/request_id`，ID 字符串、时间 UTC，正文 32 KiB 上限。

| 请求 | 输入 | 输出及副作用 |
| --- | --- | --- |
| GET | 路径会话 ID，无正文 | 200 当前状态；无记录为 `not_started`；绝不认领、调用模型或消费生成额度 |
| POST 首次 | `{"retry":false}`（也允许 `{}`） | 首次认领 version=1；成功 201；失败为受控 502/503/504；认领后的失败计一次额度 |
| POST 显式重试 | `{"retry":true,"expected_attempt_version":1}` | 仅 failed 且查询版本匹配，原子 version+1；成功 200；复用服务端快照，不接受新 topic/style/content |
| POST 重放 | 相同会话资源 | completed 为 200 原结果，generating 为 202 状态；failed + retry=false 为 200 失败状态；均不调用模型、不消费每日额度，仍受分钟防刷 |

`retry` 必须为布尔；`retry=true` 必须携带正整数版本；`retry=false` 不允许携带版本。not_started + retry=true 为 400。未知输入字段拒绝 400（本策略接口的严格输入约束，不顺带修改旧接口）。failed + retry=true 的版本冲突为 409 `attempt_version_conflict`，返回查询地址与当前版本；客户端只 GET，不自动换版本重发。completed/generating 的合法重试请求按上表重放，不产生第二次调用。

GET 未开始：

```json
{"data":{"status":"not_started","attempt_version":null},"request_id":"req-strategy-get"}
```

POST/GET 成功（schema_version 与 prompt_version 各自独立）：

```json
{"data":{"status":"completed","attempt_version":1,"schema_version":1,"prompt_version":"strategy-v1","ai_style":"aggressive","content":{"thesis":"开放式考核应兼顾应用能力与评价公平。","arguments":["用统一评分标准评估真实任务成果。"],"risks":["预先回应评分一致性与代做风险。"]}},"request_id":"req-strategy-complete"}
```

GET failed（POST retry=false 同形）；generating 只含 status/attempt_version：

```json
{"data":{"status":"failed","attempt_version":2,"error_code":"model_timeout"},"request_id":"req-strategy-failed"}
```

POST 已确认失败或版本冲突的 details（提交结果不明省略 details，先 GET）：

```json
{"error":{"code":"attempt_version_conflict","message":"请求未完成，请查看会话状态后再操作","details":{"status":"failed","attempt_version":2,"query_url":"/api/v1/debates/123/strategy"}},"request_id":"req-strategy-conflict"}
```

## 权限、额度与持久化

- GET 要求 Session；POST 同时要求精确 Origin、Session、CSRF。不存在与越权统一 404；认证/CSRF 在业务认领前完成。查询或失败响应不得泄露题目、内容、供应商原始错误或凭据。
- 复用 `debate.Gate` 同一实例：6 次/分钟、20 次/UTC 日、单用户并发 1、单进程全局 2。策略、普通轮次、流式轮次、轮次重试、总结共享额度，不新增限流体系。每日额度仅新认领消耗，失败也计；确定回滚不计，不确定提交保守计一次。费用账本另在出网前持久预留，进程内 Gate 不能充当计费账本。
- 每次显式新尝试最多一次模型调用；禁止 SDK 自动重试、模型修复 JSON 或 fallback。disabled 返回受控不可用；Fake 支持结构化成功、超时、非法结构与取消。真实模式仍要求另外授权并具备费用控制。
- 沿用 60 秒请求、45 秒模型、3 秒短事务、90 秒租约；启动及每 30 秒扫描过期 generating，按状态和版本置 failed。失败/取消保留版本，不推进 completed_rounds、不写 messages，不混入总结。迟到输出须 status/version 条件写回，不能覆盖新尝试。
- 新增单表 `debate_strategies`（已核对并新增 `00013_create_debate_strategies.sql`，不修改历史迁移；后续编号从 00014 起再次核对）：唯一 debate_id、status、attempt_version、lease_expires_at、error_code、schema_version、prompt_version、model_name、content_json、生成用量及 UTC 时间。owner 经 debates 校验；无需复制 user_id。围绕会话行加锁、认领事务外调用、结果事务内提交。已成功结果不可再生成或因 Prompt 升版自动重写；历史重读返回保存版本。
- 输出 schema 1 固定三键：`thesis` 为 1–300 Unicode 码点非空白字符串；`arguments/risks` 各 1–3 项，每项同上；完整 JSON ≤8 KiB，拒绝额外键、空白、错误类型和无效 UTF-8。前端逐字段转义展示，不渲染 HTML/Markdown 原文。无效输出变 failed，不保存成功内容。
- 策略首版 Prompt 为 `strategy-v1`；轮次当前为 `debate-v1`，总结为 `summary-v1`。三者分别版本化，修改措辞/结构时升级对应版本；模型名与版本在服务端元数据可追溯，不由浏览器指定。对手流派会影响策略中的应对风险；轮次继续从会话行读取流派。

## 失败路径与最小验收矩阵

| 用例 | 必须观察的结果 | 负责人 |
| --- | --- | --- |
| 四种流派、省略与历史重读 | 精确持久化；省略 logical；刷新重登不变；Prompt 取保存值 | A API/MySQL，B 表单/详情 |
| null/空串/未知/大写/数值/数组 | 400，无会话、无模型/额度副作用；创建全分支模型调用数为零 | A |
| 跳过策略直接发送 | 正常进入第 1 轮；不后台补调策略 | A/B |
| GET/刷新/历史进入 | 只有查询；not_started 不自动 POST | B |
| 无 Session/无 CSRF/错 Origin/他人会话 | 401/403/404，无策略认领与模型调用；所有权覆盖 GET/POST | A |
| 并发首次 POST/与首轮抢占 | 策略只认领一次，其余 202/重放；先有 turn 则拒绝新认领 | A MySQL |
| 失败、超时、非法 JSON/长度、取消 | failed、可查询；无伪造成功/自动修复，轮次及消息不变 | A，B 失败展示 |
| 显式 retry + 再失败 + 旧版本重发 | 版本递增一次；旧版 409，无额外模型调用；B 只刷新 | A/B |
| 崩溃/租约/迟到结果 | 恢复为 failed；旧输出不能覆盖新版本；成功后重读不重调 | A MySQL |
| Gate/费用准入拒绝 | 429 带 Retry-After 或受控不可用；无隐式重试；共享额度与并发可测 | A |

实施顺序：A 先补输入与持久化/Fake 接口，B 可按本 Spec 准备表单、状态展示及隔离响应测试；Fake 联调可使用本分支接口；真实调用仍须独立授权与费用装配。前后端按适用检查验收，AC16 通过后才能标记功能完成。P1 战报/谬误/双 AI 不纳入此任务。
