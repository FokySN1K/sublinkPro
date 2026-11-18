package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"sublink/models"
	"sublink/utils"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// shareTelemetryMaxBytesPerReport caps a single traffic report per direction
// (1 PiB). The bound protects the counter from broken clients while staying
// far above any realistic report.
const shareTelemetryMaxBytesPerReport int64 = 1 << 50

// shareTelemetryMaxTotalBytes caps an absolute installation counter (1 EiB).
// The bound stays far below int64 overflow even after years of accumulation.
const shareTelemetryMaxTotalBytes int64 = 1 << 60

// shareTelemetryIDMaxLen caps client supplied identifiers (report_id,
// install_id). Longer values are truncated, not rejected.
const shareTelemetryIDMaxLen = 64

// shareTelemetryReportDedupTTL is how long a diff-mode report id stays
// remembered. The window only guards retries of lost responses; absolute
// reports rely on the baseline+epoch pair instead.
const shareTelemetryReportDedupTTL = 30 * time.Minute

// shareTelemetryReportDedupLimit caps the in-memory dedup map.
const shareTelemetryReportDedupLimit = 4096

type shareTelemetryRequest struct {
	Up   *int64 `json:"up"`
	Down *int64 `json:"down"`
	// ReportID deduplicates diff-mode reports: a retry with the same id inside
	// the TTL window is answered as a duplicate without writing anything.
	ReportID string `json:"report_id"`
	// ReportedTotalBytes switches the report to the absolute installation
	// counter mode. Idempotency then comes from the device baseline+epoch.
	ReportedTotalBytes *int64 `json:"reported_total_bytes"`
	// InstallID is the client installation epoch. A new value restarts the
	// absolute baseline from zero (reinstall or moved data).
	InstallID string `json:"install_id"`
}

type shareTelemetryResponse struct {
	ReportedTrafficBytes       int64      `json:"reported_traffic_bytes"`
	TrafficLimitBytes          int64      `json:"traffic_limit_bytes"`
	RemainingBytes             int64      `json:"remaining_bytes"`
	QuotaExhausted             bool       `json:"quota_exhausted"`
	LastUsageAt                *time.Time `json:"last_usage_at"`
	DeviceStatus               string     `json:"device_status"`
	DeviceReportedTrafficBytes int64      `json:"device_reported_traffic_bytes"`
	AcceptedBytes              int64      `json:"accepted_bytes"`
	Duplicate                  bool       `json:"duplicate"`
}

// shareTelemetryReportCache remembers diff-mode report ids per link/scope.
// It is process local: after a restart the 30-minute window is lost, which is
// acceptable for legacy clients and irrelevant for absolute reporting.
var shareTelemetryReportCache = struct {
	mu   sync.Mutex
	seen map[string]time.Time
}{seen: make(map[string]time.Time)}

