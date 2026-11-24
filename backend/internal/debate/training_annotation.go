package debate

import (
	"errors"
	"slices"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func annotationTable(tx *gorm.DB, d Debate) *gorm.DB {
	if d.IsTraining() {
		return tx.Table("training_annotations")
	}
	return tx.Model(&TurnAnnotation{})
}

func trainingAnnotationTurn(step TrainingStep) Turn {
	return Turn{ID: step.ID, RoundNo: step.StepNo, Status: "completed", AnnotationEligible: step.Speaker == ai.SpeakerAssistant}
}

func listTrainingAnnotations(tx *gorm.DB, d Debate) ([]AnnotationBatch, error) {
	steps, err := readTrainingSteps(tx, d.ID)
	if err != nil {
		return nil, err
	}
	out := []AnnotationBatch{}
	for _, step := range steps {
		turn := trainingAnnotationTurn(step)
		if !turn.AnnotationEligible {
			continue
		}
		batch, err := readAnnotation(tx, d, turn.ID, false)
		if err != nil {
			return nil, err
		}
		out = append(out, AnnotationBatch{Turn: turn, Annotation: batch})
	}
	return out, nil
}

func annotationTrigger(tx *gorm.DB, d Debate, id uint64) (Turn, []AnnotationSource, error) {
	if !d.IsTraining() {
		var turn Turn
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND debate_id = ?", id, d.ID).First(&turn).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Turn{}, nil, ErrNotFound
		}
		if err != nil {
			return Turn{}, nil, dependency(err, false)
		}
		return turn, nil, nil
	}
	steps, err := readTrainingSteps(tx, d.ID)
	if err != nil {
		return Turn{}, nil, err
	}
	for _, step := range steps {
		if step.ID != id {
			continue
		}
		targets := trainingAnnotationTargets(steps, step.StepNo)
		sources := trainingAnalysisSources(steps, step.StepNo)
		for i := range sources {
			// 批注模型的触发位置与候选片段资格共用同一批次编号。
			if slices.Contains(targets, sources[i].MessageID) {
				sources[i].RoundNo = step.StepNo
			}
		}
		return trainingAnnotationTurn(step), sources, nil
	}
	return Turn{}, nil, ErrNotFound
}
