# P3 A：B1 策略 Fake 故障装配交接

日期：2026-11-15。基线 `72d2e3d20a62396fb245282e18c736e145d6cd25`；独立 worktree、功能分支 `codex/p3-a-strategy-fake`，最终 SHA 以本次交付提交及交接消息为准。负责人本轮授权本地开发、独占隔离验证、Commit、普通 Push；未授权更新 PR #19、整合分支、真实模型或部署。

## 交付范围

- 服务装配添加两个显式账号 ID 选择器：每场策略首败/后续版本成功、8 秒延迟。缺省关闭，dev/test/demo + fake 限定、部署探针冲突拒绝、ID 校验及同账号场景冲突拒绝。
- 内部策略生成请求转发已认领行版本；不改 HTTP、Prompt、Repository、Gate、额度或认领规则，不直写数据库造失败。
- 复用原 Fake 的 Generate、Summary、Stream；保留 T15 SSE 场景。
- 补装配/账号隔离/服务端版本传递/重建装配/延迟/取消/超时/显式重试/SSE 兼容单元测试；专属 Compose、HTTP 探针与 [运行说明](../../infra/strategy-fake/README.md)。
- Windows 上 Demo 初始化 Bash 缺 LF 属性的基线问题，通过补 `.gitattributes` 修复。没有新增依赖或 SQL 迁移。

## 实际环境与验证

仅 `desktop-linux` / Compose 项目 `siyige-p3-a-strategy-fake`，后端 `127.0.0.1:18081`，MySQL 容器 `siyige-p3-a-strategy-fake-mysql-1` 内部 `mysql:3306/siyige_demo`，应用账号 `siyige_demo_app`、迁移账号 `siyige_demo_migrator`。数据库不发布端口；独立数据卷 `siyige-p3-a-strategy-fake_demo-data`，秘密仅当前 worktree 的忽略目录。未使用共享的 13306/13307 数据库，也未管理另一成员的项目。

已执行：

1. 先新增测试运行 `go test ./cmd/server -run TestStrategyFake -count=1`，按预期发现原装配不限制这些开关、未首败、未延迟；实现后通过。后续服务版本转发及单开关/默认场景测试通过。
2. 清除 `SIYIGE_INTEGRATION`、`SIYIGE_MIGRATION_TEST`，`AI_MODE=disabled` 下后端 `go test ./...`、`go vet ./...`、`go build ./...` 全通过。固定 13307 的数据库集成测试默认跳过；实际 DB 回归通过下述专属 HTTP 链路执行。
3. Compose config、app/migrate 镜像构建、独占 MySQL 初始化、13 个既有前向迁移、逐表应用权限及健康检查通过。
4. `python infra/strategy-fake/probe.py prepare` 仅通过正式注册/登录 API 生成三个合成账号；run 通过正式 HTTP + 真实 Service/Repository/Gate/MySQL，验证其他账号先成功不消耗目标失败、同账号两场首败、failed 持久查询、普通 POST 不重试、错误版本 409、最新版本显式 retry completed。
5. 延迟场景观察到 GET generating、普通与 retry 重复 POST 均 202，原请求 201，之后 GET completed，第一次实测约 8.038 秒。关闭真实 HTTP socket 后查询 failed / generation_cancelled，再按 GET 版本 retry completed。
6. 保持同一 app/mysql 进程、同一账号与数据卷，间隔限流窗口后完整 run 再次通过（延迟 8.025 秒），没有重启补额。只读 SQL 确认 `DATABASE()=siyige_demo`、`CURRENT_USER()=siyige_demo_app@%`，10 个策略行最终 completed，首败/取消后的行均版本 2、延迟和普通行版本 1，模型/Prompt 为 fake/strategy-v1。两个 probe run 均保留记录。

首次初始化因 `infra/demo/init-accounts.sh` 的 CRLF 返回 exit 127；补 LF 后保留原卷，重新启动本任务 mysql，等待恢复就绪后显式补执行未完成的账户初始化，再迁移/授权成功。首次恢复尝试在 MySQL 尚未 ready 时返回 socket 111，未执行后续迁移。没有删除卷、重置数据库或写失败状态。复用内部 isolated 网络时 HTTP 端口未发布，探针在注册前拒绝；专属 app 补入口网后回环发布及 DB 不发布的检查通过。

## 边界与整合

本次技术结果供 A 整合窗口接入；未更新 PR #19、未合入整合分支。B1 浏览器与完整 AC16 尚待受测前端联调及负责人确认，Phase 3 未结项。前端未修改，未运行前端测试/构建/浏览器验收；真实模型、费用、公网和部署均未执行。

