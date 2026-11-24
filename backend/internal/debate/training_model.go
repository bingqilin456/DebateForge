package debate

import (
	"errors"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/google/uuid"
)

// 产品 V2 训练赛（V2-02）的服务端权威阶段进度。
//
// 与 V1 的关键差异（PRD V2 §3.2 / 架构 V2 目标状态模型）：
//   - V1 的「(赛制, 轮号) → 环节」推导以「一轮 = 用户一条 + AI 一条」为前提；
//     训练赛存在 AI 先发言与跨阶段连续同方，不能再用轮号反推阶段。
//   - 因此训练赛把「阶段 + 阶段内已完成步数」作为唯一权威进度，存在 debates 行上
//     （training_stage / training_stage_step；V1 行为 NULL）。自由辩已完成回合数复用
//     completed_rounds（对训练赛即「已完成自由辩回合数」，0–10）。
//   - 正式步骤内容另存 debate_steps；AI 步骤的认领/幂等/版本/租约另存
//     debate_step_claims，但语义与 V1 轮次完全一致：client_request_id 幂等、
//     attempt_version、90 秒租约、进程内并发配额。
//
// 本文件只放纯函数与 DTO，不访问数据库。

// TrainingStage 是训练赛四阶段在数据库与 API 中的受控取值。
// 直接复用 ai.Stage 的四阶段枚举，避免两套字符串各写一遍。
type TrainingStage = ai.Stage

const (
	TrainingStageOpening    = ai.StageOpening
	TrainingStageRebuttal   = ai.StageRebuttal
	TrainingStageFreeDebate = ai.StageFreeDebate
	TrainingStageClosing    = ai.StageClosing
)

// TrainingMaxStageStep 是立论/驳立论/结辩的每阶段步数（双方各一次）。
const TrainingMaxStageStep = 2

// TrainingMode 是训练赛在 debates.mode 中的取值。
const TrainingMode = ai.ModeTrainingV2

// TrainingStepInput 是训练赛步骤提交的请求体。
//
// Content 非空表示提交当前用户步骤；Content 为空表示请求由服务端生成当前 AI 步骤。
// 发言方由服务端按权威进度判定，客户端不能用字段指定；请求与实际发言方不符时返回冲突。
type TrainingStepInput struct {
	ClientRequestID string        `json:"client_request_id"`
	Content         string        `json:"content"`
	ExpectedStage   TrainingStage `json:"expected_stage"`
	ExpectedStep    int           `json:"expected_step"`
}

type TrainingEndInput struct {
	ExpectedFailedAttempt uint64 `json:"expected_failed_attempt_version"`
}

// TrainingRetryInput 是失败 AI 步骤的显式重试请求。
type TrainingRetryInput struct {
	ExpectedAttemptVersion uint64 `json:"expected_attempt_version"`
}

// TrainingStep 是一个已提交的正式发言步骤。失败步骤不写成该行，
// 因此 StepNo 始终是已提交步骤的连续序号（从 1 开始，无空洞）。
type TrainingStep struct {
	ID              uint64
	DebateID        uint64
	StepNo          int
	Stage           TrainingStage
	StageStep       int
	FreeDebateRound int // 非自由辩步骤为 0
	Speaker         ai.Speaker
	ClientRequestID string
	Content         string
	CreatedAt       time.Time
}

// TableName 与迁移保持一致。
func (TrainingStep) TableName() string { return "debate_steps" }

// TrainingStepDTO 是步骤的对外投影；用户与模型内容一律按纯文本渲染。
type TrainingStepDTO struct {
	ID              string    `json:"id"`
	StepNo          int       `json:"step_no"`
	Stage           string    `json:"stage"`
	StageStep       int       `json:"stage_step"`
	FreeDebateRound int       `json:"free_debate_round"`
	Speaker         string    `json:"speaker"`
	ClientRequestID string    `json:"client_request_id"`
	Content         string    `json:"content"`
	CreatedAt       time.Time `json:"created_at"`
}

