package debate

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// 本文件是 V2-06/V2-07 争点卡的服务端契约：稳定的卡片身份、双方原文引用、
// 待澄清内容、人工编辑与晚到分析的合并规则。
//
// 边界（与 02 设计一致）：
//   - 只对已完赛的四轮基础练习（standard_4）做赛后集中整理；未完赛与历史 quick_3 隔离。
//   - 卡片身份来自“提出位置”这条正式消息的原文引用；重复触发与晚到分析都按它归并，
//     不新建第二张同源卡。
//   - 锚点复用 V2-04 的 ResolveAnchor / VerifiedQuote：精确命中才带坐标，
//     重复歧义、失效、未命中一律只保留引用文本并标记待核对，绝不猜测位置。
//   - 人工改名 / 合并 / 移除优先于 AI 更新；AI 只追加相关回应与待澄清，
//     冲突以卡片上的 notices 呈现，不静默覆盖。
//   - 分析使用本服务自己的单槽，不占用正式发言与总结的 Gate。

const (
	// MaxIssueCardsDisplayed 是默认最多展示的主要争点卡数量（PRD §6.1）。
	MaxIssueCardsDisplayed = 5
	// MaxIssueCardCandidates 限制单次分析返回的候选条数，超出即整份拒绝。
	MaxIssueCardCandidates = 12
	// MaxIssueCardTitleCodepoints 是卡片标题上限。
	MaxIssueCardTitleCodepoints = 80
	// MaxIssueCardExplanationCodepoints 是“回应是否切中问题”解释的上限。
	MaxIssueCardExplanationCodepoints = 600
	// MaxIssueCardNoteCodepoints 是单条引用说明的上限。
	MaxIssueCardNoteCodepoints = 200
	// MaxIssueCardPendingCodepoints 是单条待澄清问题的上限。
	MaxIssueCardPendingCodepoints = 300
	// MaxIssueCardResponses 是单张卡的相关回应上限。
	MaxIssueCardResponses = 6
	// MaxIssueCardPendingItems 是单张卡的待澄清条数上限。
	MaxIssueCardPendingItems = 5
	// MaxIssueCardQuoteCodepoints 与批注一致：逐字引用上限，复用同一原文复核。
	MaxIssueCardQuoteCodepoints = MaxAnnotationQuoteCodepoints
)

// issueCardCleanupTimeout 是失败落库的清理期限。
// 清理必须脱离请求取消（否则用户断开就永远停在 generating），但也必须有界，
// 不能在没有 deadline 的 context 上等待存储。
const issueCardCleanupTimeout = 3 * time.Second

// issueCardDependencyCode 是分析写回失败对外暴露的受控错误码；
// 内部分析失败仍沿用 issueCardFailure 的 model_* 码。
const issueCardDependencyCode = "dependency_unavailable"

// 读取投影的三种场次状态；不写库，按当前会话状态派生。
const (
	IssueCardStateNotReady    = "not_ready"
	IssueCardStateUnsupported = "unsupported"
	IssueCardStateReady       = "ready"
)

// 分析记录状态。not_started 只出现在投影里。
const (
	IssueCardAnalysisNotStarted = "not_started"
	IssueCardAnalysisGenerating = "generating"
	IssueCardAnalysisCompleted  = "completed"
	IssueCardAnalysisFailed     = "failed"
)

// 人工编辑动作。
const (
	IssueCardActionRename = "rename"
	IssueCardActionMerge  = "merge"
	IssueCardActionRemove = "remove"
)

// 标题来源：人工值永不被 AI 覆盖。
const (
	IssueCardTitleSourceAI   = "ai"
	IssueCardTitleSourceUser = "user"
)

// 卡片生命周期状态；removed 与 merged 都不再作为独立卡返回。
const (
	IssueCardStatusActive  = "active"
	IssueCardStatusRemoved = "removed"
	IssueCardStatusMerged  = "merged"
)

