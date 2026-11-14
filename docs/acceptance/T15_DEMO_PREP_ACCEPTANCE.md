# T15-DEMO-REAL 无收费准备与整合等待记录

日期：2026-11-15（Asia/Shanghai）。成员 A，分支 `codex/p3-a-integration`，起点 `b5a90d4d4f9a908b21d97489b38b0a5e0f851af9`。本轮允许本地开发、隔离验证、B 指定成果合入 A、Commit、普通 Push、更新既有草稿 PR #19；未合 main、未调用真实模型、未公网部署。

**结论：A 独立费用保护与 Fake 后端/本地环境准备技术通过；完整 A/B Fake 整合未验收。** 负责人本轮明确回答 B1/B2“未完成”，未提供功能分支/指定提交。未替 B 实现前端，未把等待项记通过。真实体验、10 场评测及 Phase 3 结项均未完成。

## 实际交付与契约核对

| 切片 | 实际交付 | 本轮状态 |
| --- | --- | --- |
| A1 | `f7febe7`：T13 技术矩阵、T14 契约、SSE 四事件及恢复样例、受控多次 Demo 方案 | 已核对实际文档与代码；不是已实现的多次入口 |
| A2 | `9b4968f`、`66dd979`：四流派/策略后端、前向迁移 00013、交接 | 保留；复用有效证据与本轮集成 |
| A3 | `b5a90d4`：Eino Stream、普通/SSE 共享认领与事务、Nginx/Fake 三链路 | CI run 43617061014 四项 SUCCESS，未替代本轮 CI |
| B1 | 自主流派选择、策略卡及显式恢复 | 负责人确认未完成；无指定 SHA，未合入 |
| B2 | 生产 UTF-8 解析、SSE/查询/显式重试交互 | 负责人确认未完成；无指定 SHA，未合入 |
| B3 | 浏览器专项、操作资料、评测准备及人工评测 | 待同一整合版本与单独真实授权，见下表 |

沿用 OpenAPI `phase3-contract-2026-11-14`，17 个已实现操作；`strategy-v1`/schema 1、`summary-v1`/schema 1 及既有轮次 Prompt 未修改。SSE start/delta/done/error、202 JSON、原 client_request_id 查询恢复、查询获得 attempt_version、409 只查规则均不变。修正 API 顶部仍称“SSE 未实现”的过时状态句，与下文 A3 实际状态统一。

## 本轮代码与环境

新增独立 `local-demo` 受保护多次调用路径，显式实现 Generate、Stream、GenerateStrategy、GenerateSummary；复用 OpenAIAdapter/Eino、响应观察、Prompt/Schema、超时和受控错误。费用预留位于最终 RoundTrip 前；严格校验供应商目标、模型/输出/thinking/stream_options、按实际赛制和调用类型推导的消息数及角色，完整历史发送而不裁剪。禁止裸适配器旁路、重定向、同次二次出网与自动补调。旧 T07 单次路径未放宽。

单实例 Linux flock + O_EXCL 文件/目录 Sync；全窗口 calls/input/output/按冻结无缓存价格向上取整的金额预留不退回。批准和 manifest 冻结；启动 boot_id 必须重新人工绑定 enable。持久 stop、在途 100ms 控制观察、出网与结算前复查；故障、未知用量、取消、超时、panic、写失败、缺失/损坏和遗留未结算均保留预留并拒绝继续。原 Gate 6/分钟、20/用户/UTC 日、1/用户并发/全局 2 不变；账本额外跨重启限制真实出网，不提供清零或自动解锁命令。停止晚于费用包装器成功返回时，业务提交可能已经完成；不能承诺取消供应商收费或撤销已提交结果。

运行环境为本机 Docker Desktop Linux，Compose `siyige-p3-demo`，页面/API 回环 18081，内部 `mysql:3306/siyige_demo` 与独立 data/ledger 卷；应用无单独宿主机端口，MySQL 不发布端口。仅隔离库显式执行原 13 份前向迁移与逐表授权，未新增 SQL 迁移。具体启动/配置/停止、参与人和预算字段见 [运行说明](../../infra/demo/README.md)。没有真实 Key、enable 或运行中的中继，实际 `AI_MODE=fake` 已检查；不把真实配置准备当真实网络/TLS或余额保护实测。

