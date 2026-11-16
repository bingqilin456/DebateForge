package integration

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

func TestT14CreateStyleStrictAndPersisted(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	for _, raw := range []string{`null`, `""`, `"Logical"`, `"unknown"`, `1`, `true`, `[]`, `{}`} {
		var before, after int64
		f.db.ORM.Table("debates").Where("user_id = ?", f.uid).Count(&before)
		r := f.do(t, "POST", "/api/v1/debates", `{"topic_id":"1","user_stance":"pro","ai_style":`+raw+`}`, integrationOrigin, f.csrf, f.cookie)
		f.db.ORM.Table("debates").Where("user_id = ?", f.uid).Count(&after)
		if r.status != 400 || r.errorCode() != "invalid_request" || before != after {
			t.Fatalf("style %s: status=%d code=%s rows=%d/%d", raw, r.status, r.errorCode(), before, after)
		}
	}
	for _, style := range []string{"", "logical", "aggressive", "empathic", "data"} {
		body := map[string]any{"topic_id": "1", "user_stance": "con", "mode": "quick_3"}
		if style != "" {
			body["ai_style"] = style
		}
		want := style
		if want == "" {
			want = "logical"
		}
		r := f.request(t, "POST", "/api/v1/debates", body)
		var d debate.DebateDTO
		b, _ := json.Marshal(r.data())
		_ = json.Unmarshal(b, &d)
		if r.status != 201 || d.AIStyle != want || d.Mode != "standard_4" || d.CompletedRounds != 0 {
			t.Fatalf("create %s: %d %+v", style, r.status, d)
		}
		var saved debate.Debate
		if err := f.db.ORM.First(&saved, "id = ?", d.ID).Error; err != nil || saved.AIStyle != want {
			t.Fatal("saved style", err, saved.AIStyle)
		}
		r = f.request(t, "GET", "/api/v1/debates/"+d.ID, nil)
		var detail debate.DetailDTO
		b, _ = json.Marshal(r.data())
		_ = json.Unmarshal(b, &detail)
		if r.status != 200 || detail.Debate.AIStyle != want {
			t.Fatal("detail style")
		}
	}
	r := f.request(t, "GET", "/api/v1/debates", nil)
	var items []debate.DebateDTO
	_ = json.Unmarshal(r.data()["items"], &items)
	if r.status != 200 || len(items) != 5 || items[0].AIStyle != "data" {
		t.Fatal("history style")
	}
	if fake.Calls() != 0 {
		t.Fatal("creation/query invoked model")
	}
	// 首轮只能读取已保存流派，不能信任轮次请求再传来的值。
	for _, item := range items {
		r = f.request(t, "POST", "/api/v1/debates/"+item.ID+"/turns", map[string]any{"round_no": 1, "content": "检验保存流派", "client_request_id": uuid.NewString(), "ai_style": "forged"})
		if r.status != 201 {
			t.Fatalf("turn style %d %s", r.status, r.body)
		}
		records := fake.Records()
		record := records[len(records)-1]
		if string(record.Request.AIStyle) != item.AIStyle || !strings.Contains(record.Prompt.Messages[0].Content, ai.AIStyle(item.AIStyle).Label()) {
			t.Fatal("turn ignored persisted style")
		}
	}
}

