package debate

import (
	"context"
	"errors"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TurnAnnotation 是一次限定范围的批注批次，以触发轮唯一。
// 它与总结/策略表同形，但业务边界独立：批注只读取已提交原文，
// 不改变轮次、会话完成或总结状态。
type TurnAnnotation struct {
	ID             uint64     `json:"-" gorm:"primaryKey"`
	DebateID       uint64     `json:"-"`
	TurnID         uint64     `json:"-"`
	Status         string     `json:"-"`
	ContentJSON    *string    `json:"-"`
	AttemptVersion uint64     `json:"-"`
	LeaseExpiresAt *time.Time `json:"-"`
	ErrorCode      *string    `json:"-"`
	ModelName      *string    `json:"-"`
	PromptVersion  *string    `json:"-"`
	InputTokens    *uint64    `json:"-"`
	OutputTokens   *uint64    `json:"-"`
	LatencyMS      *uint64    `json:"-"`
	CreatedAt      time.Time  `json:"-"`
	UpdatedAt      time.Time  `json:"-"`
}

func (TurnAnnotation) TableName() string { return "turn_annotations" }

// AnnotationBatch 是一条参与读取的合格轮次及其批注批次（可能尚未认领）。
type AnnotationBatch struct {
	Turn       Turn
	Annotation *TurnAnnotation
}

// AnnotationClaim 是一次成功认领的全部输入快照，供锁外生成使用。
type AnnotationClaim struct {
	Debate     Debate
	Turn       Turn
	Annotation TurnAnnotation
	Sources    []AnnotationSource
}

func readAnnotation(tx *gorm.DB, turnID uint64, lock bool) (*TurnAnnotation, error) {
	var out TurnAnnotation
	q := tx.Where("turn_id = ?", turnID)
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

// ListAnnotations 只读返回当前会话中合格轮次及其批次，按触发轮排序。
// 旧会话没有合格轮次，因此自然返回空列表。
func (r *Repository) ListAnnotations(ctx context.Context, uid, debateID uint64) (out []AnnotationBatch, err error) {
	out = []AnnotationBatch{}
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, debateID, false)
		if e != nil {
			return e
		}
		var turns []Turn
		if e = tx.Where("debate_id = ? AND annotation_eligible = ?", row.ID, true).
			Order("round_no ASC").Find(&turns).Error; e != nil {
			return dependency(e, false)
		}
		for i := range turns {
			batch := AnnotationBatch{Turn: turns[i]}
			if batch.Annotation, e = readAnnotation(tx, turns[i].ID, false); e != nil {
				return e
			}
			out = append(out, batch)
		}
		return nil
	})
	return
}

// annotationSources 读取触发轮及以前已提交的正式消息。
// 刻意按 round_no 过滤而不是取全部：慢批注重试时后轮可能已提交，
// 若不过滤就会把尚未发生的发言当作依据。
func annotationSources(tx *gorm.DB, debateID uint64, triggerRound int) ([]AnnotationSource, error) {
	var turns []Turn
	if err := tx.Where("debate_id = ? AND round_no <= ? AND status = ?", debateID, triggerRound, "completed").
		Order("round_no ASC").Find(&turns).Error; err != nil {
		return nil, dependency(err, false)
	}
	sources := make([]AnnotationSource, 0, len(turns)*2)
	for _, turn := range turns {
		var messages []Message
		if err := tx.Where("turn_id = ?", turn.ID).
			Order("CASE role WHEN 'user' THEN 0 ELSE 1 END ASC").Find(&messages).Error; err != nil {
			return nil, dependency(err, false)
		}
		for _, message := range messages {
			sources = append(sources, AnnotationSource{
				MessageID: message.ID,
				TurnID:    turn.ID,
				RoundNo:   turn.RoundNo,
				Role:      message.Role,
				Content:   message.Content,
			})
		}
	}
	return sources, nil
}

