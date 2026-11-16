# Phase 3 B1 补测与 B2 SSE 整合验收记录

日期：2026-11-16（Asia/Shanghai）。目标分支 `codex/phase3-demo-contracts`。本轮技术整合及 Fake 浏览器验证已完成，B2 正式验收待负责人确认；B3、完整 A4、A5、真实体验与真实评测均未标通过，Phase 3 保持进行中。

## 来源、范围与授权

- 开始工作区干净，目标本地/远程头均为 `b39a621a629da8c36d225b8f0422844e07914a20`。
- 清除本次进程的失效代理后，通过 `git ls-remote` / fetch 确认远程 `codex/b2-sse` 头为指定 `8e5e890321674bcd597ff14ba0d5d989fa6a0897`，开发基线 `270eed81ee9a29f53bc25c25cb9a9992e8b50252` 是其祖先；B2 相对此基线为 24 文件、1168 行新增/51 行删除，限前端、测试和证据。
- 合入前核对 [B2 原始交付](B2_SSE_FRONTEND_ACCEPTANCE.md)、[B1 历史整合](PHASE3_B1_INTEGRATION_ACCEPTANCE.md)、[策略 Fake 装配及固定 test 验证](P3_A_STRATEGY_FAKE_ACCEPTANCE.md)，保留历史范围与失败记录。
- 无冲突合并提交 `17850a43151e473b0a306a12cf3079d1ee131eed`，两个父提交为上述目标起点和来源 SHA；合并树与来源树完全一致，包含 A 的既有后端、契约、费用保护和 B1。没有遗漏开发基线上的 A 成果。
- 集成补测提交 `593d7fcde73ed1f9cb95830ac618e2e2327c2a7d` 只改 `frontend/e2e/stream-live.spec.ts`：补重登历史详情完全一致、四轮/总结版本、恢复前后 turn/user message ID 不变和消息 ID 唯一断言。生产源码、接口、锁文件、依赖、部署配置及迁移未再修改。之后仅登记验收、证据和任务状态。
- 本轮授权范围内完成合入、必要补测、Commit、普通 Push；不新建/更新 PR、不合 main/develop、不强推、不部署、不真实调用、不读取模型密钥。最终交付完整 SHA 以本轮最终交接及远程头为准，B3 须固定该 SHA。

## 执行环境与步骤

先核对来源和证据 → 合入 → 普通检查 → 装配固定 test/Fake → 六项真实浏览器 → 受影响 Mock 与数据库专项 → 只读持久化收据 → 文档/状态与普通推送。CodeGraph 优先调用，但索引未覆盖新测试及 SSE 符号，随后按实际文件核对，未重建索引。

本机 Docker Desktop 最初未运行。只启动并核对既有 `siyige-t02-mysql-test-1`：项目 `siyige-t02`、服务 `mysql-test`、镜像 `mysql:8.4.13`、回环 `127.0.0.1:13307`、原卷 `siyige-t02_mysql-test-data`。实际 SQL 身份为 `siyige_test / siyige_test_app@% / 8.4.13`，迁移已到 00013。未迁移、播种、重建卷或操作开发库。

真实链路为 **Microsoft Edge → Vite 127.0.0.1:5173 → 本次源码构建 Go 127.0.0.1:8080 → 固定 MySQL → Fake**。`APP_ENV=test`、`AI_MODE=fake`、`T15_FAKE_SCENARIO=slow`。经正式注册/登录新建三名合成用户，ID 从实际登录取得；按账号启用两个策略选择器。只切换本轮未做生成的 bootstrap 后端，配置后故障/生成测试全程未重启补额度。保留合成数据，不直写业务表造状态。

浏览器复用已安装 Edge，无新增依赖。用忽略目录 `.cache/p3-b2-integration.config.mjs` 复用正式 Playwright 配置，固定单 worker、禁 trace/自动失败截图、自有 Vite、输出到 `.cache/`；每次 Vite 随 Playwright 正常退出，不复用别人的服务。元数据 `.cache/b2-live-env.json` 仅含基线、合成用户名和 ID，没有密码、Cookie、CSRF 或 DB 凭据。公开合成密码常量见测试源码，不用于真实账号。

## 已执行验证

