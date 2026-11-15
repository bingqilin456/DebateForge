# A：B1 策略故障 Fake（本地独占）

源功能基于 `72d2e3d20a62396fb245282e18c736e145d6cd25`，交付分支 `codex/p3-a-strategy-fake`、指定提交 `4c5ffbbb8bfb797243b6bb84d7fa28312e78e917`；现已接入 `codex/p3-a-integration` 并保留 B1。正式 HTTP/额度/认领规则均不变；B 的固定测试库浏览器联调用下节，原独占 Demo HTTP 验证入口保留在后文。

## 原理与选择

环境变量均为可选，空值关闭；只允许显式 `AI_MODE=fake` 与 `APP_ENV=dev|test|demo`：

| 配置 | 行为 |
| --- | --- |
| `P3_STRATEGY_FAKE_FAILURE_USER_ID` | 指定账号的每个新会话第 1 个策略认领返回 model_unavailable；Service 正常保存 failed。版本 2 起普通 Fake 成功 |
| `P3_STRATEGY_FAKE_SLOW_USER_ID` | 指定另一账号的每次策略生成等待 8 秒，再用普通 Fake 返回；Context 取消/超时立即结束 |

账号 ID 必须是非零 uint64，两个 ID 不能相同；可单独启用。其他账号普通成功，互不消费故障。没有全局首调计数，没有数据库注入。取消后 Service 使用现有清理路径保存 `generation_cancelled`，最新版本显式 retry 可成功（仍等待 8 秒）。同一 completed 会话只重放；复现场景请新建会话，不清库或改状态。失败与重试正常占用生成额度：每用户每天 20 次、并发 1、生成 POST 每分钟 6 次、全局并发 2 均沿用。

内部 StrategyRequest 增加服务端认领版本，不进入 Prompt 或 HTTP；Service 只转发当前数据库行的版本。禁止与 T07 部署探针混用，关闭/真实模式携带这些开关会拒绝装配。`T15_FAKE_SCENARIO=slow|failure|timeout|heartbeat` 的 SSE 行为仍由原生成器处理，不被策略开关替换。

## 固定 test 环境给 B 联调

这两个策略开关可直接用于 `APP_ENV=test`、`DB_ADDR=127.0.0.1:13307`、`DB_NAME=siyige_test`、`DB_USER=siyige_test_app`、`AI_MODE=fake`，不要求 demo 模式。`T15_FAKE_SCENARIO` 只影响轮次 SSE，不能代替策略故障开关；可同时设为 slow 供 B2 使用。

先与环境使用者约定窗口，确认固定测试库已运行、迁移到 00013、应用口令文件匹配当前库，且 8080 无人占用。不要执行后文 Demo Compose/probe 去接管此库，也不要自动重启/重置既有 MySQL。两个合成账号须通过正常注册/登录取得，ID 从登录响应或 `GET /api/v1/auth/me` 的 `data.user.id` 读取，不猜 ID，不共享凭据。若暂时没有账号，可先以空选择器启动普通 Fake，注册后仅在空闲窗口停止自己的后端，再配置实际 ID；不得借重启绕过已消费额度。

在固定整合版本的仓库根目录执行（替换三个占位值）：

```powershell
$env:P3_STRATEGY_FAKE_FAILURE_USER_ID = '<失败测试账号实际 ID>'
$env:P3_STRATEGY_FAKE_SLOW_USER_ID = '<另一延迟账号实际 ID>'
$env:T07_FAKE_SCENARIO = ''
pwsh -NoProfile -File infra/sse/start-local.ps1 -PasswordFile '<当前固定测试库 test-app.txt 的绝对路径>' -Scenario slow
```

现有脚本继承上述策略选择器，固定设置 APP_ENV/test、AI_MODE/fake 和准确 DB 目标；后端 `127.0.0.1:8080`、Origin `http://127.0.0.1:5173`，Vite 原 `/api/` 代理即可。脚本遇 8080 已占用会拒绝替换；若已有后端未启用选择器，必须由该进程使用者安排切换，不能只给浏览器设置变量。停止时核对脚本输出 PID 与当前 worktree 的 `.cache/t15-local/server.exe`，只停止自己的进程；不要停止数据库或其他人的 Vite。

触发步骤见下文 **B1 手工触发与查询**：失败账号每个新会话首次策略 503 并持久 failed，查询版本后明确点击重试才成功；延迟账号生成约 8 秒，期间另一标签页/HTTP 客户端 GET 为 generating，重复 POST 为 202。前端同一面板的防重锁不应为了观察 202 而移除。首次悬挂请求保持到返回，再查询 completed；已 completed 会话不重新生成，复测需新会话并保留数据。

