# Phase 3 B3 浏览器验收、操作说明与评测准备

日期：2026-11-16（Asia/Shanghai）。本地分支 `codex/b3-acceptance`，固定生产验收基线 **`134b845e5f9352d83c711d763471d1452ba336c0`**。开始工作区干净，已核实该提交属于 `origin/codex/phase3-demo-contracts`，没有改用后续远端头。

**B3 本批技术工作完成，待负责人确认：新增真实 Fake/Nginx 浏览器 2/2 通过，操作说明与 10 场真实评测准备已交付。** B2/B3 正式签收、完整 A4 汇总及 A5 启动由 A/负责人确认；Phase 3 仍进行中。真实体验、10 场真实质量评测和部署均未执行，真实模型调用为 0。负责人已在本轮明确授权本分支 B3 清单内的本地 Commit 与普通 Push；最终执行状态和完整交付 SHA 以 Git 回执及远端核对为准，不预写成功。未授权 PR/合并/强推/部署；固定 SHA 是受测生产基线，不能冒充本批交付 SHA。

## 1. 范围与实际版本

- 按 AGENTS 导航核对正式 PRD/API/OpenAPI、架构、Phase 3 交接及既有 B1/B2 证据。当前无 `.codegraph/`，使用实际文件，没有初始化索引。
- 固定 SHA 相比 B2 `8e5e890321674bcd597ff14ba0d5d989fa6a0897` 只增加 A 的验收/证据、状态登记及 `stream-live.spec.ts` 的持久化断言；生产源码、请求底座、依赖/锁文件、后端和运行配置未变。
- 本轮只补代理整合影响及验收材料：两个业务用例、专用 Vite/Playwright 配置和 setup、操作/评测指南、匿名证据；修正导航及 Demo 说明里“B1/B2 尚未交付”的过期文字。不改顶层入口、认证/业务实现、共享 client、API、后端、迁移或架构。TASKS 及阶段结论交 A 维护。
- **前端生产代码/工作树：134b845。后端实际构建：270eed81ee9a29f53bc25c25cb9a9992e8b50252，vcs.modified=true，不是本轮重新构建。** 沿用 B2 Fake PID 70072 / `.cache/t15-local/server.exe`，进程启动时间 `2026-11-15T16:31:41.3451532Z` 未变。原 B2 启动脚本要求构建前后端/启动脚本无改动；本轮 `git diff --exit-code 270eed81… 134b845… -- backend infra/sse/start-local.ps1` 无差异，后端/前端生产源码工作区无改动。因此复用已核实的同源码运行进程，同时在收据明确保留真实构建版本，不冒充 134b845 新二进制，不重启补额度。

### 本轮 13 文件交接清单

- 前端验收 4 个：`frontend/e2e/b3-live.spec.ts`、`b3.playwright.config.ts`、`b3.vite.config.ts`、`b3.setup.ts`。
- 新文档 2 个：本文、`docs/guides/PHASE3_BROWSER_AND_EVAL_GUIDE.md`。
- 既有说明修正 2 个：`docs/README.md`、`infra/demo/README.md`，只修正文案/导航。
- 证据 5 个：`docs/acceptance/evidence/p3-b3-nginx/` 下 `nginx-full.json`、`nginx-refresh-recovery.json`、`nginx-incremental.png`、`nginx-relogin-history.png`、`nginx-refresh-failed.png`。匿名业务 ID 仅对应本机本次隔离库，不是另一环境可直接复用的账号/会话。

## 2. 执行环境与安全边界

链路为 **Microsoft Edge → Vite 127.0.0.1:5173 → Nginx 127.0.0.1:18080 → Go 127.0.0.1:8080 → 固定 MySQL → Fake**。沿用 B2 已核实装配的 `APP_ENV=test`、`AI_MODE=fake`、`T15_FAKE_SCENARIO=slow` 和 `127.0.0.1:13307/siyige_test/siyige_test_app`。迁移 00013 与固定库身份沿用既有保护记录，本轮未直连 SQL、迁移、播种、清理夹具或重启数据库。

