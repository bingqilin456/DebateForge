package ai

import (
	"context"
	"strings"
	"testing"
)

// V2-10 四维契约测试：先于实现锁定“1–4 或观察不足、无依据不赋档、
// 与旧总结（schema_version=1/2）彻底分离”的边界。

func reviewFixtureRequest() ReviewRequest {
	messages := []ReviewMessage{
		{MessageID: 11, TurnNo: 1, Role: RoleUser, Content: "我方认为开放式考核更能检验真实能力"},
		{MessageID: 12, TurnNo: 1, Role: RoleAssistant, Content: "对方强调公平性"},
		{MessageID: 13, TurnNo: 2, Role: RoleUser, Content: "统一标准同样可以公开透明"},
		{MessageID: 14, TurnNo: 2, Role: RoleAssistant, Content: "但成本更高"},
		{MessageID: 15, TurnNo: 3, Role: RoleUser, Content: "评价标准应当可检验并可复核"},
		{MessageID: 16, TurnNo: 3, Role: RoleAssistant, Content: "如何保证复核一致性"},
		{MessageID: 17, TurnNo: 4, Role: RoleUser, Content: "综上收益大于成本"},
		{MessageID: 18, TurnNo: 4, Role: RoleAssistant, Content: "仍有争议"},
	}
	return ReviewRequest{
		UserID: 1, Mode: ModeStandard4, Topic: Topic{Title: "题目", Description: "说明"},
		UserStance: StancePro, Messages: messages,
	}
}

// reviewJSON 用给定维度与条目构造模型回复形状。
func reviewJSON(dimensions, keyIssues, strengths, improvements string) string {
	return `{"dimensions":[` + dimensions + `],"key_issues":[` + keyIssues + `],"strengths":[` + strengths + `],"improvements":[` + improvements + `]}`
}

func dimensionJSON(key string, level string, quote string) string {
	body := `"key":"` + key + `","level":` + level + `,"basis":"行为依据"`
	if level == "null" {
		body += `,"insufficient_reason":"本场缺少可观察机会"`
	}
	if quote != "" {
		body += `,"quote":"` + quote + `","improvement":"改进提示"`
	} else {
		body += `,"improvement":"改进提示"`
	}
	return "{" + body + "}"
}

func fullDimensions(first, second, third, fourth string) string {
	return strings.Join([]string{first, second, third, fourth}, ",")
}

func reviewTexts(text string) string {
	return `{"text":"` + text + `","quote":"我方认为开放式考核更能检验真实能力"}`
}

func TestReviewFourDimensionsAreFixedAndOrdered(t *testing.T) {
	dims := fullDimensions(
		dimensionJSON(ReviewDimensionArgumentStructure, "3", "我方认为开放式考核更能检验真实能力"),
		dimensionJSON(ReviewDimensionResponsiveness, "null", ""),
		dimensionJSON(ReviewDimensionEvidenceUse, "2", "但成本更高"),
		dimensionJSON(ReviewDimensionConceptScope, "null", ""),
	)
	draft, err := ParseReviewDraft(reviewJSON(dims, reviewTexts("争点"), reviewTexts("优点"), reviewTexts("改进")))
	if err != nil {
		t.Fatalf("合法四维应通过: %v", err)
	}
	if len(draft.Dimensions) != 4 {
		t.Fatalf("必须恰好四维，实际 %d", len(draft.Dimensions))
	}
	// 观察不足是合法结果：level=nil，且不属于 0 分或 1 档。
	if draft.Dimensions[1].Level != nil || draft.Dimensions[1].InsufficientReason == "" {
		t.Fatalf("观察不足必须 level=null 且带原因: %+v", draft.Dimensions[1])
	}
	if draft.Dimensions[0].Level == nil || *draft.Dimensions[0].Level != 3 {
		t.Fatalf("档级必须保留 1–4: %+v", draft.Dimensions[0])
	}

	for name, bad := range map[string]string{
		"缺一维": fullDimensions(
			dimensionJSON(ReviewDimensionArgumentStructure, "3", "但成本更高"),
			dimensionJSON(ReviewDimensionResponsiveness, "null", ""),
			dimensionJSON(ReviewDimensionEvidenceUse, "2", "但成本更高"),
			"",
		),
		"顺序错": fullDimensions(
			dimensionJSON(ReviewDimensionResponsiveness, "null", ""),
			dimensionJSON(ReviewDimensionArgumentStructure, "3", "但成本更高"),
			dimensionJSON(ReviewDimensionEvidenceUse, "2", "但成本更高"),
			dimensionJSON(ReviewDimensionConceptScope, "null", ""),
		),
		"维度键重复": fullDimensions(
			dimensionJSON(ReviewDimensionArgumentStructure, "3", "但成本更高"),
			dimensionJSON(ReviewDimensionArgumentStructure, "2", "但成本更高"),
			dimensionJSON(ReviewDimensionEvidenceUse, "2", "但成本更高"),
			dimensionJSON(ReviewDimensionConceptScope, "null", ""),
		),
	} {
		if _, err := ParseReviewDraft(reviewJSON(bad, reviewTexts("争点"), reviewTexts("优点"), reviewTexts("改进"))); err == nil {
			t.Fatalf("%s 必须被拒绝", name)
		}
	}
}

