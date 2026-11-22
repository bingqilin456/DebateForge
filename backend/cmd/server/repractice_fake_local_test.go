package main

import (
	"context"
	"errors"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
)

// V2-08 局部重练 Fake 装配的三种分支：
//   - 未设置开关：关闭且不报错；
//   - 开关齐全但环境不正确：关闭并返回受控错误；
//   - 条件齐全但生成器/存储不支持：返回受控错误；
//   - 条件齐全且都支持：打开能力。

func TestRepracticeFakeDisabledWithoutSwitch(t *testing.T) {
	t.Setenv(repracticeEnableEnv, "")
	enabled, err := repracticeFakeEnabled()
	if err != nil || enabled {
		t.Fatalf("未设置开关时应关闭且无错误，got enabled=%v err=%v", enabled, err)
	}
}

func TestRepracticeFakeRejectsNonFakeModeOrNonTestEnv(t *testing.T) {
	cases := []struct{ name, mode, env string }{
		{"非 Fake 模式", "disabled", "test"},
		{"非测试环境", "fake", "dev"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(repracticeEnableEnv, repracticeEnableValue)
			t.Setenv("AI_MODE", tc.mode)
			t.Setenv("APP_ENV", tc.env)
			enabled, err := repracticeFakeEnabled()
			if enabled || err == nil {
				t.Fatalf("不满足固定条件时必须关闭并报错，got enabled=%v err=%v", enabled, err)
			}
		})
	}
}

func TestRepracticeCapabilityRequiresGeneratorAndStoreSupport(t *testing.T) {
	t.Setenv(repracticeEnableEnv, repracticeEnableValue)
	t.Setenv("AI_MODE", "fake")
	t.Setenv("APP_ENV", "test")

	// 生成器不具备局部重练能力：明确失败，不能静默打开半个能力。
	service := debate.NewService(&repracticeUnsupportedStore{}, unavailableGenerator{}, debate.NewGate(nil))
	if enabled, err := applyRepracticeCapability(service, unavailableGenerator{}); enabled || err == nil {
		t.Fatalf("生成器不支持时应报错，got enabled=%v err=%v", enabled, err)
	}
	// 存储不具备局部重练能力：同样明确失败。
	service = debate.NewService(&repracticeUnsupportedStore{}, ai.NewFake(), debate.NewGate(nil))
	if enabled, err := applyRepracticeCapability(service, ai.NewFake()); enabled || err == nil {
		t.Fatalf("存储不支持时应报错，got enabled=%v err=%v", enabled, err)
	}
}

// repracticeUnsupportedStore 只实现既有 Store 的最小声明，用来证明能力装配
// 会拒绝“存储没有局部重练边界”的环境。
type repracticeUnsupportedStore struct{}

func (s *repracticeUnsupportedStore) Create(context.Context, uint64, debate.CreateInput) (debate.Debate, error) {
	return debate.Debate{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) List(context.Context, uint64, int, int) (debate.DebateList, error) {
	return debate.DebateList{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) Detail(context.Context, uint64, uint64) (debate.Detail, error) {
	return debate.Detail{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) Claim(context.Context, uint64, uint64, debate.SubmitInput, func() error) (debate.Detail, bool, error) {
	return debate.Detail{}, false, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) ClaimRetry(context.Context, uint64, uint64, uint64, uint64, func() error) (debate.Detail, bool, error) {
	return debate.Detail{}, false, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) Complete(context.Context, uint64, debate.Debate, debate.Turn, ai.Result, bool) (debate.Detail, error) {
	return debate.Detail{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) Fail(context.Context, uint64, debate.Debate, debate.Turn, string, ai.Usage, ai.Metadata) (debate.Turn, error) {
	return debate.Turn{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) GetSummary(context.Context, uint64, uint64) (*debate.Summary, error) {
	return nil, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) ClaimSummary(context.Context, uint64, uint64, debate.SummaryInput, func() error) (debate.Detail, bool, error) {
	return debate.Detail{}, false, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) CompleteSummary(context.Context, uint64, debate.Debate, debate.Summary, ai.SummaryResult) (debate.Summary, error) {
	return debate.Summary{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) FailSummary(context.Context, uint64, debate.Debate, debate.Summary, string, ai.Usage, ai.Metadata) (debate.Summary, error) {
	return debate.Summary{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) GetStrategy(context.Context, uint64, uint64) (debate.StrategyState, error) {
	return debate.StrategyState{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) ClaimStrategy(context.Context, uint64, uint64, debate.StrategyInput, func() error) (debate.StrategyState, bool, error) {
	return debate.StrategyState{}, false, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) CompleteStrategy(context.Context, uint64, debate.Debate, debate.Strategy, ai.StrategyResult) (debate.Strategy, error) {
	return debate.Strategy{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) FailStrategy(context.Context, uint64, debate.Debate, debate.Strategy, string, ai.Usage, ai.Metadata) (debate.Strategy, error) {
	return debate.Strategy{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) GetReview(context.Context, uint64, uint64) (*debate.Review, error) {
	return nil, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) ClaimReview(context.Context, uint64, uint64, debate.ReviewInput, func() error) (debate.ReviewClaim, bool, error) {
	return debate.ReviewClaim{}, false, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) CompleteReview(context.Context, uint64, debate.Debate, debate.Review, string, ai.Usage, ai.Metadata) (debate.Review, error) {
	return debate.Review{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) FailReview(context.Context, uint64, debate.Debate, debate.Review, string, ai.Usage, ai.Metadata) (debate.Review, error) {
	return debate.Review{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) ListAnnotations(context.Context, uint64, uint64) ([]debate.AnnotationBatch, error) {
	return nil, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) ClaimAnnotation(context.Context, uint64, uint64, uint64, uint64, func() error) (debate.AnnotationClaim, bool, error) {
	return debate.AnnotationClaim{}, false, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) CompleteAnnotation(context.Context, uint64, debate.Debate, debate.TurnAnnotation, ai.AnnotationResult) (debate.TurnAnnotation, error) {
	return debate.TurnAnnotation{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) FailAnnotation(context.Context, uint64, debate.Debate, debate.TurnAnnotation, string, ai.Usage, ai.Metadata) (debate.TurnAnnotation, error) {
	return debate.TurnAnnotation{}, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) RecoverExpiredLeases(context.Context) (int, error) {
	return 0, errors.New("未实现")
}
func (s *repracticeUnsupportedStore) RecoverExpiredReviewLeases(context.Context) (int, error) {
	return 0, errors.New("未实现")
}
