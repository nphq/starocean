package server

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func LoggerMiddleware() gin.HandlerFunc {
	return gin.LoggerWithFormatter(func(param gin.LogFormatterParams) string {
		return fmt.Sprintf("%s %s %s %s\n",
			param.Method,
			param.Path,
			param.Latency.Round(param.Latency).String(),
			strconv.Itoa(param.StatusCode),
		)
	})
}

func RecoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered interface{}) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "内部服务器错误"})
		c.Abort()
	})
}

// SecurityHeadersMiddleware P0: 基础安全头。frame-ancestors 防点击劫持
// （打印页仍同源可用），nosniff 防 MIME 嗅探；SPA 需要 inline script/style，
// 故 CSP 保持宽松 report-only 思路，这里只加非破坏性头。
func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("Referrer-Policy", "same-origin")
		c.Next()
	}
}
