package platform

import (
	"crypto/rand"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// NewRouter 不使用默认日志与 Recovery，避免记录原始路径、查询参数和 panic 堆栈。
func NewRouter(logger *slog.Logger) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	// 本地直连只信任 socket 对端，不接受外部伪造的代理地址头。
	_ = router.SetTrustedProxies(nil)
	router.HandleMethodNotAllowed = true
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
	router.GET("/health/live", live)
	router.NoRoute(func(c *gin.Context) {
		writeError(c, http.StatusNotFound, "not_found", "接口不存在")
	})
	router.NoMethod(func(c *gin.Context) {
		writeError(c, http.StatusMethodNotAllowed, "method_not_allowed", "请求方法不支持")
	})
	return router
}

func writeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error":      gin.H{"code": code, "message": message},
		"request_id": c.GetString("request_id"),
	})
}
