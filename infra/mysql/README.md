# T02 本地隔离 MySQL 环境

仅用于 Windows、PowerShell **7.2+**（`pwsh`）与 Docker Desktop Linux Engine 下的开发和集成测试。T01 已结项，2026 年 11 月 7 日用户只授权 T02 的本环境子任务；不包含生产部署、业务建表/种子、GORM、后端连接或 `/health/ready`。完整 T02 保持进行中，本子任务待人工验收。

## 固定资源与权限

Compose 项目固定为 `siyige-t02`。两个实例使用官方 `mysql:8.4.13`，InnoDB、`utf8mb4` / `utf8mb4_0900_ai_ci`、UTC `+00:00`。不发布 MySQL X Protocol，不配置自动重启。

| 环境 | 服务 / 容器 | 主机地址 | 数据库 | 独立卷 / 网络 |
| --- | --- | --- | --- | --- |
| 开发 | `mysql-dev` / `siyige-t02-mysql-dev-1` | `127.0.0.1:13306` | `siyige_dev` | `siyige-t02_mysql-dev-data` / `siyige-t02_dev-db` |
| 测试 | `mysql-test` / `siyige-t02-mysql-test-1` | `127.0.0.1:13307` | `siyige_test` | `siyige-t02_mysql-test-data` / `siyige-t02_test-db` |

每个环境独立持有以下账户和随机口令：

- `siyige_dev_app` / `siyige_test_app`：仅各自数据库的 `SELECT, INSERT, UPDATE, DELETE`，不允许 DDL。
- `siyige_dev_migrator` / `siyige_test_migrator`：仅各自数据库的 `ALL PRIVILEGES`，无全局权限和 `GRANT OPTION`。库级 GRANT 显式转义下划线以精确匹配库名。后续版本化迁移使用此账户。
- `root@localhost`：仅容器本地管理，不供应用使用，不允许远程 root。

Docker 网络互不共享，未接入其他项目网络。宿主机通过两个回环端口访问；同机具有 Docker 管理权限的用户仍能管理容器，这不是对 Docker 管理员的隔离。

## 初始化与启动

从仓库根目录启动 PowerShell 7.2+，核对版本，再执行下方命令。Windows 自带的 `powershell.exe` 5.1 不受支持；两份操作脚本使用 `#requires -Version 7.2` 在执行正文前拒绝旧版本。PowerShell 文件保留 UTF-8 BOM，使旧引擎也能正确读取版本要求及中文；Bash 文件由根 `.gitattributes` 固定为 LF，避免 `core.autocrlf=true` 将 Docker 挂载脚本检出为 CRLF。

```powershell
pwsh -NoProfile
$PSVersionTable.PSVersion  # 必须为 7.2 或更高
```

在该 `pwsh` 终端中执行：

```powershell
docker --context desktop-linux info --format '{{.OSType}}'
docker --context desktop-linux ps -a
Get-NetTCPConnection -State Listen | Where-Object { $_.LocalPort -in 13306,13307 }
& ./infra/mysql/init-secrets.ps1
docker --context desktop-linux compose --project-name siyige-t02 -f infra/mysql/compose.yaml config --quiet
docker --context desktop-linux compose --project-name siyige-t02 -f infra/mysql/compose.yaml pull
docker --context desktop-linux compose --project-name siyige-t02 -f infra/mysql/compose.yaml up --detach --wait --wait-timeout 180 mysql-dev mysql-test
```

初始化前确认项目名、卷名、端口未由其他工作占用。若端口被占用，停止并检查来源，不停止其他项目。首次启动在空卷中创建两个库和各自账户，不创建业务表。后续启动保留已有卷，镜像只在空数据目录执行初始化脚本。

`init-secrets.ps1` 用系统加密随机数生成器产生六份不同的 32 字节十六进制口令，存于 `.cache/t02-mysql/secrets/{dev,test}-{root,app,migrator}.txt`。目录已由现有 `.gitignore` 的 `.cache/` 规则忽略，脚本限制 NTFS 目录权限为当前 Windows 用户；既有完整文件不覆盖，不完整集合直接报错。秘密文件不能提交，也不要把内容贴到日志或答辩材料。清理普通构建缓存时保留此目录，避免遗失已有账户口令。

Compose 仅挂载各自三个 secret，使用 `MYSQL_ROOT_PASSWORD_FILE`，不在 Compose 环境变量里传入口令。账户初始化限制口令和标识格式，避免任意文本进入 SQL。更改本地文件不会修改已有数据库账户，重新生成口令不能代替受控改密；初始化失败也不得用删除卷掩盖问题。

## 连接与验证

不需要在 Windows 安装 MySQL CLI。以下命令通过容器内客户端读取本环境 secret，以临时 option file 连接；口令不会出现在命令行参数或输出中，退出后清理临时文件。

```powershell
'SELECT DATABASE(), CURRENT_USER(), VERSION();' | docker --context desktop-linux compose --project-name siyige-t02 -f infra/mysql/compose.yaml exec -T mysql-dev bash /opt/siyige/mysql-client.sh app
'SELECT DATABASE(), CURRENT_USER(), VERSION();' | docker --context desktop-linux compose --project-name siyige-t02 -f infra/mysql/compose.yaml exec -T mysql-test bash /opt/siyige/mysql-client.sh app
& ./infra/mysql/verify-isolation.ps1 -Restart
```

