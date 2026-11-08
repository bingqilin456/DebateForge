package ai

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	adapteropenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// maxAllowedOutputTokens 是本地允许配置的输出上限上界。
// 供应商虽有 384K 输出能力，但本 MVP 单条回复不超过 2000 码点，
// 使用供应商默认值会放大 45 秒超时与费用风险，必须显式配置保守上限。
const maxAllowedOutputTokens = 8192

// OpenAIConfig 是真实适配器的显式配置。APIKey 只由后端注入，
// 不写入日志、错误或结构体 JSON；BaseURL 允许自建回环地址用于测试。
type OpenAIConfig struct {
	APIKey          string `json:"-"`
	BaseURL         string
	Model           string
	MaxOutputTokens int
	Window          WindowLimits
	HTTPClient      *http.Client
	Logger          *slog.Logger
}

// OpenAIAdapter 使用 CloudWeGo Eino 官方 OpenAI 协议适配器执行完整响应调用。
// 它只实现 Generator；不注册 HTTP 路由，也不装配 main。
type OpenAIAdapter struct {
	chatModel model.BaseChatModel
	config    OpenAIConfig
	logger    *slog.Logger
}

// NewOpenAIGenerator 校验配置后构造适配器。配置不合法时立即失败，
// 避免带着未知能力值或宽泛地址发起请求。
func NewOpenAIGenerator(ctx context.Context, cfg OpenAIConfig) (Generator, error) {
	if err := validateOpenAIConfig(cfg); err != nil {
		return nil, err
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultModelTimeout}
	}
	// 复制客户端，避免为观察响应而修改调用方共享的 Transport。
	clientCopy := *httpClient
	transport := clientCopy.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clientCopy.Transport = responseObserverTransport{base: transport}

	maxTokens := cfg.MaxOutputTokens
	chatModel, err := adapteropenai.NewChatModel(ctx, &adapteropenai.ChatModelConfig{
		APIKey:     cfg.APIKey,
		BaseURL:    cfg.BaseURL,
		Model:      cfg.Model,
		MaxTokens:  &maxTokens,
		HTTPClient: &clientCopy,
	})
	if err != nil {
		return nil, &Error{Code: CodeProviderError, Message: "模型适配器初始化失败", cause: err}
	}
	return &OpenAIAdapter{chatModel: chatModel, config: cfg, logger: logger}, nil
}

