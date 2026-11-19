package models

import (
	"strings"
	"sync"
	"time"

	"sublink/database"
	"sublink/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ShareNodeUsageRetentionDays bounds the per-node hourly buckets. The details
// views support a 7/30 day window, so buckets must survive 30 days.
const ShareNodeUsageRetentionDays = 30

// ShareNodeUsageHour is one hourly traffic bucket of one installation on one
// node. Node keys are the stable sublinkpro_unique_key values exported in the
// client profile, so renames do not break attribution.
type ShareNodeUsageHour struct {
	ID          int       `gorm:"primaryKey" json:"id"`
	DeviceID    int       `gorm:"uniqueIndex:idx_share_node_usage_identity,priority:1;not null" json:"device_id"`
	ShareID     int       `gorm:"index" json:"share_id"`
	NodeKey     string    `gorm:"size:32;uniqueIndex:idx_share_node_usage_identity,priority:2" json:"node_key"`
	WindowStart time.Time `gorm:"uniqueIndex:idx_share_node_usage_identity,priority:3" json:"window_start"`
	UpBytes     int64     `gorm:"default:0" json:"up_bytes"`
	DownBytes   int64     `gorm:"default:0" json:"down_bytes"`
	Bytes       int64     `gorm:"default:0" json:"bytes"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

var shareNodeUsagePruneMu sync.Mutex
var shareNodeUsagePrunedAt time.Time

// NormalizeShareNodeKey normalises a client supplied node key. The key is the
// 32 hex prefix of the node content hash; longer values are truncated and
// unknown short values are still stored so no traffic is dropped.
func NormalizeShareNodeKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > nodeUniqueKeyLength {
		value = value[:nodeUniqueKeyLength]
	}
	return value
}

// RecordShareNodeUsage adds one node row of an accepted /c/nodes report to the
// device/node/hour bucket.
func RecordShareNodeUsage(device *ShareDevice, nodeKey string, up, down int64, at time.Time) error {
	if device == nil || device.ID <= 0 {
		return nil
	}
	nodeKey = NormalizeShareNodeKey(nodeKey)
	if nodeKey == "" {
		return nil
	}
	if up < 0 {
		up = 0
	}
	if down < 0 {
		down = 0
	}
	total := up + down
	if total <= 0 {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()

	row := ShareNodeUsageHour{
		DeviceID:    device.ID,
		ShareID:     device.ShareID,
		NodeKey:     nodeKey,
		WindowStart: at.Truncate(time.Hour),
		UpBytes:     up,
		DownBytes:   down,
		Bytes:       total,
	}
	err := database.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_id"}, {Name: "node_key"}, {Name: "window_start"}},
		DoUpdates: clause.Assignments(map[string]any{
			"share_id":   device.ShareID,
			"up_bytes":   gorm.Expr("up_bytes + ?", up),
			"down_bytes": gorm.Expr("down_bytes + ?", down),
			"bytes":      gorm.Expr("bytes + ?", total),
			"updated_at": at,
		}),
	}).Create(&row).Error
	if err != nil {
		return err
	}
	pruneShareNodeUsage(at)
	return nil
}

// pruneShareNodeUsage drops buckets outside the retention window. It runs at
// most once per hour per process so the hot ingest path stays cheap.
func pruneShareNodeUsage(at time.Time) {
	shareNodeUsagePruneMu.Lock()
	if !shareNodeUsagePrunedAt.IsZero() && at.Sub(shareNodeUsagePrunedAt) < time.Hour {
		shareNodeUsagePruneMu.Unlock()
		return
	}
	shareNodeUsagePrunedAt = at
	shareNodeUsagePruneMu.Unlock()

	cutoff := at.AddDate(0, 0, -ShareNodeUsageRetentionDays)
	if err := database.DB.Where("window_start < ?", cutoff).Delete(&ShareNodeUsageHour{}).Error; err != nil {
		utils.Warn("Failed to prune share node usage buckets: %v", err)
	}
}

// PruneShareNodeUsage deletes node usage buckets older than the retention
// window. The maintenance task calls it with an explicit cutoff.
func PruneShareNodeUsage(cutoff time.Time) error {
	if cutoff.IsZero() {
		cutoff = time.Now().AddDate(0, 0, -ShareNodeUsageRetentionDays)
	}
	return database.DB.Where("window_start < ?", cutoff.UTC()).Delete(&ShareNodeUsageHour{}).Error
}

// ShareNodeUsageSum is the traffic of one node key inside an aggregating scope.
type ShareNodeUsageSum struct {
	NodeKey   string
	UpBytes   int64
	DownBytes int64
	Bytes     int64
}

// SumShareNodeUsage aggregates node buckets by node key. When deviceIDs is not
// empty the scope is the installation rows, otherwise the share links.
func SumShareNodeUsage(shareIDs []int, deviceIDs []int, since time.Time) ([]ShareNodeUsageSum, error) {
	query := database.DB.Model(&ShareNodeUsageHour{}).
		Select("node_key, SUM(up_bytes) AS up_bytes, SUM(down_bytes) AS down_bytes, SUM(bytes) AS bytes")
	if len(deviceIDs) > 0 {
		query = query.Where("device_id IN ?", deviceIDs)
	} else if len(shareIDs) > 0 {
		query = query.Where("share_id IN ?", shareIDs)
	} else {
		return []ShareNodeUsageSum{}, nil
	}
	if !since.IsZero() {
		query = query.Where("window_start >= ?", since.UTC())
	}
	var rows []ShareNodeUsageSum
	err := query.Group("node_key").Order("bytes DESC").Scan(&rows).Error
	return rows, err
}

// DeleteShareNodeUsageForDevices removes node buckets of the given device rows.
func DeleteShareNodeUsageForDevices(tx *gorm.DB, deviceIDs []int) error {
	if tx == nil || len(deviceIDs) == 0 {
		return nil
	}
	return tx.Where("device_id IN ?", deviceIDs).Delete(&ShareNodeUsageHour{}).Error
}
