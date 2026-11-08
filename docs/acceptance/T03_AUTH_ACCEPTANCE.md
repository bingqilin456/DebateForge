# T03 注册登录与认证保护：技术验证记录

日期：2026 年 11 月 8 日（Asia/Shanghai）。基线 `63d287e`，分支 `feat/t02-database`。用户本轮明确授权完整 T03，覆盖历史“不启动 T03”的停止边界；明确禁止 commit/push/merge/deploy 和进入 T04–T06。

**判定：T03 实现及 Codex 技术验证通过，可以进入独立审查。独立审查、项目负责人人工验收均未完成，任务不标记已完成。** 未虚构成员学习、讲解或互审经历，Phase 1 未结项。

## 实施内容与分工

Codex 读取冻结契约、核对环境、写实施计划并主控监督；DSH 在同一任务 `dsh_a69ff44f263a` 内实现密码、认证核心与 HTTP/真实 MySQL 测试。采用 new-session，后续复用同一已知任务；保留当前模型与 Agent Preset，未发起模型选择或默认配置写入。DSH 完成后停止，Codex 确认 turn_completed 并释放写入 claim，再作独立修正、技术验证与文档同步。桥接不暴露供应商缓存遥测，providerCacheEvidence=not_exposed。

主要文件：

- `backend/internal/auth/`：凭据规范化、Argon2id、Session、DTO、Repository、Service、有界限流、HTTP Handler 和可复用中间件，及相邻行为测试/小样本 benchmark。
- `backend/internal/platform/config.go`、`router.go` 与新增认证测试：AUTH_ORIGIN 配置校验、路由接线、404/405/no-store。
- `backend/cmd/server/main.go`、`main_test.go`：组装真实认证依赖、验证启动入口确实注册认证路由。
- `backend/tests/integration/auth_test.go`：真实 HTTP、Argon2id、GORM/MySQL 的完整认证链路。
- `backend/go.mod`：仅将原已锁定的 x/crypto v0.48.0 改为直接依赖；go.sum 不变。`backend/.env.example` 增加非秘密 AUTH_ORIGIN。
- 当前状态及契约说明：AGENTS、README、TASKS、PLAN、ARCHITECTURE、DECISIONS、API/OpenAPI 与本记录；[实施计划](../planning/2026-11-08-t03-auth.md)。

复用 T02 的数据库、账户、连接配置、十份迁移和五条种子。未修改迁移/迁移执行器、基础设施、秘密或前端文件；没有服务启动自动迁移/播种。

## 行为与安全契约

| 能力 | 本轮实现 |
| --- | --- |
| 注册 | POST /api/v1/auth/register；trim 后小写，3–32 ASCII 字母数字下划线；密码原文 12–128 码点且最多 512 字节；201 只返回字符串 ID 和用户名，不自动登录；重复/并发唯一冲突 409。 |
| 密码 | Go 官方扩展库 Argon2id，19 MiB/2/1，独立 16 字节随机盐、32 字节派生值，保存带参数编码；限制损坏编码的资源参数，常量时间比较。 |
| 登录 | POST /api/v1/auth/login；未知用户名和错误密码同为 401 invalid_credentials、同提示，未知用户也执行同参数密码验证；Session 写入确认成功后才签发 Cookie。 |
| Session | 32 随机字节 base64url 原值仅通过 Cookie；数据库 token_hash 是 SHA-256 原始 BINARY(32)；独立 32 随机字节 CSRF；固定 24 小时，无滑动续期。 |
| me/退出 | GET /api/v1/auth/me 恢复用户及当前 Session 的 CSRF；失效/伪造/到期为 401；退出只删除当前 Session，清除同名同路径 Cookie，204 无正文，旧 Cookie 不能复用，其他 Session 保留。 |
| Cookie | HTTP 开发 siyige_session_dev；HTTPS __Host-siyige_session 且 Secure；均 HttpOnly、Lax、Path=/、无 Domain，Max-Age=86400，退出 Max-Age<0。 |
| Origin/CSRF | POST 精确匹配唯一 Origin，缺失/null/不匹配/重复或合并头均 403 csrf_failed；仅需 Session 的 POST 另校验唯一 CSRF；匿名注册/登录忽略旧 Cookie/CSRF。GET me 不要求 Origin/CSRF。重复同名 Session Cookie 为 401。 |
| 输入与响应 | MaxBytesReader 强制 32 KiB，包含未知长度/分块输入；JSON 媒体类型、UTF-8、单文档校验；未知字段按 T01 未裁定规则容忍。成功/错误 no-store，request_id 与头一致，不泄露内部原因或凭据。 |
| 资源保护 | 注册 IP 5/h，登录 IP 与规范用户名分别 10/min，密码运算之前限流；429 带正整数 Retry-After。计数最多 20000 key，只清过期项；密码并发最多 2，满载受控 503，不排长队。 |
| 后续复用 | RequireAuth、RequireOrigin、RequireCSRF；Gin 的 current_user 保存 typed CurrentUser，通过 CurrentUserFrom 获取；Handler 不直接执行 SQL，Repository 从请求派生最长 3 秒 Context。 |