func (s TrainingStep) DTO() TrainingStepDTO {
	return TrainingStepDTO{formatID(s.ID), s.StepNo, string(s.Stage), s.StageStep, s.FreeDebateRound, string(s.Speaker), s.ClientRequestID, s.Content, s.CreatedAt.UTC()}
}

// TrainingClaim 是一次 AI 步骤生成尝试（认领记录）。
//
// 认领内核与 V1 轮次一致：client_request_id 幂等、attempt_version 版本、
// 90 秒租约与并发配额；失败重试在同一行上递增版本，不新建第二套幂等。
// status 额外允许 abandoned：自由辩失败回复被用户明确放弃后保留记录可见。
type TrainingClaim struct {
	ID              uint64
	DebateID        uint64
	StepNo          int
	ClientRequestID string
	Status          string
	AttemptVersion  uint64
	LeaseExpiresAt  *time.Time
	ErrorCode       *string
	ModelName       *string
	PromptVersion   *string
	InputTokens     *uint64
	OutputTokens    *uint64
	LatencyMS       *uint64
	CreatedAt       time.Time
}

// TableName 与迁移保持一致。
func (TrainingClaim) TableName() string { return "debate_step_claims" }

// TrainingPosition 是训练赛权威进度的纯值表示：阶段、阶段内已完成步数、
// 已完成自由辩回合数与已提交正式步骤总数。
type TrainingPosition struct {
	Stage      TrainingStage
	StageStep  int // 当前阶段已完成步数：0、1、2
	FreeRounds int // 已完成自由辩完整回合数
	Committed  int // 已提交正式步骤总数（失败步骤不占编号，不进入该计数）
	TotalSteps int // 结辩阶段已知总步数；未进入结辩为 0（自由辩回合数未定）
	DebateID   uint64
	UserStance string
	Status     string
}

// TrainingPendingAIDTO 暴露当前步骤上仍在生成或已失败的 AI 尝试，
// 失败记录必须保留可见，不能因为阶段推进而静默消失。
type TrainingPendingAIDTO struct {
	ClaimID        string  `json:"claim_id"`
	StepNo         int     `json:"step_no"`
	Status         string  `json:"status"`
	AttemptVersion uint64  `json:"attempt_version"`
	ErrorCode      *string `json:"error_code"`
}

// TrainingAbandonedAIDTO 暴露已被放弃的失败 AI 尝试，保留可见但不可重试。
type TrainingAbandonedAIDTO struct {
	ClaimID        string  `json:"claim_id"`
	StepNo         int     `json:"step_no"`
	AttemptVersion uint64  `json:"attempt_version"`
	ErrorCode      *string `json:"error_code"`
}

// TrainingProgressDTO 是训练赛进度的对外投影；客户端只用它展示，
// 不得据此自行推导或改写阶段（阶段由服务端在提交事务中推进）。
type TrainingProgressDTO struct {
	Stage               string                  `json:"stage"`
	StageStep           int                     `json:"stage_step"`
	FreeDebateRounds    int                     `json:"free_debate_rounds"`
	FreeDebateMaxRounds int                     `json:"free_debate_max_rounds"`
	CommittedSteps      int                     `json:"committed_steps"`
	TotalSteps          int                     `json:"total_steps"`
	NextStepNo          *int                    `json:"next_step_no"`
	NextSpeaker         *string                 `json:"next_speaker"`
	CanSubmitStep       bool                    `json:"can_submit_step"`
	CanRetryAI          bool                    `json:"can_retry_ai"`
	CanEndFreeDebate    bool                    `json:"can_end_free_debate"`
	EndBlockedReason    *string                 `json:"end_blocked_reason"`
	PendingAI           *TrainingPendingAIDTO   `json:"pending_ai"`
	AbandonedAI         *TrainingAbandonedAIDTO `json:"abandoned_ai"`
}

