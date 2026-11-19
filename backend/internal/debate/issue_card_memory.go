package debate

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MemoryIssueCardStore 是按 IssueCardStore 契约实现的内存 Fake。
//
// 它只用于单元测试与隔离 Mock：不连数据库、不落盘，也不宣称生产持久化。
// 所有方法共用一把互斥锁，等价于生产实现短事务内的串行化临界区，
// 因此可以直接验证版本 CAS、归属隔离与晚到合并的竞争结果。
type MemoryIssueCardStore struct {
	mu      sync.Mutex
	now     func() time.Time
	nextID  uint64
	debates map[uint64]Debate
	turns   map[uint64][]Turn
	// analyses 以会话为单位，记录赛后整理状态。
	analyses map[uint64]IssueCardAnalysis
	// cards 以会话为单位，保持创建顺序，读取时按该顺序展示。
	cards map[uint64][]IssueCard
}

// NewMemoryIssueCardStore 构造内存 Fake；now 为空时使用 UTC 当前时间。
func NewMemoryIssueCardStore(now func() time.Time) *MemoryIssueCardStore {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &MemoryIssueCardStore{
		now:      now,
		nextID:   1000,
		debates:  make(map[uint64]Debate),
		turns:    make(map[uint64][]Turn),
		analyses: make(map[uint64]IssueCardAnalysis),
		cards:    make(map[uint64][]IssueCard),
	}
}

func (m *MemoryIssueCardStore) allocID() uint64 {
	m.nextID++
	return m.nextID
}

// SeedDebate 直接写入夹具会话与轮次；只用于测试准备，不经过状态机。
func (m *MemoryIssueCardStore) SeedDebate(d Debate, turns []Turn) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.debates[d.ID] = d
	m.turns[d.ID] = append([]Turn(nil), turns...)
	if _, exists := m.cards[d.ID]; !exists {
		m.cards[d.ID] = []IssueCard{}
	}
	if _, exists := m.analyses[d.ID]; !exists {
		m.analyses[d.ID] = IssueCardAnalysis{Status: IssueCardAnalysisNotStarted}
	}
}

// SetMessageContent 替换某条已保存发言的原文，用于验证失效锚点只在读取期降级。
func (m *MemoryIssueCardStore) SetMessageContent(debateID, messageID uint64, content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	turns := m.turns[debateID]
	for i := range turns {
		for j := range turns[i].Messages {
			if turns[i].Messages[j].ID == messageID {
				turns[i].Messages[j].Content = content
			}
		}
	}
	m.turns[debateID] = turns
}

// Cards 返回某场当前保存的全部卡片快照，供测试断言内部状态（含 removed/merged）。
func (m *MemoryIssueCardStore) Cards(debateID uint64) []IssueCard {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]IssueCard(nil), m.cards[debateID]...)
}

// issueCardSnapshotLocked 组装读取视图；调用方必须已持有 m.mu。
func (m *MemoryIssueCardStore) issueCardSnapshotLocked(d Debate) IssueCardSnapshot {
	snapshot := IssueCardSnapshot{
		Debate:   d,
		Analysis: m.analyses[d.ID],
		Cards:    append([]IssueCard(nil), m.cards[d.ID]...),
	}
	for _, turn := range m.turns[d.ID] {
		if turn.Status != "completed" {
			continue
		}
		for _, message := range turn.Messages {
			snapshot.Sources = append(snapshot.Sources, IssueCardSource{
				MessageID: message.ID,
				TurnID:    turn.ID,
				RoundNo:   turn.RoundNo,
				Role:      message.Role,
				Content:   message.Content,
			})
		}
	}
	return snapshot
}

// ownedDebateLocked 校验归属：不存在或不属于当前用户都按 ErrNotFound 处理。
func (m *MemoryIssueCardStore) ownedDebateLocked(uid, debateID uint64) (Debate, error) {
	d, ok := m.debates[debateID]
	if !ok || d.UserID != uid {
		return Debate{}, ErrNotFound
	}
	return d, nil
}

