package debate

import (
	"context"
	"errors"
	"testing"
	"time"
)

type repracticeMemoryStore struct {
	detail       Detail
	versions     []RepracticeVersion
	createErr    error
	completeErr  error
	createCalls  int
	completeCall int
}

func (m *repracticeMemoryStore) Detail(_ context.Context, owner, id uint64) (Detail, error) {
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID {
		return Detail{}, ErrNotFound
	}
	return m.detail, nil
}

func (m *repracticeMemoryStore) ListRepractice(_ context.Context, owner, id uint64) ([]RepracticeVersion, error) {
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID || m.detail.Debate.Status != "completed" {
		return nil, ErrNotFound
	}
	return append([]RepracticeVersion(nil), m.versions...), nil
}

func (m *repracticeMemoryStore) CreateRepractice(_ context.Context, owner, id uint64, in RepracticeInput, _ time.Time) (RepracticeVersion, bool, error) {
	m.createCalls++
	if m.createErr != nil {
		return RepracticeVersion{}, false, m.createErr
	}
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID || m.detail.Debate.Status != "completed" {
		return RepracticeVersion{}, false, ErrNotFound
	}
	for _, version := range m.versions {
		if version.Input.RequestID == in.RequestID {
			if version.Input != in {
				return RepracticeVersion{}, false, ErrRepracticeInput
			}
			return version, false, nil
		}
	}
	version := RepracticeVersion{ID: uint64(len(m.versions) + 1), Attempt: 1, Status: "generating", Input: in}
	m.versions = append(m.versions, version)
	return version, true, nil
}

func (m *repracticeMemoryStore) ClaimRepracticeRetry(_ context.Context, owner, id, versionID, attempt uint64, _ time.Time) (RepracticeVersion, bool, error) {
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID || m.detail.Debate.Status != "completed" {
		return RepracticeVersion{}, false, ErrNotFound
	}
	if versionID == 0 || versionID > uint64(len(m.versions)) {
		return RepracticeVersion{}, false, ErrRepracticeVersion
	}
	version := m.versions[versionID-1]
	if version.Status != "failed" || version.Attempt != attempt {
		return version, false, nil
	}
	version.Attempt++
	version.Status = "generating"
	m.versions[versionID-1] = version
	return version, true, nil
}

func (m *repracticeMemoryStore) CompleteRepractice(_ context.Context, owner, id, versionID, attempt uint64, feedback RepracticeFeedback) (RepracticeVersion, error) {
	m.completeCall++
	if m.completeErr != nil {
		return RepracticeVersion{}, m.completeErr
	}
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID || versionID == 0 || versionID > uint64(len(m.versions)) {
		return RepracticeVersion{}, ErrNotFound
	}
	version := m.versions[versionID-1]
	if version.Attempt != attempt || version.Status != "generating" {
		return RepracticeVersion{}, ErrAttemptVersionConflict
	}
	version.Status = "completed"
	version.Feedback = feedback
	m.versions[versionID-1] = version
	return version, nil
}

func (m *repracticeMemoryStore) FailRepractice(_ context.Context, owner, id, versionID, attempt uint64, code string) (RepracticeVersion, error) {
	if owner != m.detail.Debate.UserID || id != m.detail.Debate.ID || versionID == 0 || versionID > uint64(len(m.versions)) {
		return RepracticeVersion{}, ErrNotFound
	}
	version := m.versions[versionID-1]
	if version.Attempt != attempt || version.Status != "generating" {
		return RepracticeVersion{}, ErrAttemptVersionConflict
	}
	version.Status = "failed"
	version.ErrorCode = code
	m.versions[versionID-1] = version
	return version, nil
}

type repracticeFakeGenerator struct {
	calls int
	err   error
}

func (f *repracticeFakeGenerator) GenerateRepractice(context.Context, RepracticeInput) (RepracticeFeedback, error) {
	f.calls++
	if f.err != nil {
		return RepracticeFeedback{}, f.err
	}
	return RepracticeFeedback{Improved: true, Reason: "改写补足了判断依据"}, nil
}

func repracticeCompletedDetail() Detail {
	return Detail{
		Debate: Debate{ID: 2, UserID: 1, Status: "completed"},
		Turns: []Turn{{Status: "completed", Messages: []Message{
			{ID: 3, Role: "user", Content: "甲😀乙"},
			{ID: 4, Role: "assistant", Content: "甲😀乙"},
		}}},
	}
}

