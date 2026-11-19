package debate

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
)

// 批注的服务端边界：
//   - 读取只查已有批次，无模型调用、不写库；
//   - 首次分析与手动重试共用同一认领内核与同一个非阻塞分析槽；
//   - 分析永远发生在正式轮次事务之外，失败只写受控错误码，
//     不改变轮次、会话完成或总结状态。

const (
	// AnnotationAnalysisTimeout 限制单次批注分析的固定期限。
	AnnotationAnalysisTimeout = 10 * time.Second
	// AnnotationSlotUnavailable 表示分析槽正忙，本次不排队（调用方保留资格，稍后手动补做）。
	AnnotationSlotUnavailable = "annotation_slot_unavailable"
)

// 批注批次状态。not_started 只出现在读取投影里，不落库。
const (
	AnnotationNotStarted = "not_started"
	AnnotationGenerating = "generating"
	AnnotationCompleted  = "completed"
	AnnotationFailed     = "failed"
)

// AnnotationItemDTO 是读取接口中的单条批注。
// 锚点中 Resolved=false 时前端只显示“原文待核对”，不按坐标高亮。
type AnnotationItemDTO struct {
	Category    string `json:"category"`
	Label       string `json:"label"`
	Reason      string `json:"reason"`
	Improvement string `json:"improvement"`
	Anchor      Anchor `json:"anchor"`
	TurnNo      int    `json:"turn_no"`
	Role        string `json:"role"`
}

// AnnotationBatchDTO 是一个触发轮对应的批注批次。
type AnnotationBatchDTO struct {
	TurnID         string              `json:"turn_id"`
	RoundNo        int                 `json:"round_no"`
	Status         string              `json:"status"`
	AttemptVersion uint64              `json:"attempt_version"`
	Items          []AnnotationItemDTO `json:"items"`
	ErrorCode      *string             `json:"error_code,omitempty"`
}

// AnnotationListDTO 是按触发轮排序的批次列表；没有合格轮次时 items 为空数组。
type AnnotationListDTO struct {
	Items []AnnotationBatchDTO `json:"items"`
}

// AnnotationRetryInput 是手动补做的唯一允许字段。
type AnnotationRetryInput struct {
	ExpectedAttemptVersion uint64 `json:"expected_attempt_version"`
}

// ValidateAnnotationRetryInput 只接受“未认领（0）”或某一个已落库的正版本。
//
// “手动重试最多一次”不是输入格式约束，而是认领时的业务约束：
// 首次认领为 v1，唯一一次补做为 v2，此后失败批次不再可补做。
// 版本号先做格式校验，是否允许补做由 Repository 的当前状态决定。
func ValidateAnnotationRetryInput(in AnnotationRetryInput) (AnnotationRetryInput, error) {
	if in.ExpectedAttemptVersion == ^uint64(0) {
		return in, ErrInvalidRequest
	}
	return in, nil
}

// projectAnnotation 把存储行投影成 DTO。损坏的已保存内容只做读取期投影，
// 不写库、不重新生成。
func projectAnnotation(batch AnnotationBatch) AnnotationBatchDTO {
	turn := batch.Turn
	dto := AnnotationBatchDTO{TurnID: formatID(turn.ID), RoundNo: turn.RoundNo, Items: []AnnotationItemDTO{}}
	if batch.Annotation == nil {
		dto.Status = AnnotationNotStarted
		return dto
	}
	dto.AttemptVersion = batch.Annotation.AttemptVersion
	dto.Status = batch.Annotation.Status
	switch batch.Annotation.Status {
	case AnnotationFailed:
		dto.ErrorCode = batch.Annotation.ErrorCode
	case AnnotationCompleted:
		items, ok := decodeStoredAnnotations(batch.Annotation.ContentJSON)
		if !ok {
			// 存储内容不可读时如实标记为不可读结果，不假装 0 条，也不在读取期改写数据库。
			code := "content_invalid"
			dto.Status = AnnotationFailed
			dto.ErrorCode = &code
			return dto
		}
		dto.Items = items
	}
	return dto
}

