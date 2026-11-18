package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"sublink/models"
	"sublink/utils"

	"github.com/gin-gonic/gin"
)

// ShareGroupReq is the create/update payload for a share group.
type ShareGroupReq struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`

	SubscriptionID int `json:"subscription_id"`

	Enabled    *bool `json:"enabled"`
	HideConfig *bool `json:"hide_config"`

	Templates  []string `json:"templates"`
	MihomoSnkp *bool    `json:"mihomo_snkp"`

	// ClashTemplate / SurgeTemplate select the rendered template file for links
	// served by this group; empty inherits the node pool default.
	ClashTemplate string `json:"clash_template"`
	SurgeTemplate string `json:"surge_template"`

	AllowLegacyClients *bool  `json:"allow_legacy_clients"`
	DeviceMode         string `json:"device_mode"`
	MaxDevices         int    `json:"max_devices"`

	TrafficLimitBytes int64 `json:"traffic_limit_bytes"`

	ExpireType int    `json:"expire_type"`
	ExpireDays int    `json:"expire_days"`
	ExpireAt   string `json:"expire_at"`

	TelemetryEnabled          *bool `json:"telemetry_enabled"`
	ReportIntervalSessions    int   `json:"report_interval_sessions"`
	ReportIntervalNodes       int   `json:"report_interval_nodes"`
	ReportIntervalReliability int   `json:"report_interval_reliability"`
}

type ShareGroupStubReq struct {
	GroupID int            `json:"group_id" binding:"required"`
	Stubs   map[string]int `json:"stubs"`
}

type shareGroupListItem struct {
	models.ShareGroup
	TemplateList         []string       `json:"template_list"`
	PoolName             string         `json:"pool_name"`
	ShareCount           int            `json:"share_count"`
	DeviceCount          int            `json:"device_count"`
	PendingDeviceCount   int            `json:"pending_device_count"`
	ReportedTrafficBytes int64          `json:"reported_traffic_bytes"`
	LastUsageAt          *time.Time     `json:"last_usage_at"`
	ExhaustedShareCount  int            `json:"exhausted_share_count"`
	Stubs                map[string]int `json:"stubs"`
}

func applyShareGroupReq(group *models.ShareGroup, req *ShareGroupReq, creating bool) error {
	group.Name = strings.TrimSpace(req.Name)
	group.Description = strings.TrimSpace(req.Description)
	group.SubscriptionID = req.SubscriptionID
	group.ClashTemplate = strings.TrimSpace(req.ClashTemplate)
	group.SurgeTemplate = strings.TrimSpace(req.SurgeTemplate)
	group.DeviceMode = models.NormalizeShareDeviceMode(req.DeviceMode)
	group.MaxDevices = req.MaxDevices
	group.TrafficLimitBytes = req.TrafficLimitBytes
	group.ExpireType = req.ExpireType
	group.ExpireDays = req.ExpireDays

	expireAt, err := parseShareExpireAt(req.ExpireType, req.ExpireAt)
	if err != nil {
		return err
	}
	group.ExpireAt = expireAt

	if creating {
		group.Templates = models.NormalizeShareTemplatesString(strings.Join(req.Templates, ","))
		if req.Enabled != nil {
			group.Enabled = *req.Enabled
		} else {
			group.Enabled = true
		}
		if req.HideConfig != nil {
			group.HideConfig = *req.HideConfig
		}
		if req.MihomoSnkp != nil {
			group.MihomoSnkp = *req.MihomoSnkp
		} else {
			group.MihomoSnkp = true
		}
		if req.AllowLegacyClients != nil {
			group.AllowLegacyClients = *req.AllowLegacyClients
		} else {
			group.AllowLegacyClients = true
		}
		if req.TelemetryEnabled != nil {
			group.TelemetryEnabled = *req.TelemetryEnabled
		}
	} else {
		if len(req.Templates) > 0 {
			group.Templates = models.NormalizeShareTemplatesString(strings.Join(req.Templates, ","))
		}
		if req.Enabled != nil {
			group.Enabled = *req.Enabled
		}
		if req.HideConfig != nil {
			group.HideConfig = *req.HideConfig
		}
		if req.MihomoSnkp != nil {
			group.MihomoSnkp = *req.MihomoSnkp
		}
		if req.AllowLegacyClients != nil {
			group.AllowLegacyClients = *req.AllowLegacyClients
		}
		if req.TelemetryEnabled != nil {
			group.TelemetryEnabled = *req.TelemetryEnabled
		}
	}

	group.ReportIntervalSessions = req.ReportIntervalSessions
	group.ReportIntervalNodes = req.ReportIntervalNodes
	group.ReportIntervalReliability = req.ReportIntervalReliability
	return nil
}

// ShareGroupList returns groups with delivery stats.
func ShareGroupList(c *gin.Context) {
	groups := models.ListShareGroups()
	items := make([]shareGroupListItem, 0, len(groups))
	for _, group := range groups {
		item := shareGroupListItem{
			ShareGroup:   group,
			TemplateList: group.EffectiveTemplates(),
			Stubs:        models.GetShareGroupStubMap(group.ID),
		}
		var pool models.Subcription
		pool.ID = group.SubscriptionID
		if err := pool.Find(); err == nil {
			item.PoolName = pool.Name
		}
		shares := models.GetSharesByGroupID(group.ID)
		item.ShareCount = len(shares)
		ids := make([]int, 0, len(shares))
		for _, share := range shares {
			ids = append(ids, share.ID)
			// Aggregate client-reported telemetry across the group's links.
			item.ReportedTrafficBytes += share.ReportedTrafficBytes
			if share.LastUsageAt != nil && (item.LastUsageAt == nil || share.LastUsageAt.After(*item.LastUsageAt)) {
				item.LastUsageAt = share.LastUsageAt
			}
			if share.TrafficLimitBytes > 0 && share.ReportedTrafficBytes >= share.TrafficLimitBytes {
				item.ExhaustedShareCount++
			}
		}
		if devices, err := models.ListShareDevicesByShareIDs(ids); err == nil {
			item.DeviceCount = len(devices)
			for _, device := range devices {
				if device.Status == models.ShareDeviceStatusPending {
					item.PendingDeviceCount++
				}
			}
		}
		items = append(items, item)
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": items})
}

// ShareGroupGet returns one group, its stub mapping and its subscription links.
func ShareGroupGet(c *gin.Context) {
	id, err := strconv.Atoi(c.Query("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid group ID"})
		return
	}
	group, err := models.GetShareGroupByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Group not found"})
		return
	}
	shares := models.GetSharesByGroupID(group.ID)
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"group":         group,
			"template_list": group.EffectiveTemplates(),
			"stubs":         models.GetShareGroupStubMap(group.ID),
			"shares":        shares,
		},
	})
}

// ShareGroupAdd creates a group.
func ShareGroupAdd(c *gin.Context) {
	var req ShareGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}
	group := &models.ShareGroup{}
	if err := applyShareGroupReq(group, &req, true); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	if err := group.Add(); err != nil {
		utils.Error("Failed to create share group: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Created successfully", "data": group})
}

// ShareGroupUpdate updates a group.
func ShareGroupUpdate(c *gin.Context) {
	var req ShareGroupReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}
	if req.ID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid group ID"})
		return
	}
	group, err := models.GetShareGroupByID(req.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Group not found"})
		return
	}
	if err := applyShareGroupReq(group, &req, false); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	if err := group.Update(); err != nil {
		utils.Error("Failed to update share group: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Updated successfully", "data": group})
}

// ShareGroupDelete removes a group and detaches its links.
func ShareGroupDelete(c *gin.Context) {
	id, err := strconv.Atoi(c.Query("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid group ID"})
		return
	}
	group, err := models.GetShareGroupByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Group not found"})
		return
	}
	if err := group.Delete(); err != nil {
		utils.Error("Failed to delete share group: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Delete failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Deleted successfully"})
}

// ShareGroupStubsGet returns the case -> stub mapping for a group.
func ShareGroupStubsGet(c *gin.Context) {
	id, err := strconv.Atoi(c.Query("groupId"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid group ID"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": models.GetShareGroupStubMap(id)})
}

// ShareGroupStubsUpdate replaces the case -> stub mapping.
func ShareGroupStubsUpdate(c *gin.Context) {
	var req ShareGroupStubReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}
	if _, err := models.GetShareGroupByID(req.GroupID); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Group not found"})
		return
	}
	if err := models.SetShareGroupStubs(req.GroupID, req.Stubs); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Updated successfully"})
}

// ShareGroupActivity returns recent access rows across the group links.
func ShareGroupActivity(c *gin.Context) {
	id, err := strconv.Atoi(c.Query("groupId"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid group ID"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	shares := models.GetSharesByGroupID(id)
	ids := make([]int, 0, len(shares))
	names := make(map[int]string, len(shares))
	for _, share := range shares {
		ids = append(ids, share.ID)
		names[share.ID] = share.Name
	}
	logs, err := models.ListShareAccessLogsByShareIDs(ids, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to load activity"})
		return
	}
	items := make([]gin.H, 0, len(logs))
	for _, entry := range logs {
		items = append(items, gin.H{
			"id":         entry.ID,
			"share_id":   entry.ShareID,
			"share_name": names[entry.ShareID],
			"device_id":  entry.DeviceID,
			"hwid":       entry.HWID,
			"action":     entry.Action,
			"template":   entry.Template,
			"reason":     entry.Reason,
			"created_at": entry.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": items})
}
