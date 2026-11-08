# 思弈阁 Lite 文档导航

本文负责文档入口、阅读顺序与路径迁移对照。产品范围、技术契约、任务进度及验收证据仍由各自文档维护，不在此重复整份状态说明。

## 新成员阅读顺序

1. [根 README](../README.md)：环境准备、配置、启动和本地检查命令。
2. [AGENTS](../AGENTS.md)：协作规则、安全要求及操作授权边界；同时核对本轮明确授权。
3. [PRD](product/PRD.md)：MVP 做什么、不做什么，以及 AC 验收标准。
4. [ARCHITECTURE](architecture/ARCHITECTURE.md)：模块职责、数据关系、状态机及安全约束；区分已实现内容与目标设计。
5. [API](api/API.md) 与 [OpenAPI](api/openapi.yaml)：HTTP 契约、DTO、错误处理和接口实施边界。
6. [PLAN](planning/PLAN.md) 与 [TASKS](planning/TASKS.md)：阶段依赖、任务分工、进度和风险；结合对应验收记录核对状态。
7. [DECISIONS](decisions/DECISIONS.md)：选型理由、决策替代关系和授权增量；实施具体任务前再读相关计划及验收证据。

## 按用途查找

| 目录 | 职责与入口 |
| --- | --- |
| [product/](product/) | 产品需求：[PRD](product/PRD.md) |
| [architecture/](architecture/) | 当前架构与目标设计：[ARCHITECTURE](architecture/ARCHITECTURE.md) |
| [api/](api/) | 接口契约：[API](api/API.md)、[OpenAPI 3.1](api/openapi.yaml) |
| [decisions/](decisions/) | 决策与假设：[DECISIONS](decisions/DECISIONS.md)，本次不拆分 ADR |
| [planning/](planning/) | 阶段与任务：[PLAN](planning/PLAN.md)、[TASKS](planning/TASKS.md)；历史实施计划按原文件名保留 |
| [acceptance/](acceptance/) | 技术验证、审查、人工验收及实施交接记录；保留原证据与各轮语境 |
| [guides/](guides/) | [协作教程](guides/Codex团队协作实战教程.md)与[阶段 0 学习笔记](guides/学习笔记/)；学习资料不能替代当前契约或验收证据 |

## 启动与操作

