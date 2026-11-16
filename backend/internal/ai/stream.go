package ai

import (
	"context"
	"errors"
	"io"
	"time"
	"unicode/utf8"

	adapteropenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// Stream 复用 Generate 的输入校验、窗口预检与安全配置，只把“整流调用”
// 换成官方适配器的真实增量读取：增量到达回调的时机由供应商输出决定。
//
// 契约：
//   - emit 只收到临时增量文字，不是完整回复，调用方不得据此落库；
//   - 返回值是最终完整回复，必须通过与 Generate 完全一致的回复校验；
//   - emit 报错或 Context 取消时立即关闭 reader 并取消上游请求。取消只能
//     尽快止损：已发出的请求和已产生的 token 仍可能计费，调用方须保留既有
//     费用预留与未知结果处理，不得据“已取消”推断零费用；
//   - 供应商缺尾帧、非法 UTF-8、非法结束原因、未知角色、工具调用与超长回复
//     全部受控失败，绝不把半截内容当成功。
func (a *OpenAIAdapter) Stream(ctx context.Context, req Request, emit func(text string) error) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if emit == nil {
		return Result{}, invalidInput("流式生成必须提供增量回调")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, contextError(err, Usage{}, Metadata{})
	}

	prompt, err := BuildPrompt(req)
	if err != nil {
		a.log(ctx, req, err, 0, Usage{})
		return Result{}, err
	}
	if err := a.config.Window.ValidatePrompt(prompt); err != nil {
		a.log(ctx, req, err, 0, Usage{})
		return Result{}, err
	}

	callCtx, cancel := newModelCallContext(ctx)
	defer cancel()

	messages := make([]*schema.Message, len(prompt.Messages))
	for i, message := range prompt.Messages {
		messages[i] = &schema.Message{Role: schema.RoleType(message.Role), Content: message.Content}
	}

	// 流式观察与整流观察互斥：同一个请求只会设置其中一个 Context 键。
	var observation streamObservation
	callCtx = context.WithValue(callCtx, streamObservationContextKey{}, &observation)

	// 计时覆盖从发起到最后一个增量到达（含 emit）的全过程，而不是只看建连。
	started := time.Now()
	reader, err := a.chatModel.Stream(callCtx, messages,
		model.WithMaxTokens(a.config.MaxOutputTokens),
		// 思考模式同样必须关闭，否则 45 秒预算会被思考消耗。
		adapteropenai.WithExtraFields(map[string]any{"thinking": map[string]any{"type": "disabled"}}),
	)
	if err != nil {
		return a.failStream(ctx, req, callCtx, &observation, err, started, prompt.Version)
	}
	if reader == nil {
		return a.failStream(ctx, req, callCtx, &observation, errors.New("流式读取器为空"), started, prompt.Version)
	}
	// 任何返回路径都必须关闭 reader，尽快释放供应商连接；已产生的费用不因关闭消失。
	defer reader.Close()

	var builder []byte
	for {
		message, recvErr := reader.Recv()
		if errors.Is(recvErr, io.EOF) {
			break
		}
		if recvErr != nil {
			return a.failStream(ctx, req, callCtx, &observation, recvErr, started, prompt.Version)
		}
		if message == nil {
			continue
		}
		if len(message.ToolCalls) > 0 {
			observation.markToolCalls()
		}
		if message.Content == "" {
			continue
		}
		// 增量必须逐块校验 UTF-8 与累计长度：超限立即中止上游读取，
		// 只能减小后续消耗，已产生的费用仍需由调用方预留承担。
		if !utf8.ValidString(message.Content) {
			observation.markInvalidUTF8()
			return a.streamFailure(ctx, req, &observation, "模型流式回复不是合法 UTF-8", started, prompt.Version)
		}
		builder = append(builder, message.Content...)
		if utf8.RuneCount(builder) > MaxUserContentCodepoints {
			return a.streamFailure(ctx, req, &observation, "模型回复超过长度上限", started, prompt.Version)
		}
		if err := emit(message.Content); err != nil {
			cancel()
			return a.canceledStream(ctx, req, &observation, err, started, prompt.Version)
		}
		if err := ctx.Err(); err != nil {
			return a.failStream(ctx, req, callCtx, &observation, err, started, prompt.Version)
		}
	}

	// EOF 也可能是“期限恰好到期”的表现：先看 Context，避免把超时误判成完整结束。
	// 父 Context 已取消/超时同样不能当成功，否则会把半截结果落库。
	if err := callCtx.Err(); err != nil {
		return a.failStream(ctx, req, callCtx, &observation, err, started, prompt.Version)
	}

	snapshot := observation.snapshot()
	metadata := a.streamMetadata(snapshot, started, prompt.Version)
	usage := snapshot.usage
	content := string(builder)

	if failure := classifyStreamReply(content, snapshot); failure != nil {
		failure.Usage = usage
		failure.Metadata = metadata
		a.log(ctx, req, failure, time.Since(started), usage)
		return Result{}, failure
	}

	result := Result{Content: content, Usage: usage, Metadata: metadata}
	a.log(ctx, req, nil, time.Since(started), usage)
	return result, nil
}

