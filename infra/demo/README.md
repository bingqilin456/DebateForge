# Phase 3 本地 Demo（无收费准备）

本轮只启用 Fake。B1 流派/策略卡和 B2 SSE/恢复尚未交付，当前页面为 A 分支已有前端，**不能用于完整 AC12/AC13/AC16/AC17 验收**。真实体验与 10 场评测均未执行。当前证据、B 分工和缺失授权见 [验收记录](../../docs/acceptance/T15_DEMO_PREP_ACCEPTANCE.md)。

## 固定目标与独占使用

| 项目 | 固定值 |
| --- | --- |
| Compose 项目 | `siyige-p3-demo`；Docker context `desktop-linux` |
| 页面和 API | `http://127.0.0.1:18081`；后端不单独发布端口 |
| 模式 | `APP_ENV=demo`、默认 `AI_MODE=fake`、slow 分块 |
| 数据库 | 项目内 `mysql:3306/siyige_demo`，不发布宿主机端口 |
| 账户 | `siyige_demo_app` 逐业务表最小权限；`siyige_demo_migrator` 只供显式前向迁移 |
| 数据卷 | `siyige-p3-demo_demo-data`；与 `siyige_test`/`siyige_deploy` 完全分开 |
| 账本卷 | `siyige-p3-demo_demo-ledger`，容器内 `/var/lib/siyige/demo`（0700、UID 10001） |
| 数据库秘密 | 本 worktree `.cache/p3-demo/secrets`；初始化脚本不覆盖已有完整集合 |
| 密钥 | 默认没有模型密钥；另获授权后只给后端挂载 `/run/secrets/model_key` |

体验开始前 A/B 约定占用时段、固定 Git SHA 和谁负责停止。另一成员体验中禁止执行构建替换、`up --force-recreate`、restart、迁移、重置或清库；不删除卷、账本或计数文件。已有 8080/5173/18080 和测试库不由本环境管理。数据库集成只接受固定 `127.0.0.1:13307/siyige_test/siyige_test_app`，拒绝 demo 库；本项目测试不应重启任何共享数据库，只清理各夹具自己创建的匿名记录。

## 首次启动（PowerShell 7.2+，仓库根目录）

先检查 `git status --short --branch` 与 `docker --context desktop-linux compose -f infra/demo/compose.yaml ps`，确认没有另一成员正在体验。若已运行直接访问，不为刷新计数重启。

```powershell
pwsh -NoProfile -File infra/demo/init-secrets.ps1
$env:P3_DEMO_SECRETS_DIR = (Resolve-Path .cache/p3-demo/secrets).Path
docker --context desktop-linux compose -f infra/demo/compose.yaml config --quiet
```

前端依赖未安装时执行 `npm ci`；已安装直接检查并构建。B1/B2 到齐后必须在整合提交重新构建，不能挂载另一分支的旧 dist。

```powershell
Push-Location frontend
npm ci
npm run typecheck
npm run test
npm run build
Pop-Location
docker --context desktop-linux compose -f infra/demo/compose.yaml build app migrate
docker --context desktop-linux compose -f infra/demo/compose.yaml up -d --wait --wait-timeout 180 mysql
docker --context desktop-linux compose -f infra/demo/compose.yaml run --rm migrate up
docker --context desktop-linux compose -f infra/demo/compose.yaml exec -T mysql bash /opt/siyige/mysql-client.sh root --execute='source /opt/siyige/grant-app.sql'
docker --context desktop-linux compose -f infra/demo/compose.yaml up -d --wait --wait-timeout 90 app edge
docker --context desktop-linux compose -f infra/demo/compose.yaml exec -T edge nginx -t
```

逐条检查退出码；`migrate up` 只执行未执行的前向 SQL，服务启动不自动迁移/播种。数据库初始化只发生在新空卷，口令文件改变不会改已有账户。`grant-app.sql` 由 root 在这个专用隔离库显式执行；应用无 DDL 和 Goose 历史权限。依赖与镜像沿用仓库版本，无新增 Go/npm 产品依赖。

打开页面，注册/登录合成账号，查看辩题与历史；页面当前缺少 B1/B2 入口，完整体验步骤待交付。届时按“选题→立场→四流派之一→创建→显式生成策略卡→四轮 SSE→显式总结→刷新→退出重登→历史重读”执行。分钟额度满时等待，不自动重复调用。

无人体验时可执行 `python infra/demo/probe.py`，先核对固定容器 `APP_ENV=demo` + `AI_MODE=fake`，再走 18081 的完整后端 HTTP 链路：四流派、分页/空列表/非法值、一策略+四轮增量 SSE+总结、重登/历史。它只新增合成记录并保留，不重启、不迁移、不重置，绝不替代 B 的生产解析器和浏览器专项。

## 停止

