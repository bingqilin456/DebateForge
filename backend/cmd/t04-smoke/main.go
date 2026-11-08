// Command t04-smoke 是本次授权专用的本地一次性工具，不装配业务路由。
package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/joho/godotenv"
)

const (
	baseURL         = "https://api.deepseek.com"
	modelID         = "deepseek-flash"
	authorizationID = "T04-20261108-local-once"
	inputLimit      = 8192
	outputLimit     = 512
	// 使用高峰、输入全未命中价格，即使跨时段也不低估预检。
	estimatedCost = "0.02048000"
)

type balanceInfo struct {
	Currency string `json:"currency"`
	Total    string `json:"total_balance"`
	Granted  string `json:"granted_balance"`
	ToppedUp string `json:"topped_up_balance"`
}
type balance struct {
	Available *bool         `json:"is_available"`
	Infos     []balanceInfo `json:"balance_infos"`
}
type providerUsage struct {
	Prompt     *int `json:"prompt_tokens"`
	Completion *int `json:"completion_tokens"`
	Total      *int `json:"total_tokens"`
	Hit        *int `json:"prompt_cache_hit_tokens"`
	Miss       *int `json:"prompt_cache_miss_tokens"`
}
type providerSnapshot struct {
	ID    string        `json:"id,omitempty"`
	Model string        `json:"model,omitempty"`
	Usage providerUsage `json:"usage"`
}
type evidence struct {
	Authorization  string           `json:"authorization"`
	Status         string           `json:"status"`
	ErrorCode      string           `json:"error_code,omitempty"`
	StartedAt      time.Time        `json:"started_at"`
	FinishedAt     time.Time        `json:"finished_at"`
	ElapsedMS      int64            `json:"total_elapsed_ms"`
	ModelElapsedMS int64            `json:"model_elapsed_ms"`
	Consumed       bool             `json:"consumed"`
	ModelAttempts  int              `json:"model_transport_attempts"`
	HTTPStatus     int              `json:"model_http_status,omitempty"`
	InputBound     int              `json:"complete_input_conservative_bound"`
	EstimatedCost  string           `json:"conservative_cost_cny"`
	Before         *balance         `json:"balance_before,omitempty"`
	After          *balance         `json:"balance_after,omitempty"`
	AfterError     string           `json:"balance_after_error,omitempty"`
	BalanceDelta   string           `json:"observed_balance_delta_cny,omitempty"`
	CalculatedCost string           `json:"usage_calculated_cost_cny,omitempty"`
	CostBasis      string           `json:"cost_basis,omitempty"`
	Provider       providerSnapshot `json:"provider"`
	Response       ai.Result        `json:"response"`
}

func main() {
	// 显式固定参数，默认启动不会触网；没有重置/换目录/重试选项。
	if len(os.Args) != 2 || os.Args[1] != "--execute-once" {
		fmt.Println("未执行：仅支持本次授权的 --execute-once；没有重置或重试入口")
		return
	}
	wd, err := os.Getwd()
	if err != nil {
		fmt.Println("BLOCKED: workspace_unavailable")
		os.Exit(1)
	}
	mod, err := os.ReadFile(filepath.Join(wd, "go.mod"))
	if err != nil || !strings.HasPrefix(string(mod), "module github.com/bingqilin456/DebateForge/backend") {
		fmt.Println("BLOCKED: run_from_backend_required")
		os.Exit(1)
	}
	key, err := loadKey()
	if err != nil {
		fmt.Println("BLOCKED: credential_unavailable")
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	transport := newDirectTransport()
	defer transport.CloseIdleConnections()
	dir := filepath.Join(wd, ".cache", "t04-smoke-20261108")
	e := execute(ctx, dir, key, transport)
	fmt.Printf("status=%s error_code=%s consumed=%t model_attempts=%d evidence=%s\n", e.Status, e.ErrorCode, e.Consumed, e.ModelAttempts, filepath.Join(dir, "evidence.json"))
	if e.Status != "success" {
		os.Exit(1)
	}
}

// loadKey 只向调用器返回内存值；解析错误可能含原文，因此不传播错误细节。
func loadKey() (string, error) {
	key, present := os.LookupEnv("DEEPSEEK_API_KEY")
	if !present {
		values, err := godotenv.Read(".env")
		if err != nil {
			return "", errors.New("credential_unavailable")
		}
		key = values["DEEPSEEK_API_KEY"]
	}
	if strings.TrimSpace(key) == "" || strings.TrimSpace(key) != key || strings.ContainsAny(key, "\r\n") {
		return "", errors.New("credential_unavailable")
	}
	return key, nil
}

// createRecord 使用排他创建及同步落盘。任何既有文件（包括半写入文件）均拒绝重入。
// Windows 的 File.Sync 使用 FlushFileBuffers；本工具不清理锁或消耗标记。
func createRecord(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return errors.New("record_encode_failed")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return errors.New("record_create_failed")
	}
	n, writeErr := f.Write(append(raw, '\n'))
	if writeErr == nil && n != len(raw)+1 {
		writeErr = io.ErrShortWrite
	}
	if writeErr == nil {
		writeErr = f.Sync()
	}
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		return errors.New("record_sync_failed")
	}
	return nil
}

