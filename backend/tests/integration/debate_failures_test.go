package integration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/google/uuid"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestT05CancelDuringFinalWritePreservesMetadata(t *testing.T) {
	usage := ai.Usage{PromptTokens: ai.KnownCount(100), CompletionTokens: ai.KnownCount(10)}
	meta := ai.Metadata{Model: "synthetic-model", PromptVersion: ai.PromptVersion, LatencyMS: 123}
	generator := testGenerator(func(context.Context, ai.Request) (ai.Result, error) {
		return ai.Result{Content: "已生成回复", Usage: usage, Metadata: meta}, nil
	})
	f := newDebateFixture(t, generator)
	id := f.create(t)
	pool, _ := f.db.ORM.DB()
	orm := wrappedORM(t, f, pool)
	writing := make(chan struct{})
	orm.Callback().Create().Before("gorm:create").Register("t05_cancel_final_write", func(tx *gorm.DB) {
		if tx.Statement.Table == "messages" {
			if message, ok := tx.Statement.Dest.(*debate.Message); ok && message.Role == "assistant" {
				close(writing)
				<-tx.Statement.Context.Done()
			}
		}
	})
	f.mountDB(t, generator, orm)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	body, _ := json.Marshal(submit("最终写回取消", uuid.NewString(), 1))
	request, _ := http.NewRequestWithContext(ctx, "POST", f.server.URL+"/api/v1/debates/"+id+"/turns", strings.NewReader(string(body)))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", integrationOrigin)
	request.Header.Set("X-CSRF-Token", f.csrf)
	request.AddCookie(f.cookie)
	done := make(chan error, 1)
	go func() {
		r, e := f.client.Do(request)
		if r != nil {
			r.Body.Close()
		}
		done <- e
	}()
	select {
	case <-writing:
	case <-time.After(4 * time.Second):
		t.Fatal("final write did not start")
	}
	cancel()
	if e := <-done; !errors.Is(e, context.Canceled) {
		t.Fatal("expected client cancellation")
	}
	deadline := time.Now().Add(4 * time.Second)
	for {
		r := f.request(t, "GET", "/api/v1/debates/"+id, nil)
		var turns []debate.TurnDTO
		_ = json.Unmarshal(r.data()["turns"], &turns)
		if len(turns) == 1 && turns[0].Status == "failed" {
			if turns[0].ErrorCode == nil || *turns[0].ErrorCode != "generation_cancelled" || len(turns[0].Messages) != 1 {
				t.Fatal("incorrect final cancellation state")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("final cancellation left generating")
		}
		time.Sleep(10 * time.Millisecond)
	}
	var saved struct {
		InputTokens   *int
		OutputTokens  *int
		ModelName     *string
		PromptVersion *string
		LatencyMS     *int
	}
	if e := f.db.ORM.Table("debate_turns").Where("debate_id = ?", id).First(&saved).Error; e != nil {
		t.Fatal("read cancellation metadata")
	}
	if saved.InputTokens == nil || *saved.InputTokens != 100 || saved.OutputTokens == nil || *saved.OutputTokens != 10 || saved.ModelName == nil || *saved.ModelName != meta.Model || saved.PromptVersion == nil || *saved.PromptVersion != ai.PromptVersion || saved.LatencyMS == nil || *saved.LatencyMS != 123 {
		t.Fatal("known metadata not persisted")
	}
}

func TestT05DailyAttemptsAndReplayExemption(t *testing.T) {
	var calls atomic.Int32
	g := testGenerator(func(context.Context, ai.Request) (ai.Result, error) {
		calls.Add(1)
		return ai.Result{}, &ai.Error{Code: ai.CodeModelError}
	})
	f := newDebateFixture(t, g)
	var moment atomic.Int64
	moment.Store(time.Date(2026, 11, 8, 1, 0, 0, 0, time.UTC).Unix())
	f.gate = debate.NewGate(func() time.Time { return time.Unix(moment.Load(), 0) })
	f.mount(t, g)
	first, key := "", uuid.NewString()
	for i := 0; i < 20; i++ {
		id := f.create(t)
		k := uuid.NewString()
		if i == 0 {
			first = id
			k = key
		}
		r := f.request(t, "POST", "/api/v1/debates/"+id+"/turns", submit("每日额度", k, 1))
		if r.status != 502 {
			t.Fatalf("attempt %d status %d", i, r.status)
		}
		moment.Add(11)
	}
	r := f.request(t, "POST", "/api/v1/debates/"+first+"/turns", submit("每日额度", key, 1))
	if r.status != 409 || r.errorCode() != "turn_failed" {
		t.Fatal("daily quota blocked replay")
	}
	id := f.create(t)
	r = f.request(t, "POST", "/api/v1/debates/"+id+"/turns", submit("第21次", uuid.NewString(), 1))
	if r.status != 429 || r.errorCode() != "rate_limited" || r.header.Get("Retry-After") == "" || calls.Load() != 20 {
		t.Fatal("daily attempt quota")
	}
	var count int64
	f.db.ORM.Table("debate_turns").Where("debate_id = ?", id).Count(&count)
	if count != 0 {
		t.Fatal("quota rejection wrote turn")
	}
}

