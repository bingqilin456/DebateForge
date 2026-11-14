package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 未显式选择模式时关闭真实调用；已有本地 Key 不会启用收费入口。
type unavailableGenerator struct{}

func (unavailableGenerator) Generate(context.Context, ai.Request) (ai.Result, error) {
	return ai.Result{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "真实模型入口未启用"}
}

func configuredGenerator(ctx context.Context, logger *slog.Logger) (ai.Generator, error) {
	switch os.Getenv("AI_MODE") {
	case "", "disabled":
		return unavailableGenerator{}, nil
	case "fake":
		return configuredFakeGenerator()
	case "deepseek":
		return configuredTrialGenerator(ctx, logger)
	case "local-demo":
		return configuredDemoGenerator(ctx, logger)
	default:
		return nil, errors.New("AI_MODE 必须为 disabled、fake、deepseek 或 local-demo")
	}
}

func configuredTrialGenerator(ctx context.Context, logger *slog.Logger) (ai.Generator, error) {
	keyPath := os.Getenv("DEEPSEEK_API_KEY_FILE")
	if os.Getenv("APP_ENV") != "deploy" || !filepath.IsAbs(keyPath) {
		return nil, errors.New("真实试运行要求部署模式和专用密钥文件")
	}
	keyFile, err := os.Open(keyPath)
	if err != nil {
		return nil, errors.New("试运行密钥文件不可用")
	}
	defer keyFile.Close()
	key, err := io.ReadAll(io.LimitReader(keyFile, 1025))
	if err != nil || len(key) > 1024 || strings.TrimSpace(string(key)) == "" {
		return nil, errors.New("试运行密钥文件无效")
	}
	const outputLimit = 1024
	cfg := ai.OpenAIConfig{
		APIKey:          strings.TrimSpace(string(key)),
		BaseURL:         "https://api.deepseek.com",
		Model:           "deepseek-flash",
		MaxOutputTokens: outputLimit,
		Window:          ai.WindowLimits{ContextTokens: 1000000, MaxOutputTokens: outputLimit, SummaryPromptReserveTokens: 60000, SummaryOutputReserveTokens: 4096},
		Logger:          logger,
	}
	return ai.NewTrialGenerator(ctx, cfg, os.Getenv("T07_TRIAL_LEDGER"), os.Getenv("T07_TRIAL_STAGE"), os.Stdout)
}

// 临时部署探针只使用 Fake。三种模式分别覆盖错误、真实超时边界和并发测试等待。
func configuredFakeGenerator() (ai.Generator, error) {
	scenario := os.Getenv("T07_FAKE_SCENARIO")
	if scenario == "" {
		return configuredT15Fake()
	}
	if os.Getenv("APP_ENV") != "deploy" {
		return nil, errors.New("部署探针只能在部署模式启用")
	}
	switch scenario {
	case "failure", "timeout", "hold":
		return fakeProbe{scenario: scenario, fake: ai.NewFake()}, nil
	default:
		return nil, errors.New("未知的 Fake 部署探针")
	}
}

type fakeProbe struct {
	scenario string
	fake     *ai.Fake
}

func (p fakeProbe) Generate(ctx context.Context, req ai.Request) (ai.Result, error) {
	switch p.scenario {
	case "failure":
		return ai.Result{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "合成供应商失败"}
	case "timeout":
		<-ctx.Done()
		return ai.Result{}, ctx.Err()
	default:
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ai.Result{}, ctx.Err()
		case <-timer.C:
			return p.fake.Generate(ctx, req)
		}
	}
}
