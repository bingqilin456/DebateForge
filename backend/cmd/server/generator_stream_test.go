package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 未启用的真实模型入口必须对流式也 fail closed，不能因不支持而退回普通生成。
func TestGeneratorDefaultStreamFailsClosed(t *testing.T) {
	t.Setenv("AI_MODE", "")
	t.Setenv("DEEPSEEK_API_KEY", "synthetic-unused-key")
	g, err := configuredGenerator(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	streamer, ok := g.(ai.StreamGenerator)
	if !ok {
		t.Fatal("默认生成器必须显式实现流式拒绝")
	}
	emitted := 0
	_, streamErr := streamer.Stream(context.Background(), ai.Request{}, func(string) error {
		emitted++
		return nil
	})
	var controlled *ai.Error
	if !errors.As(streamErr, &controlled) || controlled.Code != ai.CodeModelUnavailable {
		t.Fatalf("默认入口流式必须受控拒绝，实际 %v", streamErr)
	}
	if emitted != 0 {
		t.Fatalf("拒绝时不得伪造增量，实际 %d 次", emitted)
	}
}

func TestGeneratorFakeStreamIsIncremental(t *testing.T) {
	t.Setenv("AI_MODE", "fake")
	g, err := configuredGenerator(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	streamer, ok := g.(ai.StreamGenerator)
	if !ok {
		t.Fatal("Fake 生成器必须实现流式")
	}
	req := ai.Request{
		Mode: ai.ModeQuick3, AIStyle: ai.StyleLogical,
		Topic:      ai.Topic{Title: "合成辩题", Description: "合成描述"},
		UserStance: ai.StancePro, RoundNo: 1, UserContent: "合成发言",
	}
	var emitted []string
	result, err := streamer.Stream(context.Background(), req, func(text string) error {
		emitted = append(emitted, text)
		return nil
	})
	if err != nil {
		t.Fatalf("Fake 流式不应报错: %v", err)
	}
	if len(emitted) < 2 {
		t.Fatalf("Fake 必须真的分块，实际 %v", emitted)
	}
	if strings.Join(emitted, "") != result.Content {
		t.Fatalf("增量拼接与最终结果不一致: %v vs %q", emitted, result.Content)
	}
}

// 部署探针的流式必须保持原场景语义：failure 不回调，timeout 等 Context。
func TestFakeProbeStreamScenarios(t *testing.T) {
	t.Setenv("AI_MODE", "fake")
	t.Setenv("APP_ENV", "deploy")
	t.Setenv("T07_FAKE_SCENARIO", "failure")
	g, err := configuredGenerator(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	streamer, ok := g.(ai.StreamGenerator)
	if !ok {
		t.Fatal("部署探针必须实现流式")
	}
	emitted := 0
	_, streamErr := streamer.Stream(context.Background(), ai.Request{}, func(string) error {
		emitted++
		return nil
	})
	var controlled *ai.Error
	if !errors.As(streamErr, &controlled) || controlled.Code != ai.CodeModelUnavailable {
		t.Fatalf("failure 场景必须受控失败，实际 %v", streamErr)
	}
	if emitted != 0 {
		t.Fatalf("failure 场景不得回调增量，实际 %d 次", emitted)
	}

	t.Setenv("T07_FAKE_SCENARIO", "timeout")
	g, err = configuredGenerator(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	streamer = g.(ai.StreamGenerator)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := streamer.Stream(ctx, ai.Request{}, func(string) error { return nil }); err == nil {
		t.Fatal("timeout 场景必须随 Context 结束返回错误")
	}
}

// hold 场景必须先等待再产生结果，且不得绕过 Fake 的回复校验。
func TestFakeProbeStreamHoldUsesFake(t *testing.T) {
	t.Setenv("AI_MODE", "fake")
	t.Setenv("APP_ENV", "deploy")
	t.Setenv("T07_FAKE_SCENARIO", "hold")
	g, err := configuredGenerator(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	streamer := g.(ai.StreamGenerator)
	req := ai.Request{
		Mode: ai.ModeQuick3, AIStyle: ai.StyleLogical,
		Topic:      ai.Topic{Title: "合成辩题", Description: "合成描述"},
		UserStance: ai.StancePro, RoundNo: 1, UserContent: "合成发言",
	}
	var emitted []string
	result, err := streamer.Stream(context.Background(), req, func(text string) error {
		emitted = append(emitted, text)
		return nil
	})
	if err != nil {
		t.Fatalf("hold 场景应最终成功: %v", err)
	}
	if strings.Join(emitted, "") != result.Content || len(emitted) < 2 {
		t.Fatalf("hold 场景应透传 Fake 的真实分块: emitted=%v result=%q", emitted, result.Content)
	}
}
