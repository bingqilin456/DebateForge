package ai

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	// testAPIKey 是测试用占位值，不是真实凭据；用例断言它只出现在传输层请求头。
	testAPIKey  = "test-key-not-a-real-secret"
	testBaseURL = "https://api.deepseek.com"
	testModel   = "deepseek-flash"
)

func testConfig(transport http.RoundTripper) OpenAIConfig {
	return OpenAIConfig{
		APIKey:          testAPIKey,
		BaseURL:         testBaseURL,
		Model:           testModel,
		MaxOutputTokens: 512,
		Window:          validWindow(),
		HTTPClient:      &http.Client{Transport: transport},
	}
}

func newTestGenerator(t *testing.T, transport *scriptedTransport) Generator {
	t.Helper()
	generator, err := NewOpenAIGenerator(context.Background(), testConfig(transport))
	if err != nil {
		t.Fatalf("构造官方适配器失败: %v", err)
	}
	return generator
}

func decodeBody(t *testing.T, body string) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("请求体不是合法 JSON: %v\n%s", err, body)
	}
	return payload
}

func chatCompletionJSON(content, finishReason, usage string) string {
	usagePart := ""
	if usage != "" {
		usagePart = ",\"usage\":" + usage
	}
	return `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"deepseek-flash",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":` + jsonString(content) + `},` +
		`"finish_reason":` + jsonString(finishReason) + `}]` + usagePart + `}`
}

func jsonString(v string) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// 该用例捕获：适配器没有真正把服务端构建的消息、输出上限与非流式参数发到传输层。
func TestOpenAIGeneratorSendsExpectedJSONThroughAdapter(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON("统一考核仍有助于横向比较。", "stop",
			`{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}`)), nil
	})
	generator := newTestGenerator(t, transport)

	req := validRequest()
	req.RoundNo = 2
	req.CompletedTurns = []CompletedTurn{{RoundNo: 1, UserContent: "第一轮用户发言", AssistantContent: "第一轮 AI 回复"}}
	if _, err := generator.Generate(context.Background(), req); err != nil {
		t.Fatalf("正常响应不应报错: %v", err)
	}
	if transport.attempts() != 1 {
		t.Fatalf("成功调用应恰好发出 1 次请求，实际 %d", transport.attempts())
	}

	record := transport.record(t, 0)
	if record.Method != http.MethodPost {
		t.Fatalf("应使用 POST，实际 %s", record.Method)
	}
	if record.Path != "/chat/completions" {
		t.Fatalf("官方 OpenAI 协议路径应为 /chat/completions，实际 %s", record.Path)
	}
	if got := record.Header.Get("Authorization"); got != "Bearer "+testAPIKey {
		t.Fatalf("Authorization 头应为 Bearer 注入的 Key，实际 %q", got)
	}
	if got := record.Header.Get("Accept"); !strings.Contains(got, "application/json") {
		t.Fatalf("非流式调用应接受 application/json，实际 %q", got)
	}
	if strings.Contains(record.Header.Get("Accept"), "text/event-stream") {
		t.Fatal("本轮不支持流式，不得请求 SSE")
	}

	payload := decodeBody(t, record.Body)
	if got := payload["model"]; got != testModel {
		t.Fatalf("model 应为 %q，实际 %v", testModel, got)
	}
	// 输出上限必须真实出现在传输层，且使用保守的小上限而非供应商默认。
	if got, ok := payload["max_tokens"].(float64); !ok || int(got) != 512 {
		t.Fatalf("max_tokens 必须真实发出 512，实际 %v", payload["max_tokens"])
	}
	if _, ok := payload["max_completion_tokens"]; ok {
		t.Fatalf("不应混用 max_completion_tokens: %v", record.Body)
	}
	if stream, ok := payload["stream"]; ok {
		if value, isBool := stream.(bool); !isBool || value {
			t.Fatalf("非流式调用不得发送 stream=true，实际 %v", stream)
		}
	}
	// DeepSeek 默认开启思考，必须显式关闭，否则 45 秒与输出预算会被思考耗尽。
	thinking, ok := payload["thinking"].(map[string]any)
	if !ok {
		t.Fatalf("必须显式发送 thinking 参数，实际请求体: %s", record.Body)
	}
	if thinking["type"] != "disabled" {
		t.Fatalf(`thinking.type 应为 "disabled"，实际 %v`, thinking["type"])
	}

	messages, ok := payload["messages"].([]any)
	if !ok || len(messages) != 4 {
		t.Fatalf("第二轮应发送 4 条消息（system+1 轮历史 2 条+本轮 1 条），实际 %v", payload["messages"])
	}
	wantRoles := []string{"system", "user", "assistant", "user"}
	for i, want := range wantRoles {
		message, ok := messages[i].(map[string]any)
		if !ok {
			t.Fatalf("第 %d 条消息格式异常: %v", i, messages[i])
		}
		if message["role"] != want {
			t.Fatalf("第 %d 条消息角色应为 %q，实际 %v", i, want, message["role"])
		}
	}
	first := messages[1].(map[string]any)
	if first["content"] != "第一轮用户发言" {
		t.Fatalf("历史用户原文被改写: %v", first["content"])
	}
	last := messages[3].(map[string]any)
	if last["content"] != req.UserContent {
		t.Fatalf("本轮用户原文被改写: %v", last["content"])
	}
	if !strings.Contains(messages[0].(map[string]any)["content"].(string), req.Topic.Title) {
		t.Fatal("system 消息必须携带受信任的辩题快照")
	}
}