func TestReviewLevelsRejectOutOfRangeAndZero(t *testing.T) {
	// 0、5、-1、1.0、字符串都不得当作档级；0 更不得被解释为“观察不足”。
	for _, level := range []string{"0", "5", "-1", "1.0", `"3"`, "true"} {
		dims := fullDimensions(
			dimensionJSON(ReviewDimensionArgumentStructure, level, "但成本更高"),
			dimensionJSON(ReviewDimensionResponsiveness, "null", ""),
			dimensionJSON(ReviewDimensionEvidenceUse, "2", "但成本更高"),
			dimensionJSON(ReviewDimensionConceptScope, "null", ""),
		)
		if _, err := ParseReviewDraft(reviewJSON(dims, reviewTexts("争点"), reviewTexts("优点"), reviewTexts("改进"))); err == nil {
			t.Fatalf("非法档级 %s 必须被拒绝", level)
		}
	}
}

func TestReviewGradedDimensionRequiresQuote(t *testing.T) {
	// 无依据不得赋档：有档级却没有 quote 必须整份拒绝，而不是默认给低档。
	dims := fullDimensions(
		dimensionJSON(ReviewDimensionArgumentStructure, "3", ""),
		dimensionJSON(ReviewDimensionResponsiveness, "null", ""),
		dimensionJSON(ReviewDimensionEvidenceUse, "2", "但成本更高"),
		dimensionJSON(ReviewDimensionConceptScope, "null", ""),
	)
	if _, err := ParseReviewDraft(reviewJSON(dims, reviewTexts("争点"), reviewTexts("优点"), reviewTexts("改进"))); err == nil {
		t.Fatal("有档级但缺原文依据必须被拒绝")
	}
}

func TestReviewInsufficientDimensionRejectsLevelAndQuote(t *testing.T) {
	// 观察不足不得带档位标签或坐标，也不得用它凑数。
	bad := `{"dimensions":[` + fullDimensions(
		`{"key":"argument_structure","level":null,"basis":"依据","insufficient_reason":"原因","quote":"但成本更高"}`,
		dimensionJSON(ReviewDimensionResponsiveness, "null", ""),
		dimensionJSON(ReviewDimensionEvidenceUse, "2", "但成本更高"),
		dimensionJSON(ReviewDimensionConceptScope, "null", ""),
	) + `],"key_issues":[` + reviewTexts("争点") + `],"strengths":[` + reviewTexts("优点") + `],"improvements":[` + reviewTexts("改进") + `]}`
	if _, err := ParseReviewDraft(bad); err == nil {
		t.Fatal("观察不足维不得携带原文引用")
	}

	missingReason := `{"dimensions":[` + fullDimensions(
		`{"key":"argument_structure","level":null,"basis":"依据"}`,
		dimensionJSON(ReviewDimensionResponsiveness, "null", ""),
		dimensionJSON(ReviewDimensionEvidenceUse, "2", "但成本更高"),
		dimensionJSON(ReviewDimensionConceptScope, "null", ""),
	) + `],"key_issues":[` + reviewTexts("争点") + `],"strengths":[` + reviewTexts("优点") + `],"improvements":[` + reviewTexts("改进") + `]}`
	if _, err := ParseReviewDraft(missingReason); err == nil {
		t.Fatal("观察不足必须说明原因")
	}
}

func TestReviewRejectsUnknownFieldsAndOldSummaryShape(t *testing.T) {
	dims := fullDimensions(
		dimensionJSON(ReviewDimensionArgumentStructure, "3", "我方认为开放式考核更能检验真实能力"),
		dimensionJSON(ReviewDimensionResponsiveness, "null", ""),
		dimensionJSON(ReviewDimensionEvidenceUse, "2", "但成本更高"),
		dimensionJSON(ReviewDimensionConceptScope, "null", ""),
	)
	valid := reviewJSON(dims, reviewTexts("争点"), reviewTexts("优点"), reviewTexts("改进"))
	if _, err := ParseReviewDraft(valid); err != nil {
		t.Fatalf("基线样本应通过: %v", err)
	}
	for name, bad := range map[string]string{
		"未知顶层字段":    strings.Replace(valid, `}`, `,"score":95}`, 1),
		"含 V1 总结字段": `{"strengths":[{"round_no":1,"point":"a","reason":"b"}],"issues":[],"suggestions":[],"dimensions":[]}`,
		"含旧四维 0–100": `{"dimensions":[` + fullDimensions(
			`{"key":"argumentation","level":80,"basis":"x","quote":"但成本更高"}`,
			dimensionJSON(ReviewDimensionResponsiveness, "null", ""),
			dimensionJSON(ReviewDimensionEvidenceUse, "2", "但成本更高"),
			dimensionJSON(ReviewDimensionConceptScope, "null", ""),
		) + `],"key_issues":[` + reviewTexts("争点") + `],"strengths":[` + reviewTexts("优点") + `],"improvements":[` + reviewTexts("改进") + `]}`,
	} {
		if _, err := ParseReviewDraft(bad); err == nil {
			t.Fatalf("%s 必须被拒绝", name)
		}
	}
}

