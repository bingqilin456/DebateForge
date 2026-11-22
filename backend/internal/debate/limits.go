package debate

import (
	"sync"
	"time"
)

const maxLimitKeys = 20000

const (
	// perMinuteRequests 是 V1 生成入口的每分钟请求上限（沿用既有行为）。
	perMinuteRequests = 6
	// trainingPerMinuteRequests 是训练赛入口的每分钟请求上限：
	// 一场最多 26 个正式步骤，加显式重试与结束操作，需覆盖一整场正常对局。
	trainingPerMinuteRequests = 40
)

type counter struct {
	minute   time.Time
	requests int
	day      time.Time
	attempts int
	pending  int
	active   bool
}

// Gate 是进程单例；只协调本进程，不提供多副本全局限制或可靠计费账本。
type Gate struct {
	mu     sync.Mutex
	clock  func() time.Time
	users  map[uint64]*counter
	active int
}

func NewGate(clock func() time.Time) *Gate {
	if clock == nil {
		clock = time.Now
	}
	return &Gate{clock: clock, users: make(map[uint64]*counter)}
}
func retry(code string, d time.Duration) error {
	n := int((d + time.Second - 1) / time.Second)
	if n < 1 {
		n = 1
	}
	cause := ErrRateLimited
	if code == "generation_busy" {
		cause = ErrGenerationBusy
	}
	return &Failure{Code: code, Status: 429, RetryAfter: n, Cause: cause}
}
func (g *Gate) entry(id uint64, now time.Time) (*counter, error) {
	day := now.UTC().Truncate(24 * time.Hour)
	if c := g.users[id]; c != nil {
		if !c.day.Equal(day) && c.pending == 0 {
			c.day = day
			c.attempts = 0
		}
		if !now.Before(c.minute.Add(time.Minute)) {
			c.minute = now
			c.requests = 0
		}
		return c, nil
	}
	if len(g.users) >= maxLimitKeys {
		for key, c := range g.users {
			if !c.active && c.pending == 0 && !now.Before(c.minute.Add(time.Minute)) && !c.day.Equal(day) {
				delete(g.users, key)
			}
		}
	}
	if len(g.users) >= maxLimitKeys {
		return nil, retry("rate_limited", time.Minute)
	}
	c := &counter{minute: now, day: day}
	g.users[id] = c
	return c, nil
}

// Request 是 HTTP 防刷，与只对新认领消费的每日额度分开。
func (g *Gate) Request(id uint64) error {
	return g.requestWithin(id, perMinuteRequests)
}

// TrainingRequest 是训练赛入口的 HTTP 防刷。
//
// 训练赛一场最多 26 个正式步骤（自由辩十回合时 2n+6），加上显式重试与结束操作，
// 单人一分钟内完成整场是合法路径；沿用 V1 的每分钟 6 次会把正常对局卡死。
// 因此训练赛入口使用独立的分钟上限，但仍与 V1 共用同一计数器、每日额度与并发槽，
// 不另建一套限流内核。每日额度与单用户在途调用上限不变。
func (g *Gate) TrainingRequest(id uint64) error {
	return g.requestWithin(id, trainingPerMinuteRequests)
}

func (g *Gate) requestWithin(id uint64, limit int) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock().UTC()
	c, e := g.entry(id, now)
	if e != nil {
		return e
	}
	if c.requests >= limit {
		return retry("rate_limited", c.minute.Add(time.Minute).Sub(now))
	}
	c.requests++
	return nil
}

type Permit struct {
	gate *Gate
	user *counter
	once sync.Once
}

func (g *Gate) Acquire(id uint64) (*Permit, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.clock().UTC()
	c, e := g.entry(id, now)
	if e != nil {
		return nil, e
	}
	if c.active || g.active >= 2 {
		return nil, retry("generation_busy", time.Second)
	}
	if c.attempts+c.pending >= 20 {
		return nil, retry("rate_limited", c.day.Add(24*time.Hour).Sub(now))
	}
	c.pending++
	c.active = true
	g.active++
	return &Permit{gate: g, user: c}, nil
}

// Finish 只执行一次，确定回滚不消费额度；不确定提交保守记一次，避免低估费用风险。
func (p *Permit) Finish(consumed bool) {
	if p == nil {
		return
	}
	p.once.Do(func() {
		g := p.gate
		g.mu.Lock()
		defer g.mu.Unlock()
		p.user.pending--
		if consumed {
			p.user.attempts++
		}
		p.user.active = false
		g.active--
	})
}