// TrainingSummaryDTO 是列表与创建响应中的轻量进度，只来自 debates 行，不查步骤表。
type TrainingSummaryDTO struct {
	Stage               string `json:"stage"`
	StageStep           int    `json:"stage_step"`
	FreeDebateRounds    int    `json:"free_debate_rounds"`
	FreeDebateMaxRounds int    `json:"free_debate_max_rounds"`
}

// TrainingDetailDTO 是训练赛详情的完整结构：权威进度 + 已提交步骤。
// 成败仅由服务端给出，客户端不得从检查点/本地缓存推导。
type TrainingDetailDTO struct {
	Progress TrainingProgressDTO `json:"progress"`
	Steps    []TrainingStepDTO   `json:"steps"`
	QueryURL string              `json:"query_url"`
}

// TrainingStepResult 是步骤提交、重试与结束自由辩的共享响应结构。
// Step 仅在本次请求真正提交了正式步骤时出现（202 重放与结束操作时为 null）。
type TrainingStepResult struct {
	Step         *TrainingStepDTO    `json:"step"`
	Progress     TrainingProgressDTO `json:"progress"`
	DebateStatus string              `json:"debate_status"`
	QueryURL     string              `json:"query_url"`
}

// TrainingDetail 是 Repository 读出的训练赛完整状态，供 Service 投影与校验。
type TrainingDetail struct {
	Debate   Debate
	Steps    []TrainingStep
	Claims   []TrainingClaim
	Progress TrainingProgressDTO
}

// trainingQueryURL 是训练赛查询地址；与 V1 一样使用相对路径。
func trainingQueryURL(id uint64) string { return "/api/v1/debates/" + formatID(id) }

// validTrainingStage 报告阶段是否为训练赛四阶段之一。
func validTrainingStage(stage TrainingStage) bool {
	switch stage {
	case TrainingStageOpening, TrainingStageRebuttal, TrainingStageFreeDebate, TrainingStageClosing:
		return true
	default:
		return false
	}
}

// trainingPosition 从 debates 行与已提交步数还原权威进度并做自洽校验。
// 数据不自洽属于编程/迁移错误，返回依赖不可用而不是静默纠正。
func trainingPosition(d Debate, committed int) (TrainingPosition, error) {
	pos := TrainingPosition{
		Stage:      TrainingStage(d.TrainingStageValue()),
		StageStep:  d.TrainingStageStep,
		FreeRounds: d.CompletedRounds,
		Committed:  committed,
		DebateID:   d.ID,
		UserStance: d.UserStance,
		Status:     d.Status,
	}
	if !validTrainingStage(pos.Stage) {
		return pos, dependency(errors.New("training stage invalid"), false)
	}
	if pos.StageStep < 0 || pos.StageStep > TrainingMaxStageStep {
		return pos, dependency(errors.New("training stage step invalid"), false)
	}
	if pos.FreeRounds < 0 || pos.FreeRounds > ai.TrainingMaxFreeDebateRounds {
		return pos, dependency(errors.New("training free rounds invalid"), false)
	}
	if pos.Status != "active" && pos.Status != "completed" {
		return pos, dependency(errors.New("training debate status invalid"), false)
	}
	// 名义步数由阶段进度推导；失败步骤不占编号，因此 committed 只能小于或等于名义值。
	// 结辩另有一种合法形状：自由辩最后一回合用户已发言、AI 回复失败并被明确放弃后进入结辩，
	// 此时自由辩少了一个 AI 步骤但多了一个已提交的用户步骤，committed 恰为名义值加一。
	var nominal int
	switch pos.Stage {
	case TrainingStageOpening:
		nominal = pos.StageStep
	case TrainingStageRebuttal:
		nominal = TrainingMaxStageStep + pos.StageStep
	case TrainingStageFreeDebate:
		nominal = 2*TrainingMaxStageStep + 2*pos.FreeRounds + pos.StageStep
	case TrainingStageClosing:
		nominal = 2*TrainingMaxStageStep + 2*pos.FreeRounds + pos.StageStep
		pos.TotalSteps = committed + (TrainingMaxStageStep - pos.StageStep)
	}
	if committed > nominal {
		// 放弃失败的末回合 AI 回复后进入结辩是唯一允许的 +1 情形。
		if pos.Stage == TrainingStageClosing && pos.FreeRounds >= 1 && committed == nominal+1 {
			return pos, nil
		}
		return pos, dependency(errors.New("training committed steps exceed position"), false)
	}
	if pos.Stage == TrainingStageClosing && pos.FreeRounds < 1 {
		return pos, dependency(errors.New("training closing without free debate"), false)
	}
	return pos, nil
}