var (
	// ErrIssueCardVersionConflict 表示人工编辑携带的版本已过期（CAS 失败）。
	ErrIssueCardVersionConflict = errors.New("issue_card_version_conflict")
	// ErrIssueCardNotReady 表示该场尚未完赛，赛后整理尚不可用。
	ErrIssueCardNotReady = errors.New("issue_card_not_ready")
	// ErrIssueCardModeUnsupported 表示历史 quick_3 等非四轮基础练习场次被隔离。
	ErrIssueCardModeUnsupported = errors.New("issue_card_mode_unsupported")
	// ErrIssueCardNotFound 表示卡片不存在或不属于该场（对外统一 404）。
	ErrIssueCardNotFound = errors.New("issue_card_not_found")
)

// IssueCardSource 是一条允许被引用的正式发言（已保存、已授权、已加载）。
type IssueCardSource struct {
	MessageID uint64
	TurnID    uint64
	RoundNo   int
	Role      string
	Content   string
}

// IssueCardCitation 是一处双方原句引用。Anchor.Resolved=false 时前端只显示“待核对”。
type IssueCardCitation struct {
	Anchor  Anchor `json:"anchor"`
	Speaker string `json:"speaker"`
	TurnNo  int    `json:"turn_no"`
	Note    string `json:"note"`
}

