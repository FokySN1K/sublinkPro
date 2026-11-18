package models

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"sublink/database"

	"gorm.io/gorm"
)

// Device statuses for a subscription link.
const (
	ShareDeviceStatusActive  = "active"
	ShareDeviceStatusPending = "pending"
	ShareDeviceStatusBlocked = "blocked"
)

// ShareDevice is one installation of SNKP bound to a subscription link.
// X-HWID is the owner/account identity and may cover several installations;
// X-Device-ID identifies the installation itself.
type ShareDevice struct {
	ID          int       `gorm:"primaryKey" json:"id"`
	ShareID     int       `gorm:"uniqueIndex:idx_share_device_identity;not null" json:"share_id"`
	HWID        string    `gorm:"column:hwid;uniqueIndex:idx_share_device_identity;size:128" json:"hwid"`
	DeviceID    string    `gorm:"uniqueIndex:idx_share_device_identity;size:128" json:"device_id"`
	Name        string    `gorm:"size:100" json:"name"`
	Description string    `gorm:"size:500" json:"description"`
	Platform    string    `gorm:"size:50" json:"platform"`
	AppVersion  string    `gorm:"size:50" json:"app_version"`
	Status      string    `gorm:"size:20" json:"status"`
	Secret      string    `gorm:"size:64" json:"secret"`
	FirstSeenAt time.Time `json:"first_seen_at"`
	LastSeenAt  time.Time `json:"last_seen_at"`

	// Telemetry counters attributed to this installation. The total follows
	// the same rule as the link counter: every accepted report is added up.
	ReportedTrafficBytes int64      `gorm:"default:0" json:"reported_traffic_bytes"`
	LastReportAt         *time.Time `json:"last_report_at"`

	// Absolute reporting state: the last accepted installation total and the
	// installation epoch it belongs to (SNKP install_id). A new epoch starts a
	// fresh baseline, so reinstalls and moved data are not double counted.
	ReportBaselineBytes int64  `gorm:"default:0" json:"report_baseline_bytes"`
	ReportBaselineEpoch string `gorm:"size:64;default:''" json:"report_baseline_epoch"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// NormalizeShareDeviceValue trims and truncates a requested header value.
func NormalizeShareDeviceValue(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen > 0 && len(value) > maxLen {
		value = value[:maxLen]
	}
	return value
}

// GenerateShareDeviceSecret creates a per-device shared secret.
func GenerateShareDeviceSecret() (string, error) {
	return GenerateToken()
}

// FindShareDevice looks up a device by the composite identity.
func FindShareDevice(shareID int, hwid, deviceID string) (*ShareDevice, error) {
	var device ShareDevice
	err := database.DB.Where("share_id = ? AND hwid = ? AND device_id = ?", shareID, hwid, deviceID).First(&device).Error
	if err != nil {
		return nil, err
	}
	return &device, nil
}

// FindShareDeviceBySecret resolves a device by its per-installation secret
// inside one share link. An empty secret never matches.
func FindShareDeviceBySecret(shareID int, secret string) (*ShareDevice, error) {
	secret = strings.TrimSpace(secret)
	if shareID <= 0 || secret == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var device ShareDevice
	if err := database.DB.Where("share_id = ? AND secret = ?", shareID, secret).First(&device).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

// GetShareDeviceByID loads a device by primary key.
func GetShareDeviceByID(id int) (*ShareDevice, error) {
	var device ShareDevice
	if err := database.DB.First(&device, id).Error; err != nil {
		return nil, err
	}
	return &device, nil
}

// ListShareDevices returns devices for one link ordered by last activity.
func ListShareDevices(shareID int) ([]ShareDevice, error) {
	var devices []ShareDevice
	err := database.DB.Where("share_id = ?", shareID).Order("last_seen_at DESC, id ASC").Find(&devices).Error
	return devices, err
}

// ListShareDevicesByShareIDs returns devices across several links.
func ListShareDevicesByShareIDs(shareIDs []int) ([]ShareDevice, error) {
	if len(shareIDs) == 0 {
		return []ShareDevice{}, nil
	}
	var devices []ShareDevice
	err := database.DB.Where("share_id IN ?", shareIDs).Order("last_seen_at DESC, id ASC").Find(&devices).Error
	return devices, err
}

// ListShareDevicesByIdentity returns every link binding of one installation.
// The same HWID/device pair may hold several rows, one per share link.
func ListShareDevicesByIdentity(hwid, deviceID string) ([]ShareDevice, error) {
	hwid = NormalizeShareDeviceValue(hwid, 128)
	deviceID = NormalizeShareDeviceValue(deviceID, 128)
	if hwid == "" || deviceID == "" {
		return []ShareDevice{}, nil
	}
	var devices []ShareDevice
	err := database.DB.Where("hwid = ? AND device_id = ?", hwid, deviceID).Order("last_seen_at DESC, id ASC").Find(&devices).Error
	return devices, err
}

// CountActiveShareDevices counts devices that currently consume a slot.
func CountActiveShareDevices(shareID int) (int64, error) {
	var count int64
	err := database.DB.Model(&ShareDevice{}).
		Where("share_id = ? AND status = ?", shareID, ShareDeviceStatusActive).
		Count(&count).Error
	return count, err
}

// EnsureShareDevice returns the matching device, creating it when missing.
// The unique composite index makes concurrent auto-binding safe: a creation
// race is resolved by re-reading the row.
func EnsureShareDevice(shareID int, hwid, deviceID, name, platform, appVersion, status string) (*ShareDevice, error) {
	hwid = NormalizeShareDeviceValue(hwid, 128)
	deviceID = NormalizeShareDeviceValue(deviceID, 128)
	if shareID <= 0 || hwid == "" || deviceID == "" {
		return nil, fmt.Errorf("missing device identity")
	}
	status = normalizeShareDeviceStatus(status)
	now := time.Now()

	device, err := FindShareDevice(shareID, hwid, deviceID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		secret, secretErr := GenerateShareDeviceSecret()
		if secretErr != nil {
			return nil, secretErr
		}
		device = &ShareDevice{
			ShareID:     shareID,
			HWID:        hwid,
			DeviceID:    deviceID,
			Name:        NormalizeShareDeviceValue(name, 100),
			Platform:    NormalizeShareDeviceValue(platform, 50),
			AppVersion:  NormalizeShareDeviceValue(appVersion, 50),
			Status:      status,
			Secret:      secret,
			FirstSeenAt: now,
			LastSeenAt:  now,
		}
		if device.Name == "" {
			device.Name = defaultShareDeviceName(platform, deviceID)
		}
		if createErr := database.DB.Create(device).Error; createErr != nil {
			// Another request may have inserted the same identity concurrently.
			device, err = FindShareDevice(shareID, hwid, deviceID)
			if err != nil {
				return nil, createErr
			}
		}
	} else {
		updates := map[string]any{
			"last_seen_at": now,
			"updated_at":   now,
		}
		if name != "" {
			updates["name"] = NormalizeShareDeviceValue(name, 100)
		}
		if platform != "" {
			updates["platform"] = NormalizeShareDeviceValue(platform, 50)
		}
		if appVersion != "" {
			updates["app_version"] = NormalizeShareDeviceValue(appVersion, 50)
		}
		if err := database.DB.Model(&ShareDevice{}).Where("id = ?", device.ID).Updates(updates).Error; err != nil {
			return nil, err
		}
		device.LastSeenAt = now
	}
	return device, nil
}

func normalizeShareDeviceStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case ShareDeviceStatusActive:
		return ShareDeviceStatusActive
	case ShareDeviceStatusPending:
		return ShareDeviceStatusPending
	case ShareDeviceStatusBlocked:
		return ShareDeviceStatusBlocked
	default:
		return ShareDeviceStatusActive
	}
}

func defaultShareDeviceName(platform, deviceID string) string {
	name := strings.TrimSpace(platform)
	if name == "" {
		name = "Device"
	}
	suffix := deviceID
	if len(suffix) > 6 {
		suffix = suffix[len(suffix)-6:]
	}
	if suffix == "" {
		return name
	}
	return name + " " + suffix
}

// shareDeviceLimitMu serialises limit checks inside this process so two
// concurrent auto-binds cannot both take the last free slot.
var shareDeviceLimitMu sync.Mutex

// EnsureShareDeviceWithLimit binds a device while enforcing the active-device
// limit. The second return value is false when the limit is already reached.
func EnsureShareDeviceWithLimit(shareID, maxDevices int, hwid, deviceID, name, platform, appVersion, status string) (*ShareDevice, bool, error) {
	shareDeviceLimitMu.Lock()
	defer shareDeviceLimitMu.Unlock()

	if status == ShareDeviceStatusActive && maxDevices > 0 {
		count, err := CountActiveShareDevices(shareID)
		if err != nil {
			return nil, false, err
		}
		if count >= int64(maxDevices) {
			return nil, false, nil
		}
	}

	device, err := EnsureShareDevice(shareID, hwid, deviceID, name, platform, appVersion, status)
	if err != nil {
		return nil, false, err
	}
	return device, true, nil
}

// AddShareDeviceAbsoluteReport applies one absolute installation counter
// report (reported_total_bytes). The compare-and-swap on the baseline pair
// keeps concurrent reports safe: a lost race re-reads the row and retries.
// It returns the accepted delta; replays, stale smaller totals and unknown
// regressions yield a zero delta and update the local device copy only.
func AddShareDeviceAbsoluteReport(device *ShareDevice, total int64, installID string, at time.Time) (int64, bool, error) {
	if device == nil || device.ID <= 0 {
		return 0, false, nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	at = at.UTC()

	for attempt := 0; attempt < 3; attempt++ {
		current, err := GetShareDeviceByID(device.ID)
		if err != nil {
			return 0, false, err
		}
		baseline := current.ReportBaselineBytes
		newEpoch := current.ReportBaselineEpoch
		if installID != "" && installID != current.ReportBaselineEpoch {
			// A new installation epoch restarts the client counter from zero.
			baseline = 0
			newEpoch = installID
		}
		delta := total - baseline
		if delta <= 0 {
			// Replay or a stale smaller total: the baseline stays untouched,
			// otherwise a crash between the client and server writes would keep
			// re-adding the same bytes.
			*device = *current
			return 0, false, nil
		}

		result := database.DB.Model(&ShareDevice{}).
			Where("id = ? AND report_baseline_bytes = ? AND report_baseline_epoch = ?",
				current.ID, current.ReportBaselineBytes, current.ReportBaselineEpoch).
			Updates(map[string]any{
				"report_baseline_bytes":  total,
				"report_baseline_epoch":  newEpoch,
				"reported_traffic_bytes": gorm.Expr("reported_traffic_bytes + ?", delta),
				"last_report_at":         at,
				"updated_at":             at,
			})
		if result.Error != nil {
			return 0, false, result.Error
		}
		if result.RowsAffected == 0 {
			continue
		}

		updated, err := GetShareDeviceByID(device.ID)
		if err != nil {
			// The write succeeded; failing to re-read must not fail the report.
			current.ReportBaselineBytes = total
			current.ReportBaselineEpoch = newEpoch
			current.ReportedTrafficBytes += delta
			current.LastReportAt = &at
			*device = *current
			return delta, true, nil
		}
		*device = *updated
		return delta, true, nil
	}
	return 0, false, fmt.Errorf("traffic baseline changed concurrently; retry the report")
}

// Update persists administrator-editable device fields.
func (d *ShareDevice) Update() error {
	d.Name = NormalizeShareDeviceValue(d.Name, 100)
	d.Description = NormalizeShareDeviceValue(d.Description, 500)
	if d.Status == "" {
		d.Status = ShareDeviceStatusActive
	}
	d.Status = normalizeShareDeviceStatus(d.Status)

	updates := map[string]any{
		"name":        d.Name,
		"description": d.Description,
		"status":      d.Status,
		"updated_at":  time.Now(),
	}
	if d.HWID != "" {
		updates["hwid"] = NormalizeShareDeviceValue(d.HWID, 128)
	}
	if d.Secret != "" {
		updates["secret"] = d.Secret
	}
	if err := database.DB.Model(&ShareDevice{}).Where("id = ?", d.ID).Updates(updates).Error; err != nil {
		return err
	}
	return nil
}

// Delete removes the device together with its hourly traffic buckets.
// Historical access rows remain for auditing.
func (d *ShareDevice) Delete() error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("device_id = ?", d.ID).Delete(&ShareDeviceUsageHour{}).Error; err != nil {
			return err
		}
		return tx.Delete(&ShareDevice{}, d.ID).Error
	})
}

// TouchShareDevice updates the last-seen timestamp.
func TouchShareDevice(id int) {
	if id <= 0 {
		return
	}
	now := time.Now()
	_ = database.DB.Model(&ShareDevice{}).Where("id = ?", id).Updates(map[string]any{
		"last_seen_at": now,
		"updated_at":   now,
	}).Error
}

// DeleteShareDevices removes all devices bound to a link together with their
// hourly traffic buckets.
func DeleteShareDevices(shareID int) error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		var ids []int
		if err := tx.Model(&ShareDevice{}).Where("share_id = ?", shareID).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) > 0 {
			if err := tx.Where("device_id IN ?", ids).Delete(&ShareDeviceUsageHour{}).Error; err != nil {
				return err
			}
		}
		return tx.Where("share_id = ?", shareID).Delete(&ShareDevice{}).Error
	})
}