func TestRepracticeServiceStoresIndependentVersionsAndRestoresFailedInput(t *testing.T) {
	store := &repracticeMemoryStore{detail: repracticeCompletedDetail()}
	fake := &repracticeFakeGenerator{err: errors.New("fake failure")}
	service := NewRepracticeService(store, fake)
	in := RepracticeInput{RequestID: "first", MessageID: 3, Start: 1, End: 2, Quote: "😀", Rewrite: "先说明共同标准。"}

	if _, err := service.Submit(context.Background(), 1, 2, RepracticeInput{RequestID: "empty", MessageID: 3, Start: 1, End: 1, Quote: "", Rewrite: "改写"}); !errors.Is(err, ErrRepracticeSource) {
		t.Fatalf("空选段必须被拒绝，got %v", err)
	}
	failed, err := service.Submit(context.Background(), 1, 2, in)
	if err != nil || failed.Status != "failed" || failed.Input.Rewrite != in.Rewrite {
		t.Fatalf("失败后需保存原改写，version=%+v err=%v", failed, err)
	}
	if _, err := service.Submit(context.Background(), 9, 2, in); !errors.Is(err, ErrNotFound) {
		t.Fatalf("越权读取必须返回 not found，got %v", err)
	}

	fake.err = nil
	done, err := service.Retry(context.Background(), 1, 2, failed.ID, failed.Attempt)
	if err != nil || done.Status != "completed" || done.ID != failed.ID || done.Attempt != 2 {
		t.Fatalf("恢复须更新同一版本，version=%+v err=%v", done, err)
	}

	in.RequestID = "second"
	in.Rewrite = "第二版改写。"
	second, err := service.Submit(context.Background(), 1, 2, in)
	if err != nil || second.ID == done.ID {
		t.Fatalf("新改写应创建独立版本，version=%+v err=%v", second, err)
	}
	versions, err := service.List(context.Background(), 1, 2)
	if err != nil || len(versions) != 2 || versions[0].Input.Rewrite != "先说明共同标准。" || versions[1].Input.Rewrite != "第二版改写。" {
		t.Fatalf("历史版本应可恢复读取，versions=%+v err=%v", versions, err)
	}
	if store.detail.Turns[0].Messages[0].Content != "甲😀乙" {
		t.Fatal("局部重练不得覆盖正式比赛原文")
	}
}

func TestRepracticeServiceDoesNotCallFakeWhenInitialSaveFails(t *testing.T) {
	saveErr := errors.New("fake storage failure")
	store := &repracticeMemoryStore{detail: repracticeCompletedDetail(), createErr: saveErr}
	fake := &repracticeFakeGenerator{}
	service := NewRepracticeService(store, fake)
	in := RepracticeInput{RequestID: "save-failure", MessageID: 3, Start: 0, End: 1, Quote: "甲", Rewrite: "改写"}

	if _, err := service.Submit(context.Background(), 1, 2, in); !errors.Is(err, saveErr) {
		t.Fatalf("保存错误应传回调用方，got %v", err)
	}
	if fake.calls != 0 {
		t.Fatalf("首次保存失败不能触发生成，calls=%d", fake.calls)
	}
}

func TestRepracticeServiceCanRetryAfterFeedbackSaveFails(t *testing.T) {
	store := &repracticeMemoryStore{
		detail: repracticeCompletedDetail(), completeErr: errors.New("fake feedback save failure"),
	}
	fake := &repracticeFakeGenerator{}
	service := NewRepracticeService(store, fake)
	ctx := context.Background()
	in := RepracticeInput{RequestID: "feedback-save-failure", MessageID: 3, Start: 0, End: 1, Quote: "甲", Rewrite: "改写"}

	failed, err := service.Submit(ctx, 1, 2, in)
	if err != nil || failed.Status != repracticeStatusFailed || failed.ErrorCode != "dependency_unavailable" || failed.Input != in {
		t.Fatalf("反馈保存失败应保留可重试的版本，version=%+v err=%v", failed, err)
	}
	store.completeErr = nil
	done, err := service.Retry(ctx, 1, 2, failed.ID, failed.Attempt)
	if err != nil || done.Status != repracticeStatusCompleted || done.ID != failed.ID || done.Attempt != 2 || fake.calls != 2 {
		t.Fatalf("应重试同一版本，version=%+v calls=%d err=%v", done, fake.calls, err)
	}
}
