package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"
)

// NewDemoGenerator 仅由专用 demo 装配调用；公网 T07 入口保持原规则。
// transport 不接受调用方注入，TCP 只能经过隔离中继且 TLS 仍验证供应商。
func NewDemoGenerator(ctx context.Context, cfg OpenAIConfig, root string) (Generator, error) {
	if cfg.HTTPClient != nil || cfg.BaseURL != "https://api.deepseek.com" {
		return nil, errDemoDenied
	}
	approval, err := readDemoRecord[DemoApproval](root, "approval.json")
	if err != nil {
		return nil, errDemoDenied
	}
	l, err := openDemoLedger(root, approval)
	if err != nil {
		return nil, errDemoDenied
	}
	cfg.Model = approval.Model
	cfg.MaxOutputTokens = int(approval.PerCallOutputTokens)
	cfg.Window.MaxOutputTokens = cfg.MaxOutputTokens
	dialer := &net.Dialer{Timeout: 5 * time.Second}
	base := &http.Transport{DisableKeepAlives: true, TLSHandshakeTimeout: 5 * time.Second, DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" || addr != "api.deepseek.com:443" {
			return nil, errDemoDenied
		}
		return dialer.DialContext(ctx, "tcp", "trial-relay:8443")
	}}
	g, err := newDemoGenerator(ctx, cfg, l, base)
	if err != nil {
		l.close()
		return nil, err
	}
	// 进程退出自动关闭锁；未结算记录在下次启动被识别为未知并持久停用。
	go func() { <-ctx.Done(); _ = l.close(); base.CloseIdleConnections() }()
	return g, nil
}

type demoGenerator struct {
	adapter *OpenAIAdapter
	ledger  *demoLedger
}

