package debate

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

// 本文件是创建接口的 HTTP 边界回归：Origin、Session、CSRF 与现有错误封装不变，
// 二选一校验先于任何数据库写入，自定义 201 响应的 topic.id 为 null。
// 使用内存 Store 与 ai.Fake，不连接数据库、不调用真实模型。

type customHTTPFixture struct {
	router *gin.Engine
	token  string
	csrf   string
	store  *memoryStore
	fake   *ai.Fake
}

func newCustomHTTPFixture(t *testing.T) *customHTTPFixture {
	t.Helper()
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{3}, 32))
	hash := sha256.Sum256([]byte(token))
	csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{4}, 32))
	now := time.Now().UTC()
	store := reviewAuthStore{auth.Session{ID: 1, UserID: 1, TokenHash: hash[:], CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
	a, err := auth.NewHandler(auth.NewService(store), auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatal(err)
	}
	mem := newMemoryStore(func() time.Time { return now })
	fake := ai.NewFake()
	service := NewService(mem, fake, NewGate(func() time.Time { return now }))
	defer service.StopLeaseRecovery()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(service).RegisterRoutes(router.Group("/api/v1"), a)
	return &customHTTPFixture{router: router, token: token, csrf: csrf, store: mem, fake: fake}
}

func (f *customHTTPFixture) post(t *testing.T, body string, withCSRF bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/v1/debates", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	if withCSRF {
		req.Header.Set("X-CSRF-Token", f.csrf)
	}
	req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: f.token})
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestCreateCustomTopicOverHTTP(t *testing.T) {
	f := newCustomHTTPFixture(t)
	body := `{"custom_topic":{"title":"是否应延长图书馆开放时间？","background":"背景：期末周复习需求上升。"},"user_stance":"con","ai_style":"empathic"}`
	rec := f.post(t, body, true)
	if rec.Code != 201 {
		t.Fatalf("自定义创建应返回 201，实际 %d %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			ID    string `json:"id"`
			Topic struct {
				ID          *string `json:"id"`
				Title       string  `json:"title"`
				Description string  `json:"description"`
			} `json:"topic"`
			UserStance      string `json:"user_stance"`
			AIStance        string `json:"ai_stance"`
			Mode            string `json:"mode"`
			AIStyle         string `json:"ai_style"`
			TotalRounds     int    `json:"total_rounds"`
			Status          string `json:"status"`
			CompletedRounds int    `json:"completed_rounds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	d := envelope.Data
	if d.Topic.ID != nil {
		t.Fatalf("自定义会话的 topic.id 必须是 JSON null，实际 %q", *d.Topic.ID)
	}
	if d.Topic.Title != "是否应延长图书馆开放时间？" || d.Topic.Description != "背景：期末周复习需求上升。" {
		t.Fatalf("快照应保留用户原文: %+v", d.Topic)
	}
	if d.UserStance != "con" || d.AIStance != "pro" || d.Mode != "standard_4" || d.TotalRounds != 4 || d.Status != "active" || d.CompletedRounds != 0 {
		t.Fatalf("创建响应不符合契约: %+v", d)
	}
	if d.AIStyle != "empathic" {
		t.Fatalf("流派未按请求保存: %q", d.AIStyle)
	}
	if f.fake.Calls() != 0 {
		t.Fatal("创建会话不得调用模型")
	}
	// 自定义题不进入任何公共题库：列表只来自 topics 表，这里不产生该实体。
	if len(f.store.debates) != 1 {
		t.Fatal("应只创建一条会话")
	}
}

func TestCreateCustomTopicRejectsAmbiguousBodies(t *testing.T) {
	f := newCustomHTTPFixture(t)
	cases := []string{
		`{"topic_id":"1","custom_topic":{"title":"题"},"user_stance":"pro"}`,
		`{"user_stance":"pro"}`,
		`{"topic_id":null,"user_stance":"pro"}`,
		`{"custom_topic":null,"user_stance":"pro"}`,
		`{"custom_topic":{"title":"   "},"user_stance":"pro"}`,
		`{"custom_topic":{"title":""},"user_stance":"pro"}`,
		`{"custom_topic":{"title":"` + strings.Repeat("题", 201) + `"},"user_stance":"pro"}`,
		`{"custom_topic":{"title":"题","background":"` + strings.Repeat("背", 1001) + `"},"user_stance":"pro"}`,
		`{"custom_topic":{"title":"题","background":1},"user_stance":"pro"}`,
		`{"custom_topic":{"title":"题"}}`,
		// 重复顶层字段按 encoding/json 的既有语义取最后一次，
		// 最后一次为 null 时必须被二选一校验拒绝，不能回退到先出现的分支。
		`{"custom_topic":{"title":"题"},"custom_topic":null,"user_stance":"pro"}`,
		`{"custom_topic":{"title":"题"},"topic_id":"1","topic_id":null,"user_stance":"pro"}`,
	}
	for _, body := range cases {
		rec := f.post(t, body, true)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"invalid_request"`) {
			t.Fatalf("body %s 应返回 400 invalid_request，实际 %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if len(f.store.debates) != 0 {
		t.Fatal("被拒绝的请求不得写入任何会话")
	}
	if f.fake.Calls() != 0 {
		t.Fatal("被拒绝的请求不得调用模型")
	}
}

// 顶层未知字段的严格程度按分支区分：自定义分支拒绝（新入口，避免混入赛制/归属），
// 预设分支沿用既有忽略行为（旧客户端与旧示例继续有效）。
func TestCreateTopLevelUnknownFieldsStrictnessPerBranch(t *testing.T) {
	f := newCustomHTTPFixture(t)
	// 自定义分支：未知字段与重复键都 400。
	for _, body := range []string{
		`{"custom_topic":{"title":"题"},"user_stance":"pro","mode":"quick_3"}`,
		`{"custom_topic":{"title":"题"},"user_stance":"pro","user_id":"999"}`,
		`{"custom_topic":{"title":"题"},"custom_topic":{"title":"另一题"},"user_stance":"pro"}`,
	} {
		rec := f.post(t, body, true)
		if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"invalid_request"`) {
			t.Fatalf("自定义分支 body %s 应返回 400，实际 %d %s", body, rec.Code, rec.Body.String())
		}
	}
	if len(f.store.debates) != 0 {
		t.Fatal("被拒绝的自定义请求不得写入会话")
	}

	// 预设分支：未知字段被忽略，赛制/轮次/归属仍由服务端决定。
	rec := f.post(t, `{"topic_id":"1","user_stance":"pro","mode":"quick_3","user_id":"999","rounds":7}`, true)
	if rec.Code != 201 {
		t.Fatalf("预设分支顶层未知字段应被忽略，实际 %d %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Mode            string `json:"mode"`
			TotalRounds     int    `json:"total_rounds"`
			CompletedRounds int    `json:"completed_rounds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Mode != "standard_4" || envelope.Data.TotalRounds != 4 || envelope.Data.CompletedRounds != 0 {
		t.Fatalf("赛制与轮次不得由客户端指定: %+v", envelope.Data)
	}
	// 归属来自 Session：会话必须挂在当前用户 1 名下。
	for _, d := range f.store.debates {
		if d.UserID != 1 {
			t.Fatalf("归属必须取自 Session，实际 user_id=%d", d.UserID)
		}
	}
}

func TestCreatePresetTopicStillWorksOverHTTP(t *testing.T) {
	f := newCustomHTTPFixture(t)
	rec := f.post(t, `{"topic_id":"1","user_stance":"pro"}`, true)
	if rec.Code != 201 {
		t.Fatalf("预设创建应保持 201，实际 %d %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data struct {
			Topic struct {
				ID *string `json:"id"`
			} `json:"topic"`
			AIStyle string `json:"ai_style"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Topic.ID == nil || *envelope.Data.Topic.ID != "1" {
		t.Fatalf("预设会话 topic.id 应仍为字符串，实际 %+v", envelope.Data.Topic.ID)
	}
	if envelope.Data.AIStyle != "logical" {
		t.Fatalf("省略 ai_style 应默认 logical，实际 %q", envelope.Data.AIStyle)
	}
}

// 权限与写保护顺序不变：缺 CSRF 为 403、缺 Session 为 401，都不会进入业务解析。
func TestCreateCustomTopicKeepsCSRFAndAuthGuards(t *testing.T) {
	f := newCustomHTTPFixture(t)
	rec := f.post(t, `{"custom_topic":{"title":"题"},"user_stance":"pro"}`, false)
	if rec.Code != 403 || !strings.Contains(rec.Body.String(), `"csrf_failed"`) {
		t.Fatalf("缺 CSRF 应为 403，实际 %d %s", rec.Code, rec.Body.String())
	}
	req := httptest.NewRequest("POST", "/api/v1/debates", strings.NewReader(`{"custom_topic":{"title":"题"},"user_stance":"pro"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.Header.Set("X-CSRF-Token", f.csrf)
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("缺 Session 应为 401，实际 %d %s", rec.Code, rec.Body.String())
	}
	if len(f.store.debates) != 0 {
		t.Fatal("未通过写保护的请求不得写入")
	}
}
