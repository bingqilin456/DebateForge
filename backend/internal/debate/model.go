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
	TurnID         string `json:"turn_id"`
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
	ID                       uint64    `json:"-" gorm:"primaryKey"`
	UserID                   uint64    `json:"-"`
	TopicID                  uint64    `json:"-"`
	TopicTitleSnapshot       string    `json:"-"`
	TopicDescriptionSnapshot string    `json:"-"`
	UserStance               string    `json:"-"`
	Status                   string    `json:"-"`
	CompletedRounds          int       `json:"-"`
	CreatedAt                time.Time `json:"-"`
	UpdatedAt                time.Time `json:"-"`
}

func (Debate) TableName() string { return "debates" }

type DebateDTO struct {
	ID              string    `json:"id"`
	Topic           Topic     `json:"topic"`
	UserStance      string    `json:"user_stance"`
	AIStance        string    `json:"ai_stance"`
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
	return DebateDTO{ID: formatID(d.ID), Topic: Topic{formatID(d.TopicID), d.TopicTitleSnapshot, d.TopicDescriptionSnapshot}, UserStance: d.UserStance, AIStance: opposite, Status: d.Status, CompletedRounds: d.CompletedRounds, CreatedAt: d.CreatedAt.UTC(), UpdatedAt: d.UpdatedAt.UTC()}
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

type Detail struct {
	Debate Debate
	Turns  []Turn
}
type DetailDTO struct {
	Debate        DebateDTO     `json:"debate"`
	Turns         []TurnDTO     `json:"turns"`
	SummaryStatus SummaryStatus `json:"summary_status"`
}
type SummaryStatus struct {
	Status         string  `json:"status"`
	AttemptVersion *uint64 `json:"attempt_version"`
}

func (d Detail) DTO() DetailDTO {
	turns := make([]TurnDTO, 0, len(d.Turns))
	for _, t := range d.Turns {
		turns = append(turns, t.DTO())
	}
	return DetailDTO{d.Debate.DTO(), turns, SummaryStatus{Status: "not_started"}}
}

type TurnData struct {
	Turn            TurnDTO `json:"turn"`
	CompletedRounds int     `json:"completed_rounds"`
	QueryURL        string  `json:"query_url"`
}
type SubmitInput struct {
	RoundNo         int    `json:"round_no"`
	Content         string `json:"content"`
	ClientRequestID string `json:"client_request_id"`
}
type RetryInput struct {
	ExpectedAttemptVersion uint64 `json:"expected_attempt_version"`
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
	Create(context.Context, uint64, uint64, string) (Debate, error)
	List(context.Context, uint64, int, int) (DebateList, error)
	Detail(context.Context, uint64, uint64) (Detail, error)
	Claim(context.Context, uint64, uint64, SubmitInput, func() error) (Detail, bool, error)
	ClaimRetry(context.Context, uint64, uint64, uint64, uint64, func() error) (Detail, bool, error)
	Complete(context.Context, uint64, Debate, Turn, ai.Result) (Detail, error)
	Fail(context.Context, uint64, Debate, Turn, string, ai.Usage, ai.Metadata) (Turn, error)
	RecoverExpiredLeases(context.Context) (int, error)
}
