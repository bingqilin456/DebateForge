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
func (s *testStore) List(context.Context, uint64, int, int) (DebateList, error) {
	return DebateList{Items: []Debate{}, Page: 1, PageSize: 20}, nil
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
func (s *testStore) ClaimRetry(_ context.Context, _, _, _, _ uint64, admit func() error) (Detail, bool, error) {
	if !s.replay {
		if e := admit(); e != nil {
			return Detail{}, false, e
		}
	}
	return sample(), !s.replay, nil
}
func (s *testStore) RecoverExpiredLeases(context.Context) (int, error) { return 0, nil }
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

func TestGenerationTimeoutPreservesLedgerFailure(t *testing.T) {
	ledgerErr := errors.New("synthetic ledger write failure")
	err := generationContextError(ledgerErr, ai.Result{}, context.DeadlineExceeded)
	if !errors.Is(err, ledgerErr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("combined causes lost: %v", err)
	}
	code, status, _, _ := classifyFailure(context.Background(), err)
	if code != "model_timeout" || status != 504 {
		t.Fatalf("timeout classification changed: %s %d", code, status)
	}
}

// memoryStore 在无数据库单测中模拟真实短事务状态机，用于验证三轮推进、显式重试、版本保护、租约恢复与分页隔离。
type memoryStore struct {
	now     func() time.Time
	nextID  uint64
	debates map[uint64]Debate
	turns   map[uint64][]Turn
}

func newMemoryStore(now func() time.Time) *memoryStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &memoryStore{
		now:     now,
		nextID:  100,
		debates: make(map[uint64]Debate),
		turns:   make(map[uint64][]Turn),
	}
}

func (m *memoryStore) allocID() uint64 {
	m.nextID++
	return m.nextID
}

func (m *memoryStore) Create(_ context.Context, uid, tid uint64, stance string) (Debate, error) {
	now := m.now().UTC()
	d := Debate{
		ID:                       m.allocID(),
		UserID:                   uid,
		TopicID:                  tid,
		TopicTitleSnapshot:       "大学课程是否应采用开放式考核",
		TopicDescriptionSnapshot: "讨论学习效果与评价公平",
		UserStance:               stance,
		Status:                   "active",
		CompletedRounds:          0,
		CreatedAt:                now,
		UpdatedAt:                now,
	}
	m.debates[d.ID] = d
	return d, nil
}

