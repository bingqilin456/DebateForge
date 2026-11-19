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
	ErrDailyQuotaExhausted    = errors.New("daily_quota_exhausted")
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
	// ID 为预设题目的十进制字符串；私有自定义辩题没有题目实体，
	// 因此会话快照中该字段为 JSON null（对应数据库 topic_id 为 NULL）。
	ID          *string `json:"id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
}
type Debate struct {
	ID                       uint64    `json:"-" gorm:"primaryKey"`
	UserID                   uint64    `json:"-"`
	TopicID                  *uint64   `json:"-"`
	TopicTitleSnapshot       string    `json:"-"`
	TopicDescriptionSnapshot string    `json:"-"`
	UserStance               string    `json:"-"`
	Mode                     string    `json:"-"`
	AIStyle                  string    `json:"-"`
	Status                   string    `json:"-"`
	CompletedRounds          int       `json:"-"`
	CreatedAt                time.Time `json:"-"`
	UpdatedAt                time.Time `json:"-"`
}

func (Debate) TableName() string { return "debates" }

// Custom 报告本场是否使用私有自定义辩题。
// topic_id 为空就是自定义语义的唯一事实来源，不额外落库布尔列。
func (d Debate) Custom() bool { return d.TopicID == nil }

// CustomTopic 是创建请求中的私有自定义辩题原文。
// 它只存在于本场会话快照中，不是可复用的题库实体，也没有编辑或收藏生命周期。
type CustomTopic struct {
	Title      string `json:"title"`
	Background string `json:"background"`
}

// CreateInput 是严格二选一的创建输入：TopicID 与 CustomTopic 恰有一个非 nil。
// 服务端归属、赛制与轮次都不从请求接收。
type CreateInput struct {
	TopicID     *uint64
	CustomTopic *CustomTopic
	UserStance  string
	AIStyle     string
}

type DebateDTO struct {
	ID              string    `json:"id"`
	Topic           Topic     `json:"topic"`
	UserStance      string    `json:"user_stance"`
	AIStance        string    `json:"ai_stance"`
	Mode            string    `json:"mode"`
	AIStyle         string    `json:"ai_style"`
	TotalRounds     int       `json:"total_rounds"`
	Status          string    `json:"status"`
	CompletedRounds int       `json:"completed_rounds"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

func (d Debate) DTO() DebateDTO {
	opposite := "pro"
	if d.UserStance == "pro" {
		opposite = "con"
	}
	// 赛制与总轮数必须自洽：无法识别赛制时统一回退为 quick_3，
	// 避免出现 mode 为空而 total_rounds 却为 3 的矛盾输出（前端会判为格式错误）。
	mode := d.Mode
	style := d.AIStyle
	total := 3
	if rounds, err := ai.RoundsFor(ai.Mode(mode)); err == nil {
		total = rounds
	} else {
		mode = string(ai.ModeQuick3)
	}
	if !ai.AIStyle(style).Valid() {
		style = string(ai.StyleLogical)
	}
	return DebateDTO{ID: formatID(d.ID), Topic: Topic{d.topicIDString(), d.TopicTitleSnapshot, d.TopicDescriptionSnapshot}, UserStance: d.UserStance, AIStance: opposite, Mode: mode, AIStyle: style, TotalRounds: total, Status: d.Status, CompletedRounds: d.CompletedRounds, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC()}
}

