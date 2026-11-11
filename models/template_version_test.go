package models

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"sublink/database"
	"sublink/internal/testutil"

	"gorm.io/gorm"
)

func setupTemplateVersionTestDB(t *testing.T) {
	t.Helper()

	oldDB := database.DB
	oldDialect := database.Dialect
	oldInitialized := database.IsInitialized

	db := testutil.OpenMemoryDB(t, "template_version_test")
	if err := db.AutoMigrate(&Template{}, &TemplateVersion{}); err != nil {
		t.Fatalf("auto migrate template versions: %v", err)
	}

	database.DB = db
	database.Dialect = database.DialectSQLite
	database.IsInitialized = false

	t.Cleanup(func() {
		database.DB = oldDB
		database.Dialect = oldDialect
		database.IsInitialized = oldInitialized
		testutil.CloseDB(t, db)
	})
}

func createTemplateRecordForVersionTest(t *testing.T, name string) *Template {
	t.Helper()
	tmpl := &Template{Name: name, Category: InferTemplateCategory(name)}
	if err := database.DB.Create(tmpl).Error; err != nil {
		t.Fatalf("create template record: %v", err)
	}
	return tmpl
}

func TestAppendTemplateVersionIncrementsAndStoresContent(t *testing.T) {
	setupTemplateVersionTestDB(t)
	templateDir := t.TempDir()
	tmpl := createTemplateRecordForVersionTest(t, "clash.yaml")

	first, created, err := AppendTemplateVersion(templateDir, tmpl.ID, []byte("rules: []\n"), TemplateVersionSourceAdd, "first", "admin")
	if err != nil {
		t.Fatalf("append first version: %v", err)
	}
	if !created || first.Version != 1 || first.Source != TemplateVersionSourceAdd {
		t.Fatalf("unexpected first version: %#v created=%v", first, created)
	}

	second, created, err := AppendTemplateVersion(templateDir, tmpl.ID, []byte("rules:\n  - MATCH,DIRECT\n"), TemplateVersionSourceSave, "", "admin")
	if err != nil {
		t.Fatalf("append second version: %v", err)
	}
	if !created || second.Version != 2 {
		t.Fatalf("unexpected second version: %#v created=%v", second, created)
	}

	content, err := ReadTemplateVersionContent(templateDir, first)
	if err != nil {
		t.Fatalf("read first version content: %v", err)
	}
	if string(content) != "rules: []\n" {
		t.Fatalf("first version content = %q", string(content))
	}

	latest, err := LatestTemplateVersion(tmpl.ID)
	if err != nil {
		t.Fatalf("latest version: %v", err)
	}
	if latest.Version != 2 || latest.ContentHash != BuildTemplateContentHash([]byte("rules:\n  - MATCH,DIRECT\n")) {
		t.Fatalf("unexpected latest version: %#v", latest)
	}
}

func TestAppendTemplateVersionSkipsIdenticalContent(t *testing.T) {
	setupTemplateVersionTestDB(t)
	templateDir := t.TempDir()
	tmpl := createTemplateRecordForVersionTest(t, "clash.yaml")

	first, _, err := AppendTemplateVersion(templateDir, tmpl.ID, []byte("same\n"), TemplateVersionSourceAdd, "", "")
	if err != nil {
		t.Fatalf("append first version: %v", err)
	}

	duplicate, created, err := AppendTemplateVersion(templateDir, tmpl.ID, []byte("same\n"), TemplateVersionSourceSave, "", "")
	if err != nil {
		t.Fatalf("append duplicate version: %v", err)
	}
	if created {
		t.Fatal("duplicate content must not create a new version")
	}
	if duplicate.ID != first.ID || duplicate.Version != first.Version {
		t.Fatalf("duplicate returned %#v, want existing %#v", duplicate, first)
	}

	count, err := CountTemplateVersions(tmpl.ID)
	if err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if count != 1 {
		t.Fatalf("version count = %d, want 1", count)
	}
}

func TestAppendTemplateVersionKeepsUnlimitedHistory(t *testing.T) {
	setupTemplateVersionTestDB(t)
	templateDir := t.TempDir()
	tmpl := createTemplateRecordForVersionTest(t, "clash.yaml")

	const total = 120
	for i := 1; i <= total; i++ {
		if _, _, err := AppendTemplateVersion(templateDir, tmpl.ID, []byte(fmt.Sprintf("version-%d\n", i)), TemplateVersionSourceSave, "", ""); err != nil {
			t.Fatalf("append version %d: %v", i, err)
		}
	}

	count, err := CountTemplateVersions(tmpl.ID)
	if err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if count != total {
		t.Fatalf("version count = %d, want %d", count, total)
	}

	entries, err := os.ReadDir(TemplateVersionDir(templateDir, tmpl.ID))
	if err != nil {
		t.Fatalf("read version dir: %v", err)
	}
	if len(entries) != total {
		t.Fatalf("snapshot files = %d, want %d", len(entries), total)
	}

	first, err := FindTemplateVersion(tmpl.ID, 1)
	if err != nil {
		t.Fatalf("find first version: %v", err)
	}
	content, err := ReadTemplateVersionContent(templateDir, first)
	if err != nil {
		t.Fatalf("read first version: %v", err)
	}
	if string(content) != "version-1\n" {
		t.Fatalf("first version content = %q", string(content))
	}
}

