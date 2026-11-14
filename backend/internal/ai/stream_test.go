package ai

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// 本文件只使用进程内回环 httptest 合成供应商，不发起任何真实模型调用。

// ---------- 合成 SSE 供应商 ----------

type sseRequest struct {
	Accept string
	Body   string
}

type sseProvider struct {
	*httptest.Server
	mu       sync.Mutex
	requests []sseRequest
}

func newSSEProvider(t *testing.T, handler func(t *testing.T, w http.ResponseWriter, r *http.Request)) *sseProvider {
	t.Helper()
	provider := &sseProvider{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			return
		}
		provider.mu.Lock()
		provider.requests = append(provider.requests, sseRequest{Accept: r.Header.Get("Accept"), Body: string(body)})
		provider.mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		handler(t, w, r)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (p *sseProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.requests)
}

func (p *sseProvider) request(t *testing.T, index int) sseRequest {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	if index < 0 || index >= len(p.requests) {
		t.Fatalf("请求记录不足：希望第 %d 次，实际共 %d 次", index, len(p.requests))
	}
	return p.requests[index]
}

func writeSSEFrame(w http.ResponseWriter, payload string) {
	_, _ = io.WriteString(w, "data: "+payload+"\n\n")
	flushResponse(w)
}

func writeSSERaw(w http.ResponseWriter, raw string) {
	_, _ = io.WriteString(w, raw)
	flushResponse(w)
}

func flushResponse(w http.ResponseWriter) {
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

// ---------- SSE 帧构造 ----------

func streamChunkFrame(role, content string, finish *string, usage string) string {
	fields := make([]string, 0, 2)
	if role != "" {
		fields = append(fields, `"role":`+jsonString(role))
	}
	if content != "" {
		fields = append(fields, `"content":`+jsonString(content))
	}
	finishPart := "null"
	if finish != nil {
		finishPart = jsonString(*finish)
	}
	usagePart := ""
	if usage != "" {
		usagePart = `,"usage":` + usage
	}
	return `{"id":"chunk","object":"chat.completion.chunk","model":"deepseek-flash","choices":[` +
		`{"index":0,"delta":{` + strings.Join(fields, ",") + `},"finish_reason":` + finishPart + `}]` + usagePart + `}`
}

func streamUsageFrame(usage string) string {
	return `{"id":"chunk","object":"chat.completion.chunk","model":"deepseek-flash","choices":[],"usage":` + usage + `}`
}

func streamStop() *string {
	stop := "stop"
	return &stop
}

func streamReason(reason string) *string { return &reason }

const (
	streamUsageOK      = `{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}`
	streamUsageUnknown = ""
)

func writeStandardStream(w http.ResponseWriter, deltas []string, finish *string, usage string) {
	first := true
	for _, delta := range deltas {
		role := ""
		if first {
			role = "assistant"
			first = false
		}
		writeSSEFrame(w, streamChunkFrame(role, delta, nil, ""))
	}
	writeSSEFrame(w, streamChunkFrame("", "", finish, ""))
	if usage != "" {
		writeSSEFrame(w, streamUsageFrame(usage))
	}
	writeSSEFrame(w, "[DONE]")
}

func newSSEStreamer(t *testing.T, provider *sseProvider) StreamGenerator {
	t.Helper()
	cfg := testConfig(nil)
	cfg.BaseURL = provider.URL
	cfg.HTTPClient = &http.Client{}
	generator, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("构造适配器失败: %v", err)
	}
	streamer, ok := generator.(StreamGenerator)
	if !ok {
		t.Fatal("OpenAIAdapter 必须实现可选的 StreamGenerator")
	}
	return streamer
}

// ---------- 接口与保护包装 ----------

func TestStreamGeneratorIsOptionalInterface(t *testing.T) {
	var adapter StreamGenerator = (*OpenAIAdapter)(nil)
	var fake StreamGenerator = NewFake()
	if adapter == nil || fake == nil {
		t.Fatal("StreamGenerator 编译期断言失败")
	}
}

func TestOpenAIStreamRejectsNilCallbackBeforeHTTP(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Error("回调缺失时不得发起供应商请求")
	})
	if _, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), nil); err == nil {
		t.Fatal("缺少回调必须受控失败")
	}
	if provider.calls() != 0 {
		t.Fatalf("不得发起 HTTP 请求，实际 %d 次", provider.calls())
	}
}

