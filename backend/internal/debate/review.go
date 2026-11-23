package debate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

// V2-13 赛后复盘：独立于轮次、总结与批注的第三类生成结果。
//
// 边界（PRD §8.6、§10.2、§10.3、AC-V2-24）：
//   - 比赛完成状态与复盘状态是两个独立事实；复盘失败不改写会话状态。
//   - 失败只保留受控错误码，不保存半成品；重试整体重生成这一份复盘（原子写入）。
//   - 已成功的总结、批注、人工编辑分属其它行/表，本模块从不写入或清空它们。
//   - 首版不自动触发：只有用户显式请求或显式重试才会生成。

const (
	reviewStatusNotStarted = "not_started"
	reviewStatusGenerating = "generating"
	reviewStatusCompleted  = "completed"
	reviewStatusFailed     = "failed"

	// ReviewAnalysisTimeout 限制单次复盘生成的期限，与模型单次调用上限一致。
	ReviewAnalysisTimeout = ai.DefaultModelTimeout
)

// Review 是 debate_reviews 的持久化行，字段形状与总结/策略/批注表保持一致，
// 但业务语义、表、版本和状态机完全独立。
type Review struct {
	ID             uint64     `json:"-" gorm:"primaryKey"`
	DebateID       uint64     `json:"-"`
	Status         string     `json:"-"`
	SchemaVersion  *int       `json:"-"`
	ContentJSON    *string    `json:"-"`
	AttemptVersion uint64     `json:"-"`
	LeaseExpiresAt *time.Time `json:"-"`
	ErrorCode      *string    `json:"-"`
	ModelName      *string    `json:"-"`
	PromptVersion  *string    `json:"-"`
	InputTokens    *uint64    `json:"-"`
	OutputTokens   *uint64    `json:"-"`
	LatencyMS      *uint64    `json:"-"`
	CreatedAt      time.Time  `json:"-"`
	UpdatedAt      time.Time  `json:"-"`
}

func (Review) TableName() string { return "debate_reviews" }

// ReviewStatus 是读取接口的投影：状态、版本、受控错误码与已保存内容。
// not_started 只出现在投影里，不落库。
//
// Available 只在复盘功能关闭时显式返回 false：此时 status 恒为 not_started，
// 但客户端必须隐藏复盘界面并禁止发起生成，不能把它当成“可以点了生成”的未开始。
// 功能启用时该字段省略（nil），旧客户端与旧 Mock 的 not_started 形状保持可用。
type ReviewStatus struct {
	Status         string     `json:"status"`
	AttemptVersion *uint64    `json:"attempt_version"`
	Available      *bool      `json:"available,omitempty"`
	ErrorCode      *string    `json:"error_code,omitempty"`
	SchemaVersion  *int       `json:"schema_version,omitempty"`
	PromptVersion  *string    `json:"prompt_version,omitempty"`
	Content        *ai.Review `json:"content,omitempty"`
}

func (r *Review) DTO() ReviewStatus {
	if r == nil {
		return ReviewStatus{Status: reviewStatusNotStarted}
	}
	version := r.AttemptVersion
	out := ReviewStatus{Status: r.Status, AttemptVersion: &version}
	switch r.Status {
	case reviewStatusFailed:
		out.ErrorCode = r.ErrorCode
	case reviewStatusCompleted:
		if r.ContentJSON != nil {
			if parsed, err := ai.ParseReview(*r.ContentJSON); err == nil {
				schemaVer := ai.ReviewSchemaVersion
				if r.SchemaVersion != nil && *r.SchemaVersion > 0 {
					schemaVer = *r.SchemaVersion
				}
				out.SchemaVersion = &schemaVer
				out.PromptVersion = r.PromptVersion
				out.Content = &parsed
			} else {
				// 已保存内容不可读时如实标记为失败，不假装成功、也不在读取期改写数据库。
				code := "content_invalid"
				out.Status = reviewStatusFailed
				out.ErrorCode = &code
			}
		}
	}
	return out
}

// ReviewInput 是显式生成/重试的请求正文；首版不提供自动触发与额外字段。
type ReviewInput struct {
	Retry                  bool
	ExpectedAttemptVersion *uint64
}

