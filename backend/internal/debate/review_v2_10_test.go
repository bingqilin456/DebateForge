package debate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// V2-10 / V2-11 服务端联动测试：生成结果必须用本场权威原文复核引用，
// 无法核对的引用不得赋档；观察不足是合法结果；四维数据定型后才供雷达消费。
// 旧总结（V1 schema_version=1 的三栏）读取行为保持不变。

// scriptedReview 用给定维度与条目构造 Fake 复盘回复。
func scriptedReview(t *testing.T, dimensions, issues, strengths, improvements string) string {
	t.Helper()
	return `{"dimensions":[` + dimensions + `],"key_issues":[` + issues + `],"strengths":[` + strengths + `],"improvements":[` + improvements + `]}`
}

func reviewTextItem(text, quote string) string {
	if quote == "" {
		return `{"text":"` + text + `"}`
	}
	return `{"text":"` + text + `","quote":"` + quote + `"}`
}

func reviewDimensionItem(key, level, quote string) string {
	body := `"key":"` + key + `","level":` + level + `,"basis":"行为依据"`
	if level == "null" {
		body += `,"insufficient_reason":"本场缺少可观察机会"`
	}
	return `{` + body + `,"improvement":"改进提示","quote":"` + quote + `"}`
}

func reviewAllDimensions(first, second, third, fourth string) string {
	return strings.Join([]string{first, second, third, fourth}, ",")
}

func TestReviewLocatesQuotesFromAuthoritativeTranscript(t *testing.T) {
	mem := newMemoryStore(nil)
	fake := ai.NewFake()
	service := NewService(mem, fake, NewGate(nil))
	service.EnableReviews(true)
	created := completeFourRoundDebate(t, mem, service)
	turns := mem.turns[created.ID]
	userQuote := turns[0].Messages[0].Content
	aiQuote := turns[0].Messages[1].Content

	fake.ScriptReviews(scriptedReview(t,
		reviewAllDimensions(
			reviewDimensionItem(ai.ReviewDimensionArgumentStructure, "3", userQuote),
			reviewDimensionItem(ai.ReviewDimensionResponsiveness, "null", ""),
			reviewDimensionItem(ai.ReviewDimensionEvidenceUse, "2", aiQuote),
			reviewDimensionItem(ai.ReviewDimensionConceptScope, "null", ""),
		),
		reviewTextItem("主要争点", userQuote),
		reviewTextItem("优点", userQuote),
		reviewTextItem("改进", userQuote),
	))
	status, code, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{})
	if err != nil || code != 201 || status.Content == nil {
		t.Fatalf("生成失败: code=%d err=%v", code, err)
	}
	dimensions := status.Content.Dimensions
	if dimensions[0].Quote == nil || !dimensions[0].Quote.Resolved {
		t.Fatalf("唯一匹配的引用必须定位成功: %+v", dimensions[0].Quote)
	}
	if dimensions[0].Quote.MessageID != formatID(turns[0].Messages[0].ID) {
		t.Fatalf("引用必须挂在权威消息上: %+v", dimensions[0].Quote)
	}
	if dimensions[2].Quote == nil || dimensions[2].Quote.MessageID != formatID(turns[0].Messages[1].ID) {
		t.Fatalf("AI 原文引用同样要可定位: %+v", dimensions[2].Quote)
	}
	// 入库后再读一次，确认保存的就是服务端复核后的坐标。
	stored, err := service.GetReview(context.Background(), 1, created.ID)
	if err != nil || stored.Content == nil {
		t.Fatalf("读取失败: %+v err=%v", stored, err)
	}
	if stored.Content.Dimensions[0].Quote == nil || stored.Content.Dimensions[0].Quote.StartCP == nil {
		t.Fatal("落库必须保留已定位坐标")
	}
}