// 关键保护：增量必须在供应商仍在写响应时就到达回调，不能被整流缓冲后伪造。
func TestOpenAIStreamDeliversDeltasBeforeProviderFinishes(t *testing.T) {
	firstDelivered := make(chan struct{})
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSEFrame(w, streamChunkFrame("assistant", "评价公平", nil, ""))
		select {
		case <-firstDelivered:
		case <-time.After(2 * time.Second):
			t.Error("首个增量未在供应商完成前到达回调，疑似整流缓冲后伪造流式")
			return
		}
		writeSSEFrame(w, streamChunkFrame("", "仍需统一标准。", nil, ""))
		writeSSEFrame(w, streamChunkFrame("", "", streamStop(), ""))
		writeSSEFrame(w, streamUsageFrame(streamUsageOK))
		writeSSEFrame(w, "[DONE]")
	})

	var deltas []string
	result, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(text string) error {
		deltas = append(deltas, text)
		if len(deltas) == 1 {
			close(firstDelivered)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("正常流式响应不应报错: %v", err)
	}
	if strings.Join(deltas, "") != "评价公平仍需统一标准。" {
		t.Fatalf("增量拼接应为完整回复，实际 %v", deltas)
	}
	if result.Content != strings.Join(deltas, "") {
		t.Fatalf("最终 Result 必须等于增量拼接，实际 %q", result.Content)
	}
	if !result.Usage.PromptTokens.Known || result.Usage.PromptTokens.Value != 11 ||
		!result.Usage.CompletionTokens.Known || result.Usage.CompletionTokens.Value != 7 ||
		!result.Usage.TotalTokens.Known || result.Usage.TotalTokens.Value != 18 {
		t.Fatalf("最终用量应取供应商 usage，实际 %+v", result.Usage)
	}
	if result.Metadata.FinishReason != "stop" || result.Metadata.Model != testModel {
		t.Fatalf("最终元数据错误: %+v", result.Metadata)
	}
}

func TestOpenAIStreamSendsStreamingRequestShape(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeStandardStream(w, []string{"回复"}, streamStop(), streamUsageOK)
	})
	if _, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(string) error { return nil }); err != nil {
		t.Fatalf("正常流式响应不应报错: %v", err)
	}
	if provider.calls() != 1 {
		t.Fatalf("成功调用应恰好 1 次请求，实际 %d", provider.calls())
	}
	request := provider.request(t, 0)
	if !strings.Contains(request.Accept, "text/event-stream") {
		t.Fatalf("流式请求必须接受 text/event-stream，实际 %q", request.Accept)
	}
	payload := decodeBody(t, request.Body)
	if stream, ok := payload["stream"].(bool); !ok || !stream {
		t.Fatalf("请求体必须 stream=true，实际 %v", payload["stream"])
	}
	options, ok := payload["stream_options"].(map[string]any)
	if !ok || options["include_usage"] != true {
		t.Fatalf("请求体必须要求 usage 汇总，实际 %v", payload["stream_options"])
	}
	if got, ok := payload["max_tokens"].(float64); !ok || int(got) != 512 {
		t.Fatalf("流式仍必须发出锁定输出上限，实际 %v", payload["max_tokens"])
	}
	thinking, ok := payload["thinking"].(map[string]any)
	if !ok || thinking["type"] != "disabled" {
		t.Fatalf("流式仍必须显式关闭思考，实际 %v", payload["thinking"])
	}
}

func TestOpenAIStreamMissingDoneFrameFails(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSEFrame(w, streamChunkFrame("assistant", "未结束的回复", nil, ""))
		writeSSEFrame(w, streamChunkFrame("", "", streamStop(), ""))
		writeSSEFrame(w, streamUsageFrame(streamUsageOK))
		// 故意不写 [DONE]，模拟供应商缺尾帧。
	})
	var emitted []string
	result, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(text string) error {
		emitted = append(emitted, text)
		return nil
	})
	assertCode(t, err, CodeModelError)
	if result.Content != "" {
		t.Fatalf("缺尾帧不得返回可用结果，实际 %q", result.Content)
	}
	var controlled *Error
	if !errors.As(err, &controlled) || !controlled.Usage.TotalTokens.Known {
		t.Fatal("缺尾帧失败仍须保留已返回的计费用量")
	}
	if strings.Join(emitted, "") != "未结束的回复" {
		t.Fatalf("临时增量应已回调（由上层丢弃），实际 %v", emitted)
	}
}

func TestOpenAIStreamMissingUsageStaysUnknown(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeStandardStream(w, []string{"回复"}, streamStop(), streamUsageUnknown)
	})
	result, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(string) error { return nil })
	if err != nil {
		t.Fatalf("缺少 usage 不应导致流式失败: %v", err)
	}
	usage := result.Usage
	if usage.PromptTokens.Known || usage.CompletionTokens.Known || usage.TotalTokens.Known {
		t.Fatalf("usage 缺失时必须保持未知，不能用 0 伪装已知: %+v", usage)
	}
}

