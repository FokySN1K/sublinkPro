package models

import (
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

// Share templates exposed by a share group. They are independent from the
// renderer client types: "auto" resolves from the request, "mihomo_snkp" is
// the SNKP flavoured mihomo profile.
const (
	ShareTemplateAuto       = "auto"
	ShareTemplateMihomo     = "mihomo"
	ShareTemplateMihomoSnkp = "mihomo_snkp"
	ShareTemplateSurge      = "surge"
	ShareTemplateV2Ray      = "v2ray"
)

// DefaultShareTemplates is used for groups created without an explicit list.
const DefaultShareTemplates = "auto,mihomo,mihomo_snkp,surge,v2ray"

// Device enrolment modes for a group.
const (
	ShareDeviceModeOpen     = "open"      // any device is served and recorded
	ShareDeviceModeAutoBind = "auto_bind" // unknown devices are bound automatically
	ShareDeviceModeApprove  = "approve"   // unknown devices wait for admin approval
)

// Telemetry report interval bounds (seconds).
const (
	DefaultReportIntervalSeconds = 3600
	MinReportIntervalSeconds     = 300
	MaxReportIntervalSeconds     = 86400
)

// Telemetry channels reported by SNKP.
const (
	ShareTelemetryChannelSessions    = "sessions"
	ShareTelemetryChannelNodes       = "nodes"
	ShareTelemetryChannelReliability = "reliability"
)

// ShareGroup is a delivery policy. It references an existing Subcription
// (the node pool) and is inherited by subscription links created from it.
type ShareGroup struct {
	ID          int    `gorm:"primaryKey" json:"id"`
	Name        string `gorm:"size:100" json:"name"`
	Description string `gorm:"size:500" json:"description"`

	// Node pool reference (existing filtered/named subscription).
	SubscriptionID int `gorm:"index" json:"subscription_id"`

	Enabled    bool `json:"enabled"`
	HideConfig bool `json:"hide_config"` // only SNKP / mihomo_snkp may be served

	// Templates is a comma separated subset of ShareTemplate* values.
	Templates string `gorm:"size:255" json:"templates"`
	// MihomoSnkp serves mihomo_snkp to SNKP clients that request the mihomo template.
	MihomoSnkp bool `json:"mihomo_snkp"`

	// ClashTemplate / SurgeTemplate select the template file rendered for links
	// served by this group. Empty values inherit the node pool (and built-in
	// defaults) so existing groups keep their behaviour.
	ClashTemplate string `gorm:"size:191" json:"clash_template"`
	SurgeTemplate string `gorm:"size:191" json:"surge_template"`

	// AllowLegacyClients serves links without X-HWID/X-Device-ID headers.
	// It only applies when the group is managed by devices; legacy links are
	// always served.
	AllowLegacyClients bool `json:"allow_legacy_clients"`

	DeviceMode string `gorm:"size:20" json:"device_mode"`
	MaxDevices int    `gorm:"default:0" json:"max_devices"` // 0 = unlimited

	TrafficLimitBytes int64 `gorm:"default:0" json:"traffic_limit_bytes"` // 0 = unlimited

	ExpireType int        `gorm:"default:0" json:"expire_type"`
	ExpireDays int        `gorm:"default:0" json:"expire_days"`
	ExpireAt   *time.Time `json:"expire_at"`

	TelemetryEnabled          bool `json:"telemetry_enabled"`
	ReportIntervalSessions    int  `gorm:"default:3600" json:"report_interval_sessions"`
	ReportIntervalNodes       int  `gorm:"default:3600" json:"report_interval_nodes"`
	ReportIntervalReliability int  `gorm:"default:3600" json:"report_interval_reliability"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

var shareGroupCache *cache.MapCache[int, ShareGroup]

func init() {
	shareGroupCache = cache.NewMapCache(func(g ShareGroup) int { return g.ID })
	shareGroupCache.AddIndex("subscriptionID", func(g ShareGroup) string { return strconv.Itoa(g.SubscriptionID) })
}

// InitShareGroupCache loads share groups into the write-through cache.
func InitShareGroupCache() error {
	utils.Info("Loading share groups into cache")
	var groups []ShareGroup
	if err := database.DB.Find(&groups).Error; err != nil {
		return err
	}
	shareGroupCache.LoadAll(groups)
	utils.Info("Share group cache initialized with %d records", shareGroupCache.Count())
	cache.Manager.Register("share_groups", shareGroupCache)
	return nil
}

// NormalizeShareTemplate returns a supported template name or "".
func NormalizeShareTemplate(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case ShareTemplateAuto:
		return ShareTemplateAuto
	case ShareTemplateMihomo:
		return ShareTemplateMihomo
	case ShareTemplateMihomoSnkp:
		return ShareTemplateMihomoSnkp
	case ShareTemplateSurge:
		return ShareTemplateSurge
	case ShareTemplateV2Ray:
		return ShareTemplateV2Ray
	default:
		return ""
	}
}

// ParseShareTemplates splits a raw comma separated template list and removes
// unsupported or duplicated values. Order follows the input.
func ParseShareTemplates(raw string) []string {
	result := make([]string, 0, 5)
	seen := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		template := NormalizeShareTemplate(part)
		if template == "" || seen[template] {
			continue
		}
		seen[template] = true
		result = append(result, template)
	}
	return result
}

// NormalizeShareTemplatesString normalizes a raw template list and falls back
// to the built-in default when nothing valid is left.
func NormalizeShareTemplatesString(raw string) string {
	templates := ParseShareTemplates(raw)
	if len(templates) == 0 {
		templates = ParseShareTemplates(DefaultShareTemplates)
	}
	return strings.Join(templates, ",")
}

// NormalizeShareDeviceMode returns a supported device mode or the open mode.
func NormalizeShareDeviceMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case ShareDeviceModeOpen:
		return ShareDeviceModeOpen
	case ShareDeviceModeAutoBind:
		return ShareDeviceModeAutoBind
	case ShareDeviceModeApprove:
		return ShareDeviceModeApprove
	default:
		return ShareDeviceModeOpen
	}
}

// NormalizeReportInterval clamps a telemetry interval into the supported range.
func NormalizeReportInterval(seconds int) int {
	if seconds <= 0 {
		return DefaultReportIntervalSeconds
	}
	if seconds < MinReportIntervalSeconds {
		return MinReportIntervalSeconds
	}
	if seconds > MaxReportIntervalSeconds {
		return MaxReportIntervalSeconds
	}
	return seconds
}

func (g *ShareGroup) normalize() {
	g.Name = strings.TrimSpace(g.Name)
	g.Description = strings.TrimSpace(g.Description)
	g.Templates = NormalizeShareTemplatesString(g.Templates)
	g.ClashTemplate = strings.TrimSpace(g.ClashTemplate)
	g.SurgeTemplate = strings.TrimSpace(g.SurgeTemplate)
	g.DeviceMode = NormalizeShareDeviceMode(g.DeviceMode)
	g.ReportIntervalSessions = NormalizeReportInterval(g.ReportIntervalSessions)
	g.ReportIntervalNodes = NormalizeReportInterval(g.ReportIntervalNodes)
	g.ReportIntervalReliability = NormalizeReportInterval(g.ReportIntervalReliability)
	if g.ExpireType != ExpireTypeDateTime {
		g.ExpireAt = nil
	} else {
		g.ExpireAt = normalizeOptionalTime(g.ExpireAt)
	}
}

// Validate checks required fields and the referenced node pool.
func (g *ShareGroup) Validate() error {
	if g.Name == "" {
		return fmt.Errorf("group name is required")
	}
	if g.SubscriptionID <= 0 {
		return fmt.Errorf("node pool is required")
	}
	var sub Subcription
	sub.ID = g.SubscriptionID
	if err := sub.Find(); err != nil {
		return fmt.Errorf("node pool not found")
	}
	if err := validateShareGroupTemplate(g.ClashTemplate, "clash"); err != nil {
		return err
	}
	if err := validateShareGroupTemplate(g.SurgeTemplate, "surge"); err != nil {
		return err
	}
	return nil
}

// validateShareGroupTemplate ensures an explicitly selected template file exists
// and belongs to the expected category. Empty means "inherit".
func validateShareGroupTemplate(name, category string) error {
	if name == "" {
		return nil
	}
	var tmpl Template
	if err := tmpl.FindByName(name); err != nil {
		return fmt.Errorf("%s template not found: %s", category, name)
	}
	if tmpl.Category != "" && tmpl.Category != category {
		return fmt.Errorf("%s is not a %s template", name, category)
	}
	return nil
}

// TemplateList returns the configured templates.
func (g *ShareGroup) TemplateList() []string {
	return ParseShareTemplates(g.Templates)
}

// EffectiveTemplates returns the templates actually served. Hide-config groups
// are restricted to mihomo_snkp.
func (g *ShareGroup) EffectiveTemplates() []string {
	if g.HideConfig {
		return []string{ShareTemplateMihomoSnkp}
	}
	return g.TemplateList()
}

// AllowsTemplate reports whether a template may be requested by a client.
func (g *ShareGroup) AllowsTemplate(template string) bool {
	template = NormalizeShareTemplate(template)
	if template == "" {
		return false
	}
	if g.HideConfig {
		return template == ShareTemplateMihomoSnkp
	}
	for _, allowed := range g.TemplateList() {
		if allowed == template {
			return true
		}
	}
	return false
}

// Add creates a group and writes it into the cache.
func (g *ShareGroup) Add() error {
	g.normalize()
	if err := g.Validate(); err != nil {
		return err
	}
	if err := database.DB.Create(g).Error; err != nil {
		return err
	}
	shareGroupCache.Set(g.ID, *g)
	return nil
}

// Update persists the mutable fields and refreshes the cache.
func (g *ShareGroup) Update() error {
	g.normalize()
	if err := g.Validate(); err != nil {
		return err
	}
	err := database.DB.Model(&ShareGroup{}).Where("id = ?", g.ID).Updates(map[string]any{
		"name":                        g.Name,
		"description":                 g.Description,
		"subscription_id":             g.SubscriptionID,
		"enabled":                     g.Enabled,
		"hide_config":                 g.HideConfig,
		"templates":                   g.Templates,
		"mihomo_snkp":                 g.MihomoSnkp,
		"clash_template":              g.ClashTemplate,
		"surge_template":              g.SurgeTemplate,
		"allow_legacy_clients":        g.AllowLegacyClients,
		"device_mode":                 g.DeviceMode,
		"max_devices":                 g.MaxDevices,
		"traffic_limit_bytes":         g.TrafficLimitBytes,
		"expire_type":                 g.ExpireType,
		"expire_days":                 g.ExpireDays,
		"expire_at":                   g.ExpireAt,
		"telemetry_enabled":           g.TelemetryEnabled,
		"report_interval_sessions":    g.ReportIntervalSessions,
		"report_interval_nodes":       g.ReportIntervalNodes,
		"report_interval_reliability": g.ReportIntervalReliability,
		"updated_at":                  time.Now(),
	}).Error
	if err != nil {
		return err
	}
	shareGroupCache.Set(g.ID, *g)
	return nil
}

// Delete removes the group. Subscription links that belonged to it fall back
// to unmanaged legacy serving; their snapshot fields stay in place but are
// ignored while group_id is NULL.
func (g *ShareGroup) Delete() error {
	detached := make([]SubscriptionShare, 0)
	for _, share := range subscriptionShareCache.GetAll() {
		if share.GroupID != nil && *share.GroupID == g.ID {
			detached = append(detached, share)
		}
	}

	err := database.DB.Transaction(func(tx *gorm.DB) error {
		for _, share := range detached {
			if err := tx.Model(&SubscriptionShare{}).Where("id = ?", share.ID).Update("group_id", nil).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("group_id = ?", g.ID).Delete(&ShareGroupStub{}).Error; err != nil {
			return err
		}
		return tx.Delete(&ShareGroup{}, g.ID).Error
	})
	if err != nil {
		return err
	}

	for _, share := range detached {
		share.GroupID = nil
		subscriptionShareCache.Set(share.ID, share)
	}
	shareGroupCache.Delete(g.ID)
	for _, mapping := range shareGroupStubCache.GetAll() {
		if mapping.GroupID == g.ID {
			shareGroupStubCache.Delete(mapping.ID)
		}
	}
	return nil
}

// Find loads a group by ID, using the cache when possible.
func (g *ShareGroup) Find() error {
	if cached, ok := shareGroupCache.Get(g.ID); ok {
		*g = cached
		return nil
	}
	if err := database.DB.First(g, g.ID).Error; err != nil {
		return err
	}
	shareGroupCache.Set(g.ID, *g)
	return nil
}

// GetShareGroupByID is a convenience wrapper around Find.
func GetShareGroupByID(id int) (*ShareGroup, error) {
	var group ShareGroup
	group.ID = id
	if err := group.Find(); err != nil {
		return nil, err
	}
	return &group, nil
}

// ListShareGroups returns all groups ordered by ID.
func ListShareGroups() []ShareGroup {
	groups := shareGroupCache.GetAll()
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	return groups
}

// GetShareGroupsBySubscriptionID returns groups that use the given node pool.
func GetShareGroupsBySubscriptionID(subID int) []ShareGroup {
	groups := shareGroupCache.GetByIndex("subscriptionID", strconv.Itoa(subID))
	sort.Slice(groups, func(i, j int) bool { return groups[i].ID < groups[j].ID })
	return groups
}

// IsShareGroupEnabled reports whether the group currently allows serving.
func IsShareGroupEnabled(group *ShareGroup) bool {
	return group == nil || group.Enabled
}
