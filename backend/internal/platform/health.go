package platform

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
)

// live 只报告进程存活，不访问数据库或模型。
func live(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"status": "alive"})
}

// DatabasePinger 只提供健康检查需要的能力，避免 Handler 执行 SQL。
type DatabasePinger interface{ PingContext(context.Context) error }

func ready(database DatabasePinger) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(c.Request.Context(), DatabaseTimeout)
		defer cancel()
		if database == nil || database.PingContext(ctx) != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "not_ready"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	}
}
