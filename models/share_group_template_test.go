package models

import (
	"strconv"
	"strings"
	"testing"

	"sublink/cache"
	"sublink/database"
	"sublink/internal/testutil"
)

func setupShareGroupTemplateTestDB(t *testing.T) {
	t.Helper()

	oldDB := database.DB
	oldDialect := database.Dialect
	oldInitialized := database.IsInitialized
	oldSubcriptionCache := subcriptionCache
	oldShareGroupCache := shareGroupCache
	oldTemplateCache := templateCache

	db := testutil.OpenMemoryDB(t, "share_group_template_test")
	if err := db.AutoMigrate(&Subcription{}, &Template{}, &ShareGroup{}); err != nil {
		t.Fatalf("auto migrate share group tables: %v", err)
	}

	database.DB = db
	database.Dialect = database.DialectSQLite
	database.IsInitialized = true

	resetSubcriptionCacheForTest()
	shareGroupCache = cache.NewMapCache(func(g ShareGroup) int { return g.ID })
	shareGroupCache.AddIndex("subscriptionID", func(g ShareGroup) string { return strconv.Itoa(g.SubscriptionID) })
	templateCache = cache.NewMapCache(func(tmpl Template) int { return tmpl.ID })
	templateCache.AddIndex("name", func(tmpl Template) string { return tmpl.Name })

	t.Cleanup(func() {
		database.DB = oldDB
		database.Dialect = oldDialect
		database.IsInitialized = oldInitialized
		subcriptionCache = oldSubcriptionCache
		shareGroupCache = oldShareGroupCache
		templateCache = oldTemplateCache
		testutil.CloseDB(t, db)
	})
}

func createShareGroupTestTemplate(t *testing.T, name, category string) {
	t.Helper()
	tmpl := &Template{Name: name, Category: category}
	if err := tmpl.Add(); err != nil {
		t.Fatalf("add template %q: %v", name, err)
	}
}

func createShareGroupTestPool(t *testing.T, name string) *Subcription {
	t.Helper()
	sub := &Subcription{Name: name, RefreshUsageOnRequest: true}
	if err := sub.Add(); err != nil {
		t.Fatalf("add node pool %q: %v", name, err)
	}
	return sub
}

func TestShareGroupPersistsTemplateFiles(t *testing.T) {
	setupShareGroupTemplateTestDB(t)
	createShareGroupTestTemplate(t, "group-clash.yaml", "clash")
	createShareGroupTestTemplate(t, "group-surge.conf", "surge")
	pool := createShareGroupTestPool(t, "pool-with-templates")

	group := &ShareGroup{
		Name:           "template group",
		SubscriptionID: pool.ID,
		Enabled:        true,
		ClashTemplate:  "  group-clash.yaml  ",
		SurgeTemplate:  " group-surge.conf ",
	}
	if err := group.Add(); err != nil {
		t.Fatalf("add share group: %v", err)
	}

	var stored ShareGroup
	if err := database.DB.First(&stored, group.ID).Error; err != nil {
		t.Fatalf("reload share group: %v", err)
	}
	if stored.ClashTemplate != "group-clash.yaml" || stored.SurgeTemplate != "group-surge.conf" {
		t.Fatalf("stored templates = (%q, %q), want trimmed file names", stored.ClashTemplate, stored.SurgeTemplate)
	}

	group.ClashTemplate = ""
	if err := group.Update(); err != nil {
		t.Fatalf("clear clash template: %v", err)
	}
	if err := database.DB.First(&stored, group.ID).Error; err != nil {
		t.Fatalf("reload updated share group: %v", err)
	}
	if stored.ClashTemplate != "" {
		t.Fatalf("expected cleared clash template to inherit the node pool, got %q", stored.ClashTemplate)
	}
	if stored.SurgeTemplate != "group-surge.conf" {
		t.Fatalf("surge template = %q, want group-surge.conf", stored.SurgeTemplate)
	}
}

func TestShareGroupValidatesTemplateFiles(t *testing.T) {
	setupShareGroupTemplateTestDB(t)
	createShareGroupTestTemplate(t, "clash-only.yaml", "clash")
	createShareGroupTestTemplate(t, "surge-only.conf", "surge")
	pool := createShareGroupTestPool(t, "pool-validation")

	tests := []struct {
		name      string
		clash     string
		surge     string
		wantError string
	}{
		{name: "inherit both", clash: "", surge: ""},
		{name: "valid files", clash: "clash-only.yaml", surge: "surge-only.conf"},
		{name: "unknown clash file", clash: "missing.yaml", wantError: "clash template not found"},
		{name: "unknown surge file", surge: "missing.conf", wantError: "surge template not found"},
		{name: "surge file as clash", clash: "surge-only.conf", wantError: "is not a clash template"},
		{name: "clash file as surge", surge: "clash-only.yaml", wantError: "is not a surge template"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			group := &ShareGroup{
				Name:           "group-" + strings.ReplaceAll(tt.name, " ", "-"),
				SubscriptionID: pool.ID,
				Enabled:        true,
				ClashTemplate:  tt.clash,
				SurgeTemplate:  tt.surge,
			}
			err := group.Add()
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("add share group: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantError)
			}
		})
	}
}

func TestShareGroupRejectsUnknownNodePool(t *testing.T) {
	setupShareGroupTemplateTestDB(t)

	group := &ShareGroup{Name: "orphan group", SubscriptionID: 424242, Enabled: true}
	err := group.Add()
	if err == nil || !strings.Contains(err.Error(), "node pool not found") {
		t.Fatalf("error = %v, want node pool not found", err)
	}
}
