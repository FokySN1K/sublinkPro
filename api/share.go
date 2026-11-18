package api

import (
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sublink/models"
	"sublink/utils"
	"time"

	"github.com/gin-gonic/gin"
)

// ShareListReq 获取分享列表请求
type ShareListReq struct {
	SubID     int    `form:"subId" binding:"required"`
	GroupID   int    `form:"groupId"`   // 可选，仅返回该分享分组下的链接
	Page      int    `form:"page"`      // 可选，分页页码
	PageSize  int    `form:"pageSize"`  // 可选，每页数量
	Keyword   string `form:"keyword"`   // 可选，搜索关键词
	IPFilter  string `form:"ipFilter"`  // 可选，按IP筛选
	SortBy    string `form:"sortBy"`    // 可选，排序字段(如: access_count, name)
	SortOrder string `form:"sortOrder"` // 可选，排序方向(asc/desc)
}

// ShareBatchCreateReq 批量创建分享请求
type ShareBatchCreateReq struct {
	SubscriptionID int    `json:"subscription_id" binding:"required"`
	BaseName       string `json:"base_name" binding:"required"`
	Count          int    `json:"count" binding:"required,min=1,max=100"`
	GroupID        *int   `json:"group_id"`
	ExpireType     int    `json:"expire_type"`
	ExpireDays     int    `json:"expire_days"`
	ExpireAt       string `json:"expire_at"`
	Enabled        bool   `json:"enabled"`
}

// ShareBatchDeleteReq 批量删除分享请求
type ShareBatchDeleteReq struct {
	IDs []int `json:"ids" binding:"required,min=1"`
}

// ShareBatchUpdateReq 批量更新分享请求
type ShareBatchUpdateReq struct {
	IDs        []int  `json:"ids" binding:"required,min=1"`
	Enabled    *bool  `json:"enabled"` // 指针允许 null
	ExpireType *int   `json:"expire_type"`
	ExpireDays *int   `json:"expire_days"`
	ExpireAt   string `json:"expire_at"`
}

// ShareCreateReq 创建分享请求
type ShareCreateReq struct {
	SubscriptionID int    `json:"subscription_id" binding:"required"`
	Name           string `json:"name"`
	Token          string `json:"token"` // 可选，为空则自动生成
	GroupID        *int   `json:"group_id"`

	// Expiry is optional: when a group is selected and no expiry is sent the
	// link inherits the group snapshot.
	ExpireType *int   `json:"expire_type"`
	ExpireDays *int   `json:"expire_days"`
	ExpireAt   string `json:"expire_at"` // ISO格式日期时间字符串

	TrafficLimitBytes *int64 `json:"traffic_limit_bytes"`
	MaxDevices        *int   `json:"max_devices"`

	ReportIntervalSessions    *int `json:"report_interval_sessions"`
	ReportIntervalNodes       *int `json:"report_interval_nodes"`
	ReportIntervalReliability *int `json:"report_interval_reliability"`
}

// ShareUpdateReq 更新分享请求
type ShareUpdateReq struct {
	ID         int    `json:"id" binding:"required"`
	Name       string `json:"name"`
	Token      string `json:"token"`
	ExpireType int    `json:"expire_type"`
	ExpireDays int    `json:"expire_days"`
	ExpireAt   string `json:"expire_at"`
	Enabled    bool   `json:"enabled"`

	// GroupID is nil when unchanged and 0 when the link must be detached.
	GroupID *int `json:"group_id"`

	TrafficLimitBytes *int64 `json:"traffic_limit_bytes"`
	MaxDevices        *int   `json:"max_devices"`

	ReportIntervalSessions    *int `json:"report_interval_sessions"`
	ReportIntervalNodes       *int `json:"report_interval_nodes"`
	ReportIntervalReliability *int `json:"report_interval_reliability"`
}

// applyShareGroupSnapshot copies group policy into a new link. It rejects
// groups that serve a different node pool.
func applyShareGroupSnapshot(share *models.SubscriptionShare, groupID *int, subscriptionID int) error {
	if groupID == nil || *groupID <= 0 {
		return nil
	}
	group, err := models.GetShareGroupByID(*groupID)
	if err != nil {
		return fmt.Errorf("share group not found")
	}
	if group.SubscriptionID != subscriptionID {
		return fmt.Errorf("the selected group serves another node pool")
	}
	share.ApplyGroupSnapshot(group)
	return nil
}

func parseShareExpireAt(expireType int, raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if expireType != models.ExpireTypeDateTime {
		return nil, nil
	}
	if raw == "" {
		return nil, fmt.Errorf("an expiry time is required when expiring at a specific time")
	}

	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		parsed, err = time.ParseInLocation("2006-01-02 15:04:05", raw, time.Local)
		if err != nil {
			parsed, err = time.ParseInLocation("2006-01-02T15:04", raw, time.Local)
			if err != nil {
				return nil, fmt.Errorf("invalid expiry time format")
			}
		}
	}

	return &parsed, nil
}

