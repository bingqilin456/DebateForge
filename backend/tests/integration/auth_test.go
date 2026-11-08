package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/bingqilin456/DebateForge/backend/internal/platform"
)

// 本文件的所有失败诊断只输出状态码、字段名、数量与属性布尔值。
// 禁止输出 Cookie/Set-Cookie/Header 原值、成功 Auth 响应正文、CSRF 或 Session 原值。
const (
	integrationOrigin   = "http://127.0.0.1:5173"
	integrationPassword = "integration-password-01"
)

// authFixture 用真实 GORM 连接、真实 Argon2id 与完整 Gin 路由搭建隔离的认证服务。
// 夹具只记录“本测试成功创建”的确切 user/session ID，清理时绝不影响既有数据。
type authFixture struct {
	db       *platform.Database
	server   *httptest.Server
	client   *http.Client
	logs     *bytes.Buffer
	service  *auth.Service
	handler  *auth.Handler
	username string
	userIDs  []uint64
	sessIDs  []uint64
	cleaned  bool
	// concurrentClient 不共享 Cookie，用于并发请求；带 10 秒超时避免无界等待。
	concurrentClient *http.Client
}

// newAuthFixture 复用 openTestDatabase 的连接前白名单与连接后身份校验。
// 每个测试使用新 Service 与限流器，避免上一个用例的进程内计数影响下一个。
func newAuthFixture(t *testing.T) *authFixture {
	t.Helper()
	db := openTestDatabase(t)
	fixture := &authFixture{db: db, logs: &bytes.Buffer{}}
	fixture.rebuild(t)
	t.Cleanup(func() { fixture.cleanup(t) })
	return fixture
}

// rebuild 用新的 Service/Handler/Router 重建 HTTP 端，模拟进程重启与连接池重建。
func (fixture *authFixture) rebuild(t *testing.T) {
	t.Helper()
	if fixture.server != nil {
		fixture.server.Close()
	}
	// 真实 Repository + 真实 Argon2id + 进程内限流器，日志写入可断言的缓冲区。
	fixture.logs.Reset()
	logger := slog.New(slog.NewJSONHandler(fixture.logs, nil))
	fixture.service = auth.NewService(auth.NewRepository(fixture.db.ORM), auth.WithLimiter(auth.NewLimiter(nil, 0)))
	handler, err := auth.NewHandler(fixture.service, auth.HandlerConfig{Origin: integrationOrigin, Logger: logger})
	if err != nil {
		t.Fatal(err)
	}
	fixture.handler = handler
	router := platform.NewRouter(logger, fixture.db, handler)
	fixture.server = httptest.NewServer(router)
	// 不使用 CookieJar：测试显式传递 Cookie，避免 jar 与显式值叠加成重复 Cookie，
	// 从而误触发“拒绝重复同名 Session Cookie”的保护。
	fixture.client = &http.Client{Timeout: 10 * time.Second}
	fixture.concurrentClient = &http.Client{Timeout: 10 * time.Second}
}

// cleanup 是幂等收尾：关闭 HTTP 端并用独立连接删除本轮记录。
// 独立连接必须重新通过测试目标白名单与实际数据库/账户身份校验，
// 避免测试中途替换或关闭 fixture.db 后清理失效，进而残留夹具。
func (fixture *authFixture) cleanup(t *testing.T) {
	t.Helper()
	if fixture.cleaned {
		return
	}
	fixture.cleaned = true
	if fixture.server != nil {
		fixture.server.Close()
		fixture.server = nil
	}
	if err := databaseConfig.ValidateTestTarget(); err != nil {
		t.Errorf("清理连接的配置不符合测试白名单")
		return
	}
	cleaner, err := platform.OpenDatabase(databaseConfig)
	if err != nil {
		t.Errorf("清理时无法建立测试连接")
		return
	}
	defer cleaner.Close()
	ctx, cancel := context.WithTimeout(context.Background(), platform.DatabaseTimeout)
	defer cancel()
	var identity struct {
		Database string
		Account  string
	}
	if err := cleaner.ORM.WithContext(ctx).Raw("SELECT DATABASE() AS `database`, CURRENT_USER() AS account").Scan(&identity).Error; err != nil {
		t.Errorf("清理连接无法验证身份")
		return
	}
	if identity.Database != "siyige_test" || identity.Account != "siyige_test_app@%" {
		t.Errorf("清理连接目标不符，拒绝删除")
		return
	}
	// 只删除本测试成功创建的确切 ID，先 Session 后 User；绝不批量清空或删除既有数据。
	for _, id := range fixture.sessIDs {
		if err := cleaner.ORM.Exec("DELETE FROM auth_sessions WHERE id = ?", id).Error; err != nil {
			t.Errorf("清理本轮 Session id=%d 失败", id)
		}
	}
	for _, id := range fixture.userIDs {
		if err := cleaner.ORM.Exec("DELETE FROM users WHERE id = ?", id).Error; err != nil {
			t.Errorf("清理本轮 User id=%d 失败", id)
		}
	}
	fixture.sessIDs = nil
	fixture.userIDs = nil
}

