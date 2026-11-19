package debate

import (
	"bytes"
	"context"
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

// 批注的 HTTP 边界测试：读写保护顺序、越权 404、未启用切片的行为与响应形状。
// 使用内存 Store + ai.Fake，不连接数据库、不调用真实模型。

type annotationHTTPFixture struct {
	router *gin.Engine
	token  string
	csrf   string
	store  *memoryStore
	fake   *ai.Fake
	svc    *Service
	// 一轮已完成的会话，用于读取与补做。
	debateID uint64
	turnID   uint64
}

func newAnnotationHTTPFixture(t *testing.T, enable bool) *annotationHTTPFixture {
	t.Helper()
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))
	hash := sha256.Sum256([]byte(token))
	csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{8}, 32))
	now := time.Now().UTC()
	authStore := reviewAuthStore{auth.Session{ID: 1, UserID: 1, TokenHash: hash[:], CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
	a, err := auth.NewHandler(auth.NewService(authStore), auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatal(err)
	}
	mem := newMemoryStore(func() time.Time { return now })
	fake := ai.NewFake(ai.FakeStep{Result: ai.Result{Content: "AI 回复"}})
	svc := NewService(mem, fake, NewGate(func() time.Time { return now }))
	if enable {
		svc.EnableAnnotations(true)
	}
	t.Cleanup(func() { svc.waitBackground(); svc.StopLeaseRecovery() })

	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(svc).RegisterRoutes(router.Group("/api/v1"), a)
	// RegisterRoutes 会启动租约扫描；memoryStore 不是并发安全的测试替身，
	// 因此先等启动扫描结束再写入夹具，避免扫描 goroutine 与夹具写入竞争。
	// 扫描间隔 30 秒远长于单个用例，用例期间不会再次扫描。
	if err := svc.waitStartupScan(context.Background()); err != nil {
		t.Fatal(err)
	}

	f := &annotationHTTPFixture{router: router, token: token, csrf: csrf, store: mem, fake: fake, svc: svc}
	// 直接构造一轮已完成的会话与正式消息，避免依赖提交流程的额外分支。
	debate := Debate{ID: 11, UserID: 1, TopicID: topicID(1), TopicTitleSnapshot: "题目",
		TopicDescriptionSnapshot: "说明", UserStance: "pro", Mode: string(ai.ModeStandard4),
		AIStyle: string(ai.StyleLogical), Status: "active", CompletedRounds: 1}
	mem.debates[debate.ID] = debate
	userMessage := Message{ID: 21, TurnID: 31, Role: "user", Content: "第一轮用户发言：标准应当可检验。"}
	assistantMessage := Message{ID: 22, TurnID: 31, Role: "assistant", Content: "第一轮回复：可检验不等于可量化。"}
	turn := Turn{
		ID: 31, DebateID: debate.ID, RoundNo: 1, ClientRequestID: "550e8400-e29b-41d4-a716-446655440001",
		Status: "completed", AttemptVersion: 1, AnnotationEligible: true,
		Messages: []Message{userMessage, assistantMessage},
	}
	mem.turns[debate.ID] = []Turn{turn}
	f.debateID, f.turnID = debate.ID, turn.ID
	return f
}

func (f *annotationHTTPFixture) do(t *testing.T, method, path string, body string, withCSRF, withOrigin, withSession bool) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if withOrigin {
		req.Header.Set("Origin", "http://127.0.0.1:5173")
	}
	if withCSRF {
		req.Header.Set("X-CSRF-Token", f.csrf)
	}
	if withSession {
		req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: f.token})
	}
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