复现、账号 ID 配置、POST/GET/retry 正文、停止与精确数据库目标以 [运行说明](../../infra/strategy-fake/README.md) 为准。合成记录与卷保留；重复探针遵守每分钟与每日既有限额，禁止重启补额。交付后专属 app/mysql 停止，其他项目保持不动。负责人验收结论：待确认。

## 整合后固定 test 目标复核

2026-11-15 A 固定基线交接：在干净的 `codex/p3-a-integration` / `877a8730fe0ed84b56bd3e51d7558277e3bbbabd` 上合入已推送的本功能指定 `4c5ffbbb8bfb797243b6bb84d7fa28312e78e917`。B1 `a1e613b6c04243e6599316fa580a9242aa2d8060`、A SSE 后端与费用保护均保留；只有导航文档冲突，保留双方入口。另一 A4/A5 窗口未产出新交付提交，未将其计划视为成果。本轮授权本地开发、功能成果合入、隔离验证、Commit、普通 Push、更新 PR #19；不合 main、不强推、不部署、不真实调用。

为核实 B 要使用的准确目标，确认 13307 空闲后新建本轮容器 `siyige-p3-fixed-handoff-mysql`，独立卷 `siyige-p3-fixed-handoff-test-data`，只发布 `127.0.0.1:13307`。没有启动、重建或修改原 `siyige-t02-mysql-test-1` 或其他人的数据库。使用新生成的秘密文件与既有初始化脚本，显式执行 13 个前向迁移；只读 SQL 验证 `DATABASE()=siyige_test`、`CURRENT_USER()=siyige_test_app@%`、MySQL `8.4.13`。

整合源码构建本窗口后端，实际 `APP_ENV=test`、`AI_MODE=fake`、`DB_ADDR=127.0.0.1:13307`、`DB_NAME=siyige_test`、`DB_USER=siyige_test_app`；HTTP/Origin 独占 `127.0.0.1:18083`，同时启用 `T15_FAKE_SCENARIO=slow`。先以普通 Fake 注册三个新合成账号，按真实响应中的 ID 配置失败/延迟选择器，再启动本窗口故障后端；此后故障测试全程不重启、不重置额度。秘密、账号和临时探针仅在忽略目录 `.cache/p3-handoff/`，不共享给 B。

已执行：

- 后端 `go test ./...`、`go vet ./...`、`go build ./...` 通过；默认数据库 integration 未启用，实际持久化另由本轮 HTTP 链路验证。
- 复用既有策略探针业务断言，只将临时副本的环境保护适配为本窗口容器/端口/固定 test 库，HTTP 真实链路通过：其他账号不受影响、同账号两场首败、持久 failed、普通 POST 不重试、旧版本 409、查询最新版本后显式 retry、generating GET、两种重复 POST 202、最终 completed、HTTP 取消落 failed 后 retry。延迟 `8.045s`；最终 5 个策略行均 completed，版本 1 两行、版本 2 三行，模型/Prompt 为 fake/strategy-v1。没有直写业务表造状态。
- SSE 探针 UTF-8、LF/CRLF、多 data、心跳、半事件 self-test 通过；同一策略装配后端上真实流式 start/4 个 delta/done 通过，首/末 delta 为 0.029/0.630 秒，拼接与落库查询一致。前端未改，复用 `877a873` 的 typecheck、255 单测、build、11 项浏览器 Mock；完整 B 浏览器验收未执行。费用保护实现未变，复用 A 准备和后端专项证据。

首次新 MySQL 初始化因本轮临时秘密文件名不匹配停止；补齐文件名后仅恢复本轮容器，先查确认两个应用/迁移账号均未创建，再显式运行既有账户初始化，保留原卷完成迁移。后台启动自己的后端被自动审核拒绝，改用前台工具会话完成相同隔离验证。一次 SSE self-test 从 backend 错误工作目录执行失败，随后从仓库根执行通过。以上均未操作他人服务或数据。验证结束停止本窗口前台后端及新 MySQL，保留卷和数据；13307/18083 已释放，不交给 B 复用本轮临时凭据。

B 的准确启动入口见 [固定 test 环境给 B 联调](../../infra/strategy-fake/README.md#固定-test-环境给-b-联调)，原 Demo Compose/probe 不能拿来连接固定 test 库。本轮仅补后端目标验证；B1 两项真实浏览器补测仍待 B，B2/B3、完整 A4、真实体验与 10 场评测均未通过。PR #19 已于北京时间 2026-11-15 01:58:56 合并，不能恢复草稿；只更新其交接说明，新 SHA 的 CI 通过手动 workflow_dispatch 检查，最终结果以检查页与交接消息为准。