// decodeStoredAnnotations 解析已保存的批注 JSON；空数组是合法结果。
func decodeStoredAnnotations(raw *string) ([]AnnotationItemDTO, bool) {
	if raw == nil {
		return nil, false
	}
	var payload struct {
		Items []AnnotationItem `json:"items"`
	}
	if err := json.Unmarshal([]byte(*raw), &payload); err != nil {
		return nil, false
	}
	items := make([]AnnotationItemDTO, 0, len(payload.Items))
	for _, item := range payload.Items {
		items = append(items, AnnotationItemDTO{
			Category:    item.Category,
			Label:       item.Label,
			Reason:      item.Reason,
			Improvement: item.Improvement,
			Anchor:      item.Anchor,
			TurnNo:      item.TurnNo,
			Role:        item.Role,
		})
	}
	return items, true
}

// GetAnnotations 只读返回当前会话的合格轮次批次。旧会话没有合格轮次，返回空列表。
func (s *Service) GetAnnotations(ctx context.Context, uid, id uint64) (AnnotationListDTO, error) {
	if err := s.waitStartupScan(ctx); err != nil {
		return AnnotationListDTO{}, dependency(err, false)
	}
	batches, err := s.store.ListAnnotations(ctx, uid, id)
	if err != nil {
		return AnnotationListDTO{}, err
	}
	out := AnnotationListDTO{Items: make([]AnnotationBatchDTO, 0, len(batches))}
	var needsLocation bool
	for _, batch := range batches {
		projected := projectAnnotation(batch)
		for _, item := range projected.Items {
			needsLocation = needsLocation || !item.Anchor.Resolved
		}
		out.Items = append(out.Items, projected)
	}
	if needsLocation {
		detail, err := s.store.Detail(ctx, uid, id)
		if err != nil {
			return AnnotationListDTO{}, err
		}
		locateStoredAnnotations(out.Items, detail.Turns)
	}
	return out, nil
}

// 历史批注可按已提交且仍属于该场的正式消息重定位。只修改本次响应。
func locateStoredAnnotations(batches []AnnotationBatchDTO, turns []Turn) {
	sources := make(map[string]AnnotationSource)
	for _, turn := range turns {
		if turn.Status != "completed" {
			continue
		}
		for _, message := range turn.Messages {
			sources[formatID(message.ID)] = AnnotationSource{
				MessageID: message.ID, RoundNo: turn.RoundNo, Content: message.Content,
			}
		}
	}
	for _, batch := range batches {
		for i := range batch.Items {
			item := &batch.Items[i]
			source, ok := sources[item.Anchor.MessageID]
			if item.Anchor.Resolved || !ok || source.RoundNo > batch.RoundNo {
				continue
			}
			anchor, err := ResolveAnchor(source.MessageID, source.Content, item.Anchor.Quote, nil)
			if err == nil && anchor.Resolved {
				item.Anchor = anchor
			}
		}
	}
}

// RetryAnnotation 是唯一的手动补做入口：只补未认领或已失败的那一批。
func (s *Service) RetryAnnotation(ctx context.Context, uid, debateID, turnID uint64, in AnnotationRetryInput) (AnnotationBatchDTO, int, error) {
	in, err := ValidateAnnotationRetryInput(in)
	if err != nil {
		return AnnotationBatchDTO{}, 0, err
	}
	if !s.annotationsEnabled() || s.demoAnnotationsEnabled() {
		// 未启用切片时不提供生成能力，也不写任何批次行。
		return AnnotationBatchDTO{}, 0, ErrInvalidRequest
	}
	return s.runAnnotation(ctx, uid, debateID, turnID, in.ExpectedAttemptVersion)
}

// runAnnotation 统一处理首次分析与手动重试。
//
// 占满分析槽时返回 202 与当前状态（未启动则保持 not_started），调用方稍后手动补做；
// 不排队、不重试、不占用正式发言与总结的 Gate。
func (s *Service) runAnnotation(ctx context.Context, uid, debateID, turnID, expectedVersion uint64) (AnnotationBatchDTO, int, error) {
	if !s.acquireAnnotationSlot() {
		batch, err := s.annotationBatchOf(ctx, uid, debateID, turnID)
		if err != nil {
			return AnnotationBatchDTO{}, 0, err
		}
		return batch, 202, nil
	}
	defer s.releaseAnnotationSlot()
	return s.runAnnotationWithSlot(ctx, uid, debateID, turnID, expectedVersion)
}

