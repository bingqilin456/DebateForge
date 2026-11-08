package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

// 该用例捕获：配置输出上限与窗口输出上限不一致时，预检按小值、实际发大值。
func TestOpenAIConfigRejectsOutputCapMismatch(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		t.Error("配置不一致时不得发起请求")
		return nil, errors.New("unexpected")
	})
	cfg := testConfig(transport)
	cfg.Window.MaxOutputTokens = cfg.MaxOutputTokens + 128
	if _, err := NewOpenAIGenerator(context.Background(), cfg); err == nil {
		t.Fatal("输出上限与窗口输出上限不一致时必须拒绝构造")
	}

	cfg = testConfig(transport)
	cfg.Window.MaxOutputTokens = cfg.MaxOutputTokens - 128
	if _, err := NewOpenAIGenerator(context.Background(), cfg); err == nil {
		t.Fatal("输出上限与窗口输出上限不一致时必须拒绝构造")
	}
}

func TestOpenAIGeneratorSendsWindowOutputCap(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON("回复", "stop",
			`{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}`)), nil
	})
	generator := newTestGenerator(t, transport)
	if _, err := generator.Generate(context.Background(), validRequest()); err != nil {
		t.Fatalf("正常响应不应报错: %v", err)
	}
	payload := decodeBody(t, transport.record(t, 0).Body)
	if got, ok := payload["max_tokens"].(float64); !ok || int(got) != 512 {
		t.Fatalf("实际发出的 max_tokens 必须等于窗口输出上限 512，实际 %v", payload["max_tokens"])
	}
}

// 该用例捕获：日志只记 known 布尔，缺少真实已知 token 数，无法核对计费。
func TestOpenAIGeneratorLogsKnownTokenCounts(t *testing.T) {
	var buffer strings.Builder
	logger := newBufferLogger(&buffer)
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON("回复", "stop",
			`{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}`)), nil
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

	entry := lastLogEntry(t, buffer.String())
	if got := entry["prompt_tokens"]; got != float64(11) {
		t.Fatalf("日志应记录真实已知 prompt_tokens=11，实际 %v", got)
	}
	if got := entry["completion_tokens"]; got != float64(7) {
		t.Fatalf("日志应记录真实已知 completion_tokens=7，实际 %v", got)
	}
	if got := entry["total_tokens"]; got != float64(18) {
		t.Fatalf("日志应记录真实已知 total_tokens=18，实际 %v", got)
	}
}

func TestOpenAIGeneratorLogsUnknownUsageWithoutZeroFields(t *testing.T) {
	var buffer strings.Builder
	logger := newBufferLogger(&buffer)
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

	entry := lastLogEntry(t, buffer.String())
	for _, field := range []string{"prompt_tokens", "completion_tokens", "total_tokens"} {
		if value, present := entry[field]; present && value != nil {
			t.Fatalf("用法未知时不得记录 %s=%v（0 会伪装成已知）", field, value)
		}
	}
}

// 该用例捕获：Metadata 携带供应商拒答原文，越过“元数据不含正文”的安全边界。
func TestOpenAIGeneratorMetadataDoesNotCarryRefusalText(t *testing.T) {
	const refusalText = "我不能协助这个请求-机密拒答原文"
	body := `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"deepseek-flash",` +
		`"choices":[{"index":0,"message":{"role":"assistant","content":"","refusal":` + jsonString(refusalText) + `},` +
		`"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":9,"completion_tokens":2,"total_tokens":11}}`
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, body), nil
	})
	_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	assertCode(t, err, CodeModelRefused)

	var controlled *Error
	if !errors.As(err, &controlled) {
		t.Fatalf("应为受控错误，实际 %T", err)
	}
	// 元数据里任何字段都不能出现原文。
	serialized, marshalErr := json.Marshal(controlled.Metadata)
	if marshalErr != nil {
		t.Fatalf("元数据应可序列化: %v", marshalErr)
	}
	if strings.Contains(string(serialized), refusalText) {
		t.Fatalf("Metadata 泄漏了拒答原文: %s", serialized)
	}
	if !controlled.Metadata.Refused {
		t.Fatal("必须以受控标志记录拒答事实，供诊断使用")
	}
}

