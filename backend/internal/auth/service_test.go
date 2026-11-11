package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// testClock 提供确定性时间，避免测试等待真实窗口。
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newTestClock(start time.Time) *testClock { return &testClock{now: start} }

func (clock *testClock) Now() time.Time {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	return clock.now
}

func (clock *testClock) Advance(delta time.Duration) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.now = clock.now.Add(delta)
}

// storageFailure 模拟数据库故障；Service 必须把它映射为受控的依赖不可用。
type storageFailure struct{}

func (storageFailure) Error() string { return "driver: connection refused" }

// fakeStore 是可注入失败的 Store 实现，隔离 Service 与真实 MySQL。
type fakeStore struct {
	mu sync.Mutex

	users       map[string]User
	sessions    map[string]Session
	nextUserID  uint64
	nextSession uint64

	createUserCalls    int
	findUserCalls      int
	createSessionCalls int
	findSessionCalls   int
	deleteSessionCalls int

	createUserErr    error
	findUserErr      error
	createSessionErr error
	findSessionErr   error
	deleteSessionErr error
}

func newFakeStore() *fakeStore {
	return &fakeStore{users: map[string]User{}, sessions: map[string]Session{}, nextUserID: 100, nextSession: 500}
}

func (store *fakeStore) CreateUser(_ context.Context, username, passwordHash string) (User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.createUserCalls++
	if store.createUserErr != nil {
		return User{}, store.createUserErr
	}
	if _, exists := store.users[username]; exists {
		return User{}, ErrUsernameConflict
	}
	store.nextUserID++
	user := User{ID: store.nextUserID, Username: username, PasswordHash: passwordHash}
	store.users[username] = user
	return user, nil
}

func (store *fakeStore) FindUserByUsername(_ context.Context, username string) (User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.findUserCalls++
	if store.findUserErr != nil {
		return User{}, store.findUserErr
	}
	user, exists := store.users[username]
	if !exists {
		return User{}, ErrNotFound
	}
	return user, nil
}

func (store *fakeStore) FindUserByID(_ context.Context, id uint64) (User, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.findUserCalls++
	if store.findUserErr != nil {
		return User{}, store.findUserErr
	}
	for _, user := range store.users {
		if user.ID == id {
			return user, nil
		}
	}
	return User{}, ErrNotFound
}

func (store *fakeStore) CreateSession(_ context.Context, session Session) (uint64, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.createSessionCalls++
	if store.createSessionErr != nil {
		return 0, store.createSessionErr
	}
	store.nextSession++
	session.ID = store.nextSession
	store.sessions[string(session.TokenHash)] = session
	return session.ID, nil
}

func (store *fakeStore) FindSessionByTokenHash(_ context.Context, tokenHash []byte) (Session, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.findSessionCalls++
	if store.findSessionErr != nil {
		return Session{}, store.findSessionErr
	}
	session, exists := store.sessions[string(tokenHash)]
	if !exists {
		return Session{}, ErrNotFound
	}
	return session, nil
}

func (store *fakeStore) DeleteSession(_ context.Context, sessionID uint64) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.deleteSessionCalls++
	if store.deleteSessionErr != nil {
		return false, store.deleteSessionErr
	}
	for key, session := range store.sessions {
		if session.ID == sessionID {
			delete(store.sessions, key)
			return true, nil
		}
	}
	return false, nil
}

// passwordOpsCounter 记录密码原语调用，用于证明限流发生在哈希之前。
type passwordOpsCounter struct {
	hashCalls   int
	verifyCalls int
	hashedWith  []string
	verifiedFor []string
}

