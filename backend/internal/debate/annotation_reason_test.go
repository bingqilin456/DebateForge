package debate

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// newDebateLogger 返回写入 buffer 的 JSON 日志器，用于断言白名单字段。
func newDebateLogger(buffer *strings.Builder) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// lastDebateLog 解析最后一条 JSON 日志，用于按字段断言。
func lastDebateLog(t *testing.T, logged string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logged), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[len(lines)-1]) == "" {
		t.Fatal("没有日志输出")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("日志不是 JSON: %v\n%s", err, lines[len(lines)-1])
	}
	return entry
}

// TestToAnnotationItemsReasonCodes 证明批注业务校验的每条拒绝路径都有固定原因码，
// 同时原错误判定语义（errors.Is）保持不变。
func TestToAnnotationItemsReasonCodes(t *testing.T) {
	source := AnnotationSource{MessageID: 7, TurnID: 4, RoundNo: 1, Role: "user", Content: "原文内容应当可检验。"}
	// annotation-v2 的锚点必须带服务端坐标；引文只需与坐标处的原文逐字相等。
	item := func(messageID, quote, reason string, start, end int) ai.AnnotationItem {
		return ai.AnnotationItem{
			Category: AnnotationStrength, Label: "论证有效", Reason: reason, Improvement: "继续补充依据",
			Anchor: ai.AnnotationAnchor{MessageID: messageID, Quote: quote, StartCP: intPtr(start), EndCP: intPtr(end)},
		}
	}
	result := func(items ...ai.AnnotationItem) ai.AnnotationResult { return ai.AnnotationResult{Items: items} }
	cases := []struct {
		name         string
		items        ai.AnnotationResult
		wantReason   string
		wantSentinel error
	}{
		{
			name:         "引用消息不在白名单",
			items:        result(item("999", "原文内容", "理由", 0, 4)),
			wantReason:   "message_id_not_allowed",
			wantSentinel: ErrAnchorNotLocated,
		},
		{
			name:         "引用在原文中无匹配",
			items:        result(item("7", "不存在的引用", "理由", 0, 5)),
			wantReason:   "anchor_not_located",
			wantSentinel: ErrAnchorNotLocated,
		},
		{
			name:         "完全重复条目",
			items:        result(item("7", "原文内容", "理由", 0, 4), item("7", "原文内容", "理由", 0, 4)),
			wantReason:   "duplicate_item",
			wantSentinel: ErrInvalidRequest,
		},
		{
			name: "超过批次上限",
			items: result(
				item("7", "原文内容", "理由一", 0, 4), item("7", "原文内容", "理由二", 0, 4),
				item("7", "原文内容", "理由三", 0, 4), item("7", "原文内容", "理由四", 0, 4),
			),
			wantReason:   "too_many_items",
			wantSentinel: ErrInvalidRequest,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := toAnnotationItems(tc.items, []AnnotationSource{source})
			if err == nil {
				t.Fatal("应拒绝整份结果")
			}
			if !errors.Is(err, tc.wantSentinel) {
				t.Fatalf("应保留原判定语义 %v: %v", tc.wantSentinel, err)
			}
			if got := annotationReasonOf(err); got != tc.wantReason {
				t.Fatalf("原因码 = %q，期望 %q", got, tc.wantReason)
			}
		})
	}

	// 同一消息、同一原句、不同坐标：即使反馈相同也必须保存两条。
	twice := result(item("7", "原文内容", "理由", 0, 4), item("7", "原文内容", "理由", 2, 6))
	if len(twice.Items) != 2 {
		t.Fatal("夹具应有两条")
	}
	// 坐标 2..6 处的原文是 "内容应当"，需要按实际切片修正引文，否则整批拒绝。
	twice.Items[1].Anchor.Quote = "内容应当"
	out, err := toAnnotationItems(twice, []AnnotationSource{source})
	if err != nil || len(out) != 2 {
		t.Fatalf("同文不同位置应各自保存: %+v %v", out, err)
	}
	if *out[0].Anchor.StartCP == *out[1].Anchor.StartCP {
		t.Fatalf("两条批注应保留各自坐标: %+v %+v", out[0].Anchor, out[1].Anchor)
	}

	out, err = toAnnotationItems(result(item("7", "原文内容", "理由", 0, 4)), []AnnotationSource{source})
	if err != nil || len(out) != 1 || out[0].Anchor.MessageID != "7" {
		t.Fatalf("合法条目应通过: %+v %v", out, err)
	}
}

// syntheticAnnotationGenerator 用固定条目替换模型输出，仍复用 Fake 的其余能力。
type syntheticAnnotationGenerator struct {
	*ai.Fake
	items func(req ai.AnnotationRequest) []ai.AnnotationItem
}

func (g syntheticAnnotationGenerator) GenerateAnnotations(_ context.Context, req ai.AnnotationRequest) (ai.AnnotationResult, error) {
	return ai.AnnotationResult{Items: g.items(req), ContentJSON: `{"items":[]}`}, nil
}