// ShareGet 获取订阅的所有分享列表（支持分页、搜索、IP筛选、排序）
func ShareGet(c *gin.Context) {
	var req ShareListReq
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}

	// 如果提供了IP筛选，验证IP格式
	if req.IPFilter != "" {
		ip := net.ParseIP(req.IPFilter)
		if ip == nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid IP address"})
			return
		}
	}

	// 如果提供了分页参数，返回分页数据
	if req.Page > 0 && req.PageSize > 0 {
		var (
			shares []models.SubscriptionShare
			total  int
			err    error
		)
		if req.GroupID > 0 {
			shares, total, err = models.GetSharesByGroupIDPaginated(
				req.GroupID, req.Page, req.PageSize, req.Keyword, req.IPFilter, req.SortBy, req.SortOrder)
		} else {
			shares, total, err = models.GetSharesBySubscriptionIDPaginated(
				req.SubID, req.Page, req.PageSize, req.Keyword, req.IPFilter, req.SortBy, req.SortOrder)
		}
		if err != nil {
			utils.Error("Failed to load share list: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to fetch share list"})
			return
		}

		totalPages := (total + req.PageSize - 1) / req.PageSize
		hasMore := req.Page < totalPages

		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"data": gin.H{
				"items":      shares,
				"total":      total,
				"page":       req.Page,
				"pageSize":   req.PageSize,
				"totalPages": totalPages,
				"hasMore":    hasMore,
			},
		})
		return
	}

	// 不分页，返回所有分享（向后兼容，也支持搜索）
	if req.GroupID > 0 {
		shares := models.GetSharesByGroupID(req.GroupID)
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": shares})
		return
	}
	shares := models.GetSharesBySubscriptionID(req.SubID, req.Keyword)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": shares})
}

// randomShareName builds a short random default link name.
func randomShareName() string {
	token, err := models.GenerateToken()
	if err != nil || len(token) < 6 {
		return "link"
	}
	return "link-" + token[:6]
}

// ShareAdd 创建新分享
func ShareAdd(c *gin.Context) {
	var req ShareCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}

	var pool models.Subcription
	pool.ID = req.SubscriptionID
	if err := pool.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Node pool not found"})
		return
	}

	share := &models.SubscriptionShare{
		SubscriptionID: req.SubscriptionID,
		Name:           strings.TrimSpace(req.Name),
		Token:          strings.TrimSpace(req.Token),
		Enabled:        true,
	}
	if err := applyShareGroupSnapshot(share, req.GroupID, req.SubscriptionID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}

	// Explicit request values override the inherited snapshot.
	if req.ExpireType != nil {
		expireAt, err := parseShareExpireAt(*req.ExpireType, req.ExpireAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		share.ExpireType = *req.ExpireType
		share.ExpireDays = 0
		if req.ExpireDays != nil {
			share.ExpireDays = *req.ExpireDays
		}
		share.ExpireAt = expireAt
	}
	if req.TrafficLimitBytes != nil {
		share.TrafficLimitBytes = *req.TrafficLimitBytes
	}
	if req.MaxDevices != nil {
		share.MaxDevices = *req.MaxDevices
	}
	if req.ReportIntervalSessions != nil {
		share.ReportIntervalSessions = models.NormalizeReportInterval(*req.ReportIntervalSessions)
	}
	if req.ReportIntervalNodes != nil {
		share.ReportIntervalNodes = models.NormalizeReportInterval(*req.ReportIntervalNodes)
	}
	if req.ReportIntervalReliability != nil {
		share.ReportIntervalReliability = models.NormalizeReportInterval(*req.ReportIntervalReliability)
	}

	// Random defaults for name and token; both stay editable later.
	if share.Token == "" {
		token, err := models.GenerateToken()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to generate token"})
			return
		}
		share.Token = token
	}
	if share.Name == "" {
		share.Name = randomShareName()
	}

	if err := share.Add(); err != nil {
		utils.Error("Failed to create share: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Created successfully", "data": share})
}

