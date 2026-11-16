package ai

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	adapteropenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// GenerateStrategy 使用真实 Eino OpenAI 协议适配器执行策略生成与 Schema 校验。
// 校验失败直接返回 CodeModelError，绝不自动发起修复调用或保存无效结构。
func (a *OpenAIAdapter) GenerateStrategy(ctx context.Context, req StrategyRequest) (StrategyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return StrategyResult{}, contextError(err, Usage{}, Metadata{})
	}

	prompt, err := BuildStrategyPrompt(req)
	if err != nil {
		a.logStrategy(ctx, err, 0, Usage{})
		return StrategyResult{}, err
	}
	if err := a.config.Window.ValidateSummaryPrompt(prompt); err != nil {
		a.logStrategy(ctx, err, 0, Usage{})
		return StrategyResult{}, err
	}

	callCtx, cancel := newModelCallContext(ctx)
	defer cancel()

	messages := make([]*schema.Message, len(prompt.Messages))
	for i, message := range prompt.Messages {
		messages[i] = &schema.Message{Role: schema.RoleType(message.Role), Content: message.Content}
	}

	var observation responseObservation
	callCtx = context.WithValue(callCtx, observationContextKey{}, &observation)
	started := time.Now()
	message, err := a.chatModel.Generate(callCtx, messages,
		model.WithMaxTokens(a.config.MaxOutputTokens),
		adapteropenai.WithExtraFields(map[string]any{"thinking": map[string]any{"type": "disabled"}}),
	)
	latency := time.Since(started)
	metadata := Metadata{
		Model:         a.config.Model,
		PromptVersion: prompt.Version,
		LatencyMS:     latency.Milliseconds(),
	}
	usage := observation.usage
	if err != nil {
		controlled := classifyModelError(callCtx, err, usage, metadata)
		if callCtx.Err() == nil && controlled.Code != CodeModelTimeout && controlled.Code != CodeModelCanceled {
			if observation.httpStatus >= 400 {
				controlled = httpStatusError(observation.httpStatus, err, usage, metadata)
			} else if observation.invalidBody || observation.invalidUTF8 || observation.httpStatus == http.StatusOK {
				controlled = &Error{Code: CodeModelError, Message: "模型响应无效", Usage: usage, Metadata: metadata}
			}
		}
		if controlled.Code == CodeProviderError && observation.hasRefusal {
			controlled.Code, controlled.Message = CodeModelRefused, "模型拒绝回复本次内容"
		}
		controlled.Metadata.Refused = observation.hasRefusal || observation.finishReason == "content_filter"
		controlled.Metadata.FinishReason = observation.finishReason
		a.logStrategy(ctx, controlled, latency, usage)
		return StrategyResult{}, controlled
	}

	finishReason := observation.finishReason
	if !observation.hasFinishReason {
		if message != nil && message.ResponseMeta != nil {
			finishReason = message.ResponseMeta.FinishReason
		}
	}
	metadata.FinishReason = finishReason
	metadata.Refused = observation.hasRefusal || observation.finishReason == "content_filter"

	content := ""
	if message != nil {
		content = message.Content
	}

	strategy, failure := classifyStrategyReply(content, observation)
	if failure != nil {
		failure.Usage = usage
		failure.Metadata = metadata
		a.logStrategy(ctx, failure, latency, usage)
		return StrategyResult{}, failure
	}

	canonicalJSON, err := json.Marshal(strategy)
	if err != nil || len(canonicalJSON) > MaxStrategyJSONBytes {
		controlled := &Error{Code: CodeModelError, Message: "策略 JSON 序列化无效或超过大小上限", Usage: usage, Metadata: metadata}
		a.logStrategy(ctx, controlled, latency, usage)
		return StrategyResult{}, controlled
	}

	result := StrategyResult{
		SchemaVersion: StrategySchemaVersion,
		Strategy:      strategy,
		ContentJSON:   string(canonicalJSON),
		Usage:         usage,
		Metadata:      metadata,
	}
	a.logStrategy(ctx, nil, latency, usage)
	return result, nil
}

func classifyStrategyReply(content string, observation responseObservation) (Strategy, *Error) {
	switch {
	case observation.invalidBody || observation.invalidUTF8:
		return Strategy{}, &Error{Code: CodeModelError, Message: "模型响应格式或大小无效"}
	case !observation.hasRole || observation.role != "assistant":
		return Strategy{}, &Error{Code: CodeModelError, Message: "模型响应角色无效"}
	case observation.hasRefusal:
		return Strategy{}, &Error{Code: CodeModelRefused, Message: "模型拒绝回复本次内容"}
	case observation.hasToolCalls:
		return Strategy{}, &Error{Code: CodeModelError, Message: "模型返回了不支持的工具调用输出"}
	case observation.finishReason == "content_filter":
		return Strategy{}, &Error{Code: CodeModelRefused, Message: "模型回复被内容策略拦截"}
	case observation.finishReason == "length":
		return Strategy{}, &Error{Code: CodeModelError, Message: "模型回复因输出上限被截断"}
	case !observation.hasFinishReason || observation.finishReason != "stop":
		return Strategy{}, &Error{Code: CodeModelError, Message: "模型未正常结束回复"}
	}
	strategy, err := ParseStrategy(content)
	if err != nil {
		var controlled *Error
		if errors.As(err, &controlled) {
			return Strategy{}, controlled
		}
		return Strategy{}, &Error{Code: CodeModelError, Message: "模型策略结构无效"}
	}
	return strategy, nil
}

func (a *OpenAIAdapter) logStrategy(ctx context.Context, err error, latency time.Duration, usage Usage) {
	code := "ok"
	if err != nil {
		var controlled *Error
		if errors.As(err, &controlled) {
			code = string(controlled.Code)
		} else {
			code = string(CodeProviderError)
		}
	}
	attrs := []slog.Attr{
		slog.String("error_code", code),
		slog.String("model", a.config.Model),
		slog.String("prompt_version", StrategyPromptVersion),
		slog.Int("schema_version", StrategySchemaVersion),
		slog.Int64("latency_ms", latency.Milliseconds()),
	}
	attrs = append(attrs, tokenCountAttrs(usage)...)
	a.logger.LogAttrs(ctx, slog.LevelInfo, "模型策略调用", attrs...)
}

var _ StrategyGenerator = (*OpenAIAdapter)(nil)
