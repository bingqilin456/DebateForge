# T07 收尾执行包

当前状态见 [T07 验收](../docs/acceptance/T07_DEPLOYMENT_PREPARATION_ACCEPTANCE.md)。以下探针只操作负责人已授权的实验项目。所有现场输出只回传 PASS/BLOCKED、状态码、脱敏请求 ID、计数、完整镜像 ID 与清理结果。

## 实验项目 Go 413 透传

在已验证的 Windows SSH 终端登录 `ssh aliyun-siyige`。将 [探针](t07-upstream-413.sh) 内容保存到 ECS 私有临时文件，或从本机以已验证的 SCP 上传该单个文件。从此前部署记录填写同一实验发布目录、实验 CA 公钥证书、经冻结记录核对的 app、edge 和 MySQL 完整本地镜像 ID。不要从正在运行的容器反向认定它们为已批准 ID。

```bash
bash /path/to/t07-upstream-413.sh "$LAB_ROOT" "$LAB_CA" "$APP_FULL_ID" "$EDGE_FULL_ID" "$MYSQL_FULL_ID"
```

脚本要求本机 Docker 默认 Unix socket、实验 Compose 项目、原卷、内部网络、回环端口及三个原容器都匹配且已停止。只有全部前置匹配才启动原 MySQL 和 app，临时重建同镜像的 edge，将 Nginx 请求体上限改为 64 KiB。两次登录请求仅带合成凭据且超过 Go 上限，分别用固定长度和 HTTP/1.1 分块。预期两条 `PASS_GO_413` 后出现 `PASS_STOPPED`。异常时由脚本停止实验容器并恢复基础 edge 配置。出现 `BLOCKED` 时保持实验入口停用，回传报错阶段及 `docker --context default compose -p siyige-t07-lab ps -a` 的脱敏状态。实验 CA 已过期时先停在 TLS 检查，按原实验 CA 流程准备有效证书。

## Fake 失败、超时和并发

使用已冻结且包含 `T07_FAKE_SCENARIO` 的候选 app 镜像。继续使用同一实验目标校验、现有卷、HTTPS 和回环入口。仅在 `AI_MODE=fake` 且 `APP_ENV=deploy` 的临时 app Compose 覆盖中设置 `T07_FAKE_SCENARIO=failure`、`timeout` 或 `hold`，每个场景单独以完整镜像 ID 核对 app；`hold` 使 Fake 等待约 3 秒。结束时停止实验容器并移除覆盖，恢复普通 Fake app 配置。

- 失败和超时各建一个新会话，提交首轮。期望 503 `model_unavailable` 或 504 `model_timeout`。用相同 `client_request_id` 和正文重放，期望 409 `turn_failed`。本人详情和只读库查询核对同一 turn ID、`attempt_version=1`、完成轮数为 0、仅一条用户消息。回传状态和计数。
- 认证限流先在单一真实代理地址用不同合成用户名测试 IP 维度，使用不同的入站 `X-Real-IP`、`X-Forwarded-For` 和 `Forwarded`。11 次登录应有 10 次 401 和第 11 次 429，注册同一代理来源第 6 次应为 429。用户名维度要用能被 Nginx 实际区分的来源，同一归一化用户名累积 10 次后第 11 次为 429。只有单个真实来源时，记录两个维度尚无法独立验证。
- 生成分钟额度以同一用户在一个分钟窗口发送 7 次，确认第 7 次 429 `rate_limited`。使用新会话与 UUID，跨分钟累计 20 次新生成尝试，在同一 UTC 日以第 21 次验证 429；每批留足分钟配额以区分分钟与日门槛。并发用 `hold`，分别验证同用户第二次，以及两名用户在途时第三名用户的 `generation_busy`。合成账号仅在实验库中使用。
- 查询、正文、Cookie、Authorization 和入站请求 ID 均使用独立合成标记。只在现场日志检索标记和受控请求 ID，回传命中数。记录 `X-Request-ID` 与响应 JSON 的一致性。任何非合成数据留在现场。

## 应用回退

