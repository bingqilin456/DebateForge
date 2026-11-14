package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"unicode/utf8"
)

// SDK 会在空 choices 等情况下提前失败，必须在解码前保存计费事实。
// 观察状态通过单次调用的 Context 传递，不在共享 Transport 上保存请求数据。
type observationContextKey struct{}

// streamObservationContextKey 与 observationContextKey 互斥：同一请求要么
// 走整流观察（普通生成），要么走增量观察（流式生成），不会同时持有两者。
type streamObservationContextKey struct{}

type responseObserverTransport struct{ base http.RoundTripper }

const maxProviderResponseBytes = 1 << 20

var errInvalidProviderResponse = errors.New("模型响应超过大小限制")
var errInvalidStreamFrame = errors.New("模型流式响应帧无效")

func (t responseObserverTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	response, err := t.base.RoundTrip(req)
	if err != nil || response == nil {
		return response, err
	}
	// 普通生成：完整读取后交还给 SDK；流式生成：只包装 Body，绝不整流缓冲。
	if observation, ok := req.Context().Value(observationContextKey{}).(*responseObservation); ok {
		return observeJSONResponse(response, observation)
	}
	if observation, ok := req.Context().Value(streamObservationContextKey{}).(*streamObservation); ok {
		return observeStreamResponse(response, observation)
	}
	return response, nil
}

func observeJSONResponse(response *http.Response, observation *responseObservation) (*http.Response, error) {
	observation.httpStatus = response.StatusCode
	if response.Body == nil {
		observation.invalidBody = true
		return response, nil
	}
	raw, readErr := io.ReadAll(io.LimitReader(response.Body, maxProviderResponseBytes+1))
	_ = response.Body.Close()
	if readErr != nil {
		observation.invalidBody = true
		return nil, readErr
	}
	if len(raw) > maxProviderResponseBytes {
		observation.invalidBody = true
		return nil, errInvalidProviderResponse
	}
	*observation = parseResponseObservation(raw)
	observation.httpStatus = response.StatusCode
	observation.invalidUTF8 = !utf8.Valid(raw)
	// 原始字节继续交给官方 Eino/SDK 解析；本包只保留白名单事实，不留正文。
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, nil
}

// observeStreamResponse 只记录状态并包装 Body：读多少由官方适配器决定，
// 因此增量到达回调的时机由供应商真实输出决定，而不是整流完成后伪造。
func observeStreamResponse(response *http.Response, observation *streamObservation) (*http.Response, error) {
	observation.setHTTPStatus(response.StatusCode)
	if response.Body == nil {
		observation.markInvalidBody()
		return response, nil
	}
	response.Body = &sseObservationBody{base: response.Body, observation: observation}
	return response, nil
}

// streamObservation 是流式请求的白名单事实集合，由传输层随读随写、
// 由适配器在流结束后快照读取；用互斥锁保证跨 goroutine 读取安全。
type streamObservation struct {
	mu              sync.Mutex
	httpStatus      int
	invalidUTF8     bool
	invalidBody     bool
	oversize        bool
	sawDone         bool
	hasRole         bool
	role            string
	hasRefusal      bool
	hasToolCalls    bool
	hasFinishReason bool
	finishReason    string
	usage           Usage
}

type streamObservationSnapshot struct {
	httpStatus      int
	invalidUTF8     bool
	invalidBody     bool
	oversize        bool
	sawDone         bool
	hasRole         bool
	role            string
	hasRefusal      bool
	hasToolCalls    bool
	hasFinishReason bool
	finishReason    string
	usage           Usage
}

func (o *streamObservation) setHTTPStatus(status int) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.httpStatus = status
}

func (o *streamObservation) markInvalidUTF8() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.invalidUTF8 = true
}

func (o *streamObservation) markInvalidBody() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.invalidBody = true
}

func (o *streamObservation) markOversize() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.oversize = true
}

func (o *streamObservation) markDone() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sawDone = true
}

func (o *streamObservation) snapshot() streamObservationSnapshot {
	o.mu.Lock()
	defer o.mu.Unlock()
	return streamObservationSnapshot{
		httpStatus:      o.httpStatus,
		invalidUTF8:     o.invalidUTF8,
		invalidBody:     o.invalidBody,
		oversize:        o.oversize,
		sawDone:         o.sawDone,
		hasRole:         o.hasRole,
		role:            o.role,
		hasRefusal:      o.hasRefusal,
		hasToolCalls:    o.hasToolCalls,
		hasFinishReason: o.hasFinishReason,
		finishReason:    o.finishReason,
		usage:           o.usage,
	}
}

// applyLine 处理一行 SSE。只在完整行（以 \n 结尾）上校验 UTF-8：
// 多字节字符不可能包含 0x0A，因此按行校验既严格又不会把跨读拆分的字符误判。
func (o *streamObservation) applyLine(line []byte) error {
	line = bytes.TrimRight(line, "\r")
	if !utf8.Valid(line) {
		o.markInvalidUTF8()
		return errInvalidStreamFrame
	}
	trimmed := bytes.TrimSpace(line)
	// 空行是帧边界，冒号开头是注释（心跳），event:/id: 等字段与正文无关。
	if len(trimmed) == 0 || trimmed[0] == ':' || !bytes.HasPrefix(trimmed, []byte("data:")) {
		return nil
	}
	payload := bytes.TrimSpace(trimmed[len("data:"):])
	if len(payload) == 0 {
		return nil
	}
	if bytes.Equal(payload, []byte("[DONE]")) {
		o.markDone()
		return nil
	}
	if !o.applyFrame(payload) {
		o.markInvalidBody()
		return errInvalidStreamFrame
	}
	return nil
}

