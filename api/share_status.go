package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"sublink/models"
	"sublink/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ShareStatus returns a machine-readable snapshot for SNKP: current template,
// group policy, device state and telemetry intervals. It never binds a device.
func ShareStatus(c *gin.Context) {
	c.Header("X-Sublink-Server", "sublinkpro")
	c.Header("X-Sublink-Proto", "1")

	token := strings.ToLower(strings.TrimSpace(c.Query("token")))
	data := gin.H{
		"server": "sublinkpro",
		"proto":  1,
		"valid":  false,
	}
	if token == "" {
		data["reason"] = models.ShareStubCaseInvalidToken
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": data})
		return
	}

	share, err := models.GetSubscriptionShareByToken(token)
	if err != nil {
		data["reason"] = models.ShareStubCaseInvalidToken
		c.JSON(http.StatusOK, gin.H{"code": 200, "data": data})
		return
	}

	group := loadShareGroup(share)
	clientType := resolveShareClientType(c, group)
	c.Header("X-Sublink-Template", clientType)

	hwid := models.NormalizeShareDeviceValue(c.GetHeader("X-HWID"), 128)
	deviceID := models.NormalizeShareDeviceValue(c.GetHeader("X-Device-ID"), 128)
	models.RecordShareAccessAsync(&models.ShareAccessLog{
		ShareID:   share.ID,
		HWID:      hwid,
		DeviceKey: deviceID,
		Action:    models.ShareAccessActionStatus,
		Template:  clientType,
	})

	device, deviceStatus, stubCase := resolveShareStatusDevice(share, group, hwid, deviceID)

	reason := ""
	if stubCase != "" {
		reason = stubCase
	}
	switch {
	case group != nil && !group.Enabled:
		reason = models.ShareStubCaseGroupDisabled
	case share.IsExpired():
		reason = models.ShareStubCaseExpired
	case share.IsQuotaExhausted():
		reason = models.ShareStubCaseQuotaExhausted
	case group != nil && group.HideConfig && !isSnkpRequest(c):
		reason = models.ShareStubCaseUnsupportedApp
	case group != nil && !shareTemplateServed(group, clientType):
		reason = models.ShareStubCaseTemplateDisabled
	case stubCase != "":
		reason = stubCase
	}

	data["valid"] = reason == ""
	data["reason"] = reason
	data["template"] = clientType

	data["share"] = gin.H{
		"id":                     share.ID,
		"name":                   share.Name,
		"enabled":                share.Enabled,
		"expire_type":            share.ExpireType,
		"expire_days":            share.ExpireDays,
		"expire_at":              share.ExpireAt,
		"expire_at_unix":         share.EffectiveExpireUnix(),
		"created_at":             share.CreatedAt,
		"traffic_limit_bytes":    share.TrafficLimitBytes,
		"reported_traffic_bytes": share.ReportedTrafficBytes,
		"remaining_bytes":        shareRemainingBytes(share),
		"quota_exhausted":        share.IsQuotaExhausted(),
		"max_devices":            share.MaxDevices,
	}

	var sub models.Subcription
	sub.ID = share.SubscriptionID
	if err := sub.Find(); err == nil {
		data["subscription"] = gin.H{"id": sub.ID, "name": sub.Name}
	}

	if group != nil {
		data["group"] = gin.H{
			"id":                group.ID,
			"name":              group.Name,
			"enabled":           group.Enabled,
			"hide_config":       group.HideConfig,
			"device_mode":       group.DeviceMode,
			"max_devices":       share.MaxDevices,
			"templates":         group.EffectiveTemplates(),
			"telemetry_enabled": group.TelemetryEnabled,
		}
	}

	deviceData := gin.H{"status": deviceStatus}
	if device != nil {
		deviceData["id"] = device.ID
		deviceData["name"] = device.Name
		deviceData["platform"] = device.Platform
		deviceData["app_version"] = device.AppVersion
		deviceData["last_seen_at"] = device.LastSeenAt
		if deviceStatus == models.ShareDeviceStatusActive {
			deviceData["secret"] = device.Secret
		}
	}
	data["device"] = deviceData

	if share.IsManaged() {
		counts := struct {
			Total   int `json:"total"`
			Active  int `json:"active"`
			Pending int `json:"pending"`
			Blocked int `json:"blocked"`
		}{}
		if devices, listErr := models.ListShareDevices(share.ID); listErr == nil {
			counts.Total = len(devices)
			for _, item := range devices {
				switch item.Status {
				case models.ShareDeviceStatusActive:
					counts.Active++
				case models.ShareDeviceStatusPending:
					counts.Pending++
				case models.ShareDeviceStatusBlocked:
					counts.Blocked++
				}
			}
		}
		data["devices"] = counts
	}

	telemetry := gin.H{
		"enabled":     group != nil && group.TelemetryEnabled,
		"sessions":    share.ReportIntervalSessions,
		"nodes":       share.ReportIntervalNodes,
		"reliability": share.ReportIntervalReliability,
	}
	// SNKP prefers report_interval_seconds and falls back to the per-channel
	// values above (then to 60) when the field is absent.
	if share.ReportIntervalSessions > 0 {
		telemetry["report_interval_seconds"] = share.ReportIntervalSessions
	}
	data["telemetry"] = telemetry

	if reason != "" {
		notice := shareStatusNotice(share, group, reason)
		data["notices"] = []shareStatusNoticeData{notice}
		c.Header("X-Sublink-Reason", reason)
		c.Header("X-Sublink-Stub", notice.Case)
	}

	c.JSON(http.StatusOK, gin.H{"code": 200, "data": data})
}