// Generate 依次执行：输入校验 → Prompt 构建 → 窗口预检 → 有界模型调用 → 回复校验。
// 任一步失败都不会发起多余请求，也不会把供应商正文或用户内容写入日志。
func (a *OpenAIAdapter) Generate(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Result{}, contextError(err, Usage{}, Metadata{})
	}

	prompt, err := BuildPrompt(req)
	if err != nil {
		a.log(ctx, req, err, 0, Usage{})
		return Result{}, err
	}
	if err := a.config.Window.ValidatePrompt(prompt); err != nil {
		a.log(ctx, req, err, 0, Usage{})
		return Result{}, err
	}

	callCtx, cancel := newModelCallContext(ctx)
	defer cancel()

	messages := make([]*schema.Message, len(prompt.Messages))
	for i, message := range prompt.Messages {
		messages[i] = &schema.Message{Role: schema.RoleType(message.Role), Content: message.Content}
	}

	// 逐次调用独立观察：闭包只捕获本次请求的局部变量，避免并发串台。
	// 官方适配器在转换时丢弃 message.refusal，且 usage 是非指针结构，
	// 因此必须从原始 JSON 保留“字段是否真的存在”，不能只依赖 Eino 输出。
	var observation responseObservation
	callCtx = context.WithValue(callCtx, observationContextKey{}, &observation)
	started := time.Now()
	message, err := a.chatModel.Generate(callCtx, messages,
		model.WithMaxTokens(a.config.MaxOutputTokens),
		// DeepSeek 默认开启思考模式；必须显式关闭，否则 45 秒与输出预算会被思考耗尽。
		adapteropenai.WithExtraFields(map[string]any{"thinking": map[string]any{"type": "disabled"}}),
	)
	latency := time.Since(started)
	metadata := Metadata{
		Model:         a.config.Model,
		PromptVersion: prompt.Version,
		LatencyMS:     latency.Milliseconds(),
	}
	usage := observation.usage
	if err != nil {
		controlled := classifyModelError(callCtx, err, usage, metadata)
		// HTTPClient 可以拥有比模型 Context 更短的期限；保持已识别的超时/取消。
		if callCtx.Err() == nil && controlled.Code != CodeModelTimeout && controlled.Code != CodeModelCanceled {
			if observation.httpStatus >= 400 {
				controlled = httpStatusError(observation.httpStatus, err, usage, metadata)
			} else if observation.invalidBody || observation.invalidUTF8 || observation.httpStatus == http.StatusOK {
				controlled = &Error{Code: CodeModelError, Message: "模型响应无效", Usage: usage, Metadata: metadata}
			}
		}
		// 即使适配器报错，原始响应里的拒答事实仍可用于受控分类。
		if controlled.Code == CodeProviderError && observation.hasRefusal {
			controlled.Code, controlled.Message = CodeModelRefused, "模型拒绝回复本次内容"
		}
		controlled.Metadata.Refused = observation.hasRefusal || observation.finishReason == "content_filter"
		controlled.Metadata.FinishReason = observation.finishReason
		a.log(ctx, req, controlled, latency, usage)
		return Result{}, controlled
	}

	// 官方适配器会把 finish_reason 转成字符串；优先使用原始 JSON 观测值。
	finishReason := observation.finishReason
	if !observation.hasFinishReason {
		if message != nil && message.ResponseMeta != nil {
			finishReason = message.ResponseMeta.FinishReason
		}
	}
	metadata.FinishReason = finishReason
	metadata.Refused = observation.hasRefusal || observation.finishReason == "content_filter"

	content := ""
	if message != nil {
		content = message.Content
	}

	if failure := classifyReply(content, observation); failure != nil {
		failure.Usage = usage
		failure.Metadata = metadata
		a.log(ctx, req, failure, latency, usage)
		return Result{}, failure
	}

	result := Result{Content: content, Usage: usage, Metadata: metadata}
	a.log(ctx, req, nil, latency, usage)
	return result, nil
}

// classifyReply 把“HTTP 成功但回复不可用”的情况转为受控错误，绝不返回截断内容。
func classifyReply(content string, observation responseObservation) *Error {
	switch {
	case observation.invalidBody || observation.invalidUTF8:
		return &Error{Code: CodeModelError, Message: "模型响应格式或大小无效"}
	case !observation.hasRole || observation.role != "assistant":
		return &Error{Code: CodeModelError, Message: "模型响应角色无效"}
	case observation.hasRefusal:
		return &Error{Code: CodeModelRefused, Message: "模型拒绝回复本次内容"}
	case observation.hasToolCalls:
		return &Error{Code: CodeModelError, Message: "模型返回了不支持的工具调用输出"}
	case observation.finishReason == "content_filter":
		return &Error{Code: CodeModelRefused, Message: "模型回复被内容策略拦截"}
	case observation.finishReason == "length":
		return &Error{Code: CodeModelError, Message: "模型回复因输出上限被截断"}
	case !observation.hasFinishReason || observation.finishReason != "stop":
		return &Error{Code: CodeModelError, Message: "模型未正常结束回复"}
	}
	if err := ValidateReply(content); err != nil {
		var controlled *Error
		if errors.As(err, &controlled) {
			return controlled
		}
		return &Error{Code: CodeModelError, Message: "模型回复无效"}
	}
	return nil
}

// classifyModelError 只依赖 HTTP 状态码与错误链分类，不从敏感正文猜测原因。
func classifyModelError(callCtx context.Context, err error, usage Usage, metadata Metadata) *Error {
	if ctxErr := callCtx.Err(); ctxErr != nil {
		return contextError(ctxErr, usage, metadata)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &Error{Code: CodeModelTimeout, Message: "模型生成超时", Usage: usage, Metadata: metadata, cause: err}
	}
	if errors.Is(err, context.Canceled) {
		return &Error{Code: CodeModelCanceled, Message: "生成请求已取消", Usage: usage, Metadata: metadata, cause: err}
	}

	var apiError *adapteropenai.APIError
	if errors.As(err, &apiError) {
		return httpStatusError(apiError.HTTPStatusCode, err, usage, metadata)
	}

	var urlError *url.Error
	var netError net.Error
	if errors.As(err, &urlError) || errors.As(err, &netError) {
		return &Error{Code: CodeModelUnavailable, Message: "无法连接模型供应商",
			Usage: usage, Metadata: metadata, cause: err}
	}

	return &Error{Code: CodeProviderError, Message: "模型调用失败", Usage: usage, Metadata: metadata, cause: err}
}

