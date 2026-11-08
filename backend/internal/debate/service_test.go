package debate

import (
	"context"
	"errors"
	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"testing"
	"time"
)

type genFunc func(context.Context, ai.Request) (ai.Result, error)

func (f genFunc) Generate(c context.Context, r ai.Request) (ai.Result, error) { return f(c, r) }

type testStore struct {
	claiming    bool
	replay      bool
	failContext error
	failCode    string
	completed   bool
}

func (s *testStore) Create(context.Context, uint64, uint64, string) (Debate, error) {
	return Debate{}, nil
}
func (s *testStore) Detail(context.Context, uint64, uint64) (Detail, error) { return sample(), nil }
func sample() Detail {
	return Detail{Debate: Debate{ID: 3, UserID: 1, TopicID: 1, TopicTitleSnapshot: "题目", TopicDescriptionSnapshot: "说明", UserStance: "pro", Status: "active"}, Turns: []Turn{{ID: 4, DebateID: 3, RoundNo: 1, ClientRequestID: "550e8400-e29b-41d4-a716-446655440000", Status: "generating", AttemptVersion: 1, Messages: []Message{{ID: 5, Role: "user", Content: "原文"}}}}}
}
func (s *testStore) Claim(_ context.Context, _, _ uint64, _ SubmitInput, admit func() error) (Detail, bool, error) {
	s.claiming = true
	defer func() { s.claiming = false }()
	if !s.replay {
		if e := admit(); e != nil {
			return Detail{}, false, e
		}
	}
	return sample(), !s.replay, nil
}
func (s *testStore) Complete(_ context.Context, _ uint64, _ Debate, _ Turn, _ ai.Result) (Detail, error) {
	s.completed = true
	d := sample()
	d.Debate.CompletedRounds = 1
	d.Turns[0].Status = "completed"
	return d, nil
}
func (s *testStore) Fail(c context.Context, _ uint64, _ Debate, t Turn, code string, _ ai.Usage, _ ai.Metadata) (Turn, error) {
	s.failContext = c.Err()
	s.failCode = code
	t.Status = "failed"
	return t, nil
}
func TestServiceGenerationOutsideClaimAndReplay(t *testing.T) {
	store := &testStore{}
	calls := 0
	service := NewService(store, genFunc(func(ctx context.Context, r ai.Request) (ai.Result, error) {
		calls++
		if store.claiming {
			t.Fatal("model inside claim")
		}
		d, ok := ctx.Deadline()
		if !ok || time.Until(d) > 45*time.Second {
			t.Fatal("missing model deadline")
		}
		if r.Topic.Title != "题目" || r.UserStance != ai.StancePro || r.UserContent != "原文" {
			t.Fatal("wrong saved input")
		}
		return ai.Result{Content: "反驳"}, nil
	}), NewGate(nil))
	in := SubmitInput{1, "原文", "550e8400-e29b-41d4-a716-446655440000"}
	_, status, e := service.Submit(context.Background(), 1, 3, in)
	if e != nil || status != 201 || !store.completed || calls != 1 {
		t.Fatal("new submission failed")
	}
	store.replay = true
	_, status, e = service.Submit(context.Background(), 1, 3, in)
	if e != nil || status != 202 || calls != 1 {
		t.Fatal("replay generated")
	}
}
func TestServiceCancellationAndPanicRelease(t *testing.T) {
	for _, panicModel := range []bool{false, true} {
		store := &testStore{}
		ctx, cancel := context.WithCancel(context.Background())
		gate := NewGate(nil)
		service := NewService(store, genFunc(func(context.Context, ai.Request) (ai.Result, error) {
			if panicModel {
				panic("private provider panic")
			}
			cancel()
			return ai.Result{}, context.Canceled
		}), gate)
		_, _, e := service.Submit(ctx, 1, 3, SubmitInput{1, "原文", "550e8400-e29b-41d4-a716-446655440000"})
		cancel()
		if e == nil || store.failContext != nil {
			t.Fatal("failure must use live independent bounded context")
		}
		if !panicModel && store.failCode != "generation_cancelled" {
			t.Fatal("cancellation not persisted")
		}
		p, e := gate.Acquire(1)
		if e != nil {
			t.Fatal("slot leaked")
		}
		p.Finish(false)
	}
}
func TestServiceFailureMapping(t *testing.T) {
	for _, tc := range []struct {
		code    ai.ErrorCode
		http    int
		persist string
	}{{ai.CodeModelTimeout, 504, "model_timeout"}, {ai.CodeQuotaExceeded, 503, "model_unavailable"}, {ai.CodeRateLimited, 503, "model_unavailable"}, {ai.CodeModelRefused, 502, "model_error"}} {
		s := &testStore{}
		service := NewService(s, genFunc(func(context.Context, ai.Request) (ai.Result, error) { return ai.Result{}, &ai.Error{Code: tc.code} }), NewGate(nil))
		_, _, err := service.Submit(context.Background(), 1, 3, SubmitInput{1, "原文", "550e8400-e29b-41d4-a716-446655440000"})
		var f *Failure
		if !errors.As(err, &f) || f.Status != tc.http || s.failCode != tc.persist || f.Details == nil {
			t.Fatal("wrong controlled failure")
		}
	}
}

func TestServiceDeadlinePersistsFailureWithLiveContext(t *testing.T) {
	store := &testStore{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	s := NewService(store, genFunc(func(c context.Context, _ ai.Request) (ai.Result, error) { <-c.Done(); return ai.Result{}, c.Err() }), NewGate(nil))
	_, _, err := s.Submit(ctx, 1, 3, SubmitInput{1, "原文", "550e8400-e29b-41d4-a716-446655440000"})
	var failure *Failure
	if !errors.As(err, &failure) || failure.Status != 504 || store.failCode != "model_timeout" || store.failContext != nil {
		t.Fatal("deadline cleanup")
	}
}
