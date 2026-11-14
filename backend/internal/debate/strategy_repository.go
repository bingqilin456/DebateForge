package debate

import (
	"context"
	"errors"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func readStrategy(tx *gorm.DB, id uint64, lock bool) (*Strategy, error) {
	var out Strategy
	q := tx.Where("debate_id = ?", id)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	err := q.First(&out).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, dependency(err, false)
	}
	return &out, nil
}
func (r *Repository) GetStrategy(ctx context.Context, uid, id uint64) (state StrategyState, err error) {
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		d, e := owned(tx, uid, id, false)
		if e != nil {
			return e
		}
		strategy, e := readStrategy(tx, id, false)
		state = StrategyState{Debate: d, Strategy: strategy}
		return e
	})
	return
}
func (r *Repository) ClaimStrategy(ctx context.Context, uid, id uint64, in StrategyInput, admit func() error) (state StrategyState, fresh bool, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		d, e := owned(tx, uid, id, true)
		if e != nil {
			return e
		}
		existing, e := readStrategy(tx, id, true)
		if e != nil {
			return e
		}
		state = StrategyState{Debate: d, Strategy: existing}
		if existing != nil {
			switch existing.Status {
			case "completed", "generating":
				return nil
			case "failed":
				if !in.Retry || in.ExpectedAttemptVersion == nil || *in.ExpectedAttemptVersion != existing.AttemptVersion {
					return nil
				}
			default:
				return dependency(nil, false)
			}
		} else if in.Retry {
			return ErrInvalidRequest
		}
		// 与首轮认领持同一会话锁；任何状态的 turn 都表示已经开始。
		var turns int64
		if e = tx.Model(&Turn{}).Where("debate_id = ?", id).Count(&turns).Error; e != nil {
			return dependency(e, false)
		}
		if turns != 0 || d.Status != "active" || d.CompletedRounds != 0 {
			return ErrRoundConflict
		}
		if e = admit(); e != nil {
			return e
		}
		now := r.now()
		lease := now.Add(LeaseDuration)
		if existing == nil {
			existing = &Strategy{DebateID: id, Status: "generating", AttemptVersion: 1, LeaseExpiresAt: &lease, CreatedAt: now, UpdatedAt: now}
			if e = tx.Create(existing).Error; e != nil {
				return dependency(e, false)
			}
		} else {
			values := metadata(ai.Usage{}, ai.Metadata{})
			values["status"] = "generating"
			values["attempt_version"] = existing.AttemptVersion + 1
			values["lease_expires_at"] = lease
			values["error_code"] = nil
			values["schema_version"] = nil
			values["content_json"] = nil
			values["updated_at"] = now
			update := tx.Model(&Strategy{}).Where("id = ? AND status = ? AND attempt_version = ?", existing.ID, "failed", existing.AttemptVersion).Updates(values)
			if update.Error != nil {
				return dependency(update.Error, false)
			}
			if update.RowsAffected != 1 {
				return ErrAttemptVersionConflict
			}
			existing.Status = "generating"
			existing.AttemptVersion++
			existing.LeaseExpiresAt = &lease
			existing.ErrorCode = nil
			existing.SchemaVersion = nil
			existing.ContentJSON = nil
		}
		state.Strategy = existing
		fresh = true
		return nil
	})
	if err != nil {
		fresh = false
	}
	return
}
func currentStrategy(tx *gorm.DB, d Debate, s Strategy) (Strategy, error) {
	var current Strategy
	e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND debate_id = ?", s.ID, d.ID).First(&current).Error
	if e != nil {
		return current, dependency(e, false)
	}
	if current.Status != "generating" || current.AttemptVersion != s.AttemptVersion {
		return current, ErrRoundConflict
	}
	return current, nil
}
func (r *Repository) CompleteStrategy(ctx context.Context, uid uint64, d Debate, s Strategy, result ai.StrategyResult) (saved Strategy, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if saved, e = currentStrategy(tx, row, s); e != nil {
			return e
		}
		schemaVer := result.SchemaVersion
		if schemaVer <= 0 {
			schemaVer = ai.StrategySchemaVersion
		}
		contentJSON := result.ContentJSON
		now := r.now()
		values := metadata(result.Usage, result.Metadata)
		values["status"] = "completed"
		values["schema_version"] = schemaVer
		values["content_json"] = contentJSON
		values["error_code"] = nil
		values["updated_at"] = now
		update := tx.Model(&Strategy{}).Where("id = ? AND status = ? AND attempt_version = ?", s.ID, "generating", s.AttemptVersion).Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		saved.PromptVersion = &result.Metadata.PromptVersion
		saved.ModelName = &result.Metadata.Model
		saved.Status = "completed"
		saved.SchemaVersion = &schemaVer
		saved.ContentJSON = &contentJSON
		saved.ErrorCode = nil
		saved.LeaseExpiresAt = nil
		saved.UpdatedAt = now
		return nil
	})
	return
}
func (r *Repository) FailStrategy(ctx context.Context, uid uint64, d Debate, s Strategy, code string, usage ai.Usage, m ai.Metadata) (saved Strategy, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if saved, e = currentStrategy(tx, row, s); e != nil {
			return e
		}
		now := r.now()
		values := metadata(usage, m)
		values["status"] = "failed"
		values["schema_version"] = nil
		values["content_json"] = nil
		values["error_code"] = code
		values["updated_at"] = now
		update := tx.Model(&Strategy{}).Where("id = ? AND status = ? AND attempt_version = ?", s.ID, "generating", s.AttemptVersion).Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		saved.Status = "failed"
		saved.SchemaVersion = nil
		saved.ContentJSON = nil
		saved.ErrorCode = &code
		saved.LeaseExpiresAt = nil
		saved.UpdatedAt = now
		return nil
	})
	return
}

func (r *Repository) recoverStrategyLeases(ctx context.Context, now time.Time) (int, error) {
	recovered := 0
	var expiredStrategies []Strategy
	if err := r.db.WithContext(ctx).
		Select("id", "debate_id", "attempt_version").
		Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", "generating", now).
		Find(&expiredStrategies).Error; err != nil {
		return recovered, dependency(err, false)
	}
	for _, s := range expiredStrategies {
		update := r.db.WithContext(ctx).Model(&Strategy{}).
			Where("id = ? AND status = ? AND attempt_version = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?",
				s.ID, "generating", s.AttemptVersion, now).
			Updates(map[string]any{
				"status":           "failed",
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