// 该用例捕获：把 SDK 的 int 默认 0 当成供应商真实返回的 0，伪装成已知用量。
func TestOpenAIGeneratorReportsMissingUsageAsUnknown(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON("回复", "stop", "")), nil
	})
	result, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("缺少 usage 不应导致调用失败: %v", err)
	}
	if result.Content != "回复" {
		t.Fatalf("回复内容应保留，实际 %q", result.Content)
	}
	usage := result.Usage
	if usage.PromptTokens.Known || usage.CompletionTokens.Known || usage.TotalTokens.Known {
		t.Fatalf("usage 缺失时必须是未知，实际 %+v", usage)
	}
}

func TestOpenAIGeneratorReportsPartialUsagePerField(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON("回复", "stop", `{"prompt_tokens":11}`)), nil
	})
	result, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("部分 usage 不应导致调用失败: %v", err)
	}
	usage := result.Usage
	if !usage.PromptTokens.Known || usage.PromptTokens.Value != 11 {
		t.Fatalf("prompt_tokens 应已知为 11，实际 %+v", usage.PromptTokens)
	}
	if usage.CompletionTokens.Known || usage.TotalTokens.Known {
		t.Fatalf("缺失字段必须保持未知，实际 %+v", usage)
	}
}

func TestOpenAIGeneratorReportsExplicitZeroUsageAsKnown(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON("回复", "stop",
			`{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`)), nil
	})
	result, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("显式零 usage 不应导致调用失败: %v", err)
	}
	usage := result.Usage
	if !usage.PromptTokens.Known || usage.PromptTokens.Value != 0 {
		t.Fatalf("显式零必须记为已知 0，实际 %+v", usage.PromptTokens)
	}
	if !usage.CompletionTokens.Known || !usage.TotalTokens.Known {
		t.Fatalf("显式零字段必须均为已知，实际 %+v", usage)
	}
}

// 该用例捕获：官方适配器丢掉 message.refusal，导致拒答被当成正常回复。
func TestOpenAIGeneratorRefusalIsControlledFailure(t *testing.T) {
	body := `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"deepseek-flash",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"","refusal":"我不能协助这个请求"},` +
		`"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":9,"completion_tokens":2,"total_tokens":11}}`
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, body), nil
	})
	result, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	if err == nil {
		t.Fatalf("拒答必须转为受控失败，实际返回成功: %+v", result)
	}
	assertCode(t, err, CodeModelRefused)
	var controlled *Error
	if !errors.As(err, &controlled) {
		t.Fatalf("应为受控错误，实际 %T", err)
	}
	// Eino 输出丢失 refusal，必须由本地封装从原始 JSON 保留“确实拒答”这一受控事实；
	// 但元数据不得保存供应商拒答原文。
	if !controlled.Metadata.Refused {
		t.Fatal("拒答事实必须在受控元数据中保留，不能依赖 Eino 输出")
	}
	if !controlled.Usage.TotalTokens.Known || controlled.Usage.TotalTokens.Value != 11 {
		t.Fatalf("失败仍须保留可用计费元数据，实际 %+v", controlled.Usage)
	}
	if result.Content != "" {
		t.Fatalf("拒答不得返回可用回复，实际 %q", result.Content)
	}
}

