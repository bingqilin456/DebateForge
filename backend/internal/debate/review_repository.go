package debate

import (
	"context"
	"errors"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// V2-13 复盘的持久化实现。
//
// 复用总结/策略/批注表的形状（状态、尝试版本、租约、受控错误与模型元数据），
// 但表、唯一键与状态机独立；它从不读写 debate_summaries、turn_annotations 或轮次行，
// 因此复盘失败或重试不会覆盖既有的成功总结、批注与人工编辑。

func readReview(tx *gorm.DB, debateID uint64, lock bool) (*Review, error) {
	var review Review
	q := tx.Where("debate_id = ?", debateID)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.First(&review).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, dependency(err, false)
	}
	return &review, nil
}

// GetReview 只读返回会话的复盘行；不存在时返回 nil，由 DTO 投影为 not_started。
func (r *Repository) GetReview(ctx context.Context, uid, id uint64) (review *Review, err error) {
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, id, false)
		if e != nil {
			return e
		}
		review, e = readReview(tx, row.ID, false)
		return e
	})
	return
}

// ClaimReview 在短事务内校验归属、完赛状态与版本，然后认领一次复盘生成。
//
// expected 语义与总结一致：未开始时只能以 Retry=false 首次认领；
// 失败时按 expected_attempt_version 显式重试；已完成或在途都不重新认领。
// 认领时一并读取本场全部正式消息，作为锁外生成与锚点复核的唯一输入。
func (r *Repository) ClaimReview(ctx context.Context, uid, debateID uint64, in ReviewInput, admit func() error) (claim ReviewClaim, fresh bool, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, debateID, true)
		if e != nil {
			return e
		}
		// 两种模式都使用会话自身的完赛状态。
		if e := reviewEligibility(row); e != nil {
			return e
		}
		existing, e := readReview(tx, debateID, true)
		if e != nil {
			return e
		}
		claim = ReviewClaim{Debate: row}
		if existing != nil {
			claim.Review = *existing
			switch existing.Status {
			case reviewStatusCompleted, reviewStatusGenerating:
				return nil
			case reviewStatusFailed:
				if !in.Retry || in.ExpectedAttemptVersion == nil || existing.AttemptVersion != *in.ExpectedAttemptVersion {
					return nil
				}
			default:
				return dependency(errors.New("unexpected review status"), false)
			}
		} else if in.Retry {
			// 未开始的复盘不能按版本重试：调用方应以不重试的首次请求生成。
			return ErrInvalidRequest
		}
		messages, e := reviewSources(tx, row)
		if e != nil {
			return e
		}
		if e = admit(); e != nil {
			return e
		}
		now := r.now()
		lease := now.Add(LeaseDuration)
		if existing == nil {
			created := Review{
				DebateID:       debateID,
				Status:         reviewStatusGenerating,
				AttemptVersion: 1,
				LeaseExpiresAt: &lease,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if e = tx.Create(&created).Error; e != nil {
				return dependency(e, false)
			}
			claim.Review = created
		} else {
			nextVersion := existing.AttemptVersion + 1
			values := metadata(ai.Usage{}, ai.Metadata{})
			values["status"] = reviewStatusGenerating
			values["attempt_version"] = nextVersion
			values["lease_expires_at"] = lease
			values["error_code"] = nil
			values["schema_version"] = nil
			values["content_json"] = nil
			values["updated_at"] = now
			update := tx.Model(&Review{}).
				Where("id = ? AND status = ? AND attempt_version = ?", existing.ID, reviewStatusFailed, existing.AttemptVersion).
				Updates(values)
			if update.Error != nil {
				return dependency(update.Error, false)
			}
			if update.RowsAffected != 1 {
				return ErrAttemptVersionConflict
			}
			existing.Status = reviewStatusGenerating
			existing.AttemptVersion = nextVersion
			existing.LeaseExpiresAt = &lease
			existing.ErrorCode = nil
			existing.SchemaVersion = nil
			existing.ContentJSON = nil
			existing.UpdatedAt = now
			claim.Review = *existing
		}
		claim.Messages = messages
		fresh = true
		return nil
	})
	if err != nil {
		fresh = false
	}
	return
}

func reviewSources(tx *gorm.DB, row Debate) ([]ai.ReviewMessage, error) {
	if row.IsTraining() {
		detail, err := trainingDetailTx(tx, row)
		if err != nil {
			return nil, err
		}
		messages := make([]ai.ReviewMessage, 0, len(detail.Steps))
		for _, step := range detail.Steps {
			messages = append(messages, ai.ReviewMessage{MessageID: step.ID, TurnNo: step.StepNo, Role: ai.Role(step.Speaker), Content: step.Content})
		}
		return messages, nil
	}
	rounds, _ := ai.RoundsFor(ai.Mode(row.Mode))
	history, err := readTurns(tx, row, "")
	if err != nil {
		return nil, err
	}
	if len(history.Turns) != rounds {
		return nil, ErrRoundConflict
	}
	messages := make([]ai.ReviewMessage, 0, rounds*2)
	for i, turn := range history.Turns {
		if turn.RoundNo != i+1 || turn.Status != "completed" || len(turn.Messages) != 2 ||
			turn.Messages[0].Role != "user" || turn.Messages[1].Role != "assistant" {
			return nil, ErrRoundConflict
		}
		for _, m := range turn.Messages {
			messages = append(messages, ai.ReviewMessage{MessageID: m.ID, TurnNo: turn.RoundNo, Role: ai.Role(m.Role), Content: m.Content})
		}
	}
	return messages, nil
}

