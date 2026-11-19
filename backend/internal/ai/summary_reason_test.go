package ai

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestGenerateSummaryLogsStableReasonCodes 证明复盘日志的 reason_code 能区分
// 供应商/响应信封、结束状态与输出校验，同时保留既有 error_code 与 usage 日志，
// 且不把原文、模型输出、供应商正文或凭据写进日志。
func TestGenerateSummaryLogsStableReasonCodes(t *testing.T) {
	const supplierSecret = "供应商原始报错不得入日志"
	usage := `{"prompt_tokens":120,"completion_tokens":85,"total_tokens":205}`
	cases := []struct {
		name       string
		status     int
		body       string
		wantReason string
		wantCode   string
	}{
		{name: "成功", status: 200, body: chatCompletionJSON(validSummaryJSON(), "stop", usage), wantReason: "ok", wantCode: "ok"},
		{name: "输出不是JSON", status: 200, body: chatCompletionJSON("这不是JSON总结", "stop", usage), wantReason: "output_invalid", wantCode: "model_error"},
		{name: "输出自带版本字段", status: 200, body: chatCompletionJSON(`{"schema_version":1,"strengths":[],"issues":[],"suggestions":[]}`, "stop", usage), wantReason: "output_invalid", wantCode: "model_error"},
		{name: "输出被截断", status: 200, body: chatCompletionJSON(validSummaryJSON(), "length", usage), wantReason: "response_truncated", wantCode: "model_error"},
		{name: "结束状态缺失", status: 200, body: `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"deepseek-flash",` +
			`"choices":[{"index":0,"message":{"role":"assistant","content":` + jsonString(validSummaryJSON()) + `}}]}`, wantReason: "response_incomplete", wantCode: "model_error"},
		{name: "内容策略拦截", status: 200, body: chatCompletionJSON(validSummaryJSON(), "content_filter", usage), wantReason: "content_filtered", wantCode: "model_refused"},
		{name: "响应信封损坏", status: 200, body: `{"choices": [`, wantReason: "response_invalid", wantCode: "model_error"},
		{name: "供应商返回错误", status: 401, body: `{"error":{"message":"` + supplierSecret + `"}}`, wantReason: "provider_error", wantCode: "provider_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport := newScriptedTransport(func(*http.Request, int) (*http.Response, error) {
				return jsonResponse(tc.status, tc.body), nil
			})
			var logged strings.Builder
			cfg := testConfig(transport)
			cfg.Logger = newBufferLogger(&logged)
			generator, err := NewOpenAIGenerator(context.Background(), cfg)
			if err != nil {
				t.Fatalf("构造适配器失败: %v", err)
			}
			summaryGenerator, ok := generator.(SummaryGenerator)
			if !ok {
				t.Fatal("适配器应实现总结生成接口")
			}

			result, err := summaryGenerator.GenerateSummary(context.Background(), validSummaryRequest())
			entry := lastLogEntry(t, logged.String())
			if got := entry["reason_code"]; got != tc.wantReason {
				t.Fatalf("reason_code = %v，期望 %s", got, tc.wantReason)
			}
			if got := entry["error_code"]; got != tc.wantCode {
				t.Fatalf("error_code = %v，期望 %s", got, tc.wantCode)
			}
			if tc.wantCode == "ok" {
				if err != nil {
					t.Fatalf("成功路径不应报错: %v", err)
				}
				// 版本由服务端设置，模型输出与落库 JSON 都不含版本字段。
				if result.SchemaVersion != SummarySchemaVersion || strings.Contains(result.ContentJSON, "schema_version") {
					t.Fatalf("版本应由服务端设置且不写入输出: %d %s", result.SchemaVersion, result.ContentJSON)
				}
				if entry["prompt_tokens"] != float64(120) || entry["total_tokens"] != float64(205) {
					t.Fatalf("既有 usage 日志应保留: %+v", entry)
				}
				if entry["prompt_version"] != SummaryPromptVersion {
					t.Fatalf("既有 prompt_version 日志应保留: %+v", entry)
				}
			} else if err == nil {
				t.Fatal("失败路径应返回错误")
			}

			loggedText := logged.String()
			for _, forbidden := range []string{supplierSecret, fixedAnonymousMessages()[0].Content, "这不是JSON总结", testAPIKey} {
				if strings.Contains(loggedText, forbidden) {
					t.Fatalf("日志泄露了敏感内容 %q：%s", forbidden, loggedText)
				}
			}
		})
	}
}

// TestSummaryPromptsLeaveSchemaVersionToServer 证明 system 与 trigger 都只声明
// strengths/issues/suggestions 三个顶层字段，并明确版本字段由服务端设置。
func TestSummaryPromptsLeaveSchemaVersionToServer(t *testing.T) {
	for _, custom := range []bool{false, true} {
		req := validSummaryRequest()
		req.Custom = custom
		prompt, err := BuildSummaryPrompt(req)
		if err != nil {
			t.Fatalf("custom=%v 构建提示词失败: %v", custom, err)
		}
		system := prompt.Messages[0].Content
		trigger := prompt.Messages[len(prompt.Messages)-1].Content
		for name, text := range map[string]string{"system": system, "trigger": trigger} {
			if strings.Contains(text, "schema_version") {
				t.Fatalf("custom=%v 的 %s 提示词不应要求模型输出 schema_version:\n%s", custom, name, text)
			}
			for _, field := range []string{"strengths", "issues", "suggestions"} {
				if !strings.Contains(text, field) {
					t.Fatalf("custom=%v 的 %s 提示词应明确只允许 %s 字段", custom, name, field)
				}
			}
		}
		if !strings.Contains(system, "版本") || !strings.Contains(system, "服务端") {
			t.Fatalf("system 提示词应说明版本字段由服务端设置:\n%s", system)
		}
	}
}

// TestParseSummaryStillRejectsModelSuppliedSchemaVersion 固定原有严格契约：
// 即使提示词不再要求版本字段，模型自带版本字段也必须按额外键整份拒绝。
func TestParseSummaryStillRejectsModelSuppliedSchemaVersion(t *testing.T) {
	raw := `{"schema_version":1,` + strings.TrimPrefix(validSummaryJSON(), "{")
	if _, err := ParseSummaryForMode(raw, ModeQuick3); err == nil {
		t.Fatal("严格解析应拒绝模型自带的版本字段")
	}
}