// 严格解析：未知键、重复键、null 与类型不符一律 400，不静默回退。
func (in *ReviewInput) UnmarshalJSON(raw []byte) error {
	*in = ReviewInput{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ErrInvalidRequest
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return ErrInvalidRequest
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return ErrInvalidRequest
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return ErrInvalidRequest
		}
		switch key {
		case "retry":
			if bytes.Equal(bytes.TrimSpace(value), []byte("null")) || json.Unmarshal(value, &in.Retry) != nil {
				return ErrInvalidRequest
			}
		case "expected_attempt_version":
			var version uint64
			if json.Unmarshal(value, &version) != nil || version == 0 || version == ^uint64(0) {
				return ErrInvalidRequest
			}
			in.ExpectedAttemptVersion = &version
		default:
			return ErrInvalidRequest
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return ErrInvalidRequest
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidRequest
	}
	return ValidateReviewInput(*in)
}

// ValidateReviewInput 要求 retry 与 expected_attempt_version 严格同时出现或同时缺失。
func ValidateReviewInput(in ReviewInput) error {
	if in.Retry != (in.ExpectedAttemptVersion != nil) {
		return ErrInvalidRequest
	}
	if in.ExpectedAttemptVersion != nil && (*in.ExpectedAttemptVersion == 0 || *in.ExpectedAttemptVersion == ^uint64(0)) {
		return ErrInvalidRequest
	}
	return nil
}

// ReviewClaim 是一次成功认领的全部输入快照，供锁外生成使用。
// Messages 只包含本场已提交的正式消息，是锚点复核的唯一权威来源。
type ReviewClaim struct {
	Debate   Debate
	Review   Review
	Messages []ai.ReviewMessage
}

// failedReviewDetails 返回受控失败信封的定位信息。
func failedReviewDetails(d Debate, r Review) *FailureDetails {
	return &FailureDetails{Status: reviewStatusFailed, AttemptVersion: r.AttemptVersion,
		QueryURL: "/api/v1/debates/" + formatID(d.ID) + "/review"}
}

func (s *Service) EnableReviews(enabled bool) {
	s.reviewMu.Lock()
	defer s.reviewMu.Unlock()
	s.reviewEnabled = enabled
}

// reviewsEnabled 同时要求显式开关与生成器具备复盘能力。
func (s *Service) reviewsEnabled() bool {
	if s == nil || s.generator == nil {
		return false
	}
	s.reviewMu.Lock()
	enabled := s.reviewEnabled
	s.reviewMu.Unlock()
	if !enabled {
		return false
	}
	_, ok := s.generator.(ai.ReviewGenerator)
	return ok
}

// GetReview 只读返回复盘状态，不触发任何生成。
//
// 功能关闭时必须完全不读 debate_reviews：该表属于新切片，旧部署可能尚未执行
// 迁移，直接查询会得到 503，违反“关闭即不访问新表”的既有开关契约。
// 关闭分支只借用既有会话只读接口校验归属（非法 ID 与越权仍是 404，
// 不能对任意 ID 一律回 not_started），再投影 not_started。
func (s *Service) GetReview(ctx context.Context, uid, id uint64) (ReviewStatus, error) {
	if err := s.waitStartupScan(ctx); err != nil {
		return ReviewStatus{}, dependency(err, false)
	}
	if !s.reviewsEnabled() {
		if _, err := s.store.Detail(ctx, uid, id); err != nil {
			return ReviewStatus{}, err
		}
		// 关闭时除了 not_started 还要显式告知不可用：否则客户端会把“未开始”
		// 当成可以点击生成的正常状态，再去撞写入口的 400。
		unavailable := false
		return ReviewStatus{Status: reviewStatusNotStarted, Available: &unavailable}, nil
	}
	review, err := s.store.GetReview(ctx, uid, id)
	if err != nil {
		return ReviewStatus{}, err
	}
	return review.DTO(), nil
}