func (m *memoryStore) List(_ context.Context, uid uint64, page, pageSize int) (DebateList, error) {
	var all []Debate
	for _, d := range m.debates {
		if d.UserID == uid {
			all = append(all, d)
		}
	}
	for i := 0; i < len(all); i++ {
		for j := i + 1; j < len(all); j++ {
			if all[j].CreatedAt.After(all[i].CreatedAt) || (all[j].CreatedAt.Equal(all[i].CreatedAt) && all[j].ID > all[i].ID) {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	total := int64(len(all))
	offset := (page - 1) * pageSize
	items := []Debate{}
	if offset < len(all) {
		end := offset + pageSize
		if end > len(all) {
			end = len(all)
		}
		items = append(items, all[offset:end]...)
	}
	return DebateList{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (m *memoryStore) Detail(_ context.Context, uid, id uint64) (Detail, error) {
	d, ok := m.debates[id]
	if !ok || d.UserID != uid {
		return Detail{}, ErrNotFound
	}
	turns := append([]Turn(nil), m.turns[id]...)
	return Detail{Debate: d, Turns: turns}, nil
}

func (m *memoryStore) Claim(_ context.Context, uid, id uint64, in SubmitInput, admit func() error) (Detail, bool, error) {
	d, ok := m.debates[id]
	if !ok || d.UserID != uid {
		return Detail{}, false, ErrNotFound
	}
	turns := m.turns[id]
	for _, t := range turns {
		if t.ClientRequestID == in.ClientRequestID {
			if t.RoundNo != in.RoundNo || len(t.Messages) == 0 || t.Messages[0].Content != in.Content {
				return Detail{}, false, ErrIdempotencyConflict
			}
			return Detail{Debate: d, Turns: []Turn{t}}, false, nil
		}
	}
	if d.Status != "active" || in.RoundNo < 1 || in.RoundNo > ai.TotalRounds || in.RoundNo != d.CompletedRounds+1 || len(turns) != in.RoundNo-1 {
		return Detail{}, false, ErrRoundConflict
	}
	if err := admit(); err != nil {
		return Detail{}, false, err
	}
	lease := m.now().UTC().Add(LeaseDuration)
	turn := Turn{
		ID:              m.allocID(),
		DebateID:        id,
		RoundNo:         in.RoundNo,
		ClientRequestID: in.ClientRequestID,
		Status:          "generating",
		AttemptVersion:  1,
		LeaseExpiresAt:  &lease,
		Messages: []Message{{
			ID:        m.allocID(),
			Role:      "user",
			Content:   in.Content,
			CreatedAt: m.now().UTC(),
		}},
	}
	turns = append(turns, turn)
	m.turns[id] = turns
	return Detail{Debate: d, Turns: append([]Turn(nil), turns...)}, true, nil
}

func (m *memoryStore) ClaimRetry(_ context.Context, uid, debateID, turnID, expectedVersion uint64, admit func() error) (Detail, bool, error) {
	d, ok := m.debates[debateID]
	if !ok || d.UserID != uid {
		return Detail{}, false, ErrNotFound
	}
	turns := m.turns[debateID]
	idx := -1
	for i := range turns {
		if turns[i].ID == turnID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return Detail{}, false, ErrNotFound
	}
	turn := turns[idx]
	if turn.Status == "completed" || turn.Status == "generating" {
		return Detail{Debate: d, Turns: []Turn{turn}}, false, nil
	}
	if turn.AttemptVersion != expectedVersion {
		return Detail{Debate: d, Turns: []Turn{turn}}, false, nil
	}
	if d.Status != "active" || turn.RoundNo != d.CompletedRounds+1 || idx != len(turns)-1 {
		return Detail{}, false, ErrRoundConflict
	}
	if err := admit(); err != nil {
		return Detail{}, false, err
	}
	lease := m.now().UTC().Add(LeaseDuration)
	turn.Status = "generating"
	turn.AttemptVersion++
	turn.LeaseExpiresAt = &lease
	turn.ErrorCode = nil
	turns[idx] = turn
	m.turns[debateID] = turns
	return Detail{Debate: d, Turns: append([]Turn(nil), turns...)}, true, nil
}

func (m *memoryStore) Complete(_ context.Context, uid uint64, d Debate, t Turn, result ai.Result) (Detail, error) {
	curDebate, ok := m.debates[d.ID]
	if !ok || curDebate.UserID != uid {
		return Detail{}, ErrNotFound
	}
	turns := m.turns[d.ID]
	idx := -1
	for i := range turns {
		if turns[i].ID == t.ID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return Detail{}, ErrRoundConflict
	}
	cur := turns[idx]
	if cur.Status != "generating" || cur.AttemptVersion != t.AttemptVersion || curDebate.Status != "active" || curDebate.CompletedRounds != t.RoundNo-1 {
		return Detail{}, ErrRoundConflict
	}
	cur.Status = "completed"
	cur.ErrorCode = nil
	cur.LeaseExpiresAt = nil
	cur.Messages = append(cur.Messages, Message{
		ID:        m.allocID(),
		TurnID:    cur.ID,
		Role:      "assistant",
		Content:   result.Content,
		CreatedAt: m.now().UTC(),
	})
	turns[idx] = cur
	m.turns[d.ID] = turns
	curDebate.CompletedRounds = t.RoundNo
	if curDebate.CompletedRounds >= ai.TotalRounds {
		curDebate.Status = "completed"
	}
	curDebate.UpdatedAt = m.now().UTC()
	m.debates[d.ID] = curDebate
	return Detail{Debate: curDebate, Turns: []Turn{cur}}, nil
}

func (m *memoryStore) Fail(_ context.Context, uid uint64, d Debate, t Turn, code string, _ ai.Usage, _ ai.Metadata) (Turn, error) {
	curDebate, ok := m.debates[d.ID]
	if !ok || curDebate.UserID != uid {
		return Turn{}, ErrNotFound
	}
	turns := m.turns[d.ID]
	for i := range turns {
		if turns[i].ID == t.ID {
			if turns[i].Status != "generating" || turns[i].AttemptVersion != t.AttemptVersion {
				return Turn{}, ErrRoundConflict
			}
			turns[i].Status = "failed"
			turns[i].ErrorCode = &code
			turns[i].LeaseExpiresAt = nil
			m.turns[d.ID] = turns
			return turns[i], nil
		}
	}
	return Turn{}, ErrRoundConflict
}

func (m *memoryStore) RecoverExpiredLeases(context.Context) (int, error) {
	now := m.now().UTC()
	recovered := 0
	code := "model_timeout"
	for dID, turns := range m.turns {
		for i := range turns {
			if turns[i].Status == "generating" && turns[i].LeaseExpiresAt != nil && !turns[i].LeaseExpiresAt.After(now) {
				turns[i].Status = "failed"
				turns[i].ErrorCode = &code
				turns[i].LeaseExpiresAt = nil
				recovered++
			}
		}
		m.turns[dID] = turns
	}
	return recovered, nil
}

func TestT09ThreeRoundsExplicitRetryLeaseAndT11Pagination(t *testing.T) {
	clock := time.Date(2026, 11, 11, 8, 0, 0, 0, time.UTC)
	store := newMemoryStore(func() time.Time { return clock })
	fake := ai.NewFake()
	gate := NewGate(func() time.Time { return clock })
	service := NewService(store, fake, gate)

	created, err := service.Create(context.Background(), 1, 1, "pro")
	if err != nil {
		t.Fatal(err)
	}
	// 第 1 轮成功
	r1, status, err := service.Submit(context.Background(), 1, created.ID, SubmitInput{
		RoundNo:         1,
		Content:         "第一轮正方立论",
		ClientRequestID: "550e8400-e29b-41d4-a716-446655440001",
	})
	if err != nil || status != 201 || r1.CompletedRounds != 1 || len(r1.Turn.Messages) != 2 {
		t.Fatalf("round 1 failed: status=%d err=%v", status, err)
	}
	// 第 2 轮首次超时失败，随后重试再次失败，测试旧版本 409 不调模型，再用最新版本重试成功
	stepGen := ai.NewFake(
		ai.FakeStep{Err: &ai.Error{Code: ai.CodeModelTimeout}},
		ai.FakeStep{Err: &ai.Error{Code: ai.CodeModelError}},
		ai.FakeStep{Result: ai.Result{Content: "第二轮反方回应"}},
		ai.FakeStep{Result: ai.Result{Content: "第三轮反方总结回应"}},
	)
	service.generator = stepGen
	_, _, err = service.Submit(context.Background(), 1, created.ID, SubmitInput{
		RoundNo:         2,
		Content:         "第二轮正方攻辩",
		ClientRequestID: "550e8400-e29b-41d4-a716-446655440002",
	})
	var failErr *Failure
	if !errors.As(err, &failErr) || failErr.Status != 504 || failErr.Details == nil || failErr.Details.AttemptVersion != 1 {
		t.Fatalf("expected round 2 504 failure with version 1, got %v", err)
	}
	turn2ID, _ := ParseID(failErr.Details.TurnID)

	// 重试第 2 轮但再次失败（attempt_version 变为 2）
	_, _, err = service.Retry(context.Background(), 1, created.ID, turn2ID, RetryInput{ExpectedAttemptVersion: 1})
	if !errors.As(err, &failErr) || failErr.Status != 502 || failErr.Details == nil || failErr.Details.AttemptVersion != 2 {
		t.Fatalf("expected retry 502 failure with version 2, got %v", err)
	}
	recordsBeforeConflict := len(stepGen.Records())

	// 重发旧版本 expected_attempt_version=1 必须返回 409 attempt_version_conflict，不调模型
	clock = clock.Add(time.Minute)
	_, _, err = service.Retry(context.Background(), 1, created.ID, turn2ID, RetryInput{ExpectedAttemptVersion: 1})
	if !errors.As(err, &failErr) || failErr.Status != 409 || failErr.Code != "attempt_version_conflict" || failErr.Details == nil || failErr.Details.AttemptVersion != 2 {
		t.Fatalf("expected 409 attempt_version_conflict with version 2, got %v", err)
	}
	if len(stepGen.Records()) != recordsBeforeConflict {
		t.Fatal("version conflict called model")
	}

	// 使用最新版本 expected_attempt_version=2 显式重试成功（返回 200）
	r2, status, err := service.Retry(context.Background(), 1, created.ID, turn2ID, RetryInput{ExpectedAttemptVersion: 2})
	if err != nil || status != 200 || r2.CompletedRounds != 2 || r2.Turn.AttemptVersion != 3 || len(r2.Turn.Messages) != 2 || r2.Turn.Messages[0].Content != "第二轮正方攻辩" {
		t.Fatalf("retry success failed: status=%d err=%v", status, err)
	}
	// 已完成轮次再次调用 retry 返回 200 重放，不调模型
	recordsBeforeReplay := len(stepGen.Records())
	r2Replay, status, err := service.Retry(context.Background(), 1, created.ID, turn2ID, RetryInput{ExpectedAttemptVersion: 2})
	if err != nil || status != 200 || r2Replay.Turn.ID != r2.Turn.ID || len(stepGen.Records()) != recordsBeforeReplay {
		t.Fatal("completed retry replay called model or failed")
	}

	// 第 3 轮成功，会话变为 completed
	clock = clock.Add(time.Minute)
	r3, status, err := service.Submit(context.Background(), 1, created.ID, SubmitInput{
		RoundNo:         3,
		Content:         "第三轮正方总结陈词",
		ClientRequestID: "550e8400-e29b-41d4-a716-446655440003",
	})
	if err != nil || status != 201 || r3.CompletedRounds != 3 || len(r3.Turn.Messages) != 2 {
		t.Fatalf("round 3 failed: status=%d err=%v", status, err)
	}
	detail, err := service.Detail(context.Background(), 1, created.ID)
	if err != nil || detail.Debate.Status != "completed" || detail.Debate.CompletedRounds != 3 || len(detail.Turns) != 3 {
		t.Fatalf("debate not completed after 3 rounds: %+v", detail.Debate)
	}

	// 第 4 轮被拒绝（409 round_conflict）
	_, _, err = service.Submit(context.Background(), 1, created.ID, SubmitInput{
		RoundNo:         4,
		Content:         "第四轮越界发言",
		ClientRequestID: "550e8400-e29b-41d4-a716-446655440004",
	})
	if !errors.Is(err, ErrRoundConflict) {
		t.Fatalf("expected round 4 ErrRoundConflict, got %v", err)
	}

	// 验证租约过期扫描与历史分页
	d2, _ := service.Create(context.Background(), 1, 1, "con")
	expiredTime := clock.Add(-time.Second)
	store.turns[d2.ID] = []Turn{{
		ID:              store.allocID(),
		DebateID:        d2.ID,
		RoundNo:         1,
		ClientRequestID: "550e8400-e29b-41d4-a716-446655440005",
		Status:          "generating",
		AttemptVersion:  1,
		LeaseExpiresAt:  &expiredTime,
		Messages:        []Message{{ID: store.allocID(), Role: "user", Content: "崩溃残留发言", CreatedAt: clock}},
	}}
	recovered, err := service.RecoverExpiredLeases(context.Background())
	if err != nil || recovered != 1 {
		t.Fatalf("expected 1 recovered lease, got %d err=%v", recovered, err)
	}
	d2Detail, _ := service.Detail(context.Background(), 1, d2.ID)
	if d2Detail.Turns[0].Status != "failed" || d2Detail.Turns[0].ErrorCode == nil || *d2Detail.Turns[0].ErrorCode != "model_timeout" {
		t.Fatalf("expired turn not marked failed: %+v", d2Detail.Turns[0])
	}

	// 另一个用户创建的会话不应出现在用户 1 的分页列表中
	_, _ = service.Create(context.Background(), 2, 1, "pro")
	list, err := service.List(context.Background(), 1, 1, 20)
	if err != nil || list.Total != 2 || len(list.Items) != 2 || list.Items[0].ID != d2.ID || list.Items[1].ID != created.ID {
		t.Fatalf("unexpected history list: %+v err=%v", list, err)
	}
}