func TestAnnotationRoutesRegisteredWithGuards(t *testing.T) {
	f := newAnnotationHTTPFixture(t, true)
	getPath := "/api/v1/debates/11/annotations"
	postPath := "/api/v1/debates/11/turns/31/annotations/retry"

	// 读取只要求 Session。
	rec := f.do(t, "GET", getPath, "", false, false, true)
	if rec.Code != 200 {
		t.Fatalf("读取应为 200，实际 %d %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("业务响应必须 no-store")
	}
	// 缺 Session 为 401。
	if rec = f.do(t, "GET", getPath, "", false, false, false); rec.Code != 401 {
		t.Fatalf("缺 Session 应为 401，实际 %d", rec.Code)
	}
	// 补做缺 CSRF 为 403，缺 Origin 为 403，缺 Session 为 401。
	body := `{"expected_attempt_version":0}`
	if rec = f.do(t, "POST", postPath, body, false, true, true); rec.Code != 403 || !strings.Contains(rec.Body.String(), "csrf_failed") {
		t.Fatalf("缺 CSRF 应为 403，实际 %d %s", rec.Code, rec.Body.String())
	}
	if rec = f.do(t, "POST", postPath, body, true, false, true); rec.Code != 403 {
		t.Fatalf("缺 Origin 应为 403，实际 %d", rec.Code)
	}
	if rec = f.do(t, "POST", postPath, body, true, true, false); rec.Code != 401 {
		t.Fatalf("缺 Session 应为 401，实际 %d", rec.Code)
	}
	// GET 不支持的方法为 405，未知路径为 404。
	if rec = f.do(t, "POST", getPath, "", false, false, true); rec.Code != 404 && rec.Code != 405 {
		t.Fatalf("读取路径不接受 POST，实际 %d", rec.Code)
	}
}

func TestAnnotationReadReturnsEmptyForIneligibleHistory(t *testing.T) {
	f := newAnnotationHTTPFixture(t, true)
	// 把资格标志去掉，模拟旧会话：读取返回空数组而不是错误。
	turn := f.store.turns[f.debateID][0]
	turn.AnnotationEligible = false
	f.store.turns[f.debateID][0] = turn
	rec := f.do(t, "GET", "/api/v1/debates/11/annotations", "", false, false, true)
	if rec.Code != 200 {
		t.Fatalf("旧会话应返回 200，实际 %d", rec.Code)
	}
	var envelope struct {
		Data struct {
			Items []AnnotationBatchDTO `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.Items == nil || len(envelope.Data.Items) != 0 {
		t.Fatalf("旧会话应返回空数组: %s", rec.Body.String())
	}
}

func TestAnnotationHTTPOwnershipIs404(t *testing.T) {
	f := newAnnotationHTTPFixture(t, true)
	// 会话属于其他用户：读取与补做都按 404 处理，不泄露存在性。
	f.store.debates[f.debateID] = Debate{ID: f.debateID, UserID: 2}
	rec := f.do(t, "GET", "/api/v1/debates/11/annotations", "", false, false, true)
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Fatalf("越权读取应为 404，实际 %d %s", rec.Code, rec.Body.String())
	}
	rec = f.do(t, "POST", "/api/v1/debates/11/turns/31/annotations/retry", `{"expected_attempt_version":0}`, true, true, true)
	if rec.Code != 404 {
		t.Fatalf("越权补做应为 404，实际 %d", rec.Code)
	}
}

func TestAnnotationHTTPRejectsBadBodies(t *testing.T) {
	f := newAnnotationHTTPFixture(t, true)
	path := "/api/v1/debates/11/turns/31/annotations/retry"
	// 非整数、浮点、字符串与哨兵值都必须被 JSON 层或业务校验拒绝为 400。
	for _, body := range []string{
		`{"expected_attempt_version":-1}`,
		`{"expected_attempt_version":"1"}`,
		`{"expected_attempt_version":1.5}`,
		`{"expected_attempt_version":18446744073709551615}`,
	} {
		rec := f.do(t, "POST", path, body, true, true, true)
		if rec.Code != 400 {
			t.Fatalf("body %s 应 400，实际 %d %s", body, rec.Code, rec.Body.String())
		}
	}
	// 省略字段等价于 0（未启动），是合法请求，不应被解析层拒绝。
	rec := f.do(t, "POST", path, `{}`, true, true, true)
	if rec.Code == 400 && strings.Contains(rec.Body.String(), "invalid_request") {
		t.Fatalf("省略字段应视为 0，实际被拒绝: %s", rec.Body.String())
	}
	// 非 JSON 媒体类型按既有契约 400。
	req := httptest.NewRequest("POST", path, strings.NewReader(`{"expected_attempt_version":0}`))
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.Header.Set("X-CSRF-Token", f.csrf)
	req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: f.token})
	rec = httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	if rec.Code != 400 {
		t.Fatalf("非 JSON 应为 400，实际 %d", rec.Code)
	}
}

func TestAnnotationHTTPRetrySuccessShape(t *testing.T) {
	f := newAnnotationHTTPFixture(t, true)
	rec := f.do(t, "POST", "/api/v1/debates/11/turns/31/annotations/retry", `{"expected_attempt_version":0}`, true, true, true)
	if rec.Code != 201 {
		t.Fatalf("首次补做应为 201，实际 %d %s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Data AnnotationBatchDTO `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	batch := envelope.Data
	if batch.Status != AnnotationCompleted || batch.TurnID != "31" || batch.RoundNo != 1 || len(batch.Items) != 1 {
		t.Fatalf("响应形状错误: %+v", batch)
	}
	item := batch.Items[0]
	if item.Category == "" || item.Label == "" || item.Reason == "" || item.Improvement == "" {
		t.Fatalf("条目缺少必需文本: %+v", item)
	}
	if item.Anchor.MessageID != "21" && item.Anchor.MessageID != "22" {
		t.Fatalf("锚点消息必须在白名单内: %+v", item.Anchor)
	}
	if !item.Anchor.Resolved || item.Anchor.StartCP == nil || item.Anchor.EndCP == nil {
		t.Fatalf("唯一引用应带坐标: %+v", item.Anchor)
	}
	// 成功批次重复补做只返回已存结果，不再生成。
	calls := f.fake.AnnotationCalls()
	rec = f.do(t, "POST", "/api/v1/debates/11/turns/31/annotations/retry", `{"expected_attempt_version":1}`, true, true, true)
	if rec.Code != 200 {
		t.Fatalf("成功批次重放应为 200，实际 %d", rec.Code)
	}
	if f.fake.AnnotationCalls() != calls {
		t.Fatal("成功批次不得重复生成")
	}
}

func TestAnnotationHTTPUnavailableWhenNotEnabled(t *testing.T) {
	f := newAnnotationHTTPFixture(t, false)
	// 关闭切片：读取返回空列表，补做受控拒绝且不写批次。
	rec := f.do(t, "GET", "/api/v1/debates/11/annotations", "", false, false, true)
	if rec.Code != 200 {
		t.Fatalf("关闭时读取仍应 200，实际 %d", rec.Code)
	}
	rec = f.do(t, "POST", "/api/v1/debates/11/turns/31/annotations/retry", `{"expected_attempt_version":0}`, true, true, true)
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), `"invalid_request"`) {
		t.Fatalf("关闭时补做应拒绝，实际 %d %s", rec.Code, rec.Body.String())
	}
	if len(f.store.annotations) != 0 {
		t.Fatal("关闭时不得写入批次")
	}
}

func TestAnnotationHTTPNonEligibleTurnIsConflict(t *testing.T) {
	f := newAnnotationHTTPFixture(t, true)
	// 非合格轮（例如未开启切片时的旧轮）不启动生成。
	turn := f.store.turns[f.debateID][0]
	turn.AnnotationEligible = false
	f.store.turns[f.debateID][0] = turn
	rec := f.do(t, "POST", "/api/v1/debates/11/turns/31/annotations/retry", `{"expected_attempt_version":0}`, true, true, true)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), `"round_conflict"`) {
		t.Fatalf("非合格轮应为 409，实际 %d %s", rec.Code, rec.Body.String())
	}
	if f.fake.AnnotationCalls() != 0 {
		t.Fatal("非合格轮不得调用模型")
	}
}