// 该用例捕获：解析固定取 choices[0]，忽略 index，与官方适配器取 index==0 不一致。
func TestOpenAIGeneratorResponseObservationMatchesIndexZeroChoice(t *testing.T) {
	body := `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"deepseek-flash",` +
		`"choices":[{"index":1,"message":{"role":"assistant","content":"错误分支","refusal":"无关拒答"},` +
		`"finish_reason":"length"},` +
		`{"index":0,"message":{"role":"assistant","content":"正确回复"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, body), nil
	})
	result, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	if err != nil {
		t.Fatalf("index==0 的 choice 正常结束，不应报错: %v", err)
	}
	if result.Content != "正确回复" {
		t.Fatalf("应采用 index==0 的回复，实际 %q", result.Content)
	}
	if result.Metadata.FinishReason != "stop" {
		t.Fatalf("应采用 index==0 的 finish_reason，实际 %q", result.Metadata.FinishReason)
	}
	if result.Metadata.Refused {
		t.Fatal("不得把 index!=0 的拒答误判到本次结果")
	}
}

func TestOpenAIGeneratorMissingIndexZeroChoiceKeepsUsage(t *testing.T) {
	body := `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"deepseek-flash",` +
		`"choices":[{"index":1,"message":{"role":"assistant","content":"非零分支"},"finish_reason":"stop"}],` +
		`"usage":{"prompt_tokens":4,"completion_tokens":3,"total_tokens":7}}`
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, body), nil
	})
	_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	// 官方适配器会因找不到 index==0 而报错；本包必须把它归为受控错误。
	if err == nil {
		t.Fatal("缺少 index==0 的 choice 必须受控失败")
	}
	var controlled *Error
	if !errors.As(err, &controlled) {
		t.Fatalf("应为受控错误，实际 %T", err)
	}
	if !controlled.Usage.TotalTokens.Known || controlled.Usage.TotalTokens.Value != 7 {
		t.Fatalf("已返回的 usage 应保留，实际 %+v", controlled.Usage)
	}
}

// 空 choices 失败也必须保留已经返回的用量，不能因 SDK 提前退出而丢失。
func TestOpenAIGeneratorEmptyChoicesKeepsReturnedUsage(t *testing.T) {
	body := `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"deepseek-flash",` +
		`"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":0,"total_tokens":4}}`
	transport := newScriptedTransport(func(req *http.Request, attempt int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, body), nil
	})
	_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	if err == nil {
		t.Fatal("空 choices 必须受控失败")
	}
	var controlled *Error
	if !errors.As(err, &controlled) {
		t.Fatalf("应为受控错误，实际 %T", err)
	}
	if !controlled.Usage.TotalTokens.Known || controlled.Usage.TotalTokens.Value != 4 {
		t.Fatalf("空 choices 时仍应保留已返回的用量，实际 %+v", controlled.Usage)
	}
}

// 该用例捕获：负 token 数被当作已知用量。
func TestParseUsageIgnoresNegativeCounts(t *testing.T) {
	observation := parseResponseObservation([]byte(`{"choices":[{"index":0,"message":{"content":"x"},` +
		`"finish_reason":"stop"}],"usage":{"prompt_tokens":-5,"completion_tokens":3,"total_tokens":-2}}`))
	if observation.usage.PromptTokens.Known {
		t.Fatalf("负数 prompt_tokens 必须视为未知，实际 %+v", observation.usage.PromptTokens)
	}
	if observation.usage.TotalTokens.Known {
		t.Fatalf("负数 total_tokens 必须视为未知，实际 %+v", observation.usage.TotalTokens)
	}
	if !observation.usage.CompletionTokens.Known || observation.usage.CompletionTokens.Value != 3 {
		t.Fatalf("合法字段应保持已知，实际 %+v", observation.usage.CompletionTokens)
	}
}
