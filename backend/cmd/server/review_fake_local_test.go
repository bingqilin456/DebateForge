package main

import (
	"context"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
)

// 复盘启动装配沿用 V2-04 的白名单。仅允许在测试与部署 Fake 模式下显式启用。
// 关闭时既不启用能力，也不访问 debate_reviews 新表。

func TestReviewFakeEnabledRequiresAllConditions(t *testing.T) {
	cases := []struct {
		name    string
		env     map[string]string
		enabled bool
		wantErr bool
	}{
		{"默认关闭", map[string]string{"APP_ENV": "test", "AI_MODE": "fake"}, false, false},
		{"开关为 0", map[string]string{"V2_REVIEW_FAKE": "0", "APP_ENV": "test", "AI_MODE": "fake"}, false, false},
		{"开关为 true 文本", map[string]string{"V2_REVIEW_FAKE": "true", "APP_ENV": "test", "AI_MODE": "fake"}, false, false},
		{"开启但模式非 fake", map[string]string{"V2_REVIEW_FAKE": "1", "APP_ENV": "test", "AI_MODE": "deepseek"}, false, true},
		{"开启但模式 disabled", map[string]string{"V2_REVIEW_FAKE": "1", "APP_ENV": "test", "AI_MODE": "disabled"}, false, true},
		{"部署环境默认关闭", map[string]string{"APP_ENV": "deploy", "AI_MODE": "fake"}, false, false},
		{"部署环境显式开启 Fake", map[string]string{"V2_REVIEW_FAKE": "1", "APP_ENV": "deploy", "AI_MODE": "fake"}, true, false},
		{"部署环境禁止真实模式", map[string]string{"V2_REVIEW_FAKE": "1", "APP_ENV": "deploy", "AI_MODE": "deepseek"}, false, true},
		{"部署环境禁止个人密钥模式", map[string]string{"V2_REVIEW_FAKE": "1", "APP_ENV": "deploy", "AI_MODE": "byok"}, false, true},
		{"开启但 dev 环境", map[string]string{"V2_REVIEW_FAKE": "1", "APP_ENV": "dev", "AI_MODE": "fake"}, false, true},
		{"开启但 demo 环境", map[string]string{"V2_REVIEW_FAKE": "1", "APP_ENV": "demo", "AI_MODE": "fake"}, false, true},
		{"开启但未设置 APP_ENV", map[string]string{"V2_REVIEW_FAKE": "1", "AI_MODE": "fake"}, false, true},
		{"开启且 test fake", map[string]string{"V2_REVIEW_FAKE": "1", "APP_ENV": "test", "AI_MODE": "fake"}, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range []string{"V2_REVIEW_FAKE", "APP_ENV", "AI_MODE"} {
				t.Setenv(key, tc.env[key])
			}
			enabled, err := reviewFakeEnabled()
			if enabled != tc.enabled || (err != nil) != tc.wantErr {
				t.Fatalf("enabled=%v err=%v，期望 enabled=%v wantErr=%v", enabled, err, tc.enabled, tc.wantErr)
			}
		})
	}
}

func TestApplyReviewCapabilityRequiresGeneratorSupport(t *testing.T) {
	t.Setenv("V2_REVIEW_FAKE", "1")
	t.Setenv("APP_ENV", "test")
	t.Setenv("AI_MODE", "fake")
	service := debate.NewService(newFakeStore(), ai.NewFake(), debate.NewGate(nil))
	enabled, err := applyReviewCapability(service, ai.NewFake())
	if err != nil || !enabled {
		t.Fatalf("Fake 应能开启复盘: enabled=%v err=%v", enabled, err)
	}
	// 不支持复盘的生成器必须受控拒绝，不能伪装开启。
	if _, err := applyReviewCapability(service, unavailableGenerator{}); err == nil {
		t.Fatal("不支持复盘的生成器必须拒绝开启")
	}
	// 关闭条件不满足时保持关闭且不报错。
	t.Setenv("V2_REVIEW_FAKE", "")
	enabled, err = applyReviewCapability(service, ai.NewFake())
	if err != nil || enabled {
		t.Fatalf("未显式开启时应保持关闭: enabled=%v err=%v", enabled, err)
	}
}

// 关闭复盘能力时，服务不会扫描新表：既有的过期租约回收保持原行为。
func TestReviewLeaseRecoverySkipsNewTableWhenDisabled(t *testing.T) {
	store := &reviewLeaseStore{}
	service := debate.NewService(store, ai.NewFake(), debate.NewGate(nil))
	if _, err := service.RecoverExpiredLeases(t.Context()); err != nil {
		t.Fatalf("关闭复盘时回收不应失败: %v", err)
	}
	if store.reviewScanned {
		t.Fatal("关闭复盘时不得扫描 debate_reviews")
	}
}

// 启用复盘时才允许扫描新表。
func TestReviewLeaseRecoveryScansNewTableWhenEnabled(t *testing.T) {
	store := &reviewLeaseStore{}
	service := debate.NewService(store, ai.NewFake(), debate.NewGate(nil))
	service.EnableReviews(true)
	if _, err := service.RecoverExpiredLeases(t.Context()); err != nil {
		t.Fatalf("启用复盘后回收不应失败: %v", err)
	}
	if !store.reviewScanned {
		t.Fatal("启用复盘后应扫描 debate_reviews")
	}
}

// reviewLeaseStore 只实现两个租约回收方法；其余方法嵌入 nil Store，
// 任何被调用的业务方法都会 panic，从而证明这些用例只经过租约路径。
type reviewLeaseStore struct {
	debate.Store
	reviewScanned bool
}

func (s *reviewLeaseStore) RecoverExpiredLeases(ctx context.Context) (int, error) { return 0, nil }

func (s *reviewLeaseStore) RecoverExpiredReviewLeases(ctx context.Context) (int, error) {
	s.reviewScanned = true
	return 0, nil
}