func TestOpenAIStreamFailureClassifications(t *testing.T) {
	cases := []struct {
		name     string
		finish   *string
		toolCall bool
		wantCode ErrorCode
	}{
		{name: "长度截断", finish: streamReason("length"), wantCode: CodeModelError},
		{name: "内容过滤", finish: streamReason("content_filter"), wantCode: CodeModelRefused},
		{name: "未知原因", finish: streamReason("weird_reason"), wantCode: CodeModelError},
		{name: "缺少结束原因", finish: nil, wantCode: CodeModelError},
		{name: "工具调用", finish: streamStop(), toolCall: true, wantCode: CodeModelError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
				if tc.toolCall {
					writeSSEFrame(w, `{"id":"chunk","object":"chat.completion.chunk","model":"deepseek-flash",`+
						`"choices":[{"index":0,"delta":{"role":"assistant","content":"",`+
						`"tool_calls":[{"id":"call_1","type":"function","function":{"name":"danger","arguments":"{}"}}]},`+
						`"finish_reason":null}]}`)
				} else {
					writeSSEFrame(w, streamChunkFrame("assistant", "回复正文", nil, ""))
				}
				if tc.finish != nil {
					writeSSEFrame(w, streamChunkFrame("", "", tc.finish, ""))
				}
				writeSSEFrame(w, streamUsageFrame(streamUsageOK))
				writeSSEFrame(w, "[DONE]")
			})
			_, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(string) error { return nil })
			assertCode(t, err, tc.wantCode)
			if provider.calls() != 1 {
				t.Fatalf("失败不得自动重试，实际 %d 次请求", provider.calls())
			}
		})
	}
}

func TestOpenAIStreamInvalidUTF8Fails(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSERaw(w, "data: {\"id\":\"chunk\",\"model\":\"deepseek-flash\",\"choices\":[{\"index\":0,"+
			"\"delta\":{\"role\":\"assistant\",\"content\":\"bad\xffbyte\"},\"finish_reason\":null}]}\n\n")
		writeSSEFrame(w, streamChunkFrame("", "", streamStop(), ""))
		writeSSEFrame(w, "[DONE]")
	})
	_, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(string) error { return nil })
	assertCode(t, err, CodeModelError)
}

// 网络分块可能把一个多字节字符拆到两次读中，不能误判为非法 UTF-8。
func TestOpenAIStreamToleratesRuneSplitAcrossReads(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		content := "𠮷字"
		encoded := jsonString(content)
		frame := streamChunkFrame("assistant", "@@", nil, "")
		frame = strings.Replace(frame, `"content":"@@"`, `"content":`+encoded, 1)
		cut := 2 // 在 JSON 字符串内部切开，模拟 TCP 分块切断四字节字符
		writeSSERaw(w, "data: "+frame[:cut])
		writeSSERaw(w, frame[cut:]+"\n\n")
		writeSSEFrame(w, streamChunkFrame("", "", streamStop(), ""))
		writeSSEFrame(w, "[DONE]")
	})
	var emitted []string
	result, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(text string) error {
		emitted = append(emitted, text)
		return nil
	})
	if err != nil {
		t.Fatalf("合法多字节字符分块不应失败: %v", err)
	}
	if result.Content != "𠮷字" || strings.Join(emitted, "") != "𠮷字" {
		t.Fatalf("分块合并错误: result=%q emitted=%v", result.Content, emitted)
	}
}

// 回调错误必须立即停止上游读取并关闭 reader；这只减少后续消耗，
// 已发出的请求仍可能计费，因此失败结果需保留 usage 供上层预留与核对。
func TestOpenAIStreamCallbackErrorStopsUpstream(t *testing.T) {
	upstreamClosed := make(chan struct{})
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSEFrame(w, streamChunkFrame("assistant", "第一段", nil, ""))
		select {
		case <-r.Context().Done():
			close(upstreamClosed)
		case <-time.After(3 * time.Second):
			t.Error("回调失败后上游连接未被取消")
		}
	})
	var emitted []string
	done := make(chan error, 1)
	go func() {
		_, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(text string) error {
			emitted = append(emitted, text)
			return errors.New("下游写入失败")
		})
		done <- err
	}()
	select {
	case err := <-done:
		assertCode(t, err, CodeModelCanceled)
	case <-time.After(5 * time.Second):
		t.Fatal("回调错误后适配器未及时返回")
	}
	select {
	case <-upstreamClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("回调错误后未关闭上游连接")
	}
	if len(emitted) != 1 {
		t.Fatalf("回调失败后不得继续回调，实际 %d 次", len(emitted))
	}
}