func TestEnsureTemplateBaselineOnlyCreatesFirstSnapshot(t *testing.T) {
	setupTemplateVersionTestDB(t)
	templateDir := t.TempDir()
	tmpl := createTemplateRecordForVersionTest(t, "clash.yaml")

	if err := EnsureTemplateBaseline(templateDir, tmpl.ID, []byte("original\n"), "admin"); err != nil {
		t.Fatalf("ensure baseline: %v", err)
	}
	if err := EnsureTemplateBaseline(templateDir, tmpl.ID, []byte("later\n"), "admin"); err != nil {
		t.Fatalf("ensure baseline second run: %v", err)
	}

	count, err := CountTemplateVersions(tmpl.ID)
	if err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if count != 1 {
		t.Fatalf("baseline version count = %d, want 1", count)
	}

	version, err := FindTemplateVersion(tmpl.ID, 1)
	if err != nil {
		t.Fatalf("find baseline version: %v", err)
	}
	if version.Source != TemplateVersionSourceBaseline {
		t.Fatalf("baseline source = %q", version.Source)
	}
}

func TestListTemplateVersionsPagination(t *testing.T) {
	setupTemplateVersionTestDB(t)
	templateDir := t.TempDir()
	tmpl := createTemplateRecordForVersionTest(t, "clash.yaml")

	for i := 1; i <= 5; i++ {
		if _, _, err := AppendTemplateVersion(templateDir, tmpl.ID, []byte(fmt.Sprintf("v%d\n", i)), TemplateVersionSourceSave, "", ""); err != nil {
			t.Fatalf("append version %d: %v", i, err)
		}
	}

	items, total, err := ListTemplateVersions(tmpl.ID, 1, 2)
	if err != nil {
		t.Fatalf("list versions: %v", err)
	}
	if total != 5 || len(items) != 2 {
		t.Fatalf("total=%d items=%d, want total=5 items=2", total, len(items))
	}
	if items[0].Version != 5 || items[1].Version != 4 {
		t.Fatalf("unexpected page order: %#v", items)
	}
}

func TestDeleteTemplateVersionsRemovesRowsAndSnapshots(t *testing.T) {
	setupTemplateVersionTestDB(t)
	templateDir := t.TempDir()
	tmpl := createTemplateRecordForVersionTest(t, "clash.yaml")

	for i := 1; i <= 3; i++ {
		if _, _, err := AppendTemplateVersion(templateDir, tmpl.ID, []byte(fmt.Sprintf("v%d\n", i)), TemplateVersionSourceSave, "", ""); err != nil {
			t.Fatalf("append version %d: %v", i, err)
		}
	}

	if err := DeleteTemplateVersions(templateDir, tmpl.ID); err != nil {
		t.Fatalf("delete template versions: %v", err)
	}

	count, err := CountTemplateVersions(tmpl.ID)
	if err != nil {
		t.Fatalf("count versions: %v", err)
	}
	if count != 0 {
		t.Fatalf("version count after delete = %d, want 0", count)
	}
	if _, err := os.Stat(TemplateVersionDir(templateDir, tmpl.ID)); !os.IsNotExist(err) {
		t.Fatalf("version dir still exists: %v", err)
	}
}

func TestReadTemplateVersionContentMissingSnapshot(t *testing.T) {
	setupTemplateVersionTestDB(t)
	templateDir := t.TempDir()
	tmpl := createTemplateRecordForVersionTest(t, "clash.yaml")

	version, _, err := AppendTemplateVersion(templateDir, tmpl.ID, []byte("content\n"), TemplateVersionSourceAdd, "", "")
	if err != nil {
		t.Fatalf("append version: %v", err)
	}
	if err := os.Remove(filepath.Join(TemplateVersionDir(templateDir, tmpl.ID), fmt.Sprintf("%06d.snapshot", version.Version))); err != nil {
		t.Fatalf("remove snapshot: %v", err)
	}

	if _, err := ReadTemplateVersionContent(templateDir, version); err == nil {
		t.Fatal("expected error for missing snapshot")
	}
}

func TestFindTemplateVersionNotFound(t *testing.T) {
	setupTemplateVersionTestDB(t)
	tmpl := createTemplateRecordForVersionTest(t, "clash.yaml")

	if _, err := FindTemplateVersion(tmpl.ID, 99); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected record not found, got %v", err)
	}
}
