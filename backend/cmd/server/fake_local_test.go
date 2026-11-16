package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

func TestLocalStreamFakeIsBoundedAndRejectsDeployment(t *testing.T) {
	t.Setenv("T15_FAKE_SCENARIO", "slow")
	t.Setenv("APP_ENV", "deploy")
	if _, err := configuredT15Fake(); err == nil {
		t.Fatal("local fault scenarios must reject deployment")
	}
	t.Setenv("APP_ENV", "test")
	g, err := configuredT15Fake()
	if err != nil {
		t.Fatal(err)
	}
	streamer, ok := g.(interface {
		Stream(context.Context, ai.Request, func(string) error) (ai.Result, error)
	})
	if !ok {
		t.Fatal("missing stream capability")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	_, err = streamer.Stream(ctx, ai.Request{UserID: 1, Mode: ai.ModeStandard4, AIStyle: ai.StyleLogical, Topic: ai.Topic{Title: "测试辩题", Description: "测试描述"}, UserStance: ai.StancePro, RoundNo: 1, UserContent: "测试发言"}, func(string) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "中断") {
		t.Fatalf("must stop on cancellation: %v", err)
	}
}
