package ai

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// V2-08 局部重练的 AI 契约测试：
//   - Prompt 把用户原文只放进 user 数据段，system 段不出现用户可控文本；
//   - 结构校验拒绝未知/缺失/重复字段、非布尔 improved、空白 reason 与超长文本；
//   - 生成器失败与 Fake 注入失败都能被上层识别为受控错误。

func repracticeRequest() RepracticeRequest {
	return RepracticeRequest{
		Topic:      Topic{Title: "大学课程是否应采用开放式考核", Description: "讨论学习效果与评价公平"},
		UserStance: StancePro,
		Goal:       "补一句判断依据",
		Quote:      "评价标准应当可检验",
		Rewrite:    "评价标准应当可检验，否则无法比较学习效果。",
	}
}

func TestBuildRepracticePromptKeepsUserTextInDataSegment(t *testing.T) {
	prompt, err := BuildRepracticePrompt(repracticeRequest())
	if err != nil {
		t.Fatal(err)
	}
	if prompt.Version != RepracticePromptVersion {
		t.Fatalf("Prompt 版本不符：%s", prompt.Version)
	}
	if len(prompt.Messages) != 3 || prompt.Messages[0].Role != RoleSystem {
		t.Fatalf("Prompt 结构应为 system + 两段 user：%+v", prompt.Messages)
	}
	// system 段不得包含任何用户可控文本，避免借原文改写规则。
	system := prompt.Messages[0].Content
	for _, injected := range []string{"评价标准应当可检验", "补一句判断依据", "开放式考核"} {
		if strings.Contains(system, injected) {
			t.Fatalf("system 段混入用户文本 %q：%s", injected, system)
		}
	}
	// 原句、改写与目标只出现在 user 数据段。
	data := prompt.Messages[1].Content
	for _, want := range []string{"评价标准应当可检验", "评价标准应当可检验，否则无法比较学习效果。", "补一句判断依据"} {
		if !strings.Contains(data, want) {
			t.Fatalf("数据段缺少 %q：%s", want, data)
		}
	}
	if !strings.Contains(data, "不可信") {
		t.Fatalf("数据段必须声明不可信：%s", data)
	}
}

func TestValidateRepracticeRequestRejectsInvalidInput(t *testing.T) {
	cases := map[string]func(*RepracticeRequest){
		"空原句":     func(r *RepracticeRequest) { r.Quote = "" },
		"空改写":     func(r *RepracticeRequest) { r.Rewrite = "" },
		"超长改写":    func(r *RepracticeRequest) { r.Rewrite = strings.Repeat("😀", MaxUserContentCodepoints+1) },
		"超长目标":    func(r *RepracticeRequest) { r.Goal = strings.Repeat("改", MaxUserContentCodepoints+1) },
		"非法立场":    func(r *RepracticeRequest) { r.UserStance = Stance("neutral") },
		"非法 UTF8": func(r *RepracticeRequest) { r.Rewrite = string([]byte{0xff, 0xfe}) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			req := repracticeRequest()
			mutate(&req)
			if _, err := BuildRepracticePrompt(req); err == nil {
				t.Fatal("非法输入必须被拒绝")
			}
		})
	}
}

func TestParseRepracticeDraftStrictJSON(t *testing.T) {
	valid := `{"improved":true,"change":"补了依据","reason":"理由与主张衔接","new_problem":""}`
	draft, err := ParseRepracticeDraft(valid)
	if err != nil || !draft.Improved || draft.Reason == "" {
		t.Fatalf("合法反馈应解析成功：%+v err=%v", draft, err)
	}

	for name, raw := range map[string]string{
		"未知字段": `{"improved":true,"change":"","reason":"理由","new_problem":"","extra":1}`,
		"缺少字段": `{"improved":true,"change":"","reason":"理由"}`,
		"重复字段": `{"improved":true,"improved":false,"change":"","reason":"理由","new_problem":""}`,
		"非布尔":  `{"improved":"yes","change":"","reason":"理由","new_problem":""}`,
		"空理由":  `{"improved":true,"change":"","reason":"   ","new_problem":""}`,
		"理由超长": `{"improved":true,"change":"","reason":"` + strings.Repeat("理", RepracticeTextMaxCodepoints+1) + `","new_problem":""}`,
		"非对象":  `[]`,
		"空白正文": `   `,
		"尾部多余": `{"improved":true,"change":"","reason":"理由","new_problem":""}{}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseRepracticeDraft(raw); err == nil {
				t.Fatal("非法反馈必须被拒绝")
			}
		})
	}
}

func TestFakeRepracticeGeneratorScripts(t *testing.T) {
	fake := NewFake()
	req := repracticeRequest()
	first, err := fake.GenerateRepractice(context.Background(), req)
	if err != nil || first.Draft.Reason == "" {
		t.Fatalf("默认 Fake 应返回合法反馈：%+v err=%v", first, err)
	}
	if fake.RepracticeCalls() != 1 {
		t.Fatalf("Fake 应记录一次调用，实际 %d", fake.RepracticeCalls())
	}

	// 脚本注入非法 JSON：必须返回受控错误，而不是把坏数据当成功。
	fake.ScriptRepractices(`{"improved":true}`)
	if _, err := fake.GenerateRepractice(context.Background(), req); err == nil {
		t.Fatal("非法脚本回复必须报错")
	}

	// 注射失败：按次数生效，耗尽后恢复正常。
	injected := errors.New("synthetic repractice failure")
	fake.FailRepractices(&Error{Code: CodeModelUnavailable, Message: "合成不可用"}, 1)
	if _, err := fake.GenerateRepractice(context.Background(), req); err == nil {
		t.Fatal("注入失败应返回错误")
	}
	if _, err := fake.GenerateRepractice(context.Background(), req); err != nil {
		t.Fatalf("失败次数耗尽后应恢复：%v", err)
	}
	_ = injected
}

func TestFakeRepracticeRespectsCancelledContext(t *testing.T) {
	fake := NewFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fake.GenerateRepractice(ctx, repracticeRequest()); err == nil {
		t.Fatal("已取消的上下文必须返回错误")
	}
}
