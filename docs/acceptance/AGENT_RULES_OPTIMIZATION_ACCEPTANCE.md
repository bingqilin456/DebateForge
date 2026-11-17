# 协作规则与局部规则优化验收记录

日期：2026-11-17。分支 `codex/prd-v2-docs`，基线 `ee664f53dfbd78332161c3caa6390efebaed5f76`。本轮由 DSH 实现和自测，Codex 核对实际 diff、修正措辞并复验；仅修改文档与规则，不改变产品行为、既有 Phase 状态或验收门槛。

## 变更范围

| 文件 | 交付 |
| --- | --- |
| `AGENTS.md` | 合并重复分工说明；路由局部规则与权威来源；保留 V1/V2、安全、授权和验收边界；增加按需 Skills、Codex/DSH 协作及冲突处理 |
| `backend/AGENTS.md`、`frontend/AGENTS.md` | 新增两份局部规则，说明分层、检查入口和测试选择；不新增依赖审批或重复 CodeGraph 要求 |
| `docs/README.md` | 权威来源与冲突处理表、验收导航 |
| `docs/planning/PHASE2_SDD_WORKFLOW.md` | V1/V2 基线引用、模板选择、现有 CI 浏览器 Mock 与条件验证口径；保留历史实施记录 |
| `docs/decisions/DECISIONS.md`、`docs/guides/Codex团队协作实战教程.md` | 修正可取消 SSE、强制队友审查的残留规则；高风险专项验证及合并授权保持 |
| `docs/architecture/ARCHITECTURE.md` | 禁止第二个权威事实源，允许必要步骤/分析记录，继续复用认领内核 |
| `infra/demo/README.md`、`deploy/README.md` | Demo 操作入口不再复写过期状态；目标提交构建说明；迁移清单由旧 10 个更新为当前 13 个并要求核对实际文件 |
| `docs/planning/TASKS.md`、本记录 | 独立维护事项及验证证据；不增产品阶段，不改原任务负责人、估算、状态和证据 |

共 12 份交付文件。用户预存修改 `backend/migrations/README.md`、未跟踪 `docs/research/`、`infra/demo/quick-start.ps1`、`启动思弈阁 Demo.cmd` 不属于本轮交付，未触碰、不暂存。

## 已执行验证

DSH 自测完成后，Codex 阅读检查脚本、核对规则和 diff，并在最终规则修正后重新执行下列检查。脚本位于 Git 忽略目录 `.cache/agent-rules-selfcheck/`，不作为交付文件。

| 命令/方法 | 结果 |
| --- | --- |
| `git diff --check` | 退出 0，无空白错误；Git 提示部分文件会按本机配置转换 CRLF |
| `python .cache/agent-rules-selfcheck/check_links.py` | 12 份文档，本地文件链接 369 处、缺失 0；锚点 145 处、失效 0 |
| `python .cache/agent-rules-selfcheck/check_inbound.py` | 扫描 60 份 Markdown（含三份新增文件）；指向改动文件的入链 237 处（含锚点 146 处），失效本地链接 0 |
| `python .cache/agent-rules-selfcheck/check_consistency.py` | 入链锚点 145 处有效；七个 npm 脚本及其引用 spec、Go 模块与目录入口存在；迁移实际 13 个；CI 含 Mock E2E、不含 live/B2 live；Playwright 的端口、worker、重试与浏览器参数一致；关键规则核对通过，输出 `ALL CHECKS PASSED` |
| Git 状态与 diff 核对 | 修改只限上表；没有产品源码、SQL、锁文件、部署配置、CI 或全局 Skills 变更，预存修改未纳入交付 |

### 规则场景走查

| 输入场景 | 核对结论 |
| --- | --- |
| 只读审查发现冲突 | 只报告、不改文件，不进入实施审批或强制多 Agent 审查 |
| 前端小改 | 读前端局部规则，按影响选择检查，不另建计划目录或增加依赖审批 |
| 后端并发/租约修改 | 定向规则与失败路径验证，并按条件执行 `-race`；不要求无关全量复跑 |
| DSH 可写并行 | 不同绝对 cwd 且互不包含/重叠；不能只分文件清单；DSH 持有 `exclusive-write` claim 时 Codex 不编辑该目录，结束并释放后再整合 |
| 未授权 Commit/Push | Skill、委派或计划不能授予 Git 写权限 |
| 产品 V2 未授权 | 不进入开发，区分产品 V2、`phase3-summary-v2` 和 `v2-training-reference` |
| 显式要求 DSH 但缺桥 | 停止委派并报告，不静默换执行者或通道 |

## 未执行项与遗留问题

- 仅文档变更，未运行 Go/前端构建与测试、浏览器、服务、数据库/迁移或项目业务真实模型；未修改或触发 CI，不声称 CI 已通过。
- CI 的 `repo-guard` 在干净 checkout 上执行 `git diff --check`，不能证明提交差异没有空白问题。该缺口本轮未修复；后续如需调整，检查实际提交范围，避免扩展本轮文档任务。
- 链接与锚点使用脚本级 slug 检查，未在实际渲染器逐一点击；后续标题变化需同步引用。
- 本轮不安装/整理全局 Skills，不新建加载器或项目 Skills；局部规则由根文件显式路由，不能假定所有运行环境均已自动加载。

## 授权与负责人结论

用户本轮明确要求使用 `dsh-task` 执行，并授权完成后 Commit、普通 Push 到当前 `codex/prd-v2-docs`。DSH 未做 Git 写操作；Codex 在核对后执行，实际提交与推送结果以 Git 记录为准。不创建 PR、不合并、不部署，不写数据库或调用业务真实模型。

技术核对完成，待项目负责人确认本次规范交付。该记录不替代 Phase 结项或产品 V2 开发授权。
