package routers

import (
	"sublink/api"
	"sublink/middlewares"

	"github.com/gin-gonic/gin"
)

// ShareGroup 注册订阅组路由
func ShareGroup(r *gin.Engine) {
	group := r.Group("/api/v1/share-groups")
	group.Use(middlewares.AuthToken)
	{
		group.GET("/list", api.ShareGroupList)          // 获取订阅组列表
		group.GET("/get", api.ShareGroupGet)            // 获取订阅组详情
		group.POST("/add", api.ShareGroupAdd)           // 创建订阅组
		group.POST("/update", api.ShareGroupUpdate)     // 更新订阅组
		group.DELETE("/delete", api.ShareGroupDelete)   // 删除订阅组
		group.GET("/stubs", api.ShareGroupStubsGet)     // 获取组附加的Заглушка映射
		group.POST("/stubs", api.ShareGroupStubsUpdate) // 更新组Заглушка映射
		group.GET("/activity", api.ShareGroupActivity)  // 获取组访问记录
	}
}

// ShareStub 注册Заглушка库路由
func ShareStub(r *gin.Engine) {
	group := r.Group("/api/v1/share-stubs")
	group.Use(middlewares.AuthToken)
	{
		group.GET("/list", api.ShareStubList)                      // 获取Заглушка列表
		group.POST("/add", api.ShareStubAdd)                       // 创建Заглушка
		group.POST("/update", api.ShareStubUpdate)                 // 更新Заглушка
		group.DELETE("/delete", api.ShareStubDelete)               // 删除Заглушка
		group.GET("/overrides", api.ShareSubscriptionStubsGet)     // 获取订阅链接的Заглушка覆盖
		group.POST("/overrides", api.ShareSubscriptionStubsUpdate) // 更新订阅链接的Заглушка覆盖
	}
}
