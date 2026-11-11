package main

import (
	"context"
	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"io"
	"log/slog"
	"testing"
)

func TestGeneratorDefaultDoesNotEnableExistingKey(t *testing.T) {
	t.Setenv("AI_MODE", "")
	t.Setenv("DEEPSEEK_API_KEY", "synthetic-unused-key")
	g, e := configuredGenerator(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	_, err := g.Generate(context.Background(), ai.Request{})
	if e, ok := err.(*ai.Error); !ok || e.Code != ai.CodeModelUnavailable {
		t.Fatal("default must not call provider")
	}
}
func TestGeneratorExplicitModes(t *testing.T) {
	t.Setenv("AI_MODE", "fake")
	g, e := configuredGenerator(context.Background(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := g.(*ai.Fake); !ok {
		t.Fatal("fake not selected")
	}
	t.Setenv("AI_MODE", "deepseek")
	t.Setenv("APP_ENV", "dev")
	t.Setenv("DEEPSEEK_API_KEY", "synthetic-unused-key")
	if _, e = configuredGenerator(context.Background(), nil); e == nil {
		t.Fatal("local DeepSeek mode must stay disabled")
	}
}
