package main

import (
	"errors"
	"os"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
)

// V2-08 局部重练的启动装配，沿用批注/复盘同一套“默认关闭、显式开启”规则。
//
// 局部重练是产品 V2 的新能力，首版只有 Fake 实现与独立新表；若默认开启，
// 任何 Fake 环境都会凭空产生版本行与额外模型调用，而且旧部署在尚未执行
// 00018 前会因缺表而失败。因此必须同时满足：
//   - 显式开关 V2_REPRACTICE_FAKE=1；
//   - AI_MODE=fake；
//   - APP_ENV=test；
//   - 存储与生成器分别实现 debate.RepracticeStore / ai.RepracticeGenerator。
//
// 关闭时：读接口返回 available=false，写接口 400，不产生版本行、不调用模型，
// 也不因缺表影响旧原文、总结、批注与复盘的读取。

const (
	// repracticeEnableEnv 是唯一允许开启局部重练切片的开关。
	repracticeEnableEnv = "V2_REPRACTICE_FAKE"
	// repracticeEnableValue 是开关的唯一合法取值。
	repracticeEnableValue = "1"
)

// repracticeFakeEnabled 判断是否满足固定本地测试条件；任何一项不满足都返回关闭。
func repracticeFakeEnabled() (bool, error) {
	if os.Getenv(repracticeEnableEnv) != repracticeEnableValue {
		return false, nil
	}
	if os.Getenv("AI_MODE") != "fake" {
		return false, errors.New("局部重练 Fake 切片只允许 AI_MODE=fake")
	}
	if os.Getenv("APP_ENV") != "test" {
		return false, errors.New("局部重练 Fake 切片只允许 APP_ENV=test")
	}
	return true, nil
}

// applyRepracticeCapability 在满足条件时把局部重练能力打开到服务上。
func applyRepracticeCapability(service *debate.Service, generator ai.Generator) (bool, error) {
	if service == nil {
		return false, nil
	}
	enabled, err := repracticeFakeEnabled()
	if err != nil || !enabled {
		return false, err
	}
	if _, ok := generator.(ai.RepracticeGenerator); !ok {
		return false, errors.New("局部重练 Fake 切片要求生成器实现局部重练能力")
	}
	if err := service.EnableRepractice(); err != nil {
		return false, err
	}
	return true, nil
}
