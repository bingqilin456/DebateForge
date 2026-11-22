package debate

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
)

// V2-08 局部重练的领域契约（PRD §5.7、AC-V2-17、AC-V2-26）。
//
// 边界：
//   - 只接受本场完赛后、用户本人已保存的 user 消息中的一段原文；
//     AI 原文、他人会话与未完成训练一律拒绝。
//   - 每次改写都是独立版本，绑定同一原文；正式消息、会话状态、复盘与后续
//     轮次状态都不被写入或重算。
//   - 首版不自动触发：只有用户显式提交或显式重试才会调用模型。
//   - 反馈只描述“是否改善 / 理由 / 可能的新问题”，不评分、不给整段范文。

var (
	// ErrRepracticeSource 表示选段不成立：不是本场完赛后本人已保存的 user 原文，
	// 或坐标与本场原文无法逐字核对。对外统一 400。
	ErrRepracticeSource = errors.New("重练必须引用本场完赛后本人已保存的原文")
	// ErrRepracticeInput 表示输入不合法或同一请求编号被用于不同内容。
	ErrRepracticeInput = errors.New("重练输入无效或请求编号已被使用")
	// ErrRepracticeVersion 表示版本不存在或不属于本场。
	ErrRepracticeVersion = errors.New("未找到该重练版本")
)

const (
	repracticeStatusGenerating = "generating"
	repracticeStatusCompleted  = "completed"
	repracticeStatusFailed     = "failed"

	// RepracticeMaxCodepoints 是改写与改进目标的单字段上限，与发言正文同规则。
	RepracticeMaxCodepoints = 2000
	// RepracticeMaxRequestIDLength 限制请求编号长度，避免超长键与日志膨胀。
	RepracticeMaxRequestIDLength = 128
	// RepracticeTimeout 限制单次反馈生成的期限，与模型单次调用上限一致。
	RepracticeTimeout = 180 * time.Second
)

// RepracticeInput 是一次提交的完整输入。RequestID 由客户端生成，用于幂等重放；
// Start/End 是从 0 开始的 Unicode 码点半开区间，Quote 必须与该区间逐字相等。
// RestoredFrom 非零时表示这一版是从某历史版本恢复到编辑区后继续改写的：
// 恢复本身不覆盖原版本，仅作为新版本的来源说明。
type RepracticeInput struct {
	RequestID    string
	MessageID    uint64
	Start        int
	End          int
	Quote        string
	Goal         string
	Rewrite      string
	RestoredFrom uint64
}

// RepracticeFeedback 是“改写是否改善”的对照结论，不评分、不返回整段范文。
type RepracticeFeedback struct {
	Improved   bool
	Change     string
	Reason     string
	NewProblem string
}

