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

// ShareDeviceUpdateReq changes administrator-editable device fields.
type ShareDeviceUpdateReq struct {
	ID          int    `json:"id" binding:"required"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	HWID        string `json:"hwid"`
	Secret      string `json:"secret"`
}

type shareDeviceItem struct {
	models.ShareDevice
	ShareName string `json:"share_name"`
}

// ShareDeviceList returns devices bound to one link with limit metadata.
func ShareDeviceList(c *gin.Context) {
	shareID, err := strconv.Atoi(c.Query("shareId"))
	if err != nil || shareID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid share ID"})
		return
	}
	share := &models.SubscriptionShare{ID: shareID}
	if err := share.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Share not found"})
		return
	}
	devices, err := models.ListShareDevices(shareID)
	if err != nil {
		utils.Error("Failed to load share devices: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to load devices"})
		return
	}
	items := make([]shareDeviceItem, 0, len(devices))
	active, pending, blocked := 0, 0, 0
	for _, device := range devices {
		switch device.Status {
		case models.ShareDeviceStatusActive:
			active++
		case models.ShareDeviceStatusPending:
			pending++
		case models.ShareDeviceStatusBlocked:
			blocked++
		}
		items = append(items, shareDeviceItem{ShareDevice: device, ShareName: share.Name})
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"items":         items,
			"active":        active,
			"pending":       pending,
			"blocked":       blocked,
			"max_devices":   share.MaxDevices,
			"limit_reached": share.MaxDevices > 0 && active >= share.MaxDevices,
		},
	})
}

// ShareDeviceUpdate edits name/description/status or the owner identity.
func ShareDeviceUpdate(c *gin.Context) {
	var req ShareDeviceUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid parameters: " + err.Error()})
		return
	}
	device, err := models.GetShareDeviceByID(req.ID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Device not found"})
		return
	}
	device.Name = strings.TrimSpace(req.Name)
	device.Description = strings.TrimSpace(req.Description)
	if req.Status != "" {
		device.Status = req.Status
	}
	if req.HWID != "" {
		device.HWID = strings.TrimSpace(req.HWID)
	}
	if req.Secret != "" {
		device.Secret = strings.TrimSpace(req.Secret)
	}
	if err := device.Update(); err != nil {
		utils.Error("Failed to update share device: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Updated successfully", "data": device})
}

// ShareDeviceDelete unbinds one device.
func ShareDeviceDelete(c *gin.Context) {
	id, err := strconv.Atoi(c.Query("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid device ID"})
		return
	}
	device, err := models.GetShareDeviceByID(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Device not found"})
		return
	}
	if err := device.Delete(); err != nil {
		utils.Error("Failed to delete share device: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Delete failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "msg": "Deleted successfully"})
}

// ShareDeviceClear unbinds every device of one link together with its
// telemetry history, so a link can start its device list from scratch.
func ShareDeviceClear(c *gin.Context) {
	shareID, err := strconv.Atoi(c.Query("shareId"))
	if err != nil || shareID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid share ID"})
		return
	}
	share := &models.SubscriptionShare{ID: shareID}
	if err := share.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Share not found"})
		return
	}
	devices, err := models.ListShareDevices(shareID)
	if err != nil {
		utils.Error("Failed to load share devices: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to load devices"})
		return
	}
	if err := models.DeleteShareDevices(shareID); err != nil {
		utils.Error("Failed to clear share devices: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Clear failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"msg":  "Devices cleared",
		"data": gin.H{"deleted": len(devices)},
	})
}

// ShareActivity returns recent access rows for one link.
func ShareActivity(c *gin.Context) {
	shareID, err := strconv.Atoi(c.Query("shareId"))
	if err != nil || shareID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid share ID"})
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
	logs, err := models.ListShareAccessLogs(shareID, limit)
	if err != nil {
		utils.Error("Failed to load share activity: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to load activity"})
		return
	}
	names := make(map[int]string)
	if devices, deviceErr := models.ListShareDevices(shareID); deviceErr == nil {
		for _, device := range devices {
			names[device.ID] = device.Name
		}
	}
	items := make([]gin.H, 0, len(logs))
	for _, entry := range logs {
		items = append(items, gin.H{
			"id":          entry.ID,
			"share_id":    entry.ShareID,
			"device_id":   entry.DeviceID,
			"device_name": names[entry.DeviceID],
			"hwid":        entry.HWID,
			"action":      entry.Action,
			"template":    entry.Template,
			"reason":      entry.Reason,
			"created_at":  entry.CreatedAt.Format(time.RFC3339),
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 200, "data": items})
}
