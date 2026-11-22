package debate

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// V2-08 局部重练：规则层（服务）测试。
//
// 覆盖：选段复核（本人 user 原文、码点半开区间、emoji 边界）、越权 404、
// 未完成训练拒绝、空选段拒绝、幂等重放、失败保留改写并原地重试、
// 新请求编号产生独立版本、历史顺序、正式原文不被改写。

// repracticeTestStore 是内存替身：一把锁模拟生产短事务的临界区语义。
// 原文复核与版本状态机都放在这里，确保服务层测试不依赖真实数据库。
type repracticeTestStore struct {
	detail    Detail
	versions  []RepracticeVersion
	calls     int
	createErr error
	lastInput RepracticeInput
	// cloneStash 保存“恢复历史版本到编辑区”时读取的版本，模拟不覆盖既有版本。
	cloneStash RepracticeVersion
}

func newRepracticeTestDetail() Detail {
	return Detail{
		Debate: Debate{ID: 2, UserID: 1, Status: "completed", Mode: "standard_4", CompletedRounds: 4},
		Turns: []Turn{
			{Status: "completed", Messages: []Message{
				{ID: 3, Role: "user", Content: "甲😀乙"},
				{ID: 4, Role: "assistant", Content: "甲😀乙"},
			}},
		},
	}
}

func (m *repracticeTestStore) Detail(_ context.Context, owner, id uint64) (Detail, error) {
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID {
		return Detail{}, ErrNotFound
	}
	return m.detail, nil
}

func (m *repracticeTestStore) ListRepractice(_ context.Context, owner, id uint64) ([]RepracticeVersion, error) {
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID {
		return nil, ErrNotFound
	}
	return append([]RepracticeVersion(nil), m.versions...), nil
}

