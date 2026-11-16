package debate

import (
	"context"
	"errors"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

type strategyGeneratorFunc func(context.Context, ai.StrategyRequest) (ai.StrategyResult, error)

func (g strategyGeneratorFunc) Generate(context.Context, ai.Request) (ai.Result, error) {
	return ai.Result{}, nil
}
func (g strategyGeneratorFunc) GenerateStrategy(c context.Context, r ai.StrategyRequest) (ai.StrategyResult, error) {
	return g(c, r)
}

type strategyFailureStore struct {
	Store
	usage ai.Usage
	meta  ai.Metadata
}

func (s *strategyFailureStore) FailStrategy(ctx context.Context, _ uint64, _ Debate, st Strategy, code string, usage ai.Usage, meta ai.Metadata) (Strategy, error) {
	if ctx.Err() != nil {
		return Strategy{}, ctx.Err()
	}
	s.usage = usage
	s.meta = meta
	st.Status = "failed"
	st.ErrorCode = &code
	return st, nil
}
func TestStrategyValidationFailurePreservesGeneratorUsage(t *testing.T) {
	store := &strategyFailureStore{}
	service := NewService(store, strategyGeneratorFunc(func(context.Context, ai.StrategyRequest) (ai.StrategyResult, error) {
		return ai.StrategyResult{ContentJSON: `{}`, Usage: ai.Usage{PromptTokens: ai.TokenCount{Known: true, Value: 17}}, Metadata: ai.Metadata{Model: "synthetic"}}, nil
	}), nil)
	_, _, err := service.runStrategyGeneration(context.Background(), 1, StrategyState{Debate: sample().Debate, Strategy: &Strategy{AttemptVersion: 1, Status: "generating"}}, false)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Status != 502 || store.usage.PromptTokens.Value != 17 || !store.usage.PromptTokens.Known || store.meta.Model != "synthetic" || store.meta.PromptVersion != "strategy-v1" {
		t.Fatalf("lost billing metadata: %+v %+v %v", store.usage, store.meta, err)
	}
}

func TestStrategyGenerationPassesClaimedAttemptVersion(t *testing.T) {
	store := &strategyFailureStore{}
	var got uint64
	service := NewService(store, strategyGeneratorFunc(func(_ context.Context, req ai.StrategyRequest) (ai.StrategyResult, error) {
		got = req.AttemptVersion
		return ai.StrategyResult{}, &ai.Error{Code: ai.CodeModelUnavailable}
	}), nil)
	_, _, err := service.runStrategyGeneration(context.Background(), 1, StrategyState{Debate: sample().Debate, Strategy: &Strategy{AttemptVersion: 7, Status: "generating"}}, true)
	var failure *Failure
	if got != 7 || !errors.As(err, &failure) || failure.Details.AttemptVersion != 7 {
		t.Fatalf("lost claimed version: %d %v", got, err)
	}
}
