package main

import (
	"context"
	"errors"
	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

func configuredDemoGenerator(ctx context.Context, logger *slog.Logger) (ai.Generator, error) {
	root, keyPath := os.Getenv("DEMO_LEDGER"), os.Getenv("DEEPSEEK_API_KEY_FILE")
	if runtime.GOOS != "linux" || os.Getenv("APP_ENV") != "demo" || !filepath.IsAbs(root) || !filepath.IsAbs(keyPath) {
		return nil, errors.New("本地 Demo 要求 Linux 专用环境、账本及密钥文件")
	}
	file, err := os.Open(keyPath)
	if err != nil {
		return nil, errors.New("本地 Demo 密钥文件不可用")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 1025))
	if err != nil || len(raw) > 1024 || strings.TrimSpace(string(raw)) == "" {
		return nil, errors.New("本地 Demo 密钥文件无效")
	}
	cfg := ai.OpenAIConfig{APIKey: strings.TrimSpace(string(raw)), BaseURL: "https://api.deepseek.com", Window: ai.WindowLimits{ContextTokens: 1000000, SummaryPromptReserveTokens: 60000, SummaryOutputReserveTokens: 8192}, Logger: logger}
	return ai.NewDemoGenerator(ctx, cfg, root)
}
