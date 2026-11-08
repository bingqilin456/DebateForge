package ai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReviewRejectsNonAssistantWireRole(t *testing.T) {
	for _, role := range []string{"user", "system", "tool", "alien", "", "missing"} {
		t.Run(role, func(t *testing.T) {
			body := chatCompletionJSON("回复正文", "stop", `{"total_tokens":4}`)
			if role == "missing" {
				body = strings.Replace(body, `"role":"assistant",`, "", 1)
			} else {
				body = strings.Replace(body, `"role":"assistant"`, `"role":`+jsonString(role), 1)
			}
			transport := newScriptedTransport(func(*http.Request, int) (*http.Response, error) {
				return jsonResponse(http.StatusOK, body), nil
			})
			result, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
			assertCode(t, err, CodeModelError)
			if result.Content != "" || transport.attempts() != 1 {
				t.Fatal("非法角色必须一次失败且不返回正文")
			}
		})
	}
}

func TestReviewPreservesUsageBeforeSDKChoiceFailure(t *testing.T) {
	for _, choices := range []string{`[]`, `[{"index":1,"message":{"role":"assistant","content":"回复"},"finish_reason":"stop"}]`} {
		t.Run(choices, func(t *testing.T) {
			body := `{"choices":` + choices + `,"usage":{"prompt_tokens":4,"completion_tokens":0,"total_tokens":4}}`
			transport := newScriptedTransport(func(*http.Request, int) (*http.Response, error) {
				return jsonResponse(http.StatusOK, body), nil
			})
			_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
			var controlled *Error
			if !errors.As(err, &controlled) {
				t.Fatalf("缺少 index=0 应受控失败: %v", err)
			}
			if !controlled.Usage.TotalTokens.Known || controlled.Usage.TotalTokens.Value != 4 ||
				!controlled.Usage.CompletionTokens.Known || controlled.Usage.CompletionTokens.Value != 0 {
				t.Fatalf("失败不能丢失供应商已返回的计费用量: %+v", controlled.Usage)
			}
		})
	}
}

func TestReviewClassifiesStatusWithoutJSONErrorObject(t *testing.T) {
	for _, tc := range []struct {
		status int
		code   ErrorCode
	}{{402, CodeQuotaExceeded}, {429, CodeRateLimited}, {503, CodeModelUnavailable}} {
		transport := newScriptedTransport(func(*http.Request, int) (*http.Response, error) {
			return jsonResponse(tc.status, "upstream rejected request with private details"), nil
		})
		_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
		assertCode(t, err, tc.code)
		if strings.Contains(err.Error(), "private details") || transport.attempts() != 1 {
			t.Fatal("状态分类必须脱敏且不重试")
		}
	}
}

func TestReviewRejectsInvalidWireUTF8(t *testing.T) {
	body := strings.Replace(chatCompletionJSON("正常", "stop", `{"total_tokens":4}`), "正常", string([]byte{0xff}), 1)
	transport := newScriptedTransport(func(*http.Request, int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, body), nil
	})
	_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	assertCode(t, err, CodeModelError)
}

func TestReviewContentFilterMetadataRecordsRefusal(t *testing.T) {
	transport := newScriptedTransport(func(*http.Request, int) (*http.Response, error) {
		return jsonResponse(http.StatusOK, chatCompletionJSON("", "content_filter", "")), nil
	})
	_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	var controlled *Error
	if !errors.As(err, &controlled) || !controlled.Metadata.Refused {
		t.Fatalf("内容过滤必须在受控元数据中保留拒答标志: %v", err)
	}
}

func TestReviewBodyReadTimeoutStaysTimeout(t *testing.T) {
	transport := newScriptedTransport(func(req *http.Request, _ int) (*http.Response, error) {
		response := jsonResponse(http.StatusOK, "")
		response.Body = waitingContextBody{ctx: req.Context()}
		return response, nil
	})
	cfg := testConfig(transport)
	cfg.HTTPClient.Timeout = 25 * time.Millisecond
	generator, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = generator.Generate(context.Background(), validRequest())
	assertCode(t, err, CodeModelTimeout)
	if transport.attempts() != 1 {
		t.Fatal("Body 读取超时不得重试")
	}
}

type waitingContextBody struct{ ctx context.Context }

func (b waitingContextBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b waitingContextBody) Close() error { return nil }

func TestReviewBoundsProviderResponseBody(t *testing.T) {
	body := chatCompletionJSON(strings.Repeat("x", 2<<20), "stop", "")
	reader := &countingResponseBody{Reader: strings.NewReader(body)}
	transport := newScriptedTransport(func(*http.Request, int) (*http.Response, error) {
		response := jsonResponse(http.StatusOK, "")
		response.Body = reader
		return response, nil
	})
	_, err := newTestGenerator(t, transport).Generate(context.Background(), validRequest())
	assertCode(t, err, CodeModelError)
	if reader.bytesRead > (1<<20)+1 || !reader.closed {
		t.Fatalf("响应必须有界读取并关闭: bytes=%d closed=%v", reader.bytesRead, reader.closed)
	}
}

type countingResponseBody struct {
	io.Reader
	bytesRead int
	closed    bool
}

func (r *countingResponseBody) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.bytesRead += n
	return n, err
}

func (r *countingResponseBody) Close() error { r.closed = true; return nil }