// ClaimAnnotation 在短事务内校验归属、合格性与版本，然后认领一次生成。
//
// expectedVersion 为 0 表示首次认领（未开始），大于 0 表示按版本重试失败或过期批次。
// 已成功或在途的批次不重新认领；版本不符返回 ErrAttemptVersionConflict。
func (r *Repository) ClaimAnnotation(ctx context.Context, uid, debateID, turnID, expectedVersion uint64, admit func() error) (claim AnnotationClaim, fresh bool, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, debateID, true)
		if e != nil {
			return e
		}
		var turn Turn
		if e = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND debate_id = ?", turnID, debateID).First(&turn).Error; e != nil {
			if errors.Is(e, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return dependency(e, false)
		}
		if !turn.AnnotationEligible || turn.Status != "completed" {
			// 非合格轮不启动：未开启切片时的旧轮次、失败轮与在途轮都不参与。
			return ErrRoundConflict
		}
		existing, e := readAnnotation(tx, turnID, true)
		if e != nil {
			return e
		}
		claim = AnnotationClaim{Debate: row, Turn: turn}
		if existing != nil {
			claim.Annotation = *existing
			switch existing.Status {
			case "completed", "generating":
				return nil
			case "failed":
				// 手动补做最多一次：v1 是首次分析，v2 是唯一一次补做。
				// 超过 v2 的失败批次保持失败，只允许查询，避免形成自动无限重试或重复计费。
				if expectedVersion == 0 || expectedVersion != existing.AttemptVersion || existing.AttemptVersion >= 2 {
					return nil
				}
			default:
				return dependency(errors.New("unexpected annotation status"), false)
			}
		} else if expectedVersion != 0 {
			// 未开始的批次不能按版本重试；调用方应以 0 发起首次认领。
			return ErrAttemptVersionConflict
		}
		if e = admit(); e != nil {
			return e
		}
		now := r.now()
		lease := now.Add(LeaseDuration)
		if existing == nil {
			created := TurnAnnotation{
				DebateID:       debateID,
				TurnID:         turnID,
				Status:         "generating",
				AttemptVersion: 1,
				LeaseExpiresAt: &lease,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if e = tx.Create(&created).Error; e != nil {
				return dependency(e, false)
			}
			claim.Annotation = created
		} else {
			nextVersion := existing.AttemptVersion + 1
			values := metadata(ai.Usage{}, ai.Metadata{})
			values["status"] = "generating"
			values["attempt_version"] = nextVersion
			values["lease_expires_at"] = lease
			values["error_code"] = nil
			values["content_json"] = nil
			values["updated_at"] = now
			update := tx.Model(&TurnAnnotation{}).
				Where("id = ? AND status = ? AND attempt_version = ?", existing.ID, "failed", existing.AttemptVersion).
				Updates(values)
			if update.Error != nil {
				return dependency(update.Error, false)
			}
			if update.RowsAffected != 1 {
				return ErrAttemptVersionConflict
			}
			existing.Status = "generating"
			existing.AttemptVersion = nextVersion
			existing.LeaseExpiresAt = &lease
			existing.ErrorCode = nil
			existing.ContentJSON = nil
			existing.UpdatedAt = now
			claim.Annotation = *existing
		}
		if claim.Sources, e = annotationSources(tx, debateID, turn.RoundNo); e != nil {
			return e
		}
		fresh = true
		return nil
	})
	if err != nil {
		fresh = false
	}
	return
}

// currentAnnotation 锁定批次行并校验状态与版本，防止旧响应覆盖新尝试。
func currentAnnotation(tx *gorm.DB, d Debate, a TurnAnnotation) (TurnAnnotation, error) {
	var current TurnAnnotation
	e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND debate_id = ?", a.ID, d.ID).First(&current).Error
	if e != nil {
		return current, dependency(e, false)
	}
	if current.Status != "generating" || current.AttemptVersion != a.AttemptVersion {
		return current, ErrRoundConflict
	}
	return current, nil
}

// CompleteAnnotation 整体原子保存成功结果，包括空数组。
func (r *Repository) CompleteAnnotation(ctx context.Context, uid uint64, d Debate, a TurnAnnotation, result ai.AnnotationResult) (saved TurnAnnotation, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if saved, e = currentAnnotation(tx, row, a); e != nil {
			return e
		}
		contentJSON := result.ContentJSON
		now := r.now()
		values := metadata(result.Usage, result.Metadata)
		values["status"] = "completed"
		values["content_json"] = contentJSON
		values["error_code"] = nil
		values["updated_at"] = now
		update := tx.Model(&TurnAnnotation{}).
			Where("id = ? AND status = ? AND attempt_version = ?", a.ID, "generating", a.AttemptVersion).
			Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		saved.Status = "completed"
		saved.ContentJSON = &contentJSON
		saved.ErrorCode = nil
		saved.LeaseExpiresAt = nil
		if result.Metadata.Model != "" {
			saved.ModelName = &result.Metadata.Model
		}
		if result.Metadata.PromptVersion != "" {
			saved.PromptVersion = &result.Metadata.PromptVersion
		}
		saved.UpdatedAt = now
		return nil
	})
	return
}

// FailAnnotation 只保存受控错误码，不写入供应商原始响应。
func (r *Repository) FailAnnotation(ctx context.Context, uid uint64, d Debate, a TurnAnnotation, code string, usage ai.Usage, m ai.Metadata) (saved TurnAnnotation, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if saved, e = currentAnnotation(tx, row, a); e != nil {
			return e
		}
		now := r.now()
		values := metadata(usage, m)
		values["status"] = "failed"
		values["content_json"] = nil
		values["error_code"] = code
		values["updated_at"] = now
		update := tx.Model(&TurnAnnotation{}).
			Where("id = ? AND status = ? AND attempt_version = ?", a.ID, "generating", a.AttemptVersion).
			Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		saved.Status = "failed"
		saved.ContentJSON = nil
		saved.ErrorCode = &code
		saved.LeaseExpiresAt = nil
		saved.UpdatedAt = now
		return nil
	})
	return
}

// recoverAnnotationLeases 把过期在途批次标记为失败，使下一次手动认领可以恢复。
// 只按状态与版本条件更新，不覆盖并发完成的结果。
func (r *Repository) recoverAnnotationLeases(ctx context.Context, now time.Time) (int, error) {
	var expired []TurnAnnotation
	if err := r.db.WithContext(ctx).
		Select("id", "debate_id", "attempt_version").
		Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", "generating", now).
		Find(&expired).Error; err != nil {
		return 0, dependency(err, false)
	}
	recovered := 0
	for _, a := range expired {
		update := r.db.WithContext(ctx).Model(&TurnAnnotation{}).
			Where("id = ? AND status = ? AND attempt_version = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?",
				a.ID, "generating", a.AttemptVersion, now).
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