// topicIDString 只对预设题输出字符串 ID；自定义题没有题目实体，输出 null，
// 避免用一个不存在的编号伪装成可查询的公共辩题。
func (d Debate) topicIDString() *string {
	if d.TopicID == nil {
		return nil
	}
	id := formatID(*d.TopicID)
	return &id
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
	// AnnotationEligible 只表示“本轮在提交时被本次切片允许生成批注”，
	// 不属于正式轮次状态机：它不参与 generating/completed/failed 的判断，
	// 也不改变完成的轮数或会话状态。默认 false，旧行保持原状。
	// 该字段不进入 TurnDTO，读取批注统一走独立接口。
	AnnotationEligible bool       `json:"-" gorm:"column:annotation_eligible"`
	ModelName          *string    `json:"-"`
	PromptVersion      *string    `json:"-"`
	InputTokens        *uint64    `json:"-"`
	OutputTokens       *uint64    `json:"-"`
	LatencyMS          *uint64    `json:"-"`
	CreatedAt          time.Time  `json:"-"`
	Messages           []Message  `json:"-" gorm:"-"`
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
}
type DetailDTO struct {
	Debate        DebateDTO     `json:"debate"`
	Turns         []TurnDTO     `json:"turns"`
	SummaryStatus SummaryStatus `json:"summary_status"`
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
	return DetailDTO{d.Debate.DTO(), turns, d.Summary.DTO()}
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

// ValidateCreateInput 校验严格二选一的创建输入。
//
// 约束（PRD §4.2、§12 与设计文档）：
//   - topic_id 与 custom_topic 恰有一个非 nil；两者同时出现或都缺失都拒绝。
//   - 自定义标题 1–200 码点、拒绝纯空白（Unicode White_Space 与 U+FEFF）；
//     背景 0–1000 码点，可省略或为空串，长度计算包含空白。
//   - 长度按解码后的 Unicode 码点计，组合字符计多个、补充平面字符计一个。
//   - 持久化保留原文：不 trim、不折行、不做任何 Unicode 归一化。
//   - 立场与流派仍由服务端精确校验，赛制与归属不从请求接收。
func ValidateCreateInput(in CreateInput) (CreateInput, error) {
	if (in.TopicID == nil) == (in.CustomTopic == nil) {
		return in, ErrInvalidRequest
	}
	if in.UserStance != "pro" && in.UserStance != "con" {
		return in, ErrInvalidRequest
	}
	if !ai.AIStyle(in.AIStyle).Valid() {
		return in, ErrInvalidRequest
	}
	if in.TopicID != nil {
		if *in.TopicID == 0 {
			return in, ErrInvalidRequest
		}
		return in, nil
	}
	custom := *in.CustomTopic
	if !utf8.ValidString(custom.Title) || !utf8.ValidString(custom.Background) {
		return in, ErrInvalidRequest
	}
	if utf8.RuneCountInString(custom.Title) < 1 || utf8.RuneCountInString(custom.Title) > ai.MaxCustomTopicTitleCodepoints {
		return in, ErrInvalidRequest
	}
	if strings.TrimFunc(custom.Title, isBlankRune) == "" {
		return in, ErrInvalidRequest
	}
	if utf8.RuneCountInString(custom.Background) > ai.MaxCustomTopicBackgroundCodepoints {
		return in, ErrInvalidRequest
	}
	in.CustomTopic = &custom
	return in, nil
}

// isBlankRune 与 ai 包的判空口径一致：Unicode White_Space 加上 U+FEFF。
// 前端 hasNonWhitespace 使用 \p{White_Space} 与 U+FEFF，二者结果必须一致。
func isBlankRune(r rune) bool { return unicode.IsSpace(r) || r == '\ufeff' }
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
//
// 批注方法独立于轮次状态机：Complete 仍然只写轮次与会话的既有字段，
// 另由 MarkAnnotationEligible 单独设置资格标志，两者互不成事务依赖。
type Store interface {
	Create(context.Context, uint64, CreateInput) (Debate, error)
	List(context.Context, uint64, int, int) (DebateList, error)
	Detail(context.Context, uint64, uint64) (Detail, error)
	Claim(context.Context, uint64, uint64, SubmitInput, func() error) (Detail, bool, error)
	ClaimRetry(context.Context, uint64, uint64, uint64, uint64, func() error) (Detail, bool, error)
	Complete(context.Context, uint64, Debate, Turn, ai.Result, bool) (Detail, error)
	Fail(context.Context, uint64, Debate, Turn, string, ai.Usage, ai.Metadata) (Turn, error)
	GetSummary(context.Context, uint64, uint64) (*Summary, error)
	ClaimSummary(context.Context, uint64, uint64, SummaryInput, func() error) (Detail, bool, error)
	CompleteSummary(context.Context, uint64, Debate, Summary, ai.SummaryResult) (Summary, error)
	FailSummary(context.Context, uint64, Debate, Summary, string, ai.Usage, ai.Metadata) (Summary, error)
	GetStrategy(context.Context, uint64, uint64) (StrategyState, error)
	ClaimStrategy(context.Context, uint64, uint64, StrategyInput, func() error) (StrategyState, bool, error)
	CompleteStrategy(context.Context, uint64, Debate, Strategy, ai.StrategyResult) (Strategy, error)
	FailStrategy(context.Context, uint64, Debate, Strategy, string, ai.Usage, ai.Metadata) (Strategy, error)
	ListAnnotations(context.Context, uint64, uint64) ([]AnnotationBatch, error)
	ClaimAnnotation(context.Context, uint64, uint64, uint64, uint64, func() error) (AnnotationClaim, bool, error)
	CompleteAnnotation(context.Context, uint64, Debate, TurnAnnotation, ai.AnnotationResult) (TurnAnnotation, error)
	FailAnnotation(context.Context, uint64, Debate, TurnAnnotation, string, ai.Usage, ai.Metadata) (TurnAnnotation, error)
	RecoverExpiredLeases(context.Context) (int, error)
}