本机代理起初未运行。受管理会话不能执行 Docker/读取秘密文件，准备环境脚本的命令也被策略拒绝；没有换工具/编码绕过，没有生成或执行该脚本。负责人在正常用户会话运行现有 `infra/sse/start-nginx.ps1` 后就绪。本轮观察 8080/18080 ready JSON 均为 200、代理 Server 为 `nginx/1.30.6`；每次实际 SSE 又断言 `X-T15-SSE-Route: matched`，不是只检查配置文件存在。

非秘密运行来源登记在本机忽略目录 `.cache/b3-live-env.json`，含真实构建版本、源码等价依据、PID/启动时间、固定目标及健康检查，不含密码/Cookie/CSRF/DSN/密钥。真实用例新注册两个独立合成用户，保留数据，不绕限额。8 个 Fake 生成请求：完整路径 6 个（策略1、四轮4、总结1），恢复路径2个（1个刷新中断、1个显式 retry）。配置/启停复验只 GET，没有额外生成。没有真实模型请求或质量评分。

## 3. 已执行验证

| 检查 | 实际结果 |
| --- | --- |
| 原仓库、分支、HEAD、Git 范围及后端来源/源码等价 | 通过；保留全部本轮改动，无 reset/复制项目/覆盖用户内容 |
| 前端 `npm run typecheck` | 通过；最终 setup、运行 ID 及配置调整后再次通过 |
| `npx playwright test --config e2e/b3.playwright.config.ts --list` | 最终正常收集 2 项；收集不冒充执行通过 |
| 真实 Fake/Nginx 浏览器两项 | **2/2 通过**，无 skipped/flaky/unexpected，CLI 最终 exit 0。用例耗时 17.8s / 1.3s；整次命令约 3.1min，包含 Windows 临时服务退出等待和已核实的自身进程收尾，不把它当模型性能数据 |
| 最终运行器专项（无生成） | 进程内 Vite 启停、页面/代理 ready、主进程/worker 运行 ID 一致：**1/1 通过，最终 1.9s、exit 0**；结束后 5173 自动释放。未重复两项生成场景 |
| Vite 专用代理配置加载/实际配置检查 | 普通开发仍直连 8080，B3 专用 `/api/` 经 18080；保持 127.0.0.1、5173、strictPort 与同源 Origin。最终不继承普通 webServer，不接管已有服务，trace/失败自动截图关闭 |
| 原始截图和收据 | 已逐张查看 3 张 PNG；仅合成用户名/发言和 Fake 内容，无密码/Cookie/CSRF/密钥。2 份 JSON 只含匿名业务/请求标识、版本、计数、时序和真实构建来源 |
| 文档/范围检查 | 本地链接、10 行未授权评测状态、差异空白和必要敏感信息检查通过；不纳入网络 trace 或测试密码录制 |
| 全量单元测试/build/后端检查 | 生产源码未改，复用固定 SHA 的 A 记录及原 300 项前端/构建与适用 CI；没有机械重跑，也不把复用写成新执行 |

### 验收运行器中遇到并修正的问题

