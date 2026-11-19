package api

import (
	"net/http"
	"sort"
	"strings"
	"time"

	"sublink/models"
	"sublink/utils"

	"github.com/gin-gonic/gin"
)

// Caps of the auxiliary telemetry channels. They keep one broken client from
// forcing an unbounded write transaction.
const (
	shareNodeReportMaxRows        = 200
	shareReliabilityReportMaxRows = 100
)

// shareIngestContext carries the resolved link/group/device of an auxiliary
// telemetry channel request.
type shareIngestContext struct {
	Share        *models.SubscriptionShare
	Device       *models.ShareDevice
	HWID         string
	DeviceKey    string
	DeviceStatus string
}

// resolveShareIngest applies the same token/group/expiry/device policy as the
// traffic endpoint. It writes the error response itself and reports success.
func resolveShareIngest(c *gin.Context) (*shareIngestContext, bool) {
	token := strings.ToLower(strings.TrimSpace(c.Query("token")))
	if token == "" {
		utils.Result(c, http.StatusNotFound, http.StatusNotFound, "Invalid share token", nil)
		return nil, false
	}
	share, err := models.GetSubscriptionShareByToken(token)
	if err != nil {
		utils.Result(c, http.StatusNotFound, http.StatusNotFound, "Invalid share token", nil)
		return nil, false
	}
	group := loadShareGroup(share)
	if group == nil || !group.TelemetryEnabled {
		utils.Forbidden(c, "Telemetry is not enabled for this link")
		return nil, false
	}
	if !group.Enabled {
		utils.Forbidden(c, "This subscription group is disabled")
		return nil, false
	}
	if share.IsExpired() {
		utils.Forbidden(c, "This share link is not active")
		return nil, false
	}

	device, deviceStatus, deviceErr := resolveShareTelemetryDevice(c, share)
	if deviceErr != nil {
		utils.Forbidden(c, deviceErr.Error())
		return nil, false
	}
	hwid, deviceKey := shareTelemetryDeviceIdentity(c, device)
	return &shareIngestContext{
		Share:        share,
		Device:       device,
		HWID:         hwid,
		DeviceKey:    deviceKey,
		DeviceStatus: deviceStatus,
	}, true
}

// shareTelemetryDeviceIdentity resolves the reporting installation identity:
// explicit headers win, a bound device supplies the fallback.
func shareTelemetryDeviceIdentity(c *gin.Context, device *models.ShareDevice) (string, string) {
	hwid := models.NormalizeShareDeviceValue(c.GetHeader("X-HWID"), 128)
	deviceKey := models.NormalizeShareDeviceValue(c.GetHeader("X-Device-ID"), 128)
	if device != nil {
		if hwid == "" {
			hwid = device.HWID
		}
		if deviceKey == "" {
			deviceKey = device.DeviceID
		}
	}
	return hwid, deviceKey
}

type shareSessionEventRequest struct {
	Event     string `json:"event"`
	SessionID string `json:"session_id"`
	// The counters are accepted for forward compatibility but never applied:
	// /c/telemetry already owns the share and device counters, applying them
	// here would double count.
	Up                 *int64 `json:"up"`
	Down               *int64 `json:"down"`
	ReportedTotalBytes *int64 `json:"reported_total_bytes"`
	InstallID          string `json:"install_id"`
	ReportID           string `json:"report_id"`
}

type shareSessionEventResponse struct {
	Event        string            `json:"event"`
	SessionID    string            `json:"session_id"`
	Accepted     bool              `json:"accepted"`
	Duplicate    bool              `json:"duplicate"`
	Ignored      bool              `json:"ignored,omitempty"`
	Session      *shareSessionView `json:"session,omitempty"`
	DeviceStatus string            `json:"device_status"`
}

type shareSessionView struct {
	ID           int        `json:"id"`
	SessionKey   string     `json:"session_key"`
	StartedAt    time.Time  `json:"started_at"`
	EndedAt      *time.Time `json:"ended_at"`
	LastReportAt time.Time  `json:"last_report_at"`
	UpBytes      int64      `json:"up_bytes"`
	DownBytes    int64      `json:"down_bytes"`
	Bytes        int64      `json:"bytes"`
	Open         bool       `json:"open"`
}

func newShareSessionView(session *models.ShareSession) *shareSessionView {
	if session == nil {
		return nil
	}
	return &shareSessionView{
		ID:           session.ID,
		SessionKey:   session.SessionKey,
		StartedAt:    session.StartedAt,
		EndedAt:      session.EndedAt,
		LastReportAt: session.LastReportAt,
		UpBytes:      session.UpBytes,
		DownBytes:    session.DownBytes,
		Bytes:        session.Bytes,
		Open:         !session.Closed,
	}
}