本轮已在上述固定 test 地址/库及 Fake 下用独占新数据卷实测 HTTP/Service/Repository，结果见 [装配记录的固定 test 复核](../../docs/acceptance/P3_A_STRATEGY_FAKE_ACCEPTANCE.md#整合后固定-test-目标复核)。这不是 B 浏览器补测通过，也不改变正式配额。

## 独占 demo 精确资源与启动

仅使用 Docker context `desktop-linux`、Compose 项目 `siyige-p3-a-strategy-fake`：

| 目标 | 固定值 |
| --- | --- |
| 后端 HTTP / Origin | `http://127.0.0.1:18081` |
| MySQL 容器 / 库 | `siyige-p3-a-strategy-fake-mysql-1` 内部 `mysql:3306/siyige_demo`，不发布 DB 端口 |
| 应用 / 迁移账号 | `siyige_demo_app` / `siyige_demo_migrator` |
| DB / 账本卷 | `siyige-p3-a-strategy-fake_demo-data` / `siyige-p3-a-strategy-fake_demo-ledger` |
| 网络 | 同项目前缀的 `demo` 内网、`ingress` 入口网 |
| 秘密 | 当前独立 worktree 的 `.cache/p3-demo/secrets`；不复用其他 worktree |

与旧 `siyige-p3-demo` 同为 18081，不能同时运行。启动前检查下列输出：若端口被其他项目占用，或本项目已有其他成员使用，停止本次启动并约定窗口；不要停止/重建对方服务。不要用 `-p` 更换项目名去绕过独占约束。

以下从本分支独立 worktree 根目录、PowerShell 7.2+ 执行。每条命令检查退出码，失败即停止；不执行清卷/重置：

```powershell
git status --short --branch
docker --context desktop-linux ps -a
Get-NetTCPConnection -State Listen | Where-Object LocalPort -eq 18081
pwsh -NoProfile -File infra/demo/init-secrets.ps1
$env:P3_DEMO_SECRETS_DIR = (Resolve-Path .cache/p3-demo/secrets).Path
$env:P3_STRATEGY_FAKE_FAILURE_USER_ID = ''
$env:P3_STRATEGY_FAKE_SLOW_USER_ID = ''
docker --context desktop-linux compose -f infra/strategy-fake/compose.yaml config --quiet
docker --context desktop-linux compose -f infra/strategy-fake/compose.yaml build app migrate
docker --context desktop-linux compose -f infra/strategy-fake/compose.yaml up -d --wait --wait-timeout 180 mysql
docker --context desktop-linux compose -f infra/strategy-fake/compose.yaml run --rm migrate up
docker --context desktop-linux compose -f infra/strategy-fake/compose.yaml exec -T mysql bash /opt/siyige/mysql-client.sh root --execute='source /opt/siyige/grant-app.sql'
docker --context desktop-linux compose -f infra/strategy-fake/compose.yaml up -d --wait --wait-timeout 90 app
python infra/strategy-fake/probe.py prepare
```

`prepare` 先核对固定容器、Fake 模式、回环端口、独立 DB 卷和 DB 无端口发布，再通过 API 注册三个合成账号。凭据仅保存在已限制 ACL 的秘密目录内 `strategy-probe.json`，不输出密码/Cookie/CSRF。已有该文件就复用，不重复 prepare。不要提交或分享该文件。也可自行通过正式注册/登录 API 获取两个测试账号及 ID，指定如下变量即可。

仅在本任务独占时段重新装配本项目 app；mysql 不重启。使用 probe 生成的账号：

```powershell
$probeAccounts = Get-Content .cache/p3-demo/secrets/strategy-probe.json -Raw | ConvertFrom-Json
$env:P3_STRATEGY_FAKE_FAILURE_USER_ID = $probeAccounts.failure.id
$env:P3_STRATEGY_FAKE_SLOW_USER_ID = $probeAccounts.slow.id
docker --context desktop-linux compose -f infra/strategy-fake/compose.yaml up -d --wait --wait-timeout 90 app
python infra/strategy-fake/probe.py run
```

`run` 新建会话并保留所有合成记录，覆盖其他账号先成功、同账号两场首败、failed 查询/普通 POST 重放、错误版本 409、最新版本 retry、generating 查询、重复 POST 202、completed 查询、真实 HTTP 断开后 failed 及 retry。每轮 failure 账号恰好 6 个生成 POST；再次 run 前距上轮首个生成 POST 至少 60 秒，日额度不足则换合成账号并在空闲窗口更新 ID，不能重启补额或清计数。

## B1 手工触发与查询

在对应账号登录后创建新会话，赛前尚无 turn，使用正式 Session、Origin 和 CSRF：

1. `POST /api/v1/debates/{id}/strategy`，正文 `{}`。failure 账号返回 HTTP 503 / `model_unavailable`。
2. `GET /api/v1/debates/{id}/strategy` 返回 `failed` 和最新 `attempt_version`；刷新/重登仍读取该行。普通 POST 仍 failed，不自动重试。
3. 用户点击重试时，POST 正文 `{"retry":true,"expected_attempt_version":<GET取得的版本>}`；HTTP 200 / completed / 版本加 1。旧版本返回 409，应先查询最新状态。
4. slow 账号发送首个 POST 后，该请求约 8 秒内未返回。保留该请求，在另一窗口 GET 可见 generating；重复 POST `{}` 或同版本 retry 均 HTTP 202 / generating。原请求最终 HTTP 201，之后 GET completed / 版本不变。
5. 关闭 slow 的在途 HTTP 连接，再查询直至 failed / generation_cancelled；显式 retry 约 8 秒后 completed。探针已自动执行此项，不靠客户端展示猜测状态。

## 停止与恢复

体验结束，停止本任务项目，保留卷与合成记录：

```powershell
docker --context desktop-linux compose -f infra/strategy-fake/compose.yaml stop app mysql
```

后续恢复需同一 worktree 秘密与 ID 配置，再 up mysql/app；不重新 prepare。关闭故障应在空闲窗口将两个变量清空并重新装配本项目 app。不要执行 down --volumes、prune、全局 stop、共享测试库清理或重启。本次没有真实调用或部署授权。

## 验证

普通后端检查清除 SIYIGE_INTEGRATION/SIYIGE_MIGRATION_TEST，AI_MODE=disabled，执行 `go test ./...`、`go vet ./...`、`go build ./...`。专用 HTTP 探针承担本次实际 MySQL/Service/Repository/Gate 端到端验证；不把 demo 库传入固定 13307 测试入口。

实际结果见 [本次交接](../../docs/acceptance/P3_A_STRATEGY_FAKE_ACCEPTANCE.md)。无新增依赖或迁移；Compose 复用现有版本。Windows Demo Bash 的 LF 检出规则已补齐。