// RepracticeVersion 是一次独立重练版本；失败时保留全部输入以便显式重试。
type RepracticeVersion struct {
	ID       uint64
	DebateID uint64
	Attempt  uint64
	Status   string
	Input    RepracticeInput
	Feedback RepracticeFeedback
	// ErrorCode 只在 status=failed 时有意义，保存受控失败码。
	ErrorCode string
	// RestoredFrom 记录这一版是从哪一历史版本恢复到编辑区后继续改写的；
	// 恢复本身不覆盖原版本，只是新版本的来源说明。
	RestoredFrom uint64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// RepracticeSourceDTO 是选段回显，便于页面刷新后仍能显示原句与落点。
type RepracticeSourceDTO struct {
	MessageID string `json:"message_id"`
	StartCP   int    `json:"start_cp"`
	EndCP     int    `json:"end_cp"`
	Quote     string `json:"quote"`
}

// RepracticeFeedbackDTO 是反馈投影。
type RepracticeFeedbackDTO struct {
	Improved   bool   `json:"improved"`
	Change     string `json:"change"`
	Reason     string `json:"reason"`
	NewProblem string `json:"new_problem"`
}

// RepracticeDTO 是读取投影：明确哪些字段可以出网。
type RepracticeDTO struct {
	ID           string                 `json:"id"`
	Status       string                 `json:"status"`
	Attempt      uint64                 `json:"attempt_version"`
	Source       RepracticeSourceDTO    `json:"source"`
	Goal         string                 `json:"goal"`
	Rewrite      string                 `json:"rewrite"`
	Feedback     *RepracticeFeedbackDTO `json:"feedback"`
	ErrorCode    *string                `json:"error_code"`
	RestoredFrom *string                `json:"restored_from"`
	CreatedAt    time.Time              `json:"created_at"`
	UpdatedAt    time.Time              `json:"updated_at"`
}

// RepracticeListDTO 是列表投影。Available=false 表示本环境未开启该能力，
// 客户端据此隐藏整个重练区，而不是把它当成“还没有历史版本”。
type RepracticeListDTO struct {
	Available bool            `json:"available"`
	Items     []RepracticeDTO `json:"items"`
}

func (v RepracticeVersion) DTO() RepracticeDTO {
	out := RepracticeDTO{
		ID:        formatID(v.ID),
		Status:    v.Status,
		Attempt:   v.Attempt,
		Source:    RepracticeSourceDTO{MessageID: formatID(v.Input.MessageID), StartCP: v.Input.Start, EndCP: v.Input.End, Quote: v.Input.Quote},
		Goal:      v.Input.Goal,
		Rewrite:   v.Input.Rewrite,
		CreatedAt: v.CreatedAt.UTC(),
		UpdatedAt: v.UpdatedAt.UTC(),
	}
	if v.Status == repracticeStatusFailed {
		code := v.ErrorCode
		out.ErrorCode = &code
	}
	if v.Status == repracticeStatusCompleted {
		out.Feedback = &RepracticeFeedbackDTO{
			Improved:   v.Feedback.Improved,
			Change:     v.Feedback.Change,
			Reason:     v.Feedback.Reason,
			NewProblem: v.Feedback.NewProblem,
		}
	}
	if v.RestoredFrom != 0 {
		from := formatID(v.RestoredFrom)
		out.RestoredFrom = &from
	}
	return out
}

// RepracticeList 是服务层读取结果；Available 只在能力关闭时为 false。
type RepracticeList struct {
	Available bool
	Items     []RepracticeVersion
}

func (l RepracticeList) DTO() RepracticeListDTO {
	items := make([]RepracticeDTO, 0, len(l.Items))
	for _, version := range l.Items {
		items = append(items, version.DTO())
	}
	return RepracticeListDTO{Available: l.Available, Items: items}
}

// RepracticeGenerator 是反馈生成的业务接口：Fake 与真实适配器共用。
// 只接收已由存储层复核过的选段与改写，生成失败不影响已保存版本。
type RepracticeGenerator interface {
	GenerateRepractice(ctx context.Context, in RepracticeInput) (RepracticeFeedback, error)
}

// ValidateRepracticeInput 按固定顺序校验：请求编号 → 文本编码 → 改写 → 改进目标。
// 选段是否真实存在属于存储层职责（必须对照权威原文），不在这里猜；
// 空选段会在原文复核阶段统一返回 ErrRepracticeSource。
func ValidateRepracticeInput(in RepracticeInput) error {
	if in.RequestID == "" || len(in.RequestID) > RepracticeMaxRequestIDLength || isBlankText(in.RequestID) {
		return ErrRepracticeInput
	}
	if !utf8.ValidString(in.Rewrite) || !utf8.ValidString(in.Goal) || !utf8.ValidString(in.Quote) {
		return ErrRepracticeInput
	}
	if isBlankText(in.Rewrite) || utf8.RuneCountInString(in.Rewrite) > RepracticeMaxCodepoints {
		return ErrRepracticeInput
	}
	if utf8.RuneCountInString(in.Goal) > RepracticeMaxCodepoints {
		return ErrRepracticeInput
	}
	return nil
}

// ValidateRepracticeFeedback 校验生成结果是否达到可保存的最小质量。
//
// 只强制“必须给出理由”：改写是否改善本身就是布尔判断，Change（哪些地方变了）
// 与 NewProblem（可能引入的新问题）允许为空，因为“没有新问题”是合法结论。
// 但任何一段回填都不得超长，避免把整段范文塞进反馈。
func ValidateRepracticeFeedback(feedback RepracticeFeedback) error {
	if !utf8.ValidString(feedback.Change) || !utf8.ValidString(feedback.Reason) || !utf8.ValidString(feedback.NewProblem) {
		return ErrRepracticeInput
	}
	if isBlankText(feedback.Reason) {
		return ErrRepracticeInput
	}
	if utf8.RuneCountInString(feedback.Change) > RepracticeMaxCodepoints ||
		utf8.RuneCountInString(feedback.Reason) > RepracticeMaxCodepoints ||
		utf8.RuneCountInString(feedback.NewProblem) > RepracticeMaxCodepoints {
		return ErrRepracticeInput
	}
	return nil
}

// RepracticeStore 是局部重练的持久化边界。所有方法都必须：
//   - 校验会话归属：不属于当前用户一律 ErrNotFound（对外统一 404）；
//   - 在临界区内复核“本场已完赛”与选段逐字匹配，不接受调用方自报的原文；
//   - 每次提交创建独立版本，绝不写 messages、debates、debate_reviews；
//   - 重试只推进已有失败版本的 attempt_version，不创建新版本。
type RepracticeStore interface {
	// Detail 只读返回会话与会话内正式消息，用于归属与完赛校验。
	Detail(ctx context.Context, owner, debateID uint64) (Detail, error)
	// ListRepractice 按写入顺序返回本场全部版本。
	ListRepractice(ctx context.Context, owner, debateID uint64) ([]RepracticeVersion, error)
	// CreateRepractice 在校验归属/完赛/原文后创建版本；fresh=false 表示同一
	// client_request_id 已存在，调用方应按幂等重放处理。
	CreateRepractice(ctx context.Context, owner, debateID uint64, in RepracticeInput, now time.Time) (RepracticeVersion, bool, error)
	// ClaimRepracticeRetry 只认领 failed 且 attempt 匹配的版本；fresh=false 表示
	// 无需再次生成（已完成/生成中/版本不匹配），调用方应读回当前状态。
	ClaimRepracticeRetry(ctx context.Context, owner, debateID, versionID, attempt uint64, now time.Time) (RepracticeVersion, bool, error)
	// CompleteRepractice 按 attempt CAS 写回反馈；失败返回 ErrAttemptVersionConflict。
	CompleteRepractice(ctx context.Context, owner, debateID, versionID, attempt uint64, feedback RepracticeFeedback) (RepracticeVersion, error)
	// FailRepractice 按 attempt CAS 只保存受控错误码，保留输入不改写。
	FailRepractice(ctx context.Context, owner, debateID, versionID, attempt uint64, code string) (RepracticeVersion, error)
}

// RepracticeService 是局部重练的规则入口，与轮次/总结/复盘的 Service 完全隔离：
// 它只读写独立版本表，不触碰正式消息与会话状态。
type RepracticeService struct {
	store     RepracticeStore
	generator RepracticeGenerator
	// gate 可选：装配后单次生成受与正式发言相同的并发/日额度约束。
	gate *Gate
	now  func() time.Time
}

// NewRepracticeService 构造规则入口；store 与 generator 都必须非 nil 才能生成。
func NewRepracticeService(store RepracticeStore, generator RepracticeGenerator) *RepracticeService {
	return &RepracticeService{store: store, generator: generator, now: func() time.Time { return time.Now().UTC() }}
}

// SetGate 注入与正式发言共用的配额入口；nil 表示只由 HTTP 防刷限流。
func (s *RepracticeService) SetGate(gate *Gate) {
	if s == nil {
		return
	}
	s.gate = gate
}

// List 返回本场全部重练版本，按写入顺序；读取不触发任何生成。
func (s *RepracticeService) List(ctx context.Context, owner, debateID uint64) ([]RepracticeVersion, error) {
	if err := s.checkOwner(ctx, owner, debateID); err != nil {
		return nil, err
	}
	versions, err := s.store.ListRepractice(ctx, owner, debateID)
	if err != nil {
		return nil, err
	}
	if versions == nil {
		versions = []RepracticeVersion{}
	}
	return versions, nil
}

// Submit 校验选段与输入后创建独立版本，并在保存成功后调用一次反馈生成。
//
// 关键顺序：先保存版本再调模型。保存失败时原样返回错误且绝不调用模型；
// 生成失败时版本落为 failed 并保留改写，返回值不带 error（供界面直接展示
// “失败版本 + 原改写”并提供显式重试）。
func (s *RepracticeService) Submit(ctx context.Context, owner, debateID uint64, in RepracticeInput) (RepracticeVersion, error) {
	if err := ValidateRepracticeInput(in); err != nil {
		return RepracticeVersion{}, err
	}
	if s == nil || s.store == nil {
		return RepracticeVersion{}, dependency(nil, false)
	}
	detail, err := s.store.Detail(ctx, owner, debateID)
	if err != nil {
		return RepracticeVersion{}, err
	}
	if err := repracticeSessionEligible(owner, debateID, detail); err != nil {
		return RepracticeVersion{}, err
	}
	if _, _, ok := repracticeSourceMatches(detail, in); !ok {
		return RepracticeVersion{}, ErrRepracticeSource
	}
	version, fresh, err := s.store.CreateRepractice(ctx, owner, debateID, in, s.now())
	if err != nil {
		return RepracticeVersion{}, err
	}
	if !fresh {
		// 同一请求编号的幂等重放：不再生成，直接返回已保存版本。
		return version, nil
	}
	return s.run(ctx, owner, debateID, detail.Debate, version)
}

// Retry 显式重试一个失败版本：只推进该版本的 attempt，不创建新版本。
func (s *RepracticeService) Retry(ctx context.Context, owner, debateID, versionID, attempt uint64) (RepracticeVersion, error) {
	if s == nil || s.store == nil {
		return RepracticeVersion{}, dependency(nil, false)
	}
	if versionID == 0 || attempt == 0 {
		return RepracticeVersion{}, ErrRepracticeVersion
	}
	detail, err := s.store.Detail(ctx, owner, debateID)
	if err != nil {
		return RepracticeVersion{}, err
	}
	if err := repracticeSessionEligible(owner, debateID, detail); err != nil {
		return RepracticeVersion{}, err
	}
	version, fresh, err := s.store.ClaimRepracticeRetry(ctx, owner, debateID, versionID, attempt, s.now())
	if err != nil {
		return RepracticeVersion{}, err
	}
	if !fresh {
		return version, nil
	}
	if _, _, ok := repracticeSourceMatches(detail, version.Input); !ok {
		// 正式原文不可变；走到这里说明数据不一致，按受控失败收尾并可再次重试。
		failed, failErr := s.fail(ctx, owner, debateID, version, "invalid_request")
		if failErr != nil {
			return RepracticeVersion{}, failErr
		}
		return failed, ErrRepracticeSource
	}
	return s.run(ctx, owner, debateID, detail.Debate, version)
}

// checkOwner 用既有只读详情入口校验归属与会话资格；错误原样向上传递
// （越权是 ErrNotFound，不能改写成选段错误，否则会泄露会话是否存在）。
func (s *RepracticeService) checkOwner(ctx context.Context, owner, debateID uint64) error {
	if s == nil || s.store == nil {
		return dependency(nil, false)
	}
	detail, err := s.store.Detail(ctx, owner, debateID)
	if err != nil {
		return err
	}
	return repracticeSessionEligible(owner, debateID, detail)
}

// repracticeSessionEligible 复核归属与完赛状态；未完成结辩不开放局部重练。
func repracticeSessionEligible(owner, debateID uint64, detail Detail) error {
	if owner == 0 || debateID == 0 || detail.Debate.ID != debateID || detail.Debate.UserID != owner {
		return ErrNotFound
	}
	if detail.Debate.Status != "completed" {
		return ErrRoundConflict
	}
	return nil
}

// repracticeSourceMatches 在本场已完赛轮次的正式 user 消息里逐字复核选段。
// 返回命中的消息与轮号；任何一项不满足都视为选段不成立。
func repracticeSourceMatches(detail Detail, in RepracticeInput) (Message, int, bool) {
	if in.MessageID == 0 || in.Start < 0 || in.End <= in.Start {
		return Message{}, 0, false
	}
	if !utf8.ValidString(in.Quote) || isBlankText(in.Quote) {
		return Message{}, 0, false
	}
	for _, turn := range detail.Turns {
		if turn.Status != "completed" {
			continue
		}
		for _, message := range turn.Messages {
			if message.ID != in.MessageID || message.Role != "user" {
				continue
			}
			if VerifiedQuote(message.Content, in.Start, in.End, in.Quote) {
				return message, turn.RoundNo, true
			}
			// 同一编号只可能命中一条消息；坐标或原文不符即选段不成立。
			return Message{}, 0, false
		}
	}
	return Message{}, 0, false
}

// run 在版本已保存之后调用一次模型，并把结果按 attempt CAS 写回。
// run 在版本已保存之后调用一次模型，并把结果按 attempt CAS 写回。
//
// 生成只接收“用户选段 + 改写 + 改进目标”，题面与立场仅用于让模型判断是否
// 回应了辩题；正式消息、会话状态与复盘从不参与写入。
func (s *RepracticeService) run(ctx context.Context, owner, debateID uint64, session Debate, version RepracticeVersion) (RepracticeVersion, error) {
	if s.generator == nil {
		return s.fail(ctx, owner, debateID, version, "model_unavailable")
	}
	var permit *Permit
	if s.gate != nil {
		acquired, err := s.gate.Acquire(owner)
		if err != nil {
			// 配额或并发不满足：落受控失败码，保留改写供用户稍后显式重试。
			return s.fail(ctx, owner, debateID, version, repracticeFailureCode(err))
		}
		permit = acquired
	}
	// 认领成功后的模型调用无论成败都算一次尝试，避免低估费用。
	defer func() { permit.Finish(true) }()
	feedback, err := s.generate(ctx, session, version.Input)
	if err != nil {
		code, _, _, _ := classifyFailure(ctx, err)
		return s.fail(ctx, owner, debateID, version, codeForHTTP(code))
	}
	if err := ValidateRepracticeFeedback(feedback); err != nil {
		return s.fail(ctx, owner, debateID, version, "model_error")
	}
	saved, saveErr := s.store.CompleteRepractice(ctx, owner, debateID, version.ID, version.Attempt, feedback)
	if saveErr != nil {
		// CAS 只允许收尾仍在生成的尝试，成功写入的反馈不会被覆盖。
		failed, failErr := s.fail(ctx, owner, debateID, version, "dependency_unavailable")
		if failErr != nil {
			return RepracticeVersion{}, saveErr
		}
		return failed, nil
	}
	return saved, nil
}

// generate 在一次固定期限的调用内生成反馈；panic 与超时都转成受控错误。
// 生成器如具备场次上下文能力（生产适配器），把辩题快照与立场作为只读上下文传入；
// 最小 Fake 只需实现无上下文的 RepracticeGenerator。
func (s *RepracticeService) generate(ctx context.Context, session Debate, in RepracticeInput) (feedback RepracticeFeedback, err error) {
	callCtx, cancel := context.WithTimeout(ctx, RepracticeTimeout)
	defer cancel()
	defer func() {
		if recover() != nil {
			feedback = RepracticeFeedback{}
			err = &ai.Error{Code: ai.CodeModelError, Message: "重练反馈生成失败"}
		}
	}()
	if contextual, ok := s.generator.(RepracticeContextGenerator); ok {
		feedback, err = contextual.GenerateRepracticeWithContext(callCtx, RepracticeContext{
			Topic:      ai.Topic{Title: session.TopicTitleSnapshot, Description: session.TopicDescriptionSnapshot},
			UserStance: ai.Stance(session.UserStance),
		}, in)
	} else {
		feedback, err = s.generator.GenerateRepractice(callCtx, in)
	}
	if callCtx.Err() != nil {
		code := ai.CodeModelCanceled
		if errors.Is(callCtx.Err(), context.DeadlineExceeded) {
			code = ai.CodeModelTimeout
		}
		err = (&ai.Error{Code: code, Message: "重练反馈生成已中断"}).WithCause(errors.Join(callCtx.Err(), err))
	}
	return feedback, err
}

// fail 用独立短超时落受控失败码；失败状态仍保留原改写与选段。
func (s *RepracticeService) fail(ctx context.Context, owner, debateID uint64, version RepracticeVersion, code string) (RepracticeVersion, error) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	saved, err := s.store.FailRepractice(cleanup, owner, debateID, version.ID, version.Attempt, code)
	if err != nil {
		return RepracticeVersion{}, err
	}
	return saved, nil
}

