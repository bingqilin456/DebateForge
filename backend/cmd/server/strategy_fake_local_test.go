package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

func strategyFakeEnv(t *testing.T) {
	t.Helper()
	t.Setenv("AI_MODE", "fake")
	t.Setenv("APP_ENV", "test")
	t.Setenv("T07_FAKE_SCENARIO", "")
	t.Setenv("T15_FAKE_SCENARIO", "")
	t.Setenv("P3_STRATEGY_FAKE_FAILURE_USER_ID", "11")
	t.Setenv("P3_STRATEGY_FAKE_SLOW_USER_ID", "22")
}

func TestStrategyFakeAssemblyGuards(t *testing.T) {
	for _, tc := range []struct{ key, value string }{
		{"AI_MODE", "disabled"}, {"AI_MODE", ""}, {"AI_MODE", "deepseek"}, {"AI_MODE", "local-demo"},
		{"APP_ENV", "deploy"}, {"APP_ENV", ""}, {"APP_ENV", "production"},
		{"P3_STRATEGY_FAKE_FAILURE_USER_ID", "0"}, {"P3_STRATEGY_FAKE_FAILURE_USER_ID", "-1"},
		{"P3_STRATEGY_FAKE_FAILURE_USER_ID", "abc"}, {"P3_STRATEGY_FAKE_FAILURE_USER_ID", "18446744073709551616"},
		{"P3_STRATEGY_FAKE_SLOW_USER_ID", "11"}, {"T07_FAKE_SCENARIO", "hold"},
	} {
		t.Run(tc.key+"="+tc.value, func(t *testing.T) {
			strategyFakeEnv(t)
			t.Setenv(tc.key, tc.value)
			if _, err := configuredGenerator(context.Background(), nil); err == nil {
				t.Fatal("unsafe assembly accepted")
			}
		})
	}
}

func TestStrategyFakeOptionalUsersAndDefault(t *testing.T) {
	for _, env := range []string{"dev", "test", "demo"} {
		t.Run(env, func(t *testing.T) {
			strategyFakeEnv(t)
			t.Setenv("APP_ENV", env)
			for _, disabled := range []string{"P3_STRATEGY_FAKE_FAILURE_USER_ID", "P3_STRATEGY_FAKE_SLOW_USER_ID"} {
				t.Setenv(disabled, "")
				if _, err := configuredGenerator(context.Background(), nil); err != nil {
					t.Fatal(err)
				}
			}
			g, err := configuredGenerator(context.Background(), nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := g.(*ai.Fake); !ok {
				t.Fatal("empty selectors changed ordinary Fake")
			}
		})
	}
}

func TestStrategyFakeFailureIsPerAttemptAndUser(t *testing.T) {
	strategyFakeEnv(t)
	g, err := configuredGenerator(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	sg := g.(ai.StrategyGenerator)
	req := ai.StrategyRequest{AttemptVersion: 1, UserID: 11, Mode: ai.ModeStandard4, AIStyle: ai.StyleLogical, Topic: ai.Topic{Title: "测试辩题", Description: "测试说明"}, UserStance: ai.StancePro}
	// 不依赖进程中的“第一次调用”：同版本再次调用仍失败，另一会话也一样。
	for i := 0; i < 2; i++ {
		_, err = sg.GenerateStrategy(context.Background(), req)
		var ae *ai.Error
		if !errors.As(err, &ae) || ae.Code != ai.CodeModelUnavailable {
			t.Fatalf("first attempt must fail: %v", err)
		}
	}
	req.UserID = 33
	if _, err = sg.GenerateStrategy(context.Background(), req); err != nil {
		t.Fatal("other user affected", err)
	}
	req.UserID = 11
	req.AttemptVersion = 2
	if result, err := sg.GenerateStrategy(context.Background(), req); err != nil || result.Strategy.Thesis == "" {
		t.Fatalf("explicit retry must succeed: %v", err)
	}
	// 新装配也按持久版本运行，不能重置为全局首败。
	g, err = configuredGenerator(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = g.(ai.StrategyGenerator).GenerateStrategy(context.Background(), req); err != nil {
		t.Fatal(err)
	}
}

func TestStrategyFakeDelayCancellationAndSSE(t *testing.T) {
	strategyFakeEnv(t)
	t.Setenv("T15_FAKE_SCENARIO", "failure")
	g, err := configuredGenerator(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	req := ai.StrategyRequest{UserID: 22, Mode: ai.ModeStandard4, AIStyle: ai.StyleLogical, Topic: ai.Topic{Title: "测试辩题", Description: "测试说明"}, UserStance: ai.StancePro}
	for _, timeout := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		if timeout {
			cancel()
			ctx, cancel = context.WithTimeout(context.Background(), 15*time.Millisecond)
		} else {
			cancel()
		}
		started := time.Now()
		_, err = g.(ai.StrategyGenerator).GenerateStrategy(ctx, req)
		cancel()
		var ae *ai.Error
		if !errors.As(err, &ae) || (ae.Code != ai.CodeModelCanceled && ae.Code != ai.CodeModelTimeout) || time.Since(started) > time.Second {
			t.Fatalf("cancel/timeout: %v", err)
		}
	}
	chunks := 0
	_, err = g.(ai.StreamGenerator).Stream(context.Background(), ai.Request{UserID: 11, Mode: req.Mode, AIStyle: req.AIStyle, Topic: req.Topic, UserStance: req.UserStance, RoundNo: 1, UserContent: "测试发言"}, func(string) error { chunks++; return nil })
	var ae *ai.Error
	if chunks != 1 || !errors.As(err, &ae) || ae.Code != ai.CodeModelUnavailable {
		t.Fatalf("SSE scenario lost: chunks=%d err=%v", chunks, err)
	}
}

func TestStrategyFakeDelayCompletesAndCancelsDuringWait(t *testing.T) {
	strategyFakeEnv(t)
	g, err := configuredGenerator(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	fake := g.(localStrategyFake)
	fake.delay = 40 * time.Millisecond
	req := ai.StrategyRequest{UserID: 22, AttemptVersion: 1, Mode: ai.ModeStandard4, AIStyle: ai.StyleLogical, Topic: ai.Topic{Title: "测试辩题", Description: "测试说明"}, UserStance: ai.StancePro}
	start := time.Now()
	result, err := fake.GenerateStrategy(context.Background(), req)
	if err != nil || result.Strategy.Thesis == "" || time.Since(start) < 40*time.Millisecond {
		t.Fatalf("delay completion: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(5*time.Millisecond, cancel)
	defer cancel()
	_, err = fake.GenerateStrategy(ctx, req)
	var ae *ai.Error
	if !errors.As(err, &ae) || ae.Code != ai.CodeModelCanceled {
		t.Fatalf("in-flight cancel: %v", err)
	}
}
