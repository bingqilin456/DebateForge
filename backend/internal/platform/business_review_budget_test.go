package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
)

// 复盘生成在请求内同步等待一次模型调用，必须拿到生成预算；读取仍是普通短预算。
//
// 这里不注册临时 handler：走 NewBusinessRouter 装配出的真实业务路由，
// 在认证中间件读取 Cookie 的那一刻记录请求期限，等于观察请求刚进入路由时的预算。
func TestBusinessRouterReviewBudgetThroughRealMiddleware(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	store := &reviewBudgetAuthStore{token: token, csrf: csrf}
	authHandler, err := auth.NewHandler(auth.NewService(store), auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatal(err)
	}
	debateStore := &reviewBudgetDebateStore{}
	service := debate.NewService(debateStore, nil, debate.NewGate(nil))
	router := NewBusinessRouter(slog.New(slog.NewTextHandler(io.Discard, nil)), nil, authHandler, nil, debate.NewHandler(service))
	defer service.StopLeaseRecovery()

	do := func(method, body string) *httptest.ResponseRecorder {
		var reader io.Reader
		if body != "" {
			reader = strings.NewReader(body)
		}
		req := httptest.NewRequest(method, "/api/v1/debates/1/review", reader)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:5173")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: token})
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	// POST：同步生成入口必须使用生成预算（60s），而不是普通 5s。
	do("POST", `{"retry":false}`)
	postBudget, ok := store.lastBudget()
	if !ok || postBudget > 60*time.Second || postBudget < 59*time.Second {
		t.Fatalf("POST /review 应使用生成预算，实际 remaining=%v ok=%v", postBudget, ok)
	}

	// GET：仍是普通 5s 预算，并且关闭切片时只做既有归属校验、投影 not_started。
	rec := do("GET", "")
	getBudget, ok := store.lastBudget()
	if !ok || getBudget > 5*time.Second || getBudget < 4*time.Second {
		t.Fatalf("GET /review 应保持普通预算，实际 remaining=%v ok=%v", getBudget, ok)
	}
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"status":"not_started"`) {
		t.Fatalf("关闭时应投影未开始: code=%d body=%s", rec.Code, rec.Body.String())
	}
	if debateStore.reviewReads != 0 {
		t.Fatalf("关闭时禁止读取复盘存储，实际读取 %d 次", debateStore.reviewReads)
	}
}

// reviewBudgetAuthStore 是有效的固定 Session 桩，并在认证读取 Cookie 时记录预算。
type reviewBudgetAuthStore struct {
	token string
	csrf  string
	bud   time.Duration
	found bool
}

func (s *reviewBudgetAuthStore) record(ctx context.Context) {
	deadline, ok := ctx.Deadline()
	s.bud, s.found = time.Until(deadline), ok
}

func (s *reviewBudgetAuthStore) lastBudget() (time.Duration, bool) { return s.bud, s.found }

func (s *reviewBudgetAuthStore) CreateUser(context.Context, string, string) (auth.User, error) {
	return auth.User{}, errors.New("unused")
}
func (s *reviewBudgetAuthStore) FindUserByUsername(context.Context, string) (auth.User, error) {
	return auth.User{}, errors.New("unused")
}
func (s *reviewBudgetAuthStore) FindUserByID(_ context.Context, id uint64) (auth.User, error) {
	if id != 1 {
		return auth.User{}, auth.ErrNotFound
	}
	return auth.User{ID: 1, Username: "review_user"}, nil
}
func (s *reviewBudgetAuthStore) CreateSession(context.Context, auth.Session) (uint64, error) {
	return 0, errors.New("unused")
}
func (s *reviewBudgetAuthStore) FindSessionByTokenHash(ctx context.Context, hash []byte) (auth.Session, error) {
	s.record(ctx)
	digest := sha256.Sum256([]byte(s.token))
	if !bytes.Equal(hash, digest[:]) {
		return auth.Session{}, auth.ErrNotFound
	}
	now := time.Now().UTC()
	return auth.Session{ID: 1, UserID: 1, TokenHash: digest[:], CSRFToken: s.csrf,
		CreatedAt: now, ExpiresAt: now.Add(time.Hour)}, nil
}
func (s *reviewBudgetAuthStore) DeleteSession(context.Context, uint64) (bool, error) {
	return false, nil
}

// reviewBudgetDebateStore 只实现路由实际会触达的方法：
// 租约回收（启动扫描）、关闭分支的归属校验 Detail，以及用于证明未被调用的 GetReview。
type reviewBudgetDebateStore struct {
	debate.Store
	reviewReads int
}

func (s *reviewBudgetDebateStore) RecoverExpiredLeases(context.Context) (int, error) { return 0, nil }

func (s *reviewBudgetDebateStore) Detail(_ context.Context, uid, id uint64) (debate.Detail, error) {
	if uid != 1 || id != 1 {
		return debate.Detail{}, debate.ErrNotFound
	}
	return debate.Detail{Debate: debate.Debate{ID: 1, UserID: 1, Mode: "standard_4", Status: "completed"}, Turns: []debate.Turn{}}, nil
}

func (s *reviewBudgetDebateStore) GetReview(context.Context, uint64, uint64) (*debate.Review, error) {
	s.reviewReads++
	return nil, errors.New("table debate_reviews doesn't exist")
}