// streamMetadata 组装最终元数据；LatencyMS 覆盖完整读取过程（含 emit）。
func (a *OpenAIAdapter) streamMetadata(snapshot streamObservationSnapshot, started time.Time,
	promptVersion string) Metadata {
	return Metadata{
		Model:         a.config.Model,
		PromptVersion: promptVersion,
		FinishReason:  snapshot.finishReason,
		Refused:       snapshot.hasRefusal || snapshot.finishReason == "content_filter",
		LatencyMS:     time.Since(started).Milliseconds(),
	}
}

// canceledStream 记录“回调失败或调用方取消”的受控错误，并保留已到达的 usage。
func (a *OpenAIAdapter) canceledStream(ctx context.Context, req Request, observation *streamObservation,
	cause error, started time.Time, promptVersion string) (Result, error) {
	snapshot := observation.snapshot()
	metadata := a.streamMetadata(snapshot, started, promptVersion)
	controlled := (&Error{
		Code: CodeModelCanceled, Message: "生成请求已取消",
		Usage: snapshot.usage, Metadata: metadata,
	}).WithCause(cause)
	a.log(ctx, req, controlled, time.Since(started), snapshot.usage)
	return Result{}, controlled
}

// failStream 统一处理流式调用错误，并保留已到达的计费事实与受控分类。
func (a *OpenAIAdapter) failStream(ctx context.Context, req Request, callCtx context.Context,
	observation *streamObservation, cause error, started time.Time, promptVersion string) (Result, error) {
	snapshot := observation.snapshot()
	metadata := a.streamMetadata(snapshot, started, promptVersion)
	usage := snapshot.usage

	controlled := classifyModelError(callCtx, cause, usage, metadata)
	if callCtx.Err() == nil && controlled.Code != CodeModelTimeout && controlled.Code != CodeModelCanceled {
		switch {
		case snapshot.httpStatus >= 400:
			controlled = httpStatusError(snapshot.httpStatus, cause, usage, metadata)
		case snapshot.invalidBody || snapshot.invalidUTF8 || snapshot.oversize:
			controlled = &Error{Code: CodeModelError, Message: "模型流式响应无效", Usage: usage, Metadata: metadata}
		}
	}
	if controlled.Code == CodeProviderError && snapshot.hasRefusal {
		controlled.Code, controlled.Message = CodeModelRefused, "模型拒绝回复本次内容"
	}
	controlled.Metadata.Refused = metadata.Refused
	controlled.Metadata.FinishReason = metadata.FinishReason
	a.log(ctx, req, controlled, time.Since(started), usage)
	return Result{}, controlled
}

// streamFailure 用于“HTTP 与读取都成功，但内容不可用”的受控失败。
func (a *OpenAIAdapter) streamFailure(ctx context.Context, req Request, observation *streamObservation,
	message string, started time.Time, promptVersion string) (Result, error) {
	snapshot := observation.snapshot()
	metadata := a.streamMetadata(snapshot, started, promptVersion)
	controlled := &Error{Code: CodeModelError, Message: message, Usage: snapshot.usage, Metadata: metadata}
	a.log(ctx, req, controlled, time.Since(started), snapshot.usage)
	return Result{}, controlled
}

// classifyStreamReply 把“读完了但回复不可用”的情况转为受控错误。
// 缺尾帧（没有 [DONE]）与未知结束原因都必须失败，绝不能当作正常结束。
func classifyStreamReply(content string, snapshot streamObservationSnapshot) *Error {
	switch {
	case snapshot.invalidBody || snapshot.invalidUTF8 || snapshot.oversize:
		return &Error{Code: CodeModelError, Message: "模型流式响应格式或大小无效"}
	case snapshot.hasToolCalls:
		return &Error{Code: CodeModelError, Message: "模型返回了不支持的工具调用输出"}
	case snapshot.hasRefusal:
		return &Error{Code: CodeModelRefused, Message: "模型拒绝回复本次内容"}
	case snapshot.finishReason == "content_filter":
		return &Error{Code: CodeModelRefused, Message: "模型回复被内容策略拦截"}
	case snapshot.finishReason == "length":
		return &Error{Code: CodeModelError, Message: "模型回复因输出上限被截断"}
	case !snapshot.hasFinishReason || snapshot.finishReason != "stop":
		return &Error{Code: CodeModelError, Message: "模型未正常结束回复"}
	case !snapshot.sawDone:
		return &Error{Code: CodeModelError, Message: "模型流式响应缺少结束帧"}
	case !snapshot.hasRole || snapshot.role != "assistant":
		return &Error{Code: CodeModelError, Message: "模型响应角色无效"}
	}
	if err := ValidateReply(content); err != nil {
		var controlled *Error
		if errors.As(err, &controlled) {
			return controlled
		}
		return &Error{Code: CodeModelError, Message: "模型回复无效"}
	}
	return nil
}

// markToolCalls/markInvalidUTF8 供适配器在读取循环中补充观察事实。
func (o *streamObservation) markToolCalls() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.hasToolCalls = true
}

// 编译期断言：真实适配器必须同时满足普通与流式两个业务接口。
var _ StreamGenerator = (*OpenAIAdapter)(nil)
