# T05 DSH 实施进度

2026-11-08（Asia/Shanghai）。本文件记录 DSH 实现 Worker 在 T05 的真实完成项、命令结果与阻塞，
只含非敏感信息，可提交审查。契约与计划见 [T05 首轮实施计划](T05_FIRST_TURN_PLAN.md)。

## DSH 切片交付时状态

| 切片 | 内容 | 状态 |
| --- | --- | --- |
| 1 | `backend/internal/topic/`（model/repository/service/handler + 测试） | 已完成，自测通过 |
| 2 | `backend/internal/debate/`（model/repository/service/handler + 测试） | 未开始 |
| 3 | 路由与 `main` 装配、HTTP 期限 | 未开始（按授权不在本 Worker 写入范围） |

## 切片 1：topic 查询接口

### 修改文件（全部为新增）

- `backend/internal/topic/model.go`
- `backend/internal/topic/repository.go`
- `backend/internal/topic/service.go`
- `backend/internal/topic/handler.go`
- `backend/internal/topic/model_test.go`
- `backend/internal/topic/repository_test.go`
- `backend/internal/topic/service_test.go`
- `backend/internal/topic/handler_test.go`

未修改 `main`/`router`/docs/既有 `auth`、`ai`、迁移、`go.mod`、`go.sum`。不新增依赖，
rebuilding 只用既有 `gorm.io/gorm`、`gin-gonic/gin`、`gorm.io/driver/mysql`（仅测试构造 DryRun 会话）。

### 交付签名

```go
// package topic
func NewRepository(orm *gorm.DB) *Repository
func NewService(store Store) *Service
func NewHandler(service *Service) *Handler
func (h *Handler) RegisterRoutes(group *gin.RouterGroup, authHandler *auth.Handler)
```

装配示例（Codex 侧真实接线）：

```go
topicRepo := topic.NewRepository(db.ORM)
topicService := topic.NewService(topicRepo)
topicHandler := topic.NewHandler(topicService)
topicHandler.RegisterRoutes(apiGroup.Group("/v1"), authHandler) // 注册 GET /v1/topics
```

`Store` 是最小可注入接口：`ListEnabled(ctx context.Context) ([]Topic, error)`。

### 行为实现要点

- 鉴权：`RegisterRoutes` 传入 `*auth.Handler` 时使用 `auth.RequireAuth()`；GET 不要求 Origin/CSRF。
  缺失身份由 Handler 自身再兜底返回 `401 unauthenticated`。
- 查询：`SELECT id,title,description FROM topics WHERE enabled = ? ORDER BY id ASC`，`enabled` 用参数绑定，
  排序在 SQL 中完成。只映射 `id/title/description`，`enabled`、时间列不出现在响应。
- 空列表：Service 返回非 nil 空切片，HTTP 输出 `"items": []`（非 `null`）。
- 数据校验：ID 非 0、UTF-8 合法、非全空白、标题 ≤200 码点、描述 ≤2000 码点；脏数据以受控错误失败，
  不返回给客户端。
- 错误映射：存储/依赖故障 `503 dependency_unavailable`；数据契约破坏 `500 internal_error`。
  错误文本不含 SQL、DSN、账户或底层 cause。
- 信封：`{"data":{"items":[...]},"request_id":"..."}`；`Cache-Control: no-store`；
  `X-Request-ID` 与信封 `request_id` 一致。数据库操作 3 秒超时从请求 Context 派生。

### 命令与真实结果

测试先行（red，实现前）：

```text
$ cd backend; go test ./internal/topic
FAIL github.com/bingqilin456/DebateForge/backend/internal/topic [build failed]
service_test.go:12:11: undefined: Topic
service_test.go:27:13: undefined: NewService
repository_test.go:59:43: undefined: Repository
repository_test.go:98:21: undefined: ErrDependencyUnavailable
handler_test.go:18:43: undefined: Store
handler_test.go:143:48: undefined: storeError
... 其余 undefined 省略
```

green（实现后）：