func TestOpenAIStreamContextCancelStopsUpstream(t *testing.T) {
	upstreamClosed := make(chan struct{})
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSEFrame(w, streamChunkFrame("assistant", "第一段", nil, ""))
		select {
		case <-r.Context().Done():
			close(upstreamClosed)
		case <-time.After(3 * time.Second):
			t.Error("取消后上游连接未被取消")
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := newSSEStreamer(t, provider).Stream(ctx, validRequest(), func(text string) error {
		cancel()
		return nil
	})
	assertCode(t, err, CodeModelCanceled)
	select {
	case <-upstreamClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("取消后未关闭上游连接")
	}
}

func TestOpenAIStreamOversizedReplyAbortsUpstream(t *testing.T) {
	upstreamClosed := make(chan struct{})
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSEFrame(w, streamChunkFrame("assistant", strings.Repeat("𠮷", 2000), nil, ""))
		for range 40 {
			writeSSEFrame(w, streamChunkFrame("", strings.Repeat("𠮷", 40), nil, ""))
			select {
			case <-r.Context().Done():
				close(upstreamClosed)
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
		t.Error("超长回复未被提前中止")
	})
	var emitted int
	_, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(text string) error {
		emitted++
		return nil
	})
	assertCode(t, err, CodeModelError)
	select {
	case <-upstreamClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("超长回复必须立即停止上游读取，减少后续消耗")
	}
	if emitted > 3 {
		t.Fatalf("超长回复应在少量增量后中止，实际回调 %d 次", emitted)
	}
}

func TestOpenAIStreamRejectsInvalidRequestBeforeHTTP(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Error("非法请求不得发起供应商请求")
	})
	req := validRequest()
	req.UserContent = strings.Repeat("a", 2001)
	_, err := newSSEStreamer(t, provider).Stream(context.Background(), req, func(string) error { return nil })
	assertCode(t, err, CodeInvalidInput)
	if provider.calls() != 0 {
		t.Fatalf("非法请求不得发起 HTTP 请求，实际 %d 次", provider.calls())
	}
}

// 流式同样必须在出网前做窗口预检，不能因为“边生成边看”而跳过预算。
func TestOpenAIStreamWindowExceededBeforeHTTP(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		t.Error("窗口不足时不得发起供应商请求")
	})
	cfg := testConfig(nil)
	cfg.BaseURL = provider.URL
	cfg.HTTPClient = &http.Client{}
	generator, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("构造适配器失败: %v", err)
	}
	// 直接收紧窗口：模拟运行期预算不足，必须在出网前拒绝。
	adapter := generator.(*OpenAIAdapter)
	adapter.config.Window = WindowLimits{
		ContextTokens: 1000, MaxOutputTokens: 512,
		SummaryPromptReserveTokens: 10, SummaryOutputReserveTokens: 10,
	}
	_, err = generator.(StreamGenerator).Stream(context.Background(), validRequest(), func(string) error { return nil })
	assertCode(t, err, CodeWindowExceeded)
	if provider.calls() != 0 {
		t.Fatalf("窗口不足时不得发起 HTTP 请求，实际 %d 次", provider.calls())
	}
}

// deadlineTransport 在客户端侧观测真实请求 Context 的剩余期限。
type deadlineTransport struct {
	base     http.RoundTripper
	observed chan time.Duration
}

func (t deadlineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	remaining := time.Duration(0)
	if deadline, ok := req.Context().Deadline(); ok {
		remaining = time.Until(deadline)
	}
	t.observed <- remaining
	return t.base.RoundTrip(req)
}

// 流式调用同样受 45 秒模型预算约束（尊重更短父期限）。
func TestOpenAIStreamModelDeadlineReachesTransport(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeStandardStream(w, []string{"回复"}, streamStop(), streamUsageOK)
	})
	observed := make(chan time.Duration, 1)
	cfg := testConfig(nil)
	cfg.BaseURL = provider.URL
	cfg.HTTPClient = &http.Client{Transport: deadlineTransport{base: http.DefaultTransport, observed: observed}}
	generator, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("构造适配器失败: %v", err)
	}
	if _, err := generator.(StreamGenerator).Stream(context.Background(), validRequest(), func(string) error {
		return nil
	}); err != nil {
		t.Fatalf("正常流式响应不应报错: %v", err)
	}
	remaining := <-observed
	if remaining <= 0 || remaining > DefaultModelTimeout || remaining < DefaultModelTimeout-5*time.Second {
		t.Fatalf("流式模型预算应约为 %v，实际 %v", DefaultModelTimeout, remaining)
	}
}

