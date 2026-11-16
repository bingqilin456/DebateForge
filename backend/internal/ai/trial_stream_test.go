package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// 本文件只使用进程内桩传输与合成 SSE 帧，不发起任何真实模型调用。
// 目录持久同步（dir.Sync）只在 Linux 部署目标可用，账本落盘断言按既有约定跳过，
// 其余白名单、Fail closed 与包装委托在所有平台运行。

// ---------- 桩传输 ----------

// trialSSETransport 是合成流式供应商：按 SSE 帧返回，并记录实际出网请求体。
type trialSSETransport struct {
	mu       sync.Mutex
	bodies   []string
	response func(req *http.Request) (*http.Response, error)
}

func (s *trialSSETransport) RoundTrip(req *http.Request) (*http.Response, error) {
	raw, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(raw))
	s.mu.Lock()
	s.bodies = append(s.bodies, string(raw))
	s.mu.Unlock()
	return s.response(req)
}

func (s *trialSSETransport) calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}

func (s *trialSSETransport) body(index int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if index >= len(s.bodies) {
		return ""
	}
	return s.bodies[index]
}

func sseResponse(frames ...string) *http.Response {
	var body strings.Builder
	for _, frame := range frames {
		body.WriteString("data: " + frame + "\n\n")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body.String())),
	}
}

// trialStreamChunk 构造带 assistant 角色的增量帧：真实适配器要求角色可识别，
// 否则会按“模型响应角色无效”受控失败。
func trialStreamChunk(content string, finish *string) string {
	return streamChunkFrame("assistant", content, finish, "")
}

func trialStreamRequest() Request {
	return Request{UserID: 1, Mode: ModeQuick3, Topic: Topic{Title: "合成辩题", Description: "合成描述"},
		UserStance: StancePro, RoundNo: 1, UserContent: "合成发言"}
}

// trialStreamingLedger 构造已批准、可预留的最小账本环境（需要目录持久同步）。
func trialStreamingLedger(t *testing.T) (*trialLedger, trialWindow, time.Time) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("账本目录持久同步只在 Linux 部署目标验证，与既有试运行用例一致")
	}
	root := t.TempDir()
	now := time.Now().In(time.FixedZone("CST", 8*3600)).Truncate(time.Second)
	start := now.Add(-time.Minute)
	window := trialWindow{Start: start, End: start.Add(2 * time.Hour), Users: []uint64{1, 2}}
	put := func(name string, value any) {
		t.Helper()
		if err := writeTrialRecord(root, name, value); err != nil {
			t.Fatal(err)
		}
	}
	put("window.json", window)
	put("isolated-approval.json", trialApproval{
		BootID: "local", WindowStart: start, MonitorAt: now, Start: now, End: window.End, Users: window.Users,
	})
	put("monitor", struct{}{})
	if err := os.Chtimes(filepath.Join(root, "monitor"), now, now); err != nil {
		t.Fatal(err)
	}
	ledger := &trialLedger{root: root, stage: isolatedTrial, bootID: "local", window: window,
		alert: io.Discard, now: func() time.Time { return now }}
	return ledger, window, now
}

func newTrialStreamGenerator(t *testing.T, ledger *trialLedger, transport http.RoundTripper) trialGenerator {
	t.Helper()
	cfg := testConfig(transport)
	cfg.MaxOutputTokens, cfg.Window.MaxOutputTokens = trialOutputTokens, trialOutputTokens
	adapter, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatalf("构造适配器失败: %v", err)
	}
	return trialGenerator{adapter: adapter, ledger: ledger}
}

func trialCallContext(user uint64) context.Context {
	return context.WithValue(context.Background(), trialCallKey{}, &trialCall{user: user})
}

// streamlessGenerator 只实现 Generator，用于验证试运行包装对不支持的生成器 fail closed。
type streamlessGenerator struct{ calls int }