验证脚本先校验实际容器的项目标签、服务名、固定镜像、独立卷、独立网络及唯一回环端口，再执行 SQL。它会检查 Windows 端口上的 MySQL greeting、库名、运行参数、不同实例 UUID、六个口令不同、root Host 范围、应用与迁移账户身份、双向交叉凭据拒绝、跨库与系统表访问拒绝、应用 CRUD 成功和 DDL 拒绝。为验证实际持久化，两库使用同名随机探针表写入不同标记，`-Restart` 只重启本项目两个服务并读取标记；最后只清理本次创建的探针，不删除业务数据或卷。不传 `-Restart` 可跳过重启。

后续后端集成测试还必须在连接入口校验测试目标（固定端口、库名与测试账户），拒绝开发库及非白名单目标。本次脚本的防误连检查不能替代后端测试入口的保护。

### P1 可移植性回归

从仓库根目录运行，不需要启动或操作 Docker：

```powershell
pwsh -NoProfile -File ./infra/mysql/test-portability.ps1
```

测试复用本机 Git for Windows 自带的 Bash 与 Windows PowerShell 5.1，不安装额外依赖。它在 `.cache/t02-mysql/p1-portability/<随机目录>/` 新建临时 Git 仓库，以 `core.autocrlf=true` 建立独立索引并实际执行 `checkout-index`，检查检出脚本的 CR/LF 字节和 Git Bash 语法；临时样本保留供检查，不创建提交，不修改项目索引。

原生命令测试仅载入实际 `verify-isolation.ps1` 的错误处理设置与 SQL 包装函数，用本地 `pwsh` 子进程替代 Docker，验证调用方偏好为 false/true 时的成功、预期失败、错误码不符及意外成功。操作脚本在自身作用域设置 `$PSNativeCommandUseErrorActionPreference=$false`，继续按 `$LASTEXITCODE` 和错误码判断结果，不依赖调用方配置。测试还确认 5.1 在脚本正文执行前明确拒绝版本；不读取秘密、不写数据库、不重启服务。

停止时只操作本项目，保留卷：

```powershell
docker --context desktop-linux compose --project-name siyige-t02 -f infra/mysql/compose.yaml stop mysql-dev mysql-test
```

没有执行 `down --volumes`、`volume prune`、`system prune`、全局停止或重置。删除卷、改密、恢复数据须按相应授权另行处理。

## 依赖与依据

MySQL Community Server 8.4 是冻结的成熟 LTS 方案，服务端采用 GPLv2；镜像内各组件保留各自许可证。替代方案是手动安装两个 MySQL 实例，但增加 Windows 服务、数据目录和端口管理成本，本次不采用。不新增 Go/npm 依赖，复用本机 Docker/Compose 和 PowerShell。

- [MySQL 8.4.13 发布说明](https://dev.mysql.com/doc/relnotes/mysql/8.4/en/news-8-4-13.html)
- [Docker 官方镜像精确标签](https://github.com/docker-library/official-images/blob/master/library/mysql)
- [MySQL 官方镜像初始化与环境变量](https://github.com/docker-library/docs/blob/master/mysql/README.md)
- [Compose secrets 与 MySQL `_FILE` 示例](https://docs.docker.com/compose/how-tos/use-secrets/)
- [Git 换行属性](https://git-scm.com/docs/gitattributes)、[PowerShell 版本门槛](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_requires)、[原生命令错误策略](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.core/about/about_preference_variables#psnativecommanduseerroractionpreference)
- [MySQL GRANT](https://dev.mysql.com/doc/refman/8.4/en/grant.html)、[客户端 option file](https://dev.mysql.com/doc/refman/8.4/en/option-files.html)、[MySQL 许可证](https://www.mysql.com/about/legal/licensing/opensource-license.html)

实际执行记录、首次失败及恢复、未执行项见 [T02 环境验收记录](../../docs/acceptance/T02_MYSQL_ENV_ACCEPTANCE.md)。本子任务完成技术验证后停止，等待人工验收，不自动进入剩余 T02 或 T03–T06。

## 迁移种子的重启持久化核对

2026 年 11 月 8 日另获授权，00007–00010 及五条种子已技术验证，见 [当前记录](../../docs/acceptance/T02_SEEDS_PERSISTENCE_ACCEPTANCE.md)。仅测试库已迁移，服务启动不自动建表或播种。先按 [迁移说明](../../backend/migrations/README.md) 显式 up，再停止并发写入，从根目录执行：

```powershell
pwsh -NoProfile -File infra/mysql/verify-t02-persistence.ps1
```

该脚本会校验固定 Docker context/project/service、镜像/回环端口/数据卷和实际迁移账户，只 restart mysql-test，保留容器/卷，开发容器不能变化。比较重启前后 0–10 正式历史、六表结构/行数及五条种子字节内容/时间；证据仅存 Git 忽略的 `.cache/t02-mysql/persistence-*.json`。不迁移、播种、清理数据或重建容器。当前严格要求测试库 topics 仅有 ID 1–5 五条启用种子，后续出现其他辩题时须重新审阅验收条件。技术通过不代表独立审查或完整 T02 验收通过。