type apiResult struct {
	status  int
	body    []byte
	cookies []*http.Cookie
	header  http.Header
}

func (result apiResult) keys() string {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(result.body, &object); err != nil {
		return "<non-object>"
	}
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	return strings.Join(keys, ",")
}

func (result apiResult) errorCode() string {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	_ = json.Unmarshal(result.body, &envelope)
	return envelope.Error.Code
}

func (result apiResult) data() map[string]json.RawMessage {
	var envelope struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(result.body, &envelope)
	return envelope.Data
}

func (fixture *authFixture) do(t *testing.T, method, path, body, origin, csrf string, cookies ...*http.Cookie) apiResult {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, fixture.server.URL+path, reader)
	if err != nil {
		t.Fatal(err)
	}
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	if csrf != "" {
		request.Header.Set("X-CSRF-Token", csrf)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response, err := fixture.client.Do(request)
	if err != nil {
		t.Fatalf("%s %s 请求失败（网络层）", method, path)
	}
	defer response.Body.Close()
	payload, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("%s %s 读取响应失败", method, path)
	}
	return apiResult{status: response.StatusCode, body: payload, cookies: response.Cookies(), header: response.Header}
}

func (fixture *authFixture) randomUsername(t *testing.T) string {
	t.Helper()
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		t.Fatal(err)
	}
	// t03 前缀 + 16 位十六进制 = 19 字符，满足 3..32 ASCII 约束且便于识别清理范围。
	return "t03" + hex.EncodeToString(buffer)
}

func (fixture *authFixture) register(t *testing.T, username, password string) (apiResult, uint64) {
	t.Helper()
	result := fixture.do(t, "POST", "/api/v1/auth/register",
		`{"username":"`+username+`","password":"`+password+`"}`, integrationOrigin, "")
	if result.status != http.StatusCreated {
		return result, 0
	}
	// data 就是 UserDTO：id 是十进制字符串。注册已写入数据库，因此无论响应解析是否成功，
	// 都必须立刻确定该行 ID 以便清理，绝不留下本轮夹具。
	normalized := strings.ToLower(strings.TrimSpace(username))
	var idText string
	if err := json.Unmarshal(result.data()["id"], &idText); err != nil || idText == "" {
		// 响应异常时按本轮唯一随机用户名精确反查，只可能命中本次创建的行。
		fallback := fixture.userIDByUsername(t, normalized)
		t.Fatalf("注册响应 id 无法解析（已登记本轮 user id=%d 供清理）", fallback)
	}
	id, parseErr := strconv.ParseUint(idText, 10, 64)
	if parseErr != nil || id == 0 {
		fallback := fixture.userIDByUsername(t, normalized)
		t.Fatalf("注册响应 id 不是十进制字符串（已登记本轮 user id=%d 供清理）", fallback)
	}
	fixture.userIDs = append(fixture.userIDs, id)
	return result, id
}

// userIDByUsername 按唯一随机用户名精确查回本轮创建的用户 ID 并登记清理。
func (fixture *authFixture) userIDByUsername(t *testing.T, username string) uint64 {
	t.Helper()
	var id uint64
	if err := fixture.db.ORM.Raw("SELECT id FROM users WHERE username = ?", username).Scan(&id).Error; err != nil || id == 0 {
		t.Errorf("无法按本轮随机用户名回查用户 ID")
		return 0
	}
	fixture.userIDs = append(fixture.userIDs, id)
	return id
}