| 检查 | 实际结果 |
| --- | --- |
| 前端 `npm run typecheck` | 通过；补测试断言后再次通过 |
| 前端 `npm run test` | 19 文件/300 项：首次 289 通过、11 项本机 HTTP 因受限回环访问失败；允许回环访问并清除失效代理后，定向 `npx vitest run src/api/client.test.ts src/api/health.test.ts` 的 50 项全通过。其余 250 项沿用首次结果，未声称整组一次全绿 |
| 前端 `npm run build` | 通过，666 modules；此后只改 E2E/文档 |
| 后端 `go test ./...` / `go vet ./...` / `go build ./...` | 允许回环访问后全通过，模型 disabled、两个 integration 开关清空。首次沙箱中回环相关测试失败，未据此修改产品逻辑；Windows 不支持的 Linux/Nginx 专项沿用原证据及云端检查 |
| `npx playwright test --config ../.cache/p3-b2-integration.config.mjs e2e/strategy-fault-live.spec.ts e2e/stream-live.spec.ts` | `SIYIGE_BROWSER_LIVE=1`，真实浏览器 **6/6 通过，52.7 秒**，包括本次补强断言 |
| 同配置执行 `e2e/views.spec.ts e2e/style.spec.ts e2e/stream.spec.ts` | 清除 live 开关，浏览器 Mock **15/15 通过，25.2 秒** |
| 固定 test 库 `go test -count=1 -v ./tests/integration -run 'Stream\|Strategy\|Style'` | 17 项顶层测试中 16 项首次通过；`TestT15StreamPreflightStaysJSON/json` 协议断言后夹具清理失败。浏览器结束后，仅串行重跑 `^TestT15StreamPreflightStaysJSON$`，5 子项全通过（包耗时 1.501 秒）。不把首次整组记为通过 |
| 六场会话只读重查、`git diff --check`、变更及本地文档链接 | 通过；证据只含合成账号页面和脱敏 ID/版本，无凭据/网络 trace/供应商原文 |

本地数据库专项与真实浏览器最初并行写同一测试库，期间出现一次夹具清理失败；停止并行后定向复测通过，未证实具体 SQL 错误原因，也未改写清理逻辑、重置库或反复跑全组。后续数据库写入专项与浏览器 live 应串行安排。迁移工具本轮无增量，复用既有专项与适用 CI；未再本地全跑迁移工具。

## 真实浏览器与持久化结果

| 场景 | 实测与幂等/版本结论 |
| --- | --- |
| 策略失败恢复，会话 1811 | 首次 503/model_unavailable；刷新、退出重登仍 failed v1；GET 查询后用户明确 retry，正文 `retry:true, expected_attempt_version:1`，完成 v2；双击仍只发一次 retry |
| 策略延迟，会话 1812 | 实际等待 8 秒；另一页 GET 看见 generating v1；真实重复 POST 返回 202；原请求 201，最终 completed v1；观察页 POST=0 |
| 四轮 SSE，会话 1813 | 无流响应拦截；预览由 6 到 18 码点，间隔 372ms，当时未计轮/写正式消息；4 次 stream POST、普通 turns POST=0，4 个唯一业务键、4 个 v1 轮次、8 个唯一消息 ID；总结 completed v1 |
| 刷新/重登/历史 | 上述四轮后刷新、退出重登、历史点击进入，重新 GET 的整个详情与已存结果完全一致；仍 8 消息/原版本，未增加生成请求 |
| 取消，会话 1814 | 中止真实流后 GET 至 failed/generation_cancelled v1，仅原文；按查询版本 JSON retry 后 completed v2；turn ID、client_request_id、原文消息 ID 不变，最终恰两消息，stream POST=1 |
| 202，会话 1815 | 只暂缓浏览器发出请求，让另一真实同键请求先认领；浏览器收到真实 202 application/json 后只 GET，最终一个 v1 turn、两个不同消息 ID，前端 POST=1 |
| 丢 done，会话 1816 | 客户端仅丢弃真实后端响应的 done；后端已实际提交，EOF 后 GET 恢复 completed v1，一个 turn、两消息、前端 POST=1；此注入不当作未拦截流或逐块证据 |

可查：[脱敏持久化收据](evidence/p3-b2-integration/persistence.json)、[真实增量预览](evidence/p3-b2-integration/incremental-preview.png)、[重登历史详情](evidence/p3-b2-integration/relogin-history.png)。两张新增纳入截图均已查看。时序仅证明分块到达，不是性能评测。

数据库专项实测复核了 stream/JSON 共用认领无双写、认领/提交结果不明不发 done、旧流版本不能覆盖新重试，以及策略所有权、CSRF、版本、快照、并发配额、租约和事务失败等。未受影响的完整安全矩阵、两赛制边界、Nginx 直连/透传/心跳/超时等沿用 [A 集成矩阵](../planning/PHASE3_A_INTEGRATION_SPEC.md)、[T15 后端专项](T15_CORE_BACKEND_ACCEPTANCE.md)、[Demo 准备](T15_DEMO_PREP_ACCEPTANCE.md) 和 B 原始记录；本轮没有新增 Nginx 浏览器验收。

## 给 B3 的启动与使用

1. 正常 fetch 后，固定本轮最终交接的完整 SHA；先检查工作区，保护未提交修改。运行版本的生产源码与 `593d7fcde73ed1f9cb95830ac618e2e2327c2a7d` 一致，后续仅验收文档/证据。
2. 从仓库根目录按 [MySQL 说明](../../infra/mysql/README.md) 核对固定测试容器、端口、秘密文件归属和迁移 00013。当前环境已迁移，无需再迁移。本次结束已停止自己启动的后端及测试容器，保留数据卷；恢复只需 `docker --context desktop-linux start siyige-t02-mysql-test-1`，等 ready。
3. 在 PowerShell 7.2+ 中启动普通 Fake（两个选择器清空；8080/5173 必须空闲且核对归属）：

