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

func (r *Repository) now() time.Time {
	if r != nil && r.db != nil && r.db.Config != nil && r.db.Config.NowFunc != nil {
		return r.db.Config.NowFunc().UTC()
	}
	return time.Now().UTC()
}

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
		// 赛制与流派由服务端固定：新会话一律 standard_4，不接受客户端传参覆盖。
		// quick_3 仅供历史数据与隔离 E2E 使用，生产创建路径不提供该选项。
		d = Debate{
			UserID:                   uid,
			TopicID:                  topic.ID,
			TopicTitleSnapshot:       topic.Title,
			TopicDescriptionSnapshot: topic.Description,
			UserStance:               stance,
			Mode:                     string(ai.ModeStandard4),
			AIStyle:                  string(ai.StyleLogical),
			Status:                   "active",
		}
		if e = tx.Create(&d).Error; e != nil {
			return dependency(e, false)
		}
		return nil
	})
	return
}
func (r *Repository) List(ctx context.Context, uid uint64, page, pageSize int) (list DebateList, err error) {
	list = DebateList{Items: []Debate{}, Page: page, PageSize: pageSize}
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		var total int64
		if e := tx.Model(&Debate{}).Where("user_id = ?", uid).Count(&total).Error; e != nil {
			return dependency(e, false)
		}
		list.Total = total
		offset := int64(page-1) * int64(pageSize)
		if total == 0 || offset >= total {
			return nil
		}
		if e := tx.Where("user_id = ?", uid).
			Order("created_at DESC, id DESC").
			Limit(pageSize).
			Offset(int(offset)).
			Find(&list.Items).Error; e != nil {
			return dependency(e, false)
		}
		return nil
	})
	return
}
func readSummary(tx *gorm.DB, debateID uint64, lock bool) (*Summary, error) {
	var s Summary
	q := tx.Where("debate_id = ?", debateID)
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	e := q.First(&s).Error
	if errors.Is(e, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if e != nil {
		return nil, dependency(e, false)
	}
	return &s, nil
}
func (r *Repository) Detail(ctx context.Context, uid, id uint64) (d Detail, err error) {
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, id, false)
		if e != nil {
			return e
		}
		d, e = readTurns(tx, row, "")
		if e != nil {
			return e
		}
		d.Summary, e = readSummary(tx, row.ID, false)
		return e
	})
	return
}
func (r *Repository) GetSummary(ctx context.Context, uid, id uint64) (s *Summary, err error) {
	err = r.transaction(ctx, true, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, id, false)
		if e != nil {
			return e
		}
		s, e = readSummary(tx, row.ID, false)
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
		rounds, e := ai.RoundsFor(ai.Mode(row.Mode))
		if e != nil {
			return dependency(e, false)
		}
		if row.Status != "active" || in.RoundNo < 1 || in.RoundNo > rounds || in.RoundNo != row.CompletedRounds+1 {
			return ErrRoundConflict
		}
		history, e := readTurns(tx, row, "")
		if e != nil {
			return e
		}
		if len(history.Turns) != in.RoundNo-1 {
			return ErrRoundConflict
		}
		for i := range history.Turns {
			if history.Turns[i].RoundNo != i+1 || history.Turns[i].Status != "completed" || len(history.Turns[i].Messages) != 2 {
				return ErrRoundConflict
			}
		}
		if e = admit(); e != nil {
			return e
		}
		lease := r.now().Add(LeaseDuration)
		turn := Turn{DebateID: id, RoundNo: in.RoundNo, ClientRequestID: in.ClientRequestID, Status: "generating", AttemptVersion: 1, LeaseExpiresAt: &lease}
		if e = tx.Create(&turn).Error; e != nil {
			return dependency(e, false)
		}
		message := Message{TurnID: turn.ID, Role: "user", Content: in.Content}
		if e = tx.Create(&message).Error; e != nil {
			return dependency(e, false)
		}
		turn.Messages = []Message{message}
		history.Turns = append(history.Turns, turn)
		d = history
		fresh = true
		return nil
	})
	if err != nil {
		fresh = false
	}
	return
}
func (r *Repository) ClaimRetry(ctx context.Context, uid, debateID, turnID, expectedVersion uint64, admit func() error) (d Detail, fresh bool, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, debateID, true)
		if e != nil {
			return e
		}
		all, e := readTurns(tx, row, "")
		if e != nil {
			return e
		}
		idx := -1
		for i := range all.Turns {
			if all.Turns[i].ID == turnID {
				idx = i
				break
			}
		}
		if idx == -1 {
			return ErrNotFound
		}
		turn := all.Turns[idx]
		if turn.Status == "completed" || turn.Status == "generating" {
			d = Detail{Debate: row, Turns: []Turn{turn}}
			return nil
		}
		if turn.Status != "failed" {
			return dependency(errors.New("unexpected turn status"), false)
		}
		if turn.AttemptVersion != expectedVersion {
			d = Detail{Debate: row, Turns: []Turn{turn}}
			return nil
		}
		rounds, e := ai.RoundsFor(ai.Mode(row.Mode))
		if e != nil {
			return dependency(e, false)
		}
		if row.Status != "active" || turn.RoundNo < 1 || turn.RoundNo > rounds || row.CompletedRounds != turn.RoundNo-1 || idx != len(all.Turns)-1 || len(all.Turns) != turn.RoundNo {
			return ErrRoundConflict
		}
		for i := 0; i < idx; i++ {
			if all.Turns[i].RoundNo != i+1 || all.Turns[i].Status != "completed" || len(all.Turns[i].Messages) != 2 {
				return ErrRoundConflict
			}
		}
		if len(turn.Messages) != 1 || turn.Messages[0].Role != "user" {
			return dependency(errors.New("failed turn missing user message"), false)
		}
		if e = admit(); e != nil {
			return e
		}
		lease := r.now().Add(LeaseDuration)
		nextVersion := turn.AttemptVersion + 1
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
		update := tx.Model(&Turn{}).
			Where("id = ? AND debate_id = ? AND status = ? AND attempt_version = ?",
				turn.ID, debateID, "failed", expectedVersion).
			Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrAttemptVersionConflict
		}
		turn.Status = "generating"
		turn.AttemptVersion = nextVersion
		turn.LeaseExpiresAt = &lease
		turn.ErrorCode = nil
		all.Turns[idx] = turn
		d = all
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
	e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND debate_id = ?", t.ID, d.ID).First(&current).Error
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
		rounds, e := ai.RoundsFor(ai.Mode(row.Mode))
		if e != nil {
			return dependency(e, false)
		}
		if row.Status != "active" || t.RoundNo < 1 || t.RoundNo > rounds || row.CompletedRounds != t.RoundNo-1 {
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
		newRounds := t.RoundNo
		newStatus := "active"
		if newRounds >= rounds {
			newStatus = "completed"
		}
		now := r.now()
		update = tx.Model(&Debate{}).Where("id = ? AND user_id = ? AND status = ? AND completed_rounds = ?", d.ID, uid, "active", t.RoundNo-1).Updates(map[string]any{"completed_rounds": newRounds, "status": newStatus, "updated_at": now})
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
		row.CompletedRounds = newRounds
		row.Status = newStatus
		row.UpdatedAt = now
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

func currentSummary(tx *gorm.DB, d Debate, s Summary) (Summary, error) {
	var current Summary
	e := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND debate_id = ?", s.ID, d.ID).First(&current).Error
	if e != nil {
		return current, dependency(e, false)
	}
	if current.Status != "generating" || current.AttemptVersion != s.AttemptVersion {
		return current, ErrRoundConflict
	}
	return current, nil
}
func (r *Repository) ClaimSummary(ctx context.Context, uid, debateID uint64, in SummaryInput, admit func() error) (d Detail, fresh bool, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, debateID, true)
		if e != nil {
			return e
		}
		rounds, e := ai.RoundsFor(ai.Mode(row.Mode))
		if e != nil {
			return dependency(e, false)
		}
		if row.Status != "completed" || row.CompletedRounds != rounds {
			return ErrRoundConflict
		}
		existing, e := readSummary(tx, debateID, true)
		if e != nil {
			return e
		}
		if existing == nil {
			if in.Retry {
				return ErrInvalidRequest
			}
			history, e := readTurns(tx, row, "")
			if e != nil {
				return e
			}
			if len(history.Turns) != rounds {
				return ErrRoundConflict
			}
			for i := range history.Turns {
				if history.Turns[i].RoundNo != i+1 || history.Turns[i].Status != "completed" || len(history.Turns[i].Messages) != 2 {
					return ErrRoundConflict
				}
			}
			if e = admit(); e != nil {
				return e
			}
			now := r.now()
			lease := now.Add(LeaseDuration)
			summary := Summary{
				DebateID:       debateID,
				Status:         "generating",
				AttemptVersion: 1,
				LeaseExpiresAt: &lease,
				CreatedAt:      now,
				UpdatedAt:      now,
			}
			if e = tx.Create(&summary).Error; e != nil {
				return dependency(e, false)
			}
			history.Summary = &summary
			d = history
			fresh = true
			return nil
		}
		if existing.Status == "completed" || existing.Status == "generating" {
			d = Detail{Debate: row, Summary: existing}
			return nil
		}
		if existing.Status != "failed" {
			return dependency(errors.New("unexpected summary status"), false)
		}
		if !in.Retry {
			d = Detail{Debate: row, Summary: existing}
			return nil
		}
		if in.ExpectedAttemptVersion == nil || existing.AttemptVersion != *in.ExpectedAttemptVersion {
			d = Detail{Debate: row, Summary: existing}
			return nil
		}
		history, e := readTurns(tx, row, "")
		if e != nil {
			return e
		}
		if len(history.Turns) != rounds {
			return ErrRoundConflict
		}
		for i := range history.Turns {
			if history.Turns[i].RoundNo != i+1 || history.Turns[i].Status != "completed" || len(history.Turns[i].Messages) != 2 {
				return ErrRoundConflict
			}
		}
		if e = admit(); e != nil {
			return e
		}
		now := r.now()
		lease := now.Add(LeaseDuration)
		nextVersion := existing.AttemptVersion + 1
		values := map[string]any{
			"status":           "generating",
			"schema_version":   nil,
			"content_json":     nil,
			"attempt_version":  nextVersion,
			"lease_expires_at": lease,
			"error_code":       nil,
			"model_name":       nil,
			"prompt_version":   nil,
			"input_tokens":     nil,
			"output_tokens":    nil,
			"latency_ms":       nil,
			"updated_at":       now,
		}
		update := tx.Model(&Summary{}).
			Where("id = ? AND debate_id = ? AND status = ? AND attempt_version = ?",
				existing.ID, debateID, "failed", *in.ExpectedAttemptVersion).
			Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrAttemptVersionConflict
		}
		existing.Status = "generating"
		existing.SchemaVersion = nil
		existing.ContentJSON = nil
		existing.AttemptVersion = nextVersion
		existing.LeaseExpiresAt = &lease
		existing.ErrorCode = nil
		existing.UpdatedAt = now
		history.Summary = existing
		d = history
		fresh = true
		return nil
	})
	if err != nil {
		fresh = false
	}
	return
}
func (r *Repository) CompleteSummary(ctx context.Context, uid uint64, d Debate, s Summary, result ai.SummaryResult) (saved Summary, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if saved, e = currentSummary(tx, row, s); e != nil {
			return e
		}
		rounds, e := ai.RoundsFor(ai.Mode(row.Mode))
		if e != nil {
			return dependency(e, false)
		}
		if row.Status != "completed" || row.CompletedRounds != rounds {
			return ErrRoundConflict
		}
		schemaVer := result.SchemaVersion
		if schemaVer <= 0 {
			schemaVer = ai.SummarySchemaVersion
		}
		contentJSON := result.ContentJSON
		now := r.now()
		values := metadata(result.Usage, result.Metadata)
		values["status"] = "completed"
		values["schema_version"] = schemaVer
		values["content_json"] = contentJSON
		values["error_code"] = nil
		values["updated_at"] = now
		update := tx.Model(&Summary{}).Where("id = ? AND status = ? AND attempt_version = ?", s.ID, "generating", s.AttemptVersion).Updates(values)
		if update.Error != nil {
			return dependency(update.Error, false)
		}
		if update.RowsAffected != 1 {
			return ErrRoundConflict
		}
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
func (r *Repository) FailSummary(ctx context.Context, uid uint64, d Debate, s Summary, code string, usage ai.Usage, m ai.Metadata) (saved Summary, err error) {
	err = r.transaction(ctx, false, func(tx *gorm.DB) error {
		row, e := owned(tx, uid, d.ID, true)
		if e != nil {
			return e
		}
		if saved, e = currentSummary(tx, row, s); e != nil {
			return e
		}
		now := r.now()
		values := metadata(usage, m)
		values["status"] = "failed"
		values["schema_version"] = nil
		values["content_json"] = nil
		values["error_code"] = code
		values["updated_at"] = now
		update := tx.Model(&Summary{}).Where("id = ? AND status = ? AND attempt_version = ?", s.ID, "generating", s.AttemptVersion).Updates(values)
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

// RecoverExpiredLeases 扫描租约已过期的 generating 轮次与总结，按状态和 attempt_version 条件标记为 failed。
func (r *Repository) RecoverExpiredLeases(ctx context.Context) (int, error) {
	if r == nil || r.db == nil {
		return 0, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	now := r.now()
	var expired []Turn
	if err := r.db.WithContext(ctx).
		Select("id", "debate_id", "attempt_version").
		Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", "generating", now).
		Find(&expired).Error; err != nil {
		return 0, dependency(err, false)
	}
	recovered := 0
	for _, t := range expired {
		update := r.db.WithContext(ctx).Model(&Turn{}).
			Where("id = ? AND status = ? AND attempt_version = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?",
				t.ID, "generating", t.AttemptVersion, now).
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
	var expiredSummaries []Summary
	if err := r.db.WithContext(ctx).
		Select("id", "debate_id", "attempt_version").
		Where("status = ? AND lease_expires_at IS NOT NULL AND lease_expires_at <= ?", "generating", now).
		Find(&expiredSummaries).Error; err != nil {
		return recovered, dependency(err, false)
	}
	for _, s := range expiredSummaries {
		update := r.db.WithContext(ctx).Model(&Summary{}).
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

var _ Store = (*Repository)(nil)
