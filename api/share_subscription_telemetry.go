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
	shareSubscriptionDeviceListLimit = 50
	shareSubscriptionSessionLimit    = 200
	shareSubscriptionActivityLimit   = 100
	shareSubscriptionEventLimit      = 100
	shareSubscriptionTopNodeLimit    = 3
	shareSubscriptionReliableLimit   = 12
	shareSubscriptionReputationLimit = 200
)

type shareSubscriptionDeviceCounts struct {
	Total   int `json:"total"`
	Active  int `json:"active"`
	Pending int `json:"pending"`
	Blocked int `json:"blocked"`
}

type shareSubscriptionDeviceEntry struct {
	ID                   int        `json:"id"`
	Name                 string     `json:"name"`
	HWID                 string     `json:"hwid"`
	DeviceID             string     `json:"device_id"`
	Platform             string     `json:"platform"`
	Status               string     `json:"status"`
	ReportedTrafficBytes int64      `json:"reported_traffic_bytes"`
	WindowTrafficBytes   int64      `json:"window_traffic_bytes"`
	LastSeenAt           time.Time  `json:"last_seen_at"`
	LastReportAt         *time.Time `json:"last_report_at"`
}

type shareSubscriptionLastSession struct {
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at"`
	Bytes     int64      `json:"bytes"`
}

type shareSubscriptionOverviewItem struct {
	ShareID          int    `json:"share_id"`
	ShareName        string `json:"share_name"`
	Token            string `json:"token"`
	GroupID          *int   `json:"group_id"`
	GroupName        string `json:"group_name"`
	SubscriptionID   int    `json:"subscription_id"`
	SubscriptionName string `json:"subscription_name"`
	ClientID         *int   `json:"client_id"`
	ClientName       string `json:"client_name"`

	Status           string `json:"status"`
	TelemetryEnabled bool   `json:"telemetry_enabled"`

	TrafficLimitBytes    int64 `json:"traffic_limit_bytes"`
	ReportedTrafficBytes int64 `json:"reported_traffic_bytes"`
	RemainingBytes       int64 `json:"remaining_bytes"`
	QuotaExhausted       bool  `json:"quota_exhausted"`

	Devices    shareSubscriptionDeviceCounts  `json:"devices"`
	DeviceList []shareSubscriptionDeviceEntry `json:"device_list"`

	TodayBytes    int64 `json:"today_bytes"`
	WindowBytes   int64 `json:"window_bytes"`
	LifetimeBytes int64 `json:"lifetime_bytes"`

	LastSeenAt    *time.Time                    `json:"last_seen_at"`
	LastReportAt  *time.Time                    `json:"last_report_at"`
	OnlineNow     bool                          `json:"online_now"`
	SessionsToday int64                         `json:"sessions_today"`
	ProblemNodes  int                           `json:"problem_nodes"`
	TopNodes      []shareNodeTrafficEntry       `json:"top_nodes"`
	Airports      []shareAirportTrafficEntry    `json:"airports"`
	LastSession   *shareSubscriptionLastSession `json:"last_session"`

	searchText string
}

type shareSubscriptionMeta struct {
	ShareID          int    `json:"share_id"`
	ShareName        string `json:"share_name"`
	Token            string `json:"token"`
	GroupID          *int   `json:"group_id"`
	GroupName        string `json:"group_name"`
	SubscriptionID   int    `json:"subscription_id"`
	SubscriptionName string `json:"subscription_name"`
	ClientID         *int   `json:"client_id"`
	ClientName       string `json:"client_name"`

	Status           string `json:"status"`
	TelemetryEnabled bool   `json:"telemetry_enabled"`

	TrafficLimitBytes    int64 `json:"traffic_limit_bytes"`
	ReportedTrafficBytes int64 `json:"reported_traffic_bytes"`
	RemainingBytes       int64 `json:"remaining_bytes"`
	QuotaExhausted       bool  `json:"quota_exhausted"`

	AccessCount  int        `json:"access_count"`
	CreatedAt    time.Time  `json:"created_at"`
	LastAccessAt *time.Time `json:"last_access_at"`
	LastUsageAt  *time.Time `json:"last_usage_at"`
}

type shareDailyBucket struct {
	Day       string `json:"day"`
	UpBytes   int64  `json:"up_bytes"`
	DownBytes int64  `json:"down_bytes"`
	Bytes     int64  `json:"bytes"`
}

type shareNodeTrafficEntry struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Group       string `json:"group"`
	Country     string `json:"country"`
	AirportID   int    `json:"airport_id"`
	AirportName string `json:"airport_name"`
	UpBytes     int64  `json:"up_bytes"`
	DownBytes   int64  `json:"down_bytes"`
	Bytes       int64  `json:"bytes"`
}

type shareAirportTrafficEntry struct {
	AirportID   int     `json:"airport_id"`
	AirportName string  `json:"airport_name"`
	Bytes       int64   `json:"bytes"`
	Share       float64 `json:"share"`
}