// RequestReview 是显式入口：带 HTTP 防刷计数，未启用切片时拒绝。
func (s *Service) RequestReview(ctx context.Context, uid, debateID uint64, in ReviewInput) (ReviewStatus, int, error) {
	if err := s.gate.Request(uid); err != nil {
		return ReviewStatus{}, 0, err
	}
	return s.requestReview(ctx, uid, debateID, in)
}

func (s *Service) requestReview(ctx context.Context, uid, debateID uint64, in ReviewInput) (ReviewStatus, int, error) {
	if err := ValidateReviewInput(in); err != nil {
		return ReviewStatus{}, 0, err
	}
	if !s.reviewsEnabled() {
		// 未启用时不产生任何复盘行，也不调用模型。
		return ReviewStatus{}, 0, ErrInvalidRequest
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return ReviewStatus{}, 0, dependency(err, false)
	}
	var permit *Permit
	consumed := false
	defer func() { permit.Finish(consumed) }()
	claim, fresh, err := s.store.ClaimReview(ctx, uid, debateID, in, func() error { var e error; permit, e = s.gate.Acquire(uid); return e })
	if err != nil {
		var f *Failure
		if errors.As(err, &f) && f.CommitUnknown {
			consumed = true
		}
		return ReviewStatus{}, 0, err
	}
	current := claim.Review
	if !fresh {
		switch current.Status {
		case reviewStatusCompleted:
			return current.DTO(), 200, nil
		case reviewStatusGenerating:
			return current.DTO(), 202, nil
		case reviewStatusFailed:
			if in.Retry {
				return ReviewStatus{}, 0, &Failure{Code: "attempt_version_conflict", Status: 409,
					Cause: ErrAttemptVersionConflict, Details: failedReviewDetails(claim.Debate, current)}
			}
			return current.DTO(), 200, nil
		}
		return ReviewStatus{}, 0, dependency(nil, false)
	}
	consumed = true
	successStatus := 201
	if in.Retry {
		successStatus = 200
	}
	return s.runReviewGeneration(ctx, uid, claim, successStatus)
}

// runReviewGeneration 在认领事务之外调用模型，成功时整体原子保存；失败只落受控错误码。
func (s *Service) runReviewGeneration(ctx context.Context, uid uint64, claim ReviewClaim, successStatus int) (ReviewStatus, int, error) {
	d, current := claim.Debate, claim.Review
	// 只接受完赛会话的完整正式消息；历史不完整时按依赖失败处理，不生成半份复盘。
	if !d.IsTraining() {
		rounds, err := ai.RoundsFor(ai.Mode(d.Mode))
		if err != nil || len(claim.Messages) != rounds*2 {
			return ReviewStatus{}, 0, dependency(err, false)
		}
	}
	result, genErr := s.generateReview(ctx, ai.ReviewRequest{
		UserID:     uid,
		Mode:       ai.Mode(d.Mode),
		Topic:      ai.Topic{Title: d.TopicTitleSnapshot, Description: d.TopicDescriptionSnapshot},
		UserStance: ai.Stance(d.UserStance),
		Messages:   claim.Messages,
		Custom:     d.Custom(),
	})
	if genErr == nil {
		genErr = s.locateAndValidateReview(ctx, uid, d, claim.Messages, result, &result.ContentJSON)
	}
	if ctx.Err() != nil {
		genErr = reviewContextError(genErr, result, ctx.Err())
	}
	if genErr != nil {
		code, status, usage, meta := classifyFailure(ctx, genErr)
		return ReviewStatus{}, 0, s.saveReviewFailure(ctx, uid, d, current, code, status, usage, meta, genErr)
	}
	saved, saveErr := s.store.CompleteReview(ctx, uid, d, current, result.ContentJSON, result.Usage, result.Metadata)
	if saveErr != nil {
		var failure *Failure
		if errors.As(saveErr, &failure) && failure.CommitUnknown {
			return ReviewStatus{}, 0, dependency(saveErr, true)
		}
		code, status := "dependency_unavailable", 503
		if ctx.Err() != nil {
			code, status, _, _ = classifyFailure(ctx, reviewContextError(nil, result, ctx.Err()))
		}
		return ReviewStatus{}, 0, s.saveReviewFailure(ctx, uid, d, current, code, status, result.Usage, result.Metadata, saveErr)
	}
	return saved.DTO(), successStatus, nil
}

