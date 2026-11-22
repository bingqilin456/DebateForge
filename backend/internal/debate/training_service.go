package debate

import (
	"context"
	"errors"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 产品 V2 训练赛的服务编排。与 V1 一样：规则与事务边界在 Service，
// 持久化与事务内一致性校验在 Repository，模型调用一律在短事务之外。

// CreateTraining 创建一场训练赛。赛制由服务端固定为 training_v2，
// 客户端不接受 mode 字段；创建不产生模型调用。
func (s *Service) CreateTraining(ctx context.Context, uid, tid uint64, stance, style string) (Debate, error) {
	if uid == 0 || tid == 0 || (stance != "pro" && stance != "con") || !ai.AIStyle(style).Valid() {
		return Debate{}, ErrInvalidRequest
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return Debate{}, dependency(err, false)
	}
	return s.store.CreateTraining(ctx, uid, tid, stance, style)
}

// TrainingDetail 读取训练赛权威进度与已提交步骤。
func (s *Service) TrainingDetail(ctx context.Context, uid, id uint64) (TrainingDetail, error) {
	if err := s.waitStartupScan(ctx); err != nil {
		return TrainingDetail{}, dependency(err, false)
	}
	return s.store.GetTrainingDetail(ctx, uid, id)
}

// SubmitTrainingStep 是训练赛步骤提交入口（普通 JSON 与流式共用）。
//
// 认领先于生成，且与 V1 轮次同源：client_request_id 幂等、attempt_version 版本、
// 90 秒租约与并发配额。用户步骤在认领事务内直接落库并推进阶段；
// AI 步骤取得并发槽后在事务外生成，只有最终事务提交成功才返回结果或发送 done。
func (s *Service) SubmitTrainingStep(ctx context.Context, uid, id uint64, in TrainingStepInput, stream *turnStream) (TrainingStepResult, int, error) {
	in, err := ValidateTrainingStepInput(in)
	if err != nil {
		return TrainingStepResult{}, 0, err
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return TrainingStepResult{}, 0, dependency(err, false)
	}
	return runClaimedTurn(s.gate, uid, func(admit func() error) (TrainingDetail, bool, error) {
		return s.store.ClaimTrainingStep(ctx, uid, id, in, admit)
	}, func(d TrainingDetail) (TrainingStepResult, int, error) {
		return s.trainingReplay(d, in)
	}, func(d TrainingDetail) (TrainingStepResult, int, error) {
		// 用户步骤已在认领事务内提交完成，不调用模型。
		if step, ok := stepByRequestID(d.Steps, in.ClientRequestID); ok {
			result := trainingResult(d, &step)
			return result, 201, nil
		}
		claim, ok := claimByRequestID(d.Claims, in.ClientRequestID)
		if !ok {
			return TrainingStepResult{}, 0, dependency(nil, false)
		}
		if stream != nil {
			if err := stream.start(formatID(claim.ID)); err != nil {
				return TrainingStepResult{}, 0, s.saveTrainingFailure(ctx, uid, d.Debate, claim, "generation_cancelled", 503, ai.Usage{}, ai.Metadata{}, err)
			}
			return s.runTrainingGeneration(ctx, uid, d, claim, stream.delta, 201)
		}
		return s.runTrainingGeneration(ctx, uid, d, claim, nil, 201)
	})
}

// RetryTrainingStep 只在失败 AI 步骤且期望版本匹配时重新认领，复用同一幂等行。
func (s *Service) RetryTrainingStep(ctx context.Context, uid, id uint64, stepNo int, in TrainingRetryInput) (TrainingStepResult, int, error) {
	in, err := ValidateTrainingRetryInput(in)
	if err != nil {
		return TrainingStepResult{}, 0, err
	}
	if stepNo < 1 || stepNo > trainingMaxSteps {
		return TrainingStepResult{}, 0, ErrInvalidRequest
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return TrainingStepResult{}, 0, dependency(err, false)
	}
	var permit *Permit
	consumed := false
	defer func() { permit.Finish(consumed) }()
	d, fresh, err := s.store.ClaimTrainingRetry(ctx, uid, id, stepNo, in.ExpectedAttemptVersion, func() error {
		var e error
		permit, e = s.gate.Acquire(uid)
		return e
	})
	if err != nil {
		var f *Failure
		if errors.As(err, &f) && f.CommitUnknown {
			consumed = true
		}
		return TrainingStepResult{}, 0, err
	}
	if !fresh {
		claim, ok := activeClaimForStep(d.Claims, stepNo)
		if !ok {
			return TrainingStepResult{}, 0, ErrNotFound
		}
		switch claim.Status {
		case "completed":
			if step, exists := stepByStepNo(d.Steps, stepNo); exists {
				result := trainingResult(d, &step)
				return result, 200, nil
			}
			return TrainingStepResult{}, 0, dependency(nil, false)
		case "generating":
			result := trainingResult(d, nil)
			return result, 202, nil
		case "failed":
			return TrainingStepResult{}, 0, trainingFailure(d.Debate, claim)
		case "abandoned":
			return TrainingStepResult{}, 0, ErrRoundConflict
		}
		return TrainingStepResult{}, 0, dependency(nil, false)
	}
	consumed = true
	claim, ok := activeClaimForStep(d.Claims, stepNo)
	if !ok {
		return TrainingStepResult{}, 0, dependency(nil, false)
	}
	return s.runTrainingGeneration(ctx, uid, d, claim, nil, 200)
}

// EndFreeDebate 是用户显式「进入结辩」的业务操作：不调用模型、不占用并发槽；
// 重放不重复切换；与发言提交竞争时由服务端事务裁决。
func (s *Service) EndFreeDebate(ctx context.Context, uid, id uint64, in TrainingEndInput) (TrainingStepResult, int, error) {
	if err := s.waitStartupScan(ctx); err != nil {
		return TrainingStepResult{}, 0, dependency(err, false)
	}
	d, err := s.store.EndFreeDebate(ctx, uid, id, in)
	if err != nil {
		return TrainingStepResult{}, 0, err
	}
	return trainingResult(d, nil), 200, nil
}

// trainingReplay 把「非新认领」的认领结果映射为受控响应：
// 已提交步骤 200、生成中 202、已失败 409 turn_failed、已放弃 409 round_conflict。
func (s *Service) trainingReplay(d TrainingDetail, in TrainingStepInput) (TrainingStepResult, int, error) {
	if step, ok := stepByRequestID(d.Steps, in.ClientRequestID); ok {
		result := trainingResult(d, &step)
		return result, 200, nil
	}
	if claim, ok := claimByRequestID(d.Claims, in.ClientRequestID); ok {
		switch claim.Status {
		case "generating":
			return trainingResult(d, nil), 202, nil
		case "completed":
			if step, exists := stepByRequestID(d.Steps, claim.ClientRequestID); exists {
				result := trainingResult(d, &step)
				return result, 200, nil
			}
			return TrainingStepResult{}, 0, dependency(nil, false)
		case "failed":
			return TrainingStepResult{}, 0, trainingFailure(d.Debate, claim)
		case "abandoned":
			return TrainingStepResult{}, 0, ErrRoundConflict
		}
		return TrainingStepResult{}, 0, dependency(nil, false)
	}
	// 同一步骤已有其他幂等键的认领：返回该步骤状态，不新开第二条生成。
	pos, err := trainingPosition(d.Debate, len(d.Steps))
	if err != nil {
		return TrainingStepResult{}, 0, err
	}
	if claim, ok := activeClaimForStep(d.Claims, pos.nextStepNo()); ok {
		switch claim.Status {
		case "generating":
			return trainingResult(d, nil), 202, nil
		case "failed":
			return TrainingStepResult{}, 0, trainingFailure(d.Debate, claim)
		case "abandoned":
			return TrainingStepResult{}, 0, ErrRoundConflict
		case "completed":
			if step, exists := stepByStepNo(d.Steps, claim.StepNo); exists {
				result := trainingResult(d, &step)
				return result, 200, nil
			}
		}
	}
	return TrainingStepResult{}, 0, dependency(nil, false)
}

// runTrainingGeneration 在认领事务之外调用模型并写回最终结果。
func (s *Service) runTrainingGeneration(ctx context.Context, uid uint64, d TrainingDetail, claim TrainingClaim, emit func(string) error, successStatus int) (TrainingStepResult, int, error) {
	pos, err := trainingPosition(d.Debate, len(d.Steps))
	if err != nil {
		return TrainingStepResult{}, 0, err
	}
	req, err := trainingRequest(uid, d.Debate, pos, d.Steps)
	if err != nil {
		return TrainingStepResult{}, 0, err
	}
	result, genErr := s.generateTurn(ctx, req, emit)
	if genErr == nil {
		genErr = ai.ValidateReply(result.Content)
		if failure, ok := genErr.(*ai.Error); ok {
			clone := *failure
			clone.Usage = result.Usage
			clone.Metadata = result.Metadata
			genErr = &clone
		}
	}
	if ctx.Err() != nil {
		genErr = generationContextError(genErr, result, ctx.Err())
	}
	if genErr != nil {
		code, status, usage, meta := classifyFailure(ctx, genErr)
		return TrainingStepResult{}, 0, s.saveTrainingFailure(ctx, uid, d.Debate, claim, code, status, usage, meta, genErr)
	}
	completed, err := s.store.CompleteTrainingStep(ctx, uid, d.Debate, claim, result)
	if err != nil {
		var failure *Failure
		if errors.As(err, &failure) && failure.CommitUnknown {
			// 可能已经完成，不能把未知提交结果当成确定回滚或失败。
			return TrainingStepResult{}, 0, dependency(err, true)
		}
		code, status := "dependency_unavailable", 503
		if ctx.Err() != nil {
			code, status, _, _ = classifyFailure(ctx, generationContextError(nil, result, ctx.Err()))
		}
		return TrainingStepResult{}, 0, s.saveTrainingFailure(ctx, uid, d.Debate, claim, code, status, result.Usage, result.Metadata, err)
	}
	step, ok := stepByStepNo(completed.Steps, claim.StepNo)
	if !ok {
		return TrainingStepResult{}, 0, dependency(nil, false)
	}
	return trainingResult(completed, &step), successStatus, nil
}

// saveTrainingFailure 尽力在取消后落库失败状态，仍由状态与版本保护。
func (s *Service) saveTrainingFailure(ctx context.Context, uid uint64, d Debate, claim TrainingClaim, code string, status int, usage ai.Usage, meta ai.Metadata, cause error) error {
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer stop()
	saved, err := s.store.FailTrainingClaim(cleanup, uid, d, claim, code, usage, meta)
	if err != nil {
		return dependency(errors.Join(err, cause), false)
	}
	if status == 0 {
		status = 503
	}
	return &Failure{Code: codeForHTTP(code), Status: status, Cause: cause, Details: trainingFailedDetails(d, saved)}
}

// trainingFailure 由已持久化的失败认领构造 409 turn_failed。
func trainingFailure(d Debate, claim TrainingClaim) error {
	return &Failure{Code: "turn_failed", Status: 409, Cause: ErrTurnFailed, Details: trainingFailedDetails(d, claim)}
}

func trainingFailedDetails(d Debate, claim TrainingClaim) *FailureDetails {
	return &FailureDetails{TurnID: formatID(claim.ID), Status: "failed", AttemptVersion: claim.AttemptVersion, QueryURL: trainingQueryURL(d.ID)}
}

// trainingResult 组装训练赛响应；step 为 nil 时表示本次没有提交新的正式步骤。
func trainingResult(d TrainingDetail, step *TrainingStep) TrainingStepResult {
	result := TrainingStepResult{
		Progress:     d.Progress,
		DebateStatus: d.Debate.Status,
		QueryURL:     trainingQueryURL(d.Debate.ID),
	}
	if step != nil {
		dto := step.DTO()
		result.Step = &dto
	}
	return result
}

// stepByStepNo 返回指定步骤序号的正式步骤。
func stepByStepNo(steps []TrainingStep, stepNo int) (TrainingStep, bool) {
	for _, step := range steps {
		if step.StepNo == stepNo {
			return step, true
		}
	}
	return TrainingStep{}, false
}