func (fixture *authFixture) login(t *testing.T, username, password string) (apiResult, *http.Cookie, string) {
	t.Helper()
	result := fixture.do(t, "POST", "/api/v1/auth/login",
		`{"username":"`+username+`","password":"`+password+`"}`, integrationOrigin, "")
	if result.status != http.StatusOK {
		return result, nil, ""
	}
	var csrf string
	if err := json.Unmarshal(result.data()["csrf_token"], &csrf); err != nil || csrf == "" {
		t.Fatal("登录响应缺少非空 csrf_token")
	}
	var sessionCookie *http.Cookie
	for _, cookie := range result.cookies {
		if cookie.Name == auth.CookieNameDev {
			sessionCookie = cookie
		}
	}
	if sessionCookie == nil {
		t.Fatal("登录未签发开发 Session Cookie")
	}
	return result, sessionCookie, csrf
}

// sessionIDByToken 用 SHA-256 摘要查询本轮创建的 Session ID，便于精确清理。
func (fixture *authFixture) sessionIDByToken(t *testing.T, token string) uint64 {
	t.Helper()
	digest := sha256.Sum256([]byte(token))
	var id uint64
	if err := fixture.db.ORM.Raw("SELECT id FROM auth_sessions WHERE token_hash = ?", digest[:]).Scan(&id).Error; err != nil {
		t.Fatal("查询本轮 Session 失败")
	}
	if id == 0 {
		t.Fatal("未找到本轮创建的 Session")
	}
	fixture.sessIDs = append(fixture.sessIDs, id)
	return id
}

func assertNoStore(t *testing.T, result apiResult) {
	t.Helper()
	if got := result.header.Get("Cache-Control"); got != "no-store" {
		t.Fatal("响应缺少 Cache-Control: no-store")
	}
}