// nextStepNo 返回下一步的正式步骤序号。
func (p TrainingPosition) nextStepNo() int { return p.Committed + 1 }

// nextSpeaker 返回下一步的发言方；没有下一步时返回 false。
//
// 返回 false 的两种情形都表示「当前不能提交新步骤」：
//   - 对话已 completed；
//   - 自由辩已完成十回合、仍在自由辩阶段等待用户显式进入结辩。
func (p TrainingPosition) nextSpeaker() (ai.Speaker, bool) {
	if p.Status == "completed" {
		return "", false
	}
	if !validTrainingStage(p.Stage) {
		return "", false
	}
	if p.Stage == TrainingStageFreeDebate && p.StageStep == 0 && p.FreeRounds >= ai.TrainingMaxFreeDebateRounds {
		return "", false
	}
	if p.StageStep >= TrainingMaxStageStep {
		return "", false
	}
	first, ok := p.firstSpeaker()
	if !ok {
		return "", false
	}
	if p.StageStep%2 == 1 {
		return otherSpeaker(first), true
	}
	return first, true
}

// firstSpeaker 返回当前阶段第一步的发言方：
// 立论、驳立论由正方先；自由辩由用户先；结辩由反方先（PRD V2 §3.1）。
func (p TrainingPosition) firstSpeaker() (ai.Speaker, bool) {
	switch p.Stage {
	case TrainingStageOpening, TrainingStageRebuttal:
		if p.UserStance == "pro" {
			return ai.SpeakerUser, true
		}
		if p.UserStance == "con" {
			return ai.SpeakerAssistant, true
		}
	case TrainingStageFreeDebate:
		return ai.SpeakerUser, true
	case TrainingStageClosing:
		if p.UserStance == "pro" {
			return ai.SpeakerAssistant, true
		}
		if p.UserStance == "con" {
			return ai.SpeakerUser, true
		}
	}
	return "", false
}

func otherSpeaker(s ai.Speaker) ai.Speaker {
	if s == ai.SpeakerUser {
		return ai.SpeakerAssistant
	}
	return ai.SpeakerUser
}

// advanceTraining 在一个正式步骤提交后推进权威进度（调用方须已持会话行锁）。
//
// 立论→驳立论→自由辩的跨阶段推进是自动的；自由辩→结辩必须由用户显式结束操作完成，
// 因此这里不会自动离开自由辩（第 10 回合结束后仍停在自由辩，等待显式结束）。
func advanceTraining(d *Debate) error {
	stage := TrainingStage(d.TrainingStageValue())
	step := d.TrainingStageStep
	switch stage {
	case TrainingStageOpening, TrainingStageRebuttal:
		if step+1 < TrainingMaxStageStep {
			d.TrainingStageStep = step + 1
			return nil
		}
		if stage == TrainingStageOpening {
			d.TrainingStage = ptr(string(TrainingStageRebuttal))
		} else {
			d.TrainingStage = ptr(string(TrainingStageFreeDebate))
		}
		d.TrainingStageStep = 0
		return nil
	case TrainingStageFreeDebate:
		switch step {
		case 0:
			d.TrainingStageStep = 1
			return nil
		case 1:
			if d.CompletedRounds >= ai.TrainingMaxFreeDebateRounds {
				return dependency(errors.New("training free debate rounds overflow"), false)
			}
			d.CompletedRounds++
			d.TrainingStageStep = 0
			return nil
		default:
			return dependency(errors.New("training free debate stage step invalid"), false)
		}
	case TrainingStageClosing:
		if step+1 < TrainingMaxStageStep {
			d.TrainingStageStep = step + 1
			return nil
		}
		d.TrainingStageStep = TrainingMaxStageStep
		d.Status = "completed"
		return nil
	default:
		return dependency(errors.New("training stage invalid"), false)
	}
}