func TestT14StrategyHTTPContract(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	id := f.create(t)
	path := "/api/v1/debates/" + id + "/strategy"
	r := f.request(t, "GET", path, nil)
	if r.status != 200 || string(r.data()["status"]) != `"not_started"` || string(r.data()["attempt_version"]) != "null" {
		t.Fatalf("GET not_started: %d %s", r.status, r.body)
	}
	for _, raw := range []string{`null`, `[]`, `{"retry":null}`, `{"retry":"true"}`, `{"retry":true}`, `{"retry":true,"expected_attempt_version":1}`, `{"retry":true,"expected_attempt_version":0}`, `{"retry":false,"expected_attempt_version":1}`, `{"expected_attempt_version":null}`, `{"retry":false,"topic":"injected"}`, `{"retry":false,"Retry":true}`, `{"retry":false,"retry":true}`} {
		f.gate = debate.NewGate(nil)
		f.mount(t, fake)
		r = f.do(t, "POST", path, raw, integrationOrigin, f.csrf, f.cookie)
		if r.status != 400 || r.errorCode() != "invalid_request" {
			t.Fatalf("invalid %s: %d %s", raw, r.status, r.body)
		}
	}
	f.gate = debate.NewGate(nil)
	f.mount(t, fake)
	r = f.request(t, "POST", path, map[string]any{})
	if r.status != 201 || string(r.data()["status"]) != `"completed"` || string(r.data()["prompt_version"]) != `"strategy-v1"` || string(r.data()["ai_style"]) != `"logical"` {
		t.Fatalf("strategy: %d %s", r.status, r.body)
	}
	original := string(r.data()["content"])
	for _, method := range []string{"GET", "POST"} {
		var body any
		if method == "POST" {
			body = map[string]any{"retry": true, "expected_attempt_version": 1}
		}
		r = f.request(t, method, path, body)
		if r.status != 200 || string(r.data()["content"]) != original {
			t.Fatalf("replay: %d %s", r.status, r.body)
		}
	}
	if fake.Calls() != 1 {
		t.Fatal("strategy replay called model")
	}
	r = f.request(t, "GET", "/api/v1/debates/"+id, nil)
	var turns []debate.TurnDTO
	_ = json.Unmarshal(r.data()["turns"], &turns)
	if len(turns) != 0 {
		t.Fatal("strategy wrote turns")
	}
}

func strategyStatus(t *testing.T, r apiResult) debate.StrategyStatus {
	t.Helper()
	var status debate.StrategyStatus
	b, _ := json.Marshal(r.data())
	if err := json.Unmarshal(b, &status); err != nil {
		t.Fatal(err)
	}
	return status
}
func strategyPath(f *debateFixture, t *testing.T) string {
	return "/api/v1/debates/" + f.create(t) + "/strategy"
}
func expectStrategy(t *testing.T, r apiResult, httpStatus int, status string, version uint64) {
	t.Helper()
	s := strategyStatus(t, r)
	if r.status != httpStatus || s.Status != status || s.AttemptVersion == nil || *s.AttemptVersion != version {
		t.Fatalf("wanted %d %s v%d, got %d %s", httpStatus, status, version, r.status, r.body)
	}
}
func TestT14StrategyFailureRetryVersionAndSnapshot(t *testing.T) {
	fake := ai.NewFake(ai.FakeStep{Err: &ai.Error{Code: ai.CodeModelTimeout}}, ai.FakeStep{Result: ai.Result{Content: `{}`, Usage: ai.Usage{PromptTokens: ai.TokenCount{Known: true, Value: 9}}}})
	f := newDebateFixture(t, fake)
	r := f.request(t, "POST", "/api/v1/debates", map[string]any{"topic_id": "1", "user_stance": "con", "ai_style": "aggressive"})
	var id string
	_ = json.Unmarshal(r.data()["id"], &id)
	path := "/api/v1/debates/" + id + "/strategy"
	r = f.request(t, "POST", path, map[string]any{})
	if r.status != 504 || r.errorCode() != "model_timeout" || !strings.Contains(string(r.body), path) {
		t.Fatalf("timeout %d %s", r.status, r.body)
	}
	expectStrategy(t, f.request(t, "GET", path, nil), 200, "failed", 1)
	expectStrategy(t, f.request(t, "POST", path, map[string]any{}), 200, "failed", 1)
	r = f.request(t, "POST", path, map[string]any{"retry": true, "expected_attempt_version": 1})
	if r.status != 502 || r.errorCode() != "model_error" {
		t.Fatalf("invalid output %d %s", r.status, r.body)
	}
	r = f.request(t, "POST", path, map[string]any{"retry": true, "expected_attempt_version": 1})
	if r.status != 409 || r.errorCode() != "attempt_version_conflict" || !strings.Contains(string(r.body), `"attempt_version":2`) {
		t.Fatalf("old version %d %s", r.status, r.body)
	}
	r = f.request(t, "POST", path, map[string]any{"retry": true, "expected_attempt_version": 2})
	expectStrategy(t, r, 200, "completed", 3)
	if fake.Calls() != 3 {
		t.Fatal("retry called model more than once")
	}
	for _, record := range fake.Records() {
		if record.Request.UserStance != ai.StanceCon || record.Request.AIStyle != ai.StyleAggressive || record.Prompt.Version != "strategy-v1" {
			t.Fatal("lost immutable snapshot")
		}
	}
	var saved debate.Strategy
	if err := f.db.ORM.First(&saved, "debate_id = ?", id).Error; err != nil || saved.PromptVersion == nil || *saved.PromptVersion != "strategy-v1" || saved.SchemaVersion == nil || *saved.SchemaVersion != 1 || saved.ModelName == nil || *saved.ModelName != "fake" {
		t.Fatal("metadata not persisted", err)
	}
	// 重建 Service 模拟刷新/服务重启，重读不能覆盖保存的版本或调用模型。
	f.mount(t, ai.NewFake())
	expectStrategy(t, f.request(t, "GET", path, nil), 200, "completed", 3)
	r = f.request(t, "POST", "/api/v1/debates/"+id+"/turns", submit("直接进入第一轮", uuid.NewString(), 1))
	if r.status != 201 {
		t.Fatalf("round after strategy %d %s", r.status, r.body)
	}
}

