# T07 首次受控部署准备验收

日期 2026-11-09（Asia/Shanghai）。**T07 部署准备已审查，待负责人验收。** T07 进行中，Phase 1 未结项。部署配置及获准后操作见 [运行清单](../../deploy/README.md)。

## 基线和范围

- 基线 `78ba3ffb66165e7942cd130308e83690f4c22f83`（`feat/t06-completion`）。本机 `develop` 和 `origin/develop` 引用都是其祖先，均不包含 T06 结项；本轮采用有 T06 人工验收及新版协作规则的指定基线。T02、T03、T05 及集成分支均为其祖先，T04 实现随 T05 继承。
- 实施分支 `feat/t07-controlled-deployment`，工作区为仓库根目录下 `.cache/worktrees/t07-controlled-deployment`。原工作区的 `AGENTS.md` 未暂存修改归属原工作区。
- 范围为 Compose、镜像构建、Nginx、账户初始化、运行清单及必要的 Go 接线。基线仅允许 dev/test 回环，迁移 CLI 也仅接受两个固定回环目标。显式 `deploy` 才接受固定容器监听与内部 MySQL 服务；要求 HTTPS Origin 和独立应用账号，显式迁移仍使用固定库及独立迁移账号，复用同会话身份、版本、锁和前向历史检查。
- T03 原限流以 TCP 对端计数，经 Nginx 时所有浏览器会被合并成一个地址。部署模式只接受专用代理网中固定 Nginx 对端重写的单值 `X-Real-IP`，拒绝直连伪造和多值/无效头影响限流。Gin 全局仍关闭信任代理，开发和测试模式保留直连。T03 认证策略、T04/T05 生成预算、路由和 DTO、SQL 版本、T02 Compose 均沿用。

## 本轮验证

