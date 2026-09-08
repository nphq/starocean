package shared

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

type PageResult[T any] struct {
	Items []T   `json:"items"`
	Total int64 `json:"total"`
	Page  int32 `json:"page"`
}

func JSONOK(c *gin.Context, v any) {
	c.JSON(http.StatusOK, v)
}

func JSONCreated(c *gin.Context, v any) {
	c.JSON(http.StatusCreated, v)
}

func JSONError(c *gin.Context, status int, msg string) {
	c.JSON(status, gin.H{"error": msg})
}

func JSONBadRequest(c *gin.Context, msg string) {
	JSONError(c, http.StatusBadRequest, msg)
}

func JSONNotFound(c *gin.Context, msg string) {
	JSONError(c, http.StatusNotFound, msg)
}

func JSONInternal(c *gin.Context, err error) {
	// P0: 不向客户端泄漏内部错误细节（SQL/路径/驱动信息），统一返回通用文案，
	// 根因在此单点记录（Logger 中间件只记状态码，无法定位 500 成因）。
	// 调用方如需面向用户的业务错误，请用 JSONBadRequest/JSONError。
	if err != nil {
		log.Printf("[ERROR] %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	}
	c.JSON(http.StatusInternalServerError, gin.H{"error": "内部服务器错误，请稍后重试"})
}

func BindJSON(c *gin.Context, dest any) bool {
	if err := c.ShouldBindJSON(dest); err != nil {
		JSONBadRequest(c, "请求体无效")
		return false
	}
	return true
}

func EmptySlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
