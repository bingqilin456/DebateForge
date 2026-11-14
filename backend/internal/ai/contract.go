package ai

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"
)

// PromptVersion 是本轮唯一的 Prompt 版本标识；任何措辞或结构变更都必须换版本，
// 便于 T05 在 debate_turns.prompt_version 中留下可追溯记录。
const PromptVersion = "debate-v1"

// Mode 是会话的赛制模式标识。它是轮数上限的唯一事实来源：
// 数据库 debates.mode 决定该会话跑几轮，代码不再保留固定的轮数常量。
type Mode string

const (
	// ModeQuick3 是三轮快速赛制（立论/攻辩/结辩）。
	// 仅用于历史数据兼容与隔离 E2E 测试，生产 API 不允许新建。
	ModeQuick3 Mode = "quick_3"
	// ModeStandard4 是四轮标准赛制（立论/驳立论/自由辩/结辩），新建会话的默认赛制。
	ModeStandard4 Mode = "standard_4"
)

// roundsByMode 是赛制到轮数上限的唯一映射。新增赛制只在此处登记。
var roundsByMode = map[Mode]int{
	ModeQuick3:    3,
	ModeStandard4: 4,
}

// Valid 报告赛制是否为已知枚举值。
func (m Mode) Valid() bool {
	_, ok := roundsByMode[m]
	return ok
}

// RoundsFor 返回该赛制的正式轮数上限。未知赛制返回错误，
// 不静默降级到任何默认轮数，避免用错误的上限推进状态机。
func RoundsFor(m Mode) (int, error) {
	rounds, ok := roundsByMode[m]
	if !ok {
		return 0, invalidInput("赛制模式必须是 quick_3 或 standard_4")
	}
	return rounds, nil
}

// MustRoundsFor 用于配置校验等已确知合法的场景；未知赛制属于编程错误，直接 panic。
func MustRoundsFor(m Mode) int {
	rounds, err := RoundsFor(m)
	if err != nil {
		panic("未知赛制模式: " + string(m))
	}
	return rounds
}

// AIStyle 是陪练 AI 的辩手流派，影响 Prompt 的论证风格描述。
// 与赛制模式分开：流派改变话术风格，不改变轮数或状态机。
type AIStyle string

const (
	// StyleLogical 逻辑型：循前提与推理推进，默认流派。
	StyleLogical AIStyle = "logical"
	// StyleAggressive 强攻型：高频质疑与压迫式追问。
	StyleAggressive AIStyle = "aggressive"
	// StyleEmpathic 共情型：先复述理解再指出分歧。
	StyleEmpathic AIStyle = "empathic"
	// StyleData 数据型：强调量化依据与可核验事实。
	StyleData AIStyle = "data"
)

// styleLabels 是流派的固定中文描述，用于 Prompt。
var styleLabels = map[AIStyle]string{
	StyleLogical:    "逻辑型：按前提与推理逐层推进",
	StyleAggressive: "强攻型：高频质疑并压迫式追问对方前提",
	StyleEmpathic:   "共情型：先复述并承认合理处，再指出分歧",
	StyleData:       "数据型：强调可核验的量化依据与事实",
}

// Valid 报告流派是否为已知枚举值。
func (s AIStyle) Valid() bool {
	_, ok := styleLabels[s]
	return ok
}

// Label 返回流派的固定中文描述；未知流派返回空串，由调用方决定是否报错。
func (s AIStyle) Label() string { return styleLabels[s] }

const (
	// MaxUserContentCodepoints 与 API 契约的 1–2000 Unicode 码点一致。
	MaxUserContentCodepoints = 2000
	// MaxTopicTitleCodepoints 限制受信任辩题标题长度，防止 Prompt 无界膨胀。
	MaxTopicTitleCodepoints = 200
	// MaxTopicDescriptionCodepoints 限制受信任辩题描述长度。
	MaxTopicDescriptionCodepoints = 2000
)

// DefaultRequestBudget 是单个生成请求从 HTTP 入口开始计算的总预算。
// 注意：这是本包的 Context 辅助，不等于端到端 HTTP 验收；T05 仍须在
// HTTP 入口创建并在真实链路上验证该预算。
const DefaultRequestBudget = 60 * time.Second

// DefaultModelTimeout 是单次模型调用的上限，必须短于 DefaultRequestBudget，
// 给数据库写回和响应序列化留出余量。
const DefaultModelTimeout = 45 * time.Second

// Role 只由服务端按 Prompt 结构赋值，调用方无法传入任意角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Stance 是用户在会话创建时确定的立场；AI 立场始终由服务端取反。
type Stance string

const (
	StancePro Stance = "pro"
	StanceCon Stance = "con"
)

