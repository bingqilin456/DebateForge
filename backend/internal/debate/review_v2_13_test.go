package debate

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// V2-13 先行测试：复盘与比赛完成状态独立；失败可手动重试，刷新后可恢复；
// 重试只补失败本身，不覆盖已成功内容、批注也不受影响。
// 本文件先于实现落地，用于锁定失败状态、幂等与恢复语义。

func TestReviewFailureIsIndependentFromCompletedDebate(t *testing.T) {
	mem := newMemoryStore(nil)
	service := NewService(mem, ai.NewFake(), NewGate(nil))
	service.EnableReviews(true)
	created := completeFourRoundDebate(t, mem, service)

	fake := ai.NewFake()
	fake.FailReviews(&ai.Error{Code: ai.CodeModelTimeout, Message: "复盘生成超时"}, 1)
	service = NewService(mem, fake, NewGate(nil))
	service.EnableReviews(true)

	if _, _, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{}); err == nil {
		t.Fatal("首次复盘失败必须返回受控错误")
	}

	// 刷新：详情与复盘状态都要能恢复。比赛仍是已完成，复盘独立为 failed。
	detail, err := service.Detail(context.Background(), 1, created.ID)
	if err != nil {
		t.Fatalf("刷新详情失败: %v", err)
	}
	if detail.Debate.Status != "completed" || detail.Debate.CompletedRounds != 4 {
		t.Fatalf("复盘失败不得改变比赛完成状态: %+v", detail.Debate)
	}
	status, err := service.GetReview(context.Background(), 1, created.ID)
	if err != nil {
		t.Fatalf("刷新复盘状态失败: %v", err)
	}
	if status.Status != "failed" || status.ErrorCode == nil || *status.ErrorCode != "model_timeout" {
		t.Fatalf("刷新后应恢复独立失败状态: %+v", status)
	}
	if status.AttemptVersion == nil || *status.AttemptVersion != 1 {
		t.Fatalf("失败状态必须带可重试版本: %+v", status)
	}

	// 重试：只补失败的复盘，成功后被保存，可再次读取。
	version := uint64(1)
	result, code, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{Retry: true, ExpectedAttemptVersion: &version})
	if err != nil || code != 200 {
		t.Fatalf("失败后显式重试应成功: code=%d err=%v", code, err)
	}
	if result.Status != "completed" || result.Content == nil {
		t.Fatalf("重试成功应返回已保存复盘: %+v", result)
	}
	stored, err := service.GetReview(context.Background(), 1, created.ID)
	if err != nil || stored.Status != "completed" || stored.Content == nil {
		t.Fatalf("复盘应已独立持久化: %+v err=%v", stored, err)
	}
	if got := len(stored.Content.Dimensions); got != 4 {
		t.Fatalf("复盘必须保存四个维度，实际 %d", got)
	}
}

func TestReviewRetryPreservesSuccessfulContentAndAnnotations(t *testing.T) {
	mem := newMemoryStore(nil)
	fake := ai.NewFake()
	service := NewService(mem, fake, NewGate(nil))
	service.EnableReviews(true)
	created := completeFourRoundDebate(t, mem, service)

	// 制造“复盘之前就存在的成功结果”：V1 结构化总结与一条已完成批注。
	// 两者都属既有基础设施的独立行，复盘失败与重试都不得覆盖它们。
	summaryJSON := `{"strengths":[{"round_no":1,"point":"a","reason":"b"}],"issues":[{"round_no":1,"point":"c","reason":"d"}],"suggestions":[{"round_no":1,"action":"e","example":"f"}]}`
	schema := 1
	before := mem.summaries[created.ID]
	before.Status = "completed"
	before.SchemaVersion = &schema
	before.ContentJSON = &summaryJSON
	mem.summaries[created.ID] = before

	turns := mem.turns[created.ID]
	firstTurn := turns[0]
	firstTurn.AnnotationEligible = true
	turns[0] = firstTurn
	mem.turns[created.ID] = turns
	mem.annotations[firstTurn.ID] = TurnAnnotation{
		ID: 9001, DebateID: created.ID, TurnID: firstTurn.ID, Status: "completed", AttemptVersion: 1,
		ContentJSON: strPtr(`{"items":[{"category":"strength","label":"有效","reason":"理由","improvement":"改进","anchor":{"message_id":"` + formatID(firstTurn.Messages[0].ID) + `","quote":"` + firstTurn.Messages[0].Content + `","start_cp":0,"end_cp":` + strconv.Itoa(len([]rune(firstTurn.Messages[0].Content))) + `,"resolved":true},"turn_no":1,"role":"user"}]}`),
	}

	fake.FailReviews(&ai.Error{Code: ai.CodeModelError, Message: "复盘结构无效"}, 1)
	if _, _, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{}); err == nil {
		t.Fatal("首次复盘应失败")
	}
	afterFail, err := service.GetSummary(context.Background(), 1, created.ID)
	if err != nil || afterFail.Status != "completed" || afterFail.Content == nil {
		t.Fatalf("复盘失败不得覆盖既有总结: %+v err=%v", afterFail, err)
	}
	batches, err := service.GetAnnotations(context.Background(), 1, created.ID)
	if err != nil || len(batches.Items) != 1 || batches.Items[0].Status != "completed" || len(batches.Items[0].Items) != 1 {
		t.Fatalf("复盘失败不得覆盖既有批注: %+v err=%v", batches, err)
	}

	version := uint64(1)
	if _, _, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{Retry: true, ExpectedAttemptVersion: &version}); err != nil {
		t.Fatalf("重试应成功: %v", err)
	}
	afterRetry, err := service.GetSummary(context.Background(), 1, created.ID)
	if err != nil || afterRetry.Status != "completed" || afterRetry.Content == nil {
		t.Fatalf("重试不得覆盖既有总结: %+v err=%v", afterRetry, err)
	}
	batches, err = service.GetAnnotations(context.Background(), 1, created.ID)
	if err != nil || len(batches.Items) != 1 || batches.Items[0].Status != "completed" || len(batches.Items[0].Items) != 1 {
		t.Fatalf("重试不得覆盖既有批注: %+v err=%v", batches, err)
	}
}

