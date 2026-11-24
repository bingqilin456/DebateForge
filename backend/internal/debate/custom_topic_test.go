package debate

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 本文件是 V2-03 私有自定义辩题的回归测试：严格二选一、Unicode 码点边界、
// 原文保留、预设分支不变，以及 DTO 的空 ID 语义。全部为纯单元测试，不连接数据库。

func mustCustom(t *testing.T, title, background string) *CustomTopic {
	t.Helper()
	return &CustomTopic{Title: title, Background: background}
}

func TestValidateCreateInputRequiresExactlyOneTopicSource(t *testing.T) {
	preset := topicID(7)
	custom := mustCustom(t, "辩题原文", "")
	cases := []struct {
		name string
		in   CreateInput
		ok   bool
	}{
		{"仅预设", CreateInput{TopicID: preset, UserStance: "pro", AIStyle: "logical"}, true},
		{"仅自定义", CreateInput{CustomTopic: custom, UserStance: "pro", AIStyle: "logical"}, true},
		{"两者同时出现", CreateInput{TopicID: preset, CustomTopic: custom, UserStance: "pro", AIStyle: "logical"}, false},
		{"两者都缺失", CreateInput{UserStance: "pro", AIStyle: "logical"}, false},
		{"预设 ID 为 0", CreateInput{TopicID: topicID(0), UserStance: "pro", AIStyle: "logical"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ValidateCreateInput(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("应接受该输入，实际 %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("应拒绝该输入")
			}
		})
	}
}

func TestValidateCreateInputChecksStanceAndStyle(t *testing.T) {
	for _, in := range []CreateInput{
		{TopicID: topicID(1), UserStance: "", AIStyle: "logical"},
		{TopicID: topicID(1), UserStance: "pro", AIStyle: ""},
		{TopicID: topicID(1), UserStance: "pro", AIStyle: "unknown"},
		{CustomTopic: mustCustom(t, "题", ""), UserStance: "pro", AIStyle: "Logical"},
	} {
		if _, err := ValidateCreateInput(in); err == nil {
			t.Fatalf("应拒绝立场/流派非法的输入: %+v", in)
		}
	}
}

// 标题 1–200 码点，按解码后的 Unicode 码点计数：
// 组合字符计多个码点、补充平面字符计一个码点，两者都不能用 UTF-16 长度口径。
func TestValidateCreateInputCountsTitleByCodepoints(t *testing.T) {
	cases := []struct {
		name  string
		title string
		ok    bool
	}{
		{"单码点", "题", true},
		{"200 个中文", strings.Repeat("题", 200), true},
		{"201 个中文", strings.Repeat("题", 201), false},
		{"200 个补充平面字符", strings.Repeat("𠮷", 200), true},
		{"201 个补充平面字符", strings.Repeat("𠮷", 201), false},
		{"组合字符按码点计数", strings.Repeat("e\u0301", 101), false},
		{"纯空白", " \t\n", false},
		{"全角空格", "　　", false},
		{"BOM 与空白", "\ufeff \ufeff", false},
		{"空白包裹的有效题目", "  有效题目\t", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := CreateInput{CustomTopic: &CustomTopic{Title: tc.title}, UserStance: "pro", AIStyle: "logical"}
			_, err := ValidateCreateInput(in)
			if tc.ok && err != nil {
				t.Fatalf("%d 码点标题应被接受，实际 %v", utf8.RuneCountInString(tc.title), err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("%d 码点标题应被拒绝", utf8.RuneCountInString(tc.title))
			}
		})
	}
}

// 背景 0–1000 码点：可省略、可为空串，长度包含空白，且不要求非空白。
func TestValidateCreateInputBackgroundBounds(t *testing.T) {
	cases := []struct {
		name       string
		background string
		ok         bool
	}{
		{"省略", "", true},
		{"全空白", "   \n\t", true},
		{"1000 个中文", strings.Repeat("说", 1000), true},
		{"1001 个中文", strings.Repeat("说", 1001), false},
		{"1000 个补充平面字符", strings.Repeat("𠮷", 1000), true},
		{"1001 个补充平面字符", strings.Repeat("𠮷", 1001), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := CreateInput{CustomTopic: &CustomTopic{Title: "题", Background: tc.background}, UserStance: "pro", AIStyle: "logical"}
			_, err := ValidateCreateInput(in)
			if tc.ok && err != nil {
				t.Fatal("应在范围内")
			}
			if !tc.ok && err == nil {
				t.Fatal("应超限拒绝")
			}
		})
	}
}

// 校验只判断空白与长度，持久化必须保留原文：首尾空白、换行与原 Unicode 字符都不被改写。
func TestValidateCreateInputPreservesOriginalText(t *testing.T) {
	title := "  是否应保留首尾空白？\n"
	background := " 背景第一行\n背景第二行\ufeff  "
	in := CreateInput{CustomTopic: &CustomTopic{Title: title, Background: background}, UserStance: "con", AIStyle: "data"}
	got, err := ValidateCreateInput(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomTopic.Title != title || got.CustomTopic.Background != background {
		t.Fatalf("原文被改写: %+v", got.CustomTopic)
	}
}

// 非法 UTF-8 不能被当作合法原文写入快照。
func TestValidateCreateInputRejectsInvalidUTF8(t *testing.T) {
	bad := string([]byte{0xff, 0xfe})
	in := CreateInput{CustomTopic: &CustomTopic{Title: bad}, UserStance: "pro", AIStyle: "logical"}
	if _, err := ValidateCreateInput(in); err == nil {
		t.Fatal("非法 UTF-8 标题应被拒绝")
	}
	in = CreateInput{CustomTopic: &CustomTopic{Title: "题", Background: bad}, UserStance: "pro", AIStyle: "logical"}
	if _, err := ValidateCreateInput(in); err == nil {
		t.Fatal("非法 UTF-8 背景应被拒绝")
	}
}

// attachCustomTopic 之外还要断言 DTO 语义：预设题 id 仍为字符串，自定义题为 null。
func TestDebateDTOTopicIDIsNullOnlyForCustom(t *testing.T) {
	preset := Debate{ID: 1, TopicID: topicID(7), TopicTitleSnapshot: "预设", UserStance: "pro", Mode: "standard_4", AIStyle: "logical"}
	dto := preset.DTO()
	if dto.Topic.ID == nil || *dto.Topic.ID != "7" {
		t.Fatalf("预设题应输出字符串 ID，实际 %+v", dto.Topic.ID)
	}
	custom := Debate{ID: 2, TopicTitleSnapshot: "自定义题", UserStance: "pro", Mode: "standard_4", AIStyle: "logical"}
	dto = custom.DTO()
	if dto.Topic.ID != nil {
		t.Fatalf("自定义题的 topic.id 必须是 null，实际 %q", *dto.Topic.ID)
	}
	if dto.Topic.Title != "自定义题" {
		t.Fatal("自定义题仍须输出会话快照中的原文")
	}
	if !custom.Custom() || preset.Custom() {
		t.Fatal("Custom() 必须由 topic_id 是否为空派生")
	}
}