1. 初次 `defineConfig(base, override)` 把两个 webServer 拼接并争用 5173，业务用例未开始、没有生成调用。改为单对象明确覆盖；实际内存配置检查确认不再启动两个服务。
2. 两项业务用例均通过后，Windows shell/npm 子进程树收尾卡住；只结束本次已核对 PID/路径/启动时间的 Vite，原 Go/MySQL/Nginx 不动，CLI 随后返回 0。最终 setup 改为同一进程 `createServer` / `server.close`，只持有自己的 Vite，不使用系统进程树清理。修改只涉及验收启停，不影响产品/业务断言；以最终 typecheck、实际配置及零生成启停用例定向复验，未重复消耗生成请求。
3. 主进程/worker 重载配置时各取当前时间曾导致报告和图片分目录；最终共享校验过格式的 `SIYIGE_B3_RUN_ID`，专项检查了父/worker 一致性。首次临时启停检查的缓存 `.ts` 模块格式、过宽 testDir 也已分别改为 `.mjs` / 专用测试子目录；过宽扫描曾报受限秘密目录 EPERM，未读取其中内容、未改权限或绕过，之后只扫描自己的夹具目录。
4. Vite 对未来 native 配置加载的无扩展名 import 给出兼容警告；当前锁版正常，未关闭警告、升级依赖或改请求底座。它不是本轮产品失败项。

上述修正后的配置/启停检查均通过。完整业务收据来自原已通过的两项运行，生产代码与 `b3.vite.config.ts` 未变；后续只对运行器定向复验，不把测试源变更后未经检查的结果混入交付。

## 4. 新增真实浏览器结果与证据

业务用例 `frontend/e2e/b3-live.spec.ts` 不使用 page.route、fetch 替换、伪造响应或客户端丢帧。与 B2 的 Mock、202 并发注入及 lost-done 演练分别计数。

| 场景 | 实测结果 | 证据 |
| --- | --- | --- |
| Nginx 完整练习 | 真实注册不自动登录且密码清空；本人空历史；辩题页反方/数据型会话 16，双击只创建一次。显式策略 POST=1；四次 SSE 均命中实际 Nginx location，普通 turns POST=0；正式轮数4、8个唯一消息、4个唯一原键，版本均1；总结一次。刷新、退出重登及历史打开与服务器详情完全相同，本地时间按 Asia/Shanghai 显示 | [匿名收据](evidence/p3-b3-nginx/nginx-full.json)、[实际增量](evidence/p3-b3-nginx/nginx-incremental.png)、[重登历史详情](evidence/p3-b3-nginx/nginx-relogin-history.png) |
| Nginx 刷新中断恢复 | 主视图默认 logical，会话17跳过策略。真实 delta 后刷新，GET 按原键取得 failed generation_cancelled v1；用户显式 JSON retry 携查询版本1，completed v2。turn_id=15、user message id=29 不变，最终只有29/30两条消息；SSE POST=1、retry=1、策略 POST=0，再刷新不重发 | [匿名收据](evidence/p3-b3-nginx/nginx-refresh-recovery.json)、[刷新后的持久失败](evidence/p3-b3-nginx/nginx-refresh-failed.png) |

首轮浏览器临时文字由 6 → 12 个字符，间隔 383ms，当时正式消息/轮数未推进；只证明不同时间收到增量，不是性能评测。策略保存的 Prompt 版本 `strategy-v1`，不是对真实流派质量的结论。

原完整运行日志 `.cache/b3-live-run-2026-11-16T12-22-24-299Z.log`；原 JSON 报告 `.cache/b3-browser/2026-11-16T12-22-24-707Z/results.json`，原图片/收据在同根 `2026-11-16T12-22-25-942Z` 子目录，均保留。最终只读启停日志 `.cache/b3-lifecycle-final-2026-11-16T12-34-50-537Z.log`。归档证据未编辑图片或改写测试结果。

## 5. 复用的有效证据（不是本轮重跑）

