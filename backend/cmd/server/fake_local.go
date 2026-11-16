package main

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 仅用于固定隔离库的本地联调，不能借此启用真实模型或部署探针。
func configuredT15Fake() (ai.Generator, error) {
	scenario := os.Getenv("T15_FAKE_SCENARIO")
	if scenario == "" {
		return ai.NewFake(), nil
	}
	if env := os.Getenv("APP_ENV"); env != "dev" && env != "test" && env != "demo" {
		return nil, errors.New("T15 Fake 场景只允许本地 dev/test/demo")
	}
	switch scenario {
	case "slow", "failure", "timeout", "heartbeat":
		return localStreamFake{scenario: scenario, fake: ai.NewFake()}, nil
	}
	return nil, errors.New("未知 T15 Fake 场景")
}

type localStreamFake struct {
	scenario string
	fake     *ai.Fake
}

func (g localStreamFake) Generate(ctx context.Context, req ai.Request) (ai.Result, error) {
	return g.fake.Generate(ctx, req)
}

func (g localStreamFake) GenerateSummary(ctx context.Context, req ai.SummaryRequest) (ai.SummaryResult, error) {
	return g.fake.GenerateSummary(ctx, req)
}
func (g localStreamFake) GenerateStrategy(ctx context.Context, req ai.StrategyRequest) (ai.StrategyResult, error) {
	return g.fake.GenerateStrategy(ctx, req)
}

func (g localStreamFake) Stream(ctx context.Context, req ai.Request, emit func(string) error) (ai.Result, error) {
	if emit == nil {
		return ai.Result{}, &ai.Error{Code: ai.CodeModelError}
	}
	result, err := g.fake.Generate(ctx, req)
	if err != nil {
		return result, err
	}
	text := []rune(result.Content)
	for i := 0; i < len(text); i += 6 {
		if err := ctx.Err(); err != nil {
			return ai.Result{}, (&ai.Error{Code: ai.CodeModelCanceled, Message: "合成流已中断", Usage: result.Usage, Metadata: result.Metadata}).WithCause(err)
		}
		end := i + 6
		if end > len(text) {
			end = len(text)
		}
		if err := emit(string(text[i:end])); err != nil {
			return ai.Result{}, err
		}
		if g.scenario == "failure" {
			return ai.Result{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "合成流内失败", Usage: result.Usage, Metadata: result.Metadata}
		}
		if g.scenario == "timeout" {
			<-ctx.Done()
			return ai.Result{}, (&ai.Error{Code: ai.CodeModelTimeout, Message: "合成流超时", Usage: result.Usage, Metadata: result.Metadata}).WithCause(ctx.Err())
		}
		delay := 200 * time.Millisecond
		if g.scenario == "heartbeat" && i == 0 {
			delay = 16 * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ai.Result{}, (&ai.Error{Code: ai.CodeModelCanceled, Message: "合成流已中断", Usage: result.Usage, Metadata: result.Metadata}).WithCause(ctx.Err())
		case <-timer.C:
		}
	}
	return result, nil
}
