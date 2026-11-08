package auth

import (
	"errors"
	"time"
)

// 认证模块对外的受控错误。HTTP 边界按契约映射状态码，
// 内部 cause 只用于 errors.Is/As 诊断，不进入响应或日志文本。
var (
	// ErrInvalidRequest 对应 400 invalid_request：字段归一化或长度校验失败。
	ErrInvalidRequest = errors.New("请求参数不符合契约")
	// ErrUsernameConflict 对应 409 username_conflict。
	ErrUsernameConflict = errors.New("用户名已被占用")
	// ErrNotFound 表示查询目标不存在，用于区分“没有这一行”和“数据库故障”。
	ErrNotFound = errors.New("记录不存在")
	// ErrInvalidCredentials 对应 401 invalid_credentials，未知用户名与错误密码共用。
	ErrInvalidCredentials = errors.New("用户名或密码错误")
	// ErrUnauthenticated 对应 401 unauthenticated：Session 缺失、伪造、过期或已撤销。
	ErrUnauthenticated = errors.New("未登录或登录已失效")
	// ErrDependencyUnavailable 对应 503 dependency_unavailable，不暴露底层原因。
	ErrDependencyUnavailable = errors.New("依赖服务暂时不可用")
	// ErrAuthBusy 对应 503，表示认证哈希并发槽已满；请求不排队，避免资源被拖满。
	ErrAuthBusy = errors.New("认证服务繁忙")
	// ErrInvalidToken 表示 Cookie 中的 Session 原值不是规范随机编码。
	ErrInvalidToken = errors.New("会话 token 格式无效")
)

// RateLimitError 携带 429 所需的 Retry-After 等待时间，不包含被限制的 key。
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string { return "请求过于频繁" }

// storageError 保留底层 cause 供 errors.Is/As 诊断，但 Error 字符串固定受控，
// 防止 SQL、DSN、账户或表结构经错误文本泄露到日志与响应。
type storageError struct {
	operation string
	cause     error
}

func (e storageError) Error() string { return e.operation }

func (e storageError) Unwrap() error { return e.cause }

// storeError 包装 Repository 的原始错误；operation 必须是预定义的受控短语。
func storeError(operation string, cause error) error {
	return storageError{operation: operation, cause: cause}
}