func (m *repracticeTestStore) CreateRepractice(_ context.Context, owner, id uint64, in RepracticeInput, now time.Time) (RepracticeVersion, bool, error) {
	m.calls++
	if m.createErr != nil {
		return RepracticeVersion{}, false, m.createErr
	}
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID {
		return RepracticeVersion{}, false, ErrNotFound
	}
	for _, version := range m.versions {
		if version.Input.RequestID != in.RequestID {
			continue
		}
		if version.Input != in {
			return RepracticeVersion{}, false, ErrRepracticeInput
		}
		return version, false, nil
	}
	m.lastInput = in
	version := RepracticeVersion{
		ID:           uint64(len(m.versions) + 1),
		DebateID:     id,
		Attempt:      1,
		Status:       repracticeStatusGenerating,
		Input:        in,
		RestoredFrom: in.RestoredFrom,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.versions = append(m.versions, version)
	return version, true, nil
}

func (m *repracticeTestStore) ClaimRepracticeRetry(_ context.Context, owner, id, versionID, attempt uint64, now time.Time) (RepracticeVersion, bool, error) {
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID {
		return RepracticeVersion{}, false, ErrNotFound
	}
	if versionID == 0 || versionID > uint64(len(m.versions)) {
		return RepracticeVersion{}, false, ErrRepracticeVersion
	}
	version := m.versions[versionID-1]
	if version.Status != repracticeStatusFailed || version.Attempt != attempt {
		return version, false, nil
	}
	version.Attempt++
	version.Status = repracticeStatusGenerating
	version.ErrorCode = ""
	version.UpdatedAt = now
	m.versions[versionID-1] = version
	return version, true, nil
}

func (m *repracticeTestStore) CompleteRepractice(_ context.Context, owner, id, versionID, attempt uint64, feedback RepracticeFeedback) (RepracticeVersion, error) {
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID || versionID == 0 || versionID > uint64(len(m.versions)) {
		return RepracticeVersion{}, ErrNotFound
	}
	version := m.versions[versionID-1]
	if version.Attempt != attempt || version.Status != repracticeStatusGenerating {
		return RepracticeVersion{}, ErrAttemptVersionConflict
	}
	version.Status = repracticeStatusCompleted
	version.Feedback = feedback
	version.ErrorCode = ""
	m.versions[versionID-1] = version
	return version, nil
}

func (m *repracticeTestStore) FailRepractice(_ context.Context, owner, id, versionID, attempt uint64, code string) (RepracticeVersion, error) {
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID || versionID == 0 || versionID > uint64(len(m.versions)) {
		return RepracticeVersion{}, ErrNotFound
	}
	version := m.versions[versionID-1]
	if version.Attempt != attempt || version.Status != repracticeStatusGenerating {
		return RepracticeVersion{}, ErrAttemptVersionConflict
	}
	version.Status = repracticeStatusFailed
	version.ErrorCode = code
	m.versions[versionID-1] = version
	return version, nil
}

// repracticeTestGenerator 是只实现业务接口的最小 Fake：
// 默认成功；设置 err 后按脚本注入失败，用于验证失败保留与重试。
type repracticeTestGenerator struct {
	calls int
	err   error
}

func (g *repracticeTestGenerator) GenerateRepractice(_ context.Context, in RepracticeInput) (RepracticeFeedback, error) {
	g.calls++
	if g.err != nil {
		return RepracticeFeedback{}, g.err
	}
	return RepracticeFeedback{Improved: true, Change: "补了一句依据", Reason: "理由与主张衔接", NewProblem: ""}, nil
}

func newRepracticeTestService(store *repracticeTestStore, generator *repracticeTestGenerator) *RepracticeService {
	return NewRepracticeService(store, generator)
}

func TestRepracticeSubmitStoresIndependentVersionsAndRetries(t *testing.T) {
	store := &repracticeTestStore{detail: newRepracticeTestDetail()}
	fake := &repracticeTestGenerator{err: errors.New("fake failure")}
	service := newRepracticeTestService(store, fake)
	ctx := context.Background()
	in := RepracticeInput{RequestID: "first", MessageID: 3, Start: 1, End: 2, Quote: "😀", Rewrite: "先说明共同标准。"}

	// 空选段不是合法原文：按选段不成立拒绝，且不创建任何版本。
	if _, err := service.Submit(ctx, 1, 2, RepracticeInput{RequestID: "empty", MessageID: 3, Start: 1, End: 1, Quote: "", Rewrite: "改写"}); !errors.Is(err, ErrRepracticeSource) {
		t.Fatalf("空选段必须被拒绝，got %v", err)
	}

	failed, err := service.Submit(ctx, 1, 2, in)
	if err != nil || failed.Status != repracticeStatusFailed || failed.Input.Rewrite != in.Rewrite {
		t.Fatalf("失败后需保存原改写，version=%+v err=%v", failed, err)
	}
	if failed.ErrorCode == "" {
		t.Fatal("失败版本必须记录受控错误码")
	}

	// 越权读取与提交都返回 not found，不泄露会话是否存在。
	if _, err := service.Submit(ctx, 9, 2, in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("越权提交必须返回 not found，got %v", err)
	}
	if _, err := service.List(ctx, 9, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("越权读取必须返回 not found，got %v", err)
	}

	// 同一请求编号重放：幂等返回同一版本，不再调用模型。
	calls := fake.calls
	replay, err := service.Submit(ctx, 1, 2, in)
	if err != nil || replay.ID != failed.ID || fake.calls != calls {
		t.Fatalf("重复请求应幂等，replay=%+v calls=%d/%d err=%v", replay, fake.calls, calls, err)
	}

	// 同一请求编号换内容：拒绝，不覆盖既有版本。
	changed := in
	changed.Rewrite = "完全不同的改写"
	if _, err := service.Submit(ctx, 1, 2, changed); !errors.Is(err, ErrRepracticeInput) {
		t.Fatalf("同编号不同内容必须拒绝，got %v", err)
	}

	fake.err = nil
	done, err := service.Retry(ctx, 1, 2, failed.ID, failed.Attempt)
	if err != nil || done.Status != repracticeStatusCompleted || done.ID != failed.ID || done.Attempt != 2 {
		t.Fatalf("重试须更新同一版本，version=%+v err=%v", done, err)
	}
	if done.Feedback.Reason == "" || !done.Feedback.Improved {
		t.Fatalf("成功版本必须带改善判断与理由：%+v", done.Feedback)
	}

	// 已完成版本再次重试：不重新调用模型，读回当前状态。
	calls = fake.calls
	if again, err := service.Retry(ctx, 1, 2, failed.ID, failed.Attempt); err != nil || again.ID != failed.ID || fake.calls != calls {
		t.Fatalf("已完成版本不应重复生成：%+v calls=%d/%d err=%v", again, fake.calls, calls, err)
	}

	second := in
	second.RequestID = "second"
	second.Rewrite = "第二版改写。"
	created, err := service.Submit(ctx, 1, 2, second)
	if err != nil || created.ID == done.ID {
		t.Fatalf("新改写应创建独立版本，version=%+v err=%v", created, err)
	}

	versions, err := service.List(ctx, 1, 2)
	if err != nil || len(versions) != 2 || versions[0].Input.Rewrite != in.Rewrite || versions[1].Input.Rewrite != second.Rewrite {
		t.Fatalf("历史版本应按写入顺序可读，versions=%+v err=%v", versions, err)
	}
	// 正式原文绝不被重练覆盖。
	if store.detail.Turns[0].Messages[0].Content != "甲😀乙" {
		t.Fatal("局部重练不得覆盖正式比赛原文")
	}
}

func TestRepracticeRejectsUnfinishedOrForeignSourceAndCountsCodepoints(t *testing.T) {
	store := &repracticeTestStore{detail: Detail{
		Debate: Debate{ID: 2, UserID: 1, Status: "active", Mode: "standard_4", CompletedRounds: 1},
		Turns:  []Turn{{Status: "generating", Messages: []Message{{ID: 3, Role: "user", Content: "原句"}}}},
	}}
	fake := &repracticeTestGenerator{}
	service := newRepracticeTestService(store, fake)
	ctx := context.Background()
	in := RepracticeInput{RequestID: "length", MessageID: 3, Start: 0, End: 2, Quote: "原句", Rewrite: "改写"}

	// 未完赛：不开放局部重练，按轮次冲突拒绝。
	if _, err := service.Submit(ctx, 1, 2, in); !errors.Is(err, ErrRoundConflict) {
		t.Fatalf("未完赛必须拒绝，got %v", err)
	}
	store.detail.Debate.Status = "completed"
	store.detail.Debate.CompletedRounds = 4
	// 轮次状态仍在生成：选段不属于“已保存发言”。
	if _, err := service.Submit(ctx, 1, 2, in); !errors.Is(err, ErrRepracticeSource) {
		t.Fatalf("未完成的轮次不能作为选段，got %v", err)
	}
	store.detail.Turns[0].Status = "completed"
	// AI 消息编号不能作为重练对象。
	aiSource := in
	aiSource.MessageID = 4
	store.detail.Turns[0].Messages = append(store.detail.Turns[0].Messages, Message{ID: 4, Role: "assistant", Content: "原句"})
	if _, err := service.Submit(ctx, 1, 2, aiSource); !errors.Is(err, ErrRepracticeSource) {
		t.Fatalf("AI 原文不得作为重练对象，got %v", err)
	}
	// 坐标与原文不一致（emoji 按码点计，UTF-16 下标会错位）。
	store.detail.Turns[0].Messages = []Message{{ID: 3, Role: "user", Content: "甲😀乙"}}
	mismatch := RepracticeInput{RequestID: "mismatch", MessageID: 3, Start: 0, End: 1, Quote: "甲😀", Rewrite: "改写"}
	if _, err := service.Submit(ctx, 1, 2, mismatch); !errors.Is(err, ErrRepracticeSource) {
		t.Fatalf("坐标与原文不符必须拒绝，got %v", err)
	}
	// 全空白改写（Unicode 空白 + U+FEFF）拒绝。
	blank := RepracticeInput{RequestID: "blank", MessageID: 3, Start: 0, End: 1, Quote: "甲", Rewrite: "\u0085\ufeff"}
	if _, err := service.Submit(ctx, 1, 2, blank); !errors.Is(err, ErrRepracticeInput) {
		t.Fatalf("全空白改写必须拒绝，got %v", err)
	}
	// 2001 码点拒绝、2000 码点接受（按码点而不是字节/UTF-16 计数）。
	over := blank
	over.Rewrite = strings.Repeat("😀", RepracticeMaxCodepoints+1)
	if _, err := service.Submit(ctx, 1, 2, over); !errors.Is(err, ErrRepracticeInput) {
		t.Fatalf("超长改写必须拒绝，got %v", err)
	}
	atLimit := blank
	atLimit.Rewrite = strings.Repeat("😀", RepracticeMaxCodepoints)
	atLimit.RequestID = "at-limit"
	if _, err := service.Submit(ctx, 1, 2, atLimit); err != nil {
		t.Fatalf("临界长度应接受，got %v", err)
	}
}

func TestRepracticeEmojiBoundariesAndHistoryRestore(t *testing.T) {
	store := &repracticeTestStore{detail: Detail{
		Debate: Debate{ID: 2, UserID: 1, Status: "completed", Mode: "standard_4", CompletedRounds: 4},
		Turns: []Turn{{Status: "completed", Messages: []Message{
			{ID: 3, Role: "user", Content: "甲😀乙𠮷"},
			{ID: 4, Role: "assistant", Content: "甲😀乙𠮷"},
		}}},
	}}
	service := newRepracticeTestService(store, &repracticeTestGenerator{})
	ctx := context.Background()

	// 补充平面字符各算一个码点：取 [1,3) 应得到 "😀乙"。
	in := RepracticeInput{RequestID: "emoji", MessageID: 3, Start: 1, End: 3, Quote: "😀乙", Rewrite: "改写"}
	if _, err := service.Submit(ctx, 1, 2, in); err != nil {
		t.Fatalf("emoji 边界选段应接受，got %v", err)
	}
	// 相同的字节数切错一个码点必须拒绝（防 UTF-16 下标混用）。
	wrong := in
	wrong.RequestID = "emoji-wrong"
	wrong.Quote = "甲"
	wrong.Start = 0
	wrong.End = 2
	if _, err := service.Submit(ctx, 1, 2, wrong); !errors.Is(err, ErrRepracticeSource) {
		t.Fatalf("跨码点切片必须拒绝，got %v", err)
	}

	// 历史版本可回看；把某一版恢复到编辑区后再次提交应产生新版本，
	// 且不覆盖被恢复的历史版本（PRD §5.7“不覆盖比赛”延伸到重练历史）。
	versions, err := service.List(ctx, 1, 2)
	if err != nil || len(versions) != 1 {
		t.Fatalf("历史版本读取失败：%+v err=%v", versions, err)
	}
	restored := versions[0]
	next := RepracticeInput{RequestID: "restored", MessageID: 3, Start: 1, End: 3, Quote: "😀乙", Rewrite: restored.Input.Rewrite + "（继续改写）", RestoredFrom: restored.ID}
	if _, err := service.Submit(ctx, 1, 2, next); err != nil {
		t.Fatalf("恢复后继续改写应创建新版本，got %v", err)
	}
	after, err := service.List(ctx, 1, 2)
	if err != nil || len(after) != 2 || after[0].Input.Rewrite != restored.Input.Rewrite || after[1].RestoredFrom != restored.ID {
		t.Fatalf("恢复历史版本不能覆盖既有版本，versions=%+v err=%v", after, err)
	}
}

func TestRepracticeDoesNotCallGeneratorWhenInitialSaveFails(t *testing.T) {
	saveErr := errors.New("fake storage failure")
	store := &repracticeTestStore{detail: newRepracticeTestDetail()}
	store.createErr = saveErr
	fake := &repracticeTestGenerator{}
	service := newRepracticeTestService(store, fake)
	in := RepracticeInput{RequestID: "save-failure", MessageID: 3, Start: 0, End: 1, Quote: "甲", Rewrite: "改写"}

	if _, err := service.Submit(context.Background(), 1, 2, in); !errors.Is(err, saveErr) {
		t.Fatalf("保存错误应原样传回调用方，got %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("首次保存失败不能触发生成，calls=%d", fake.calls)
	}
}

// TestRepracticeFeedbackRejectsUnusableOutput 覆盖“空白理由/超长回填”这类
// 不能直接展示给用户的模型输出：按受控失败保存，保留改写供显式重试。
func TestRepracticeFeedbackRejectsUnusableOutput(t *testing.T) {
	store := &repracticeTestStore{detail: newRepracticeTestDetail()}
	fake := &repracticeTestGenerator{}
	service := newRepracticeTestService(store, fake)
	ctx := context.Background()
	in := RepracticeInput{RequestID: "bad-feedback", MessageID: 3, Start: 0, End: 1, Quote: "甲", Rewrite: "改写"}

	for _, feedback := range []RepracticeFeedback{
		{Improved: true, Change: "有变化", Reason: "   "},
		{Improved: true, Change: strings.Repeat("改", RepracticeMaxCodepoints+1), Reason: "理由"},
	} {
		scripted := &repracticeTestGenerators{feedback: feedback}
		service = NewRepracticeService(store, scripted)
		version, err := service.Submit(ctx, 1, 2, in)
		if err != nil {
			t.Fatalf("不可用反馈应按失败保存而不是抛错，err=%v", err)
		}
		if version.Status != repracticeStatusFailed || version.Input.Rewrite != in.Rewrite {
			t.Fatalf("不可用反馈必须落失败并保留改写：%+v", version)
		}
		in.RequestID = in.RequestID + "-next"
	}
}

// repracticeTestGenerators 返回固定反馈，用于覆盖模型输出不合规的分支。
type repracticeTestGenerators struct {
	feedback RepracticeFeedback
}

func (g *repracticeTestGenerators) GenerateRepractice(context.Context, RepracticeInput) (RepracticeFeedback, error) {
	return g.feedback, nil
}
