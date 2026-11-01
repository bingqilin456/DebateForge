package platform

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// live 只报告进程存活，不把尚未接入的数据库或模型视为就绪。
func live(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"status": "alive"})
}
