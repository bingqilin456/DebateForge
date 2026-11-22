package debate

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/google/uuid"
)

var (
	ErrInvalidRequest         = errors.New("invalid_request")
	ErrNotFound               = errors.New("not_found")
	ErrRoundConflict          = errors.New("round_conflict")
	ErrIdempotencyConflict    = errors.New("idempotency_conflict")
	ErrTurnFailed             = errors.New("turn_failed")
	ErrAttemptVersionConflict = errors.New("attempt_version_conflict")
	ErrDependencyUnavailable  = errors.New("dependency_unavailable")
	ErrRateLimited            = errors.New("rate_limited")
	ErrGenerationBusy         = errors.New("generation_busy")
)

const (
	LeaseDuration     = 90 * time.Second
	LeaseScanInterval = 30 * time.Second
	DefaultPage       = 1
	DefaultPageSize   = 20
	MaxPageSize       = 50
	MaxPage           = 1_000_000_000
)

// Failure 只暴露受控码；底层原因只通过 Unwrap 留给内部检查。
type Failure struct {
	Code          string
	Status        int
	RetryAfter    int
	Details       *FailureDetails
	Cause         error
	CommitUnknown bool
}

func (e *Failure) Error() string { return e.Code }
func (e *Failure) Unwrap() error { return e.Cause }

type FailureDetails struct {
	TurnID         string `json:"turn_id,omitempty"`
	Status         string `json:"status"`
	AttemptVersion uint64 `json:"attempt_version"`
	QueryURL       string `json:"query_url"`
}

func dependency(cause error, unknown bool) error {
	return &Failure{Code: "dependency_unavailable", Status: 503, Cause: errors.Join(ErrDependencyUnavailable, cause), CommitUnknown: unknown}
}

type Topic struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}
type Debate struct {
	ID                       uint64 `json:"-" gorm:"primaryKey"`
	UserID                   uint64 `json:"-"`
	TopicID                  uint64 `json:"-"`
	TopicTitleSnapshot       string `json:"-"`
	TopicDescriptionSnapshot string `json:"-"`
	UserStance               string `json:"-"`
	Mode                     string `json:"-"`
	AIStyle                  string `json:"-"`
	// TrainingStage / TrainingStageStep 是产品 V2 训练赛的权威阶段进度；
	// V1 行保持 NULL 与 0，且只有训练赛路径读写它们（见 training_model.go）。
	TrainingStage     *string   `json:"-"`
	TrainingStageStep int       `json:"-"`
	Status            string    `json:"-"`
	CompletedRounds   int       `json:"-"`
	CreatedAt         time.Time `json:"-"`
	UpdatedAt         time.Time `json:"-"`
}

// TrainingStageValue 返回训练赛阶段字符串；非训练赛行返回空串。
func (d Debate) TrainingStageValue() string {
	if d.TrainingStage == nil {
		return ""
	}
	return *d.TrainingStage
}

// IsTraining 报告该会话是否使用产品 V2 训练赛阶段模型。
func (d Debate) IsTraining() bool {
	return ai.Mode(d.Mode).IsTraining() && d.TrainingStage != nil
}

func (Debate) TableName() string { return "debates" }