```powershell
$env:P3_STRATEGY_FAKE_FAILURE_USER_ID = ''
$env:P3_STRATEGY_FAKE_SLOW_USER_ID = ''
$env:T07_FAKE_SCENARIO = ''
$testAppSecret = (Resolve-Path '.cache/t02-mysql/secrets/test-app.txt').Path
pwsh -NoProfile -File infra/sse/start-local.ps1 -PasswordFile $testAppSecret -Scenario slow
```

另开终端 `Set-Location frontend`，已安装锁定依赖则直接 `npm run dev`（否则 `npm ci`）。打开 `http://127.0.0.1:5173`，正常注册/登录，选择题目/立场/流派、创建会话；可显式生成策略，四轮发言后自动总结，再刷新/重登历史核对。Fake 内容不代表真实模型质量。

4. 重现 B1 故障须按 [策略 Fake 固定 test 装配](../../infra/strategy-fake/README.md#固定-test-环境给-b-联调)，通过正式 API 注册不同合成用户并从登录/Me 取得真实 ID；仅切换自己的无生成 bootstrap 后端，设置 failure/slow 选择器后启动同一脚本。不能猜 ID、接管其他进程或靠重启恢复额度。
5. 自动 live 入口 `SIYIGE_BROWSER_LIVE=1` + `npm run test:e2e:b2:live`，需按 [B 原始说明](B2_SSE_FRONTEND_ACCEPTANCE.md#复现入口需明确预约固定-fake-环境) 在当前工作树准备非秘密 `.cache/b2-live-env.json`；其 `base_sha` 是开发基线，不是最新运行 SHA。本地可设 `SIYIGE_BROWSER_EXECUTABLE` 为已有 Edge 的绝对路径。不要在同一 DB 同时跑数据库写入专项；生成仍遵守 6/分钟、20/日等现有额度，重复执行会保留新增会话并消费配额。
6. 停止时核对脚本输出 PID 对应当前工作树 `.cache/t15-local/server.exe`，只停止自己的后端/Vite；本次测试容器原为停止，故测试结束恢复停止状态。不要停止整个 Docker 或其他项目，不删除卷/合成历史。浏览器 trace 保持关闭，避免保存登录凭据。

## 遗留与验收边界

- 未发现本轮必须修改的生产逻辑缺陷。夹具清理偶发失败及本机受限回环问题已如实登记；前者定向复测通过，具体原因未确认，保留后续观察项。
- B2 为“已整合且技术验证完成、待负责人人工验收”。B3 可在最终固定 SHA 开始其独立完整浏览器验收、操作说明和评测准备；本记录不替 B3 签收。
- 完整 A4 汇总、A5 启动、真实 Demo/10 场评测、HTTPS/公网、负载/性能及部署均未在本轮验收。T07-COST-01 等既有风险继续保留，Phase 3 进行中。
- 云端检查的运行链接、所检查完整 SHA 和实际结论以最终 Git/CI 交接为准，不把旧基线 CI 当本次结果。负责人尚未给出 B2 正式验收结论。

## 后续签收说明（2026-11-16，追加；上文历史原文保留）

本节由 A4 签收轮次追补，不修改上文的原始证据、计数、失败记录与“待负责人确认”等当时表述。

- **签收结论**：按负责人本轮“完成剩下 B2/B3 正式签收”的明确要求，经证据核对，**B2 本批无收费工作（固定 test 库 + Fake，含其后 B3 补的本地 Nginx 代理影响）登记通过，范围限定受测生产基线 `134b845e5f9352d83c711d763471d1452ba336c0`。** 该受测基线即本记录末轮状态提交，B2 合并、补测与验收登记均在其祖先链上；完整结论、A4 累计矩阵与 A5 启动前置条件见 [B2/B3 正式签收记录](PHASE3_B2_B3_SIGNOFF.md)。
- **签收依据**：第 4 节六项真实 Fake 浏览器 6/6、Mock 15/15、固定 test 库专项（认领/事务/版本/租约/断线/丢 done）、后端 test/vet/build 与前端 typecheck/test/build，加上 B3 追加的 Nginx 代理真实浏览器 2/2；生产源码在 `593d7fc` 之后未再改动，故 B2 通过项与 B3 代理补验同属 `134b845` 可引用证据。
- **本轮性质**：本轮只做**既有证据的静态与视觉复核**，没有新的浏览器执行；第 4 节的六项真实浏览器结果与截图均为历史已执行证据，不由本轮产生。**未伪造负责人亲自点击体验**，人工体验由负责人在真实 Demo 阶段单独进行。
- **后端版本**：本记录第 2 节的实际构建 `270eed81ee9a29f53bc25c25cb9a9992e8b50252` 与 `134b845`、`73bf1d0` 在 `backend` 与 `infra/sse/start-local.ps1` 上差异为空，源等价成立，继续按“不冒充新构建”的口径引用。
- **未变化**：完整 A4 已由签收记录承接、A5 只登记前置条件而未启动；真实 Demo/10 场评测、HTTPS/公网、负载/性能与部署未执行；第 4 节保留的夹具清理偶发失败根因未确认，继续作为观察项；T07-COST-01 等费用风险不因签收关闭；Phase 3 保持进行中。