func TestT14StrategyOwnershipAndWriteProtection(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	path := strategyPath(f, t)
	for _, tc := range []struct {
		method, origin, csrf string
		cookie               *http.Cookie
		code                 int
	}{
		{"GET", "", "", nil, 401}, {"POST", integrationOrigin, "", nil, 401},
		{"POST", integrationOrigin, "", f.cookie, 403}, {"POST", "http://evil.invalid", f.csrf, f.cookie, 403},
	} {
		cookies := []*http.Cookie{}
		if tc.cookie != nil {
			cookies = append(cookies, tc.cookie)
		}
		r := f.do(t, tc.method, path, `{}`, tc.origin, tc.csrf, cookies...)
		if r.status != tc.code {
			t.Fatalf("write protection: %d %s", r.status, r.body)
		}
	}
	user := f.randomUsername(t)
	r, _ := f.register(t, user, integrationPassword)
	if r.status != 201 {
		t.Fatal("register")
	}
	r, cookie, csrf := f.login(t, user, integrationPassword)
	if r.status != 200 {
		t.Fatal("login")
	}
	for _, method := range []string{"GET", "POST"} {
		r = f.do(t, method, path, `{}`, integrationOrigin, csrf, cookie)
		if r.status != 404 || r.errorCode() != "not_found" {
			t.Fatalf("ownership %d %s", r.status, r.body)
		}
		r = f.request(t, method, "/api/v1/debates/18446744073709551614/strategy", map[string]any{})
		if r.status != 404 {
			t.Fatal("missing resource")
		}
	}
	r = f.do(t, "POST", path, `{"retry":false,"x":"`+strings.Repeat("x", 32768)+`"}`, integrationOrigin, f.csrf, f.cookie)
	if r.status != 413 {
		t.Fatalf("body limit %d", r.status)
	}
	var count int64
	f.db.ORM.Table("debate_strategies").Where("debate_id = ?", strings.Split(path, "/")[4]).Count(&count)
	if count != 0 || fake.Calls() != 0 {
		t.Fatal("rejected request produced side effect")
	}
}