// 供应商错误状态必须按受控错误码分类、不重试、不泄漏原始正文。
func TestOpenAIStreamHTTPErrorClassification(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   ErrorCode
	}{
		{http.StatusPaymentRequired, CodeQuotaExceeded},
		{http.StatusTooManyRequests, CodeRateLimited},
		{http.StatusServiceUnavailable, CodeModelUnavailable},
	} {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			var calls int
			var mu sync.Mutex
			provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				calls++
				mu.Unlock()
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, `{"error":{"message":"secret-stream-body"}}`)
			}))
			defer provider.Close()
			cfg := testConfig(nil)
			cfg.BaseURL = provider.URL
			cfg.HTTPClient = &http.Client{}
			generator, err := NewOpenAIGenerator(context.Background(), cfg)
			if err != nil {
				t.Fatal(err)
			}
			_, err = generator.(StreamGenerator).Stream(context.Background(), validRequest(), func(string) error { return nil })
			assertCode(t, err, tc.code)
			mu.Lock()
			got := calls
			mu.Unlock()
			if got != 1 {
				t.Fatalf("失败不得自动重试，实际 %d 次请求", got)
			}
			if strings.Contains(err.Error(), "secret-stream-body") {
				t.Fatalf("受控错误泄漏了供应商正文: %v", err)
			}
		})
	}
}

// CRLF 行尾、注释心跳与多 data 行不得破坏解析；注释不能被当成事件。
func TestOpenAIStreamAcceptsCRLFAndComments(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSERaw(w, ": keepalive\r\n\r\n")
		writeSSERaw(w, "data: "+streamChunkFrame("assistant", "跨行", nil, "")+"\r\n\r\n")
		writeSSERaw(w, "event: delta\r\n")
		writeSSERaw(w, "data: "+streamChunkFrame("", "回复", nil, "")+"\r\n\r\n")
		writeSSERaw(w, "data: "+streamChunkFrame("", "", streamStop(), "")+"\r\n\r\n")
		writeSSERaw(w, "data: "+streamUsageFrame(streamUsageOK)+"\r\n\r\n")
		writeSSERaw(w, "data: [DONE]\r\n\r\n")
	})
	var emitted []string
	result, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(text string) error {
		emitted = append(emitted, text)
		return nil
	})
	if err != nil {
		t.Fatalf("CRLF 与注释不应破坏流式解析: %v", err)
	}
	if result.Content != "跨行回复" || strings.Join(emitted, "") != "跨行回复" {
		t.Fatalf("解析结果错误: result=%q emitted=%v", result.Content, emitted)
	}
}

// 空白回复必须是受控失败，不能把空 delta 当成功保存。
func TestOpenAIStreamBlankReplyFails(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeStandardStream(w, []string{"   "}, streamStop(), streamUsageOK)
	})
	_, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(string) error { return nil })
	assertCode(t, err, CodeModelError)
}

// 缺少 assistant 角色的响应必须受控失败，避免把非回复内容当回复。
func TestOpenAIStreamMissingAssistantRoleFails(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSEFrame(w, streamChunkFrame("", "无角色回复", nil, ""))
		writeSSEFrame(w, streamChunkFrame("", "", streamStop(), ""))
		writeSSEFrame(w, "[DONE]")
	})
	_, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(string) error { return nil })
	assertCode(t, err, CodeModelError)
}

// 流式日志同样只写白名单字段：不写 Key、发言、辩题、Prompt 或供应商正文。
func TestOpenAIStreamLogsWhitelistedFieldsOnly(t *testing.T) {
	var buffer strings.Builder
	logger := newBufferLogger(&buffer)
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSEFrame(w, streamChunkFrame("assistant", "回复正文-stream-secret", nil, ""))
		writeSSEFrame(w, streamChunkFrame("", "", streamReason("length"), ""))
		writeSSEFrame(w, streamUsageFrame(streamUsageOK))
		writeSSEFrame(w, "[DONE]")
	})
	cfg := testConfig(nil)
	cfg.BaseURL = provider.URL
	cfg.HTTPClient = &http.Client{}
	cfg.Logger = logger
	generator, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("构造适配器失败: %v", err)
	}
	req := validRequest()
	req.UserContent = "用户发言不得进日志-唯一标记"
	_, err = generator.(StreamGenerator).Stream(context.Background(), req, func(string) error { return nil })
	if err == nil {
		t.Fatal("长度截断必须受控失败")
	}

	logged := buffer.String()
	for _, forbidden := range []string{
		"用户发言不得进日志-唯一标记", testAPIKey, "stream-secret", req.Topic.Title,
	} {
		if strings.Contains(logged, forbidden) {
			t.Fatalf("流式日志不得包含敏感或正文内容 %q：%s", forbidden, logged)
		}
	}
	for _, required := range []string{"error_code", "model", "prompt_version", "latency_ms"} {
		if !strings.Contains(logged, required) {
			t.Fatalf("流式日志缺少白名单字段 %q：%s", required, logged)
		}
	}
	// 已知用量按真实值记录，缺失时不得以 0 冒充。
	entry := lastLogEntry(t, logged)
	if got := entry["prompt_tokens"]; got != float64(11) {
		t.Fatalf("流式日志应记录已知 prompt_tokens=11，实际 %v", got)
	}
}

