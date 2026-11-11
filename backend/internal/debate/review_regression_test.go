package debate

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type reviewStore struct {
	testStore
	cancel    func()
	unknown   bool
	failCalls int
	usage     ai.Usage
	meta      ai.Metadata
}

func (s *reviewStore) Complete(c context.Context, u uint64, d Debate, t Turn, r ai.Result) (Detail, error) {
	if s.cancel != nil {
		s.cancel()
		return Detail{}, dependency(context.Canceled, s.unknown)
	}
	return s.testStore.Complete(c, u, d, t, r)
}
func (s *reviewStore) Fail(c context.Context, u uint64, d Debate, t Turn, code string, usage ai.Usage, m ai.Metadata) (Turn, error) {
	s.failCalls++
	s.usage = usage
	s.meta = m
	return s.testStore.Fail(c, u, d, t, code, usage, m)
}
func TestReviewCancelDuringCompletion(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		s := &reviewStore{unknown: unknown}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		s.cancel = cancel
		service := NewService(s, ai.NewFake(), NewGate(nil))
		_, _, err := service.Submit(ctx, 1, 3, SubmitInput{1, "原文", "550e8400-e29b-41d4-a716-446655440000"})
		if err == nil {
			t.Fatal("cancellation reported success")
		}
		if unknown {
			if s.failCalls != 0 {
				t.Fatal("must not overwrite uncertain completion")
			}
		} else if s.failCalls != 1 || s.failCode != "generation_cancelled" || s.failContext != nil {
			t.Fatal("known cancellation did not persist failure independently")
		}
	}
}
func TestReviewCancellationPreservesUsage(t *testing.T) {
	for _, success := range []bool{false, true} {
		s := &reviewStore{}
		ctx, cancel := context.WithCancel(context.Background())
		usage := ai.Usage{PromptTokens: ai.KnownCount(100), CompletionTokens: ai.KnownCount(10)}
		meta := ai.Metadata{Model: "synthetic-model", PromptVersion: ai.PromptVersion, LatencyMS: 123}
		generator := genFunc(func(context.Context, ai.Request) (ai.Result, error) {
			cancel()
			if success {
				return ai.Result{Content: "成功后取消", Usage: usage, Metadata: meta}, nil
			}
			return ai.Result{}, &ai.Error{Code: ai.CodeModelCanceled, Usage: usage, Metadata: meta}
		})
		_, _, err := NewService(s, generator, NewGate(nil)).Submit(ctx, 1, 3, SubmitInput{1, "原文", "550e8400-e29b-41d4-a716-446655440000"})
		cancel()
		if err == nil || s.failCalls != 1 || s.usage != usage || s.meta != meta {
			t.Fatal("known model metadata discarded on cancellation")
		}
	}
}

type reviewAuthStore struct{ session auth.Session }

