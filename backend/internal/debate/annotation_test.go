package debate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

func TestGetAnnotationsRecoversStoredUniqueAnchorWithoutGeneration(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[1].ID
	saved := f.store.annotations[turnID]
	var payload struct {
		Items []AnnotationItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(*saved.ContentJSON), &payload); err != nil {
		t.Fatal(err)
	}
	old := &payload.Items[0].Anchor
	old.StartCP, old.EndCP, old.Resolved = nil, nil, false
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	legacyJSON := string(data)
	saved.ContentJSON = &legacyJSON
	f.store.annotations[turnID] = saved
	out, err := f.service.GetAnnotations(context.Background(), 1, f.debate.ID)
	if err != nil || !out.Items[1].Items[0].Anchor.Resolved {
		t.Fatalf("只读重定位失败: %+v, %v", out, err)
	}
	if f.fake.AnnotationCalls() != 2 || *f.store.annotations[turnID].ContentJSON != legacyJSON {
		t.Fatal("读取批注不得调用模型或写回历史结果")
	}
}

// 批注服务端行为测试：正常完成、非合格轮、重复重放、版本保护、槽位繁忙、
// 生成拒绝/超时/panic、跨轮输入边界与失败后唯一一次补做。
// 全部使用内存 Store 与 ai.Fake，不连接数据库、不调用真实模型。

// annotationFixture 组织一个已开启批注的服务实例与两轮已完成的会话。
type annotationFixture struct {
	store   *memoryStore
	fake    *ai.Fake
	service *Service
	debate  Debate
	turns   []Turn
}

func newAnnotationFixture(t *testing.T) *annotationFixture {
	t.Helper()
	clock := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	store := newMemoryStore(func() time.Time { return clock })
	fake := ai.NewFake(ai.FakeStep{Result: ai.Result{Content: "AI 第一轮回复"}}, ai.FakeStep{Result: ai.Result{Content: "AI 第二轮回复"}})
	service := NewService(store, fake, NewGate(func() time.Time { return clock }))
	service.EnableAnnotations(true)
	t.Cleanup(func() { service.waitBackground() })

	created, err := service.Create(context.Background(), 1, CreateInput{TopicID: topicID(1), UserStance: "pro", AIStyle: "logical"})
	if err != nil {
		t.Fatal(err)
	}
	f := &annotationFixture{store: store, fake: fake, service: service, debate: created}
	// 两轮：第 1 轮作为背景，第 2 轮作为触发轮。轮次内容刻意互不相同，
	// 便于断言“只评价本轮新增原文”。
	contents := []string{"第1轮用户发言：标准应当可检验。", "第2轮用户发言：范围需要先界定。"}
	ids := []string{"550e8400-e29b-41d4-a716-446655440001", "550e8400-e29b-41d4-a716-446655440002"}
	for round := 1; round <= 2; round++ {
		data, status, err := service.Submit(context.Background(), 1, created.ID, SubmitInput{
			RoundNo:         round,
			Content:         contents[round-1],
			ClientRequestID: ids[round-1],
		})
		if err != nil || status != 201 {
			t.Fatalf("第 %d 轮提交失败: %d %v", round, status, err)
		}
		f.turns = append(f.turns, Turn{ID: mustParse(t, data.Turn.ID), RoundNo: round})
		// 分析槽只有一个：等上一轮后台分析结束再提交下一轮，
		// 否则按“忙则保留资格”的规则该轮不会被自动分析（这正是设计行为）。
		service.waitBackground()
	}
	return f
}

func mustParse(t *testing.T, raw string) uint64 {
	t.Helper()
	id, err := ParseID(raw)
	if err != nil {
		t.Fatalf("非法 ID %q: %v", raw, err)
	}
	return id
}

type demoWithFailedSummary struct{ *ai.Fake }

func (demoWithFailedSummary) DemoAnnotationsEnabled() bool { return true }

func (demoWithFailedSummary) GenerateSummary(context.Context, ai.SummaryRequest) (ai.SummaryResult, error) {
	return ai.SummaryResult{}, &ai.Error{Code: ai.CodeModelError, Message: "合成总结结构错误"}
}

