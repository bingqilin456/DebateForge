package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// 本文件的诊断只输出状态码、字段名、属性布尔值与数量。
// 禁止输出 Cookie/Set-Cookie/Header 原值、成功 Auth 响应正文、CSRF 或 Session 原值，
// 以免失败路径把可用凭据写进测试日志。
const (
	testOrigin   = "http://127.0.0.1:5173"
	httpPassword = "correct-horse-battery"
	httpUsername = "alice_01"
)

func newHandlerEngine(t *testing.T) (*gin.Engine, *fakeStore, *Service) {
	t.Helper()
	store := newFakeStore()
	service, _, _ := newTestService(t, store)
	return engineFor(t, newTestHandler(t, service, testOrigin)), store, service
}

func newTestHandler(t *testing.T, service *Service, origin string) *Handler {
	t.Helper()
	handler, err := NewHandler(service, HandlerConfig{Origin: origin})
	if err != nil {
		t.Fatalf("构造 Handler 失败: %v", err)
	}
	return handler
}

func engineFor(t *testing.T, handler *Handler) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	// 复用 platform 的 request_id 约定，保证信封与响应头一致。
	engine.Use(func(c *gin.Context) {
		c.Header("X-Request-ID", "req-test-1")
		c.Set("request_id", "req-test-1")
		c.Next()
	})
	handler.RegisterRoutes(engine.Group("/api/v1/auth"))
	return engine
}

func serve(engine *gin.Engine, request *http.Request) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	engine.ServeHTTP(recorder, request)
	return recorder
}

func jsonRequest(method, path, body string) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testOrigin)
	return request
}

// jsonKeys 只返回顶层键名，用于诊断，不暴露值。
func jsonKeys(body []byte) string {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		return "<non-object>"
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	return strings.Join(keys, ",")
}

func decodeEnvelope(t *testing.T, recorder *httptest.ResponseRecorder) (map[string]json.RawMessage, string, string) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
		Data      map[string]json.RawMessage `json:"data"`
		RequestID string                     `json:"request_id"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("响应不是 JSON 对象（长度 %d）", recorder.Body.Len())
	}
	return envelope.Data, envelope.Error.Code, envelope.RequestID
}

func assertNoStore(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if got := recorder.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control 必须为 no-store，实际为空=%v", got == "")
	}
}

func assertEnvelopeID(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	_, _, requestID := decodeEnvelope(t, recorder)
	if requestID == "" {
		t.Fatal("信封缺少 request_id")
	}
	if header := recorder.Header().Get("X-Request-ID"); header == "" || header != requestID {
		t.Fatalf("X-Request-ID 与信封不一致：header 为空=%v", header == "")
	}
}

func assertErrorResponse(t *testing.T, recorder *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	assertNoStore(t, recorder)
	if recorder.Code != status {
		t.Fatalf("status=%d want=%d 顶层键=%s", recorder.Code, status, jsonKeys(recorder.Body.Bytes()))
	}
	data, errorCode, _ := decodeEnvelope(t, recorder)
	if data != nil {
		t.Fatal("错误响应不应包含 data")
	}
	if errorCode != code {
		t.Fatalf("error.code 不匹配（want=%s）", code)
	}
	var envelope struct {
		Error struct{ Message string } `json:"error"`
	}
	_ = json.Unmarshal(recorder.Body.Bytes(), &envelope)
	if strings.TrimSpace(envelope.Error.Message) == "" {
		t.Fatal("error.message 为空")
	}
	assertEnvelopeID(t, recorder)
}

// assertData 校验成功信封，只报告字段数量与键名。
func assertData(t *testing.T, recorder *httptest.ResponseRecorder, status int) map[string]json.RawMessage {
	t.Helper()
	assertNoStore(t, recorder)
	if recorder.Code != status {
		t.Fatalf("status=%d want=%d 顶层键=%s", recorder.Code, status, jsonKeys(recorder.Body.Bytes()))
	}
	data, errorCode, _ := decodeEnvelope(t, recorder)
	if errorCode != "" {
		t.Fatalf("成功响应出现 error.code（长度 %d）", recorder.Body.Len())
	}
	if data == nil {
		t.Fatal("成功响应缺少 data")
	}
	assertEnvelopeID(t, recorder)
	return data
}

// sessionCookieOf 只返回 Cookie 指针供属性断言，调用方不得打印其值。
func sessionCookieOf(t *testing.T, recorder *httptest.ResponseRecorder, secure bool) *http.Cookie {
	t.Helper()
	name := CookieNameDev
	if secure {
		name = CookieNameSecure
	}
	found := 0
	var cookie *http.Cookie
	for _, candidate := range recorder.Result().Cookies() {
		if candidate.Name == name {
			found++
			cookie = candidate
		}
	}
	if found != 1 {
		t.Fatalf("Session Cookie 数量=%d，want=1", found)
	}
	return cookie
}

func registerUser(t *testing.T, engine *gin.Engine, username string) {
	t.Helper()
	recorder := serve(engine, jsonRequest("POST", "/api/v1/auth/register",
		`{"username":"`+username+`","password":"`+httpPassword+`"}`))
	if recorder.Code != http.StatusCreated {
		t.Fatalf("注册失败：status=%d 顶层键=%s", recorder.Code, jsonKeys(recorder.Body.Bytes()))
	}
}

func loginUser(t *testing.T, engine *gin.Engine, username string) (*http.Cookie, string) {
	t.Helper()
	recorder := serve(engine, jsonRequest("POST", "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+httpPassword+`"}`))
	data := assertData(t, recorder, http.StatusOK)
	var csrf string
	if err := json.Unmarshal(data["csrf_token"], &csrf); err != nil || csrf == "" {
		t.Fatal("登录响应缺少非空 csrf_token")
	}
	return sessionCookieOf(t, recorder, false), csrf
}