func (g *streamlessGenerator) Generate(context.Context, Request) (Result, error) {
	g.calls++
	return Result{Content: "整流回复", Metadata: Metadata{PromptVersion: PromptVersion, FinishReason: "stop"}}, nil
}

// ---------- 接口与委托 ----------

// 关键保护：试运行包装必须真的暴露流式能力，否则禁用/降级会被静默绕过。
func TestTrialGeneratorExposesStream(t *testing.T) {
	var _ StreamGenerator = trialGenerator{}
	var _ StreamGenerator = (*Fake)(nil)
	var _ StreamGenerator = (*OpenAIAdapter)(nil)
}

func TestTrialStreamDelegatesToInnerStreamer(t *testing.T) {
	fake := NewFake(FakeStep{StreamChunks: []string{"甲", "乙"}})
	generator := trialGenerator{adapter: fake, ledger: &trialLedger{root: t.TempDir(), stage: isolatedTrial}}
	var emitted []string
	result, err := generator.Stream(trialCallContext(1), trialStreamRequest(), func(text string) error {
		emitted = append(emitted, text)
		return nil
	})
	if err != nil {
		t.Fatalf("内层支持流式时应成功: %v", err)
	}
	if strings.Join(emitted, "") != "甲乙" || result.Content != "甲乙" {
		t.Fatalf("增量与结果必须来自内层真实流式: emitted=%v result=%q", emitted, result.Content)
	}
}

// 内层不支持流式时必须 fail closed：不能退回普通生成整流后伪造 delta。
func TestTrialStreamFailsClosedWithoutInnerStream(t *testing.T) {
	inner := &streamlessGenerator{}
	generator := trialGenerator{adapter: inner, ledger: &trialLedger{root: t.TempDir(), stage: isolatedTrial}}
	emitted := 0
	_, err := generator.Stream(trialCallContext(1), trialStreamRequest(), func(string) error {
		emitted++
		return nil
	})
	if err == nil {
		t.Fatal("内层不支持流式时必须受控失败")
	}
	if emitted != 0 {
		t.Fatalf("不得伪造增量回调，实际 %d 次", emitted)
	}
}

// ---------- 请求白名单 ----------

