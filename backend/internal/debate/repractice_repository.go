package debate

import (
	"context"
	"errors"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// V2-08 局部重练的持久化实现。
//
// 与正式发言完全隔离：本表只读引用 debates/messages，从不写回 messages、
// debates、debate_reviews 或轮次行；因此改写与重练失败都不会改变正式比赛，
// 也不会重新计算后续轮次或复盘。
//
// 版本模型：每次提交新增一行（client_request_id 在会话内唯一，用于幂等重放）；
// 失败重试只推进同一行的 attempt_version，不新增行；历史回看按 id 升序，
// 不重排、不覆盖既有版本。

type repracticeRow struct {
	ID              uint64 `gorm:"primaryKey"`
	DebateID        uint64
	ClientRequestID string
	MessageID       uint64
	StartCP         int
	EndCP           int
	Quote           string
	Goal            string
	Rewrite         string
	Status          string
	AttemptVersion  uint64
	Improved        *bool
	ChangeText      *string
	ReasonText      *string
	NewProblem      *string
	ErrorCode       *string
	RestoredFrom    *uint64
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (repracticeRow) TableName() string { return "debate_repractice_versions" }

// version 把持久化行投影为领域对象；字段语义与 DTO 一致。
func (row repracticeRow) version() RepracticeVersion {
	out := RepracticeVersion{
		ID:       row.ID,
		DebateID: row.DebateID,
		Attempt:  row.AttemptVersion,
		Status:   row.Status,
		Input: RepracticeInput{
			RequestID:    row.ClientRequestID,
			MessageID:    row.MessageID,
			Start:        row.StartCP,
			End:          row.EndCP,
			Quote:        row.Quote,
			Goal:         row.Goal,
			Rewrite:      row.Rewrite,
			RestoredFrom: restoredFromValue(row.RestoredFrom),
		},
		RestoredFrom: restoredFromValue(row.RestoredFrom),
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
	if row.ErrorCode != nil {
		out.ErrorCode = *row.ErrorCode
	}
	if row.Improved != nil {
		out.Feedback.Improved = *row.Improved
	}
	if row.ChangeText != nil {
		out.Feedback.Change = *row.ChangeText
	}
	if row.ReasonText != nil {
		out.Feedback.Reason = *row.ReasonText
	}
	if row.NewProblem != nil {
		out.Feedback.NewProblem = *row.NewProblem
	}
	return out
}

func restoredFromValue(value *uint64) uint64 {
	if value == nil {
		return 0
	}
	return *value
}

// ownedCompleted 在临界区内复核归属与完赛状态，返回加锁的会话行。
// 未完成结辩的训练不开放局部重练（AC-V2-26）。
func ownedCompleted(tx *gorm.DB, uid, debateID uint64, lock bool) (Debate, error) {
	row, err := owned(tx, uid, debateID, lock)
	if err != nil {
		return Debate{}, err
	}
	rounds, err := ai.RoundsFor(ai.Mode(row.Mode))
	if err != nil {
		return Debate{}, dependency(err, false)
	}
	if row.Status != "completed" || row.CompletedRounds != rounds {
		return Debate{}, ErrRoundConflict
	}
	return row, nil
}

func readRepracticeVersion(tx *gorm.DB, debateID, versionID uint64, lock bool) (repracticeRow, error) {
	var row repracticeRow
	q := tx.Where("id = ? AND debate_id = ?", versionID, debateID)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	if err := q.First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return repracticeRow{}, ErrRepracticeVersion
		}
		return repracticeRow{}, dependency(err, false)
	}
	return row, nil
}

// ListRepractice 只读返回本场全部版本，按 id 升序。
func (r *Repository) ListRepractice(ctx context.Context, uid, debateID uint64) (versions []RepracticeVersion, err error) {
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		if _, e := ownedCompleted(tx, uid, debateID, false); e != nil {
			return e
		}
		var rows []repracticeRow
		if e := tx.Where("debate_id = ?", debateID).Order("id ASC").Find(&rows).Error; e != nil {
			return dependency(e, false)
		}
		versions = make([]RepracticeVersion, 0, len(rows))
		for _, row := range rows {
			versions = append(versions, row.version())
		}
		return nil
	})
	return versions, err
}