type strategyBlockingGenerator struct {
	*ai.Fake
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func (g *strategyBlockingGenerator) GenerateStrategy(ctx context.Context, req ai.StrategyRequest) (ai.StrategyResult, error) {
	g.calls.Add(1)
	g.started <- struct{}{}
	select {
	case <-ctx.Done():
		return ai.StrategyResult{}, ctx.Err()
	case <-g.release:
		return g.Fake.GenerateStrategy(ctx, req)
	}
}
func TestT14StrategyConcurrentReplayAndSharedGate(t *testing.T) {
	g := &strategyBlockingGenerator{Fake: ai.NewFake(), started: make(chan struct{}, 1), release: make(chan struct{})}
	f := newDebateFixture(t, g)
	path := strategyPath(f, t)
	defer close(g.release)
	done := make(chan apiResult, 1)
	go func() { done <- f.request(t, "POST", path, map[string]any{}) }()
	select {
	case <-g.started:
	case <-time.After(3 * time.Second):
		t.Fatal("not started")
	}
	expectStrategy(t, f.request(t, "GET", path, nil), 200, "generating", 1)
	expectStrategy(t, f.request(t, "POST", path, map[string]any{}), 202, "generating", 1)
	expectStrategy(t, f.request(t, "POST", path, map[string]any{"retry": true, "expected_attempt_version": 1}), 202, "generating", 1)
	r := f.request(t, "POST", strings.TrimSuffix(path, "/strategy")+"/turns", submit("首轮与策略共享并发槽", uuid.NewString(), 1))
	if r.status != 429 || r.errorCode() != "generation_busy" || r.header.Get("Retry-After") == "" {
		t.Fatalf("gate %d %s", r.status, r.body)
	}
	g.release <- struct{}{}
	expectStrategy(t, <-done, 201, "completed", 1)
	if g.calls.Load() != 1 {
		t.Fatal("duplicate model calls")
	}
	r = f.request(t, "POST", strings.TrimSuffix(path, "/strategy")+"/turns", submit("释放后首轮可发送", uuid.NewString(), 1))
	if r.status != 201 {
		t.Fatalf("first turn %d %s", r.status, r.body)
	}
	expectStrategy(t, f.request(t, "POST", path, map[string]any{}), 200, "completed", 1)
}

func TestT14StrategyAfterAnyTurnAndSkip(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "failed"}[failure], func(t *testing.T) {
			fake := ai.NewFake()
			if failure {
				fake = ai.NewFake(ai.FakeStep{Err: &ai.Error{Code: ai.CodeModelTimeout}})
			}
			f := newDebateFixture(t, fake)
			id := f.create(t)
			base := "/api/v1/debates/" + id
			r := f.request(t, "POST", base+"/turns", submit("跳过策略直接辩论", uuid.NewString(), 1))
			if (!failure && r.status != 201) || (failure && r.status != 504) {
				t.Fatalf("turn %d %s", r.status, r.body)
			}
			r = f.request(t, "POST", base+"/strategy", map[string]any{})
			if r.status != 409 || r.errorCode() != "round_conflict" || fake.Calls() != 1 {
				t.Fatalf("strategy after turn %d %s", r.status, r.body)
			}
		})
	}
}

func TestT14StrategyLeaseRecoveryAndLateOutput(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	id := f.create(t)
	var d debate.Debate
	f.db.ORM.First(&d, "id = ?", id)
	oldTime := time.Now().UTC().Add(-time.Minute)
	old := debate.Strategy{DebateID: d.ID, Status: "generating", AttemptVersion: 1, LeaseExpiresAt: &oldTime, CreatedAt: oldTime, UpdatedAt: oldTime}
	if err := f.db.ORM.Create(&old).Error; err != nil {
		t.Fatal(err)
	}
	// 新服务的启动扫描应回收过期策略。
	f.mount(t, fake)
	path := "/api/v1/debates/" + id + "/strategy"
	expectStrategy(t, f.request(t, "GET", path, nil), 200, "failed", 1)
	expectStrategy(t, f.request(t, "POST", path, map[string]any{"retry": true, "expected_attempt_version": 1}), 200, "completed", 2)
	repo := debate.NewRepository(f.db.ORM)
	_, err := repo.CompleteStrategy(context.Background(), f.uid, d, old, ai.StrategyResult{ContentJSON: `{"thesis":"迟到","arguments":["旧"],"risks":["旧"]}`})
	if !errors.Is(err, debate.ErrRoundConflict) {
		t.Fatal("late completion overwrote new version", err)
	}
	_, err = repo.FailStrategy(context.Background(), f.uid, d, old, "model_timeout", ai.Usage{}, ai.Metadata{})
	if !errors.Is(err, debate.ErrRoundConflict) {
		t.Fatal("late failure overwrote new version", err)
	}
	expectStrategy(t, f.request(t, "GET", path, nil), 200, "completed", 2)
}