// ShareReportSession ingests an explicit "client turned on/off" event:
//
//	POST /c/sessions?token=<share token>
//	{"event": "session_start", "session_id": "ab12cd34"}
//	{"event": "session_stop", "session_id": "ab12cd34"}
//
// Sessions are a read-only view of intervals; traffic is attributed by regular
// telemetry reports that carry the same session_id.
func ShareReportSession(c *gin.Context) {
	ingest, ok := resolveShareIngest(c)
	if !ok {
		return
	}

	var req shareSessionEventRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "Invalid session payload: "+err.Error(), nil)
		return
	}
	event := strings.ToLower(strings.TrimSpace(req.Event))
	sessionKey := models.NormalizeShareSessionKey(req.SessionID)
	if sessionKey == "" {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "session_id is required", nil)
		return
	}
	if event != "session_start" && event != "session_stop" {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "event must be session_start or session_stop", nil)
		return
	}

	now := time.Now()
	response := shareSessionEventResponse{
		Event:        event,
		SessionID:    sessionKey,
		DeviceStatus: ingest.DeviceStatus,
	}

	if ingest.HWID == "" || ingest.DeviceKey == "" {
		// Without an installation identity a session cannot be attributed.
		response.Ignored = true
		utils.OkDetailed(c, "Ignored", response)
		return
	}

	switch event {
	case "session_start":
		session, err := models.StartShareSession(ingest.Share.ID, ingest.Device, ingest.HWID, ingest.DeviceKey, sessionKey, now)
		if err != nil {
			utils.FailWithMsg(c, "Failed to start session: "+err.Error())
			return
		}
		response.Accepted = true
		response.Session = newShareSessionView(session)
	case "session_stop":
		closed, err := models.CloseShareSession(ingest.Share.ID, ingest.HWID, ingest.DeviceKey, sessionKey, now)
		if err != nil {
			utils.FailWithMsg(c, "Failed to stop session: "+err.Error())
			return
		}
		response.Accepted = closed
		response.Duplicate = !closed
	}

	utils.OkDetailed(c, "Success", response)
}

type shareNodeTrafficRow struct {
	Key  string `json:"key"`
	Up   int64  `json:"up"`
	Down int64  `json:"down"`
}

type shareNodesRequest struct {
	ReportID      string                `json:"report_id"`
	SessionID     string                `json:"session_id"`
	InstallID     string                `json:"install_id"`
	WindowSeconds int                   `json:"window_seconds"`
	Nodes         []shareNodeTrafficRow `json:"nodes"`
}

