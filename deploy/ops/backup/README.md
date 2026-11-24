# T16A 备份与恢复工具（切片 A1）

本目录是 T16 备份恢复切片 A1 的最小交付：**从固定隔离源库生成 MySQL 一致性快照 → 用成熟加密工具 age 加密 → 校验后原子发布密文 → 在固定独立空目标库验证并恢复**。全部操作只针对本机专用隔离环境，不连接、不修改任何既有数据库，不做生产恢复，不使用真实数据。

状态：A1 工具与静态/拒绝路径测试已实现；**独立库整库恢复实测与应用历史可读证据未在本轮完成**，见文末「未验证项」。本文不是验收记录。

## 1. 隔离边界与固定资源

| 资源 | 固定值 | 说明 |
| --- | --- | --- |
| 源项目 | `t16a-backup-source` | 只读；快照来源 |
| 恢复目标项目 | `t16a-backup-restore` | 只接受空库恢复 |
| 源数据库 | `siyige_t16a_source` | 合成数据 |
| 恢复目标库 | `siyige_t16a_restore` | 恢复后使用 |
| 密文目录 | 宿主机 `.local/artifacts/`（由 `--artifacts-dir` 指定） | 只存密文、摘要与清单 |
| 私钥位置 | 恢复端操作者自持的目录（`--identity-file`） | 不在仓库、不与密文同目录 |
| MySQL 镜像 | `mysql:8.4.13` | 与 T07 部署一致 |

约束：不发布任何宿主端口；网络为内部网络；不挂载 Docker socket；不使用 `privileged`；两个项目各自独立数据卷、互不共享；不引用 `siyige-t02`、`siyige-t07`、`siyige-p3-demo` 等既有项目。脚本对项目名做白名单断言，参数不可把目标改成其他项目或库。

合成数据需在下一轮由演练脚本写入；本目录**不含**夹具或演练编排脚本，不含任何真实用户、真实辩论正文、真实凭据。

## 2. 密钥与密文分离

- **加密只用公钥**：备份端只持有 age recipient（公钥，`age1...`）。`t16a-backup.sh` 发现 `--recipient-file` 内出现 `AGE-SECRET-KEY-` 即拒绝。
- **私钥只在恢复端**：age 私钥文件由恢复端单独保存，`--identity-file` 传入；`t16a-restore.sh` 要求私钥与密文不在同一目录，并对权限松于 `0600` 的文件给出警告。
- **接收端只保留密文**：备份产物只有 `<label>.sql.gz.age`、`<label>.sha256`、`<label>.manifest.json`。明文 `payload.sql.gz` 只在目标端私有暂存目录短暂存在，导入后删除；备份暂存目录在成功或失败后都会清理。
- 仓库内不保存任何密钥、口令或密文；`.gitignore` 已忽略 `.local/`、`*.age`、`*.sql.gz` 等运行期产物。

## 3. 备份

前置：固定隔离环境已启动（见第 6 节），`docker compose` 可用。

```bash
./bin/t16a-backup.sh \
  --artifacts-dir "$PWD/.local/artifacts" \
  --recipient-file "$HOME/.config/t16a/recipient.pub"
```

流程：

1. 校验项目名、容器标签、无宿主端口，确认源服务在运行。
2. 容器内 `mysqldump --single-transaction --quick --skip-lock-tables`：InnoDB 事务级一致视图，不锁表。
3. gzip 压缩后立即用 age 加密；密文头部必须为 `age-encryption.org/v1`。
4. 计算密文 SHA-256，写脱敏清单（表数、MySQL 版本、UTC 起止时间，不含业务数据）。
5. 先写入 `.staging.*` 临时目录，全部成功后才逐个原子改名发布；任何失败删除已发布的不完全产物。

产出示例（时间与摘要为示意，非本轮证据）：

```json
{"schema":"t16a-backup/v1","artifact":"t16a-20261120T020000Z.sql.gz.age",
 "sha256":"<64 位十六进制>","source_project":"t16a-backup-source",
 "source_db":"siyige_t16a_source","consistency":"mysqldump --single-transaction --quick",
 "encryption":"age v1.2.1 (X25519 recipient)"}
```

## 4. 恢复

```bash
./bin/t16a-restore.sh \
  --artifact "$PWD/.local/artifacts/t16a-20261120T020000Z.sql.gz.age" \
  --identity-file "$HOME/.config/t16a/identity.txt" \
  --confirm-empty-target
```

恢复前依次校验，任一失败即停止且**目标库不写入**：

| 拒绝条件 | 检查时机 |
| --- | --- |
| 目标项目不是 `t16a-backup-restore` | 参数解析后立即 |
| 源目标重合（目标项目/库与清单源相同） | 摘要校验后、任何写入前 |
| 密文缺失、扩展名不对、摘要不匹配 | 解密前 |
| age 解密失败（错误密钥或损坏） | 暂存解密时 |
| 解密结果 gzip 校验失败 | 导入前 |
| 私钥与密文同目录 | 解密前 |
| 目标库非空 | 导入前（读表数） |
| 未显式 `--confirm-empty-target` | 导入前 |