- 通用启动、配置、测试及构建步骤见[根 README](../README.md#获取与启动)。
- 本地 MySQL 的资源、初始化、连接及持久化核对见 [infra/mysql/README](../infra/mysql/README.md)。
- 迁移入口、固定目标保护、失败与恢复说明见 [backend/migrations/README](../backend/migrations/README.md)。
- T04 一次性冒烟工具的历史约束见[工具 README](../backend/cmd/t04-smoke/README.md)及[验收记录](acceptance/T04_AI_ACCEPTANCE.md#最终人工验收与结项)。工具存在、文档命令或历史授权均不授权再次执行。

模块专属 README 留在模块目录。上述操作说明是查阅入口，执行数据库、真实模型、部署或 Git 写操作仍须符合本轮明确授权。

## 历史计划与验收证据

- 阶段 0 / PB：[INIT_ACCEPTANCE](acceptance/INIT_ACCEPTANCE.md)。
- T02：[环境](acceptance/T02_MYSQL_ENV_ACCEPTANCE.md)、[后端连接](acceptance/T02_DATABASE_CONNECTION_ACCEPTANCE.md)、[dotenv](acceptance/T02_DOTENV_ACCEPTANCE.md)、[配置修复](acceptance/T02_CONFIG_FIX_ACCEPTANCE.md)、[初始迁移](acceptance/T02_MIGRATIONS_ACCEPTANCE.md)、[种子与持久化](acceptance/T02_SEEDS_PERSISTENCE_ACCEPTANCE.md)。
- T03：[认证验收记录](acceptance/T03_AUTH_ACCEPTANCE.md)，后续结论在[独立审查与人工验收结果](acceptance/T03_AUTH_ACCEPTANCE.md#独立审查与人工验收结果)。
- T04：[AI 验收记录](acceptance/T04_AI_ACCEPTANCE.md)、[实施交接](acceptance/T04_AI_IMPLEMENTATION.md)及[实施计划](planning/T04_AI_PLAN.md)。
- 历史实施计划：[T02 连接](planning/2026-11-08-t02-database-connection.md)、[T02 迁移](planning/2026-11-08-t02-migrations.md)、[T03 认证](planning/2026-11-08-t03-auth.md)。原 `superpowers/plans/` 仅承担计划存放职责，本次归并到 `planning/`，文件名保留。

历史记录不等于当前状态，也不构成新增操作授权。应按明确的后续授权、决策替代关系和验收结论核对，不能仅凭文件日期、文件存在或早期段落判断任务完成、阻塞解除或可执行某项操作。本次整理不新增业务验收结论。

## 整理时发现的状态差异与保留项

2026 年 11 月 8 日整理时核对到以下差异，登记于此；本次不改写既有授权原文、历史状态或验收证据。

| 差异或保留项 | 查阅依据与处理 |
| --- | --- |
| 根 README、AGENTS、PLAN、ARCHITECTURE 及 TASKS 的部分 T03 说明仍写待独立审查或待人工验收 | [T03 后续验收章节](acceptance/T03_AUTH_ACCEPTANCE.md#独立审查与人工验收结果)已记载有条件独立审查通过、负责人人工验收及接受风险延期；接受延期不代表问题已修复。原说明保留，阅读时核对后续证据 |
| AGENTS、PLAN、ARCHITECTURE 和部分 T04 计划、交接内容仍为未启动、阻塞或待验收快照 | [T04 最终人工验收与结项](acceptance/T04_AI_ACCEPTANCE.md#最终人工验收与结项)及 [TASKS](planning/TASKS.md)已登记后续结论；本次仅指向既有证据，不重新判定结项 |
| T07-COST-01 的供应商硬预算缺口仍保留 | [风险登记与关闭条件](planning/TASKS.md#t07-cost-01-供应商硬预算缺失)明确其仍未关闭，T04 结项不解除公网开放阻塞 |
| OpenAPI 的说明性来源字段仍使用原路径 | 按本次“OpenAPI 只移动、内容不变”的要求保留 `x-contract-source: docs/API.md` 与 4 处 `source: docs/ARCHITECTURE.md#phase-2-api-草案`。对应入口现为 [API](api/API.md) 与 [Phase 2 草案](architecture/ARCHITECTURE.md#phase-2-api-草案)；这些旧路径不作为有效文件链接，内部 `$ref` 不受移动影响 |

## 本次路径迁移对照

以下旧路径仅用于追溯，不是仍存在的文件入口。除新增本文外，原文件名全部保留；文档内的运行命令仅随迁移修复文档路径，不改变执行目录或命令行为。`backend/`、`frontend/`、`infra/` 中的文件均未移动。

| 原路径 | 新路径 |
| --- | --- |
| `docs/PRD.md` | [docs/product/PRD.md](product/PRD.md) |
| `docs/ARCHITECTURE.md` | [docs/architecture/ARCHITECTURE.md](architecture/ARCHITECTURE.md) |
| `docs/API.md` | [docs/api/API.md](api/API.md) |
| `docs/openapi.yaml` | [docs/api/openapi.yaml](api/openapi.yaml) |
| `docs/DECISIONS.md` | [docs/decisions/DECISIONS.md](decisions/DECISIONS.md) |
| `docs/PLAN.md` | [docs/planning/PLAN.md](planning/PLAN.md) |
| `docs/TASKS.md` | [docs/planning/TASKS.md](planning/TASKS.md) |
| `docs/T04_AI_PLAN.md` | [docs/planning/T04_AI_PLAN.md](planning/T04_AI_PLAN.md) |
| `docs/superpowers/plans/2026-11-08-t02-database-connection.md` | [docs/planning/2026-11-08-t02-database-connection.md](planning/2026-11-08-t02-database-connection.md) |
| `docs/superpowers/plans/2026-11-08-t02-migrations.md` | [docs/planning/2026-11-08-t02-migrations.md](planning/2026-11-08-t02-migrations.md) |
| `docs/superpowers/plans/2026-11-08-t03-auth.md` | [docs/planning/2026-11-08-t03-auth.md](planning/2026-11-08-t03-auth.md) |
| `docs/INIT_ACCEPTANCE.md` | [docs/acceptance/INIT_ACCEPTANCE.md](acceptance/INIT_ACCEPTANCE.md) |
| `docs/T02_MYSQL_ENV_ACCEPTANCE.md` | [docs/acceptance/T02_MYSQL_ENV_ACCEPTANCE.md](acceptance/T02_MYSQL_ENV_ACCEPTANCE.md) |
| `docs/T02_DATABASE_CONNECTION_ACCEPTANCE.md` | [docs/acceptance/T02_DATABASE_CONNECTION_ACCEPTANCE.md](acceptance/T02_DATABASE_CONNECTION_ACCEPTANCE.md) |
| `docs/T02_DOTENV_ACCEPTANCE.md` | [docs/acceptance/T02_DOTENV_ACCEPTANCE.md](acceptance/T02_DOTENV_ACCEPTANCE.md) |
| `docs/T02_CONFIG_FIX_ACCEPTANCE.md` | [docs/acceptance/T02_CONFIG_FIX_ACCEPTANCE.md](acceptance/T02_CONFIG_FIX_ACCEPTANCE.md) |
| `docs/T02_MIGRATIONS_ACCEPTANCE.md` | [docs/acceptance/T02_MIGRATIONS_ACCEPTANCE.md](acceptance/T02_MIGRATIONS_ACCEPTANCE.md) |
| `docs/T02_SEEDS_PERSISTENCE_ACCEPTANCE.md` | [docs/acceptance/T02_SEEDS_PERSISTENCE_ACCEPTANCE.md](acceptance/T02_SEEDS_PERSISTENCE_ACCEPTANCE.md) |
| `docs/T03_AUTH_ACCEPTANCE.md` | [docs/acceptance/T03_AUTH_ACCEPTANCE.md](acceptance/T03_AUTH_ACCEPTANCE.md) |
| `docs/T04_AI_ACCEPTANCE.md` | [docs/acceptance/T04_AI_ACCEPTANCE.md](acceptance/T04_AI_ACCEPTANCE.md) |
| `docs/T04_AI_IMPLEMENTATION.md` | [docs/acceptance/T04_AI_IMPLEMENTATION.md](acceptance/T04_AI_IMPLEMENTATION.md) |
| `docs/Codex团队协作实战教程.md` | [docs/guides/Codex团队协作实战教程.md](guides/Codex团队协作实战教程.md) |
| `docs/学习笔记/思弈阁阶段0核心源码复习笔记（11.2~11.5）.md` | [docs/guides/学习笔记/思弈阁阶段0核心源码复习笔记（11.2~11.5）.md](guides/学习笔记/思弈阁阶段0核心源码复习笔记（11.2~11.5）.md) |
