# 思弈阁 API 契约

## 当前 V1 契约与 V2 待设计的边界

**本文描述的是当前 V1 的 HTTP 契约，处于冻结状态。** 2026-11-17 负责人以 Q52 确认了产品 V2 的整体需求，规格见 [PRD V2](../product/PRD.md)。本节只说明边界，**不改动本文已实现的任何接口承诺**。

- **V1 契约冻结**：下文的路径、方法、请求/响应字段、状态码、错误码、幂等与版本语义、SSE 事件均为当前有效契约，不因产品 V2 重构而修改。17 个已实现操作的实现状态不变。
- **V2 已实现增量**：V2-03 私有自定义辩题扩展 `POST /api/v1/debates` 的 `custom_topic` 和会话快照的 `topic.id`。V2-04 批注契约见 §15，四轮练习的复盘和四维契约见 §16。
- **V2 后续接口**：训练赛阶段、争点卡、局部重练和 PDF 等能力的接口在各自切片确定。
- **`phase3-summary-v2` 不是产品 V2**：下文总结的 `schema_version = 2`（四维 0–100）属于 V1 基线下 Phase 3 的 P1 目标（T14-REPORT，未开始），与产品 V2 的 `v2-training-reference`（1–4 档）不是同一件事，两者不共享字段与取值，见 [ADR-027](../decisions/DECISIONS.md#adr-027-v2-需求基线与版本边界)。
- **V2-13 复盘为本地可运行切片，未验收**：§16 的复盘接口是错误边界复用 V1、字段独立设计的新契约，默认关闭（`V2_REVIEW_FAKE=1` + `AI_MODE=fake` + `APP_ENV=test`）。关闭时不注册生成能力、不访问新表，既有 19 个操作的请求/响应语义保持不变；迁移 `00016` 仅新增正向 SQL 文件，未执行，编号需在整合时复核。
- **V2 设计须沿用的既有约束**：所有权校验、`client_request_id` 幂等、`attempt_version`、租约、并发与额度、事务提交后才发 `done`、错误信封与越权 404 语义，均为 V1 已实现并要求 V2 继续遵守的约束；V2 不得为此另建第二套幂等。

产品 V2 的整体开发与验收待安排。V2-03 已按本地隔离 MySQL、Fake 联调和前端 Mock 的证据范围签收。

**T03 当前增量（2026 年 11 月 8 日）：四条认证接口已通过有条件独立审查与人工验收，原延期风险保留；T05 四接口无收费实现、累计验证与新会话独立复审 PASS，三关人工验收通过，无收费范围正式结项；真实模型链路仍待另行授权验证。** 当前 Cookie 名、配置及验证边界见下文和 [认证记录](../acceptance/T03_AUTH_ACCEPTANCE.md)。T01 业务字段、状态码、错误码和参数保持不变；历史“全部业务未实现”说明保留当时语境。

2026 年 11 月 6 日，T01 固定 Phase 1 的 8 个业务接口；11 月 7 日项目负责人确认契约设计通过人工验收，T01 已结项。仅代表接口契约设计完成，不代表业务代码已实现。业务实现按 [TASKS](../planning/TASKS.md) 分批交付。历史分页、完整三轮、显式重试与总结仍以 [架构草案](../architecture/ARCHITECTURE.md#phase-2-api-草案) 为设计参考。

2026 年 11 月 8 日（Asia/Shanghai）文档核对：请求中指定的 `backend/docs/api` 在当前工作区不存在，继续复用本文作为契约来源，机器规范见 [openapi.yaml](openapi.yaml)。项目负责人在本轮明确确认健康路径以当前 Gin 路由 `/api/health/live`、`/api/health/ready` 为准，并授权同步相关文档和代码。业务接口保持 T01 冻结设计；本次不表示接口已部署或 T02 已结项。

## 接口清单与实现状态

**2026-11-14 T14-STYLE 后端增量：** 创建支持自主选择 `ai_style`，策略 GET/POST 已按 A1 定稿契约实现并使用 Fake 与隔离 MySQL 验证，见 [专项记录](../acceptance/T14_STYLE_BACKEND_ACCEPTANCE.md)。2026-11-15 A3 已交付 SSE 后端和本地代理；完整浏览器 AC12/AC16 待 B1/B2 交付后联调，真实体验未执行。

以下按当前工作区源码核对。真实启动入口向 [NewBusinessRouter](../../backend/internal/platform/business.go) 注入认证、辩题、辩论 Handler，注册 2 个健康接口、4 个认证接口、1 个辩题接口和 12 个辩论/历史/总结/策略/批注/流式接口，共 19 个；V2-13 复盘另新增 2 个独立接口（见 §16），默认关闭。**当前有效健康契约为 `/api/health/live`、`/api/health/ready`**。历史 `/health/live`、`/health/ready` 未注册，返回 `404 not_found`，不提供兼容别名。未注入认证 Handler 的两参构造只保留健康测试用途。

| 方法 | 路径 | 用途 | 当前实现状态 | 成功状态（契约或 Handler） |
| --- | --- | --- | --- | --- |
| GET | `/api/health/live` | 存活检查 | 已实现 | 200 |
| GET | `/api/health/ready` | 就绪检查 | 已实现 | 200；依赖失败 503 |
| POST | `/api/v1/auth/register` | 注册 | 已实现，认证验收已通过 | 201 |
| POST | `/api/v1/auth/login` | 登录 | 已实现，认证验收已通过 | 200 |
| POST | `/api/v1/auth/logout` | 退出当前 Session | 已实现，认证验收已通过 | 204，无正文 |
| GET | `/api/v1/auth/me` | 查询当前用户 | 已实现，认证验收已通过 | 200 |
| GET | `/api/v1/topics` | 查询启用辩题 | 已实现，无收费范围复审与人工验收通过 | 200 |
| GET | `/api/v1/debates` | 查询本人历史会话分页 | 已实现（T11） | 200 |
| POST | `/api/v1/debates` | 创建会话（服务端固定 `standard_4`） | 已实现（T05/T14-STYLE） | 201 |
| POST | `/api/v1/debates/{id}/turns` | 提交轮次（上限由会话赛制决定）、幂等重放 | 已实现（T05/T09） | 201、200、202 |
| POST | `/api/v1/debates/{id}/turns/stream` | 流式提交轮次（SSE，复用同一认领内核） | 已实现（P0，后端专项已验证） | 200（`text/event-stream`） |
| GET | `/api/v1/debates/{id}/strategy` | 只读策略状态与已保存结果 | 已实现（T14-STYLE 后端） | 200 |
| POST | `/api/v1/debates/{id}/strategy` | 显式生成或版本绑定重试策略 | 已实现（T14-STYLE 后端） | 201、200、202 |
| POST | `/api/v1/debates/{id}/turns/{turn_id}/retry` | 显式重试失败轮次 | 已实现（T09） | 200、202 |
| POST | `/api/v1/debates/{id}/summary` | 生成或显式重试结构化总结 | 已实现（T10） | 201、200、202 |
| GET | `/api/v1/debates/{id}/summary` | 查询本人会话总结状态与结果 | 已实现（T10） | 200 |
| GET | `/api/v1/debates/{id}` | 查询本人会话 | 已实现，无收费范围复审与人工验收通过 | 200 |
| GET | `/api/v1/debates/{id}/annotations` | 只读本场原文批注批次 | 已实现（V2-04 切片，仅 Fake 与显式本地测试配置） | 200 |
| POST | `/api/v1/debates/{id}/turns/{turn_id}/annotations/retry` | 手动补做未启动或已失败的批注 | 已实现（V2-04 切片，仅 Fake 与显式本地测试配置） | 201、200、202 |

本文与 OpenAPI `paths` 记录 **19 个已实现操作**（其中 17 个为 V1 冻结契约，2 个为 V2-04 批注增量，后者仅在显式本地测试配置下启用）。T05 无收费范围独立复审 PASS、人工验收通过并正式结项；T09 轮次状态机、显式重试、租约恢复、T10 结构化总结与重试及 T11 本人历史分页接口已在后端实现；赛制驱动（`quick_3`/`standard_4`）已实现，默认四轮。真实模型验收仍待另行授权，未部署。首批契约与验收矩阵见 [Phase 3 A 整合 Spec](../planning/PHASE3_A_INTEGRATION_SPEC.md)。

**V2-13 复盘另有 2 个已在代码中实现、默认关闭的接口**（见 §16）；本轮**未**把它们登记进 OpenAPI `paths`，登记属于整合事项，避免与并行分支争写同一份机器规范。

本文与 OpenAPI 统一用 `{id}` 表示必填路径参数，对应 Gin 的 `:id`，只是文档表示法统一。没有给首批接口增加查询参数或分页。OpenAPI 的 `x-implementation-status` 区分 `implemented` 和 `planned`；`x-phase2-drafts` 只记录后续草案索引。

**Phase 3 产品体验目标（T15-DEMO-REAL，P0，尚未交付）：** 在本地浏览器经上述正常业务接口使用真实模型完成选题/立场/流派、策略、四轮 SSE、真实总结及历史恢复，按 PRD AC17 验收；复用同一认证、所有权、认领、版本、限额与受控错误，不新增免鉴权 Demo 路径或浏览器直连模型接口。维护者按独立明确的真实模型、体验窗口、次数/Token/费用授权启用，Fake 不代替真实验收；Demo 复用业务路径，不另设免鉴权演示入口；策略 GET/POST 已实现；SSE 后端已实现；浏览器解析与完整 AC12 待 B 联调。公网体验仍待 Phase 4。

## 存活检查

- 方法与路径：`GET /api/health/live`，不带业务版本 `/v1`。
- 输入：无请求体，无查询参数要求，无认证要求；只读，不调用数据库或模型。
- 输出：HTTP 200，`Content-Type: application/json; charset=utf-8`，JSON `{"status":"alive"}`，不套业务包装。
- 响应头：`X-Request-ID` 为服务器生成的关联标识，`Cache-Control: no-store`。
- 语义：仅表示 Go HTTP 进程可以处理请求，不代表数据库、模型或整个产品就绪。

## 就绪检查与 PB 错误

`GET /api/health/ready` 归 T02。数据库短超时连通且必要配置就绪时返回 `200` 与 `{"status":"ready"}`，否则为 `503` 与 `{"status":"not_ready"}`。路径不带业务版本 `/v1`，无认证，固定 JSON，无模型调用。

T02 当前已实现 [ready Handler](../../backend/internal/platform/health.go)，最多使用 3 秒请求 Context 调用数据库 Ping，响应包含 `X-Request-ID` 和 `Cache-Control: no-store`。缺失或非法启动配置会直接启动失败；配置有效但数据库不可达时，live 返回 200、ready 返回 503。`503 {"status":"not_ready"}` 是健康响应，不套业务错误信封。此前 [接入记录](../acceptance/T02_DATABASE_CONNECTION_ACCEPTANCE.md) 和 [dotenv 记录](../acceptance/T02_DOTENV_ACCEPTANCE.md) 保留各自日期的历史证据，其中旧健康路径不再作为当前调用地址。

未知路由返回 404，已知路径不支持的方法返回 405，意外 panic 返回受控 500。错误 JSON：

```json
{"error":{"code":"not_found","message":"接口不存在"},"request_id":"服务端生成的标识"}
```

错误码分别为 `not_found`、`method_not_allowed`、`internal_error`；不包含堆栈、请求正文或内部错误细节。T03 已将路由层错误统一设为 `Cache-Control: no-store`，并包含 `X-Request-ID`。405 适用于已注册路径的不支持方法，例如 `GET /api/v1/auth/login`；当前 `POST /health/live` 为 404。

## 本地请求链路

浏览器访问 `http://127.0.0.1:5173`，用相对路径 `/api/health/live` 请求 Vite；Vite 将 `/api/` 前缀原样代理至 `http://127.0.0.1:8080`，不去掉前缀，不使用 SPA HTML 作为 API 成功结果。不为历史 `/health/` 路径保留代理或后端兼容别名。

可直连 `http://127.0.0.1:8080/api/health/live` 或经 Vite 请求 `http://127.0.0.1:5173/api/health/live`。本轮同步前端默认地址、页面提示和相关测试，不修改健康 Handler 或业务逻辑。

前端检查 HTTP 状态、JSON Content-Type 和 `status=alive`，总超时 3 秒（包括读取响应体）。失败只显示受控中文提示，不自动重试；用户点击后重新检查。

## Phase 1 业务接口

业务请求使用相对路径 `/api/v1/...`。有请求体的接口使用 `Content-Type: application/json`，请求体上限 32 KiB。业务 JSON 成功响应包含 `data` 和 `request_id`，204 无正文。业务错误包含 `error.code`、`error.message`、`request_id`。响应头 `X-Request-ID` 与信封中的标识一致。业务响应使用 `Cache-Control: no-store`。错误提示供展示，客户端按状态码和 `error.code` 分支，代理自行产生的非契约响应按传输失败处理。

实体 ID 是正整数十进制字符串，范围为 MySQL `BIGINT UNSIGNED`。轮号和轮次的 `attempt_version` 是 JSON 正整数，`completed_rounds` 是非负整数。未开始的总结以 `summary_status.attempt_version: null` 表示。时间字段用 UTC RFC 3339。列表为空时返回 `[]`。

注册和登录可匿名调用，其余接口要求有效 Session Cookie。每个 POST 校验配置中的完整 Origin（协议、主机、端口），拒绝缺失、`null` 或不匹配的值。开发 Origin 为 `http://127.0.0.1:5173`，生产为配置的 HTTPS Origin。只有要求 Session 的 POST 才额外校验 `X-CSRF-Token`。注册和登录即使收到旧 Cookie，仍只做 Origin 校验作为 CSRF 防护。CSRF token 由登录或当前用户响应返回，客户端只保存在内存。Cookie 和口令细节见 [安全与资源控制](../architecture/ARCHITECTURE.md#安全与资源控制)。

### 公共参数与安全方案

| 位置 | 参数/头 | 要求与用途 |
| --- | --- | --- |
| 路径 | `id` | 发送轮次、显式重试、生成/查询总结、读取详情必填；会话 ID，类型见下表。非法值 400，不存在或非本人会话同为 404。 |
| 路径 | `turn_id` | 显式重试必填；轮次 ID，类型见下表。非法值 400，不存在或不属于当前会话同为 404。 |
| 查询 | `page`、`page_size` | 本人历史列表可选；`page` 默认 1，`page_size` 默认 20、最大 50，均为正整数十进制字符串，非法值 400。 |
| Header | `Content-Type` | 有 JSON 请求体时为 `application/json`；非法媒体类型按冻结契约为 400，不擅自改为 415。 |
| Header | `Origin` | 全部业务 POST 必须与配置的完整 Origin 一致；GET 不要求。浏览器同源请求自然携带，非浏览器调用也须显式提供合法值。 |
| Cookie | 配置的 Session Cookie | 除注册、登录外的业务接口必需；HTTP 开发为 `siyige_session_dev`，HTTPS 安全策略为 `__Host-siyige_session`。 |
| Header | `X-CSRF-Token` | 退出、创建会话、发送轮次、显式重试轮次、生成/重试总结必需，与 Session 同时校验；注册、登录不要求。 |
| 响应头 | `Set-Cookie` | 登录签发、退出清除；生产 `Secure; HttpOnly; SameSite=Lax; Path=/`，不设 Domain，有效期暂定 24 小时。开发可单独命名并关闭 Secure。 |
| 响应头 | `X-Request-ID` | 所有服务端契约响应的关联标识；JSON 信封中 `request_id` 与其一致，204 仅有响应头。 |
| 响应头 | `Retry-After` | 429 必须携带等待秒数。 |

OpenAPI 用 Cookie `apiKey` 表达 Session，用 Header `apiKey` 表达 CSRF；要求二者的 POST 放在同一个安全要求对象中，语义为同时满足。T03 已替换 Cookie 名占位符：机器规范对应当前本地服务器，`SessionCookie.name` 为 `siyige_session_dev`，`x-https-cookie-name` 记录 `__Host-siyige_session`；HTTPS 客户端须按该名称替换，不能沿用开发 Cookie。不添加 JWT、Bearer 或 Authorization Header。

`AUTH_ORIGIN` 默认为 `http://127.0.0.1:5173`，配置可规范化协议默认端口；请求中的 Origin 必须与规范形式精确匹配且只有一个头值。重复/合并 Origin 或 CSRF 头失败为 403；重复同名 Session Cookie 为 401。HTTP 配置仅回环 IP；HTTPS 可域名/IP，Cookie 自动开启 Secure。本机 dev/test 保持回环，T07 的 deploy 模式要求 HTTPS Origin 和固定内部数据库，隔离实跑及部署待另行授权。 [T03 记录](../acceptance/T03_AUTH_ACCEPTANCE.md) · [T07 部署准备](../../deploy/README.md)

### 请求字段

| 请求 Schema | 字段 | JSON 类型 | 必填 | 约束 |
| --- | --- | --- | --- | --- |
| `Credentials`（注册/登录复用） | `username` | string | 是 | trim 后转小写；归一化后 3–32 位 ASCII 字母、数字、下划线。限制作用于归一化值。 |
| `Credentials` | `password` | string | 是 | 原样校验，12–128 个 Unicode 码点、UTF-8 最多 512 字节。 |
| `CreateDebateRequest` | `topic_id` | string | 二选一 | 预设题 ID，正整数十进制字符串。与 `custom_topic` **恰有一个出现**。 |
| `CreateDebateRequest` | `custom_topic` | object | 二选一 | 私有自定义辩题，包含必填 `title` 和可选 `background`。与 `topic_id` 恰有一个出现。 |
| `CreateDebateRequest.custom_topic` | `title` | string | 是 | 1–200 个 Unicode 码点，拒绝纯空白（Unicode White_Space 与 U+FEFF），保存原文不裁剪。 |
| `CreateDebateRequest.custom_topic` | `background` | string | 否 | 0–1000 个 Unicode 码点，可省略或为空串，长度计算包含空白，保存原文。 |
| `CreateDebateRequest` | `user_stance` | string | 是 | `pro` 正方或 `con` 反方。 |
| `CreateDebateRequest` | `ai_style` | string | 否 | **Phase 3 计划增量**：精确枚举 logical/aggressive/empathic/data，省略默认 logical；null、空串、未知值、大小写不符或非字符串为 400 invalid_request，不写会话。创建后不可修改。 |
| `SubmitTurnRequest` | `round_no` | integer | 是 | 正整数；须满足 `round_no = completed_rounds + 1` 且不超过该会话 `mode` 的轮数上限（`quick_3` 为 3、`standard_4` 为 4）；越界或状态/轮序不允许时 409 `round_conflict`。 |
| `SubmitTurnRequest` | `content` | string | 是 | 1–2000 个 Unicode 码点，拒绝全空白，保存原文。 |
| `SubmitTurnRequest` | `client_request_id` | string（UUID） | 是 | 解析后统一为小写带连字符形式；会话内唯一。 |
| `RetryTurnRequest` | `expected_attempt_version` | integer | 是 | 正整数；必须匹配失败轮次当前的 `attempt_version`，缺失或非法返回 400。 |
| `SummaryRequest` | `retry` | boolean | 否 | 默认 `false`；为 `true` 时必填 `expected_attempt_version`，`not_started` 时传 `retry=true` 返回 400。 |
| `SummaryRequest` | `expected_attempt_version` | integer | 否 | `retry=true` 时必填正整数；必须匹配失败总结当前的 `attempt_version`，版本不匹配返回 409 `attempt_version_conflict`。 |

退出无请求体；当前用户、辩题、会话详情无请求体或查询参数。除下述 `error.details` 外，响应表中的字段均为对应对象必有字段；`null` 是显式值，不表示省略字段。

### 共享响应结构

| Schema | 字段与 JSON 类型 | 语义 |
| --- | --- | --- |
| `EntityId` | string | `1` 至 `18446744073709551615`，对应 `BIGINT UNSIGNED`；不输出 JSON 数值。 |
| 成功信封 | `data`: object；`request_id`: string | `data` 的具体结构见下一表；204 无正文。 |
| `User` | `id`: EntityId；`username`: string | 用户名为归一化小写值，不返回口令或哈希。 |
| `AuthData` | `user`: User；`csrf_token`: string | 登录及当前用户复用；Session 原值不在 JSON 中。 |
| `Topic` | `id`: EntityId；`title`: string；`description`: string | 辩题列表为当前启用题目；会话中的 topic 为创建时快照。 |
| `DebateTopic` | `id`: EntityId 或 null；`title`: string；`description`: string | **V2-03 起的会话快照**：预设题 `id` 为原字符串；私有自定义辩题 `id` 为 `null`。`title`/`description` 为该会话固定快照，缺失背景为空字符串。公共 `Topic` 类型仍要求字符串 ID。 |
| `Debate` | `id`: EntityId；`topic`: DebateTopic；`user_stance`、`ai_stance`: string；`mode`: string；`ai_style`: string；`total_rounds`: integer；`status`: string；`completed_rounds`: integer；`created_at`、`updated_at`: string | 立场为 `pro/con` 且相反；`mode` 取 `quick_3`/`standard_4`，`ai_style` 取 `logical`/`aggressive`/`empathic`/`data`；`total_rounds` 由 `mode` 推导（3 或 4），前端以此作为轮次上限；状态为 `active/completed`；创建时 active、完成轮数 0，达到该赛制轮数上限的完整 AI 回复落库后 status 置为 completed。时间为 UTC RFC 3339。 |
| `Turn` | `id`: EntityId；`round_no`: integer；`client_request_id`: string（UUID）；`status`: string；`attempt_version`: integer；`error_code`: string 或 null；`messages`: Message[] | 轮号和版本为正整数；状态 `generating/completed/failed`；错误枚举见详情节；按同轮 user、assistant 顺序返回消息。 |
| `Message` | `id`: EntityId；`role`: string；`content`: string；`created_at`: string | role 仅 `user/assistant`；内容为原文或正式 AI 回复；时间为 UTC RFC 3339。 |
| `SummaryStatus` | `status`: string；`attempt_version`: integer 或 null；可选 `error_code`: string、`schema_version`: integer、`content`: SummaryContent | 状态取 `not_started/generating/completed/failed`；未开始时 `attempt_version` 为 `null`，其余状态为正整数；失败时附 `error_code`，已完成时附 `schema_version`（`1` 或 `2`）与结构化 `content`（`schema_version=1` 为 `strengths`、`issues`、`suggestions`；`schema_version=2` 即 `phase3-summary-v2`，另含 `scores` 四维 0–100 参考值，属 T14-REPORT 目标设计、未实现）。客户端必须按 `schema_version` 分支渲染，不得把 `1` 判为格式错误。注意 `phase3-summary-v2` 不是产品 PRD V2，产品 V2 的四维 1–4 档尚无任何接口契约。 |
| 错误信封 | `error`: object；`request_id`: string | error 必有 `code`、`message`；可选 `details`，受控错误码见下文。 |
| `TurnFailureDetails` | 可选 `turn_id`: EntityId；`status`: string；`attempt_version`: integer；`query_url`: string | status 固定 `failed`；用于已确认持久化的失败轮次/总结或 `attempt_version_conflict`，提交结果不明时省略；轮次 `query_url` 为会话详情地址，总结 `query_url` 为 `/api/v1/debates/{id}/summary`。 |

| 成功响应 Schema | `data` 结构 | 适用接口 |
| --- | --- | --- |
| `UserResponse` | User | 注册 |
| `AuthResponse` | AuthData | 登录、当前用户 |
| `TopicsResponse` | `items`: Topic[] | 辩题列表，空列表为 `[]` |
| `DebateListResponse` | `items`: Debate[]；`total`: 非负 integer；`page`: 正整数；`page_size`: 正整数 | 本人历史会话分页，空列表 `items` 为 `[]` |
| `DebateResponse` | Debate | 创建会话 |
| `TurnResponse` | `turn`: Turn；`completed_rounds`: 非负 integer；`query_url`: string | 发送、重放或显式重试轮次；query_url 为 `/api/v1/debates/{id}` 的具体相对地址 |
| `StrategyResponse` | StrategyStatus | 显式生成、重放、重试或只读查询策略 |
| `SummaryResponse` | SummaryStatus | 生成、重放、显式重试或查询结构化总结 |
| `DebateDetailResponse` | `debate`: Debate；`turns`: Turn[]；`summary_status`: SummaryStatus | 本人会话详情；turns 按 round_no 升序 |

Schema 不推定数据库内部字段都是 HTTP 字段：例如 `user_id`、`password_hash`、`enabled`、`lease_expires_at` 和模型计费元数据不在这些响应中。旧接口未额外拒绝未知 JSON 字段；策略 POST 按 A1 定稿契约严格拒绝未知、重复和大小写不符的字段。ID 数值上界、UTF-8 字节上限、归一化、全空白校验、所有权、立场相反及状态流转仍须应用层验证；OpenAPI 的注释/扩展不等同于这些行为已实现。

### 各接口状态码

以下已实现响应遵循冻结契约，与 OpenAPI 一一对应。路由层通用 404/405/500 见健康错误说明。

| 接口 | 成功状态 | 失败状态与 `error.code` |
| --- | --- | --- |
| `POST /api/v1/auth/register` | 201 | 400 invalid_request；403 csrf_failed；409 username_conflict；413 request_too_large；429 rate_limited；500 internal_error；503 dependency_unavailable |
| `POST /api/v1/auth/login` | 200 | 400 invalid_request；401 invalid_credentials；403 csrf_failed；413 request_too_large；429 rate_limited；500 internal_error；503 dependency_unavailable |
| `POST /api/v1/auth/logout` | 204 | 401 unauthenticated；403 csrf_failed；413 request_too_large；500 internal_error；503 dependency_unavailable |
| `GET /api/v1/auth/me` | 200 | 401 unauthenticated；500 internal_error；503 dependency_unavailable |
| `GET /api/v1/topics` | 200 | 401 unauthenticated；500 internal_error；503 dependency_unavailable |
| `GET /api/v1/debates` | 200 | 400 invalid_request；401 unauthenticated；500 internal_error；503 dependency_unavailable |
| `POST /api/v1/debates` | 201 | 400 invalid_request；401 unauthenticated；403 csrf_failed；404 not_found；413 request_too_large；500 internal_error；503 dependency_unavailable |
| `POST /api/v1/debates/{id}/turns` | 200、201、202 | 400 invalid_request；401 unauthenticated；403 csrf_failed；404 not_found；409 round_conflict/idempotency_conflict/turn_failed；413 request_too_large；429 rate_limited/generation_busy/daily_quota_exhausted；500 internal_error；502 model_error；503 dependency_unavailable/model_unavailable；504 model_timeout |
| `POST /api/v1/debates/{id}/turns/{turn_id}/retry` | 200、202 | 400 invalid_request；401 unauthenticated；403 csrf_failed；404 not_found；409 round_conflict/attempt_version_conflict；413 request_too_large；429 rate_limited/generation_busy/daily_quota_exhausted；500 internal_error；502 model_error；503 dependency_unavailable/model_unavailable；504 model_timeout |
| `POST /api/v1/debates/{id}/summary` | 200、201、202 | 400 invalid_request；401 unauthenticated；403 csrf_failed；404 not_found；409 round_conflict/attempt_version_conflict；413 request_too_large；429 rate_limited/generation_busy/daily_quota_exhausted；500 internal_error；502 model_error；503 dependency_unavailable/model_unavailable；504 model_timeout |
| `GET /api/v1/debates/{id}/strategy` | 200 | 400 invalid_request；401 unauthenticated；404 not_found；500 internal_error；503 dependency_unavailable |
| `POST /api/v1/debates/{id}/strategy` | 200、201、202 | 400 invalid_request；401 unauthenticated；403 csrf_failed；404 not_found；409 round_conflict/attempt_version_conflict；413 request_too_large；429 rate_limited/generation_busy/daily_quota_exhausted；500 internal_error；502 model_error；503 dependency_unavailable/model_unavailable；504 model_timeout |
| `GET /api/v1/debates/{id}/summary` | 200 | 400 invalid_request；401 unauthenticated；404 not_found；500 internal_error；503 dependency_unavailable |
| `GET /api/v1/debates/{id}` | 200 | 400 invalid_request；401 unauthenticated；404 not_found；500 internal_error；503 dependency_unavailable |

### 1. POST /api/v1/auth/register

登录复用以下注册请求字段。

```json
{"username":"Alice","password":"example-password"}
```

用户名去除首尾空白后转小写，长度为 3–32 位，仅允许 ASCII 字母、数字和下划线。密码按原样校验，长度为 12–128 个 Unicode 码点且最多 512 字节。`users.username` 以归一化值唯一约束，`password_hash` 仅用于服务端认证。注册同一 IP 每小时最多 5 次，限流先于密码哈希。

`201` 返回新用户，随后由用户登录。

```json
{"data":{"id":"42","username":"alice"},"request_id":"req-register"}
```

特有失败为 `409 username_conflict`，通用输入、Origin、限流和存储错误见下表。

### 2. POST /api/v1/auth/login

请求同注册。按 IP、归一化用户名分别每分钟最多 10 次，限流先于密码校验。错误密码和不存在的用户名均返回 `401 invalid_credentials`。成功签发新的 Session Cookie 并持久化 `auth_sessions`，Session 原值只通过 Cookie 传输。

`200` 响应

```json
{"data":{"user":{"id":"42","username":"alice"},"csrf_token":"example-csrf"},"request_id":"req-login"}
```

### 3. POST /api/v1/auth/logout

无请求体。有效 Session、同源 Origin 和 CSRF 校验通过后，删除 `auth_sessions` 中当前 Session，并清除同名、同路径的 Cookie。成功为 `204`。Session 已失效为 `401 unauthenticated`，校验失败为 `403 csrf_failed`，删除失败为 `503 dependency_unavailable`。

### 4. GET /api/v1/auth/me

无请求参数。由有效 `auth_sessions` 关联 `users`，恢复登录状态及当前 Session 的 CSRF token。失效返回 `401 unauthenticated`，存储不可用返回 `503 dependency_unavailable`。

`200` 响应

```json
{"data":{"user":{"id":"42","username":"alice"},"csrf_token":"example-csrf"},"request_id":"req-me"}
```

### 5. GET /api/v1/topics

无请求参数。T05 读取 T02 写入真实 MySQL 的 `topics`，仅返回 `enabled=true` 的预设题，按 `id` 升序。T06 最小面板与 T08 完整选题页使用同一接口。未登录返回 `401 unauthenticated`，存储不可用返回 `503 dependency_unavailable`。

`200` 响应（截取单题示例）

```json
{"data":{"items":[{"id":"7","title":"大学课程是否应采用开放式考核","description":"讨论学习效果与评价公平"}]},"request_id":"req-topics"}
```

### 6. GET /api/v1/debates

需认证。支持可选查询参数 `page`（默认 1，正整数）与 `page_size`（默认 20，最大 50，正整数）；非法查询参数返回 `400 invalid_request`。仅查询当前登录用户的会话，按 `(created_at DESC, id DESC)` 排序，返回 `items`、`total`、`page`、`page_size`。列表为空时 `items` 为 `[]`。

`200` 响应

```json
{
  "data": {
    "items": [
      {
        "id": "123",
        "topic": {"id":"7","title":"大学课程是否应采用开放式考核","description":"讨论学习效果与评价公平"},
        "user_stance": "pro",
        "ai_stance": "con",
        "status": "completed",
        "completed_rounds": 3,
        "created_at": "2026-11-06T08:00:00Z",
        "updated_at": "2026-11-06T08:05:00Z"
      }
    ],
    "total": 1,
    "page": 1,
    "page_size": 20
  },
  "request_id": "req-list"
}
```

### 7. POST /api/v1/debates

需认证、Origin 和 CSRF 校验。`user_stance` 取 `pro` 或 `con`。服务端从 Session 取得 `user_id`，在事务中检查辩题可用并写入 `debates`，保存辩题标题及描述快照，反转得到 `ai_stance`。创建会话的模型调用次数为零。非法 ID 或立场为 `400 invalid_request`，不存在或禁用的辩题为 `404 not_found`。每次成功创建独立会话，客户端遇到传输故障时不自动重发创建请求。

**T14-STYLE 已实现契约：** 用户在创建前选择 `logical`（逻辑型）、`aggressive`（进攻型）、`empathic`（共情型）或 `data`（数据型）。可选请求字段 `ai_style` 省略时默认 `logical`，显式 null、空串、非字符串或非精确枚举值返回 `400 invalid_request`，校验失败不创建会话。Service 校验后由 Repository 在创建事务中写入 `debates.ai_style`，响应返回所保存值；详情/历史及后续 Prompt、策略卡统一读取会话值。创建后不可修改，无流派更新接口。`mode` 仍不接受客户端选择，传入不生效，统一 `standard_4`；流派不改变轮数、立场、状态机、配额或所有权规则。历史会话不回填、不切换。策略卡调用不合并到创建事务或创建请求，具体生成接口、配额和失败响应在实施前另行细化，不在本次新增已实现路径。

**V2-03 私有自定义辩题（已实现）：** 请求严格二选一 —— `topic_id` 与 `custom_topic` **恰有一个出现**，以字段是否出现判定；显式 `null` 视为该字段出现，因此 `topic_id` 与 `null` 的 `custom_topic` 一起提交同样被拒。两者同时出现、都缺失、任一显式 `null`、自定义对象含未知/重复字段、`title` 缺失或非字符串、`background` 非字符串，均返回 `400 invalid_request` 且不写任何会话。

顶层字段的严格程度按分支区分：**自定义分支**拒绝任何顶层未知字段与重复字段（新入口，避免混入赛制、归属或轮次）；**预设分支**沿用本接口既有行为——顶层未知与重复字段被忽略，重复键取最后一次赋值。无论哪条分支，`mode` 固定 `standard_4`、`user_id` 来自 Session。

自定义题不进入公共题库：服务端跳过 `topics` 查询，把原文与登录用户 ID 一次写入会话快照，`debates.topic_id` 写 `NULL`。`GET /topics` 仍只返回启用的预设题；另一账号查不到该会话（跨账号详情沿用 `404 not_found`）。题目与背景长度按解码后的 Unicode 码点计算（组合字符计多个、补充平面字符计一个），标题 1–200、背景 0–1000；服务端只做判空与长度校验，持久化保留首尾空白、换行与原字符。创建不调用模型、不触发策略卡或审题，也没有编辑接口。客户端遇到传输故障不得自动重发创建请求。

预设请求示例（旧示例继续有效）：

```json
{"topic_id":"7","user_stance":"pro","ai_style":"aggressive"}
```

自定义请求示例：

```json
{"custom_topic":{"title":"是否应把远程办公设为默认？","background":"讨论协作成本与人才半径"},"user_stance":"pro"}
```

自定义成功时 `topic` 为会话内快照，`id` 为 `null`：

```json
{
  "data": {
    "id": "124",
    "topic": {"id":null,"title":"是否应把远程办公设为默认？","description":"讨论协作成本与人才半径"},
    "user_stance": "pro",
    "ai_stance": "con",
    "mode": "standard_4",
    "ai_style": "logical",
    "total_rounds": 4,
    "status": "active",
    "completed_rounds": 0,
    "created_at": "2026-11-18T08:00:00Z",
    "updated_at": "2026-11-18T08:00:00Z"
  },
  "request_id": "req-create-custom"
}
```

后续对辩（普通、SSE、重试）、策略卡与总结都从同一份会话快照取题。预设题 Prompt 保持旧结构（`debate-v1`/`summary-v1`/`strategy-v2`）；自定义题使用独立版本标识（`debate-custom-v1`/`summary-custom-v1`/`strategy-custom-v1`），system 规则段不含用户题目原文，题目以 JSON 数据段放在第一条 user 消息中，JSON 转义阻断伪造角色与规则段。既有计费准入、Prompt 窗口预检、生成认领与 SSE 事件契约不变。

预设题 `201` 响应沿用以下形状（`topic.id` 仍为原字符串）：

```json
{
  "data": {
    "id": "123",
    "topic": {"id":"7","title":"大学课程是否应采用开放式考核","description":"讨论学习效果与评价公平"},
    "user_stance": "pro",
    "ai_stance": "con",
    "mode": "standard_4",
    "ai_style": "aggressive",
    "total_rounds": 4,
    "status": "active",
    "completed_rounds": 0,
    "created_at": "2026-11-06T08:00:00Z",
    "updated_at": "2026-11-06T08:00:00Z"
  },
  "request_id": "req-create"
}
```

### 8. POST /api/v1/debates/{id}/turns

需认证、Origin、CSRF 及会话所有权。`round_no = completed_rounds + 1`，上限从会话 mode 取得（standard_4 为 4，quick_3 为 3），越界或状态/轮序不允许时返回 `409 round_conflict`；最后一轮完整 AI 回复落库后将 `debates.status` 置为 `completed`。`content` 为 1–2000 个 Unicode 码点，拒绝全空白，保存原文。`client_request_id` 为 UUID，解析后统一为小写带连字符形式，在会话内唯一。

```json
{"round_no":1,"content":"开放式考核可以评估实际应用能力。","client_request_id":"550e8400-e29b-41d4-a716-446655440000"}
```

`201` 新轮成功，`200` 重放已完成结果，均返回已持久化的轮次和当前会话完成轮数。

```json
{
  "data": {
    "turn": {
      "id": "456",
      "round_no": 1,
      "client_request_id": "550e8400-e29b-41d4-a716-446655440000",
      "status": "completed",
      "attempt_version": 1,
      "error_code": null,
      "messages": [
        {"id":"901","role":"user","content":"开放式考核可以评估实际应用能力。","created_at":"2026-11-06T08:01:00Z"},
        {"id":"902","role":"assistant","content":"统一考核仍有助于比较基础知识掌握程度。","created_at":"2026-11-06T08:01:05Z"}
      ]
    },
    "completed_rounds": 1,
    "query_url": "/api/v1/debates/123"
  },
  "request_id": "req-turn"
}
```

`202` 表示同键、同轮、同原文的请求查到轮次仍为 `generating`。

```json
{
  "data": {
    "turn": {
      "id": "456",
      "round_no": 1,
      "client_request_id": "550e8400-e29b-41d4-a716-446655440000",
      "status": "generating",
      "attempt_version": 1,
      "error_code": null,
      "messages": [
        {"id":"901","role":"user","content":"开放式考核可以评估实际应用能力。","created_at":"2026-11-06T08:01:00Z"}
      ]
    },
    "completed_rounds": 0,
    "query_url": "/api/v1/debates/123"
  },
  "request_id": "req-pending"
}
```

同键附带不同内容或轮号返回 `409 idempotency_conflict`。同键同轮同原文的已失败重放返回 `409 turn_failed`。使用新键抢占已有轮次返回 `409 round_conflict`。重放不重新调用模型或消耗生成额度。新尝试在短事务内认领轮次、写 user 消息和 `attempt_version=1`，事务外调用模型，最终以状态及版本校验写回。模型失败保留原文及已确认的失败状态，完成轮数不变。

`504 model_timeout` 的示例中，失败状态已落库。

```json
{
  "error": {
    "code": "model_timeout",
    "message": "回复生成超时，请查看会话状态",
    "details": {"turn_id":"456","status":"failed","attempt_version":1,"query_url":"/api/v1/debates/123"}
  },
  "request_id": "req-timeout"
}
```

`502`、`504`、`503` 或 `409 turn_failed` 确认已有失败轮次时，返回同形状的 `error.details`。数据库提交结果不明时，返回 `503 dependency_unavailable`，省略 `details`。客户端已知会话地址，收到传输错误或生成状态不明时先查详情。

### 9. POST /api/v1/debates/{id}/turns/{turn_id}/retry

需认证、Origin、CSRF 及会话与轮次归属校验。请求体必填正整数 `expected_attempt_version`（缺失或非法返回 `400 invalid_request`），不接收新发言内容。仅当 `turn_id` 属于该会话及当前用户、`status=failed` 且当前 `attempt_version` 与 `expected_attempt_version` 匹配时，在短事务内非阻塞获取并发槽并原子更新为 `generating`、`attempt_version+1`，复用已保存的 user 原文调用模型。写回时同样校验 `status=generating` 与 `attempt_version`，防止迟到的旧尝试结果覆盖新尝试。

```json
{"expected_attempt_version":1}
```

`200` 重试生成完成（或已完成结果重放），返回持久化后的轮次与当前完成轮数：

```json
{
  "data": {
    "turn": {
      "id": "456",
      "round_no": 1,
      "client_request_id": "550e8400-e29b-41d4-a716-446655440000",
      "status": "completed",
      "attempt_version": 2,
      "error_code": null,
      "messages": [
        {"id":"901","role":"user","content":"开放式考核可以评估实际应用能力。","created_at":"2026-11-06T08:01:00Z"},
        {"id":"902","role":"assistant","content":"统一考核仍有助于比较基础知识掌握程度。","created_at":"2026-11-06T08:02:05Z"}
      ]
    },
    "completed_rounds": 1,
    "query_url": "/api/v1/debates/123"
  },
  "request_id": "req-retry"
}
```

查到轮次仍为 `generating` 时返回 `202`。当轮次为 `failed` 但 `attempt_version` 已变化（如一次重试再次失败后重发旧版本请求）时，返回 `409 attempt_version_conflict` 并附当前 `status`、`attempt_version`、`query_url`，不再调用模型或消耗生成额度：

```json
{
  "error": {
    "code": "attempt_version_conflict",
    "message": "请求未完成，请查看会话状态后再操作",
    "details": {"turn_id":"456","status":"failed","attempt_version":2,"query_url":"/api/v1/debates/123"}
  },
  "request_id": "req-version-conflict"
}
```

### 10. GET /api/v1/debates/{id}

无查询参数。按 Session 用户和路径 ID 同时匹配。非法 ID 为 `400 invalid_request`，不存在和非本人会话同为 `404 not_found`。读取 `debates` 的快照、按 `round_no` 排序的 `debate_turns` 和已保存 `messages`，同轮先 user 后 assistant。`Turn.error_code` 为已持久化的失败原因，取 `model_error`、`model_timeout`、`model_unavailable`、`dependency_unavailable` 或 `generation_cancelled`，无错误时为 `null`。`generation_cancelled` 只用于请求取消后的已保存状态，客户端通过详情读取。`summary_status` 返回真实总结状态与 `attempt_version`（未开始时为 `not_started` 和 `null`，已完成时包含 `schema_version` 与 `content`，已失败时包含 `error_code`）。

`200` 响应（以同一会话的超时失败分支为例）

```json
{
  "data": {
    "debate": {
      "id": "123",
      "topic": {"id":"7","title":"大学课程是否应采用开放式考核","description":"讨论学习效果与评价公平"},
      "user_stance": "pro",
      "ai_stance": "con",
      "mode": "standard_4",
      "ai_style": "logical",
      "total_rounds": 4,
      "status": "active",
      "completed_rounds": 0,
      "created_at": "2026-11-06T08:00:00Z",
      "updated_at": "2026-11-06T08:00:00Z"
    },
    "turns": [
      {
        "id": "456",
        "round_no": 1,
        "client_request_id": "550e8400-e29b-41d4-a716-446655440000",
        "status": "failed",
        "attempt_version": 1,
        "error_code": "model_timeout",
        "messages": [
          {"id":"901","role":"user","content":"开放式考核可以评估实际应用能力。","created_at":"2026-11-06T08:01:00Z"}
        ]
      }
    ],
    "summary_status": {"status":"not_started","attempt_version":null}
  },
  "request_id": "req-detail"
}
```

### 11. POST /api/v1/debates/{id}/summary

需认证、Origin、CSRF 及会话所有权校验。仅允许 `status=completed`（已完成该会话 mode 的全部轮次）的会话生成总结，未完成会话拒绝生成并返回 `409 round_conflict`。共享既有用户分钟防刷（6 次/分钟）、每日生成额度（20 次/日）与模型并发槽（单用户 1、全局 2）。

请求体中 `retry` 默认为 `false`；首次生成（`not_started`）时若传 `retry=true` 返回 `400 invalid_request`。当已有总结处于 `failed` 状态时，`retry=false` 不重调模型，直接返回 `200` 与当前失败状态；必须显式传 `retry=true` 且提供匹配的 `expected_attempt_version` 才允许认领重试并将 `attempt_version` 加 1。若 `failed` 总结的 `attempt_version` 已变化，返回 `409 attempt_version_conflict` 并附当前状态、版本与查询地址 `/api/v1/debates/{id}/summary`。

```json
{"retry":false}
```

`201` 首次生成成功（已完成重放或显式重试成功返回 `200`）：

```json
{
  "data": {
    "status": "completed",
    "attempt_version": 1,
    "schema_version": 1,
    "content": {
      "strengths": [
        {"round_no":1,"point":"首轮立场表达明确","reason":"结合实际应用能力展开核心论点"}
      ],
      "issues": [
        {"round_no":2,"point":"二轮反驳链条可进一步展开","reason":"对反方评价标准前提的拆解还可以更具体"}
      ],
      "suggestions": [
        {"round_no":3,"action":"三轮总结时先回应对方核心交锋点再收束主线","example":"先指出对方标准的前提局限，再重申我方核心判断标准"}
      ]
    }
  },
  "request_id": "req-summary"
}
```

`202` 表示查到总结仍处于 `generating` 状态：

```json
{
  "data": {
    "status": "generating",
    "attempt_version": 1
  },
  "request_id": "req-summary-pending"
}
```

### 12. GET /api/v1/debates/{id}/summary

需认证及会话所有权校验。只读查询当前会话的总结状态与可选结构化内容，不触发模型调用；尚未请求总结时返回 `not_started` 和 `attempt_version: null`。

`200` 响应（已完成总结示例同上；未开始示例）：

```json
{
  "data": {
    "status": "not_started",
    "attempt_version": null
  },
  "request_id": "req-summary-get"
}
```

### 13. POST /api/v1/debates/{id}/turns/stream（P0）

流式提交轮次。**请求体与 `POST /api/v1/debates/{id}/turns` 完全一致**，响应改为 `text/event-stream`。

- 请求头要求：`Accept: text/event-stream`；Cookie Session、`X-CSRF-Token`、正确 Origin 与普通写请求相同。
- 成功：`200` + `Content-Type: text/event-stream`、`Cache-Control: no-cache`、`X-Accel-Buffering: no`。
- **复用同一认领内核**：`client_request_id` 幂等、`attempt_version` 版本检查、90 秒租约、并发配额与普通发送共用。禁止为流式另建幂等键。
- 鉴权、限流、请求校验、并发配额**在发送任何事件之前**完成；这些阶段失败仍返回普通 HTTP JSON（信封同其余接口）。
- `generating` 期间的重复请求按约定返回 `202` JSON，不进入事件流。
- completed 同键重放为 200 SSE `start` → `done`，复用已存结果，不调用模型或发送 delta；start 的 request_id 对应本次 HTTP。202 完整信封与普通发送一致；事件前错误包括 400/401/403/404/409/413/429/500/503，保持 JSON。可复制的完整四事件、202、普通错误及恢复 fixture 见 [跨成员样例](../planning/PHASE3_A_INTEGRATION_SPEC.md#t15-core-冻结契约与可复制样例)。

事件序列固定四个，名称不得改为 `chunk` 等别名：

| 事件 | 时机 | `data` 载荷 |
| --- | --- | --- |
| `start` | 认领成功后立即 | `{"turn_id":"…","request_id":"…"}`；轮次定位与请求关联的恢复锚点，不携带 attempt_version；版本从详情/状态查询取得 |
| `delta` | 模型每个增量 | `{"text":"增量片段"}`；临时展示内容，非正式消息 |
| `done` | 最终事务**提交成功后** | `{"turn":{…},"completed_rounds":3,"status":"active"}`；turn 沿用正式 Turn 结构，其余为提交后的会话完成轮数与状态快照 |
| `error` | 流内失败 | `{"error":{"code":"…","message":"…"},"request_id":"…"}`；发送后关闭连接 |

```
event: start
data: {"turn_id":"42","request_id":"req-stream-example"}

event: delta
data: {"text":"对方认为"}

event: done
data: {"turn":{...},"completed_rounds":3,"status":"active"}
```

约束：只有最终事务提交成功才能发 `done`；数据库提交失败只能发 `error`，并按状态查询恢复。不做事件重放或断点续传；断线后先 `GET` 会话判定真实状态。每 15 秒发送注释行 `: keepalive`。代理层必须关闭缓冲并放宽读取超时，详见 [ARCHITECTURE](../architecture/ARCHITECTURE.md#eino-流式双轨管道)。

**断线恢复与显式重试：**

1. 收到 start 后保存 turn_id/request_id；turn_id 定位轮次，request_id 关联本次 HTTP 请求和排错信息，不是 client_request_id，也不是重试版本。丢弃未确认的 delta 展示，以数据库正式结果为准。
2. 断线、流内 error、未收到 done 或刷新后，先 `GET /api/v1/debates/{id}`，读取该 Turn 最新 status/attempt_version；总结查询只提供总结版本，不能替代轮次版本。若 start 尚未收到，按发送前保留的 client_request_id 在详情 turns 中定位，不盲目重发。暂未查到时继续有限查询，仍无法确认则提示稍后查询，不自动生成或更换键。
3. completed 读取正式消息；generating 按既有每 2 秒、最多 120 秒有限轮询，超时提示稍后查询；failed 才显示用户手动重试入口。
4. 用户明确重试时，使用查询到的失败版本调用已有 `POST /api/v1/debates/{id}/turns/{turn_id}/retry`，请求体为 `{"expected_attempt_version":N}`，复用已保存原文，不重新 POST /turns/stream 或创建新幂等键。重试沿用现有 JSON 响应和查询流程，本次不新增流式重试端点。
5. 202 继续查询；409 attempt_version_conflict 仅刷新状态，不自动换新版本重发。再次失败后，只有用户看过最新失败版本并再次明确重试才允许新尝试。SSE 事件或本地缓存不得作为最新版本事实来源。



## 错误码与客户端恢复

| HTTP | `error.code` | 适用情况 |
| --- | --- | --- |
| 400 | `invalid_request` | 非法参数、ID、JSON、媒体类型或字段限制。 |
| 401 | `unauthenticated`、`invalid_credentials` | 前者为 Session 缺失或失效，后者仅用于登录凭据错误。 |
| 403 | `csrf_failed` | POST 的 Origin 或适用的 CSRF 校验失败。 |
| 404 | `not_found` | 路由不存在、辩题不可用、会话不存在或不归当前用户。 |
| 405 | `method_not_allowed` | 路径不支持该方法。 |
| 409 | `username_conflict`、`round_conflict`、`idempotency_conflict`、`turn_failed`、`attempt_version_conflict` | 分别为用户名占用、状态或轮序冲突、幂等内容冲突、已失败轮次重放、显式重试版本不匹配。 |
| 413 | `request_too_large` | 请求体超过 32 KiB。 |
| 429 | `rate_limited`、`generation_busy`、`daily_quota_exhausted` | 分钟防刷超限、模型并发槽满、每用户每日生成尝试用尽；均带 `Retry-After` 秒数。三类业务码不同，客户端可据此区分恢复动作（按秒重试、等待当前生成、查看历史并等待服务端开启下一个 UTC 日窗口）。 |
| 500 | `internal_error` | 未分类内部错误。 |
| 502 | `model_error` | 模型调用失败、输出无效或拒答。 |
| 503 | `dependency_unavailable`、`model_unavailable` | 数据库不可用或提交结果不明、供应商容量或额度不可用。 |
| 504 | `model_timeout` | 模型生成超时。 |

会话不存在和越权返回相同的 `404 not_found` 结构与提示。错误正文和日志只使用受控消息及关联标识，敏感凭据、SQL、堆栈、用户原文和供应商原始响应留在对外边界之外。

客户端普通请求使用短超时，生成请求使用 70 秒超时。丢失响应、`202` 或生成结果不明时，先 GET 详情或总结状态，生成中每 2 秒查一次，最多轮询 120 秒。完成后展示已落库结果，失败则显示原文和受控提示，仍生成中则提示稍后再查。前端不自动发起生成或换键重试。`attempt_version` 从 1 开始并参与写回校验；显式重试须携带 `expected_attempt_version`。服务启动及每 30 秒扫描 `lease_expires_at` 已过期的 `generating` 轮次与总结（租约 90 秒），以 `status='generating'` 和 `attempt_version` 为条件更新为 `failed`，确保崩溃后不永久卡在 `generating`。

## T05/T09/T10/T11 运行与验证状态

八个辩论与辩题业务接口已装配到真实 main。HTTP 入口对轮次生成、轮次重试与总结生成请求使用 60 秒预算、模型 45 秒、数据库短事务 3 秒；普通查询接口保留 5 秒短预算。并发“全局 2”仅指单个 Go 进程内所有用户合计，不提供跨实例协调。分钟防刷 6 次也适用于重放，每日 20 次只计新认领（含显式重试与总结生成成功认领），失败也计入；内存计数重启会丢失。

AI_MODE 默认 disabled，显式 fake 仅用于无收费联调；真实 deepseek 模式目前装配 T07 单次 trialGenerator，仅 Linux/内部中继可用，尚不具备完整 Demo 所需总结与流式能力；最小改造方案见 [首批 Spec](../planning/PHASE3_A_INTEGRATION_SPEC.md#本地完整-demo-与-5-场评测的最小方案)，真实调用仍需独立授权。技术证据见 [T05 记录](../acceptance/T05_FIRST_TURN_ACCEPTANCE.md)，Fake 成功不代表真实模型已验收。

## Phase 2 接口晋升说明

原 [架构草案](../architecture/ARCHITECTURE.md#phase-2-api-草案) 中的 4 个 Phase 2 接口（`GET /api/v1/debates`、`POST /api/v1/debates/{id}/turns/{turn_id}/retry`、`POST /api/v1/debates/{id}/summary`、`GET /api/v1/debates/{id}/summary`）均已落地并晋升至本文与 OpenAPI `paths`。SSE 已是 Phase 3 必要 P0，后端与本地代理已实现并验证，完整浏览器 AC12 仍待 B 联调，见 [专项记录](../acceptance/T15_CORE_BACKEND_ACCEPTANCE.md)；自主流派选择和策略 GET/POST 已按 T14-STYLE 实现。

## 核对差异与待确认问题

1. **健康路径差异已裁定**：2026 年 11 月 8 日负责人明确以 `/api/health/live`、`/api/health/ready` 为准；本轮同步前端、测试和当前运行文档。历史验收及学习笔记中的旧路径保留历史语境，不作为新契约，也不提供兼容别名。
2. **Session Cookie 名已在 T03 具体化**：HTTP 开发为 `siyige_session_dev`，HTTPS 策略为 `__Host-siyige_session`；OpenAPI 占位符已替换，安全属性与 T01 一致。
3. **当前契约与实现边界**：Phase 2 的赛制驱动轮次、显式重试、总结和分页已实现；T14-STYLE 自主流派选择与策略卡后端已实现；SSE 和总结 V2 等增量尚未实现。策略卡契约已在下节及 [T14 Spec](../planning/T14_STYLE_SPEC.md) 定稿；P1 谬误存储/API 仍待其子项细化。

## 14. GET/POST /api/v1/debates/{id}/strategy（T14-STYLE）

策略是会话下的单份资源，帮助用户自身立场破题，结合已保存的对手 ai_style；不会成为正式消息或计轮。创建会话不调用模型；仅用户点击「生成赛前策略」才 POST。页面进入、刷新、历史及重登只 GET，未开始不自动生成。完整行为和验收见 [T14-STYLE Spec](../planning/T14_STYLE_SPEC.md)。

GET 要求 Session/所有权；POST 额外要求精确 Origin/CSRF。不存在和越权统一 404。首次生成与失败重试仅在会话尚无任何 turn 时认领，否则 409 round_conflict；已完成重放和在途查询不受此新认领条件限制。

POST `{}` 或 `{"retry":false}` 首次认领 version=1，成功 201；用户显式重试传 `{"retry":true,"expected_attempt_version":N}`，仅 failed 且匹配版本时 version+1，成功 200。retry 只允许布尔，true 必须有正整数版本，false 不允许版本，未知字段拒绝 400；not_started + retry=true 也为 400。completed 重放 200，generating 202，failed + retry=false 返回 200 失败状态，均不重新生成；failed 重试版本不符为 409 attempt_version_conflict，只查询不自动换版本。

```json
{"data":{"status":"not_started","attempt_version":null},"request_id":"req-strategy-get"}
```

generating 仅含 status/attempt_version；failed 增加受控 error_code（model_error/model_timeout/model_unavailable/dependency_unavailable/generation_cancelled）。成功结果：

```json
{"data":{"status":"completed","attempt_version":1,"schema_version":1,"prompt_version":"strategy-v1","ai_style":"aggressive","content":{"thesis":"开放式考核应兼顾应用能力与评价公平。","arguments":["用统一评分标准评估真实任务成果。"],"risks":["预先回应评分一致性与代做风险。"]}},"request_id":"req-strategy-complete"}
```

内容固定三键，每段 1–300 Unicode 码点非空白文本，arguments/risks 各 1–3 项，完整 JSON ≤8 KiB，拒绝额外字段/无效 UTF-8；前端转义显示。首次 Prompt 现为 `strategy-v2`（`strategy-v1` 为历史版本，仅出现在已保存记录中），输出 schema_version=1，历史读取保存版本。成功不可重生成。一次新认领最多一次调用，禁止自动重试/JSON 修复，失败不存成功内容。

与轮次/总结共用 Gate：6/分钟、20/UTC 日、单用户并发 1、单进程全局 2；失败消耗认领额度，重放不消耗每日额度但受分钟限制。60 秒请求、45 秒模型、3 秒短事务、90 秒租约及版本条件写回；过期恢复同现有扫描。GET 不消费模型额度。标准 400/401/403/404/413/429/500 及模型 502/503/504 信封沿用；已确认失败或版本冲突附 `details:{status,attempt_version,query_url}`，query_url 为本策略 GET，无 turn_id；提交不明省略 details。策略接口不改变现有 Turn 错误结构。`AI_MODE=fake` 支持完整策略链路；disabled 和现有真实试运行包装器都受控不可用，后者未暴露策略能力，不能绕过专用费用账本。OpenAI 策略适配器仅用合成 Transport 验证，本轮没有真实调用或新的收费装配。

## 15. GET /api/v1/debates/{id}/annotations 与 POST .../turns/{turn_id}/annotations/retry（V2-04）

原文批注是 V2-04 的独立分析能力：在正式轮次提交成功之后，对本次双方新增原文生成 **0–3 条**带原句锚点的训练反馈。它与轮次、总结、策略各自拥有独立的状态、版本与表，**不改变** `debates.completed_rounds`、`debates.status`、轮次状态机、SSE 事件契约或任何既有响应字段。设计与锚点契约见 [V2-04 设计](../planning/V2_04_ANNOTATION_ANCHOR_DESIGN.md)。

### 15.1 锚点契约

- `message_id`：服务端已知、属于当前会话的正式消息 ID，JSON 为字符串。
- `start_cp` / `end_cp`：从 0 开始的 **Unicode 码点**半开区间，按原始消息文本计数。
- `quote`：对应的连续原文，**逐字符精确相等**；禁止 trim、归一化、大小写折叠和标点替换。
- `resolved`：为 `false` 时表示引用在该消息中出现多次且没有可逐字复核的偏移。唯一的精确引用可在读取时重算坐标，已保存的批注不改写。此时 `start_cp`/`end_cp` 必须为 `null`，前端只显示“原文待核对”，**不得**按坐标高亮。

服务端在落库前按原文重新解析坐标，不采信生成器给出的位置；同一句在原文出现两处（含重叠，例如“哈哈哈”中的“哈哈”）视为歧义。跨消息、未来轮、空引用、全空白、非法 UTF-8 与越界偏移一律拒绝，且不回显引用。

### 15.2 读取

`GET /api/v1/debates/{id}/annotations` 要求 Session 与所有权；不存在与越权统一 `404 not_found`。**无模型调用、不写库。**

- 只返回「已开启本切片时提交的合格轮次」，按 `round_no` 升序；旧会话没有合格轮次，返回 `{"items":[]}`。
- 批次 `status` 为 `not_started` / `generating` / `completed` / `failed`；未认领时 `attempt_version` 为 `0`。
- `failed` 才带 `error_code`；`completed` 的 `items` 可以是空数组——没有批注不等于没有问题。

```json
{"data":{"items":[{"turn_id":"31","round_no":1,"status":"completed","attempt_version":1,"items":[{"category":"strength","label":"论证有效","reason":"该句把主张落在可检验的判断标准上，使后续交锋有明确落点。","improvement":"可以再补一句该标准为何适用于本场辩题。","anchor":{"message_id":"21","quote":"标准应当可检验","start_cp":0,"end_cp":7,"resolved":true},"turn_no":1,"role":"user"}]}]},"request_id":"req-annotation-list"}
```

旧会话：`{"data":{"items":[]},"request_id":"req-annotation-empty"}`。

### 15.3 手动补做

`POST /api/v1/debates/{id}/turns/{turn_id}/annotations/retry` 要求 Session、精确 Origin 与 CSRF，共用现有防刷前置。正文**只有**一个字段：

```json
{"expected_attempt_version":0}
```

- `0` 表示未认领的首次补做；正数表示期望的当前版本。
- 未启动 → 认领并生成，成功 `201`；生成中 → `202` 与当前状态，**不排队**；已成功 → `200` 与已存结果。
- 版本不符 → `409 attempt_version_conflict`，附 `details:{turn_id,status,attempt_version,query_url}`；`query_url` 是本场批注 GET。
- 非合格轮（未开启切片时的旧轮、失败轮）→ `409 round_conflict`，不启动生成、不写批次。
- **手动补做最多一次**：首次分析为 v1，唯一一次补做为 v2；此后失败批次保持失败，只允许查询。
- 本切片未启用时该入口受控 `400 invalid_request`，不写任何批次行。

### 15.4 生成与边界

- 分析在正式轮次事务**之外**执行，有固定 10 秒期限；不继承浏览器取消，进程退出与重启都**不补跑**。
- 有**一个非阻塞分析槽**：忙时不排队、不重放，保留资格供手动补做；它**不占用**正式发言与总结的 Gate 配额。
- 输入只取当前会话触发轮及以前已提交的正式消息。慢批注重试时后轮可能已提交，因此按 `round_no` 过滤，绝不把尚未发生的发言当作依据。
- 成功结果整体原子保存（包括空数组）；失败只保存受控错误码，不写入供应商原始响应。
- 租约 90 秒，过期扫描与轮次/总结共用同一恢复循环；过期批次标记为失败后可由下一次限定范围的手动认领恢复。

### 15.5 当前实现范围

当前进程**只有 Fake 生成实现**，且由显式本地配置开启：`V2_ANNOTATION_FAKE=1` + `AI_MODE=fake` + `APP_ENV=test`，同时要求生成器实现批注能力。默认关闭时：`Complete` 不设置资格标志、不启动分析、不写批次，读取对旧会话返回空列表。`dev`/`demo`/`deploy` 一律拒绝开启。Fake 结果用于验证行为，**不验证输出质量**；真实供应商接入（模型、费用、总并发、前台优先与限额）属于另一项独立授权任务，现有 Demo 的真实模型额度不扩充。

前端读取与展示：场中标记渲染在**正式消息流原文**上，侧栏（桌面右栏 / 移动底部面板）只放解释；“显示批注”开关同时控制正文标记和批注面板。关闭后仍生成并保存。坐标回读使用 `Array.from` 按码点切分，并用浏览器原生 `Intl.Segmenter` 排除切中字素簇的位置；缺少该 API 时只有整条原文这类安全端点可标，内部区间降级为待核对，避免切开 ZWJ 序列。

## 16. GET /api/v1/debates/{id}/review 与 POST /api/v1/debates/{id}/review（V2-13 + V2-10 + V2-11）

赛后复盘是 **V2 的独立生成能力**，与轮次、总结、策略、批注各自拥有独立的状态、版本与表。它不改变 `debates.status`、`debates.completed_rounds`、轮次状态机、SSE 事件契约、总结 `schema_version=1/2` 的既有语义或任何既有响应字段。

- **与旧四维彻底分离**：`phase3-summary-v2`（`schema_version=2`，`argumentation/rebuttal/logic/presentation` 各 0–100）属 V1 基线的 P1 目标，本接口**不读取、不输出、不换算**这套字段；本契约的四维是 `argument_structure / responsiveness / evidence_use / concept_scope`，取值 1–4 或 `null`（观察不足），Schema 版本独立为 `review` 的 `schema_version=1`。
- **比赛完成与复盘状态独立**：复盘失败只写受控错误码，不改会话完成状态，不要求重新辩论。

### 16.1 读取

`GET /api/v1/debates/{id}/review` 只要求 Session 与所有权；不存在与越权统一 `404 not_found`。**无模型调用、不写库。**

- 状态：`not_started`（未开始，仅投影）、`generating`、`completed`、`failed`。
- `attempt_version`：未开始时为 `null`，其余为 ≥1 的整数。
- `available`：**可选**，仅当服务端未开启复盘切片时显式返回 `false`，且此时 `status` 恒为 `not_started`。客户端收到 `false` 必须隐藏复盘界面并禁止发起生成；省略该字段等价于“可用”，旧响应与旧 Mock 的 `not_started` 形状不变。功能启用时不输出该字段。
- `failed` 才带 `error_code`（`model_error`/`model_timeout`/`model_unavailable`/`dependency_unavailable`/`generation_cancelled`/`content_invalid`）。
- `completed` 才带 `schema_version`、`prompt_version` 与 `content`；已保存内容不可解析时读取投影为 `failed` + `content_invalid`，不在读取期改写数据库。

功能关闭时的读取形状（仍做会话归属校验，不存在与越权为 `404`，且**不读取 `debate_reviews`**）：

```json
{"data":{"status":"not_started","attempt_version":null,"available":false},"request_id":"req-review-get"}
```

```json
{"data":{"status":"completed","attempt_version":1,"schema_version":1,"prompt_version":"review-v1","content":{"dimensions":[{"key":"argument_structure","label":"论证结构","level":3,"level_label":"较稳","basis":"主要论证有理由支撑。","improvement":"补一句关键前提。","insufficient_reason":null,"quote":{"message_id":"21","quote":"标准应当可检验","start_cp":0,"end_cp":7,"resolved":true}},{"key":"responsiveness","label":"回应针对性","level":null,"level_label":null,"basis":"本场可观察机会有限。","improvement":"","insufficient_reason":"本场只有一轮完整交锋。","quote":null}],"key_issues":[{"text":"主要争点在于评价标准是否可检验。","quote":{"message_id":"21","quote":"标准应当可检验","start_cp":0,"end_cp":7,"resolved":true}}],"strengths":[{"text":"把主张落在可检验标准上。","quote":null}],"improvements":[{"text":"先回应标准质疑再收束主线。","quote":null}]}},"request_id":"req-review-get"}
```

维度契约：

| 字段 | 约束 |
| --- | --- |
| `dimensions` | 恰好 4 项，顺序固定为 `argument_structure`、`responsiveness`、`evidence_use`、`concept_scope`；`label` 由服务端按固定映射派生，不采信模型文本 |
| `level` | `1`/`2`/`3`/`4` 或 `null`（观察不足）；**0、5、字符串、浮点都非法**；`null` 不是 0 分、也不降到 1 档 |
| `level_label` | 有档级时必须是 `起步`/`发展中`/`较稳`/`充分`；观察不足时必须为 `null` |
| `insufficient_reason` | 观察不足必填（说明为什么观察不足）；有档级时必须为 `null` |
| `quote` | 有档级必须有引用且可核对；观察不足必须为 `null` |
| `basis` | 每维必须非空 |
| `improvement` | 有档级必须非空；观察不足允许为空 |

`quote.resolved`：`true` 表示在**唯一一条**本场正式消息中逐字定位成功，`start_cp`/`end_cp` 为 Unicode 码点半开区间；`false` 表示引用确实存在但跨消息歧义，此时坐标必须为 `null`，前端只显示“原文待核对”，**不按坐标高亮**。服务端只采信本场权威原文：外场引用、模型改写、无法逐字核对的引用**不得赋档**，该维降级为“观察不足”并写明原因（AC-V2-22“无依据不赋档”）。

### 16.2 显式生成与重试

`POST /api/v1/debates/{id}/review` 要求 Session、精确 Origin 与 CSRF，共用现有防刷前置。正文只有两种合法形状：

```json
{"retry":false}
```
```json
{"retry":true,"expected_attempt_version":1}
```

- `{}` 等价于 `{"retry":false}`（与策略入口一致）；`retry` 与 `expected_attempt_version` 必须**同时出现或同时缺失**。
- 未开始 → 首次生成，成功 `201`；生成中 → `202` 与当前状态；已成功 → **幂等 `200`** 与已保存内容，不重新生成、不覆盖结果（与总结/策略/批注的“成功批次重放”一致）。
- 失败且带正确当前版本 → 重新生成，成功 `200`，尝试版本 +1；版本不符 → `409 attempt_version_conflict` + `details:{status,attempt_version,query_url}`；失败但未带重试形状 → `200` 与当前失败状态（只查询，不自动重试）。
- 未完成结辩 → `409 round_conflict`，不生成完整复盘、不写复盘行（AC-V2-26）。
- 本切片未启用时写入口受控 `400 invalid_request`，不写任何复盘行、不调用模型。
- **没有任何自动触发**：不自动生成、不自动无限重试、不后台补跑；只有用户显式点击。

### 16.3 失败、重试与持久化边界

- 首版采用**整份复盘原子写入**：`completed` 时一次性保存 `content_json`；失败只保存受控错误码与 NULL 内容，不存半成品。因此失败重试是**重生成整份复盘**，不是“补失败段落”。若将来改为分段保存，必须显式记录已落库段落并只补失败部分，不得伪造局部恢复。
- 复盘表 `debate_reviews` 与 `debate_summaries`、`turn_annotations`、`debate_turns` 分开：复盘失败/重试**从不写入或清空**已成功的总结、批注与人工编辑内容。
- 租约 90 秒；过期 `generating` 复盘在**功能启用时**才扫描并标记为失败（`model_timeout`），使刷新后能恢复出可重试的独立失败状态。功能关闭时不访问新表，旧部署与旧原文/旧总结读取不受影响。
- 与轮次/总结共用 Gate（分钟/日额度、单用户并发 1、单进程全局 2）；单次调用有 45 秒模型期限、60 秒请求预算、3 秒短事务。

### 16.4 当前实现范围（待整合）

- 只有 Fake 生成实现，并由显式本地配置开启：`V2_REVIEW_FAKE=1` + `AI_MODE=fake` + `APP_ENV=test`，且生成器实现复盘能力。`dev`/`demo`/`deploy` 一律拒绝开启。Fake 用于验证结构与恢复，**不验证输出质量**。
- 关闭时写入口仍为受控 `400 invalid_request`；读入口返回 `not_started` + `available:false`，前端据此隐藏复盘界面且不暴露生成入口。
- 迁移 `00016_create_debate_reviews.sql` 为**新增正向 SQL 文件**，本工作树**未执行**；并行迁移编号需在整合时重新核对（README 记录下一候选号）。
- 前端：完赛后展示复盘面板；失败独立展示并可手动重试；四维卡片始终可读；雷达只用 1–4 档，观察不足轴留空。原文定位通过工作台消息 `data-message-id` 定位，图例按钮键盘可达。局部重练、争点卡与 PDF 入口按“待整合”并列显示，不接死链、不伪造能力。收到 `available:false` 时整个复盘区隐藏。

## T01 文档任务的历史验证记录

2026 年 11 月 8 日完成技术自查，待项目负责人最终验收；不替代完整 T02、T03 或 T05 的业务验收。

| 检查 | 实际结果 |
| --- | --- |
| YAML 语法及 OpenAPI 规范 | 无重复 YAML 键；`openapi-spec-validator 0.9.0` 验证 OpenAPI 3.1.1 通过。 |
| 引用及 Schema | 200 个本地 `$ref` 全部可解析，28 个共享 Schema 通过 JSON Schema 结构检查。 |
| 示例与字段 | 原有 13 个 Markdown JSON 示例全部保留且通过对应 Schema；21 个 OpenAPI 请求、响应及参数示例通过类型、格式和已定义字段检查。 |
| Markdown/OpenAPI 一致性 | 10 个方法/路径完全对应；8 个业务接口的成功/失败 HTTP 状态及错误码逐项一致。 |
| 安全及实现边界 | 路径参数、Origin、Session/CSRF 的同时满足关系、204 无正文、2 个 implemented / 8 个 planned 和 4 个 Phase 2 草案索引通过核对。 |
| 当前路由与调用方 | OpenAPI 已实现路径与 Gin `/api` 路由组一致；前端默认请求和页面提示同步为 `/api/health/live`。 |
| 文档链接 | 84 个本地链接及锚点检查通过。 |
| 后端回归 | `go test ./...`、`go vet ./...`、`go build ./...` 通过；`gofmt -l cmd internal tests` 无输出。默认 MySQL 集成测试跳过，不计为真实数据库验证。 |
| 前端回归 | `npm test` 的 10 个测试全部通过；`npm run typecheck`、`npm run build` 通过。 |
| 变更范围 | 与开始时未提交工作区比对，本次仅变更下列 15 个交付文件；未改 Handler、Gin 路由实现、后端启动实现、依赖锁文件或历史验收记录。`git diff --check` 通过。 |

本次文件范围：

- API 交付：`docs/api/API.md`、`docs/api/openapi.yaml`。
- 路径说明与决定：`README.md`、`AGENTS.md`、`docs/architecture/ARCHITECTURE.md`、`docs/decisions/DECISIONS.md`、`docs/planning/PLAN.md`、`docs/planning/TASKS.md`。
- 相关调用和测试：`frontend/src/api/health.ts`、`frontend/src/api/health.test.ts`、`frontend/src/App.vue`、`backend/internal/platform/router_test.go`、`backend/internal/platform/health_test.go`、`backend/cmd/server/main_test.go`、`backend/tests/integration/database_test.go`。

验证器仅安装在 Git 忽略的 `.cache/api-docs/python-deps`，没有加入 Go/npm 产品依赖。该工具是持续维护的开源 OpenAPI 验证 CLI，采用 Apache-2.0；替代方案是使用官方 OpenAPI Schema 配合 JSON Schema 验证器。本次选择和用法依据其 [官方文档](https://openapi-spec-validator.readthedocs.io/en/latest/)。临时生成、检查脚本和开始时的文件哈希保存在 `.cache/api-docs/`，不作为仓库交付文件。

从仓库根目录复核规范的实际命令：

```powershell
$env:PYTHONPATH = Join-Path (Get-Location) '.cache/api-docs/python-deps'
python -m openapi_spec_validator docs/api/openapi.yaml
```

路径修正遵循测试先行：新增前端默认请求用例在修正前因 HTTP 404 失败，修正后全套通过；旧后端路径用例在同步前也观测到 404。本机受限沙箱无法连接临时回环 HTTP 服务，最初前端测试因此出现网络失败；在允许回环访问的执行环境重跑通过，未把沙箱失败计为通过。

本次未显式启用真实 MySQL 集成测试，未执行浏览器/Vite 端到端冒烟、真实模型调用或部署；没有 Git 提交、推送或任务结项操作。后续认证客户端联调前需确认并替换 Cookie 名占位符；进入其他业务任务仍按各自授权和前置验收执行。
