package auth

import (
	"sync"
	"testing"
	"time"
)

func TestLimiterFixedWindowAndRetryAfter(t *testing.T) {
	clock := newTestClock(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	limiter := NewLimiter(clock.Now, 0)
	for attempt := 0; attempt < RegisterIPLimit; attempt++ {
		allowed, retryAfter := limiter.Allow("register_ip", "127.0.0.1", RegisterIPLimit, RegisterIPWindow)
		if !allowed || retryAfter != 0 {
			t.Fatalf("attempt %d rejected: %s", attempt, retryAfter)
		}
	}
	allowed, retryAfter := limiter.Allow("register_ip", "127.0.0.1", RegisterIPLimit, RegisterIPWindow)
	if allowed {
		t.Fatal("registered more than the hourly IP limit")
	}
	if retryAfter <= 0 || retryAfter > RegisterIPWindow {
		t.Fatalf("unexpected retry-after: %s", retryAfter)
	}
	// 不同 IP、不同 scope 各自计数。
	if allowed, _ := limiter.Allow("register_ip", "127.0.0.2", RegisterIPLimit, RegisterIPWindow); !allowed {
		t.Fatal("another IP must have its own bucket")
	}
	if allowed, _ := limiter.Allow("login_ip", "127.0.0.1", LoginAttemptLimit, LoginAttemptWindow); !allowed {
		t.Fatal("scopes must not share counters")
	}
	// 窗口最后一纳秒仍受限，跨过窗口后恢复。
	clock.Advance(RegisterIPWindow - time.Nanosecond)
	if allowed, _ := limiter.Allow("register_ip", "127.0.0.1", RegisterIPLimit, RegisterIPWindow); allowed {
		t.Fatal("limit reset before the window elapsed")
	}
	clock.Advance(time.Nanosecond)
	if allowed, _ := limiter.Allow("register_ip", "127.0.0.1", RegisterIPLimit, RegisterIPWindow); !allowed {
		t.Fatal("limit did not reset after the window elapsed")
	}
}

func TestLimiterConcurrentAllowIsAtomic(t *testing.T) {
	clock := newTestClock(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	limiter := NewLimiter(clock.Now, 0)
	var mu sync.Mutex
	allowedCount := 0
	var waitGroup sync.WaitGroup
	for attempt := 0; attempt < 64; attempt++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			allowed, _ := limiter.Allow("login_ip", "127.0.0.1", LoginAttemptLimit, LoginAttemptWindow)
			if allowed {
				mu.Lock()
				allowedCount++
				mu.Unlock()
			}
		}()
	}
	waitGroup.Wait()
	if allowedCount != LoginAttemptLimit {
		t.Fatalf("allowed %d concurrent requests, want %d", allowedCount, LoginAttemptLimit)
	}
}

func TestLimiterBoundEvictionNeverBypassesActiveKeys(t *testing.T) {
	clock := newTestClock(time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC))
	limiter := NewLimiter(clock.Now, 2)
	// 两个活跃 key 填满上限，各消耗 10 次中的 1 次。
	for _, key := range []string{"10.0.0.1", "10.0.0.2"} {
		if allowed, _ := limiter.Allow("login_ip", key, LoginAttemptLimit, LoginAttemptWindow); !allowed {
			t.Fatal("setup request rejected")
		}
	}
	// 新 key 不能挤掉活跃 key，只能失败关闭。
	allowed, retryAfter := limiter.Allow("login_ip", "10.0.0.3", LoginAttemptLimit, LoginAttemptWindow)
	if allowed || retryAfter <= 0 {
		t.Fatal("new key must be rejected while the bounded store is full of active keys")
	}
	// 活跃 key 的计数没有被淘汰：10.0.0.1 只剩 9 次。
	for attempt := 0; attempt < LoginAttemptLimit-1; attempt++ {
		if allowed, _ := limiter.Allow("login_ip", "10.0.0.1", LoginAttemptLimit, LoginAttemptWindow); !allowed {
			t.Fatalf("active key was evicted at attempt %d", attempt)
		}
	}
	if allowed, _ := limiter.Allow("login_ip", "10.0.0.1", LoginAttemptLimit, LoginAttemptWindow); allowed {
		t.Fatal("active key lost its counter")
	}
	// 过期条目被清理后可接纳新 key。
	clock.Advance(LoginAttemptWindow + time.Second)
	if allowed, _ := limiter.Allow("login_ip", "10.0.0.3", LoginAttemptLimit, LoginAttemptWindow); !allowed {
		t.Fatal("expired entries were not reclaimed")
	}
}

func TestRetryAfterSecondsRoundsUp(t *testing.T) {
	for _, tc := range []struct {
		delay time.Duration
		want  int
	}{
		{0, 1}, {time.Nanosecond, 1}, {time.Millisecond, 1}, {999 * time.Millisecond, 1},
		{time.Second, 1}, {time.Second + time.Nanosecond, 2}, {59*time.Second + 500*time.Millisecond, 60},
		{-time.Second, 1},
	} {
		if got := RetryAfterSeconds(tc.delay); got != tc.want {
			t.Fatalf("RetryAfterSeconds(%s) = %d, want %d", tc.delay, got, tc.want)
		}
	}
}