func TestOpenAIStreamBoundedProviderResponse(t *testing.T) {
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		payload := strings.Repeat("x", 4096)
		for range 300 {
			if _, err := io.WriteString(w, "data: "+streamChunkFrame("", payload, nil, "")+"\n\n"); err != nil {
				return
			}
			flushResponse(w)
		}
	})
	_, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(string) error { return nil })
	if err == nil {
		t.Fatal("超过大小上限的流式响应必须受控失败")
	}
	assertCode(t, err, CodeModelError)
}

func TestOpenAIStreamDoesNotFollowRedirect(t *testing.T) {
	var calls int
	var mu sync.Mutex
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.Header().Set("Location", "/redirect-target")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer provider.Close()
	cfg := testConfig(nil)
	cfg.BaseURL = provider.URL
	cfg.HTTPClient = &http.Client{}
	generator, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	streamer := generator.(StreamGenerator)
	_, err = streamer.Stream(context.Background(), validRequest(), func(string) error { return nil })
	if err == nil {
		t.Fatal("重定向必须受控失败")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("重定向不得再次发送模型请求，实际 %d 次", calls)
	}
}

// ---------- Fake 的流式能力 ----------

func TestFakeStreamEmitsScriptedChunksAndResult(t *testing.T) {
	fake := NewFake(FakeStep{
		Result:       Result{Usage: Usage{TotalTokens: KnownCount(18)}, Metadata: Metadata{FinishReason: "stop"}},
		StreamChunks: []string{"甲", "乙", "丙丁"},
		StreamDelays: []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond},
	})
	var emitted []string
	result, err := fake.Stream(context.Background(), validRequest(), func(text string) error {
		emitted = append(emitted, text)
		return nil
	})
	if err != nil {
		t.Fatalf("Fake 流式不应报错: %v", err)
	}
	if strings.Join(emitted, "") != "甲乙丙丁" || result.Content != "甲乙丙丁" {
		t.Fatalf("脚本分块错误: emitted=%v result=%q", emitted, result.Content)
	}
	if !result.Usage.TotalTokens.Known || result.Usage.TotalTokens.Value != 18 {
		t.Fatalf("脚本用量应保留，实际 %+v", result.Usage)
	}
	if result.Metadata.PromptVersion != PromptVersion {
		t.Fatalf("Fake 必须补齐 Prompt 版本，实际 %q", result.Metadata.PromptVersion)
	}
	if fake.Calls() != 1 {
		t.Fatalf("应记录 1 次调用，实际 %d", fake.Calls())
	}
}

func TestFakeStreamFailureStepEmitsNothing(t *testing.T) {
	fake := NewFake(FakeStep{Err: &Error{Code: CodeModelTimeout, Message: "回复生成超时"}})
	emitted := 0
	_, err := fake.Stream(context.Background(), validRequest(), func(string) error {
		emitted++
		return nil
	})
	assertCode(t, err, CodeModelTimeout)
	if emitted != 0 {
		t.Fatalf("失败步骤不得回调增量，实际 %d 次", emitted)
	}
}

func TestFakeStreamFaultAfterChunksStops(t *testing.T) {
	// 语义：Err 非 nil 时先按脚本发出分块，再返回故障，模拟供应商中途断开；
	// 上层必须丢弃这些临时增量，且故障后不得再有回调。
	t.Run("发出全部分块后失败", func(t *testing.T) {
		fake := NewFake(FakeStep{
			Err:          &Error{Code: CodeModelUnavailable, Message: "合成供应商中途失败"},
			StreamChunks: []string{"部分一", "部分二"},
		})
		var emitted []string
		result, err := fake.Stream(context.Background(), validRequest(), func(text string) error {
			emitted = append(emitted, text)
			return nil
		})
		assertCode(t, err, CodeModelUnavailable)
		if strings.Join(emitted, "") != "部分一部分二" {
			t.Fatalf("故障前应恰好发出脚本分块，实际 %v", emitted)
		}
		if result.Content != "" {
			t.Fatalf("故障不得返回可落库的完整回复，实际 %q", result.Content)
		}
	})
	t.Run("中途失败", func(t *testing.T) {
		fake := NewFake(FakeStep{
			Err:             &Error{Code: CodeModelUnavailable, Message: "合成供应商中途失败"},
			StreamChunks:    []string{"部分一", "不应到达"},
			StreamFailAfter: 1,
		})
		var emitted []string
		_, err := fake.Stream(context.Background(), validRequest(), func(text string) error {
			emitted = append(emitted, text)
			return nil
		})
		assertCode(t, err, CodeModelUnavailable)
		if strings.Join(emitted, "") != "部分一" {
			t.Fatalf("应在指定分块后停止，实际 %v", emitted)
		}
	})
}