按 [已冻结回退操作](README.md#3-首次受控发布与回退) 先核对候选与上一版各自 app/edge 的完整镜像 ID、目标 Schema 与旧版兼容证据，以及已有备份的预期 SHA-256 和可读性。对 gzip 备份在现场用 `sha256sum --check` 与 `gzip -t` 核对，校验记录须来自备份生成时。按备份格式只读检查预期 Schema 标识和归档元数据，原 SQL 内容留在现场。此批不恢复数据库。缺任一证据，停止实验项目 edge/app 并保留卷。满足时，仅在已核对的实验项目环境持久切换发布标识。健康、同会话复读和常规 up 后镜像 ID 都要复验。若恢复原候选，也须同样核对目标与完整 ID。

## ADR-019 无收费候选与真实启用前置

基础 Compose 保持隔离 Fake/disabled。将 [compose.trial-network.yaml](compose.trial-network.yaml) 叠加到基础 Compose，可用内部网络和合成 TLS 供应商验证出站限制。真实候选还需叠加 [compose.trial.yaml](compose.trial.yaml)，仅在单独获准后使用。应用只接内部网络，供应商 TLS 通过 Nginx 中继。中继只接受 `api.deepseek.com` 的 SNI，并连接固定的 `api.deepseek.com:443`。TLS 仍由 Go 校验，禁直连回退。Fake 验证时将中继的出站网络也设为内部网络，连接合成供应商。真实候选的外部网络、DNS、证书和目标环境另行核对。

同一试运行窗口的隔离及公网槽须按顺序运行并共享同一持久账本。专用私有目录由运维预先准备，应用 UID 10001 需能写入，Key 用独立只读 secret 挂载。公网绑定、放行、DNS 和现场目标由 Phase 4 另行审批。

- 启动前冻结 `window.json`，字段 `start`、`end` 使用 RFC3339 与 `+08:00`，跨度恰为两小时，`users` 是两名隔离预建账号的有序数字 ID。隔离槽仅首位指定账号可触发模型请求，第二位保留在账本。启动期读取，运行时校验未变。公网槽沿用其独立批准名单。
- `isolated-approval.json` 和 `public-approval.json` 字段为 `boot_id`、`window_start`、`start`、`end`、`monitor_at`、`users`。二者绑定同一总窗口。每次启动输出 JSON 事件 `t07_trial_awaiting_approval` 和新 `boot_id`，旧批准文件失效。负责人在场核对账本和终端采集后才绑定新启动标识。先建立终端采集心跳，用现场 `monitor` 的实际 mtime 作为 `monitor_at`，要求处于开始时间之前 15 秒内。批准文件通过同目录临时文件原子发布。公网批准文件只在隔离浏览器实跑明确成功、所有公网前置与资源批准后创建，`users` 使用公网独立库的两个预建账号 ID，公网跨度不超过 30 分钟。
- 负责人在终端接收专用试运行日志。账本 `monitor` 文件只可由确认采集在线的现场流程刷新，最多允许 15 秒间隔。发出预留告警失败、采集陈旧、可检测账本故障、用量未知或模型失败会使新出网停止。独立验证前仍需对接并演练真实终端的心跳和告警接收。终止时在账本目录持久创建 `stop` 文件、关闭真实入口和公网入口，已有调用可能继续计费。恢复须负责人核对账本和在途后另批，移除 `stop` 后重启并以新启动标识批准。仅恢复心跳或移除标记不会解除原进程的锁定。保留两枚 `*-reserved.json`，切换与重启均不重置；已耗尽槽没有补发入口。
- 出网前排他创建与同步一次 16,384/1,024 Token 预留，每环境一次。结果持久化后先发送 `t07_trial_result_persisted`，终端写入成功才发布完成标记。完成标记通过已同步准备文件的硬链接原子发布，挂载须支持本地持久化和硬链接。公网槽同时核对同窗隔离预留、已同步的成功结果及完成标记；最长在途一调用。实际返回用量、未命中峰时估算、可核实结算和余额分别记录。未知用量保留完整预留。结算与余额为空时须在后续只读对账中分别登记，不能把预算或响应估算记成实扣。
- 现场安全审查须另核对 Linux 账本底层持久性、应用停用/恢复、告警采集与终端、全部真实出网配置、回环试运行和最终公网隔离。独立安全审查 PASS 前保持 Fake。真实 DeepSeek 试运行前复核官方价格与 Token 语义。 [DeepSeek 定价](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/) · [请求协议](https://api-docs.deepseek.com/api/create-chat-completion/)
