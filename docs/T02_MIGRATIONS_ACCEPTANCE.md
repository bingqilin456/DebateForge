# T02 版本化迁移技术验证记录

当前结论（2026 年 11 月 8 日）：后续种子与持久化闭环已通过最终独立审查及项目负责人人工验收，完整 T02 已完成；M01/M02 关闭，M03 等风险继续延期。详见 [最终记录](T02_SEEDS_PERSISTENCE_ACCEPTANCE.md)。本文下方待验收、未提交和进行中等状态为原轮次历史证据。

日期：2026 年 11 月 8 日（Asia/Shanghai）。分支：`feat/t02-database`。实施前工作区干净；实施阶段未 commit、push、merge 或部署。后续验收与提交授权见本文末尾。

状态：**本迁移子任务技术验证及有条件独立审查通过，负责人已接受验证边界及延期项并授权本轮提交/推送；学习讲解尚未登记，完整 T02 保持进行中。** 本文是作者的技术自查及实际测试证据，不冒充独立审查或人工验收结论。

后续状态：2026 年 11 月 8 日另获种子与持久化授权，新增 00007–00010、历史保护回归和测试库重启验证。M01/M02 已技术验证待独立复验，M03 仍延期，见 [后续记录](T02_SEEDS_PERSISTENCE_ACCEPTANCE.md)。本文以下六表/无种子等内容保留原轮次证据，历史提交授权不适用于本轮；本轮不 commit/push/merge，完整 T02 继续进行中。

## 本轮范围与文件

用户仅授权版本化迁移、重复执行保护、失败处理和必要测试/文档。5 条辩题种子、其他 T02 子任务、T03、总结表和业务接口均未实施。

| 范围 | 文件 |
| --- | --- |
| 初始 SQL | `backend/migrations/00001_create_users.sql` 至 `00006_create_messages.sql` |
| 使用与恢复 | `backend/migrations/README.md` |
| 独立迁移工具 | `backend/tools/migrate/{main,config,runner}.go`、独立 `go.mod/go.sum` |
| 自动化测试 | 工具目录 `config_test.go`、`runner_test.go`、`integration_test.go` |
| 状态与选型 | `AGENTS.md`、`README.md`、`docs/{ARCHITECTURE,DECISIONS,PLAN,TASKS}.md` |
| 本轮执行计划 | `docs/superpowers/plans/2026-11-08-t02-migrations.md` |

服务端 `go.mod/go.sum`、已有业务/健康代码及测试、前端、API/OpenAPI、Compose 与账户均未修改。没有创建 `deploy/`，没有接入 AutoMigrate。编译产物只在被忽略的 `.cache/t02-mysql/migrate.exe`。

## 架构和数据库结果