func TestRegisterHTTPContract(t *testing.T) {
	engine, store, _ := newHandlerEngine(t)
	recorder := serve(engine, jsonRequest("POST", "/api/v1/auth/register",
		`{"username":"  Alice_01  ","password":"`+httpPassword+`"}`))
	data := assertData(t, recorder, http.StatusCreated)
	if string(data["id"]) != `"101"` {
		t.Fatal("id 不是预期的十进制字符串")
	}
	if string(data["username"]) != `"alice_01"` {
		t.Fatal("username 未被归一化为小写")
	}
	if len(data) != 2 {
		t.Fatalf("注册响应字段数=%d，应只有 id 与 username", len(data))
	}
	// 只检查键名，不输出正文。
	for _, forbidden := range []string{"password", "hash", "token", "csrf"} {
		if _, exists := data[forbidden]; exists {
			t.Fatalf("注册响应出现敏感字段 %q", forbidden)
		}
	}
	if cookies := recorder.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("注册不应签发 Cookie，实际数量=%d", len(cookies))
	}
	if store.users["alice_01"].PasswordHash != "fake-hash" {
		t.Fatal("密码哈希未入库")
	}
}

func TestRegisterRejectsInvalidBodies(t *testing.T) {
	engine, store, _ := newHandlerEngine(t)
	cases := map[string]struct {
		body        string
		contentType string
		status      int
		errorCode   string
	}{
		"missing password": {`{"username":"alice_01"}`, "application/json", 400, "invalid_request"},
		"missing username": {`{"password":"` + httpPassword + `"}`, "application/json", 400, "invalid_request"},
		"empty object":     {`{}`, "application/json", 400, "invalid_request"},
		"not json":         {`username=alice_01`, "application/json", 400, "invalid_request"},
		"trailing json":    {`{"username":"alice_01","password":"x"} {"username":"bob_01"}`, "application/json", 400, "invalid_request"},
		"trailing garbage": {`{"username":"alice_01","password":"x"}x`, "application/json", 400, "invalid_request"},
		"json array":       {`[{"username":"alice_01","password":"x"}]`, "application/json", 400, "invalid_request"},
		"json null":        {`null`, "application/json", 400, "invalid_request"},
		"wrong media":      {`{"username":"alice_01","password":"` + httpPassword + `"}`, "text/plain", 400, "invalid_request"},
		"missing media":    {`{"username":"alice_01","password":"` + httpPassword + `"}`, "", 400, "invalid_request"},
		"invalid password": {`{"username":"alice_01","password":"short"}`, "application/json", 400, "invalid_request"},
		"invalid username": {`{"username":"a!","password":"` + httpPassword + `"}`, "application/json", 400, "invalid_request"},
		// 契约未裁定未知字段的拒绝策略，因此必须容忍而非 400。
		"unknown fields": {`{"username":"bob_01","password":"` + httpPassword + `","extra":{"nested":[1,2]}}`, "application/json", 201, ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(tc.body))
			if tc.contentType != "" {
				request.Header.Set("Content-Type", tc.contentType)
			}
			request.Header.Set("Origin", testOrigin)
			recorder := serve(engine, request)
			if tc.status == http.StatusCreated {
				assertData(t, recorder, http.StatusCreated)
				return
			}
			assertErrorResponse(t, recorder, tc.status, tc.errorCode)
		})
	}
	// 非法输入一律不得穿透到存储层；唯一成功用例是 unknown fields。
	if store.createUserCalls != 1 {
		t.Fatalf("存储层调用次数=%d，应只有 unknown fields 成功一次", store.createUserCalls)
	}
}

