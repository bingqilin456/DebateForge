package platform

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/bingqilin456/DebateForge/backend/internal/auth"
)

// NewRouter 不使用默认日志与 Recovery，避免记录原始路径、查询参数和 panic 堆栈。
// authHandler 为可选参数：既有两参调用保持健康检查行为，真实 main 必须传入以注册认证路由。
func NewRouter(logger *slog.Logger, database DatabasePinger, authHandler ...*auth.Handler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	// 本地直连只信任 socket 对端，不接受外部伪造的代理地址头。
	_ = router.SetTrustedProxies(nil)
	router.HandleMethodNotAllowed = true
	router.Use(requestBudget(logger))
	router.Use(func(c *gin.Context) {
		id := rand.Text()
		c.Header("X-Request-ID", id)
		c.Set("request_id", id)
		started := time.Now()
		defer func() {
			if recover() != nil {
				logger.Error("请求处理异常", "request_id", id, "error_code", "internal_error")
				writeError(c, http.StatusInternalServerError, "internal_error", "服务暂时不可用")
			}
			route := c.FullPath()
			if route == "" {
				route = "<unmatched>"
			}
			logger.Info("HTTP 请求", "request_id", id, "method", c.Request.Method,
				"route", route, "status", c.Writer.Status(), "latency_ms", time.Since(started).Milliseconds())
		}()
		c.Next()
	})

	apiGroup := router.Group("/api")
	apiGroup.GET("/health/live", live)
	apiGroup.GET("/health/ready", ready(database))
	// 只有真实入口传入 Handler 时才挂载；业务路由一律带 no-store 与 request_id 信封。
	if len(authHandler) > 0 && authHandler[0] != nil {
		authHandler[0].RegisterRoutes(apiGroup.Group("/v1/auth"))
	}
	router.NoRoute(func(c *gin.Context) {
		writeError(c, http.StatusNotFound, "not_found", "接口不存在")
	})
	router.NoMethod(func(c *gin.Context) {
		writeError(c, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不支持")
	})
	return router
}

func writeError(c *gin.Context, status int, code, message string) {
	// 业务与路由层错误都禁止缓存，避免代理或浏览器复用错误响应。
	c.Header("Cache-Control", "no-store")
	c.AbortWithStatusJSON(status, gin.H{
		"error":      gin.H{"code": code, "message": message},
		"request_id": c.GetString("request_id"),
	})
}