沿用 Goose v3.29.0，用官方 Provider 构建独立 CLI module，不把迁移依赖引入服务。六张表及物理字段选择见 [迁移说明](../backend/migrations/README.md#版本与物理结构)，落地决策见 [ADR-013](DECISIONS.md#adr-013-t02-独立-goose-迁移入口与失败保护)。

流程为：解析显式 env/秘密文件/up或status → 固定本地目标校验 → 有界读取秘密 → 同一个实际 SQL 连接校验库/账户/MySQL 版本 → 命名锁 → 校验已执行历史为本地版本的前缀 → Goose 初始化元数据/执行单版本 SQL/登记成功 → 解锁。已执行版本不重复 DDL 或 INSERT；多进程可在版本边界交替，锁冲突立即拒绝。原始 SQL、DSN 和驱动错误不对外输出。

只对 `127.0.0.1:13307/siyige_test`、`siyige_test_migrator@%` 执行迁移。迁移前 `information_schema.tables` 查询无行，证明本次从测试空库开始。首次执行输出 `applied=6`；最后的 `status` 显示 1–6 均 applied。正式历史为：

```text
version_id  is_applied  tstamp (UTC)
0           1           2026-11-07 18:36:26
1           1           2026-11-07 18:36:26
2           1           2026-11-07 18:36:26
3           1           2026-11-07 18:36:26
4           1           2026-11-07 18:36:26
5           1           2026-11-07 18:36:26
6           1           2026-11-07 18:36:26
```

对应本地时间为 2026 年 11 月 8 日 02:36:26。六表 users、auth_sessions、topics、debates、debate_turns、messages 的持久行数均为 **0**；业务夹具已事务回滚，topics 没有种子。`debate_summaries` 不存在。开发库只读核对表数为 **0**，没有执行迁移。

## 实际验证

工具环境 Go 1.27.1/windows amd64、MySQL 8.4.13；race 使用现有 MinGW GCC。两个 T02 容器均保持原有运行实例、healthy；未重启/重建容器、未删除卷或清空数据，未操作其他项目容器。

从 `backend/tools/migrate/` 执行：

```powershell
go test -count=1 -v ./...
go vet ./...
go build -o ../../../.cache/t02-mysql/migrate.exe .
go mod verify
gofmt -l .

$env:SIYIGE_MIGRATION_TEST = '1'
$env:SIYIGE_MIGRATION_PASSWORD_FILE = (Resolve-Path '../../../.cache/t02-mysql/secrets/test-migrator.txt').Path
go test -race -count=1 -v .
Remove-Item Env:SIYIGE_MIGRATION_TEST, Env:SIYIGE_MIGRATION_PASSWORD_FILE
```

默认测试通过并跳过四个真实 MySQL 用例；显式含 race 的最终全量测试 **8 个测试全部通过**。vet/build/模块校验通过，gofmt 无输出。真实 MySQL 覆盖：

- 六表 InnoDB/utf8mb4、六个自增 unsigned BIGINT 主键、五条 RESTRICT 外键；版本 1–6 各成功一次。
- 新 provider 模拟进程重跑：`applied=0`，版本历史（含 ID/时间）和各业务表行数不变。
- 用户名/Session 摘要/会话轮号/幂等键/每轮角色的唯一保护；非法外键、状态、轮号、完成轮数、attempt_version 被拒绝；中文和 emoji 保真；未知 token 用量保持 NULL。
- 固定测试连接的实际身份、UTC/utf8mb4 参数；错误环境、开发端口/库、root/应用账户及 down/reset/redo 等在读秘密前拒绝，服务/Goose 环境变量不能重定向入口。
- 故障夹具 version 1 成功，version 2 在 CREATE TABLE 后 SIGNAL 失败：实际表存在，但历史只有 0/1；重跑因已有表再次失败。仅修订内存隔离夹具模拟经批准的前向恢复后，2 成功登记，再次执行不增加记录；删除内存中已执行文件后 status 拒绝。没有自动修复正式版本或补记正式历史。
- 两个独立连接验证锁冲突时拒绝、释放后继续；真实解锁后注入清理错误，确认错误与已执行信号合并时不会误判成功。

从仓库根目录调用已编译 CLI（秘密通过绝对文件路径传入，不传内容）：`status`、`up`、`up` 均成功，两次 up 均 `applied=0`；误用 `test-app.txt` 配合迁移账户时退出非零，仅输出受控错误与 `mysql_code=1045`。

从 `backend/` 执行服务回归：`go test -count=1 ./...`、`go vet ./...`、`go build ./...`、`go mod verify` 均通过，`gofmt -l cmd internal tests` 无输出。再按现有 README 显式设置 `SIYIGE_INTEGRATION=1` 和完整固定测试应用账户参数，执行 `go test -count=1 -v ./tests/integration`，原有两个真实 GORM/健康用例通过；只读，不清理或迁移。

交付检查覆盖 23 个新增/修改文件和 96 个本地文档链接，无缺失目标/尾空白/缺少末尾换行；以六份本地秘密原值比对交付文件，无命中。`git diff --check` 通过，仅有现有 Windows 行尾转换提示；暂存区为空，范围外服务/前端/API/Compose 文件及服务模块依赖与 HEAD 一致。`.cache` 产物和秘密仍被忽略。

## 开发时失败与修正

不把中途失败隐藏为全程成功：

1. 默认沙箱不允许 Docker named pipe 及部分本地套接字；默认服务回归出现 `TestRunServesHealthDuringDatabaseOutageAndShutsDown` 的启动失败、`TestDatabaseSecretsAndLazyConnection` 的 deadline 断言失败。提升到允许本地套接字的执行环境后同一服务测试全通过，服务源文件未修改。依赖缓存写入被拒后，通过获准提升下载固定工具依赖。
2. 首批测试先因入口/执行器尚未实现失败；配置实现后单元通过，真实初始迁移用例因 `no migrations found` 失败，随后添加六个 SQL 文件。
3. 新工具驱动 v1.10.1 把 `Params["charset"]` 当作系统变量，连接报 1193。查官方 Charset API 后改为 `Config.Apply(mysql.Charset(...))`，真实连接/会话参数检查通过；没有修改既有服务驱动代码。
4. 首次建表已输出 `applied=6`，该轮测试随后因非法立场夹具长于 VARCHAR(3) 得到 1406 而失败。改用长度合法但枚举非法的 `bad`，验证真正触发 CHECK 3819；六表和已登记历史没有删除重建。
5. Goose Up 会在 SessionLocker 前初始化元数据且无 pending 时不取锁，初版锁反例失败。改用逐版本 ApplyVersion，并补充历史前缀检查；锁反例、缺失本地已执行文件反例转为通过。
6. 技术自查复现 `errors.Is(ErrAlreadyApplied)` 会吞合并的解锁错误；首次改成直接相等又因 Goose 的单层版本包装导致正常重跑失败。最终允许单一包装链中的已执行信号，拒绝含 `Unwrap() []error` 的合并错误；正常重跑、解锁失败反例和完整 race 套件均通过。

## 保留证据、风险与未执行项

失败恢复测试按本轮“不清空现有数据”约束保留独立证据表，每次运行增加一对。本轮共保留六对，后缀如下；既有失败记录和处于部分完成状态的探针均未清除：

```text
18e5d7269d2bce1c
18e5d7355d3dd868
18e5d74015e7d5f8
18e5d767f6ae76b0
18e5d7abc5c34258
18e5d7ba094c3a58
```

每个后缀对应 `t02_failure_versions_<后缀>` 和 `t02_failure_probe_<后缀>`；这些不是业务实体或种子，清理需单独授权。事务回滚可能消耗自增编号，不承诺 ID 连续。

- MySQL DDL 与版本登记仍有故障窗口；本轮验证了“保留部分 DDL、失败版本不登记、重复不静默跳过”，没有声称数据库操作整体可回滚。恢复指引见 [迁移说明](../backend/migrations/README.md#失败重跑与恢复)。
- 未在正式历史上模拟进程硬终止、补记版本或执行破坏性恢复；不提供自动修复/Down，不声称覆盖所有断电时序。
- Goose 原生历史无 SQL 校验和，不检测所有已登记表的后续手工结构漂移；文件不可变和独立结构审查仍是要求。
- 工具只支持冻结的本地 MySQL 8.4.13，不是生产迁移入口；未来生产配置/备份/迁移权限需单独设计与授权。
- 未做容器重启、种子持久化、浏览器/前端、真实模型、生产部署验证。这些与本轮范围无关或尚未获准，不能算作已通过。

## 审查与下一项

作者已逐项检查目标保护发生在秘密/联网前、实际身份检查发生在元数据前、DDL 与成功登记的顺序、锁冲突/解锁错误、历史缺失和错误脱敏，并核对表/字段/索引与冻结逻辑设计。最终自查未发现待修复的阻断问题；上述数据库故障窗口和漂移限制明确保留。

- [x] 独立代码/SQL 审查有条件通过，未发现 P0/P1。
- [x] 2026 年 11 月 8 日负责人接受本轮验证边界及两个 P2/一个 P3 延期，并授权提交/推送。
- [ ] 学习讲解：入口为何独立、六表依赖、版本重复执行、MySQL 隐式提交与恢复路径。
- [ ] 另行授权后实现 5 条人工审阅的中文辩题种子及重启持久化验证。

至此停止，不推进其他 T02 或 T03，不将完整 T02 标记完成。

## 独立复审、负责人验收与提交授权

2026 年 11 月 8 日独立复审对照冻结模型、实际文件及 Goose v3.29.0 源码，确认独立 module 与服务解耦、同会话身份校验/锁/DDL/登记顺序和解锁错误处理；四个离线单测、vet、模块校验、gofmt 及 Git 差异检查通过。仅以 SELECT 独立核对测试库身份、六表引擎/排序规则、版本 0–6 及时间戳，与本文记录一致；并确认 ascii_bin 的 PAD SPACE 属性及枚举尾随空格比较问题。

独立复审未重跑真实迁移、数据库失败注入、锁冲突或 race 集成测试，也未验证硬终止后的人工恢复；前文八项测试含 race 的结果属于开发阶段记录，不冒充独立复验结果。SQL 无校验和和后续手工结构漂移风险仍保留。复审发现两个 P2 和一个 P3，完整问题、影响、负责人及关闭条件登记于 [TASKS](TASKS.md#t02-迁移延期问题登记)。

项目负责人在本次对话中接受有条件通过、上述验证边界及三个延期项，授权登记、核对交付后在当前分支提交，并推送到 origin/feat/t02-database。枚举 CHECK 必须在业务状态写入前解决，禁止修改已执行的六份 SQL。学习讲解未登记，5 条辩题种子和重启持久化验证未实施；完整 T02 继续进行中。本次授权不包括 PR、合并、部署或后续开发。

## 本次提交前核对

本次仅更新验收状态、授权边界及风险登记，不改迁移工具实现/测试或六份 SQL。工具目录执行 `go test -mod=readonly -count=1 -v ./...`（显式关闭 SIYIGE_MIGRATION_TEST）：四个离线测试通过、四个 MySQL 用例跳过；`go vet -mod=readonly ./...`、`go mod verify` 通过，`gofmt -l .` 无输出。未重新执行真实迁移、写入集成测试或 race。

六份 SQL 的 SHA-256 与本次开始时逐一一致。交付限定为迁移 SQL/说明、独立工具及其测试/依赖、相关验收和状态文档；以六份本地秘密原值及凭据模式检查交付内容，无命中，秘密与编译产物仍被 Git 忽略。本次核对未发现新的 P0/P1 或提交阻断。远程分支在提交前查询为 `75de380f8c9f4f9dfb338830c2852f2eb835f265`，与本地提交基线一致；后续普通推送不得强推覆盖远程。
