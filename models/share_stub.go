package models

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"sublink/cache"
	"sublink/database"
	"sublink/utils"

	"gorm.io/gorm"
)

// Stub cases. A stub is an editable message returned instead of a real
// subscription payload when a share cannot be served.
const (
	ShareStubCaseDeviceLimit      = "device_limit"
	ShareStubCaseDevicePending    = "device_pending"
	ShareStubCaseDeviceBlocked    = "device_blocked"
	ShareStubCaseQuotaExhausted   = "quota_exhausted"
	ShareStubCaseExpired          = "expired"
	ShareStubCaseGroupDisabled    = "group_disabled"
	ShareStubCaseUnsupportedApp   = "unsupported_app"
	ShareStubCaseTemplateDisabled = "template_disabled"
	ShareStubCaseInvalidToken     = "invalid_token"
)

// ShareStubCaseKeys is the stable, ordered list used by the UI and validation.
var ShareStubCaseKeys = []string{
	ShareStubCaseDeviceLimit,
	ShareStubCaseDevicePending,
	ShareStubCaseDeviceBlocked,
	ShareStubCaseQuotaExhausted,
	ShareStubCaseExpired,
	ShareStubCaseGroupDisabled,
	ShareStubCaseUnsupportedApp,
	ShareStubCaseTemplateDisabled,
	ShareStubCaseInvalidToken,
}

