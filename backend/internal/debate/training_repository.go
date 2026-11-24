package debate

import (
	"context"
	"errors"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// 训练赛的持久化路径。所有写操作都在短事务内先锁 debates 行，
// 再读写 debate_steps / debate_step_claims，保证阶段推进只有一个事实源。
// 模型调用一律在事务外进行（见 training_service.go）。

// readTrainingSteps 按 step_no 升序读取已提交正式步骤。
func readTrainingSteps(tx *gorm.DB, debateID uint64) ([]TrainingStep, error) {
	steps := []TrainingStep{}
	if err := tx.Where("debate_id = ?", debateID).Order("step_no ASC").Find(&steps).Error; err != nil {
		return steps, dependency(err, false)
	}
	return steps, nil
}

// readTrainingClaims 按 id 升序读取认领记录；调用方按 step_no 选择最新一条。
func readTrainingClaims(tx *gorm.DB, debateID uint64, lock bool) ([]TrainingClaim, error) {
	claims := []TrainingClaim{}
	q := tx.Where("debate_id = ?", debateID)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.Order("id ASC").Find(&claims).Error; err != nil {
		return claims, dependency(err, false)
	}
	return claims, nil
}

// trainingDetailTx 在事务内组装训练赛完整状态。
func trainingDetailTx(tx *gorm.DB, row Debate) (TrainingDetail, error) {
	steps, err := readTrainingSteps(tx, row.ID)
	if err != nil {
		return TrainingDetail{}, err
	}
	claims, err := readTrainingClaims(tx, row.ID, false)
	if err != nil {
		return TrainingDetail{}, err
	}
	return TrainingDetail{
		Debate:   row,
		Steps:    steps,
		Claims:   claims,
		Progress: trainingProgressDTO(row, steps, claims),
	}, nil
}

// requireTraining 拒绝把 V1 轮次接口用于训练赛，避免两套状态机互相踩踏。
func requireTraining(d Debate) error {
	if !d.IsTraining() {
		return ErrRoundConflict
	}
	return nil
}

// GetTrainingDetail 读取训练赛权威进度与全部已提交步骤。
func (r *Repository) GetTrainingDetail(ctx context.Context, uid, id uint64) (detail TrainingDetail, err error) {
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, id, false)
		if e != nil {
			return e
		}
		if e = requireTraining(row); e != nil {
			return e
		}
		detail, e = trainingDetailTx(tx, row)
		return e
	})
	return
}