// IssueCard 是一张持久化的争点卡，身份在人工编辑和晚到分析之间保持稳定。
type IssueCard struct {
	ID          string
	DebateID    uint64
	Title       string
	TitleSource string
	Explanation string
	Origin      IssueCardCitation
	Responses   []IssueCardCitation
	Pending     []string
	Notices     []string
	Version     uint64
	Status      string
	MergedInto  string
	SourceKey   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IssueCardAnalysis 是一场会话唯一的赛后整理记录；版本用于读取与重试的 CAS。
type IssueCardAnalysis struct {
	Status         string
	AttemptVersion uint64
	ErrorCode      *string
	ThroughStep    int
	LeaseExpiresAt *time.Time
	UpdatedAt      time.Time
}

// IssueCardSnapshot 是 Store 返回的读取视图：会话、已保存发言与争点卡数据。
type IssueCardSnapshot struct {
	Debate   Debate
	Sources  []IssueCardSource
	Analysis IssueCardAnalysis
	Cards    []IssueCard
	Pending  bool
	Ready    bool
}

// IssueCardClaim 是一次成功认领的分析输入快照。
type IssueCardClaim struct {
	Snapshot IssueCardSnapshot
}

// IssueCardDraft 是校验并复核锚点后的候选，可直接交给 Store 合并。
type IssueCardDraft struct {
	Title       string
	Explanation string
	Origin      IssueCardCitation
	Responses   []IssueCardCitation
	Pending     []string
}

// IssueCardCandidate 是分析器给出的候选；坐标只能作为提示，必须服务端复核。
type IssueCardCandidate struct {
	Title       string
	Explanation string
	Origin      IssueCardCitation
	Responses   []IssueCardCitation
	Pending     []string
}

// IssueCardResult 是一次分析的候选集合。
type IssueCardResult struct {
	Candidates []IssueCardCandidate
}

// IssueCardRequest 是一次赛后整理的分析输入：只含该场已保存的正式发言。
type IssueCardRequest struct {
	UserID     uint64
	Mode       ai.Mode
	Topic      ai.Topic
	UserStance ai.Stance
	Sources    []IssueCardSource
}

// IssueCardAnalyzer 是分析能力的业务接口，由 Fake 与未来真实适配器实现。
type IssueCardAnalyzer interface {
	AnalyzeIssueCards(ctx context.Context, req IssueCardRequest) (IssueCardResult, error)
}

// IssueCardAnalyzeInput 是赛后整理的唯一请求体。
// Retry 是显式动作：失败或需要重新整理时才为 true，默认重复触发只读回既有结果。
type IssueCardAnalyzeInput struct {
	Retry                  bool   `json:"retry"`
	ExpectedAttemptVersion uint64 `json:"expected_attempt_version"`
}

// IssueCardEditInput 是人工编辑的唯一请求体；三种动作共用版本检查。
type IssueCardEditInput struct {
	ExpectedVersion uint64  `json:"expected_version"`
	Action          string  `json:"action"`
	Title           *string `json:"title,omitempty"`
	TargetCardID    *string `json:"target_card_id,omitempty"`
}

// IssueCardDTO 是读取与编辑响应中的一张卡；只含展示与定位所需字段。
type IssueCardDTO struct {
	ID          string              `json:"id"`
	Title       string              `json:"title"`
	TitleSource string              `json:"title_source"`
	Explanation string              `json:"explanation"`
	Origin      IssueCardCitation   `json:"origin"`
	Responses   []IssueCardCitation `json:"responses"`
	Pending     []string            `json:"pending"`
	Notices     []string            `json:"notices"`
	Version     uint64              `json:"version"`
}

// IssueCardAnalysisDTO 是赛后整理的当前状态。
type IssueCardAnalysisDTO struct {
	Status         string  `json:"status"`
	AttemptVersion uint64  `json:"attempt_version"`
	ErrorCode      *string `json:"error_code,omitempty"`
}

// IssueCardListDTO 是读取与整理响应。
type IssueCardListDTO struct {
	State      string               `json:"state"`
	DisplayCap int                  `json:"display_cap"`
	Pending    bool                 `json:"pending,omitempty"`
	Analysis   IssueCardAnalysisDTO `json:"analysis"`
	Items      []IssueCardDTO       `json:"items"`
}

// issueCardAnchorKey 是引用去重键：同一消息、同一坐标、同一逐字原文才算同一条引用。
// 待核对引用没有坐标，因此只按消息与原文比较，避免把两处同文引用误当成一条。
func issueCardAnchorKey(anchor Anchor) string {
	start, end := "x", "x"
	if anchor.StartCP != nil {
		start = strconv.Itoa(*anchor.StartCP)
	}
	if anchor.EndCP != nil {
		end = strconv.Itoa(*anchor.EndCP)
	}
	return anchor.MessageID + "|" + start + "|" + end + "|" + anchor.Quote
}

// issueCardCitationSourceKey 是卡片稳定身份：提出位置的消息与逐字原文。
// 它不含坐标，因此同一处原文在不同尝试中被重新定位时仍归并到同一张卡。
func issueCardCitationSourceKey(citation IssueCardCitation) string {
	return citation.Anchor.MessageID + "\x00" + citation.Anchor.Quote
}

// issueCardEligibility 判断该场是否允许赛后整理；顺序保证 quick_3 先被隔离。
func issueCardEligibility(d Debate) error {
	if d.IsTraining() {
		return nil // 具体完成位置在训练赛 Store 内从已提交步骤读取。
	}
	if d.Mode != string(ai.ModeStandard4) {
		return ErrIssueCardModeUnsupported
	}
	rounds, err := ai.RoundsFor(ai.Mode(d.Mode))
	if err != nil {
		return ErrIssueCardModeUnsupported
	}
	if d.Status != "completed" || d.CompletedRounds != rounds {
		return ErrIssueCardNotReady
	}
	return nil
}

// resolveIssueCardCitation 用保存的权威原文复核一条候选引用。
//
// 语义：
//   - 引用必须逐字出现在该条正式发言里；只有唯一命中（或可逐字复核的提示偏移）才带坐标。
//   - 重复歧义、坐标失配与“未命中”都只保留引用文本并标记待核对，卡片仍可读。
//   - 空引用、全空白、非法 UTF-8 或超长属于候选结构错误，整份分析拒绝。
func resolveIssueCardCitation(source IssueCardSource, quote string, hint *int) (IssueCardCitation, error) {
	if !validAnnotationText(quote, MaxIssueCardQuoteCodepoints) {
		return IssueCardCitation{}, ErrInvalidRequest
	}
	anchor, err := ResolveAnchor(source.MessageID, source.Content, quote, hint)
	if err != nil {
		// 未命中：不猜测位置，但保留原句供用户自行核对。
		anchor = Anchor{MessageID: formatID(source.MessageID), Quote: quote}
	}
	return IssueCardCitation{Anchor: anchor, Speaker: source.Role, TurnNo: source.RoundNo}, nil
}

// normalizeIssueCardNote 校验引用说明；空说明使用默认文案。
func normalizeIssueCardNote(value, fallback string) (string, error) {
	if value == "" {
		return fallback, nil
	}
	if !validAnnotationText(value, MaxIssueCardNoteCodepoints) {
		return "", ErrInvalidRequest
	}
	return value, nil
}

// normalizeIssueCardCandidates 校验候选并复核全部锚点。
//
// 任何结构性问题都整份拒绝（分析失败但保留既有卡片）：
// 标题/解释/待澄清非法、引用消息不属于本场、引用文本非法、候选超量。
// 重复歧义与未命中不是拒绝理由，而是降级为待核对。
func normalizeIssueCardCandidates(sources []IssueCardSource, candidates []IssueCardCandidate) ([]IssueCardDraft, error) {
	if len(candidates) > MaxIssueCardCandidates {
		return nil, ErrInvalidRequest
	}
	byID := make(map[uint64]IssueCardSource, len(sources))
	for _, source := range sources {
		byID[source.MessageID] = source
	}
	drafts := make([]IssueCardDraft, 0, len(candidates))
	seenSources := make(map[string]bool, len(candidates))
	for _, candidate := range candidates {
		if !validAnnotationText(candidate.Title, MaxIssueCardTitleCodepoints) ||
			!validAnnotationText(candidate.Explanation, MaxIssueCardExplanationCodepoints) {
			return nil, ErrInvalidRequest
		}
		originID, err := ParseID(candidate.Origin.Anchor.MessageID)
		if err != nil {
			return nil, ErrInvalidRequest
		}
		source, ok := byID[originID]
		if !ok {
			// 跨场或未保存的消息绝不接受。
			return nil, ErrInvalidRequest
		}
		origin, err := resolveIssueCardCitation(source, candidate.Origin.Anchor.Quote, candidate.Origin.Anchor.StartCP)
		if err != nil {
			return nil, err
		}
		if origin.Note, err = normalizeIssueCardNote(candidate.Origin.Note, "提出位置"); err != nil {
			return nil, err
		}
		draft := IssueCardDraft{Title: candidate.Title, Explanation: candidate.Explanation, Origin: origin}
		if len(candidate.Responses) > MaxIssueCardResponses {
			return nil, ErrInvalidRequest
		}
		seenCitations := map[string]bool{issueCardAnchorKey(origin.Anchor): true}
		for _, raw := range candidate.Responses {
			responseID, err := ParseID(raw.Anchor.MessageID)
			if err != nil {
				return nil, ErrInvalidRequest
			}
			responseSource, ok := byID[responseID]
			if !ok {
				return nil, ErrInvalidRequest
			}
			citation, err := resolveIssueCardCitation(responseSource, raw.Anchor.Quote, raw.Anchor.StartCP)
			if err != nil {
				return nil, err
			}
			if citation.Note, err = normalizeIssueCardNote(raw.Note, "相关回应"); err != nil {
				return nil, err
			}
			key := issueCardAnchorKey(citation.Anchor)
			if seenCitations[key] {
				continue
			}
			seenCitations[key] = true
			draft.Responses = append(draft.Responses, citation)
		}
		if len(candidate.Pending) > MaxIssueCardPendingItems {
			return nil, ErrInvalidRequest
		}
		for _, pending := range candidate.Pending {
			if !validAnnotationText(pending, MaxIssueCardPendingCodepoints) {
				return nil, ErrInvalidRequest
			}
			if issueCardContainsString(draft.Pending, pending) {
				continue
			}
			draft.Pending = append(draft.Pending, pending)
		}
		sourceKey := issueCardCitationSourceKey(draft.Origin)
		if seenSources[sourceKey] {
			// 同一提出位置的重复候选只保留第一条，绝不产生第二张同源卡。
			continue
		}
		seenSources[sourceKey] = true
		drafts = append(drafts, draft)
	}
	return drafts, nil
}

func issueCardContainsString(values []string, value string) bool {
	for _, current := range values {
		if current == value {
			return true
		}
	}
	return false
}

// ValidateIssueCardEditInput 只做格式校验；版本与卡片状态由 Store 在临界区内检查。
func ValidateIssueCardEditInput(in IssueCardEditInput) (IssueCardEditInput, error) {
	switch in.Action {
	case IssueCardActionRename:
		if in.Title == nil || !validAnnotationText(*in.Title, MaxIssueCardTitleCodepoints) {
			return in, ErrInvalidRequest
		}
		if in.TargetCardID != nil {
			return in, ErrInvalidRequest
		}
	case IssueCardActionMerge:
		if in.TargetCardID == nil || in.Title != nil {
			return in, ErrInvalidRequest
		}
		if _, err := ParseID(*in.TargetCardID); err != nil {
			return in, ErrInvalidRequest
		}
	case IssueCardActionRemove:
		if in.Title != nil || in.TargetCardID != nil {
			return in, ErrInvalidRequest
		}
	default:
		return in, ErrInvalidRequest
	}
	return in, nil
}

// projectIssueCard 把存储卡片投影成 DTO，并按当前权威原文重新复核每条引用。
//
// 已保存的坐标只在回读逐字相等时继续使用；否则降级为待核对。
// 从未解析成功的引用会再尝试一次唯一定位（仍歧义或未命中则保持待核对）。
func projectIssueCard(card IssueCard, sources map[string]IssueCardSource) IssueCardDTO {
	dto := IssueCardDTO{
		ID:          card.ID,
		Title:       card.Title,
		TitleSource: card.TitleSource,
		Explanation: card.Explanation,
		Origin:      projectIssueCardCitation(card.Origin, sources),
		Responses:   make([]IssueCardCitation, 0, len(card.Responses)),
		Pending:     append([]string{}, card.Pending...),
		Notices:     append([]string{}, card.Notices...),
		Version:     card.Version,
	}
	for _, citation := range card.Responses {
		dto.Responses = append(dto.Responses, projectIssueCardCitation(citation, sources))
	}
	return dto
}

func projectIssueCardCitation(citation IssueCardCitation, sources map[string]IssueCardSource) IssueCardCitation {
	projected := IssueCardCitation{Anchor: citation.Anchor, Speaker: citation.Speaker, TurnNo: citation.TurnNo, Note: citation.Note}
	source, ok := sources[citation.Anchor.MessageID]
	if !ok {
		projected.Anchor = unresolvedIssueCardAnchor(citation.Anchor)
		return projected
	}
	if citation.Anchor.Resolved {
		start, end := citation.Anchor.StartCP, citation.Anchor.EndCP
		if start == nil || end == nil || !VerifiedQuote(source.Content, *start, *end, citation.Anchor.Quote) {
			// 原文已变化或坐标损坏：只降级读取投影，不写库、不猜测新位置。
			projected.Anchor = unresolvedIssueCardAnchor(citation.Anchor)
		}
		return projected
	}
	if anchor, err := ResolveAnchor(source.MessageID, source.Content, citation.Anchor.Quote, nil); err == nil && anchor.Resolved {
		projected.Anchor = anchor
		return projected
	}
	projected.Anchor = unresolvedIssueCardAnchor(citation.Anchor)
	return projected
}

func unresolvedIssueCardAnchor(anchor Anchor) Anchor {
	return Anchor{MessageID: anchor.MessageID, Quote: anchor.Quote}
}

// projectIssueCardList 组装读取响应；只有 active 卡参与展示，且默认最多 5 张。
func projectIssueCardList(snapshot IssueCardSnapshot) IssueCardListDTO {
	dto := IssueCardListDTO{
		State:      IssueCardStateReady,
		DisplayCap: MaxIssueCardsDisplayed,
		Analysis:   projectIssueCardAnalysis(snapshot.Analysis),
		Items:      []IssueCardDTO{},
		Pending:    snapshot.Pending,
	}
	if snapshot.Debate.IsTraining() && !snapshot.Ready {
		dto.State = IssueCardStateNotReady
		return dto
	}
	if err := issueCardEligibility(snapshot.Debate); err != nil {
		if errors.Is(err, ErrIssueCardModeUnsupported) {
			dto.State = IssueCardStateUnsupported
		} else {
			dto.State = IssueCardStateNotReady
		}
		return dto
	}
	sources := make(map[string]IssueCardSource, len(snapshot.Sources))
	for _, source := range snapshot.Sources {
		sources[formatID(source.MessageID)] = source
	}
	for _, card := range snapshot.Cards {
		if card.Status != IssueCardStatusActive {
			continue
		}
		if len(dto.Items) >= MaxIssueCardsDisplayed {
			break
		}
		dto.Items = append(dto.Items, projectIssueCard(card, sources))
	}
	return dto
}

func projectIssueCardAnalysis(analysis IssueCardAnalysis) IssueCardAnalysisDTO {
	dto := IssueCardAnalysisDTO{Status: analysis.Status, AttemptVersion: analysis.AttemptVersion}
	if dto.Status == "" {
		dto.Status = IssueCardAnalysisNotStarted
	}
	if dto.Status == IssueCardAnalysisFailed {
		dto.ErrorCode = analysis.ErrorCode
	}
	return dto
}

// IssueCardService 编排赛后整理与人工编辑；持久化与 CAS 全部交给 Store。
type IssueCardService struct {
	store    IssueCardStore
	analyzer IssueCardAnalyzer
	slot     chan struct{}
}

// NewIssueCardService 装配独立争点卡服务；分析槽容量为 1 且不排队。
func NewIssueCardService(store IssueCardStore, analyzer IssueCardAnalyzer) *IssueCardService {
	return &IssueCardService{store: store, analyzer: analyzer, slot: make(chan struct{}, 1)}
}

func (s *IssueCardService) acquireSlot() bool {
	select {
	case s.slot <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *IssueCardService) releaseSlot() { <-s.slot }

// Get 只读返回当前会话的争点卡；不调用模型、不写库。
func (s *IssueCardService) Get(ctx context.Context, uid, debateID uint64) (IssueCardListDTO, error) {
	if uid == 0 || debateID == 0 {
		return IssueCardListDTO{}, ErrInvalidRequest
	}
	snapshot, err := s.store.LoadIssueCards(ctx, uid, debateID)
	if err != nil {
		return IssueCardListDTO{}, err
	}
	return projectIssueCardList(snapshot), nil
}

// Analyze 是赛后集中整理的入口：重复触发读回既有结果，Retry 才真正重新分析。
//
// 槽位忙时返回 202 与当前状态，不排队、不重试；分析失败只写受控错误码，
// 已成功卡片与人工编辑保持不变。
func (s *IssueCardService) Analyze(ctx context.Context, uid, debateID uint64, in IssueCardAnalyzeInput) (IssueCardListDTO, int, error) {
	return s.analyze(ctx, uid, debateID, in, false)
}

func (s *IssueCardService) analyze(ctx context.Context, uid, debateID uint64, in IssueCardAnalyzeInput, waitForSlot bool) (IssueCardListDTO, int, error) {
	if uid == 0 || debateID == 0 {
		return IssueCardListDTO{}, 0, ErrInvalidRequest
	}
	if s.analyzer == nil {
		return IssueCardListDTO{}, 0, ErrIssueCardModeUnsupported
	}
	if waitForSlot {
		select {
		case s.slot <- struct{}{}:
		case <-ctx.Done():
			return IssueCardListDTO{}, 0, ctx.Err()
		}
	} else if !s.acquireSlot() {
		snapshot, err := s.store.LoadIssueCards(ctx, uid, debateID)
		if err != nil {
			return IssueCardListDTO{}, 0, err
		}
		return projectIssueCardList(snapshot), 202, nil
	}
	defer s.releaseSlot()
	claim, fresh, err := s.store.ClaimIssueCardAnalysis(ctx, uid, debateID, in.Retry, in.ExpectedAttemptVersion)
	if err != nil {
		return IssueCardListDTO{}, 0, err
	}
	if !fresh {
		list := projectIssueCardList(claim.Snapshot)
		if claim.Snapshot.Analysis.Status == IssueCardAnalysisGenerating {
			return list, 202, nil
		}
		return list, 200, nil
	}
	result, genErr := s.analyzer.AnalyzeIssueCards(ctx, IssueCardRequest{
		UserID:     uid,
		Mode:       ai.Mode(claim.Snapshot.Debate.Mode),
		Topic:      ai.Topic{Title: claim.Snapshot.Debate.TopicTitleSnapshot, Description: claim.Snapshot.Debate.TopicDescriptionSnapshot},
		UserStance: ai.Stance(claim.Snapshot.Debate.UserStance),
		Sources:    claim.Snapshot.Sources,
	})
	if genErr == nil {
		drafts, normErr := normalizeIssueCardCandidates(claim.Snapshot.Sources, result.Candidates)
		if normErr == nil {
			snapshot, saveErr := s.store.CompleteIssueCardAnalysis(ctx, uid, claim, drafts)
			if saveErr != nil {
				// 写回结果的两种语义不能混用：
				//   - 确定未提交（CAS 冲突 / 带 ErrIssueCardNotCommitted 的写入拒绝）：
				//     按同一 attempt 落受控失败，允许用户带该版本显式重试；
				//   - 提交结果不明：既不宣布失败（可能已落库），也不自动重跑，
				//     只报带 CommitUnknown 的错误，由读取状态恢复。
				if issueCardWriteDeterminate(saveErr) {
					s.failIssueCardAnalysis(ctx, uid, claim, issueCardDependencyCode)
					return IssueCardListDTO{}, 0, dependency(saveErr, false)
				}
				return IssueCardListDTO{}, 0, dependency(saveErr, true)
			}
			return projectIssueCardList(snapshot), 201, nil
		}
		genErr = normErr
	}
	code, status := issueCardFailure(genErr)
	s.failIssueCardAnalysis(ctx, uid, claim, code)
	return IssueCardListDTO{}, 0, &Failure{Code: code, Status: status, Cause: genErr}
}

// issueCardWriteDeterminate 判断一次写回错误是否“确定未提交”。
//
// 只有 CAS 冲突与显式标记 ErrIssueCardNotCommitted 的错误可以确定未提交；
// 其它错误（连接中断、超时、上下文取消）都可能是提交后失联，不能当作失败处理。
func issueCardWriteDeterminate(err error) bool {
	return errors.Is(err, ErrIssueCardNotCommitted) || errors.Is(err, ErrIssueCardVersionConflict)
}

// failIssueCardAnalysis 在取消后仍尽力落受控失败，但清理 context 必须有界。
//
// 用 WithoutCancel 是为了避免用户断开后分析永久停在 generating；用 WithTimeout 是
// 为了不让清理在无期限的 context 上无限等待存储。失败写回仍走同 attempt 的 CAS，
// 因此不会覆盖已经提交的成功状态或更新的尝试。
func (s *IssueCardService) failIssueCardAnalysis(ctx context.Context, uid uint64, claim IssueCardClaim, code string) {
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), issueCardCleanupTimeout)
	defer stop()
	_, _ = s.store.FailIssueCardAnalysis(cleanup, uid, claim, code)
}

