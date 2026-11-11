package auth

import (
	"context"
	"errors"
	"io"
	"time"
)

// 认证哈希的默认并发上限：Argon2id 每次约 19 MiB，2 个并发既避免登录挤占内存，
// 又不会因单请求排队过长。槽位不可用时立即返回繁忙，而不是无限等待。
const defaultHashConcurrency = 2

// passwordOps 把密码原语收敛为两个函数，便于测试注入计数器并证明限流先于哈希。
type passwordOps struct {
	hash   func(password string) (string, error)
	verify func(encoded, password string) (bool, error)
}

var defaultPasswordOps = passwordOps{hash: HashPassword, verify: VerifyPassword}

// placeholderPasswordHash 是格式合法但不对应任何口令的 Argon2id 编码（同参数、固定盐）。
// 未知用户名时对它执行一次真实验证，使“账号不存在”与“密码错误”成本一致。
const placeholderPasswordHash = "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// Service 实现注册、登录、认证恢复与退出；不感知 HTTP，也不 import platform。
type Service struct {
	store         Store
	limiter       *Limiter
	clock         func() time.Time
	passwords     passwordOps
	hashSlots     chan struct{}
	sessionReader io.Reader
	sessionTTL    time.Duration
}

// Option 用于在构造函数中替换时钟、限流器与资源上限，生产代码使用默认值。
type Option func(*Service)

// WithClock 注入时钟；测试用它固定窗口与过期边界，不产生真实等待。
func WithClock(now func() time.Time) Option {
	return func(service *Service) {
		if now != nil {
			service.clock = now
		}
	}
}

// WithLimiter 注入共享限流器；生产入口应传入单例以跨请求累计计数。
func WithLimiter(limiter *Limiter) Option {
	return func(service *Service) {
		if limiter != nil {
			service.limiter = limiter
		}
	}
}

// WithSessionTTL 只允许测试缩短有效期；默认固定 24 小时。
func WithSessionTTL(ttl time.Duration) Option {
	return func(service *Service) {
		if ttl > 0 {
			service.sessionTTL = ttl
		}
	}
}

// WithHashConcurrency 调整哈希并发槽；小于 1 时忽略。
func WithHashConcurrency(limit int) Option {
	return func(service *Service) {
		if limit > 0 {
			service.hashSlots = make(chan struct{}, limit)
		}
	}
}

// NewService 组装默认依赖：系统时钟、进程内限流器、真实 Argon2id 与 crypto/rand。
func NewService(store Store, options ...Option) *Service {
	service := &Service{
		store:         store,
		clock:         func() time.Time { return time.Now().UTC() },
		passwords:     defaultPasswordOps,
		hashSlots:     make(chan struct{}, defaultHashConcurrency),
		sessionReader: defaultSessionReader(),
		sessionTTL:    DefaultSessionTTL,
	}
	for _, option := range options {
		option(service)
	}
	if service.limiter == nil {
		service.limiter = NewLimiter(service.clock, 0)
	}
	return service
}

// Register 先按 IP 限流，再校验用户名及密码原文，然后哈希并入库。
// 返回的 User 不含密码哈希；用户名冲突映射为 ErrUsernameConflict。
func (service *Service) Register(ctx context.Context, clientIP, username, password string) (User, error) {
	if err := ctx.Err(); err != nil {
		return User{}, err
	}
	if err := service.limit("register_ip", clientIP, RegisterIPLimit, RegisterIPWindow); err != nil {
		return User{}, err
	}
	normalized, err := NormalizeUsername(username)
	if err != nil {
		return User{}, ErrInvalidRequest
	}
	if err := ValidatePassword(password); err != nil {
		return User{}, ErrInvalidRequest
	}
	release, err := service.acquireHashSlot(ctx)
	if err != nil {
		return User{}, err
	}
	defer release()
	passwordHash, err := service.passwords.hash(password)
	if err != nil {
		return User{}, ErrDependencyUnavailable
	}
	user, err := service.store.CreateUser(ctx, normalized, passwordHash)
	if err != nil {
		return User{}, service.mapStoreError(ctx, err)
	}
	user.PasswordHash = ""
	return user, nil
}