type DebateDTO struct {
	ID              string `json:"id"`
	Topic           Topic  `json:"topic"`
	UserStance      string `json:"user_stance"`
	AIStance        string `json:"ai_stance"`
	Mode            string `json:"mode"`
	AIStyle         string `json:"ai_style"`
	TotalRounds     int    `json:"total_rounds"`
	Status          string `json:"status"`
	CompletedRounds int    `json:"completed_rounds"`
	// Training 仅在产品 V2 训练赛会话上非空，是列表与详情共用的轻量进度；
	// V1 会话保持 null，V1 语义与字段不变。
	Training  *TrainingSummaryDTO `json:"training"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

func (d Debate) DTO() DebateDTO {
	opposite := "pro"
	if d.UserStance == "pro" {
		opposite = "con"
	}
	// 赛制与总轮数必须自洽：训练赛不使用「轮数」，其 total_rounds 固定投影为
	// 自由辩回合上限，真实进度一律来自 training 字段与服务端阶段模型；
	// 无法识别赛制时统一回退为 quick_3，避免出现矛盾输出。
	mode := d.Mode
	style := d.AIStyle
	total := 3
	switch {
	case ai.Mode(mode).IsTraining():
		total = trainingStepNumber
	case func() bool { _, err := ai.RoundsFor(ai.Mode(mode)); return err == nil }():
		total, _ = ai.RoundsFor(ai.Mode(mode))
	default:
		mode = string(ai.ModeQuick3)
	}
	if !ai.AIStyle(style).Valid() {
		style = string(ai.StyleLogical)
	}
	dto := DebateDTO{ID: formatID(d.ID), Topic: Topic{formatID(d.TopicID), d.TopicTitleSnapshot, d.TopicDescriptionSnapshot}, UserStance: d.UserStance, AIStance: opposite, Mode: mode, AIStyle: style, TotalRounds: total, Status: d.Status, CompletedRounds: d.CompletedRounds, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC()}
	if d.IsTraining() {
		dto.Training = trainingSummaryDTO(d)
	}
	return dto
}

type Turn struct {
	ID              uint64     `json:"-" gorm:"primaryKey"`
	DebateID        uint64     `json:"-"`
	RoundNo         int        `json:"round_no"`
	ClientRequestID string     `json:"client_request_id"`
	Status          string     `json:"status"`
	AttemptVersion  uint64     `json:"attempt_version"`
	LeaseExpiresAt  *time.Time `json:"-"`
	ErrorCode       *string    `json:"error_code"`
	ModelName       *string    `json:"-"`
	PromptVersion   *string    `json:"-"`
	InputTokens     *uint64    `json:"-"`
	OutputTokens    *uint64    `json:"-"`
	LatencyMS       *uint64    `json:"-"`
	CreatedAt       time.Time  `json:"-"`
	Messages        []Message  `json:"-" gorm:"-"`
}

func (Turn) TableName() string { return "debate_turns" }

type Message struct {
	ID        uint64    `json:"-" gorm:"primaryKey"`
	TurnID    uint64    `json:"-"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

func (Message) TableName() string { return "messages" }

type MessageDTO struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}
type TurnDTO struct {
	ID              string       `json:"id"`
	RoundNo         int          `json:"round_no"`
	ClientRequestID string       `json:"client_request_id"`
	Status          string       `json:"status"`
	AttemptVersion  uint64       `json:"attempt_version"`
	ErrorCode       *string      `json:"error_code"`
	Messages        []MessageDTO `json:"messages"`
}

func (t Turn) DTO() TurnDTO {
	messages := make([]MessageDTO, 0, len(t.Messages))
	for _, m := range t.Messages {
		messages = append(messages, MessageDTO{formatID(m.ID), m.Role, m.Content, m.CreatedAt.UTC()})
	}
	return TurnDTO{formatID(t.ID), t.RoundNo, t.ClientRequestID, t.Status, t.AttemptVersion, t.ErrorCode, messages}
}

