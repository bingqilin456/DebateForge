# T05 / T06-B Git 集成记录

日期：2026-11-09（Asia/Shanghai）。本轮只整合既有成果与必要验证，无 DSH 协作；不实施 T06 剩余功能，不重新认定 T02–T05 验收，不将 T06-B 合入视为 T06 整体结项。

## 基线与合并关系

开始时工作区和暂存区干净，HEAD 为 `feat/t05-first-turn` 的 `04a9c9115589f7ecea6cda55180bbf03ad7c3a9a`。通过 GitHub PR 元数据、远程 fetch、提交父节点与祖先关系交叉核实：

| 引用或提交 | SHA 与结论 |
| --- | --- |
| 最新 `origin/develop` | `43fab8f1b443e9c7f2470e4cd8a26e2cc4ccbb25`；直接作为集成分支起点 |
| 最新 `origin/codex/docs-reorganization` | `537bb3e8df22f64f958529aa0762eced35f32e09`；已包含 T02–T05 与文档重组 |
| PR #4 | [负责人已合并](https://github.com/bingqilin456/DebateForge/pull/4)；实际方式为 merge commit，父节点依次为 `b1c255b12ad89078478c816d1ad125ec9ee7c69e`、`04a9c9115589f7ecea6cda55180bbf03ad7c3a9a`，合并树与第二父节点相同；未重复合并 PR |
| 最新 `origin/feat/t06-b-auth` | `e9ac070edbe099cea7c2abc80a35f32954e87c2e`；含 `84de6fb`、`e9ac070` 两个 B 部分提交 |
| 两个来源的共同祖先 | `e986ca038267f070740ad2543bf799cd0de8bc73` |
| 第一次集成 merge commit | `b96a1db`；第一父节点为远程 develop 基线，第二父节点为 `537bb3e`，树与文档分支完全一致，无冲突 |
| 第二次集成 merge commit | 当前记录随此 merge commit 提交；第一父节点为 `b96a1db`，第二父节点为 `e9ac070`。最终完整 SHA 见 PR 与 Git 历史 |

本地 `develop=e986ca0` 比远程 develop 多 1 个提交，且跟踪的是 `origin/docs/t01-acceptance`；本地 `main=43fab8f` 比 `origin/main=17192fc` 多 2 个提交。没有 pull、修改这些本地分支引用或恢复 PR #3；仅创建用户明确指定的 `feat/integration-t05-t06-b`。提交前再次 fetch/ls-remote，三个来源 SHA 均未改变，远程尚无同名集成分支。

## 冲突处理与文件范围

第二次合并仅 `AGENTS.md`、根 README、迁移后的 `docs/planning/PLAN.md`、`docs/planning/TASKS.md` 存在文本冲突。以最新 T02–T05 结论及文档目录为基底，补回 T06-B 的交付边界与待执行项；不采用 B 分支中因旧基线而出现的“T02–T05 尚未开始”状态。历史授权原文继续保留当轮语境，顶部明确本次授权与状态。

- T06 整体登记为进行中；统一 client、认证 API/内存状态、独立登录注册表单和本地模拟预览归 B 已交付范围。正式 App.vue、首轮面板与真实 T03/T05 浏览器联调仍待 A 后续实施。
- `docs/T06_B_HANDOFF.md` 迁至 [acceptance/T06_B_HANDOFF.md](T06_B_HANDOFF.md)，仅补迁移说明、修复相对链接并更新导航；其原有验证和用户人工确认均保留为历史证据，不冒充本轮执行。
- 分支名称优先遵循用户明确指定；保留默认前缀规则并说明获准时可按用途命名。本轮名称与用户要求一致。
- T06-B 的前端源码、package.json 和锁文件原样合入；后端、infra 与已验收迁移在第二次合并中无改动。前端相对 B 分支仅额外保留已有健康路径 `/api/health/live` 与 Vite `/api/` 代理修订，不重新引入旧 `/health/` 路径。

## 本轮实际验证

环境：Windows amd64，Go 1.27.1、Node.js 24.11.1、npm 11.6.2。全程显式清空 `SIYIGE_INTEGRATION` 与 `SIYIGE_MIGRATION_TEST`，设置 `AI_MODE=disabled`；未读取真实模型凭据，未启用数据库集成或迁移。

| 检查 | 本轮结果 |
| --- | --- |
| `backend`: `go test -count=1 ./...` | 通过，8 个包显示 ok；真实 MySQL 用例因未启用集成模式跳过，不能将 integration 包的 ok 视为数据库验证 |
| `backend`: `go vet ./...`、`go build ./...` | 均退出 0 |
| `backend/tools/migrate`: `go test -count=1 ./...`、`go vet ./...`、`go build -o ../../../.cache/t05-t06-integration/migrate.exe .` | 均退出 0；真实迁移/种子/历史保护等数据库用例跳过，没有执行工具的 up/status |
| `frontend`: `npm ci --cache ../.npm --no-audit --no-fund` | 按现有锁文件安装成功；没有修改依赖版本或锁文件 |
| `frontend`: `npm test` | 7 个文件、123 项测试通过，覆盖 client、认证 API/状态、表单、预览模拟与健康请求 |
| `frontend`: `npm run typecheck`、`npm run build` | 均退出 0；生产构建仅包含正式 index 入口，未接入本地认证模拟预览 |
| 接口契约静态核对 | 认证四路径/状态码、注册不自动登录、字符串 uint64 ID、User/Auth DTO、请求/错误信封与 X-Request-ID、Cookie 同源、Origin/CSRF、204、凭据长度及规范化、短超时/生成超时、401 失效与禁止自动重试均与当前 API 和后端实现核对；不替代真实浏览器联调 |
| 文档与 Git 检查 | 本地 Markdown 链接目标均存在，无残留冲突标记；提交前核对暂存路径、空白错误、源码归属和父节点，来源提交保留 |

首次沙箱验证遇到 git FETCH_HEAD 写入受限、npm 创建依赖目录 EPERM，以及 Go 回环 HTTP/模拟 DB 连接失败。按原授权范围提升执行后全部通过，未通过修改产品代码规避失败。迁移工具首轮已通过，未无意义重复。首轮 Go 有既有 sonic 对 Go 1.27 的兼容性提示并回退 encoding/json，不作为本轮新增缺陷。

CodeGraph 已按规则先查询；索引提示部分文件过期且缺少新增认证模块，因此相关接口核对使用实际文件，没有自行重新索引。

## 审查结论与剩余边界

本轮集成检查未发现新增 P0/P1；这是集成技术检查，待全新 Codex 会话执行独立只读审查后由负责人决定是否合入 develop。T02 已接受风险、T03 原 P2/P3 延期、T04 精确结算未知和 T05 已接受限制全部保留，未宣称修复或关闭。

**T07-COST-01 供应商硬预算缺失继续未关闭，仍是 P0 公网真实入口阻塞。** T05 真实模型链路未验收；当前单进程模型并发仍为 2。未运行真实 MySQL 集成、数据库写入/迁移/持久化、真实模型、真实认证浏览器端到端、部署或公网验证，也未重做 B 历史预览人工验收。

本轮仅推送集成分支并创建面向 develop 的 PR，创建后停止；不自动合并、删除原分支、改动 main/develop、Rebase、Reset、强推或推进 T06 剩余功能。