// CreateRepractice 在短事务内复核归属、完赛、选段逐字匹配后创建独立版本。
// fresh=false 表示同一 client_request_id 已存在：输入完全一致时按幂等重放返回，
// 输入不同则按请求编号冲突拒绝，不覆盖既有版本。
func (r *Repository) CreateRepractice(ctx context.Context, uid, debateID uint64, in RepracticeInput, now time.Time) (version RepracticeVersion, fresh bool, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		if _, e := ownedCompleted(tx, uid, debateID, true); e != nil {
			return e
		}
		history, e := readTurns(tx, Debate{ID: debateID}, "")
		if e != nil {
			return e
		}
		if _, _, ok := repracticeSourceMatches(history, in); !ok {
			return ErrRepracticeSource
		}
		var existing []repracticeRow
		if e := tx.Where("debate_id = ? AND client_request_id = ?", debateID, in.RequestID).Limit(1).Find(&existing).Error; e != nil {
			return dependency(e, false)
		}
		if len(existing) > 0 {
			version = existing[0].version()
			if version.Input == in {
				fresh = false
				return nil
			}
			return ErrRepracticeInput
		}
		row := repracticeRow{
			DebateID:        debateID,
			ClientRequestID: in.RequestID,
			MessageID:       in.MessageID,
			StartCP:         in.Start,
			EndCP:           in.End,
			Quote:           in.Quote,
			Goal:            in.Goal,
			Rewrite:         in.Rewrite,
			Status:          repracticeStatusGenerating,
			AttemptVersion:  1,
			CreatedAt:       now.UTC(),
			UpdatedAt:       now.UTC(),
		}
		if in.RestoredFrom != 0 {
			previous, e := readRepracticeVersion(tx, debateID, in.RestoredFrom, false)
			if e != nil {
				return e
			}
			if previous.MessageID != in.MessageID || previous.StartCP != in.Start || previous.EndCP != in.End {
				return ErrRepracticeSource
			}
			from := in.RestoredFrom
			row.RestoredFrom = &from
		}
		if e := tx.Create(&row).Error; e != nil {
			return dependency(e, false)
		}
		version = row.version()
		fresh = true
		return nil
	})
	return version, fresh, err
}

// ClaimRepracticeRetry 只认领 failed 且 attempt 匹配的版本；成功后同一行推进
// attempt_version 并回到 generating，绝不创建新版本、不改动既有反馈。
func (r *Repository) ClaimRepracticeRetry(ctx context.Context, uid, debateID, versionID, attempt uint64, now time.Time) (version RepracticeVersion, fresh bool, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		if _, e := ownedCompleted(tx, uid, debateID, true); e != nil {
			return e
		}
		row, e := readRepracticeVersion(tx, debateID, versionID, true)
		if e != nil {
			return e
		}
		version = row.version()
		if row.Status != repracticeStatusFailed || row.AttemptVersion != attempt {
			fresh = false
			return nil
		}
		next := row.AttemptVersion + 1
		update := tx.Model(&repracticeRow{}).
			Where("id = ? AND debate_id = ? AND status = ? AND attempt_version = ?", row.ID, debateID, repracticeStatusFailed, row.AttemptVersion).
			Updates(map[string]any{
				"status":          repracticeStatusGenerating,
				"attempt_version": next,
				"error_code":      nil,
				"updated_at":      now.UTC(),
			})
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrAttemptVersionConflict
		}
		row.Status = repracticeStatusGenerating
		row.AttemptVersion = next
		row.ErrorCode = nil
		row.UpdatedAt = now.UTC()
		version = row.version()
		fresh = true
		return nil
	})
	return version, fresh, err
}

// CompleteRepractice 按 attempt CAS 写回反馈；成功即代表已提交。
func (r *Repository) CompleteRepractice(ctx context.Context, uid, debateID, versionID, attempt uint64, feedback RepracticeFeedback) (version RepracticeVersion, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		if _, e := ownedCompleted(tx, uid, debateID, true); e != nil {
			return e
		}
		update := tx.Model(&repracticeRow{}).
			Where("id = ? AND debate_id = ? AND status = ? AND attempt_version = ?", versionID, debateID, repracticeStatusGenerating, attempt).
			Updates(map[string]any{
				"status":      repracticeStatusCompleted,
				"improved":    feedback.Improved,
				"change_text": feedback.Change,
				"reason_text": feedback.Reason,
				"new_problem": feedback.NewProblem,
				"error_code":  nil,
				"updated_at":  r.now(),
			})
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrAttemptVersionConflict
		}
		row, e := readRepracticeVersion(tx, debateID, versionID, false)
		if e != nil {
			return e
		}
		version = row.version()
		return nil
	})
	return version, err
}

// FailRepractice 按 attempt CAS 只保存受控错误码，保留输入与选段不改写。
func (r *Repository) FailRepractice(ctx context.Context, uid, debateID, versionID, attempt uint64, code string) (version RepracticeVersion, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		if _, e := ownedCompleted(tx, uid, debateID, true); e != nil {
			return e
		}
		update := tx.Model(&repracticeRow{}).
			Where("id = ? AND debate_id = ? AND status = ? AND attempt_version = ?", versionID, debateID, repracticeStatusGenerating, attempt).
			Updates(map[string]any{
				"status":     repracticeStatusFailed,
				"error_code": code,
				"updated_at": r.now(),
			})
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrAttemptVersionConflict
		}
		row, e := readRepracticeVersion(tx, debateID, versionID, false)
		if e != nil {
			return e
		}
		version = row.version()
		return nil
	})
	return version, err
}

// 编译期确认生产 Repository 满足局部重练的持久化边界。
var _ RepracticeStore = (*Repository)(nil)
