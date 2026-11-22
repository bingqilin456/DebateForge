# T16 运维验证记录

本记录覆盖部署前演练和获准的 `syg` 私有 IP 部署。该范围已于 2026-11-22 获负责人签收。日常站点保持 Fake；真实模型只在另启的私有候选完成一次合成首轮。公网开放与既有生产库操作未执行。

## 2026-11-22 补验（北京时间）

受测基线为 `5447890e6e5d08a619c15f73fa34ca36377d9fb9`。A2、A2-R、B2 的本地恢复、同卷回退、32 项单测和 10 项 CLI 检查沿用原记录。下面的具体收件和握手时间均为 UTC。

| 工作线 | 本轮核实 | 现场所缺资源 |
| --- | --- | --- |
| A 异机恢复 | `syg` 临时目录实测存放与取回加密备份。三文件摘要一致，取回副本恢复进本机独立空库，经认证读回预置历史。RPO 约 18.1 秒，RTO 约 14.9 秒。 | 正式源环境、持久异机存储与留存周期、目标恢复环境尚待确认。 |
| B 故障与告警 | 本机和 `syg` 隔离 Compose 均收到应用、MySQL、备份任务故障及恢复通知。`syg` 通过 Nginx HTTPS 探测应用和 MySQL，备份过期分支沿用本机接线证据。 | 正式调度及状态目录、正式收件端和端到端送达记录。 |
| C TLS | 本机和 `syg` 的独立 Nginx 均通过测试 CA 换证、重载、新连接握手及错误配对告警演练。 | 负责人确认目前没有正式域名。DNS-01 客户端、正式证书续期与实际握手留待域名就绪后取证。 |
| D 回退与费用 | 在 `syg` 核对 app 镜像 ID、Schema 13 和同一数据卷，应用切换前后健康且可登录读回同一历史。修复了部署授权脚本的两处表权限缺漏，已在隔离库验证。 | 正式回退窗口和发布镜像、T07-COST-01 的费用账户及 Key 脱敏标识、充值限制、预算和持久账本范围。 |

### A 异机取回与读回

源与恢复库分别是本机的 `t16a-backup-source/siyige_t16a_source` 和 `t16a-backup-restore/siyige_t16a_restore`，使用独立内网和卷，MySQL 无宿主端口。源测试库用仓库 00001–00013 的 SQL 建表并写入合成辩论，恢复库预启动且为 0 表。两端使用同一提交的应用源码和真实认证、仓储路径，模型使用 Fake。临时构建中的 Go overlay 仅允许应用连接这两套白名单库和对应账户，产品源码没有修改。前几次准备过程中遇到客户端响应解析、Git Bash 容器路径转换和测试限流问题。前序合成数据留在本轮源库，本次按新会话标记核对。

`syg` 作为与源主机独立的存储端。用 SSH 在权限为 `0700` 的临时目录存放同批次的 `.sql.gz.age`、`.sha256` 和 `.manifest.json`。三文件在源端、ECS 和取回目录的 SHA-256 均一致。age 私钥留在本机，与备份文件分开。密文 `t16a-field-1122-r2.sql.gz.age` 为 4991 字节，SHA-256 为 `126f91866e6cc72d19be114b1bdd70c2a124a9e04592d8b75c8476a95ceac6d9`。临时密文副本仍留在 ECS，本次未建立持久备份存储。

| UTC（11-22） | 实测 |
| --- | --- |
| 14:03:38.343 | 源应用提交预置的第 1 轮，返回 201。 |
| 14:03:43–14:03:44 | 实际备份脚本返回 `BACKUP_OK`，源库 8 表。 |
| 14:03:50.810 | 源应用提交备份后第 2 轮标记，返回 201。 |
| 14:03:56.457 | 源 MySQL 停止，随即开始从 `syg` 取回。 |
| 14:04:01–14:04:07 | 从取回副本校验、解密并执行 `RESTORE_OK`，目标库 8 表，users 6 / debates 5 / messages 12。 |
| 14:04:11.155–14:04:11.309 | ready、登录、me 和历史详情均返回 200。详情可读备份内标记，未出现备份后标记。后续用 `messages.turn_id` 关联复核本场为 2 条消息，两个标记命中分别为 1 和 0。 |

**RPO 约 18.1 秒**，以源 MySQL 停止时间减去最新可读标记提交时间。**RTO 约 14.9 秒**，以同一停止时间为起点，涵盖从 ECS 取回、恢复和通过认证读回历史。恢复库已预启动，演练数据量较小，这两个数值适用于本次拓扑与数据量。首次恢复后的 SQL 附加核对用了不存在的列，命令退出失败，随后已按表关系重新取证。 [备份与恢复工具](../../deploy/ops/backup/README.md)

### B Compose 故障与回环收件

项目 `t16b-field-1122` 使用独立网络、新命名卷和独立库，MySQL 8.4.13 已应用 13 个迁移。应用镜像 `t16b-app:1d974c9` 对应的 `backend` 和部署 Dockerfile 与基线无 diff。健康检查直连随机回环端口，MySQL 无宿主映射。数据库停机首次探测的 2 秒客户端超时早于服务端 ready 判定，恢复后用 7 秒预算重做，取得了下面的 503 证据。每个真实故障均先恢复服务再推进，结束时应用和数据库均为 `healthy`。

| 收件编号 | UTC（11-21） | 收件端 HTTP | 观测 |
| --- | --- | --- | --- |
| `B-app-01` / `B-app-02` | 18:04:05 / 18:04:07 | 204 / 204 | 应用停止时 live、ready 均失败。重启后两项通过。 |
| `B-db-01` / `B-db-02` | 18:04:44 / 18:04:48 | 204 / 204 | MySQL 停止时 live 为 200/alive，ready 为 503/not_ready。重启后 ready 为 200/ready。 |
| `B-backup-job-01` / `B-backup-job-02` | 18:02:48 / 18:02:48 | 204 / 204 | 合成任务失败、恢复分别送达。失败时保留上次成功时间。 |
| `B-backup-age-01` / `B-backup-age-02` | 18:02:48 / 18:02:48 | 204 / 204 | 合成快照实际已过期 25 小时，即使文件 mtime 很新也报过期。切换到新快照后恢复。 |

接线以备份任务 exit 0、`BACKUP_OK` 以及密文、摘要、清单一致为成功依据。调度包装器原子发布探针所需的 `ok` 和 `last_success_utc`。成功时取清单的 `dump_started_at` 为快照新鲜度参考，失败时写 `ok=false` 并保留上次成功时间。探针分别报告任务状态与快照过期。11-22 另将实际脚本在缺失公钥时的退出码 1 发布为失败，保留上次成功时间。再读取本轮真实的 `BACKUP_OK`、密文、摘要和清单发布恢复。临时 HTTP 接收端分别在 14:04:56 UTC 收到 `B-backup-real-job-01`、14:04:57 UTC 收到 `B-backup-real-job-02`，均返回 204。过期仍用夹具验证。正式发送器及调度尚未接通。 [探针接口](../../deploy/ops/health/README.md)

### C 测试 CA 证书更换

使用 Docker 中的 `nginx:1.30.6-alpine-slim`，挂载独立证书目录，仅绑定随机回环端口。测试 CA 只由探测客户端显式信任，客户端核过签发链和 SAN `t16c-lab.test`，三次均以新建连接完成 TLS 1.3 握手。初次准备的 CA 缺签发用途扩展，客户端拒绝验证。补齐 `keyCertSign` 和 `cRLSign` 后完成以下演练。实际使用的是临时 Nginx TLS 配置，目标站点部署钩子和 ACME 调度未运行。

| UTC（11-21） | 证据 |
| --- | --- |
| 18:03:33.397 | 旧证书序列号 `03E9`，SHA-256 `9763f5d69e2a648aef7c74e74f459004c3b92cedba331ba0dace82a0f977a67a`。 |
| 18:03:33.646 / 18:03:33.653 | 临时换入不匹配的证书后 `nginx -t` 拒绝，未重载。收到 `C-tls-failed`（HTTP 204），随后新连接仍读到旧证书。 |
| 18:03:34.065 / 18:03:34.067 | 配对文件就绪后 `nginx -t` 和 `nginx -s reload` 成功。新连接读到 `03EA`，SHA-256 `8e9eb54df1a085b12c464f4022b1720d00cdbbd887145f14d742740f8678e3ea`，接收端收到 `C-tls-recovered`（HTTP 204）。 |