func anonymousRequest() ai.Request {
	return ai.Request{Topic: ai.Topic{Title: "大学课程是否应采用开放式考核", Description: "讨论学习效果与评价公平"}, UserStance: ai.StancePro, RoundNo: 1, UserContent: "开放式考核可以评估实际应用能力。请在120字以内回应。"}
}

func anonymousWire() ([]byte, error) {
	p, err := ai.BuildPrompt(anonymousRequest())
	if err != nil {
		return nil, err
	}
	messages := make([]map[string]string, 0, len(p.Messages))
	for _, m := range p.Messages {
		messages = append(messages, map[string]string{"role": string(m.Role), "content": m.Content})
	}
	return json.Marshal(map[string]any{"model": modelID, "messages": messages, "max_tokens": outputLimit, "thinking": map[string]string{"type": "disabled"}, "stream": false})
}

// guardWire 验证 SDK 实际序列化的数据，而不只验证构造配置。
// 完整 JSON 的 UTF-8 字节数 + 1024 消息/协议预留比正文逐字节口径更保守；不是精确 tokenizer。
func guardWire(raw []byte) ([]byte, int, error) {
	if len(raw) > inputLimit {
		return nil, 0, errors.New("wire_input_exceeded")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return nil, 0, errors.New("wire_invalid")
	}
	for key := range fields {
		switch key {
		case "model", "messages", "max_tokens", "thinking", "stream":
		default:
			return nil, 0, errors.New("wire_unexpected_field")
		}
	}
	var got, want any
	if json.Unmarshal(raw, &got) != nil {
		return nil, 0, errors.New("wire_invalid")
	}
	expected, _ := anonymousWire()
	_ = json.Unmarshal(expected, &want)
	gm, wm := got.(map[string]any), want.(map[string]any)
	// SDK 默认省略 false；本地发送守卫补成显式 false。
	if _, ok := gm["stream"]; !ok {
		gm["stream"] = false
	}
	if !reflect.DeepEqual(gm, wm) {
		return nil, 0, errors.New("wire_contract_mismatch")
	}
	gm["tool_choice"] = "none"
	guarded, err := json.Marshal(gm)
	bound := len(guarded) + 1024
	if err != nil || bound > inputLimit {
		return nil, 0, errors.New("wire_input_exceeded")
	}
	return guarded, bound, nil
}