// ValidateTrainingStepInput 校验步骤提交输入：用户步骤必须为 1–2000 码点，
// AI 步骤必须为空内容；两者都必须携带规范化 UUID 幂等键。发言方由服务端判定。
func ValidateTrainingStepInput(in TrainingStepInput) (TrainingStepInput, error) {
	if !utf8.ValidString(in.Content) {
		return in, ErrInvalidRequest
	}
	trimmed := strings.TrimFunc(in.Content, func(r rune) bool { return unicode.IsSpace(r) || r == '\ufeff' })
	if in.Content != "" {
		if trimmed == "" || utf8.RuneCountInString(in.Content) > ai.MaxUserContentCodepoints {
			return in, ErrInvalidRequest
		}
	}
	id, e := uuid.Parse(in.ClientRequestID)
	if e != nil {
		return in, ErrInvalidRequest
	}
	in.ClientRequestID = id.String()
	return in, nil
}

// ValidateTrainingRetryInput 校验显式重试的期望版本号。
func ValidateTrainingRetryInput(in TrainingRetryInput) (TrainingRetryInput, error) {
	if in.ExpectedAttemptVersion < 1 || in.ExpectedAttemptVersion == ^uint64(0) {
		return in, ErrInvalidRequest
	}
	return in, nil
}

// trainingStepNumber 返回训练赛会话在 V1 轮次字段上的展示上限（自由辩回合上限）；
// 仅用于 DTO 的 total_rounds 投影，不参与任何阶段推导。
const trainingStepNumber = ai.TrainingMaxFreeDebateRounds

// trainingMaxSteps 是理论上限：自由辩十回合时共 2n+6 = 26 个正式步骤。
const trainingMaxSteps = 2*ai.TrainingMaxFreeDebateRounds + 6

// trainingSummaryDTO 由 debates 行投影列表/创建响应用的轻量进度。
func trainingSummaryDTO(d Debate) *TrainingSummaryDTO {
	return &TrainingSummaryDTO{
		Stage:               d.TrainingStageValue(),
		StageStep:           d.TrainingStageStep,
		FreeDebateRounds:    d.CompletedRounds,
		FreeDebateMaxRounds: ai.TrainingMaxFreeDebateRounds,
	}
}

