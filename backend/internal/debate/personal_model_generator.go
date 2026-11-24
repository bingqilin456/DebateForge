package debate

import (
	"context"
	"errors"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 生成只读取当前请求所属用户的 Key。各能力仍用原有适配器与业务认领流程。
func (p *PersonalModel) adapter(ctx context.Context, uid uint64) (*ai.OpenAIAdapter, personalModelRow, error) {
	row, key, err := p.key(ctx, uid)
	if errors.Is(err, errPersonalKeyRequired) {
		return nil, row, nil
	}
	if err != nil {
		return nil, row, &ai.Error{Code: ai.CodeModelUnavailable, Message: "个人模型配置不可用"}
	}
	adapter, err := p.adapterForConfiguration(ctx, row.BaseURL, row.Model, key)
	return adapter, row, err
}

func (p *PersonalModel) adapterForConfiguration(ctx context.Context, baseURL, model, key string) (*ai.OpenAIAdapter, error) {
	const output = 2048
	g, err := ai.NewOpenAIGenerator(ctx, ai.OpenAIConfig{
		APIKey: key, BaseURL: baseURL, Model: model, MaxOutputTokens: output,
		Window:     ai.WindowLimits{ContextTokens: 1000000, MaxOutputTokens: output, SummaryPromptReserveTokens: 60000, SummaryOutputReserveTokens: 4096},
		HTTPClient: p.client,
	})
	if err != nil {
		return nil, err
	}
	return g.(*ai.OpenAIAdapter), nil
}

func (p *PersonalModel) Generate(ctx context.Context, req ai.Request) (ai.Result, error) {
	if req.ForceFake {
		return p.fake.Generate(ctx, req)
	}
	g, row, err := p.adapter(ctx, req.UserID)
	if err != nil {
		return ai.Result{}, err
	}
	if g == nil {
		if req.RequireReal {
			return ai.Result{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "请恢复本场使用的模型配置后重试"}
		}
		return p.fake.Generate(ctx, req)
	}
	result, err := g.Generate(ctx, req)
	if err == nil {
		_ = p.mark(ctx, row, "used")
	}
	return result, err
}

func (p *PersonalModel) Stream(ctx context.Context, req ai.Request, emit func(string) error) (ai.Result, error) {
	if req.ForceFake {
		return p.fake.Stream(ctx, req, emit)
	}
	g, row, err := p.adapter(ctx, req.UserID)
	if err != nil {
		return ai.Result{}, err
	}
	if g == nil {
		if req.RequireReal {
			return ai.Result{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "请恢复本场使用的模型配置后重试"}
		}
		return p.fake.Stream(ctx, req, emit)
	}
	result, err := g.Stream(ctx, req, emit)
	if err == nil {
		_ = p.mark(ctx, row, "used")
	}
	return result, err
}

func (p *PersonalModel) GenerateSummary(ctx context.Context, req ai.SummaryRequest) (ai.SummaryResult, error) {
	if req.ForceFake {
		return p.fake.GenerateSummary(ctx, req)
	}
	g, _, err := p.adapter(ctx, req.UserID)
	if err != nil {
		return ai.SummaryResult{}, err
	}
	if g == nil {
		return p.fake.GenerateSummary(ctx, req)
	}
	return g.GenerateSummary(ctx, req)
}

func (p *PersonalModel) GenerateStrategy(ctx context.Context, req ai.StrategyRequest) (ai.StrategyResult, error) {
	if req.ForceFake {
		return p.fake.GenerateStrategy(ctx, req)
	}
	g, _, err := p.adapter(ctx, req.UserID)
	if err != nil {
		return ai.StrategyResult{}, err
	}
	if g == nil {
		return p.fake.GenerateStrategy(ctx, req)
	}
	return g.GenerateStrategy(ctx, req)
}

func (p *PersonalModel) GenerateReview(ctx context.Context, req ai.ReviewRequest) (ai.ReviewResult, error) {
	if req.ForceFake {
		return p.fake.GenerateReview(ctx, req)
	}
	g, _, err := p.adapter(ctx, req.UserID)
	if err != nil {
		return ai.ReviewResult{}, err
	}
	if g == nil {
		return p.fake.GenerateReview(ctx, req)
	}
	return g.GenerateReview(ctx, req)
}

func (p *PersonalModel) GenerateAnnotations(ctx context.Context, req ai.AnnotationRequest) (ai.AnnotationResult, error) {
	if req.ForceFake {
		return p.fake.GenerateAnnotations(ctx, req)
	}
	g, _, err := p.adapter(ctx, req.UserID)
	if err != nil {
		return ai.AnnotationResult{}, err
	}
	if g == nil {
		return p.fake.GenerateAnnotations(ctx, req)
	}
	return g.GenerateAnnotations(ctx, req)
}
