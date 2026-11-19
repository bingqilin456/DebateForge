package debate

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// 争点卡写回失败的可恢复性回归（Fake Store 注入）。
//
// 关注三条边界：
//   - 分析合法但写回“确定未提交”时，分析状态必须落到受控失败，而不是永久 generating；
//     用户可用同一个 attempt 版本显式重试，已成功卡片保持不变。
//   - 写回“提交结果不明”时既不能宣布失败（可能已提交），也不能自动重跑。
//   - 失败落库的清理 context 不沿用请求取消，但必须有界；清理本身仍走同 attempt 的 CAS，
//     绝不能覆盖已完成状态。

// issueCardRecoveryStore 包装内存 Fake：
//   - 可让 CompleteIssueCardAnalysis 返回注入错误，或先真正提交再返回注入错误
//     （用于验证“成功状态不得被覆盖”）；
//   - 记录 FailIssueCardAnalysis 收到的 context 是否带期限、是否已被取消。
type issueCardRecoveryStore struct {
	*MemoryIssueCardStore
	mu               sync.Mutex
	completeErr      error
	commitBeforeFail bool
	failCalls        int
	failCtxErr       error
	failHasDeadline  bool
}

func (s *issueCardRecoveryStore) SetCompleteError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.completeErr = err
}

func (s *issueCardRecoveryStore) SetCommitBeforeFail(value bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.commitBeforeFail = value
}

func (s *issueCardRecoveryStore) FailSnapshot() (calls int, ctxErr error, hasDeadline bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.failCalls, s.failCtxErr, s.failHasDeadline
}

func (s *issueCardRecoveryStore) CompleteIssueCardAnalysis(ctx context.Context, uid uint64, claim IssueCardClaim, drafts []IssueCardDraft) (IssueCardSnapshot, error) {
	s.mu.Lock()
	injected := s.completeErr
	commitFirst := s.commitBeforeFail
	s.mu.Unlock()
	if injected != nil && commitFirst {
		if _, err := s.MemoryIssueCardStore.CompleteIssueCardAnalysis(ctx, uid, claim, drafts); err != nil {
			return IssueCardSnapshot{}, err
		}
	}
	if injected != nil {
		return IssueCardSnapshot{}, injected
	}
	return s.MemoryIssueCardStore.CompleteIssueCardAnalysis(ctx, uid, claim, drafts)
}

func (s *issueCardRecoveryStore) FailIssueCardAnalysis(ctx context.Context, uid uint64, claim IssueCardClaim, code string) (IssueCardAnalysis, error) {
	s.mu.Lock()
	s.failCalls++
	_, hasDeadline := ctx.Deadline()
	if hasDeadline {
		s.failHasDeadline = true
	}
	s.failCtxErr = ctx.Err()
	s.mu.Unlock()
	return s.MemoryIssueCardStore.FailIssueCardAnalysis(ctx, uid, claim, code)
}

var _ IssueCardStore = (*issueCardRecoveryStore)(nil)

func issueCardRecoveryFixture(t *testing.T) (*issueCardRecoveryStore, *FakeIssueCardAnalyzer, *IssueCardService, Debate, []Turn) {
	t.Helper()
	clock := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	base := NewMemoryIssueCardStore(func() time.Time { return clock })
	debate, turns := issueCardCompletedDebate()
	base.SeedDebate(debate, turns)
	store := &issueCardRecoveryStore{MemoryIssueCardStore: base}
	fake := NewFakeIssueCardAnalyzer()
	return store, fake, NewIssueCardService(store, fake), debate, turns
}