先通知同伴结束体验，再停止本项目，保留数据库和账本：

```powershell
docker --context desktop-linux compose -f infra/demo/compose.yaml stop edge app mysql
```

不提供 reset、删除 stop、删计数、清卷或自动补额命令。恢复使用同一数据卷、账本卷与批准配置。

## 真实模式的准备边界（本轮不可执行）

`compose.real.yaml` 仅为将来单独获准后的叠加配置：`AI_MODE=local-demo`；Go 只经 `trial-relay:8443` 出网，TLS 验证 `api.deepseek.com`；无裸适配器回退、重定向、SDK 二次请求或失败自动重试。默认 Compose 没有中继、外网网络和模型密钥。旧 T07 `deepseek` 单次试运行入口保持原规则。

负责人须批准以下所有值并冻结到私密 `approval.json`；仓库样例故意全零无效，不能拿来启用。下列代码允许范围是验证上界，不是批准额度。

| 参数 | 责任/要求 | 当前 |
| --- | --- | --- |
| id / 固定 SHA / 模型 | 本次授权编号、A/B 同一受测提交；固定适配器模型 `deepseek-flash`，调用前重新核实能力及价格 | 未批准 |
| users | A、B 或负责人指定的两个真实账户 ID，匿名登记，不把合成测试账号当授权 | 未批准 |
| start/end | UTC `Z` 时间，明确负责人在场窗口，最长 72 小时；每次出网前必须余留完整 45 秒预算 | 未批准 |
| max_calls | 全窗口收费尝试数，含失败/超时/未知；代码最多 1000，不代表可以使用 1000 次 | 未批准 |
| max_input_tokens / max_output_tokens | 全窗口输入/输出最坏预留上限 | 未批准 |
| per_call_input_tokens | 完整请求 JSON 的 UTF-8 字节数 + 1024 协议预留，不裁剪历史；最多 200000 | 未批准 |
| per_call_output_tokens | 每次实际 max_tokens，1024–8192 | 未批准 |
| 两个 *_micro_cny_per_million | 冻结的无缓存保守价格（每百万 Token 的微元人民币），必须独立核实并留据 | 未批准 |
| max_cost_micro_cny | 全窗口人民币最坏估算上限，1 元 = 1000000 微元；向上取整，不用实耗退回预留 | 未批准 |
| 独立余额/负责人/紧急停用人 | 专用服务端密钥、独立可消耗余额范围及余额观察；本地估算不等于供应商绝对封顶 | 未批准 |

六次成功是“一策略+四轮+总结”的单场下界；10 场至少 60 次，失败不能补调。现有 Gate 的 6 次/分钟、20 次/用户/UTC 日、每用户并发 1/全局 2 保持不变；账本额外持久限制真实出网，重启不恢复这些已预留额度。两个批准账号各最多 3 场/UTC 日（18 次），10 场需至少两个 UTC 日和合适的批准窗口。

将来批准后，由 A 在 Fake 停止且同伴离开环境时，用 UID 10001 写入账本卷 `approval.json`（0600），配置 `P3_DEMO_MODEL_KEY_FILE` 为服务端专用密钥的绝对路径，再显式叠加 real 配置。启动冻结 `manifest.json` 并生成 `boot.json`，此时仍拒绝调用。负责人核对预算/模式/窗口/余额后，将本次 boot_id 写入 `enable.json` 才能生成；重启产生新 boot_id，旧 enable 自动失效。不得换账本目录规避限制。真实环境的密钥、中继/TLS与网络专项仍需在获准后验证，本轮无真实出网结论。

紧急停用：由停止负责人在账本卷写入 `stop`；进程每 100ms 观察，在途请求会取消，出网前与结果结算均再次校验。可在容器内以应用用户执行 `umask 077; printf operator_stop > /var/lib/siyige/demo/stop`。然后停止 app 与 trial-relay；不得删除 stop 自动恢复。已经发往供应商的请求可能继续计费，最多两个在途且均已全额预留；若停止发生在费用包装已返回成功之后，业务提交可能已经完成，保留正式结果并按查询核对，不能假称回滚或退款。

账本先 O_EXCL 创建预留、同步文件/目录，再原子更新含累计次数和预留摘要的 head，全部成功后才出网；结果另写不可覆盖记录。单实例 flock，重启复核批准/manifest/head/全部预留和结果；尾部缺失、更新中断、配置变化、缺失/损坏、未完成记录、写失败、usage 未知/超界、异常退出均拒绝新真实调用并保留预留。失败/超时/取消/未知不记 0，不释放最坏成本。head 用于发现误删和中断，不对持有宿主机权限的主动篡改提供保证，禁止修改或替换账本卷。预留与业务提交是不同边界：数据库提交未知继续按既有轮次查询恢复，不能因此释放已可能收费的预留。