func TestT14StrategyClaimRollbackAndCompletionFailure(t *testing.T) {
	for _, stage := range []string{"claim", "complete"} {
		t.Run(stage, func(t *testing.T) {
			fake := ai.NewFake()
			f := newDebateFixture(t, fake)
			path := strategyPath(f, t)
			orm := f.db.ORM.Session(&gorm.Session{NewDB: true})
			callback := "t14_" + stage
			if stage == "claim" {
				orm.Callback().Create().Before("gorm:create").Register(callback, func(tx *gorm.DB) {
					if tx.Statement.Table == "debate_strategies" {
						tx.AddError(errors.New("synthetic write failure"))
					}
				})
				defer orm.Callback().Create().Remove(callback)
			} else {
				orm.Callback().Update().Before("gorm:update").Register(callback, func(tx *gorm.DB) {
					values, ok := tx.Statement.Dest.(map[string]any)
					if tx.Statement.Table == "debate_strategies" && ok && values["status"] == "completed" {
						tx.AddError(errors.New("synthetic final write failure"))
					}
				})
				defer orm.Callback().Update().Remove(callback)
			}
			f.mountDB(t, fake, orm)
			r := f.request(t, "POST", path, map[string]any{})
			if r.status != 503 {
				t.Fatalf("write failure %d %s", r.status, r.body)
			}
			s := strategyStatus(t, f.request(t, "GET", path, nil))
			if stage == "claim" && (s.Status != "not_started" || fake.Calls() != 0) {
				t.Fatal("claim rollback")
			}
			if stage == "complete" && (s.Status != "failed" || fake.Calls() != 1 || s.ErrorCode == nil || *s.ErrorCode != "dependency_unavailable") {
				t.Fatal("completion rollback")
			}
			p, err := f.gate.Acquire(f.uid)
			if err != nil {
				t.Fatal("permit leaked", err)
			}
			p.Finish(false)
		})
	}
}