func httpStatusError(status int, cause error, usage Usage, metadata Metadata) *Error {
	controlled := &Error{Usage: usage, Metadata: metadata, cause: cause}
	switch status {
	case http.StatusPaymentRequired:
		controlled.Code, controlled.Message = CodeQuotaExceeded, "模型供应商额度或余额不足"
	case http.StatusTooManyRequests:
		controlled.Code, controlled.Message = CodeRateLimited, "模型供应商限频或并发受限"
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		controlled.Code, controlled.Message = CodeModelUnavailable, "模型供应商暂时不可用"
	default:
		controlled.Code, controlled.Message = CodeProviderError, "模型供应商返回错误"
	}
	return controlled
}

func contextError(ctxErr error, usage Usage, metadata Metadata) *Error {
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		return &Error{Code: CodeModelTimeout, Message: "模型生成超时", Usage: usage, Metadata: metadata, cause: ctxErr}
	}
	return &Error{Code: CodeModelCanceled, Message: "生成请求已取消", Usage: usage, Metadata: metadata, cause: ctxErr}
}

// log 只写白名单字段：不写 Key、发言、辩题正文、Prompt 或供应商原始错误。
// 已知 token 数按真实值记录；未知字段直接省略，绝不以 0 冒充已知。
func (a *OpenAIAdapter) log(ctx context.Context, req Request, err error, latency time.Duration, usage Usage) {
	code := "ok"
	if err != nil {
		var controlled *Error
		if errors.As(err, &controlled) {
			code = string(controlled.Code)
		} else {
			code = string(CodeProviderError)
		}
	}
	attrs := []slog.Attr{
		slog.String("error_code", code),
		slog.String("model", a.config.Model),
		slog.String("prompt_version", PromptVersion),
		slog.Int("round_no", req.RoundNo),
		slog.Int64("latency_ms", latency.Milliseconds()),
	}
	attrs = append(attrs, tokenCountAttrs(usage)...)
	a.logger.LogAttrs(ctx, slog.LevelInfo, "模型调用", attrs...)
}

// tokenCountAttrs 只把已知计数写入日志；未知字段不出现，避免与真实 0 混淆。
func tokenCountAttrs(usage Usage) []slog.Attr {
	attrs := []slog.Attr{
		slog.Bool("usage_known", usage.PromptTokens.Known && usage.CompletionTokens.Known && usage.TotalTokens.Known),
	}
	if usage.PromptTokens.Known {
		attrs = append(attrs, slog.Int("prompt_tokens", usage.PromptTokens.Value))
	}
	if usage.CompletionTokens.Known {
		attrs = append(attrs, slog.Int("completion_tokens", usage.CompletionTokens.Value))
	}
	if usage.TotalTokens.Known {
		attrs = append(attrs, slog.Int("total_tokens", usage.TotalTokens.Value))
	}
	return attrs
}

// responseObservation 是从供应商原始 JSON 中保留的最小事实集合。
// 它只存活于单次调用栈内，不保存完整正文、拒答原文或用户内容。
type responseObservation struct {
	httpStatus      int
	invalidUTF8     bool
	invalidBody     bool
	role            string
	hasRole         bool
	usage           Usage
	hasRefusal      bool
	finishReason    string
	hasFinishReason bool
	hasToolCalls    bool
}

