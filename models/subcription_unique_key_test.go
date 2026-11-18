package models

import (
	"regexp"
	"strings"
	"testing"

	"sublink/database"
)

// uniqueKeyPattern 校验稳定唯一键的形态：<slug>-<8 位十六进制>。
var uniqueKeyPattern = regexp.MustCompile(`^[a-z0-9-]+-[0-9a-f]{8}$`)

func TestGenerateSubcriptionUniqueKeyUsesSlugAndRandomSuffix(t *testing.T) {
	key := GenerateSubcriptionUniqueKey("My Pool 01")
	if !uniqueKeyPattern.MatchString(key) {
		t.Fatalf("unique key %q does not match slug-suffix pattern", key)
	}
	if !strings.HasPrefix(key, "my-pool-01-") {
		t.Fatalf("expected slug prefix my-pool-01-, got %q", key)
	}

	nonASCII := GenerateSubcriptionUniqueKey("节点池")
	if !uniqueKeyPattern.MatchString(nonASCII) || !strings.HasPrefix(nonASCII, "pool-") {
		t.Fatalf("expected pool fallback slug, got %q", nonASCII)
	}

	longName := GenerateSubcriptionUniqueKey(strings.Repeat("a", 120))
	if len(longName) > 64 {
		t.Fatalf("unique key exceeds column size: %d chars", len(longName))
	}

	seen := make(map[string]bool)
	for i := 0; i < 200; i++ {
		candidate := GenerateSubcriptionUniqueKey("duplicate-name")
		if seen[candidate] {
			t.Fatalf("generated duplicate unique key %q", candidate)
		}
		seen[candidate] = true
	}
}

func TestSubcriptionUniqueKeySurvivesRename(t *testing.T) {
	setupSubcriptionCopyTestDB(t)

	sub := &Subcription{Name: "pool-original", RefreshUsageOnRequest: true}
	if err := sub.Add(); err != nil {
		t.Fatalf("add subscription: %v", err)
	}
	if sub.UniqueKey == "" {
		t.Fatal("expected a unique key to be generated on add")
	}
	originalKey := sub.UniqueKey

	sub.Name = "pool-renamed"
	// 即使调用方误传了别的唯一键，Update 也不应改写它。
	sub.UniqueKey = "tampered-key"
	if err := sub.Update(); err != nil {
		t.Fatalf("rename subscription: %v", err)
	}

	var stored Subcription
	if err := database.DB.First(&stored, sub.ID).Error; err != nil {
		t.Fatalf("reload subscription: %v", err)
	}
	if stored.Name != "pool-renamed" {
		t.Fatalf("stored name = %q, want pool-renamed", stored.Name)
	}
	if stored.UniqueKey != originalKey {
		t.Fatalf("unique key changed on rename: got %q, want %q", stored.UniqueKey, originalKey)
	}

	found := Subcription{UniqueKey: originalKey}
	if err := found.Find(); err != nil {
		t.Fatalf("find by unique key: %v", err)
	}
	if found.ID != sub.ID || found.Name != "pool-renamed" {
		t.Fatalf("find by unique key returned id=%d name=%q, want id=%d name=pool-renamed", found.ID, found.Name, sub.ID)
	}
}

func TestSubcriptionFindPrefersUniqueKeyOverName(t *testing.T) {
	setupSubcriptionCopyTestDB(t)

	primary := &Subcription{Name: "alpha", RefreshUsageOnRequest: true}
	if err := primary.Add(); err != nil {
		t.Fatalf("add primary subscription: %v", err)
	}

	// 另一个节点池的显示名恰好与 primary 的唯一键相同，用于验证解析优先级。
	shadow := &Subcription{Name: primary.UniqueKey, RefreshUsageOnRequest: true}
	if err := shadow.Add(); err != nil {
		t.Fatalf("add shadow subscription: %v", err)
	}

	byKey := Subcription{UniqueKey: primary.UniqueKey}
	if err := byKey.Find(); err != nil {
		t.Fatalf("find by unique key: %v", err)
	}
	if byKey.ID != primary.ID {
		t.Fatalf("find by unique key returned id %d, want %d", byKey.ID, primary.ID)
	}

	byID := Subcription{ID: shadow.ID, UniqueKey: primary.UniqueKey}
	if err := byID.Find(); err != nil {
		t.Fatalf("find by id: %v", err)
	}
	if byID.Name != primary.UniqueKey {
		t.Fatalf("find by id returned name %q, want %q", byID.Name, primary.UniqueKey)
	}

	// 名称仍然兼容，用于历史数据与外部脚本。
	byName := Subcription{Name: "alpha"}
	if err := byName.Find(); err != nil {
		t.Fatalf("find by name: %v", err)
	}
	if byName.ID != primary.ID {
		t.Fatalf("find by name returned id %d, want %d", byName.ID, primary.ID)
	}
}

func TestBackfillSubcriptionUniqueKeysIsIdempotent(t *testing.T) {
	setupSubcriptionCopyTestDB(t)

	legacyNames := []string{"legacy-one", "legacy-two"}
	for _, name := range legacyNames {
		// 直接用 SQL 写入，模拟唯一键列为空的存量数据。
		if err := database.DB.Exec("INSERT INTO subcriptions (name) VALUES (?)", name).Error; err != nil {
			t.Fatalf("insert legacy subscription %q: %v", name, err)
		}
	}

	if err := BackfillSubcriptionUniqueKeys(); err != nil {
		t.Fatalf("backfill unique keys: %v", err)
	}

	var subs []Subcription
	if err := database.DB.Where("name IN ?", legacyNames).Order("id ASC").Find(&subs).Error; err != nil {
		t.Fatalf("load subscriptions: %v", err)
	}
	if len(subs) != len(legacyNames) {
		t.Fatalf("got %d subscriptions, want %d", len(subs), len(legacyNames))
	}

	before := make(map[int]string, len(subs))
	seen := make(map[string]bool, len(subs))
	for _, sub := range subs {
		if sub.UniqueKey == "" {
			t.Fatalf("subscription %q still has an empty unique key", sub.Name)
		}
		if seen[sub.UniqueKey] {
			t.Fatalf("duplicate unique key %q", sub.UniqueKey)
		}
		seen[sub.UniqueKey] = true
		before[sub.ID] = sub.UniqueKey
	}

	if err := BackfillSubcriptionUniqueKeys(); err != nil {
		t.Fatalf("second backfill pass: %v", err)
	}
	var reloaded []Subcription
	if err := database.DB.Where("name IN ?", legacyNames).Find(&reloaded).Error; err != nil {
		t.Fatalf("reload subscriptions: %v", err)
	}
	for _, sub := range reloaded {
		if before[sub.ID] != sub.UniqueKey {
			t.Fatalf("backfill rewrote unique key for %q: %q -> %q", sub.Name, before[sub.ID], sub.UniqueKey)
		}
	}
}