// runAnnotationWithSlot 假定调用方已持有分析槽，负责认领、生成与写回。
// 认领与生成都在锁外执行，认领事务只覆盖状态与版本的写入。
func (s *Service) runAnnotationWithSlot(ctx context.Context, uid, debateID, turnID, expectedVersion uint64) (AnnotationBatchDTO, int, error) {
	claim, fresh, err := s.store.ClaimAnnotation(ctx, uid, debateID, turnID, expectedVersion, func() error { return nil })
	if err != nil {
		return AnnotationBatchDTO{}, 0, err
	}
	batch := projectAnnotation(AnnotationBatch{Turn: claim.Turn, Annotation: &claim.Annotation})
	if !fresh {
		switch claim.Annotation.Status {
		case AnnotationCompleted:
			// 成功批次始终保留，重放与查询都直接返回已存结果。
			return batch, 200, nil
		case AnnotationGenerating:
			return batch, 202, nil
		default:
			// 失败且版本不符：只查询，不自动换版本重试。
			return AnnotationBatchDTO{}, 0, &Failure{Code: "attempt_version_conflict", Status: 409,
				Cause: ErrAttemptVersionConflict, Details: failedAnnotationDetails(claim.Debate, claim.Turn, claim.Annotation)}
		}
	}
	return s.generateAnnotation(ctx, uid, claim, 201)
}

// annotationBatchOf 读取单轮当前批次投影，用于槽位忙时返回既有状态。
// 轮次不属于合格批次时按轮次冲突处理，不泄露资源是否存在之外的任何信息。
func (s *Service) annotationBatchOf(ctx context.Context, uid, debateID, turnID uint64) (AnnotationBatchDTO, error) {
	batches, err := s.store.ListAnnotations(ctx, uid, debateID)
	if err != nil {
		return AnnotationBatchDTO{}, err
	}
	for _, batch := range batches {
		if batch.Turn.ID == turnID {
			return projectAnnotation(batch), nil
		}
	}
	return AnnotationBatchDTO{}, ErrRoundConflict
}

func failedAnnotationDetails(d Debate, t Turn, a TurnAnnotation) *FailureDetails {
	return &FailureDetails{TurnID: formatID(t.ID), Status: AnnotationFailed, AttemptVersion: a.AttemptVersion,
		QueryURL: "/api/v1/debates/" + formatID(d.ID) + "/annotations"}
}

// 批注日志的固定原因码，只描述失败或部分成功发生在哪条校验路径，不含任何内容。
const (
	annotationReasonOK                    = "ok"
	annotationReasonPartialItemsRejected  = "partial_items_rejected"
	annotationReasonAllCandidatesRejected = "all_candidates_rejected"
	annotationReasonGenerationFailed      = "generation_failed"
	annotationReasonMessageIDNotAllowed   = "message_id_not_allowed"
	annotationReasonAnchorNotLocated      = "anchor_not_located"
	annotationReasonDuplicateItem         = "duplicate_item"
	annotationReasonTooManyItems          = "too_many_items"
	annotationReasonSerializeFailed       = "serialize_failed"
	annotationReasonStoreFailed           = "store_failed"
)

// annotationLogFields 是日志中的固定计数字段：只有数字与 Prompt 版本，
// 不含原文、模型回复、模型给出的编号或理由。
type annotationLogFields struct {
	promptVersion         string
	candidates            int
	accepted              int
	unknownFragment       int
	invalidFields         int
	duplicate             int
	unselectableFragments int
}

// annotationLogFieldsOf 从生成结果提取固定计数，供成功与部分成功路径使用。
func annotationLogFieldsOf(result ai.AnnotationResult) annotationLogFields {
	return annotationLogFields{
		promptVersion:         ai.AnnotationPromptVersion,
		candidates:            result.Candidates,
		unknownFragment:       result.Rejected.UnknownFragment,
		invalidFields:         result.Rejected.InvalidFields,
		duplicate:             result.Rejected.Duplicate,
		unselectableFragments: result.UnselectableFragments,
	}
}

// annotationRejectError 在保留原有 errors.Is 语义的同时，携带稳定的拒绝原因码。
type annotationRejectError struct {
	reason string
	cause  error
}

func (e *annotationRejectError) Error() string { return e.reason }

func (e *annotationRejectError) Unwrap() error { return e.cause }

// annotationReasonOf 提取拒绝原因码；非本类型错误一律归为 generation_failed。
func annotationReasonOf(err error) string {
	var reject *annotationRejectError
	if errors.As(err, &reject) {
		return reject.reason
	}
	return annotationReasonGenerationFailed
}