func TestDemoFinalAnnotationDoesNotDependOnSummarySuccess(t *testing.T) {
	f := newAnnotationFixture(t)
	f.service.generator = demoWithFailedSummary{f.fake}
	ctx := context.Background()
	ids := []string{"550e8400-e29b-41d4-a716-446655440003", "550e8400-e29b-41d4-a716-446655440004"}
	for i, id := range ids {
		_, _, err := f.service.Submit(ctx, 1, f.debate.ID, SubmitInput{RoundNo: i + 3, Content: "先界定辩题范围，再交代理由。", ClientRequestID: id})
		if err != nil {
			t.Fatal(err)
		}
		f.service.waitBackground()
	}
	if got := f.fake.AnnotationCalls(); got != 3 {
		t.Fatalf("末轮应先等待总结退出，批注调用数 %d", got)
	}
	if _, _, err := f.service.Summary(ctx, 1, f.debate.ID, SummaryInput{}); err == nil {
		t.Fatal("应保留总结业务失败")
	}
	f.service.waitBackground()
	list, err := f.service.GetAnnotations(ctx, 1, f.debate.ID)
	if err != nil || len(list.Items) != 4 || list.Items[3].Status != AnnotationCompleted || f.fake.AnnotationCalls() != 4 {
		t.Fatalf("总结失败后，末轮批注仍应单独执行一次，结果=%+v，错误=%v", list, err)
	}
}

func TestAnnotationGeneratedAfterCommitAndSaved(t *testing.T) {
	f := newAnnotationFixture(t)
	// 后台分析在提交成功后自动完成，资格标志与结果同时存在。
	list, err := f.service.GetAnnotations(context.Background(), 1, f.debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("两轮合格轮次都应参与读取: %+v", list.Items)
	}
	for _, batch := range list.Items {
		if batch.Status != AnnotationCompleted {
			t.Fatalf("自动分析应完成: %+v", batch)
		}
		if batch.AttemptVersion != 1 || len(batch.Items) != 1 {
			t.Fatalf("应有一条 v1 结果: %+v", batch)
		}
		if batch.Items[0].Category != AnnotationStrength || batch.Items[0].Anchor.MessageID == "" {
			t.Fatalf("条目结构不完整: %+v", batch.Items[0])
		}
		// 自动定位必须给出坐标，且引用可回读。
		item := batch.Items[0]
		if !item.Anchor.Resolved || item.Anchor.StartCP == nil || item.Anchor.EndCP == nil {
			t.Fatalf("唯一引用应定位: %+v", item.Anchor)
		}
	}
	if f.fake.AnnotationCalls() != 2 {
		t.Fatalf("每轮只安排一次分析，实际 %d", f.fake.AnnotationCalls())
	}
	// 批注不影响正式轮次与会话状态。
	detail, err := f.store.Detail(context.Background(), 1, f.debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Debate.CompletedRounds != 2 || detail.Debate.Status != "active" {
		t.Fatalf("批注不得改变轮次或会话状态: %+v", detail.Debate)
	}
	for _, turn := range detail.Turns {
		if turn.Status != "completed" {
			t.Fatalf("轮次状态不得被批注改写: %+v", turn)
		}
	}
}

func TestAnnotationReadIsReadOnly(t *testing.T) {
	f := newAnnotationFixture(t)
	before := f.fake.AnnotationCalls()
	for i := 0; i < 3; i++ {
		if _, err := f.service.GetAnnotations(context.Background(), 1, f.debate.ID); err != nil {
			t.Fatal(err)
		}
	}
	if f.fake.AnnotationCalls() != before {
		t.Fatal("读取批注不得触发模型调用")
	}
}

func TestAnnotationDisabledProducesNothing(t *testing.T) {
	clock := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	store := newMemoryStore(func() time.Time { return clock })
	fake := ai.NewFake()
	service := NewService(store, fake, NewGate(func() time.Time { return clock }))
	// 未显式开启：Complete 不设置资格，读取返回空列表。
	created, err := service.Create(context.Background(), 1, CreateInput{TopicID: topicID(1), UserStance: "pro", AIStyle: "logical"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.Submit(context.Background(), 1, created.ID, SubmitInput{
		RoundNo: 1, Content: "第一轮发言", ClientRequestID: "550e8400-e29b-41d4-a716-446655440001",
	}); err != nil {
		t.Fatal(err)
	}
	list, err := service.GetAnnotations(context.Background(), 1, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Items) != 0 {
		t.Fatalf("关闭切片时不得产生合格轮次: %+v", list.Items)
	}
	if fake.AnnotationCalls() != 0 {
		t.Fatal("关闭切片时不得调用批注生成")
	}
	// 手动补做在关闭时受控拒绝，且不写批次。
	_, _, err = service.RetryAnnotation(context.Background(), 1, created.ID, 1, AnnotationRetryInput{})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("关闭时应返回 invalid_request，实际 %v", err)
	}
}

func TestAnnotationRetryReplaysCompletedBatch(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[0].ID
	before := f.fake.AnnotationCalls()
	// 已成功批次：重放直接返回已存结果，不重新生成。
	batch, status, err := f.service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 1})
	if err != nil || status != 200 {
		t.Fatalf("已成功批次应 200 返回: %d %v", status, err)
	}
	if batch.Status != AnnotationCompleted || len(batch.Items) != 1 {
		t.Fatalf("应返回已存结果: %+v", batch)
	}
	if f.fake.AnnotationCalls() != before {
		t.Fatal("成功批次不得重复生成")
	}
}

