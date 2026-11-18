package models

import (
	"sync"
	"time"

	"sublink/database"
	"sublink/utils"
)

// Access log actions.
const (
	ShareAccessActionFetch     = "fetch"
	ShareAccessActionStatus    = "status"
	ShareAccessActionStub      = "stub"
	ShareAccessActionBind      = "bind"
	ShareAccessActionAdmin     = "admin"
	ShareAccessActionTelemetry = "telemetry"
)

// ShareAccessLog records that a share link was used. Client IP addresses are
// intentionally not stored.
type ShareAccessLog struct {
	ID        int       `gorm:"primaryKey" json:"id"`
	ShareID   int       `gorm:"index:idx_share_access_share_created,priority:1" json:"share_id"`
	DeviceID  int       `gorm:"index" json:"device_id"`
	HWID      string    `gorm:"column:hwid;size:128" json:"hwid"`
	DeviceKey string    `gorm:"size:128" json:"device_key"`
	Action    string    `gorm:"size:32" json:"action"`
	Template  string    `gorm:"size:32" json:"template"`
	Reason    string    `gorm:"size:64" json:"reason"`
	CreatedAt time.Time `gorm:"index:idx_share_access_share_created,priority:2;autoCreateTime" json:"created_at"`
}

var shareAccessLogWG sync.WaitGroup

// WaitForPendingShareAccessLogs blocks until asynchronous access writes finish.
func WaitForPendingShareAccessLogs() {
	shareAccessLogWG.Wait()
}

// RecordShareAccess writes an access row asynchronously.
func RecordShareAccess(log *ShareAccessLog) {
	if log == nil || log.ShareID <= 0 {
		return
	}
	db := database.DB
	if db == nil {
		return
	}
	if err := db.Create(log).Error; err != nil {
		utils.Warn("Failed to record share access: %v", err)
	}
}

// RecordShareAccessAsync queues an access row without blocking the hot path.
func RecordShareAccessAsync(log *ShareAccessLog) {
	if log == nil || log.ShareID <= 0 {
		return
	}
	entry := *log
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
	shareAccessLogWG.Add(1)
	go func() {
		defer shareAccessLogWG.Done()
		RecordShareAccess(&entry)
	}()
}

// ListShareAccessLogs returns the most recent rows for one link.
func ListShareAccessLogs(shareID, limit int) ([]ShareAccessLog, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var logs []ShareAccessLog
	err := database.DB.Where("share_id = ?", shareID).Order("created_at DESC, id DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// ListShareAccessLogsByShareIDs returns recent rows across several links.
func ListShareAccessLogsByShareIDs(shareIDs []int, limit int) ([]ShareAccessLog, error) {
	if len(shareIDs) == 0 {
		return []ShareAccessLog{}, nil
	}
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	var logs []ShareAccessLog
	err := database.DB.Where("share_id IN ?", shareIDs).Order("created_at DESC, id DESC").Limit(limit).Find(&logs).Error
	return logs, err
}

// DeleteShareAccessLogs removes access history for a link.
func DeleteShareAccessLogs(shareID int) error {
	return database.DB.Where("share_id = ?", shareID).Delete(&ShareAccessLog{}).Error
}