func TestReviewRejectsForeignOrInventedQuotes(t *testing.T) {
	mem := newMemoryStore(nil)
	fake := ai.NewFake()
	service := NewService(mem, fake, NewGate(nil))
	service.EnableReviews(true)
	created := completeFourRoundDebate(t, mem, service)

	// 外场引用与模型改写都不在本场原文里：有档级必须降级为观察不足，不得赋档。
	fake.ScriptReviews(scriptedReview(t,
		reviewAllDimensions(
			reviewDimensionItem(ai.ReviewDimensionArgumentStructure, "4", "这场比赛从未出现过的一句话"),
			reviewDimensionItem(ai.ReviewDimensionResponsiveness, "3", "对方强调公平性但忽略了实施成本"),
			reviewDimensionItem(ai.ReviewDimensionEvidenceUse, "2", "另一段并不存在的原文"),
			reviewDimensionItem(ai.ReviewDimensionConceptScope, "null", ""),
		),
		reviewTextItem("主要争点", "外场引用"),
		reviewTextItem("优点", "外场引用"),
		reviewTextItem("改进", "外场引用"),
	))
	status, _, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{})
	if err != nil || status.Content == nil {
		t.Fatalf("结构合法但引用无效仍应保存观察不足结果: err=%v", err)
	}
	for _, dimension := range status.Content.Dimensions {
		if dimension.Level != nil {
			t.Fatalf("引用无法核对的维度不得赋档: %+v", dimension)
		}
		if dimension.InsufficientReason == nil || *dimension.InsufficientReason == "" {
			t.Fatalf("降级后的观察不足必须说明原因: %+v", dimension)
		}
		if dimension.Quote != nil || dimension.LevelLabel != nil {
			t.Fatalf("观察不足不得携带档位标签或坐标: %+v", dimension)
		}
	}
	// 争点/优点/改进文字仍可读，但不可核对的引用不挂入口。
	for _, item := range status.Content.KeyIssues {
		if item.Quote != nil {
			t.Fatalf("外场引用不得落成可点击定位: %+v", item)
		}
	}
}

func TestReviewAmbiguousQuoteStaysUnresolved(t *testing.T) {
	mem := newMemoryStore(nil)
	fake := ai.NewFake()
	service := NewService(mem, fake, NewGate(nil))
	service.EnableReviews(true)
	created := completeFourRoundDebate(t, mem, service)
	// 让同一句话出现在两条正式消息里，制造跨消息歧义。
	turns := mem.turns[created.ID]
	shared := "统一标准可以公开透明"
	turns[0].Messages[0].Content = shared
	turns[2].Messages[0].Content = shared
	mem.turns[created.ID] = turns

	fake.ScriptReviews(scriptedReview(t,
		reviewAllDimensions(
			reviewDimensionItem(ai.ReviewDimensionArgumentStructure, "3", shared),
			reviewDimensionItem(ai.ReviewDimensionResponsiveness, "null", ""),
			reviewDimensionItem(ai.ReviewDimensionEvidenceUse, "null", ""),
			reviewDimensionItem(ai.ReviewDimensionConceptScope, "null", ""),
		),
		reviewTextItem("主要争点", shared),
		reviewTextItem("优点", ""),
		reviewTextItem("改进", ""),
	))
	status, _, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{})
	if err != nil || status.Content == nil {
		t.Fatalf("歧义引用不应导致整份失败: err=%v", err)
	}
	quote := status.Content.Dimensions[0].Quote
	if quote == nil || quote.Resolved || quote.StartCP != nil {
		t.Fatalf("跨消息歧义只能标记待核对，不按坐标高亮: %+v", quote)
	}
	if status.Content.Dimensions[0].Level == nil {
		t.Fatal("引用确实存在（只是歧义）时应保留档级")
	}
}

func TestReviewPartialAndTotalInsufficientDimensions(t *testing.T) {
	for _, total := range []bool{false, true} {
		mem := newMemoryStore(nil)
		fake := ai.NewFake()
		service := NewService(mem, fake, NewGate(nil))
		service.EnableReviews(true)
		created := completeFourRoundDebate(t, mem, service)
		turns := mem.turns[created.ID]
		quote := turns[0].Messages[0].Content

		dimensions := reviewAllDimensions(
			reviewDimensionItem(ai.ReviewDimensionArgumentStructure, "null", ""),
			reviewDimensionItem(ai.ReviewDimensionResponsiveness, "null", ""),
			reviewDimensionItem(ai.ReviewDimensionEvidenceUse, "null", ""),
			reviewDimensionItem(ai.ReviewDimensionConceptScope, "null", ""),
		)
		if !total {
			dimensions = reviewAllDimensions(
				reviewDimensionItem(ai.ReviewDimensionArgumentStructure, "3", quote),
				reviewDimensionItem(ai.ReviewDimensionResponsiveness, "null", ""),
				reviewDimensionItem(ai.ReviewDimensionEvidenceUse, "null", ""),
				reviewDimensionItem(ai.ReviewDimensionConceptScope, "null", ""),
			)
		}
		fake.ScriptReviews(scriptedReview(t, dimensions,
			reviewTextItem("主要争点", quote), reviewTextItem("优点", ""), reviewTextItem("改进", "")))
		status, code, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{})
		if err != nil || code != 201 || status.Content == nil {
			t.Fatalf("total=%v 观察不足也是合法结果: code=%d err=%v", total, code, err)
		}
		graded := 0
		for _, dimension := range status.Content.Dimensions {
			if dimension.Level == nil {
				if dimension.InsufficientReason == nil || *dimension.InsufficientReason == "" {
					t.Fatalf("total=%v 观察不足必须说明原因", total)
				}
				continue
			}
			graded++
			if *dimension.Level < 1 || *dimension.Level > 4 {
				t.Fatalf("total=%v 档级必须 1–4: %d", total, *dimension.Level)
			}
			if dimension.Quote == nil {
				t.Fatalf("total=%v 有档级必须有原文依据", total)
			}
		}
		if total && graded != 0 {
			t.Fatalf("total=true 不应有任何档级，实际 %d", graded)
		}
		if !total && graded != 1 {
			t.Fatalf("total=false 应只有一个档级，实际 %d", graded)
		}
	}
}

