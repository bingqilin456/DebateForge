package debate

import (
	"bytes"
	"encoding/json"
	"errors"
	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/gin-gonic/gin"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"unicode/utf8"
)

type Handler struct{ service *Service }

func NewHandler(service *Service) *Handler { return &Handler{service} }
func (h *Handler) RegisterRoutes(group *gin.RouterGroup, a *auth.Handler) {
	// 没有认证依赖时不注册业务写入口，避免遗漏 Origin/CSRF。
	if a == nil {
		return
	}
	if h.service != nil {
		h.service.StartLeaseRecovery(0)
	}
	group.GET("/debates", a.RequireAuth(), h.list)
	group.POST("/debates", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.create)
	group.POST("/debates/:id/turns", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.submit)
	group.POST("/debates/:id/turns/stream", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.stream)
	group.POST("/debates/:id/turns/:turn_id/retry", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.retry)
	// 产品 V2 训练赛：按发言步骤推进，与 V1 轮次端点并存但互不复用状态机。
	// 创建端点单独放在 /training-debates，避免与 /debates/:id 静态段和通配段冲突。
	group.POST("/training-debates", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.createTraining)
	group.POST("/debates/:id/steps", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitTrainingGeneration, h.submitStep)
	group.POST("/debates/:id/steps/stream", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitTrainingGeneration, h.streamStep)
	group.POST("/debates/:id/steps/:step_no/retry", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitTrainingGeneration, h.retryStep)
	group.POST("/debates/:id/free-debate/end", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.endFreeDebate)
	group.POST("/debates/:id/summary", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.summary)
	group.GET("/debates/:id/summary", a.RequireAuth(), h.getSummary)
	group.GET("/debates/:id/strategy", a.RequireAuth(), h.getStrategy)
	group.POST("/debates/:id/strategy", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.strategy)
	group.GET("/debates/:id", a.RequireAuth(), h.detail)
	// 批注读取与手动补做：读取只要 Session；补做额外要求 Origin/CSRF 与防刷前置。
	// 路径与既有轮次/总结入口并列，不改动任何既有路由。
	h.registerAnnotationRoutes(group, a)
	// V2-13 复盘：读取只要 Session；显式生成/重试额外要求 Origin/CSRF 与防刷前置。
	h.registerReviewRoutes(group, a)
	h.registerTrainingRepractice(group, a)
	if h.service != nil && h.service.trainingIssues != nil {
		NewIssueCardHandler(h.service.trainingIssues).RegisterIssueCardRoutes(group, a)
	}
}

// 有效身份与写保护通过后、路径/正文解析前计数；坏 JSON、超长和非法字段也受防刷限制。
func (h *Handler) limitGeneration(c *gin.Context) {
	current, _ := auth.CurrentUserFrom(c)
	if err := h.service.gate.Request(current.User.ID); err != nil {
		serviceError(c, err)
		return
	}
	c.Next()
}
func decode(c *gin.Context, payload any) bool {
	body, e := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, auth.MaxRequestBodyBytes))
	if e != nil {
		var large *http.MaxBytesError
		if errors.As(e, &large) {
			writeError(c, 413, "request_too_large", nil)
		} else {
			writeError(c, 400, "invalid_request", nil)
		}
		return false
	}
	media, _, e := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if e != nil || media != "application/json" || !utf8.Valid(body) {
		writeError(c, 400, "invalid_request", nil)
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if e = decoder.Decode(payload); e != nil {
		writeError(c, 400, "invalid_request", nil)
		return false
	}
	if e = decoder.Decode(new(any)); !errors.Is(e, io.EOF) {
		writeError(c, 400, "invalid_request", nil)
		return false
	}
	return true
}

// createRequest 是 POST /api/v1/debates 的原始请求形状。
// 用 RawMessage 保留“字段是否出现”，以便严格二选一：
// 字段缺失与显式 null 必须区分，两个字段都出现也要拒绝。
type createRequest struct {
	TopicID     json.RawMessage
	CustomTopic json.RawMessage
	UserStance  string
	AIStyle     json.RawMessage
	// unknown/duplicate 记录顶层额外键与重复键。自定义分支是新入口，
	// 必须严格拒绝；预设分支保留既有“忽略未知字段”的行为，避免破坏旧客户端。
	unknown   []string
	duplicate []string
}

