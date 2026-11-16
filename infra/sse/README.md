# T15-Core 本地 Fake 流式验证

仅连接固定隔离测试库 `127.0.0.1:13307/siyige_test`，应用账户 `siyige_test_app`，迁移须已到 00013。保持原测试库数据与密码文件，不创建新库、不访问开发/生产库、不读取真实模型密钥。所有命令在本分支仓库根目录执行，PowerShell 7.2+。

## 启动与停止

先按 [MySQL 说明](../mysql/README.md) 核对既有 `siyige-t02-mysql-test-1` 的项目与隔离目标，再启动该测试容器。不要重建卷或启动开发容器。

```powershell
pwsh -NoProfile -File infra/sse/start-local.ps1 -PasswordFile '<固定测试库 test-app.txt 的绝对路径>' -Scenario slow
```

脚本构建后在后台启动后端，绑定 `127.0.0.1:8080`，`APP_ENV=test`、`AI_MODE=fake`、`AUTH_ORIGIN=http://127.0.0.1:5173`。不自动迁移/播种；端口被占用会拒绝替换。输出 PID，停止时核实 PID 对应本次 `.cache/t15-local/server.exe` 后 `Stop-Process -Id <PID>`。日志和 PID 位于 Git 忽略目录 `.cache/t15-local/`。切换场景先停止本次进程，再重新启动，不通过重启绕过业务额度。

另开终端，沿用 B 的 Vite 原代理配置，不修改前端源码：

```powershell
Set-Location frontend
npm ci  # 已安装锁定依赖时无需重复
npm run dev
```

可选本地 Nginx：

```powershell
pwsh -NoProfile -File infra/sse/start-nginx.ps1
```

脚本用已有固定 `nginx:1.30.6-alpine-slim` 镜像，回环映射 `127.0.0.1:18080`。从实际 `site.conf.template` 提取站点，仅替换本地监听/TLS/主机名，继续包含实际 proxy 配置；增加本地专用路由标记验证 SSE location 被选中。既有同名容器必须同时匹配 `siyige.scope=t15-local` 与本 worktree 的精确配置挂载才可重启，不删除容器。停止命令：`docker --context desktop-linux stop siyige-t15-local-nginx`。这是本地 API 代理验收入口，不构成公网或 HTTPS 部署验收。

## 可复用探针

Python 3 标准库，无新增产品依赖。探针只允许三个固定回环入口，会通过 API 创建随机合成测试用户和会话，最后退出登录；不输出密码、Cookie、CSRF 或用户发言。合成用户与会话保留在测试库，便于人工详情重读。每次新探针都产生新测试记录，仍遵守现有限流。

```powershell
python infra/sse/probe.py --self-test
python infra/sse/probe.py --base http://127.0.0.1:8080 --require-incremental
python infra/sse/probe.py --base http://127.0.0.1:5173 --require-incremental
python infra/sse/probe.py --base http://127.0.0.1:18080 --require-incremental --nginx
python infra/sse/probe.py --base http://127.0.0.1:5173 --scenario disconnect
```

探针逐字节读取，严格增量 UTF-8 解码并缓存半行/半事件；支持 LF/CRLF、多 data 行和心跳，EOF 半事件不当成功。`--require-incremental` 断言至少两个 delta 在不同时间到达；最终内容必须与查询到的正式消息一致。`--nginx` 另外断言命中真实模板的 SSE location。测试不能以配置文件存在或单次读完整 body 代替逐块证据。

## Fake 故障与恢复

`T15_FAKE_SCENARIO` 只在显式 dev/test + Fake 下启用，不改变 deepseek 的费用闸门。脚本固定 test 模式。

| 启动场景 | 实际行为 | 探针 |
| --- | --- | --- |
| slow | 每 6 个 Unicode 字符一个 delta，间隔 200ms | 默认 success + `--require-incremental` |
| failure | 一个临时 delta 后受控 error，无 done | `--scenario failure` |
| timeout | 一个 delta 后等待 45 秒模型预算取消 | `--scenario timeout` |
| heartbeat | 首个 delta 后等待 16 秒，触发 15 秒 keepalive，再完成 | 默认 success + `--require-heartbeat` |
| slow + 客户端断线 | 读到首个 delta 后关闭连接，上游取消且正式状态可查 | `--scenario disconnect` |

恢复只用已有 `GET /api/v1/debates/{id}`：未收到 start 时按原 `client_request_id` 匹配。completed 读取正式结果；generating 有限查询；failed 后用户显式 JSON retry，版本取查询结果中的 `attempt_version`，请求体为 `{"expected_attempt_version":<查询值>}`。409 只查，不自动换版本再发。start 不提供版本，无 SSE retry 新路径。

`--scenario lost-done` 是客户端丢弃最终事件后的查询演练；真实“提交成功但下游丢失 done”、提交确认丢失、旧版本迟到与无双写由 Go 隔离 MySQL 专项注入并断言，见 [验收记录](../../docs/acceptance/T15_CORE_BACKEND_ACCEPTANCE.md)。B 的生产解析器与浏览器 AC12 须另行联调，本探针不替代它。
