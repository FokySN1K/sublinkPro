package models

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sublink/cache"
	"sublink/database"
	"sublink/utils"
	"sync"
	"time"

	"gorm.io/gorm"
)

// 过期类型常量
const (
	ExpireTypeNever    = 0 // 永不过期
	ExpireTypeDays     = 1 // 按天数过期
	ExpireTypeDateTime = 2 // 指定时间过期
)

// SubscriptionShare 订阅分享表
type SubscriptionShare struct {
	ID             int        `gorm:"primaryKey" json:"id"`
	SubscriptionID int        `gorm:"index" json:"subscription_id"`     // 关联订阅ID
	Token          string     `gorm:"uniqueIndex;size:64" json:"token"` // 分享token（支持自定义或自动生成）
	Name           string     `gorm:"size:100" json:"name"`             // 分享名称/备注
	ExpireType     int        `gorm:"default:0" json:"expire_type"`     // 过期类型
	ExpireDays     int        `gorm:"default:0" json:"expire_days"`     // 过期天数
	ExpireAt       *time.Time `json:"expire_at"`                        // 过期时间
	IsLegacy       bool       `gorm:"default:false" json:"is_legacy"`   // 是否为迁移的老链接
	Enabled        bool       `gorm:"default:true" json:"enabled"`      // 是否启用
	AccessCount    int        `gorm:"default:0" json:"access_count"`    // 访问次数
	LastAccessAt   *time.Time `json:"last_access_at"`                   // 最后访问时间

	// GroupID is nil for legacy/unmanaged links. Managed links inherit a
	// snapshot of the group settings at creation time.
	GroupID                   *int       `gorm:"index" json:"group_id"`
	TrafficLimitBytes         int64      `gorm:"default:0" json:"traffic_limit_bytes"`
	MaxDevices                int        `gorm:"default:0" json:"max_devices"`
	ReportIntervalSessions    int        `gorm:"default:0" json:"report_interval_sessions"`
	ReportIntervalNodes       int        `gorm:"default:0" json:"report_interval_nodes"`
	ReportIntervalReliability int        `gorm:"default:0" json:"report_interval_reliability"`
	ReportedTrafficBytes      int64      `gorm:"default:0" json:"reported_traffic_bytes"`
	LastUsageAt               *time.Time `json:"last_usage_at"`

	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// subscriptionShareCache 使用泛型缓存
var subscriptionShareCache *cache.MapCache[int, SubscriptionShare]

func init() {
	subscriptionShareCache = cache.NewMapCache(func(s SubscriptionShare) int { return s.ID })
	subscriptionShareCache.AddIndex("token", func(s SubscriptionShare) string { return s.Token })
	subscriptionShareCache.AddIndex("subscriptionID", func(s SubscriptionShare) string { return strconv.Itoa(s.SubscriptionID) })
	subscriptionShareCache.AddIndex("groupID", func(s SubscriptionShare) string {
		if s.GroupID == nil {
			return ""
		}
		return strconv.Itoa(*s.GroupID)
	})
}

// InitSubscriptionShareCache 初始化订阅分享缓存
func InitSubscriptionShareCache() error {
	utils.Info("Loading subscription shares into cache")
	var shares []SubscriptionShare
	if err := database.DB.Find(&shares).Error; err != nil {
		return err
	}

	subscriptionShareCache.LoadAll(shares)
	utils.Info("Subscription share cache initialized with %d records", subscriptionShareCache.Count())

	cache.Manager.Register("subscription_shares", subscriptionShareCache)
	return nil
}

// GenerateToken 生成随机 token
func GenerateToken() (string, error) {
	bytes := make([]byte, 16) // 16字节 = 32个十六进制字符
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// IsTokenExists 检查 token 是否已存在
func IsTokenExists(token string, excludeID int) bool {
	shares := subscriptionShareCache.GetByIndex("token", token)
	for _, s := range shares {
		if s.ID != excludeID {
			return true
		}
	}
	return false
}

func normalizeOptionalTime(value *time.Time) *time.Time {
	if value == nil || value.IsZero() {
		return nil
	}
	return new(*value)
}

func (s *SubscriptionShare) normalizeOptionalFields() {
	if s.ExpireType != ExpireTypeDateTime {
		s.ExpireAt = nil
	} else {
		s.ExpireAt = normalizeOptionalTime(s.ExpireAt)
	}

	if s.AccessCount <= 0 {
		s.LastAccessAt = nil
	} else {
		s.LastAccessAt = normalizeOptionalTime(s.LastAccessAt)
	}
}

func compareNaturalStrings(left, right string) int {
	left = strings.ToLower(left)
	right = strings.ToLower(right)
	leftIndex, rightIndex := 0, 0

	for leftIndex < len(left) && rightIndex < len(right) {
		leftChar := left[leftIndex]
		rightChar := right[rightIndex]

		if isASCIIDigit(leftChar) && isASCIIDigit(rightChar) {
			leftStart, rightStart := leftIndex, rightIndex
			for leftIndex < len(left) && isASCIIDigit(left[leftIndex]) {
				leftIndex++
			}
			for rightIndex < len(right) && isASCIIDigit(right[rightIndex]) {
				rightIndex++
			}

			leftNumber := strings.TrimLeft(left[leftStart:leftIndex], "0")
			rightNumber := strings.TrimLeft(right[rightStart:rightIndex], "0")
			if leftNumber == "" {
				leftNumber = "0"
			}
			if rightNumber == "" {
				rightNumber = "0"
			}
			if len(leftNumber) != len(rightNumber) {
				return len(leftNumber) - len(rightNumber)
			}
			if leftNumber != rightNumber {
				if leftNumber < rightNumber {
					return -1
				}
				return 1
			}

			leftDigitLength := leftIndex - leftStart
			rightDigitLength := rightIndex - rightStart
			if leftDigitLength != rightDigitLength {
				return leftDigitLength - rightDigitLength
			}
			continue
		}

		if leftChar != rightChar {
			if leftChar < rightChar {
				return -1
			}
			return 1
		}
		leftIndex++
		rightIndex++
	}

	return len(left) - len(right)
}

func isASCIIDigit(value byte) bool {
	return value >= '0' && value <= '9'
}

// CreateDefaultShareForSubscription 为订阅创建默认分享链接
// 创建一个永不过期、启用状态的默认分享链接，标记为 IsLegacy=true
func CreateDefaultShareForSubscription(subscriptionID int) error {
	share := &SubscriptionShare{
		SubscriptionID: subscriptionID,
		Name:           "Default share link",
		ExpireType:     ExpireTypeNever,
		IsLegacy:       true,
		Enabled:        true,
	}
	return share.Add()
}

// Add 添加分享 (Write-Through)
func (s *SubscriptionShare) Add() error {
	s.normalizeOptionalFields()

	// 如果没有提供 token，自动生成
	if s.Token == "" {
		token, err := GenerateToken()
		if err != nil {
			return err
		}
		s.Token = token
	}

	// 检查 token 唯一性
	if IsTokenExists(s.Token, 0) {
		return fmt.Errorf("this token is already in use; choose another one")
	}

	err := database.DB.Create(s).Error
	if err != nil {
		return err
	}
	subscriptionShareCache.Set(s.ID, *s)
	return nil
}

// Update 更新分享 (Write-Through)
func (s *SubscriptionShare) Update() error {
	s.normalizeOptionalFields()

	// 检查 token 唯一性（排除自己）
	if IsTokenExists(s.Token, s.ID) {
		return fmt.Errorf("this token is already in use; choose another one")
	}

	err := database.DB.Model(s).Updates(map[string]any{
		"name":                        s.Name,
		"token":                       s.Token,
		"expire_type":                 s.ExpireType,
		"expire_days":                 s.ExpireDays,
		"expire_at":                   s.ExpireAt,
		"enabled":                     s.Enabled,
		"group_id":                    s.GroupID,
		"traffic_limit_bytes":         s.TrafficLimitBytes,
		"max_devices":                 s.MaxDevices,
		"report_interval_sessions":    s.ReportIntervalSessions,
		"report_interval_nodes":       s.ReportIntervalNodes,
		"report_interval_reliability": s.ReportIntervalReliability,
		"reported_traffic_bytes":      s.ReportedTrafficBytes,
		"last_usage_at":               s.LastUsageAt,
	}).Error
	if err != nil {
		return err
	}

	// 更新缓存
	var updated SubscriptionShare
	if err := database.DB.First(&updated, s.ID).Error; err == nil {
		subscriptionShareCache.Set(updated.ID, updated)
	}
	return nil
}

// Delete 删除分享 (Write-Through)
func (s *SubscriptionShare) Delete() error {
	err := database.DB.Delete(s).Error
	if err != nil {
		return err
	}
	subscriptionShareCache.Delete(s.ID)
	return nil
}

// IsManaged reports whether the link belongs to a share group and therefore
// obeys device/expiry/quota policy.
func (s *SubscriptionShare) IsManaged() bool {
	return s != nil && s.GroupID != nil
}

// IsQuotaExhausted reports whether the reported traffic reached the snapshot
// limit. A limit of zero means unlimited.
func (s *SubscriptionShare) IsQuotaExhausted() bool {
	return s != nil && s.TrafficLimitBytes > 0 && s.ReportedTrafficBytes >= s.TrafficLimitBytes
}

// ApplyGroupSnapshot copies the group settings that a new subscription link
// inherits. Later edits to the link are independent from the group.
func (s *SubscriptionShare) ApplyGroupSnapshot(group *ShareGroup) {
	if group == nil {
		return
	}
	groupID := group.ID
	s.GroupID = &groupID
	s.TrafficLimitBytes = group.TrafficLimitBytes
	s.MaxDevices = group.MaxDevices
	s.ExpireType = group.ExpireType
	s.ExpireDays = group.ExpireDays
	s.ExpireAt = normalizeOptionalTime(group.ExpireAt)
	s.ReportIntervalSessions = group.ReportIntervalSessions
	s.ReportIntervalNodes = group.ReportIntervalNodes
	s.ReportIntervalReliability = group.ReportIntervalReliability
}

// ReportInterval returns the snapshot interval for a telemetry channel.
func (s *SubscriptionShare) ReportInterval(channel string) int {
	if s == nil {
		return 0
	}
	switch channel {
	case ShareTelemetryChannelSessions:
		return s.ReportIntervalSessions
	case ShareTelemetryChannelNodes:
		return s.ReportIntervalNodes
	case ShareTelemetryChannelReliability:
		return s.ReportIntervalReliability
	default:
		return 0
	}
}

// GetSharesByGroupID returns links that belong to a group.
func GetSharesByGroupID(groupID int) []SubscriptionShare {
	return subscriptionShareCache.GetByIndex("groupID", strconv.Itoa(groupID))
}

// Find 根据 ID 查找
func (s *SubscriptionShare) Find() error {
	if cached, ok := subscriptionShareCache.Get(s.ID); ok {
		*s = cached
		return nil
	}
	return database.DB.First(s, s.ID).Error
}

// GetByToken 根据 token 查找分享
func GetSubscriptionShareByToken(token string) (*SubscriptionShare, error) {
	shares := subscriptionShareCache.GetByIndex("token", token)
	if len(shares) > 0 {
		return &shares[0], nil
	}
	// 缓存未命中，从数据库查
	var share SubscriptionShare
	if err := database.DB.Where("token = ?", token).First(&share).Error; err != nil {
		return nil, err
	}
	subscriptionShareCache.Set(share.ID, share)
	return &share, nil
}

// GetSharesBySubscriptionID 获取订阅的所有分享
func GetSharesBySubscriptionID(subID int, keyword ...string) []SubscriptionShare {
	shares := subscriptionShareCache.GetByIndex("subscriptionID", strconv.Itoa(subID))

	// 如果提供了搜索关键词，进行过滤
	// 规则：名称使用模糊搜索，token使用精确匹配
	if len(keyword) > 0 && keyword[0] != "" {
		kw := keyword[0]
		kwLower := strings.ToLower(kw)
		filtered := make([]SubscriptionShare, 0)
		for _, share := range shares {
			// 名称：模糊搜索（不区分大小写）
			nameMatch := strings.Contains(strings.ToLower(share.Name), kwLower)
			// Token：精确匹配（区分大小写）
			tokenMatch := share.Token == kw

			if nameMatch || tokenMatch {
				filtered = append(filtered, share)
			}
		}
		return filtered
	}

	return shares
}

// GetShareIDsByIP 根据IP地址获取访问过的分享ID列表
func GetShareIDsByIP(ip string) []int {
	logs := subLogsCache.GetAll()
	shareIDMap := make(map[int]bool)

	for _, log := range logs {
		if log.IP == ip && log.ShareID > 0 {
			shareIDMap[log.ShareID] = true
		}
	}

	shareIDs := make([]int, 0, len(shareIDMap))
	for id := range shareIDMap {
		shareIDs = append(shareIDs, id)
	}
	return shareIDs
}

// GetSharesBySubscriptionIDPaginated 获取订阅的分享列表（分页，支持搜索、IP筛选、排序）
func GetSharesBySubscriptionIDPaginated(subID, page, pageSize int, keyword, ipFilter, sortBy, sortOrder string) ([]SubscriptionShare, int, error) {
	// 先从缓存获取该订阅的所有分享
	allShares := subscriptionShareCache.GetByIndex("subscriptionID", strconv.Itoa(subID))
	return paginateSubscriptionShares(allShares, page, pageSize, keyword, ipFilter, sortBy, sortOrder)
}

// GetSharesByGroupIDPaginated 获取某个分享分组下的链接（分页，支持搜索、IP筛选、排序）。
// 与订阅维度的列表保持一致的分页与排序语义，供分组页面的链接管理使用。
func GetSharesByGroupIDPaginated(groupID, page, pageSize int, keyword, ipFilter, sortBy, sortOrder string) ([]SubscriptionShare, int, error) {
	allShares := GetSharesByGroupID(groupID)
	return paginateSubscriptionShares(allShares, page, pageSize, keyword, ipFilter, sortBy, sortOrder)
}

func paginateSubscriptionShares(allShares []SubscriptionShare, page, pageSize int, keyword, ipFilter, sortBy, sortOrder string) ([]SubscriptionShare, int, error) {
	// IP过滤优先级最高
	if ipFilter != "" {
		shareIDs := GetShareIDsByIP(ipFilter)
		shareIDSet := make(map[int]bool)
		for _, id := range shareIDs {
			shareIDSet[id] = true
		}

		filtered := make([]SubscriptionShare, 0)
		for _, share := range allShares {
			if shareIDSet[share.ID] {
				filtered = append(filtered, share)
			}
		}
		allShares = filtered
	}

	// 如果提供了搜索关键词，进行过滤
	// 规则：名称使用模糊搜索，token使用精确匹配
	if keyword != "" {
		kwLower := strings.ToLower(keyword)
		filtered := make([]SubscriptionShare, 0)
		for _, share := range allShares {
			// 名称：模糊搜索（不区分大小写）
			nameMatch := strings.Contains(strings.ToLower(share.Name), kwLower)
			// Token：精确匹配（区分大小写）
			tokenMatch := share.Token == keyword

			if nameMatch || tokenMatch {
				filtered = append(filtered, share)
			}
		}
		allShares = filtered
	}

	// 排序
	if sortBy == "access_count" || sortBy == "name" {
		sort.SliceStable(allShares, func(i, j int) bool {
			if sortBy == "name" {
				comparison := compareNaturalStrings(allShares[i].Name, allShares[j].Name)
				if comparison == 0 {
					comparison = allShares[i].ID - allShares[j].ID
				}
				if sortOrder == "desc" {
					return comparison > 0
				}
				return comparison < 0
			}

			if allShares[i].AccessCount == allShares[j].AccessCount {
				return allShares[i].ID < allShares[j].ID
			}
			if sortOrder == "asc" {
				return allShares[i].AccessCount < allShares[j].AccessCount
			}
			return allShares[i].AccessCount > allShares[j].AccessCount
		})
	}

	total := len(allShares)

	// 计算分页
	offset := (page - 1) * pageSize
	if offset >= total {
		return []SubscriptionShare{}, total, nil
	}

	end := offset + pageSize
	if end > total {
		end = total
	}

	return allShares[offset:end], total, nil
}

// GetDefaultShareForSubscription 获取订阅的默认分享链接
// 优先返回老的迁移链接（IsLegacy=true），否则返回第一个启用的分享
func GetDefaultShareForSubscription(subID int) (*SubscriptionShare, error) {
	shares := GetSharesBySubscriptionID(subID)
	if len(shares) == 0 {
		return nil, fmt.Errorf("this subscription has no share links")
	}

	// 优先找 legacy 链接
	for _, s := range shares {
		if s.IsLegacy && s.Enabled {
			return &s, nil
		}
	}

	// 否则返回第一个启用的
	for _, s := range shares {
		if s.Enabled {
			return &s, nil
		}
	}

	return nil, fmt.Errorf("this subscription has no available share links")
}

// IsExpired 检查分享是否已过期
func (s *SubscriptionShare) IsExpired() bool {
	if !s.Enabled {
		return true
	}

	switch s.ExpireType {
	case ExpireTypeNever:
		return false
	case ExpireTypeDays:
		if s.ExpireDays <= 0 {
			return false
		}
		expireTime := s.CreatedAt.AddDate(0, 0, s.ExpireDays)
		return time.Now().After(expireTime)
	case ExpireTypeDateTime:
		if s.ExpireAt == nil || s.ExpireAt.IsZero() {
			return false
		}
		return time.Now().After(*s.ExpireAt)
	default:
		return false
	}
}

// EffectiveExpireUnix returns the link expiry as a Unix timestamp. Zero means
// the link never expires or has no configured expiry; the value matches the
// rule used by IsExpired so header/status consumers agree with enforcement.
func (s *SubscriptionShare) EffectiveExpireUnix() int64 {
	if s == nil {
		return 0
	}
	switch s.ExpireType {
	case ExpireTypeDays:
		if s.ExpireDays > 0 {
			return s.CreatedAt.AddDate(0, 0, s.ExpireDays).Unix()
		}
	case ExpireTypeDateTime:
		if s.ExpireAt != nil && !s.ExpireAt.IsZero() {
			return s.ExpireAt.Unix()
		}
	}
	return 0
}

// accessRecordWG 跟踪在飞的异步访问统计写入，便于测试拆库前与服务优雅关闭时等待其完成。
var accessRecordWG sync.WaitGroup

// WaitForPendingAccessRecords 阻塞直到所有异步访问统计写入完成。
// 测试在重置/关闭 database.DB 前调用；服务优雅关闭时亦可调用以避免丢失访问计数。
func WaitForPendingAccessRecords() {
	accessRecordWG.Wait()
}

// RecordAccess 记录一次访问，并使用数据库原子自增避免并发访问丢失计数。
func (s *SubscriptionShare) RecordAccess() {
	if s == nil || s.ID <= 0 {
		return
	}

	// 只读取一次全局句柄：异步 goroutine 可能与测试/关闭流程并发，
	// 若全局 database.DB 中途被重置为 nil，直接跳过，避免空指针解引用。
	db := database.DB
	if db == nil {
		return
	}

	if err := db.Model(&SubscriptionShare{}).
		Where("id = ?", s.ID).
		Updates(map[string]any{
			"access_count":   gorm.Expr("access_count + ?", 1),
			"last_access_at": new(time.Now()),
		}).Error; err != nil {
		utils.Warn("Failed to record subscription share access: %v", err)
		return
	}

	var updated SubscriptionShare
	if err := db.First(&updated, s.ID).Error; err != nil {
		utils.Warn("Failed to refresh subscription share access cache: %v", err)
		return
	}
	*s = updated
	subscriptionShareCache.Set(updated.ID, updated)
}

// RecordAccessAsync 异步记录一次访问，避免订阅拉取热路径等待数据库写入。
func (s *SubscriptionShare) RecordAccessAsync() {
	if s == nil || s.ID <= 0 {
		return
	}

	shareID := s.ID
	accessRecordWG.Add(1)
	go func() {
		defer accessRecordWG.Done()
		share := SubscriptionShare{ID: shareID}
		share.RecordAccess()
	}()
}

// AddReportedTraffic adds a client-reported traffic delta to the link and
// refreshes the cached row. Callers must reject negative values; the database
// increment itself is atomic so concurrent reports cannot lose bytes.
func (s *SubscriptionShare) AddReportedTraffic(delta int64) error {
	if s == nil || s.ID <= 0 {
		return fmt.Errorf("share link is required")
	}
	if delta < 0 {
		return fmt.Errorf("traffic delta must be non-negative")
	}
	db := database.DB
	if db == nil {
		return fmt.Errorf("database is not available")
	}

	now := time.Now()
	updates := map[string]any{"last_usage_at": now}
	if delta > 0 {
		updates["reported_traffic_bytes"] = gorm.Expr("reported_traffic_bytes + ?", delta)
	}
	if err := db.Model(&SubscriptionShare{}).Where("id = ?", s.ID).Updates(updates).Error; err != nil {
		return err
	}

	var updated SubscriptionShare
	if err := db.First(&updated, s.ID).Error; err != nil {
		return err
	}
	*s = updated
	subscriptionShareCache.Set(updated.ID, updated)
	return nil
}

// List 获取所有分享列表
func (s *SubscriptionShare) List() ([]SubscriptionShare, error) {
	return subscriptionShareCache.GetAll(), nil
}