// ShareReportTraffic ingests one client traffic report for a shared
// subscription link:
//
//	POST /c/telemetry?token=<share token>
//	{"up": 12345, "down": 67890, "report_id": "r-1"}
//	{"reported_total_bytes": 987654, "install_id": "install-abc", "up": 1234}
//
// The link must belong to a share group with telemetry enabled. A device
// secret is optional; when it is present it must belong to a device bound to
// the same link. Absolute reports require a resolved device; without one the
// report falls back to the diff semantics.
func ShareReportTraffic(c *gin.Context) {
	token := strings.ToLower(strings.TrimSpace(c.Query("token")))
	if token == "" {
		utils.Result(c, http.StatusNotFound, http.StatusNotFound, "Invalid share token", nil)
		return
	}
	share, err := models.GetSubscriptionShareByToken(token)
	if err != nil {
		utils.Result(c, http.StatusNotFound, http.StatusNotFound, "Invalid share token", nil)
		return
	}

	group := loadShareGroup(share)
	if group == nil || !group.TelemetryEnabled {
		utils.Forbidden(c, "Telemetry is not enabled for this link")
		return
	}
	if !group.Enabled {
		utils.Forbidden(c, "This subscription group is disabled")
		return
	}
	if share.IsExpired() {
		utils.Forbidden(c, "This share link is not active")
		return
	}

	var req shareTelemetryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "Invalid telemetry payload: "+err.Error(), nil)
		return
	}
	if req.Up == nil && req.Down == nil && req.ReportedTotalBytes == nil {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "Provide at least one of up/down/reported_total_bytes traffic values", nil)
		return
	}
	up, down := int64(0), int64(0)
	if req.Up != nil {
		up = *req.Up
	}
	if req.Down != nil {
		down = *req.Down
	}
	if up < 0 || down < 0 {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "Traffic values must be non-negative", nil)
		return
	}
	if up > shareTelemetryMaxBytesPerReport || down > shareTelemetryMaxBytesPerReport {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "Traffic value is too large", nil)
		return
	}
	if req.ReportedTotalBytes != nil {
		if *req.ReportedTotalBytes < 0 {
			utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "Absolute traffic value must be non-negative", nil)
			return
		}
		if *req.ReportedTotalBytes > shareTelemetryMaxTotalBytes {
			utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "Absolute traffic value is too large", nil)
			return
		}
	}

	device, deviceStatus, deviceErr := resolveShareTelemetryDevice(c, share)
	if deviceErr != nil {
		utils.Forbidden(c, deviceErr.Error())
		return
	}

	now := time.Now()
	accepted := int64(0)
	duplicate := false

	if req.ReportedTotalBytes != nil && device != nil {
		// Absolute installation counter: replays and stale smaller totals are
		// duplicates, a new install_id restarts the baseline. report_id is
		// intentionally ignored here (the baseline+epoch pair is the identity).
		installID := models.NormalizeShareDeviceValue(req.InstallID, shareTelemetryIDMaxLen)
		delta, _, applyErr := models.AddShareDeviceAbsoluteReport(device, *req.ReportedTotalBytes, installID, now)
		if applyErr != nil {
			utils.FailWithMsg(c, "Failed to record traffic: "+applyErr.Error())
			return
		}
		if delta <= 0 {
			duplicate = true
		} else {
			upPart := up
			if upPart < 0 {
				upPart = 0
			}
			if upPart > delta {
				upPart = delta
			}
			downPart := delta - upPart
			accepted = delta
			if err := share.AddReportedTraffic(delta); err != nil {
				utils.FailWithMsg(c, "Failed to record traffic: "+err.Error())
				return
			}
			// The absolute path already moved the device lifetime counter;
			// only the hourly bucket is recorded here.
			if usageErr := models.RecordShareDeviceUsage(device, upPart, downPart, now); usageErr != nil {
				utils.Warn("Failed to record share device traffic: %v", usageErr)
			}
		}
	} else {
		reportID := models.NormalizeShareDeviceValue(req.ReportID, shareTelemetryIDMaxLen)
		if reportID != "" {
			key := shareTelemetryReportKey(share.ID, shareTelemetryScope(c), reportID)
			if !claimShareTelemetryReportID(key, now) {
				duplicate = true
			}
		}
		if !duplicate {
			accepted = up + down
			if err := share.AddReportedTraffic(accepted); err != nil {
				utils.FailWithMsg(c, "Failed to record traffic: "+err.Error())
				return
			}
			// 已绑定的设备单独累计一份：设备视图需要按安装维度的用量与时间分布，
			// 链接总量已经在上面写入，这里失败不影响本次上报结果。
			if device != nil {
				if deviceTrafficErr := models.RecordShareDeviceTraffic(device, up, down, now); deviceTrafficErr != nil {
					utils.Warn("Failed to record share device traffic: %v", deviceTrafficErr)
				}
			}
		}
	}

	if !duplicate {
		models.RecordShareAccessAsync(&models.ShareAccessLog{
			ShareID:   share.ID,
			DeviceID:  deviceIDInt(device),
			HWID:      models.NormalizeShareDeviceValue(c.GetHeader("X-HWID"), 128),
			DeviceKey: models.NormalizeShareDeviceValue(c.GetHeader("X-Device-ID"), 128),
			Action:    models.ShareAccessActionTelemetry,
		})
	}

	utils.OkDetailed(c, "Success", newShareTelemetryResponse(share, device, deviceStatus, accepted, duplicate))
}

