package debate

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// batchAnnotationGenerator 用固定批次结果替换模型输出，其余能力复用 Fake。
// 这样可以在内存仓储上验证服务层的批次落库规则，而不依赖真实模型解析。
type batchAnnotationGenerator struct {
	*ai.Fake
	batch func(ai.AnnotationRequest) (ai.AnnotationResult, error)
}

func (g batchAnnotationGenerator) GenerateAnnotations(_ context.Context, req ai.AnnotationRequest) (ai.AnnotationResult, error) {
	return g.batch(req)
}

// triggerUserAnchor 取触发轮用户消息的前 6 个码点作为合法锚点。
func triggerUserAnchor(req ai.AnnotationRequest) ai.AnnotationAnchor {
	for _, message := range req.Messages {
		if !message.Trigger || message.Role != ai.RoleUser {
			continue
		}
		runes := []rune(message.Content)
		quote := runes
		if len(quote) > 6 {
			quote = quote[:6]
		}
		return ai.AnnotationAnchor{
			MessageID: message.MessageID,
			Quote:     string(quote),
			StartCP:   intPtr(0),
			EndCP:     intPtr(len(quote)),
		}
	}
	return ai.AnnotationAnchor{}
}

// storedAnnotationItems 解码内存仓储中保存的批注条目。
func storedAnnotationItems(t *testing.T, batch TurnAnnotation) []AnnotationItem {
	t.Helper()
	if batch.ContentJSON == nil {
		t.Fatal("完成的批次必须保存规范化内容")
	}
	var payload struct {
		Items []AnnotationItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(*batch.ContentJSON), &payload); err != nil {
		t.Fatalf("落库内容不是合法 JSON: %v\n%s", err, *batch.ContentJSON)
	}
	return payload.Items
}

// retryWithBatchGenerator 在内存仓储上执行一次手动补做，并返回日志缓冲供断言。
func retryWithBatchGenerator(
	t *testing.T,
	f *annotationFixture,
	turnID uint64,
	batch func(ai.AnnotationRequest) (ai.AnnotationResult, error),
) (*strings.Builder, AnnotationBatchDTO, int, error) {
	t.Helper()
	var logged strings.Builder
	generator := batchAnnotationGenerator{Fake: f.fake, batch: batch}
	service := NewService(f.store, generator, NewGate(nil), WithLogger(newDebateLogger(&logged)))
	service.EnableAnnotations(true)
	t.Cleanup(func() { service.waitBackground() })
	out, status, err := service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 1})
	return &logged, out, status, err
}

// seedFailedAnnotation 预置一条失败批次，使补做满足 attempt_version 原子写回条件。
func seedFailedAnnotation(f *annotationFixture, turnID uint64) {
	f.store.annotations[turnID] = TurnAnnotation{
		ID: 901, DebateID: f.debate.ID, TurnID: turnID, Status: AnnotationFailed, AttemptVersion: 1,
		ErrorCode: strPtr("model_error"),
	}
}

func findBatch(t *testing.T, batches []AnnotationBatchDTO, turnID uint64) AnnotationBatchDTO {
	t.Helper()
	for _, batch := range batches {
		if batch.TurnID == formatID(turnID) {
			return batch
		}
	}
	t.Fatalf("未找到轮次 %d 的批注批次", turnID)
	return AnnotationBatchDTO{}
}

