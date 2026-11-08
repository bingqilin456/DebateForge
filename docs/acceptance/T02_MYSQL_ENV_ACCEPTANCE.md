# T02 首个子任务：隔离 MySQL 环境验收记录

日期：2026 年 11 月 7 日（Asia/Shanghai）。**技术验证通过，本子任务待项目负责人手工验收；完整 T02 保持进行中。** 本记录不代表 Phase 1 通过，不授权继续其他任务、提交 Git 或生产部署。

## 授权与范围

用户确认 Docker Linux Engine 已恢复，在 T01 正式结项后明确授权：使用冻结的 MySQL 8.4 系列，建立独立开发与集成测试库，分离容器、卷、账户与回环端口，完成 Compose、启动及隔离验证，不影响现有 `eino-qdrant`。按 `AGENTS.md`、`ARCHITECTURE.md`、`API.md`、`TASKS.md` 及 PRD/DECISIONS 核对范围。

交付仅为本地数据库环境。输入是固定 Compose 与本地随机秘密文件；输出是两个健康、可认证且相互隔离的 MySQL 实例。权限分为应用 DML、本库迁移 DDL、容器本地 root。失败路径覆盖未就绪、交叉凭据、越库、系统表访问及应用 DDL 拒绝；探针只写入本次专用库，结束时只清理本次创建的随机探针表。

## 文件范围

新增：

- `infra/mysql/compose.yaml`：固定镜像、两个服务/卷/网络/回环端口、按环境挂载 secrets、认证 SQL 健康检查及日志轮转。
- `infra/mysql/init-accounts.sh`：空库账户初始化，校验标识与口令格式，精确限定库级授权，区分应用和迁移权限。
- `infra/mysql/mysql-client.sh`：从 secret 构造临时客户端 option file，不通过参数输出口令。
- `infra/mysql/init-secrets.ps1`：六份独立随机口令、NTFS 目录权限、不覆盖既有文件。
- `infra/mysql/verify-isolation.ps1`：实际 Docker 元数据、SQL 权限、交叉凭据、Windows 回环连接和可选重启验证。
- `infra/mysql/README.md`：资源、启动、连接、停止、依赖和失败处置说明。
- 本验收记录。

修改 `AGENTS.md`、根 `README.md`、`docs/architecture/ARCHITECTURE.md`、`docs/decisions/DECISIONS.md`、`docs/planning/PLAN.md`、`docs/planning/TASKS.md`，仅同步本次授权、环境说明与准确进度。`docs/api/API.md`、`.gitignore`、`backend/`、`frontend/` 不变。

本地生成但由既有 `.cache/` 忽略规则排除的文件：`.cache/t02-mysql/secrets/` 下六份秘密文件，`verification.txt`、`fresh-init-verification.txt`、前后容器元数据快照及临时验证脚本。秘密文件未输出、未进入 Git 暂存区或跟踪文件；不要清理秘密目录来重置已初始化数据库。

## 实际运行环境

核验 Docker Server `29.8.2`、Context `desktop-linux`、OSType `linux`、Compose `v5.5.1`。本机未安装 MySQL CLI 不影响验证，使用镜像内客户端。Compose 项目固定为 `siyige-t02`，镜像为官方 `mysql:8.4.13`，数据库查询 `VERSION()` 同为 `8.4.13`。

实际拉取镜像摘要：

```text
mysql@sha256:a81d043930e51745522bd5c430961cf8286f0a23f0a9a2a6e0108f7ea71b0902
```

| 容器（短 ID） | 数据库 / 主机映射 | 数据卷 | 网络 | 状态 |
| --- | --- | --- | --- | --- |
| `siyige-t02-mysql-dev-1`（`8f3cc260605c`） | `siyige_dev` / `127.0.0.1:13306 → 3306/tcp` | `siyige-t02_mysql-dev-data` | `siyige-t02_dev-db` | healthy |
| `siyige-t02-mysql-test-1`（`745065a61e39`） | `siyige_test` / `127.0.0.1:13307 → 3306/tcp` | `siyige-t02_mysql-test-data` | `siyige-t02_test-db` | healthy |