func TestAnnotationRetryAfterFailureOnceOnly(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[1].ID
	// 把第 2 轮批次置为失败态，模拟一次已落库受控错误码的分析失败。
	current := f.store.annotations[turnID]
	current.Status = AnnotationFailed
	current.AttemptVersion = 1
	current.ContentJSON = nil
	current.ErrorCode = strPtr("model_timeout")
	f.store.annotations[turnID] = current

	// 按当前版本补做一次，成功并提升版本。
	recovered, status, err := f.service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 1})
	if err != nil || status != 201 {
		t.Fatalf("失败后补做应成功: %d %v", status, err)
	}
	if recovered.Status != AnnotationCompleted || recovered.AttemptVersion != 2 || len(recovered.Items) != 1 {
		t.Fatalf("补做应提升版本并完成: %+v", recovered)
	}
	// 成功批次始终保留，再次补做只返回已存结果，不重新生成。
	calls := f.fake.AnnotationCalls()
	again, status, err := f.service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 2})
	if err != nil || status != 200 || again.Status != AnnotationCompleted {
		t.Fatalf("成功批次应保留: %d %+v %v", status, again, err)
	}
	if f.fake.AnnotationCalls() != calls {
		t.Fatal("成功批次不得再生成")
	}
	// 唯一一次补做已用完：v2 之后不再补做，按版本冲突返回 409 并指向读取接口。
	failedAgain := f.store.annotations[turnID]
	failedAgain.Status = AnnotationFailed
	failedAgain.ContentJSON = nil
	failedAgain.ErrorCode = strPtr("model_timeout")
	f.store.annotations[turnID] = failedAgain
	_, _, err = f.service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 2})
	var conflict *Failure
	if !errors.As(err, &conflict) || conflict.Status != 409 || conflict.Details == nil || conflict.Details.QueryURL == "" {
		t.Fatalf("v2 失败后应返回 409 并附查询地址: %v", err)
	}
	if f.fake.AnnotationCalls() != calls {
		t.Fatal("已用完补做次数后不得再生成")
	}
}

func TestAnnotationRetryVersionConflict(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[0].ID
	// 传 0 表示“未启动”，但批次已完成：直接返回已存结果，不报 409。
	batch, status, err := f.service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 0})
	if err != nil || status != 200 || batch.Status != AnnotationCompleted {
		t.Fatalf("成功批次不应 409: %d %+v %v", status, batch, err)
	}
	// 已成功批次带任意正版本都只返回已存结果，不会被当作可补做的失败态。
	again, status, err := f.service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 7})
	if err != nil || status != 200 || again.AttemptVersion != 1 {
		t.Fatalf("成功批次应保持原版本: %d %+v %v", status, again, err)
	}
}

func TestAnnotationOwnershipIsolation(t *testing.T) {
	f := newAnnotationFixture(t)
	// 其他用户读取与补做都按既有 404 语义处理，不泄露资源是否存在。
	if _, err := f.service.GetAnnotations(context.Background(), 2, f.debate.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("越权读取应为 ErrNotFound: %v", err)
	}
	_, _, err := f.service.RetryAnnotation(context.Background(), 2, f.debate.ID, f.turns[0].ID, AnnotationRetryInput{})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("越权补做应为 ErrNotFound: %v", err)
	}
	// 会话不存在同样是 404。
	if _, err := f.service.GetAnnotations(context.Background(), 1, 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("不存在的会话应为 ErrNotFound: %v", err)
	}
}

