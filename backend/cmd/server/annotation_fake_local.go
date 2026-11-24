package main

import (
	"errors"
	"os"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
)

// 批注的启动装配只有一条允许路径：显式本地测试开关 + AI_MODE=fake + APP_ENV=test。
//
// 采用“默认关闭、显式开启”的理由：批注当前只有 Fake 实现，真实供应商接入
// （模型、费用、总并发、前台优先与限额）属于另一项独立授权任务；
// 若默认开启，任何 Fake 环境都会凭空产生批注批次，使旧行为与费用边界失控。
// 关闭时 Complete 不设置资格标志、不启动分析，读取接口对旧会话返回空列表。
//
// 只接受 APP_ENV=test：本切片是固定隔离测试环境的验收对象，
// dev/demo 虽然不是生产环境，但它们有独立的人工联调用途，
// 不应因为本切片而在开发者日常环境里静默产生批注批次与额外模型调用。

const (
	// annotationEnableEnv 是唯一允许开启本切片的开关。
	annotationEnableEnv = "V2_ANNOTATION_FAKE"
	// annotationEnableValue 是开关的唯一合法取值，避免 "0"/"false" 被误解为开启。
	annotationEnableValue = "1"
)

// annotationFakeEnabled 判断是否满足固定本地测试条件；任何一项不满足都返回关闭。
func annotationFakeEnabled() (bool, error) {
	if os.Getenv(annotationEnableEnv) != annotationEnableValue {
		return false, nil
	}
	if os.Getenv("AI_MODE") != "fake" {
		return false, errors.New("批注 Fake 切片只允许 AI_MODE=fake")
	}
	// 只允许显式测试环境：dev/demo 与生产一律拒绝，避免日常联调环境被静默开启。
	if os.Getenv("APP_ENV") != "test" {
		return false, errors.New("批注 Fake 切片只允许 APP_ENV=test")
	}
	return true, nil
}

// applyAnnotationCapability 在满足条件时把批注能力打开到服务上。
//
// 同时要求生成器实际实现 ai.AnnotationGenerator：Fake 满足，disabled 与现有
// 真实试运行包装器（以及 demo 包装器）不满足，因此不会绕过其专用费用账本。
func applyAnnotationCapability(service *debate.Service, generator ai.Generator) (bool, error) {
	if service == nil {
		return false, nil
	}
	if trial, ok := generator.(interface{ DemoAnnotationsEnabled() bool }); ok && trial.DemoAnnotationsEnabled() {
		service.EnableAnnotations(true)
		return true, nil
	}
	enabled, err := annotationFakeEnabled()
	if err != nil || !enabled {
		return false, err
	}
	if _, ok := generator.(ai.AnnotationGenerator); !ok {
		return false, errors.New("批注 Fake 切片要求生成器实现批注能力")
	}
	service.EnableAnnotations(true)
	return true, nil
}