// Opposite 返回相反的 AI 立场。未知立场必须报错，不能默认偏向某一方。
func (s Stance) Opposite() (Stance, error) {
	switch s {
	case StancePro:
		return StanceCon, nil
	case StanceCon:
		return StancePro, nil
	default:
		return "", invalidInput("立场必须是 pro 或 con")
	}
}

// label 是写入中文 Prompt 的固定措辞，不接受外部输入。
func (s Stance) label() string {
	switch s {
	case StancePro:
		return "正方"
	case StanceCon:
		return "反方"
	default:
		return "未知立场"
	}
}

// Topic 是调用方从数据库读取的受信任快照，ai 本身没有数据库访问权限。
type Topic struct {
	Title       string
	Description string
}

// CompletedTurn 仅代表数据库中已成功完成的轮次对，不含失败片段或伪造角色。
type CompletedTurn struct {
	RoundNo          int
	UserContent      string
	AssistantContent string
}

// Request 是一次无状态生成请求：每轮都完整重建消息，不跨请求共享可变状态。
type Request struct {
	// UserID 来自认证会话，只用于真实试运行准入。
	UserID         uint64
	Mode           Mode
	AIStyle        AIStyle
	Topic          Topic
	UserStance     Stance
	RoundNo        int
	CompletedTurns []CompletedTurn
	UserContent    string
}

// PromptMessage 是一条待发送消息，角色由服务端构造，调用方不可指定。
type PromptMessage struct {
	Role    Role
	Content string
}

// Prompt 是构建完成、可直接交给模型适配器的一组消息。
type Prompt struct {
	Version  string
	Messages []PromptMessage
}

// TokenCount 区分“供应商显式返回 0”和“供应商未返回”两种语义。
// 未知必须保持 Unknown，不能用 0 伪装已知，否则计费与限额统计会被误导。
type TokenCount struct {
	Known bool
	Value int
}

// Known 构造一个已知的 token 数量。
func KnownCount(value int) TokenCount { return TokenCount{Known: true, Value: value} }

// Usage 保存一次调用的 token 用量，逐字段记录已知性。
type Usage struct {
	PromptTokens     TokenCount
	CompletionTokens TokenCount
	TotalTokens      TokenCount
}

// Metadata 是可供持久化和排障的受控元数据；不含发言、Key、拒答原文或供应商原始正文。
type Metadata struct {
	Model         string
	PromptVersion string
	FinishReason  string
	// Refused 只记录“供应商确实给出拒答/内容过滤”这一事实，不保存拒答文本。
	Refused   bool
	LatencyMS int64
}

// Result 是一次成功生成的完整回复。
type Result struct {
	Content  string
	Usage    Usage
	Metadata Metadata
}

// Generator 是业务侧最小接口，真实 Eino 适配器与 Fake 共用同一契约。
// 调用方负责所有权校验与数据库读写，ai 不决定业务状态。
type Generator interface {
	Generate(ctx context.Context, req Request) (Result, error)
}

// ErrorCode 是对外受控错误码，映射到 API 契约中的 model_error / model_timeout /
// model_unavailable / rate_limited 等分类。原始供应商正文不进入该字段。
type ErrorCode string

const (
	// CodeInvalidInput 表示请求在调用模型前就被拒绝。
	CodeInvalidInput ErrorCode = "invalid_input"
	// CodeWindowExceeded 表示显式配置的上下文窗口不足以容纳本轮与未来总结预留。
	CodeWindowExceeded ErrorCode = "window_exceeded"
	// CodeModelError 表示回复无效：空白、超长、截断、工具输出或未知结束原因。
	CodeModelError ErrorCode = "model_error"
	// CodeModelRefused 表示供应商拒答或内容过滤。
	CodeModelRefused ErrorCode = "model_refused"
	// CodeModelTimeout 表示模型调用超过 45 秒上限或父期限。
	CodeModelTimeout ErrorCode = "model_timeout"
	// CodeModelCanceled 表示调用方主动取消。
	CodeModelCanceled ErrorCode = "model_canceled"
	// CodeModelUnavailable 表示供应商容量或连接不可用。
	CodeModelUnavailable ErrorCode = "model_unavailable"
	// CodeRateLimited 表示供应商限频或并发限制（HTTP 429）。
	CodeRateLimited ErrorCode = "rate_limited"
	// CodeQuotaExceeded 表示供应商额度或余额不足（HTTP 402）。
	CodeQuotaExceeded ErrorCode = "quota_exceeded"
	// CodeProviderError 表示其余未分类的供应商错误。
	CodeProviderError ErrorCode = "provider_error"
)