func TestOpenAIGeneratorFinishReasonClassification(t *testing.T) {
	cases := []struct {
		name         string
		finishReason string
		content      string
		wantCode     ErrorCode
	}{
		{name: "正常结束", finishReason: "stop", content: "正常回复"},
		{name: "长度截断", finishReason: "length", content: "被截断的内容", wantCode: CodeModelError},
		{name: "内容过滤", finishReason: "content_filter", content: "", wantCode: CodeModelRefused},
		{name: "未知原因", finishReason: "weird_reason", content: "内容", wantCode: CodeModelError},
		{name: "空原因", finishReason: "", content: "内容", wantCode: CodeModelError},
		{name: "工具调用原因", finishReason: "tool_calls", content: "", wantCode: CodeModelError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
				return jsonResponse(http.StatusOK, chatCompletionJSON(tc.content, tc.finishReason,
					`{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}`)), nil
			})
			result, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
			if tc.wantCode == "" {
				if err != nil {
					t.Fatalf("stop 应成功，实际 %v", err)
				}
				if result.Content != tc.content {
					t.Fatalf("回复内容应为 %q，实际 %q", tc.content, result.Content)
				}
				return
			}
			if err == nil {
				t.Fatalf("finish_reason=%q 不得伪装成成功回复: %+v", tc.finishReason, result)
			}
			assertCode(t, err, tc.wantCode)
			// 截断/过滤时已消耗的 token 仍须保留，便于 T05 计费记录。
			var controlled *Error
			if errors.As(err, &controlled) && !controlled.Usage.TotalTokens.Known {
				t.Fatal("失败必须保留供应商返回的可用用量")
			}
		})
	}
}

func TestOpenAIGeneratorRejectsToolCallOutput(t *testing.T) {
	body := `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"deepseek-flash",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"",` +
		`"tool_calls":[{"id":"call_1","type":"function","function":{"name":"danger","arguments":"{}"}}]},` +
		`"finish_reason":"stop"}]}`
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, body), nil
	})
	result, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	if err == nil {
		t.Fatalf("模型不得借工具输出绕过纯文本回复，实际: %+v", result)
	}
	assertCode(t, err, CodeModelError)
}

func TestOpenAIGeneratorHTTPErrorClassification(t *testing.T) {
	cases := []struct {
		name       string
		status     int
		body       string
		wantCode   ErrorCode
		secretBody string
	}{
		{name: "余额不足 402", status: http.StatusPaymentRequired,
			body:     `{"error":{"message":"Insufficient Balance secret-body-402","type":"invalid_request_error","code":"invalid_request_error"}}`,
			wantCode: CodeQuotaExceeded, secretBody: "secret-body-402"},
		{name: "限频 429", status: http.StatusTooManyRequests,
			body:     `{"error":{"message":"Rate limit secret-body-429","type":"rate_limit_error"}}`,
			wantCode: CodeRateLimited, secretBody: "secret-body-429"},
		{name: "鉴权失败 401", status: http.StatusUnauthorized,
			body:     `{"error":{"message":"Bad key secret-body-401"}}`,
			wantCode: CodeProviderError, secretBody: "secret-body-401"},
		{name: "服务端错误 500", status: http.StatusInternalServerError,
			body:     `{"error":{"message":"internal secret-body-500"}}`,
			wantCode: CodeModelUnavailable, secretBody: "secret-body-500"},
		{name: "容量不足 503", status: http.StatusServiceUnavailable,
			body:     `{"error":{"message":"overloaded secret-body-503"}}`,
			wantCode: CodeModelUnavailable, secretBody: "secret-body-503"},
		{name: "非 JSON 错误体", status: http.StatusBadGateway,
			body: `upstream gateway secret-body-plain`, wantCode: CodeModelUnavailable, secretBody: "secret-body-plain"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
				return jsonResponse(tc.status, tc.body), nil
			})
			_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
			assertCode(t, err, tc.wantCode)
			if transport.attempts() != 1 {
				t.Fatalf("失败不得自动重试，实际请求 %d 次", transport.attempts())
			}
			if strings.Contains(err.Error(), tc.secretBody) {
				t.Fatalf("受控错误泄漏了供应商原始正文: %v", err)
			}
			if strings.Contains(err.Error(), testAPIKey) {
				t.Fatalf("受控错误泄漏了 API Key: %v", err)
			}
		})
	}
}

