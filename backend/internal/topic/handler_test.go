package topic

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
)

// newHandlerRouter 用真实 Gin 路由 + 可注入 Store 组装，绕过认证中间件后直接设置
// typed 当前用户，验证 Handler 自身的协议行为（鉴权缺失分支另用未注入的请求覆盖）。
func newHandlerRouter(t *testing.T, store Store, authenticated bool) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("request_id", "req-topic-test")
		c.Header("X-Request-ID", "req-topic-test")
		if authenticated {
			c.Set(auth.CurrentUserKey, auth.CurrentUser{User: auth.User{ID: 42, Username: "alice"}})
		}
		c.Next()
	})
	handler := NewHandler(NewService(store))
	// nil 认证 Handler 表示“不做中间件”，用于检验 Handler 自身缺失身份的分支。
	handler.RegisterRoutes(router.Group("/api/v1"), nil)
	return router
}

type apiResult struct {
	status int
	body   []byte
	header http.Header
}

func doGet(t *testing.T, router *gin.Engine, path string) apiResult {
	t.Helper()
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, path, nil)
	router.ServeHTTP(response, request)
	return apiResult{status: response.Code, body: response.Body.Bytes(), header: response.Header()}
}

func decodeEnvelope(t *testing.T, result apiResult) (map[string]json.RawMessage, string) {
	t.Helper()
	var envelope struct {
		Data      map[string]json.RawMessage `json:"data"`
		Error     struct{ Code string }      `json:"error"`
		RequestID string                     `json:"request_id"`
	}
	if err := json.Unmarshal(result.body, &envelope); err != nil {
		t.Fatalf("响应不是 JSON 信封: %v", err)
	}
	if envelope.Error.Code != "" {
		return nil, envelope.Error.Code
	}
	return envelope.Data, ""
}

func assertContractHeaders(t *testing.T, result apiResult) {
	t.Helper()
	if got := result.header.Get("Cache-Control"); got != "no-store" {
		t.Fatalf("缺少 Cache-Control: no-store，实际=%q", got)
	}
	if got := result.header.Get("X-Request-ID"); got != "req-topic-test" {
		t.Fatalf("X-Request-ID 不匹配，实际=%q", got)
	}
	if got := result.header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type 不匹配，实际=%q", got)
	}
	var envelope struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(result.body, &envelope); err != nil {
		t.Fatalf("响应不是 JSON 信封: %v", err)
	}
	if envelope.RequestID != "req-topic-test" {
		t.Fatalf("信封 request_id 不匹配，实际=%q", envelope.RequestID)
	}
}

