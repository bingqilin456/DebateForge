package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
	"unicode/utf8"
)

const (
	trialInputTokens  = 16384
	trialOutputTokens = 1024
	// 在完整 JSON 字节上界之外，为供应商消息模板和协议额外留量。
	trialProtocolReserve = 1024
)

type trialWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Users []uint64  `json:"users"`
}

func (w trialWindow) valid() bool {
	_, startOffset := w.Start.Zone()
	_, endOffset := w.End.Zone()
	return startOffset == 8*3600 && endOffset == 8*3600 &&
		w.End.Sub(w.Start) == 2*time.Hour &&
		len(w.Users) == 2 && w.Users[0] != 0 && w.Users[1] != 0 && w.Users[0] != w.Users[1]
}

// trialTransport 在 SDK 最后一个出网边界校验实际请求。reserve 必须原子、
// 持久且只能成功一次，结果不明时返回错误并保留已产生的预留。
type trialTransport struct {
	base    http.RoundTripper
	reserve func(context.Context) error
}

func (t trialTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		return nil, errors.New("试运行请求缺少正文")
	}
	defer req.Body.Close()
	if req.Method != http.MethodPost || req.URL.String() != "https://api.deepseek.com/chat/completions" ||
		req.Host != req.URL.Host || t.reserve == nil || t.base == nil {
		return nil, errors.New("试运行出网目标或准入配置无效")
	}

	const maxBytes = trialInputTokens - trialProtocolReserve
	raw, err := io.ReadAll(io.LimitReader(req.Body, maxBytes+1))
	if err != nil || len(raw) > maxBytes || !validTrialPayload(raw) {
		return nil, errors.New("试运行请求超出模型协议或预算")
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	if err := t.reserve(req.Context()); err != nil {
		return nil, fmt.Errorf("试运行额度不可用: %w", err)
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
		return nil, errors.New("试运行禁止供应商重定向")
	}
	return resp, nil
}

func validTrialPayload(raw []byte) bool {
	if !utf8.Valid(raw) {
		return false
	}
	var payload struct {
		Model     string `json:"model"`
		MaxTokens int    `json:"max_tokens"`
		Stream    bool   `json:"stream"`
		// 流式请求必须显式要求 usage 汇总，否则无法核对计费；非流式不得携带。
		StreamOptions *struct {
			IncludeUsage bool `json:"include_usage"`
		} `json:"stream_options"`
		Thinking struct {
			Type string `json:"type"`
		} `json:"thinking"`
		Messages []struct {
			Role    Role   `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&payload) != nil || decoder.Decode(new(any)) != io.EOF {
		return false
	}
	// 流式与非流式共用同一套费用约束，只按实际调用的传输形态放行真实协议字段：
	// 流式要求 stream=true 且 include_usage=true，非流式要求不携带 stream_options。
	streamShape := payload.Stream && payload.StreamOptions != nil && payload.StreamOptions.IncludeUsage
	plainShape := !payload.Stream && payload.StreamOptions == nil
	if !streamShape && !plainShape {
		return false
	}
	return payload.Model == "deepseek-flash" && payload.MaxTokens == trialOutputTokens &&
		payload.Thinking.Type == "disabled" &&
		len(payload.Messages) == 2 && payload.Messages[0].Role == "system" &&
		payload.Messages[1].Role == "user" && payload.Messages[0].Content != "" &&
		payload.Messages[1].Content != ""
}