// newDirectTransport 不使用环境代理、复用连接或 HTTP/2。POST GetBody 亦移除，
// 避免 net/http 的重放条件；上层任何重复 RoundTrip 都被 CAS 与持久化标记拒绝。
func newDirectTransport() *http.Transport {
	return &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, DisableKeepAlives: true, ForceAttemptHTTP2: false, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, TLSHandshakeTimeout: 5 * time.Second, ResponseHeaderTimeout: 45 * time.Second}
}
func newClient(rt http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{Transport: rt, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
}

var decimalPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]{1,12})?$`)

func amount(s string) (*big.Rat, bool) {
	if len(s) > 40 || !decimalPattern.MatchString(s) {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(s)
	return r, ok
}
func cny(b *balance) (*big.Rat, bool) {
	if b == nil {
		return nil, false
	}
	var result *big.Rat
	for _, v := range b.Infos {
		if v.Currency == "CNY" {
			if result != nil {
				return nil, false
			}
			r, ok := amount(v.Total)
			if !ok {
				return nil, false
			}
			result = r
		}
	}
	return result, result != nil
}

// queryBalance 仅访问官方只读接口；不返回响应正文/请求头/底层错误。
func queryBalance(ctx context.Context, key string, rt http.RoundTripper) (*balance, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if ctx.Err() != nil {
		return nil, errors.New("balance_context_done")
	}
	req, _ := http.NewRequestWithContext(ctx, "GET", baseURL+"/user/balance", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := newClient(rt, 5*time.Second).Do(req)
	if err != nil {
		return nil, errors.New("balance_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, errors.New("balance_http_rejected")
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 16385))
	if err != nil || len(raw) > 16384 {
		return nil, errors.New("balance_body_invalid")
	}
	var b balance
	if json.Unmarshal(raw, &b) != nil || b.Available == nil {
		return nil, errors.New("balance_invalid")
	}
	if _, ok := cny(&b); !ok {
		return nil, errors.New("balance_cny_unknown")
	}
	// 余额接口的白名单字符串均须是金额，避免供应商异常正文被落盘。
	for _, v := range b.Infos {
		if v.Currency != "CNY" && v.Currency != "USD" {
			return nil, errors.New("balance_currency_invalid")
		}
		for _, s := range []string{v.Total, v.Granted, v.ToppedUp} {
			if s != "" {
				if _, ok := amount(s); !ok {
					return nil, errors.New("balance_amount_invalid")
				}
			}
		}
	}
	return &b, nil
}

type singleModelTransport struct {
	base   http.RoundTripper
	dir    string
	key    string
	e      *evidence
	called atomic.Bool
}

func (t *singleModelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Method != "POST" || req.URL.String() != baseURL+"/chat/completions" {
		return nil, errors.New("model_destination_rejected")
	}
	if req.Context().Err() != nil {
		return nil, req.Context().Err()
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, inputLimit+1))
	_ = req.Body.Close()
	if err != nil {
		return nil, errors.New("wire_read_failed")
	}
	body, bound, err := guardWire(raw)
	if err != nil {
		return nil, err
	}
	if !t.called.CompareAndSwap(false, true) {
		return nil, errors.New("second_attempt_forbidden")
	}
	t.e.InputBound = bound
	if createRecord(filepath.Join(t.dir, "wire-request.json"), json.RawMessage(body)) != nil {
		return nil, errors.New("wire_evidence_failed")
	}
	if createRecord(filepath.Join(t.dir, "consumed.json"), map[string]any{"authorization": authorizationID, "consumed_before_network": true, "at": time.Now().UTC(), "max_model_attempts": 1}) != nil {
		return nil, errors.New("consume_persistence_failed")
	}
	t.e.Consumed = true
	// 消耗事实已同步落盘；取消/网络错误/任何 HTTP 响应都不恢复授权。
	if req.Context().Err() != nil {
		return nil, req.Context().Err()
	}
	safe := req.Clone(req.Context())
	safe.Body = io.NopCloser(bytes.NewReader(body))
	safe.ContentLength = int64(len(body))
	safe.GetBody = nil
	safe.Header.Del("Idempotency-Key")
	safe.Header.Del("X-Idempotency-Key")
	t.e.ModelAttempts = 1
	response, err := t.base.RoundTrip(safe)
	if err != nil || response == nil {
		return response, err
	}
	t.e.HTTPStatus = response.StatusCode
	raw, err = io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	_ = response.Body.Close()
	if err != nil || len(raw) > 1<<20 {
		return nil, errors.New("model_body_invalid")
	}
	// 只保留白名单 metadata/usage；原始 error/body/响应头均不保存。
	if response.StatusCode == 200 {
		_ = json.Unmarshal(raw, &t.e.Provider)
		t.e.Provider.ID = redact(t.e.Provider.ID, t.key)
		t.e.Provider.Model = redact(t.e.Provider.Model, t.key)
	}
	response.Body = io.NopCloser(bytes.NewReader(raw))
	return response, nil
}
func redact(s, key string) string {
	if key != "" {
		s = strings.ReplaceAll(s, key, "[redacted]")
	}
	if len(s) > 4096 {
		return "[value_omitted]"
	}
	return s
}

func execute(parent context.Context, dir, key string, rt http.RoundTripper) (e evidence) {
	started := time.Now()
	e = evidence{Authorization: authorizationID, Status: "blocked", StartedAt: started.UTC(), EstimatedCost: estimatedCost}
	ctx, cancel := context.WithTimeout(parent, 60*time.Second)
	defer cancel()
	if err := os.MkdirAll(dir, 0700); err != nil {
		e.ErrorCode = "state_directory_failed"
		return
	}
	if createRecord(filepath.Join(dir, "launched.json"), map[string]any{"authorization": authorizationID, "at": started.UTC(), "no_reentry": true}) != nil {
		e.ErrorCode = "already_launched_or_state_unwritable"
		return
	}
	defer func() {
		e.FinishedAt = time.Now().UTC()
		e.ElapsedMS = time.Since(started).Milliseconds()
		if createRecord(filepath.Join(dir, "evidence.json"), e) != nil {
			e.Status = "evidence_write_failed"
			e.ErrorCode = "evidence_write_failed"
		}
	}()
	pre, err := queryBalance(ctx, key, rt)
	if err != nil {
		e.ErrorCode = err.Error()
		return
	}
	e.Before = pre
	total, _ := cny(pre)
	cost, _ := amount(estimatedCost)
	if !*pre.Available || total.Cmp(cost) < 0 {
		e.ErrorCode = "balance_precheck_failed"
		return
	}
	if cost.Cmp(big.NewRat(1, 10)) > 0 {
		e.ErrorCode = "cost_target_exceeded"
		return
	}
	guard := &singleModelTransport{base: rt, dir: dir, key: key, e: &e}
	generator, err := ai.NewOpenAIGenerator(ctx, ai.OpenAIConfig{APIKey: key, BaseURL: baseURL, Model: modelID, MaxOutputTokens: outputLimit, Window: ai.WindowLimits{ContextTokens: 1000000, MaxOutputTokens: outputLimit, SummaryPromptReserveTokens: 60000, SummaryOutputReserveTokens: 4096}, HTTPClient: newClient(guard, 45*time.Second)})
	if err != nil {
		e.ErrorCode = "adapter_initialization_failed"
		return
	}
	modelStarted := time.Now()
	e.Response, err = generator.Generate(ctx, anonymousRequest())
	e.ModelElapsedMS = time.Since(modelStarted).Milliseconds()
	e.Response.Content = redact(e.Response.Content, key)
	if err != nil {
		e.Status = "failed"
		e.ErrorCode = "model_error"
		var controlled *ai.Error
		if errors.As(err, &controlled) {
			e.ErrorCode = string(controlled.Code)
			e.Response.Usage = controlled.Usage
			e.Response.Metadata = controlled.Metadata
		}
		if !e.Consumed {
			e.Status = "blocked"
		}
	} else {
		e.Status = "success"
	}
	// 失败路径亦可能带供应商 finish_reason，不允许借 metadata 回显凭据。
	e.Response.Metadata.FinishReason = redact(e.Response.Metadata.FinishReason, key)
	e.Response.Metadata.Model = redact(e.Response.Metadata.Model, key)
	e.Response.Metadata.PromptVersion = redact(e.Response.Metadata.PromptVersion, key)
	if e.Consumed {
		e.After, err = queryBalance(ctx, key, rt)
		if err != nil {
			e.AfterError = err.Error()
		}
		if after, ok := cny(e.After); ok {
			e.BalanceDelta = new(big.Rat).Sub(total, after).FloatString(8)
		}
		calculateCost(&e)
	}
	return
}

// calculateCost 以官方时段价格和已知 usage 复算；这是用量计价，不冒充供应商账单。
func calculateCost(e *evidence) {
	u := e.Provider.Usage
	if u.Prompt == nil || u.Completion == nil || *u.Prompt < 0 || *u.Completion < 0 {
		return
	}
	// 北京时间周末全天空闲；平日保守采用高峰，法定节假日不自行判断。
	local := e.StartedAt.In(time.FixedZone("Asia/Shanghai", 8*60*60))
	peak := local.Weekday() != time.Saturday && local.Weekday() != time.Sunday && ((local.Hour() >= 9 && local.Hour() < 12) || (local.Hour() >= 14 && local.Hour() < 18))
	missRate, hitRate, outRate := int64(100), int64(2), int64(400)
	if peak {
		missRate *= 2
		hitRate *= 2
		outRate *= 2
	}
	hit, miss := 0, *u.Prompt
	e.CostBasis = "official_rate_all_input_cache_miss_estimate_not_invoice"
	if u.Hit != nil && u.Miss != nil && *u.Hit >= 0 && *u.Miss >= 0 && *u.Hit+*u.Miss == *u.Prompt {
		hit, miss = *u.Hit, *u.Miss
		e.CostBasis = "official_rate_returned_cache_usage_not_invoice"
	}
	n := new(big.Int).Mul(big.NewInt(int64(miss)), big.NewInt(missRate))
	n.Add(n, new(big.Int).Mul(big.NewInt(int64(hit)), big.NewInt(hitRate)))
	n.Add(n, new(big.Int).Mul(big.NewInt(int64(*u.Completion)), big.NewInt(outRate)))
	e.CalculatedCost = new(big.Rat).SetFrac(n, big.NewInt(100000000)).FloatString(8)
}