恢复顺序是「先验证完整副本，再导入」：摘要 → 解密到私有暂存 → gzip 完整 → 目标为空 → 导入。导入失败时脚本明确报告目标库可能部分写入，须人工核对，不做自动重试掩盖。

## 5. 一致性、RPO 与 RTO 口径

- **一致性**：快照由 `mysqldump --single-transaction` 在单个事务内取得，InnoDB 表内部一致；MyISAM 表不在本演练范围。
- **RPO**：以恢复后可读到的最新一条预置标记时间与模拟故障时点之差计算，记录 UTC 与时区。基准标记时间由夹具写入、时间戳落在 `manifest.json` 的 `dump_started_at`/`dump_finished_at` 区间内。**本轮未实测 RPO 数值。**
- **RTO**：从宣布开始恢复到表数、关键表行数与标记可读为止的墙钟秒数。**本轮未实测 RTO 数值。**
- 数值只在本机 Docker Desktop 上有效，不代表异机或生产。

## 6. 启动固定隔离环境

```bash
export T16A_SECRETS_DIR="$PWD/.local/secrets"   # 绝对路径，0700
# 每个项目分别启动；库名随项目固定
T16A_DB=siyige_t16a_source T16A_APP_USER=siyige_t16a_source_app \
T16A_MIGRATOR_USER=siyige_t16a_source_migrator \
docker compose -p t16a-backup-source -f compose.yaml up -d --wait
```

恢复目标同理，把 `T16A_DB` 换成 `siyige_t16a_restore`、`-p` 换成 `t16a-backup-restore`。口令文件 `root.txt`、`app.txt`、`migrator.txt` 必须是 64 位小写十六进制，且两项目互不相同。

## 7. 测试

```powershell
pwsh -NoProfile -File ./deploy/ops/backup/tests/run-static.ps1
```

实际执行（`pwsh -NoProfile -File ./deploy/ops/backup/tests/run-static.ps1`，60 项全部通过）：

| 覆盖面 | 具体断言 |
| --- | --- |
| 交付完整性 | 13 个文件存在；每个 `.sh` 无 CRLF、`set -euo pipefail`、shebang、`bash -n` 通过 |
| 帮助入口 | 备份/恢复脚本 `--help` 退出码 0 且含「用法」 |
| 参数拒绝 | 缺参、未知参数退出码 2；相对路径拒绝（命中「绝对路径」） |
| 密钥形态 | 备份端拒绝把私钥当 recipient（命中「含私钥」） |
| 目标防护 | 恢复端拒绝非白名单项目、源项目当目标（命中「项目名被拒绝」） |
| 文件校验 | 拒绝不存在密文、拒绝私钥与密文同目录、拒绝摘要不匹配（命中对应原因，不进入写入） |
| 加密正确性 | 容器内 age v1.2.1 加解密往返摘要一致；截断密文与错误私钥均被拒绝 |

测试用例断言的是**预期拒绝原因文本**，而不是只看退出码；因此参数解析、密钥分离、项目白名单若被改坏会直接失败。

暂不覆盖（需要运行中的隔离库或尚未构建的工具镜像）：完整备份→恢复、gzip 损坏的端到端拒绝、RPO/RTO 实测。

## 8. 未验证项

- 独立库整库恢复实测、恢复后应用历史可读：**未执行**（本轮范围收窄）。
- 备份端完整执行：本机无 `age` 可执行文件，脚本会走「工具镜像 `t16a-backup-tool:v1.2.1`」回退分支，而该镜像本轮未构建，因此**备份全链路未实测**；仅静态与参数/密钥形态路径已测。
- 摘要不匹配、gzip 损坏的端到端拒绝（需真实密文与目标库）：**未执行**，目前只有脚本内判定逻辑，未用真实产物触发。
- `t16a-lib.sh` 中 `assert_no_published_ports` 等运行时断言：**未在运行中的容器上触发**。
- RPO/RTO 实测数值：**未执行**。
- 异机传输：**未执行**；本机同 Docker 环境不能当作真实异机证据。
- 明文全链路不落盘、删除卷与清理策略：未做破坏性验证。
- 应用层（HTTP 接口）历史可读证据：**未执行**，本轮只有 SQL 层证据口径。

## 9. 依据

- age 加密：<https://github.com/FiloSottile/age>（BSD-3-Clause，v1.2.1 固定版本）
- MySQL `mysqldump` 一致性与选项：<https://dev.mysql.com/doc/refman/8.4/en/mysqldump.html>
- 现有隔离 MySQL 口径：[infra/mysql/README.md](../../../infra/mysql/README.md)
- 计划与边界：[deploy/T16_PLAN.md](../../T16_PLAN.md)