// acknowledgedCommitPool 真实提交 MySQL 后，测试性丢失指定次提交的确认。
// 它不伪造业务查询，验证客户端查询确实能看到已经提交的结果。
type acknowledgedCommitPool struct {
	*sql.DB
	commits atomic.Int32
	loseAt  int32
}

func (p *acknowledgedCommitPool) BeginTx(c context.Context, o *sql.TxOptions) (gorm.ConnPool, error) {
	tx, e := p.DB.BeginTx(c, o)
	if e != nil {
		return nil, e
	}
	return &acknowledgedTx{Tx: tx, pool: p}, nil
}

type acknowledgedTx struct {
	*sql.Tx
	pool *acknowledgedCommitPool
}

func (t *acknowledgedTx) Commit() error {
	e := t.Tx.Commit()
	if e == nil && t.pool.commits.Add(1) == t.pool.loseAt {
		return errors.New("synthetic lost commit acknowledgement")
	}
	return e
}
func wrappedORM(t *testing.T, f *debateFixture, pool gorm.ConnPool) *gorm.DB {
	t.Helper()
	orm, e := gorm.Open(gormmysql.New(gormmysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Discard, NowFunc: func() time.Time { return time.Now().UTC() }})
	if e != nil {
		t.Fatal("wrap test pool")
	}
	return orm
}

func TestT05CommitUncertaintyAndKnownRollback(t *testing.T) {
	for _, loseAt := range []int32{1, 2} {
		t.Run(string(rune('0'+loseAt)), func(t *testing.T) {
			fake := ai.NewFake()
			f := newDebateFixture(t, fake)
			id := f.create(t)
			pool, e := f.db.ORM.DB()
			if e != nil {
				t.Fatal(e)
			}
			wrapped := wrappedORM(t, f, &acknowledgedCommitPool{DB: pool, loseAt: loseAt})
			f.mountDB(t, fake, wrapped)
			key := uuid.NewString()
			path := "/api/v1/debates/" + id
			r := f.request(t, "POST", path+"/turns", submit("提交确认丢失", key, 1))
			if r.status != 503 || r.errorCode() != "dependency_unavailable" {
				t.Fatal("unknown commit must be 503")
			}
			var envelope struct {
				Error struct {
					Details json.RawMessage `json:"details"`
				} `json:"error"`
			}
			_ = json.Unmarshal(r.body, &envelope)
			if len(envelope.Error.Details) != 0 {
				t.Fatal("unknown commit claimed confirmed failure")
			}
			r = f.request(t, "GET", path, nil)
			var turns []debate.TurnDTO
			_ = json.Unmarshal(r.data()["turns"], &turns)
			if r.status != 200 || len(turns) != 1 {
				t.Fatal("query lost committed turn")
			}
			wantStatus, wantCalls := "generating", 0
			if loseAt == 2 {
				wantStatus, wantCalls = "completed", 1
			}
			if turns[0].Status != wantStatus || len(fake.Records()) != wantCalls {
				t.Fatal("commit result or generation count")
			}
			r = f.request(t, "POST", path+"/turns", submit("提交确认丢失", key, 1))
			wantHTTP := 202
			if loseAt == 2 {
				wantHTTP = 200
			}
			if r.status != wantHTTP || len(fake.Records()) != wantCalls {
				t.Fatal("uncertain replay generated again")
			}
		})
	}
	t.Run("known_rollback", func(t *testing.T) {
		fake := ai.NewFake()
		f := newDebateFixture(t, fake)
		id := f.create(t)
		pool, _ := f.db.ORM.DB()
		orm := wrappedORM(t, f, pool)
		var once atomic.Bool
		orm.Callback().Create().Before("gorm:create").Register("t05_fail_user_message", func(tx *gorm.DB) {
			if tx.Statement.Table == "messages" && !once.Swap(true) {
				tx.AddError(errors.New("synthetic write failure"))
			}
		})
		f.mountDB(t, fake, orm)
		r := f.request(t, "POST", "/api/v1/debates/"+id+"/turns", submit("回滚", uuid.NewString(), 1))
		if r.status != 503 || len(fake.Records()) != 0 {
			t.Fatal("rollback generated")
		}
		var count int64
		f.db.ORM.Table("debate_turns").Where("debate_id = ?", id).Count(&count)
		if count != 0 {
			t.Fatal("rollback kept turn")
		}
		r = f.request(t, "POST", "/api/v1/debates/"+id+"/turns", submit("回滚后新尝试", uuid.NewString(), 1))
		if r.status != 201 {
			t.Fatal("rollback leaked slot")
		}
	})
}