// repracticeFailureCode 把配额/并发受控失败映射成可落库的错误码。
func repracticeFailureCode(err error) string {
	var failure *Failure
	if errors.As(err, &failure) && failure.Code != "" {
		return failure.Code
	}
	return "dependency_unavailable"
}

// Repractice 能力挂载：显式开关 + 所需接口齐备时才创建子服务。

// EnableRepractice 在本服务上打开局部重练；存储必须实现 RepracticeStore，
// 生成器需实现 ai.RepracticeGenerator（生产适配器与 Fake 均满足）。
func (s *Service) EnableRepractice() error {
	if s == nil || s.store == nil || s.generator == nil {
		return errors.New("局部重练需要存储与生成器")
	}
	store, ok := s.store.(RepracticeStore)
	if !ok {
		return errors.New("当前存储不支持局部重练")
	}
	generator, ok := s.generator.(ai.RepracticeGenerator)
	if !ok {
		return errors.New("当前生成器不支持局部重练")
	}
	service := NewRepracticeService(store, aiRepracticeAdapter{generator: generator})
	service.SetGate(s.gate)
	s.repracticeMu.Lock()
	s.repractice = service
	s.repracticeMu.Unlock()
	return nil
}

// DisableRepractice 关闭局部重练；关闭后读取返回 available=false，写入口 400。
func (s *Service) DisableRepractice() {
	if s == nil {
		return
	}
	s.repracticeMu.Lock()
	s.repractice = nil
	s.repracticeMu.Unlock()
}