func assertRequestID(t *testing.T, result apiResult) {
	t.Helper()
	var envelope struct {
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(result.body, &envelope); err != nil {
		t.Fatal("响应不是 JSON 信封")
	}
	header := result.header.Get("X-Request-ID")
	if header == "" || header != envelope.RequestID {
		t.Fatal("X-Request-ID 与信封 request_id 不一致")
	}
}

func assertStatus(t *testing.T, result apiResult, status int, code string) {
	t.Helper()
	assertNoStore(t, result)
	if result.status != status {
		t.Fatalf("status=%d want=%d 顶层键=%s", result.status, status, result.keys())
	}
	// 204 契约要求无正文：只核对响应头里的 request_id，不解码信封。
	if status == http.StatusNoContent {
		if len(result.body) != 0 {
			t.Fatalf("204 正文长度=%d want=0", len(result.body))
		}
		if result.header.Get("X-Request-ID") == "" {
			t.Fatal("204 缺少 X-Request-ID")
		}
		return
	}
	assertRequestID(t, result)
	if code != "" && result.errorCode() != code {
		t.Fatalf("error.code 不匹配（want=%s）", code)
	}
}

func TestAuthRegisterLoginMeLogoutWithRealMySQL(t *testing.T) {
	fixture := newAuthFixture(t)
	username := fixture.randomUsername(t)

	// 大小写与首尾空白的归一化在真实链路上生效。
	registerResult, _ := fixture.register(t, "  "+strings.ToUpper(username)+"  ", integrationPassword)
	assertStatus(t, registerResult, http.StatusCreated, "")
	if len(registerResult.data()) != 2 {
		t.Fatalf("注册响应字段数=%d", len(registerResult.data()))
	}
	if string(registerResult.data()["username"]) != `"`+username+`"` {
		t.Fatal("注册响应 username 不是归一化小写值")
	}
	if count := len(registerResult.cookies); count != 0 {
		t.Fatalf("注册签发 Cookie 数量=%d", count)
	}

	// 重复注册（再次归一化到同名）必须 409。
	duplicate, _ := fixture.register(t, username, integrationPassword)
	assertStatus(t, duplicate, http.StatusConflict, "username_conflict")

	loginResult, cookie, csrf := fixture.login(t, strings.ToUpper(username), integrationPassword)
	assertStatus(t, loginResult, http.StatusOK, "")
	if cookie.Value == "" || csrf == "" || cookie.Value == csrf {
		t.Fatal("登录未返回独立的 Session 与 CSRF")
	}
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/" || cookie.Domain != "" || cookie.Secure {
		t.Fatalf("开发 Cookie 属性不符：HttpOnly=%v Lax=%v path=/=%v domain 空=%v secure=%v",
			cookie.HttpOnly, cookie.SameSite == http.SameSiteLaxMode, cookie.Path == "/", cookie.Domain == "", cookie.Secure)
	}
	sessionID := fixture.sessionIDByToken(t, cookie.Value)

	// 数据库只保存 SHA-256 原始 32 字节摘要与 Argon2id 编码。
	var row struct {
		TokenHash    []byte
		CSRFToken    string
		PasswordHash string
	}
	if err := fixture.db.ORM.Raw(`SELECT s.token_hash AS token_hash, s.csrf_token AS csrf_token, u.password_hash AS password_hash
		FROM auth_sessions s JOIN users u ON u.id = s.user_id WHERE s.id = ?`, sessionID).Scan(&row).Error; err != nil {
		t.Fatal("读取本轮 Session 行失败")
	}
	if len(row.TokenHash) != 32 {
		t.Fatalf("token_hash 长度=%d want=32", len(row.TokenHash))
	}
	if bytes.Equal(row.TokenHash, []byte(cookie.Value)) {
		t.Fatal("数据库保存了明文 Session 原值")
	}
	if !strings.HasPrefix(row.PasswordHash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Fatal("password_hash 不是冻结参数的 Argon2id 编码")
	}
	if strings.Contains(row.PasswordHash, integrationPassword) || row.CSRFToken == cookie.Value {
		t.Fatal("数据库出现明文密码或复用的 CSRF")
	}

	// me 用同一 Cookie 恢复用户与当前 Session 的 CSRF；GET 不要求 Origin/CSRF。
	me := fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", cookie)
	assertStatus(t, me, http.StatusOK, "")
	var restoredCSRF string
	if err := json.Unmarshal(me.data()["csrf_token"], &restoredCSRF); err != nil || restoredCSRF != csrf {
		t.Fatal("me 未恢复当前 Session 的 CSRF")
	}

	// 退出：204 无正文、清除 Cookie、当前 Session 失效。
	logout := fixture.do(t, "POST", "/api/v1/auth/logout", "", integrationOrigin, csrf, cookie)
	assertStatus(t, logout, http.StatusNoContent, "")
	var cleared *http.Cookie
	for _, candidate := range logout.cookies {
		if candidate.Name == auth.CookieNameDev {
			cleared = candidate
		}
	}
	if cleared == nil || cleared.Value != "" || cleared.MaxAge >= 0 {
		t.Fatal("退出未清除同名 Cookie")
	}
	afterLogout := fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", cookie)
	assertStatus(t, afterLogout, http.StatusUnauthorized, "unauthenticated")

	// 日志不得出现密码、Session 原值、CSRF、完整响应或有用的存储细节。
	fixture.assertNoSecretsInLogs(t, integrationPassword, cookie.Value, csrf, string(registerResult.body), string(loginResult.body))
}