func TestRegisterRejectsInvalidUTF8(t *testing.T) {
	engine, store, _ := newHandlerEngine(t)
	body := []byte(`{"username":"alice_01","password":"`)
	body = append(body, 0xff, 0xfe, 0xfd)
	body = append(body, []byte(`"}`)...)
	request := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testOrigin)
	assertErrorResponse(t, serve(engine, request), 400, "invalid_request")
	if store.createUserCalls != 0 {
		t.Fatal("非法 UTF-8 到达存储层")
	}
}

func TestRegisterBodyLimitWithUnknownContentLength(t *testing.T) {
	engine, store, _ := newHandlerEngine(t)
	prefix := `{"username":"body_size","password":"` + httpPassword + `","pad":"`
	suffix := `"}`
	// 恰好 32 KiB 的单文档必须被接受（未知 Content-Length，模拟分块上传）。
	padding := strings.Repeat("a", MaxRequestBodyBytes-len(prefix)-len(suffix))
	body := prefix + padding + suffix
	if len(body) != MaxRequestBodyBytes {
		t.Fatalf("构造的正文长度=%d", len(body))
	}
	request := httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testOrigin)
	request.ContentLength = -1
	assertData(t, serve(engine, request), http.StatusCreated)

	// 多一个字节必须 413，且不产生新的用户。
	request = httptest.NewRequest("POST", "/api/v1/auth/register", strings.NewReader(body+"a"))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", testOrigin)
	request.ContentLength = -1
	before := store.createUserCalls
	assertErrorResponse(t, serve(engine, request), http.StatusRequestEntityTooLarge, "request_too_large")
	if store.createUserCalls != before {
		t.Fatal("超限请求到达存储层")
	}
}