// LoadIssueCards 只读返回；资格错误不返回 error，而由投影表达为 not_ready/unsupported。
func (m *MemoryIssueCardStore) LoadIssueCards(_ context.Context, uid, debateID uint64) (IssueCardSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.ownedDebateLocked(uid, debateID)
	if err != nil {
		return IssueCardSnapshot{}, err
	}
	return m.issueCardSnapshotLocked(d), nil
}

// ClaimIssueCardAnalysis 在临界区内检查资格与版本，然后认领一次分析。
//
// retry=false 是“重复触发”：已完成/已失败/生成中都只读回既有状态。
// retry=true 是显式重试：expectedVersion 为 0 表示不校验版本（调用方未观察到版本），
// 大于 0 时必须与当前版本一致，否则返回版本冲突而不覆盖任何已保存数据。
func (m *MemoryIssueCardStore) ClaimIssueCardAnalysis(_ context.Context, uid, debateID uint64, retry bool, expectedVersion uint64) (IssueCardClaim, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.ownedDebateLocked(uid, debateID)
	if err != nil {
		return IssueCardClaim{}, false, err
	}
	if err := issueCardEligibility(d); err != nil {
		return IssueCardClaim{}, false, err
	}
	analysis := m.analyses[debateID]
	claim := IssueCardClaim{Snapshot: m.issueCardSnapshotLocked(d)}
	switch analysis.Status {
	case "", IssueCardAnalysisNotStarted:
		if retry && expectedVersion != 0 {
			return IssueCardClaim{}, false, ErrIssueCardVersionConflict
		}
	case IssueCardAnalysisGenerating:
		return claim, false, nil
	case IssueCardAnalysisCompleted, IssueCardAnalysisFailed:
		if !retry {
			return claim, false, nil
		}
		if expectedVersion != 0 && expectedVersion != analysis.AttemptVersion {
			return IssueCardClaim{}, false, ErrIssueCardVersionConflict
		}
	default:
		return IssueCardClaim{}, false, ErrIssueCardModeUnsupported
	}
	now := m.now().UTC()
	analysis.Status = IssueCardAnalysisGenerating
	analysis.AttemptVersion++
	analysis.ErrorCode = nil
	analysis.UpdatedAt = now
	m.analyses[debateID] = analysis
	claim.Snapshot.Analysis = analysis
	return claim, true, nil
}