// trainingProgressDTO 由 debates 行、已提交步骤与认领记录投影出对外进度。
func trainingProgressDTO(d Debate, steps []TrainingStep, claims []TrainingClaim) TrainingProgressDTO {
	progress := TrainingProgressDTO{
		Stage:               d.TrainingStageValue(),
		StageStep:           d.TrainingStageStep,
		FreeDebateRounds:    d.CompletedRounds,
		FreeDebateMaxRounds: ai.TrainingMaxFreeDebateRounds,
		CommittedSteps:      len(steps),
	}
	pos, err := trainingPosition(d, len(steps))
	if err != nil {
		return progress
	}
	progress.TotalSteps = pos.TotalSteps
	if abandoned, ok := latestClaimWithStatus(claims, "abandoned"); ok {
		progress.AbandonedAI = &TrainingAbandonedAIDTO{
			ClaimID: formatID(abandoned.ID), StepNo: abandoned.StepNo,
			AttemptVersion: abandoned.AttemptVersion, ErrorCode: abandoned.ErrorCode,
		}
	}
	speaker, hasNext := pos.nextSpeaker()
	if !hasNext {
		progress.CanEndFreeDebate, progress.EndBlockedReason = endFreeDebateAvailability(pos, claims)
		return progress
	}
	stepNo := pos.nextStepNo()
	next := string(speaker)
	progress.NextStepNo = &stepNo
	progress.NextSpeaker = &next
	claim, hasClaim := activeClaimForStep(claims, stepNo)
	switch {
	case !hasClaim:
		progress.CanSubmitStep = true
	case claim.Status == "generating":
		progress.PendingAI = trainingPendingDTO(claim)
	case claim.Status == "failed":
		progress.PendingAI = trainingPendingDTO(claim)
		progress.CanRetryAI = true
	default:
		// 该步骤已被处理（completed）或放弃（abandoned），按不可提交处理。
	}
	progress.CanEndFreeDebate, progress.EndBlockedReason = endFreeDebateAvailability(pos, claims)
	return progress
}

func trainingPendingDTO(claim TrainingClaim) *TrainingPendingAIDTO {
	return &TrainingPendingAIDTO{ClaimID: formatID(claim.ID), StepNo: claim.StepNo, Status: claim.Status, AttemptVersion: claim.AttemptVersion, ErrorCode: claim.ErrorCode}
}

// endFreeDebateAvailability 返回「进入结辩」是否可用及被阻止的原因。
//
// 允许的两种情形（PRD V2 §3.4、§3.6）：
//   - 自由辩当前回合已完整结束且至少完成 1 个完整回合；
//   - 当前 AI 回复已明确失败且至少完成 1 个完整回合：允许明确放弃本次失败回应并进入结辩。
//
// 生成中、0 回合、以及非自由辩阶段都不可结束。
func endFreeDebateAvailability(pos TrainingPosition, claims []TrainingClaim) (bool, *string) {
	blocked := func(reason string) (bool, *string) { return false, &reason }
	if pos.Stage != TrainingStageFreeDebate {
		return blocked("not_free_debate")
	}
	if hasGeneratingClaim(claims) {
		return blocked("reply_in_progress")
	}
	if pos.FreeRounds < 1 {
		return blocked("no_completed_round")
	}
	if pos.StageStep == 0 {
		return true, nil
	}
	// 阶段内仍有未完成的 AI 步骤：只有该步骤已明确失败时才能放弃并进入结辩。
	if claim, ok := activeClaimForStep(claims, pos.nextStepNo()); ok && claim.Status == "failed" {
		return true, nil
	}
	return blocked("round_incomplete")
}

func hasGeneratingClaim(claims []TrainingClaim) bool {
	for _, claim := range claims {
		if claim.Status == "generating" {
			return true
		}
	}
	return false
}

// activeClaimForStep 返回指定步骤当前活动的认领记录（generating/failed/completed）。
// 已放弃（abandoned）的历史尝试不参与阶段判定，但其记录仍然可查、必须可见。
func activeClaimForStep(claims []TrainingClaim, stepNo int) (TrainingClaim, bool) {
	var found TrainingClaim
	ok := false
	for _, claim := range claims {
		if claim.StepNo != stepNo || claim.Status == "abandoned" {
			continue
		}
		if !ok || claim.ID > found.ID {
			found = claim
			ok = true
		}
	}
	return found, ok
}

// latestClaimForStep 返回指定步骤最新的认领记录（含已放弃），供展示使用。
func latestClaimForStep(claims []TrainingClaim, stepNo int) (TrainingClaim, bool) {
	var found TrainingClaim
	ok := false
	for _, claim := range claims {
		if claim.StepNo == stepNo && (!ok || claim.ID > found.ID) {
			found = claim
			ok = true
		}
	}
	return found, ok
}