```text
$ cd backend; go test ./internal/topic -count=1
ok  github.com/bingqilin456/DebateForge/backend/internal/topic  0.242s

$ cd backend; go test -race ./internal/topic -count=1
ok  github.com/bingqilin456/DebateForge/backend/internal/topic  1.446s

$ cd backend; go vet ./internal/topic
(无输出，exit=0)

$ cd backend; gofmt -l internal/topic
(无输出，gofmt clean)

$ cd backend; go build ./...
(无输出，exit=0)
```

17 个用例全部 PASS：Handler 7 个（含真实 `auth.RequireAuth` 接线、空 `[]`、503 不泄露、只注册 GET、
nil Service 受控 503）、Service 5 个、Repository 3 个（DryRun 断言 `enabled`/`ORDER BY id` SQL 形状）、
Model 2 个。

### 无关既有失败（需要 Codex 判断，本 Worker 未修改）

为确认回归范围，额外运行了 `go test ./...`，发现 **auth 包存在与本切片无关的既有失败**：

```text
$ cd backend; go test ./internal/auth -count=1 -run TestLoginHTTPContract
--- FAIL: TestLoginHTTPContract (0.04s)
    handler_test.go:390: Expires 不是约 24 小时后
```

判定依据与原因分析（未修改，`git status` 显示 `internal/auth` 无任何改动）：

1. 该用例使用 `newTestService` 注入固定时钟 `2026-11-08 12:00:00 UTC`（`service_test.go:160`），
   于是 Set-Cookie 的 `Expires` 固定为 `2026-11-09 12:00:00 UTC`；但断言用的是**真实时间**
   `time.Until(cookie.Expires) < DefaultSessionTTL-time.Minute`（`handler_test.go:389`）。
2. 当前真实时间已晚于固定的假时钟起点（本次运行约 `2026-11-08T14:33Z`），
   `time.Until` 约 21 小时 27 分，小于 23 小时 59 分，断言必然失败。
3. 因此这是**与墙钟相关的既有测试缺陷**，在假时钟起点之后运行即失败，与 topic 切片、T05
   业务代码无关；HEAD `b1c255b` 下同样失败。
4. 修复方向应由 Codex 决定（例如改为断言 `cookie.Expires.Sub(clock.Now())` 或换成相对比较），
   本 Worker 未越权改动 `internal/auth`。

其余包全部通过：`cmd/server`、`cmd/t04-smoke`、`internal/ai`、`internal/platform`、
`internal/topic`、`tests/integration`（集成用例按设计跳过）。

## 已知限制与未覆盖项

1. **未接真实 MySQL**：本切片的仓储测试使用 DryRun 与失败连接器，断言的是 SQL 形状与错误映射；
   真实 `enabled=1` 排序、种子 5 条辩题与跨连接行为留待 Codex 串行集成验证
   （`SIYIGE_INTEGRATION=1` 的固定测试库）。
2. **未装配路由**：`GET /api/v1/topics` 尚未挂到 `platform/router.go`，当前 HTTP 上仍为 404。
   计划第 2 项属于 Codex 装配范围，本 Worker 未写入 `main`/`router`。
3. **未做真实 HTTP 调用**：新建连接的验证与 503 断言只在进程内 Handler 层完成。
4. debate 切片（创建会话、首轮认领/生成/写回、幂等、并发与额度、详情）尚未开始。
5. 无收费调用、无数据库写、无 Git commit/push/merge、无部署；`.env` 与凭据未读取或输出。


## Codex 最终交接增量

本文件上述内容仅记录 DSH 当时的切片交付与自测，不表示最终仓库仍未装配。DSH 已结束并释放独占 claim；Codex 后续已完成 debate、main/router、真实 HTTP/隔离 MySQL、累计 race、静态检查和文档。两次新 Codex 会话初审/复审后，三项 P2 已修复并独立复验关闭，最终复审 PASS。2026-11-08 项目负责人确认三关人工验收全部通过，正式批准 T05 无收费范围结项并授权 Git Commit；真实模型链路仍未验收，后续调用须另行授权。本次结项登记与提交未使用 DSH。最终事实以 [T05 验收记录](../acceptance/T05_FIRST_TURN_ACCEPTANCE.md) 为准。