// ClaimTrainingStep 是训练赛两种步骤提交共用的认领事务：
//
//   - 用户步骤：直接落正式步骤并推进阶段，不调用模型、不占并发槽；
//   - AI 步骤：创建或复用 debate_step_claims 行，取得并发槽后由 Service 在事务外生成。
//
// 幂等键与版本语义与 V1 轮次一致：同一 client_request_id 重放只返回已存状态，
// 同一 step_no 已有未完成认领时返回该认领状态，不重复生成、不重复计费。
func (r *Repository) ClaimTrainingStep(ctx context.Context, uid, id uint64, in TrainingStepInput, admit func() error) (detail TrainingDetail, fresh bool, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, id, true)
		if e != nil {
			return e
		}
		if e = requireTraining(row); e != nil {
			return e
		}
		steps, e := readTrainingSteps(tx, row.ID)
		if e != nil {
			return e
		}
		claims, e := readTrainingClaims(tx, row.ID, true)
		if e != nil {
			return e
		}
		detail = TrainingDetail{Debate: row, Steps: steps, Claims: claims, Progress: trainingProgressDTO(row, steps, claims)}
		// 幂等重放优先于任何阶段/期望校验：原请求已经成功落库的部分必须可被安全重读。
		// 用户步骤还须比对原文；AI 步骤正文由服务端生成，客户端请求不含内容，因此只按幂等键定位。
		if step, ok := stepByRequestID(steps, in.ClientRequestID); ok {
			if step.Stage != in.ExpectedStage || step.StepNo != in.ExpectedStep ||
				(step.Speaker == ai.SpeakerUser && step.Content != in.Content) ||
				(step.Speaker == ai.SpeakerAssistant && in.Content != "") {
				return ErrIdempotencyConflict
			}
			detail.Progress = trainingProgressDTO(row, steps, claims)
			return nil
		}
		if claim, ok := claimByRequestID(claims, in.ClientRequestID); ok {
			if in.Content != "" || claim.StepNo != in.ExpectedStep {
				return ErrIdempotencyConflict
			}
			if claim.Status == "generating" || claim.Status == "failed" || claim.Status == "completed" || claim.Status == "abandoned" {
				detail.Progress = trainingProgressDTO(row, steps, claims)
				return nil
			}
			return dependency(errors.New("unexpected training claim status"), false)
		}
		pos, e := trainingPosition(row, len(steps))
		if e != nil {
			return e
		}
		if pos.Status != "active" || pos.Stage != in.ExpectedStage || pos.nextStepNo() != in.ExpectedStep {
			return ErrRoundConflict
		}
		speaker, ok := pos.nextSpeaker()
		if !ok {
			return ErrRoundConflict
		}
		switch speaker {
		case ai.SpeakerUser:
			if in.Content == "" {
				return ErrInvalidRequest
			}
			step, e := trainingStepForCommit(row, pos, speaker, in.Content, in.ClientRequestID)
			if e != nil {
				return e
			}
			if e = tx.Create(&step).Error; e != nil {
				return dependency(e, false)
			}
			next, e := advanceTrainingRow(tx, row, pos)
			if e != nil {
				return e
			}
			steps = append(steps, step)
			detail, e = trainingDetailTxWith(tx, next, steps)
			fresh = true
			return e
		case ai.SpeakerAssistant:
			if in.Content != "" {
				// 服务端判定当前应由 AI 发言；客户端却带用户内容，说明双方对进度的
				// 认知不一致，一律按状态冲突处理，不静默丢弃或改写用户输入。
				return ErrRoundConflict
			}
			if existing, ok := activeClaimForStep(claims, pos.nextStepNo()); ok {
				if existing.Status == "generating" || existing.Status == "failed" {
					detail.Progress = trainingProgressDTO(row, steps, claims)
					return nil
				}
				return ErrRoundConflict
			}
			if e = admit(); e != nil {
				return e
			}
			now := r.now()
			lease := now.Add(LeaseDuration)
			claim := TrainingClaim{DebateID: row.ID, StepNo: pos.nextStepNo(), ClientRequestID: in.ClientRequestID, Status: "generating", AttemptVersion: 1, LeaseExpiresAt: &lease, CreatedAt: now}
			if e = tx.Create(&claim).Error; e != nil {
				return dependency(e, false)
			}
			claims = append(claims, claim)
			detail.Claims = claims
			detail.Progress = trainingProgressDTO(row, steps, claims)
			fresh = true
			return nil
		default:
			return dependency(errors.New("training speaker invalid"), false)
		}
	})
	if err != nil {
		fresh = false
	}
	return
}

// advanceTrainingRow 以阶段/步数/回合数作为条件更新 debates，避免并发覆盖；
// 条件不匹配说明有另一个事务先推进，返回状态冲突。返回推进后的会话行。
func advanceTrainingRow(tx *gorm.DB, row Debate, pos TrainingPosition) (Debate, error) {
	next := row
	if e := advanceTraining(&next); e != nil {
		return row, e
	}
	update := tx.Model(&Debate{}).
		Where("id = ? AND user_id = ? AND mode = ? AND training_stage = ? AND training_stage_step = ? AND completed_rounds = ?",
			row.ID, row.UserID, row.Mode, row.TrainingStageValue(), pos.StageStep, pos.FreeRounds).
		Updates(map[string]any{
			"training_stage":      next.TrainingStage,
			"training_stage_step": next.TrainingStageStep,
			"completed_rounds":    next.CompletedRounds,
			"status":              next.Status,
			"updated_at":          next.UpdatedAt,
		})
	if update.Error != nil {
		return row, dependency(update.Error, false)
	}
	if update.RowsAffected != 1 {
		return row, ErrRoundConflict
	}
	return next, nil
}

