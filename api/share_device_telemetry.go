package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"sublink/models"
	"sublink/utils"

	"github.com/gin-gonic/gin"
)

const (
	shareDeviceDefaultPageSize = 20
	shareDeviceMaxPageSize     = 200
	shareDeviceActivityLimit   = 100
)

// shareDeviceShareEntry describes one link a device is bound to. The telemetry
// tab needs the owning node pool and group names, not just the share row.
type shareDeviceShareEntry struct {
	ShareID              int        `json:"share_id"`
	ShareName            string     `json:"share_name"`
	Token                string     `json:"token"`
	Status               string     `json:"status"`
	GroupID              *int       `json:"group_id"`
	GroupName            string     `json:"group_name"`
	SubscriptionID       int        `json:"subscription_id"`
	SubscriptionName     string     `json:"subscription_name"`
	ReportedTrafficBytes int64      `json:"reported_traffic_bytes"`
	LastSeenAt           time.Time  `json:"last_seen_at"`
	LastReportAt         *time.Time `json:"last_report_at"`
}

// shareDeviceOverviewItem is one installation aggregated across every link it
// is bound to. HWID/device ID is the identity, share rows are the bindings.
type shareDeviceOverviewItem struct {
	HWID                 string                  `json:"hwid"`
	DeviceID             string                  `json:"device_id"`
	Name                 string                  `json:"name"`
	Platform             string                  `json:"platform"`
	AppVersion           string                  `json:"app_version"`
	Status               string                  `json:"status"`
	ActiveCount          int                     `json:"active_count"`
	PendingCount         int                     `json:"pending_count"`
	BlockedCount         int                     `json:"blocked_count"`
	FirstSeenAt          time.Time               `json:"first_seen_at"`
	LastSeenAt           time.Time               `json:"last_seen_at"`
	LastReportAt         *time.Time              `json:"last_report_at"`
	ReportedTrafficBytes int64                   `json:"reported_traffic_bytes"`
	WindowTrafficBytes   int64                   `json:"window_traffic_bytes"`
	ShareCount           int                     `json:"share_count"`
	Shares               []shareDeviceShareEntry `json:"shares"`

	deviceRowIDs []int
	searchText   string
}

type shareDeviceHourlyBucket struct {
	WindowStart time.Time `json:"window_start"`
	UpBytes     int64     `json:"up_bytes"`
	DownBytes   int64     `json:"down_bytes"`
	Bytes       int64     `json:"bytes"`
}