func TestAnnotationInputExcludesFutureRounds(t *testing.T) {
	f := newAnnotationFixture(t)
	// 第 3 轮之后再提交一轮，然后检查第 3 轮批注输入里没有第 4 轮内容。
	_, _, err := f.service.Submit(context.Background(), 1, f.debate.ID, SubmitInput{
		RoundNo: 3, Content: "第三轮用户发言：补充范围界定。", ClientRequestID: "550e8400-e29b-41d4-a716-446655440301",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.service.waitBackground()
	records := f.fake.AnnotationRecords()
	// 找到触发第 3 轮的那条记录。
	var found bool
	for _, record := range records {
		if record.Request.TriggerRoundNo != 3 {
			continue
		}
		found = true
		for _, message := range record.Request.Messages {
			if message.TurnNo > 3 {
				t.Fatalf("批注输入不得包含未来轮: %+v", message)
			}
			// 触发轮的双方原文必须在白名单内。
			if message.TurnNo == 3 && message.Content == "" {
				t.Fatal("触发轮原文不得为空")
			}
		}
		var hasUser, hasAssistant bool
		for _, message := range record.Request.Messages {
			if message.TurnNo != 3 {
				continue
			}
			if message.Role == "user" {
				hasUser = true
			}
			if message.Role == "assistant" {
				hasAssistant = true
			}
		}
		if !hasUser || !hasAssistant {
			t.Fatalf("必须包含触发轮双方原文: user=%v assistant=%v", hasUser, hasAssistant)
		}
	}
	if !found {
		t.Fatal("缺少第 3 轮批注记录")
	}
}

func TestAnnotationGeneratorRejectionSavesSafeCode(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[0].ID
	// 让下一次批注返回无法定位的锚点：整份结果应被拒绝并落库受控错误码。
	f.store.annotations[turnID] = TurnAnnotation{
		ID: 900, DebateID: f.debate.ID, TurnID: turnID, Status: "failed", AttemptVersion: 1,
		ErrorCode: strPtr("model_error"),
	}
	f.fake.FailAnnotations(&ai.Error{Code: ai.CodeModelError, Message: "合成结构错误"}, 1)
	if _, _, err := f.service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 1}); err == nil {
		t.Fatal("应返回失败")
	}
	list, err := f.service.GetAnnotations(context.Background(), 1, f.debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range list.Items {
		if batch.TurnID != formatID(turnID) {
			continue
		}
		if batch.Status != AnnotationFailed || batch.ErrorCode == nil || *batch.ErrorCode != "model_error" {
			t.Fatalf("失败批次应只保留受控码: %+v", batch)
		}
		if len(batch.Items) != 0 {
			t.Fatal("失败批次不得返回条目")
		}
	}
}

func strPtr(value string) *string { return &value }

func TestAnnotationPanicIsContained(t *testing.T) {
	f := newAnnotationFixture(t)
	panicGen := panicAnnotationGenerator{}
	service := NewService(f.store, panicGen, NewGate(nil))
	service.EnableAnnotations(true)
	// 直接调用生成路径：panic 必须转成受控失败，不冒泡为进程级 panic。
	result, err := service.generateAnnotations(context.Background(), ai.AnnotationRequest{
		Mode: ai.ModeStandard4, Topic: ai.Topic{Title: "题目", Description: "说明"}, UserStance: ai.StancePro,
		TriggerRoundNo: 1,
		Messages:       []ai.AnnotationMessage{{MessageID: "1", TurnNo: 1, Role: ai.RoleUser, Content: "原文", Trigger: true}},
	})
	if err == nil {
		t.Fatalf("panic 应转成错误: %+v", result)
	}
	var ae *ai.Error
	if !errors.As(err, &ae) || ae.Code != ai.CodeModelError {
		t.Fatalf("应为受控 model_error: %v", err)
	}
}

type panicAnnotationGenerator struct{}

func (panicAnnotationGenerator) Generate(context.Context, ai.Request) (ai.Result, error) {
	return ai.Result{}, nil
}
func (panicAnnotationGenerator) GenerateAnnotations(context.Context, ai.AnnotationRequest) (ai.AnnotationResult, error) {
	panic("synthetic annotation panic")
}

func TestAnnotationTimeoutIsClassified(t *testing.T) {
	f := newAnnotationFixture(t)
	slow := slowAnnotationGenerator{delay: 200 * time.Millisecond}
	service := NewService(f.store, slow, NewGate(nil))
	service.EnableAnnotations(true)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, err := service.generateAnnotations(ctx, ai.AnnotationRequest{
		Mode: ai.ModeStandard4, Topic: ai.Topic{Title: "题目", Description: "说明"}, UserStance: ai.StancePro,
		TriggerRoundNo: 1,
		Messages:       []ai.AnnotationMessage{{MessageID: "1", TurnNo: 1, Role: ai.RoleUser, Content: "原文", Trigger: true}},
	})
	var ae *ai.Error
	if !errors.As(err, &ae) || ae.Code != ai.CodeModelTimeout {
		t.Fatalf("超时应为 model_timeout: %v", err)
	}
}

type slowAnnotationGenerator struct{ delay time.Duration }

func (slowAnnotationGenerator) Generate(context.Context, ai.Request) (ai.Result, error) {
	return ai.Result{}, nil
}
func (g slowAnnotationGenerator) GenerateAnnotations(ctx context.Context, _ ai.AnnotationRequest) (ai.AnnotationResult, error) {
	timer := time.NewTimer(g.delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ai.AnnotationResult{}, ctx.Err()
	case <-timer.C:
		return ai.AnnotationResult{ContentJSON: `{"items":[]}`}, nil
	}
}

func TestAnnotationSlotBusyReturns202WithoutQueueing(t *testing.T) {
	f := newAnnotationFixture(t)
	// 占住分析槽，模拟另一个仍在进行的分析。
	if !f.service.acquireAnnotationSlot() {
		t.Fatal("首次占用应成功")
	}
	defer f.service.releaseAnnotationSlot()
	calls := f.fake.AnnotationCalls()
	batch, status, err := f.service.RetryAnnotation(context.Background(), 1, f.debate.ID, f.turns[0].ID, AnnotationRetryInput{ExpectedAttemptVersion: 0})
	if err != nil {
		t.Fatalf("槽位忙不应报错: %v", err)
	}
	if status != 202 || batch.Status != AnnotationCompleted {
		t.Fatalf("槽位忙应返回既有状态与 202: %d %+v", status, batch)
	}
	if f.fake.AnnotationCalls() != calls {
		t.Fatal("槽位忙不得排队或调用模型")
	}
}

func TestAnnotationBackgroundDoesNotBlockSubmit(t *testing.T) {
	clock := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	store := newMemoryStore(func() time.Time { return clock })
	// 批注很慢，但正式发言必须立即返回。
	fake := ai.NewFake(ai.FakeStep{Result: ai.Result{Content: "AI 回复"}})
	service := NewService(store, fake, NewGate(func() time.Time { return clock }))
	service.EnableAnnotations(true)
	defer service.waitBackground()
	created, err := service.Create(context.Background(), 1, CreateInput{TopicID: topicID(1), UserStance: "pro", AIStyle: "logical"})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, status, err := service.Submit(context.Background(), 1, created.ID, SubmitInput{
		RoundNo: 1, Content: "第一轮发言", ClientRequestID: "550e8400-e29b-41d4-a716-446655440001",
	}); err != nil || status != 201 {
		t.Fatalf("提交应成功: %d %v", status, err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("后台批注不得阻塞正式提交，实际耗时 %s", elapsed)
	}
}

func TestAnnotationNoDuplicateOnReplay(t *testing.T) {
	f := newAnnotationFixture(t)
	// 以相同 client_request_id 重放第 2 轮：不应再安排一次批注。
	calls := f.fake.AnnotationCalls()
	if _, status, err := f.service.Submit(context.Background(), 1, f.debate.ID, SubmitInput{
		RoundNo: 2, Content: "第2轮用户发言：范围需要先界定。", ClientRequestID: "550e8400-e29b-41d4-a716-446655440002",
	}); err != nil || status != 200 {
		t.Fatalf("重放应返回 200: %d %v", status, err)
	}
	service := f.service
	service.waitBackground()
	if f.fake.AnnotationCalls() != calls {
		t.Fatal("幂等重放不得重复生成批注")
	}
}

func TestAnnotationConcurrentRetryGeneratesOnce(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[0].ID
	// 先把已完成的批次改为失败，模拟可补做状态。
	current := f.store.annotations[turnID]
	current.Status = "failed"
	current.AttemptVersion = 1
	current.ContentJSON = nil
	current.ErrorCode = strPtr("model_timeout")
	f.store.annotations[turnID] = current

	calls := f.fake.AnnotationCalls()
	var wg sync.WaitGroup
	results := make([]int, 4)
	for i := range results {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			_, status, err := f.service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 1})
			if err != nil {
				results[index] = -1
				return
			}
			results[index] = status
		}(i)
	}
	wg.Wait()
	// 分析槽只有一个：其余并发请求返回 202 或版本结果，绝不重复生成。
	if got := f.fake.AnnotationCalls() - calls; got > 1 {
		t.Fatalf("并发补做最多生成一次，实际 %d", got)
	}
}