// Login 在限流之后才做密码验证：不存在账号与错误密码都返回 ErrInvalidCredentials，
// 且都执行一次同参数 Argon2id。创建 Session 失败时不返回任何 token。
func (service *Service) Login(ctx context.Context, clientIP, username, password string) (LoginResult, error) {
	if err := ctx.Err(); err != nil {
		return LoginResult{}, err
	}
	// IP 维度先计数，使非法用户名也无法绕过按 IP 的限制。
	if err := service.limit("login_ip", clientIP, LoginAttemptLimit, LoginAttemptWindow); err != nil {
		return LoginResult{}, err
	}
	normalized, err := NormalizeUsername(username)
	if err != nil {
		return LoginResult{}, ErrInvalidRequest
	}
	if err := ValidatePassword(password); err != nil {
		return LoginResult{}, ErrInvalidRequest
	}
	if err := service.limit("login_username", normalized, LoginAttemptLimit, LoginAttemptWindow); err != nil {
		return LoginResult{}, err
	}
	user, err := service.store.FindUserByUsername(ctx, normalized)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			// 账号不存在也执行一次真实验证，避免通过响应时间枚举账号。
			release, slotErr := service.acquireHashSlot(ctx)
			if slotErr != nil {
				return LoginResult{}, slotErr
			}
			defer release()
			if _, verifyErr := service.passwords.verify(placeholderPasswordHash, password); verifyErr != nil {
				return LoginResult{}, ErrDependencyUnavailable
			}
			return LoginResult{}, ErrInvalidCredentials
		}
		return LoginResult{}, service.mapStoreError(ctx, err)
	}
	release, err := service.acquireHashSlot(ctx)
	if err != nil {
		return LoginResult{}, err
	}
	matched, verifyErr := service.passwords.verify(user.PasswordHash, password)
	release()
	if verifyErr != nil {
		// 存储的哈希损坏属于内部错误，不能伪装成凭据错误，也不能签发会话。
		return LoginResult{}, ErrDependencyUnavailable
	}
	if !matched {
		return LoginResult{}, ErrInvalidCredentials
	}
	session, token, err := newSessionRecord(user.ID, service.sessionReader, service.clock(), service.sessionTTL)
	if err != nil {
		return LoginResult{}, ErrDependencyUnavailable
	}
	sessionID, err := service.store.CreateSession(ctx, *session)
	if err != nil {
		return LoginResult{}, service.mapStoreError(ctx, err)
	}
	session.ID = sessionID
	user.PasswordHash = ""
	return LoginResult{User: user, Session: *session, Token: token}, nil
}

// Authenticate 用 Cookie 原值恢复当前用户与 Session；格式非法、伪造、过期或已撤销
// 一律返回 ErrUnauthenticated，格式非法时不会访问数据库。
func (service *Service) Authenticate(ctx context.Context, token string) (Session, User, error) {
	if err := ctx.Err(); err != nil {
		return Session{}, User{}, err
	}
	digest, err := sessionTokenDigest(token)
	if err != nil {
		return Session{}, User{}, ErrUnauthenticated
	}
	session, err := service.store.FindSessionByTokenHash(ctx, digest)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Session{}, User{}, ErrUnauthenticated
		}
		return Session{}, User{}, service.mapStoreError(ctx, err)
	}
	if !sessionValidAt(session, service.clock()) {
		return Session{}, User{}, ErrUnauthenticated
	}
	user, err := service.store.FindUserByID(ctx, session.UserID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Session{}, User{}, ErrUnauthenticated
		}
		return Session{}, User{}, service.mapStoreError(ctx, err)
	}
	user.PasswordHash = ""
	return session, user, nil
}

// Logout 只撤销当前 Session，其他设备的 Session 保持有效。
func (service *Service) Logout(ctx context.Context, session Session) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if session.ID == 0 {
		return ErrUnauthenticated
	}
	deleted, err := service.store.DeleteSession(ctx, session.ID)
	if err != nil {
		return service.mapStoreError(ctx, err)
	}
	if !deleted {
		return ErrUnauthenticated
	}
	return nil
}

// limit 把限流拒绝转换为携带 Retry-After 的受控错误。
func (service *Service) limit(scope, key string, limit int, window time.Duration) error {
	allowed, retryAfter := service.limiter.Allow(scope, key, limit, window)
	if allowed {
		return nil
	}
	if retryAfter <= 0 {
		retryAfter = window
	}
	return &RateLimitError{RetryAfter: retryAfter}
}

// acquireHashSlot 非阻塞地占用哈希槽；槽满立即返回 ErrAuthBusy，不排队等待。
func (service *Service) acquireHashSlot(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	select {
	case service.hashSlots <- struct{}{}:
		return func() { <-service.hashSlots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return nil, ErrAuthBusy
	}
}

// mapStoreError 收敛存储错误：调用方取消优先，其次保留业务冲突，其余统一 503。
func (service *Service) mapStoreError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, ErrUsernameConflict) {
		return ErrUsernameConflict
	}
	if errors.Is(err, ErrNotFound) {
		return ErrNotFound
	}
	// 保留 cause 供 errors.Is/As 诊断；Error() 仍只暴露固定的受控分类。
	return storeError(ErrDependencyUnavailable.Error(), errors.Join(ErrDependencyUnavailable, err))
}