func TestPOSTRequiresExactOrigin(t *testing.T) {
	engine, store, _ := newHandlerEngine(t)
	for name, tc := range map[string]struct {
		origin  string
		present bool
		accept  bool
	}{
		"missing":       {"", false, false},
		"empty":         {"", true, false},
		"null":          {"null", true, false},
		"wrong port":    {"http://127.0.0.1:5174", true, false},
		"wrong scheme":  {"https://127.0.0.1:5173", true, false},
		"with path":     {"http://127.0.0.1:5173/", true, false},
		"with query":    {"http://127.0.0.1:5173/?a=1", true, false},
		"trailing junk": {"http://127.0.0.1:5173 #x", true, false},
		"userinfo":      {"http://user@127.0.0.1:5173", true, false},
		"exact":         {testOrigin, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			username := "origin_" + strings.ReplaceAll(name, " ", "_")
			request := httptest.NewRequest("POST", "/api/v1/auth/register",
				strings.NewReader(`{"username":"`+username+`","password":"`+httpPassword+`"}`))
			request.Header.Set("Content-Type", "application/json")
			if tc.present {
				request.Header.Set("Origin", tc.origin)
			}
			recorder := serve(engine, request)
			if tc.accept {
				assertData(t, recorder, http.StatusCreated)
				return
			}
			assertErrorResponse(t, recorder, http.StatusForbidden, "csrf_failed")
		})
	}
	if store.createUserCalls != 1 {
		t.Fatalf("存储层调用次数=%d，应只有精确 Origin 用例成功", store.createUserCalls)
	}
}

func TestRegisterDuplicateUsernameIsConflict(t *testing.T) {
	engine, _, _ := newHandlerEngine(t)
	registerUser(t, engine, httpUsername)
	recorder := serve(engine, jsonRequest("POST", "/api/v1/auth/register",
		`{"username":"ALICE_01","password":"`+httpPassword+`"}`))
	assertErrorResponse(t, recorder, http.StatusConflict, "username_conflict")
}

func TestLoginHTTPContract(t *testing.T) {
	engine, _, _ := newHandlerEngine(t)
	registerUser(t, engine, httpUsername)
	recorder := serve(engine, jsonRequest("POST", "/api/v1/auth/login",
		`{"username":"Alice_01","password":"`+httpPassword+`"}`))
	data := assertData(t, recorder, http.StatusOK)
	if len(data) != 2 {
		t.Fatalf("登录响应字段数=%d，应只有 user 与 csrf_token", len(data))
	}
	var csrf string
	if err := json.Unmarshal(data["csrf_token"], &csrf); err != nil || csrf == "" {
		t.Fatal("缺少非空 csrf_token")
	}
	var user UserDTO
	if err := json.Unmarshal(data["user"], &user); err != nil || user.Username != httpUsername || user.ID == "" {
		t.Fatal("user 字段结构不符合契约")
	}
	cookie := sessionCookieOf(t, recorder, false)
	if cookie.Value == csrf {
		t.Fatal("Session 原值与 CSRF 相同")
	}
	if cookie.Secure {
		t.Fatal("开发环境 Cookie 不应带 Secure")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("Cookie 属性不符：HttpOnly=%v Lax=%v Path=/=%v DomainEmpty=%v",
			cookie.HttpOnly, cookie.SameSite == http.SameSiteLaxMode, cookie.Path == "/", cookie.Domain == "")
	}
	if cookie.MaxAge != int(DefaultSessionTTL/time.Second) {
		t.Fatalf("Max-Age=%d", cookie.MaxAge)
	}
	if cookie.Expires.IsZero() || time.Until(cookie.Expires) < DefaultSessionTTL-time.Minute {
		t.Fatal("Expires 不是约 24 小时后")
	}
	// 只断言字段名白名单，不输出正文。
	for _, forbidden := range []string{"token", "password", "session"} {
		if _, exists := data[forbidden]; exists {
			t.Fatalf("登录响应出现敏感字段 %q", forbidden)
		}
	}
}

