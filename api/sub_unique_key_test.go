package api

import (
	"net/url"
	"strings"
	"testing"

	"sublink/database"
	"sublink/models"
)

func addNodePoolForUniqueKeyTest(t *testing.T, name string) *models.Subcription {
	t.Helper()
	sub := &models.Subcription{Name: name, RefreshUsageOnRequest: true}
	if err := sub.Add(); err != nil {
		t.Fatalf("add node pool %q: %v", name, err)
	}
	if sub.UniqueKey == "" {
		t.Fatalf("node pool %q has no unique key", name)
	}
	return sub
}

func TestSubUpdateFindsNodePoolByUniqueKey(t *testing.T) {
	setupProxyRelayTestDB(t)

	primary := addNodePoolForUniqueKeyTest(t, "pool-alpha")
	other := addNodePoolForUniqueKeyTest(t, "pool-beta")

	recorder := performSubcriptionFormRequest(t, SubUpdate, url.Values{
		// 旧名称是过期值：接口必须优先按稳定唯一键定位。
		"unique_key": {primary.UniqueKey},
		"oldname":    {"pool-stale-name"},
		"name":       {"pool-alpha-renamed"},
		"type":       {"collection"},
		"groups":     {"rename-group"},
	})
	if response := decodeAPIResponse(t, recorder); response.Code != 200 {
		t.Fatalf("update by unique key failed: %s", response.Msg)
	}

	var stored models.Subcription
	if err := database.DB.First(&stored, primary.ID).Error; err != nil {
		t.Fatalf("load renamed node pool: %v", err)
	}
	if stored.Name != "pool-alpha-renamed" {
		t.Fatalf("name = %q, want pool-alpha-renamed", stored.Name)
	}
	if stored.UniqueKey != primary.UniqueKey {
		t.Fatalf("unique key changed on rename: got %q, want %q", stored.UniqueKey, primary.UniqueKey)
	}

	var untouched models.Subcription
	if err := database.DB.First(&untouched, other.ID).Error; err != nil {
		t.Fatalf("load untouched node pool: %v", err)
	}
	if untouched.Name != "pool-beta" || untouched.UniqueKey != other.UniqueKey {
		t.Fatalf("unrelated node pool was modified: name=%q key=%q", untouched.Name, untouched.UniqueKey)
	}
}

func TestSubUpdateFallsBackToOldNameWithoutUniqueKey(t *testing.T) {
	setupProxyRelayTestDB(t)

	sub := addNodePoolForUniqueKeyTest(t, "pool-legacy")
	originalKey := sub.UniqueKey

	recorder := performSubcriptionFormRequest(t, SubUpdate, url.Values{
		// 旧版前端不提交 unique_key，继续按 oldname 兼容解析。
		"oldname": {"pool-legacy"},
		"name":    {"pool-legacy-renamed"},
		"type":    {"collection"},
		"groups":  {"legacy-group"},
	})
	if response := decodeAPIResponse(t, recorder); response.Code != 200 {
		t.Fatalf("legacy update failed: %s", response.Msg)
	}

	var stored models.Subcription
	if err := database.DB.First(&stored, sub.ID).Error; err != nil {
		t.Fatalf("load renamed node pool: %v", err)
	}
	if stored.Name != "pool-legacy-renamed" {
		t.Fatalf("name = %q, want pool-legacy-renamed", stored.Name)
	}
	if stored.UniqueKey != originalKey {
		t.Fatalf("unique key changed on legacy rename: got %q, want %q", stored.UniqueKey, originalKey)
	}
}

func TestSubUpdateRejectsDuplicateNameWhenRenamingByUniqueKey(t *testing.T) {
	setupProxyRelayTestDB(t)

	addNodePoolForUniqueKeyTest(t, "pool-taken")
	sub := addNodePoolForUniqueKeyTest(t, "pool-renaming")

	recorder := performSubcriptionFormRequest(t, SubUpdate, url.Values{
		"unique_key": {sub.UniqueKey},
		"name":       {"pool-taken"},
		"type":       {"collection"},
		"groups":     {"rename-group"},
	})
	response := decodeAPIResponse(t, recorder)
	if response.Code == 200 {
		t.Fatal("expected rename to an existing name to be rejected")
	}
	if !strings.Contains(response.Msg, "must be unique") {
		t.Fatalf("unexpected error message: %q", response.Msg)
	}

	var stored models.Subcription
	if err := database.DB.First(&stored, sub.ID).Error; err != nil {
		t.Fatalf("load node pool: %v", err)
	}
	if stored.Name != "pool-renaming" {
		t.Fatalf("name changed despite error: %q", stored.Name)
	}
	if stored.UniqueKey != sub.UniqueKey {
		t.Fatalf("unique key changed despite error: got %q, want %q", stored.UniqueKey, sub.UniqueKey)
	}
}

func TestSubUpdateRejectsUnknownUniqueKey(t *testing.T) {
	setupProxyRelayTestDB(t)

	sub := addNodePoolForUniqueKeyTest(t, "pool-real")

	recorder := performSubcriptionFormRequest(t, SubUpdate, url.Values{
		"unique_key": {"does-not-exist-00000000"},
		"name":       {"pool-anything"},
		"type":       {"collection"},
		"groups":     {"rename-group"},
	})
	if response := decodeAPIResponse(t, recorder); response.Code == 200 {
		t.Fatal("expected unknown unique key to be rejected")
	}

	var stored models.Subcription
	if err := database.DB.First(&stored, sub.ID).Error; err != nil {
		t.Fatalf("load node pool: %v", err)
	}
	if stored.Name != sub.Name {
		t.Fatalf("node pool renamed despite error: %q", stored.Name)
	}
}