// generateAnnotation 执行一次分析并写回结果。锁在模型调用之外，结果整体原子保存。
func (s *Service) generateAnnotation(ctx context.Context, uid uint64, claim AnnotationClaim, successStatus int) (AnnotationBatchDTO, int, error) {
	d, turn := claim.Debate, claim.Turn
	started := time.Now()
	reason := annotationReasonGenerationFailed
	fields := annotationLogFields{promptVersion: ai.AnnotationPromptVersion}
	messages := make([]ai.AnnotationMessage, 0, len(claim.Sources))
	for _, source := range claim.Sources {
		messages = append(messages, ai.AnnotationMessage{
			MessageID: formatID(source.MessageID),
			TurnNo:    source.RoundNo,
			Role:      ai.Role(source.Role),
			Content:   source.Content,
			Trigger:   source.RoundNo == turn.RoundNo,
		})
	}
	result, genErr := s.generateAnnotations(ctx, ai.AnnotationRequest{
		UserID:         uid,
		Mode:           ai.Mode(d.Mode),
		AIStyle:        ai.AIStyle(d.AIStyle),
		Topic:          ai.Topic{Title: d.TopicTitleSnapshot, Description: d.TopicDescriptionSnapshot},
		UserStance:     ai.Stance(d.UserStance),
		TriggerRoundNo: turn.RoundNo,
		Messages:       messages,
		Custom:         d.Custom(),
	})
	if genErr == nil {
		fields = annotationLogFieldsOf(result)
		items, validErr := toAnnotationItems(result, claim.Sources)
		if validErr != nil {
			reason = annotationReasonOf(validErr)
			// 不信任生成器输出，包括 Fake 与真实适配器：落库前再按权威原文复核一次。
			genErr = (&ai.Error{Code: ai.CodeModelError, Message: "批注结构无效", Usage: result.Usage, Metadata: result.Metadata}).WithCause(validErr)
		} else {
			fields.accepted = len(items)
			// 部分候选被弃置仍是成功批次：保存有效条目，并记录固定分类计数。
			reason = annotationReasonOK
			if result.Rejected.Total() > 0 {
				reason = annotationReasonPartialItemsRejected
			}
			// 落库内容一律是服务端复核并重新定位后的规范化 JSON（空数组同样整体保存）。
			canonical, marshalErr := json.Marshal(map[string]any{"items": items})
			if marshalErr != nil || len(canonical) > ai.MaxAnnotationJSONBytes {
				reason = annotationReasonSerializeFailed
				genErr = &ai.Error{Code: ai.CodeModelError, Message: "批注序列化失败或超过大小上限", Usage: result.Usage, Metadata: result.Metadata}
			} else {
				result.ContentJSON = string(canonical)
			}
		}
	} else {
		fields = annotationLogFieldsOf(result)
		if errors.Is(genErr, ai.ErrAnnotationAllRejected) {
			// 有候选但全部无法绑定权威片段：整批失败，与模型显式返回空数组严格区分。
			reason = annotationReasonAllCandidatesRejected
		}
	}
	if ctx.Err() != nil {
		genErr = annotationContextError(genErr, result, ctx.Err())
	}
	if genErr != nil {
		code, status, usage, meta := classifyFailure(ctx, genErr)
		s.saveAnnotationFailure(ctx, uid, d, claim.Annotation, code, usage, meta)
		s.logAnnotation(ctx, reason, string(code), turn.ID, time.Since(started), fields)
		return AnnotationBatchDTO{}, 0, &Failure{Code: codeForHTTP(code), Status: status, Cause: genErr,
			Details: failedAnnotationDetails(d, turn, claim.Annotation)}
	}
	saved, saveErr := s.store.CompleteAnnotation(ctx, uid, d, claim.Annotation, result)
	if saveErr != nil {
		s.saveAnnotationFailure(ctx, uid, d, claim.Annotation, "dependency_unavailable", result.Usage, result.Metadata)
		s.logAnnotation(ctx, annotationReasonStoreFailed, "dependency_unavailable", turn.ID, time.Since(started), fields)
		return AnnotationBatchDTO{}, 0, dependency(saveErr, false)
	}
	s.logAnnotation(ctx, reason, "ok", turn.ID, time.Since(started), fields)
	return projectAnnotation(AnnotationBatch{Turn: turn, Annotation: &saved}), successStatus, nil
}