func TestLoginRejectsBadCredentialsUniformly(t *testing.T) {
	engine, _, _ := newHandlerEngine(t)
	registerUser(t, engine, httpUsername)
	wrongPassword := serve(engine, jsonRequest("POST", "/api/v1/auth/login",
		`{"username":"alice_01","password":"wrong-password-1"}`))
	unknownUser := serve(engine, jsonRequest("POST", "/api/v1/auth/login",
		`{"username":"nobody_01","password":"wrong-password-1"}`))
	assertErrorResponse(t, wrongPassword, http.StatusUnauthorized, "invalid_credentials")
	assertErrorResponse(t, unknownUser, http.StatusUnauthorized, "invalid_credentials")
	var first, second struct {
		Error struct{ Message string } `json:"error"`
	}
	_ = json.Unmarshal(wrongPassword.Body.Bytes(), &first)
	_ = json.Unmarshal(unknownUser.Body.Bytes(), &second)
	if first.Error.Message != second.Error.Message {
		t.Fatal("未知账号与错误密码的提示不同")
	}
	for _, recorder := range []*httptest.ResponseRecorder{wrongPassword, unknownUser} {
		if count := len(recorder.Result().Cookies()); count != 0 {
			t.Fatalf("登录失败签发 Cookie 数量=%d", count)
		}
	}
}

func TestAnonymousEndpointsIgnoreStaleCookieAndCSRF(t *testing.T) {
	engine, _, _ := newHandlerEngine(t)
	// 固定的无意义旧值，不对应任何真实 Session。
	stale := &http.Cookie{Name: CookieNameDev, Value: strings.Repeat("z", 43), Path: "/"}

	register := jsonRequest("POST", "/api/v1/auth/register",
		`{"username":"alice_01","password":"`+httpPassword+`"}`)
	register.AddCookie(stale)
	assertData(t, serve(engine, register), http.StatusCreated)

	login := jsonRequest("POST", "/api/v1/auth/login",
		`{"username":"alice_01","password":"`+httpPassword+`"}`)
	login.AddCookie(stale)
	// 即使带旧 Cookie 也不要求 CSRF，仍应签发新 Session。
	assertData(t, serve(engine, login), http.StatusOK)
}

func TestMeRequiresValidSession(t *testing.T) {
	engine, _, _ := newHandlerEngine(t)
	registerUser(t, engine, httpUsername)

	for name, cookies := range map[string][]*http.Cookie{
		"missing": nil,
		"forged":  {{Name: CookieNameDev, Value: strings.Repeat("A", 43), Path: "/"}},
		"garbage": {{Name: CookieNameDev, Value: "!!!", Path: "/"}},
		"duplicate": {
			{Name: CookieNameDev, Value: strings.Repeat("A", 43), Path: "/"},
			{Name: CookieNameDev, Value: strings.Repeat("B", 43), Path: "/"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
			for _, cookie := range cookies {
				request.AddCookie(cookie)
			}
			assertErrorResponse(t, serve(engine, request), http.StatusUnauthorized, "unauthenticated")
		})
	}

	cookie, csrf := loginUser(t, engine, httpUsername)
	request := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	request.AddCookie(cookie)
	// me 是 GET：不要求 Origin 或 CSRF。
	data := assertData(t, serve(engine, request), http.StatusOK)
	if len(data) != 2 {
		t.Fatalf("me 响应字段数=%d，应只有 user 与 csrf_token", len(data))
	}
	var restored string
	if err := json.Unmarshal(data["csrf_token"], &restored); err != nil || restored != csrf {
		t.Fatal("me 未恢复当前 Session 的 CSRF")
	}
	var user UserDTO
	if err := json.Unmarshal(data["user"], &user); err != nil || user.Username != httpUsername {
		t.Fatal("me 未恢复用户")
	}
}