// UnmarshalJSON 逐键扫描顶层对象，记录未知键与重复键，同时保留
// encoding/json 的“重复键取最后一次”赋值语义供预设分支沿用。
func (in *createRequest) UnmarshalJSON(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return ErrInvalidRequest
	}
	*in = createRequest{}
	seen := map[string]bool{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return ErrInvalidRequest
		}
		key, ok := token.(string)
		if !ok {
			return ErrInvalidRequest
		}
		if seen[key] {
			in.duplicate = append(in.duplicate, key)
		}
		seen[key] = true
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return ErrInvalidRequest
		}
		switch key {
		case "topic_id":
			in.TopicID = value
		case "custom_topic":
			in.CustomTopic = value
		case "user_stance":
			if isJSONNull(value) || json.Unmarshal(value, &in.UserStance) != nil {
				return ErrInvalidRequest
			}
		case "ai_style":
			in.AIStyle = value
		default:
			in.unknown = append(in.unknown, key)
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return ErrInvalidRequest
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return ErrInvalidRequest
	}
	return nil
}

// customTopicRequest 只接受 title 与 background 两个字段；
// 未知字段、重复字段与 null 值在 parse 阶段直接拒绝。
type customTopicRequest struct {
	Title      string `json:"title"`
	Background string `json:"background"`
}

// parseCreateRequest 把原始请求严格解析为 CreateInput。
//
// 二选一以“字段是否出现”为准：两个字段同时出现、都缺失、或任一显式 null 均为 400。
// 自定义题目的 title 必填为字符串，background 可省略或为空字符串。
func parseCreateRequest(in createRequest) (CreateInput, error) {
	out := CreateInput{UserStance: in.UserStance}
	hasTopic := len(in.TopicID) > 0
	hasCustom := len(in.CustomTopic) > 0
	if hasTopic == hasCustom {
		return out, ErrInvalidRequest
	}
	if hasCustom {
		// 自定义分支是新入口：顶层未知键与重复键都严格拒绝，
		// 不能让客户端借额外字段混入赛制、轮次或归属。
		if len(in.unknown) > 0 || len(in.duplicate) > 0 {
			return out, ErrInvalidRequest
		}
	}
	if hasTopic {
		var raw string
		if isJSONNull(in.TopicID) || json.Unmarshal(in.TopicID, &raw) != nil {
			return out, ErrInvalidRequest
		}
		id, err := ParseID(raw)
		if err != nil {
			return out, ErrInvalidRequest
		}
		out.TopicID = &id
	} else {
		if isJSONNull(in.CustomTopic) {
			return out, ErrInvalidRequest
		}
		custom, err := decodeCustomTopic(in.CustomTopic)
		if err != nil {
			return out, err
		}
		out.CustomTopic = custom
	}
	style := "logical"
	if len(in.AIStyle) > 0 {
		// 显式 null、空串、非字符串、大小写不符与未知值都是 400，不静默回退默认值。
		if isJSONNull(in.AIStyle) || json.Unmarshal(in.AIStyle, &style) != nil || !ai.AIStyle(style).Valid() {
			return out, ErrInvalidRequest
		}
	}
	out.AIStyle = style
	return out, nil
}

// decodeCustomTopic 严格解码自定义辩题对象：拒绝未知键、重复键、null 与类型不符。
func decodeCustomTopic(raw json.RawMessage) (*CustomTopic, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return nil, ErrInvalidRequest
	}
	seen := map[string]bool{}
	out := &CustomTopic{}
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return nil, ErrInvalidRequest
		}
		key, ok := token.(string)
		if !ok || seen[key] {
			return nil, ErrInvalidRequest
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil || isJSONNull(value) {
			return nil, ErrInvalidRequest
		}
		switch key {
		case "title":
			if json.Unmarshal(value, &out.Title) != nil {
				return nil, ErrInvalidRequest
			}
		case "background":
			if json.Unmarshal(value, &out.Background) != nil {
				return nil, ErrInvalidRequest
			}
		default:
			return nil, ErrInvalidRequest
		}
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') {
		return nil, ErrInvalidRequest
	}
	if _, err = decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrInvalidRequest
	}
	if !seen["title"] {
		return nil, ErrInvalidRequest
	}
	return out, nil
}

// isJSONNull 判断原始 JSON 值是否为显式 null（含前后空白）。
func isJSONNull(raw json.RawMessage) bool { return string(bytes.TrimSpace(raw)) == "null" }