type shareNodesResponse struct {
	AcceptedRows int    `json:"accepted_rows"`
	IgnoredRows  int    `json:"ignored_rows"`
	SkippedRows  int    `json:"skipped_rows"`
	Duplicate    bool   `json:"duplicate"`
	Ignored      bool   `json:"ignored,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// ShareReportNodes ingests per-node traffic deltas:
//
//	POST /c/nodes?token=<share token>
//	{"report_id": "...", "nodes": [{"key": "0123...cdef", "up": 1, "down": 2}]}
//
// Rows are stored per installation/node/hour; the node name, pool and airport
// are resolved on the server from the stable sublinkpro_unique_key.
func ShareReportNodes(c *gin.Context) {
	ingest, ok := resolveShareIngest(c)
	if !ok {
		return
	}

	var req shareNodesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "Invalid nodes payload: "+err.Error(), nil)
		return
	}
	if len(req.Nodes) == 0 {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "nodes are required", nil)
		return
	}
	if len(req.Nodes) > shareNodeReportMaxRows {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "too many node rows", nil)
		return
	}

	now := time.Now()
	if ingest.Device == nil {
		utils.OkDetailed(c, "Ignored", shareNodesResponse{Ignored: true, Reason: "device not bound", IgnoredRows: len(req.Nodes)})
		return
	}

	// Deduplicate by report id so a retried batch does not double count.
	reportID := models.NormalizeShareDeviceValue(req.ReportID, shareTelemetryIDMaxLen)
	if reportID != "" {
		dedupKey := shareTelemetryReportKey(ingest.Share.ID, "nodes|"+shareTelemetryScope(c), reportID)
		if !claimShareTelemetryReportID(dedupKey, now) {
			utils.OkDetailed(c, "Duplicate", shareNodesResponse{Duplicate: true})
			return
		}
	}

	// Merge duplicate keys inside one batch so the same node is not added
	// twice by a single report.
	type nodeTotals struct {
		up      int64
		down    int64
		ignored bool
	}
	order := make([]string, 0, len(req.Nodes))
	totals := make(map[string]*nodeTotals, len(req.Nodes))
	for _, row := range req.Nodes {
		key := models.NormalizeShareNodeKey(row.Key)
		if key == "" {
			continue
		}
		if row.Up < 0 || row.Down < 0 || row.Up > shareTelemetryMaxBytesPerReport || row.Down > shareTelemetryMaxBytesPerReport {
			if entry, exists := totals[key]; exists {
				entry.ignored = true
			} else {
				order = append(order, key)
				totals[key] = &nodeTotals{ignored: true}
			}
			continue
		}
		entry, exists := totals[key]
		if !exists {
			entry = &nodeTotals{}
			order = append(order, key)
			totals[key] = entry
		}
		entry.up += row.Up
		entry.down += row.Down
	}

	response := shareNodesResponse{}
	for _, key := range order {
		entry := totals[key]
		if entry.ignored || entry.up+entry.down <= 0 {
			response.SkippedRows++
			continue
		}
		if err := models.RecordShareNodeUsage(ingest.Device, key, entry.up, entry.down, now); err != nil {
			utils.Warn("Failed to record share node usage: %v", err)
			response.SkippedRows++
			continue
		}
		response.AcceptedRows++
	}
	response.IgnoredRows = len(req.Nodes) - len(order)
	utils.OkDetailed(c, "Success", response)
}

type shareReliabilityRow struct {
	Key       string     `json:"key"`
	Score     int        `json:"score"`
	Fails     int        `json:"fails"`
	Streak    int        `json:"streak"`
	Silent    bool       `json:"silent"`
	LastSeen  *time.Time `json:"last_seen"`
	Recovered bool       `json:"recovered"`
}

type shareReliabilityRequest struct {
	ReportID   string                `json:"report_id"`
	InstallID  string                `json:"install_id"`
	ReportedAt *time.Time            `json:"reported_at"`
	Nodes      []shareReliabilityRow `json:"nodes"`
}

type shareReliabilityResponse struct {
	AcceptedRows int    `json:"accepted_rows"`
	SkippedRows  int    `json:"skipped_rows"`
	Events       int    `json:"events"`
	Duplicate    bool   `json:"duplicate"`
	Ignored      bool   `json:"ignored,omitempty"`
	Reason       string `json:"reason,omitempty"`
}

// ShareReportReliability ingests the latest reputation snapshot of the nodes
// an installation considers problematic:
//
//	POST /c/reliability?token=<share token>
//	{"nodes": [{"key": "0123...cdef", "score": -55, "fails": 3, "silent": true}]}
//
// Only state transitions are appended to the event history; snapshots replace
// the previous values.
func ShareReportReliability(c *gin.Context) {
	ingest, ok := resolveShareIngest(c)
	if !ok {
		return
	}

	var req shareReliabilityRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "Invalid reliability payload: "+err.Error(), nil)
		return
	}
	if len(req.Nodes) == 0 {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "nodes are required", nil)
		return
	}
	if len(req.Nodes) > shareReliabilityReportMaxRows {
		utils.Result(c, http.StatusBadRequest, http.StatusBadRequest, "too many reliability rows", nil)
		return
	}
	if ingest.HWID == "" || ingest.DeviceKey == "" {
		utils.OkDetailed(c, "Ignored", shareReliabilityResponse{Ignored: true, Reason: "device identity required", SkippedRows: len(req.Nodes)})
		return
	}

	reportedAt := time.Now()
	if req.ReportedAt != nil && !req.ReportedAt.IsZero() && req.ReportedAt.Before(time.Now().Add(24*time.Hour)) {
		reportedAt = *req.ReportedAt
	}

	reportID := models.NormalizeShareDeviceValue(req.ReportID, shareTelemetryIDMaxLen)
	if reportID != "" {
		dedupKey := shareTelemetryReportKey(ingest.Share.ID, "rel|"+shareTelemetryScope(c), reportID)
		if !claimShareTelemetryReportID(dedupKey, time.Now()) {
			utils.OkDetailed(c, "Duplicate", shareReliabilityResponse{Duplicate: true})
			return
		}
	}

	// Deduplicate per node key, keeping the last occurrence of the batch.
	order := make([]string, 0, len(req.Nodes))
	rows := make(map[string]shareReliabilityRow, len(req.Nodes))
	for _, row := range req.Nodes {
		key := models.NormalizeShareNodeKey(row.Key)
		if key == "" {
			continue
		}
		if _, exists := rows[key]; !exists {
			order = append(order, key)
		}
		row.Key = key
		rows[key] = row
	}
	sort.Strings(order)

	response := shareReliabilityResponse{}
	for _, key := range order {
		row := rows[key]
		created, err := models.ReportShareNodeReliability(ingest.Share.ID, ingest.Device, ingest.HWID, ingest.DeviceKey, key, models.ShareNodeReliabilityReport{
			Score:     row.Score,
			Fails:     row.Fails,
			Streak:    row.Streak,
			Silent:    row.Silent,
			LastSeen:  row.LastSeen,
			Recovered: row.Recovered,
		}, reportedAt)
		if err != nil {
			utils.Warn("Failed to record share node reliability: %v", err)
			response.SkippedRows++
			continue
		}
		response.AcceptedRows++
		if created {
			response.Events++
		}
	}
	response.SkippedRows += len(req.Nodes) - len(order)
	utils.OkDetailed(c, "Success", response)
}
