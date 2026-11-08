package main

import (
	"context"
	"errors"
	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"log/slog"
	"os"
	"strconv"
)

// 未显式选择模式时关闭真实调用；不会因为 .env 中已有 Key 就启用收费入口。
type unavailableGenerator struct{}

func (unavailableGenerator) Generate(context.Context, ai.Request) (ai.Result, error) {
	return ai.Result{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "真实模型入口未启用"}
}
func configuredGenerator(ctx context.Context, logger *slog.Logger) (ai.Generator, error) {
	switch os.Getenv("AI_MODE") {
	case "", "disabled":
		return unavailableGenerator{}, nil
	case "fake":
		return ai.NewFake(), nil
	case "deepseek":
		// 只有显式模式才读取密钥。开启本地真实调用须另获负责人授权；不支持公网监听。
		limit, e := strconv.Atoi(os.Getenv("AI_MAX_OUTPUT_TOKENS"))
		if e != nil {
			return nil, errors.New("必须显式配置 AI_MAX_OUTPUT_TOKENS")
		}
		g, e := ai.NewOpenAIGenerator(ctx, ai.OpenAIConfig{APIKey: os.Getenv("DEEPSEEK_API_KEY"), BaseURL: "https://api.deepseek.com", Model: "deepseek-flash", MaxOutputTokens: limit, Window: ai.WindowLimits{ContextTokens: 1000000, MaxOutputTokens: limit, SummaryPromptReserveTokens: 60000, SummaryOutputReserveTokens: 4096}, Logger: logger})
		if e != nil {
			return nil, errors.New("模型配置无效")
		}
		return g, nil
	default:
		return nil, errors.New("AI_MODE 必须为 disabled、fake 或 deepseek")
	}
}
