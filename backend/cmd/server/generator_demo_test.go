package main

import (
	"context"
	"log/slog"
	"testing"
)

func TestLocalDemoCannotBorrowOtherEnvironment(t *testing.T) {
	t.Setenv("AI_MODE", "local-demo")
	for _, env := range []string{"dev", "test", "deploy", "demo"} {
		t.Run(env, func(t *testing.T) {
			t.Setenv("APP_ENV", env)
			t.Setenv("DEMO_LEDGER", "")
			if _, err := configuredGenerator(context.Background(), slog.Default()); err == nil {
				t.Fatal("unapproved real path enabled")
			}
		})
	}
}
