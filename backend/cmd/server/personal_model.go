package main

import (
	"context"
	"encoding/hex"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"gorm.io/gorm"
)

func businessGenerator(ctx context.Context, logger *slog.Logger, db *gorm.DB) (ai.Generator, error) {
	if os.Getenv("AI_MODE") != "byok" {
		return configuredGenerator(ctx, logger)
	}
	path := os.Getenv("BYOK_MASTER_KEY_FILE")
	if !filepath.IsAbs(path) {
		return nil, errors.New("BYOK_MASTER_KEY_FILE 须为秘密文件绝对路径")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, errors.New("个人模型加密密钥文件不可用")
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, 129))
	if err != nil || len(raw) > 128 {
		return nil, errors.New("个人模型加密密钥文件无效")
	}
	key, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return nil, errors.New("个人模型加密密钥文件无效")
	}
	defer clear(key)
	return debate.NewPersonalModel(db, key)
}