func newTestService(t *testing.T, store Store) (*Service, *testClock, *passwordOpsCounter) {
	t.Helper()
	clock := newTestClock(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	service := NewService(store, WithClock(clock.Now), WithLimiter(NewLimiter(clock.Now, 0)))
	counter := &passwordOpsCounter{}
	service.passwords = passwordOps{
		hash: func(password string) (string, error) {
			counter.hashCalls++
			counter.hashedWith = append(counter.hashedWith, password)
			return "fake-hash", nil
		},
		verify: func(encoded, password string) (bool, error) {
			counter.verifyCalls++
			counter.verifiedFor = append(counter.verifiedFor, encoded)
			return encoded == "fake-hash" && password == "correct-horse-battery", nil
		},
	}
	return service, clock, counter
}

func TestRegisterNormalizesAndHashes(t *testing.T) {
	store := newFakeStore()
	service, _, counter := newTestService(t, store)
	user, err := service.Register(context.Background(), "127.0.0.1", "  Alice_01  ", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if user.ID == 0 || user.Username != "alice_01" {
		t.Fatalf("unexpected user: id=%d username normalized=%v", user.ID, user.Username == "alice_01")
	}
	if counter.hashCalls != 1 || counter.hashedWith[0] != "correct-horse-battery" {
		t.Fatal("password must be hashed verbatim exactly once")
	}
	stored, _ := store.FindUserByUsername(context.Background(), "alice_01")
	if stored.PasswordHash != "fake-hash" || stored.PasswordHash == "correct-horse-battery" {
		t.Fatal("plaintext password must never reach the store")
	}
	if user.PasswordHash != "" {
		t.Fatal("service must not expose the stored hash")
	}
}

func TestRegisterRejectsInvalidInputBeforeHashing(t *testing.T) {
	store := newFakeStore()
	service, _, counter := newTestService(t, store)
	for name, tc := range map[string]struct{ username, password string }{
		"bad username":   {"a!", "correct-horse-battery"},
		"short password": {"alice_01", "too-short"},
		"non ascii":      {"阿里斯", "correct-horse-battery"},
		"invalid utf8":   {"alice\xff", "correct-horse-battery"},
		"too long rune":  {"alice_01", "😀"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Register(context.Background(), "127.0.0.1", tc.username, tc.password); err == nil {
				t.Fatal("invalid credentials accepted")
			}
		})
	}
	if counter.hashCalls != 0 || store.createUserCalls != 0 {
		t.Fatalf("invalid input reached hashing or storage: hash=%d store=%d", counter.hashCalls, store.createUserCalls)
	}
}

func TestRegisterRateLimitHappensBeforeHashing(t *testing.T) {
	store := newFakeStore()
	service, _, counter := newTestService(t, store)
	for attempt := 0; attempt < RegisterIPLimit; attempt++ {
		username := "alice_" + string(rune('0'+attempt))
		if _, err := service.Register(context.Background(), "127.0.0.1", username, "correct-horse-battery"); err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	hashedBefore, storedBefore := counter.hashCalls, store.createUserCalls
	_, err := service.Register(context.Background(), "127.0.0.1", "bob_01", "correct-horse-battery")
	rateErr, ok := err.(*RateLimitError)
	if !ok {
		t.Fatalf("want rate limit error, got %v", err)
	}
	if rateErr.RetryAfter <= 0 || rateErr.RetryAfter > RegisterIPWindow {
		t.Fatalf("unexpected retry-after: %s", rateErr.RetryAfter)
	}
	if counter.hashCalls != hashedBefore || store.createUserCalls != storedBefore {
		t.Fatal("rate limited request still hashed or reached storage")
	}
}

func TestRegisterMapsConflictsAndStorageFailures(t *testing.T) {
	store := newFakeStore()
	service, _, _ := newTestService(t, store)
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	// 归一化后相同即冲突，且来自另一个 IP 以避开注册 IP 限流。
	if _, err := service.Register(context.Background(), "127.0.0.2", "ALICE_01", "correct-horse-battery"); err != ErrUsernameConflict {
		t.Fatalf("want username conflict, got %v", err)
	}
	store.createUserErr = storageFailure{}
	if _, err := service.Register(context.Background(), "127.0.0.3", "carol_01", "correct-horse-battery"); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("want dependency unavailable, got %v", err)
	}
}

func TestLoginUnknownUserCostsSameAsWrongPassword(t *testing.T) {
	store := newFakeStore()
	service, _, counter := newTestService(t, store)
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Login(context.Background(), "127.0.0.1", "alice_01", "wrong-password-1"); err != ErrInvalidCredentials {
		t.Fatalf("want invalid credentials, got %v", err)
	}
	verifyForExisting := counter.verifyCalls
	if _, err := service.Login(context.Background(), "127.0.0.2", "nobody_01", "wrong-password-1"); err != ErrInvalidCredentials {
		t.Fatalf("want invalid credentials, got %v", err)
	}
	if counter.verifyCalls != verifyForExisting+1 {
		t.Fatal("unknown username must still run one password verification")
	}
	// 未知账号使用同参数占位哈希，不能让“不存在”更快返回。
	dummy := counter.verifiedFor[len(counter.verifiedFor)-1]
	if dummy == "" {
		t.Fatal("unknown username did not verify against a well-formed placeholder hash")
	}
}

func TestLoginSuccessIssuesSessionWithoutStoringPlaintext(t *testing.T) {
	store := newFakeStore()
	service, clock, _ := newTestService(t, store)
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	result, err := service.Login(context.Background(), "127.0.0.2", "Alice_01", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if result.User.Username != "alice_01" || result.Token == "" || result.Session.CSRFToken == "" {
		t.Fatalf("incomplete login result: user ok=%v token empty=%v csrf empty=%v",
			result.User.Username == "alice_01", result.Token == "", result.Session.CSRFToken == "")
	}
	if result.Token == result.Session.CSRFToken {
		t.Fatal("csrf token must be independent of the session token")
	}
	if !result.Session.ExpiresAt.Equal(clock.Now().Add(DefaultSessionTTL)) {
		t.Fatalf("unexpected expiry: %s", result.Session.ExpiresAt)
	}
	if len(result.Session.TokenHash) != 32 || string(result.Session.TokenHash) == result.Token {
		t.Fatal("session must store only the token digest")
	}
	stored, err := store.FindSessionByTokenHash(context.Background(), result.Session.TokenHash)
	if err != nil || stored.ID != result.Session.ID {
		t.Fatal("session not persisted with the digest")
	}
	if result.Session.UserID != result.User.ID {
		t.Fatal("session bound to the wrong user")
	}
	if result.User.PasswordHash != "" {
		t.Fatal("login result leaked the password hash")
	}
}

func TestLoginDoesNotDeliverTokenWhenSessionCannotBeCreated(t *testing.T) {
	store := newFakeStore()
	service, _, _ := newTestService(t, store)
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	store.createSessionErr = storageFailure{}
	result, err := service.Login(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery")
	if !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("want dependency unavailable, got %v", err)
	}
	if result.Token != "" || result.Session.CSRFToken != "" || result.Session.ID != 0 {
		t.Fatal("failed login must not leak a session token or csrf token")
	}
}

func TestLoginRateLimitPerUsernameBeforeHashing(t *testing.T) {
	store := newFakeStore()
	service, _, counter := newTestService(t, store)
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	counter.verifyCalls = 0
	// 同一规范化用户名从不同 IP 登录，用户名维度独立生效。
	for attempt := 0; attempt < LoginAttemptLimit; attempt++ {
		if _, err := service.Login(context.Background(), "127.0.0."+string(rune('1'+attempt)), "Alice_01", "wrong-password-1"); err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	verifyBefore := counter.verifyCalls
	_, err := service.Login(context.Background(), "127.0.0.99", "alice_01", "correct-horse-battery")
	rateErr, ok := err.(*RateLimitError)
	if !ok {
		t.Fatalf("want rate limit error, got %v", err)
	}
	if rateErr.RetryAfter <= 0 || rateErr.RetryAfter > LoginAttemptWindow {
		t.Fatalf("unexpected retry-after: %s", rateErr.RetryAfter)
	}
	if counter.verifyCalls != verifyBefore || store.createSessionCalls != 0 {
		t.Fatal("rate limited login still verified a password or created a session")
	}
}

func TestLoginRateLimitPerIPBeforeHashing(t *testing.T) {
	store := newFakeStore()
	service, _, counter := newTestService(t, store)
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	counter.verifyCalls = 0
	for attempt := 0; attempt < LoginAttemptLimit; attempt++ {
		username := "ghost_" + string(rune('0'+attempt))
		if _, err := service.Login(context.Background(), "127.0.0.1", username, "wrong-password-1"); err != ErrInvalidCredentials {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
	}
	verifyBefore := counter.verifyCalls
	if _, err := service.Login(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err == nil {
		t.Fatal("IP limit was not enforced")
	}
	if counter.verifyCalls != verifyBefore {
		t.Fatal("rate limited login still verified a password")
	}
}

func TestLoginMalformedStoredHashFailsSafely(t *testing.T) {
	store := newFakeStore()
	service, _, _ := newTestService(t, store)
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	service.passwords.verify = func(string, string) (bool, error) { return false, ErrInvalidHash }
	result, err := service.Login(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery")
	if err == nil || err == ErrInvalidCredentials {
		t.Fatalf("corrupt stored hash must not be reported as a credential mismatch: %v", err)
	}
	if result.Token != "" || store.createSessionCalls != 0 {
		t.Fatal("corrupt hash must not issue a session")
	}
}

func TestLoginStorageFailureIsDependencyUnavailable(t *testing.T) {
	store := newFakeStore()
	service, _, counter := newTestService(t, store)
	store.findUserErr = storageFailure{}
	if _, err := service.Login(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("want dependency unavailable, got %v", err)
	}
	if counter.verifyCalls != 0 {
		t.Fatal("database failure must not be masked by a password check")
	}
}

func TestAuthenticateRestoresSessionAndRejectsInvalidTokens(t *testing.T) {
	store := newFakeStore()
	service, clock, _ := newTestService(t, store)
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	result, err := service.Login(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	session, user, err := service.Authenticate(context.Background(), result.Token)
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != result.Session.ID || user.ID != result.User.ID || session.CSRFToken != result.Session.CSRFToken {
		t.Fatal("authenticate did not restore the current session")
	}

	findBefore := store.findSessionCalls
	for name, token := range map[string]string{
		"empty":     "",
		"garbage":   "!!!",
		"truncated": result.Token[:len(result.Token)-1],
		"padded":    result.Token + "=",
		"hex":       "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := service.Authenticate(context.Background(), token); err != ErrUnauthenticated {
				t.Fatalf("want unauthenticated, got %v", err)
			}
		})
	}
	if store.findSessionCalls != findBefore {
		t.Fatal("malformed tokens must not reach the database")
	}
	// 伪造但格式合法的 token 会查询存储，仍必须是 401。
	forged := "A" + result.Token[1:]
	if forged == result.Token {
		forged = "B" + result.Token[1:]
	}
	if _, _, err := service.Authenticate(context.Background(), forged); err != ErrUnauthenticated {
		t.Fatalf("want unauthenticated for forged token, got %v", err)
	}

	// 固定过期：到期前 1ns 有效，到期瞬间失效。
	clock.Advance(DefaultSessionTTL - time.Nanosecond)
	if _, _, err := service.Authenticate(context.Background(), result.Token); err != nil {
		t.Fatalf("session must stay valid before expiry: %v", err)
	}
	clock.Advance(time.Nanosecond)
	if _, _, err := service.Authenticate(context.Background(), result.Token); err != ErrUnauthenticated {
		t.Fatalf("expired session must be rejected, got %v", err)
	}
}

func TestAuthenticateStorageFailuresAreDependencyUnavailable(t *testing.T) {
	store := newFakeStore()
	service, _, _ := newTestService(t, store)
	fresh := func(t *testing.T) (string, Session) {
		t.Helper()
		if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
			t.Fatal(err)
		}
		result, err := service.Login(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery")
		if err != nil {
			t.Fatal(err)
		}
		return result.Token, result.Session
	}
	token, session := fresh(t)
	store.findSessionErr = storageFailure{}
	if _, _, err := service.Authenticate(context.Background(), token); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("want dependency unavailable from session lookup, got %v", err)
	}
	store.findSessionErr = nil
	store.findUserErr = storageFailure{}
	if _, _, err := service.Authenticate(context.Background(), token); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("want dependency unavailable from user lookup, got %v", err)
	}
	store.findUserErr = nil
	if restored, _, err := service.Authenticate(context.Background(), token); err != nil || restored.ID != session.ID {
		t.Fatalf("session lookup must recover: %v", err)
	}
}

func TestAuthenticateRejectsUnknownAndExpiredSessions(t *testing.T) {
	store := newFakeStore()
	service, clock, _ := newTestService(t, store)
	unknown := generateTokenForTest(t)
	if _, _, err := service.Authenticate(context.Background(), unknown); err != ErrUnauthenticated {
		t.Fatalf("want unauthenticated for unknown session, got %v", err)
	}
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	result, err := service.Login(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	clock.Advance(DefaultSessionTTL)
	if _, _, err := service.Authenticate(context.Background(), result.Token); err != ErrUnauthenticated {
		t.Fatalf("expired session must be unauthenticated, got %v", err)
	}
}

func TestLogoutRevokesOnlyCurrentSession(t *testing.T) {
	store := newFakeStore()
	service, _, _ := newTestService(t, store)
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatal(err)
	}
	first, err := service.Login(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Login(context.Background(), "127.0.0.2", "alice_01", "correct-horse-battery")
	if err != nil {
		t.Fatal(err)
	}
	if first.Session.ID == second.Session.ID {
		t.Fatal("each login must create a new session")
	}
	if err := service.Logout(context.Background(), first.Session); err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Authenticate(context.Background(), first.Token); err != ErrUnauthenticated {
		t.Fatalf("logged out session must be revoked, got %v", err)
	}
	if _, _, err := service.Authenticate(context.Background(), second.Token); err != nil {
		t.Fatalf("other sessions must survive logout: %v", err)
	}
	if err := service.Logout(context.Background(), first.Session); err != ErrUnauthenticated {
		t.Fatalf("second logout must be unauthenticated, got %v", err)
	}
}

func TestLogoutStorageFailureIsDependencyUnavailable(t *testing.T) {
	store := newFakeStore()
	service, _, _ := newTestService(t, store)
	store.deleteSessionErr = storageFailure{}
	if err := service.Logout(context.Background(), Session{ID: 1}); !errors.Is(err, ErrDependencyUnavailable) {
		t.Fatalf("want dependency unavailable, got %v", err)
	}
}

func TestPasswordHashingConcurrencyIsBounded(t *testing.T) {
	store := newFakeStore()
	service, _, _ := newTestService(t, store)
	service.hashSlots = make(chan struct{}, 1)
	service.hashSlots <- struct{}{}
	hashCalls := 0
	service.passwords.hash = func(string) (string, error) {
		hashCalls++
		return "fake-hash", nil
	}
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != ErrAuthBusy {
		t.Fatalf("want busy error, got %v", err)
	}
	if hashCalls != 0 {
		t.Fatal("busy service still performed password hashing")
	}
	<-service.hashSlots
	if _, err := service.Register(context.Background(), "127.0.0.1", "alice_01", "correct-horse-battery"); err != nil {
		t.Fatalf("slot release must restore capacity: %v", err)
	}
	if hashCalls != 1 {
		t.Fatal("expected exactly one hash after capacity was released")
	}
}

func TestServiceHonoursCallerCancellation(t *testing.T) {
	store := newFakeStore()
	service, _, _ := newTestService(t, store)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Register(ctx, "127.0.0.1", "alice_01", "correct-horse-battery"); err != context.Canceled {
		t.Fatalf("want context cancellation, got %v", err)
	}
	if store.createUserCalls != 0 {
		t.Fatal("cancelled request reached storage")
	}
}

func generateTokenForTest(t *testing.T) string {
	t.Helper()
	_, token, err := newSessionRecord(1, randReader(t), time.Now(), DefaultSessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	return token
}