func TestReviewPromptKeepsTranscriptOutOfSystem(t *testing.T) {
	req := reviewFixtureRequest()
	prompt, err := BuildReviewPrompt(req)
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Version != ReviewPromptVersion {
		t.Fatalf("Prompt 版本必须独立: %s", prompt.Version)
	}
	if prompt.Messages[0].Role != RoleSystem {
		t.Fatal("首条必须是 system")
	}
	for _, message := range req.Messages {
		if strings.Contains(prompt.Messages[0].Content, message.Content) {
			t.Fatal("用户原文不得进入 system 规则段")
		}
	}
	if !strings.Contains(prompt.Messages[1].Content, req.Messages[0].Content) {
		t.Fatal("原文必须作为 user 数据段外发")
	}
}

func TestReviewRequestRequiresCompleteFourRounds(t *testing.T) {
	req := reviewFixtureRequest()
	req.Messages = req.Messages[:6]
	if _, err := BuildReviewPrompt(req); err == nil {
		t.Fatal("不足四轮的原文不得生成完整复盘")
	}
	if err := ValidateReviewRequest(req); err == nil {
		t.Fatal("校验入口必须同样拒绝不完整历史")
	}
}

func TestFakeReviewIsDeterministicAndRecorded(t *testing.T) {
	fake := NewFake()
	result, err := fake.GenerateReview(context.Background(), reviewFixtureRequest())
	if err != nil {
		t.Fatalf("Fake 复盘生成失败: %v", err)
	}
	if fake.ReviewCalls() != 1 || len(fake.ReviewRecords()) != 1 {
		t.Fatal("Fake 必须记录复盘调用")
	}
	if result.SchemaVersion != ReviewSchemaVersion {
		t.Fatalf("复盘 Schema 版本必须独立: %d", result.SchemaVersion)
	}
	// Fake 输出必须能通过同一份草稿校验；且引用必须来自本场原文。
	draft, err := ParseReviewDraft(result.ContentJSON)
	if err != nil {
		t.Fatalf("Fake 输出必须通过草稿校验: %v", err)
	}
	if draft.Dimensions[0].Quote == "" || !strings.Contains(reviewFixtureRequest().Messages[0].Content, draft.Dimensions[0].Quote) {
		t.Fatalf("Fake 引用必须是本场原文片段: %q", draft.Dimensions[0].Quote)
	}

	fake.FailReviews(&Error{Code: CodeModelTimeout}, 1)
	if _, err := fake.GenerateReview(context.Background(), reviewFixtureRequest()); err == nil {
		t.Fatal("脚本化失败必须生效")
	}
	if _, err := fake.GenerateReview(context.Background(), reviewFixtureRequest()); err != nil {
		t.Fatalf("失败只应影响一次: %v", err)
	}
}

func TestParseReviewRejectsLevelOutOfRangeOnRead(t *testing.T) {
	// 已落库内容也必须按同一规则读取；坏数据报错而不静默改成 1 档或 0 分。
	bad := `{"dimensions":[` + strings.Join([]string{
		`{"key":"argument_structure","label":"论证结构","level":0,"level_label":null,"basis":"x","improvement":"y","insufficient_reason":null,"quote":null}`,
		`{"key":"responsiveness","label":"回应针对性","level":null,"level_label":null,"basis":"x","improvement":"","insufficient_reason":"原因","quote":null}`,
		`{"key":"evidence_use","label":"证据使用","level":2,"level_label":"发展中","basis":"x","improvement":"y","insufficient_reason":null,"quote":{"message_id":"11","quote":"原文","start_cp":0,"end_cp":2,"resolved":true}}`,
		`{"key":"concept_scope","label":"概念与范围","level":null,"level_label":null,"basis":"x","improvement":"","insufficient_reason":"原因","quote":null}`,
	}, ",") + `],"key_issues":[{"text":"争点","quote":null}],"strengths":[{"text":"优点","quote":null}],"improvements":[{"text":"改进","quote":null}]}`
	if _, err := ParseReview(bad); err == nil {
		t.Fatal("落库内容档级越界必须读取失败")
	}
}