AUTH_ORIGIN 默认 `http://127.0.0.1:5173`。仅 http/https，HTTP 仅回环 IP，HTTPS 可域名/IP；拒绝路径、userinfo、查询/片段（含空标记），默认端口规范化，IPv6 保留方括号。配置规范结果可再次用于初始化；APP_ENV 仍只允许 dev/test。安全 Cookie 策略通过测试不代表生产部署已就绪。

## 测试先行与审查修正

DSH 的密码与核心切片先观测缺失实现失败，再实现通过；测试还捕获了 Unicode `İ` 转为 ASCII 绕过白名单、短窗口限流记录未回收的问题。Codex 审查要求补上重复 Origin/CSRF 头拒绝和测试诊断脱敏。

首次真实 MySQL 测试有夹具失败：注册 ID 解码类型错误、CookieJar 与显式 Cookie 重复、204 被解成 JSON、重建池的 Cleanup LIFO 顺序错误。修正为字符串解析/立即登记确切 ID、显式 Cookie、204 单独检查、独立校验身份的清理连接。该失败曾留下本轮 7 个随机测试用户和 1 个 Session，DSH 已按确切 ID 一次性清理；没有清空表或删除非本轮数据。Codex 最终另行核对零残留。

Codex 接管后新增四项回归，实测 RED：IPv6 方括号丢失/默认端口无法复用、空 query/fragment 标记被接受、Service 丢失存储 cause、WithClock 不控制默认 Limiter。修正并验证 GREEN；错误字符串仍受控，cause 只供 errors.Is/As，不记录原始驱动信息。没有削弱认证、安全参数或数据库测试。

DSH 分轮原始自检记录保存在 Git 忽略的 `.cache/t03-dsh-status.md`；以下最终判定依据 Codex 独立运行，不仅依据 DSH 声明。

## Codex 独立验证结果

环境：Windows amd64、PowerShell 7.6.9、Go 1.27.1；现有 Docker desktop-linux MySQL 8.4.13。固定测试目标 `127.0.0.1:13307/siyige_test`，应用账户 `siyige_test_app@%`。连接前白名单、连接后实际身份均保留；清理连接也先校验目标再读秘密、建连并核对身份。

| 实际命令/检查 | 结果 |
| --- | --- |
| backend：go test -count=1 ./...（默认，不开集成） | 四个包通过；真实 MySQL 用例默认跳过，不把跳过计为数据库验证。 |
| backend：SIYIGE_INTEGRATION=1，go test -count=1 ./... | 四个包通过；真实 MySQL 包含 9 个认证用例和 2 个既有连接/健康用例。 |
| backend：相同隔离目标，go test -race -count=1 ./... | 四个包通过，包含真实 MySQL HTTP 并发注册、连接重建、注销/过期及健康回归，无竞态报告。 |
| backend：go vet ./...、go build ./...、go mod verify | 全部退出 0，all modules verified。 |
| backend：gofmt -l cmd internal tests | 无输出。 |
| backend/tools/migrate：go test -count=1 ./...、go vet ./...、go build -o <临时路径> .、go mod verify | 默认单测、检查及构建通过；未开启迁移写库测试、up/status 或容器重启。 |
| SQL 只读核对 | 最终 users=0、auth_sessions=0、topics=5；goose_db_version=11 行、max_version=10，与开始时一致。 |
| OpenAPI/契约 | openapi-spec-validator 通过；无重复 YAML 键；6 个 implemented、4 个 planned；与 HEAD 的路径、请求、响应、安全要求及共享 Schema 逐项一致，Cookie 名占位符已具体化。 |
| 文档/范围/隐私 | 本地文档文件链接检查通过；git diff --check 无空白错误；未改迁移、迁移工具、infra/mysql、frontend、go.sum；秘密和 DSH 临时证据均被 Git 忽略。 |

认证测试覆盖正常链路、重复/并发注册、非法输入、统一登录失败、未登录、Session 伪造/撤销/固定到期、两 Session 互不误删、存储失败、日志及响应脱敏、Cookie、Origin/CSRF、限流前不哈希、体积边界和健康回归。连接池及 HTTP Service 重建后可恢复 Session，证明数据来自真实持久化；未用 Mock 代替此项验证。

初次受限沙箱的 Docker named pipe、回环 HTTP/MySQL 访问失败，导致既有 TestRunServesHealthDuringDatabaseOutageAndShutsDown、TestDatabaseSecretsAndLazyConnection、两项 MySQL 测试失败；正常权限环境对同一基线及最终实现复跑均通过，未为此改弱测试。补充回归测试在受限构建缓存也最终返回预期 RED；后续统一用正常工具链/回环权限执行，未反复重建环境。