开发账户 `siyige_dev_app`、`siyige_dev_migrator`；测试账户 `siyige_test_app`、`siyige_test_migrator`，另各有仅容器本地的 `root@localhost`。六份口令不同。应用仅有本库 SELECT/INSERT/UPDATE/DELETE，迁移账户有精确本库 ALL PRIVILEGES，无 GRANT OPTION 或全局管理权限。库名中的下划线在 GRANT 中转义，避免扩大到类似库名。

现有 `eino-qdrant`（`020ba1b65b22`）持续运行，原有 `6333–6334` 端口保持原样。实施前后对比 Id、StartedAt、RestartCount、端口绑定和挂载均一致。没有停止、重启或修改该容器，也没有执行全局 Docker 清理。

## 实际命令与结果

以下在仓库根目录的 PowerShell 执行。命令中不含实际口令。

```powershell
git status --short
docker version --format '{{.Server.Version}}'
docker context show
docker info --format '{{.OSType}}'
docker compose version
docker ps -a
Get-NetTCPConnection -State Listen | Where-Object { $_.LocalPort -in 13306,13307 }
& ./infra/mysql/init-secrets.ps1
git check-ignore .cache/t02-mysql/secrets/dev-root.txt .cache/t02-mysql/secrets/dev-app.txt .cache/t02-mysql/secrets/dev-migrator.txt .cache/t02-mysql/secrets/test-root.txt .cache/t02-mysql/secrets/test-app.txt .cache/t02-mysql/secrets/test-migrator.txt
docker --context desktop-linux compose --project-name siyige-t02 -f infra/mysql/compose.yaml config --quiet
docker --context desktop-linux compose --project-name siyige-t02 -f infra/mysql/compose.yaml pull
docker --context desktop-linux compose --project-name siyige-t02 -f infra/mysql/compose.yaml up --detach --wait --wait-timeout 180 mysql-dev mysql-test
& ./infra/mysql/verify-isolation.ps1 -Restart
docker --context desktop-linux compose --project-name siyige-t02 -f infra/mysql/compose.yaml ps -a
docker --context desktop-linux image inspect mysql:8.4.13 --format '{{json .RepoDigests}}'
git diff --check
git diff --exit-code HEAD -- backend frontend docs/api/API.md .gitignore
git diff --cached --quiet
```

首次 `up` 未通过，恢复过程见下节；最终 `up --wait` 成功，两个服务均 healthy。所有 SQL 探针使用容器内真实 MySQL，而非 Mock 或 SQLite。验证脚本实际执行指定服务的 `restart` 和 `up --wait`，未操作其他项目。

| 验证 | 实际结果 |
| --- | --- |
| 两容器、两卷、两网络、项目标签与实例 UUID | 独立；脚本先核对身份再写探针 |
| 回环端口 | Docker 实际绑定均为 `127.0.0.1`，仅发布各自 3306；Windows TCP 连接两端口，读取 greeting 均为 8.4.13 |
| MySQL 运行参数 | 两库均为 `8.4.13 / utf8mb4 / +00:00 / InnoDB` |
| 账户与 root 范围 | 每实例仅有各自两项目账户；root Host 仅 localhost |
| 开发凭据登录测试实例、测试凭据登录开发实例 | 应用及迁移账户双向均拒绝，`ERROR 1045` |
| 访问另一库、将下划线替换为其他字符的类似库名 | 应用及迁移账户双向均拒绝，`ERROR 1044` |
| 读取 `mysql.user` | 应用及迁移账户均拒绝，`ERROR 1142` |
| 本库应用读写 / 迁移建表 | 成功；应用 CREATE/DROP TABLE 均拒绝，`ERROR 1142` |
| 数据隔离与持久化 | 同名随机表分别写 dev/test 标记；两个实例重启后各自标记正确 |
| 探针清理 | 只清理本次随机表，两库最终均为 0 张业务表 |
| 秘密生成器重跑 | 文件 SHA256 在内存中比较一致，未覆盖口令；六个文件均被 Git 忽略 |
| 最终文件检查 | Compose config、两份 Bash 脚本与 PowerShell 语法、61 个本地 Markdown 链接目标、`git diff --check` 通过；变更候选与脱敏证据不含六份实际口令，范围外文件不变，Git 暂存区为空 |

