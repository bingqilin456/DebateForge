package main

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 显式账号选择隔离场景；不使用进程内首调计数，也不读写数据库。
func strategyFakeUsers() (uint64, uint64, error) {
	parse := func(key string) (uint64, error) {
		raw := os.Getenv(key)
		if raw == "" {
			return 0, nil
		}
		id, err := strconv.ParseUint(raw, 10, 64)
		if err != nil || id == 0 {
			return 0, errors.New("策略 Fake 账号 ID 必须为非零 uint64")
		}
		return id, nil
	}
	failure, err := parse("P3_STRATEGY_FAKE_FAILURE_USER_ID")
	if err != nil {
		return 0, 0, err
	}
	slow, err := parse("P3_STRATEGY_FAKE_SLOW_USER_ID")
	if err != nil {
		return 0, 0, err
	}
	if failure == 0 && slow == 0 {
		return 0, 0, nil
	}
	if os.Getenv("AI_MODE") != "fake" {
		return 0, 0, errors.New("策略故障装配只允许 AI_MODE=fake")
	}
	switch os.Getenv("APP_ENV") {
	case "dev", "test", "demo":
	default:
		return 0, 0, errors.New("策略故障装配只允许显式本地 dev/test/demo")
	}
	if failure != 0 && failure == slow {
		return 0, 0, errors.New("策略失败与延迟账号必须不同")
	}
	if os.Getenv("T07_FAKE_SCENARIO") != "" {
		return 0, 0, errors.New("策略本地 Fake 不能与部署探针混用")
	}
	return failure, slow, nil
}

type localFakeCapabilities interface {
	ai.Generator
	ai.StreamGenerator
	ai.SummaryGenerator
	ai.StrategyGenerator
}

type localStrategyFake struct {
	localFakeCapabilities
	failureUser, slowUser uint64
	delay                 time.Duration
}

func (g localStrategyFake) GenerateStrategy(ctx context.Context, req ai.StrategyRequest) (ai.StrategyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	contextFailure := func() (ai.StrategyResult, error) {
		code := ai.CodeModelCanceled
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = ai.CodeModelTimeout
		}
		return ai.StrategyResult{}, (&ai.Error{Code: code, Message: "本地策略 Fake 等待已终止", Metadata: ai.Metadata{Model: "fake", PromptVersion: ai.StrategyPromptVersion}}).WithCause(ctx.Err())
	}
	if ctx.Err() != nil {
		return contextFailure()
	}
	if req.UserID == g.failureUser && req.AttemptVersion == 1 {
		return ai.StrategyResult{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "本地策略 Fake 首次生成失败", Metadata: ai.Metadata{Model: "fake", PromptVersion: ai.StrategyPromptVersion}}
	}
	if req.UserID == g.slowUser {
		timer := time.NewTimer(g.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return contextFailure()
		case <-timer.C:
		}
	}
	return g.localFakeCapabilities.GenerateStrategy(ctx, req)
}