func (h *Handler) create(c *gin.Context) {
	var in createRequest
	if !decode(c, &in) {
		return
	}
	input, e := parseCreateRequest(in)
	if e != nil {
		serviceError(c, e)
		return
	}
	current, ok := auth.CurrentUserFrom(c)
	if !ok {
		writeError(c, 401, "unauthenticated", nil)
		return
	}
	d, e := h.service.Create(c.Request.Context(), current.User.ID, input)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, 201, d.DTO())
}
func (h *Handler) list(c *gin.Context) {
	query, e := url.ParseQuery(c.Request.URL.RawQuery)
	if e != nil {
		serviceError(c, ErrInvalidRequest)
		return
	}
	page, pageSize, e := ParsePagination(query["page"], query["page_size"])
	if e != nil {
		serviceError(c, e)
		return
	}
	current, ok := auth.CurrentUserFrom(c)
	if !ok {
		writeError(c, 401, "unauthenticated", nil)
		return
	}
	list, e := h.service.List(c.Request.Context(), current.User.ID, page, pageSize)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, 200, list.DTO())
}
func (h *Handler) detail(c *gin.Context) {
	id, e := ParseID(c.Param("id"))
	if e != nil {
		serviceError(c, e)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	d, e := h.service.Detail(c.Request.Context(), current.User.ID, id)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, 200, d.DTO())
}
func (h *Handler) submit(c *gin.Context) {
	id, e := ParseID(c.Param("id"))
	if e != nil {
		serviceError(c, e)
		return
	}
	var in SubmitInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, status, e := h.service.submit(c.Request.Context(), current.User.ID, id, in)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, status, data)
}
func (h *Handler) retry(c *gin.Context) {
	id, e := ParseID(c.Param("id"))
	if e != nil {
		serviceError(c, e)
		return
	}
	turnID, e := ParseID(c.Param("turn_id"))
	if e != nil {
		serviceError(c, e)
		return
	}
	var in RetryInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, status, e := h.service.retry(c.Request.Context(), current.User.ID, id, turnID, in)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, status, data)
}
func (h *Handler) summary(c *gin.Context) {
	id, e := ParseID(c.Param("id"))
	if e != nil {
		serviceError(c, e)
		return
	}
	var in SummaryInput
	if !decode(c, &in) {
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, status, e := h.service.summary(c.Request.Context(), current.User.ID, id, in)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, status, data)
}
func (h *Handler) getSummary(c *gin.Context) {
	id, e := ParseID(c.Param("id"))
	if e != nil {
		serviceError(c, e)
		return
	}
	current, _ := auth.CurrentUserFrom(c)
	data, e := h.service.GetSummary(c.Request.Context(), current.User.ID, id)
	if e != nil {
		serviceError(c, e)
		return
	}
	writeData(c, 200, data)
}
func serviceError(c *gin.Context, e error) {
	var f *Failure
	if errors.As(e, &f) {
		if f.RetryAfter > 0 {
			c.Header("Retry-After", strconv.Itoa(f.RetryAfter))
		}
		writeError(c, f.Status, f.Code, f.Details)
		return
	}
	status, code := 500, "internal_error"
	switch {
	case errors.Is(e, ErrInvalidRequest):
		status, code = 400, "invalid_request"
	case errors.Is(e, ErrNotFound):
		status, code = 404, "not_found"
	case errors.Is(e, ErrRoundConflict):
		status, code = 409, "round_conflict"
	case errors.Is(e, ErrIdempotencyConflict):
		status, code = 409, "idempotency_conflict"
	case errors.Is(e, ErrAttemptVersionConflict):
		status, code = 409, "attempt_version_conflict"
	case errors.Is(e, ErrDependencyUnavailable):
		status, code = 503, "dependency_unavailable"
	}
	writeError(c, status, code, nil)
}
func writeData(c *gin.Context, status int, data any) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"data": data, "request_id": c.GetString("request_id")})
}
func writeError(c *gin.Context, status int, code string, details *FailureDetails) {
	c.Header("Cache-Control", "no-store")
	message := "请求未完成，请查看会话状态后再操作"
	if code == "not_found" {
		message = "资源不存在"
	}
	err := gin.H{"code": code, "message": message}
	if details != nil {
		err["details"] = details
	}
	c.AbortWithStatusJSON(status, gin.H{"error": err, "request_id": c.GetString("request_id")})
}
