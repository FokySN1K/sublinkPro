package models

import (
	"fmt"
	"strings"
	"time"

	"sublink/database"
)

// ShareClient is a lightweight "person" entity: one client may own several
// subscription links across different node pools. Links without a client are
// never hidden; the UI shows them as unassigned rows.
type ShareClient struct {
	ID        int       `gorm:"primaryKey" json:"id"`
	Name      string    `gorm:"size:100;not null" json:"name"`
	Note      string    `gorm:"size:500" json:"note"`
	Enabled   bool      `gorm:"default:true" json:"enabled"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

// ListShareClients returns every client ordered by name.
func ListShareClients() ([]ShareClient, error) {
	var clients []ShareClient
	err := database.DB.Order("name ASC, id ASC").Find(&clients).Error
	return clients, err
}

// GetShareClientByID loads one client.
func GetShareClientByID(id int) (*ShareClient, error) {
	var client ShareClient
	if err := database.DB.First(&client, id).Error; err != nil {
		return nil, err
	}
	return &client, nil
}

// Add creates a client.
func (c *ShareClient) Add() error {
	c.Name = NormalizeShareDeviceValue(c.Name, 100)
	c.Note = NormalizeShareDeviceValue(c.Note, 500)
	if c.Name == "" {
		return fmt.Errorf("client name is required")
	}
	return database.DB.Create(c).Error
}

// Update persists the editable client fields.
func (c *ShareClient) Update() error {
	c.Name = NormalizeShareDeviceValue(c.Name, 100)
	c.Note = NormalizeShareDeviceValue(c.Note, 500)
	if c.ID <= 0 {
		return fmt.Errorf("client id is required")
	}
	if c.Name == "" {
		return fmt.Errorf("client name is required")
	}
	return database.DB.Model(&ShareClient{}).Where("id = ?", c.ID).Updates(map[string]any{
		"name":       c.Name,
		"note":       c.Note,
		"enabled":    c.Enabled,
		"updated_at": time.Now(),
	}).Error
}

// Delete removes a client and unassigns its links (client_id -> NULL).
func (c *ShareClient) Delete() error {
	if c.ID <= 0 {
		return fmt.Errorf("client id is required")
	}
	if err := database.DB.Model(&SubscriptionShare{}).Where("client_id = ?", c.ID).
		Update("client_id", nil).Error; err != nil {
		return err
	}
	if err := database.DB.Delete(&ShareClient{}, c.ID).Error; err != nil {
		return err
	}
	refreshSubscriptionShareCache()
	return nil
}

// AssignShareClients attaches or detaches links from a client. A nil clientID
// detaches the links.
func AssignShareClients(shareIDs []int, clientID *int) error {
	ids := uniquePositiveShareIDs(shareIDs)
	if len(ids) == 0 {
		return nil
	}
	var value any
	if clientID != nil {
		if *clientID > 0 {
			if _, err := GetShareClientByID(*clientID); err != nil {
				return fmt.Errorf("client not found")
			}
			value = *clientID
		}
	}
	if err := database.DB.Model(&SubscriptionShare{}).Where("id IN ?", ids).
		Update("client_id", value).Error; err != nil {
		return err
	}
	refreshSubscriptionShareCache()
	return nil
}

// ClientIDsOfShares maps share ids to their client ids.
func ClientIDsOfShares(shareIDs []int) map[int]int {
	result := make(map[int]int, len(shareIDs))
	ids := uniquePositiveShareIDs(shareIDs)
	if len(ids) == 0 {
		return result
	}
	type row struct {
		ID       int
		ClientID *int
	}
	var rows []row
	err := database.DB.Model(&SubscriptionShare{}).
		Select("id, client_id").
		Where("id IN ?", ids).
		Scan(&rows).Error
	if err != nil {
		return result
	}
	for _, entry := range rows {
		if entry.ClientID != nil && *entry.ClientID > 0 {
			result[entry.ID] = *entry.ClientID
		}
	}
	return result
}

// ClientNameIndex maps client ids to display names.
func ClientNameIndex() map[int]string {
	clients, err := ListShareClients()
	names := make(map[int]string, len(clients))
	if err != nil {
		return names
	}
	for _, client := range clients {
		names[client.ID] = client.Name
	}
	return names
}

// CountShareCardsByClient maps client ids to the number of links they own.
func CountShareCardsByClient() (map[int]int, error) {
	type row struct {
		ClientID int
		Total    int
	}
	var rows []row
	err := database.DB.Model(&SubscriptionShare{}).
		Select("client_id, COUNT(*) AS total").
		Where("client_id IS NOT NULL").
		Group("client_id").
		Scan(&rows).Error
	counts := make(map[int]int, len(rows))
	if err != nil {
		return counts, err
	}
	for _, entry := range rows {
		counts[entry.ClientID] = entry.Total
	}
	return counts, nil
}

func uniquePositiveShareIDs(ids []int) []int {
	unique := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	return unique
}

// refreshSubscriptionShareCache reloads links after bulk column updates that
// bypass SubscriptionShare.Update (client assignments).
func refreshSubscriptionShareCache() {
	var shares []SubscriptionShare
	if err := database.DB.Find(&shares).Error; err != nil {
		return
	}
	subscriptionShareCache.LoadAll(shares)
}

// SearchShareClients filters clients by a keyword; empty keyword returns all.
func SearchShareClients(keyword string) ([]ShareClient, error) {
	clients, err := ListShareClients()
	if err != nil {
		return nil, err
	}
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return clients, nil
	}
	filtered := make([]ShareClient, 0, len(clients))
	for _, client := range clients {
		if strings.Contains(strings.ToLower(client.Name), keyword) ||
			strings.Contains(strings.ToLower(client.Note), keyword) {
			filtered = append(filtered, client)
		}
	}
	return filtered, nil
}
