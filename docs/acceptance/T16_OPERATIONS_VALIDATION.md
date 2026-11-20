# T16 运维验证记录

本记录只覆盖部署前准备与隔离演练。T16 的验收还需要目标环境证据和负责人确认。本轮不部署 ECS、不开放公网、不写生产库、不调用产品真实模型。

## 已验证

- B1 健康与备份新鲜度探针：在整合工作区执行 `python -B -m unittest discover -s deploy/ops/health -p 'test_*.py'`，27 项通过。覆盖 live/ready HTTP 状态、JSON 和 status 契约、非回环拒绝、错误/缺失/过期/空归档、有限数值参数及不回显输入内容。CLI 冒烟仅对本地合成回环服务和临时文件执行。
- A1 备份脚本隔离静态/拒绝路径测试：`pwsh -NoProfile -File ./deploy/ops/backup/tests/run-static.ps1`，本轮最终 **76 项通过、0 项失败**（A2 演练开始时基线实测 61 项；演练中为 D1–D7 补断言至 72 项，代码复核发现 D8 后再补 4 项 fail-closed 断言至 76 项）；`tests/age-roundtrip.sh` 的 age 加解密、截断密文拒绝、错误私钥拒绝均通过。仅测试脚本与 age，不是数据库备份/恢复实测。
- 上述证据只证明工具逻辑与静态边界；不证明 T16 目标环境控制通过，也不证明 A08 的 RPO 24 小时/RTO 2 小时已达到。

## 尚未验证

- A1 工具已交付；演练开始时本机没有 age 可执行文件且工具镜像未构建。该状态已改变：本轮已在工作区外私有目录安装 age v1.2.1 并构建工具镜像，随后完成 MySQL 快照、完整加密备份、独立库恢复、合成辩论历史读取与 RPO/RTO 实测（见「A2 线」），并补做旧镜像同卷回退验证（见「A2-R 线」）。**这些仍是本机隔离证据，异机取回、目标环境与生产范围未验证。**
- B2 未执行：无 Docker 故障注入（A2 只做了服务停止级的故障模拟，非引擎/节点故障）、告警投递/恢复通知、正式证书续期与 TLS 握手。应用镜像回退的**本地同卷**部分已由「A2-R 线」覆盖，但目标环境的回退流程未验证。
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