// ShareUpdate 更新分享设置
func ShareUpdate(c *gin.Context) {
	var req ShareUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}

	// 获取现有分享
	share := &models.SubscriptionShare{ID: req.ID}
	if err := share.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Share not found"})
		return
	}

	// Group move: nil keeps the current group, 0 detaches, >0 switches and
	// re-applies the group snapshot before explicit overrides. Re-sending the
	// current group is a no-op so per-link overrides survive a normal edit.
	if req.GroupID != nil {
		groupID := *req.GroupID
		currentGroupID := 0
		if share.GroupID != nil {
			currentGroupID = *share.GroupID
		}
		if groupID > 0 && groupID != currentGroupID {
			group, err := models.GetShareGroupByID(groupID)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Group not found"})
				return
			}
			if group.SubscriptionID != share.SubscriptionID {
				c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "The selected group serves another node pool"})
				return
			}
			share.ApplyGroupSnapshot(group)
		} else if groupID <= 0 {
			share.GroupID = nil
		}
	}

	expireAt, err := parseShareExpireAt(req.ExpireType, req.ExpireAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}

	// 更新字段
	share.Name = strings.TrimSpace(req.Name)
	share.Token = strings.TrimSpace(req.Token)
	share.ExpireType = req.ExpireType
	share.ExpireDays = req.ExpireDays
	share.ExpireAt = expireAt
	share.Enabled = req.Enabled
	if req.TrafficLimitBytes != nil {
		share.TrafficLimitBytes = *req.TrafficLimitBytes
	}
	if req.MaxDevices != nil {
		share.MaxDevices = *req.MaxDevices
	}
	if req.ReportIntervalSessions != nil {
		share.ReportIntervalSessions = models.NormalizeReportInterval(*req.ReportIntervalSessions)
	}
	if req.ReportIntervalNodes != nil {
		share.ReportIntervalNodes = models.NormalizeReportInterval(*req.ReportIntervalNodes)
	}
	if req.ReportIntervalReliability != nil {
		share.ReportIntervalReliability = models.NormalizeReportInterval(*req.ReportIntervalReliability)
	}

	if err := share.Update(); err != nil {
		utils.Error("Failed to update share: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Updated successfully"})
}

// cleanupShareRelations removes data that belongs to a deleted link.
func cleanupShareRelations(shareID int) {
	if err := models.DeleteShareDevices(shareID); err != nil {
		utils.Warn("Failed to delete devices of share %d: %v", shareID, err)
	}
	if err := models.DeleteShareAccessLogs(shareID); err != nil {
		utils.Warn("Failed to delete access logs of share %d: %v", shareID, err)
	}
	if err := models.SetShareSubscriptionStubs(shareID, nil); err != nil {
		utils.Warn("Failed to delete stub overrides of share %d: %v", shareID, err)
	}
}

// ShareDelete 删除分享
func ShareDelete(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid share ID"})
		return
	}

	share := &models.SubscriptionShare{ID: id}
	if err := share.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Share not found"})
		return
	}

	// 禁止删除默认分享链接
	if share.IsLegacy {
		c.JSON(http.StatusForbidden, gin.H{"code": 403, "msg": "The default share link cannot be deleted; if it leaked, use the refresh token feature"})
		return
	}

	if err := share.Delete(); err != nil {
		utils.Error("Failed to delete share: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Delete failed"})
		return
	}
	cleanupShareRelations(share.ID)

	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Deleted successfully"})
}

// ShareRefreshToken 刷新分享Token
func ShareRefreshToken(c *gin.Context) {
	idStr := c.Query("id")
	id, err := strconv.Atoi(idStr)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid share ID"})
		return
	}

	share := &models.SubscriptionShare{ID: id}
	if err := share.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Share not found"})
		return
	}

	// 生成新Token
	newToken, err := models.GenerateToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to generate token"})
		return
	}

	share.Token = newToken
	if err := share.Update(); err != nil {
		utils.Error("Failed to refresh token: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Token refreshed", "data": gin.H{"token": newToken}})
}

// ShareLogs 获取分享的访问日志
func ShareLogs(c *gin.Context) {
	shareIdStr := c.Query("shareId")
	shareId, err := strconv.Atoi(shareIdStr)
	if err != nil || shareId <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid share ID"})
		return
	}

	logs := models.GetSubLogsByShareID(shareId)
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": logs})
}