// newShareTelemetryResponse builds the shared telemetry reply payload.
func newShareTelemetryResponse(share *models.SubscriptionShare, device *models.ShareDevice, deviceStatus string, accepted int64, duplicate bool) shareTelemetryResponse {
	response := shareTelemetryResponse{
		ReportedTrafficBytes: share.ReportedTrafficBytes,
		TrafficLimitBytes:    share.TrafficLimitBytes,
		RemainingBytes:       shareRemainingBytes(share),
		QuotaExhausted:       share.IsQuotaExhausted(),
		LastUsageAt:          share.LastUsageAt,
		DeviceStatus:         deviceStatus,
		AcceptedBytes:        accepted,
		Duplicate:            duplicate,
	}
	if device != nil {
		response.DeviceReportedTrafficBytes = device.ReportedTrafficBytes
	}
	return response
}

// shareTelemetryReportKey scopes a report id to one link and one reporter.
func shareTelemetryReportKey(shareID int, scope, reportID string) string {
	return strconv.Itoa(shareID) + "|" + scope + "|" + reportID
}

// shareTelemetryScope identifies the reporter of a diff-mode report.
func shareTelemetryScope(c *gin.Context) string {
	if secret := models.NormalizeShareDeviceValue(c.GetHeader("X-Sublink-Device-Secret"), 64); secret != "" {
		return "d:" + secret
	}
	hwid := models.NormalizeShareDeviceValue(c.GetHeader("X-HWID"), 128)
	deviceID := models.NormalizeShareDeviceValue(c.GetHeader("X-Device-ID"), 128)
	if hwid != "" && deviceID != "" {
		return "h:" + hwid + "|" + deviceID
	}
	return "link"
}

// claimShareTelemetryReportID reports whether the report id is new. Expired
// entries are pruned on insert and the map never exceeds its hard cap.
func claimShareTelemetryReportID(key string, now time.Time) bool {
	if key == "" {
		return true
	}
	cache := &shareTelemetryReportCache
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if seenAt, exists := cache.seen[key]; exists {
		if now.Sub(seenAt) < shareTelemetryReportDedupTTL {
			return false
		}
		delete(cache.seen, key)
	}
	for candidate, seenAt := range cache.seen {
		if now.Sub(seenAt) >= shareTelemetryReportDedupTTL {
			delete(cache.seen, candidate)
		}
	}
	for len(cache.seen) >= shareTelemetryReportDedupLimit {
		oldestKey := ""
		var oldestAt time.Time
		for candidate, seenAt := range cache.seen {
			if oldestKey == "" || seenAt.Before(oldestAt) {
				oldestKey, oldestAt = candidate, seenAt
			}
		}
		delete(cache.seen, oldestKey)
	}
	cache.seen[key] = now
	return true
}

// resolveShareTelemetryDevice validates the reporter identity. A report
// without any identity headers stays link-scoped; a supplied secret or
// HWID/device pair must resolve to an active device of the same link.
func resolveShareTelemetryDevice(c *gin.Context, share *models.SubscriptionShare) (*models.ShareDevice, string, error) {
	secret := models.NormalizeShareDeviceValue(c.GetHeader("X-Sublink-Device-Secret"), 64)
	if secret != "" {
		device, err := models.FindShareDeviceBySecret(share.ID, secret)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, "", errors.New("invalid device secret")
			}
			return nil, "", errors.New("failed to verify device secret")
		}
		if err := ensureShareTelemetryDeviceActive(device); err != nil {
			return nil, device.Status, err
		}
		models.TouchShareDevice(device.ID)
		return device, device.Status, nil
	}

	hwid := models.NormalizeShareDeviceValue(c.GetHeader("X-HWID"), 128)
	deviceID := models.NormalizeShareDeviceValue(c.GetHeader("X-Device-ID"), 128)
	if hwid == "" || deviceID == "" {
		return nil, "unbound", nil
	}
	device, err := models.FindShareDevice(share.ID, hwid, deviceID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, "unbound", nil
		}
		return nil, "", errors.New("failed to verify device identity")
	}
	if err := ensureShareTelemetryDeviceActive(device); err != nil {
		return nil, device.Status, err
	}
	models.TouchShareDevice(device.ID)
	return device, device.Status, nil
}

func ensureShareTelemetryDeviceActive(device *models.ShareDevice) error {
	switch device.Status {
	case models.ShareDeviceStatusBlocked:
		return errors.New("device is blocked")
	case models.ShareDeviceStatusPending:
		return errors.New("device is pending approval")
	default:
		return nil
	}
}