// Edit 是人工改名 / 合并 / 移除的唯一入口。
//
// 版本冲突返回 ErrIssueCardVersionConflict，已保存卡片与人工值不被覆盖；
// 合并保留双方原句链接，被合并卡不再作为独立卡返回，也不会被晚到分析拆回。
func (s *IssueCardService) Edit(ctx context.Context, uid, debateID uint64, cardID string, in IssueCardEditInput) (IssueCardDTO, int, error) {
	if uid == 0 || debateID == 0 {
		return IssueCardDTO{}, 0, ErrInvalidRequest
	}
	if _, err := ParseID(cardID); err != nil {
		return IssueCardDTO{}, 0, ErrInvalidRequest
	}
	in, err := ValidateIssueCardEditInput(in)
	if err != nil {
		return IssueCardDTO{}, 0, err
	}
	snapshot, err := s.store.ApplyIssueCardEdit(ctx, uid, debateID, cardID, in)
	if err != nil {
		return IssueCardDTO{}, 0, err
	}
	for _, card := range snapshot.Cards {
		if card.ID != cardID {
			continue
		}
		sources := make(map[string]IssueCardSource, len(snapshot.Sources))
		for _, source := range snapshot.Sources {
			sources[formatID(source.MessageID)] = source
		}
		return projectIssueCard(card, sources), 200, nil
	}
	return IssueCardDTO{}, 0, ErrIssueCardNotFound
}

