package main

import (
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
)

// 批注启动装配是“默认关闭、显式开启”的白名单：任何一项条件不满足都必须保持关闭，
// 且关闭时不改变既有服务行为。全部使用内存 Store 与 Fake。

func TestAnnotationFakeEnabledRequiresAllConditions(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		enabled bool
		wantErr bool
	}{
		{"默认关闭", map[string]string{"APP_ENV": "test", "AI_MODE": "fake"}, false, false},
		{"开关为 0", map[string]string{"V2_ANNOTATION_FAKE": "0", "APP_ENV": "test", "AI_MODE": "fake"}, false, false},
		{"开关为 true 文本", map[string]string{"V2_ANNOTATION_FAKE": "true", "APP_ENV": "test", "AI_MODE": "fake"}, false, false},
		{"开启但模式非 fake", map[string]string{"V2_ANNOTATION_FAKE": "1", "APP_ENV": "test", "AI_MODE": "deepseek"}, false, true},
		{"开启但模式 disabled", map[string]string{"V2_ANNOTATION_FAKE": "1", "APP_ENV": "test", "AI_MODE": "disabled"}, false, true},
		{"部署环境默认关闭", map[string]string{"APP_ENV": "deploy", "AI_MODE": "fake"}, false, false},
		{"部署环境显式开启 Fake", map[string]string{"V2_ANNOTATION_FAKE": "1", "APP_ENV": "deploy", "AI_MODE": "fake"}, true, false},
		{"部署环境禁止真实模式", map[string]string{"V2_ANNOTATION_FAKE": "1", "APP_ENV": "deploy", "AI_MODE": "deepseek"}, false, true},
		{"部署环境禁止个人密钥模式", map[string]string{"V2_ANNOTATION_FAKE": "1", "APP_ENV": "deploy", "AI_MODE": "byok"}, false, true},
		// dev 与 demo 有独立的人工联调用途，不能在这些环境产生批注批次。
		{"开启但 dev 环境", map[string]string{"V2_ANNOTATION_FAKE": "1", "APP_ENV": "dev", "AI_MODE": "fake"}, false, true},
		{"开启但 demo 环境", map[string]string{"V2_ANNOTATION_FAKE": "1", "APP_ENV": "demo", "AI_MODE": "fake"}, false, true},
		{"开启但未设置 APP_ENV", map[string]string{"V2_ANNOTATION_FAKE": "1", "AI_MODE": "fake"}, false, true},
		{"开启且 test fake", map[string]string{"V2_ANNOTATION_FAKE": "1", "APP_ENV": "test", "AI_MODE": "fake"}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"V2_ANNOTATION_FAKE", "APP_ENV", "AI_MODE"} {
				t.Setenv(key, tc.env[key])
			}
			enabled, err := annotationFakeEnabled()
			if enabled != tc.enabled || (err != nil) != tc.wantErr {
				t.Fatalf("enabled=%v err=%v，期望 enabled=%v wantErr=%v", enabled, err, tc.enabled, tc.wantErr)
			}
		})
	}
}

func TestApplyAnnotationCapabilityRequiresGeneratorSupport(t *testing.T) {
	t.Setenv("V2_ANNOTATION_FAKE", "1")
	t.Setenv("APP_ENV", "test")
	t.Setenv("AI_MODE", "fake")
	store := newFakeStore()
	service := debate.NewService(store, ai.NewFake(), debate.NewGate(nil))
	enabled, err := applyAnnotationCapability(service, ai.NewFake())
	if err != nil || !enabled {
		t.Fatalf("Fake 应能开启批注: enabled=%v err=%v", enabled, err)
	}
	// 不具备批注能力（也不实现 Generator）的生成器必须受控拒绝，不能伪装开启。
	if _, err := applyAnnotationCapability(service, unavailableGenerator{}); err == nil {
		t.Fatal("不支持批注的生成器必须拒绝开启")
	}
	// 关闭条件不满足时保持关闭且不报错。
	t.Setenv("V2_ANNOTATION_FAKE", "")
	enabled, err = applyAnnotationCapability(service, ai.NewFake())
	if err != nil || enabled {
		t.Fatalf("未显式开启时应保持关闭: enabled=%v err=%v", enabled, err)
	}
}

// fakeStore 只满足 debate.Store 的最小实现，用于装配层测试；任何业务调用都会 panic，
// 因为这些用例只验证开关，不验证生成行为。
type fakeStore struct{ debate.Store }

func newFakeStore() *fakeStore { return &fakeStore{} }