func TestFakeStreamCallbackErrorStops(t *testing.T) {
	fake := NewFake(FakeStep{StreamChunks: []string{"甲", "乙", "丙"}})
	var emitted []string
	_, err := fake.Stream(context.Background(), validRequest(), func(text string) error {
		emitted = append(emitted, text)
		return errors.New("下游断开")
	})
	assertCode(t, err, CodeModelCanceled)
	if len(emitted) != 1 {
		t.Fatalf("回调错误后必须立即停止，实际 %d 次", len(emitted))
	}
}

func TestFakeStreamCanceledContextRecordsNothing(t *testing.T) {
	fake := NewFake()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := fake.Stream(ctx, validRequest(), func(string) error { return nil })
	assertCode(t, err, CodeModelCanceled)
	if fake.Calls() != 0 {
		t.Fatalf("已取消请求不得记录模型调用，实际 %d 次", fake.Calls())
	}
}

func TestFakeStreamDefaultEchoIsIncrementalAndValid(t *testing.T) {
	fake := NewFake()
	var emitted []string
	result, err := fake.Stream(context.Background(), validRequest(), func(text string) error {
		if text == "" {
			t.Fatal("增量不得为空字符串")
		}
		emitted = append(emitted, text)
		return nil
	})
	if err != nil {
		t.Fatalf("默认 Fake 流式不应报错: %v", err)
	}
	if len(emitted) < 2 {
		t.Fatalf("默认回声应分成多块，实际 %v", emitted)
	}
	if strings.Join(emitted, "") != result.Content {
		t.Fatalf("增量拼接与最终结果不一致: %v vs %q", emitted, result.Content)
	}
	if err := ValidateReply(result.Content); err != nil {
		t.Fatalf("Fake 流式结果必须通过同一回复校验: %v", err)
	}
	if result.Usage.TotalTokens.Known {
		t.Fatal("默认用量必须保持未知")
	}
}

func TestFakeStreamScriptedOversizedReplyFails(t *testing.T) {
	fake := NewFake(FakeStep{StreamChunks: []string{strings.Repeat("a", 1500), strings.Repeat("a", 501)}})
	_, err := fake.Stream(context.Background(), validRequest(), func(string) error { return nil })
	assertCode(t, err, CodeModelError)
}

// 脚本化的空回复必须像 Generate 一样受控失败，不能被静默替换成回声。
func TestFakeStreamScriptedEmptyReplyFails(t *testing.T) {
	fake := NewFake(FakeStep{Result: Result{Content: ""}})
	emitted := 0
	_, err := fake.Stream(context.Background(), validRequest(), func(string) error {
		emitted++
		return nil
	})
	assertCode(t, err, CodeModelError)
	if emitted != 0 {
		t.Fatalf("空回复失败前不得回调增量，实际 %d 次", emitted)
	}
}

// ---------- 审查修正的回归用例 ----------

// latency 必须覆盖完整 Recv/emit 过程，而不是只统计建连。
func TestOpenAIStreamLatencyCoversFullRead(t *testing.T) {
	const readDelay = 120 * time.Millisecond
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSEFrame(w, streamChunkFrame("assistant", "慢回复", nil, ""))
		time.Sleep(readDelay)
		writeSSEFrame(w, streamChunkFrame("", "", streamStop(), ""))
		writeSSEFrame(w, streamUsageFrame(streamUsageOK))
		writeSSEFrame(w, "[DONE]")
	})
	result, err := newSSEStreamer(t, provider).Stream(context.Background(), validRequest(), func(string) error {
		return nil
	})
	if err != nil {
		t.Fatalf("正常流式响应不应报错: %v", err)
	}
	if result.Metadata.LatencyMS < readDelay.Milliseconds() {
		t.Fatalf("latency 必须覆盖完整读取过程，实际 %dms 小于延迟 %dms",
			result.Metadata.LatencyMS, readDelay.Milliseconds())
	}
}