func reviewEligibility(row Debate) error {
	if row.Status != "completed" {
		return ErrRoundConflict
	}
	if row.IsTraining() {
		return nil
	}
	rounds, err := ai.RoundsFor(ai.Mode(row.Mode))
	if err != nil {
		return dependency(err, false)
	}
	if row.CompletedRounds != rounds {
		return ErrRoundConflict
	}
	return nil
}

// currentReview 锁定复盘行并校验状态与版本，防止旧响应覆盖新尝试。
func currentReview(tx *gorm.DB, d Debate, review Review) (Review, error) {
	var current Review
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND debate_id = ?", review.ID, d.ID).First(&current).Error
	if err != nil {
		return current, dependency(err, false)
	}
	if current.Status != reviewStatusGenerating || current.AttemptVersion != review.AttemptVersion {
		return current, ErrRoundConflict
	}
	return current, nil
}

// CompleteReview 原子保存整份复盘（不含半成品）：内容、版本与元数据一次写入。
// 若未来改为分段保存，必须显式记录已落库的段落并只补失败部分，不得假装局部恢复。
func (r *Repository) CompleteReview(ctx context.Context, uid uint64, d Debate, review Review, contentJSON string, usage ai.Usage, meta ai.Metadata) (saved Review, err error) {
	if len(contentJSON) > ai.MaxReviewJSONBytes {
		return Review{}, &Failure{Code: "model_error", Status: 502}
	}
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if saved, e = currentReview(tx, row, review); e != nil {
			return e
		}
		if e := reviewEligibility(row); e != nil {
			return e
		}
		schemaVersion := ai.ReviewSchemaVersion
		now := r.now()
		values := metadata(usage, meta)
		values["status"] = reviewStatusCompleted
		values["schema_version"] = schemaVersion
		values["content_json"] = contentJSON
		values["error_code"] = nil
		values["lease_expires_at"] = nil
		values["updated_at"] = now
		update := tx.Model(&Review{}).
			Where("id = ? AND status = ? AND attempt_version = ?", review.ID, reviewStatusGenerating, review.AttemptVersion).
			Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		saved.Status = reviewStatusCompleted
		saved.SchemaVersion = &schemaVersion
		saved.ContentJSON = &contentJSON
		saved.ErrorCode = nil
		saved.LeaseExpiresAt = nil
		if meta.Model != "" {
			saved.ModelName = &meta.Model
		}
		if meta.PromptVersion != "" {
			saved.PromptVersion = &meta.PromptVersion
		}
		saved.UpdatedAt = now
		return nil
	})
	return
}

// FailReview 只保存受控错误码；不写入任何半成品内容，也不触碰其它生成结果。
func (r *Repository) FailReview(ctx context.Context, uid uint64, d Debate, review Review, code string, usage ai.Usage, meta ai.Metadata) (saved Review, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if saved, e = currentReview(tx, row, review); e != nil {
			return e
		}
		now := r.now()
		values := metadata(usage, meta)
		values["status"] = reviewStatusFailed
		values["schema_version"] = nil
		values["content_json"] = nil
		values["error_code"] = code
		values["lease_expires_at"] = nil
		values["updated_at"] = now
		update := tx.Model(&Review{}).
			Where("id = ? AND status = ? AND attempt_version = ?", review.ID, reviewStatusGenerating, review.AttemptVersion).
			Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		saved.Status = reviewStatusFailed
		saved.SchemaVersion = nil
		saved.ContentJSON = nil
		saved.ErrorCode = &code
		saved.LeaseExpiresAt = nil
		saved.UpdatedAt = now
		return nil
	})
	return
}

// recoverReviewLeases 把过期在途复盘标记为失败，使刷新后能显示可重试的独立失败状态。
// 只按状态与版本条件更新，不覆盖并发完成的成功结果。
func (r *Repository) recoverReviewLeases(ctx context.Context, now time.Time) (int, error) {
	var expired []Review
	if err := r.db.WithContext(ctx).
		Select("id", "debate_id", "attempt_version").
		Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", reviewStatusGenerating, now).
		Find(&expired).Error; err != nil {
		return 0, dependency(err, false)
	}
	recovered := 0
	for _, review := range expired {
		update := r.db.WithContext(ctx).Model(&Review{}).
			Where("id = ? AND status = ? AND attempt_version = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?",
				review.ID, reviewStatusGenerating, review.AttemptVersion, now).
			Updates(map[string]any{
				"status":           reviewStatusFailed,
				"error_code":       "model_timeout",
				"lease_expires_at": nil,
				"updated_at":       now,
			})
		if update.Error != nil {
			return recovered, dependency(update.Error, false)
		}
		recovered += int(update.RowsAffected)
	}
	return recovered, nil
}