// TestIssueCardCompleteDeterminateFailureMarksFailedAndRetriesWithSameVersion 验证核心缺口：
// 分析合法、CompleteIssueCardAnalysis 确定未提交时，分析不能永久停在 generating。
func TestIssueCardCompleteDeterminateFailureMarksFailedAndRetriesWithSameVersion(t *testing.T) {
	store, fake, service, debate, turns := issueCardRecoveryFixture(t)
	ctx := context.Background()
	origin := turns[0].Messages[0].Content
	fake.SetCandidates([]IssueCardCandidate{issueCardCandidate(t, turns, origin)})

	initial, status, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil || status != 201 || len(initial.Items) != 1 {
		t.Fatalf("首次整理应成功: err=%v status=%d len=%d", err, status, len(initial.Items))
	}
	card := initial.Items[0]
	attempt := initial.Analysis.AttemptVersion

	// 分析合法，但写回确定未提交：必须是受控失败，且可以凭同一版本显式重试。
	store.SetCompleteError(errors.Join(ErrIssueCardNotCommitted, errors.New("写回事务已回滚")))
	_, _, err = service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{Retry: true, ExpectedAttemptVersion: attempt})
	if err == nil {
		t.Fatal("确定未提交必须返回受控错误")
	}
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("写回失败应包装成受控 Failure，实际 %T %v", err, err)
	}
	if failure.Status != 503 || failure.Code != "dependency_unavailable" {
		t.Fatalf("写回失败应映射为 503 dependency_unavailable，实际 %d %q", failure.Status, failure.Code)
	}
	if failure.CommitUnknown {
		t.Fatal("确定未提交不得标记为提交结果不明")
	}

	failed, err := service.Get(ctx, 1, debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if failed.Analysis.Status != IssueCardAnalysisFailed {
		t.Fatalf("确定未提交后分析状态必须为 failed（不能永久 generating），实际 %q", failed.Analysis.Status)
	}
	if failed.Analysis.ErrorCode == nil || *failed.Analysis.ErrorCode != "dependency_unavailable" {
		t.Fatalf("失败状态必须带受控错误码: %+v", failed.Analysis)
	}
	if failed.Analysis.AttemptVersion != attempt+1 {
		t.Fatalf("失败落库必须使用认领时的同一 attempt 版本 %d，实际 %d", attempt+1, failed.Analysis.AttemptVersion)
	}
	if len(failed.Items) != 1 || failed.Items[0].ID != card.ID || failed.Items[0].Version != card.Version {
		t.Fatalf("写回失败不得改写已成功卡片: %+v", failed.Items)
	}
	calls, ctxErr, hasDeadline := store.FailSnapshot()
	if calls != 1 {
		t.Fatalf("失败落库应执行一次，实际 %d 次", calls)
	}
	if !hasDeadline {
		t.Fatal("失败落库的 context 必须有期限，避免无界阻塞")
	}
	if ctxErr != nil {
		t.Fatalf("失败落库不得沿用请求取消，实际 ctx.Err()=%v", ctxErr)
	}

	// 同一 attempt 版本显式重试必须被接受，并保持已有卡片身份。
	store.SetCompleteError(nil)
	retried, status, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{
		Retry: true, ExpectedAttemptVersion: failed.Analysis.AttemptVersion,
	})
	if err != nil || status != 201 {
		t.Fatalf("同版本显式重试应成功: err=%v status=%d", err, status)
	}
	if len(retried.Items) != 1 || retried.Items[0].ID != card.ID {
		t.Fatalf("重试不得新建卡或改变卡 ID: %+v", issueCardIDs(retried.Items))
	}
	if retried.Analysis.Status != IssueCardAnalysisCompleted {
		t.Fatalf("重试成功后状态应为 completed，实际 %q", retried.Analysis.Status)
	}
}

// TestIssueCardCompleteUnknownCommitStaysGenerating 验证“提交不明”不按失败处理、不自动重跑。
func TestIssueCardCompleteUnknownCommitStaysGenerating(t *testing.T) {
	store, fake, service, debate, turns := issueCardRecoveryFixture(t)
	ctx := context.Background()
	origin := turns[0].Messages[0].Content
	fake.SetCandidates([]IssueCardCandidate{issueCardCandidate(t, turns, origin)})

	initial, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil || len(initial.Items) != 1 {
		t.Fatalf("首次整理应成功: %v", err)
	}
	card := initial.Items[0]
	attempt := initial.Analysis.AttemptVersion
	callsBefore := fake.Calls()

	// 不带“确定未提交”标记的错误一律按提交结果不明处理。
	store.SetCompleteError(errors.New("连接在提交后中断，无法确认是否落库"))
	_, _, err = service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{Retry: true, ExpectedAttemptVersion: attempt})
	if err == nil {
		t.Fatal("提交结果不明必须返回受控错误")
	}
	var failure *Failure
	if !errors.As(err, &failure) || !failure.CommitUnknown {
		t.Fatalf("提交结果不明应标记 CommitUnknown，实际 %T %v", err, err)
	}
	if fake.Calls() != callsBefore+1 {
		t.Fatalf("提交结果不明不得自动重跑分析，实际调用 %d 次", fake.Calls()-callsBefore)
	}
	calls, _, _ := store.FailSnapshot()
	if calls != 0 {
		t.Fatalf("提交结果不明不得宣布失败，实际落失败 %d 次", calls)
	}

	current, err := service.Get(ctx, 1, debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Analysis.Status != IssueCardAnalysisGenerating {
		t.Fatalf("提交结果不明应保留在途状态，实际 %q", current.Analysis.Status)
	}
	if len(current.Items) != 1 || current.Items[0].ID != card.ID || current.Items[0].Version != card.Version {
		t.Fatalf("提交结果不明不得改写已保存卡片: %+v", current.Items)
	}
}