## 实际验证

| 验证 | 结果及边界 |
| --- | --- |
| Windows / Go 1.27.1：`AI_MODE=disabled`、清除两个集成开关，后端 `go test ./... -count=1`、`go vet ./...`、`go build ./...` | 通过；Windows 持久目录同步用例明确跳过，由下述 Linux 实跑补齐 |
| 独立迁移模块：`go test ./...`、`go vet ./...`、`go build ./...` | 通过；新增 demo 固定目标保护测试，不改变 dev/test/deploy 目标 |
| Linux golang:1.27.1，无外网、只读模块缓存，`go test -p 1 -race ./internal/ai -run 'TestDemo\|TestTrial' -count=1` | 通过；Demo 持久用例实际执行，无 Windows 跳过；旧 trial 原保护继续通过 |
| 合成供应商 + 真实锁版适配器 | 一策略+四轮 SSE+总结六次均共用费用包装，第七次拒绝；每轮携带完整历史；无真实模型出网 |
| 费用 Fake 专项 | 最后一个槽 12 路竞争仅一次预留；次数/输入/输出/金额触顶；6/分钟与20/UTC日；身份/窗口余量不足；未知 usage、无 usage SSE、HTTP 失败、超时/取消、panic、停止/窗口在途边界；跨重启额度/stop/boot 绑定、双实例锁、配置变化/损坏/丢失、写失败与遗留未完成均通过 |
| 固定隔离 MySQL：`APP_ENV=test`、`127.0.0.1:13307/siyige_test/siyige_test_app`，`go test -race ./tests/integration -count=1` | 串行复验通过 35.168s；没有重启/重置库。用例只清理自己创建的匿名记录 |
| 前端锁定依赖 `npm ci`、typecheck/test/build | 通过，12 文件/186 测试；仍为 A 基线已有前端，不表示 B1/B2 已实现 |
| Demo Docker 构建、配置、隔离迁移和权限 | app/migrate 构建通过，`applied=13`；实际身份 `siyige_demo_app@%`、MySQL 8.4.13；ready 200、Nginx `-t` 通过；秘密初始化重复运行不覆盖 |
| `python infra/demo/probe.py`，固定 Fake 容器检查后经 18081 Nginx | 四流派、空列表、分页/越界空页、非法分页/流派、策略、四轮 SSE、总结、重登历史通过。四轮首/末 delta 秒分别 0.018/0.220、0.014/0.215、0.015/0.215、0.015/0.215；逐字节解析且拼接与正式查询一致。仅新增合成记录，不停止/重启/迁移/清库 |
| Chrome 157 / agent-browser 0.43.0 独立会话 | 实际注册、登录、空历史、禁用分页按钮、刷新登录态/历史通过；没有流派/策略生产控件，不记完整浏览器 AC16 或 SSE 交互通过 |
| A3 未受影响证据 | 复用 UTF-8 拆块、断线/超时/心跳、提交后 done 丢失、共享认领/无双写专项；B 生产解析器及新整合版本浏览器仍须另验 |

保留首次失败：Windows 首次前端缺依赖，按 lockfile npm ci 后通过。Linux 新测试发现私密目录权限、历史消息白名单、输出窗口配置、panic 未持久停用、缺失 manifest 自动补建等问题，已修正并复验。Nginx 首次因只读文件系统下默认 fastcgi 临时目录启动失败，改用 /tmp；隔离网络不发布端口，edge 单独接入入口网络后仅回环发布，app/DB 继续内部隔离。agent-browser 全局命令不存在，npx 运行遇 Windows 原生二进制占用，使用已下载的同版本原生 CLI 完成，没有删除任何用户文件。