func TestAuthConcurrentRegisterKeepsSingleWinner(t *testing.T) {
	fixture := newAuthFixture(t)
	username := fixture.randomUsername(t)
	type outcome struct {
		status int
		code   string
		id     uint64
	}
	results := make(chan outcome, 2)
	var waitGroup sync.WaitGroup
	for attempt := 0; attempt < 2; attempt++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			// 并发请求必须各自独立构造，避免共享 Cookie jar 与连接语义。
			request, err := http.NewRequest("POST", fixture.server.URL+"/api/v1/auth/register",
				strings.NewReader(`{"username":"`+username+`","password":"`+integrationPassword+`"}`))
			if err != nil {
				results <- outcome{status: -1}
				return
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", integrationOrigin)
			response, err := fixture.concurrentClient.Do(request)
			if err != nil {
				results <- outcome{status: -1}
				return
			}
			defer response.Body.Close()
			payload, _ := io.ReadAll(response.Body)
			value := outcome{status: response.StatusCode}
			var envelope struct {
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			_ = json.Unmarshal(payload, &envelope)
			value.code = envelope.Error.Code
			var id uint64
			_, _ = fmt.Sscanf(envelope.Data.ID, "%d", &id)
			value.id = id
			results <- value
		}()
	}
	waitGroup.Wait()
	close(results)
	created, conflicts := 0, 0
	for value := range results {
		switch value.status {
		case http.StatusCreated:
			created++
			if value.id == 0 {
				t.Fatal("成功注册未返回可用 id")
			}
			fixture.userIDs = append(fixture.userIDs, value.id)
		case http.StatusConflict:
			conflicts++
			if value.code != "username_conflict" {
				t.Fatal("并发冲突未返回 username_conflict")
			}
		default:
			t.Fatalf("并发注册出现非预期状态：status=%d", value.status)
		}
	}
	if created != 1 || conflicts != 1 {
		t.Fatalf("并发注册结果 created=%d conflicts=%d，应各为 1", created, conflicts)
	}
	// 唯一索引保证数据库只有一行。
	var count int64
	if err := fixture.db.ORM.Raw("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count).Error; err != nil {
		t.Fatal("统计并发注册结果失败")
	}
	if count != 1 {
		t.Fatalf("数据库同名用户行数=%d want=1", count)
	}
}

func TestAuthInvalidCredentialsAndUnauthenticated(t *testing.T) {
	fixture := newAuthFixture(t)
	username := fixture.randomUsername(t)
	if _, created := fixture.register(t, username, integrationPassword); created == 0 {
		t.Fatal("注册未成功创建用户")
	}

	wrongPassword, _, _ := fixture.login(t, username, "wrong-password-0001")
	assertStatus(t, wrongPassword, http.StatusUnauthorized, "invalid_credentials")
	if count := len(wrongPassword.cookies); count != 0 {
		t.Fatalf("错密码签发 Cookie 数量=%d", count)
	}
	unknownUser, _, _ := fixture.login(t, fixture.randomUsername(t), "wrong-password-0001")
	assertStatus(t, unknownUser, http.StatusUnauthorized, "invalid_credentials")
	// 同一提示，避免账号枚举。
	var first, second struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(wrongPassword.body, &first)
	_ = json.Unmarshal(unknownUser.body, &second)
	if first.Error.Message != second.Error.Message {
		t.Fatal("未知账号与错误密码的提示不同")
	}

	for name, cookies := range map[string][]*http.Cookie{
		"missing": nil,
		"forged":  {{Name: auth.CookieNameDev, Value: strings.Repeat("A", 43), Path: "/"}},
		"garbage": {{Name: auth.CookieNameDev, Value: "!!!", Path: "/"}},
	} {
		t.Run("me "+name, func(t *testing.T) {
			result := fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", cookies...)
			assertStatus(t, result, http.StatusUnauthorized, "unauthenticated")
		})
	}

	// 伪造但格式合法的 token 不会匹配任何 Session。
	forged := strings.Repeat("B", 43)
	forgedResult := fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", &http.Cookie{Name: auth.CookieNameDev, Value: forged, Path: "/"})
	assertStatus(t, forgedResult, http.StatusUnauthorized, "unauthenticated")
}

