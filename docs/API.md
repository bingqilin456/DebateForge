# 思弈阁 API 契约

2026 年 11 月 6 日，T01 固定 Phase 1 的 8 个业务接口；11 月 7 日项目负责人确认契约设计通过人工验收，T01 已结项。仅代表接口契约设计完成，不代表业务代码已实现。业务实现按 [TASKS](TASKS.md) 分批交付。历史分页、完整三轮、显式重试与总结仍以 [架构草案](ARCHITECTURE.md#phase-2-api-草案) 为设计参考。

2026 年 11 月 8 日（Asia/Shanghai）文档核对：请求中指定的 `backend/docs/api` 在当前工作区不存在，继续复用本文作为契约来源，机器规范见 [openapi.yaml](openapi.yaml)。项目负责人在本轮明确确认健康路径以当前 Gin 路由 `/api/health/live`、`/api/health/ready` 为准，并授权同步相关文档和代码。业务接口保持 T01 冻结设计；本次不表示接口已部署或 T02 已结项。

## 接口清单与实现状态

以下按当前工作区源码核对，而非沿用历史验收结果。当前 [NewRouter](../backend/internal/platform/router.go) 通过 `/api` 路由组注册 2 个健康接口，8 个业务接口均未实现。**当前有效健康契约为 `/api/health/live`、`/api/health/ready`**。历史 `/health/live`、`/health/ready` 已由本轮负责人裁定替代，当前未注册，返回 `404 not_found`，不提供兼容别名。

| 方法 | 路径 | 用途 | 当前实现状态 | 成功状态（契约或 Handler） |
| --- | --- | --- | --- | --- |
| GET | `/api/health/live` | 存活检查 | 已实现 | 200 |
| GET | `/api/health/ready` | 就绪检查 | 已实现 | 200；依赖失败 503 |
| POST | `/api/v1/auth/register` | 注册 | T03 未实现 | 201 |
| POST | `/api/v1/auth/login` | 登录 | T03 未实现 | 200 |
| POST | `/api/v1/auth/logout` | 退出当前 Session | T03 未实现 | 204，无正文 |
| GET | `/api/v1/auth/me` | 查询当前用户 | T03 未实现 | 200 |
| GET | `/api/v1/topics` | 查询启用辩题 | T05 未实现 | 200 |
| POST | `/api/v1/debates` | 创建会话 | T05 未实现 | 201 |
| POST | `/api/v1/debates/{id}/turns` | 提交首轮、幂等重放 | T05 未实现 | 201、200、202 |
| GET | `/api/v1/debates/{id}` | 查询本人会话 | T05 未实现 | 200 |

本文与 OpenAPI `paths` 均为 **10 个接口（2 个已实现健康接口 + 8 个尚未实现的业务契约）**。所有业务节中的请求、响应、安全和限额均为冻结的目标契约，当前访问这些业务路径实际返回路由层 404。

本文与 OpenAPI 统一用 `{id}` 表示必填路径参数，对应 Gin 的 `:id`，只是文档表示法统一。没有给首批接口增加查询参数或分页。OpenAPI 的 `x-implementation-status` 区分 `implemented` 和 `planned`；`x-phase2-drafts` 只记录后续草案索引。

## 存活检查

- 方法与路径：`GET /api/health/live`，不带业务版本 `/v1`。
- 输入：无请求体，无查询参数要求，无认证要求；只读，不调用数据库或模型。
- 输出：HTTP 200，`Content-Type: application/json; charset=utf-8`，JSON `{"status":"alive"}`，不套业务包装。
- 响应头：`X-Request-ID` 为服务器生成的关联标识，`Cache-Control: no-store`。
- 语义：仅表示 Go HTTP 进程可以处理请求，不代表数据库、模型或整个产品就绪。

## 就绪检查与 PB 错误

`GET /api/health/ready` 归 T02。数据库短超时连通且必要配置就绪时返回 `200` 与 `{"status":"ready"}`，否则为 `503` 与 `{"status":"not_ready"}`。路径不带业务版本 `/v1`，无认证，固定 JSON，无模型调用。

T02 当前已实现 [ready Handler](../backend/internal/platform/health.go)，最多使用 3 秒请求 Context 调用数据库 Ping，响应包含 `X-Request-ID` 和 `Cache-Control: no-store`。缺失或非法启动配置会直接启动失败；配置有效但数据库不可达时，live 返回 200、ready 返回 503。`503 {"status":"not_ready"}` 是健康响应，不套业务错误信封。此前 [接入记录](T02_DATABASE_CONNECTION_ACCEPTANCE.md) 和 [dotenv 记录](T02_DOTENV_ACCEPTANCE.md) 保留各自日期的历史证据，其中旧健康路径不再作为当前调用地址。

未知路由返回 404，已知路径不支持的方法返回 405，意外 panic 返回受控 500。错误 JSON：

```json
{"error":{"code":"not_found","message":"接口不存在"},"request_id":"服务端生成的标识"}
```

错误码分别为 `not_found`、`method_not_allowed`、`internal_error`；不包含堆栈、请求正文或内部错误细节。路由层错误包含 `X-Request-ID`，当前未统一设置 `Cache-Control: no-store`；不能把健康 Handler 的缓存头保证套用到这些错误。405 只适用于已注册路径的不支持方法，例如 `POST /api/health/live`；当前 `POST /health/live` 为 404。当前没有业务接口，不借此声称认证、权限或限流已实现。

## 本地请求链路

浏览器访问 `http://127.0.0.1:5173`，用相对路径 `/api/health/live` 请求 Vite；Vite 将 `/api/` 前缀原样代理至 `http://127.0.0.1:8080`，不去掉前缀，不使用 SPA HTML 作为 API 成功结果。不为历史 `/health/` 路径保留代理或后端兼容别名。

可直连 `http://127.0.0.1:8080/api/health/live` 或经 Vite 请求 `http://127.0.0.1:5173/api/health/live`。本轮同步前端默认地址、页面提示和相关测试，不修改健康 Handler 或业务逻辑。

前端检查 HTTP 状态、JSON Content-Type 和 `status=alive`，总超时 3 秒（包括读取响应体）。失败只显示受控中文提示，不自动重试；用户点击后重新检查。

## Phase 1 业务接口

业务请求使用相对路径 `/api/v1/...`。有请求体的接口使用 `Content-Type: application/json`，请求体上限 32 KiB。业务 JSON 成功响应包含 `data` 和 `request_id`，204 无正文。业务错误包含 `error.code`、`error.message`、`request_id`。响应头 `X-Request-ID` 与信封中的标识一致。业务响应使用 `Cache-Control: no-store`。错误提示供展示，客户端按状态码和 `error.code` 分支，代理自行产生的非契约响应按传输失败处理。

实体 ID 是正整数十进制字符串，范围为 MySQL `BIGINT UNSIGNED`。轮号和轮次的 `attempt_version` 是 JSON 正整数，`completed_rounds` 是非负整数。未开始的总结以 `summary_status.attempt_version: null` 表示。时间字段用 UTC RFC 3339。列表为空时返回 `[]`。

注册和登录可匿名调用，其余接口要求有效 Session Cookie。每个 POST 校验配置中的完整 Origin（协议、主机、端口），拒绝缺失、`null` 或不匹配的值。开发 Origin 为 `http://127.0.0.1:5173`，生产为配置的 HTTPS Origin。只有要求 Session 的 POST 才额外校验 `X-CSRF-Token`。注册和登录即使收到旧 Cookie，仍只做 Origin 校验作为 CSRF 防护。CSRF token 由登录或当前用户响应返回，客户端只保存在内存。Cookie 和口令细节见 [安全与资源控制](ARCHITECTURE.md#安全与资源控制)。

### 公共参数与安全方案

| 位置 | 参数/头 | 要求与用途 |
| --- | --- | --- |
| 路径 | `id` | 发送轮次、读取详情必填；会话 ID，类型见下表。非法值 400，不存在或非本人会话同为 404。 |
| Header | `Content-Type` | 有 JSON 请求体时为 `application/json`；非法媒体类型按冻结契约为 400，不擅自改为 415。 |
| Header | `Origin` | 全部业务 POST 必须与配置的完整 Origin 一致；GET 不要求。浏览器同源请求自然携带，非浏览器调用也须显式提供合法值。 |
| Cookie | 配置的 Session Cookie | 除注册、登录外的业务接口必需；当前未实现。具体 Cookie 名尚未冻结。 |
| Header | `X-CSRF-Token` | 退出、创建会话、发送轮次必需，与 Session 同时校验；注册、登录不要求。 |
| 响应头 | `Set-Cookie` | 登录签发、退出清除；生产 `Secure; HttpOnly; SameSite=Lax; Path=/`，不设 Domain，有效期暂定 24 小时。开发可单独命名并关闭 Secure。 |
| 响应头 | `X-Request-ID` | 所有服务端契约响应的关联标识；JSON 信封中 `request_id` 与其一致，204 仅有响应头。 |
| 响应头 | `Retry-After` | 429 必须携带等待秒数。 |

OpenAPI 用 Cookie `apiKey` 表达 Session，用 Header `apiKey` 表达 CSRF；要求二者的 POST 放在同一个安全要求对象中，语义为同时满足。**`SessionCookie.name: SESSION_COOKIE_NAME_TBD` 是显式文档占位符，不是已冻结的 Cookie 名或实现配置。** 由于 [OpenAPI 3.1.1 安全方案](https://spec.openapis.org/oas/v3.1.1.html#security-scheme-object) 要求 Cookie 方案填写 `name`，先标记 `x-name-unresolved: true`，待 T03 确认后替换；确认前不能直接据此生成可用的认证客户端。不添加 JWT、Bearer 或 Authorization Header。

### 请求字段

| 请求 Schema | 字段 | JSON 类型 | 必填 | 约束 |
| --- | --- | --- | --- | --- |
| `Credentials`（注册/登录复用） | `username` | string | 是 | trim 后转小写；归一化后 3–32 位 ASCII 字母、数字、下划线。限制作用于归一化值。 |
| `Credentials` | `password` | string | 是 | 原样校验，12–128 个 Unicode 码点、UTF-8 最多 512 字节。 |
| `CreateDebateRequest` | `topic_id` | string | 是 | 辩题 ID，正整数十进制字符串。 |
| `CreateDebateRequest` | `user_stance` | string | 是 | `pro` 正方或 `con` 反方。 |
| `SubmitTurnRequest` | `round_no` | integer | 是 | 正整数；T05 只接受首轮，状态或轮序不允许时 409。 |
| `SubmitTurnRequest` | `content` | string | 是 | 1–2000 个 Unicode 码点，拒绝全空白，保存原文。 |
| `SubmitTurnRequest` | `client_request_id` | string（UUID） | 是 | 解析后统一为小写带连字符形式；会话内唯一。 |

退出无请求体；当前用户、辩题、会话详情无请求体或查询参数。除下述 `error.details` 外，响应表中的字段均为对应对象必有字段；`null` 是显式值，不表示省略字段。

### 共享响应结构

| Schema | 字段与 JSON 类型 | 语义 |
| --- | --- | --- |
| `EntityId` | string | `1` 至 `18446744073709551615`，对应 `BIGINT UNSIGNED`；不输出 JSON 数值。 |
| 成功信封 | `data`: object；`request_id`: string | `data` 的具体结构见下一表；204 无正文。 |
| `User` | `id`: EntityId；`username`: string | 用户名为归一化小写值，不返回口令或哈希。 |
| `AuthData` | `user`: User；`csrf_token`: string | 登录及当前用户复用；Session 原值不在 JSON 中。 |
| `Topic` | `id`: EntityId；`title`: string；`description`: string | 辩题列表为当前启用题目；会话中的 topic 为创建时快照。 |
| `Debate` | `id`: EntityId；`topic`: Topic；`user_stance`、`ai_stance`: string；`status`: string；`completed_rounds`: integer；`created_at`、`updated_at`: string | 立场为 `pro/con` 且相反；状态为 `active/completed`；创建时 active、完成轮数 0；完整三轮由 T09 接入。时间为 UTC RFC 3339。 |
| `Turn` | `id`: EntityId；`round_no`: integer；`client_request_id`: string（UUID）；`status`: string；`attempt_version`: integer；`error_code`: string 或 null；`messages`: Message[] | 轮号和版本为正整数；状态 `generating/completed/failed`；错误枚举见详情节；按同轮 user、assistant 顺序返回消息。 |
| `Message` | `id`: EntityId；`role`: string；`content`: string；`created_at`: string | role 仅 `user/assistant`；内容为原文或正式 AI 回复；时间为 UTC RFC 3339。 |
| `SummaryStatus` | `status`: string；`attempt_version`: null | Phase 1 固定 `not_started` 和 null，后续总结归 T10。 |
| 错误信封 | `error`: object；`request_id`: string | error 必有 `code`、`message`；可选 `details`，受控错误码见下文。 |
| `TurnFailureDetails` | `turn_id`: EntityId；`status`: string；`attempt_version`: integer；`query_url`: string | status 固定 `failed`；仅已确认持久化的失败轮次附此对象，提交结果不明时省略；query_url 为相对会话详情地址。 |

| 成功响应 Schema | `data` 结构 | 适用接口 |
| --- | --- | --- |
| `UserResponse` | User | 注册 |
| `AuthResponse` | AuthData | 登录、当前用户 |
| `TopicsResponse` | `items`: Topic[] | 辩题列表，空列表为 `[]` |
| `DebateResponse` | Debate | 创建会话 |
| `TurnResponse` | `turn`: Turn；`completed_rounds`: 非负 integer；`query_url`: string | 发送或重放首轮；query_url 为 `/api/v1/debates/{id}` 的具体相对地址 |
| `DebateDetailResponse` | `debate`: Debate；`turns`: Turn[]；`summary_status`: SummaryStatus | 本人会话详情；turns 按 round_no 升序 |

Schema 不推定数据库内部字段都是 HTTP 字段：例如 `user_id`、`password_hash`、`enabled`、`lease_expires_at` 和模型计费元数据不在这些响应中。冻结契约没有裁定未知 JSON 字段的拒绝策略，OpenAPI 不额外设置 `additionalProperties: false`。ID 数值上界、UTF-8 字节上限、归一化、全空白校验、所有权、立场相反及状态流转仍须应用层验证；OpenAPI 的注释/扩展不等同于这些行为已实现。

### 各接口状态码

以下为冻结的目标响应，与 OpenAPI 一一对应。路由层通用 404/405/500 见健康错误说明；当前未注册的业务路径实际只会进入 404 分支。

| 接口 | 成功状态 | 失败状态与 `error.code` |
| --- | --- | --- |
| `POST /api/v1/auth/register` | 201 | 400 invalid_request；403 csrf_failed；409 username_conflict；413 request_too_large；429 rate_limited；500 internal_error；503 dependency_unavailable |
| `POST /api/v1/auth/login` | 200 | 400 invalid_request；401 invalid_credentials；403 csrf_failed；413 request_too_large；429 rate_limited；500 internal_error；503 dependency_unavailable |
| `POST /api/v1/auth/logout` | 204 | 401 unauthenticated；403 csrf_failed；413 request_too_large；500 internal_error；503 dependency_unavailable |
| `GET /api/v1/auth/me` | 200 | 401 unauthenticated；500 internal_error；503 dependency_unavailable |
| `GET /api/v1/topics` | 200 | 401 unauthenticated；500 internal_error；503 dependency_unavailable |
| `POST /api/v1/debates` | 201 | 400 invalid_request；401 unauthenticated；403 csrf_failed；404 not_found；413 request_too_large；500 internal_error；503 dependency_unavailable |
| `POST /api/v1/debates/{id}/turns` | 200、201、202 | 400 invalid_request；401 unauthenticated；403 csrf_failed；404 not_found；409 round_conflict/idempotency_conflict/turn_failed；413 request_too_large；429 rate_limited/generation_busy；500 internal_error；502 model_error；503 dependency_unavailable/model_unavailable；504 model_timeout |
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

### 6. POST /api/v1/debates

需认证、Origin 和 CSRF 校验。`user_stance` 取 `pro` 或 `con`。服务端从 Session 取得 `user_id`，在事务中检查辩题可用并写入 `debates`，保存辩题标题及描述快照，反转得到 `ai_stance`。创建会话的模型调用次数为零。非法 ID 或立场为 `400 invalid_request`，不存在或禁用的辩题为 `404 not_found`。每次成功创建独立会话，客户端遇到传输故障时不自动重发创建请求。

```json
{"topic_id":"7","user_stance":"pro"}
```

`201` 响应

```json
{
  "data": {
    "id": "123",
    "topic": {"id":"7","title":"大学课程是否应采用开放式考核","description":"讨论学习效果与评价公平"},
    "user_stance": "pro",
    "ai_stance": "con",
    "status": "active",
    "completed_rounds": 0,
    "created_at": "2026-11-06T08:00:00Z",
    "updated_at": "2026-11-06T08:00:00Z"
  },
  "request_id": "req-create"
}
```

### 7. POST /api/v1/debates/{id}/turns

需认证、Origin、CSRF 及会话所有权。T05 接受第 1 轮。`round_no` 是正整数，状态或轮序不允许时返回 `409 round_conflict`。`content` 为 1–2000 个 Unicode 码点，拒绝全空白，保存原文。`client_request_id` 为 UUID，解析后统一为小写带连字符形式，在会话内唯一。

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

### 8. GET /api/v1/debates/{id}

无查询参数。按 Session 用户和路径 ID 同时匹配。非法 ID 为 `400 invalid_request`，不存在和非本人会话同为 `404 not_found`。读取 `debates` 的快照、按 `round_no` 排序的 `debate_turns` 和已保存 `messages`，同轮先 user 后 assistant。`Turn.error_code` 为已持久化的失败原因，首轮取 `model_error`、`model_timeout`、`model_unavailable`、`dependency_unavailable` 或 `generation_cancelled`，无错误时为 `null`。`generation_cancelled` 只用于请求取消后的已保存状态，客户端通过详情读取。Phase 1 的 `summary_status` 为未开始，后续总结状态由 T10 接入。

`200` 响应（以同一会话的超时失败分支为例）

```json
{
  "data": {
    "debate": {
      "id": "123",
      "topic": {"id":"7","title":"大学课程是否应采用开放式考核","description":"讨论学习效果与评价公平"},
      "user_stance": "pro",
      "ai_stance": "con",
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

### 错误码与客户端恢复

| HTTP | `error.code` | 适用情况 |
| --- | --- | --- |
| 400 | `invalid_request` | 非法参数、ID、JSON、媒体类型或字段限制。 |
| 401 | `unauthenticated`、`invalid_credentials` | 前者为 Session 缺失或失效，后者仅用于登录凭据错误。 |
| 403 | `csrf_failed` | POST 的 Origin 或适用的 CSRF 校验失败。 |
| 404 | `not_found` | 路由不存在、辩题不可用、会话不存在或不归当前用户。 |
| 405 | `method_not_allowed` | 路径不支持该方法。 |
| 409 | `username_conflict`、`round_conflict`、`idempotency_conflict`、`turn_failed` | 分别为用户名占用、状态或轮序冲突、幂等内容冲突、已失败轮次重放。 |
| 413 | `request_too_large` | 请求体超过 32 KiB。 |
| 429 | `rate_limited`、`generation_busy` | 请求频率或每日生成次数超限、模型并发槽满，带 `Retry-After` 秒数。 |
| 500 | `internal_error` | 未分类内部错误。 |
| 502 | `model_error` | 模型调用失败、输出无效或拒答。 |
| 503 | `dependency_unavailable`、`model_unavailable` | 数据库不可用或提交结果不明、供应商容量或额度不可用。 |
| 504 | `model_timeout` | 模型生成超时。 |

会话不存在和越权返回相同的 `404 not_found` 结构与提示。错误正文和日志只使用受控消息及关联标识，敏感凭据、SQL、堆栈、用户原文和供应商原始响应留在对外边界之外。

客户端普通请求使用短超时，生成请求使用 70 秒超时。丢失响应、`202` 或生成结果不明时，先 GET 详情，生成中每 2 秒查一次，最多轮询 120 秒。完成后展示已落库结果，失败则显示原文和受控提示，仍生成中则提示稍后再查。前端不自动发起生成或换键重试。Phase 1 首轮的 `attempt_version` 固定从 1 开始并参与写回校验。显式重试的 `expected_attempt_version`、进程崩溃后的租约扫描及第 2/3 轮归 T09。

## Phase 2 草案索引

下列 4 个操作均未实现，详细契约及 JSON 示例尚待对应任务完善；保持 [架构草案](ARCHITECTURE.md#phase-2-api-草案) 的权威位置，只在这里索引，不将草案加入 OpenAPI `paths` 或补造 Schema。

| 方法 | 完整路径 | 用途与现有草案约束 |
| --- | --- | --- |
| GET | `/api/v1/debates` | 本人历史，page 默认 1、page_size 默认 20/最大 50；按 created_at、id 倒序。 |
| POST | `/api/v1/debates/{id}/turns/{turn_id}/retry` | 显式重试已有失败轮次，必填 expected_attempt_version；复用保存原文。 |
| POST | `/api/v1/debates/{id}/summary` | completed 会话总结生成或显式重试；retry=true 时必填期望版本。 |
| GET | `/api/v1/debates/{id}/summary` | 本人总结状态与可选结果；未开始为 not_started。 |

SSE 仍是可选设计，本次 OpenAPI 只描述冻结的 JSON 契约，不声明 `text/event-stream` 已实现。

## 核对差异与待确认问题

1. **健康路径差异已裁定**：2026 年 11 月 8 日负责人明确以 `/api/health/live`、`/api/health/ready` 为准；本轮同步前端、测试和当前运行文档。历史验收及学习笔记中的旧路径保留历史语境，不作为新契约，也不提供兼容别名。
2. **Session Cookie 名未冻结**：生产/开发具体名称待 T03 确认；OpenAPI 显式占位，未将候选 `__Host-` 前缀变成指定名称。
3. **后续契约范围**：Phase 2 仍为草案，不在本次机器规范的冻结范围中；完整三轮、显式重试、总结和分页不作为已实现交付。

## 本次文档验证

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

- API 交付：`docs/API.md`、`docs/openapi.yaml`。
- 路径说明与决定：`README.md`、`AGENTS.md`、`docs/ARCHITECTURE.md`、`docs/DECISIONS.md`、`docs/PLAN.md`、`docs/TASKS.md`。
- 相关调用和测试：`frontend/src/api/health.ts`、`frontend/src/api/health.test.ts`、`frontend/src/App.vue`、`backend/internal/platform/router_test.go`、`backend/internal/platform/health_test.go`、`backend/cmd/server/main_test.go`、`backend/tests/integration/database_test.go`。

验证器仅安装在 Git 忽略的 `.cache/api-docs/python-deps`，没有加入 Go/npm 产品依赖。该工具是持续维护的开源 OpenAPI 验证 CLI，采用 Apache-2.0；替代方案是使用官方 OpenAPI Schema 配合 JSON Schema 验证器。本次选择和用法依据其 [官方文档](https://openapi-spec-validator.readthedocs.io/en/latest/)。临时生成、检查脚本和开始时的文件哈希保存在 `.cache/api-docs/`，不作为仓库交付文件。

从仓库根目录复核规范的实际命令：

```powershell
$env:PYTHONPATH = Join-Path (Get-Location) '.cache/api-docs/python-deps'
python -m openapi_spec_validator docs/openapi.yaml
```

路径修正遵循测试先行：新增前端默认请求用例在修正前因 HTTP 404 失败，修正后全套通过；旧后端路径用例在同步前也观测到 404。本机受限沙箱无法连接临时回环 HTTP 服务，最初前端测试因此出现网络失败；在允许回环访问的执行环境重跑通过，未把沙箱失败计为通过。

本次未显式启用真实 MySQL 集成测试，未执行浏览器/Vite 端到端冒烟、真实模型调用或部署；没有 Git 提交、推送或任务结项操作。后续认证客户端联调前需确认并替换 Cookie 名占位符；进入其他业务任务仍按各自授权和前置验收执行。