// Error 是对外受控错误：Message 固定为中文受控提示，cause 仅用于内部诊断，
// 但不会被 Error() 泄漏；Usage/Metadata 保留失败时仍可用的计费信息。
type Error struct {
	Code     ErrorCode
	Message  string
	Usage    Usage
	Metadata Metadata
	cause    error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap 暴露内部 cause 供 errors.Is 判断，但不参与对外文本。
func (e *Error) Unwrap() error { return e.cause }

// WithCause 复制错误并附加内部 cause，保持受控消息不变。
func (e *Error) WithCause(cause error) *Error {
	clone := *e
	clone.cause = cause
	return &clone
}

func invalidInput(message string) *Error {
	return &Error{Code: CodeInvalidInput, Message: message}
}

func windowExceeded(message string) *Error {
	return &Error{Code: CodeWindowExceeded, Message: message}
}

// ValidateRequest 在调用任何模型之前校验输入，绝不截断或静默丢弃内容。
func ValidateRequest(req Request) error {
	if !utf8.ValidString(req.Topic.Title) || !utf8.ValidString(req.Topic.Description) ||
		!utf8.ValidString(req.UserContent) {
		return invalidInput("输入必须是合法 UTF-8")
	}
	if err := validateText("辩题标题", req.Topic.Title, MaxTopicTitleCodepoints); err != nil {
		return err
	}
	if err := validateText("辩题描述", req.Topic.Description, MaxTopicDescriptionCodepoints); err != nil {
		return err
	}
	// 轮数上限来自会话赛制，不从常量读取；未知赛制在 RoundsFor 处受控失败。
	rounds, err := RoundsFor(req.Mode)
	if err != nil {
		return err
	}
	// 流派为空表示使用默认逻辑型；显式传入则必须是已知枚举。
	if req.AIStyle != "" && !req.AIStyle.Valid() {
		return invalidInput("辩手流派必须是 logical、aggressive、empathic 或 data")
	}
	if req.RoundNo < 1 || req.RoundNo > rounds {
		return invalidInput(fmt.Sprintf("轮次必须是 1 至 %d", rounds))
	}
	if _, err := req.UserStance.Opposite(); err != nil {
		return err
	}
	if err := validateText("用户发言", req.UserContent, MaxUserContentCodepoints); err != nil {
		return err
	}
	// 历史必须是从第 1 轮开始的连续已完成轮次对，调用方不能跳轮或补造内容。
	if len(req.CompletedTurns) != req.RoundNo-1 {
		return invalidInput("已完成轮次数量必须等于当前轮次减一")
	}
	for index, turn := range req.CompletedTurns {
		if turn.RoundNo != index+1 {
			return invalidInput("历史轮次必须从 1 开始连续且有序")
		}
		if !utf8.ValidString(turn.UserContent) || !utf8.ValidString(turn.AssistantContent) {
			return invalidInput("历史内容必须是合法 UTF-8")
		}
		if err := validateText("历史用户发言", turn.UserContent, MaxUserContentCodepoints); err != nil {
			return err
		}
		if err := validateText("历史 AI 回复", turn.AssistantContent, MaxUserContentCodepoints); err != nil {
			return err
		}
	}
	return nil
}

// ValidateReply 校验模型完整回复；超长、空白或非法 UTF-8 一律受控失败，不截断。
func ValidateReply(content string) error {
	if !utf8.ValidString(content) {
		return &Error{Code: CodeModelError, Message: "模型回复不是合法 UTF-8"}
	}
	if err := validateText("模型回复", content, MaxUserContentCodepoints); err != nil {
		return &Error{Code: CodeModelError, Message: err.(*Error).Message}
	}
	return nil
}

func validateText(field, value string, maxCodepoints int) error {
	if !utf8.ValidString(value) {
		return invalidInput(field + "必须是合法 UTF-8")
	}
	trimmed := trimSpace(value)
	if trimmed == "" {
		return invalidInput(field + "不能为空白")
	}
	if utf8.RuneCountInString(value) > maxCodepoints {
		return invalidInput(fmt.Sprintf("%s最多 %d 个 Unicode 码点", field, maxCodepoints))
	}
	return nil
}

// trimSpace 覆盖常见空白，包括全角空格，避免只校验 ASCII 空白被绕过。
func trimSpace(value string) string {
	start := 0
	end := len(value)
	for start < end {
		r, size := utf8.DecodeRuneInString(value[start:])
		if !isSpaceRune(r) {
			break
		}
		start += size
	}
	for end > start {
		r, size := utf8.DecodeLastRuneInString(value[:end])
		if !isSpaceRune(r) {
			break
		}
		end -= size
	}
	return value[start:end]
}

func isSpaceRune(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x85, 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// IsCanceled 判断错误链中是否包含取消，供上层映射受控错误码。
func IsCanceled(err error) bool { return errors.Is(err, context.Canceled) }