func TestReviewDeadlinePreservesUsage(t *testing.T) {
	s := &reviewStore{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	usage := ai.Usage{PromptTokens: ai.KnownCount(0), CompletionTokens: ai.KnownCount(10)}
	meta := ai.Metadata{Model: "synthetic-model", PromptVersion: ai.PromptVersion}
	g := genFunc(func(c context.Context, _ ai.Request) (ai.Result, error) {
		<-c.Done()
		return ai.Result{}, &ai.Error{Code: ai.CodeModelTimeout, Usage: usage, Metadata: meta}
	})
	_, _, err := NewService(s, g, NewGate(nil)).Submit(ctx, 1, 3, SubmitInput{1, "原文", "550e8400-e29b-41d4-a716-446655440000"})
	if err == nil || s.failCode != "model_timeout" || s.usage != usage || s.meta != meta {
		t.Fatal("timeout lost known zero/metadata")
	}
}

func (s reviewAuthStore) CreateUser(context.Context, string, string) (auth.User, error) {
	return auth.User{}, errors.New("unused")
}
func (s reviewAuthStore) FindUserByUsername(context.Context, string) (auth.User, error) {
	return auth.User{}, errors.New("unused")
}
func (s reviewAuthStore) FindUserByID(context.Context, uint64) (auth.User, error) {
	return auth.User{ID: 1, Username: "review_user"}, nil
}
func (s reviewAuthStore) CreateSession(context.Context, auth.Session) (uint64, error) {
	return 0, errors.New("unused")
}
func (s reviewAuthStore) FindSessionByTokenHash(_ context.Context, hash []byte) (auth.Session, error) {
	if !bytes.Equal(hash, s.session.TokenHash) {
		return auth.Session{}, auth.ErrNotFound
	}
	return s.session, nil
}
func (s reviewAuthStore) DeleteSession(context.Context, uint64) (bool, error) {
	return false, errors.New("unused")
}

func TestHTTPRetryAndPaginationRoutes(t *testing.T) {
	token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	hash := sha256.Sum256([]byte(token))
	csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
	now := time.Now().UTC()
	authStore := reviewAuthStore{auth.Session{ID: 1, UserID: 1, TokenHash: hash[:], CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
	a, err := auth.NewHandler(auth.NewService(authStore), auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatal(err)
	}
	mem := newMemoryStore(func() time.Time { return now })
	svc := NewService(mem, ai.NewFake(), NewGate(func() time.Time { return now }))
	defer svc.StopLeaseRecovery()

	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(svc).RegisterRoutes(router.Group("/api/v1"), a)

	// 空历史分页查询
	req := httptest.NewRequest("GET", "/api/v1/debates?page=1&page_size=20", nil)
	req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: token})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"items":[]`) || !strings.Contains(rec.Body.String(), `"total":0`) {
		t.Fatalf("empty list failed: code=%d body=%s", rec.Code, rec.Body.String())
	}

	// 非法分页参数返回 400
	for _, q := range []string{"page=0", "page_size=0", "page_size=51", "page=abc"} {
		req = httptest.NewRequest("GET", "/api/v1/debates?"+q, nil)
		req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: token})
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatalf("query %s got %d want 400", q, rec.Code)
		}
	}

	// 重试接口缺少 CSRF 返回 403，缺少或非法 expected_attempt_version 返回 400
	req = httptest.NewRequest("POST", "/api/v1/debates/101/turns/102/retry", strings.NewReader(`{"expected_attempt_version":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: token})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("retry missing csrf got %d want 403", rec.Code)
	}

	for _, body := range []string{`{}`, `{"expected_attempt_version":0}`, `{"expected_attempt_version":-1}`, `{"expected_attempt_version":"1"}`} {
		req = httptest.NewRequest("POST", "/api/v1/debates/101/turns/102/retry", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:5173")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: token})
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatalf("retry body %s got %d want 400", body, rec.Code)
		}
	}

	// 总结接口缺少 CSRF 返回 403，retry=true 缺少或非法 expected_attempt_version 返回 400
	req = httptest.NewRequest("POST", "/api/v1/debates/101/summary", strings.NewReader(`{"retry":false}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: token})
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatalf("summary missing csrf got %d want 403", rec.Code)
	}

	now = now.Add(2 * time.Minute)
	for _, body := range []string{`{"retry":true}`, `{"retry":true,"expected_attempt_version":0}`, `{"retry":true,"expected_attempt_version":-1}`, `{"retry":false,"expected_attempt_version":0}`} {
		req = httptest.NewRequest("POST", "/api/v1/debates/101/summary", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://127.0.0.1:5173")
		req.Header.Set("X-CSRF-Token", csrf)
		req.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: token})
		rec = httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != 400 {
			t.Fatalf("summary body %s got %d want 400", body, rec.Code)
		}
	}
}
func TestReviewInvalidHTTPRequestsAreRateLimited(t *testing.T) {
	for _, tc := range []struct {
		name, path, body string
		want             int
	}{{"whitespace", "3", `{"round_no":1,"content":"　","client_request_id":"550e8400-e29b-41d4-a716-446655440000"}`, 400}, {"json", "3", "{", 400}, {"id", "0", `{}`, 400}, {"large", "3", strings.Repeat(" ", 32769), 413}} {
		t.Run(tc.name, func(t *testing.T) {
			token := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
			hash := sha256.Sum256([]byte(token))
			csrf := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32))
			now := time.Now()
			store := reviewAuthStore{auth.Session{ID: 1, UserID: 1, TokenHash: hash[:], CSRFToken: csrf, CreatedAt: now, ExpiresAt: now.Add(time.Hour)}}
			a, err := auth.NewHandler(auth.NewService(store), auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
			if err != nil {
				t.Fatal(err)
			}
			gin.SetMode(gin.TestMode)
			router := gin.New()
			NewHandler(NewService(&testStore{}, ai.NewFake(), NewGate(nil))).RegisterRoutes(router.Group("/api/v1"), a)
			for i := 0; i < 7; i++ {
				request := httptest.NewRequest("POST", "/api/v1/debates/"+tc.path+"/turns", strings.NewReader(tc.body))
				request.Header.Set("Content-Type", "application/json")
				request.Header.Set("Origin", "http://127.0.0.1:5173")
				request.Header.Set("X-CSRF-Token", csrf)
				request.AddCookie(&http.Cookie{Name: auth.CookieNameDev, Value: token})
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				want := tc.want
				if i == 6 {
					want = 429
				}
				if response.Code != want {
					t.Fatalf("request %d got %d want %d", i+1, response.Code, want)
				}
				if i == 6 && response.Header().Get("Retry-After") == "" {
					t.Fatal("missing Retry-After")
				}
				io.Copy(io.Discard, response.Result().Body)
			}
		})
	}
}