func resolveShareStatusDevice(share *models.SubscriptionShare, group *models.ShareGroup, hwid, deviceID string) (*models.ShareDevice, string, string) {
	if share == nil || group == nil || !share.IsManaged() {
		return nil, "", ""
	}
	if hwid == "" || deviceID == "" {
		if group.AllowLegacyClients {
			return nil, "unbound", ""
		}
		return nil, "unbound", models.ShareStubCaseUnsupportedApp
	}

	device, err := models.FindShareDevice(share.ID, hwid, deviceID)
	if err == nil {
		switch device.Status {
		case models.ShareDeviceStatusBlocked:
			return device, models.ShareDeviceStatusBlocked, models.ShareStubCaseDeviceBlocked
		case models.ShareDeviceStatusPending:
			return device, models.ShareDeviceStatusPending, models.ShareStubCaseDevicePending
		}
		return device, models.ShareDeviceStatusActive, ""
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		utils.Warn("Failed to load share device for status: %v", err)
		return nil, "error", models.ShareStubCaseUnsupportedApp
	}

	if group.DeviceMode == models.ShareDeviceModeApprove {
		return nil, models.ShareDeviceStatusPending, models.ShareStubCaseDevicePending
	}
	if share.MaxDevices > 0 {
		count, countErr := models.CountActiveShareDevices(share.ID)
		if countErr == nil && count >= int64(share.MaxDevices) {
			return nil, "limit", models.ShareStubCaseDeviceLimit
		}
	}
	return nil, "new", ""
}

// shareStatusNoticeData is one resolved stub notice for /c/status.
type shareStatusNoticeData struct {
	Case    string `json:"case"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

func shareStatusNotice(share *models.SubscriptionShare, group *models.ShareGroup, caseKey string) shareStatusNoticeData {
	stub := models.ResolveShareStub(share, caseKey)
	vars := map[string]string{"case": caseKey}
	if share != nil {
		vars["share_name"] = share.Name
		vars["limit"] = intToString(share.MaxDevices)
		vars["used"] = utils.FormatBytes(share.ReportedTrafficBytes)
		vars["expires_at"] = shareExpiresAtLabel(share)
	}
	if group != nil {
		vars["group_name"] = group.Name
	}
	title, content := models.RenderShareStub(stub, vars)
	return shareStatusNoticeData{
		Case:    stub.CaseKey,
		Title:   title,
		Content: content,
	}
}

func intToString(value int) string {
	if value <= 0 {
		return ""
	}
	return strconv.Itoa(value)
}

// shareRemainingBytes returns the link traffic left before the quota is
// exhausted. A zero limit means unlimited and yields zero.
func shareRemainingBytes(share *models.SubscriptionShare) int64 {
	if share == nil || share.TrafficLimitBytes <= 0 {
		return 0
	}
	remaining := share.TrafficLimitBytes - share.ReportedTrafficBytes
	if remaining < 0 {
		return 0
	}
	return remaining
}