func newDemoGenerator(ctx context.Context, cfg OpenAIConfig, l *demoLedger, base http.RoundTripper) (*demoGenerator, error) {
	if cfg.Model != l.approval.Model || int64(cfg.MaxOutputTokens) != l.approval.PerCallOutputTokens || base == nil {
		return nil, errDemoDenied
	}
	cfg.HTTPClient = &http.Client{Timeout: DefaultModelTimeout, Transport: demoTransport{base: base, ledger: l}}
	adapter, err := NewOpenAIGenerator(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return &demoGenerator{adapter: adapter.(*OpenAIAdapter), ledger: l}, nil
}

// 每次调用的控制观察器只负责停用/窗口/配置变化的取消，费用仍在实际 RoundTrip 前预留。
func (g *demoGenerator) call(ctx context.Context, user uint64) (context.Context, *demoCall, func(), error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	g.ledger.mu.Lock()
	err := g.ledger.checkLocked(user, DefaultModelTimeout)
	g.ledger.mu.Unlock()
	if err != nil {
		return nil, nil, nil, err
	}
	call := &demoCall{user: user}
	callCtx, cancel := context.WithCancel(context.WithValue(ctx, demoCallKey{}, call))
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-callCtx.Done():
				return
			case <-ticker.C:
				g.ledger.mu.Lock()
				err := g.ledger.checkLocked(user, 0)
				g.ledger.mu.Unlock()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()
	return callCtx, call, func() { cancel(); <-done }, nil
}
func (g *demoGenerator) settle(ctx context.Context, c *demoCall, usage Usage, err error) error {
	var failure *Error
	if errors.As(err, &failure) {
		usage = failure.Usage
	}
	if err == nil {
		err = ctx.Err()
	}
	if finishErr := g.ledger.finish(c, usage, err); finishErr != nil {
		// 费用停用不改变适配器已给出的超时/取消等受控业务分类。
		var controlled *Error
		if errors.As(err, &controlled) {
			return err
		}
		return (&Error{Code: CodeModelUnavailable, Message: "本地 Demo 已停止，请核对授权和预留账本", Usage: usage}).WithCause(errors.Join(err, finishErr))
	}
	return err
}

// panic 由 HTTP 边界处理，但已可能收费的预留必须在栈退出时持久停用。
func (g *demoGenerator) abandon(c *demoCall) {
	if c.slot != 0 && !c.finished {
		_ = g.ledger.finish(c, Usage{}, errDemoDenied)
	}
}
func (g *demoGenerator) Generate(ctx context.Context, req Request) (Result, error) {
	cctx, c, close, err := g.call(ctx, req.UserID)
	if err != nil {
		return Result{}, demoError(err)
	}
	defer close()
	defer g.abandon(c)
	c.messages = 2 * req.RoundNo
	r, err := g.adapter.Generate(cctx, req)
	if err = g.settle(cctx, c, r.Usage, err); err != nil {
		return Result{}, err
	}
	return r, nil
}
func (g *demoGenerator) Stream(ctx context.Context, req Request, emit func(string) error) (Result, error) {
	cctx, c, close, err := g.call(ctx, req.UserID)
	if err != nil {
		return Result{}, demoError(err)
	}
	defer close()
	defer g.abandon(c)
	c.messages = 2 * req.RoundNo
	r, err := g.adapter.Stream(cctx, req, emit)
	if err = g.settle(cctx, c, r.Usage, err); err != nil {
		return Result{}, err
	}
	return r, nil
}
func (g *demoGenerator) GenerateStrategy(ctx context.Context, req StrategyRequest) (StrategyResult, error) {
	cctx, c, close, err := g.call(ctx, req.UserID)
	if err != nil {
		return StrategyResult{}, demoError(err)
	}
	defer close()
	defer g.abandon(c)
	c.messages = 2
	r, err := g.adapter.GenerateStrategy(cctx, req)
	if err = g.settle(cctx, c, r.Usage, err); err != nil {
		return StrategyResult{}, err
	}
	return r, nil
}
func (g *demoGenerator) GenerateSummary(ctx context.Context, req SummaryRequest) (SummaryResult, error) {
	cctx, c, close, err := g.call(ctx, req.UserID)
	if err != nil {
		return SummaryResult{}, demoError(err)
	}
	defer close()
	defer g.abandon(c)
	historyMessages, modeErr := SummaryMessageCount(req.Mode)
	if modeErr != nil {
		return SummaryResult{}, modeErr
	}
	c.messages = historyMessages + 2
	r, err := g.adapter.GenerateSummary(cctx, req)
	if err = g.settle(cctx, c, r.Usage, err); err != nil {
		return SummaryResult{}, err
	}
	return r, nil
}
func demoError(err error) *Error {
	return (&Error{Code: CodeModelUnavailable, Message: "本地 Demo 真实入口未获准或已停止"}).WithCause(err)
}

type demoTransport struct {
	base   http.RoundTripper
	ledger *demoLedger
}

func (t demoTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		return nil, errDemoDenied
	}
	defer req.Body.Close()
	c, ok := req.Context().Value(demoCallKey{}).(*demoCall)
	if !ok || req.Method != http.MethodPost || req.URL.String() != "https://api.deepseek.com/chat/completions" || req.Host != req.URL.Host {
		return nil, errDemoDenied
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, t.ledger.approval.PerCallInputTokens-1024+1))
	if err != nil || int64(len(raw))+1024 > t.ledger.approval.PerCallInputTokens || !validDemoPayload(raw, t.ledger.approval, c.messages) {
		return nil, errDemoDenied
	}
	if err := t.ledger.reserve(req.Context(), c, int64(len(raw))+1024); err != nil {
		return nil, err
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	out.Body = io.NopCloser(bytes.NewReader(raw))
	out.ContentLength = int64(len(raw))
	out.GetBody = nil
	out.Header.Del("Idempotency-Key")
	out.Header.Del("X-Idempotency-Key")
	resp, err := t.base.RoundTrip(out)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		_ = resp.Body.Close()
		return nil, errDemoDenied
	}
	return resp, nil
}
func validDemoPayload(raw []byte, a DemoApproval, messageCount int) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	allowed := []string{"model", "max_tokens", "stream", "stream_options", "thinking", "messages"}
	// decodeStrictJSONObject 禁止重复/未知键；stream_options 对非流式为可选。
	keys := make([]string, 0, len(fields))
	for k := range fields {
		found := false
		for _, v := range allowed {
			if k == v {
				found = true
			}
		}
		if !found {
			return false
		}
		keys = append(keys, k)
	}
	if _, err := decodeStrictJSONObject(raw, keys); err != nil {
		return false
	}
	var output int64
	if json.Unmarshal(fields["max_tokens"], &output) != nil || output != a.PerCallOutputTokens {
		return false
	}
	fields["max_tokens"] = json.RawMessage("1024")
	// 完整轮次/总结为 system + 历史 user/assistant 对 + 最后一条 user。
	// 先验证完整历史，再借用既有 T07 对模型、thinking 和流式字段的白名单。
	var messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if json.Unmarshal(fields["messages"], &messages) != nil || len(messages) < 2 || len(messages) != messageCount || len(messages)%2 != 0 {
		return false
	}
	rawMessages, err := decodeStrictJSONArray("messages", fields["messages"])
	if err != nil {
		return false
	}
	for _, rawMessage := range rawMessages {
		if _, err := decodeStrictJSONObject(rawMessage, []string{"role", "content"}); err != nil {
			return false
		}
	}
	for i, m := range messages {
		want := "user"
		if i == 0 {
			want = "system"
		} else if i%2 == 0 {
			want = "assistant"
		}
		if m.Role != want || m.Content == "" {
			return false
		}
	}
	// 保留真实请求用于出网和预算；此副本仅复用旧协议字段校验，不裁剪发送历史。
	fields["messages"], _ = json.Marshal(messages[:2])
	normalized, err := json.Marshal(fields)
	return err == nil && validTrialPayload(normalized)
}

var _ Generator = (*demoGenerator)(nil)
var _ StreamGenerator = (*demoGenerator)(nil)
var _ SummaryGenerator = (*demoGenerator)(nil)
var _ StrategyGenerator = (*demoGenerator)(nil)