本轮在 Windows 本机使用已有工具和缓存，检视 [Phase 1 门槛](../planning/PLAN.md#phase-1基础设施与最小业务链路)、[PRD](../product/PRD.md) AC07/AC09、[部署架构](../architecture/ARCHITECTURE.md#部署持久化与恢复方案)、[API](../api/API.md)、[OpenAPI](../api/openapi.yaml) 与 ADR-004/005/008/012/013/015/016/017/018。

| 检查 | 实际结果 |
| --- | --- |
| Docker Compose v5.5.1 `config --quiet` / `--format json` | 默认及 migration profile 离线解析通过。核对端口、内部网络、固定对端、命名卷、探针、日志轮转、模板变量、仅文件挂载的凭据与 profile 隔离。 |
| Nginx 静态结构 | 模板替换、块结构、健康及 API 匹配、无 URI 的 `proxy_pass`、HTTPS 跳转、覆盖来源地址、上游状态与媒体类型策略、68 秒超时、SPA 分离，与 OpenAPI 健康响应交叉核对通过。此项未使用 Nginx 二进制。 |
| Bash 与文档 | `bash -n` 检查两个入口及清单 Bash 片段，通过五个非法域名输入拒绝检查（含换行）；两个脚本为 LF。新增链接和锚点、示例占位、秘密忽略规则、敏感模式及 `git diff --check` 通过。沙箱内 Git Bash 无法初始化，通过本机静态检查重试。 |
| 后端 Go | 先确认部署配置和迁移测试因缺少 deploy 模式失败，认证回归在缺少可信代理配置字段时无法编译。修改后，以 `GOPROXY=off`、`GOSUMDB=off`、`AI_MODE=disabled` 且清除 MySQL 集成开关执行 server module `go test -count=1 ./... -skip <下述五项>`，全部纳入的用例通过。两个 Go module 的 `go vet ./...` 及 Linux amd64、CGO 关闭的 `go build -mod=readonly -trimpath` 均通过。迁移 module `go test -count=1 ./...` 通过，真实 MySQL 用例按开关跳过。 |

最终回归排除 `TestRunServesHealthDuringDatabaseOutageAndShutsDown`、`TestRunRegistersAuthRoutes`、`TestReadinessDatabaseHandshakeTimeout`、`TestDatabaseSecretsAndLazyConnection`、`TestOpenAIGeneratorLoopbackHTTP` 五项本机监听/连接夹具。第一次误纳入的 `TestDatabaseSecretsAndLazyConnection` 在沙箱返回受控连接错误而非预期 deadline，未用它作为通过证据。既有 Go/sonic 兼容提示不计为部署表现。前端业务源未改，未重跑前端依赖安装或 Vite 构建。

## 独立只读审查

初审为全新 Codex 会话 `01a1beb1-8440-7402-bc23-c9384017c27a`。审查开始和结束均核对基线、23 个文件及逐文件 SHA-256，核对固定 diff 哈希；只读初审不通过，报告 1 项 P1、4 项 P2、1 项 P3，无新增 P0。以下修复由实施方完成，按新快照独立复审。

| 发现 | 修复与隔离复验 |
| --- | --- |
| P1 · 新卷初始化脚本被 source 后可让官方入口继承 `nounset` 并中断 | 主体在子 Shell 中执行。隔离实跑确认空卷初始化完整结束、重启可用及后续迁移。 |
| P2 · 应用账号库级权限可改写 Goose 历史 | 启动初始化只建账户，迁移完成后管理员按六张业务表分别授权。隔离实跑核对业务读写、版本表写权限拒绝及 SHOW GRANTS。 |
| P2 · Nginx 本地 413 缺少业务信封 | 仅本地 API 超限响应返回 JSON、请求 ID、no-store，继续透传上游 413。隔离验证 32 KiB 边界、固定长度和分块。 |
| P2 · 回退的单命令环境覆盖会被后续 `up` 反转 | 回退写入当前环境实际读取的发布版本，常规 `up` 再跑并核对镜像 ID。 |
| P2 · TLS 私钥的容器 GID 可能与宿主机组冲突 | 在宿主机 root 专用父目录下保存证书，容器只挂载所需子目录。隔离核对容器能读且宿主机非授权账号无法遍历。 |
| P3 · API 仍称只支持本地模式，PLAN 中 T06 状态过期 | 更新两处当前说明并链接验收记录。 |

实施方另为可信代理固定地址划出动态分配范围，并拒绝 URI 点段穿过 API/健康与 SPA 边界。修后两套 Compose 离线解析、静态代理 413/路径断言、Bash 模拟 source（替代读取及 MySQL 命令）、六表权限清单、连续两次离线渲染回退版本、32 个新增文档链接或锚点及敏感/差异检查均通过。Go 变更与初审快照相同。复审快照包含上述增量。独立审查仅做源码核对，隔离项仍待执行。

全新 Codex 会话 `01a1bebb-5483-7560-b72c-4e54f0acf367` 独立只读复审有条件通过。开始与结束均核对基线、空暂存区、26 个路径和逐文件 SHA-256，固定 diff 无漂移。初审 P1 及四项 P2 均静态关闭，固定 IP 分配与点段修复亦通过，无新增 P0–P2。两项 P3 按本轮授权登记后续处理，交付文件相对复审快照仅本记录和 TASKS 增加结果：

- T07-PREP-01 · PLAN 页首已更新，「当前阶段与证据」内仍有未标明的历史阶段快照。A 在后续文档维护中补标，当前状态以 TASKS 和各任务验收记录为准。
- T07-PREP-02 · 边缘本地 413 有响应请求 ID，但 Nginx 访问日志尚无法按该 ID 关联。A 在隔离实跑时核对关联需求和脱敏日志。当前业务 413 信封与状态的静态复核已通过。

## 复用的既有证据

| 证据 | 本轮复用边界 |
| --- | --- |
| [T02 结项](T02_SEEDS_PERSISTENCE_ACCEPTANCE.md#最终独立审查人工验收与-t02-结项) | 固定隔离 MySQL 迁移、种子、重启持久化；新 Compose 权限及新卷待实跑 |
| [T03 验收](T03_AUTH_ACCEPTANCE.md#独立审查与人工验收结果) | 认证安全与已接受 P2/P3；部署 HTTPS Cookie、反代限流、双来源和实际网络待复验 |
| [T04 结项](T04_AI_ACCEPTANCE.md#最终人工验收与结项) | 模型适配及一次已获准本地冒烟；该授权已消耗，精确结算费用未知 |
| [T05 无收费结项](T05_FIRST_TURN_ACCEPTANCE.md#最终人工验收与无收费范围结项) | 首轮、限流、持久化、越权、幂等和失败分支；无真实 AI 首轮部署证据 |
| [T06 人工验收](T06_COMPLETION_ACCEPTANCE.md#负责人最终人工验收) | 本地 Vite、真实 Go 与测试 MySQL 的 Fake 浏览器联调；不计为 Nginx、HTTPS 或公网验收 |

## 后续授权及风险

- 未执行容器启动、镜像拉取或构建、原生 `nginx -t`、证书或 DNS/防火墙操作、MySQL 连接或写入、迁移、实际发布和回退、真实模型及公网访问。授权隔离实跑后先验证镜像和 Nginx 解析、内网隔离、初始授权 SQL 与新卷迁移，再执行故障、HTTPS 认证、AC09 首轮与 AC07 重启子集。首发/生产数据库操作、证书/网络资源、真实模型及公网开放逐项明确授权。
- [T07-COST-01](../planning/TASKS.md#t07-cost-01-供应商硬预算缺失) 未关闭，P0 阻塞公网真实模型入口。关闭须取得供应商强制硬上限，并在另行授权的隔离环境验证范围、触顶拒绝、在途并发和结算边界，负责人根据脱敏证据批准关闭。预付、应用计数、告警与人工停用不替代该门槛。
- T02-M03、T03 已接受的 P2/P3、T05 首轮边界与崩溃残留、T06 已登记验证边界继续有效。已采用固定 Nginx 子网、主机证书续期、回环发布和内部无供应商出网。公网发布与真实模型出网需要单独审查配置变更；完整三轮、总结、历史及独立备份恢复演练按后续任务验收。
