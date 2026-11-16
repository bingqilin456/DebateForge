package main

import (
	"context"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 本文件让进程入口的三种生成器都显式暴露流式能力：
// 未启用时 fail closed，Fake 与真实试运行走真实增量，不退回普通生成整流伪造。
// 只做装配适配，不改变费用、授权或配置约束。

// Stream 对未启用的真实模型入口显式拒绝，不能因为不支持流式而退回普通生成。
func (unavailableGenerator) Stream(context.Context, ai.Request, func(string) error) (ai.Result, error) {
	return ai.Result{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "真实模型入口未启用"}
}

// Stream 让部署探针保持与 Generate 相同的场景语义，只是把结果按增量回放。
// failure 场景直接失败且不回调；timeout 场景等待 Context 结束；
// hold 场景先等待 3 秒再走 Fake，用于验证客户端取消与等待边界。
func (p fakeProbe) Stream(ctx context.Context, req ai.Request, emit func(string) error) (ai.Result, error) {
	switch p.scenario {
	case "failure":
		return ai.Result{}, &ai.Error{Code: ai.CodeModelUnavailable, Message: "合成供应商失败"}
	case "timeout":
		<-ctx.Done()
		return ai.Result{}, ctx.Err()
	default:
		timer := time.NewTimer(3 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ai.Result{}, ctx.Err()
		case <-timer.C:
			return p.fake.Stream(ctx, req, emit)
		}
	}
}

// 编译期断言：入口装配的三种生成器都必须满足可选流式接口。
var (
	_ ai.StreamGenerator = unavailableGenerator{}
	_ ai.StreamGenerator = fakeProbe{}
)