// CompleteIssueCardAnalysis 把候选合并进卡片集合。
func (m *MemoryIssueCardStore) CompleteIssueCardAnalysis(_ context.Context, uid uint64, claim IssueCardClaim, drafts []IssueCardDraft) (IssueCardSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.ownedDebateLocked(uid, claim.Snapshot.Debate.ID)
	if err != nil {
		return IssueCardSnapshot{}, err
	}
	analysis := m.analyses[d.ID]
	// 只有仍在途且版本一致的认领才能写回；旧响应不得覆盖新尝试。
	if analysis.Status != IssueCardAnalysisGenerating || analysis.AttemptVersion != claim.Snapshot.Analysis.AttemptVersion {
		return IssueCardSnapshot{}, ErrIssueCardVersionConflict
	}
	cards := m.cards[d.ID]
	activeBySource := make(map[string]int, len(cards))
	removedBySource := make(map[string]bool, len(cards))
	mergedBySource := make(map[string]string, len(cards))
	activeCount := 0
	for index := range cards {
		key := issueCardSourceKeyOf(cards[index])
		switch cards[index].Status {
		case IssueCardStatusActive:
			activeBySource[key] = index
			activeCount++
		case IssueCardStatusRemoved:
			removedBySource[key] = true
		case IssueCardStatusMerged:
			mergedBySource[key] = cards[index].MergedInto
		}
	}
	now := m.now().UTC()
	for _, draft := range drafts {
		key := issueCardCitationSourceKey(draft.Origin)
		if removedBySource[key] {
			// 已移除的卡片不因再次分析而复活。
			continue
		}
		if index, ok := activeBySource[key]; ok {
			merged := mergeIssueCardDraft(&cards[index], draft, now)
			cards[index] = merged
			continue
		}
		if targetID, ok := mergedBySource[key]; ok {
			if index, exists := issueCardIndexByID(cards, targetID); exists && cards[index].Status == IssueCardStatusActive {
				merged := mergeIssueCardDraft(&cards[index], draft, now)
				merged = appendIssueCardNotice(merged, "已合并来源的相关回应已补充到本卡。")
				cards[index] = merged
			}
			// 目标卡若已不可用，宁可丢弃这次补充，也不拆回已合并卡。
			continue
		}
		if activeCount >= MaxIssueCardsDisplayed {
			// 展示上限优先保证既有卡片身份稳定，不挤掉更早的卡。
			continue
		}
		card := IssueCard{
			ID:          strconv.FormatUint(m.allocID(), 10),
			DebateID:    d.ID,
			Title:       draft.Title,
			TitleSource: IssueCardTitleSourceAI,
			Explanation: draft.Explanation,
			Origin:      draft.Origin,
			Responses:   append([]IssueCardCitation{}, draft.Responses...),
			Pending:     append([]string{}, draft.Pending...),
			Version:     1,
			Status:      IssueCardStatusActive,
			SourceKey:   key,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		cards = append(cards, card)
		activeBySource[key] = len(cards) - 1
		activeCount++
	}
	m.cards[d.ID] = cards
	analysis.Status = IssueCardAnalysisCompleted
	analysis.ErrorCode = nil
	analysis.UpdatedAt = now
	m.analyses[d.ID] = analysis
	return m.issueCardSnapshotLocked(d), nil
}

// issueCardSourceKeyOf 兼容历史行可能未写入 SourceKey 的情况：按提出位置重算。
func issueCardSourceKeyOf(card IssueCard) string {
	if card.SourceKey != "" {
		return card.SourceKey
	}
	return issueCardCitationSourceKey(card.Origin)
}

func issueCardIndexByID(cards []IssueCard, id string) (int, bool) {
	for index := range cards {
		if cards[index].ID == id {
			return index, true
		}
	}
	return 0, false
}

// mergeIssueCardDraft 只追加兼容内容：人工标题永不被覆盖，冲突写进 notices。
func mergeIssueCardDraft(card *IssueCard, draft IssueCardDraft, now time.Time) IssueCard {
	changed := false
	// 人工标题优先；AI 标题只在其自身来源时更新，并把差异显式记录为冲突提示。
	if card.TitleSource == IssueCardTitleSourceUser {
		if draft.Title != "" && draft.Title != card.Title {
			*card = appendIssueCardNotice(*card, "晚到分析建议的标题「"+draft.Title+"」未覆盖你的人工标题。")
		}
	} else if draft.Title != "" && draft.Title != card.Title {
		card.Title = draft.Title
		changed = true
	}
	if draft.Explanation != "" && draft.Explanation != card.Explanation {
		card.Explanation = draft.Explanation
		changed = true
	}
	seen := make(map[string]bool, len(card.Responses)+len(draft.Responses))
	for _, citation := range card.Responses {
		seen[issueCardAnchorKey(citation.Anchor)] = true
	}
	for _, citation := range draft.Responses {
		key := issueCardAnchorKey(citation.Anchor)
		if seen[key] {
			continue
		}
		seen[key] = true
		card.Responses = append(card.Responses, citation)
		changed = true
	}
	if len(card.Responses) > MaxIssueCardResponses {
		card.Responses = card.Responses[:MaxIssueCardResponses]
	}
	for _, pending := range draft.Pending {
		if issueCardContainsString(card.Pending, pending) {
			continue
		}
		if len(card.Pending) >= MaxIssueCardPendingItems {
			break
		}
		card.Pending = append(card.Pending, pending)
		changed = true
	}
	if changed {
		card.Version++
		card.UpdatedAt = now
	}
	return *card
}

func appendIssueCardNotice(card IssueCard, notice string) IssueCard {
	for _, current := range card.Notices {
		if current == notice {
			return card
		}
	}
	card.Notices = append(card.Notices, notice)
	return card
}

// FailIssueCardAnalysis 只保存受控错误码；卡片与人工编辑原样保留。
func (m *MemoryIssueCardStore) FailIssueCardAnalysis(_ context.Context, uid uint64, claim IssueCardClaim, code string) (IssueCardAnalysis, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.ownedDebateLocked(uid, claim.Snapshot.Debate.ID)
	if err != nil {
		return IssueCardAnalysis{}, err
	}
	analysis := m.analyses[d.ID]
	if analysis.Status != IssueCardAnalysisGenerating || analysis.AttemptVersion != claim.Snapshot.Analysis.AttemptVersion {
		return IssueCardAnalysis{}, ErrIssueCardVersionConflict
	}
	analysis.Status = IssueCardAnalysisFailed
	analysis.ErrorCode = &code
	analysis.UpdatedAt = m.now().UTC()
	m.analyses[d.ID] = analysis
	return analysis, nil
}

// ApplyIssueCardEdit 在临界区内做版本 CAS，再应用改名 / 合并 / 移除。
func (m *MemoryIssueCardStore) ApplyIssueCardEdit(_ context.Context, uid, debateID uint64, cardID string, in IssueCardEditInput) (IssueCardSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	d, err := m.ownedDebateLocked(uid, debateID)
	if err != nil {
		return IssueCardSnapshot{}, err
	}
	if err := issueCardEligibility(d); err != nil {
		return IssueCardSnapshot{}, err
	}
	cards := m.cards[debateID]
	index, ok := issueCardIndexByID(cards, cardID)
	if !ok || cards[index].DebateID != debateID {
		return IssueCardSnapshot{}, ErrIssueCardNotFound
	}
	card := cards[index]
	if card.Status != IssueCardStatusActive {
		// 已合并或被移除的卡不能通过编辑拆回或复活。
		return IssueCardSnapshot{}, ErrInvalidRequest
	}
	if card.Version != in.ExpectedVersion {
		return IssueCardSnapshot{}, ErrIssueCardVersionConflict
	}
	now := m.now().UTC()
	switch in.Action {
	case IssueCardActionRename:
		card.Title = *in.Title
		card.TitleSource = IssueCardTitleSourceUser
		card.Version++
		card.UpdatedAt = now
		cards[index] = card
	case IssueCardActionRemove:
		card.Status = IssueCardStatusRemoved
		card.Version++
		card.UpdatedAt = now
		cards[index] = card
	case IssueCardActionMerge:
		targetIndex, exists := issueCardIndexByID(cards, *in.TargetCardID)
		if !exists || *in.TargetCardID == card.ID || cards[targetIndex].Status != IssueCardStatusActive {
			return IssueCardSnapshot{}, ErrInvalidRequest
		}
		target := cards[targetIndex]
		// 合并保留双方原句：被合并卡的提出位置与相关回应都进入目标卡的回应列表。
		target = mergeIssueCardDraft(&target, IssueCardDraft{
			Responses: append([]IssueCardCitation{card.Origin}, card.Responses...),
			Pending:   card.Pending,
		}, now)
		target = appendIssueCardNotice(target, "已合并「"+card.Title+"」，其原句链接保留在本卡。")
		target.Version++
		target.UpdatedAt = now
		cards[targetIndex] = target

		card.Status = IssueCardStatusMerged
		card.MergedInto = target.ID
		card.Version++
		card.UpdatedAt = now
		cards[index] = card
	default:
		return IssueCardSnapshot{}, ErrInvalidRequest
	}
	m.cards[debateID] = cards
	return m.issueCardSnapshotLocked(d), nil
}

// issueCardSortedSources 返回按轮次与消息 ID 排序的发言，供分析输入保持确定性。
func issueCardSortedSources(sources []IssueCardSource) []IssueCardSource {
	out := append([]IssueCardSource(nil), sources...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].RoundNo != out[j].RoundNo {
			return out[i].RoundNo < out[j].RoundNo
		}
		if out[i].TurnID != out[j].TurnID {
			return out[i].TurnID < out[j].TurnID
		}
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return out[i].MessageID < out[j].MessageID
	})
	return out
}

// issueCardTrimTitle 把标题限制在上限内，避免 Fake 样例超长。
func issueCardTrimTitle(value string) string {
	trimmed := strings.TrimSpace(value)
	runes := []rune(trimmed)
	if len(runes) > MaxIssueCardTitleCodepoints {
		runes = runes[:MaxIssueCardTitleCodepoints]
	}
	return string(runes)
}