func TestAnnotationStoredResultRoundTrip(t *testing.T) {
	clock := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	raw := `{"items":[{"category":"possible_logic_gap","label":"可能的推理缺口","reason":"从可检验直接推出可量化，中间缺少前提。","improvement":"补上可检验与可量化之间的关系。","anchor":{"message_id":"5","quote":"标准应当可检验","start_cp":0,"end_cp":7,"resolved":true},"turn_no":1,"role":"user"}]}`
	batch := AnnotationBatch{
		Turn: Turn{ID: 4, RoundNo: 1},
		Annotation: &TurnAnnotation{
			ID: 9, DebateID: 3, TurnID: 4, Status: "completed", AttemptVersion: 1, ContentJSON: &raw,
		},
	}
	dto := projectAnnotation(batch)
	if dto.Status != AnnotationCompleted || len(dto.Items) != 1 {
		t.Fatalf("投影失败: %+v", dto)
	}
	item := dto.Items[0]
	if item.Category != "possible_logic_gap" || item.Anchor.StartCP == nil || *item.Anchor.StartCP != 0 {
		t.Fatalf("条目字段未保留: %+v", item)
	}
	if item.TurnNo != 1 || item.Role != "user" {
		t.Fatalf("轮次与角色应保留: %+v", item)
	}
	_ = clock
}

