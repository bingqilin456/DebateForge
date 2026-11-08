package debate

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db} }

// transaction 为整个短事务共享 3 秒，而非为事务中的每条 SQL 重置预算。
// Commit 错误不证明回滚，保留不确定标志，禁止在调用方重试生成。
func (r *Repository) transaction(ctx context.Context, readOnly bool, fn func(*gorm.DB) error) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx := r.db.WithContext(ctx).Begin(&sql.TxOptions{ReadOnly: readOnly, Isolation: sql.LevelRepeatableRead})
	if tx.Error != nil {
		return dependency(tx.Error, false)
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit().Error; err != nil {
		return dependency(err, true)
	}
	return nil
}
func owned(tx *gorm.DB, uid, id uint64, lock bool) (Debate, error) {
	var d Debate
	q := tx.Where("id = ? AND user_id = ?", id, uid)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	e := q.First(&d).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return d, ErrNotFound
	}
	if e != nil {
		return d, dependency(e, false)
	}
	return d, nil
}
func readTurns(tx *gorm.DB, d Debate, key string) (Detail, error) {
	result := Detail{Debate: d, Turns: []Turn{}}
	q := tx.Where("debate_id = ?", d.ID)
	if key != "" {
		q = q.Where("client_request_id = ?", key)
	}
	if e := q.Order("round_no ASC").Find(&result.Turns).Error; e != nil {
		return result, dependency(e, false)
	}
	for i := range result.Turns {
		messages := []Message{}
		if e := tx.Where("turn_id = ?", result.Turns[i].ID).Order("CASE role WHEN 'user' THEN 0 ELSE 1 END ASC").Find(&messages).Error; e != nil {
			return result, dependency(e, false)
		}
		result.Turns[i].Messages = messages
	}
	return result, nil
}
func (r *Repository) Create(ctx context.Context, uid, tid uint64, stance string) (d Debate, err error) {
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
		d = Debate{UserID: uid, TopicID: topic.ID, TopicTitleSnapshot: topic.Title, TopicDescriptionSnapshot: topic.Description, UserStance: stance, Status: "active"}
		if e = tx.Create(&d).Error; e != nil {
			return dependency(e, false)
		}
		return nil
	})
	return
}
func (r *Repository) Detail(ctx context.Context, uid, id uint64) (d Detail, err error) {
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, id, false)
		if e != nil {
			return e
		}
		d, e = readTurns(tx, row, "")
		return e
	})
	return
}
func (r *Repository) Claim(ctx context.Context, uid, id uint64, in SubmitInput, admit func() error) (d Detail, fresh bool, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, id, true)
		if e != nil {
			return e
		}
		existing, e := readTurns(tx, row, in.ClientRequestID)
		if e != nil {
			return e
		}
		if len(existing.Turns) > 0 {
			turn := existing.Turns[0]
			if turn.RoundNo != in.RoundNo || len(turn.Messages) == 0 || turn.Messages[0].Role != "user" || turn.Messages[0].Content != in.Content {
				return ErrIdempotencyConflict
			}
			d = existing
			return nil
		}
		if row.Status != "active" || in.RoundNo != 1 || row.CompletedRounds != 0 {
			return ErrRoundConflict
		}
		var count int64
		if e = tx.Model(&Turn{}).Where("debate_id = ? AND round_no = ?", id, in.RoundNo).Count(&count).Error; e != nil {
			return dependency(e, false)
		}
		if count != 0 {
			return ErrRoundConflict
		}
		if e = admit(); e != nil {
			return e
		}
		lease := time.Now().UTC().Add(90 * time.Second)
		turn := Turn{DebateID: id, RoundNo: 1, ClientRequestID: in.ClientRequestID, Status: "generating", AttemptVersion: 1, LeaseExpiresAt: &lease}
		if e = tx.Create(&turn).Error; e != nil {
			return dependency(e, false)
		}
		message := Message{TurnID: turn.ID, Role: "user", Content: in.Content}
		if e = tx.Create(&message).Error; e != nil {
			return dependency(e, false)
		}
		turn.Messages = []Message{message}
		d = Detail{row, []Turn{turn}}
		fresh = true
		return nil
	})
	if err != nil {
		fresh = false
	}
	return
}
func currentTurn(tx *gorm.DB, d Debate, t Turn) (Turn, error) {
	var current Turn
	e := tx.Where("id = ? AND debate_id = ?", t.ID, d.ID).First(&current).Error
	if e != nil {
		return current, dependency(e, false)
	}
	if current.Status != "generating" || current.AttemptVersion != t.AttemptVersion {
		return current, ErrRoundConflict
	}
	return current, nil
}
func metadata(usage ai.Usage, m ai.Metadata) map[string]any {
	values := map[string]any{"model_name": nil, "prompt_version": nil, "input_tokens": nil, "output_tokens": nil, "latency_ms": nil, "lease_expires_at": nil}
	if m.Model != "" && len(m.Model) <= 255 {
		values["model_name"] = m.Model
	}
	if m.PromptVersion != "" && len(m.PromptVersion) <= 64 {
		values["prompt_version"] = m.PromptVersion
	}
	if usage.PromptTokens.Known && usage.PromptTokens.Value >= 0 && uint64(usage.PromptTokens.Value) <= 4294967295 {
		values["input_tokens"] = usage.PromptTokens.Value
	}
	if usage.CompletionTokens.Known && usage.CompletionTokens.Value >= 0 && uint64(usage.CompletionTokens.Value) <= 4294967295 {
		values["output_tokens"] = usage.CompletionTokens.Value
	}
	if m.LatencyMS >= 0 && m.LatencyMS <= 4294967295 {
		values["latency_ms"] = m.LatencyMS
	}
	return values
}
func (r *Repository) Complete(ctx context.Context, uid uint64, d Debate, t Turn, result ai.Result) (detail Detail, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if _, e = currentTurn(tx, row, t); e != nil {
			return e
		}
		if row.Status != "active" || row.CompletedRounds != 0 {
			return ErrRoundConflict
		}
		message := Message{TurnID: t.ID, Role: "assistant", Content: result.Content}
		if e = tx.Create(&message).Error; e != nil {
			return dependency(e, false)
		}
		values := metadata(result.Usage, result.Metadata)
		values["status"] = "completed"
		values["error_code"] = nil
		update := tx.Model(&Turn{}).Where("id = ? AND status = ? AND attempt_version = ?", t.ID, "generating", t.AttemptVersion).Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		update = tx.Model(&Debate{}).Where("id = ? AND user_id = ? AND status = ? AND completed_rounds = ?", d.ID, uid, "active", 0).Updates(map[string]any{"completed_rounds": 1, "updated_at": time.Now().UTC()})
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		row.CompletedRounds = 1
		row.UpdatedAt = time.Now().UTC()
		detail, e = readTurns(tx, row, t.ClientRequestID)
		return e
	})
	return
}
func (r *Repository) Fail(ctx context.Context, uid uint64, d Debate, t Turn, code string, usage ai.Usage, m ai.Metadata) (saved Turn, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if saved, e = currentTurn(tx, row, t); e != nil {
			return e
		}
		values := metadata(usage, m)
		values["status"] = "failed"
		values["error_code"] = code
		update := tx.Model(&Turn{}).Where("id = ? AND status = ? AND attempt_version = ?", t.ID, "generating", t.AttemptVersion).Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		saved.Status = "failed"
		saved.ErrorCode = &code
		return nil
	})
	return
}

var _ Store = (*Repository)(nil)
