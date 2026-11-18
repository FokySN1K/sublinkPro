package models

import (
	"sync"
	"time"

	"sublink/database"
	"sublink/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ShareDeviceUsageWindowDays bounds the per-hour traffic history kept for a
// device. The admin UI folds this window into hour-of-day buckets so users can
// see at which hours a device consumes traffic.
const ShareDeviceUsageWindowDays = 7

// ShareDeviceUsageHour is one hourly traffic bucket of one device binding.
// Buckets are keyed by device row and UTC hour start; reports inside the same
// hour accumulate in place so the table stays small and aggregation is cheap.
type ShareDeviceUsageHour struct {
	ID          int       `gorm:"primaryKey" json:"id"`
	DeviceID    int       `gorm:"uniqueIndex:idx_share_device_usage_identity;not null" json:"device_id"`
	ShareID     int       `gorm:"index" json:"share_id"`
	WindowStart time.Time `gorm:"uniqueIndex:idx_share_device_usage_identity" json:"window_start"`
	UpBytes     int64     `gorm:"default:0" json:"up_bytes"`
	DownBytes   int64     `gorm:"default:0" json:"down_bytes"`
	Bytes       int64     `gorm:"default:0" json:"bytes"`
	UpdatedAt   time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

var shareDeviceUsagePruneMu sync.Mutex
var shareDeviceUsagePrunedAt time.Time

// RecordShareDeviceTraffic attributes one accepted telemetry report to a bound
// device: the lifetime counter moves on the device row and the report lands in
// the device/hour bucket used by the telemetry views.
func RecordShareDeviceTraffic(device *ShareDevice, up, down int64, at time.Time) error {
	if device == nil || device.ID <= 0 {
		return nil
	}
	total := up + down
	if total < 0 {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()

	updates := map[string]any{
		"reported_traffic_bytes": gorm.Expr("reported_traffic_bytes + ?", total),
		"last_report_at":         at,
		"updated_at":             at,
	}
	if err := database.DB.Model(&ShareDevice{}).Where("id = ?", device.ID).Updates(updates).Error; err != nil {
		return err
	}
	device.ReportedTrafficBytes += total
	device.LastReportAt = &at

	return RecordShareDeviceUsage(device, up, down, at)
}

// RecordShareDeviceUsage records only the device/hour bucket of one accepted
// report. The absolute counter path already applied its own lifetime increment,
// so it must call this helper instead of RecordShareDeviceTraffic.
func RecordShareDeviceUsage(device *ShareDevice, up, down int64, at time.Time) error {
	if device == nil || device.ID <= 0 {
		return nil
	}
	total := up + down
	if total <= 0 {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()

	row := ShareDeviceUsageHour{
		DeviceID:    device.ID,
		ShareID:     device.ShareID,
		WindowStart: at.Truncate(time.Hour),
		UpBytes:     up,
		DownBytes:   down,
		Bytes:       total,
	}
	err := database.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "device_id"}, {Name: "window_start"}},
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
	pruneShareDeviceUsage(at)
	return nil
}

// pruneShareDeviceUsage drops buckets outside the retention window. It runs at
// most once per hour per process so the hot ingest path stays cheap.
func pruneShareDeviceUsage(at time.Time) {
	shareDeviceUsagePruneMu.Lock()
	if !shareDeviceUsagePrunedAt.IsZero() && at.Sub(shareDeviceUsagePrunedAt) < time.Hour {
		shareDeviceUsagePruneMu.Unlock()
		return
	}
	shareDeviceUsagePrunedAt = at
	shareDeviceUsagePruneMu.Unlock()

	cutoff := at.AddDate(0, 0, -ShareDeviceUsageWindowDays)
	if err := database.DB.Where("window_start < ?", cutoff).Delete(&ShareDeviceUsageHour{}).Error; err != nil {
		utils.Warn("Failed to prune share device usage buckets: %v", err)
	}
}

// SumShareDeviceUsage returns the traffic of each device row inside the window.
// The zero time means "no lower bound".
func SumShareDeviceUsage(deviceIDs []int, since time.Time) (map[int]int64, error) {
	sums := make(map[int]int64, len(deviceIDs))
	if len(deviceIDs) == 0 {
		return sums, nil
	}
	type row struct {
		DeviceID   int
		TotalBytes int64
	}
	var rows []row
	query := database.DB.Model(&ShareDeviceUsageHour{}).
		Select("device_id, SUM(bytes) AS total_bytes").
		Where("device_id IN ?", deviceIDs).
		Group("device_id")
	if !since.IsZero() {
		query = query.Where("window_start >= ?", since.UTC())
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, entry := range rows {
		sums[entry.DeviceID] = entry.TotalBytes
	}
	return sums, nil
}

// ListShareDeviceUsage returns the hourly buckets of the given device rows in
// ascending time order.
func ListShareDeviceUsage(deviceIDs []int, since time.Time) ([]ShareDeviceUsageHour, error) {
	if len(deviceIDs) == 0 {
		return []ShareDeviceUsageHour{}, nil
	}
	var rows []ShareDeviceUsageHour
	query := database.DB.Where("device_id IN ?", deviceIDs)
	if !since.IsZero() {
		query = query.Where("window_start >= ?", since.UTC())
	}
	err := query.Order("window_start ASC").Find(&rows).Error
	return rows, err
}
