package debate

import (
	"context"
	"encoding/json"
	"errors"

	"gorm.io/gorm"
)

// 持有会话行锁时复用第一批卡片的版本和合并规则，锁外才做分析。
type trainingIssueState struct {
	DebateID uint64 `gorm:"primaryKey"`
	Payload  string
}

func (trainingIssueState) TableName() string { return "training_issue_states" }

type savedTrainingIssues struct {
	NextID   uint64
	Analysis IssueCardAnalysis
	Cards    []IssueCard
}

func (r *Repository) withTrainingIssues(ctx context.Context, uid, id uint64, write bool, apply func(*MemoryIssueCardStore, []int) (bool, error)) error {
	return r.transaction(ctx, !write, func(tx *gorm.DB) error {
		row, err := owned(tx, uid, id, write)
		if err != nil {
			return err
		}
		if !row.IsTraining() {
			return ErrIssueCardModeUnsupported
		}
		detail, err := trainingDetailTx(tx, row)
		if err != nil {
			return err
		}
		milestones := trainingIssueMilestones(row, detail.Steps)
		mem := NewMemoryIssueCardStore(r.now)
		mem.SeedDebate(row, nil)
		for _, source := range trainingAnalysisSources(detail.Steps, len(detail.Steps)) {
			mem.turns[id] = append(mem.turns[id], Turn{
				ID: source.TurnID, RoundNo: source.RoundNo, Status: "completed",
				Messages: []Message{{ID: source.MessageID, Role: source.Role, Content: source.Content}},
			})
		}
		var record trainingIssueState
		err = tx.First(&record, "debate_id = ?", id).Error
		exists := err == nil
		if exists {
			var saved savedTrainingIssues
			if err := json.Unmarshal([]byte(record.Payload), &saved); err != nil {
				return dependency(err, false)
			}
			mem.nextID, mem.analyses[id], mem.cards[id] = saved.NextID, saved.Analysis, saved.Cards
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return dependency(err, false)
		}
		analysis := mem.analyses[id]
		if analysis.Status == IssueCardAnalysisGenerating && analysis.LeaseExpiresAt != nil && !analysis.LeaseExpiresAt.After(r.now()) {
			code := "model_timeout"
			analysis.Status, analysis.ErrorCode, analysis.LeaseExpiresAt = IssueCardAnalysisFailed, &code, nil
			mem.analyses[id] = analysis
		}
		dirty, err := apply(mem, milestones)
		if err != nil || !dirty {
			return err
		}
		payload, err := json.Marshal(savedTrainingIssues{mem.nextID, mem.analyses[id], mem.cards[id]})
		if err != nil {
			return dependency(err, false)
		}
		if exists {
			err = tx.Model(&record).Update("payload", string(payload)).Error
		} else {
			err = tx.Create(&trainingIssueState{DebateID: id, Payload: string(payload)}).Error
		}
		if err != nil {
			return dependency(err, false)
		}
		return nil
	})
}

func nextTrainingIssueMilestone(milestones []int, after int) int {
	for _, step := range milestones {
		if step > after {
			return step
		}
	}
	return 0
}

func trainingIssueSnapshot(snapshot IssueCardSnapshot, milestones []int) IssueCardSnapshot {
	snapshot.Ready = len(milestones) > 0
	snapshot.Pending = nextTrainingIssueMilestone(milestones, snapshot.Analysis.ThroughStep) != 0
	return snapshot
}

func (r *Repository) LoadIssueCards(ctx context.Context, uid, id uint64) (out IssueCardSnapshot, err error) {
	err = r.withTrainingIssues(ctx, uid, id, false, func(mem *MemoryIssueCardStore, milestones []int) (bool, error) {
		snapshot, err := mem.LoadIssueCards(ctx, uid, id)
		out = trainingIssueSnapshot(snapshot, milestones)
		return false, err
	})
	return
}

func (r *Repository) ClaimIssueCardAnalysis(ctx context.Context, uid, id uint64, retry bool, version uint64) (out IssueCardClaim, fresh bool, err error) {
	err = r.withTrainingIssues(ctx, uid, id, true, func(mem *MemoryIssueCardStore, milestones []int) (bool, error) {
		if len(milestones) == 0 {
			return false, ErrIssueCardNotReady
		}
		state := mem.analyses[id]
		target := state.ThroughStep
		next := nextTrainingIssueMilestone(milestones, target)
		switch {
		case state.Status == IssueCardAnalysisFailed && retry:
			if version != state.AttemptVersion {
				return false, ErrIssueCardVersionConflict
			}
		case (state.Status == IssueCardAnalysisCompleted || state.Status == IssueCardAnalysisNotStarted) && next != 0:
			if retry && version != state.AttemptVersion {
				return false, ErrIssueCardVersionConflict
			}
			target = next
			retry, version = state.Status == IssueCardAnalysisCompleted, state.AttemptVersion
		default:
			out.Snapshot = trainingIssueSnapshot(mem.issueCardSnapshotLocked(mem.debates[id]), milestones)
			return false, nil
		}
		var claimErr error
		out, fresh, claimErr = mem.ClaimIssueCardAnalysis(ctx, uid, id, retry, version)
		if claimErr != nil || !fresh {
			return false, claimErr
		}
		state = out.Snapshot.Analysis
		lease := r.now().Add(LeaseDuration)
		state.ThroughStep, state.LeaseExpiresAt = target, &lease
		mem.analyses[id] = state
		out.Snapshot.Analysis = state
		out.Snapshot = trainingIssueSnapshot(out.Snapshot, milestones)
		for i, source := range out.Snapshot.Sources {
			if source.RoundNo > target {
				out.Snapshot.Sources = out.Snapshot.Sources[:i]
				break
			}
		}
		return true, nil
	})
	return
}

func (r *Repository) CompleteIssueCardAnalysis(ctx context.Context, uid uint64, claim IssueCardClaim, drafts []IssueCardDraft) (out IssueCardSnapshot, err error) {
	err = r.withTrainingIssues(ctx, uid, claim.Snapshot.Debate.ID, true, func(mem *MemoryIssueCardStore, milestones []int) (bool, error) {
		snapshot, err := mem.CompleteIssueCardAnalysis(ctx, uid, claim, drafts)
		out = trainingIssueSnapshot(snapshot, milestones)
		return err == nil, err
	})
	return
}

func (r *Repository) FailIssueCardAnalysis(ctx context.Context, uid uint64, claim IssueCardClaim, code string) (out IssueCardAnalysis, err error) {
	err = r.withTrainingIssues(ctx, uid, claim.Snapshot.Debate.ID, true, func(mem *MemoryIssueCardStore, _ []int) (bool, error) {
		var err error
		out, err = mem.FailIssueCardAnalysis(ctx, uid, claim, code)
		return err == nil, err
	})
	return
}

func (r *Repository) ApplyIssueCardEdit(ctx context.Context, uid, id uint64, cardID string, input IssueCardEditInput) (out IssueCardSnapshot, err error) {
	err = r.withTrainingIssues(ctx, uid, id, true, func(mem *MemoryIssueCardStore, milestones []int) (bool, error) {
		snapshot, err := mem.ApplyIssueCardEdit(ctx, uid, id, cardID, input)
		out = trainingIssueSnapshot(snapshot, milestones)
		return err == nil, err
	})
	return
}