// EOF 也可能是“期限恰好到期/已取消”的表现：读完（含读到 EOF）之后必须先看
// callCtx.Err，否则会把超时/取消误判成“模型未正常结束”的普通模型错误。
// 用可控假模型：读取阻塞到 Context 结束之后才关闭流，确定性地只考察 EOF 分支。
func TestOpenAIStreamEOFNearDeadlineStaysTimeout(t *testing.T) {
	cases := []struct {
		name string
		// setup 返回本次 Stream 的父 Context，并在等待的时长后自行结束它。
		setup func(t *testing.T) context.Context
		want  ErrorCode
	}{
		{
			name: "读取中期限到期",
			setup: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
			want: CodeModelTimeout,
		},
		{
			name: "读取中调用方取消",
			setup: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				go func() {
					time.Sleep(40 * time.Millisecond)
					cancel()
				}()
				return ctx
			},
			want: CodeModelCanceled,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := tc.setup(t)
			reader, writer := schema.Pipe[*schema.Message](0)
			model := &scriptedChatModel{stream: func(callCtx context.Context) (*schema.StreamReader[*schema.Message], error) {
				go func() {
					// 等 Context 真正结束后再正常关闭流：Recv 返回纯 io.EOF，
					// 不夹带 Context 错误，从而确定性地只走到 EOF 之后的检查。
					<-callCtx.Done()
					writer.Close()
				}()
				return reader, nil
			}}
			adapter := newScriptedAdapter(model)

			result, err := adapter.Stream(ctx, validRequest(), func(string) error { return nil })
			if err == nil {
				t.Fatalf("Context 已结束不得返回成功结果: %+v", result)
			}
			assertCode(t, err, tc.want)
			if result.Content != "" {
				t.Fatalf("失败不得返回可用正文，实际 %q", result.Content)
			}
		})
	}
}

// newScriptedAdapter 构造仅用于单元测试的适配器：无网络、无 Key、丢弃日志。
func newScriptedAdapter(chatModel model.BaseChatModel) *OpenAIAdapter {
	cfg := testConfig(nil)
	cfg.Logger = slog.New(slog.DiscardHandler)
	return &OpenAIAdapter{chatModel: chatModel, config: cfg, logger: cfg.Logger}
}

// scriptedChatModel 是可控的 Eino 模型桩：Generate 不可用，Stream 由用例注入，
// 用于验证适配器自身（而非 SDK）的边界行为，不产生任何网络请求。
type scriptedChatModel struct {
	stream func(context.Context) (*schema.StreamReader[*schema.Message], error)
}

func (m *scriptedChatModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, errors.New("测试桩不支持普通生成")
}

func (m *scriptedChatModel) Stream(ctx context.Context, _ []*schema.Message,
	_ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.stream(ctx)
}

// sseObservationBody 的 Read/Close 并发必须安全，且 Close 不被慢速 Read 阻塞。
func TestSSEObservationBodyConcurrentReadClose(t *testing.T) {
	for range 200 {
		observation := &streamObservation{}
		base := &blockingReadCloser{release: make(chan struct{})}
		body := &sseObservationBody{base: base, observation: observation}
		readDone := make(chan struct{})
		go func() {
			defer close(readDone)
			buffer := make([]byte, 16)
			_, _ = body.Read(buffer)
		}()
		// Close 必须在读阻塞期间立即完成，否则上游取消会被卡住。
		closed := make(chan error, 1)
		go func() { closed <- body.Close() }()
		select {
		case err := <-closed:
			if err != nil {
				t.Fatalf("Close 不应报错: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("Close 被慢速 Read 阻塞")
		}
		base.releaseBase()
		<-readDone
		// 第二次 Close 必须幂等，且不再关闭底层。
		if err := body.Close(); err != nil {
			t.Fatalf("重复 Close 不应报错: %v", err)
		}
		if !body.closed.Load() {
			t.Fatal("Close 后 closed 标记必须为真")
		}
	}
}

// blockingReadCloser 模拟“供应商迟迟不返回数据”的慢速响应体。
type blockingReadCloser struct {
	release chan struct{}
	once    sync.Once
}

func (b *blockingReadCloser) Read(p []byte) (int, error) {
	<-b.release
	return 0, io.EOF
}

func (b *blockingReadCloser) Close() error {
	b.releaseBase()
	return nil
}

func (b *blockingReadCloser) releaseBase() { b.once.Do(func() { close(b.release) }) }

// 取消只能尽快止损（关闭上游），不保证不再计费；已产生的费用由调用方预留承担。
func TestOpenAIStreamCancelClosesUpstreamPromptly(t *testing.T) {
	upstreamClosed := make(chan struct{})
	provider := newSSEProvider(t, func(t *testing.T, w http.ResponseWriter, r *http.Request) {
		writeSSEFrame(w, streamChunkFrame("assistant", "第一段", nil, ""))
		select {
		case <-r.Context().Done():
			close(upstreamClosed)
		case <-time.After(3 * time.Second):
		}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := newSSEStreamer(t, provider).Stream(ctx, validRequest(), func(string) error {
		cancel()
		return nil
	})
	assertCode(t, err, CodeModelCanceled)
	select {
	case <-upstreamClosed:
	case <-time.After(3 * time.Second):
		t.Fatal("取消后未关闭上游连接")
	}
}