// ShareStub is an editable message shown to a client when a share cannot be
// delivered. Title is a short label, Content is embedded into the generated
// profile (it becomes the placeholder node name).
type ShareStub struct {
	ID        int       `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:100" json:"name"`
	CaseKey   string    `gorm:"uniqueIndex;size:40" json:"case_key"`
	Title     string    `gorm:"size:200" json:"title"`
	Content   string    `gorm:"type:text" json:"content"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// ShareGroupStub maps a group case to a stub from the shared library.
type ShareGroupStub struct {
	ID      int    `gorm:"primaryKey" json:"id"`
	GroupID int    `gorm:"uniqueIndex:idx_share_group_stub;not null" json:"group_id"`
	CaseKey string `gorm:"uniqueIndex:idx_share_group_stub;size:40" json:"case_key"`
	StubID  int    `gorm:"index" json:"stub_id"`
}

// ShareSubscriptionStub overrides a group stub for one subscription link.
type ShareSubscriptionStub struct {
	ID      int    `gorm:"primaryKey" json:"id"`
	ShareID int    `gorm:"uniqueIndex:idx_share_subscription_stub;not null" json:"share_id"`
	CaseKey string `gorm:"uniqueIndex:idx_share_subscription_stub;size:40" json:"case_key"`
	StubID  int    `gorm:"index" json:"stub_id"`
}

var shareStubCache *cache.MapCache[int, ShareStub]
var shareGroupStubCache *cache.MapCache[int, ShareGroupStub]
var shareSubscriptionStubCache *cache.MapCache[int, ShareSubscriptionStub]

func init() {
	shareStubCache = cache.NewMapCache(func(s ShareStub) int { return s.ID })
	shareStubCache.AddIndex("caseKey", func(s ShareStub) string { return s.CaseKey })
	shareGroupStubCache = cache.NewMapCache(func(m ShareGroupStub) int { return m.ID })
	shareGroupStubCache.AddIndex("groupID", func(m ShareGroupStub) string { return strconv.Itoa(m.GroupID) })
	shareSubscriptionStubCache = cache.NewMapCache(func(m ShareSubscriptionStub) int { return m.ID })
	shareSubscriptionStubCache.AddIndex("shareID", func(m ShareSubscriptionStub) string { return strconv.Itoa(m.ShareID) })
}

// IsValidShareStubCase reports whether caseKey is a known stub case.
func IsValidShareStubCase(caseKey string) bool {
	for _, key := range ShareStubCaseKeys {
		if key == caseKey {
			return true
		}
	}
	return false
}

// DefaultShareStubs returns the seeded stub library.
func DefaultShareStubs() []ShareStub {
	return []ShareStub{
		{
			Name:    "Device limit reached",
			CaseKey: ShareStubCaseDeviceLimit,
			Title:   "Device limit reached",
			Content: "This subscription link has reached its device limit ({limit}). Remove an unused device or contact the administrator.",
			Enabled: true,
		},
		{
			Name:    "Device approval required",
			CaseKey: ShareStubCaseDevicePending,
			Title:   "Device approval required",
			Content: "This device is waiting for approval. Open the link again after the administrator approves it.",
			Enabled: true,
		},
		{
			Name:    "Device blocked",
			CaseKey: ShareStubCaseDeviceBlocked,
			Title:   "Device blocked",
			Content: "This device is blocked for this subscription link.",
			Enabled: true,
		},
		{
			Name:    "Traffic limit exhausted",
			CaseKey: ShareStubCaseQuotaExhausted,
			Title:   "Traffic limit exhausted",
			Content: "This subscription link has used its traffic limit ({used} of {limit}).",
			Enabled: true,
		},
		{
			Name:    "Subscription link expired",
			CaseKey: ShareStubCaseExpired,
			Title:   "Subscription link expired",
			Content: "This subscription link expired on {expires_at}.",
			Enabled: true,
		},
		{
			Name:    "Group disabled",
			CaseKey: ShareStubCaseGroupDisabled,
			Title:   "Subscription group disabled",
			Content: "This group of subscription links is temporarily disabled.",
			Enabled: true,
		},
		{
			Name:    "Unsupported app",
			CaseKey: ShareStubCaseUnsupportedApp,
			Title:   "Unsupported app",
			Content: "This subscription is configured for SNKP only. Import it with SNKP.",
			Enabled: true,
		},
		{
			Name:    "Template not enabled",
			CaseKey: ShareStubCaseTemplateDisabled,
			Title:   "Template not enabled",
			Content: "This client format is not enabled for this subscription group.",
			Enabled: true,
		},
		{
			Name:    "Invalid link",
			CaseKey: ShareStubCaseInvalidToken,
			Title:   "Invalid subscription link",
			Content: "This subscription link does not exist.",
			Enabled: true,
		},
	}
}

// SeedDefaultShareStubs inserts missing default stubs without overwriting
// edits made by the administrator.
func SeedDefaultShareStubs(db *gorm.DB) error {
	for _, stub := range DefaultShareStubs() {
		var existing ShareStub
		err := db.Where("case_key = ?", stub.CaseKey).First(&existing).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := db.Create(&stub).Error; err != nil {
				return fmt.Errorf("failed to seed stub %s: %w", stub.CaseKey, err)
			}
			continue
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// InitShareStubCache loads stubs and their mappings into memory.
func InitShareStubCache() error {
	utils.Info("Loading share stubs into cache")

	var stubs []ShareStub
	if err := database.DB.Find(&stubs).Error; err != nil {
		return err
	}
	shareStubCache.LoadAll(stubs)

	var groupMappings []ShareGroupStub
	if err := database.DB.Find(&groupMappings).Error; err != nil {
		return err
	}
	shareGroupStubCache.LoadAll(groupMappings)

	var shareMappings []ShareSubscriptionStub
	if err := database.DB.Find(&shareMappings).Error; err != nil {
		return err
	}
	shareSubscriptionStubCache.LoadAll(shareMappings)

	cache.Manager.Register("share_stubs", shareStubCache)
	cache.Manager.Register("share_group_stubs", shareGroupStubCache)
	cache.Manager.Register("share_subscription_stubs", shareSubscriptionStubCache)
	utils.Info("Share stub cache initialized with %d stubs", shareStubCache.Count())
	return nil
}

// Add creates a stub.
func (s *ShareStub) Add() error {
	s.Name = strings.TrimSpace(s.Name)
	s.Title = strings.TrimSpace(s.Title)
	if s.Name == "" {
		s.Name = s.Title
	}
	if s.CaseKey == "" || !IsValidShareStubCase(s.CaseKey) {
		return fmt.Errorf("invalid stub case")
	}
	if err := database.DB.Create(s).Error; err != nil {
		return err
	}
	shareStubCache.Set(s.ID, *s)
	return nil
}

// Update persists editable stub fields.
func (s *ShareStub) Update() error {
	s.Name = strings.TrimSpace(s.Name)
	s.Title = strings.TrimSpace(s.Title)
	if s.Name == "" {
		s.Name = s.Title
	}
	err := database.DB.Model(&ShareStub{}).Where("id = ?", s.ID).Updates(map[string]any{
		"name":     s.Name,
		"title":    s.Title,
		"content":  s.Content,
		"enabled":  s.Enabled,
		"case_key": s.CaseKey,
	}).Error
	if err != nil {
		return err
	}
	var updated ShareStub
	if err := database.DB.First(&updated, s.ID).Error; err == nil {
		*s = updated
		shareStubCache.Set(updated.ID, updated)
	}
	return nil
}

// Delete removes a stub and any mappings that point at it.
func (s *ShareStub) Delete() error {
	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("stub_id = ?", s.ID).Delete(&ShareGroupStub{}).Error; err != nil {
			return err
		}
		if err := tx.Where("stub_id = ?", s.ID).Delete(&ShareSubscriptionStub{}).Error; err != nil {
			return err
		}
		return tx.Delete(&ShareStub{}, s.ID).Error
	})
	if err != nil {
		return err
	}
	shareStubCache.Delete(s.ID)
	for _, mapping := range shareGroupStubCache.GetAll() {
		if mapping.StubID == s.ID {
			shareGroupStubCache.Delete(mapping.ID)
		}
	}
	for _, mapping := range shareSubscriptionStubCache.GetAll() {
		if mapping.StubID == s.ID {
			shareSubscriptionStubCache.Delete(mapping.ID)
		}
	}
	return nil
}

// Find loads a stub by ID.
func (s *ShareStub) Find() error {
	if cached, ok := shareStubCache.Get(s.ID); ok {
		*s = cached
		return nil
	}
	if err := database.DB.First(s, s.ID).Error; err != nil {
		return err
	}
	shareStubCache.Set(s.ID, *s)
	return nil
}

// ListShareStubs returns all stubs ordered by case key.
func ListShareStubs() []ShareStub {
	stubs := shareStubCache.GetAll()
	sort.Slice(stubs, func(i, j int) bool { return stubs[i].CaseKey < stubs[j].CaseKey })
	return stubs
}

// GetShareStubByCaseKey returns the enabled stub assigned to a case, falling
// back to the built-in default when the library has no usable entry.
func GetShareStubByCaseKey(caseKey string) ShareStub {
	for _, stub := range shareStubCache.GetByIndex("caseKey", caseKey) {
		if stub.Enabled {
			return stub
		}
	}
	for _, stub := range DefaultShareStubs() {
		if stub.CaseKey == caseKey {
			return stub
		}
	}
	return ShareStub{
		CaseKey: caseKey,
		Title:   "Subscription unavailable",
		Content: "This subscription link is temporarily unavailable.",
		Enabled: true,
	}
}

// GetShareGroupStubMap returns the explicit case -> stub mapping for a group.
func GetShareGroupStubMap(groupID int) map[string]int {
	result := make(map[string]int)
	for _, mapping := range shareGroupStubCache.GetByIndex("groupID", strconv.Itoa(groupID)) {
		result[mapping.CaseKey] = mapping.StubID
	}
	return result
}

// GetShareSubscriptionStubMap returns the explicit case -> stub overrides.
func GetShareSubscriptionStubMap(shareID int) map[string]int {
	result := make(map[string]int)
	for _, mapping := range shareSubscriptionStubCache.GetByIndex("shareID", strconv.Itoa(shareID)) {
		result[mapping.CaseKey] = mapping.StubID
	}
	return result
}

// SetShareGroupStubs replaces the group mapping. Empty mapping removes all
// overrides and lets the library defaults apply.
func SetShareGroupStubs(groupID int, mapping map[string]int) error {
	for caseKey, stubID := range mapping {
		if !IsValidShareStubCase(caseKey) {
			return fmt.Errorf("invalid stub case: %s", caseKey)
		}
		var stub ShareStub
		stub.ID = stubID
		if err := stub.Find(); err != nil {
			return fmt.Errorf("stub %d not found", stubID)
		}
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", groupID).Delete(&ShareGroupStub{}).Error; err != nil {
			return err
		}
		for caseKey, stubID := range mapping {
			if err := tx.Create(&ShareGroupStub{GroupID: groupID, CaseKey: caseKey, StubID: stubID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, old := range shareGroupStubCache.GetAll() {
		if old.GroupID == groupID {
			shareGroupStubCache.Delete(old.ID)
		}
	}
	var rows []ShareGroupStub
	if err := database.DB.Where("group_id = ?", groupID).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		shareGroupStubCache.Set(row.ID, row)
	}
	return nil
}

// SetShareSubscriptionStubs replaces per-subscription overrides.
func SetShareSubscriptionStubs(shareID int, mapping map[string]int) error {
	for caseKey, stubID := range mapping {
		if !IsValidShareStubCase(caseKey) {
			return fmt.Errorf("invalid stub case: %s", caseKey)
		}
		var stub ShareStub
		stub.ID = stubID
		if err := stub.Find(); err != nil {
			return fmt.Errorf("stub %d not found", stubID)
		}
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("share_id = ?", shareID).Delete(&ShareSubscriptionStub{}).Error; err != nil {
			return err
		}
		for caseKey, stubID := range mapping {
			if err := tx.Create(&ShareSubscriptionStub{ShareID: shareID, CaseKey: caseKey, StubID: stubID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	for _, old := range shareSubscriptionStubCache.GetAll() {
		if old.ShareID == shareID {
			shareSubscriptionStubCache.Delete(old.ID)
		}
	}
	var rows []ShareSubscriptionStub
	if err := database.DB.Where("share_id = ?", shareID).Find(&rows).Error; err != nil {
		return err
	}
	for _, row := range rows {
		shareSubscriptionStubCache.Set(row.ID, row)
	}
	return nil
}

// ResolveShareStub picks the stub for a case: subscription override, then
// group mapping, then the global library default.
func ResolveShareStub(share *SubscriptionShare, caseKey string) ShareStub {
	if share != nil {
		for _, mapping := range shareSubscriptionStubCache.GetByIndex("shareID", strconv.Itoa(share.ID)) {
			if mapping.CaseKey == caseKey {
				if stub, ok := shareStubCache.Get(mapping.StubID); ok && stub.Enabled {
					return stub
				}
			}
		}
		if share.GroupID != nil {
			for _, mapping := range shareGroupStubCache.GetByIndex("groupID", strconv.Itoa(*share.GroupID)) {
				if mapping.CaseKey == caseKey {
					if stub, ok := shareStubCache.Get(mapping.StubID); ok && stub.Enabled {
						return stub
					}
				}
			}
		}
	}
	return GetShareStubByCaseKey(caseKey)
}

// RenderShareStub replaces {placeholders} in the stub title and content.
func RenderShareStub(stub ShareStub, vars map[string]string) (string, string) {
	title := stub.Title
	content := stub.Content
	for key, value := range vars {
		title = strings.ReplaceAll(title, "{"+key+"}", value)
		content = strings.ReplaceAll(content, "{"+key+"}", value)
	}
	return title, content
}