// TestAnnotationValidationFailureLogsFixedReason 证明业务校验失败会落一条
// 只含固定原因码与业务错误码的日志，且不泄露模型引用、理由或原文。
func TestAnnotationValidationFailureLogsFixedReason(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[0].ID
	f.store.annotations[turnID] = TurnAnnotation{
		ID: 901, DebateID: f.debate.ID, TurnID: turnID, Status: AnnotationFailed, AttemptVersion: 1,
		ErrorCode: strPtr("model_error"),
	}

	var logged strings.Builder
	generator := syntheticAnnotationGenerator{Fake: f.fake, items: func(ai.AnnotationRequest) []ai.AnnotationItem {
		return []ai.AnnotationItem{{
			Category: AnnotationStrength, Label: "论证有效", Reason: "模型给出的理由不得入日志", Improvement: "模型给出的改进",
			Anchor: ai.AnnotationAnchor{MessageID: "999999", Quote: "模型引用原文不得入日志"},
		}}
	}}
	service := NewService(f.store, generator, NewGate(nil), WithLogger(newDebateLogger(&logged)))
	service.EnableAnnotations(true)
	t.Cleanup(func() { service.waitBackground() })

	if _, _, err := service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 1}); err == nil {
		t.Fatal("跨消息引用应整份拒绝")
	}
	entry := lastDebateLog(t, logged.String())
	if entry["reason_code"] != "message_id_not_allowed" {
		t.Fatalf("reason_code = %v，期望 message_id_not_allowed", entry["reason_code"])
	}
	if entry["error_code"] != "model_error" {
		t.Fatalf("error_code = %v，期望保留 model_error", entry["error_code"])
	}
	if _, ok := entry["turn_id"]; !ok {
		t.Fatalf("日志应带服务端轮次 ID: %+v", entry)
	}
	for _, forbidden := range []string{"模型给出的理由不得入日志", "模型引用原文不得入日志", "999999"} {
		if strings.Contains(logged.String(), forbidden) {
			t.Fatalf("日志泄露了内容 %q：%s", forbidden, logged.String())
		}
	}
	// 原契约：整份失败仍按 model_error 落库，且不返回任何条目。
	list, err := f.service.GetAnnotations(context.Background(), 1, f.debate.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, batch := range list.Items {
		if batch.TurnID != formatID(turnID) {
			continue
		}
		if batch.Status != AnnotationFailed || batch.ErrorCode == nil || *batch.ErrorCode != "model_error" || len(batch.Items) != 0 {
			t.Fatalf("失败批次应保持原契约: %+v", batch)
		}
	}
}

// TestAnnotationGenerationFailureLogsReason 证明模型调用失败时原因码固定为
// generation_failed，error_code 保留原分类，且不记录模型错误原文。
func TestAnnotationGenerationFailureLogsReason(t *testing.T) {
	f := newAnnotationFixture(t)
	turnID := f.turns[0].ID
	f.store.annotations[turnID] = TurnAnnotation{
		ID: 902, DebateID: f.debate.ID, TurnID: turnID, Status: AnnotationFailed, AttemptVersion: 1,
		ErrorCode: strPtr("model_error"),
	}
	const secretOutput = "供应商返回的绝密模型输出"
	f.fake.FailAnnotations(&ai.Error{Code: ai.CodeModelError, Message: secretOutput}, 1)

	var logged strings.Builder
	service := NewService(f.store, f.fake, NewGate(nil), WithLogger(newDebateLogger(&logged)))
	service.EnableAnnotations(true)
	t.Cleanup(func() { service.waitBackground() })

	if _, _, err := service.RetryAnnotation(context.Background(), 1, f.debate.ID, turnID, AnnotationRetryInput{ExpectedAttemptVersion: 1}); err == nil {
		t.Fatal("模型失败应返回错误")
	}
	entry := lastDebateLog(t, logged.String())
	if entry["reason_code"] != "generation_failed" {
		t.Fatalf("reason_code = %v，期望 generation_failed", entry["reason_code"])
	}
	if entry["error_code"] != "model_error" {
		t.Fatalf("error_code = %v，期望 model_error", entry["error_code"])
	}
	if strings.Contains(logged.String(), secretOutput) {
		t.Fatalf("日志泄露了模型错误原文：%s", logged.String())
	}
}

// TestAnnotationSuccessLogsOKReason 证明成功路径同样记录固定原因码，便于按比例统计。
func TestAnnotationSuccessLogsOKReason(t *testing.T) {
	clock := time.Date(2026, 11, 18, 10, 0, 0, 0, time.UTC)
	store := newMemoryStore(func() time.Time { return clock })
	fake := ai.NewFake(ai.FakeStep{Result: ai.Result{Content: "AI 回复"}})
	var logged strings.Builder
	service := NewService(store, fake, NewGate(func() time.Time { return clock }), WithLogger(newDebateLogger(&logged)))
	service.EnableAnnotations(true)
	t.Cleanup(func() { service.waitBackground() })

	created, err := service.Create(context.Background(), 1, CreateInput{TopicID: topicID(1), UserStance: "pro", AIStyle: "logical"})
	if err != nil {
		t.Fatal(err)
	}
	if _, status, err := service.Submit(context.Background(), 1, created.ID, SubmitInput{
		RoundNo: 1, Content: "第1轮用户发言：标准应当可检验。", ClientRequestID: "550e8400-e29b-41d4-a716-446655440001",
	}); err != nil || status != 201 {
		t.Fatalf("提交应成功: %d %v", status, err)
	}
	service.waitBackground()

	entry := lastDebateLog(t, logged.String())
	if entry["reason_code"] != "ok" || entry["error_code"] != "ok" {
		t.Fatalf("成功路径日志应为 ok: %+v", entry)
	}
	list, err := service.GetAnnotations(context.Background(), 1, created.ID)
	if err != nil || len(list.Items) != 1 || list.Items[0].Status != AnnotationCompleted {
		t.Fatalf("成功路径应保存完成批次: %+v %v", list.Items, err)
	}
}