两张证书有效期均为 `2026-11-21T18:03:32Z` 至 `2026-11-23T18:03:32Z`。正式取证沿用部署说明的 DNS-01 方式，确认实际客户端和域名后，在新连接记录 SAN、证书链、指纹和有效期。 [HTTPS 续期检查](../../deploy/README.md#4-https-续期检查)

### D 目标回退核对与费用证据

本机 `siyige-t16-rollback:4c5ffbb` 完整镜像 ID 为 `sha256:dddbed06c647360f950943c8dc4237702dd48a69b5b0ab877d300f608e90be35`，OCI revision 为 `4c5ffbbb8bfb797243b6bb84d7fa28312e78e917`。Docker 本机列出的 RepoDigest 与该 ID 同值，正式镜像仓库可拉取性及正式配套 edge 镜像待核。候选与 `5447890` 的迁移文件无 diff，A2-R 已验证测试环境 Schema 13 上的旧应用读路径。目标切换前先核对 MySQL 版本和实际迁移清单，确认无额外结构变更、旧发行配置兼容、备份可用和维护窗口已授权。记录同一 Compose 项目的 MySQL 容器 ID、启动时间和 `/var/lib/mysql` 卷名及来源。按批准镜像切换 app/edge 后复核这些标识、实际运行镜像、经 HTTPS 的 live/ready，以及合成账号登录后同一条历史。正式目标窗口尚未排定。 [回退步骤](../../deploy/README.md#3-首次受控发布与回退)

基础 Compose 默认 `AI_MODE=disabled`，真实模型试运行使用独立覆盖配置及持久账本。以下本地证据沿用 T07 验收。目标冻结版本需核对实际配置及账本行为。

| 项目 | 已有证据 | 目标待核 |
| --- | --- | --- |
| 充值兜底 | ADR-022 已确认可用充值余额配合应用控制。历史本地单次调用授权已消耗，实际结算仍未知。 | 账户及 Key 脱敏标识、获批充值上限与可核对余额或硬限额。 |
| 01A 全站次数与 Token | 本地计数桩覆盖多用户触顶、调用前预留、重启及账本不可用时拒绝。 | 周期、时区、批准限额、账本挂载，计数桩验证超额零出网。 |
| 01B 熔断与停用 | 本地验证控制故障后拒绝新增，保留在途预留。 | 目标跨重启停用状态、拒绝新增、接收与处置记录。 |
| 01C 对账 | 历史记录已区分估算、实际扣款与余额。 | 按需核对脱敏结算账单、收件与处置。 |
| 01D 重试与在途 | 本地计数桩覆盖重定向、超时、429/503、取消和重复准入。 | 真实公网试运行前对冻结版本核实应用、SDK 与中继无隐式重试。按需核对在途上限与预留敞口。 |

01A/01B 仍是公网开放前置。本轮未读取供应商账户、未触发产品真实模型调用。 [T07 本地验收](T07_DEPLOYMENT_PREPARATION_ACCEPTANCE.md#本地无收费验证负责人人工验收) · [T07-COST-01](../planning/TASKS.md#t07-cost-01-供应商硬预算缺失)

### `syg` 现场隔离演练

获准在 `syg` 创建临时项目 `t16-syg-field-1122-r3`，使用独立卷、内部数据库网络、合成数据与 Fake。仅 Nginx 绑定宿主机回环随机端口，app 和 MySQL 无宿主绑定。远端旧 T07 网络占用默认代理段，测试另用独立子网。MySQL 采用约束内存的测试配置。

首次启动时容器无法读取初始化脚本，修正权限后使用新卷。第二次迁移在版本 7 超时，实际约束已出现、版本账本仅记录至 6。停止该测试容器并保留原卷，在第三个空卷以降低资源占用的配置完成全部 13 项迁移。两次失败均发生在本轮新建的隔离项目。

受测 app ID 为 `sha256:00e86b2917d24ce8cb1558fda89b34970c5e03a211ae8d9d0e0c7107132daddd`，候选 app ID 为 `sha256:dddbed06c647360f950943c8dc4237702dd48a69b5b0ab877d300f608e90be35`。edge 为主机已有镜像 `sha256:940c5aaaed09c66b5b1a3eef1eadc46d07306d4b569aa583171578d2cc1473a8`，迁移镜像为 `sha256:c1661f34ece33030c66ebab8a0bcc0a519d426110bb2afa83052693750ed7359`。

| 收件编号 | UTC（11-22） | 观测 |
| --- | --- | --- |
| `SYG-app-01` / `SYG-app-02` | 14:16:19.396 / 14:16:20.770 | 停止应用时，经 Nginx 检查 live 和 ready 均失败。应用启动后两项恢复。 |
| `SYG-db-01` / `SYG-db-02` | 14:16:24.602 / 14:16:27.426 | 停止 MySQL 时，经 Nginx 检查 live 200/alive、ready 503/not_ready。MySQL 启动后 ready 200/ready。 |
| `SYG-backup-01` / `SYG-backup-02` | 14:16:27.800 / 14:16:27.897 | 实际备份脚本缺公钥退出，探针检测失败。重新登记真实密文、清单和摘要校验结果后恢复。 |
| `SYG-tls-01` / `SYG-tls-02` | 14:16:28.358 / 14:16:28.794 | 不匹配的私钥使 `nginx -t` 拒绝重载。配对文件到位后校验通过，重载后新连接读取新证书。 |

八条故障与恢复通知均由 `syg` 的临时 HTTP 接收端确认（204）。TLS 使用测试 CA 和 SAN `t16-syg.test`，新建 TLS 1.3 连接证实序列号自 `03E9` 切换为 `03EA`。旧 SHA-256 为 `a0a1070731656c47f920aeaeb4538e86c9acd181f32e58e0a7dcd1c98e5152f3`，新 SHA-256 为 `a7a943bee9e69797fa4088ae50f8414041ee688e9e46dc17714b0dfec4ea2038`，两张测试证书均在 `2026-11-24T14:15:39Z` 到期。

回退前首次读取历史详情返回 503。对应用账户直接只读核对发现 `debate_summaries` 和 `debate_strategies` 均出现 MySQL 1142。按已授权范围修复 [逐表授权脚本](../../deploy/grant-app.sql)，参照 Demo 增加两张业务表的 `SELECT、INSERT、UPDATE`。仅在本轮隔离库应用，确认两表可读、六项权限实际存在，Goose 历史仍返回 1142。修复后本场历史与策略接口均返回 200。

14:18:52 UTC 确认切换前基线 app、健康和本人历史，Schema 最高版本 13，Goose 含 14 条已应用记录（含版本 0）。14:19:05 UTC 启动候选 app 后，HTTPS 健康、登录和本人历史均为 200，历史 JSON 与切换前一致。卷 `t16-syg-field-1122-r3_mysql-data`、卷创建时间、MySQL 容器和 edge 容器标识保持一致，Schema 记录保持一致。14:19:07 UTC 再次执行同配置 `up`，确认候选镜像持续生效。14:19:20 UTC 恢复基线 app 并确认健康。演练结束后已停止该项目服务，保留隔离卷和脱敏收件日志。

#### `syg` 费用门禁无收费补验

以 `5447890` 后端源码构建 Linux 测试程序，在 `syg` 现有 app 镜像内以 UID/GID 10001、只读根文件系统及 `--network none` 运行。两个独立容器先后挂载同一个 `0700` 私有宿主目录，第二个容器读回第一个容器写下的合成文件。随后在同一目录执行 9 项定向检查，全部通过。覆盖原子预留、重启后额度与批准约束、账本或心跳故障停用、在途预留、未知用量、流式结果和计数桩拒绝自动重发。测试程序 SHA-256 为 `fc9ee3068df5387b4caec64a2d2d703655b4220b8e2965fb53f717405a454b24`。

该结果新增 `syg` 上的宿主机绑定挂载与费用门禁机制证据。正式账本挂载路径、账户/Key 范围、批准额度和可核对的供应商账务仍待冻结。未挂载或读取供应商密钥，无真实模型请求。

现场资源复核只发现已停止的旧 T07 实验项目和本轮 T16 演练项目。现有旧项目保留不动。当前验收材料未登记正式域名、持久异机存储、正式告警端点或对应的已批准配置。

以上现场证据的目标是临时隔离 Compose。正式部署仍需确认持久异机存储与恢复环境、通知调度和实际收件端、正式域名的 ACME DNS-01 续期与握手、正式发布镜像及回退窗口、T07-COST-01 和负责人验收。

### 正式环境补证据的执行口径

负责人已确认拟用 `syg`，目前无正式域名，并授权按推荐方式安排备份、收件与首次真实模型试运行边界。隔离结果保留为部署前证据，正式验收仍以实际部署记录为准。

| 项目 | 取证方式 | 当前缺口 |
| --- | --- | --- |
| 部署与回退 | 在 `syg` 冻结正式 app/edge/migrate 镜像 ID，记录实际 Schema 与卷标识。首次优先沿用已验证兼容的 `4c5ffbb` app 候选，正式切换前重核。通过私有入口依次核对健康和本人历史，切换后再核对同卷、健康与历史。 | 待正式发布配置和实际操作窗口落地。 |
| 异机备份 | 通过受限任务每日生成加密备份，推荐保留最近 7 份日备份。仅向独立主机或私有对象存储复制密文、摘要和清单，从该处随机取一份还原到空隔离库，登录核对历史及 RPO/RTO。 | 正式源库接线与长期存储位置待落地。现有备份工具锁定隔离项目。 |
| 现场通知 | 首选现有的负责人运维邮箱或私有 Webhook。使用正式调度器和接收端，依次观察故障与恢复，保存脱敏收件时间。 | 已有 HTTP 回环记录，正式接收端暂无现成记录。 |
| 证书 | 无域名的阶段走 SSH 隧道与测试 CA。有域名后配置 DNS-01，留存续期成功、失败与恢复通知、Nginx 重载后新 TLS 连接的证书。 | 当前无正式域名。 |
| 费用 | 先在 `syg` 私有入口验证正式挂载的持久账本和停用链路。费用门禁与独立审查通过后，使用后端已有专用秘密来源，仅执行 `deepseek-flash` 单账号合成首轮一次，最多一条在途。 | 本机 `backend/.env` 中已核对到对应变量名，未输出密钥。目标仅通过只读秘密文件注入所需 Key。正式账本、脱敏账户范围和费用资料仍待落地。 |

按 2026-11-22 DeepSeek 人民币峰时未命中费率，16,384 输入及 1,024 输出 Token 的单次应用账本预留为 **0.04096 元**。实际扣款单列。失败、超时或结果不明即停用并保留预留，无自动重试。此次核对未启动真实模型或产生供应商账单。 [DeepSeek 官方定价](https://api-docs.deepseek.com/zh-cn/quick_start/pricing/)

### `syg` 私有部署、回退和异机备份实证

负责人在本轮确认按私有部署方案推进。新建项目 `siyige-private-t16`，配置放在受限目录 `/opt/siyige-private-t16`。14:45:16 UTC，Nginx 仅绑定 `127.0.0.1:18443`，app 和 MySQL 没有宿主绑定。MySQL、应用与 Nginx 均为 healthy。应用使用 Fake。工作站通过 SSH 隧道验证新连接为 TLS 1.3，测试证书 SAN `siyige-private.test`，首页、live、ready 均返回 200。

| 项目 | 已取得的证据 |
| --- | --- |
| 当前镜像 | app `sha256:00e86b2917d24ce8cb1558fda89b34970c5e03a211ae8d9d0e0c7107132daddd`，edge `sha256:5a5aaf4707e32af44716232b323ba8778babd6040c90f83a21095fa51c53ddd7`，迁移 `sha256:c1661f34ece33030c66ebab8a0bcc0a519d426110bb2afa83052693750ed7359`。离线标准多阶段打包缺 npm 缓存，改用相同锁文件及 Node 24.11.1 从基线构建前端，再用基线 Nginx 配置打包。 |
| 同卷回退 | 14:45:17 UTC 经 HTTPS 登录并读回合成历史。14:45:31 UTC 切换到候选 `4c5ffbb` 后，Schema 仍为 13，卷 `siyige-private-t16_mysql-data`、卷创建时间、MySQL 和 edge 容器均保持一致，健康、登录和历史详情成功。14:45:44 UTC 恢复基线 app 后再次读回相同历史，健康正常。 |
| 异机备份与恢复 | 在 `syg` 对私有部署的合成库使用事务一致性快照，源端 age 加密后取回到工作站私有目录。密文 3781 字节，SHA-256 `0783ea1444fe6440a46ce59b34104d633885820490cac7655c772b6f6b1585c9` 与源端一致，私钥留在工作站。将取回副本导入工作站无网络的空库，0 表恢复为 9 表，Schema 13，预置历史标记命中一次。恢复核对用时约 17.1 秒，以目标容器已启动后等待可用至 SQL 查回计量。本次工作站恢复只核到 SQL 层，应用登录读回和完整 RPO/RTO 沿用前述异机隔离演练证据。 |

私有项目保持运行。工作站的独立恢复容器已停止并保留数据卷。每日异机调度与工作站收件见下一节。正式 ACME 续期待域名。

### 私有部署日备份、工作站收件与费用复核

- `syg` 已启用 `siyige-private-t16-backup.timer`，每天北京时间 10:15 生成加密快照，错过的宿主调度会补跑。密钥仍留在工作站。工作站任务 `DebateForge T16 Backup and Alerts` 在用户登录时启动、每 5 分钟检查并取回源端最近 7 个归档，逐份校验摘要。手动触发服务与任务均返回 0，已经实际复制并校验密文。用户未登录时工作站取回和收件顺延，服务端保留副本；本轮未删除旧备份。
- 串行验证了应用停机、数据库断开和备份任务失败。工作站收到应用、数据库及备份的故障与恢复，共 6 条所需证据。备份单次实际因源库不可用返回失败，恢复后服务返回 0，新密文已复制并验证。工作站所有检查最终为正常。收件证据落在工作站持久文件，Windows 应用事件日志未观察到对应副本。两机时钟有约 1–3 秒偏差，收件时间仅用于记录各端顺序。
- 首次备份服务在目标机因 Python 版本缺少 `hashlib.file_digest` 失败，改用兼容的流式 SHA-256 并重试成功。定时器已启用并核对下一次运行时间，本轮未等待到次日日程触发。
- DeepSeek 官方余额接口只读复核返回可用、币种 CNY，余额可覆盖 ¥0.04096 的单次预留。未充值、未输出密钥或具体余额。合成候选在两个均无外网的内部网络上验证冻结 edge 的费用中继可转发获准 SNI、拒绝其他 SNI，并拒绝客户端 TCP 直连。从中继握手的新 TLS 连接确认合成目标证书校验通过。随后在无密钥候选上实测中继到 DeepSeek 官方的 TLS 验证通过，其他 SNI 和内网客户端直接出站均被拒绝。这次握手未发送 HTTP 请求。
- 冻结应用镜像单独在 `--network none` 候选中使用合成密钥和私有账本。工作站流程捕获启动告警，并绑定该启动 ID 的合成批准。停止续心跳后约 16.5 秒收到停用事件。重启更换启动 ID，并在同一账本上读到持久停用标记。预留记录为零。首次夹具因文件时间戳精度提前触发停用，使用实际纳秒时间戳后通过。
- 费用独立审查确认当前私有项目仍使用 Fake，对外试运行候选尚未在实际只出站拓扑中冻结。待将费用中继、账本、当前启动 ID 授权和专用心跳收件串接并复审，才能执行已授权的一次真实首轮。工作站的 5 分钟运维任务无法承担费用的 15 秒心跳租约。本轮模型供应商生成请求为零。 [DeepSeek 余额接口](https://api-docs.deepseek.com/api/get-user-balance/)

### 冻结候选费用复审与私有单次首轮

- 实际部署形态的冻结候选使用相同 app 与 edge 镜像、私有数据库网、内部中继网和独立的中继出站网。使用合成密钥复验，应用 ready 200，无宿主端口和直接出站路由，中继验证 DeepSeek 官方 TLS，拒绝其他 SNI。账本已挂载，工作站接收与 boot ID 绑定，心跳中断约 15.93 秒后写入停用，零预留。
- 独立费用安全复审 **PASS**，仅覆盖已授权的一次私有合成首轮。冻结预执行、付费执行器和工作站收件端 SHA-256 分别为 `d71d92007e44b53c8122698ef344560bd6e3c04cc9f3c0f46b2837809dd524f3`、`bb071a0641ab5155bc0eda1aeabe6a6957803fbb2fad2675d1c8d2cb5e669f0f`、`4b92e1f0d79c8b5fad9140831525986649c85a7c95fa905f88e5bfcbb29b7e6e`。复审确认范围为 `deepseek-flash`、一名合成操作账号、一条在途、16,384 输入及 1,024 输出 Token，峰时预留 ¥0.04096，无自动重试、公网和生产数据。
- 工作站收到该次启动事件并实际写盘，随后绑定批准。唯一首轮请求返回 HTTP 503，数据库记录第 1 轮 `failed / model_unavailable`。账本没有生成预留或结果文件。应用传输层在持久预留成功后才可向供应商发送生成，因此该请求止于准入，供应商生成请求为 **0**。证据不支持成功生成或结算金额。
- 已核对到具体时序。返回 503 的 UTC 时刻为 `15:36:06.261915`，早于批准开始 `15:36:06.325121`，执行器收到第一个心跳就发起了请求。下一版执行器草稿已用该时间轨迹验证会等到批准生效且收到开始后心跳，并验证停用时拒绝。已执行的窗口不重开，未重试，也未启用下一版草稿。
- 清场确认：该账本的停用标记已落盘，本轮候选应用和中继已停止，临时 Key 已移除。原 `siyige-private-t16` 保持 Fake，三项服务健康。正式证书仍待域名，真实首轮成功证据仍缺。

### 私有 IP HTTPS 与真实首轮成功

负责人明确使用 IP 访问，私有部署以 `https://127.0.0.1:18443` 经 SSH 隧道作为验收入口，证书使用该 IP 的 SAN，由独立私有 CA 签发。工作站显式信任该 CA。域名、DNS 解析和公网证书均不在本次私有 IP 范围内。若将来开放公网且仍使用公网 IP，可采用支持短期 IP 证书的公共 ACME 客户端，届时另验公网入口与续期。 [Let's Encrypt IP 证书](https://letsencrypt.org/2026/01/15/6day-and-ip-general-availability/)

| UTC（11-22） | 新增现场证据 |
| --- | --- |
| 15:49:45 | 按本轮授权，在复审通过的 v2 私有候选只触发一次 `deepseek-flash` 合成首轮。执行器等到批准窗口开启且收到新鲜心跳后发起。HTTP 201，预留 1 次、¥0.04096，供应商返回 357 输入 / 172 输出 Token（共 529）。按峰时未命中估算 ¥0.00209，实际结算未核对。登录后读回本轮原始合成用户消息和非空助手回复。 |
| 15:49:46 | 试运行账本写入持久停用，移除临时 Key，候选应用和中继均已停止。日常私有站点仍使用 Fake。 |
| 15:50:31 | 切换到回环 IP，证书 SAN 为 `IP Address:127.0.0.1`，序列号 `0BB9`，TLS 1.3，首页、健康、注册登录与本人身份正常，MySQL 容器保持一致。 |
| 15:50:33 后 | 更换 IP 证书并通过 `nginx -t` 和重载。工作站经 SSH 隧道直接访问 IP，新 TLS 1.3 连接看到序列号 `0BBA`、SHA-256 `efeba011674501ca2b50074fc9ca6eee77bf3730cf02ad94278c5d1282d634f3`，首页、live 和 ready 均返回 200。 |

v2 独立费用复审结论 **PASS**，仅覆盖这次私有单调用。运行配置、付费执行器、工作站收件端 SHA-256 分别为 `35bfb2fc2b9da98a083bd6a0c577a253168f452bd206f660a491ce62019e186a`、`f4aaabaaec84b45caf04de6e1b03aa4f194fa0c26ce9f7cec363cfb59107b01e`、`e95473813a24a3bfd00288fb6c65e6f1af8c8959881f1fd856c9c3cedbd4bc73`。公网真实模型费用准入仍由 T07-COST-01 单列。

### 负责人签收

2026-11-22（北京时间），负责人确认签收 T16 私有 IP 部署范围，并授权提交、推送及创建 PR。签收覆盖私有 IP HTTPS 和实际证书更换、加密异机备份与独立库恢复、故障与恢复送达、Schema 13 同卷回退，以及一次真实 DeepSeek Flash 合成首轮。该次真实首轮已停用，日常服务继续使用 Fake。

私有入口通过 SSH 隧道访问。公网开放、公共证书和真实模型公网费用准入仍分别归 T17 与 T07-COST-01。该次调用的供应商实扣金额未核对。

以下保留各轮演练的历史记录，以本节补验更新后的状态为准。

## 已验证

- B1 健康与备份新鲜度探针：在整合工作区执行 `python -B -m unittest discover -s deploy/ops/health -p 'test_*.py'`，27 项通过。覆盖 live/ready HTTP 状态、JSON 和 status 契约、非回环拒绝、错误/缺失/过期/空归档、有限数值参数及不回显输入内容。CLI 冒烟仅对本地合成回环服务和临时文件执行。
- B2 本地合成故障/恢复可观察路径（2026-11-21，本机，无 Docker）：同一套件现为 **32 项通过**（B1 的 27 项 + B2 新增 5 项），并在改动前基线复现出 2 项**夹具时间依赖缺陷**后修正。另有一次真实 CLI 子进程冒烟，10 项检查全部符合预期（详见「B2 线」）。此项只证明探针能把故障态与恢复态判成两个稳定、可区分的信号，**不证明告警已投递、证书已续期或目标环境恢复流程就绪**。
- A1 备份脚本隔离静态/拒绝路径测试：`pwsh -NoProfile -File ./deploy/ops/backup/tests/run-static.ps1`，本轮最终 **76 项通过、0 项失败**（A2 演练开始时基线实测 61 项；演练中为 D1–D7 补断言至 72 项，代码复核发现 D8 后再补 4 项 fail-closed 断言至 76 项）；`tests/age-roundtrip.sh` 的 age 加解密、截断密文拒绝、错误私钥拒绝均通过。仅测试脚本与 age，不是数据库备份/恢复实测。
- 上述证据只证明工具逻辑与静态边界；不证明 T16 目标环境控制通过，也不证明 A08 的 RPO 24 小时/RTO 2 小时已达到。

## 尚未验证

- A1 工具已交付；演练开始时本机没有 age 可执行文件且工具镜像未构建。该状态已改变：本轮已在工作区外私有目录安装 age v1.2.1 并构建工具镜像，随后完成 MySQL 快照、完整加密备份、独立库恢复、合成辩论历史读取与 RPO/RTO 实测（见「A2 线」），并补做旧镜像同卷回退验证（见「A2-R 线」）。**这些仍是本机隔离证据，异机取回、目标环境与生产范围未验证。**
- B2 已按「本机隔离范围」完成探针侧证据：应用/数据库不可用、备份失败/过期与各自恢复信号的可观察路径已用合成 HTTP 服务与合成归档验证，并修正测试夹具的时间依赖缺陷（见「B2 线」）。**仍未执行**：真实容器/Compose 故障注入、任何告警投递与恢复通知（仓库无发送器，不可验证）、正式证书续期与 TLS 握手（仓库无续期钩子，工具无 TLS 能力）、目标环境 Nginx 透传与超时。应用镜像回退的**本地同卷**部分已由「A2-R 线」覆盖，但目标环境的回退流程未验证。
- 未连接 T07-COST-01 生产账户或费用证据；本地无收费探针不能证明生产预算、持久账本、熔断、停用或在途调用边界。
- 无 ECS/异机目标及现场收件人证据；无公网、生产数据库、生产证书或真实模型操作。

## 部署后补证据

| 检查 | 通过依据 |
| --- | --- |
| 异机加密备份与恢复 | 从独立主机或存储取回密文，核对完整性；密钥独立保管；在隔离库验证本人历史可读，按演练说明记录 RPO/RTO，不向生产库导入。 |
| 正式证书与告警 | 核对实际续期、Nginx reload 后新连接的 SAN、证书链和有效期；应用、数据库、备份失败或过期的通知及恢复通知到达现场收件人。 |
| 应用回退 | 记录当前及回退镜像标识，核对目标 Schema 兼容性；只切换应用，确认卷未换、健康与本人历史正常。若没有兼容旧版本，登记缺口。 |
| 费用运维 | 按 [T07-COST-01](../planning/TASKS.md#t07-cost-01-供应商硬预算缺失) 核对目标配置。公网开放前验证 01A/01B；真实模型公网试运行前另核对无隐式重试和调用授权。 |

## A2 线：部署前本地隔离备份/恢复演练（2026-11-20，本机 Docker，非生产证据）

本节记录 A 线在本机 Docker 上的真实执行结果：用交付的 `deploy/ops/backup` 工具生成加密备份，恢复到独立空 MySQL，再经应用认证读回演练前预置历史。所有时间均为 UTC。**这是本地隔离证据，不能替代异机、目标环境或生产验收**；本节结论不得用于声明生产 RPO/RTO 已达标。

> 关于「尚未验证」一节原先记的「未执行 MySQL 快照、完整加密备份、独立库恢复、合成辩论历史读取或 RPO/RTO 实测」：该缺口已在**本机隔离范围**内由本节实测补齐（该条已同步改写）；异机取回、目标环境与生产范围仍未验证。

### 范围与授权边界

- 代码基线：`codex/t16-operations` HEAD `28c5bd79f100b2f73ca181dd5bbaf3085bb4de96`，演练开始时工作区干净。
- 仅新建并使用两个隔离 Compose 项目：`t16a-backup-source`（库 `siyige_t16a_source`）与 `t16a-backup-restore`（库 `siyige_t16a_restore`），两套独立口令文件、独立数据卷、独立 `internal` 网络。
- 未触碰任何既有 `siyige-*` 项目、库、卷或容器；未部署、未开公网、未调用真实模型（应用侧统一 `ai.NewFake()`）、未写生产数据、未删除卷。
- 全部密钥、口令与演练产物在工作区外的私有目录，未提交、未写入本文档。

### 环境与镜像标识

| 组件 | 标识 |
| --- | --- |
| Docker 引擎 / Compose | Linux 引擎 `29.8.2`，Compose `v5.5.1` |
| MySQL 服务端 | `mysql:8.4.13` = `sha256:a81d043930e51745522bd5c430961cf8286f0a23f0a9a2a6e0108f7ea71b0902`；`SELECT VERSION()` 实测 `8.4.13` |
| 备份/恢复工具镜像 | `t16a-backup-tool:v1.2.1` = `sha256:3c98d8f0dd03324a4f6cc7ae1175fa7c302fa5bb37050fe209d07b23316d6c8b`（由 `deploy/ops/backup/Dockerfile` 构建，age v1.2.1） |
| age（主机侧） | `age v1.2.1`、`age-keygen v1.2.1`，`go install filippo.io/age/...@v1.2.1` 安装于工作区外私有目录 |
| 迁移工具 | `goose v3.29.0`（与 `backend` 依赖同版本），临时镜像 `t16a-goose-cli:v1.2.1` = `sha256:226253ccaaba7e6964508cfa5cb03864f3ec0d6f6651dbec49704247675e6697` |
| 应用侧读回探针 | `t16a-a2-probe:v1.2.1` = `sha256:242d334446c947ef9b1e9456f6cdb00729c6c40dce6df999167b333a6a8aa5c3` |

读回探针说明（必须按此口径理解证据）：探针用**同一提交的真实后端源码**（`internal/auth`、`internal/debate`、`internal/topic`、`internal/ai`、`internal/platform` 路由）编译，走真实 gin 路由、真实 Session Cookie 与 CSRF 校验、真实仓储 SQL，模型为 `ai.NewFake()`。它**不是**目标部署镜像，也不含 `.env` 或口令。之所以不用现成应用镜像：`platform.LoadConfig`/`OpenDatabase` 与 `backend/tools/migrate` 都硬性要求库名为 `siyige_<env>`、账户名 `siyige_<env>_app`，无法指向本次白名单库 `siyige_t16a_*`；改用探针是为了在不修改后端契约、不新建白名单外库的前提下仍走真实认证代码。

### Schema 与演练数据

- 迁移：`goose up` 对 `siyige_t16a_source` 应用 `00001`–`00013`，成功到版本 13（`goose: successfully migrated database to version: 13`）。因 `backend/tools/migrate` 只接受固定库名，本次直接用同版本 goose CLI 执行同一批迁移文件；`goose_db_version` 共 14 行、`MAX(version_id)=13`。
- 恢复前后表结构一致：9 张表 `auth_sessions, debate_strategies, debate_summaries, debate_turns, debates, goose_db_version, messages, topics, users`，全部 `InnoDB` / `utf8mb4_0900_ai_ci`；服务端 `@@character_set_server=utf8mb4`、`@@collation_server=utf8mb4_0900_ai_ci`、`@@session.time_zone=+00:00`。
- 预置本人历史：用户 `t16a_a2_owner`（Argon2id 口令哈希由真实 `auth.HashPassword` 生成）→ 1 场辩论（topic 1、`user_stance=pro`、`ai_stance=con`、`mode=standard_4`、`ai_style=logical`、`total_rounds=4`）→ 多轮 turn 与 messages。写入全部经探针 HTTP API（register → login → create → turns）完成，非直接改库。
- 标记设计：RPO 基准标记写入用户消息 `content`；备份后再写入一条「备份外」标记，用于证明恢复点不包含备份之后的数据。

### 时间线与实测结果

| UTC 时刻 | 事件 |
| --- | --- |
| `10:04:08`–`10:04:28` | 预置历史写入：用户、辩论、round 1（标记 `T16A-A2-SEED-20261120T1004Z-R1`）、round 2（标记 `…T100428Z-R2`） |
| `10:12:33`–`10:12:39` | 第 1 次真实加密备份（label `t16a-a2-20261120t1012z`）成功，见下方产物；该产物后续被判定不可用于恢复（见缺陷 D4） |
| `10:13:04` | 备份后写入标记 P1 `T16A-A2-POSTBACKUP-20261120T101251Z`（round 3） |
| `10:20:38`–`10:20:41` | 第 2 次真实加密备份（label `t16a-a2-20261120t1020z`，**本次用于恢复的有效产物**） |
| `10:20:51` | 备份后写入标记 P2 `T16A-A2-POSTBACKUP2-20261120T102051Z`（round 4）；`completed_rounds=4`、`status=completed` |
| `10:21:23` | **模拟故障时点**：停止源 `mysql` 服务（`docker compose stop mysql`，容器 exited，源卷未删） |
| `10:21:29` | **RTO 起点**：宣布开始恢复，启动 `t16a-backup-restore` 项目（`up -d --wait` 返回 0，目标 Healthy，恢复前目标表数 `0`） |
| `10:21:36`–`10:21:39` | `t16a-restore.sh --confirm-empty-target` 执行成功：`RESTORE_OK`，`tables=9`，`row_counts=` users 1 / debates 1 / messages 6 |
| `10:22:05` | **RTO 终点**：探针 ready 且认证读回成功 |
| `14:51:48`–`14:51:59` | 复跑一次完整恢复（清空目标后）验证可重复性：`tables_before=0` → `RESTORE_OK` → `tables=9` |
| `14:52:18` | 复跑后再次认证读回，结果与前次一致 |

有效产物（`t16a-a2-20261120t1020z`）：

- 密文 `t16a-a2-20261120t1020z.sql.gz.age`，3952 字节，头部 21 字节 `age-encryption.org/v1`。
- `sha256=b9eb24eac99d5fff9ea89d0bd70b2faedd6909d7a64894cd35971ea00a1d8214`（`.sha256` 文件与 manifest 一致）。
- manifest：`schema=t16a-backup/v1`、`source_project=t16a-backup-source`、`source_db=siyige_t16a_source`、`mysql_version=8.4.13`、`table_count=9`、`consistency=mysqldump --single-transaction --quick`、`encryption=age v1.2.1 (X25519 recipient)`、`payload_gzip_valid=true`、`dump_started_at`/`dump_finished_at=2026-11-20T10:20:38Z`。
- 解密核验（在私有临时目录内，明文随后删除、未入库不提交）：332 行、`CREATE TABLE` 9 处、`LOCK TABLES` 0 处；标记命中 R1=1、R2=1、P1=1、P2=0。

恢复后经应用认证读回（`http://127.0.0.1:<随机回环端口>`，仅回环发布）：

- `GET /api/health/live` → 200 `{"status":"alive"}`；`GET /api/health/ready` → 200 `{"status":"ready"}`。
- `POST /api/v1/auth/login` → 200（用户 `t16a_a2_owner`，返回 Session Cookie 与 `csrf_token`）；`GET /api/v1/auth/me` → 200。
- `GET /api/v1/debates` → 200，返回 1 条，`completed_rounds=3`。
- `GET /api/v1/debates/1` → 200，`turns=3`、`messages=6`，含 round 1/2 的 R1、R2 标记与 round 3 的 P1 标记，`summary_status.status=not_started`。
- 备份点负例断言：`has_R1=True`、`has_R2=True`、`has_P1_backup_internal=True`、`has_P2_post_backup_must_be_false=False`（P2 确实不在恢复点内）。
- 结论：**恢复点包含备份时刻之前已提交的本人历史，且不包含备份之后的写入**；被写入的是同一用户的本人数据，未出现越权读取。

### RPO / RTO 计算（本地口径）

- **RPO**：`模拟故障时点 − 恢复后实际可读的最后一条预置标记时间` = `2026-11-20T10:21:23Z − 2026-11-20T10:13:04.981353Z` ≈ **8 分 18 秒**（恢复点内最后可读标记为 P1；P2 写于备份之后，按预期丢失）。
- **RTO**：`宣布启动恢复 → 应用健康且能读回预期历史` = `10:21:29Z → 10:22:05Z` ≈ **36 秒**（其中 restore 脚本自身 `10:21:36`–`10:21:39` 约 3 秒）。
- 对照 [T16_PLAN](../../deploy/T16_PLAN.md) 的 A08 目标（RPO 24 小时 / RTO 2 小时），本机演练结果远低于上限。**但本地隔离环境不含异机传输、真实数据量、网络与存储抖动、DNS/TLS 切换等因素，不能据此认定生产达标。**

### 负例（拒绝路径，均在隔离库上执行）

| 编号 | 场景 | 实测结果 |
| --- | --- | --- |
| N1 | 目标库非空（已有 9 张表） | 拒绝：`目标库非空（9 张表），恢复只允许空目标`，exit 1，未写入 |
| N2 | 真正空目标但缺少 `--confirm-empty-target` | 拒绝：`缺少 --confirm-empty-target，未执行恢复`，exit 1，目标仍 0 表 |
| N3 | 合法 age 私钥但非该 recipient（错误密钥） | 拒绝：`解密失败（密钥错误或密文损坏），未写入目标库`（底层 `age: no identity matched any of the recipients`），exit 1，目标仍 0 表 |
| N4 | 密文被篡改（改写偏移字节） | 拒绝：`密文摘要不匹配（副本损坏或被篡改），未写入目标库`，exit 1 |
| N5 | 用源项目名当恢复目标 | 拒绝：`项目名被拒绝：只允许 t16a-backup-restore，实际为 t16a-backup-source`，exit 1 |
| N6 | 私钥与密文位于同一目录 | 拒绝：`私钥与密文位于同一目录，要求密钥与密文分离`，exit 1 |
| N7 | 缺少必需参数 | 拒绝：`必须提供 --identity-file`，exit 2 |

补充：N2/N3 首次尝试因目标库当时非空，被前置的「非空目标」检查先拦下，未真正验证到目标分支；随后清空目标库（0 表基线）重做，才得到上表结果。清空仅针对本次新建的白名单库 `siyige_t16a_restore`。

### 演练中发现并修复的工具缺陷

以下缺陷都会**直接阻塞真实演练**（此前静态测试未覆盖这些运行路径），已在 `deploy/ops/backup` 内修复并补可复现断言：

| 编号 | 位置 | 缺陷 | 修复 |
| --- | --- | --- | --- |
| D1 | `bin/t16a-backup.sh` 暂存目录 | 暂存目录名用单个 `$`，不含 PID；同 label 并发备份会共用暂存目录并互删明文/错发产物 | 抽出 `lib/t16a-lib.sh` 的 `staging_dir()`，以 `$$` 拼进程号 |
| D2 | `bin/t16a-lib.sh` 端口断言 | `docker ps --format '{{.Ports}}'` 对仅 `EXPOSE`、未发布的端口也打印 `3306/tcp`，导致标准 mysql 镜像被误判「已发布宿主端口」，备份永远无法执行 | 改为读 `HostConfig.PortBindings`，仅真有绑定时拒绝；抽出 `normalize_published_ports()` 便于无 Docker 静态验证 |
| D3 | `bin/t16a-lib.sh` `compose_for()` | Git Bash/MSYS 把容器内绝对路径 `/opt/t16a/...` 改写成 Windows 路径，`compose exec` 找不到容器内脚本 | 仅对 compose 调用关闭 MSYS 路径改写，并用 `cygpath -w` 传 compose 文件（非 MSYS 环境行为不变） |
| D4 | `sql/dump.sh` | ① `database=` 写进 `[client]` 段导致 `mysqldump: unknown variable 'database='`；② `--events` 触发 `Access denied ... (1044)`（迁移账户无 EVENT 权限）；③ 默认 `LOCK TABLES ... WRITE` 包裹使恢复端（无 LOCK TABLES 权限）导入失败 | ① 库名改用位置参数；② 去掉 `--events` 并注明权限前提；③ 加 `--skip-add-locks`。D4 修复前的产物 `t16a-a2-20261120t1012z` 已判定不可恢复，保留仅作对照 |
| D5 | `bin/t16a-restore.sh` 私钥解析 | 用 `tr -d '\r\n'` 拼合整个文件，而 `age-keygen` 默认带 `# created:`/`# public key:` 注释行，导致真实私钥被误拒 | 改为只取第一条 `^AGE-SECRET-KEY-1` 行 |
| D6 | `bin/t16a-restore.sh` 摘要文件命名 | 期望 `<label>.sql.gz.sha256`，而备份发布的是 `<label>.sha256`，真实恢复必然报「缺少摘要文件」 | 统一为 `${artifact%.sql.gz.age}.sha256` |
| D7 | `bin/t16a-restore.sh` 源/目标库校验 | 调用未定义函数 `manifest_source_db`，命令行报 `command not found`，该防线实际失效 | 改为 `manifest_field source_db` |
| D8 | `bin/t16a-lib.sh` 端口断言 fail-open | `assert_no_published_ports()` 用 `docker ps ... \|\| true` 与 `docker inspect ... \|\| true` 把查询失败吞成空输出：Docker 不可用、权限不足或 CLI 报错时会被当成「没有容器」或「没有绑定端口」而**放行**，隔离保证建立在查询失败之上 | 去掉 `\|\| true`：`ps` 非零退出 → 拒绝并提示「不能确认宿主端口未发布」；任一容器 `inspect` 非零退出 → 同样拒绝。仅「查询成功且确实无绑定」才放行 |

D8 是代码复核（Codex review）发现的 fail-closed 缺陷，A2 实测时因 Docker 正常所以未暴露；它会让最该保护的场景（Docker 异常）恰好失去保护，因此单列一条并按 fail-closed 修复。

回归测试：`pwsh -NoProfile -File ./deploy/ops/backup/tests/run-static.ps1` 结果 **76 项通过、0 项失败**（本次演练前基线实测 61 项）。新增断言覆盖 D1/D2/D6 的判定口径、「age-keygen 多行私钥必须被接受」，以及 D8 的四种情形：`docker ps` 失败必须拒绝、`docker inspect` 失败必须拒绝、查询成功但无绑定端口必须放行、确实存在绑定端口必须拒绝。后四项用 shell 函数覆盖 `docker` 命令实现，**不需要真实 Docker**。`bash -n` 语法检查通过，脚本保持 LF、无 CRLF。此处与第 8 行 A1 已统一为同一实测值。

### 本次未覆盖 / 已知边界

- **异机证据缺失**：备份与恢复都在同一台主机、同一 Docker 引擎的不同卷上，不构成「异机取回」证据；密钥与密文虽分目录存放，但同机保管。
- **旧版应用镜像同卷回退验证**：首次尝试因当时授权仅覆盖 `siyige_t16a_*` 两个白名单库而停止（旧镜像强制 `DB_NAME == "siyige_"+APP_ENV`，指向恢复库直接被拒）。**后续获得白名单外隔离库单次授权后已完成**，结果见下方「A2-R 线」；本节未做任何迁移/重置/替换卷操作这一事实仍然成立。
- **应用 HTTP 进程口径**：读回使用的是同提交源码构建的临时探针（真实认证/路由/Fake 模型），不是部署镜像进程；目标部署镜像在 `siyige_t16a_*` 库名下的启动路径仍未验证（受配置强约束限制）。
- 未验证真实数据量下的备份窗口、长事务、大表、并发写入压力；未验证告警投递、证书、Nginx、费用相关项目。
- 本机原先缺少 `age`/`age-keygen`，本次为完成演练在工作区外私有目录安装了 v1.2.1；工具镜像 `t16a-backup-tool:v1.2.1` 也在本机构建，SHA 见上表。

### 收尾与残留

> 本节记录 A2 演练刚结束时的状态。**后续获得的清理授权已执行完毕，最终清理结果见文末「清理结果」**，两处不一致时以文末为准。

- A2 结束时已停止本次创建的全部演练服务：`t16a-backup-source-mysql-1`、`t16a-backup-restore-mysql-1`、`t16a-a2-probe-source`、`t16a-a2-probe-restore` 均为 Exited；当时未删除任何容器、卷或网络。
- A2 结束时保留的卷：`t16a-backup-source_mysql-data`（创建于 `2026-11-20T09:33:02Z`）、`t16a-backup-restore_mysql-data`（创建于 `2026-11-20T10:13:39Z`），均为演练新建、非既有数据卷。
- A2 结束时保留的网络：`t16a-backup-source_db`、`t16a-backup-restore_db`（`internal: true`）。
- 他人容器 `eino-qdrant` 全程未触碰。
- 变更范围：仅 `deploy/ops/backup/` 下 5 个文件（`bin/t16a-backup.sh`、`bin/t16a-lib.sh`、`bin/t16a-restore.sh`、`sql/dump.sh`、`tests/run-static.ps1`）与本文件；未提交、未 push、未改 TASKS、未改后端契约。构建私有探针时曾临时改动 `backend/go.mod`，已还原至 HEAD，最终 `git status` 不含该文件。

### 结论

在本机 Docker 的隔离范围内，交付的备份/恢复工具已能完成「真实加密备份 → 独立空库恢复 → 经应用认证读回本人预置历史」的完整闭环，负例拒绝路径按设计生效，并因此发现并修复了 8 项会阻塞真实演练的工具缺陷（D1–D7 由演练直接阻塞暴露，D8 由后续代码复核发现）。**该结果只支持「工具在隔离环境可用」这一判断，不支持「T16 生产备份恢复已验收」或「生产 RPO/RTO 已达标」**；异机取回、目标环境与旧版镜像回退仍需在获得相应授权与目标环境后单独验证。

## A2-R 线：旧版应用镜像同卷回退验证（2026-11-20，本机 Docker，非生产证据）

本节是 A2 之后单独获得授权的补充验证，目的是回答「切换应用镜像、数据卷不变时，旧版能否启动并读回本人历史」。**仍为本地隔离证据，不构成回退演练在生产环境通过。**

### 授权与范围

- 本轮获得**一次性白名单外隔离库授权**：仅允许在现有 `t16a-backup-restore` MySQL 容器与同一 `t16a-backup-restore_mysql-data` 卷内**新建**库 `siyige_test` 与账户 `siyige_test_app`，用于让旧镜像满足 `APP_ENV=test` 的库名/账户强约束。
- 除该库与账户外未新建任何库、账户或卷；未改动 `siyige_t16a_source` / `siyige_t16a_restore` 的数据；未迁移生产、未触碰任何既有 `siyige-*` 项目；未改后端代码或契约。
- 旧镜像：`siyige-t16-rollback:4c5ffbb` = `sha256:dddbed06c647360f950943c8dc4237702dd48a69b5b0ab877d300f608e90be35`，label `org.opencontainers.image.revision=4c5ffbbb8bfb797243b6bb84d7fa28312e78e917`；`Entrypoint=["/usr/local/bin/server"]`，`USER 10001:10001`。
- **不视为「目标环境部署旧版已批准」**：本节只验证本地回退可行性，不含 Nginx/TLS/目标网络，也不代表生产回退流程已验收。

### 准备步骤

1. 启动原恢复容器 `t16a-backup-restore-mysql-1`（`2026-11-20T15:55:32Z`，6 秒后 `healthy`），确认其挂载卷仍是 `t16a-backup-restore_mysql-data -> /var/lib/mysql`。
2. 用容器内 root 创建**仅两项**对象：`CREATE DATABASE siyige_test`（`utf8mb4` / `utf8mb4_0900_ai_ci`）与 `CREATE USER 'siyige_test_app'@'%'`，只授予 `SELECT, INSERT, UPDATE, DELETE ON siyige_test.*`。`mysql.user` 复核显示原两账户未变。
3. 从 A2 的有效产物恢复测试数据（`2026-11-20T15:57:09Z`）：先核 `sha256`，再用独立的 age 私钥解密、传给容器内 `mysql` 导入 `siyige_test`。
   - `expected_sha256 = actual_sha256 = b9eb24eac99d5fff9ea89d0bd70b2faedd6909d7a64894cd35971ea00a1d8214`，`sha_match=True`；密文 3952 字节。
   - 解密后 332 行、`CREATE TABLE` 9 处、R1/R2/P1 标记各 1 处、备份后标记 P2 **0** 处。
   - 导入后 `siyige_test`：9 张表，`users=1`、`debates=1`、`debate_turns=3`、`messages=6`、`topics=5`、`goose MAX(version_id)=13`，用户为 `t16a_a2_owner`、口令哈希前缀 `$argon2id$v=19$m=194`。
   - 明文 SQL 在导入后立即删除（宿主机与容器内临时文件均已清除）。

### 启动方式（同卷、零宿主端口）

旧镜像与恢复 MySQL 共用网络命名空间，因此容器内 `127.0.0.1:3306` 直连该 MySQL、`127.0.0.1:8080` 为本应用，**不发布任何宿主端口**：

```bash
docker run -d --name t16a-rollback-app --network container:t16a-backup-restore-mysql-1 \
  -e APP_ENV=test -e HTTP_ADDR=127.0.0.1:8080 -e DB_ADDR=127.0.0.1:3306 \
  -e DB_NAME=siyige_test -e DB_USER=siyige_test_app \
  -e DB_PASSWORD_FILE=/run/secrets/app_password \
  -e AUTH_ORIGIN=http://127.0.0.1:8080 -e AI_MODE=disabled \
  -v "<工作区外私有目录>/secrets/test/app.txt:/run/secrets/app_password:ro" \
  siyige-t16-rollback:4c5ffbb
```

- 结果：一次启动成功，`Status=running`、`Restarts=0`、`PortBindings={}`、`docker port` 为空。
- 日志：`HTTP 服务启动`（`address=127.0.0.1:8080`，`2026-11-20T15:57:50Z`），另有已知的 `sonic/ast ... fallback to encoding/json` 警告（Go 1.27 高于该库支持范围，属既有告警，不影响本次验证）。

### 启动、健康、登录与历史读回（`2026-11-20T15:58:15Z`）

| 检查 | 结果 |
| --- | --- |
| `GET /api/health/live` | 200 `{"status":"alive"}` |
| `GET /api/health/ready` | 200 `{"status":"ready"}`（说明旧镜像确实连上了该 MySQL 卷） |
| `POST /api/v1/auth/login` | 200，用户 `t16a_a2_owner`，返回 Session Cookie 与 43 字符 `csrf_token` |
| `GET /api/v1/auth/me` | 200，同一用户 |
| `GET /api/v1/debates?page=1&page_size=10` | 200，`total=1`，`completed_rounds=3`、`mode=standard_4`、`ai_style=logical`、`status=active` |
| `GET /api/v1/debates/1` | 200，`turns=3`、`messages=6`，含 R1（`10:04:23.154741Z`）、R2（`10:04:28.346237Z`）、P1（`10:13:04.981353Z`）；`summary_status.status=not_started` |
| `GET /api/v1/debates/999` | 404 受控错误（越权/不存在不泄露） |
| `POST /api/v1/auth/login` 缺 `Origin` | 403 `csrf_failed`（来源校验在旧版同样生效） |
| `POST /api/v1/debates/1/turns` 缺 `X-CSRF-Token` | 403 `csrf_failed`（CSRF 校验在旧版同样生效） |

结论：旧镜像在「Schema 由当前版本迁移、数据由当前版本写入」的同一卷上**能正常启动并完整读回本人历史**，认证与 CSRF 防护未因版本回退失效。

### 同卷未替换的证明

| 证据 | 实测值 |
| --- | --- |
| MySQL 容器 ID（应用启动前后一致） | `e9d1c17d8e96f23a3c623721051731f87e8cd80c965c86dd9096c82613ff5fc1`，`StartedAt=2026-11-20T15:55:32.532188693Z` 在应用运行后**未变**（未重启） |
| 卷名与创建时间 | `t16a-backup-restore_mysql-data`，`CreatedAt=2026-11-20T10:13:39Z`，label `com.docker.compose.project=t16a-backup-restore` —— 与 A2 记录完全一致，未新建、未替换 |
| 应用容器网络模式 | `container:e9d1c17d8e96…`（即上述 MySQL 容器），`PortBindings={}` |
| 卷内两库并存 | 同一卷同时含 `siyige_t16a_restore`（9 表）与 `siyige_test`（9 表） |
| 原恢复库未被替换 | `siyige_t16a_restore` 仍为 users 1 / debates 1 / debate_turns 3 / messages 6 / auth_sessions 3；其消息行指纹 `MD5(GROUP_CONCAT(...))` = `5d67ab24df4b508efcd4c2c76e66852a`，与 `siyige_test` 中 id≤6 的种子消息指纹**完全相同** |

关于「原恢复库未变」的严格口径：本次未在回退步骤前先取一次指纹再比对，因此该判断依据是**行数与 A2 记录一致 + 消息内容指纹与备份种子一致**，不是前后哈希对；证据强度足以排除「应用改写了恢复库」，但不是逐字节不变性证明。

写路径归属核对（`2026-11-20T15:59:03Z`）：用正确 Cookie + CSRF 向 `POST /api/v1/debates/1/turns` 提交第 4 轮（`client_request_id=e43e9603-…`），因 `AI_MODE=disabled` 返回 503 `model_unavailable`（预期）。该失败请求**仍被记入会话状态**：`siyige_test` 出现第 4 条 turn 与第 7 条 message（用户消息落库、生成失败），而 `siyige_t16a_restore` 中该标记命中 **0** 次、`debate_turns` 仍为 3。说明旧镜像确实写的是 `siyige_test`，未写原恢复库。此写入仅发生在授权的 `siyige_test` 内，未越界。

### 本次回退验证的限制

- 仅验证「应用镜像回退 + 同卷 + 历史可读」，**未验证** Nginx/边缘代理、TLS、跨主机切换、告警与回退通知、回退窗口内的写入丢失边界。
- 旧镜像在本轮只以 `APP_ENV=test` 启动；`deploy`/`demo` 固定库名与 HTTPS Origin 等生产配置路径未验证。
- 回退镜像与当前 HEAD 的 Schema 已由当前版本迁移创建，属「新 Schema + 旧应用」方向；反向（旧 Schema + 新应用）未验证。
- 生成能力为 `AI_MODE=disabled`，只验证读路径与写请求的失败处理，未验证回退后模型调用链。

## B2 线：健康探针的故障/恢复可观察路径（2026-11-21，本机合成验证）

本节记录 B 线在本机隔离范围内**实际执行**的验证。范围被明确收窄为：**探针是否能把故障态与恢复态判成两个稳定、可区分的信号**。所有判定证据来自本地回环合成 HTTP 服务与合成归档文件；**未启动 Compose 项目、未停止任何真实服务、未发送任何通知**。

### 范围与授权边界

- 代码基线：`codex/t16-operations` HEAD `09da1660`，工作区开始时干净（`git status` 无改动）。
- 只改 `deploy/ops/health/` 与本文；未改 T07/ECS/业务配置，未引第三方依赖，未在仓库外新增交付文件。
- 未触碰任何既有容器/卷/网络；未创建常驻（非 `--rm`）的 `t16b-local-*` 容器、卷或网络；未访问公网、未注册真实收件人、未调用真实模型。
- **Docker 实际使用情况（如实登记）**：本轮只用 `docker run --rm --network none` 做过几次**一次性只读能力探测**——确认 `mysql:8.4.13`、`golang:1.27.1` 镜像自带 `openssl`（用于判断能否做证书实验）、确认 `nginx:1.30.6-alpine-slim` 不带 `openssl`、以及用 `alpine:3.24.4` 验证私有目录挂载后的文件可见性。这些容器全部随 `--rm` 立即销毁，`docker ps -a` / `docker volume ls` / `docker network ls` 中 `t16b` 相关条目为 **0**；未构建镜像、未启动任何 Compose 项目、未停止或修改任何既有容器。既有 `siyige-*` 容器数量在操作前后均为 **22**，未变化。
- CLI 冒烟驱动脚本位于 `%TEMP%\t16b-cli-smoke\smoke.py`（CLI 冒烟驱动脚本，不属于仓库交付物）；临时目录 `C:\Users\ZhuanZ1\.codex\t16b-local-run`（仅两个占位的假证书文本文件，用于挂载路径探测）已按绝对路径校验后删除。

### 发现并修复的缺陷：测试夹具的时间依赖（D9）

B2 开始时按本文原记录执行同一命令复现出 **2 项失败**：

```
python -B -m unittest discover -s deploy/ops/health -p 'test_*.py'
Ran 27 tests ... FAILED (failures=2)
```

| 编号 | 位置 | 缺陷 | 修复 |
| --- | --- | --- | --- |
| D9 | `test_health_probe.py` `BackupCheckTest` | 夹具把 `now` 固定为 `2026-11-20T09:30Z`，但新建归档文件的 mtime 是执行时墙钟。执行时间一旦晚于该固定值，`judge_backup` 判「最后成功时间晚于当前时间，数据可疑」，`test_fresh_archive_by_mtime_passes` 与 `test_archive_and_status_file_names_are_not_echoed` 失败 | `setUp` 用 `os.utime` 把夹具 mtime 固定到 `now` 之前 1 小时，并断言夹具 mtime 早于 `now`；涉及 mtime 的两个测试同样显式设定 |

**基线复现方式（可重放）**：用 `git show HEAD:...` 把未改动的 `health_probe.py` 与 `test_health_probe.py` 导出到临时目录后在原地执行，得到同样的 `Ran 27 tests ... FAILED (failures=2)`。因此这两项失败**不是本步引入**，而是 B1 交付时未被发现的既有缺陷；其根源是夹具依赖真实墙钟而断言用了固定时间。

整合复验时，CLI 成功用例仍用实际时间比较固定 mtime，超过 24 小时就会失败。已让该用例使用与归档夹具相同的固定时钟。连接恢复用例在同一套接字上先绑定端口，再开始监听，避免释放端口后重新绑定的竞争。

### 环境与执行结果

| 项 | 值 |
| --- | --- |
| Python | 3.14.3（主机 `python`） |
| 依赖 | 仅标准库；未安装任何第三方包 |
| Docker | 仅 `--rm --network none` 一次性能力探测，已全部销毁；**未用于本次故障/恢复验证** |
| 单测命令 | `python -B -m unittest discover -s deploy/ops/health -p 'test_*.py'` |
| 单测结果 | `Ran 32 tests in 6.1s` → **OK**，退出码 0（27 项 B1 + 5 项 B2 新增） |

> 故障/恢复的判定证据全部来自 Python 进程内的回环 HTTP 服务与临时文件，不来自任何容器。Docker 只用于确认「本机有哪些镜像可用」这类环境事实，不参与结论。

新增的 5 项故障/恢复测试：

| 测试 | 故障态断言 | 恢复态断言 |
| --- | --- | --- |
| `test_app_down_then_recovery_is_observable` | live 返回代理 HTML 502 → 判失败，detail 含「应用或代理不可用」 | 同一路径改回 JSON 200 → 通过 |
| `test_db_down_then_recovery_is_observable` | ready 503 `not_ready` → 默认预期判失败；`--expect-ready not_ready` 合同核对通过 | 改回 200 `ready` → 通过 |
| `test_transport_down_then_recovery_is_observable` | 预留端口未监听 → 连接失败/超时，且不抛栈 | 同端口起服务 → 通过 |
| `test_failed_backup_then_recovery_is_observable` | 状态文件 `ok=false` → 「上次备份未成功」 | 状态文件翻转 `ok=true` → 通过 |
| `test_stale_archive_then_recovery_is_observable` | 归档 mtime 50 小时前 + 上限 24 小时 → 「备份过期」 | 新归档落盘（1 小时前）→ 通过 |

### 真实 CLI 冒烟（子进程调用工具本体，服务端为回环合成 HTTP 服务）

用 `%TEMP%\t16b-cli-smoke\smoke.py` 驱动 `python -B deploy/ops/health/health_probe.py`；共 **10 项检查全部符合预期**，脚本输出 `RESULT OK`（下表把「备份恢复」与「备份缺失」并入第 9 行，故为 9 类场景）。

| # | 场景 | 断言要点 | 退出码 | 结果 |
| --- | --- | --- | --- | --- |
| 1 | 正常 | live/ready 均 200 JSON，`summary ok` | 0 | PASS |
| 2 | 应用不可用 | live/ready 均为代理 HTML 502，detail 含「应用或代理不可用」 | 1 | PASS |
| 3 | 数据库不可用 | live 200 alive、ready 503 not_ready，判 ready 失败 | 1 | PASS |
| 4 | 数据库不可用（合同核对） | `--expect-ready not_ready` 正向核对 503 合同 | 0 | PASS |
| 5 | 恢复 | live/ready 恢复 200，`summary ok` | 0 | PASS |
| 6 | HTML 200 伪装 | live 返回 `text/html` 200，detail 含「HTML」并判失败 | 1 | PASS |
| 7 | 备份失败 | 状态文件 `ok=false`，detail 含「未成功」 | 1 | PASS |
| 8 | 备份过期 | 最后成功 473.37 小时前 > 24 小时上限，detail 含「备份过期」 | 1 | PASS |
| 9 | 备份恢复 + 缺失 | 状态翻转后通过；缺失归档判「备份文件不存在」 | 0 / 1 | PASS |

（表中 detail 为去转义后的中文；实测控制台按 GBK 代码页显示为乱码，这是 Windows 控制台渲染问题，不是工具输出编码问题——工具自身把 `stdout` 重配为 UTF-8，单测与重定向读取均正常。）

### 本轮明确不能验证的项目（目标环境必须补）

| 项 | 状态 | 原因 |
| --- | --- | --- |
| 故障告警与恢复通知投递 | **未实现、不能验证** | 仓库内没有告警发送器：`deploy/ops/health/`、`deploy/` 检索不到 SMTP/webhook/通知代码。`backend/internal/ai/trial_ledger.go` 的 `alert io.Writer` 只把试运行账本事件写到日志/终端，是**费用试运行**的账本通道，不是运维监控投递。工具只输出退出码与 JSON 结论，需外部采集器决定是否告警。**未伪造任何投递或到达记录。** |
| 正式证书续期与 TLS 握手 | **未实现、不能验证** | 工具无 `--cacert`、无证书链/SAN/到期检查、无 `tls` 子命令；仓库无 ACME 客户端或续期钩子，`deploy/README.md` 第 4 节的钩子流程（原子替换 + `nginx -t` + `nginx -s reload`）未实现为脚本。**即使后续用自签测试 CA 做回环握手，也只能算探针能力验证，不能称正式续期通过。** 本轮未做任何 TLS 握手。 |
| 真实容器/Compose 故障注入 | **未执行** | 未启动 Compose 项目、未停止任何真实服务。A2 只做过服务停止级验证，非引擎/节点故障；Nginx 透传、超时、`internal` 网络行为均未验。 |
| 应用镜像回退 | **本地同卷已由 A2-R 覆盖；目标环境未验证** | 本工具没有回退路径；回退镜像标识、卷保持、Schema 兼容硬停止属另一工作线。 |
| 目标环境监控接线与值班到达 | **未执行** | 无 ECS、无现场收件人、无真实域名与证书。 |

### 变更范围与残留

- 修改文件：`deploy/ops/health/test_health_probe.py`（D9 修复与 5 项新测试）、`deploy/ops/health/README.md`、本文件。
- `deploy/ops/health/health_probe.py` **未改动**（曾为 B2 起草 `--cacert`/`tls` 扩展，按「不扩展新环境探查」的收束要求已用 `git checkout` 还原至 HEAD，未留下半成品）。
- 未新增任何常驻 Docker 容器、卷、网络或镜像（`--rm` 能力探测容器已全部销毁）；无固定端口监听；临时冒烟脚本在系统临时目录，不在仓库。

### 结论

在本机隔离范围内，健康探针已能把「应用不可用、数据库不可用、备份失败、备份过期」与对应的「恢复」判成稳定且可区分的信号，并发现并修正了一处会使测试随执行日期翻转的夹具缺陷（D9）。**该结果不支持「T16 的告警、证书续期、现场故障响应已验收」**：告警发送器与续期钩子在仓库中不存在，真实容器故障注入与目标环境证据仍需在获得相应环境与授权后补做。

## 清理结果（本轮授权范围内，2026-11-20T16:03Z）

清理前逐一核对**精确名称与归属**，只在归属匹配时删除；`eino-qdrant` 与全部既有 `siyige-*` 资源均未触碰。

| 对象 | 处理 | 归属依据 |
| --- | --- | --- |
| 容器 `t16a-backup-restore-mysql-1` | 已删除 | label `com.docker.compose.project=t16a-backup-restore` |
| 容器 `t16a-backup-source-mysql-1` | 已删除 | label `com.docker.compose.project=t16a-backup-source` |
| 容器 `t16a-a2-probe-source` / `t16a-a2-probe-restore` | 已删除 | 本轮用私有探针镜像创建，无 compose 标签 |
| 容器 `t16a-rollback-app` | 已删除 | 本轮为旧镜像验证创建 |
| 卷 `t16a-backup-restore_mysql-data` | 已删除 | label project=`t16a-backup-restore`，`CreatedAt=10:13:39Z` |
| 卷 `t16a-backup-source_mysql-data` | 已删除 | label project=`t16a-backup-source`，`CreatedAt=09:33:02Z` |
| 网络 `t16a-backup-restore_db` / `t16a-backup-source_db` | 已删除 | label project 匹配，均为 `internal: true` |
| 镜像 `t16a-a2-probe:v1.2.1` | 已删除 | 本轮构建，`242d334446c9` |
| 镜像 `t16a-goose-cli:v1.2.1` | 已删除 | 本轮构建，`226253ccaaba` |
| 镜像 `t16a-backup-tool:v1.2.1` | 已删除 | 本轮构建，`3c98d8f0dd03` |
| 镜像 `busybox:latest`、`curlimages/curl:8.10.1` | 已删除 | 本轮为网络/端口探测拉取，删除前确认无容器引用 |
| 私有目录 `C:\Users\ZhuanZ1\.codex\t16a-run`（密钥、口令、产物、日志、探针源码与二进制） | 已删除 | 工作区外、仅本轮使用；删除前校验绝对路径精确匹配且非符号链接/重解析点；60 个文件、约 106.8 MiB |

清理后复核：`t16a*` 容器 0、卷 0、网络 0；本轮构建与拉取的镜像 0 条；`eino-qdrant` 仍 `Up`；`siyige-*` 容器数清理前后同为 22，未变化。

**证据可复核性的代价（如实登记）**：按清理授权删除私有目录与两个测试卷后，A2 / A2-R 的原始证据文件（运行日志、`manifest`、`.sha256`、解密核验文本、只读 SQL 输出等）与承载两库的卷都不再存在。本文记录的是清理前实测到的**数值与结论**，无法再在本机重放或二次抽查；若需复核，只能按本文记录的过程重新执行一次演练。删除是在「原始证据已转录进本文」之后进行的，故本文不含未实测内容，但**本文本身也不能替代可重放的环境**。

**关于「保留两卷与 `siyige_test` 库」**：代码复核阶段曾要求保留 `t16a-backup-restore_mysql-data` / `t16a-backup-source_mysql-data` 两卷及其中的 `siyige_test` 库作为验收证据。但这两个卷在此前的清理授权（"删除仅由本轮创建的容器/网络/测试数据卷"）下**已连同容器一起删除**，复核请求到达时 `docker volume ls` 中已无 `t16a-*` 卷（已核验：`t16a` 卷 0、容器 0）。**该保留要求已无法满足，属不可回退的既成事实**；如需该证据，只能按本文步骤重建环境后重跑。此项已如实登记，未以任何方式伪造保留状态。

**保留（均非本轮创建，故不在清理授权范围）**：`t16a-app:base`（`a6965c46e478`，`Created=2026-11-20T01:23:21Z`）、`t16a-migrate:base`（`c1661f34ece3`，`Created=2026-11-14T17:19:00Z`）、`siyige-t16-rollback:4c5ffbb`（`dddbed06c647`）、`mysql:8.4.13`、`golang:1.27.1`。其中两个 `t16a-*` 基础镜像在任务开始时即已在本地（A1 演练说明中记录为预置镜像，来源已按其 SHA 校验），本轮只引用未构建；本次仅删除本轮亲手构建/拉取的三个工具镜像、私有目录与两个演练卷。保留旧回退镜像便于后续目标环境验收复用。

### 关于仓库根 `AGENTS.md` 的提交命名规则

该规则**已存在**，无需新增：`AGENTS.md` 第 53–54 行已写明「提交标题统一使用 `feat/docs/chore` 等类型前缀，格式为"类型：中文描述"」「所有新提交的标题统一使用 `类型：中文描述`（全角冒号，不加空格）……例如 `docs：补充备份恢复说明`」。

因此**本轮最终未修改 `AGENTS.md`**（`git status` 中不含该文件）。此前曾对第 53 行做过一次「提交标题」→「所有 commit 标题」的措辞收紧，经复核确认属重复表述，已还原至 HEAD，避免同一规则出现两份说法。
