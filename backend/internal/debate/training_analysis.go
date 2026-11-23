package debate

import (
	"context"
	"slices"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// EnableTrainingIssues 在本地 Fake 能力启用时装配分析器。
func (s *Service) EnableTrainingIssues(store IssueCardStore, analyzer IssueCardAnalyzer) {
	s.trainingIssues = NewIssueCardService(store, analyzer)
}

func (s *Service) thenUpdateTrainingIssues(uid uint64, detail TrainingDetail, step int) {
	if s.trainingIssues == nil || !slices.Contains(trainingIssueMilestones(detail.Debate, detail.Steps), step) {
		return
	}
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		// 正式发言已提交。后台在自己的时限内补齐分析，失败由用户显式重试。
		for ctx.Err() == nil {
			result, status, err := s.trainingIssues.analyze(ctx, uid, detail.Debate.ID, IssueCardAnalyzeInput{}, true)
			if err != nil || status != 201 || !result.Pending {
				return
			}
		}
	}()
}

// 完成位置来自已提交步骤和会话的权威状态，供分析补查和去重。
func trainingIssueMilestones(d Debate, steps []TrainingStep) []int {
	var milestones []int
	for _, step := range steps {
		if step.StageStep != TrainingMaxStageStep {
			continue
		}
		switch step.Stage {
		case TrainingStageRebuttal:
			milestones = append(milestones, step.StepNo)
		case TrainingStageFreeDebate:
			if step.FreeDebateRound <= d.CompletedRounds {
				milestones = append(milestones, step.StepNo)
			}
		case TrainingStageClosing:
			if d.Status == "completed" {
				milestones = append(milestones, step.StepNo)
			}
		}
	}
	return milestones
}

func trainingAnnotationTargets(steps []TrainingStep, trigger int) []uint64 {
	for i, step := range steps {
		if step.StepNo != trigger || step.Speaker != ai.SpeakerAssistant {
			continue
		}
		if i > 0 {
			prev := steps[i-1]
			if prev.Speaker == ai.SpeakerUser && prev.Stage == step.Stage && prev.FreeDebateRound == step.FreeDebateRound {
				return []uint64{prev.ID, step.ID}
			}
		}
		return []uint64{step.ID}
	}
	return nil
}

func trainingAnalysisSources(steps []TrainingStep, through int) []AnnotationSource {
	sources := make([]AnnotationSource, 0, len(steps))
	for _, step := range steps {
		if step.StepNo > through {
			break
		}
		sources = append(sources, AnnotationSource{
			MessageID: step.ID, TurnID: step.ID, RoundNo: step.StepNo,
			Role: string(step.Speaker), Content: step.Content,
		})
	}
	return sources
}
