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
	t.Setenv("DEEPSEEK_API_KEY", "synthetic-unused-key")
	t.Setenv("AI_MAX_OUTPUT_TOKENS", "512")
	g, e = configuredGenerator(context.Background(), nil)
	if e != nil {
		t.Fatal("construct adapter without network")
	}
	if _, ok := g.(*ai.OpenAIAdapter); !ok {
		t.Fatal("T04 adapter not wired")
	}
	t.Setenv("AI_MAX_OUTPUT_TOKENS", "")
	if _, e = configuredGenerator(context.Background(), nil); e == nil {
		t.Fatal("output budget must be explicit")
	}
}
