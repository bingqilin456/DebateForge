package ai

import (
	"context"
	"errors"
)

// GenerateAnnotations 使用与赛前策略同一条结构化生成链路，并保留费用账本所需的用量。
//
// annotation-v2 起，模型只返回片段编号；这里与 Fake 共用 BuildAnnotationInput 的目录
// 和 buildAnnotationResult 的解析收尾，保证两条链路对同一份回复给出相同分类。
func (a *OpenAIAdapter) GenerateAnnotations(ctx context.Context, req AnnotationRequest) (AnnotationResult, error) {
	input, err := BuildAnnotationInput(req)
	if err != nil {
		return AnnotationResult{}, err
	}
	if err = a.config.Window.ValidateSummaryPrompt(input.Prompt); err != nil {
		return AnnotationResult{}, err
	}
	reply, err := a.generateStructuredReply(ctx, input.Prompt)
	if err != nil {
		return AnnotationResult{}, err
	}
	result, err := buildAnnotationResult(reply.Content, input)
	result.Usage = reply.Usage
	result.Metadata = reply.Metadata
	if err == nil {
		return result, nil
	}
	// 全无效与“结构整批非法”要分开：前者保留可区分的内部错误，后者统一包装。
	if errors.Is(err, ErrAnnotationAllRejected) {
		// Demo 的 settle 会从 *Error 读取用量并按真实 token 结算；
		// 全拒绝同样花掉了一次真实请求，必须把实际用量挂在错误对象上。
		if controlled, ok := err.(*Error); ok {
			controlled.Usage = reply.Usage
			controlled.Metadata = reply.Metadata
		}
		return result, err
	}
	return result, (&Error{
		Code:     CodeModelError,
		Message:  "批注结构无效",
		Usage:    reply.Usage,
		Metadata: reply.Metadata,
	}).WithCause(err)
}