// toAnnotationItems 把生成结果转换成业务条目，并按本次权威原文复核锚点。
//
// annotation-v2 的片段表由服务端生成，但这里仍不信任生成器输出（含 Fake）：
// 消息必须在本次白名单内、区间必须齐全、引用必须与权威原文逐字相等。
// 任一条件不满足即整批失败，绝不改挂到其他消息或退回未定位。
func toAnnotationItems(result ai.AnnotationResult, sources []AnnotationSource) ([]AnnotationItem, error) {
	allowed := make(map[string]AnnotationSource, len(sources))
	for _, source := range sources {
		allowed[formatID(source.MessageID)] = source
	}
	out := make([]AnnotationItem, 0, len(result.Items))
	seen := make(map[string]bool, len(result.Items))
	for _, item := range result.Items {
		source, ok := allowed[item.Anchor.MessageID]
		if !ok {
			return nil, &annotationRejectError{reason: annotationReasonMessageIDNotAllowed, cause: ErrAnchorNotLocated}
		}
		start, end := item.Anchor.StartCP, item.Anchor.EndCP
		if start == nil || end == nil || !VerifiedQuote(source.Content, *start, *end, item.Anchor.Quote) {
			return nil, &annotationRejectError{reason: annotationReasonAnchorNotLocated, cause: ErrAnchorNotLocated}
		}
		candidate := AnnotationItem{
			Category:    item.Category,
			Label:       item.Label,
			Reason:      item.Reason,
			Improvement: item.Improvement,
			Anchor:      resolvedAnchor(Anchor{MessageID: formatID(source.MessageID), Quote: item.Anchor.Quote}, *start, *end),
			TurnNo:      source.RoundNo,
			Role:        source.Role,
		}
		// 完全重复指“同一消息、同一坐标、同一反馈”：同一原句在不同位置的两次
		// 合法批注各自独立保存，不能只按 message_id + quote 判重。
		key := candidate.Category + "\x00" + candidate.Label + "\x00" + candidate.Reason + "\x00" +
			candidate.Improvement + "\x00" + candidate.Anchor.MessageID + "\x00" +
			strconv.Itoa(*start) + "\x00" + strconv.Itoa(*end) + "\x00" + candidate.Anchor.Quote
		if seen[key] {
			return nil, &annotationRejectError{reason: annotationReasonDuplicateItem, cause: ErrInvalidRequest}
		}
		seen[key] = true
		out = append(out, candidate)
	}
	if len(out) > MaxAnnotationsPerBatch {
		return nil, &annotationRejectError{reason: annotationReasonTooManyItems, cause: ErrInvalidRequest}
	}
	return out, nil
}

func annotationContextError(err error, result ai.AnnotationResult, reason error) error {
	usage, meta := result.Usage, result.Metadata
	var original *ai.Error
	if errors.As(err, &original) {
		usage, meta = original.Usage, original.Metadata
	}
	code := ai.CodeModelCanceled
	if errors.Is(reason, context.DeadlineExceeded) {
		code = ai.CodeModelTimeout
	}
	return (&ai.Error{Code: code, Message: "批注请求已中断", Usage: usage, Metadata: meta}).WithCause(errors.Join(reason, err))
}

// saveAnnotationFailure 在取消后仍尽力落库，只保存受控错误码。
func (s *Service) saveAnnotationFailure(ctx context.Context, uid uint64, d Debate, a TurnAnnotation, code string, usage ai.Usage, meta ai.Metadata) {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, _ = s.store.FailAnnotation(cleanup, uid, d, a, code, usage, meta)
}

// generateAnnotations 在一次固定期限的调用内执行分析；panic 与超时都转成受控错误。
func (s *Service) generateAnnotations(ctx context.Context, req ai.AnnotationRequest) (result ai.AnnotationResult, err error) {
	callCtx, cancel := context.WithTimeout(ctx, s.annotationTimeout())
	defer cancel()
	defer func() {
		if recover() != nil {
			result = ai.AnnotationResult{}
			err = &ai.Error{Code: ai.CodeModelError, Message: "批注生成失败"}
		}
	}()
	generator, ok := s.generator.(ai.AnnotationGenerator)
	if !ok {
		return result, &ai.Error{Code: ai.CodeModelUnavailable, Message: "当前生成器不支持批注"}
	}
	result, err = generator.GenerateAnnotations(callCtx, req)
	if callCtx.Err() != nil {
		err = annotationContextError(err, result, callCtx.Err())
	}
	return
}