// validTrialPayloadForStream 按“调用方声明的传输形态”核对白名单：
// 实际请求体必须与声明一致，不能出现非流式请求被按流式放行或反之。
func validTrialPayloadForStream(raw []byte, stream bool) bool {
	var payload struct {
		Stream bool `json:"stream"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Stream != stream {
		return false
	}
	return validTrialPayload(raw)
}

func TestTrialTransportStreamWhitelist(t *testing.T) {
	const messages = `[{"role":"system","content":"s"},{"role":"user","content":"u"}]`
	base := `{"model":"deepseek-flash","max_tokens":1024,"thinking":{"type":"disabled"},"messages":` + messages
	streamValid := base + `,"stream":true,"stream_options":{"include_usage":true}}`
	nonStreamValid := base + `}`
	cases := []struct {
		name    string
		payload string
		stream  bool
		want    bool
	}{
		{name: "流式合法", payload: streamValid, stream: true, want: true},
		{name: "流式缺 include_usage", payload: base + `,"stream":true}`, stream: true},
		{name: "流式 include_usage 为假", payload: base + `,"stream":true,"stream_options":{"include_usage":false}}`, stream: true},
		{name: "流式却标记为非流式", payload: streamValid, stream: false},
		{name: "非流式合法", payload: nonStreamValid, stream: false, want: true},
		{name: "非流式却带流式字段", payload: streamValid, stream: false},
		{name: "流式工具字段", payload: base + `,"tools":[],"stream":true,"stream_options":{"include_usage":true}}`, stream: true},
		{name: "流式模型不符", payload: strings.Replace(streamValid, "deepseek-flash", "deepseek-chat", 1), stream: true},
		{name: "流式输出上限不符", payload: strings.Replace(streamValid, `"max_tokens":1024`, `"max_tokens":2048`, 1), stream: true},
		{name: "流式思考开启", payload: strings.Replace(streamValid, `"disabled"`, `"enabled"`, 1), stream: true},
		{name: "流式单消息", payload: `{"model":"deepseek-flash","max_tokens":1024,"thinking":{"type":"disabled"},` +
			`"messages":[{"role":"user","content":"u"}],"stream":true,"stream_options":{"include_usage":true}}`, stream: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := validTrialPayloadForStream([]byte(tc.payload), tc.stream); got != tc.want {
				t.Fatalf("validTrialPayloadForStream=%v，期望 %v", got, tc.want)
			}
		})
	}
}

// 固定目标、模型、输出上限、两消息与输入字节上限在流式下不得放宽。
func TestTrialStreamTransportFailsClosed(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stream     bool
		reserveErr bool
		oversize   bool
		wantEgress int
	}{
		{name: "流式合法", stream: true, wantEgress: 1},
		{name: "流式额度不可用", stream: true, reserveErr: true, wantEgress: 0},
		{name: "非流式仍走原路径", stream: false, wantEgress: 1},
		{name: "流式超长输入", stream: true, oversize: true, wantEgress: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := &trialSSETransport{response: func(*http.Request) (*http.Response, error) {
				return sseResponse("[DONE]"), nil
			}}
			reserve := func(context.Context) error { return nil }
			if tc.reserveErr {
				reserve = func(context.Context) error { return errors.New("stub ledger unavailable") }
			}
			guard := trialTransport{base: base, reserve: reserve}
			content := "u"
			if tc.oversize {
				content = strings.Repeat("x", trialInputTokens)
			}
			payload := `{"model":"deepseek-flash","max_tokens":1024,"thinking":{"type":"disabled"},` +
				`"messages":[{"role":"system","content":"s"},{"role":"user","content":"` + content + `"}]`
			if tc.stream {
				payload += `,"stream":true,"stream_options":{"include_usage":true}}`
			} else {
				payload += `}`
			}
			req, err := http.NewRequestWithContext(trialCallContext(1), http.MethodPost,
				"https://api.deepseek.com/chat/completions", strings.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			resp, err := guard.RoundTrip(req)
			if err == nil {
				_ = resp.Body.Close()
			}
			if tc.wantEgress == 0 && err == nil {
				t.Fatal("非法或额度不可用的请求必须受控失败")
			}
			if tc.wantEgress == 1 && err != nil {
				t.Fatalf("合法请求不应被拒绝: %v", err)
			}
			if got := base.calls(); got != tc.wantEgress {
				t.Fatalf("出网次数 = %d，期望 %d", got, tc.wantEgress)
			}
		})
	}
}

// 关键跨平台保护：官方适配器真实发出的流式请求体必须通过试运行白名单，
// 否则流式会被直接拒绝（或缺 include_usage 导致无法核对计费）。不触碰账本落盘。
func TestTrialTransportAcceptsRealAdapterStreamRequest(t *testing.T) {
	base := &trialSSETransport{response: func(*http.Request) (*http.Response, error) {
		return sseResponse(
			trialStreamChunk("合成回复", nil),
			trialStreamChunk("", streamStop()),
			streamUsageFrame(`{"prompt_tokens":31,"completion_tokens":8,"total_tokens":39}`),
			"[DONE]"), nil
	}}
	var reserved int
	guard := trialTransport{base: base, reserve: func(context.Context) error {
		reserved++
		if reserved > 1 {
			return errors.New("stub ledger already reserved")
		}
		return nil
	}}
	cfg := testConfig(guard)
	cfg.MaxOutputTokens, cfg.Window.MaxOutputTokens = trialOutputTokens, trialOutputTokens
	adapter, err := NewOpenAIGenerator(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	streamer, ok := adapter.(StreamGenerator)
	if !ok {
		t.Fatal("真实适配器必须实现流式")
	}
	var emitted []string
	result, err := streamer.Stream(trialCallContext(1), trialStreamRequest(), func(text string) error {
		emitted = append(emitted, text)
		return nil
	})
	if err != nil {
		t.Fatalf("真实适配器的流式请求必须通过试运行白名单: %v", err)
	}
	if reserved != 1 {
		t.Fatalf("必须恰好预留一次，实际 %d 次", reserved)
	}
	if base.calls() != 1 {
		t.Fatalf("必须恰好出网一次，实际 %d 次", base.calls())
	}
	if strings.Join(emitted, "") != result.Content {
		t.Fatalf("增量拼接与最终结果不一致: %v vs %q", emitted, result.Content)
	}
	if !saneTrialUsage(result.Usage) {
		t.Fatalf("流式 usage 必须可用于计费核对，实际 %+v", result.Usage)
	}
	payload := base.body(0)
	for _, required := range []string{`"stream":true`, `"include_usage":true`, `"model":"deepseek-flash"`,
		`"max_tokens":1024`, `"type":"disabled"`} {
		if !strings.Contains(payload, required) {
			t.Fatalf("真实流式请求体缺少 %s: %s", required, payload)
		}
	}
}

// 旧试运行限制下完整多轮仍被拒绝，属已知边界；本切片不得为此旁路。
func TestTrialStreamStillRejectsFullMultiRound(t *testing.T) {
	multi := `{"model":"deepseek-flash","max_tokens":1024,"thinking":{"type":"disabled"},` +
		`"messages":[{"role":"system","content":"s"},{"role":"user","content":"u1"},` +
		`{"role":"assistant","content":"a1"},{"role":"user","content":"u2"}],` +
		`"stream":true,"stream_options":{"include_usage":true}}`
	if validTrialPayloadForStream([]byte(multi), true) {
		t.Fatal("完整多轮流式请求不得被白名单放行")
	}

	base := &trialSSETransport{response: func(*http.Request) (*http.Response, error) {
		t.Error("多轮请求不得出网")
		return nil, errors.New("unexpected")
	}}
	guard := trialTransport{base: base, reserve: func(context.Context) error { return nil }}
	req, err := http.NewRequestWithContext(trialCallContext(1), http.MethodPost,
		"https://api.deepseek.com/chat/completions", strings.NewReader(multi))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.RoundTrip(req); err == nil {
		t.Fatal("多轮请求必须被试运行传输拒绝")
	}
	if base.calls() != 0 {
		t.Fatalf("被拒绝的多轮请求不得出网，实际 %d 次", base.calls())
	}
}

// ---------- 账本与结果路径（Linux） ----------

func TestTrialStreamReservesOnceAndRecordsResult(t *testing.T) {
	ledger, _, _ := trialStreamingLedger(t)
	base := &trialSSETransport{response: func(*http.Request) (*http.Response, error) {
		return sseResponse(
			trialStreamChunk("合成回复", nil),
			trialStreamChunk("", streamStop()),
			streamUsageFrame(`{"prompt_tokens":31,"completion_tokens":8,"total_tokens":39}`),
			"[DONE]"), nil
	}}
	generator := newTrialStreamGenerator(t, ledger, trialTransport{base: base, reserve: ledger.reserve})

	var emitted []string
	result, err := generator.Stream(trialCallContext(1), trialStreamRequest(), func(text string) error {
		emitted = append(emitted, text)
		return nil
	})
	if err != nil {
		t.Fatalf("合法试运行流式调用不应报错: %v", err)
	}
	if strings.Join(emitted, "") != result.Content {
		t.Fatalf("增量拼接与最终结果不一致: %v vs %q", emitted, result.Content)
	}
	// 白名单必须按真实流式字段放行，同时保留固定目标与费用约束。
	payload := base.body(0)
	if !strings.Contains(payload, `"stream":true`) || !strings.Contains(payload, `"include_usage":true`) {
		t.Fatalf("实际出网请求缺少流式协议字段: %s", payload)
	}
	reserved, reserveErr := readTrialRecord[trialReservation](filepath.Join(ledger.root, "isolated-reserved.json"))
	if reserveErr != nil || reserved.Calls != 1 || reserved.Input != trialInputTokens ||
		reserved.Output != trialOutputTokens || reserved.MaxCostMicroCNY != 40960 {
		t.Fatalf("必须原子单次预留固定额度，实际 %+v %v", reserved, reserveErr)
	}
	record, resultErr := readTrialRecord[trialResult](filepath.Join(ledger.root, "isolated-result.json"))
	if resultErr != nil || record.Outcome != "success" || !saneTrialUsage(record.Usage) {
		t.Fatalf("必须记录成功结果，实际 %+v %v", record, resultErr)
	}
	if _, err := readTrialCompletion(ledger.root, isolatedTrial); err != nil {
		t.Fatalf("必须发布完成标记: %v", err)
	}
	// 旧限制下账本只能成功一次：第二次调用必须被拒绝且不出网、不重置账本。
	if _, err := generator.Stream(trialCallContext(1), trialStreamRequest(), func(string) error { return nil }); err == nil {
		t.Fatal("同一账本不得允许第二次生成")
	}
	if base.calls() != 1 {
		t.Fatalf("被拒绝的调用不得出网，实际 %d 次", base.calls())
	}
}

func TestTrialStreamFailureStillRecordsHaltedResult(t *testing.T) {
	ledger, _, _ := trialStreamingLedger(t)
	base := &trialSSETransport{response: func(*http.Request) (*http.Response, error) {
		// 供应商在缺尾帧的情况下结束：必须受控失败且保留预留、记录 halted。
		return sseResponse(trialStreamChunk("部分回复", streamStop())), nil
	}}
	generator := newTrialStreamGenerator(t, ledger, trialTransport{base: base, reserve: ledger.reserve})
	var emitted []string
	_, err := generator.Stream(trialCallContext(1), trialStreamRequest(), func(text string) error {
		emitted = append(emitted, text)
		return nil
	})
	if err == nil {
		t.Fatal("缺尾帧必须受控失败")
	}
	if len(emitted) == 0 {
		t.Fatal("失败前已到达的临时增量应已回调")
	}
	if _, reserveErr := readTrialRecord[trialReservation](filepath.Join(ledger.root, "isolated-reserved.json")); reserveErr != nil {
		t.Fatal("失败也必须保留已产生的预留")
	}
	record, resultErr := readTrialRecord[trialResult](filepath.Join(ledger.root, "isolated-result.json"))
	if resultErr != nil || record.Outcome != "halted" {
		t.Fatalf("失败必须记录 halted 结果，实际 %+v %v", record, resultErr)
	}
	if _, err := os.Stat(filepath.Join(ledger.root, "stop")); err != nil {
		t.Fatalf("失败必须写入 stop 标记: %v", err)
	}
}

func TestTrialStreamCallbackErrorDoesNotRecordSuccess(t *testing.T) {
	ledger, _, _ := trialStreamingLedger(t)
	base := &trialSSETransport{response: func(*http.Request) (*http.Response, error) {
		return sseResponse(
			trialStreamChunk("第一段", nil),
			trialStreamChunk("第二段", streamStop()),
			streamUsageFrame(`{"prompt_tokens":31,"completion_tokens":8,"total_tokens":39}`),
			"[DONE]"), nil
	}}
	generator := newTrialStreamGenerator(t, ledger, trialTransport{base: base, reserve: ledger.reserve})
	_, err := generator.Stream(trialCallContext(1), trialStreamRequest(), func(string) error {
		return errors.New("下游断开")
	})
	if err == nil {
		t.Fatal("回调失败必须受控失败")
	}
	record, resultErr := readTrialRecord[trialResult](filepath.Join(ledger.root, "isolated-result.json"))
	if resultErr == nil && record.Outcome == "success" {
		t.Fatal("回调失败不得记录成功结果")
	}
	if _, err := readTrialCompletion(ledger.root, isolatedTrial); err == nil {
		t.Fatal("回调失败不得发布完成标记")
	}
}
