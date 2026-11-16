package platform

import (
	"context"
	"errors"
	"github.com/bingqilin456/DebateForge/backend/internal/ai"
	"github.com/bingqilin456/DebateForge/backend/internal/auth"
	"github.com/bingqilin456/DebateForge/backend/internal/debate"
	"github.com/bingqilin456/DebateForge/backend/internal/topic"
	"github.com/gin-gonic/gin"
	"log/slog"
	"net/http"
	"time"
)

// HTTPWriteTimeout 为 socket 提供兜底；路由入口使用更短的实际请求期限。
const HTTPWriteTimeout = 65 * time.Second
const OrdinaryRequestBudget = 5 * time.Second

func NewBusinessRouter(logger *slog.Logger, db DatabasePinger, a *auth.Handler, t *topic.Handler, d *debate.Handler) *gin.Engine {
	router := NewRouter(logger, db, a)
	group := router.Group("/api/v1")
	if t != nil {
		t.RegisterRoutes(group, a)
	}
	if d != nil {
		d.RegisterRoutes(group, a)
	}
	return router
}

// requestBudget 必须早于认证和请求体读取。Gin 已匹配路由，客户端不能用伪造后缀取得长预算。
func requestBudget(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		budget := OrdinaryRequestBudget
		if c.Request.Method == http.MethodPost && (c.FullPath() == "/api/v1/debates/:id/turns" || c.FullPath() == "/api/v1/debates/:id/turns/stream" || c.FullPath() == "/api/v1/debates/:id/turns/:turn_id/retry" || c.FullPath() == "/api/v1/debates/:id/summary" || c.FullPath() == "/api/v1/debates/:id/strategy") {
			budget = ai.DefaultRequestBudget
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), budget)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		deadline, _ := ctx.Deadline()
		e := http.NewResponseController(c.Writer).SetWriteDeadline(deadline)
		// Recorder 不支持 socket 期限；真实 net/http writer 支持，不忽略其他失败。
		if e != nil && !errors.Is(e, http.ErrNotSupported) {
			logger.Error("HTTP 写期限设置失败", "error_code", "internal_error")
			writeError(c, 500, "internal_error", "服务暂时不可用")
			return
		}
		c.Next()
	}
}