type shareDeviceActivityEntry struct {
	ID        int       `json:"id"`
	ShareID   int       `json:"share_id"`
	ShareName string    `json:"share_name"`
	Action    string    `json:"action"`
	Template  string    `json:"template"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// ShareDeviceOverview lists every installation known to the requested share
// scope, aggregated over all links the installation is bound to.
//
//	GET /api/v1/shares/devices/overview?subId=&groupId=&status=&keyword=&days=&page=&pageSize=
func ShareDeviceOverview(c *gin.Context) {
	shares, err := resolveShareDeviceScope(c)
	if err != nil {
		utils.Error("Failed to resolve share device scope: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to load share scope"})
		return
	}
	days := shareTelemetryDaysParam(c)
	shareIDs := make([]int, 0, len(shares))
	for _, share := range shares {
		shareIDs = append(shareIDs, share.ID)
	}
	devices, err := models.ListShareDevicesByShareIDs(shareIDs)
	if err != nil {
		utils.Error("Failed to load share devices: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to load devices"})
		return
	}

	windowStart := time.Now().AddDate(0, 0, -days)
	items := aggregateShareDevices(devices, buildShareDeviceLinkIndex(shares), loadShareDeviceWindowSums(devices, windowStart))

	status := strings.ToLower(strings.TrimSpace(c.Query("status")))
	keyword := strings.ToLower(strings.TrimSpace(c.Query("keyword")))
	filtered := items[:0]
	for _, item := range items {
		if status != "" && status != "all" && item.Status != status {
			continue
		}
		if keyword != "" && !strings.Contains(item.searchText, keyword) {
			continue
		}
		filtered = append(filtered, item)
	}
	items = filtered

	summary := gin.H{
		"total":   len(items),
		"active":  0,
		"pending": 0,
		"blocked": 0,
	}
	active, pending, blocked := 0, 0, 0
	reportedTotal, windowTotal := int64(0), int64(0)
	for _, item := range items {
		reportedTotal += item.ReportedTrafficBytes
		windowTotal += item.WindowTrafficBytes
		switch item.Status {
		case models.ShareDeviceStatusPending:
			pending++
		case models.ShareDeviceStatusBlocked:
			blocked++
		default:
			active++
		}
	}
	summary["active"] = active
	summary["pending"] = pending
	summary["blocked"] = blocked
	summary["reported_traffic_bytes"] = reportedTotal
	summary["window_traffic_bytes"] = windowTotal

	sortShareDeviceItems(items)
	page, pageSize := shareDevicePagination(c)
	start := (page - 1) * pageSize
	if start > len(items) {
		start = len(items)
	}
	end := start + pageSize
	if end > len(items) {
		end = len(items)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"items":       items[start:end],
			"total":       len(items),
			"page":        page,
			"page_size":   pageSize,
			"summary":     summary,
			"window_days": days,
		},
	})
}

// ShareDeviceDetail returns one installation with its link bindings, hourly
// traffic buckets, local-day totals, sessions, node/airport traffic,
// reliability and recent activity.
//
//	GET /api/v1/shares/devices/detail?hwid=&deviceId=&days=
func ShareDeviceDetail(c *gin.Context) {
	hwid := models.NormalizeShareDeviceValue(c.Query("hwid"), 128)
	deviceID := models.NormalizeShareDeviceValue(c.Query("deviceId"), 128)
	if hwid == "" || deviceID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid device identity"})
		return
	}
	devices, err := models.ListShareDevicesByIdentity(hwid, deviceID)
	if err != nil {
		utils.Error("Failed to load share device identity: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to load devices"})
		return
	}
	if len(devices) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Device not found"})
		return
	}

	days := shareTelemetryDaysParam(c)
	shares := loadSharesByIDs(devices)
	windowStart := time.Now().AddDate(0, 0, -days)
	items := aggregateShareDevices(devices, buildShareDeviceLinkIndex(shares), loadShareDeviceWindowSums(devices, windowStart))
	if len(items) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Device not found"})
		return
	}
	item := items[0]

	buckets, err := models.ListShareDeviceUsage(item.deviceRowIDs, windowStart)
	if err != nil {
		utils.Warn("Failed to load share device usage: %v", err)
		buckets = nil
	}
	hourly := make([]shareDeviceHourlyBucket, 0, len(buckets))
	for _, bucket := range buckets {
		hourly = append(hourly, shareDeviceHourlyBucket{
			WindowStart: bucket.WindowStart,
			UpBytes:     bucket.UpBytes,
			DownBytes:   bucket.DownBytes,
			Bytes:       bucket.Bytes,
		})
	}

	sessions, err := models.ListShareSessionsByDeviceIDs(item.deviceRowIDs, windowStart, shareSubscriptionSessionLimit)
	if err != nil {
		utils.Warn("Failed to load share sessions: %v", err)
		sessions = nil
	}
	nodes, airports := buildShareNodeTraffic(nil, item.deviceRowIDs, windowStart)
	reliability, reliabilityEvents := buildShareReliability(nil, item.deviceRowIDs, windowStart)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"device":             item,
			"hourly":             hourly,
			"daily":              buildShareDailyBuckets(buckets, days),
			"sessions":           buildShareSessionEntries(sessions, devices, time.Now()),
			"activity":           loadShareDeviceActivity(item),
			"nodes":              nodes,
			"airports":           airports,
			"reliability":        reliability,
			"reliability_events": reliabilityEvents,
			"window_days":        days,
		},
	})
}

func resolveShareDeviceScope(c *gin.Context) ([]models.SubscriptionShare, error) {
	if groupID, err := strconv.Atoi(c.Query("groupId")); err == nil && groupID > 0 {
		return models.GetSharesByGroupID(groupID), nil
	}
	if subID, err := strconv.Atoi(c.Query("subId")); err == nil && subID > 0 {
		return models.GetSharesBySubscriptionID(subID), nil
	}
	return (&models.SubscriptionShare{}).List()
}

func loadSharesByIDs(devices []models.ShareDevice) []models.SubscriptionShare {
	needed := make(map[int]bool, len(devices))
	for _, device := range devices {
		needed[device.ShareID] = true
	}
	all, err := (&models.SubscriptionShare{}).List()
	if err != nil {
		return nil
	}
	shares := make([]models.SubscriptionShare, 0, len(needed))
	for _, share := range all {
		if needed[share.ID] {
			shares = append(shares, share)
		}
	}
	return shares
}

// buildShareDeviceLinkIndex maps share rows to display metadata. Group and
// node pool names come from their write-through caches.
func buildShareDeviceLinkIndex(shares []models.SubscriptionShare) map[int]shareDeviceShareEntry {
	groupNames := make(map[int]string)
	for _, group := range models.ListShareGroups() {
		groupNames[group.ID] = group.Name
	}
	poolNames := make(map[int]string)
	entries := make(map[int]shareDeviceShareEntry, len(shares))
	for _, share := range shares {
		groupName := ""
		if share.GroupID != nil {
			groupName = groupNames[*share.GroupID]
		}
		poolName := poolNames[share.SubscriptionID]
		if poolName == "" {
			if source, err := models.GetSubcriptionByID(share.SubscriptionID); err == nil && source != nil {
				poolName = source.Name
				poolNames[share.SubscriptionID] = poolName
			}
		}
		expired := share.IsExpired()
		lastSeen := shareEntryLastSeen(&share)
		entries[share.ID] = shareDeviceShareEntry{
			ShareID:              share.ID,
			ShareName:            share.Name,
			Token:                share.Token,
			Status:               shareEntryStatus(expired),
			GroupID:              share.GroupID,
			GroupName:            groupName,
			SubscriptionID:       share.SubscriptionID,
			SubscriptionName:     poolName,
			ReportedTrafficBytes: share.ReportedTrafficBytes,
			LastSeenAt:           lastSeen,
			LastReportAt:         share.LastUsageAt,
		}
	}
	return entries
}

func shareEntryStatus(expired bool) string {
	if expired {
		return "disabled"
	}
	return "enabled"
}

func shareEntryLastSeen(share *models.SubscriptionShare) time.Time {
	latest := time.Time{}
	if share.LastAccessAt != nil {
		latest = *share.LastAccessAt
	}
	if share.LastUsageAt != nil && share.LastUsageAt.After(latest) {
		latest = *share.LastUsageAt
	}
	return latest
}

func loadShareDeviceWindowSums(devices []models.ShareDevice, since time.Time) map[int]int64 {
	ids := make([]int, 0, len(devices))
	for _, device := range devices {
		ids = append(ids, device.ID)
	}
	sums, err := models.SumShareDeviceUsage(ids, since)
	if err != nil {
		utils.Warn("Failed to sum share device usage: %v", err)
		return map[int]int64{}
	}
	return sums
}

// aggregateShareDevices folds share-level device rows into one item per
// installation (HWID + device ID) and keeps the per-link bindings.
func aggregateShareDevices(devices []models.ShareDevice, links map[int]shareDeviceShareEntry, windowSums map[int]int64) []*shareDeviceOverviewItem {
	order := make([]string, 0, len(devices))
	byIdentity := make(map[string]*shareDeviceOverviewItem, len(devices))
	latestSeen := make(map[string]time.Time, len(devices))

	for _, device := range devices {
		key := device.HWID + "\x00" + device.DeviceID
		item := byIdentity[key]
		if item == nil {
			item = &shareDeviceOverviewItem{
				HWID:         device.HWID,
				DeviceID:     device.DeviceID,
				Shares:       make([]shareDeviceShareEntry, 0, 2),
				FirstSeenAt:  device.FirstSeenAt,
				LastSeenAt:   device.LastSeenAt,
				LastReportAt: device.LastReportAt,
			}
			byIdentity[key] = item
			order = append(order, key)
		}

		item.deviceRowIDs = append(item.deviceRowIDs, device.ID)
		item.ReportedTrafficBytes += device.ReportedTrafficBytes
		item.WindowTrafficBytes += windowSums[device.ID]
		item.ActiveCount, item.PendingCount, item.BlockedCount = countShareDeviceStatus(item, device.Status)
		item.LastSeenAt = laterTime(item.LastSeenAt, device.LastSeenAt)
		item.LastReportAt = laterTimePtr(item.LastReportAt, device.LastReportAt)
		if !device.FirstSeenAt.IsZero() && (item.FirstSeenAt.IsZero() || device.FirstSeenAt.Before(item.FirstSeenAt)) {
			item.FirstSeenAt = device.FirstSeenAt
		}
		// Newest binding wins for display fields so a renamed device shows the
		// latest name its client reported.
		if device.LastSeenAt.After(latestSeen[key]) {
			latestSeen[key] = device.LastSeenAt
			if name := strings.TrimSpace(device.Name); name != "" {
				item.Name = name
			}
			if platform := strings.TrimSpace(device.Platform); platform != "" {
				item.Platform = platform
			}
			if version := strings.TrimSpace(device.AppVersion); version != "" {
				item.AppVersion = version
			}
		}
		if entry, ok := links[device.ShareID]; ok {
			item.Shares = append(item.Shares, entry)
		}
	}

	items := make([]*shareDeviceOverviewItem, 0, len(order))
	for _, key := range order {
		item := byIdentity[key]
		item.Status = statusFromShareDeviceCounts(item)
		item.ShareCount = len(item.Shares)
		sort.SliceStable(item.Shares, func(i, j int) bool {
			return item.Shares[i].LastSeenAt.After(item.Shares[j].LastSeenAt)
		})
		item.searchText = strings.ToLower(strings.Join(append([]string{
			item.Name, item.HWID, item.DeviceID, item.Platform, item.AppVersion,
		}, shareDeviceSearchText(item.Shares)...), "\x00"))
		items = append(items, item)
	}
	return items
}

func countShareDeviceStatus(item *shareDeviceOverviewItem, status string) (int, int, int) {
	active, pending, blocked := item.ActiveCount, item.PendingCount, item.BlockedCount
	switch status {
	case models.ShareDeviceStatusPending:
		pending++
	case models.ShareDeviceStatusBlocked:
		blocked++
	default:
		active++
	}
	return active, pending, blocked
}

func statusFromShareDeviceCounts(item *shareDeviceOverviewItem) string {
	switch {
	case item.BlockedCount > 0:
		return models.ShareDeviceStatusBlocked
	case item.PendingCount > 0:
		return models.ShareDeviceStatusPending
	default:
		return models.ShareDeviceStatusActive
	}
}

func shareDeviceSearchText(shares []shareDeviceShareEntry) []string {
	parts := make([]string, 0, len(shares)*2)
	for _, share := range shares {
		parts = append(parts, share.ShareName, share.SubscriptionName, share.GroupName)
	}
	return parts
}

func sortShareDeviceItems(items []*shareDeviceOverviewItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].LastSeenAt.Equal(items[j].LastSeenAt) {
			return items[i].LastSeenAt.After(items[j].LastSeenAt)
		}
		return items[i].Name < items[j].Name
	})
}

func shareDevicePagination(c *gin.Context) (int, int) {
	page, err := strconv.Atoi(c.DefaultQuery("page", "1"))
	if err != nil || page < 1 {
		page = 1
	}
	pageSize, err := strconv.Atoi(c.DefaultQuery("pageSize", strconv.Itoa(shareDeviceDefaultPageSize)))
	if err != nil || pageSize < 1 {
		pageSize = shareDeviceDefaultPageSize
	}
	if pageSize > shareDeviceMaxPageSize {
		pageSize = shareDeviceMaxPageSize
	}
	return page, pageSize
}

func loadShareDeviceActivity(item *shareDeviceOverviewItem) []shareDeviceActivityEntry {
	shareIDs := make([]int, 0, len(item.Shares))
	names := make(map[int]string, len(item.Shares))
	for _, share := range item.Shares {
		shareIDs = append(shareIDs, share.ShareID)
		names[share.ShareID] = share.ShareName
	}
	rowIDs := make(map[int]bool, len(item.deviceRowIDs))
	for _, id := range item.deviceRowIDs {
		rowIDs[id] = true
	}
	logs, err := models.ListShareAccessLogsByShareIDs(shareIDs, 500)
	if err != nil {
		utils.Warn("Failed to load share device activity: %v", err)
		return []shareDeviceActivityEntry{}
	}
	entries := make([]shareDeviceActivityEntry, 0, shareDeviceActivityLimit)
	for _, entry := range logs {
		if !shareDeviceLogMatches(entry, rowIDs, item.HWID, item.DeviceID) {
			continue
		}
		entries = append(entries, shareDeviceActivityEntry{
			ID:        entry.ID,
			ShareID:   entry.ShareID,
			ShareName: names[entry.ShareID],
			Action:    entry.Action,
			Template:  entry.Template,
			Reason:    entry.Reason,
			CreatedAt: entry.CreatedAt,
		})
		if len(entries) >= shareDeviceActivityLimit {
			break
		}
	}
	return entries
}

func shareDeviceLogMatches(entry models.ShareAccessLog, rowIDs map[int]bool, hwid, deviceID string) bool {
	if entry.DeviceID > 0 && rowIDs[entry.DeviceID] {
		return true
	}
	if hwid == "" || deviceID == "" {
		return false
	}
	return strings.EqualFold(entry.HWID, hwid) && strings.EqualFold(entry.DeviceKey, deviceID)
}

func laterTime(current, candidate time.Time) time.Time {
	if candidate.After(current) {
		return candidate
	}
	return current
}

func laterTimePtr(current, candidate *time.Time) *time.Time {
	if candidate == nil {
		return current
	}
	if current == nil || candidate.After(*current) {
		return candidate
	}
	return current
}