// thenAnnotate 在正式轮次提交成功后安排一次非阻塞批注分析。
//
// 约束：不继承浏览器取消（用进程 Context 加固定期限）；忙时直接跳过并保留资格；
// 进程退出与重启都不补跑，只有下一次已授权的手动认领才会恢复。
func (s *Service) thenAnnotate(uid uint64, debate Debate, turn Turn) {
	if !s.annotationsEnabled() {
		return
	}
	if !turn.AnnotationEligible || turn.Status != "completed" {
		return
	}
	if !s.acquireAnnotationSlot() {
		// 槽位忙：不排队、不重放，资格保留给手动补做。
		return
	}
	debateID := debate.ID
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		defer s.releaseAnnotationSlot()
		defer func() { _ = recover() }()
		ctx, cancel := context.WithTimeout(context.Background(), s.annotationTimeout()+5*time.Second)
		defer cancel()
		// 后台任务自己同步跑完认领、分析与写回；调用方在提交后立刻拿到响应，
		// 因此批注永远不会延长正式发言的等待时间。
		_, _, _ = s.runAnnotationWithSlot(ctx, uid, debateID, turn.ID, 0)
	}()
}

func (s *Service) demoAnnotationsEnabled() bool {
	trial, ok := s.generator.(interface{ DemoAnnotationsEnabled() bool })
	return ok && trial.DemoAnnotationsEnabled()
}

func (s *Service) annotationTimeout() time.Duration {
	if s.demoAnnotationsEnabled() {
		return ai.DefaultModelTimeout
	}
	return AnnotationAnalysisTimeout
}

// waitBackground 供测试等待后台分析结束，避免断言早于写回。
func (s *Service) waitBackground() { s.background.Wait() }

// annotationsEnabled 同时要求显式开关与具备批注能力的生成器。
//
// 读路径与写路径共用同一把 annotationMu：服务启动后开关不再改变，
// 加锁是为了让“读取开关”和“装配期写入”在内存序上一致，
// 也避免以后有人改成运行期切换时留下无锁读。
func (s *Service) annotationsEnabled() bool {
	if s == nil || s.generator == nil {
		return false
	}
	s.annotationMu.Lock()
	enabled := s.annotationEnabled
	s.annotationMu.Unlock()
	if !enabled {
		return false
	}
	_, ok := s.generator.(ai.AnnotationGenerator)
	return ok
}

// EnableAnnotations 由启动装配在满足“显式本地测试配置 + Fake”时调用。
func (s *Service) EnableAnnotations(enabled bool) {
	s.annotationMu.Lock()
	defer s.annotationMu.Unlock()
	s.annotationEnabled = enabled
}

func (s *Service) acquireAnnotationSlot() bool {
	s.annotationSlot.Lock()
	if s.annotationBusy {
		s.annotationSlot.Unlock()
		return false
	}
	s.annotationBusy = true
	s.annotationSlot.Unlock()
	return true
}

func (s *Service) releaseAnnotationSlot() {
	s.annotationSlot.Lock()
	s.annotationBusy = false
	s.annotationSlot.Unlock()
}

// 批注路由：读取只需 Session；手动补做额外要求 Origin/CSRF 与防刷前置。
func (h *Handler) registerAnnotationRoutes(group *gin.RouterGroup, a *auth.Handler) {
	group.GET("/debates/:id/annotations", a.RequireAuth(), h.getAnnotations)
	group.POST("/debates/:id/turns/:turn_id/annotations/retry",
		a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.retryAnnotation)
}

func (h *Handler) getAnnotations(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, err := h.service.GetAnnotations(c.Request.Context(), current.User.ID, id)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, 200, data)
}

func (h *Handler) retryAnnotation(c *gin.Context) {
	id, err := ParseID(c.Param("id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	turnID, err := ParseID(c.Param("turn_id"))
	if err != nil {
		serviceError(c, err)
		return
	}
	var in AnnotationRetryInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, status, err := h.service.RetryAnnotation(c.Request.Context(), current.User.ID, id, turnID, in)
	if err != nil {
		serviceError(c, err)
		return
	}
	writeData(c, status, data)
}