| 范围 | 来源与边界 |
| --- | --- |
| 四流派、默认值、两个创建入口、持久化/历史/刷新 | [T14 前端](T14_STYLE_FRONTEND_ACCEPTANCE.md)、[后端](T14_STYLE_BACKEND_ACCEPTANCE.md)、[B1 整合](PHASE3_B1_INTEGRATION_ACCEPTANCE.md)；Fake 不能验证真实风格质量 |
| 策略 failed→查询版本→显式 retry；generating/重复 POST 202→completed | [B2 原始实测](B2_SSE_FRONTEND_ACCEPTANCE.md)、[A 固定版整合](PHASE3_B2_INTEGRATION_ACCEPTANCE.md)两个真实 Fake 浏览器场景；后续证据补足 B1 的历史未执行项，不改写当时记录 |
| 四轮 SSE、取消/202/客户端丢 done 恢复、总结、重登/历史及消息 ID 稳定 | A 整合真实 Fake 6/6、Mock 15/15；包括整合新增断言。丢 done 是明确注入，不当作本轮未拦截链路 |
| UTF-8 拆块/半事件、错误、409 不自动重发、晚响应隔离、历史分页/空/错误列表 | 300 项前端单元/组件和15项 Mock；不把 Mock 分页冒充新增真实分页验收 |
| Nginx 心跳/超时；认领/事务/版本/所有权/CSRF/费用保护 | [T15 后端专项](T15_CORE_BACKEND_ACCEPTANCE.md)、[A 整合](PHASE3_B2_INTEGRATION_ACCEPTANCE.md)、[Demo 准备](T15_DEMO_PREP_ACCEPTANCE.md)。本轮只补浏览器代理影响，不重复后端高风险专项或据此关闭既有费用风险 |
| CI | A 交接称固定 SHA 云端四项全绿；本轮未触发 CI，网页未返回可独立核对的运行内容，保留来源归属，不声称本机重新验证四项 |

## 6. 操作、评测准备与给 A 的接续

[统一操作说明及 10 场计划](../guides/PHASE3_BROWSER_AND_EVAL_GUIDE.md)覆盖启动/打开/停止责任、真实认证而非旧内存预览、正反/四流派、策略查询与生成、四轮/自动总结、刷新/重登/历史、断线/409/429、至少三题/双方立场/四流派的10行计划、逐轮人工表，以及实际版本/Prompt/授权/次数/Token/费用/匿名失败记录。**10场全部保持未授权/未执行，不能拿本轮 Fake 收据填真实质量结果。**

- 当前未发现本轮必须改动的产品逻辑缺陷，代理浏览器阻塞已解除。本轮为 Codex 驱动浏览器、读取真实后端并查看截图，不代负责人亲自体验或签收。
- A 既有一次数据库夹具清理偶发失败根因仍未确认；其串行复测/云端结论沿用，风险不删除。本轮没有改动或再次清理该夹具。
- B3 的 Git 交付已获本轮授权：只提交清单 13 文件，普通推送 `codex/b3-acceptance`，不混入其他工作。A 按最终远端核实的完整交付 SHA 接收；其生产验收基线仍是 134b845，不将两者混用。授权不包含 PR、合并、强推或部署。
- A 接收后汇总累计证据并维护 TASKS，确认 B2/B3 人工验收、完整 A4 及 A5 启动条件。本记录不替 A 决定阶段结项。
- 独立 local-demo `18081/siyige_demo`、真实 AC17、10 场真实质量评测、HTTPS/公网、负载/性能、费用风险关闭及部署均未执行。真实调用须另行明确模型、范围、窗口、次数、Token 与金额上限；Phase 3 保持进行中。
## 7. 当前本机预览与服务状态

最终启停检查确认专用 Vite 自动释放了 5173；随后为负责人查看另行保留同配置 Fake 预览，地址 `http://127.0.0.1:5173/`，Vite PID 76972。本地忽略目录 `.cache/b3-preview.json` 记录来源及执行会话。启动预览只提供页面，不额外注册或生成；可用合成账号正常登录或注册，不存在系统通用内置账号。

Go PID 70072、固定 MySQL 和本次由负责人启动的 Nginx 保持运行、数据保留。没有停止整个 Docker、删除卷或更改后端配额。下次自动验收前先结束本次自己持有的预览；setup 遇到 5173 已占用会拒绝，不接管它。停止预览不能冒充停用真实模型预算，本轮本就没有真实模型入口。