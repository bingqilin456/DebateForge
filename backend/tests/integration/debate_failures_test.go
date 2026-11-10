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