// TestIssueCardFailureCleanupUsesBoundedDetachedContext 验证失败清理 context：
// 请求取消后仍要落库，但必须是有限期限而非无 deadline 的 WithoutCancel。
func TestIssueCardFailureCleanupUsesBoundedDetachedContext(t *testing.T) {
	store, fake, service, debate, _ := issueCardRecoveryFixture(t)
	gate := make(chan struct{})
	defer close(gate)
	fake.SetGate(gate)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
		done <- err
	}()
	select {
	case <-fake.Entered():
	case <-time.After(2 * time.Second):
		t.Fatal("分析未进入执行阶段")
	}
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("取消后的分析应返回受控错误")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("取消后分析未退出")
	}

	calls, ctxErr, hasDeadline := store.FailSnapshot()
	if calls != 1 {
		t.Fatalf("请求取消后仍应尽力落一次失败，实际 %d 次", calls)
	}
	if ctxErr != nil {
		t.Fatalf("失败清理不得沿用请求取消，实际 ctx.Err()=%v", ctxErr)
	}
	if !hasDeadline {
		t.Fatal("失败清理 context 必须有期限，不能使用无 deadline 的 WithoutCancel")
	}
	current, err := service.Get(context.Background(), 1, debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Analysis.Status != IssueCardAnalysisFailed {
		t.Fatalf("取消后应落受控失败，实际 %q", current.Analysis.Status)
	}
}

// TestIssueCardFailureWriteNeverOverwritesCommittedSuccess 验证清理写回仍遵守 CAS：
// 即使调用方误标“确定未提交”，已完成的成功状态与卡片也不得被失败覆盖。
func TestIssueCardFailureWriteNeverOverwritesCommittedSuccess(t *testing.T) {
	store, fake, service, debate, turns := issueCardRecoveryFixture(t)
	ctx := context.Background()
	origin := turns[0].Messages[0].Content
	fake.SetCandidates([]IssueCardCandidate{issueCardCandidate(t, turns, origin)})
	initial, _, err := service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{})
	if err != nil || len(initial.Items) != 1 {
		t.Fatalf("首次整理应成功: %v", err)
	}
	card := initial.Items[0]

	// 先真正提交成功（attempt 2 完成），再返回“确定未提交”：失败落库必须被 CAS 拒绝。
	fake.SetCandidates([]IssueCardCandidate{issueCardCandidate(t, turns, origin, turns[0].Messages[1].Content)})
	store.SetCommitBeforeFail(true)
	store.SetCompleteError(errors.Join(ErrIssueCardNotCommitted, errors.New("提交后误标未提交")))
	_, _, err = service.Analyze(ctx, 1, debate.ID, IssueCardAnalyzeInput{
		Retry: true, ExpectedAttemptVersion: initial.Analysis.AttemptVersion,
	})
	if err == nil {
		t.Fatal("误标未提交仍应对外报错，等待用户按状态恢复")
	}

	current, err := service.Get(ctx, 1, debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Analysis.Status != IssueCardAnalysisCompleted {
		t.Fatalf("已成功状态不得被失败清理覆盖，实际 %q", current.Analysis.Status)
	}
	if current.Analysis.ErrorCode != nil {
		t.Fatalf("已完成状态不得携带错误码: %+v", current.Analysis)
	}
	if calls, _, _ := store.FailSnapshot(); calls != 1 {
		t.Fatalf("应尝试一次失败落库并由 CAS 拒绝，实际 %d 次", calls)
	}
	if len(current.Items) != 1 || current.Items[0].ID != card.ID {
		t.Fatalf("已成功卡片不得被清理改写: %+v", issueCardIDs(current.Items))
	}
	if len(current.Items[0].Responses) == 0 {
		t.Fatal("已提交的晚到内容应保留")
	}
}