// applyFrame 只解析本包需要的白名单事实：usage 字段是否存在、角色、
// 拒答、工具调用与 finish_reason；不保存任何正文。
func (o *streamObservation) applyFrame(payload []byte) bool {
	var frame struct {
		Usage   json.RawMessage `json:"usage"`
		Choices []struct {
			Index        json.RawMessage `json:"index"`
			FinishReason json.RawMessage `json:"finish_reason"`
			Delta        struct {
				Role      json.RawMessage   `json:"role"`
				Refusal   json.RawMessage   `json:"refusal"`
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"delta"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(payload, &frame); err != nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	// 缺失 usage 保持未知；只有供应商真的返回了 usage 字段才覆盖（含显式 0）。
	if hasJSONField(frame.Usage) {
		o.usage = parseUsage(frame.Usage)
	}
	for _, choice := range frame.Choices {
		if choiceIndex(choice.Index) != 0 {
			continue
		}
		if role, present := decodeStringField(choice.Delta.Role); present {
			o.hasRole, o.role = true, role
		}
		if refusal, present := decodeStringField(choice.Delta.Refusal); present && refusal != "" {
			o.hasRefusal = true
		}
		if len(choice.Delta.ToolCalls) > 0 {
			o.hasToolCalls = true
		}
		if reason, present := decodeStringField(choice.FinishReason); present {
			o.hasFinishReason, o.finishReason = true, reason
		}
		break
	}
	return true
}

// hasJSONField 区分“字段缺失/null”与“显式零值”。
func hasJSONField(raw json.RawMessage) bool {
	return len(raw) > 0 && !bytes.Equal(bytes.TrimSpace(raw), []byte("null"))
}

// sseObservationBody 是流式响应体的透明观察包装：每次 Read 只把已到达的
// 字节交给观察器，读到多少转交多少，不额外缓冲、不读取全部正文。
//
// 并发约束：Read 在官方 SDK 的读取 goroutine 中执行，Close 可能来自适配器的
// 取消路径，两者会并发。closed 用 atomic 标记、底层关闭用 sync.Once 保证只执行
// 一次；b.mu 只保护缓冲与计数，且绝不在阻塞的 base.Read 期间持有，
// 因此 Close 不会被慢速供应商读取卡住。
type sseObservationBody struct {
	base        io.ReadCloser
	observation *streamObservation
	closed      atomic.Bool
	closeOnce   sync.Once
	closeErr    error
	mu          sync.Mutex
	total       int
	pending     []byte
}

func (b *sseObservationBody) Read(p []byte) (int, error) {
	if b.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	// 不持锁执行阻塞读取：Close 必须能在读过程中取消上游连接。
	n, err := b.base.Read(p)
	if b.closed.Load() {
		// Close 与 Read 竞争时丢弃本次字节，不再解析已废弃的响应。
		return 0, io.ErrClosedPipe
	}
	if n > 0 {
		if feedErr := b.consume(p[:n], false); feedErr != nil {
			_ = b.Close()
			return 0, feedErr
		}
	}
	if errors.Is(err, io.EOF) {
		// 没有以换行结尾的最后一帧也要处理，否则会漏掉 [DONE]。
		if flushErr := b.consume(nil, true); flushErr != nil {
			_ = b.Close()
			return n, flushErr
		}
	}
	return n, err
}

// consume 把新字节并入缓冲并逐行解析。锁内只做内存拼接与切行，
// 解析放在锁外，避免 Close 被解析工作拖延。
func (b *sseObservationBody) consume(chunk []byte, eof bool) error {
	if len(chunk) > 0 {
		b.mu.Lock()
		b.total += len(chunk)
		oversize := b.total > maxProviderResponseBytes
		if !oversize {
			b.pending = append(b.pending, chunk...)
		}
		b.mu.Unlock()
		if oversize {
			b.observation.markOversize()
			return errInvalidProviderResponse
		}
	}
	for {
		b.mu.Lock()
		index := bytes.IndexByte(b.pending, '\n')
		var line, tail []byte
		if index >= 0 {
			line = append(line, b.pending[:index]...)
			b.pending = b.pending[index+1:]
		} else if eof {
			tail = append(tail, b.pending...)
			b.pending = nil
		}
		b.mu.Unlock()
		if index >= 0 {
			if err := b.observation.applyLine(line); err != nil {
				return err
			}
			continue
		}
		if len(bytes.TrimSpace(tail)) > 0 {
			return b.observation.applyLine(tail)
		}
		return nil
	}
}

// Close 幂等：只有第一次真正关闭底层响应体，其余调用返回同一结果。
func (b *sseObservationBody) Close() error {
	b.closeOnce.Do(func() {
		b.closed.Store(true)
		b.closeErr = b.base.Close()
	})
	return b.closeErr
}
