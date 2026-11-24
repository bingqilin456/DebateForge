package debate

import (
	"encoding/json"
	"testing"
)

// parseCreateRequest 是创建接口的严格二选一入口，本文件覆盖协议层边界：
// 字段出现与否、显式 null、未知/重复字段、类型错误与背景可省略。

func raw(t *testing.T, value string) json.RawMessage {
	t.Helper()
	if !json.Valid([]byte(value)) {
		t.Fatalf("测试夹具不是合法 JSON: %s", value)
	}
	return json.RawMessage(value)
}

func TestParseCreateRequestStrictExclusiveChoice(t *testing.T) {
	cases := []struct {
		name string
		in   createRequest
		ok   bool
	}{
		{"预设", createRequest{TopicID: raw(t, `"12"`), UserStance: "pro"}, true},
		{"自定义", createRequest{CustomTopic: raw(t, `{"title":"辩题原文"}`), UserStance: "con"}, true},
		{"自定义含背景", createRequest{CustomTopic: raw(t, `{"title":"题","background":"背景"}`), UserStance: "pro"}, true},
		{"自定义空背景", createRequest{CustomTopic: raw(t, `{"title":"题","background":""}`), UserStance: "pro"}, true},
		{"两者同时出现", createRequest{TopicID: raw(t, `"12"`), CustomTopic: raw(t, `{"title":"题"}`), UserStance: "pro"}, false},
		{"两者都缺失", createRequest{UserStance: "pro"}, false},
		{"topic_id 显式 null", createRequest{TopicID: raw(t, `null`), UserStance: "pro"}, false},
		{"custom_topic 显式 null", createRequest{CustomTopic: raw(t, `null`), UserStance: "pro"}, false},
		{"topic_id 类型错误", createRequest{TopicID: raw(t, `12`), UserStance: "pro"}, false},
		{"topic_id 非数字串", createRequest{TopicID: raw(t, `"abc"`), UserStance: "pro"}, false},
		{"topic_id 前导零", createRequest{TopicID: raw(t, `"012"`), UserStance: "pro"}, false},
		{"custom_topic 非对象", createRequest{CustomTopic: raw(t, `"题"`), UserStance: "pro"}, false},
		{"custom_topic 缺 title", createRequest{CustomTopic: raw(t, `{"background":"背景"}`), UserStance: "pro"}, false},
		{"custom_topic 未知字段", createRequest{CustomTopic: raw(t, `{"title":"题","topic_id":"1"}`), UserStance: "pro"}, false},
		{"custom_topic 重复字段", createRequest{CustomTopic: raw(t, `{"title":"题","title":"另一题"}`), UserStance: "pro"}, false},
		{"custom_topic 字段为 null", createRequest{CustomTopic: raw(t, `{"title":null}`), UserStance: "pro"}, false},
		{"custom_topic 背景类型错误", createRequest{CustomTopic: raw(t, `{"title":"题","background":1}`), UserStance: "pro"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseCreateRequest(tc.in)
			if tc.ok && err != nil {
				t.Fatalf("应接受该请求，实际 %v", err)
			}
			if !tc.ok {
				if err == nil {
					t.Fatal("应拒绝该请求")
				}
				return
			}
			if got.AIStyle != "logical" {
				t.Fatalf("省略 ai_style 时应默认 logical，实际 %q", got.AIStyle)
			}
			if (got.TopicID == nil) == (got.CustomTopic == nil) {
				t.Fatal("解析结果仍须严格二选一")
			}
		})
	}
}

func TestParseCreateRequestAIStyleIsStrict(t *testing.T) {
	base := createRequest{TopicID: raw(t, `"1"`), UserStance: "pro"}
	for _, value := range []string{`null`, `""`, `"Logical"`, `"unknown"`, `1`, `true`, `[]`} {
		in := base
		in.AIStyle = raw(t, value)
		if _, err := parseCreateRequest(in); err == nil {
			t.Fatalf("ai_style=%s 应被拒绝", value)
		}
	}
	in := base
	in.AIStyle = raw(t, `"aggressive"`)
	got, err := parseCreateRequest(in)
	if err != nil || got.AIStyle != "aggressive" {
		t.Fatalf("合法流派应被接受: %v %q", err, got.AIStyle)
	}
}

// 校验通过后，请求中的题目原文必须逐码点保留。
// 自定义分支严格拒绝顶层未知键，因此赛制、归属、轮次即使出现在请求里也不会被接受。
func TestParseCreateRequestPreservesCustomText(t *testing.T) {
	title := "  带首尾空白与换行\n"
	background := "背景\t含制表符"
	payload := map[string]any{
		"custom_topic": map[string]any{"title": title, "background": background},
		"user_stance":  "con",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var in createRequest
	if err := json.Unmarshal(body, &in); err != nil {
		t.Fatal(err)
	}
	got, err := parseCreateRequest(in)
	if err != nil {
		t.Fatal(err)
	}
	if got.CustomTopic == nil || got.CustomTopic.Title != title || got.CustomTopic.Background != background {
		t.Fatalf("题目原文被改写: %+v", got.CustomTopic)
	}
}

// 顶层未知/重复键：自定义分支严格拒绝，预设分支保留既有忽略行为。
func TestParseCreateRequestTopLevelStrictnessPerBranch(t *testing.T) {
	custom := newCreateRequest(t, `{"custom_topic":{"title":"题"},"user_stance":"pro","mode":"quick_3"}`)
	if _, err := parseCreateRequest(custom); err == nil {
		t.Fatal("自定义分支必须拒绝顶层未知字段 mode")
	}
	custom = newCreateRequest(t, `{"custom_topic":{"title":"题"},"custom_topic":{"title":"另一题"},"user_stance":"pro"}`)
	if _, err := parseCreateRequest(custom); err == nil {
		t.Fatal("自定义分支必须拒绝顶层重复字段 custom_topic")
	}
	custom = newCreateRequest(t, `{"custom_topic":{"title":"题"},"user_stance":"pro","user_stance":"con"}`)
	if _, err := parseCreateRequest(custom); err == nil {
		t.Fatal("自定义分支必须拒绝顶层重复字段 user_stance")
	}
	// 预设分支保持旧行为：未知与重复字段被忽略，最终取最后一次赋值。
	preset := newCreateRequest(t, `{"topic_id":"1","topic_id":"2","user_stance":"pro","mode":"quick_3"}`)
	got, err := parseCreateRequest(preset)
	if err != nil {
		t.Fatalf("预设分支应沿用既有忽略行为: %v", err)
	}
	if got.TopicID == nil || *got.TopicID != 2 {
		t.Fatalf("预设分支应按既有语义取最后一次赋值: %+v", got.TopicID)
	}
}

// newCreateRequest 用与服务端相同的解码路径构造原始请求。
func newCreateRequest(t *testing.T, body string) createRequest {
	t.Helper()
	var in createRequest
	if err := json.Unmarshal([]byte(body), &in); err != nil {
		t.Fatalf("测试夹具无法解码: %v", err)
	}
	return in
}
