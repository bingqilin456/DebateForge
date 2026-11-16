# B2 SSE 前端与 B1 真实故障补测记录

日期：2026-11-16，Asia/Shanghai。本批实现与本地技术验证已完成，交付待 A 整合及负责人确认；不据此标记 B2/B3、完整 A4、A5 或真实评测通过。

## 固定基线与环境

- 原仓库：`D:\OneDrive\Desktop\思弈阁\DebateForge`；功能分支：`codex/b2-sse`。
- 开发基线：`270eed81ee9a29f53bc25c25cb9a9992e8b50252`。负责人正常 fetch 并 ff-only 接入，开始时干净，保留 B1；最终 B2 提交 SHA 以本批 Git 交接结果为准，不用基线 SHA 冒充实现提交。
- 实际链路：Edge → Vite `127.0.0.1:5173` → 当前固定版本 Go `127.0.0.1:8080` → `127.0.0.1:13307/siyige_test` → Fake。后端为本次重建的 `.cache/t15-local/server.exe`，验证时 PID 70072，live/ready 均 200。
- `APP_ENV=test`、`AI_MODE=fake`、`T15_FAKE_SCENARIO=slow`；策略另外启用实际登录 ID 对应的 `P3_STRATEGY_FAKE_FAILURE_USER_ID` / `P3_STRATEGY_FAKE_SLOW_USER_ID`，不是只设置 T15 场景。
- 密码目录在受限会话中不可读，由负责人正常运行本地启动辅助文件。使用 A 的 [启动说明](../../infra/sse/README.md) 与 [策略 Fake 装配](../../infra/strategy-fake/README.md)，正常注册三个合成账号并从登录响应取得 ID；只切换本脚本的无生成 bootstrap 后端以装配选择器，不重启他人服务或用重启刷新已用额度。
- 保留固定 MySQL、数据与迁移 00013；未执行迁移、清库、卷重建、数据注入或凭据复制。未访问其他数据库、真实模型或模型密钥，未做真实质量/费用评测。

## 修改文件与边界

| 文件（仓库相对路径） | 本批内容 |
| --- | --- |
| `frontend/src/api/client.ts` | JSON/SSE 共用同源、Session/CSRF、请求编码、受控错误、认证代次与超时；增加可取消流读取，保留健康检查/204 |
| `frontend/src/api/sse.ts` | 严格 UTF-8 增量解码、半字符/行/事件缓存、LF/CRLF/CR、多 data 行、注释、EOF 半事件保护 |
| `frontend/src/api/debate.ts`、`frontend/src/types/api.ts` | 固定 stream 路径和四事件/关联校验，区分 200 SSE、202 JSON 与事件前 HTTP 错误 |
| `frontend/src/debate/firstTurn.ts` | 临时预览不入正式消息；原键查询、有界恢复、取消/迟到隔离；失败仍显式 JSON retry |
| `frontend/src/components/debate/FirstTurnPanel.vue` | 实时预览及未提交提示、停止等待并查询；文本转义，正式确认后计轮 |
| `frontend/src/api/sse.test.ts`、`frontend/src/api/stream.test.ts`、`frontend/src/debate/stream.test.ts`、`frontend/src/StreamPanel.test.ts` | 新增 45 项解析/API/状态/组件检查 |
| `frontend/src/App.test.ts`、`frontend/src/api/debate.test.ts` | 最小更新受影响的成功流 fixture 与 client mock 类型，保留三轮兼容和总结/重试回归 |
| `frontend/e2e/strategy-fault-live.spec.ts` | 两项 B1 真实失败/延迟补测 |
| `frontend/e2e/stream.spec.ts`、`frontend/e2e/stream-live.spec.ts` | 4 项浏览器 Mock 与 4 项真实 Fake/客户端丢帧恢复检查 |
| `frontend/package.json` | 仅测试脚本接线：默认 E2E 收录新 Mock，本批真实用例单独入口且需显式开关；无新增依赖或锁文件改动 |
| 本文件与 `docs/acceptance/evidence/b2-sse/` | 实际结果、六张合成账号页面截图及一份无正文/凭据的到达时序证据 |

未改后端、正式 API/OpenAPI、架构、TASKS、认证、顶层 App/路由、依赖版本或锁文件；未创建 PR、合并或部署。A 按当前集成基线接入本批增量，不回退自己的后续成果。

## 契约接线