// CompleteTrainingStep 在最终事务内写入 AI 正式步骤并推进阶段；
// 只有本事务提交成功后，上层才可以发送 SSE done。
func (r *Repository) CompleteTrainingStep(ctx context.Context, uid uint64, d Debate, claim TrainingClaim, result ai.Result) (detail TrainingDetail, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if e = requireTraining(row); e != nil {
			return e
		}
		steps, e := readTrainingSteps(tx, row.ID)
		if e != nil {
			return e
		}
		claims, e := readTrainingClaims(tx, row.ID, true)
		if e != nil {
			return e
		}
		current, ok := claimByID(claims, claim.ID)
		if !ok {
			return ErrNotFound
		}
		// 版本校验与 V1 一致：旧尝试的完成结果不能覆盖新尝试。
		if current.Status != "generating" || current.AttemptVersion != claim.AttemptVersion {
			return ErrRoundConflict
		}
		pos, e := trainingPosition(row, len(steps))
		if e != nil {
			return e
		}
		speaker, ok := pos.nextSpeaker()
		if !ok || speaker != ai.SpeakerAssistant || pos.nextStepNo() != current.StepNo {
			return ErrRoundConflict
		}
		step, e := trainingStepForCommit(row, pos, ai.SpeakerAssistant, result.Content, current.ClientRequestID)
		if e != nil {
			return e
		}
		if e = tx.Create(&step).Error; e != nil {
			return dependency(e, false)
		}
		values := metadata(result.Usage, result.Metadata)
		values["status"] = "completed"
		values["error_code"] = nil
		update := tx.Model(&TrainingClaim{}).Where("id = ? AND status = ? AND attempt_version = ?", current.ID, "generating", current.AttemptVersion).Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		next, e := advanceTrainingRow(tx, row, pos)
		if e != nil {
			return e
		}
		steps = append(steps, step)
		detail, e = trainingDetailTxWith(tx, next, steps)
		return e
	})
	return
}

// trainingDetailTxWith 在已知步骤切片时重新读取认领并组装详情。
func trainingDetailTxWith(tx *gorm.DB, row Debate, steps []TrainingStep) (TrainingDetail, error) {
	claims, err := readTrainingClaims(tx, row.ID, false)
	if err != nil {
		return TrainingDetail{}, err
	}
	return TrainingDetail{Debate: row, Steps: steps, Claims: claims, Progress: trainingProgressDTO(row, steps, claims)}, nil
}

// FailTrainingClaim 把当前 AI 尝试标记为 failed；失败步骤不占编号、不推进阶段。
func (r *Repository) FailTrainingClaim(ctx context.Context, uid uint64, d Debate, claim TrainingClaim, code string, usage ai.Usage, m ai.Metadata) (saved TrainingClaim, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if e = requireTraining(row); e != nil {
			return e
		}
		claims, e := readTrainingClaims(tx, row.ID, true)
		if e != nil {
			return e
		}
		current, ok := claimByID(claims, claim.ID)
		if !ok {
			return ErrNotFound
		}
		if current.Status != "generating" || current.AttemptVersion != claim.AttemptVersion {
			return ErrRoundConflict
		}
		values := metadata(usage, m)
		values["status"] = "failed"
		values["error_code"] = code
		update := tx.Model(&TrainingClaim{}).Where("id = ? AND status = ? AND attempt_version = ?", current.ID, "generating", current.AttemptVersion).Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		current.Status = "failed"
		current.ErrorCode = &code
		saved = current
		return nil
	})
	return
}

