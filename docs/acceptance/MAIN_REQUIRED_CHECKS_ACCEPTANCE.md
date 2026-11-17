# main 强制质量门禁：配置提案与核查

日期：2026-11-16（Asia/Shanghai）。状态：配置待负责人确认，远端尚未启用。

## 基线、范围与授权

- 从已登记的最新整合基线 `134b845e5f9352d83c711d763471d1452ba336c0`（`codex/phase3-demo-contracts`）新建 `codex/main-required-checks`；远端 main 仍为 `9fa19eb6c0fbd3a8d1ab3ce002a700c6235e9afc`，不以旧 main 替代含 B2 的整合基线。
- 开始时工作区干净。保留原 `codex/unify-debate-creation` / `aa8593bf42786a2b6d19d3e70cdb25799679b2fa`，未混入后续独立任务；本轮只修改规则 JSON、本文、TASKS 和文档导航，不修改应用、迁移、部署或 CI 执行逻辑。
- 本轮授权必要文件验证后 Commit、普通 Push 到本分支；远端保护写入须负责人确认。未授权 PR 创建、合并、强推、部署或真实模型调用。

## 远端只读实测

- GitHub 连接工具读取 `GET /repos/bingqilin456/DebateForge/branches/main`：`protected=false`、检查列表为空；`GET /repos/bingqilin456/DebateForge/rulesets?includes_parents=true` 返回 `[]`。
- 连接工具专用 protection 端点返回 403（installation 无 Administration 权限），不能将它解释成不存在保护；随后本机 `gh api .../branches/main/protection` 成功访问 API，返回 404 `Branch not protected`，`gh api .../rulesets` 再次返回 `[]`。读取生效分支规则的连接工具 URL 不受支持，未以该失败充作规则为空的证据。
- GitHub 工具读取 [整合基线 CI](https://github.com/bingqilin456/DebateForge/actions/runs/43796062279) 的 jobs 和 `commits/134b845.../check-runs`：四项均 success，`head_sha` 均为上述整合基线，来源均为 GitHub Actions（App ID `15368`）。这仅是检查名称与来源的核实，不冒充本分支新提交 CI。
- 首次本地 fetch 遭沙箱 `.git/FETCH_HEAD` 写入限制，提权后成功；本机 gh 首次读取受失效代理影响，仅清除该进程代理后完成核查，没有修改凭据或全局配置。

## 待确认的唯一配置

可执行请求体为 [.github/main-required-checks.ruleset.json](../../.github/main-required-checks.ruleset.json)。采用一个 repository ruleset，不叠加传统 branch protection；提交该文件本身不会启用远端保护。

| 项目 | 具体值与影响 |
| --- | --- |
| 名称、范围、生效状态 | `main-required-checks`；仅 `refs/heads/main`；`active`，不影响 develop 或功能分支 |
| PR | 必须通过 PR 更新 main；审批数 `0`；不要求 Code Owners、最近推送审批、过期审批撤销或讨论全部解决 |
| 检查 | 下表四个精确 context，全部绑定 GitHub Actions `integration_id=15368`，不使用 `CI /` 前缀或 job ID |
| 最新提交及主干同步 | `strict_required_status_checks_policy=true`；旧 SHA 绿灯不能代替最新提交检查，main 前进后需同步分支并重新通过适用检查 |
| 强推、删除 | `non_fast_forward` 和 `deletion`；禁止 main 强推及删除 |
| 绕过 | `bypass_actors=[]`，不为管理员、仓库角色、团队或 App 添加例外；管理员仍有修改规则的管理权限，修改规则须另行授权 |
| 其他 | 不增加 merge queue、签名、线性历史、部署门槛、额外审批、Restrict updates/creations 或其他检查 |

| job ID | 必需 context（实际 check-run 名称） |
| --- | --- |
| repo-guard | 仓库规范与静态门禁 |
| backend-unit | 后端与迁移工具无收费检查 |
| backend-mysql-integration | 隔离 MySQL 8.4.13 迁移与集成测试 |
| frontend | 前端类型检查、单测与构建 |

GitHub 原生 required checks 接受 success、neutral 或 skipped；当前四项没有产生 neutral/skipped 的正常成功路径。三个业务 job 依赖 repo-guard，后者失败会使其跳过，但 repo-guard 自身仍为必需失败项，因此不能靠依赖跳过放行。取消、超时、失败或缺失检查均不能满足门禁。以后不得为这些必需 job 增加可绕过检查的条件；如改变名称或 MySQL 版本，须同步规则 context。

## 触发与等待核查

- 保留现有 [ci.yml](../../.github/workflows/ci.yml)：`pull_request` 无分支、路径过滤，默认 opened/synchronize/reopened 覆盖新 PR、新提交与重开；main/develop push 继续运行；保留 workflow_dispatch。四个 job 名称唯一、无 job 级 `if`、无环境人工审批，并有 5/10/15 分钟超时。
- 因无 paths/paths-ignore，纯文档 PR 也运行全部四项；不要求仅 push main 才出现的检查。功能分支普通 Push 没有 PR 时不会自动触发，需用既有 workflow_dispatch 验证该 ref；它不能替代将来实际 PR 最新提交/测试合并提交的必需检查。
- 不启用 merge queue，因此不需要增加 merge_group。新提交会取消同一 PR 的旧运行并产生新运行，不能用被取消或旧 SHA 的检查放行。
- PR 存在合并冲突时先解决冲突；fork 首次运行可能需要 Actions 授权，按 GitHub 原有权限处理。不要在提交信息中使用 `[skip ci]`、`[ci skip]`、`skip-checks: true` 等跳过指令，否则 GitHub 会令必需检查保持 Pending；应推送不带跳过指令的新提交。不得通过临时绕过、缩减检查或旧 SHA 检查解除等待。

上述行为依据 [GitHub 必需检查排错](https://docs.github.com/en/pull-requests/how-tos/merge-and-close-pull-requests/troubleshooting-required-status-checks)、[规则说明](https://docs.github.com/en/repositories/configuring-branches-and-merges-in-your-repository/managing-rulesets/available-rules-for-rulesets)与 [Rules REST API](https://docs.github.com/en/rest/repos/rules#create-a-repository-ruleset)。

## 验证与后续执行

本地已通过：规则 JSON 解析及断言（四个精确 job 名/App、strict、PR 参数、仅 main、无额外规则及绕过、触发条件、job 依赖与超时）；执行工作流内 Python 门禁，311 个跟踪文件路径/敏感内容检查、61 份 Markdown 本地链接检查通过；`git diff --check`、`git diff --cached --check` 与四文件变更范围核对通过。首次链接检查因新文件尚未进入 Git 索引报告两个入口缺失，显式暂存本任务四文件后通过。应用代码及工作流执行逻辑未改，未额外本地复跑 Go/前端/数据库全量；云端验证使用既有无收费 CI（隔离临时 MySQL，不访问本地或生产库）。

待负责人确认后，先重读远端保护/规则集，若出现他人新设置则停止比较差异；否则以该 JSON 创建规则，再回读 ruleset 及 main 生效规则，核对四项 context/App、strict、PR、删除/强推和空绕过列表。连接插件无保护写入工具，执行时使用有管理权限的 GitHub CLI；本轮未发送该写请求，也未测试真实拒绝推送或删除。

本任务技术准备不代表保护已生效、负责人验收或 Phase 3 结项。最终提交 SHA、云端运行及四项结果随交付消息提供；未通过前不得宣称本分支 CI 全绿。