func TestCurrentUserIsTypedInGinContext(t *testing.T) {
	engine, _, service := newHandlerEngine(t)
	registerUser(t, engine, httpUsername)
	cookie, csrf := loginUser(t, engine, httpUsername)

	handler := newTestHandler(t, service, testOrigin)
	// 复用公开中间件验证 typed Context：业务模块后续可按同样方式读取。
	engine.GET("/internal/current", handler.RequireAuth(), func(c *gin.Context) {
		current, ok := CurrentUserFrom(c)
		if !ok {
			c.JSON(http.StatusInternalServerError, gin.H{"typed": false})
			return
		}
		// 不返回任何 Hash：Service 已清空，这里显式断言。
		c.JSON(http.StatusOK, gin.H{
			"typed":        true,
			"username":     current.User.Username,
			"has_hash":     current.User.PasswordHash != "",
			"has_csrf":     current.Session.CSRFToken != "",
			"session_id":   current.Session.ID,
			"csrf_matches": equalCSRF(current.Session.CSRFToken, csrf),
		})
	})
	request := httptest.NewRequest("GET", "/internal/current", nil)
	request.AddCookie(cookie)
	recorder := serve(engine, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status=%d", recorder.Code)
	}
	var body struct {
		Typed       bool   `json:"typed"`
		Username    string `json:"username"`
		HasHash     bool   `json:"has_hash"`
		HasCSRF     bool   `json:"has_csrf"`
		SessionID   uint64 `json:"session_id"`
		CSRFMatches bool   `json:"csrf_matches"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal("内部声明式响应不是 JSON")
	}
	if !body.Typed || body.Username != httpUsername || body.HasHash || !body.HasCSRF || body.SessionID == 0 || !body.CSRFMatches {
		t.Fatalf("typed=%v username_ok=%v has_hash=%v has_csrf=%v session_id_nonzero=%v csrf_matches=%v",
			body.Typed, body.Username == httpUsername, body.HasHash, body.HasCSRF, body.SessionID != 0, body.CSRFMatches)
	}
}

func TestLogoutHTTPFlow(t *testing.T) {
	engine, _, _ := newHandlerEngine(t)
	registerUser(t, engine, httpUsername)
	cookie, csrf := loginUser(t, engine, httpUsername)
	secondCookie, _ := loginUser(t, engine, httpUsername)

	for name, tc := range map[string]struct {
		cookie *http.Cookie
		csrf   string
		origin string
		status int
		code   string
	}{
		"missing csrf": {cookie, "", testOrigin, 403, "csrf_failed"},
		"wrong csrf":   {cookie, "wrong-csrf-token", testOrigin, 403, "csrf_failed"},
		"wrong origin": {cookie, csrf, "http://127.0.0.1:5174", 403, "csrf_failed"},
		"no session":   {nil, csrf, testOrigin, 401, "unauthenticated"},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
			request.Header.Set("Origin", tc.origin)
			if tc.csrf != "" {
				request.Header.Set("X-CSRF-Token", tc.csrf)
			}
			if tc.cookie != nil {
				request.AddCookie(tc.cookie)
			}
			assertErrorResponse(t, serve(engine, request), tc.status, tc.code)
		})
	}

	request := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
	request.Header.Set("Origin", testOrigin)
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(cookie)
	recorder := serve(engine, request)
	assertNoStore(t, recorder)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("退出 status=%d want=204", recorder.Code)
	}
	if recorder.Body.Len() != 0 {
		t.Fatalf("退出正文长度=%d，必须为空", recorder.Body.Len())
	}
	if recorder.Header().Get("X-Request-ID") == "" {
		t.Fatal("204 响应缺少 X-Request-ID")
	}
	if recorder.Header().Get("Content-Type") != "" {
		t.Fatalf("204 不应带 Content-Type")
	}
	cleared := sessionCookieOf(t, recorder, false)
	if cleared.Value != "" || cleared.MaxAge >= 0 {
		t.Fatalf("退出未清除 Cookie：value 为空=%v MaxAge<0=%v", cleared.Value == "", cleared.MaxAge < 0)
	}
	if cleared.Path != "/" || cleared.Domain != "" {
		t.Fatalf("清除 Cookie 必须同路径且无 Domain：path=/=%v domain 为空=%v", cleared.Path == "/", cleared.Domain == "")
	}
	// 当前 Session 失效，另一个 Session 仍有效。
	me := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	me.AddCookie(cookie)
	assertErrorResponse(t, serve(engine, me), http.StatusUnauthorized, "unauthenticated")
	me = httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	me.AddCookie(secondCookie)
	assertData(t, serve(engine, me), http.StatusOK)
}

func TestLogoutRejectsOversizedBody(t *testing.T) {
	engine, _, _ := newHandlerEngine(t)
	registerUser(t, engine, httpUsername)
	cookie, csrf := loginUser(t, engine, httpUsername)
	request := httptest.NewRequest("POST", "/api/v1/auth/logout",
		strings.NewReader(strings.Repeat("a", MaxRequestBodyBytes+1)))
	request.Header.Set("Origin", testOrigin)
	request.Header.Set("X-CSRF-Token", csrf)
	request.AddCookie(cookie)
	request.ContentLength = -1
	assertErrorResponse(t, serve(engine, request), http.StatusRequestEntityTooLarge, "request_too_large")
}

func TestSecureCookiePolicyUsesHostPrefix(t *testing.T) {
	store := newFakeStore()
	service, _, _ := newTestService(t, store)
	const secureOrigin = "https://siyige.example"
	handler := newTestHandler(t, service, secureOrigin)
	engine := engineFor(t, handler)

	register := httptest.NewRequest("POST", "/api/v1/auth/register",
		strings.NewReader(`{"username":"alice_01","password":"`+httpPassword+`"}`))
	register.Header.Set("Content-Type", "application/json")
	register.Header.Set("Origin", secureOrigin)
	assertData(t, serve(engine, register), http.StatusCreated)

	login := httptest.NewRequest("POST", "/api/v1/auth/login",
		strings.NewReader(`{"username":"alice_01","password":"`+httpPassword+`"}`))
	login.Header.Set("Content-Type", "application/json")
	login.Header.Set("Origin", secureOrigin)
	recorder := serve(engine, login)
	assertData(t, recorder, http.StatusOK)
	cookie := sessionCookieOf(t, recorder, true)
	if !cookie.Secure {
		t.Fatal("HTTPS 策略 Cookie 缺少 Secure")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" {
		t.Fatalf("__Host- Cookie 属性不符：HttpOnly=%v Lax=%v path=/=%v domain 为空=%v",
			cookie.HttpOnly, cookie.SameSite == http.SameSiteLaxMode, cookie.Path == "/", cookie.Domain == "")
	}
}

func TestRateLimitResponsesCarryRetryAfter(t *testing.T) {
	engine, store, _ := newHandlerEngine(t)
	for attempt := 0; attempt < RegisterIPLimit; attempt++ {
		registerUser(t, engine, "rate_"+string(rune('a'+attempt)))
	}
	before := store.createUserCalls
	request := jsonRequest("POST", "/api/v1/auth/register",
		`{"username":"rate_full","password":"`+httpPassword+`"}`)
	// 伪造代理头不能绕过按对端 IP 的限流。
	request.Header.Set("X-Forwarded-For", "203.0.113.9")
	request.Header.Set("X-Real-IP", "203.0.113.9")
	recorder := serve(engine, request)
	assertErrorResponse(t, recorder, http.StatusTooManyRequests, "rate_limited")
	retryAfter := recorder.Header().Get("Retry-After")
	seconds, err := strconv.Atoi(retryAfter)
	if err != nil || seconds < 1 || seconds > int(RegisterIPWindow/time.Second) {
		t.Fatalf("Retry-After 必须是 1..3600 的正整数（可解析=%v）", err == nil)
	}
	if store.createUserCalls != before {
		t.Fatal("被限流的请求到达存储层")
	}
}

func TestDependencyFailureAndBusyMapTo503(t *testing.T) {
	engine, store, service := newHandlerEngine(t)
	store.createUserErr = storageFailure{}
	recorder := serve(engine, jsonRequest("POST", "/api/v1/auth/register",
		`{"username":"alice_01","password":"`+httpPassword+`"}`))
	assertErrorResponse(t, recorder, http.StatusServiceUnavailable, "dependency_unavailable")
	if count := len(recorder.Result().Cookies()); count != 0 {
		t.Fatalf("存储失败签发 Cookie 数量=%d", count)
	}

	// 哈希槽满按现有 ErrAuthBusy 映射为 503，而不是排队等待。
	service.hashSlots = make(chan struct{}, 1)
	service.hashSlots <- struct{}{}
	recorder = serve(engine, jsonRequest("POST", "/api/v1/auth/register",
		`{"username":"bob_01","password":"`+httpPassword+`"}`))
	assertErrorResponse(t, recorder, http.StatusServiceUnavailable, "dependency_unavailable")
}

func TestPOSTRequiresExactlyOneOriginHeader(t *testing.T) {
	engine, store, _ := newHandlerEngine(t)
	for name, tc := range map[string][]string{
		// 首个合法、第二个恶意：代理合并或歧义解释都可能放行，必须拒绝。
		"valid then evil":     {testOrigin, "http://attacker.example"},
		"evil then valid":     {"http://attacker.example", testOrigin},
		"duplicate valid":     {testOrigin, testOrigin},
		"comma merged single": {"http://attacker.example, " + testOrigin},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/v1/auth/register",
				strings.NewReader(`{"username":"dup_origin","password":"`+httpPassword+`"}`))
			request.Header.Set("Content-Type", "application/json")
			for _, value := range tc {
				request.Header.Add("Origin", value)
			}
			assertErrorResponse(t, serve(engine, request), http.StatusForbidden, "csrf_failed")
		})
	}
	if store.createUserCalls != 0 {
		t.Fatalf("重复 Origin 到达存储层，调用次数=%d", store.createUserCalls)
	}
}

func TestLogoutRejectsDuplicateCSRFHeader(t *testing.T) {
	engine, _, _ := newHandlerEngine(t)
	registerUser(t, engine, httpUsername)
	cookie, csrf := loginUser(t, engine, httpUsername)
	for name, values := range map[string][]string{
		"valid then evil": {csrf, "attacker-csrf"},
		"evil then valid": {"attacker-csrf", csrf},
		"duplicate valid": {csrf, csrf},
		"comma merged":    {csrf + ",attacker-csrf"},
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/api/v1/auth/logout", nil)
			request.Header.Set("Origin", testOrigin)
			for _, value := range values {
				request.Header.Add("X-CSRF-Token", value)
			}
			request.AddCookie(cookie)
			assertErrorResponse(t, serve(engine, request), http.StatusForbidden, "csrf_failed")
		})
	}
	// 重复 CSRF 头被拒后，原 Session 必须仍然有效。
	me := httptest.NewRequest("GET", "/api/v1/auth/me", nil)
	me.AddCookie(cookie)
	assertData(t, serve(engine, me), http.StatusOK)
}

func TestNewHandlerRejectsInvalidOrigin(t *testing.T) {
	service, _, _ := newTestService(t, newFakeStore())
	for _, origin := range []string{
		"", "http://127.0.0.1:5173/", "ftp://example.com", "http://0.0.0.0:5173",
		"http://example.com:80", "http://127.0.0.1:5173?x=1", "http://user@127.0.0.1:5173",
		"https://siyige.example/path/", "http://127.0.0.1:0",
		"http://127.0.0.1:70000", "//127.0.0.1:5173",
	} {
		if _, err := NewHandler(service, HandlerConfig{Origin: origin}); err == nil {
			t.Fatalf("接受了非法 Origin（长度=%d）", len(origin))
		}
	}
	for _, origin := range []string{"http://127.0.0.1:5173", "https://siyige.example", "http://[::1]:5173", "https://127.0.0.1:8443"} {
		if _, err := NewHandler(service, HandlerConfig{Origin: origin}); err != nil {
			t.Fatalf("拒绝了合法 Origin（长度=%d）: %v", len(origin), err)
		}
	}
}