DSH 的本机 5 次小样本 benchmark 约为哈希 34.3 ms、验证 35.3 ms（Ryzen 7 6800H），仅作密码参数的本机观测，未独立重测或外推吞吐量、生产性能/容量。

未执行：浏览器/Vite 认证联调、前端测试（前端未修改）、实际生产 HTTPS/Nginx、公网、真实模型、数据库/容器重启、T02 全套迁移故障/恢复验收。无部署或生产能力声明。

## 问题、边界与后续条件

- P0/P1：最终技术审查未发现未解决项；阻断问题为零。
- P2：本轮审查发现的 Origin/错误上下文/时钟问题已修正并回归；无新增未解决 P2。
- P3/已接受边界：单实例内存限流重启丢失计数；过期 Session 已拒绝认证，但行的维护清理未自动化；当前直连仅使用 socket 对端 IP。T07 引入已知 Nginx 代理时须重新验证可信代理与限流地址来源、HTTPS Cookie 和安全门槛；本轮不扩大部署范围。
- 既有 T02-M03（P3，迁移受控诊断原因过泛）及已接受恢复边界保持延期，未借 T03 修改迁移工具。
- 开发库仍未迁移，本轮只验证固定隔离测试库；开发库需按后续明确授权显式执行既有迁移，不能以 ready=200 推断业务表齐备。

无需人工修复环境。下一步是独立审查全部未提交变更，再由项目负责人最终人工验收。此记录不授权提交/推送/合并、部署或启动 T04–T06。

## PowerShell 复核命令

在仓库根目录、新开的 PowerShell 7.2+ 终端运行（只对现有固定隔离测试库写本轮夹具）：

```powershell
$env:SIYIGE_INTEGRATION = '1'
$env:APP_ENV = 'test'
$env:DB_ADDR = '127.0.0.1:13307'
$env:DB_NAME = 'siyige_test'
$env:DB_USER = 'siyige_test_app'
$env:DB_PASSWORD_FILE = (Resolve-Path '.cache/t02-mysql/secrets/test-app.txt').Path
Set-Location backend
go test -race -count=1 ./...
go vet ./...
go build ./...
```

每条确认退出码 0 再继续。完成后关闭该终端，避免将 test 的环境变量沿用到开发服务。复制的是秘密文件路径，不是口令；不要打印密码、Cookie 或认证响应中的 CSRF。

独立审查结论：**待执行**。项目负责人最终人工验收：**待执行**。

## 独立审查与人工验收结果

日期：2026 年 11 月 8 日（Asia/Shanghai）。本节记录后续独立审查及项目负责人反馈，作为本文件的最新验收结果；前文“待执行”“人工验收未完成”等表述保留为验收前的历史状态。

**独立审查结论：有条件通过，无未解决 P0/P1。项目负责人已完成 T03 人工验收，并明确接受下列 P2、P3 风险延期；接受延期不代表问题已修复。**

### 延期风险

| 级别 | 问题与影响 | 负责人决定 |
| --- | --- | --- |
| P2 | 集成测试部分失败路径不能保证 Session 清理：登录已写库，但响应缺少 CSRF 或 Cookie 时，测试可能在登记 Session ID 前失败，清理遗漏 Session，用户删除也可能受到外键限制，留下本轮夹具。 | 接受延期。 |
| P3 | 单实例内存限流计数在进程重启后丢失。 | 接受延期。 |
| P3 | 过期 Session 已拒绝认证，但过期行的维护清理尚未自动化。 | 接受延期。 |
| P3 | 既有 T02-M03：迁移受控诊断原因过泛，影响故障定位效率。 | 接受延期。 |

### 项目负责人人工测试结果

以下结果来自项目负责人本次反馈，不记为 Codex 本轮重新执行的测试；不记录密码、Cookie、CSRF 或 Session 原值。

| 操作 | 人工测试结果 |
| --- | --- |
| 注册新账号 | 注册成功。 |
| 重复注册 | 返回 `username_conflict`；HTTP 状态码未留存，不作推测。 |
| 正确密码登录 | HTTP 200。 |
| 错误密码登录 | HTTP 401。 |
| 不存在用户名登录 | HTTP 401。 |
| 查询 `/api/v1/auth/me` | HTTP 200。 |
| 两个独立客户端 A、B 登录 | 均为 HTTP 200，Session 不同。 |
| A 退出及后续查询 | 退出 HTTP 204；A 的旧 Session 返回 HTTP 401；B 仍返回 HTTP 200。 |
| B 退出及后续查询 | 退出 HTTP 204；B 的旧 Session 返回 HTTP 401。 |

### 验证边界与后续授权

浏览器/Vite 联调、生产 HTTPS/Nginx、公网部署仍未验证。本次人工验收不构成这些能力已验证或已部署的声明，也不授权 Git 提交、推送、合并或进入 T04；后续操作等待负责人另行决定。
