package debate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

// V2-13 复盘 HTTP 边界：读取只要 Session；生成/重试要求 Origin+CSRF；
// 非法正文 400、版本冲突 409、未完成会话 409，且跨用户统一 404。

func reviewHTTPRouter(t *testing.T, service *Service, token, csrf string) *gin.Engine {
	t.Helper()
	hash := sha256.Sum256([]byte(token))
	now := time.Now().UTC()
	store := reviewAuthStore{auth.Session{ID: 1, UserID: 1, TokenHash: hash[:], CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
	a, err := auth.NewHandler(auth.NewService(store), auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(service).RegisterRoutes(router.Group("/api/v1"), a)
	return router
}

func reviewRequest(router *gin.Engine, method, path, body, token, csrf string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: token})
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestReviewHTTPBoundary(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	mem := newMemoryStore(nil)
	fake := ai.NewFake()
	clock := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	service := NewService(mem, fake, NewGate(func() time.Time { return clock }))
	service.EnableReviews(true)
	created := completeFourRoundDebate(t, mem, service)
	router := reviewHTTPRouter(t, service, token, csrf)
	base := "/api/v1/debates/" + formatID(created.ID) + "/review"

	// 读取：未开始时返回 not_started，且未触发模型。
	rec := reviewRequest(router, "GET", base, "", token, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"not_started"`) {
		t.Fatalf("读取未开始复盘失败: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// 启用时省略 available：旧客户端与旧 Mock 的 not_started 形状保持不变。
	if strings.Contains(rec.Body.String(), `"available"`) {
		t.Fatalf("启用时不应输出 available: body=%s", rec.Body.String())
	}
	if fake.ReviewCalls() != 0 {
		t.Fatal("读取不得调用模型")
	}

	// 缺少 CSRF 的生成请求被拒绝，且不写库。
	rec = reviewRequest(router, "POST", base, `{"retry":false}`, token, "")
	if rec.Code != 403 {
		t.Fatalf("缺少 CSRF 应 403，实际 %d", rec.Code)
	}
	if fake.ReviewCalls() != 0 {
		t.Fatal("被拒绝的写请求不得调用模型")
	}

	// 非法正文：null retry、retry 与版本不匹配、版本为 0、未知键、非对象都 400。
	// `{}` 是合法的首次生成形状（等价于 retry=false），与策略入口保持一致。
	for _, body := range []string{`{"retry":null}`, `{"retry":true}`, `{"retry":false,"expected_attempt_version":1}`, `{"retry":true,"expected_attempt_version":0}`, `{"retry":true,"expected_attempt_version":1,"extra":1}`, `[]`} {
		// HTTP 防刷按分钟累计 6 次；这里推进时钟，避免用限流掩盖正文校验结论。
		clock = clock.Add(time.Minute)
		rec = reviewRequest(router, "POST", base, body, token, csrf)
		if rec.Code != 400 {
			t.Fatalf("非法正文 %s 应 400，实际 %d body=%s", body, rec.Code, rec.Body.String())
		}
	}

	// 合法首次生成 201，内容已保存；重复 POST 幂等返回 200 且不再调用模型。
	clock = clock.Add(time.Minute)
	rec = reviewRequest(router, "POST", base, `{"retry":false}`, token, csrf)
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"status":"completed"`) {
		t.Fatalf("首次生成失败: code=%d body=%s", rec.Code, rec.Body.String())
	}
	calls := fake.ReviewCalls()
	clock = clock.Add(time.Minute)
	rec = reviewRequest(router, "POST", base, `{"retry":false}`, token, csrf)
	if rec.Code != 200 || fake.ReviewCalls() != calls {
		t.Fatalf("已完成复盘重复 POST 应幂等 200 且不重生成: code=%d calls=%d/%d", rec.Code, fake.ReviewCalls(), calls)
	}
}

func TestReviewHTTPRejectsIncompleteAndCrossUser(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	mem := newMemoryStore(nil)
	service := NewService(mem, ai.NewFake(), NewGate(nil))
	service.EnableReviews(true)
	created, err := service.Create(context.Background(), 1, CreateInput{TopicID: topicID(1), UserStance: "pro", AIStyle: "logical"})
	if err != nil {
		t.Fatal(err)
	}
	router := reviewHTTPRouter(t, service, token, csrf)
	base := "/api/v1/debates/" + formatID(created.ID) + "/review"

	// 未完成结辩：不生成完整复盘（409 round_conflict）。
	rec := reviewRequest(router, "POST", base, `{"retry":false}`, token, csrf)
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "round_conflict") {
		t.Fatalf("未完成会话应 409 round_conflict，实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// 他人会话统一 404，不泄露资源是否存在。
	// 这里用独立的认证桩，确保 CurrentUser 真的是另一个用户（固定 ID 2）。
	otherHash := sha256.Sum256([]byte(token))
	otherStore := reviewOtherUserStore{session: auth.Session{
		ID: 2, UserID: 2, TokenHash: otherHash[:], CSRFToken: csrf,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	otherHandler, err := auth.NewHandler(auth.NewService(otherStore), auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	otherRouter := gin.New()
	NewHandler(service).RegisterRoutes(otherRouter.Group("/api/v1"), otherHandler)
	rec = reviewRequest(otherRouter, "GET", base, "", token, "")
	if rec.Code != 404 {
		t.Fatalf("跨用户读取应 404，实际 %d", rec.Code)
	}
	rec = reviewRequest(otherRouter, "POST", base, `{"retry":false}`, token, csrf)
	if rec.Code != 404 {
		t.Fatalf("跨用户生成应 404，实际 %d", rec.Code)
	}
}

// reviewStoreProbe 记录关闭切片时是否误读复盘表；GetReview 模拟旧环境未迁移时的故障，
// 其余方法全部委托内存替身。关闭契约要求：读取只经既有会话归属校验，
// 绝不能碰尚未部署的 debate_reviews（旧环境该表不存在，会映射成 503）。
type reviewStoreProbe struct {
	*memoryStore
	reviewReads int
}

func (s *reviewStoreProbe) GetReview(context.Context, uint64, uint64) (*Review, error) {
	s.reviewReads++
	return nil, dependency(errors.New("table debate_reviews doesn't exist"), false)
}

// 关闭复盘时 GET 只做既有只读归属校验：本场投影 not_started，
// 跨用户与不存在仍为 404（不能对所有 ID 一律回 not_started），且全程不读复盘表。
func TestReviewDisabledReadProjectsNotStartedWithoutTouchingReviewTable(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	mem := newMemoryStore(nil)
	probe := &reviewStoreProbe{memoryStore: mem}
	service := NewService(probe, ai.NewFake(), NewGate(nil))
	created := completeFourRoundDebate(t, mem, service)
	router := reviewHTTPRouter(t, service, token, csrf)
	base := "/api/v1/debates/" + formatID(created.ID) + "/review"

	rec := reviewRequest(router, "GET", base, "", token, "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"status":"not_started"`) ||
		!strings.Contains(rec.Body.String(), `"attempt_version":null`) {
		t.Fatalf("关闭时应投影未开始: code=%d body=%s", rec.Code, rec.Body.String())
	}
	// 关闭时必须显式告知不可用，客户端据此隐藏复盘界面并禁止生成。
	if !strings.Contains(rec.Body.String(), `"available":false`) {
		t.Fatalf("关闭时应返回 available:false: body=%s", rec.Body.String())
	}

	// 不存在的会话必须是 404，而不是对任意 ID 回 not_started。
	rec = reviewRequest(router, "GET", "/api/v1/debates/999999/review", "", token, "")
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Fatalf("不存在会话应 404，实际 %d body=%s", rec.Code, rec.Body.String())
	}

	// 跨用户同样 404，且不得泄露存在性。
	rec = reviewRequest(otherUserReviewRouter(t, service, token, csrf), "GET", base, "", token, "")
	if rec.Code != 404 {
		t.Fatalf("关闭时跨用户读取应 404，实际 %d", rec.Code)
	}

	if probe.reviewReads != 0 {
		t.Fatalf("关闭时禁止读取复盘存储，实际读取 %d 次", probe.reviewReads)
	}
}

// otherUserReviewRouter 只用固定 ID=2 的认证桩，保证 CurrentUser 真的是另一个用户。
func otherUserReviewRouter(t *testing.T, service *Service, token, csrf string) *gin.Engine {
	t.Helper()
	otherHash := sha256.Sum256([]byte(token))
	otherStore := reviewOtherUserStore{session: auth.Session{
		ID: 2, UserID: 2, TokenHash: otherHash[:], CSRFToken: csrf,
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().UTC().Add(time.Hour),
	}}
	otherHandler, err := auth.NewHandler(auth.NewService(otherStore), auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatal(err)
	}
	gin.SetMode(gin.TestMode)
	otherRouter := gin.New()
	NewHandler(service).RegisterRoutes(otherRouter.Group("/api/v1"), otherHandler)
	return otherRouter
}

type reviewOtherUserStore struct{ session auth.Session }

func (s reviewOtherUserStore) CreateUser(context.Context, string, string) (auth.User, error) {
	return auth.User{}, auth.ErrNotFound
}
func (s reviewOtherUserStore) FindUserByUsername(context.Context, string) (auth.User, error) {
	return auth.User{}, auth.ErrNotFound
}
func (s reviewOtherUserStore) FindUserByID(_ context.Context, id uint64) (auth.User, error) {
	if id != 2 {
		return auth.User{}, auth.ErrNotFound
	}
	return auth.User{ID: 2, Username: "other_user"}, nil
}
func (s reviewOtherUserStore) CreateSession(context.Context, auth.Session) (uint64, error) {
	return 0, auth.ErrNotFound
}
func (s reviewOtherUserStore) FindSessionByTokenHash(_ context.Context, hash []byte) (auth.Session, error) {
	if !bytes.Equal(hash, s.session.TokenHash) {
		return auth.Session{}, auth.ErrNotFound
	}
	return s.session, nil
}
func (s reviewOtherUserStore) DeleteSession(context.Context, uint64) (bool, error) { return false, nil }
