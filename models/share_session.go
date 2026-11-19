package models

import (
	"errors"
	"strings"
	"time"

	"sublink/database"

	"gorm.io/gorm"
)

// ShareSessionStaleAfter is how long an open session may stay silent before
// the maintenance task closes it. A crashed client stops reporting, so the
// last report timestamp becomes the effective end of the session.
const ShareSessionStaleAfter = 10 * time.Minute

// ShareSessionRetentionDays bounds session history. Older rows are pruned by
// the maintenance task; the details views only read the recent window.
const ShareSessionRetentionDays = 90

// ShareSession is one "client was online" interval of one installation.
// Sessions are attributed traffic from regular telemetry reports that carry a
// session_id: the report already moved the share/device counters, so sessions
// only mirror the same accepted deltas and never add traffic on their own.
type ShareSession struct {
	ID         int    `gorm:"primaryKey" json:"id"`
	ShareID    int    `gorm:"index:idx_share_session_share_start,priority:1;uniqueIndex:idx_share_session_identity,priority:1" json:"share_id"`
	DeviceID   int    `gorm:"index" json:"device_id"`
	HWID       string `gorm:"column:hwid;size:128;uniqueIndex:idx_share_session_identity,priority:2" json:"hwid"`
	DeviceKey  string `gorm:"size:128;uniqueIndex:idx_share_session_identity,priority:3" json:"device_key"`
	SessionKey string `gorm:"size:64;uniqueIndex:idx_share_session_identity,priority:4" json:"session_key"`

	StartedAt    time.Time  `gorm:"index:idx_share_session_share_start,priority:2" json:"started_at"`
	EndedAt      *time.Time `json:"ended_at"`
	LastReportAt time.Time  `json:"last_report_at"`
	UpBytes      int64      `gorm:"default:0" json:"up_bytes"`
	DownBytes    int64      `gorm:"default:0" json:"down_bytes"`
	Bytes        int64      `gorm:"default:0" json:"bytes"`
	Closed       bool       `gorm:"default:false" json:"closed"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// NormalizeShareSessionKey normalises a client supplied session identifier.
func NormalizeShareSessionKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 64 {
		value = value[:64]
	}
	return value
}

func shareSessionIdentity(shareID int, hwid, deviceKey, sessionKey string) (int, string, string, string, bool) {
	hwid = NormalizeShareDeviceValue(hwid, 128)
	deviceKey = NormalizeShareDeviceValue(deviceKey, 128)
	sessionKey = NormalizeShareSessionKey(sessionKey)
	if shareID <= 0 || hwid == "" || deviceKey == "" || sessionKey == "" {
		return 0, "", "", "", false
	}
	return shareID, hwid, deviceKey, sessionKey, true
}

func findShareSession(shareID int, hwid, deviceKey, sessionKey string) (*ShareSession, error) {
	var session ShareSession
	err := database.DB.Where(
		"share_id = ? AND hwid = ? AND device_key = ? AND session_key = ?",
		shareID, hwid, deviceKey, sessionKey,
	).First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}

// StartShareSession opens a client session or refreshes an already open one.
// Repeated session_start events with the same key are idempotent; a start
// event for an already closed session does not resurrect it.
func StartShareSession(shareID int, device *ShareDevice, hwid, deviceKey, sessionKey string, at time.Time) (*ShareSession, error) {
	shareID, hwid, deviceKey, sessionKey, ok := shareSessionIdentity(shareID, hwid, deviceKey, sessionKey)
	if !ok {
		return nil, nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()
	deviceID := 0
	if device != nil {
		deviceID = device.ID
	}

	session, err := findShareSession(shareID, hwid, deviceKey, sessionKey)
	if err == nil {
		if session.Closed {
			return session, nil
		}
		updates := map[string]any{
			"last_report_at": at,
			"updated_at":     at,
		}
		if deviceID > 0 {
			updates["device_id"] = deviceID
		}
		if updateErr := database.DB.Model(&ShareSession{}).Where("id = ?", session.ID).Updates(updates).Error; updateErr != nil {
			return nil, updateErr
		}
		session.LastReportAt = at
		if deviceID > 0 {
			session.DeviceID = deviceID
		}
		return session, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	row := ShareSession{
		ShareID:      shareID,
		DeviceID:     deviceID,
		HWID:         hwid,
		DeviceKey:    deviceKey,
		SessionKey:   sessionKey,
		StartedAt:    at,
		LastReportAt: at,
	}
	if createErr := database.DB.Create(&row).Error; createErr != nil {
		// A concurrent session_start may have inserted the same identity.
		if existing, reloadErr := findShareSession(shareID, hwid, deviceKey, sessionKey); reloadErr == nil {
			return existing, nil
		}
		return nil, createErr
	}
	return &row, nil
}

// AddShareSessionTraffic mirrors an accepted telemetry delta into the open
// session. A report that arrives before session_start creates the session
// lazily so no traffic is lost for clients that only send the new field.
func AddShareSessionTraffic(shareID int, device *ShareDevice, hwid, deviceKey, sessionKey string, up, down int64, at time.Time) error {
	total := up + down
	if total <= 0 {
		return nil
	}
	shareID, hwid, deviceKey, sessionKey, ok := shareSessionIdentity(shareID, hwid, deviceKey, sessionKey)
	if !ok {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()

	updates := func() map[string]any {
		return map[string]any{
			"up_bytes":       gorm.Expr("up_bytes + ?", up),
			"down_bytes":     gorm.Expr("down_bytes + ?", down),
			"bytes":          gorm.Expr("bytes + ?", total),
			"last_report_at": at,
			"updated_at":     at,
		}
	}

	result := database.DB.Model(&ShareSession{}).
		Where("share_id = ? AND hwid = ? AND device_key = ? AND session_key = ? AND closed = ?",
			shareID, hwid, deviceKey, sessionKey, false).
		Updates(updates())
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}

	// No open session: create it lazily and retry the increment. A closed
	// session with the same key is left untouched (history is immutable).
	if _, err := StartShareSession(shareID, device, hwid, deviceKey, sessionKey, at); err != nil {
		return err
	}
	deviceID := 0
	if device != nil {
		deviceID = device.ID
	}
	if deviceID > 0 {
		if err := database.DB.Model(&ShareSession{}).
			Where("share_id = ? AND hwid = ? AND device_key = ? AND session_key = ?", shareID, hwid, deviceKey, sessionKey).
			Update("device_id", deviceID).Error; err != nil {
			return err
		}
	}
	return database.DB.Model(&ShareSession{}).
		Where("share_id = ? AND hwid = ? AND device_key = ? AND session_key = ? AND closed = ?",
			shareID, hwid, deviceKey, sessionKey, false).
		Updates(updates()).Error
}

// CloseShareSession marks a session as finished. It returns false when there
// is no open session with that identity, which also covers a retried stop.
func CloseShareSession(shareID int, hwid, deviceKey, sessionKey string, at time.Time) (bool, error) {
	shareID, hwid, deviceKey, sessionKey, ok := shareSessionIdentity(shareID, hwid, deviceKey, sessionKey)
	if !ok {
		return false, nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()

	result := database.DB.Model(&ShareSession{}).
		Where("share_id = ? AND hwid = ? AND device_key = ? AND session_key = ? AND closed = ?",
			shareID, hwid, deviceKey, sessionKey, false).
		Updates(map[string]any{
			"ended_at":       at,
			"last_report_at": at,
			"closed":         true,
			"updated_at":     at,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// CloseStaleShareSessions closes sessions that stopped reporting. The end
// timestamp is the last accepted report, not "now": the client likely died
// somewhere between its final report and the sweep.
func CloseStaleShareSessions(cutoff time.Time) (int64, error) {
	if cutoff.IsZero() {
		cutoff = time.Now().Add(-ShareSessionStaleAfter)
	}
	cutoff = cutoff.UTC()
	result := database.DB.Model(&ShareSession{}).
		Where("closed = ? AND last_report_at < ?", false, cutoff).
		Updates(map[string]any{
			"ended_at":   gorm.Expr("last_report_at"),
			"closed":     true,
			"updated_at": time.Now().UTC(),
		})
	return result.RowsAffected, result.Error
}

// PruneShareSessions deletes sessions older than the retention window.
func PruneShareSessions(cutoff time.Time) error {
	if cutoff.IsZero() {
		cutoff = time.Now().AddDate(0, 0, -ShareSessionRetentionDays)
	}
	return database.DB.Where("started_at < ?", cutoff.UTC()).Delete(&ShareSession{}).Error
}

// ShareSessionSummary aggregates sessions of one link inside a time window.
type ShareSessionSummary struct {
	ShareID      int
	Sessions     int64
	OpenSessions int64
	Bytes        int64
}

// SummarizeShareSessions aggregates session counts per link since the given
// time. It feeds the overview row counters (sessions today / online now).
func SummarizeShareSessions(shareIDs []int, since time.Time) (map[int]ShareSessionSummary, error) {
	summaries := make(map[int]ShareSessionSummary, len(shareIDs))
	if len(shareIDs) == 0 {
		return summaries, nil
	}
	type row struct {
		ShareID      int
		Sessions     int64
		OpenSessions int64
		Bytes        int64
	}
	var rows []row
	query := database.DB.Model(&ShareSession{}).
		Select("share_id, COUNT(*) AS sessions, SUM(CASE WHEN closed THEN 0 ELSE 1 END) AS open_sessions, SUM(bytes) AS bytes").
		Where("share_id IN ?", shareIDs).
		Group("share_id")
	if !since.IsZero() {
		query = query.Where("started_at >= ?", since.UTC())
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, entry := range rows {
		summaries[entry.ShareID] = ShareSessionSummary(entry)
	}
	return summaries, nil
}

// CountShareSessionsByDevice counts sessions per device row since a time.
func CountShareSessionsByDevice(deviceIDs []int, since time.Time) (map[int]int64, error) {
	counts := make(map[int]int64, len(deviceIDs))
	if len(deviceIDs) == 0 {
		return counts, nil
	}
	type row struct {
		DeviceID int
		Total    int64
	}
	var rows []row
	query := database.DB.Model(&ShareSession{}).
		Select("device_id, COUNT(*) AS total").
		Where("device_id IN ?", deviceIDs).
		Group("device_id")
	if !since.IsZero() {
		query = query.Where("started_at >= ?", since.UTC())
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, entry := range rows {
		counts[entry.DeviceID] = entry.Total
	}
	return counts, nil
}

// ListShareSessions returns the newest sessions of the given links.
func ListShareSessions(shareIDs []int, since time.Time, limit int) ([]ShareSession, error) {
	if len(shareIDs) == 0 {
		return []ShareSession{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var sessions []ShareSession
	query := database.DB.Where("share_id IN ?", shareIDs)
	if !since.IsZero() {
		query = query.Where("started_at >= ?", since.UTC())
	}
	err := query.Order("started_at DESC, id DESC").Limit(limit).Find(&sessions).Error
	return sessions, err
}

// ListShareSessionsByDeviceIDs returns the newest sessions of the given
// device rows (the device details view is scoped by installation).
func ListShareSessionsByDeviceIDs(deviceIDs []int, since time.Time, limit int) ([]ShareSession, error) {
	if len(deviceIDs) == 0 {
		return []ShareSession{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var sessions []ShareSession
	query := database.DB.Where("device_id IN ?", deviceIDs)
	if !since.IsZero() {
		query = query.Where("started_at >= ?", since.UTC())
	}
	err := query.Order("started_at DESC, id DESC").Limit(limit).Find(&sessions).Error
	return sessions, err
}

// LatestShareSessions returns the newest session row per link.
func LatestShareSessions(shareIDs []int) (map[int]ShareSession, error) {
	latest := make(map[int]ShareSession, len(shareIDs))
	if len(shareIDs) == 0 {
		return latest, nil
	}
	limit := len(shareIDs) * 20
	if limit < 200 {
		limit = 200
	}
	if limit > 5000 {
		limit = 5000
	}
	var sessions []ShareSession
	if err := database.DB.Where("share_id IN ?", shareIDs).
		Order("started_at DESC, id DESC").
		Limit(limit).
		Find(&sessions).Error; err != nil {
		return nil, err
	}
	for _, session := range sessions {
		if _, exists := latest[session.ShareID]; !exists {
			latest[session.ShareID] = session
		}
	}
	return latest, nil
}