func TestT05LateVersionCannotOverwrite(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := newDebateFixture(t, testGenerator(func(c context.Context, r ai.Request) (ai.Result, error) {
		close(started)
		select {
		case <-release:
			return ai.NewFake().Generate(c, r)
		case <-c.Done():
			return ai.Result{}, c.Err()
		}
	}))
	defer close(release)
	id := f.create(t)
	done := make(chan apiResult, 1)
	go func() {
		done <- f.request(t, "POST", "/api/v1/debates/"+id+"/turns", submit("版本保护", uuid.NewString(), 1))
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("version start")
	}
	if e := f.db.ORM.Exec("UPDATE debate_turns SET attempt_version=2 WHERE debate_id=?", id).Error; e != nil {
		t.Fatal("test version update")
	}
	release <- struct{}{}
	if (<-done).status != 503 {
		t.Fatal("stale version reported success")
	}
	var count int64
	f.db.ORM.Table("messages").Joins("JOIN debate_turns ON messages.turn_id=debate_turns.id").Where("debate_turns.debate_id=? AND messages.role='assistant'", id).Count(&count)
	if count != 0 {
		t.Fatal("stale version wrote assistant")
	}
}

func TestT09ExplicitRetryVersionConflictAndOwnership(t *testing.T) {
	var calls atomic.Int32
	step := atomic.Int32{}
	gen := testGenerator(func(c context.Context, r ai.Request) (ai.Result, error) {
		n := step.Add(1)
		calls.Add(1)
		switch n {
		case 1:
			return ai.NewFake().Generate(c, r)
		case 2:
			return ai.Result{}, &ai.Error{Code: ai.CodeModelTimeout}
		case 3:
			return ai.Result{}, &ai.Error{Code: ai.CodeModelError}
		default:
			return ai.NewFake().Generate(c, r)
		}
	})
	f := newDebateFixture(t, gen)
	var moment atomic.Int64
	moment.Store(time.Date(2026, 11, 11, 2, 0, 0, 0, time.UTC).Unix())
	f.gate = debate.NewGate(func() time.Time { return time.Unix(moment.Load(), 0) })
	f.mount(t, gen)

	id := f.create(t)
	otherDebate := f.create(t)
	path := "/api/v1/debates/" + id

	if r := f.request(t, "POST", path+"/turns", submit("第一轮原文", uuid.NewString(), 1)); r.status != 201 {
		t.Fatalf("round 1 status=%d", r.status)
	}
	moment.Add(15)

	r := f.request(t, "POST", path+"/turns", submit("第二轮原文保留", uuid.NewString(), 2))
	if r.status != 504 {
		t.Fatalf("round 2 timeout status=%d", r.status)
	}
	var failEnv struct {
		Error struct {
			Details *debate.FailureDetails `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.body, &failEnv)
	if failEnv.Error.Details == nil || failEnv.Error.Details.AttemptVersion != 1 {
		t.Fatalf("missing failure details: %+v", failEnv.Error.Details)
	}
	turnID := failEnv.Error.Details.TurnID
	retryPath := path + "/turns/" + turnID + "/retry"
	moment.Add(15)

	for _, bad := range []any{map[string]any{}, map[string]any{"expected_attempt_version": 0}} {
		if r = f.request(t, "POST", retryPath, bad); r.status != 400 || r.errorCode() != "invalid_request" {
			t.Fatalf("invalid retry input status=%d code=%s", r.status, r.errorCode())
		}
		moment.Add(15)
	}

	if r = f.request(t, "POST", "/api/v1/debates/"+otherDebate+"/turns/"+turnID+"/retry", map[string]any{"expected_attempt_version": 1}); r.status != 404 {
		t.Fatalf("mismatched debate turn_id status=%d", r.status)
	}
	moment.Add(15)

	r = f.request(t, "POST", retryPath, map[string]any{"expected_attempt_version": 1})
	if r.status != 502 {
		t.Fatalf("first retry status=%d", r.status)
	}
	callsAfterSecondFail := calls.Load()
	moment.Add(15)

	r = f.request(t, "POST", retryPath, map[string]any{"expected_attempt_version": 1})
	_ = json.Unmarshal(r.body, &failEnv)
	if r.status != 409 || r.errorCode() != "attempt_version_conflict" || failEnv.Error.Details == nil || failEnv.Error.Details.AttemptVersion != 2 || calls.Load() != callsAfterSecondFail {
		t.Fatalf("expected 409 attempt_version_conflict without model call, status=%d code=%s calls=%d", r.status, r.errorCode(), calls.Load())
	}
	moment.Add(15)

	r = f.request(t, "POST", retryPath, map[string]any{"expected_attempt_version": 2})
	if r.status != 200 {
		t.Fatalf("second retry status=%d code=%s", r.status, r.errorCode())
	}
	var turnData debate.TurnData
	b, _ := json.Marshal(r.data())
	_ = json.Unmarshal(b, &turnData)
	if turnData.CompletedRounds != 2 || turnData.Turn.AttemptVersion != 3 || turnData.Turn.Status != "completed" || len(turnData.Turn.Messages) != 2 || turnData.Turn.Messages[0].Content != "第二轮原文保留" {
		t.Fatalf("unexpected retry turn data: %+v", turnData)
	}
	callsAfterSuccess := calls.Load()
	moment.Add(15)

	r = f.request(t, "POST", retryPath, map[string]any{"expected_attempt_version": 2})
	if r.status != 200 || calls.Load() != callsAfterSuccess {
		t.Fatalf("completed retry replay status=%d calls=%d", r.status, calls.Load())
	}
}

func TestT09LeaseRecoveryOnStartupAndTicker(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	f := newDebateFixture(t, testGenerator(func(c context.Context, r ai.Request) (ai.Result, error) {
		close(started)
		select {
		case <-release:
			return ai.NewFake().Generate(c, r)
		case <-c.Done():
			return ai.Result{}, c.Err()
		}
	}))
	defer close(release)
	id := f.create(t)
	path := "/api/v1/debates/" + id
	done := make(chan apiResult, 1)
	go func() {
		done <- f.request(t, "POST", path+"/turns", submit("模拟崩溃残留", uuid.NewString(), 1))
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not start")
	}
	expired := time.Now().UTC().Add(-2 * time.Second)
	if err := f.db.ORM.Exec("UPDATE debate_turns SET lease_expires_at = ? WHERE debate_id = ?", expired, id).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := f.service.RecoverExpiredLeases(context.Background())
	if err != nil || recovered != 1 {
		t.Fatalf("expected 1 recovered lease, got %d err=%v", recovered, err)
	}
	r := f.request(t, "GET", path, nil)
	var detail debate.DetailDTO
	b, _ := json.Marshal(r.data())
	_ = json.Unmarshal(b, &detail)
	if len(detail.Turns) != 1 || detail.Turns[0].Status != "failed" || detail.Turns[0].ErrorCode == nil || *detail.Turns[0].ErrorCode != "model_timeout" {
		t.Fatalf("lease recovery did not mark failed: %+v", detail.Turns)
	}
	release <- struct{}{}
	if (<-done).status != 503 {
		t.Fatal("expired attempt reported success after lease recovery")
	}
	f.mount(t, ai.NewFake())
	r = f.request(t, "POST", path+"/turns/"+detail.Turns[0].ID+"/retry", map[string]any{"expected_attempt_version": 1})
	if r.status != 200 {
		t.Fatalf("retry after lease recovery status=%d code=%s", r.status, r.errorCode())
	}
}

type testCombinedGenerator struct {
	genTurn    func(context.Context, ai.Request) (ai.Result, error)
	genSummary func(context.Context, ai.SummaryRequest) (ai.SummaryResult, error)
}

func (g testCombinedGenerator) Generate(c context.Context, r ai.Request) (ai.Result, error) {
	if g.genTurn != nil {
		return g.genTurn(c, r)
	}
	return ai.NewFake().Generate(c, r)
}

func (g testCombinedGenerator) GenerateSummary(c context.Context, r ai.SummaryRequest) (ai.SummaryResult, error) {
	if g.genSummary != nil {
		return g.genSummary(c, r)
	}
	return ai.NewFake().GenerateSummary(c, r)
}

func TestT10SummaryRetryVersionConflictPendingAndLeaseRecovery(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var summaryCalls atomic.Int32
	step := atomic.Int32{}

	gen := testCombinedGenerator{
		genSummary: func(c context.Context, r ai.SummaryRequest) (ai.SummaryResult, error) {
			n := step.Add(1)
			summaryCalls.Add(1)
			switch n {
			case 1:
				close(started)
				select {
				case <-release:
					return ai.NewFake().GenerateSummary(c, r)
				case <-c.Done():
					return ai.SummaryResult{}, c.Err()
				}
			case 2:
				// 返回非法 Schema JSON（空数组），验证转受控 502 model_error
				return ai.NewFake(ai.FakeStep{Result: ai.Result{Content: `{"strengths":[],"issues":[],"suggestions":[]}`}}).GenerateSummary(c, r)
			default:
				return ai.NewFake().GenerateSummary(c, r)
			}
		},
	}

	f := newDebateFixture(t, gen)
	defer close(release)
	var moment atomic.Int64
	moment.Store(time.Date(2026, 11, 11, 4, 0, 0, 0, time.UTC).Unix())
	f.gate = debate.NewGate(func() time.Time { return time.Unix(moment.Load(), 0) })
	f.mount(t, gen)

	id := f.create(t)
	path := "/api/v1/debates/" + id
	for round := 1; round <= 3; round++ {
		if r := f.request(t, "POST", path+"/turns", submit("第"+string(rune('0'+round))+"轮内容", uuid.NewString(), round)); r.status != 201 {
			t.Fatalf("round %d status=%d", round, r.status)
		}
		moment.Add(15)
	}

	// 启动首次总结生成并挂起在途请求，验证进行中 202、之后租约过期扫描回收为 failed
	done := make(chan apiResult, 1)
	go func() {
		done <- f.request(t, "POST", path+"/summary", map[string]any{"retry": false})
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("summary generator did not start")
	}

	moment.Add(15)
	r := f.request(t, "POST", path+"/summary", map[string]any{"retry": false})
	if r.status != 202 || summaryCalls.Load() != 1 {
		t.Fatalf("expected 202 while generating, got status=%d calls=%d", r.status, summaryCalls.Load())
	}

	// 将 debate_summaries 的 lease_expires_at 设为已过期并运行扫描回收
	expired := time.Now().UTC().Add(-2 * time.Second)
	if err := f.db.ORM.Exec("UPDATE debate_summaries SET lease_expires_at = ? WHERE debate_id = ?", expired, id).Error; err != nil {
		t.Fatal(err)
	}
	recovered, err := f.service.RecoverExpiredLeases(context.Background())
	if err != nil || recovered != 1 {
		t.Fatalf("expected 1 recovered summary lease, got %d err=%v", recovered, err)
	}

	// 放行旧尝试，旧尝试不得覆盖已回收的 failed 状态
	release <- struct{}{}
	if (<-done).status != 503 {
		t.Fatal("expired summary attempt reported success after lease recovery")
	}

	// 失败后传 retry=false 不重调模型，返回 failed 状态
	callsBeforeNoRetry := summaryCalls.Load()
	moment.Add(15)
	r = f.request(t, "POST", path+"/summary", map[string]any{"retry": false})
	var sumStatus debate.SummaryStatus
	b, _ := json.Marshal(r.data())
	_ = json.Unmarshal(b, &sumStatus)
	if r.status != 200 || sumStatus.Status != "failed" || sumStatus.AttemptVersion == nil || *sumStatus.AttemptVersion != 1 || summaryCalls.Load() != callsBeforeNoRetry {
		t.Fatalf("retry=false on failed summary unexpected: status=%d sum=%+v calls=%d", r.status, sumStatus, summaryCalls.Load())
	}

	// 第一次显式重试（expected_attempt_version=1）因非法 Schema JSON 再次失败（502 model_error，attempt_version=2）
	moment.Add(15)
	r = f.request(t, "POST", path+"/summary", map[string]any{"retry": true, "expected_attempt_version": 1})
	var failEnv struct {
		Error struct {
			Details *debate.FailureDetails `json:"details"`
		} `json:"error"`
	}
	_ = json.Unmarshal(r.body, &failEnv)
	if r.status != 502 || r.errorCode() != "model_error" || failEnv.Error.Details == nil || failEnv.Error.Details.AttemptVersion != 2 || failEnv.Error.Details.QueryURL != path+"/summary" {
		t.Fatalf("expected 502 model_error with version 2, got status=%d code=%s details=%+v", r.status, r.errorCode(), failEnv.Error.Details)
	}
	callsAfterSecondFail := summaryCalls.Load()

	// 重发旧版本 expected_attempt_version=1 返回 409 attempt_version_conflict，不重调模型
	moment.Add(15)
	r = f.request(t, "POST", path+"/summary", map[string]any{"retry": true, "expected_attempt_version": 1})
	_ = json.Unmarshal(r.body, &failEnv)
	if r.status != 409 || r.errorCode() != "attempt_version_conflict" || failEnv.Error.Details == nil || failEnv.Error.Details.AttemptVersion != 2 || summaryCalls.Load() != callsAfterSecondFail {
		t.Fatalf("expected 409 attempt_version_conflict without model call, got status=%d code=%s calls=%d", r.status, r.errorCode(), summaryCalls.Load())
	}

	// 使用最新版本 expected_attempt_version=2 重试成功（200，attempt_version=3）
	moment.Add(15)
	r = f.request(t, "POST", path+"/summary", map[string]any{"retry": true, "expected_attempt_version": 2})
	b, _ = json.Marshal(r.data())
	_ = json.Unmarshal(b, &sumStatus)
	if r.status != 200 || sumStatus.Status != "completed" || sumStatus.AttemptVersion == nil || *sumStatus.AttemptVersion != 3 || sumStatus.SchemaVersion == nil || *sumStatus.SchemaVersion != 1 || sumStatus.Content == nil {
		t.Fatalf("expected retry summary 200 completed, got status=%d sum=%+v", r.status, sumStatus)
	}
}

func TestT07FakeFailureAndTimeoutPersistWithoutCountingRound(t *testing.T) {
	for _, tc := range []struct {
		name   string
		code   ai.ErrorCode
		status int
	}{
		{"failure", ai.CodeModelUnavailable, 503},
		{"timeout", ai.CodeModelTimeout, 504},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := ai.NewFake(ai.FakeStep{Err: &ai.Error{Code: tc.code}})
			f := newDebateFixture(t, fake)
			id := f.create(t)
			path := "/api/v1/debates/" + id
			const content = "本地合成失败原文"
			body := submit(content, uuid.NewString(), 1)
			if r := f.request(t, "POST", path+"/turns", body); r.status != tc.status || r.errorCode() != string(tc.code) {
				t.Fatalf("initial status=%d code=%s", r.status, r.errorCode())
			}
			if r := f.request(t, "POST", path+"/turns", body); r.status != 409 || r.errorCode() != "turn_failed" {
				t.Fatalf("replay status=%d code=%s", r.status, r.errorCode())
			}
			r := f.request(t, "GET", path, nil)
			var turns []debate.TurnDTO
			if err := json.Unmarshal(r.data()["turns"], &turns); err != nil {
				t.Fatal(err)
			}
			if r.status != 200 || len(turns) != 1 || turns[0].Status != "failed" ||
				turns[0].ErrorCode == nil || *turns[0].ErrorCode != string(tc.code) ||
				len(turns[0].Messages) != 1 || turns[0].Messages[0].Content != content || len(fake.Records()) != 1 {
				t.Fatal("failed turn, original content, or generation count changed")
			}
			var rounds []int
			if err := f.db.ORM.Table("debates").Where("id = ?", id).Pluck("completed_rounds", &rounds).Error; err != nil || len(rounds) != 1 || rounds[0] != 0 {
				t.Fatalf("completed rounds=%v, err=%v", rounds, err)
			}
		})
	}
}