func TestReviewDoesNotChangeLegacySummarySemantics(t *testing.T) {
	mem := newMemoryStore(nil)
	service := NewService(mem, ai.NewFake(), NewGate(nil))
	service.EnableReviews(true)
	created := completeFourRoundDebate(t, mem, service)

	// V1 总结（schema_version=1，三栏）保持原样：复盘读取与生成都不改写它。
	v1JSON := `{"strengths":[{"round_no":1,"point":"优点","reason":"依据"}],"issues":[{"round_no":2,"point":"问题","reason":"依据"}],"suggestions":[{"round_no":3,"action":"行动","example":"示例"}]}`
	schema := 1
	summary := mem.summaries[created.ID]
	summary.Status = "completed"
	summary.SchemaVersion = &schema
	summary.ContentJSON = &v1JSON
	mem.summaries[created.ID] = summary

	if _, code, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{}); err != nil || code != 201 {
		t.Fatalf("复盘生成失败: %v", err)
	}
	legacy, err := service.GetSummary(context.Background(), 1, created.ID)
	if err != nil || legacy.SchemaVersion == nil || *legacy.SchemaVersion != 1 {
		t.Fatalf("旧总结版本号必须保持 1: %+v err=%v", legacy, err)
	}
	if legacy.Content == nil || legacy.Content.Strengths[0].Point != "优点" {
		t.Fatalf("旧总结三栏内容必须原样可读: %+v", legacy.Content)
	}
	// 复盘自身的 Schema 版本独立，不与总结的 1/2 混用。
	review, err := service.GetReview(context.Background(), 1, created.ID)
	if err != nil || review.SchemaVersion == nil || *review.SchemaVersion != ai.ReviewSchemaVersion {
		t.Fatalf("复盘 Schema 版本必须独立: %+v err=%v", review, err)
	}
}

func TestReviewRejectsIncompleteDebate(t *testing.T) {
	mem := newMemoryStore(nil)
	service := NewService(mem, ai.NewFake(), NewGate(nil))
	service.EnableReviews(true)
	created, err := service.Create(context.Background(), 1, CreateInput{TopicID: topicID(1), UserStance: "pro", AIStyle: "logical"})
	if err != nil {
		t.Fatal(err)
	}
	for round := 1; round <= 3; round++ {
		in := SubmitInput{RoundNo: round, Content: "第" + string(rune('0'+round)) + "轮发言", ClientRequestID: uuidForRound(30 + round)}
		if _, _, err := service.Submit(context.Background(), 1, created.ID, in); err != nil {
			t.Fatalf("第 %d 轮失败: %v", round, err)
		}
	}
	// 未完成结辩：不生成完整复盘，也不写任何复盘行。
	if _, _, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{}); !errors.Is(err, ErrRoundConflict) {
		t.Fatalf("未完成会话应返回轮次冲突，实际 %v", err)
	}
	if review, err := service.GetReview(context.Background(), 1, created.ID); err != nil || review.Status != reviewStatusNotStarted {
		t.Fatalf("未完成会话不得产生复盘行: %+v err=%v", review, err)
	}
}

func TestReviewDisabledSliceRejectsGeneration(t *testing.T) {
	mem := newMemoryStore(nil)
	fake := ai.NewFake()
	// 未显式启用：读路径仍可查询，但生成入口拒绝且不调用模型。
	service := NewService(mem, fake, NewGate(nil))
	created := completeFourRoundDebate(t, mem, service)
	if _, _, err := service.RequestReview(context.Background(), 1, created.ID, ReviewInput{}); !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("未启用时应拒绝生成，实际 %v", err)
	}
	if fake.ReviewCalls() != 0 {
		t.Fatal("未启用时不得调用模型")
	}
	if status, err := service.GetReview(context.Background(), 1, created.ID); err != nil || status.Status != reviewStatusNotStarted {
		t.Fatalf("未启用时读取应为未开始: %+v err=%v", status, err)
	}
}
