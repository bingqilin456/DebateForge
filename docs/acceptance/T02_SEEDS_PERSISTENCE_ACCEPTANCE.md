# T02 种子数据与重启持久化技术验证

日期：2026 年 11 月 8 日（Asia/Shanghai）。基线：`feat/t02-database` / `80363ef`，实施前工作区干净。

状态：**本闭环及完整 T02 已通过最终独立技术审查和项目负责人人工验收，T02 标记已完成。** M01/M02 关闭，M03 等已接受风险继续延期；本次授权提交并推送到 origin/feat/t02-database，不创建 PR、不合并、不部署或进入 T03。下文自动化验证属于实施阶段记录，最终独立审查及人工验收见本文末尾。

## 范围与设计

- `backend/migrations/00007–00009`：分别为 debates、debate_turns、messages 增加字节精确的枚举 CHECK。保留原约束和原六份 SQL，不修改列类型、状态集合、关系或业务 API。00007 同时收紧 pro/con。
- `00010_seed_topics.sql`：一个多行 INSERT，固定 ID 1–5，enabled=true，由 Goose 事务共同提交数据和成功记录。沿用已有 up/status、固定目标/实际身份验证与会话锁；服务启动不运行迁移或种子。
- 迁移模块新增枚举异常数据、历史保护和种子测试；保留其他现有测试。
- `infra/mysql/verify-t02-persistence.ps1`：核对专用容器元数据、卷、回环端口、实际库/迁移账户/版本后，仅 restart mysql-test；重启前后逐项比较正式历史、六表 SHOW CREATE/行数、五条种子的完整字节内容/启用标志/微秒时间。不调用 up、不自动修复。
- 同步本说明、迁移 README、项目状态、决策和风险。无新增依赖，无服务/前端/Compose/权限配置修改。

CHECK 使用 `CAST(column AS BINARY) IN (_binary'…')`，按字节比较并保留尾随空格语义。每个版本仅一条 ALTER TABLE，使单表约束增量由 MySQL 原子 DDL 保护；已有不合法行会使 ALTER 失败，既有数据不被 trim/覆盖。三个版本独立登记，失败前已成功版本保留；仍存在 DDL 成功但版本登记未成功的窗口，按原恢复流程停下核对，不自动补记。

