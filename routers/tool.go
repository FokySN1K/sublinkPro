package routers

import (
	"sublink/api"
	"sublink/middlewares"

	"github.com/gin-gonic/gin"
)

// Tool 注册工具类路由
func Tool(r *gin.Engine) {
	toolGroup := r.Group("/api/v1/tools")
	toolGroup.Use(middlewares.AuthToken)
	{
		toolGroup.POST("/happ/decrypt", api.HappDecrypt) // 解密 happ:// 链接
	}
}