// locateAndValidateReview 用本场权威原文复核生成结果，并把可核对的引用定位成坐标。
//
// 规则（AC-V2-22、PRD §8.3、§8.4）：
//   - 有档级（1–4）必须有原文依据；依据无法在本场原文中逐字核对到时不赋档，
//     降级为“观察不足”并写明原因，绝不凑值或补一个低档。
//   - 引用能唯一匹配到一条正式消息时直接定位；跨消息歧义只保留“待核对”，不按坐标高亮。
//   - 观察不足是合法结果，必须带原因，且不得同时带档级标签或坐标。
func (s *Service) locateAndValidateReview(ctx context.Context, uid uint64, d Debate, messages []ai.ReviewMessage, result ai.ReviewResult, contentJSON *string) error {
	draft := result.Draft
	raw := result.ContentJSON
	if raw != "" {
		parsed, err := ai.ParseReviewDraft(raw)
		if err != nil {
			return err
		}
		draft = parsed
	} else if err := ai.ValidateReviewDraft(draft); err != nil {
		return err
	}
	// 只信任本次加载的权威消息：编号白名单与原文一致，不信任生成器自报的编号。
	sources := make(map[uint64]ai.ReviewMessage, len(messages))
	for _, message := range messages {
		sources[message.MessageID] = message
	}
	located := locateReviewDraft(draft, sources)
	canonical, err := ai.CanonicalReviewJSON(located)
	if err != nil {
		return err
	}
	if err := ai.ValidateReview(located); err != nil {
		return err
	}
	*contentJSON = canonical
	return nil
}

// locateReviewDraft 把草稿里的逐字引用解析成坐标；无法核对的有档级维度降级为观察不足。
func locateReviewDraft(draft ai.ReviewDraft, sources map[uint64]ai.ReviewMessage) ai.Review {
	out := ai.Review{
		Dimensions:   make([]ai.ReviewDimension, 0, len(draft.Dimensions)),
		KeyIssues:    make([]ai.ReviewText, 0, len(draft.KeyIssues)),
		Strengths:    make([]ai.ReviewText, 0, len(draft.Strengths)),
		Improvements: make([]ai.ReviewText, 0, len(draft.Improvements)),
	}
	for _, dimension := range draft.Dimensions {
		label := ai.ReviewDimensionLabel(dimension.Key)
		located := ai.ReviewDimension{Key: dimension.Key, Label: label, Basis: dimension.Basis, Improvement: dimension.Improvement}
		if dimension.Level == nil {
			reason := dimension.InsufficientReason
			located.InsufficientReason = &reason
			out.Dimensions = append(out.Dimensions, located)
			continue
		}
		level := *dimension.Level
		levelLabel := ai.ReviewLevelLabel(level)
		quote, verified := locateReviewQuote(dimension.Quote, sources)
		if !verified {
			// 无有效依据不得赋档：不保留任何档级，改为带原因的观察不足。
			reason := "服务端未能在本场原文中核对到该维度引用的依据，按“无依据不赋档”处理为观察不足。"
			located.Level = nil
			located.LevelLabel = nil
			located.InsufficientReason = &reason
			located.Quote = nil
			out.Dimensions = append(out.Dimensions, located)
			continue
		}
		located.Level = &level
		located.LevelLabel = &levelLabel
		located.Quote = quote
		out.Dimensions = append(out.Dimensions, located)
	}
	out.KeyIssues = locateReviewTexts(draft.KeyIssues, sources)
	out.Strengths = locateReviewTexts(draft.Strengths, sources)
	out.Improvements = locateReviewTexts(draft.Improvements, sources)
	return out
}

// locateReviewTexts 为争点/优点/改进条目附加可核对的原文坐标；核对不到时保留文字但不给入口。
func locateReviewTexts(items []ai.ReviewTextDraft, sources map[uint64]ai.ReviewMessage) []ai.ReviewText {
	out := make([]ai.ReviewText, 0, len(items))
	for _, item := range items {
		text := ai.ReviewText{Text: item.Text}
		if item.Quote != "" {
			if quote, ok := locateReviewQuote(item.Quote, sources); ok {
				text.Quote = quote
			}
		}
		out = append(out, text)
	}
	return out
}

