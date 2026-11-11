package auth

import (
	"sync"
	"time"
)

// 冻结的限流参数：注册按 IP 每小时 5 次，登录按 IP 与规范化用户名各每分钟 10 次。
const (
	RegisterIPLimit    = 5
	RegisterIPWindow   = time.Hour
	LoginAttemptLimit  = 10
	LoginAttemptWindow = time.Minute

	// defaultLimiterMaxKeys 是进程内计数表上限，保护内存不被随机 IP/用户名撑爆。
	defaultLimiterMaxKeys = 20000
)

// limiterEntry 是固定窗口计数；windowStart 决定何时整体重置。
type limiterEntry struct {
	count       int
	windowStart time.Time
	window      time.Duration
}

// Limiter 是并发安全的进程内固定窗口限流器。
// 单实例内存计数在重启后丢失，这是已知限制，由 T07 首次公网开放前的检查覆盖。
type Limiter struct {
	mu      sync.Mutex
	now     func() time.Time
	maxKeys int
	entries map[string]limiterEntry
}

// NewLimiter 允许注入时钟以便确定性测试；maxKeys 为 0 时使用默认上限。
func NewLimiter(now func() time.Time, maxKeys int) *Limiter {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	if maxKeys <= 0 {
		maxKeys = defaultLimiterMaxKeys
	}
	return &Limiter{now: now, maxKeys: maxKeys, entries: make(map[string]limiterEntry)}
}

// Allow 在固定窗口内计数并返回是否放行；拒绝时返回需要等待的时长。
// key 由调用方拼装（scope + 维度值），避免不同限流维度共享计数。
func (limiter *Limiter) Allow(scope, key string, limit int, window time.Duration) (bool, time.Duration) {
	if limit <= 0 || window <= 0 {
		return false, window
	}
	now := limiter.now()
	entryKey := scope + "\x00" + key

	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	entry, exists := limiter.entries[entryKey]
	if exists && !now.Before(entry.windowStart.Add(entry.window)) {
		// 窗口已结束，重置计数。
		entry = limiterEntry{windowStart: now, window: window}
	}
	if !exists {
		if len(limiter.entries) >= limiter.maxKeys {
			// 先回收过期条目；仍满则失败关闭，不淘汰活跃 key 以绕过限制。
			limiter.purgeExpiredLocked(now)
			if len(limiter.entries) >= limiter.maxKeys {
				// 仍满说明存在活跃 key；新 key 恒等失败关闭，计数器不会被绕过。
				return false, window
			}
		}
		entry = limiterEntry{windowStart: now, window: window}
	}
	if entry.count >= limit {
		return false, entry.windowStart.Add(entry.window).Sub(now)
	}
	entry.count++
	entry.window = window
	limiter.entries[entryKey] = entry
	return true, 0
}

// purgeExpiredLocked 只在持锁路径调用；调用方必须已持有 limiter.mu。
// 只回收到期条目（按各自窗口判断），因此绝不会误删仍活跃的计数。
func (limiter *Limiter) purgeExpiredLocked(now time.Time) {
	for key, entry := range limiter.entries {
		if !now.Before(entry.windowStart.Add(entry.window)) {
			delete(limiter.entries, key)
		}
	}
}

// RetryAfterSeconds 把等待时长换算为 Retry-After 要求的正整数秒（向上取整）。
func RetryAfterSeconds(delay time.Duration) int {
	if delay <= 0 {
		return 1
	}
	seconds := int((delay + time.Second - 1) / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}