// issueCardFailure 把分析错误映射为受控业务码；不暴露底层原因。
func issueCardFailure(err error) (string, int) {
	var aiErr *ai.Error
	if errors.As(err, &aiErr) {
		switch aiErr.Code {
		case ai.CodeModelTimeout:
			return "model_timeout", 504
		case ai.CodeModelUnavailable, ai.CodeQuotaExceeded, ai.CodeRateLimited:
			return "model_unavailable", 503
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "model_timeout", 504
	}
	if errors.Is(err, context.Canceled) {
		return "model_unavailable", 503
	}
	return "model_error", 502
}

// issueCardQuotePrefix 从一条正式发言里取不超过上限的逐字前缀，供 Fake 生成可复核引用。
// 只在 Fake 中用于构造样例；真实分析器必须给出与原文逐字相等的引用。
func issueCardQuotePrefix(content string, limit int) string {
	runes := []rune(content)
	if len(runes) <= limit {
		return content
	}
	limit = issueCardSafePrefix(runes, limit)
	return string(runes[:limit])
}

// issueCardSafePrefix 避免在组合符、ZWJ 或修饰符之前切开可见字素。
func issueCardSafePrefix(runes []rune, at int) int {
	for at > 1 && at < len(runes) {
		next := runes[at]
		if next == '\u200d' || (next >= 0x1F3FB && next <= 0x1F3FF) ||
			runes[at-1] == '\u200d' || runes[at-1] == 0xFE0F || runes[at-1] == 0xFE0E {
			at--
			continue
		}
		break
	}
	if at < 1 {
		return 1
	}
	return at
}

// issueCardHTTPError 把内部错误映射到既有 HTTP 错误形状，不改动共享的 serviceError。
//
// 包装成 *Failure 后仍保留 Unwrap，因此单元测试可以继续用 errors.Is 做断言。
func issueCardHTTPError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrIssueCardVersionConflict):
		return &Failure{Code: "issue_card_version_conflict", Status: 409, Cause: err}
	case errors.Is(err, ErrIssueCardNotReady):
		return &Failure{Code: "issue_card_not_ready", Status: 409, Cause: err}
	case errors.Is(err, ErrIssueCardModeUnsupported):
		return &Failure{Code: "issue_card_mode_unsupported", Status: 409, Cause: err}
	case errors.Is(err, ErrIssueCardNotFound):
		return &Failure{Code: "not_found", Status: 404, Cause: err}
	}
	return err
}