// ClaimTrainingRetry 在失败认领上按期望版本递增重试；成功/生成中直接返回现状，
// 由 Service 映射为 200/202/409。与 V1 重试共用同一套版本与幂等语义。
func (r *Repository) ClaimTrainingRetry(ctx context.Context, uid, debateID uint64, stepNo int, expectedVersion uint64, admit func() error) (detail TrainingDetail, fresh bool, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, debateID, true)
		if e != nil {
			return e
		}
		if e = requireTraining(row); e != nil {
			return e
		}
		steps, e := readTrainingSteps(tx, row.ID)
		if e != nil {
			return e
		}
		claims, e := readTrainingClaims(tx, row.ID, true)
		if e != nil {
			return e
		}
		detail = TrainingDetail{Debate: row, Steps: steps, Claims: claims, Progress: trainingProgressDTO(row, steps, claims)}
		current, ok := activeClaimForStep(claims, stepNo)
		if !ok {
			return ErrNotFound
		}
		if current.Status == "completed" || current.Status == "generating" || current.Status == "abandoned" {
			return nil
		}
		if current.Status != "failed" {
			return dependency(errors.New("unexpected training claim status"), false)
		}
		if current.AttemptVersion != expectedVersion {
			return nil
		}
		if row.Status != "active" {
			return ErrRoundConflict
		}
		pos, e := trainingPosition(row, len(steps))
		if e != nil {
			return e
		}
		speaker, ok := pos.nextSpeaker()
		if !ok || speaker != ai.SpeakerAssistant || pos.nextStepNo() != stepNo {
			return ErrRoundConflict
		}
		if e = admit(); e != nil {
			return e
		}
		now := r.now()
		lease := now.Add(LeaseDuration)
		nextVersion := current.AttemptVersion + 1
		values := map[string]any{
			"status":           "generating",
			"attempt_version":  nextVersion,
			"lease_expires_at": lease,
			"error_code":       nil,
			"model_name":       nil,
			"prompt_version":   nil,
			"input_tokens":     nil,
			"output_tokens":    nil,
			"latency_ms":       nil,
		}
		update := tx.Model(&TrainingClaim{}).
			Where("id = ? AND debate_id = ? AND status = ? AND attempt_version = ?", current.ID, debateID, "failed", expectedVersion).
			Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrAttemptVersionConflict
		}
		current.Status = "generating"
		current.AttemptVersion = nextVersion
		current.LeaseExpiresAt = &lease
		current.ErrorCode = nil
		for i := range claims {
			if claims[i].ID == current.ID {
				claims[i] = current
			}
		}
		detail.Claims = claims
		detail.Progress = trainingProgressDTO(row, steps, claims)
		fresh = true
		return nil
	})
	if err != nil {
		fresh = false
	}
	return
}

// EndFreeDebate 是「进入结辩」的显式业务操作：
//   - 已完成至少 1 个完整回合且当前回合已完整结束：直接进入结辩；
//   - 当前 AI 回复已明确失败：把该失败尝试标记为 abandoned（保留可见），再进入结辩。
//
// 重放不重复切换、不产生模型调用；与发言提交竞争时由会话行锁 + 条件更新裁决。
func (r *Repository) EndFreeDebate(ctx context.Context, uid, id uint64, in TrainingEndInput) (detail TrainingDetail, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, id, true)
		if e != nil {
			return e
		}
		if e = requireTraining(row); e != nil {
			return e
		}
		steps, e := readTrainingSteps(tx, row.ID)
		if e != nil {
			return e
		}
		claims, e := readTrainingClaims(tx, row.ID, true)
		if e != nil {
			return e
		}
		detail = TrainingDetail{Debate: row, Steps: steps, Claims: claims, Progress: trainingProgressDTO(row, steps, claims)}
		pos, e := trainingPosition(row, len(steps))
		if e != nil {
			return e
		}
		if pos.Stage == TrainingStageClosing || pos.Status == "completed" {
			// 重放：已经进入结辩或已结束，直接返回当前权威状态。
			return nil
		}
		if pos.Stage != TrainingStageFreeDebate {
			return ErrRoundConflict
		}
		if hasGeneratingClaim(claims) {
			return ErrRoundConflict
		}
		if pos.FreeRounds < 1 {
			return ErrRoundConflict
		}
		if pos.StageStep == 0 && in.ExpectedFailedAttempt != 0 {
			return ErrRoundConflict
		}
		if pos.StageStep != 0 {
			// 只剩一次失败的 AI 回复：允许明确放弃，但必须由用户点击触发。
			claim, ok := activeClaimForStep(claims, pos.nextStepNo())
			if !ok || claim.Status != "failed" || claim.AttemptVersion != in.ExpectedFailedAttempt {
				return ErrRoundConflict
			}
			update := tx.Model(&TrainingClaim{}).
				Where("id = ? AND status = ? AND attempt_version = ?", claim.ID, "failed", claim.AttemptVersion).
				Updates(map[string]any{"status": "abandoned", "lease_expires_at": nil, "error_code": claim.ErrorCode})
			if update.Error != nil {
				return dependency(update.Error, false)
			}
			if update.RowsAffected != 1 {
				return ErrRoundConflict
			}
		}
		update := tx.Model(&Debate{}).
			Where("id = ? AND user_id = ? AND mode = ? AND training_stage = ? AND training_stage_step = ? AND completed_rounds = ?",
				row.ID, row.UserID, row.Mode, string(TrainingStageFreeDebate), pos.StageStep, pos.FreeRounds).
			Updates(map[string]any{
				"training_stage":      string(TrainingStageClosing),
				"training_stage_step": 0,
				"updated_at":          r.now(),
			})
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		row.TrainingStage = ptr(string(TrainingStageClosing))
		row.TrainingStageStep = 0
		detail, e = trainingDetailTxWith(tx, row, steps)
		return e
	})
	return
}