// claimByRequestID 返回指定幂等键的认领记录（若有）。
func claimByRequestID(claims []TrainingClaim, key string) (TrainingClaim, bool) {
	for _, claim := range claims {
		if claim.ClientRequestID == key {
			return claim, true
		}
	}
	return TrainingClaim{}, false
}

// stepByRequestID 返回指定幂等键的正式步骤（若有）。
func stepByRequestID(steps []TrainingStep, key string) (TrainingStep, bool) {
	for _, step := range steps {
		if step.ClientRequestID == key {
			return step, true
		}
	}
	return TrainingStep{}, false
}

// latestClaimWithStatus 返回最近一条指定状态的认领记录。
func latestClaimWithStatus(claims []TrainingClaim, status string) (TrainingClaim, bool) {
	var found TrainingClaim
	ok := false
	for _, claim := range claims {
		if claim.Status == status && (!ok || claim.ID > found.ID) {
			found = claim
			ok = true
		}
	}
	return found, ok
}

// trainingRequest 把权威进度与已提交步骤投影成 AI 生成请求。
// 当前步骤一定由 AI 发言：用户步骤由服务端直接落库，不调用模型。
func trainingRequest(uid uint64, d Debate, pos TrainingPosition, steps []TrainingStep) (ai.Request, error) {
	if len(steps) != pos.Committed {
		return ai.Request{}, dependency(errors.New("training steps mismatch"), false)
	}
	history := make([]ai.TrainingStep, 0, len(steps))
	for _, step := range steps {
		history = append(history, ai.TrainingStep{
			StepNo:          step.StepNo,
			Stage:           step.Stage,
			StageStep:       step.StageStep,
			Speaker:         step.Speaker,
			FreeDebateRound: step.FreeDebateRound,
			Content:         step.Content,
		})
	}
	freeRound := 0
	if pos.Stage == TrainingStageFreeDebate && pos.StageStep == 1 {
		freeRound = pos.FreeRounds + 1
	}
	maxRounds := ai.TrainingMaxFreeDebateRounds
	if pos.Stage == TrainingStageClosing {
		maxRounds = pos.FreeRounds
		if maxRounds < 1 {
			return ai.Request{}, dependency(errors.New("training closing without rounds"), false)
		}
	}
	return ai.Request{
		UserID:     uid,
		Mode:       TrainingMode,
		AIStyle:    ai.AIStyle(d.AIStyle),
		Topic:      ai.Topic{Title: d.TopicTitleSnapshot, Description: d.TopicDescriptionSnapshot},
		UserStance: ai.Stance(d.UserStance),
		Training: &ai.TrainingContext{
			Stage:               pos.Stage,
			StepNo:              pos.nextStepNo(),
			StageStep:           pos.StageStep + 1,
			FreeDebateRound:     freeRound,
			FreeDebateRounds:    pos.FreeRounds,
			FreeDebateMaxRounds: maxRounds,
			TotalSteps:          pos.TotalSteps,
			History:             history,
		},
	}, nil
}

// trainingStepForCommit 由权威进度构造即将提交的正式步骤（调用方须已持会话行锁）。
func trainingStepForCommit(d Debate, pos TrainingPosition, speaker ai.Speaker, content, key string) (TrainingStep, error) {
	stepNo := pos.nextStepNo()
	freeRound := 0
	if pos.Stage == TrainingStageFreeDebate {
		freeRound = pos.FreeRounds + 1
	}
	stageStep := pos.StageStep + 1
	return TrainingStep{
		DebateID:        d.ID,
		StepNo:          stepNo,
		Stage:           pos.Stage,
		StageStep:       stageStep,
		FreeDebateRound: freeRound,
		Speaker:         speaker,
		ClientRequestID: key,
		Content:         content,
	}, nil
}