额外使用 `.cache/t02-mysql/verify-fresh-init.ps1` 顺序创建临时 `siyige-t02-init-check`，分别验证 dev/test 的最终账户脚本在全新空数据目录自动初始化。该容器使用 `mysql:8.4.13`、`--network none`、无端口发布、`--tmpfs /var/lib/mysql:rw,size=1g`，只读挂载本次脚本与对应 secrets；应用认证成功，root 仅 localhost。每次停止该次创建的容器 ID，由 `--rm` 自动清理临时容器和 tmpfs，未创建或删除持久化卷；最终仅保留两正式 MySQL 容器及原有 qdrant。

## 失败、修正与 Codex 技术审查

1. 受限工具最初访问 Docker 命名管道返回 permission denied。获准提高工具权限后核验成功，这是工具访问限制，不将其登记为 Docker 引擎仍故障。
2. 首次空卷初始化在创建账户前失败：Windows 挂载脚本以独立进程执行，官方入口函数未取得临时服务器 SOCKET，返回 `ERROR 2002`。改为自有客户端通过 localhost socket 连接，不依赖父进程函数；保留原卷，重启后补执行两个服务的账户初始化，未删除/重置数据。
3. 客户端 option file 显式配置协议时实际返回 `ERROR 2047`。改为 root 使用 localhost（socket）、应用/迁移使用 127.0.0.1（TCP），实际连接和 SQL 验证通过。
4. 权限验证发现镜像默认创建 `root@'%'`。Compose 显式设置 `MYSQL_ROOT_HOST=localhost`，本次两实例移除初始化产生的远程 root，保留 localhost root，按更新配置重建容器并保留原数据卷。最终空库初始化也验证了 root 范围。
5. 只读技术审查发现库名下划线在库级 GRANT 中具有通配语义。按 [MySQL 官方 GRANT 文档](https://dev.mysql.com/doc/refman/8.4/en/grant.html) 转义，收紧已有项目账户授权；增加类似库名拒绝用例，重跑隔离与重启验证全部通过。

Codex 审查覆盖项目资源边界、秘密流向、初始化/非空卷行为、账户权限、回环发布、探针清理及实际失败路径。未发现遗留的本子任务阻断问题。未进行 A/B 互审或正式讲解登记，也未将其补记为完成。

## 未执行与验收门槛

- 未安装 GORM/MySQL Go 驱动/Goose，未改后端或前端代码，未重跑或宣称业务测试通过。
- 未建立业务表、执行版本化迁移或写入 5 条辩题种子；探针不属于业务迁移。
- 未实现后端数据库配置、连接池、测试入口目标保护或 `/health/ready`；本次 Docker 数据库健康不代表后端 ready。
- 未开发 T03/T04/T05/T06，未调用模型、使用生产数据库、创建生产部署配置或对公网发布。
- 未 Git commit、push、合并或暂存文件；本子任务不等于完整 T02 或阶段完成。

请项目负责人验收 Compose 范围、两个容器/端口、账户权限、秘密忽略及运行证据。**人工验收结论：待填写，不预填通过。** 验收并取得后续实施授权后，建议继续 T02 的后端数据库连接与测试目标保护，再实施版本化业务迁移、种子和 readiness。本次完成环境交付后停止。

## 独立审查 P1 修复与针对性复验

2026 年 11 月 7 日（Asia/Shanghai），用户授权仅修复独立审查的 P1-1、P1-2。P2/P3 保留，不在本轮处理；本记录不更新任务状态或代替人工验收。

修改范围：

- 新增根 `.gitattributes`，仅为 `/infra/mysql/*.sh` 设置 `text eol=lf`。两份 Bash 脚本内容不变。
- `infra/mysql/init-secrets.ps1`、`infra/mysql/verify-isolation.ps1` 增加 `#requires -Version 7.2`，保存为 UTF-8 BOM；旧引擎可以正确解析中文并在执行正文前报告版本不符。
- `verify-isolation.ps1` 在脚本作用域设置 `$PSNativeCommandUseErrorActionPreference=$false`，由现有 `$LASTEXITCODE` 和错误码断言处理预期拒绝，不继承调用方的提前抛错行为。不改变数据库、权限或探针逻辑。
- 新增 `infra/mysql/test-portability.ps1`，提供不连接 Docker 的六项回归；根 README 和 MySQL README 明确 `pwsh` 7.2+ 及复验方法。

实际环境：Windows、PowerShell `7.6.9`、Windows PowerShell `5.1.26100.9711`、Git `2.52.0.windows.1`，复用既有 Git for Windows Bash、Docker 和 MySQL 镜像，不新增依赖。

可从仓库根目录复现：

```powershell
pwsh -NoProfile -File ./infra/mysql/test-portability.ps1
git check-attr text eol -- infra/mysql/init-accounts.sh infra/mysql/mysql-client.sh
git diff --check
```

| 针对性检查 | 实际结果 |
| --- | --- |
| 修复前失败观察 | 临时 Git 仓库以 `core.autocrlf=true` 检出两文件，均有 CR=21；调用方原生命令错误策略为 true 时，预期非零退出提前抛错 |
| 修复后实际 Windows 检出 | 在新建的忽略目录临时仓库中，以独立索引和 `checkout-index` 物化文件；两份 Bash 均 CR=0、LF=21，Git Bash `-n` 均退出 0 |
| Linux Bash 兼容 | 将上述实际检出文件的原始字节经 stdin 送入现有开发容器的 `bash -n`；两文件均退出 0，不执行脚本正文、不写数据库 |
| 原生命令错误处理 | 调用方偏好为 false/true 两组均通过；预期拒绝可继续、错误码不符和意外成功均被拒绝、正常输出保持可读，调用方偏好保持原值 |
| 旧版引擎拒绝 | 两份操作脚本在 Windows PowerShell 5.1 中均因要求 7.2 而非零退出，未执行脚本正文 |
| 真实 MySQL 只读复验 | 在调用方偏好为 true 时，仅载入实际错误处理设置与 SQL 包装函数；dev/test 的 app/migrator 共四组 `SELECT 1` 成功，`USE mysql` 和读取 `mysql.user` 的预期 1044/1142 均正常处理 |
| 现有服务保持不变 | MySQL dev/test 与 Qdrant 的 Id、StartedAt、RestartCount、状态、健康状态、端口、网络、挂载的前后快照完全一致；两个 MySQL 均 healthy |
| 秘密与 Git 索引 | 六份秘密文件的 SHA256 和 ACL 前后一致；项目 Git 索引哈希一致，未暂存或提交 |

本地脱敏证据保存在被忽略的 `.cache/t02-mysql/p1-fix-evidence/`：`portability-before.txt`、`portability-after.txt`、`live-readonly.txt`、`services-before.json`、`services-after.json`、`unchanged.txt`。实际检出样本目录由测试的 `CHECKOUT` 行给出，保留供复查；没有清理数据库、卷、秘密或其他既有文件。

限制：未安装或单独实测 PowerShell 7.2 的精确版本；本机成功执行版本为 7.6.9，5.1 的拒绝路径已实测。未运行会写探针表的完整 `verify-isolation.ps1`，未重启服务、执行迁移或新增业务验证。完整权限漂移检测、半初始化恢复及既有秘密 ACL 复核等 P2/P3 不变。

**本轮两项 P1 针对性验证通过，仍等待项目负责人最终人工验收；完整 T02 未完成，不继续后续任务。**
