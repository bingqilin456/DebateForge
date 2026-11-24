package main

import (
	"errors"
	"os"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
)

// V2-13 复盘的启动装配与 V2-04 批注保持同一套“默认关闭、显式开启”规则。
//
// 复盘是产品 V2 的新能力，首版只有 Fake 实现与独立新表；
// 若默认开启，任何 Fake 环境都会凭空产生复盘行与额外模型调用，
// 而且旧部署在尚未执行 00016 前会因缺表而失败。因此必须同时满足：
//   - 显式开关 V2_REVIEW_FAKE=1；
//   - AI_MODE=fake；
//   - APP_ENV=test 或 deploy；
//   - 生成器实际实现 ai.ReviewGenerator。
//
// 关闭时：读接口仍返回 not_started，写接口返回 400，不触发任何模型调用；
// 定期租约回收也不会扫描这张新表，旧原文与旧总结的读取完全不受影响。

const (
	// reviewEnableEnv 是唯一允许开启复盘切片的开关。
	reviewEnableEnv = "V2_REVIEW_FAKE"
	// reviewEnableValue 是开关的唯一合法取值。
	reviewEnableValue = "1"
)

// reviewFakeEnabled 校验 Fake 模式与允许显式启用的环境。
func reviewFakeEnabled() (bool, error) {
	if os.Getenv(reviewEnableEnv) != reviewEnableValue {
		return false, nil
	}
	if os.Getenv("AI_MODE") != "fake" {
		return false, errors.New("复盘 Fake 切片只允许 AI_MODE=fake")
	}
	env := os.Getenv("APP_ENV")
	if env != "test" && env != "deploy" {
		return false, errors.New("复盘 Fake 切片只允许 APP_ENV=test 或 deploy")
	}
	return true, nil
}

// applyReviewCapability 在满足条件时把复盘能力打开到服务上。
func applyReviewCapability(service *debate.Service, generator ai.Generator) (bool, error) {
	if service == nil {
		return false, nil
	}
	enabled, err := reviewFakeEnabled()
	if err != nil || !enabled {
		return false, err
	}
	if _, ok := generator.(ai.ReviewGenerator); !ok {
		return false, errors.New("复盘 Fake 切片要求生成器实现复盘能力")
	}
	service.EnableReviews(true)
	return true, nil
}
