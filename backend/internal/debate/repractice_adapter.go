package debate

import (
	"context"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// RepracticeContext 是生成反馈时的只读场次快照：只用于让模型判断“是否回应了辩题”，
// 不参与改写、不落库、不改变正式比赛。
type RepracticeContext struct {
	Topic      ai.Topic
	UserStance ai.Stance
}

// RepracticeContextGenerator 是带场次上下文的反馈生成能力。
//
// 生产适配器实现它，以便把辩题快照作为 user 数据段的一部分外发；
// 单元测试里的最小 Fake 只需实现 RepracticeGenerator，服务会自动退回无上下文路径，
// 这样业务规则（保存、版本、幂等、重试）都不依赖真实 Prompt 形状。
type RepracticeContextGenerator interface {
	GenerateRepracticeWithContext(ctx context.Context, rc RepracticeContext, in RepracticeInput) (RepracticeFeedback, error)
}

// aiRepracticeAdapter 把 ai 层的 RepracticeGenerator 适配成 debate 层的生成能力。
// 它只做协议转换与结果映射，不碰数据库、不决定版本状态。
type aiRepracticeAdapter struct {
	generator ai.RepracticeGenerator
}

func (a aiRepracticeAdapter) GenerateRepractice(ctx context.Context, in RepracticeInput) (RepracticeFeedback, error) {
	return a.generate(ctx, RepracticeContext{}, in)
}

func (a aiRepracticeAdapter) GenerateRepracticeWithContext(ctx context.Context, rc RepracticeContext, in RepracticeInput) (RepracticeFeedback, error) {
	return a.generate(ctx, rc, in)
}

func (a aiRepracticeAdapter) generate(ctx context.Context, rc RepracticeContext, in RepracticeInput) (RepracticeFeedback, error) {
	if a.generator == nil {
		return RepracticeFeedback{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "当前生成器不支持局部重练"}
	}
	result, err := a.generator.GenerateRepractice(ctx, ai.RepracticeRequest{
		Topic:      rc.Topic,
		UserStance: rc.UserStance,
		Goal:       in.Goal,
		Quote:      in.Quote,
		Rewrite:    in.Rewrite,
	})
	if err != nil {
		return RepracticeFeedback{}, err
	}
	// 生成器已做结构校验；这里再按业务口径复核一次理由与长度，
	// 防止不同实现绕过契约写入不可用的反馈。
	feedback := RepracticeFeedback{
		Improved:   result.Draft.Improved,
		Change:     result.Draft.Change,
		Reason:     result.Draft.Reason,
		NewProblem: result.Draft.NewProblem,
	}
	if err := ValidateRepracticeFeedback(feedback); err != nil {
		return RepracticeFeedback{}, &ai.Error{Code: ai.CodeModelError, Message: "重练反馈不符合业务口径"}
	}
	return feedback, nil
}

// 编译期确认适配器同时满足两种生成能力。
var (
	_ RepracticeGenerator        = aiRepracticeAdapter{}
	_ RepracticeContextGenerator = aiRepracticeAdapter{}
)