MySQL 首次并行重构建时出现 `TestT15CommittedButDoneLostRecoversWithoutRegeneration`、`TestT15StreamDatabaseFailuresNeverSendDone/claim_unknown` 注册失败和 `TestT15StreamPreflightStaysJSON/json` 依赖超时，部分匿名夹具清理也超时；测试容器 RestartCount=0。待构建结束后同一完整集成命令串行通过，未放宽 3 秒数据库预算或清库掩盖。首次失败不计成功，遗留合成数据不做广泛删除。

最终一轮 Windows 全量中，旧 `cmd/t04-smoke/TestTotalBudgetAndShortParentDeadlineReachTransport` 因 100ms 父截止时间在余额阶段先到而失败；同包单独复跑和最终全量 test/vet/build 均通过，未改测试或放宽预算。收尾发现末尾预留/结果同时缺失可能低估历史额度，另补持久 head 校验和先失败后修复用例；缺失或不一致均停用，Linux Demo/Trial race 通过。

最终源码重新构建 app 后，仅替换本轮自建 Demo app，再经 18081 复跑探针通过，四轮首/末 delta 为 0.019/0.219、0.041/0.241、0.032/0.233、0.014/0.214 秒；Nginx `-t` 通过。仓库门禁核对 274 个已暂存/受跟踪文件、54 份 Markdown 相对链接，暂存 Go 格式与 diff 检查通过。Windows 工作副本 CRLF 使直接 `gofmt -l backend` 报出既有文件；按 Git 暂存的 LF 内容逐一检查无格式差异，未改动无关文件。

交接时已退出自己的浏览器会话，并停止本轮自建 `siyige-p3-demo` 的 edge/app/mysql（均 exit 0），保留 data/ledger 卷；本环境当前未运行。共享 8080/5173/18080 和 `siyige-t02-mysql-test-1` 未停止/重启，后者仍 running、RestartCount=0。真实密钥和 enable 不存在，Fake 环境内 CA bundle 存在；真实中继/TLS仍未执行。

## 后续 A/B 分工与未执行项

| 项目 | 后续工作 | 当前结论 |
| --- | --- | --- |
| A/B 整合 | B1/B2 均交付分支、指定 SHA 和验证记录后，A 合入指定提交、解决冲突、同一整合 SHA 重建 | 等待，不是整合通过 |
| AC12 | B 测生产解析器的 UTF-8/半事件/202/错误/有限查询/显式版本重试；A 配合本地 Nginx、断线/提交不明/丢 done；完整四轮浏览器留证 | 未完成，复用 A3 未受影响后端证据 |
| AC13 | B 核对 standard_4 四环节、quick_3 历史第三轮分叉；A 提供有效后端两赛制边界和隔离历史 fixture | 后端既有集成通过；整合浏览器待验 |
| AC16 | B 测四流派、默认/非法、策略四状态、刷新/重登/历史一致、失败恢复；A 协助 API/权限异常 | 后端/HTTP准备通过，生产控件待验 |
| 列表和异常 | B 浏览器分页、空列表、越权/失效/后端异常、断线恢复；A 保持固定 Nginx/Fake 场景 | API 分页/空/非法通过，完整浏览器待验 |
| B3 体验资料 | 在固定整合 SHA 核对启动/体验/停止说明，记录模式/库目标/占用窗口；整理脱敏浏览器证据 | 待 B 完成 |
| B3 评测准备 | 准备 10 行人工检查表，覆盖至少3题、双方立场、四流派；记录每轮回应/立场/事实依据/建议、匿名会话与请求标识、Token/费用/失败及结论 | 只能准备，真实执行未授权 |
| 真实体验 / T14-EVAL | 先完整 Fake 整合，再单独批准两个参与账号、UTC窗口、次数、单次/总Token、冻结单价、总金额、独立余额、专用密钥及停止负责人 | **均未执行**；样例全零无效，无补调授权 |

本轮交接的固定代码 SHA 与当前四项 CI 在草稿 PR #19 更新及最终交接中给出。它是 **A 准备基线**，不是 B1/B2 整合或评测已通过版本；B 可基于它继续验收准备，评测实际执行须等待完整整合固定 SHA 与独立真实授权。负责人尚未确认本任务或 Phase 3 验收。