func TestHandlerListTopicsReturnsEnabledItems(t *testing.T) {
	store := &stubStore{topics: []Topic{
		{ID: 1, Title: "题目一", Description: "说明一"},
		{ID: 2, Title: "题目二", Description: "说明二"},
	}}
	router := newHandlerRouter(t, store, true)
	result := doGet(t, router, "/api/v1/topics")
	if result.status != http.StatusOK {
		t.Fatalf("status=%d want=200", result.status)
	}
	assertContractHeaders(t, result)
	data, code := decodeEnvelope(t, result)
	if code != "" {
		t.Fatalf("成功响应出现错误码=%s", code)
	}
	var items []map[string]string
	if err := json.Unmarshal(data["items"], &items); err != nil {
		t.Fatalf("items 不是 Topic 数组: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("items 长度=%d want=2", len(items))
	}
	if items[0]["id"] != "1" || items[1]["id"] != "2" {
		t.Fatalf("ID 未按字符串输出或顺序错误: %+v", items)
	}
	if items[0]["title"] != "题目一" || items[0]["description"] != "说明一" {
		t.Fatalf("Topic 字段未按契约输出: %+v", items[0])
	}
}

func TestHandlerListTopicsEmptyItemsIsArray(t *testing.T) {
	router := newHandlerRouter(t, &stubStore{}, true)
	result := doGet(t, router, "/api/v1/topics")
	if result.status != http.StatusOK {
		t.Fatalf("status=%d want=200", result.status)
	}
	data, _ := decodeEnvelope(t, result)
	if string(data["items"]) != "[]" {
		t.Fatalf("空列表必须序列化为 []，实际=%s", string(data["items"]))
	}
}

func TestHandlerListTopicsRequiresCurrentUser(t *testing.T) {
	router := newHandlerRouter(t, &stubStore{}, false)
	result := doGet(t, router, "/api/v1/topics")
	if result.status != http.StatusUnauthorized {
		t.Fatalf("status=%d want=401", result.status)
	}
	if _, code := decodeEnvelope(t, result); code != "unauthenticated" {
		t.Fatalf("error.code=%q want=unauthenticated", code)
	}
	assertContractHeaders(t, result)
}

func TestHandlerListTopicsMapsStorageFailureWithoutLeak(t *testing.T) {
	router := newHandlerRouter(t, &stubStore{err: storeError("查询辩题失败", errors.New("siyige_test_app@% SELECT 失败"))}, true)
	result := doGet(t, router, "/api/v1/topics")
	if result.status != http.StatusServiceUnavailable {
		t.Fatalf("status=%d want=503", result.status)
	}
	if _, code := decodeEnvelope(t, result); code != "dependency_unavailable" {
		t.Fatalf("error.code=%q want=dependency_unavailable", code)
	}
	assertContractHeaders(t, result)
	for _, secret := range []string{"siyige_test_app", "SELECT", "sql"} {
		if strings.Contains(string(result.body), secret) {
			t.Fatalf("503 响应泄露内部细节类别=%s", secret)
		}
	}
}

func TestHandlerListTopicsOnlyRegistersGET(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(NewService(&stubStore{}))
	handler.RegisterRoutes(router.Group("/api/v1"), nil)
	// 路由表应只有 GET /api/v1/topics；POST 不应被注册成成功路径。
	registered := map[string]bool{}
	for _, route := range router.Routes() {
		registered[route.Method+" "+route.Path] = true
	}
	if !registered["GET /api/v1/topics"] {
		t.Fatalf("未注册 GET /api/v1/topics: %+v", registered)
	}
	if registered["POST /api/v1/topics"] {
		t.Fatal("错误地注册了 POST /api/v1/topics")
	}
}

func TestHandlerListTopicsRejectsNilService(t *testing.T) {
	handler := NewHandler(nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("request_id", "req-topic-test")
		c.Set(auth.CurrentUserKey, auth.CurrentUser{User: auth.User{ID: 42, Username: "alice"}})
		c.Next()
	})
	handler.RegisterRoutes(router.Group("/api/v1"), nil)
	result := doGet(t, router, "/api/v1/topics")
	if result.status != http.StatusServiceUnavailable {
		t.Fatalf("缺少服务实现时应受控 503，实际=%d", result.status)
	}
}

// 保证 stubStore 同时满足 Handler 测试需要的 Store 契约。
var _ Store = (*stubStore)(nil)

// TestHandlerListTopicsWiresRealAuthMiddleware 证明真实装配下 GET /topics 需要有效 Session：
// 未携带 Cookie 的请求在认证中间件处即被拒绝，不会进入查询。
func TestHandlerListTopicsWiresRealAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("request_id", "req-topic-test")
		c.Header("X-Request-ID", "req-topic-test")
		c.Next()
	})
	// 无 Cookie 时 Authenticate 不会访问存储，因此这里可以只验证中间件接线本身。
	authHandler, err := auth.NewHandler(auth.NewService(nil), auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatalf("构造认证 Handler 失败: %v", err)
	}
	store := &stubStore{}
	NewHandler(NewService(store)).RegisterRoutes(router.Group("/api/v1"), authHandler)

	result := doGet(t, router, "/api/v1/topics")
	if result.status != http.StatusUnauthorized {
		t.Fatalf("未认证请求 status=%d want=401", result.status)
	}
	if _, code := decodeEnvelope(t, result); code != "unauthenticated" {
		t.Fatalf("error.code=%q want=unauthenticated", code)
	}
	if store.calls != 0 {
		t.Fatalf("未认证请求不应查询存储，实际调用=%d", store.calls)
	}
}