依据 [API 第 13 节](../api/API.md)、[OpenAPI](../api/openapi.yaml)、[冻结样例](../planning/PHASE3_A_INTEGRATION_SPEC.md#t15-core-冻结契约与可复制样例)、[A 后端专项](T15_CORE_BACKEND_ACCEPTANCE.md)。历史 A1“尚未注册”语境不作为当前状态，以正式节与实施增量声明为准。

- fetch POST 携带原 Cookie/CSRF/Origin，发送前创建并保留唯一 `client_request_id`；没有 EventSource GET、stream/retry 新路径、第二套幂等键或流错误后自动改走普通 POST。
- `start` 只关联 turn_id/request_id，不取版本；`delta` 仅临时文本。完整合法 `done` 才接纳正式 Turn/计轮；旧轮重放如返回更新进度快照，GET 补全其它轮次。
- 先判 HTTP/Content-Type；202 只解析 JSON。断线、取消、流内 error、EOF 无完整 done 均先 GET；未收到 start 时仍按原业务键定位，不将其它请求的同轮当作本请求。
- generating/暂未匹配原键：每 2 秒 GET，最多 120 秒；之后提示手动核实，不自动 POST。401/404 受控结束；旧认证请求不能清掉新 Session。
- failed 后用户明确点击现有 JSON retry，expected_attempt_version 取详情 GET；409 只重新查询，不自动换版本、键或原文重发。取消/离开会中止自身连接并作废迟到片段。
- 70 秒总预算覆盖 fetch/body，前端另有单帧 64 KiB、整流 2 MiB 防无限缓存保护；触发后查询，不修改后端字段规则。正式消息沿用 2000 字符校验，内容只按文本显示。
- 给 A：生产 `createDebateApi` 默认提供 stream，主面板已接线，无需另改 App。旧 JSON API 保留兼容，不是网络故障自动降级；原四轮/历史三轮、总结、分页、流派和策略功能保留。

## 已执行检查

新增失败测试先红后绿。普通回归清除 SIYIGE_INTEGRATION/SIYIGE_MIGRATION_TEST、AI_MODE=disabled；真实场景显式使用上述 Fake，不影响已运行进程的配置。

| 检查 | 实际结果 |
| --- | --- |
| `npm run typecheck` | 通过；最后真实 fixture 请求头修正后亦重跑通过 |
| `npm run test` | 19 文件、300 测试通过（原 255 + 新 45） |
| `npm run build` | 通过，666 modules；此后未修改业务源码 |
| 浏览器 Mock | stream/style/views 分组实际执行 4+2+9=15 项，全通过；默认 `test:e2e --list` 已确认收录 15 项，不机械复跑整组 |
| `npx playwright test e2e/strategy-fault-live.spec.ts` | 2 项真实策略补测通过，约 12.4 秒 |
| `npx playwright test e2e/stream-live.spec.ts` | 前两项真实 SSE 通过；第三项先行 HTTP 测试请求漏 Accept 被正确拒绝，第四项按 serial 未运行；修正后只重跑后两项，均通过，约 12.1 秒；最终四个独立场景均有通过结果 |
| diff、范围与隐私检查 | 无后端/正式契约/顶层/锁文件改动；只纳入本批前端、测试与证据，未夹带 `.cache`、日志、二进制、密码或网络 trace |

首次 202 用例失败属于测试 fixture：背景请求缺 `Accept: text/event-stream`，后端在认领前正确返回 400，未出现 turn。修复仅加该请求头，未修改后端/业务源码、清数据、重启服务或重跑已经成功的生成。修正后的定向命令：`npx playwright test e2e/stream-live.spec.ts -g '202 JSON|客户端丢 done'`。

真实测试均显式 `SIYIGE_BROWSER_LIVE=1`，复用已有 Edge，关闭 trace/失败自动截图以免记录 Cookie、CSRF、密码；结果页截图只含合成用户名、发言和 Fake 内容。已逐张查看本次纳入的六张截图。

## 真实浏览器结果与证据

| 场景 | 实际结果 | 证据 |
| --- | --- | --- |
| B1 首次持久 failed → 显式 retry | 首次 503 model_unavailable，GET/刷新/重登仍 failed v1；先查询后再点击，POST retry:true/expected=1 得 200 completed v2；连续点击未多发 | [重试后策略](evidence/b2-sse/strategy-failed-explicit-retry.png) |
| B1 延迟/202 | 原请求等待约 8 秒；另一页只 GET 观察 generating v1，另一真实客户端重复 POST 返回 202；原请求 201，最终 completed v1，观察页未自动 POST | [生成中](evidence/b2-sse/strategy-generating-202.png) |
| B2 Vite 实际增量/四轮 | 未拦截流的真实请求，预览先 6 后 12 字符，间隔 374ms；当时正式消息/计轮尚未推进。四次 SSE POST、普通 turns POST=0，四个唯一业务键，最终四轮八条正式消息，总结与刷新正常 | [实时预览](evidence/b2-sse/sse-incremental-preview.png)、[到达时序](evidence/b2-sse/sse-incremental-timing.json) |
| B2 真实取消与恢复 | 点击停止等待中止真实连接，GET 至 failed generation_cancelled v1；按查询版本 JSON retry=1 得 200，v2 completed，原键不变，SSE POST 仍仅一次 | [取消后只读确认失败](evidence/b2-sse/sse-cancelled-query-failed.png) |
| B2 真实 202 JSON | 只延后浏览器发送，让另一真实同键请求先认领；浏览器真实收到 202 application/json，前端只 GET 至 completed v1，一轮两消息，无第二次前端生成 | [202 后查询](evidence/b2-sse/sse-202-json-query.png) |
| B2 提交后客户端丢 done | 后端实际提交完成，客户端故障注入只丢弃真实 done、保留真实 start/delta；EOF 后 GET 恢复 completed，仍一轮两消息、一次前端 POST | [查询恢复完成](evidence/b2-sse/sse-lost-done-recovered.png) |

说明：丢 done 用例是“真实后端 + 客户端丢帧注入”，不是未拦截链路，也不作为逐块时序证明；增量证据来自上表独立的未拦截流用例。374ms 仅证明不同时间到达，不是性能评测。202 并发用例仅暂缓请求，不伪造后端状态/响应；浏览器 Mock 与真实场景分别登记。

## 复现入口（需明确预约固定 Fake 环境）

普通回归执行 `npm run typecheck`、`npm run test`、`npm run build`、`npm run test:e2e`。真实用例不在默认 CI 中自动开启：显式设置 `SIYIGE_BROWSER_LIVE=1` 后执行 `npm run test:e2e:b2:live`。已有锁定依赖和浏览器时不需重装；本机用已有 Edge，CI 默认 Chromium 不变。

真实脚本读取当前仓库本地忽略的 `.cache/b2-live-env.json`，其非秘密结构为：`{status:"ready",base_sha:"270eed81ee9a29f53bc25c25cb9a9992e8b50252",users:{failure:{username,user_id},slow:{username,user_id},stream:{username,user_id}}}`。failure/slow 必须是不同的实际登录账号 ID，stream 为普通合成账号；测试登录使用用例中公开的合成密码常量，不复用真实密码，不把 Cookie/CSRF/数据库密码放入元数据。

A 在另一工作树复现时，按上文官方 test/Fake 装配说明正常创建自己的合成账号、读取实际 ID 并设置选择器，再准备这份非秘密元数据；不要复制真实凭据、猜 ID、直接重用别人的运行进程或把元数据存在当作环境 ready。前端 5173/后端 8080 必须先核对归属。重新完整运行会保留新增测试会话并消费 Fake 配额，应协调窗口；本轮没有为了重复跑而重启清额度。

## 未执行项、接续与阶段状态

- 本轮浏览器链路是 Vite → Go → 固定 MySQL → Fake；未另做 Nginx 浏览器验收、公网、HTTPS、负载/性能或真实模型评测。A 已有后端/代理专项按其原记录使用，不冒充本轮新增浏览器证据。
- B2 功能本批技术验证完成，但不代负责人登记 B2 正式通过。仍需 A 接入本分支、检查整合后的完整 Fake 链路并给出固定 SHA；B 在该 SHA 才做 B3 完整浏览器验收、操作说明和评测准备。
- A 汇总确认完整 A4 与 A5 启动条件；本轮不标 B3/A4/A5、Phase 3 或真实评测通过，不启用任何收费模型。
- 合成账号、会话和失败场景记录均保留；没有 teardown 删除或借重启绕额度。服务仍按本次配置运行，不停止他人服务/数据库。复测需协调窗口并遵守分钟/日额度，不自动重跑完整真实用例。
- 本文件不包含尚未生成的自引用提交 SHA。提交/推送是否完成及完整 SHA 以 Git 交接结果为准；生成提交后仅普通推送本功能分支，不自动 PR、合并或部署。
