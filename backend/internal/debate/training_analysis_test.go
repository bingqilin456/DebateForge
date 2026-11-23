package debate

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

func TestTrainingAnalysisMilestones(t *testing.T) {
	stage := string(TrainingStageClosing)
	d := Debate{TrainingStage: &stage, Mode: string(TrainingMode), Status: "completed", CompletedRounds: 1}
	steps := []TrainingStep{
		{ID: 1, StepNo: 1, Stage: TrainingStageOpening, StageStep: 1, Speaker: ai.SpeakerAssistant},
		{ID: 2, StepNo: 2, Stage: TrainingStageOpening, StageStep: 2, Speaker: ai.SpeakerUser},
		{ID: 3, StepNo: 3, Stage: TrainingStageRebuttal, StageStep: 1, Speaker: ai.SpeakerAssistant},
		{ID: 4, StepNo: 4, Stage: TrainingStageRebuttal, StageStep: 2, Speaker: ai.SpeakerUser},
		{ID: 5, StepNo: 5, Stage: TrainingStageFreeDebate, StageStep: 1, FreeDebateRound: 1, Speaker: ai.SpeakerUser},
		{ID: 6, StepNo: 6, Stage: TrainingStageFreeDebate, StageStep: 2, FreeDebateRound: 1, Speaker: ai.SpeakerAssistant},
		{ID: 7, StepNo: 7, Stage: TrainingStageFreeDebate, StageStep: 1, FreeDebateRound: 2, Speaker: ai.SpeakerUser},
		{ID: 8, StepNo: 8, Stage: TrainingStageClosing, StageStep: 1, Speaker: ai.SpeakerUser},
		{ID: 9, StepNo: 9, Stage: TrainingStageClosing, StageStep: 2, Speaker: ai.SpeakerAssistant},
	}
	if got := trainingIssueMilestones(d, steps); !reflect.DeepEqual(got, []int{4, 6, 9}) {
		t.Fatalf("争点整理位置 = %v", got)
	}
	if got := trainingAnnotationTargets(steps, 3); !reflect.DeepEqual(got, []uint64{3}) {
		t.Fatalf("跨阶段 AI 先发言只批注本步：%v", got)
	}
	if got := trainingAnnotationTargets(steps, 9); !reflect.DeepEqual(got, []uint64{8, 9}) {
		t.Fatalf("结辩批注不重复评价已放弃回复的用户原文：%v", got)
	}
	d.Status = "active"
	if got := trainingIssueMilestones(d, steps[:8]); !reflect.DeepEqual(got, []int{4, 6}) {
		t.Fatalf("未完赛争点位置 = %v", got)
	}
}

func TestTrainingFakeIssueCandidatesFitFullMatch(t *testing.T) {
	var sources []IssueCardSource
	for i := 1; i <= 26; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		sources = append(sources, IssueCardSource{
			MessageID: uint64(i), RoundNo: i, Role: role, Content: fmt.Sprintf("已保存的第 %d 条发言。", i),
		})
	}
	candidates := defaultFakeIssueCardCandidates(sources, true)
	if len(candidates) != MaxIssueCardsDisplayed {
		t.Fatalf("完整训练赛候选数 = %d", len(candidates))
	}
	if _, err := normalizeIssueCardCandidates(sources, candidates); err != nil {
		t.Fatalf("候选引用必须可用：%v", err)
	}
}