func TestOpenAIGeneratorDoesNotFollowRedirect(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		if attempt == 1 {
			response := jsonResponse(http.StatusTemporaryRedirect, "")
			response.Header.Set("Location", "/redirect-target")
			return response, nil
		}
		return jsonResponse(http.StatusInternalServerError, ""), nil
	})
	_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	if calls := transport.attempts(); calls != 1 {
		t.Fatalf("重定向不得再次发送模型请求，实际请求 %d 次", calls)
	}
	assertCode(t, err, CodeProviderError)
}

func TestOpenAIGeneratorConnectionFailureIsSingleAttempt(t *testing.T) {
	sentinel := errors.New("dial tcp: connection refused secret-connect")
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return nil, sentinel
	})
	_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	assertCode(t, err, CodeModelUnavailable)
	if transport.attempts() != 1 {
		t.Fatalf("连接失败不得自动重试，实际请求 %d 次", transport.attempts())
	}
	if strings.Contains(err.Error(), "secret-connect") {
		t.Fatalf("受控错误泄漏了底层错误正文: %v", err)
	}
}

// 该用例捕获：取消只终止了上层等待，没有真正传到 HTTP 传输层。
func TestOpenAIGeneratorCancelReachesTransport(t *testing.T) {
	var transport *scriptedTransport
	transport = newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		<-req.Context().Done()
		transport.markCancelled()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	_, err := newTestGenerator(t, transport).Generate(ctx, validRequest())
	cancel()
	assertCode(t, err, CodeModelCanceled)
	if !transport.observedCancel() {
		t.Fatal("取消必须被底层传输真实观测到")
	}
	if transport.attempts() != 1 {
		t.Fatalf("取消前后不得补发请求，实际 %d 次", transport.attempts())
	}
}

// 该用例捕获：模型调用未施加 45 秒上限，或没有尊重更短的父期限。
func TestOpenAIGeneratorModelDeadlineReachesTransport(t *testing.T) {
	type observation struct {
		remaining time.Duration
		hasDL     bool
	}
	observed := make(chan observation, 1)
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		deadline, ok := req.Context().Deadline()
		if ok {
			observed <- observation{remaining: time.Until(deadline), hasDL: true}
		} else {
			observed <- observation{}
		}
		return jsonResponse(http.StatusOK, chatCompletionJSON("回复", "stop",
			`{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`)), nil
	})
	if _, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest()); err != nil {
		t.Fatalf("正常响应不应报错: %v", err)
	}
	got := <-observed
	if !got.hasDL {
		t.Fatal("模型调用必须带有界期限")
	}
	if got.remaining > DefaultModelTimeout || got.remaining < DefaultModelTimeout-5*time.Second {
		t.Fatalf("模型预算应约为 %v，实际剩余 %v", DefaultModelTimeout, got.remaining)
	}
}

func TestOpenAIGeneratorTimeoutMapsToModelTimeout(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		<-req.Context().Done()
		return nil, req.Context().Err()
	})
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	_, err := newTestGenerator(t, transport).Generate(ctx, validRequest())
	assertCode(t, err, CodeModelTimeout)
}

func TestOpenAIGeneratorRejectsInvalidRequestBeforeHTTP(t *testing.T) {
	cases := map[string]func(*Request){
		"空白发言": func(r *Request) { r.UserContent = "   " },
		"超长发言": func(r *Request) { r.UserContent = strings.Repeat("a", 2001) },
		"非法 UTF-8": func(r *Request) {
			r.UserContent = string([]byte{0xff})
		},
		"伪造轮次": func(r *Request) { r.RoundNo = 2 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
				t.Error("非法请求不得发起 HTTP 请求")
				return jsonResponse(http.StatusOK, chatCompletionJSON("回复", "stop", "")), nil
			})
			req := validRequest()
			mutate(&req)
			_, err := newTestGenerator(t, transport).Generate(context.Background(), req)
			assertCode(t, err, CodeInvalidInput)
			if transport.attempts() != 0 {
				t.Fatalf("非法请求不得发起 HTTP 请求，实际 %d 次", transport.attempts())
			}
		})
	}
}

func TestOpenAIGeneratorWindowExceededBeforeHTTP(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		t.Error("窗口不足时不得发起 HTTP 请求")
		return jsonResponse(http.StatusOK, chatCompletionJSON("回复", "stop", "")), nil
	})
	cfg := testConfig(transport)
	cfg.MaxOutputTokens = 50
	cfg.Window = WindowLimits{
		ContextTokens:              100,
		MaxOutputTokens:            50,
		SummaryPromptReserveTokens: 10,
		SummaryOutputReserveTokens: 10,
	}
	// 不足以覆盖完整三轮与总结预留的窗口属于配置错误，必须在构造期就失败，
	// 而不是等到发出请求后才发现预算不足。
	_, err := NewOpenAIGenerator(context.Background(), cfg)
	assertCode(t, err, CodeWindowExceeded)
	if transport.attempts() != 0 {
		t.Fatalf("窗口不足时不得发起 HTTP 请求，实际 %d 次", transport.attempts())
	}
}