type shareSessionDetailEntry struct {
	ID              int        `json:"id"`
	DeviceID        int        `json:"device_id"`
	DeviceName      string     `json:"device_name"`
	HWID            string     `json:"hwid"`
	DeviceKey       string     `json:"device_key"`
	SessionKey      string     `json:"session_key"`
	StartedAt       time.Time  `json:"started_at"`
	EndedAt         *time.Time `json:"ended_at"`
	DurationSeconds int64      `json:"duration_s"`
	UpBytes         int64      `json:"up_bytes"`
	DownBytes       int64      `json:"down_bytes"`
	Bytes           int64      `json:"bytes"`
	Open            bool       `json:"open"`
}

type shareReliabilityEntry struct {
	Key         string     `json:"key"`
	Name        string     `json:"name"`
	AirportName string     `json:"airport_name"`
	Score       int        `json:"score"`
	Fails       int        `json:"fails"`
	Streak      int        `json:"streak"`
	Silent      bool       `json:"silent"`
	State       string     `json:"state"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
	ReportedAt  time.Time  `json:"reported_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type shareReliabilityEventEntry struct {
	ID        int       `json:"id"`
	NodeKey   string    `json:"node_key"`
	NodeName  string    `json:"node_name"`
	FromState string    `json:"from_state"`
	ToState   string    `json:"to_state"`
	FromScore int       `json:"from_score"`
	ToScore   int       `json:"to_score"`
	Silent    bool      `json:"silent"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}

// ShareSubscriptionOverview lists one row per share link with its client,
// quota, device counters, window traffic and session state.
//
//	GET /api/v1/shares/subscriptions/overview?groupId=&subId=&clientId=&status=&keyword=&days=&page=&pageSize=
func ShareSubscriptionOverview(c *gin.Context) {
	shares, err := resolveShareDeviceScope(c)
	if err != nil {
		utils.Error("Failed to resolve share subscription scope: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to load share scope"})
		return
	}
	days := shareTelemetryDaysParam(c)
	now := time.Now()
	windowStart := now.AddDate(0, 0, -days)
	todayStart := localMidnight(now)

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
	devicesByShare := make(map[int][]models.ShareDevice, len(shares))
	deviceIDs := make([]int, 0, len(devices))
	for _, device := range devices {
		devicesByShare[device.ShareID] = append(devicesByShare[device.ShareID], device)
		deviceIDs = append(deviceIDs, device.ID)
	}

	windowSums, err := models.SumShareDeviceUsage(deviceIDs, windowStart)
	if err != nil {
		utils.Warn("Failed to sum share device usage: %v", err)
		windowSums = map[int]int64{}
	}
	todaySums, err := models.SumShareDeviceUsage(deviceIDs, todayStart)
	if err != nil {
		utils.Warn("Failed to sum share device usage (today): %v", err)
		todaySums = map[int]int64{}
	}
	sessionSummaries, err := models.SummarizeShareSessions(shareIDs, todayStart)
	if err != nil {
		utils.Warn("Failed to summarize share sessions: %v", err)
		sessionSummaries = map[int]models.ShareSessionSummary{}
	}
	problemCounts, err := models.CountProblemShareNodes(shareIDs)
	if err != nil {
		utils.Warn("Failed to count problem share nodes: %v", err)
		problemCounts = map[int]int{}
	}
	topNodes, topAirports := buildShareSubscriptionTraffic(shareIDs, windowStart)

	links := buildShareDeviceLinkIndex(shares)
	clientNames := models.ClientNameIndex()
	items := make([]*shareSubscriptionOverviewItem, 0, len(shares))
	for i := range shares {
		share := &shares[i]
		entry, ok := links[share.ID]
		if !ok {
			continue
		}
		item := buildShareSubscriptionOverviewItem(share, entry, clientNames, devicesByShare[share.ID], todaySums, windowSums, sessionSummaries[share.ID], problemCounts[share.ID])
		item.TopNodes = topNodes[share.ID]
		if item.TopNodes == nil {
			item.TopNodes = []shareNodeTrafficEntry{}
		}
		item.Airports = topAirports[share.ID]
		if item.Airports == nil {
			item.Airports = []shareAirportTrafficEntry{}
		}
		items = append(items, item)
	}

	status := strings.ToLower(strings.TrimSpace(c.Query("status")))
	keyword := strings.ToLower(strings.TrimSpace(c.Query("keyword")))
	clientFilter := strings.ToLower(strings.TrimSpace(c.Query("clientId")))
	filtered := items[:0]
	for _, item := range items {
		if status != "" && status != "all" && item.Status != status {
			continue
		}
		if clientFilter != "" && clientFilter != "all" {
			if clientFilter == "none" || clientFilter == "0" {
				if item.ClientID != nil {
					continue
				}
			} else if clientID, convErr := strconv.Atoi(clientFilter); convErr == nil {
				if item.ClientID == nil || *item.ClientID != clientID {
					continue
				}
			}
		}
		if keyword != "" && !strings.Contains(item.searchText, keyword) {
			continue
		}
		filtered = append(filtered, item)
	}
	items = filtered

	filteredShareIDs := make([]int, 0, len(items))
	for _, item := range items {
		filteredShareIDs = append(filteredShareIDs, item.ShareID)
	}
	airportSummary := buildShareAirportTraffic(filteredShareIDs, windowStart)

	filteredDeviceIDs := make([]int, 0, len(devices))
	for _, item := range items {
		for _, device := range devicesByShare[item.ShareID] {
			filteredDeviceIDs = append(filteredDeviceIDs, device.ID)
		}
	}
	dailyBuckets, err := models.ListShareDeviceUsage(filteredDeviceIDs, windowStart)
	if err != nil {
		utils.Warn("Failed to load share daily usage: %v", err)
		dailyBuckets = nil
	}
	daily := buildShareDailyBuckets(dailyBuckets, days)

	end := time.Now()
	summary := gin.H{
		"total":                  len(items),
		"enabled":                0,
		"expired":                0,
		"disabled":               0,
		"online":                 0,
		"devices":                0,
		"window_traffic_bytes":   int64(0),
		"reported_traffic_bytes": int64(0),
		"problem_nodes":          0,
	}
	enabled, expired, disabled, online, deviceTotal, problemTotal := 0, 0, 0, 0, 0, 0
	windowTotal, reportedTotal := int64(0), int64(0)
	for _, item := range items {
		switch item.Status {
		case "expired":
			expired++
		case "disabled":
			disabled++
		default:
			enabled++
		}
		if item.OnlineNow {
			online++
		}
		deviceTotal += item.Devices.Total
		problemTotal += item.ProblemNodes
		windowTotal += item.WindowBytes
		reportedTotal += item.LifetimeBytes
	}
	summary["enabled"] = enabled
	summary["expired"] = expired
	summary["disabled"] = disabled
	summary["online"] = online
	summary["devices"] = deviceTotal
	summary["window_traffic_bytes"] = windowTotal
	summary["reported_traffic_bytes"] = reportedTotal
	summary["problem_nodes"] = problemTotal

	sortShareSubscriptionItems(items)
	page, pageSize := shareDevicePagination(c)
	start := (page - 1) * pageSize
	if start > len(items) {
		start = len(items)
	}
	stop := start + pageSize
	if stop > len(items) {
		stop = len(items)
	}
	pageItems := items[start:stop]

	// Last session rows are only needed for the visible page.
	pageShareIDs := make([]int, 0, len(pageItems))
	for _, item := range pageItems {
		pageShareIDs = append(pageShareIDs, item.ShareID)
	}
	latestSessions, err := models.LatestShareSessions(pageShareIDs)
	if err != nil {
		utils.Warn("Failed to load latest share sessions: %v", err)
		latestSessions = map[int]models.ShareSession{}
	}
	for _, item := range pageItems {
		if session, ok := latestSessions[item.ShareID]; ok {
			item.LastSession = &shareSubscriptionLastSession{
				StartedAt: session.StartedAt,
				EndedAt:   session.EndedAt,
				Bytes:     session.Bytes,
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"items":        pageItems,
			"total":        len(items),
			"page":         page,
			"page_size":    pageSize,
			"summary":      summary,
			"top_airports": airportSummary,
			"daily":        daily,
			"window_days":  days,
			"generated_at": end,
		},
	})
}

// ShareSubscriptionDetail returns one link with its devices, daily traffic,
// sessions, node/airport traffic, reliability snapshot and recent events.
//
//	GET /api/v1/shares/subscriptions/detail?shareId=&days=
func ShareSubscriptionDetail(c *gin.Context) {
	shareID, err := strconv.Atoi(c.Query("shareId"))
	if err != nil || shareID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 400, "msg": "Invalid share id"})
		return
	}
	share := &models.SubscriptionShare{ID: shareID}
	if err := share.Find(); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Share link not found"})
		return
	}
	// Keep the cached row in sync with the link metadata index.
	links := buildShareDeviceLinkIndex([]models.SubscriptionShare{*share})
	entry, hasEntry := links[share.ID]
	if !hasEntry {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "msg": "Share link not found"})
		return
	}

	days := shareTelemetryDaysParam(c)
	now := time.Now()
	since := now.AddDate(0, 0, -days)

	devices, err := models.ListShareDevices(shareID)
	if err != nil {
		utils.Error("Failed to load share devices: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "msg": "Failed to load devices"})
		return
	}
	deviceIDs := make([]int, 0, len(devices))
	for _, device := range devices {
		deviceIDs = append(deviceIDs, device.ID)
	}
	windowSums, err := models.SumShareDeviceUsage(deviceIDs, since)
	if err != nil {
		utils.Warn("Failed to sum share device usage: %v", err)
		windowSums = map[int]int64{}
	}
	sessionCounts, err := models.CountShareSessionsByDevice(deviceIDs, since)
	if err != nil {
		utils.Warn("Failed to count share sessions: %v", err)
		sessionCounts = map[int]int64{}
	}

	deviceEntries := make([]gin.H, 0, len(devices))
	for _, device := range devices {
		deviceEntries = append(deviceEntries, gin.H{
			"id":                     device.ID,
			"name":                   device.Name,
			"hwid":                   device.HWID,
			"device_id":              device.DeviceID,
			"platform":               device.Platform,
			"app_version":            device.AppVersion,
			"status":                 device.Status,
			"reported_traffic_bytes": device.ReportedTrafficBytes,
			"window_traffic_bytes":   windowSums[device.ID],
			"sessions_count":         sessionCounts[device.ID],
			"last_seen_at":           device.LastSeenAt,
			"last_report_at":         device.LastReportAt,
		})
	}

	buckets, err := models.ListShareDeviceUsage(deviceIDs, since)
	if err != nil {
		utils.Warn("Failed to load share device usage: %v", err)
		buckets = nil
	}
	daily := buildShareDailyBuckets(buckets, days)

	sessions, err := models.ListShareSessions([]int{shareID}, since, shareSubscriptionSessionLimit)
	if err != nil {
		utils.Warn("Failed to load share sessions: %v", err)
		sessions = nil
	}
	sessionEntries := buildShareSessionEntries(sessions, devices, now)

	activity := convertShareActivityEntries(mustListShareAccessLogs(shareID, shareSubscriptionActivityLimit), map[int]string{shareID: share.Name})

	nodes, airports := buildShareNodeTraffic([]int{shareID}, nil, since)
	reliability, reliabilityEvents := buildShareReliability([]int{shareID}, nil, since)
	reliable := buildShareReliableNodes([]int{shareID}, nil)
	reputation := buildShareNodeReputation([]int{shareID}, nil)

	c.JSON(http.StatusOK, gin.H{
		"code": 200,
		"data": gin.H{
			"share":              buildShareSubscriptionMeta(share, entry),
			"devices":            deviceEntries,
			"daily":              daily,
			"sessions":           sessionEntries,
			"session_stats":      buildShareSessionStats(sessionEntries),
			"events":             activity,
			"nodes":              nodes,
			"airports":           airports,
			"reliability":        reliability,
			"reliable":           reliable,
			"node_reputation":    reputation,
			"reliability_events": reliabilityEvents,
			"window_days":        days,
		},
	})
}

func buildShareSubscriptionOverviewItem(share *models.SubscriptionShare, entry shareDeviceShareEntry, clientNames map[int]string, devices []models.ShareDevice, todaySums, windowSums map[int]int64, sessionSummary models.ShareSessionSummary, problemNodes int) *shareSubscriptionOverviewItem {
	item := &shareSubscriptionOverviewItem{
		ShareID:              share.ID,
		ShareName:            share.Name,
		Token:                share.Token,
		GroupID:              share.GroupID,
		GroupName:            entry.GroupName,
		SubscriptionID:       share.SubscriptionID,
		SubscriptionName:     entry.SubscriptionName,
		ClientID:             share.ClientID,
		Status:               shareSubscriptionStatus(share),
		TelemetryEnabled:     shareTelemetryEnabled(share),
		TrafficLimitBytes:    share.TrafficLimitBytes,
		ReportedTrafficBytes: share.ReportedTrafficBytes,
		RemainingBytes:       shareRemainingBytes(share),
		QuotaExhausted:       share.IsQuotaExhausted(),
		LifetimeBytes:        share.ReportedTrafficBytes,
		SessionsToday:        sessionSummary.Sessions,
		OnlineNow:            sessionSummary.OpenSessions > 0,
		ProblemNodes:         problemNodes,
		DeviceList:           make([]shareSubscriptionDeviceEntry, 0, len(devices)),
	}
	if share.ClientID != nil {
		item.ClientName = clientNames[*share.ClientID]
	}

	searchParts := []string{item.ShareName, item.Token, item.GroupName, item.SubscriptionName, item.ClientName}
	for _, device := range devices {
		item.Devices.Total++
		switch device.Status {
		case models.ShareDeviceStatusPending:
			item.Devices.Pending++
		case models.ShareDeviceStatusBlocked:
			item.Devices.Blocked++
		default:
			item.Devices.Active++
		}
		item.TodayBytes += todaySums[device.ID]
		item.WindowBytes += windowSums[device.ID]
		item.LastSeenAt = laterTimePtr(item.LastSeenAt, timePtr(device.LastSeenAt))
		item.LastReportAt = laterTimePtr(item.LastReportAt, device.LastReportAt)
		searchParts = append(searchParts, device.Name, device.HWID, device.DeviceID, device.Platform)
		if len(item.DeviceList) < shareSubscriptionDeviceListLimit {
			item.DeviceList = append(item.DeviceList, shareSubscriptionDeviceEntry{
				ID:                   device.ID,
				Name:                 device.Name,
				HWID:                 device.HWID,
				DeviceID:             device.DeviceID,
				Platform:             device.Platform,
				Status:               device.Status,
				ReportedTrafficBytes: device.ReportedTrafficBytes,
				WindowTrafficBytes:   windowSums[device.ID],
				LastSeenAt:           device.LastSeenAt,
				LastReportAt:         device.LastReportAt,
			})
		}
	}
	item.LastSeenAt = laterTimePtr(item.LastSeenAt, share.LastAccessAt)
	item.LastReportAt = laterTimePtr(item.LastReportAt, share.LastUsageAt)
	item.searchText = strings.ToLower(strings.Join(searchParts, "\x00"))
	return item
}

func sortShareSubscriptionItems(items []*shareSubscriptionOverviewItem) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].OnlineNow != items[j].OnlineNow {
			return items[i].OnlineNow
		}
		left, right := timeOrZero(items[i].LastReportAt), timeOrZero(items[j].LastReportAt)
		if !left.Equal(right) {
			return left.After(right)
		}
		if items[i].WindowBytes != items[j].WindowBytes {
			return items[i].WindowBytes > items[j].WindowBytes
		}
		return items[i].ShareName < items[j].ShareName
	})
}

func shareSubscriptionStatus(share *models.SubscriptionShare) string {
	if share == nil {
		return "disabled"
	}
	if !share.Enabled {
		return "disabled"
	}
	if share.IsExpired() {
		return "expired"
	}
	return "enabled"
}

func shareTelemetryEnabled(share *models.SubscriptionShare) bool {
	group := loadShareGroup(share)
	return group != nil && group.TelemetryEnabled && group.Enabled
}

func buildShareSubscriptionMeta(share *models.SubscriptionShare, entry shareDeviceShareEntry) shareSubscriptionMeta {
	meta := shareSubscriptionMeta{
		ShareID:              share.ID,
		ShareName:            share.Name,
		Token:                share.Token,
		GroupID:              share.GroupID,
		GroupName:            entry.GroupName,
		SubscriptionID:       share.SubscriptionID,
		SubscriptionName:     entry.SubscriptionName,
		ClientID:             share.ClientID,
		Status:               shareSubscriptionStatus(share),
		TelemetryEnabled:     shareTelemetryEnabled(share),
		TrafficLimitBytes:    share.TrafficLimitBytes,
		ReportedTrafficBytes: share.ReportedTrafficBytes,
		RemainingBytes:       shareRemainingBytes(share),
		QuotaExhausted:       share.IsQuotaExhausted(),
		AccessCount:          share.AccessCount,
		CreatedAt:            share.CreatedAt,
		LastAccessAt:         share.LastAccessAt,
		LastUsageAt:          share.LastUsageAt,
	}
	if share.ClientID != nil {
		if client, err := models.GetShareClientByID(*share.ClientID); err == nil {
			meta.ClientName = client.Name
		}
	}
	return meta
}

func shareTelemetryDaysParam(c *gin.Context) int {
	days, err := strconv.Atoi(c.DefaultQuery("days", ""))
	if err != nil {
		return models.ShareDeviceUsageWindowDays
	}
	return models.NormalizeShareTelemetryDays(days)
}

func localMidnight(at time.Time) time.Time {
	return time.Date(at.Year(), at.Month(), at.Day(), 0, 0, 0, 0, at.Location())
}

func timePtr(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	return &value
}

func timeOrZero(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

// buildShareDailyBuckets folds hourly buckets into local calendar days and
// fills missing days with zeros so charts keep a stable x axis.
func buildShareDailyBuckets(buckets []models.ShareDeviceUsageHour, days int) []shareDailyBucket {
	now := time.Now()
	start := localMidnight(now).AddDate(0, 0, -(days - 1))
	result := make([]shareDailyBucket, 0, days)
	for i := 0; i < days; i++ {
		key := start.AddDate(0, 0, i).Format("2006-01-02")
		result = append(result, shareDailyBucket{Day: key})
	}
	index := make(map[string]int, days)
	for i := range result {
		index[result[i].Day] = i
	}
	for _, bucket := range buckets {
		key := bucket.WindowStart.In(now.Location()).Format("2006-01-02")
		position, ok := index[key]
		if !ok {
			continue
		}
		result[position].UpBytes += bucket.UpBytes
		result[position].DownBytes += bucket.DownBytes
		result[position].Bytes += bucket.Bytes
	}
	return result
}

func buildShareSessionEntries(sessions []models.ShareSession, devices []models.ShareDevice, now time.Time) []shareSessionDetailEntry {
	names := make(map[int]string, len(devices))
	for _, device := range devices {
		names[device.ID] = device.Name
	}
	entries := make([]shareSessionDetailEntry, 0, len(sessions))
	for _, session := range sessions {
		duration := int64(now.Sub(session.StartedAt).Seconds())
		if session.Closed && session.EndedAt != nil {
			duration = int64(session.EndedAt.Sub(session.StartedAt).Seconds())
		}
		if duration < 0 {
			duration = 0
		}
		entries = append(entries, shareSessionDetailEntry{
			ID:              session.ID,
			DeviceID:        session.DeviceID,
			DeviceName:      names[session.DeviceID],
			HWID:            session.HWID,
			DeviceKey:       session.DeviceKey,
			SessionKey:      session.SessionKey,
			StartedAt:       session.StartedAt,
			EndedAt:         session.EndedAt,
			DurationSeconds: duration,
			UpBytes:         session.UpBytes,
			DownBytes:       session.DownBytes,
			Bytes:           session.Bytes,
			Open:            !session.Closed,
		})
	}
	return entries
}

// shareSessionStats summarizes the VPN activity of the scope: how many times
// the client turned on and off, how long it stayed on and the traffic of those
// intervals. Activity time is deliberately separate from the node table: an
// idle session moves no bytes and a busy one is not "longer".
type shareSessionStats struct {
	Sessions      int        `json:"sessions"`
	OpenSessions  int        `json:"open_sessions"`
	ActiveSeconds int64      `json:"active_seconds"`
	Bytes         int64      `json:"bytes"`
	LastStartedAt *time.Time `json:"last_started_at"`
	LastEndedAt   *time.Time `json:"last_ended_at"`
	LastOpen      bool       `json:"last_open"`
}

func buildShareSessionStats(entries []shareSessionDetailEntry) shareSessionStats {
	stats := shareSessionStats{}
	if len(entries) == 0 {
		return stats
	}
	newest := entries[0]
	for _, entry := range entries {
		stats.Sessions++
		if entry.Open {
			stats.OpenSessions++
		}
		stats.ActiveSeconds += entry.DurationSeconds
		stats.Bytes += entry.Bytes
		if entry.StartedAt.After(newest.StartedAt) {
			newest = entry
		}
	}
	started := newest.StartedAt
	stats.LastStartedAt = &started
	stats.LastEndedAt = newest.EndedAt
	stats.LastOpen = newest.Open
	return stats
}

// buildShareSubscriptionTraffic returns the busiest nodes and airports of
// every share link inside the window. It issues one grouped query and caps
// each list so the overview keeps a fixed-size payload; the airport totals of
// the whole selection come from buildShareAirportTraffic, so the summary stays
// exact even when a card list is capped.
func buildShareSubscriptionTraffic(shareIDs []int, since time.Time) (map[int][]shareNodeTrafficEntry, map[int][]shareAirportTrafficEntry) {
	nodes := make(map[int][]shareNodeTrafficEntry, len(shareIDs))
	airports := make(map[int][]shareAirportTrafficEntry, len(shareIDs))
	sums, err := models.SumShareNodeUsageByShare(shareIDs, since)
	if err != nil {
		utils.Warn("Failed to sum share node usage by share: %v", err)
		return nodes, airports
	}
	if len(sums) == 0 {
		return nodes, airports
	}
	keys := make([]string, 0, len(sums))
	for _, row := range sums {
		keys = append(keys, row.NodeKey)
	}
	infos := models.ResolveShareNodeKeys(keys)
	byShare := make(map[int][]shareNodeTrafficEntry, len(shareIDs))
	airportTotals := make(map[int]map[int]*shareAirportTrafficEntry, len(shareIDs))
	shareTotals := make(map[int]int64, len(shareIDs))
	for _, row := range sums {
		info := infos[row.NodeKey]
		byShare[row.ShareID] = append(byShare[row.ShareID], shareNodeTrafficEntry{
			Key:         row.NodeKey,
			Name:        info.Name,
			Group:       info.Group,
			Country:     info.Country,
			AirportID:   info.AirportID,
			AirportName: info.AirportName,
			UpBytes:     row.UpBytes,
			DownBytes:   row.DownBytes,
			Bytes:       row.Bytes,
		})
		shareTotals[row.ShareID] += row.Bytes
		if info.AirportID > 0 {
			byAirport := airportTotals[row.ShareID]
			if byAirport == nil {
				byAirport = make(map[int]*shareAirportTrafficEntry)
				airportTotals[row.ShareID] = byAirport
			}
			aggregate := byAirport[info.AirportID]
			if aggregate == nil {
				aggregate = &shareAirportTrafficEntry{AirportID: info.AirportID, AirportName: info.AirportName}
				byAirport[info.AirportID] = aggregate
			}
			aggregate.Bytes += row.Bytes
		}
	}
	for shareID, entries := range byShare {
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Bytes > entries[j].Bytes })
		if len(entries) > shareSubscriptionTopNodeLimit {
			entries = entries[:shareSubscriptionTopNodeLimit]
		}
		nodes[shareID] = entries
	}
	for shareID, byAirport := range airportTotals {
		entries := make([]shareAirportTrafficEntry, 0, len(byAirport))
		for _, aggregate := range byAirport {
			if shareTotals[shareID] > 0 {
				aggregate.Share = float64(aggregate.Bytes) / float64(shareTotals[shareID])
			}
			entries = append(entries, *aggregate)
		}
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Bytes > entries[j].Bytes })
		if len(entries) > shareSubscriptionTopNodeLimit {
			entries = entries[:shareSubscriptionTopNodeLimit]
		}
		airports[shareID] = entries
	}
	return nodes, airports
}

// buildShareAirportTraffic rolls the share scope up into airport rows, so the
// analyst reads "where does the traffic of this selection go" before opening a
// single card. It aggregates the uncapped buckets, unlike the per-card lists.
func buildShareAirportTraffic(shareIDs []int, since time.Time) []shareAirportTrafficEntry {
	sums, err := models.SumShareNodeUsageByShare(shareIDs, since)
	if err != nil {
		utils.Warn("Failed to sum share node usage by share: %v", err)
		return []shareAirportTrafficEntry{}
	}
	if len(sums) == 0 {
		return []shareAirportTrafficEntry{}
	}
	keys := make([]string, 0, len(sums))
	for _, row := range sums {
		keys = append(keys, row.NodeKey)
	}
	infos := models.ResolveShareNodeKeys(keys)
	totals := make(map[int]*shareAirportTrafficEntry)
	allBytes := int64(0)
	for _, row := range sums {
		info := infos[row.NodeKey]
		if info.AirportID <= 0 {
			continue
		}
		aggregate := totals[info.AirportID]
		if aggregate == nil {
			aggregate = &shareAirportTrafficEntry{AirportID: info.AirportID, AirportName: info.AirportName}
			totals[info.AirportID] = aggregate
		}
		aggregate.Bytes += row.Bytes
		allBytes += row.Bytes
	}
	entries := make([]shareAirportTrafficEntry, 0, len(totals))
	for _, aggregate := range totals {
		if allBytes > 0 {
			aggregate.Share = float64(aggregate.Bytes) / float64(allBytes)
		}
		entries = append(entries, *aggregate)
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Bytes > entries[j].Bytes })
	return entries
}

// buildShareReliableNodes returns the healthiest current snapshots of the
// scope: the nodes the installations keep reporting as working. It answers the
// mirror question of buildShareReliability, so the analyst can compare the
// nodes that carry traffic well with the ones that keep failing.
func buildShareReliableNodes(shareIDs []int, deviceIDs []int) []shareReliabilityEntry {
	snapshots, err := models.ListTopReliableShareNodeReliability(shareIDs, deviceIDs, shareSubscriptionReliableLimit)
	if err != nil {
		utils.Warn("Failed to load reliable share nodes: %v", err)
		return []shareReliabilityEntry{}
	}
	entries, _ := buildShareReliabilityEntries(snapshots)
	return entries
}

// buildShareNodeReputation returns the whole reputation table of the scope:
// every node the installations scored, problems first. It is what the node
// table joins with traffic, so a node's health sits next to the bytes it
// actually carried instead of showing up only once it fails.
func buildShareNodeReputation(shareIDs []int, deviceIDs []int) []shareReliabilityEntry {
	snapshots, err := models.ListShareNodeReliability(shareIDs, deviceIDs, shareSubscriptionReputationLimit)
	if err != nil {
		utils.Warn("Failed to load share node reputation: %v", err)
		return []shareReliabilityEntry{}
	}
	entries, _ := buildShareReliabilityEntries(snapshots)
	return entries
}

// buildShareReliabilityEntries maps stored snapshots to the API shape every
// reliability block shares and resolves the node keys to display metadata.
// One node may be scored by several installations, so only the first snapshot
// of a key is kept: the queries order problems first and that is the state the
// blocks show.
func buildShareReliabilityEntries(snapshots []models.ShareNodeReliability) ([]shareReliabilityEntry, map[string]models.ShareNodeInfo) {
	keys := make([]string, 0, len(snapshots))
	for _, row := range snapshots {
		keys = append(keys, row.NodeKey)
	}
	infos := models.ResolveShareNodeKeys(keys)
	entries := make([]shareReliabilityEntry, 0, len(snapshots))
	seen := make(map[string]struct{}, len(snapshots))
	for _, row := range snapshots {
		if _, exists := seen[row.NodeKey]; exists {
			continue
		}
		seen[row.NodeKey] = struct{}{}
		info := infos[row.NodeKey]
		entries = append(entries, shareReliabilityEntry{
			Key:         row.NodeKey,
			Name:        info.Name,
			AirportName: info.AirportName,
			Score:       row.Score,
			Fails:       row.Fails,
			Streak:      row.Streak,
			Silent:      row.Silent,
			State:       row.State,
			LastSeenAt:  row.LastSeenAt,
			ReportedAt:  row.ReportedAt,
			UpdatedAt:   row.UpdatedAt,
		})
	}
	return entries, infos
}

// buildShareNodeTraffic aggregates per-node buckets and rolls them up into
// airport rows. When deviceIDs is non empty the scope is the installation,
// otherwise the share links.
func buildShareNodeTraffic(shareIDs []int, deviceIDs []int, since time.Time) ([]shareNodeTrafficEntry, []shareAirportTrafficEntry) {
	sums, err := models.SumShareNodeUsage(shareIDs, deviceIDs, since)
	if err != nil {
		utils.Warn("Failed to sum share node usage: %v", err)
		return []shareNodeTrafficEntry{}, []shareAirportTrafficEntry{}
	}
	keys := make([]string, 0, len(sums))
	for _, row := range sums {
		keys = append(keys, row.NodeKey)
	}
	infos := models.ResolveShareNodeKeys(keys)

	nodes := make([]shareNodeTrafficEntry, 0, len(sums))
	airportTotals := make(map[int]*shareAirportTrafficEntry)
	totalBytes := int64(0)
	for _, row := range sums {
		info := infos[row.NodeKey]
		entry := shareNodeTrafficEntry{
			Key:         row.NodeKey,
			Name:        info.Name,
			Group:       info.Group,
			Country:     info.Country,
			AirportID:   info.AirportID,
			AirportName: info.AirportName,
			UpBytes:     row.UpBytes,
			DownBytes:   row.DownBytes,
			Bytes:       row.Bytes,
		}
		nodes = append(nodes, entry)
		totalBytes += row.Bytes
		if info.AirportID > 0 {
			aggregate, ok := airportTotals[info.AirportID]
			if !ok {
				aggregate = &shareAirportTrafficEntry{AirportID: info.AirportID, AirportName: info.AirportName}
				airportTotals[info.AirportID] = aggregate
			}
			aggregate.Bytes += row.Bytes
		}
	}
	airports := make([]shareAirportTrafficEntry, 0, len(airportTotals))
	for _, aggregate := range airportTotals {
		if totalBytes > 0 {
			aggregate.Share = float64(aggregate.Bytes) / float64(totalBytes)
		}
		airports = append(airports, *aggregate)
	}
	sort.SliceStable(airports, func(i, j int) bool { return airports[i].Bytes > airports[j].Bytes })
	sort.SliceStable(nodes, func(i, j int) bool { return nodes[i].Bytes > nodes[j].Bytes })
	return nodes, airports
}

func buildShareReliability(shareIDs []int, deviceIDs []int, since time.Time) ([]shareReliabilityEntry, []shareReliabilityEventEntry) {
	snapshots, err := models.ListProblemShareNodeReliability(shareIDs, deviceIDs)
	if err != nil {
		utils.Warn("Failed to load share node reliability: %v", err)
		return []shareReliabilityEntry{}, []shareReliabilityEventEntry{}
	}
	entries, infos := buildShareReliabilityEntries(snapshots)

	rows, eventErr := listShareReliabilityEventRows(shareIDs, deviceIDs, since)
	if eventErr != nil {
		utils.Warn("Failed to load share reliability events: %v", eventErr)
		return entries, []shareReliabilityEventEntry{}
	}
	return entries, buildShareReliabilityEventEntries(rows, infos)
}

// listShareReliabilityEventRows picks the query scope: a device-scoped view
// (installations) or a link-scoped view.
func listShareReliabilityEventRows(shareIDs []int, deviceIDs []int, since time.Time) ([]models.ShareNodeReliabilityEvent, error) {
	if len(deviceIDs) > 0 {
		return models.ListShareReliabilityEventsForDevices(deviceIDs, since, shareSubscriptionEventLimit)
	}
	return models.ListShareReliabilityEvents(shareIDs, since, shareSubscriptionEventLimit)
}

func buildShareReliabilityEventEntries(rows []models.ShareNodeReliabilityEvent, infos map[string]models.ShareNodeInfo) []shareReliabilityEventEntry {
	entries := make([]shareReliabilityEventEntry, 0, len(rows))
	for _, row := range rows {
		info, ok := infos[row.NodeKey]
		if !ok || info.Name == "" {
			if extra := models.ResolveShareNodeKeys([]string{row.NodeKey}); len(extra) > 0 {
				info = extra[row.NodeKey]
			}
		}
		entries = append(entries, shareReliabilityEventEntry{
			ID:        row.ID,
			NodeKey:   row.NodeKey,
			NodeName:  info.Name,
			FromState: row.FromState,
			ToState:   row.ToState,
			FromScore: row.FromScore,
			ToScore:   row.ToScore,
			Silent:    row.Silent,
			Reason:    row.Reason,
			CreatedAt: row.CreatedAt,
		})
	}
	return entries
}

func mustListShareAccessLogs(shareID, limit int) []models.ShareAccessLog {
	logs, err := models.ListShareAccessLogs(shareID, limit)
	if err != nil {
		utils.Warn("Failed to load share access logs: %v", err)
		return nil
	}
	return logs
}

func convertShareActivityEntries(logs []models.ShareAccessLog, shareNames map[int]string) []shareDeviceActivityEntry {
	entries := make([]shareDeviceActivityEntry, 0, len(logs))
	for _, entry := range logs {
		entries = append(entries, shareDeviceActivityEntry{
			ID:        entry.ID,
			ShareID:   entry.ShareID,
			ShareName: shareNames[entry.ShareID],
			Action:    entry.Action,
			Template:  entry.Template,
			Reason:    entry.Reason,
			CreatedAt: entry.CreatedAt,
		})
	}
	return entries
}
