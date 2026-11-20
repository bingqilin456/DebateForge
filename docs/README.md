# 思弈阁 Lite 文档导航

本文负责文档入口与阅读顺序。产品范围、技术契约、任务进度及验收证据仍由各自文档维护，不在此重复整份状态说明。

## 权威来源与冲突处理

不同问题有不同的权威来源；**没有「最新文档」或「代码实际行为」单方面压倒一切的规则**，遇到不一致时按下表定位该问题的权威载体，再判断是实现缺陷、文档滞后还是待决事项。

| 问题 | 权威来源 | 说明 |
| --- | --- | --- |
| 产品要做什么、AC 编号 | [PRD V2](product/PRD.md)（当前规格，**未实现**）；V1 的 F01–F14 / AC01–AC17 见 [PRD V1 冻结基线](product/PRD_V1_BASELINE.md) | V1/Phase 3 验收引用 V1 基线，两套编号不得混用；讨论 V2 需求与 AC 不构成开发授权 |
| 页面/面板、字段、交互与状态恢复 | [V2 页面设计与交互细化](product/PRD_V2_UI_DESIGN.md) | **细化文档，不是事实源**；行为与 AC 冲突时以 PRD 为准；文中逐条标注已确认、补充草案、设计建议与待决 |
| HTTP 请求/响应、错误码 | [API](api/API.md) 与 [OpenAPI](api/openapi.yaml) | 两者应一致；与代码不符时报告契约或实现偏差，核对已批准的基线与 ADR 后纠正，不擅自改动公开行为 |
| 架构与技术约束（分层、状态机、安全边界） | [ARCHITECTURE](architecture/ARCHITECTURE.md) | 描述已实现或已定稿约束；V2 目标单独标注为待实施 |
| 当前状态、负责人、进度与风险 | [TASKS](planning/TASKS.md) | 唯一状态入口；其他文档不复写状态快照 |
| 长期决策与**替代范围** | [DECISIONS](decisions/DECISIONS.md) | 决策被替代时以新 ADR 的替代范围为准，历史原文保留 |
| 已执行验证与验收结论 | 本页[历史计划与验收证据](#历史计划与验收证据)所链各记录 | 记录当时授权与证据，不构成当前操作授权 |
| 代码实际行为 | 仓库源码与测试 | 用于发现文档偏差，不能自动改写需求、契约或验收基线 |

冲突处理：一般编辑冲突在当轮授权内直接纠错；产品、契约、安全或阶段待决的冲突须指出具体决策点与受影响文档，不自行扩大范围，可先继续无关工作。

## 新成员阅读顺序

1. [根 README](../README.md)：环境准备、配置、启动和本地检查命令。
2. [AGENTS](../AGENTS.md)：协作规则、安全要求及操作授权边界；同时核对本轮明确授权。
3. [PRD V2](product/PRD.md)：当前产品规格（V2 做什么、不做什么、AC-V2）——**需求已确认但未实现**；V1 只读快照见 [PRD V1 冻结基线](product/PRD_V1_BASELINE.md)。
4. [V2 页面设计与交互细化](product/PRD_V2_UI_DESIGN.md)：页面/面板清单、入口、字段、操作、跨页数据与桌面/移动组织、共通失败态。**只做交互细化，不是产品事实源**；含待决项与设计建议，不构成开发授权。
5. [ARCHITECTURE](architecture/ARCHITECTURE.md)：模块职责、数据关系、V1 状态机及安全约束；区分已实现内容、V2 目标设计与待评估项。
6. [API](api/API.md) 与 [OpenAPI](api/openapi.yaml)：HTTP 契约、DTO、错误处理和接口实施边界；V2 尚无接口契约。
7. [PLAN](planning/PLAN.md)、[TASKS](planning/TASKS.md) 与 [Phase 2 轻量 SDD 工作流](planning/PHASE2_SDD_WORKFLOW.md)：PLAN 管阶段依赖、门槛与[现行功能分工原则](planning/PLAN.md#现行功能分工原则)，TASKS 管当前任务的功能负责人、进度和风险，Phase 2 按轻量 SDD 推进。
8. [DECISIONS](decisions/DECISIONS.md)：选型理由、长期决策、假设及决策替代关系；实施具体任务前再读相关计划及验收证据。

**版本边界（2026-11-17）**：产品 V2（[PRD V2](product/PRD.md)）需求已确认，但**未实现、未验收、未获开发阶段授权**；其逐项确认过程见 [需求确认记录](product/PRD_V2_DECISIONS.md)，相对成本、风险、依赖顺序与待评估项见 [V2 技术评估](planning/PRD_V2_TECHNICAL_ASSESSMENT.md)（含已知的生成额度不匹配）。V1 代码与 HTTP 契约、Phase 3 验收基线、`standard_4`/`quick_3` 旧含义与流派历史数据在本轮重构中不改写（范围限定，非永久禁止）。三类「V2」须分名：产品 V2、`phase3-summary-v2`（总结 `schema_version=2` 的 0–100 四维，T14-REPORT，未开始）、`v2-training-reference`（产品 V2 的 1–4 档，未实施）；依据见 [ADR-027](decisions/DECISIONS.md#adr-027-v2-需求基线与版本边界)。

分工规则入口为 [PLAN 现行功能分工原则](planning/PLAN.md#现行功能分工原则)，决策见 [ADR-026](decisions/DECISIONS.md#adr-026-功能负责人端到端交付)：自 2026-11-16 起一功能一负责人端到端交付，可跨前后端，任务负责人以 [TASKS](planning/TASKS.md) 为准。

Phase 3 优先级、自主流派和查询恢复契约见 [ADR-023](decisions/DECISIONS.md#adr-023-phase-3-优先级流派选择与查询恢复契约)，真实模型浏览器 Demo 的交付目标及阶段边界见 [ADR-024](decisions/DECISIONS.md#adr-024-phase-3-交付真实模型可体验-demo)。当前实现、已执行验证、待负责人确认及阶段结项统一见 [TASKS](planning/TASKS.md)。

真实 Demo 的产品标准见 [PRD V1 冻结基线](product/PRD_V1_BASELINE.md#phase-3-真实模型可体验-demop0-交付目标尚未实现)；模型、体验窗口及费用/调用上限须在执行轮次单独明确。文档目标和历史授权不构成当前真实调用授权，公网正式交付仍属 Phase 4。

## 按用途查找

| 目录 | 职责与入口 |
| --- | --- |
| [product/](product/) | 产品需求：[PRD V2](product/PRD.md)（当前规格，未实现）、[页面设计与交互细化](product/PRD_V2_UI_DESIGN.md)（细化文档，非事实源）、[PRD V1 冻结基线](product/PRD_V1_BASELINE.md)、[需求确认记录](product/PRD_V2_DECISIONS.md) |
| [architecture/](architecture/) | 当前架构与目标设计：[ARCHITECTURE](architecture/ARCHITECTURE.md)（含 V1 状态机与 [V2 目标状态模型](architecture/ARCHITECTURE.md#v2-目标状态模型与服务端权威阶段进度待实施)） |
| [api/](api/) | 接口契约：[API](api/API.md)、[OpenAPI 3.1](api/openapi.yaml) |
| [decisions/](decisions/) | 决策与假设：[DECISIONS](decisions/DECISIONS.md)，本次不拆分 ADR |
| [planning/](planning/) | 阶段与任务：[PLAN](planning/PLAN.md)（含[现行功能分工原则](planning/PLAN.md#现行功能分工原则)）、[TASKS](planning/TASKS.md)、[Phase 2 轻量 SDD 工作流](planning/PHASE2_SDD_WORKFLOW.md)、[V2 技术评估](planning/PRD_V2_TECHNICAL_ASSESSMENT.md)；已完成任务的旧计划已清理，按下方正式验收记录追溯 |
| [acceptance/](acceptance/) | 技术验证、审查、人工验收及实施交接记录；保留原证据与各轮语境。分工变更见[功能负责人分工变更验收](acceptance/FEATURE_OWNERSHIP_ACCEPTANCE.md)，V2 重构自测见[记录](acceptance/PRD_V2_RESTRUCTURE_ACCEPTANCE.md)，V2 页面设计补充自测见[记录](acceptance/PRD_V2_UI_SUPPLEMENT_ACCEPTANCE.md)，根协作规则与本轮局部规则优化见[记录](acceptance/AGENT_RULES_OPTIMIZATION_ACCEPTANCE.md) |
| [guides/](guides/) | [协作教程](guides/Codex团队协作实战教程.md)与[阶段 0 学习笔记](guides/学习笔记/)；学习资料不能替代当前契约或验收证据 |

## 启动与操作

- 通用启动、配置、测试及构建步骤见[根 README](../README.md#获取与启动)。
- 本地 MySQL 的资源、初始化、连接及持久化核对见 [infra/mysql/README](../infra/mysql/README.md)。
- 迁移入口、固定目标保护、失败与恢复说明见 [backend/migrations/README](../backend/migrations/README.md)。
- T04 一次性冒烟工具已随 `cmd/t04-smoke` 删除（入口早已固定返回 `BLOCKED`，历史约束与执行口径见[验收记录](acceptance/T04_AI_ACCEPTANCE.md#最终人工验收与结项)）。历史授权与文档命令均不授权再次执行。

模块专属 README 留在模块目录。上述操作说明是查阅入口，执行数据库、真实模型、部署或 Git 写操作仍须符合本轮明确授权。

## 历史计划与验收证据

**2026-11-17 清理说明**：已删除 9 份已完成任务的旧计划/临时进度和 3 份已被后续验收覆盖的早期交接记录。正式验收证据、尚未关闭风险、Phase 3 在途 Spec 与 V2 规划保留；Phase 2 SDD 文件仍含现行工作流方法，继续保留。历史文件可从 Git `91a4056d72b751473657a8329e45b86b5f2f4b33` 查询；删除不改变任务状态、历史贡献或授权边界。

- main 强制质量门禁：[规则 JSON、实际 CI 名称、触发核查及待确认远端配置](acceptance/MAIN_REQUIRED_CHECKS_ACCEPTANCE.md)；配置准备不代表已启用远端保护。

- Phase 3 B1 指定提交整合：[合入、契约接线与受影响检查](acceptance/PHASE3_B1_INTEGRATION_ACCEPTANCE.md)、[B 原始实测](acceptance/T14_STYLE_FRONTEND_ACCEPTANCE.md)。记录中“B1 故障待补、B2 未交付”是该轮历史状态，后续补测与整合见 [B2 整合记录](acceptance/PHASE3_B2_INTEGRATION_ACCEPTANCE.md)。
- B1 策略故障补测：[A 本地 Fake 装配交接](acceptance/P3_A_STRATEGY_FAKE_ACCEPTANCE.md)、[独占环境与运行说明](../infra/strategy-fake/README.md)；固定 test 后端验证之后的浏览器补测见 [B2 整合记录](acceptance/PHASE3_B2_INTEGRATION_ACCEPTANCE.md)。

- T15-DEMO-REAL 无收费准备：[Spec](planning/T15_DEMO_PREP_SPEC.md)、[费用/Fake 环境验证及当轮交接](acceptance/T15_DEMO_PREP_ACCEPTANCE.md)、[本地启动/配置/停止](../infra/demo/README.md)。其中 B1/B2 等待项保留原轮次语境，后续整合见 B2 记录；A4 累计矩阵与 A5 启动前置条件见 [B2/B3 签收记录](acceptance/PHASE3_B2_B3_SIGNOFF.md)，真实体验状态查 TASKS。
- 真实模型体验与评测执行（2026-11-17）：[两次授权、25 次预留、4 场实际结果、两次模型失败、一次补充授权与操作失误](acceptance/P3_REAL_MODEL_EXECUTION_2026-11-17.md)；授权参数见[调用授权登记](acceptance/P3_EVAL_DEMO_AUTHORIZATION.md)。4 场中 3 场完整，按 [ADR-029](decisions/DECISIONS.md#adr-029-phase-3-真实质量评测门槛由-5-场降为-3-场完整场次) 的 3 场门槛已满足场次条件，但 `data` 流派无合格样本、B 的人工结论未提交，不视为评测已通过。

- T15-Core：[后端专项与 B 交接](acceptance/T15_CORE_BACKEND_ACCEPTANCE.md)、[实施计划](planning/T15_CORE_BACKEND_PLAN.md)、[本地 Fake 三链路入口](../infra/sse/README.md)。

- 阶段 0 / PB：[INIT_ACCEPTANCE](acceptance/INIT_ACCEPTANCE.md)。
- T02：[环境](acceptance/T02_MYSQL_ENV_ACCEPTANCE.md)、[后端连接](acceptance/T02_DATABASE_CONNECTION_ACCEPTANCE.md)、[dotenv](acceptance/T02_DOTENV_ACCEPTANCE.md)、[配置修复](acceptance/T02_CONFIG_FIX_ACCEPTANCE.md)、[初始迁移](acceptance/T02_MIGRATIONS_ACCEPTANCE.md)、[种子与持久化](acceptance/T02_SEEDS_PERSISTENCE_ACCEPTANCE.md)。
- T03：[认证验收记录](acceptance/T03_AUTH_ACCEPTANCE.md)，后续结论在[独立审查与人工验收结果](acceptance/T03_AUTH_ACCEPTANCE.md#独立审查与人工验收结果)。
- T04：[AI 最终验收记录](acceptance/T04_AI_ACCEPTANCE.md)。
- T05：[首轮最终验收记录](acceptance/T05_FIRST_TURN_ACCEPTANCE.md)；结项范围限于无收费实现，真实模型链路未验收。
- T06：[正式入口与首轮联调最终验收](acceptance/T06_COMPLETION_ACCEPTANCE.md)。
- T07 部署准备见 [配置与运行清单](../deploy/README.md) 和 [验收记录](acceptance/T07_DEPLOYMENT_PREPARATION_ACCEPTANCE.md)。
- T16 Phase 4 运维演练见[执行计划](../deploy/T16_PLAN.md)、[健康与备份新鲜度检查工具](../deploy/ops/health/README.md)和[验证记录](acceptance/T16_OPERATIONS_VALIDATION.md)；记录中的未验证项仍须现场补证。
- T09 三轮状态机与失败恢复（含 T11 后端历史分页）见 [T09 验收记录](acceptance/T09_THREE_TURNS_RECOVERY_ACCEPTANCE.md)。
- T10 结构化总结与重试见 [T10 验收记录](acceptance/T10_SUMMARY_RETRY_ACCEPTANCE.md)。
- T12 核心辩论页面联调与 Phase 2（AC01–AC08）Fake 全链路验证见 [T12 验收记录](acceptance/T12_DEBATE_INTEGRATION_ACCEPTANCE.md)。
- Phase 2 最终整合（含 T08/T11 独立页面、赛制兼容、Chromium 与真实 Fake 联调）见 [最终验收记录](acceptance/PHASE2_FINAL_ACCEPTANCE.md)；后者补充并取代早先仅后端核心闭环的结项范围。
- Phase 3 首批：[A 安全回归矩阵、T15 样例与 Demo 最小方案](planning/PHASE3_A_INTEGRATION_SPEC.md)、[T14-STYLE 最小 Spec](planning/T14_STYLE_SPEC.md)、[T14-STYLE 后端专项与 B 交接](acceptance/T14_STYLE_BACKEND_ACCEPTANCE.md)；后续前端与故障验证分别见 B1/B2 记录，不回写早期证据为完整 AC16 通过。
- Phase 3 B1 补测与 B2 SSE：[B 原始交付](acceptance/B2_SSE_FRONTEND_ACCEPTANCE.md)、[指定 SHA 整合、浏览器 Fake 结果与 B3 启动交接](acceptance/PHASE3_B2_INTEGRATION_ACCEPTANCE.md)；实现与实测不替代负责人签收及阶段结项，当前待确认项查 TASKS。
- Phase 3 B3：[浏览器补验结果、证据与边界](acceptance/PHASE3_B3_BROWSER_ACCEPTANCE.md)、[操作说明与评测准备](guides/PHASE3_BROWSER_AND_EVAL_GUIDE.md)。固定受测基线 134b845，交付 SHA 为 d22487f；新增 Fake/Nginx 浏览器 2/2 与最终只读启停 1/1 通过。该批次未调用真实模型；后续真实评测见结项记录。
- Phase 3 B侧收尾（含2026-11-20人工评读追加）：[T14-EVAL三场记录、AC12/AC16复用范围与边界](acceptance/P3_B_CURRENT_BASELINE_REVIEW_2026-11-17.md)。历史初始判断保留；最新追加记录 B 已评读无异议，负责人确认会话23计入合格。
- Phase 3 B2/B3 正式签收：[签收结论、累计验收矩阵(A4)、启动前置条件(A5)与复现边界](acceptance/PHASE3_B2_B3_SIGNOFF.md)。签收范围限定 134b845 的固定 test/Fake 无收费工作，不含真实 Demo、10 场评测与阶段结项。
- Phase 3 收尾状态（2026-11-20 结项）：[五项必要 P0、受测 SHA 与历史证据、已知风险及未验证项](acceptance/PHASE3_CLOSEOUT_2026-11-19.md)。T14-EVAL 已按 B 人工评读无异议及负责人确认 23 计入合格而完成；T07-COST-01、Phase 4 与产品 V2 仍分别保持原状态。
- 确认废弃代码清理：[删除依据、保留原因与验证](acceptance/REMOVE_UNUSED_CODE_ACCEPTANCE.md)；单项结果待负责人确认，不改变阶段验收状态。
- 功能负责人分工变更（2026-11-16）：[本轮范围、验证与授权边界](acceptance/FEATURE_OWNERSHIP_ACCEPTANCE.md)；仅改文档，不代替 Phase 3 结项或 V2 决策。
- 产品 V2 文档重构（2026-11-17）：[自测、变更范围与未执行项](acceptance/PRD_V2_RESTRUCTURE_ACCEPTANCE.md)；仅改文档，不含代码/迁移/真实调用，不代替 Phase 3 结项或 V2 开发授权。
- 产品 V2 页面设计与自带 Key 需求补充（2026-11-17）：[页面文档、PRD §19 草案、技术评估 TE-14～18 与自测范围](acceptance/PRD_V2_UI_SUPPLEMENT_ACCEPTANCE.md)；仅改文档，新增 AC-V2-32～38 为待确认草案，不代替负责人签收或 V2 开发授权。
- 创建会话流程统一：[入口取舍、本地验证与未执行项](acceptance/UNIFY_DEBATE_CREATION_ACCEPTANCE.md)；技术验证完成，待负责人确认。
- 前端常量与基础样式收敛：[范围、边界测试与桌面/窄屏验证](acceptance/FRONTEND_SHARED_VALUES_ACCEPTANCE.md)；本地技术验证完成，待负责人确认。
- 根协作规则与局部规则优化（2026-11-17）：[文件范围、定向验证、未执行项与剩余风险](acceptance/AGENT_RULES_OPTIMIZATION_ACCEPTANCE.md)；仅改文档与规则，不含代码/迁移/真实调用，不代替任何阶段结论。
- 真实模型调用授权登记（2026-11-17）：[参数、5 场覆盖面与执行清单](acceptance/P3_EVAL_DEMO_AUTHORIZATION.md)；已于 2026-11-17 实际执行，完成 4 场（3 场完整）。场次门槛见 [ADR-029](decisions/DECISIONS.md#adr-029-phase-3-真实质量评测门槛由-5-场降为-3-场完整场次)。

- T13-Lite（2026-11-17）：[正式技术验收结论](acceptance/PHASE3_T13_LITE_ACCEPTANCE.md)，PR #29 的 `9ef7a54` 四项 CI 成功，技术验收通过且负责人已正式签收，PR #29 已合入 main；以下为起草时的历史边界：[历史已复验项、平台与 CI 边界](acceptance/PHASE3_T13_LITE_SIGNOFF_DRAFT.md)。草案登记历史累计回归，说明 Windows 跳过 Linux 持久化与 Nginx 专项、四项 CI 属历史证据且当前 HEAD 未核实；「无新增 P0/P1 缺陷」只限已实际执行的回归项，跳过与未执行项不在结论内，也不覆盖真实模型已知失败。
- AC17 A4/A5 文档确认（起草轮次历史记录）：[累计矩阵、启动前置条件与未登记项](acceptance/PHASE3_AC17_A4_A5_SIGNOFF_DRAFT.md)。草案以会话 22 起草 A4/A5，逐项引用执行记录；只登记「四轮与总结完整」这一事实，SSE 流式观察与刷新重登本轮未执行，浏览器具体动作、镜像 SHA、Prompt 版本未登记故不补造。`data` 无合格样本、账本修复未在真实调用验证、B 的人工评测未完成，不替代 T14-EVAL 或阶段结项。

历史记录保留当时的授权和证据。当前状态以 [TASKS](planning/TASKS.md) 为入口，操作以负责人本轮明确授权为准。

- AC17 真实浏览器补验（2026-11-17）：[会话 26 的固定版本、授权、四轮 SSE、总结、刷新重登与停止记录](acceptance/PHASE3_AC17_BROWSER_ACCEPTANCE_2026-11-17.md)，[脱敏元数据收据](acceptance/evidence/p3-ac17-20261117/execution.json)；已由负责人正式签收固定基线限定范围，不代替 B 的人工评测与 Phase 3 结项。
