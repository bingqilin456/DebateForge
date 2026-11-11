package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
)

// unavailableStore 让认证路由在没有真实 MySQL 时验证错误映射：
// 所有操作都返回存储故障，Service 必须映射为 503 dependency_unavailable。
type unavailableStore struct{}

var errStoreUnavailable = errors.New("store unavailable")

func (unavailableStore) CreateUser(context.Context, string, string) (auth.User, error) {
	return auth.User{}, errStoreUnavailable
}
func (unavailableStore) FindUserByUsername(context.Context, string) (auth.User, error) {
	return auth.User{}, errStoreUnavailable
}
func (unavailableStore) FindUserByID(context.Context, uint64) (auth.User, error) {
	return auth.User{}, errStoreUnavailable
}
func (unavailableStore) CreateSession(context.Context, auth.Session) (uint64, error) {
	return 0, errStoreUnavailable
}
func (unavailableStore) FindSessionByTokenHash(context.Context, []byte) (auth.Session, error) {
	return auth.Session{}, errStoreUnavailable
}
func (unavailableStore) DeleteSession(context.Context, uint64) (bool, error) {
	return false, errStoreUnavailable
}

func testAuthHandler(t *testing.T) *auth.Handler {
	t.Helper()
	service := auth.NewService(unavailableStore{})
	handler, err := auth.NewHandler(service, auth.HandlerConfig{Origin: "http://127.0.0.1:5173"})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func TestRouterRegistersAuthRoutesWhenHandlerProvided(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), nil, testAuthHandler(t))

	// me 无 Cookie：401 unauthenticated，不访问数据库。
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("GET", "/api/v1/auth/me", nil))
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("me status=%d want=401", recorder.Code)
	}
	var envelope struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != "unauthenticated" {
		t.Fatal("me 未返回 unauthenticated 信封")
	}

	// 注册缺少 Origin：403 csrf_failed，早于任何数据库访问。
	recorder = httptest.NewRecorder()
	request := httptest.NewRequest("POST", "/api/v1/auth/register",
		strings.NewReader(`{"username":"alice_01","password":"correct-horse-battery"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("缺少 Origin status=%d", recorder.Code)
	}

	// Origin 正确但数据库不可用：503 dependency_unavailable，且不签发 Cookie。
	recorder = httptest.NewRecorder()
	request = httptest.NewRequest("POST", "/api/v1/auth/register",
		strings.NewReader(`{"username":"alice_01","password":"correct-horse-battery"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:5173")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("数据库不可用 status=%d want=503", recorder.Code)
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != "dependency_unavailable" {
		t.Fatal("数据库不可用未返回受控依赖错误")
	}
	if count := len(recorder.Result().Cookies()); count != 0 {
		t.Fatalf("失败响应签发 Cookie 数量=%d", count)
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("认证响应缺少 X-Request-ID")
	}
	if strings.Contains(logs.String(), "store unavailable") {
		t.Fatal("日志泄露内部存储错误")
	}
}

func TestRouterUnmatchedAndMethodErrorsAreNoStore(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), nil, testAuthHandler(t))
	for _, tc := range []struct {
		method, path string
		status       int
		code         string
	}{
		{"GET", "/api/v1/missing", 404, "not_found"},
		// 已注册的 POST 路径用 GET 请求：契约要求 405 而不是 404。
		{"GET", "/api/v1/auth/login", 405, "method_not_allowed"},
		{"DELETE", "/api/v1/auth/register", 405, "method_not_allowed"},
		{"POST", "/api/health/live", 405, "method_not_allowed"},
		// 未注册的认证子路径仍是 404。
		{"POST", "/api/v1/auth/unknown", 404, "not_found"},
	} {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(tc.method, tc.path, nil))
		if recorder.Code != tc.status {
			t.Fatalf("%s %s status=%d want=%d", tc.method, tc.path, recorder.Code, tc.status)
		}
		var envelope struct {
			Error struct{ Code string } `json:"error"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || envelope.Error.Code != tc.code {
			t.Fatalf("%s %s error.code 不匹配", tc.method, tc.path)
		}
		if recorder.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s %s 缺少 no-store", tc.method, tc.path)
		}
	}
}

func TestRouterWithoutAuthHandlerKeepsHealthOnly(t *testing.T) {
	var logs bytes.Buffer
	router := NewRouter(slog.New(slog.NewJSONHandler(&logs, nil)), nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest("POST", "/api/v1/auth/login", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("无 Handler 时 status=%d want=404", recorder.Code)
	}
}