func TestOpenAIGeneratorOversizedReplyFailsWithoutTruncation(t *testing.T) {
	long := strings.Repeat("𠮷", 2001)
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON(long, "stop",
			`{"prompt_tokens":5,"completion_tokens":900,"total_tokens":905}`)), nil
	})
	result, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	if err == nil {
		t.Fatalf("超长回复必须受控失败，不得截断后返回: %d 码点", len([]rune(result.Content)))
	}
	assertCode(t, err, CodeModelError)
	var controlled *Error
	if !errors.As(err, &controlled) || !controlled.Usage.TotalTokens.Known {
		t.Fatal("超长失败仍须保留计费元数据")
	}
}

func TestOpenAIGeneratorBlankReplyFails(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON("   ", "stop", "")), nil
	})
	_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	assertCode(t, err, CodeModelError)
}

func TestOpenAIGeneratorLogsWhitelistedFieldsOnly(t *testing.T) {
	var buffer strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))

	secretBody := `{"error":{"message":"provider-secret-body","type":"rate_limit_error"}}`
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusTooManyRequests, secretBody), nil
	})
	cfg := testConfig(transport)
	cfg.Logger = logger
	generator, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("构造适配器失败: %v", err)
	}
	req := validRequest()
	req.UserContent = "用户发言不得进日志-唯一标记"
	_, err = generator.Generate(context.Background(), req)
	if err == nil {
		t.Fatal("429 应返回错误")
	}

	logged := buffer.String()
	if logged == "" {
		t.Fatal("失败必须留下可用于观测的日志")
	}
	for _, forbidden := range []string{
		"用户发言不得进日志-唯一标记",
		testAPIKey,
		"provider-secret-body",
		req.Topic.Title,
	} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("日志不得包含敏感或正文内容 %q，实际: %s", forbidden, logged)
		}
	}
	for _, required := range []string{"error_code", "model", "prompt_version", "latency_ms"} {
		if !strings.Contains(logged, required) {
			t.Fatalf("日志缺少白名单字段 %q，实际: %s", required, logged)
		}
	}
}

func TestOpenAIGeneratorLogsUnknownUsageWithoutZero(t *testing.T) {
	var buffer strings.Builder
	logger := slog.New(slog.NewJSONHandler(&buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON("回复", "stop", "")), nil
	})
	cfg := testConfig(transport)
	cfg.Logger = logger
	generator, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("构造适配器失败: %v", err)
	}
	if _, err := generator.Generate(context.Background(), validRequest()); err != nil {
		t.Fatalf("正常响应不应报错: %v", err)
	}
	logged := buffer.String()
	if !strings.Contains(logged, "usage_known") {
		t.Fatalf("日志应显式标记用量是否已知，实际: %s", logged)
	}
	if strings.Contains(logged, `"prompt_tokens":0`) {
		t.Fatalf("用量未知时不得以 0 记录，实际: %s", logged)
	}
}

func TestOpenAIConfigValidation(t *testing.T) {
	cases := map[string]func(*OpenAIConfig){
		"缺少 Key":        func(c *OpenAIConfig) { c.APIKey = "" },
		"缺少模型":          func(c *OpenAIConfig) { c.Model = "" },
		"缺少 BaseURL":    func(c *OpenAIConfig) { c.BaseURL = "" },
		"BaseURL 非法":    func(c *OpenAIConfig) { c.BaseURL = "not-a-url" },
		"未配置输出上限":       func(c *OpenAIConfig) { c.MaxOutputTokens = 0 },
		"输出上限过大":        func(c *OpenAIConfig) { c.MaxOutputTokens = 384_000 },
		"未配置窗口":         func(c *OpenAIConfig) { c.Window = WindowLimits{} },
		"非回环明文 HTTP":    func(c *OpenAIConfig) { c.BaseURL = "http://api.deepseek.com" },
		"模型名带空白":        func(c *OpenAIConfig) { c.Model = " deepseek-flash " },
		"BaseURL 带查询参数": func(c *OpenAIConfig) { c.BaseURL = "https://api.deepseek.com?x=1" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
				t.Error("配置非法时不得发起请求")
				return nil, errors.New("unexpected")
			})
			cfg := testConfig(transport)
			mutate(&cfg)
			if _, err := NewOpenAIGenerator(context.Background(), cfg); err == nil {
				t.Fatal("非法配置必须被拒绝")
			}
		})
	}
}

