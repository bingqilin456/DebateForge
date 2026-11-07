# 思弈阁 API 契约

2026 年 11 月 6 日，T01 固定 Phase 1 的 8 个业务接口；11 月 7 日项目负责人确认契约设计通过人工验收，T01 已结项。仅代表接口契约设计完成，不代表业务代码已实现。当前运行基线为 PB 存活检查，业务实现按 [TASKS](TASKS.md) 分批交付。历史分页、完整三轮、显式重试与总结仍以 [架构草案](ARCHITECTURE.md#phase-2-api-草案) 为设计参考。

## 存活检查

- 方法与路径：`GET /health/live`，不带 `/api/v1` 前缀。
- 输入：无请求体，无查询参数要求，无认证要求；只读，不调用数据库或模型。
- 输出：HTTP 200，`Content-Type: application/json; charset=utf-8`，JSON `{"status":"alive"}`，不套业务包装。
- 响应头：`X-Request-ID` 为服务器生成的关联标识，`Cache-Control: no-store`。
- 语义：仅表示 Go HTTP 进程可以处理请求，不代表数据库、模型或整个产品就绪。

## 就绪检查与 PB 错误

`GET /health/ready` 由 T02 实现。数据库短超时连通且必要配置就绪时返回 `200` 与 `{"status":"ready"}`，否则返回 `503` 与 `{"status":"not_ready"}`。路径无业务前缀，无认证，固定 JSON，无模型调用。PB 基线下该路径及未实现的业务路径返回 404。

未知路由返回 404，已知路径不支持的方法返回 405，意外 panic 返回受控 500。错误 JSON：

```json
{"error":{"code":"not_found","message":"接口不存在"},"request_id":"服务端生成的标识"}
```

错误码分别为 `not_found`、`method_not_allowed`、`internal_error`；不包含堆栈、请求正文或内部错误细节。当前没有业务接口，不借此声称认证、权限或限流已实现。

## 本地请求链路

浏览器访问 `http://127.0.0.1:5173`，用相对路径 `/health/live` 请求 Vite；Vite 将 `/health/`、`/api/` 前缀原样代理至 `http://127.0.0.1:8080`，不去掉前缀，不使用 SPA HTML 作为 API 成功结果。

前端检查 HTTP 状态、JSON Content-Type 和 `status=alive`，总超时 3 秒（包括读取响应体）。失败只显示受控中文提示，不自动重试；用户点击后重新检查。

## Phase 1 业务接口

业务请求使用相对路径 `/api/v1/...`。有请求体的接口使用 `Content-Type: application/json`，请求体上限 32 KiB。业务 JSON 成功响应包含 `data` 和 `request_id`，204 无正文。业务错误包含 `error.code`、`error.message`、`request_id`。响应头 `X-Request-ID` 与信封中的标识一致。业务响应使用 `Cache-Control: no-store`。错误提示供展示，客户端按状态码和 `error.code` 分支，代理自行产生的非契约响应按传输失败处理。

实体 ID 是正整数十进制字符串，范围为 MySQL `BIGINT UNSIGNED`。轮号和轮次的 `attempt_version` 是 JSON 正整数，`completed_rounds` 是非负整数。未开始的总结以 `summary_status.attempt_version: null` 表示。时间字段用 UTC RFC 3339。列表为空时返回 `[]`。

注册和登录可匿名调用，其余接口要求有效 Session Cookie。每个 POST 校验配置中的完整 Origin（协议、主机、端口），拒绝缺失、`null` 或不匹配的值。开发 Origin 为 `http://127.0.0.1:5173`，生产为配置的 HTTPS Origin。只有要求 Session 的 POST 才额外校验 `X-CSRF-Token`。注册和登录即使收到旧 Cookie，仍只做 Origin 校验作为 CSRF 防护。CSRF token 由登录或当前用户响应返回，客户端只保存在内存。Cookie 和口令细节见 [安全与资源控制](ARCHITECTURE.md#安全与资源控制)。

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

### 7. POST /api/v1/debates/:id/turns

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

### 8. GET /api/v1/debates/:id

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