// TestAnnotationPartialBatchSavesValidItems 证明混合有效/无效候选中，
// 有效条目真实保存为 completed 批次，并记录 partial_items_rejected 与固定分类计数。
func TestAnnotationPartialBatchSavesValidItems(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[1].ID
	seedFailedAnnotation(f, turnID)

	var want ai.AnnotationAnchor
	logged, batch, status, err := retryWithBatchGenerator(t, f, turnID, func(req ai.AnnotationRequest) (ai.AnnotationResult, error) {
		want = triggerUserAnchor(req)
		if want.MessageID == "" {
			t.Fatal("测试请求缺少触发轮用户消息")
		}
		return ai.AnnotationResult{
			Items: []ai.AnnotationItem{{
				Category: AnnotationStrength, Label: "论证有效",
				Reason: "该句给出了可检验的判断标准", Improvement: "继续补充依据",
				Anchor: want,
			}},
			Candidates: 2,
			Rejected:   ai.AnnotationRejections{UnknownFragment: 1},
			// 不可选上下文只是计数，不进入日志正文。
			UnselectableFragments: 3,
			ContentJSON:           `{"items":[]}`,
		}, nil
	})
	if err != nil || status != 201 {
		t.Fatalf("部分有效批次应成功保存: %d %v", status, err)
	}
	if batch.Status != AnnotationCompleted || len(batch.Items) != 1 {
		t.Fatalf("返回批次应为完成且含一条有效条目: %+v", batch)
	}

	stored := f.store.annotations[turnID]
	if stored.Status != AnnotationCompleted {
		t.Fatalf("内存仓储应保存完成批次: %+v", stored)
	}
	items := storedAnnotationItems(t, stored)
	if len(items) != 1 {
		t.Fatalf("有效条目必须真实落库: %+v", items)
	}
	item := items[0]
	if item.Anchor.MessageID != want.MessageID || !item.Anchor.Resolved {
		t.Fatalf("落库锚点应指向权威原文并标记已定位: %+v", item.Anchor)
	}
	if item.TurnNo != 2 || item.Role != "user" {
		t.Fatalf("轮次与角色应由服务端回填: %+v", item)
	}

	entry := lastDebateLog(t, logged.String())
	if entry["reason_code"] != "partial_items_rejected" {
		t.Fatalf("reason_code = %v，期望 partial_items_rejected", entry["reason_code"])
	}
	if entry["error_code"] != "ok" {
		t.Fatalf("部分有效仍属成功批次: %v", entry["error_code"])
	}
	for key, want := range map[string]float64{
		"candidates": 2, "accepted": 1, "unknown_fragment": 1, "invalid_fields": 0, "duplicate": 0, "unselectable_fragments": 3,
	} {
		if got, ok := entry[key].(float64); !ok || got != want {
			t.Fatalf("%s = %v，期望 %v", key, entry[key], want)
		}
	}
	if entry["prompt_version"] != ai.AnnotationPromptVersion {
		t.Fatalf("日志应记录提示词版本: %v", entry["prompt_version"])
	}
	// 模型给的编号与原文都不允许出现在日志里。
	if strings.Contains(logged.String(), "f99") || strings.Contains(logged.String(), item.Anchor.Quote) {
		t.Fatalf("日志泄露了模型编号或原文：%s", logged.String())
	}

	// 读路径应返回同一完成批次与一条已定位条目。
	list, err := f.service.GetAnnotations(context.Background(), 1, f.debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	readBack := findBatch(t, list.Items, turnID)
	if readBack.Status != AnnotationCompleted || len(readBack.Items) != 1 || !readBack.Items[0].Anchor.Resolved {
		t.Fatalf("读回的完成批次应保留有效条目: %+v", readBack)
	}
}

// TestAnnotationAllCandidatesRejectedMarksFailed 证明有候选但全部无法引用时，
// 批次落库为 failed 并记录 all_candidates_rejected，而不是当成“没有批注”。
func TestAnnotationAllCandidatesRejectedMarksFailed(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[1].ID
	seedFailedAnnotation(f, turnID)

	allRejected := (&ai.Error{Code: ai.CodeModelError, Message: "批注候选全部无法引用"}).WithCause(ai.ErrAnnotationAllRejected)
	logged, _, _, err := retryWithBatchGenerator(t, f, turnID, func(ai.AnnotationRequest) (ai.AnnotationResult, error) {
		return ai.AnnotationResult{
			Candidates: 1,
			Rejected:   ai.AnnotationRejections{UnknownFragment: 1},
		}, allRejected
	})
	if err == nil {
		t.Fatal("全部候选无效应返回失败")
	}
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatalf("服务层失败应是受控 Failure: %v", err)
	}
	if !errors.Is(failure.Cause, ai.ErrAnnotationAllRejected) {
		t.Fatalf("失败应保留全无效哨兵语义: %v", failure.Cause)
	}
	if failure.Code == "" || failure.Details == nil {
		t.Fatalf("失败应带受控业务码与轮次定位: %+v", failure)
	}

	stored := f.store.annotations[turnID]
	if stored.Status != AnnotationFailed {
		t.Fatalf("全无效批次应落库为失败: %+v", stored)
	}
	if stored.ContentJSON != nil {
		t.Fatalf("失败批次不得保存任何条目: %s", *stored.ContentJSON)
	}
	if stored.ErrorCode == nil || *stored.ErrorCode != "model_error" {
		t.Fatalf("失败批次应只存受控错误码: %+v", stored.ErrorCode)
	}

	entry := lastDebateLog(t, logged.String())
	if entry["reason_code"] != "all_candidates_rejected" {
		t.Fatalf("reason_code = %v，期望 all_candidates_rejected", entry["reason_code"])
	}
	if entry["error_code"] != "model_error" {
		t.Fatalf("error_code = %v，期望 model_error", entry["error_code"])
	}
	if entry["candidates"] != float64(1) || entry["accepted"] != float64(0) || entry["unknown_fragment"] != float64(1) {
		t.Fatalf("全无效批次应记录候选与分类计数: %+v", entry)
	}
}

// TestAnnotationExplicitEmptyBatchIsCompleted 证明模型显式返回空数组时，
// 批次是 completed 且原因码为 ok，与“有候选但全部无效”严格区分。
func TestAnnotationExplicitEmptyBatchIsCompleted(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[1].ID
	seedFailedAnnotation(f, turnID)

	logged, batch, status, err := retryWithBatchGenerator(t, f, turnID, func(ai.AnnotationRequest) (ai.AnnotationResult, error) {
		return ai.AnnotationResult{ContentJSON: `{"items":[]}`}, nil
	})
	if err != nil || status != 201 {
		t.Fatalf("显式空数组应成功: %d %v", status, err)
	}
	if batch.Status != AnnotationCompleted || len(batch.Items) != 0 {
		t.Fatalf("显式空数组应保存为完成且零条目: %+v", batch)
	}

	stored := f.store.annotations[turnID]
	if stored.Status != AnnotationCompleted {
		t.Fatalf("空批次应落库为完成: %+v", stored)
	}
	if items := storedAnnotationItems(t, stored); len(items) != 0 {
		t.Fatalf("空批次不得写入条目: %+v", items)
	}

	entry := lastDebateLog(t, logged.String())
	if entry["reason_code"] != "ok" || entry["candidates"] != float64(0) || entry["accepted"] != float64(0) {
		t.Fatalf("显式空数组应记录 ok 与零计数: %+v", entry)
	}
}