func TestT14StrategyCancellationAndDisabled(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		f := newDebateFixture(t, testGenerator(func(context.Context, ai.Request) (ai.Result, error) {
			t.Error("turn generator called for strategy")
			return ai.Result{}, nil
		}))
		path := strategyPath(f, t)
		r := f.request(t, "POST", path, map[string]any{})
		if r.status != 503 || r.errorCode() != "model_unavailable" {
			t.Fatalf("disabled %d %s", r.status, r.body)
		}
		expectStrategy(t, f.request(t, "GET", path, nil), 200, "failed", 1)
	})
	t.Run("cancelled", func(t *testing.T) {
		g := &strategyBlockingGenerator{Fake: ai.NewFake(), started: make(chan struct{}, 1), release: make(chan struct{})}
		f := newDebateFixture(t, g)
		path := strategyPath(f, t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		req, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+path, strings.NewReader(`{}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", integrationOrigin)
		req.Header.Set("X-CSRF-Token", f.csrf)
		req.AddCookie(f.cookie)
		done := make(chan error, 1)
		go func() {
			resp, err := f.client.Do(req)
			if resp != nil {
				resp.Body.Close()
			}
			done <- err
		}()
		select {
		case <-g.started:
		case <-time.After(3 * time.Second):
			t.Fatal("not started")
		}
		cancel()
		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatal("network cancel", err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for {
			current := strategyStatus(t, f.request(t, "GET", path, nil))
			if current.Status == "failed" {
				if current.ErrorCode == nil || *current.ErrorCode != "generation_cancelled" {
					t.Fatal("cancel code")
				}
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("cancel did not persist")
			}
			time.Sleep(10 * time.Millisecond)
		}
		f.mount(t, ai.NewFake())
		expectStrategy(t, f.request(t, "POST", path, map[string]any{"retry": true, "expected_attempt_version": 1}), 200, "completed", 2)
	})
}

func TestT14StrategyDailyQuotaSharedWithTurnsAndReplayExemption(t *testing.T) {
	steps := make([]ai.FakeStep, 20)
	for i := range steps {
		steps[i].Err = &ai.Error{Code: ai.CodeModelError}
	}
	fake := ai.NewFake(steps...)
	f := newDebateFixture(t, fake)
	var moment atomic.Int64
	moment.Store(time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC).Unix())
	f.gate = debate.NewGate(func() time.Time { return time.Unix(moment.Load(), 0) })
	f.mount(t, fake)
	first := ""
	for i := 0; i < 20; i++ {
		path := strategyPath(f, t)
		if i == 0 {
			first = path
		}
		r := f.request(t, "POST", path, map[string]any{})
		if r.status != 502 {
			t.Fatalf("attempt %d: %d %s", i, r.status, r.body)
		}
		moment.Add(11)
	}
	expectStrategy(t, f.request(t, "POST", first, map[string]any{}), 200, "failed", 1)
	other := strategyPath(f, t)
	r := f.request(t, "POST", other, map[string]any{})
	if r.status != 429 || r.errorCode() != "rate_limited" || r.header.Get("Retry-After") == "" {
		t.Fatalf("quota %d %s", r.status, r.body)
	}
	r = f.request(t, "POST", strings.TrimSuffix(other, "/strategy")+"/turns", submit("轮次共享每日额度", uuid.NewString(), 1))
	if r.status != 429 || fake.Calls() != 20 {
		t.Fatalf("turn quota %d %s", r.status, r.body)
	}
	s := strategyStatus(t, f.request(t, "GET", other, nil))
	if s.Status != "not_started" {
		t.Fatal("quota rejected wrote strategy")
	}
}

func TestT14StrategyMinuteLimitCountsInvalidInput(t *testing.T) {
	fake := ai.NewFake()
	f := newDebateFixture(t, fake)
	path := strategyPath(f, t)
	for i := 0; i < 6; i++ {
		r := f.request(t, "POST", path, map[string]any{"retry": "invalid"})
		if r.status != 400 {
			t.Fatal("invalid input", r.status)
		}
	}
	r := f.request(t, "POST", path, map[string]any{})
	if r.status != 429 || r.header.Get("Retry-After") == "" || fake.Calls() != 0 {
		t.Fatalf("minute gate %d %s", r.status, r.body)
	}
}

func TestT14StrategyCommitUncertainty(t *testing.T) {
	for _, loseAt := range []int32{1, 2} {
		t.Run(string(rune('0'+loseAt)), func(t *testing.T) {
			fake := ai.NewFake()
			f := newDebateFixture(t, fake)
			path := strategyPath(f, t)
			pool, _ := f.db.ORM.DB()
			f.mountDB(t, fake, wrappedORM(t, f, &acknowledgedCommitPool{DB: pool, loseAt: loseAt}))
			r := f.request(t, "POST", path, map[string]any{})
			if r.status != 503 || r.errorCode() != "dependency_unavailable" || strings.Contains(string(r.body), `"details"`) {
				t.Fatalf("unknown commit %d %s", r.status, r.body)
			}
			wantHTTP, wantStatus, wantCalls := 202, "generating", 0
			if loseAt == 2 {
				wantHTTP, wantStatus, wantCalls = 200, "completed", 1
			}
			expectStrategy(t, f.request(t, "GET", path, nil), 200, wantStatus, 1)
			expectStrategy(t, f.request(t, "POST", path, map[string]any{}), wantHTTP, wantStatus, 1)
			if fake.Calls() != wantCalls {
				t.Fatal("uncertain commit duplicated model")
			}
		})
	}
}

func TestT14StrategyAndFirstTurnSerializeOnDebate(t *testing.T) {
	f := newDebateFixture(t, ai.NewFake())
	repo := debate.NewRepository(f.db.ORM)
	for i := 0; i < 8; i++ {
		id := f.create(t)
		debateID, err := debate.ParseID(id)
		if err != nil {
			t.Fatal(err)
		}
		start := make(chan struct{})
		strategyDone := make(chan error, 1)
		turnDone := make(chan error, 1)
		go func() {
			<-start
			_, _, err := repo.ClaimStrategy(context.Background(), f.uid, debateID, debate.StrategyInput{}, func() error { return nil })
			strategyDone <- err
		}()
		go func() {
			<-start
			_, _, err := repo.Claim(context.Background(), f.uid, debateID, debate.SubmitInput{RoundNo: 1, Content: "并发首轮", ClientRequestID: uuid.NewString()}, func() error { return nil })
			turnDone <- err
		}()
		close(start)
		strategyErr, turnErr := <-strategyDone, <-turnDone
		if turnErr != nil || (strategyErr != nil && !errors.Is(strategyErr, debate.ErrRoundConflict)) {
			t.Fatal("race", strategyErr, turnErr)
		}
		state, err := repo.GetStrategy(context.Background(), f.uid, debateID)
		if err != nil {
			t.Fatal(err)
		}
		if strategyErr == nil && (state.Strategy == nil || state.Strategy.Status != "generating") {
			t.Fatal("strategy claimed before turn must survive")
		}
		if strategyErr != nil && state.Strategy != nil {
			t.Fatal("turn won but strategy still inserted")
		}
		// 两者均可认领只可能是策略先持锁；策略不成为首轮强制前置。
		_, _, err = repo.ClaimStrategy(context.Background(), f.uid, debateID, debate.StrategyInput{}, func() error { t.Error("replay acquired quota"); return nil })
		if state.Strategy != nil && err != nil {
			t.Fatal("inflight replay blocked by first turn", err)
		}
	}
}