依据：[MySQL 8.4 CAST](https://dev.mysql.com/doc/refman/8.4/en/cast-functions.html)、[ALTER TABLE / CHECK](https://dev.mysql.com/doc/refman/8.4/en/alter-table.html)、[Goose 事务注解](https://pressly.github.io/goose/documentation/annotations/)。实际 API 仍使用锁定的 Goose v3.29.0，没有新增调用方式。

## 五条种子及幂等边界

| ID | 标题 | 正方 / 反方 |
| --- | --- | --- |
| 1 | 大学生应该广泛探索不同领域还是尽早专注一个方向 | 广泛探索 / 尽早专注 |
| 2 | 团队决策应该优先追求共识还是效率 | 共识 / 效率 |
| 3 | 学习新知识应该先系统阅读还是先动手实践 | 阅读 / 实践 |
| 4 | 城市公共空间应该优先满足便利性还是保留自然环境 | 便利性 / 自然环境 |
| 5 | 评价一项工作应该更看重过程还是结果 | 过程 / 结果 |

完整 description 在种子 SQL 和独立字面量测试中，均解释正反立场并给出讨论维度。实施时内容待人工审阅；现负责人已对包含这五条辩题的闭环作出最终人工验收。 API/页面未接入。

幂等单位是受保护的迁移命令 `up`：重新创建 provider 再执行两次，均 `applied=0`，原历史及业务行数不变，五条内容再次逐字段匹配。不是可以绕过 Goose 反复手工执行的 SQL。固定 ID 冲突时，不用 IGNORE/REPLACE/UPSERT；整笔事务失败，原数据保留，版本不登记。隔离探针在 id=3 放置不同内容后执行真实种子 SQL，两次均 MySQL 1062；只保留原来一行，无部分种子、无覆盖。若真实库已有辩题占用这些 ID，须停下核对，不能删除、重编号或修改已执行文件来绕过。

## 实际目标与数据库结果

只写入 `127.0.0.1:13307/siyige_test`，实际账户 `siyige_test_migrator@%`，MySQL `8.4.13`。实施前正式历史 0–6、topics 行数 0，业务表无持久业务数据。新增首次执行输出 **applied=4**，最终正式历史 0–10 均 applied（共 11 行）；1–6 源文件未修改。

重启验证前六表行数依次为：users=0、auth_sessions=0、topics=5、debates=0、debate_turns=0、messages=0。事务回滚的约束夹具不留下业务数据，但自增序列可有空洞。未建总结表。

仅重启 `siyige-t02/mysql-test`，未重建容器、未删除卷或清空任何数据；开发容器 ID 和 StartedAt 保持一致，其他项目未操作。重启后的正式版本、六表结构/行数、全部五条种子的 ID/标题/描述/enabled/created_at 与重启前完全一致。证据在 Git 忽略的 `.cache/t02-mysql/persistence-20261108-102330-188.json`，含 Verified=true 和前后快照，不含秘密或完整 Docker inspect。

## 自动化验证与复跑

迁移模块（`backend/tools/migrate/`）：

```powershell
$env:SIYIGE_MIGRATION_TEST = '1'
$env:SIYIGE_MIGRATION_PASSWORD_FILE = (Resolve-Path '../../../.cache/t02-mysql/secrets/test-migrator.txt').Path
go test -race -count=1 -v ./...
go vet ./...
go build -o ../../../.cache/t02-mysql/migrate.exe .
go mod verify
gofmt -l .
Remove-Item Env:SIYIGE_MIGRATION_TEST, Env:SIYIGE_MIGRATION_PASSWORD_FILE
```

全量 race 通过，vet/build/模块校验通过，gofmt 无输出。覆盖：

- 六表结构、主外键、唯一性、中文/emoji、未知 token NULL，以及已有重复执行/部分 DDL 失败/并发锁/解锁错误保护。
- 合法枚举完整集合；active空格、completed空格、generating空格、failed空格、user空格、大小写、前导空格、tab 和空值反例。约束业务夹具整体回滚。
- 三种既有非法枚举：用实际迁移表达式重定向至独立探针表，ALTER 被 3819 拒绝，原非法行保留、无部分约束和成功登记。
- 历史间隙、重复、非前向、缺失版本 0、缺少本地已执行文件，分别在 status/up 下拒绝，探针 DDL 不执行，历史与业务数据不变，锁已释放；合法前缀 status 显示 pending，up 仅执行下一版本。
- 五条完整种子及重复 up；ID 冲突事务回滚和已有数据保护。

默认 `go test -count=1 -v ./...` 也通过，真实 MySQL 用例按预期跳过。重启后另行执行 `go test -race -count=1 -run '^TestMySQLTopicSeeds$' -v .` 通过，完整五题匹配，重复 up 仍为零变更；编译后的 CLI status 显示 00001–00010 全部 applied，原 1–6 成功时间不变。

后端（`backend/`）：

```powershell
$env:SIYIGE_INTEGRATION = '1'
$env:APP_ENV = 'test'
$env:DB_ADDR = '127.0.0.1:13307'
$env:DB_NAME = 'siyige_test'
$env:DB_USER = 'siyige_test_app'
$env:DB_PASSWORD_FILE = (Resolve-Path '../.cache/t02-mysql/secrets/test-app.txt').Path
go test -race -count=1 -v ./...
go vet ./...
go build -o ../.cache/t02-mysql/server.exe ./cmd/server
go mod verify
```

在获准的本地 socket 执行环境中，后端全量 race 测试（含真实测试应用账户的 MySQL 连接、UTC/字符集及真实 HTTP readiness 成功/503/恢复）全部通过；vet/build/模块校验通过。以上环境覆盖仅用于独立终端，完成后关闭终端或清除覆盖值，避免污染后续开发。

从仓库根目录复跑持久化（**会实际重启测试 MySQL 服务**，应停止并发写入）：

```powershell
pwsh -NoProfile -File infra/mysql/verify-t02-persistence.ps1
```

该脚本已实际执行并 PASS，不会在启动服务时自动运行。需先显式执行迁移与种子，再验证；脚本严格要求当前测试库仅有这五条辩题，后续增加真实题目后须另审验收条件。

## 实施阶段失败记录、边界与当时待审事项

1. 修复前先执行反例，枚举写入未报错、种子 id=1 不存在，测试按预期失败，证明覆盖实际缺口。
2. 首轮历史测试的第一个 CREATE TABLE 出现 `invalid connection`（10 秒读超时）；未重复提交该 DDL来隐藏结果、未清理探针。只读诊断未见残留活动锁；后续完整 race 测试该分支及所有用例通过。具体超时根因未证实，不能宣称已修复基础设施问题。
3. 初次后端测试在受限沙箱中因本地 socket 操作失败，`TestRunServesHealthDuringDatabaseOutageAndShutsDown` 未启动、`TestDatabaseSecretsAndLazyConnection` 未得到预期 deadline。随后在获准解除该限制的执行环境复跑，最终结果见上节。不修改这些测试或业务代码来绕过环境限制。
4. MySQL 会先截断超出 VARCHAR 长度的尾随空格：`role VARCHAR(9)` 接受输入 `assistant ` 后存成规范 `assistant`，`user_stance VARCHAR(3)` 同样不能在 CHECK 阶段区分输入 `pro ` 与 `pro`。本轮保证**持久值字节精确**；不能声称数据库拒绝所有原始越长空格输入。已有回归覆盖 assistant 空格的规范存储；T05 等入口必须按契约对原始枚举精确校验，不以数据库规范化替代输入拒绝。未更改冻结字段类型或增加触发器。
5. T02-M01/M02 已实施并通过作者技术验证，仍待独立复验，不标为正式关闭。T02-M03 稳定诊断原因码仍延期，本轮未改 runner 或恢复机制。
6. 独立审查、人工内容审阅、最终验收和学习讲解尚未执行。未验证开发库迁移、真实断电/进程崩溃、DDL/版本登记窗口人工恢复、生产环境、部署、备份恢复、业务 API 或模型。
7. 继承原测试策略，保留独立 `t02_*` 证据表。每轮完整集成新增：异常枚举 3 对、历史场景 6 张历史表及合法场景 1 张探针、种子冲突 1 对，另有原故障恢复 1 对；本次首轮失败也保留其已创建证据。清理需另行授权，不能以表总数判断六张业务表缺失/重复。

最终工作区核对：19 个变更文件，112 个本地文档链接和 30 个锚点有效；扫描全部变更文件，未发现本地六份秘密文件的任何口令值或常见私钥模式。`git diff --check` 通过（Git 提示既有 CRLF 策略）；原 00001–00006、服务代码/依赖、前端、Compose、迁移执行器/依赖均与 HEAD 一致。秘密、编译产物、持久化证据及本地环境错误记录均被忽略，暂存区为空，HEAD 仍为 `80363ef`。本轮文档改动不另行重跑前端检查。

原实施停止点：提供工作区改动供独立审查；当时尚待负责人验收与学习讲解，无后续开发或 Git 操作授权。最终验收及本次提交授权已在下节更新，不扩大为后续开发授权。

## 最终独立审查、人工验收与 T02 结项

2026 年 11 月 8 日，最终独立技术审查通过，未发现 P0/P1 或阻断提交的问题。审查核对了 00007–00010、种子完整性/幂等/冲突测试设计和历史保护覆盖；原 00001–00006、迁移执行器/依赖及服务代码未受本轮修改。新增代码仅编译检查、PowerShell 脚本仅语法检查，未执行写库测试或重启脚本。

独立解析本地持久化 JSON，确认 Before/After 的历史、六表结构、行数及五条种子完全一致；只读 SELECT 核对当前历史 0–10、五题完整字节内容/启用标志/时间与证据一致，四个精确 CHECK 已启用，尾随空格表达式反例返回拒绝；只读 Docker inspect 核对容器 ID、数据卷和重启时间。未机械重复开发阶段全量 race、冲突写入或重启测试。M01 存储枚举问题已解决，M02 新增测试覆盖符合原关闭要求，均随本次验收关闭。

项目负责人在本次对话中明确确认种子与重启持久化闭环已通过独立技术审查和人工验收，授权整理本轮已验收内容、更新 T02 状态并提交/推送至 origin/feat/t02-database。此最终验收覆盖包含五条辩题的交付，完整 T02 据此结项。学习讲解未单独登记，不补记未提供的学习过程；按 ADR-011 不强制正式讲解登记。T03–T18 及 Phase 1 其他工作未因此完成或获得启动授权。

已接受风险继续延期：M03 在下次调整迁移诊断/恢复说明时补稳定原因码和测试；VARCHAR 超长尾随空格可先截断，T05 等未来业务入口必须校验原始枚举；DDL/版本登记窗口、无校验和/结构漂移、人工恢复未演练及保留探针均沿用既定恢复和另行授权门槛。开发库迁移、真实断电、生产/备份恢复未验证；不以结项替代这些专项工作。

本次仅调整文档状态和整理 Git，不修改 SQL、测试实现、数据库、容器或秘密文件；不创建 PR、不合并 develop/main、不推进后续任务。原始持久化 JSON、缓存、编译产物及秘密只留在 Git 忽略目录，不纳入提交。

## 最终提交前核对

本次核对 19 个交付文件，00001–00006 与 80363ef 的 Git 内容哈希一致；依赖、迁移执行器、服务代码、前端和 Compose 均未变更。六份本地秘密原值及凭据模式扫描无命中，119 个本地文档链接目标存在，换行/尾空白、git diff --check 和 Go 格式检查通过。缓存、秘密、编译产物及原始持久化证据仍被 Git 忽略。

本次沿用上节已完成的独立编译/语法检查和只读核验，不重新执行写库、容器重启或全量回归；未发现新的 P0/P1 或提交阻断。提交前远程 feat/t02-database 为 80363ef0f7dc15ea954465be8871a3a56baef386，与本地基线一致。只按明确文件清单暂存和普通推送，不使用 git add -A 或强制推送；提交及远程结果以本次对话最终报告为准。
