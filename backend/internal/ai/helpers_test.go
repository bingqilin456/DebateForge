package ai

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// ptrInt 返回指向 v 的指针，用于区分“字段缺失”与“显式 0”。
func ptrInt(v int) *int { return &v }

// validRequest 构造一轮可以合法发起生成的最小请求，测试按需覆盖字段。
func validRequest() Request {
	return Request{
		Topic: Topic{
			Title:       "大学课程是否应采用开放式考核",
			Description: "讨论学习效果与评价公平",
		},
		UserStance:  StancePro,
		RoundNo:     1,
		UserContent: "开放式考核可以评估实际应用能力。",
	}
}

// wireRecord 记录测试 RoundTripper 观测到的真实 HTTP 请求内容。
type wireRecord struct {
	Method     string
	Path       string
	Header     http.Header
	Body       string
	AttemptCtx bool // 底层是否观测到请求自带的 Context
}

// scriptedTransport 是无网络 RoundTripper：按脚本返回响应或错误，并记录每次请求。
type scriptedTransport struct {
	mu        sync.Mutex
	records   []wireRecord
	handler   func(req *http.Request, attempt int) (*http.Response, error)
	cancelled bool
}

func newScriptedTransport(handler func(req *http.Request, attempt int) (*http.Response, error)) *scriptedTransport {
	return &scriptedTransport{handler: handler}
}

func (s *scriptedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
		body = string(raw)
		// 恢复 body，供 handler 再次读取，避免测试里重复读取到空内容。
		req.Body = io.NopCloser(strings.NewReader(body))
	}
	s.mu.Lock()
	s.records = append(s.records, wireRecord{
		Method:     req.Method,
		Path:       req.URL.Path,
		Header:     req.Header.Clone(),
		Body:       body,
		AttemptCtx: req.Context() != nil,
	})
	attempt := len(s.records)
	s.mu.Unlock()
	return s.handler(req, attempt)
}

func (s *scriptedTransport) attempts() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.records)
}

func (s *scriptedTransport) record(t *testing.T, index int) wireRecord {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if index < 0 || index >= len(s.records) {
		t.Fatalf("请求记录不足：希望访问第 %d 次请求，实际共 %d 次", index, len(s.records))
	}
	return s.records[index]
}

func (s *scriptedTransport) markCancelled() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelled = true
}

func (s *scriptedTransport) observedCancel() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancelled
}

// jsonResponse 构造一个完整的 HTTP 响应，用于驱动官方适配器真实解析。
func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     http.StatusText(status),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

// loopbackServer 是真实 HTTP 回环服务，用于验证传输层真的被官方适配器使用。
type loopbackServer struct {
	*httptest.Server
	hitsCount int32
}

func newLoopbackServer(t *testing.T) *loopbackServer {
	t.Helper()
	server := &loopbackServer{}
	server.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&server.hitsCount, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"chatcmpl-loop","object":"chat.completion","created":1,` +
			`"model":"deepseek-flash","choices":[{"index":0,` +
			`"message":{"role":"assistant","content":"回环回复"},"finish_reason":"stop"}]}`))
	}))
	return server
}

func (s *loopbackServer) hits() int { return int(atomic.LoadInt32(&s.hitsCount)) }

// ioCopy 是 io.Copy 的薄封装，避免测试文件为此引入额外导入顺序问题。
func ioCopy(dst *strings.Builder, src io.Reader) (int64, error) {
	return io.Copy(dst, src)
}

// newBufferLogger 返回写入 buffer 的 JSON 日志器，用于断言白名单字段。
func newBufferLogger(buffer *strings.Builder) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// lastLogEntry 解析最后一条 JSON 日志，用于按字段断言。
func lastLogEntry(t *testing.T, logged string) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(logged), "\n")
	if len(lines) == 0 {
		t.Fatal("没有日志输出")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("日志不是 JSON: %v\n%s", err, lines[len(lines)-1])
	}
	return entry
}
