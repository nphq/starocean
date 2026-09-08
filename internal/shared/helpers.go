package shared

import (
	"strconv"

	"github.com/gin-gonic/gin"
)

func GetPage(c *gin.Context) int32 {
	p, _ := strconv.ParseInt(c.DefaultQuery("page", "1"), 10, 32)
	if p < 1 {
		p = 1
	}
	return int32(p)
}