func claimByID(claims []TrainingClaim, id uint64) (TrainingClaim, bool) {
	for _, claim := range claims {
		if claim.ID == id {
			return claim, true
		}
	}
	return TrainingClaim{}, false
}

// ptr 返回字符串指针；仅用于本文件的列赋值，避免引入通用工具包。
func ptr(value string) *string { return &value }

// recoverTrainingLeases 扫描过期 generating 认领并标记为 failed，
// 与 V1 租约回收共用同一扫描节奏和条件更新语义。
func (r *Repository) recoverTrainingLeases(ctx context.Context, now time.Time) (int, error) {
	var expired []TrainingClaim
	if err := r.db.WithContext(ctx).
		Select("id", "debate_id", "step_no", "attempt_version").
		Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", "generating", now).
		Find(&expired).Error; err != nil {
		return 0, dependency(err, false)
	}
	recovered := 0
	for _, claim := range expired {
		update := r.db.WithContext(ctx).Model(&TrainingClaim{}).
			Where("id = ? AND status = ? AND attempt_version = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?",
				claim.ID, "generating", claim.AttemptVersion, now).
			Updates(map[string]any{
				"status":           "failed",
				"error_code":       "model_timeout",
				"lease_expires_at": nil,
			})
		if update.Error != nil {
			return recovered, dependency(update.Error, false)
		}
		recovered += int(update.RowsAffected)
	}
	return recovered, nil
}

// CreateTraining 创建产品 V2 训练赛会话：赛制固定 training_v2，
// 权威阶段从立论第 1 步开始，流拍与 V1 创建一样在事务内完成。
func (r *Repository) CreateTraining(ctx context.Context, uid, tid uint64, stance, style string) (d Debate, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		var topic struct {
			ID          uint64
			Title       string
			Description string
		}
		e := tx.Table("topics").Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND enabled = ?", tid, true).First(&topic).Error
		if errors.Is(e, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if e != nil {
			return dependency(e, false)
		}
		stage := string(TrainingStageOpening)
		d = Debate{
			UserID:                   uid,
			TopicID:                  &topic.ID,
			TopicTitleSnapshot:       topic.Title,
			TopicDescriptionSnapshot: topic.Description,
			UserStance:               stance,
			Mode:                     string(TrainingMode),
			AIStyle:                  style,
			TrainingStage:            &stage,
			TrainingStageStep:        0,
			Status:                   "active",
		}
		if e = tx.Create(&d).Error; e != nil {
			return dependency(e, false)
		}
		return nil
	})
	return
}
