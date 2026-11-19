package models

import (
	"errors"
	"time"

	"sublink/database"

	"gorm.io/gorm"
)

// Reliability states reported by the SNKP core reputation component.
const (
	ShareReliabilityStateOk      = "ok"
	ShareReliabilityStateNeutral = "neutral"
	ShareReliabilityStateSuspect = "suspect"
	ShareReliabilityStateSilent  = "silent"
)

// ShareReliabilityThreshold marks the "suspect" score band. The core treats
// -30 as the failure threshold; anything below is worth surfacing.
const ShareReliabilityThreshold = -30

// ShareReliabilityEventRetentionDays bounds the transition history.
const ShareReliabilityEventRetentionDays = 90

// ShareNodeReliability is the latest reputation snapshot of one node as seen
// by one installation. A refreshed snapshot replaces the previous values; the
// separate event table keeps only meaningful transitions.
type ShareNodeReliability struct {
	ID        int    `gorm:"primaryKey" json:"id"`
	ShareID   int    `gorm:"uniqueIndex:idx_share_node_reliability_identity,priority:1" json:"share_id"`
	DeviceID  int    `gorm:"index" json:"device_id"`
	HWID      string `gorm:"column:hwid;size:128;uniqueIndex:idx_share_node_reliability_identity,priority:2" json:"hwid"`
	DeviceKey string `gorm:"size:128;uniqueIndex:idx_share_node_reliability_identity,priority:3" json:"device_key"`
	NodeKey   string `gorm:"size:32;uniqueIndex:idx_share_node_reliability_identity,priority:4" json:"node_key"`

	Score      int        `gorm:"default:0" json:"score"`
	Fails      int        `gorm:"default:0" json:"fails"`
	Streak     int        `gorm:"default:0" json:"streak"`
	Silent     bool       `gorm:"default:false" json:"silent"`
	State      string     `gorm:"size:16;default:''" json:"state"`
	Recovered  bool       `gorm:"default:false" json:"recovered"`
	LastSeenAt *time.Time `json:"last_seen_at"`
	ReportedAt time.Time  `json:"reported_at"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// ShareNodeReliabilityEvent records one state transition of one node on one
// installation. Snapshots without a transition are not written.
type ShareNodeReliabilityEvent struct {
	ID        int       `gorm:"primaryKey" json:"id"`
	ShareID   int       `gorm:"index:idx_share_rel_event_share_created,priority:1" json:"share_id"`
	DeviceID  int       `gorm:"index" json:"device_id"`
	HWID      string    `gorm:"column:hwid;size:128" json:"hwid"`
	DeviceKey string    `gorm:"size:128" json:"device_key"`
	NodeKey   string    `gorm:"size:32;index" json:"node_key"`
	FromState string    `gorm:"size:16" json:"from_state"`
	ToState   string    `gorm:"size:16" json:"to_state"`
	FromScore int       `json:"from_score"`
	ToScore   int       `json:"to_score"`
	Silent    bool      `json:"silent"`
	Reason    string    `gorm:"size:32" json:"reason"`
	CreatedAt time.Time `gorm:"index:idx_share_rel_event_share_created,priority:2;autoCreateTime" json:"created_at"`
}

// ShareNodeReliabilityReport is one client supplied node snapshot.
type ShareNodeReliabilityReport struct {
	Score     int
	Fails     int
	Streak    int
	Silent    bool
	LastSeen  *time.Time
	Recovered bool
}

// ShareNodeReliabilityState maps a score and the silent flag to a display
// state. The bands follow the core reputation component.
func ShareNodeReliabilityState(score int, silent bool) string {
	if silent {
		return ShareReliabilityStateSilent
	}
	switch {
	case score <= ShareReliabilityThreshold:
		return ShareReliabilityStateSuspect
	case score >= -ShareReliabilityThreshold:
		return ShareReliabilityStateOk
	default:
		return ShareReliabilityStateNeutral
	}
}

// IsProblem reports whether a snapshot deserves the "unreliable" badge.
func (r *ShareNodeReliability) IsProblem() bool {
	if r == nil {
		return false
	}
	if r.Silent || r.Score <= ShareReliabilityThreshold {
		return true
	}
	return r.Fails > 0 && r.State != ShareReliabilityStateOk
}

func findShareNodeReliability(shareID int, hwid, deviceKey, nodeKey string) (*ShareNodeReliability, error) {
	var row ShareNodeReliability
	err := database.DB.Where(
		"share_id = ? AND hwid = ? AND device_key = ? AND node_key = ?",
		shareID, hwid, deviceKey, nodeKey,
	).First(&row).Error
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func clampReliabilityScore(score int) int {
	if score < -100 {
		return -100
	}
	if score > 100 {
		return 100
	}
	return score
}

func nonNegative(value int) int {
	if value < 0 {
		return 0
	}
	return value
}

// ReportShareNodeReliability upserts the latest snapshot and appends a
// transition event when the state band or the silent flag changed. It reports
// whether an event row was written.
func ReportShareNodeReliability(shareID int, device *ShareDevice, hwid, deviceKey, nodeKey string, report ShareNodeReliabilityReport, at time.Time) (bool, error) {
	hwid = NormalizeShareDeviceValue(hwid, 128)
	deviceKey = NormalizeShareDeviceValue(deviceKey, 128)
	nodeKey = NormalizeShareNodeKey(nodeKey)
	if shareID <= 0 || hwid == "" || deviceKey == "" || nodeKey == "" {
		return false, nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()

	score := clampReliabilityScore(report.Score)
	silent := report.Silent
	state := ShareNodeReliabilityState(score, silent)
	if report.Recovered {
		state = ShareReliabilityStateOk
		silent = false
	}
	deviceID := 0
	if device != nil {
		deviceID = device.ID
	}

	previous, err := findShareNodeReliability(shareID, hwid, deviceKey, nodeKey)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}

	if previous == nil {
		row := ShareNodeReliability{
			ShareID:    shareID,
			DeviceID:   deviceID,
			HWID:       hwid,
			DeviceKey:  deviceKey,
			NodeKey:    nodeKey,
			Score:      score,
			Fails:      nonNegative(report.Fails),
			Streak:     nonNegative(report.Streak),
			Silent:     silent,
			State:      state,
			Recovered:  report.Recovered,
			LastSeenAt: normalizeNullableTime(report.LastSeen),
			ReportedAt: at,
		}
		if createErr := database.DB.Create(&row).Error; createErr != nil {
			return false, createErr
		}
		return appendShareReliabilityEvent(shareID, deviceID, hwid, deviceKey, nodeKey, "", state, 0, score, silent, reliabilityEventReason("", state, report.Recovered), at)
	}

	prevState := previous.State
	if prevState == "" {
		prevState = ShareNodeReliabilityState(previous.Score, previous.Silent)
	}
	updates := map[string]any{
		"device_id":    deviceID,
		"score":        score,
		"fails":        nonNegative(report.Fails),
		"streak":       nonNegative(report.Streak),
		"silent":       silent,
		"state":        state,
		"recovered":    report.Recovered,
		"last_seen_at": normalizeNullableTime(report.LastSeen),
		"reported_at":  at,
		"updated_at":   at,
	}
	if updateErr := database.DB.Model(&ShareNodeReliability{}).Where("id = ?", previous.ID).Updates(updates).Error; updateErr != nil {
		return false, updateErr
	}

	if prevState == state && previous.Silent == silent && !report.Recovered {
		return false, nil
	}
	return appendShareReliabilityEvent(shareID, deviceID, hwid, deviceKey, nodeKey, prevState, state, previous.Score, score, silent, reliabilityEventReason(prevState, state, report.Recovered), at)
}

func reliabilityEventReason(fromState, toState string, recovered bool) string {
	if recovered {
		return "recovered"
	}
	if toState == ShareReliabilityStateSilent {
		return "silent"
	}
	switch toState {
	case ShareReliabilityStateSuspect:
		return "suspect"
	case ShareReliabilityStateOk:
		if fromState != "" {
			return "recovered"
		}
		return "healthy"
	default:
		return "state_change"
	}
}

func appendShareReliabilityEvent(shareID, deviceID int, hwid, deviceKey, nodeKey, fromState, toState string, fromScore, toScore int, silent bool, reason string, at time.Time) (bool, error) {
	event := ShareNodeReliabilityEvent{
		ShareID:   shareID,
		DeviceID:  deviceID,
		HWID:      hwid,
		DeviceKey: deviceKey,
		NodeKey:   nodeKey,
		FromState: fromState,
		ToState:   toState,
		FromScore: fromScore,
		ToScore:   toScore,
		Silent:    silent,
		Reason:    reason,
		CreatedAt: at,
	}
	if err := database.DB.Create(&event).Error; err != nil {
		return false, err
	}
	return true, nil
}

// ListProblemShareNodeReliability returns the current unreliable snapshots of
// the given links (or installations when deviceIDs is supplied).
func ListProblemShareNodeReliability(shareIDs []int, deviceIDs []int) ([]ShareNodeReliability, error) {
	query := database.DB.Model(&ShareNodeReliability{}).
		Where("silent = ? OR score <= ? OR (fails > 0 AND state <> ?)",
			true, ShareReliabilityThreshold, ShareReliabilityStateOk)
	if len(deviceIDs) > 0 {
		query = query.Where("device_id IN ?", deviceIDs)
	} else if len(shareIDs) > 0 {
		query = query.Where("share_id IN ?", shareIDs)
	} else {
		return []ShareNodeReliability{}, nil
	}
	var rows []ShareNodeReliability
	err := query.Order("score ASC, updated_at DESC").Limit(500).Find(&rows).Error
	return rows, err
}

// CountProblemShareNodes counts the current unreliable nodes per link.
func CountProblemShareNodes(shareIDs []int) (map[int]int, error) {
	counts := make(map[int]int, len(shareIDs))
	if len(shareIDs) == 0 {
		return counts, nil
	}
	type row struct {
		ShareID int
		Total   int
	}
	var rows []row
	err := database.DB.Model(&ShareNodeReliability{}).
		Select("share_id, COUNT(*) AS total").
		Where("share_id IN ?", shareIDs).
		Where("silent = ? OR score <= ? OR (fails > 0 AND state <> ?)",
			true, ShareReliabilityThreshold, ShareReliabilityStateOk).
		Group("share_id").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, entry := range rows {
		counts[entry.ShareID] = entry.Total
	}
	return counts, nil
}

// ListShareReliabilityEvents returns the newest transition rows of the links.
func ListShareReliabilityEvents(shareIDs []int, since time.Time, limit int) ([]ShareNodeReliabilityEvent, error) {
	if len(shareIDs) == 0 {
		return []ShareNodeReliabilityEvent{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := database.DB.Where("share_id IN ?", shareIDs)
	if !since.IsZero() {
		query = query.Where("created_at >= ?", since.UTC())
	}
	var events []ShareNodeReliabilityEvent
	err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&events).Error
	return events, err
}

// ListShareReliabilityEventsForDevices returns the newest transition rows of
// the given installations.
func ListShareReliabilityEventsForDevices(deviceIDs []int, since time.Time, limit int) ([]ShareNodeReliabilityEvent, error) {
	if len(deviceIDs) == 0 {
		return []ShareNodeReliabilityEvent{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query := database.DB.Where("device_id IN ?", deviceIDs)
	if !since.IsZero() {
		query = query.Where("created_at >= ?", since.UTC())
	}
	var events []ShareNodeReliabilityEvent
	err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&events).Error
	return events, err
}

// PruneShareReliabilityEvents deletes transition rows older than retention.
func PruneShareReliabilityEvents(cutoff time.Time) error {
	if cutoff.IsZero() {
		cutoff = time.Now().AddDate(0, 0, -ShareReliabilityEventRetentionDays)
	}
	return database.DB.Where("created_at < ?", cutoff.UTC()).Delete(&ShareNodeReliabilityEvent{}).Error
}

// DeleteShareReliabilityForDevices removes snapshots and events of devices
// that are being unbound.
func DeleteShareReliabilityForDevices(tx *gorm.DB, deviceIDs []int) error {
	if tx == nil || len(deviceIDs) == 0 {
		return nil
	}
	if err := tx.Where("device_id IN ?", deviceIDs).Delete(&ShareNodeReliability{}).Error; err != nil {
		return err
	}
	return tx.Where("device_id IN ?", deviceIDs).Delete(&ShareNodeReliabilityEvent{}).Error
}

func normalizeNullableTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	normalized := value.UTC()
	return &normalized
}