func TestAnnotationCorruptStoredContentIsProjectedSafely(t *testing.T) {
	raw := `{"items":not-json}`
	dto := projectAnnotation(AnnotationBatch{
		Turn:       Turn{ID: 4, RoundNo: 1},
		Annotation: &TurnAnnotation{ID: 9, DebateID: 3, TurnID: 4, Status: "completed", AttemptVersion: 1, ContentJSON: &raw},
	})
	if dto.Status != AnnotationFailed || dto.ErrorCode == nil || *dto.ErrorCode != "content_invalid" {
		t.Fatalf("损坏内容应标记为不可读结果: %+v", dto)
	}
	if len(dto.Items) != 0 {
		t.Fatal("损坏内容不得返回条目")
	}
}

func TestAnnotationNotStartedProjection(t *testing.T) {
	dto := projectAnnotation(AnnotationBatch{Turn: Turn{ID: 4, RoundNo: 3}})
	if dto.Status != AnnotationNotStarted || dto.AttemptVersion != 0 || dto.TurnID != "4" {
		t.Fatalf("未开始的投影错误: %+v", dto)
	}
	if dto.Items == nil {
		t.Fatal("items 必须是空数组而非 null")
	}
}

func TestAnnotationRetryInputValidation(t *testing.T) {
	if _, err := ValidateAnnotationRetryInput(AnnotationRetryInput{ExpectedAttemptVersion: 0}); err != nil {
		t.Fatal("0 表示未启动，应允许")
	}
	if _, err := ValidateAnnotationRetryInput(AnnotationRetryInput{ExpectedAttemptVersion: 2}); err != nil {
		t.Fatal("正版本是格式合法的补做请求，是否允许由认领时的状态决定")
	}
	if _, err := ValidateAnnotationRetryInput(AnnotationRetryInput{ExpectedAttemptVersion: ^uint64(0)}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatal("哨兵值必须拒绝")
	}
}