// ShareBatchAdd 批量创建分享
func ShareBatchAdd(c *gin.Context) {
	var req ShareBatchCreateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}

	// 验证订阅是否存在
	var sub models.Subcription
	sub.ID = req.SubscriptionID
	if err := sub.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Subscription not found"})
		return
	}

	// 解析过期时间
	expireAt, err := parseShareExpireAt(req.ExpireType, req.ExpireAt)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}

	// 检查名称冲突
	existingShares := models.GetSharesBySubscriptionID(req.SubscriptionID)
	existingNames := make(map[string]bool)
	for _, s := range existingShares {
		existingNames[s.Name] = true
	}

	var conflictNames []string
	for i := 1; i <= req.Count; i++ {
		name := fmt.Sprintf("%s-%d", req.BaseName, i)
		if existingNames[name] {
			conflictNames = append(conflictNames, name)
		}
	}

	if len(conflictNames) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 400,
			"msg":  fmt.Sprintf("The following names already exist: %s", strings.Join(conflictNames, ", ")),
		})
		return
	}

	// 开始批量创建
	createdShares := make([]models.SubscriptionShare, 0, req.Count)
	hasExplicitExpiry := req.ExpireType != 0 || strings.TrimSpace(req.ExpireAt) != ""
	for i := 1; i <= req.Count; i++ {
		name := fmt.Sprintf("%s-%d", req.BaseName, i)

		// 生成唯一token
		token, err := models.GenerateToken()
		if err != nil {
			utils.Error("Failed to generate token: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to generate token"})
			return
		}

		// 确保token唯一（极少情况下会冲突）
		retries := 0
		for models.IsTokenExists(token, 0) && retries < 3 {
			token, err = models.GenerateToken()
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to generate token"})
				return
			}
			retries++
		}

		share := &models.SubscriptionShare{
			SubscriptionID: req.SubscriptionID,
			Name:           name,
			Token:          token,
			Enabled:        req.Enabled,
			IsLegacy:       false,
		}
		if err := applyShareGroupSnapshot(share, req.GroupID, req.SubscriptionID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
		if req.GroupID == nil || hasExplicitExpiry {
			share.ExpireType = req.ExpireType
			share.ExpireDays = req.ExpireDays
			share.ExpireAt = expireAt
		}

		if err := share.Add(); err != nil {
			utils.Error("Failed to create share [%s]: %v", name, err)
			c.JSON(http.StatusInternalServerError, gin.H{
				"code": 500,
				"msg":  fmt.Sprintf("Failed to create share: %s (%s)", name, err.Error()),
			})
			return
		}

		createdShares = append(createdShares, *share)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  fmt.Sprintf("Created %d shares", len(createdShares)),
		"data": createdShares,
	})
}

// ShareBatchDelete 批量删除分享
func ShareBatchDelete(c *gin.Context) {
	var req ShareBatchDeleteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}

	// 验证所有ID并检查是否为legacy
	var legacyNames []string
	for _, id := range req.IDs {
		share := &models.SubscriptionShare{ID: id}
		if err := share.Find(); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": fmt.Sprintf("Share ID %d not found", id)})
			return
		}
		if share.IsLegacy {
			legacyNames = append(legacyNames, share.Name)
		}
	}

	if len(legacyNames) > 0 {
		c.JSON(http.StatusForbidden, gin.H{
			"code": 403,
			"msg":  fmt.Sprintf("The following default share links cannot be deleted: %s", strings.Join(legacyNames, ", ")),
		})
		return
	}

	// 批量删除
	successCount := 0
	var errors []string
	for _, id := range req.IDs {
		share := &models.SubscriptionShare{ID: id}
		if err := share.Find(); err == nil {
			if err := share.Delete(); err != nil {
				errors = append(errors, fmt.Sprintf("ID %d: %s", id, err.Error()))
			} else {
				cleanupShareRelations(id)
				successCount++
			}
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"msg":  fmt.Sprintf("Deleted %d of %d shares; failures: %s", successCount, len(req.IDs), strings.Join(errors, "; ")),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  fmt.Sprintf("Deleted %d shares", successCount),
	})
}

// ShareBatchUpdate 批量更新分享
func ShareBatchUpdate(c *gin.Context) {
	var req ShareBatchUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}

	// 验证所有ID是否存在
	shares := make([]*models.SubscriptionShare, 0, len(req.IDs))
	for _, id := range req.IDs {
		share := &models.SubscriptionShare{ID: id}
		if err := share.Find(); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": fmt.Sprintf("Share ID %d not found", id)})
			return
		}
		shares = append(shares, share)
	}

	// 解析过期时间（如果提供了ExpireType）
	var expireAt *time.Time
	if req.ExpireType != nil {
		var err error
		expireAt, err = parseShareExpireAt(*req.ExpireType, req.ExpireAt)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
			return
		}
	}

	// 批量更新
	successCount := 0
	var errors []string
	for _, share := range shares {
		// 只更新提供的字段
		if req.Enabled != nil {
			share.Enabled = *req.Enabled
		}
		if req.ExpireType != nil {
			share.ExpireType = *req.ExpireType
		}
		if req.ExpireDays != nil {
			share.ExpireDays = *req.ExpireDays
		}
		if expireAt != nil {
			share.ExpireAt = expireAt
		}

		if err := share.Update(); err != nil {
			errors = append(errors, fmt.Sprintf("ID %d: %s", share.ID, err.Error()))
		} else {
			successCount++
		}
	}

	if len(errors) > 0 {
		c.JSON(http.StatusOK, gin.H{
			"code": 200,
			"msg":  fmt.Sprintf("Updated %d of %d shares; failures: %s", successCount, len(req.IDs), strings.Join(errors, "; ")),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  fmt.Sprintf("Updated %d shares", successCount),
	})
}
