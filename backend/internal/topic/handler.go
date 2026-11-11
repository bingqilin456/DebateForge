package topic

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
)

// TopicDTO 是契约中的 Topic：id 为十进制字符串，title/description 为原文。
type TopicDTO struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// ItemsDTO 是 TopicsResponse.data：空列表必须是 []，不能是 null。
type ItemsDTO struct {
	Items []TopicDTO `json:"items"`
}

// Handler 只负责协议：鉴权由传入的 auth.Handler 中间件提供，读请求不校验
// Origin/CSRF（契约中 GET 不要求），也不调用数据库。
type Handler struct {
	service *Service
}

// NewHandler 允许 service 为空：此时路由仍注册，但请求受控返回 503，
// 避免启动期因为可选依赖缺失而 panic。
func NewHandler(service *Service) *Handler { return &Handler{service: service} }

// RegisterRoutes 把 GET /api/v1/topics 挂到给定组上。
// authHandler 为 nil 时不加鉴权中间件，仅供 Handler 单元测试；
// 真实装配必须传入，否则 Handler 自身会以缺失身份返回 401。
func (h *Handler) RegisterRoutes(group *gin.RouterGroup, authHandler *auth.Handler) {
	if group == nil {
		return
	}
	middlewares := make([]gin.HandlerFunc, 0, 1)
	if authHandler != nil {
		middlewares = append(middlewares, authHandler.RequireAuth())
	}
	middlewares = append(middlewares, h.list)
	group.GET("/topics", middlewares...)
}

// list 读取当前用户后查询启用辩题。
func (h *Handler) list(c *gin.Context) {
	if _, ok := auth.CurrentUserFrom(c); !ok {
		writeError(c, http.StatusUnauthorized, "unauthenticated", "未登录或登录已失效")
		return
	}
	if h == nil || h.service == nil {
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "服务暂时不可用")
		return
	}
	topics, err := h.service.ListEnabled(c.Request.Context())
	if err != nil {
		writeServiceError(c, err)
		return
	}
	items := make([]TopicDTO, 0, len(topics))
	for _, topic := range topics {
		id, err := FormatEntityID(topic.ID)
		if err != nil {
			writeError(c, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
			return
		}
		items = append(items, TopicDTO{ID: id, Title: topic.Title, Description: topic.Description})
	}
	writeData(c, http.StatusOK, ItemsDTO{Items: items})
}

// writeServiceError 把领域错误映射为冻结契约的状态码与受控提示。
func writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrInvalidData):
		writeError(c, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	case errors.Is(err, ErrDependencyUnavailable):
		writeError(c, http.StatusServiceUnavailable, "dependency_unavailable", "服务暂时不可用")
	default:
		writeError(c, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
	}
}

// writeData 统一成功信封：data + request_id，并禁止缓存。
// request_id 由路由层中间件写入，缺失时输出空字符串而不是省略字段。
func writeData(c *gin.Context, status int, data any) {
	c.Header("Cache-Control", "no-store")
	c.JSON(status, gin.H{"data": data, "request_id": c.GetString("request_id")})
}

// writeError 统一错误信封：error{code,message} + request_id，并禁止缓存。
func writeError(c *gin.Context, status int, code, message string) {
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, gin.H{
		"error":      gin.H{"code": code, "message": message},
		"request_id": c.GetString("request_id"),
	})
}
