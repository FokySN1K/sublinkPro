package models

import (
	"database/sql"
	"errors"
	"strings"
	"sublink/database"
	"sublink/internal/testutil"
	"testing"
	"time"
)

func TestValidateAccessKeyDescription(t *testing.T) {
	tests := []struct {
		name        string
		description string
		wantErr     bool
	}{
		{"empty description is allowed by the model", "", false},
		{"ascii at the limit", strings.Repeat("a", AccessKeyDescriptionMaxLength), false},
		{"ascii over the limit", strings.Repeat("a", AccessKeyDescriptionMaxLength+1), true},
		{"multibyte counted by runes", strings.Repeat("键", AccessKeyDescriptionMaxLength), false},
		{"multibyte over the limit", strings.Repeat("键", AccessKeyDescriptionMaxLength+1), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAccessKeyDescription(tt.description)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateAccessKeyDescription with %d runes returned %v, wantErr %v", len([]rune(tt.description)), err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeAccessKeyDescription(t *testing.T) {
	if _, err := NormalizeAccessKeyDescription("   "); !errors.Is(err, ErrAccessKeyDescriptionEmpty) {
		t.Fatalf("NormalizeAccessKeyDescription with blank input returned %v, want ErrAccessKeyDescriptionEmpty", err)
	}

	if _, err := NormalizeAccessKeyDescription(strings.Repeat("a", AccessKeyDescriptionMaxLength+1)); !errors.Is(err, ErrAccessKeyDescriptionTooLong) {
		t.Fatalf("NormalizeAccessKeyDescription over the limit returned %v, want ErrAccessKeyDescriptionTooLong", err)
	}

	got, err := NormalizeAccessKeyDescription("  AI skill  ")
	if err != nil {
		t.Fatalf("NormalizeAccessKeyDescription returned error: %v", err)
	}
	if got != "AI skill" {
		t.Fatalf("NormalizeAccessKeyDescription returned %q, want %q", got, "AI skill")
	}
}

func TestAccessKeyUpdateAndTouchLastUsed(t *testing.T) {
	oldDB := database.DB
	db := testutil.OpenMemoryDB(t, "accesskey")
	database.DB = db
	t.Cleanup(func() {
		testutil.CloseDB(t, db)
		database.DB = oldDB
	})

	if err := db.AutoMigrate(&AccessKey{}); err != nil {
		t.Fatalf("migrate access key table: %v", err)
	}

	accessKey := &AccessKey{
		UserID:        1,
		Username:      "admin",
		AccessKeyHash: "test-hash",
		Description:   "old name",
	}
	if err := accessKey.Generate(); err != nil {
		t.Fatalf("generate access key: %v", err)
	}

	expiredAt := time.Now().Add(24 * time.Hour).Truncate(time.Second)
	if err := accessKey.Update("renamed key", &expiredAt); err != nil {
		t.Fatalf("update access key: %v", err)
	}

	var stored AccessKey
	if err := db.First(&stored, accessKey.ID).Error; err != nil {
		t.Fatalf("reload access key: %v", err)
	}
	if stored.Description != "renamed key" {
		t.Fatalf("stored description = %q, want %q", stored.Description, "renamed key")
	}
	var storedExpiry sql.NullTime
	if err := db.Raw("SELECT expired_at FROM access_keys WHERE id = ?", accessKey.ID).Scan(&storedExpiry).Error; err != nil {
		t.Fatalf("query stored expiry: %v", err)
	}
	if !storedExpiry.Valid || !storedExpiry.Time.Equal(expiredAt) {
		t.Fatalf("stored expiry = %v, want %v", storedExpiry, expiredAt)
	}
	if cached, ok := accessKeyCache.Get(accessKey.ID); !ok || cached.Description != "renamed key" {
		t.Fatalf("cache was not refreshed after update: %+v", cached)
	}

	// 清空过期时间同样需要写回到数据库
	if err := accessKey.Update("renamed key", nil); err != nil {
		t.Fatalf("clear access key expiry: %v", err)
	}
	storedExpiry = sql.NullTime{}
	if err := db.Raw("SELECT expired_at FROM access_keys WHERE id = ?", accessKey.ID).Scan(&storedExpiry).Error; err != nil {
		t.Fatalf("query cleared expiry: %v", err)
	}
	if storedExpiry.Valid {
		t.Fatalf("stored expiry = %v, want NULL", storedExpiry.Time)
	}

	accessKey.TouchLastUsed("203.0.113.7", time.Now())
	deadline := time.Now().Add(2 * time.Second)
	for {
		if err := db.First(&stored, accessKey.ID).Error; err != nil {
			t.Fatalf("reload access key: %v", err)
		}
		if stored.LastUsedIP == "203.0.113.7" && stored.LastUsedAt != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("last used info was not persisted: %+v", stored)
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 节流期内不应重复写库
	accessKey.TouchLastUsed("198.51.100.9", time.Now())
	time.Sleep(50 * time.Millisecond)
	if err := db.First(&stored, accessKey.ID).Error; err != nil {
		t.Fatalf("reload access key: %v", err)
	}
	if stored.LastUsedIP != "203.0.113.7" {
		t.Fatalf("throttled write changed last used ip to %q", stored.LastUsedIP)
	}
}
