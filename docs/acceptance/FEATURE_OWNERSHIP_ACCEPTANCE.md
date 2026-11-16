# 功能负责人分工变更验收（2026-11-16）

本文记录本轮「从固定 A 后端 / B 前端改为一功能一负责人端到端交付」的文档变更范围、实际验证、仍待人工决定的内容与授权边界。规则入口见 [PLAN 现行功能分工原则](../planning/PLAN.md#现行功能分工原则)，决策见 [ADR-026](../decisions/DECISIONS.md#adr-026-功能负责人端到端交付)，当前任务负责人见 [TASKS](../planning/TASKS.md)。

## 1. 变更范围

本轮只修改文档，未修改代码、迁移、配置、部署或测试：

| 文件 | 变更要点 |
| --- | --- |
| [AGENTS.md](../../AGENTS.md) | 总则改为端到端归属；新增「功能负责人分工」小节（一功能一负责人、A 技术支持与风险把关、B 功能完整性、共享状态机/迁移编号/契约单一统筹、答辩按实际功能）；Agent 协作规则第 5 条同步 |
| [PLAN.md](../planning/PLAN.md) | 新增「现行功能分工原则」作为规则入口；风险表、暂停恢复、Phase 3 可并行工作、Phase 3 收尾交接与 Phase 0 历史口径同步 |
| [TASKS.md](../planning/TASKS.md) | 重写「分工、估算与依赖规则」；Phase 3/4 负责人栏改为单一功能负责人 + 协作职责；新增「Phase 3 收尾与交接」；「责任不能转交给 AI」改为按功能归属 |
| [DECISIONS.md](../decisions/DECISIONS.md) | 新增 ADR-026，说明替代的旧分工文字与生效时间（自下一待办任务起）；更新假设 A01 |
| [PRD.md](../product/PRD.md) | 仅更新 AC11 与验收责任说明，产品范围、优先级与轮次均不变 |
| [ARCHITECTURE.md](../architecture/ARCHITECTURE.md) | 补充分层不随分工变化；监控交付责任改为按 TASKS 负责人归属 |
| [PHASE2_SDD_WORKFLOW.md](../planning/PHASE2_SDD_WORKFLOW.md) | 链接新规则；Spec 模板负责人字段改为单一功能负责人；Phase 2 图表标注为历史语境 |
| [Codex团队协作实战教程.md](../guides/Codex团队协作实战教程.md) | 第 8 节职责表标注为示例并补现行做法；文档表与阶段推进段链接新规则 |
| [README.md](../../README.md) | 分工段与文档表加入新入口与简短原则 |
| [docs/README.md](../README.md) | 阅读顺序、目录表与验收导航加入新入口 |

按用户补充要求，功能负责人默认尽可能独立完成需求理解、前后端实现、测试、排错与必要文档；另一成员按需协助，A 的技术支持不构成日常审批门槛。历史证据、已完成贡献描述、任务状态与人时估算保持原样，未重排历史，未改写任何既有验收结论。

## 2. 实际执行的验证

- 文档内容核对：分组检查新增/修改段落与既有表格列数一致，未改变产品范围与 Phase 3 门槛表述。
- 本地链接与锚点：Codex 检查本轮新增或修改的 108 处本地相对链接与 `#锚点`，指向的文件与标题均存在。
- 表格一致性：Phase 3、Phase 4 任务表列数与表头一致；新增「Phase 3 收尾与交接」表的负责人栏与主表一致（T14-STYLE、T15-Core 由 B 收口，T14-EVAL 由 B 负责评测闭环，T15-DEMO-REAL 由 A 作整体负责人、B 负责 B3 子交付，T18 由 B 作整体负责人）。
- `git diff --check`：通过，无空白错误；`git ls-files --unmerged` 为空，变更文档无冲突标记。
- 任务与历史核对：24 项任务的状态和人时估算与基线一致，Phase 0–2 表格中的已完成贡献归属未改写。
- 变更范围：核对本轮变更仅含文档；工作区既有脚本及其他未跟踪文件不属于本次交付；未修改 `backend/`、`frontend/`、`infra/`、`deploy/`、`.github/`。

未执行（本轮不需要，也不代表通过）：后端 `go test/go vet/go build`、前端 typecheck/test/build、数据库迁移与集成测试、浏览器联调、真实模型调用、部署与备份恢复。

## 3. 仍未执行与需人工决定

- **Phase 3 是否结项**：本轮不结项。T14-STYLE 的完整 AC16/B3、T15-Core 的完整 AC12、T15-DEMO-REAL 的真实体验与 T14-EVAL 的 10 场评测仍未完成，Phase 3 门槛与授权不变。
- **V2 与新增功能**：本轮不批准 V2 功能，未确认的小功能仍须逐项询问负责人，不因分工调整自动授权。工作区中的 `docs/product/PRD_V2_DRAFT.md`、`docs/product/PRD_V2_DECISIONS.md`、`docs/research/` 三份调研、`infra/demo/quick-start.ps1`、根 `启动思弈阁 Demo.cmd` 与 `GLOSSARY.md` 均属先前或其他工作的未跟踪改动，本轮不修改、不纳入提交。
- **成员实际归属**：TASKS 中的负责人栏按现有主责做了最小调整，若成员实际投入变化，须在对应任务行更新并记录交接，不在本文另行决定。

## 4. 授权与 Git 边界

- 本轮授权范围：仅修改上述文档并登记本文；允许定义分工规则本身。
- DSH 未执行任何 Git 写操作（未 add、commit、switch、push），未创建分支或标签。
- Codex 已获用户授权，从 `25b8bfbbc882ff32fe1cfa3cc7b32707958811de` 创建并切换到 `codex/feature-ownership`；最终审阅、`git add` 与 Commit 由 Codex 执行，且不推送。
- Codex 已独立复核文档差异，修正主表与收尾表归属、B3 Fake 浏览器与真实模型体验的区分，并补充独立交付原则；实际 Commit 以 Git 记录为准。
- 未执行：真实模型调用、数据库写入、部署、PR 创建或合并。

## 5. 既有未提交改动的保留说明

开工前工作区已存在 `M README.md`、`M docs/README.md`、`M backend/migrations/README.md`。其中根 README 与 docs/README 经 `git hash-object --path` 与 HEAD blob 核对相同（差异仅为换行表示），本轮在其原有内容上追加分工入口，未丢弃原工作区内容；`backend/migrations/README.md` 本轮未触碰。
