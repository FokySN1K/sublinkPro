package routers

import (
	"sublink/api"
	"sublink/middlewares"

	"github.com/gin-gonic/gin"
)

// Share 注册分享管理路由
func Share(r *gin.Engine) {
	shareGroup := r.Group("/api/v1/shares")
	shareGroup.Use(middlewares.AuthToken)
	{
		shareGroup.GET("/get", api.ShareGet)                                     // 获取订阅的所有分享（支持分页）
		shareGroup.POST("/add", api.ShareAdd)                                    // 创建新分享
		shareGroup.POST("/update", api.ShareUpdate)                              // 更新分享
		shareGroup.DELETE("/delete", api.ShareDelete)                            // 删除分享
		shareGroup.POST("/refresh", api.ShareRefreshToken)                       // 刷新Token
		shareGroup.GET("/logs", api.ShareLogs)                                   // 获取分享访问日志
		shareGroup.GET("/devices", api.ShareDeviceList)                          // 获取分享绑定的设备
		shareGroup.GET("/devices/overview", api.ShareDeviceOverview)             // 设备遥测总览（按安装聚合）
		shareGroup.GET("/devices/detail", api.ShareDeviceDetail)                 // 单个设备的用量时间分布与访问记录
		shareGroup.POST("/devices/update", api.ShareDeviceUpdate)                // 更新设备
		shareGroup.DELETE("/devices/delete", api.ShareDeviceDelete)              // 解绑设备
		shareGroup.DELETE("/devices/clear", api.ShareDeviceClear)                // 清空分享下的全部设备
		shareGroup.GET("/subscriptions/overview", api.ShareSubscriptionOverview) // 订阅链接遥测总览（含用户归属）
		shareGroup.GET("/subscriptions/detail", api.ShareSubscriptionDetail)     // 单个订阅链接的会话、按日流量、节点与可靠性
		shareGroup.GET("/clients", api.ShareClientList)                          // 用户（客户端）列表
		shareGroup.POST("/clients/add", api.ShareClientAdd)                      // 新建用户
		shareGroup.POST("/clients/update", api.ShareClientUpdate)                // 更新用户
		shareGroup.DELETE("/clients/delete", api.ShareClientDelete)              // 删除用户（卡片归属置空）
		shareGroup.POST("/clients/assign", api.ShareClientAssign)                // 将订阅卡片归属到用户
		shareGroup.GET("/activity", api.ShareActivity)                           // 获取分享访问记录
		shareGroup.POST("/batch-add", api.ShareBatchAdd)                         // 批量创建分享
		shareGroup.POST("/batch-delete", api.ShareBatchDelete)                   // 批量删除分享
		shareGroup.POST("/batch-update", api.ShareBatchUpdate)                   // 批量更新分享
	}
}
