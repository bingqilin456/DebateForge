package debate

import (
	"context"
	"testing"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 复盘的内存替身：与生产 Repository 的认领/版本/只读投影语义对齐。
// 只加锁保护临界区，等价于真实数据库的事务串行化。

func (m *memoryStore) GetReview(_ context.Context, uid, id uint64) (*Review, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.debates[id]
	if !ok || d.UserID != uid {
		return nil, ErrNotFound
	}
	review, exists := m.reviews[id]
	if !exists {
		return nil, nil
	}
	copied := review
	return &copied, nil
}

func (m *memoryStore) ClaimReview(_ context.Context, uid, debateID uint64, in ReviewInput, admit func() error) (ReviewClaim, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, ok := m.debates[debateID]
	if !ok || d.UserID != uid {
		return ReviewClaim{}, false, ErrNotFound
	}
	rounds := testRounds(d.Mode)
	if d.Status != "completed" || d.CompletedRounds != rounds {
		return ReviewClaim{}, false, ErrRoundConflict
	}
	claim := ReviewClaim{Debate: d}
	existing, hasExisting := m.reviews[debateID]
	if hasExisting {
		claim.Review = existing
		switch existing.Status {
		case reviewStatusCompleted, reviewStatusGenerating:
			return claim, false, nil
		case reviewStatusFailed:
			if !in.Retry || in.ExpectedAttemptVersion == nil || existing.AttemptVersion != *in.ExpectedAttemptVersion {
				return claim, false, nil
			}
		default:
			return ReviewClaim{}, false, ErrRoundConflict
		}
	} else if in.Retry {
		return ReviewClaim{}, false, ErrInvalidRequest
	}
	turns := m.turns[debateID]
	if len(turns) != rounds {
		return ReviewClaim{}, false, ErrRoundConflict
	}
	messages := make([]ai.ReviewMessage, 0, rounds*2)
	for i, turn := range turns {
		if turn.RoundNo != i+1 || turn.Status != "completed" || len(turn.Messages) != 2 {
			return ReviewClaim{}, false, ErrRoundConflict
		}
		messages = append(messages,
			ai.ReviewMessage{MessageID: turn.Messages[0].ID, TurnNo: turn.RoundNo, Role: ai.RoleUser, Content: turn.Messages[0].Content},
			ai.ReviewMessage{MessageID: turn.Messages[1].ID, TurnNo: turn.RoundNo, Role: ai.RoleAssistant, Content: turn.Messages[1].Content},
		)
	}
	if err := admit(); err != nil {
		return ReviewClaim{}, false, err
	}
	now := m.now().UTC()
	lease := now.Add(LeaseDuration)
	if !hasExisting {
		existing = Review{
			ID:             m.allocID(),
			DebateID:       debateID,
			Status:         reviewStatusGenerating,
			AttemptVersion: 1,
			LeaseExpiresAt: &lease,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
	} else {
		existing.Status = reviewStatusGenerating
		existing.AttemptVersion++
		existing.LeaseExpiresAt = &lease
		existing.ErrorCode = nil
		existing.SchemaVersion = nil
		existing.ContentJSON = nil
		existing.UpdatedAt = now
	}
	m.reviews[debateID] = existing
	claim.Review = existing
	claim.Messages = messages
	return claim, true, nil
}

func (m *memoryStore) CompleteReview(_ context.Context, uid uint64, d Debate, review Review, contentJSON string, _ ai.Usage, meta ai.Metadata) (Review, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	curDebate, ok := m.debates[d.ID]
	if !ok || curDebate.UserID != uid {
		return Review{}, ErrNotFound
	}
	cur, exists := m.reviews[d.ID]
	if !exists || cur.ID != review.ID || cur.Status != reviewStatusGenerating || cur.AttemptVersion != review.AttemptVersion {
		return Review{}, ErrRoundConflict
	}
	schemaVersion := ai.ReviewSchemaVersion
	cur.Status = reviewStatusCompleted
	cur.SchemaVersion = &schemaVersion
	cur.ContentJSON = &contentJSON
	cur.ErrorCode = nil
	cur.LeaseExpiresAt = nil
	if meta.PromptVersion != "" {
		promptVersion := meta.PromptVersion
		cur.PromptVersion = &promptVersion
	}
	cur.UpdatedAt = m.now().UTC()
	m.reviews[d.ID] = cur
	return cur, nil
}

func (m *memoryStore) FailReview(_ context.Context, uid uint64, d Debate, review Review, code string, _ ai.Usage, _ ai.Metadata) (Review, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	curDebate, ok := m.debates[d.ID]
	if !ok || curDebate.UserID != uid {
		return Review{}, ErrNotFound
	}
	cur, exists := m.reviews[d.ID]
	if !exists || cur.ID != review.ID || cur.Status != reviewStatusGenerating || cur.AttemptVersion != review.AttemptVersion {
		return Review{}, ErrRoundConflict
	}
	cur.Status = reviewStatusFailed
	cur.SchemaVersion = nil
	cur.ContentJSON = nil
	cur.ErrorCode = &code
	cur.LeaseExpiresAt = nil
	cur.UpdatedAt = m.now().UTC()
	m.reviews[d.ID] = cur
	return cur, nil
}

// recoverReviewLeasesIfExpired 让过期在途复盘在刷新后恢复为可重试的失败状态。
// 单独暴露为显式方法：功能关闭时服务不会扫描这张表。
func (m *memoryStore) recoverReviewLeasesIfExpired(now time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	recovered := 0
	code := "model_timeout"
	for id, review := range m.reviews {
		if review.Status == reviewStatusGenerating && review.LeaseExpiresAt != nil && !review.LeaseExpiresAt.After(now) {
			review.Status = reviewStatusFailed
			review.ErrorCode = &code
			review.LeaseExpiresAt = nil
			review.UpdatedAt = now
			m.reviews[id] = review
			recovered++
		}
	}
	return recovered
}

// RecoverExpiredReviewLeases 与生产 Repository 的同名方法对齐。
func (m *memoryStore) RecoverExpiredReviewLeases(context.Context) (int, error) {
	return m.recoverReviewLeasesIfExpired(m.now().UTC()), nil
}

// completeFourRoundDebate 用现行状态机完成一场四轮标准赛，供复盘测试复用。
func completeFourRoundDebate(t *testing.T, mem *memoryStore, service *Service) Debate {
	t.Helper()
	created, err := service.Create(context.Background(), 1, CreateInput{TopicID: topicID(1), UserStance: "pro", AIStyle: "logical"})
	if err != nil {
		t.Fatalf("创建会话失败: %v", err)
	}
	// 用独立的脚本文本生成四轮 AI 回复：默认回声会把用户原文重复进 AI 消息，
	// 使“唯一匹配”的定位断言失真。提交完成后恢复调用方原有的生成器。
	turnFake := ai.NewFake(
		ai.FakeStep{Result: ai.Result{Content: "对方强调公平性，但未说明统一复核的成本来源"}},
		ai.FakeStep{Result: ai.Result{Content: "公开透明的标准仍然需要具体可执行的复核流程"}},
		ai.FakeStep{Result: ai.Result{Content: "复核一致性依赖事前约定的判定细则"}},
		ai.FakeStep{Result: ai.Result{Content: "收益与成本的比较还需要量化依据"}},
	)
	original := service.generator
	service.generator = turnFake
	defer func() { service.generator = original }()
	// 计数从 21 开始，避免 uuidForRound 的两位数编码与既有用例冲突。
	contents := []string{
		"我方认为开放式考核更能检验真实能力",
		"对方强调公平，但统一标准同样可以公开透明",
		"补充一点：评价标准应当可检验并可复核",
		"综上，开放式考核的收益大于其成本",
	}
	for round, content := range contents {
		in := SubmitInput{RoundNo: round + 1, Content: content, ClientRequestID: uuidForRound(21 + round)}
		if _, status, err := service.Submit(context.Background(), 1, created.ID, in); err != nil {
			t.Fatalf("第 %d 轮提交失败: status=%d err=%v", round+1, status, err)
		}
	}
	detail, err := service.Detail(context.Background(), 1, created.ID)
	if err != nil || detail.Debate.Status != "completed" || detail.Debate.CompletedRounds != 4 {
		t.Fatalf("四轮夹具未完成: %+v err=%v", detail.Debate, err)
	}
	return created
}