// locateReviewQuote 在权威消息里逐字定位引用。
//
// 返回 verified=false 表示该引用在本场原文中不存在（外场引用或模型改写）：
// 调用方必须据此拒绝赋档，不得改挂到其它消息。
// 返回 verified=true 且 Resolved=false 表示引用存在但跨消息有歧义，只显示“待核对”。
func locateReviewQuote(quote string, sources map[uint64]ai.ReviewMessage) (*ai.ReviewQuote, bool) {
	if quote == "" {
		return nil, false
	}
	var matched []uint64
	for id, source := range sources {
		if _, err := ResolveAnchor(source.MessageID, source.Content, quote, nil); err == nil {
			matched = append(matched, id)
		}
	}
	if len(matched) == 0 {
		return nil, false
	}
	sortIDs(matched)
	primary := sources[matched[0]]
	anchor, err := ResolveAnchor(primary.MessageID, primary.Content, quote, nil)
	if err != nil {
		return nil, false
	}
	located := &ai.ReviewQuote{
		MessageID: anchor.MessageID,
		Quote:     anchor.Quote,
		StartCP:   anchor.StartCP,
		EndCP:     anchor.EndCP,
		Resolved:  anchor.Resolved && len(matched) == 1,
	}
	if !located.Resolved {
		// 跨消息歧义：只保留“待核对”，不保留坐标，避免前端按错误位置高亮。
		located.StartCP = nil
		located.EndCP = nil
	}
	return located, true
}

func sortIDs(ids []uint64) {
	for i := 1; i < len(ids); i++ {
		for j := i; j > 0 && ids[j] < ids[j-1]; j-- {
			ids[j], ids[j-1] = ids[j-1], ids[j]
		}
	}
}

// generateReview 在一次固定期限的调用内生成复盘；panic 与超时都转成受控错误。
func (s *Service) generateReview(ctx context.Context, req ai.ReviewRequest) (result ai.ReviewResult, err error) {
	callCtx, cancel := context.WithTimeout(ctx, ReviewAnalysisTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			result = ai.ReviewResult{}
			err = &ai.Error{Code: ai.CodeModelError, Message: "复盘生成失败"}
		}
	}()
	generator, ok := s.generator.(ai.ReviewGenerator)
	if !ok {
		return result, &ai.Error{Code: ai.CodeModelUnavailable, Message: "当前生成器不支持复盘"}
	}
	result, err = generator.GenerateReview(callCtx, req)
	if callCtx.Err() != nil {
		err = reviewContextError(err, result, callCtx.Err())
	}
	return
}

func reviewContextError(err error, result ai.ReviewResult, reason error) error {
	return generationContextError(err, ai.Result{Usage: result.Usage, Metadata: result.Metadata}, reason)
}

// saveReviewFailure 在取消后仍尽力落库，只保存受控错误码，不保存任何半成品内容。
func (s *Service) saveReviewFailure(ctx context.Context, uid uint64, d Debate, r Review, code string, status int, usage ai.Usage, meta ai.Metadata, cause error) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	saved, err := s.store.FailReview(cleanup, uid, d, r, code, usage, meta)
	if err != nil {
		return dependency(errors.Join(err, cause), false)
	}
	return &Failure{Code: codeForHTTP(code), Status: status, Cause: cause, Details: failedReviewDetails(d, saved)}
}

// 复盘路由：读取只需 Session；显式生成与重试额外要求 Origin/CSRF 与防刷前置。
func (h *Handler) registerReviewRoutes(group *gin.RouterGroup, a *auth.Handler) {
	group.GET("/debates/:id/review", a.RequireAuth(), h.getReview)
	group.POST("/debates/:id/review", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.requestReview)
}

func (h *Handler) getReview(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, err := h.service.GetReview(c.Request.Context(), current.User.ID, id)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, 200, data)
}

func (h *Handler) requestReview(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	var in ReviewInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, status, err := h.service.requestReview(c.Request.Context(), current.User.ID, id, in)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, status, data)
}