func TestReviewConcurrentClicksDoNotDuplicateGeneration(t *testing.T) {
	mem := newMemoryStore(nil)
	fake := ai.NewFake()
	service := NewService(mem, fake, NewGate(nil))
	service.EnableReviews(true)
	created := completeFourRoundDebate(t, mem, service)

	var wg sync.WaitGroup
	start := make(chan struct{})
	statuses := make([]int, 4)
	errs := make([]error, 4)
	for i := range statuses {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			// 直接走不含 HTTP 防刷的认领路径，验证“重复点击不重复生成”本身；
			// 生产 HTTP 入口另有 Gate 将并发点击变为 429 generation_busy。
			_, statuses[i], errs[i] = service.requestReview(context.Background(), 1, created.ID, ReviewInput{})
		}(i)
	}
	close(start)
	wg.Wait()

	generated := 0
	for i := range statuses {
		if errs[i] != nil {
			// 并发槽只有一个，其余请求必须拿到受控的“生成占用”而不是重复生成。
			var failure *Failure
			if !errors.As(errs[i], &failure) || failure.Code != codeGenerationBusy {
				t.Fatalf("并发点击应只允许一次生成，其余为 generation_busy，实际 %d: %v", i, errs[i])
			}
		}
		if statuses[i] == 201 {
			generated++
		}
	}
	if generated != 1 {
		t.Fatalf("并发重复点击只能有一次真实生成，实际 %d（状态 %v）", generated, statuses)
	}
	if calls := fake.ReviewCalls(); calls != 1 {
		t.Fatalf("重复点击不得重复调用模型，实际 %d 次", calls)
	}
}

func TestReviewCompletionIsTerminal(t *testing.T) {
	mem := newMemoryStore(nil)
	fake := ai.NewFake()
	// 四轮提交与复盘都会占用同一账号的 HTTP 防刷计数；用可推进的时钟避免一分钟窗口打满。
	clock := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	service := NewService(mem, fake, NewGate(func() time.Time { return clock }))
	service.EnableReviews(true)
	created := completeFourRoundDebate(t, mem, service)
	clock = clock.Add(time.Minute)
	first, code, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{})
	if err != nil || code != 201 || first.Content == nil {
		t.Fatalf("首次生成失败: code=%d err=%v", code, err)
	}
	callsAfterFirst := fake.ReviewCalls()
	// 已完成复盘是终态：重复 POST（含带版本的重试形状）都按幂等重放返回 200 与已保存内容，
	// 既不再生成也不覆盖成功结果。语义与既有总结/策略/批注的“成功批次重放”一致。
	version := uint64(1)
	for _, in := range []ReviewInput{{}, {Retry: true, ExpectedAttemptVersion: &version}} {
		clock = clock.Add(time.Minute)
		again, status, err := service.RequestReview(context.Background(), 1, created.ID, in)
		if err != nil || status != 200 {
			t.Fatalf("已完成复盘重复 POST 应幂等返回 200: status=%d err=%v", status, err)
		}
		if again.Content == nil || again.AttemptVersion == nil || *again.AttemptVersion != 1 {
			t.Fatalf("重复 POST 必须返回同一份已保存复盘: %+v", again)
		}
	}
	if fake.ReviewCalls() != callsAfterFirst {
		t.Fatalf("终态复盘不得再次调用模型: before=%d after=%d", callsAfterFirst, fake.ReviewCalls())
	}
}
