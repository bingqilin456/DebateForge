package debate

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"gorm.io/gorm"
)

type trainingRepracticeState struct {
	DebateID uint64 `gorm:"primaryKey"`
	Payload  string
}

func (trainingRepracticeState) TableName() string { return "training_repractice_states" }

func (r *Repository) withTrainingRepractice(ctx context.Context, uid, id uint64, write bool, apply func(*[]RepracticeVersion) error) error {
	return r.transaction(ctx, !write, func(tx *gorm.DB) error {
		row, err := owned(tx, uid, id, write)
		if err != nil {
			return err
		}
		if (!row.IsTraining() && row.Mode != string(ai.ModeStandard4)) || row.Status != "completed" {
			return ErrRepracticeSource
		}
		versions := []RepracticeVersion{}
		var state trainingRepracticeState
		err = tx.First(&state, "debate_id = ?", id).Error
		exists := err == nil
		if exists {
			if err := json.Unmarshal([]byte(state.Payload), &versions); err != nil {
				return dependency(err, false)
			}
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return dependency(err, false)
		}
		now := r.now()
		for i := range versions {
			if versions[i].Status == "generating" && !versions[i].LeaseExpiresAt.After(now) {
				versions[i].Status, versions[i].LeaseExpiresAt = "failed", time.Time{}
			}
		}
		if err := apply(&versions); err != nil || !write {
			return err
		}
		payload, err := json.Marshal(versions)
		if err != nil {
			return dependency(err, false)
		}
		if exists {
			err = tx.Model(&state).Update("payload", string(payload)).Error
		} else {
			err = tx.Create(&trainingRepracticeState{DebateID: id, Payload: string(payload)}).Error
		}
		if err != nil {
			return dependency(err, false)
		}
		return nil
	})
}