// repracticeService 返回已装配的子服务；未装配时为 nil。
func (s *Service) repracticeService() *RepracticeService {
	if s == nil {
		return nil
	}
	s.repracticeMu.Lock()
	defer s.repracticeMu.Unlock()
	return s.repractice
}

// RepracticeAvailable 报告当前环境是否可提供局部重练。
func (s *Service) RepracticeAvailable() bool { return s.repracticeService() != nil }

// ListRepractice 读取本场重练历史；关闭能力时不访问新表，只做归属校验。
func (s *Service) ListRepractice(ctx context.Context, uid, debateID uint64) (RepracticeList, error) {
	if err := s.waitStartupScan(ctx); err != nil {
		return RepracticeList{}, dependency(err, false)
	}
	service := s.repracticeService()
	if service == nil {
		// 关闭时完全不读 debate_repractice_versions：旧部署可能尚未执行迁移。
		if _, err := s.store.Detail(ctx, uid, debateID); err != nil {
			return RepracticeList{}, err
		}
		return RepracticeList{Available: false, Items: []RepracticeVersion{}}, nil
	}
	versions, err := service.List(ctx, uid, debateID)
	if err != nil {
		return RepracticeList{}, err
	}
	return RepracticeList{Available: true, Items: versions}, nil
}

// SubmitRepractice 是显式提交入口；未启用时拒绝且不产生任何版本。
func (s *Service) SubmitRepractice(ctx context.Context, uid, debateID uint64, in RepracticeInput) (RepracticeVersion, error) {
	service := s.repracticeService()
	if service == nil {
		return RepracticeVersion{}, ErrInvalidRequest
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return RepracticeVersion{}, dependency(err, false)
	}
	return service.Submit(ctx, uid, debateID, in)
}

// RetryRepractice 显式重试失败版本，必须带当前 attempt_version。
func (s *Service) RetryRepractice(ctx context.Context, uid, debateID, versionID, attempt uint64) (RepracticeVersion, error) {
	service := s.repracticeService()
	if service == nil {
		return RepracticeVersion{}, ErrInvalidRequest
	}
	if err := s.waitStartupScan(ctx); err != nil {
		return RepracticeVersion{}, dependency(err, false)
	}
	return service.Retry(ctx, uid, debateID, versionID, attempt)
}