func TestOpenAIConfigAcceptsLoopbackHTTPForTesting(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON("回复", "stop", "")), nil
	})
	cfg := testConfig(transport)
	cfg.BaseURL = "http://127.0.0.1:18080"
	if _, err := NewOpenAIGenerator(context.Background(), cfg); err != nil {
		t.Fatalf("回环明文 HTTP 应被允许用于本地测试: %v", err)
	}
}

func TestOpenAIConfigRequiresBaseURLHost(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return nil, errors.New("unexpected")
	})
	cfg := testConfig(transport)
	cfg.BaseURL = "https://"
	if _, err := NewOpenAIGenerator(context.Background(), cfg); err == nil {
		t.Fatal("缺少主机的 BaseURL 必须被拒绝")
	}
}

// 该用例捕获：适配器把 per-request 观察状态放在共享字段里，导致并发串台。
func TestOpenAIGeneratorConcurrentRequestsAreIsolated(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body := ""
		if req.Body != nil {
			buf := new(strings.Builder)
			_, _ = ioCopy(buf, req.Body)
			body = buf.String()
		}
		if err := json.Unmarshal([]byte(body), &payload); err != nil {
			return nil, err
		}
		last := payload.Messages[len(payload.Messages)-1].Content
		return jsonResponse(http.StatusOK, chatCompletionJSON("回声:"+last, "stop",
			`{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`)), nil
	})
	generator := newTestGenerator(t, transport)

	const sessions = 8
	var wg sync.WaitGroup
	results := make([]Result, sessions)
	errs := make([]error, sessions)
	for i := 0; i < sessions; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := validRequest()
			req.UserContent = "并发发言-" + itoa(i)
			results[i], errs[i] = generator.Generate(context.Background(), req)
		}(i)
	}
	wg.Wait()
	for i := 0; i < sessions; i++ {
		if errs[i] != nil {
			t.Fatalf("并发会话 %d 不应报错: %v", i, errs[i])
		}
		want := "回声:并发发言-" + itoa(i)
		if results[i].Content != want {
			t.Fatalf("并发会话 %d 串台：期望 %q，实际 %q", i, want, results[i].Content)
		}
		if !results[i].Usage.TotalTokens.Known || results[i].Usage.TotalTokens.Value != 2 {
			t.Fatalf("并发会话 %d 用量解析错误: %+v", i, results[i].Usage)
		}
	}
}

// 回环测试：验证真实 HTTP 客户端路径。沙箱若禁止回环监听会失败，按环境边界如实记录。
func TestOpenAIGeneratorLoopbackHTTP(t *testing.T) {
	server := newLoopbackServer(t)
	defer server.Close()

	cfg := testConfig(nil)
	cfg.BaseURL = server.URL
	cfg.HTTPClient = server.Client()
	generator, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("构造适配器失败: %v", err)
	}
	result, err := generator.Generate(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("回环请求应成功: %v", err)
	}
	if result.Content != "回环回复" {
		t.Fatalf("回环回复解析错误: %q", result.Content)
	}
	if server.hits() != 1 {
		t.Fatalf("回环服务应收到 1 次请求，实际 %d", server.hits())
	}
}

func TestClassifyModelErrorPreservesCauseAfterContextEnds(t *testing.T) {
	ledgerErr := errors.New("synthetic ledger write failure")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	expired, stop := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stop()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		code ErrorCode
	}{
		{"cancel", cancelled, CodeModelCanceled},
		{"timeout", expired, CodeModelTimeout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyModelError(tc.ctx, ledgerErr, Usage{}, Metadata{})
			if got.Code != tc.code || !errors.Is(got, tc.ctx.Err()) || !errors.Is(got, ledgerErr) {
				t.Fatalf("code=%v, context=%v, ledger=%v", got.Code, errors.Is(got, tc.ctx.Err()), errors.Is(got, ledgerErr))
			}
			if strings.Contains(got.Error(), ledgerErr.Error()) {
				t.Fatal("controlled message exposes the underlying error")
			}
		})
	}
}