func TestAuthOriginAndCSRFEnforcementOverHTTP(t *testing.T) {
	fixture := newAuthFixture(t)
	username := fixture.randomUsername(t)
	if _, created := fixture.register(t, username, integrationPassword); created == 0 {
		t.Fatal("注册未成功创建用户")
	}
	_, cookie, csrf := fixture.login(t, username, integrationPassword)
	fixture.sessionIDByToken(t, cookie.Value)

	// 注册/登录：缺失、null、错误、重复 Origin 都是 403，且不接触业务逻辑。
	for name, origin := range map[string]string{
		"missing":   "",
		"null":      "null",
		"wrong":     "http://127.0.0.1:5174",
		"with path": integrationOrigin + "/",
	} {
		t.Run("origin "+name, func(t *testing.T) {
			result := fixture.do(t, "POST", "/api/v1/auth/register",
				`{"username":"`+fixture.randomUsername(t)+`","password":"`+integrationPassword+`"}`, origin, "")
			assertStatus(t, result, http.StatusForbidden, "csrf_failed")
		})
	}

	// 退出：Origin、Cookie、CSRF 三者缺一不可。
	cases := map[string]struct {
		origin string
		csrf   string
		cookie *http.Cookie
	}{
		"missing origin": {"", csrf, cookie},
		"missing csrf":   {integrationOrigin, "", cookie},
		"wrong csrf":     {integrationOrigin, strings.Repeat("x", 43), cookie},
		"missing cookie": {integrationOrigin, csrf, nil},
	}
	for name, tc := range cases {
		t.Run("logout "+name, func(t *testing.T) {
			var cookies []*http.Cookie
			if tc.cookie != nil {
				cookies = append(cookies, tc.cookie)
			}
			result := fixture.do(t, "POST", "/api/v1/auth/logout", "", tc.origin, tc.csrf, cookies...)
			expectedStatus, expectedCode := http.StatusForbidden, "csrf_failed"
			if tc.cookie == nil {
				expectedStatus, expectedCode = http.StatusUnauthorized, "unauthenticated"
			}
			assertStatus(t, result, expectedStatus, expectedCode)
		})
	}

	// 上述失败不得撤销 Session。
	me := fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", cookie)
	assertStatus(t, me, http.StatusOK, "")
}

func TestAuthLogoutOnlyRevokesCurrentSession(t *testing.T) {
	fixture := newAuthFixture(t)
	username := fixture.randomUsername(t)
	if _, created := fixture.register(t, username, integrationPassword); created == 0 {
		t.Fatal("注册未成功创建用户")
	}
	_, firstCookie, firstCSRF := fixture.login(t, username, integrationPassword)
	fixture.sessionIDByToken(t, firstCookie.Value)
	_, secondCookie, _ := fixture.login(t, username, integrationPassword)
	fixture.sessionIDByToken(t, secondCookie.Value)

	logout := fixture.do(t, "POST", "/api/v1/auth/logout", "", integrationOrigin, firstCSRF, firstCookie)
	assertStatus(t, logout, http.StatusNoContent, "")

	revoked := fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", firstCookie)
	assertStatus(t, revoked, http.StatusUnauthorized, "unauthenticated")
	survivor := fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", secondCookie)
	assertStatus(t, survivor, http.StatusOK, "")

	// 再次用已撤销的 Session 退出：401，且不会误删其他会话。
	again := fixture.do(t, "POST", "/api/v1/auth/logout", "", integrationOrigin, firstCSRF, firstCookie)
	assertStatus(t, again, http.StatusUnauthorized, "unauthenticated")
	survivor = fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", secondCookie)
	assertStatus(t, survivor, http.StatusOK, "")
}

func TestAuthSessionSurvivesServiceAndPoolRebuild(t *testing.T) {
	fixture := newAuthFixture(t)
	username := fixture.randomUsername(t)
	if _, created := fixture.register(t, username, integrationPassword); created == 0 {
		t.Fatal("注册未成功创建用户")
	}
	_, cookie, csrf := fixture.login(t, username, integrationPassword)
	fixture.sessionIDByToken(t, cookie.Value)

	// 关闭当前连接池与 HTTP 端，再用新连接、新 Service 重连：Session 必须仍可恢复。
	original := fixture.db
	fixture.server.Close()
	fixture.server = nil
	if err := original.Close(); err != nil {
		// 不忽略清理：cleanup 使用独立且重新校验身份的连接，因此这里只需避免重复关闭报错。
		t.Logf("首个连接池提前关闭（类型 %T）", err)
	}
	// 新连接池由 openTestDatabase 注册自己的 Cleanup（先于夹具清理执行）。
	// 夹具清理不依赖任何 fixture.db，而是新建独立连接并重新校验测试目标身份，
	// 因此 LIFO 顺序不会导致夹具残留。
	reopened := openTestDatabase(t)
	fixture.db = reopened
	fixture.rebuild(t)

	me := fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", cookie)
	assertStatus(t, me, http.StatusOK, "")
	var restored string
	if err := json.Unmarshal(me.data()["csrf_token"], &restored); err != nil || restored != csrf {
		t.Fatal("重建后未恢复当前 Session 的 CSRF")
	}
}