// parseResponseObservation 只解析官方适配器实际使用的 choice：index == 0。
// choices 缺 index 字段视为 0（协议默认），其余 index 会被忽略。
func parseResponseObservation(rawBody []byte) responseObservation {
	var observation responseObservation
	if len(rawBody) == 0 {
		return observation
	}
	var envelope struct {
		Usage   json.RawMessage `json:"usage"`
		Choices []struct {
			Index        json.RawMessage `json:"index"`
			FinishReason json.RawMessage `json:"finish_reason"`
			Message      struct {
				Role      json.RawMessage   `json:"role"`
				Refusal   json.RawMessage   `json:"refusal"`
				ToolCalls []json.RawMessage `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return observation
	}
	observation.usage = parseUsage(envelope.Usage)
	for _, choice := range envelope.Choices {
		if choiceIndex(choice.Index) != 0 {
			continue
		}
		observation.role, observation.hasRole = decodeStringField(choice.Message.Role)
		if reason, present := decodeStringField(choice.FinishReason); present {
			observation.hasFinishReason = true
			observation.finishReason = reason
		}
		if refusal, present := decodeStringField(choice.Message.Refusal); present && refusal != "" {
			observation.hasRefusal = true
		}
		observation.hasToolCalls = len(choice.Message.ToolCalls) > 0
		break
	}
	return observation
}

func choiceIndex(raw json.RawMessage) int {
	if len(raw) == 0 || string(raw) == "null" {
		return 0
	}
	var index int
	if err := json.Unmarshal(raw, &index); err != nil {
		return 0
	}
	return index
}

// parseUsage 逐字段判断存在性：缺失为未知，显式 0 为已知 0；负数视为无效。
func parseUsage(raw json.RawMessage) Usage {
	if len(raw) == 0 || string(raw) == "null" {
		return Usage{}
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return Usage{}
	}
	return Usage{
		PromptTokens:     decodeTokenCount(fields["prompt_tokens"]),
		CompletionTokens: decodeTokenCount(fields["completion_tokens"]),
		TotalTokens:      decodeTokenCount(fields["total_tokens"]),
	}
}

func decodeTokenCount(raw json.RawMessage) TokenCount {
	if len(raw) == 0 || string(raw) == "null" {
		return TokenCount{}
	}
	var value int
	if err := json.Unmarshal(raw, &value); err != nil {
		return TokenCount{}
	}
	if value < 0 {
		return TokenCount{}
	}
	return TokenCount{Known: true, Value: value}
}

func decodeStringField(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", false
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", false
	}
	return value, true
}

// validateOpenAIConfig 显式拒绝未知能力值与不安全地址。
func validateOpenAIConfig(cfg OpenAIConfig) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return invalidInput("必须由后端注入模型 API Key")
	}
	if cfg.Model == "" || cfg.Model != strings.TrimSpace(cfg.Model) {
		return invalidInput("模型 ID 不能为空且不能包含首尾空白")
	}
	if err := validateBaseURL(cfg.BaseURL); err != nil {
		return err
	}
	if cfg.MaxOutputTokens <= 0 {
		return invalidInput("必须显式配置模型输出上限 token 数")
	}
	if cfg.MaxOutputTokens > maxAllowedOutputTokens {
		return invalidInput("模型输出上限不得超过本地保守上界")
	}
	if err := cfg.Window.Validate(); err != nil {
		return err
	}
	// 预检使用的输出预留必须与实际发出的 max_tokens 完全一致，
	// 否则会出现“按小值检查、按大值发送”的预算缺口。
	if cfg.Window.MaxOutputTokens != cfg.MaxOutputTokens {
		return invalidInput("窗口输出上限必须与实际输出上限一致")
	}
	return nil
}

func validateBaseURL(raw string) error {
	if raw == "" {
		return invalidInput("必须显式配置模型 BaseURL")
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return invalidInput("模型 BaseURL 不是合法 URL")
	}
	if parsed.Hostname() == "" {
		return invalidInput("模型 BaseURL 必须包含主机名")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil || parsed.Path != "" {
		return invalidInput("模型 BaseURL 不得包含路径、查询、片段或用户信息")
	}
	switch parsed.Scheme {
	case "https":
		return nil
	case "http":
		if isLoopbackHost(parsed.Hostname()) {
			return nil
		}
		return invalidInput("模型 BaseURL 只允许回环地址使用明文 HTTP")
	default:
		return invalidInput("模型 BaseURL 只支持 https 或回环 http")
	}
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// 提示编译期失败：官方适配器一旦更换接口签名，本适配器必须显式更新。
var _ model.BaseChatModel = (*adapteropenai.ChatModel)(nil)

// timeUntilDeadline 集中处理时间为负的情况，供预算辅助函数使用。
func timeUntilDeadline(deadline time.Time) time.Duration {
	remaining := time.Until(deadline)
	if remaining < 0 {
		return 0
	}
	return remaining
}

var _ Generator = (*OpenAIAdapter)(nil)