type Summary struct {
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

func (Summary) TableName() string { return "debate_summaries" }

func (s *Summary) DTO() SummaryStatus {
	if s == nil {
		return SummaryStatus{Status: "not_started"}
	}
	v := s.AttemptVersion
	out := SummaryStatus{
		Status:         s.Status,
		AttemptVersion: &v,
	}
	switch s.Status {
	case "failed":
		out.ErrorCode = s.ErrorCode
	case "completed":
		if s.ContentJSON != nil {
			if parsed, err := ai.ParseSummary(*s.ContentJSON); err == nil {
				schemaVer := ai.SummarySchemaVersion
				if s.SchemaVersion != nil && *s.SchemaVersion > 0 {
					schemaVer = *s.SchemaVersion
				}
				out.SchemaVersion = &schemaVer
				out.Content = &parsed
			}
		}
	}
	return out
}

type Detail struct {
	Debate  Debate
	Turns   []Turn
	Summary *Summary
	// Training 仅在训练赛会话上非空；此时 Turns 为空，进度来自训练赛阶段模型。
	Training *TrainingDetail
}
type DetailDTO struct {
	Debate        DebateDTO     `json:"debate"`
	Turns         []TurnDTO     `json:"turns"`
	SummaryStatus SummaryStatus `json:"summary_status"`
	// Training 仅训练赛会话存在：权威阶段进度与全部已提交步骤。
	// V1 会话该字段为 null，turns 仍为 V1 轮次；训练赛会话 turns 恒为空数组。
	Training *TrainingDetailDTO `json:"training"`
}
type SummaryStatus struct {
	Status         string      `json:"status"`
	AttemptVersion *uint64     `json:"attempt_version"`
	ErrorCode      *string     `json:"error_code,omitempty"`
	SchemaVersion  *int        `json:"schema_version,omitempty"`
	Content        *ai.Summary `json:"content,omitempty"`
}

func (d Detail) DTO() DetailDTO {
	turns := make([]TurnDTO, 0, len(d.Turns))
	for _, t := range d.Turns {
		turns = append(turns, t.DTO())
	}
	out := DetailDTO{Debate: d.Debate.DTO(), Turns: turns, SummaryStatus: d.Summary.DTO()}
	if d.Training != nil {
		steps := make([]TrainingStepDTO, 0, len(d.Training.Steps))
		for _, step := range d.Training.Steps {
			steps = append(steps, step.DTO())
		}
		out.Training = &TrainingDetailDTO{Progress: d.Training.Progress, Steps: steps, QueryURL: trainingQueryURL(d.Debate.ID)}
	}
	return out
}

type TurnData struct {
	Turn            TurnDTO `json:"turn"`
	CompletedRounds int     `json:"completed_rounds"`
	QueryURL        string  `json:"query_url"`
	DebateStatus    string  `json:"-"`
}
type SubmitInput struct {
	RoundNo         int    `json:"round_no"`
	Content         string `json:"content"`
	ClientRequestID string `json:"client_request_id"`
}
type RetryInput struct {
	ExpectedAttemptVersion uint64 `json:"expected_attempt_version"`
}
type SummaryInput struct {
	Retry                  bool    `json:"retry"`
	ExpectedAttemptVersion *uint64 `json:"expected_attempt_version"`
}
type DebateList struct {
	Items    []Debate
	Total    int64
	Page     int
	PageSize int
}
type DebateListDTO struct {
	Items    []DebateDTO `json:"items"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

func (l DebateList) DTO() DebateListDTO {
	items := make([]DebateDTO, 0, len(l.Items))
	for _, d := range l.Items {
		items = append(items, d.DTO())
	}
	return DebateListDTO{
		Items:    items,
		Total:    l.Total,
		Page:     l.Page,
		PageSize: l.PageSize,
	}
}

func formatID(id uint64) string { return strconv.FormatUint(id, 10) }
func ParseID(s string) (uint64, error) {
	if len(s) == 0 || len(s) > 20 || s[0] < '1' || s[0] > '9' {
		return 0, ErrInvalidRequest
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, ErrInvalidRequest
		}
	}
	n, e := strconv.ParseUint(s, 10, 64)
	if e != nil {
		return 0, ErrInvalidRequest
	}
	return n, nil
}
func ValidateSubmit(in SubmitInput) (SubmitInput, error) {
	if in.RoundNo < 1 || !utf8.ValidString(in.Content) || strings.TrimFunc(in.Content, func(r rune) bool { return unicode.IsSpace(r) || r == '\ufeff' }) == "" || utf8.RuneCountInString(in.Content) > 2000 {
		return in, ErrInvalidRequest
	}
	id, e := uuid.Parse(in.ClientRequestID)
	if e != nil {
		return in, ErrInvalidRequest
	}
	in.ClientRequestID = id.String()
	return in, nil
}
func ValidateRetry(in RetryInput) (RetryInput, error) {
	if in.ExpectedAttemptVersion < 1 || in.ExpectedAttemptVersion == ^uint64(0) {
		return in, ErrInvalidRequest
	}
	return in, nil
}
func ValidateSummaryInput(in SummaryInput) (SummaryInput, error) {
	if in.Retry {
		if in.ExpectedAttemptVersion == nil || *in.ExpectedAttemptVersion < 1 || *in.ExpectedAttemptVersion == ^uint64(0) {
			return in, ErrInvalidRequest
		}
	} else if in.ExpectedAttemptVersion != nil && (*in.ExpectedAttemptVersion < 1 || *in.ExpectedAttemptVersion == ^uint64(0)) {
		return in, ErrInvalidRequest
	}
	return in, nil
}
func ParsePagination(pageValues, pageSizeValues []string) (int, int, error) {
	page := DefaultPage
	if len(pageValues) > 1 {
		return 0, 0, ErrInvalidRequest
	}
	if len(pageValues) == 1 {
		v, err := ParseID(pageValues[0])
		if err != nil || v > MaxPage {
			return 0, 0, ErrInvalidRequest
		}
		page = int(v)
	}
	pageSize := DefaultPageSize
	if len(pageSizeValues) > 1 {
		return 0, 0, ErrInvalidRequest
	}
	if len(pageSizeValues) == 1 {
		v, err := ParseID(pageSizeValues[0])
		if err != nil || v > MaxPageSize {
			return 0, 0, ErrInvalidRequest
		}
		pageSize = int(v)
	}
	return page, pageSize, nil
}

// Store 将短事务和提交不确定性留在 Repository；admit 必须非阻塞且不调用模型。
type Store interface {
	Create(context.Context, uint64, uint64, string, string) (Debate, error)
	// CreateTraining 创建产品 V2 训练赛（mode=training_v2，权威阶段从立论开始）。
	CreateTraining(context.Context, uint64, uint64, string, string) (Debate, error)
	List(context.Context, uint64, int, int) (DebateList, error)
	Detail(context.Context, uint64, uint64) (Detail, error)
	// GetTrainingDetail 读取训练赛权威进度与全部已提交步骤。
	GetTrainingDetail(context.Context, uint64, uint64) (TrainingDetail, error)
	Claim(context.Context, uint64, uint64, SubmitInput, func() error) (Detail, bool, error)
	ClaimRetry(context.Context, uint64, uint64, uint64, uint64, func() error) (Detail, bool, error)
	// ClaimTrainingStep 认领训练赛下一步；用户步骤在事务内直接落库并推进阶段。
	ClaimTrainingStep(context.Context, uint64, uint64, TrainingStepInput, func() error) (TrainingDetail, bool, error)
	// ClaimTrainingRetry 在失败 AI 步骤上按期望版本递增重试。
	ClaimTrainingRetry(context.Context, uint64, uint64, int, uint64, func() error) (TrainingDetail, bool, error)
	// CompleteTrainingStep 在最终事务内写入 AI 正式步骤并推进阶段。
	CompleteTrainingStep(context.Context, uint64, Debate, TrainingClaim, ai.Result) (TrainingDetail, error)
	// FailTrainingClaim 标记 AI 尝试失败，失败步骤不占编号、不推进阶段。
	FailTrainingClaim(context.Context, uint64, Debate, TrainingClaim, string, ai.Usage, ai.Metadata) (TrainingClaim, error)
	// EndFreeDebate 显式进入结辩；重放不重复切换，不产生模型调用。
	EndFreeDebate(context.Context, uint64, uint64, TrainingEndInput) (TrainingDetail, error)
	Complete(context.Context, uint64, Debate, Turn, ai.Result) (Detail, error)
	Fail(context.Context, uint64, Debate, Turn, string, ai.Usage, ai.Metadata) (Turn, error)
	GetSummary(context.Context, uint64, uint64) (*Summary, error)
	ClaimSummary(context.Context, uint64, uint64, SummaryInput, func() error) (Detail, bool, error)
	CompleteSummary(context.Context, uint64, Debate, Summary, ai.SummaryResult) (Summary, error)
	FailSummary(context.Context, uint64, Debate, Summary, string, ai.Usage, ai.Metadata) (Summary, error)
	GetStrategy(context.Context, uint64, uint64) (StrategyState, error)
	ClaimStrategy(context.Context, uint64, uint64, StrategyInput, func() error) (StrategyState, bool, error)
	CompleteStrategy(context.Context, uint64, Debate, Strategy, ai.StrategyResult) (Strategy, error)
	FailStrategy(context.Context, uint64, Debate, Strategy, string, ai.Usage, ai.Metadata) (Strategy, error)
	RecoverExpiredLeases(context.Context) (int, error)
}