func TestAuthExpiredSessionIsRejected(t *testing.T) {
	fixture := newAuthFixture(t)
	username := fixture.randomUsername(t)
	if _, created := fixture.register(t, username, integrationPassword); created == 0 {
		t.Fatal("注册未成功创建用户")
	}
	_, cookie, _ := fixture.login(t, username, integrationPassword)
	sessionID := fixture.sessionIDByToken(t, cookie.Value)

	// 直接把本轮 Session 的 expires_at 改到过去，验证固定过期判定，不进行真实等待。
	if err := fixture.db.ORM.Exec("UPDATE auth_sessions SET expires_at = ? WHERE id = ?",
		time.Now().UTC().Add(-time.Minute), sessionID).Error; err != nil {
		t.Fatal("修改本轮 Session 过期时间失败")
	}
	me := fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", cookie)
	assertStatus(t, me, http.StatusUnauthorized, "unauthenticated")

	// 未来 1 秒内仍有效：只改回未过期时间即可证明边界判定方向。
	if err := fixture.db.ORM.Exec("UPDATE auth_sessions SET expires_at = ? WHERE id = ?",
		time.Now().UTC().Add(time.Hour), sessionID).Error; err != nil {
		t.Fatal("恢复本轮 Session 过期时间失败")
	}
	me = fixture.do(t, "GET", "/api/v1/auth/me", "", "", "", cookie)
	assertStatus(t, me, http.StatusOK, "")
}

func TestAuthStorageFailureIsControlled(t *testing.T) {
	fixture := newAuthFixture(t)
	username := fixture.randomUsername(t)
	// 关闭连接池模拟数据库不可用：只影响本测试自己的连接，不触碰容器或其他数据。
	if err := fixture.db.Close(); err != nil {
		t.Fatal("关闭本测试连接池失败")
	}
	result := fixture.do(t, "POST", "/api/v1/auth/register",
		`{"username":"`+username+`","password":"`+integrationPassword+`"}`, integrationOrigin, "")
	assertStatus(t, result, http.StatusServiceUnavailable, "dependency_unavailable")
	if count := len(result.cookies); count != 0 {
		t.Fatalf("存储失败签发 Cookie 数量=%d", count)
	}
	if strings.Contains(string(result.body), "siyige_test_app") || strings.Contains(string(result.body), "sql") {
		t.Fatal("503 响应泄露数据库细节")
	}
	fixture.assertNoSecretsInLogs(t, integrationPassword, "", "", string(result.body))
}

func TestAuthHealthEndpointsStillWork(t *testing.T) {
	fixture := newAuthFixture(t)
	live := fixture.do(t, "GET", "/api/health/live", "", "", "")
	if live.status != http.StatusOK || string(live.body) != `{"status":"alive"}` {
		t.Fatalf("live status=%d", live.status)
	}
	ready := fixture.do(t, "GET", "/api/health/ready", "", "", "")
	if ready.status != http.StatusOK || string(ready.body) != `{"status":"ready"}` {
		t.Fatalf("ready status=%d", ready.status)
	}
	if ready.header.Get("Cache-Control") != "no-store" {
		t.Fatal("ready 缺少 no-store")
	}
}

// assertNoSecretsInLogs 断言日志不含密码、Session 原值、CSRF 或完整成功响应。
// 只报告命中的类别名，不输出命中内容。
func (fixture *authFixture) assertNoSecretsInLogs(t *testing.T, password, sessionToken, csrf string, bodies ...string) {
	t.Helper()
	logs := fixture.logs.String()
	for name, secret := range map[string]string{
		"password":      password,
		"session token": sessionToken,
		"csrf":          csrf,
	} {
		if secret != "" && strings.Contains(logs, secret) {
			t.Fatalf("日志出现敏感值类别=%s", name)
		}
	}
	for _, body := range bodies {
		if len(body) > 16 && strings.Contains(logs, body) {
			t.Fatal("日志出现完整响应正文")
		}
	}
	if strings.Contains(logs, "siyige_test_app") || strings.Contains(logs, "password_hash") {
		t.Fatal("日志出现数据库账户或字段细节")
	}
}