func TestAnnotationToItemsRejectsForeignMessage(t *testing.T) {
	sources := []AnnotationSource{{MessageID: 5, TurnID: 4, RoundNo: 1, Role: "user", Content: "原文内容"}}
	ok := func(reason string) ai.AnnotationItem {
		return ai.AnnotationItem{
			Category: "strength", Label: "论证有效", Reason: reason, Improvement: "改进",
			Anchor: ai.AnnotationAnchor{MessageID: "5", Quote: "原文内容", StartCP: intPtr(0), EndCP: intPtr(4)},
		}
	}
	result := func(items ...ai.AnnotationItem) ai.AnnotationResult { return ai.AnnotationResult{Items: items} }
	// 引用不在白名单：拒绝。
	if _, err := toAnnotationItems(result(ai.AnnotationItem{
		Category: "strength", Label: "论证有效", Reason: "理由", Improvement: "改进",
		Anchor: ai.AnnotationAnchor{MessageID: "6", Quote: "原文", StartCP: intPtr(0), EndCP: intPtr(2)},
	}), sources); !errors.Is(err, ErrAnchorNotLocated) {
		t.Fatalf("跨消息引用应拒绝: %v", err)
	}
	// 缺少服务端坐标：v2 不再用 quote 反推位置，整批拒绝。
	if _, err := toAnnotationItems(result(ai.AnnotationItem{
		Category: "strength", Label: "论证有效", Reason: "理由", Improvement: "改进",
		Anchor: ai.AnnotationAnchor{MessageID: "5", Quote: "原文内容"},
	}), sources); !errors.Is(err, ErrAnchorNotLocated) {
		t.Fatalf("缺少坐标应拒绝: %v", err)
	}
	// 坐标与引文不一致：不得改挂其他位置，整批拒绝。
	if _, err := toAnnotationItems(result(ai.AnnotationItem{
		Category: "strength", Label: "论证有效", Reason: "理由", Improvement: "改进",
		Anchor: ai.AnnotationAnchor{MessageID: "5", Quote: "原文内容", StartCP: intPtr(1), EndCP: intPtr(5)},
	}), sources); !errors.Is(err, ErrAnchorNotLocated) {
		t.Fatalf("坐标与引文不一致应拒绝: %v", err)
	}
	// 四条：拒绝。
	items := result(ok("理由一"), ok("理由二"), ok("理由三"), ok("理由四"))
	if _, err := toAnnotationItems(items, sources); err == nil {
		t.Fatal("超过 3 条应拒绝")
	}
	// 同一消息、同一原句、不同坐标、反馈相同：必须各自保存。
	sameText := result(
		ai.AnnotationItem{Category: "strength", Label: "论证有效", Reason: "理由", Improvement: "改进",
			Anchor: ai.AnnotationAnchor{MessageID: "5", Quote: "原文", StartCP: intPtr(0), EndCP: intPtr(2)}},
		ai.AnnotationItem{Category: "strength", Label: "论证有效", Reason: "理由", Improvement: "改进",
			Anchor: ai.AnnotationAnchor{MessageID: "5", Quote: "内容", StartCP: intPtr(2), EndCP: intPtr(4)}},
	)
	deduped, err := toAnnotationItems(sameText, sources)
	if err != nil || len(deduped) != 2 {
		t.Fatalf("不同位置应各自保存: %+v %v", deduped, err)
	}
	// 同一坐标、同一反馈才是完全重复。
	duplicate := result(ok("理由"), ok("理由"))
	if _, err := toAnnotationItems(duplicate, sources); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("完全重复应拒绝: %v", err)
	}
	// 合法条目回填轮次与角色，并给出服务端坐标。
	valid, err := toAnnotationItems(result(ok("理由")), sources)
	if err != nil {
		t.Fatalf("合法条目应通过: %v", err)
	}
	anchor := valid[0].Anchor
	if !anchor.Resolved || anchor.StartCP == nil || *anchor.StartCP != 0 || anchor.EndCP == nil || *anchor.EndCP != 4 {
		t.Fatalf("锚点应带服务端坐标: %+v", anchor)
	}
	if valid[0].TurnNo != 1 || valid[0].Role != "user" {
		t.Fatalf("轮次与角色应回填: %+v", valid[0])
	}
}

func intPtr(value int) *int { return &value }

func TestAnnotationDetailsOnlyForKnownBatch(t *testing.T) {
	d := Debate{ID: 3}
	turn := Turn{ID: 4}
	annotation := TurnAnnotation{AttemptVersion: 2}
	details := failedAnnotationDetails(d, turn, annotation)
	if details.Status != AnnotationFailed || details.AttemptVersion != 2 || details.TurnID != "4" {
		t.Fatalf("details 结构错误: %+v", details)
	}
	if !strings.HasSuffix(details.QueryURL, "/annotations") {
		t.Fatalf("query_url 应指向批注读取: %q", details.QueryURL)
	}
}
