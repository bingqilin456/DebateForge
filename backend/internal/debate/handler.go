package debate

import (
	"bytes"
	"encoding/json"
	"errors"
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
	group.POST("/debates/:id/summary", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.summary)
	group.GET("/debates/:id/summary", a.RequireAuth(), h.getSummary)
	group.GET("/debates/:id/strategy", a.RequireAuth(), h.getStrategy)
	group.POST("/debates/:id/strategy", a.RequireOrigin(), a.RequireAuth(), a.RequireCSRF(), h.limitGeneration, h.strategy)
	group.GET("/debates/:id", a.RequireAuth(), h.detail)
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
func (h *Handler) create(c *gin.Context) {
	var in struct {
		TopicID    string          `json:"topic_id"`
		UserStance string          `json:"user_stance"`
		AIStyle    json.RawMessage `json:"ai_style"`
	}
	if !decode(c, &in) {
		return
	}
	style := "logical"
	if len(in.AIStyle) > 0 {
		if string(in.AIStyle) == "null" || json.Unmarshal(in.AIStyle, &style) != nil {
			serviceError(c, ErrInvalidRequest)
			return
		}
	}
	id, e := ParseID(in.TopicID)
	if e != nil {
		serviceError(c, e)
		return
	}
	current, ok := auth.CurrentUserFrom(c)
	if !ok {
		writeError(c, 401, "unauthenticated", nil)
		return
	}
	d, e := h.service.Create(c.Request.Context(), current.User.ID, id, in.UserStance, style)
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
