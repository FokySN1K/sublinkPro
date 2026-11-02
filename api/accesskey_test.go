package api

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"sublink/database"
	"sublink/internal/testutil"
	"sublink/models"
)

func setupAccessKeyAPITestDB(t *testing.T) {
	t.Helper()

	oldDB := database.DB
	db := testutil.OpenMemoryDB(t, "accesskey_api_test")
	if err := db.AutoMigrate(&models.AccessKey{}); err != nil {
		t.Fatalf("auto migrate access_keys: %v", err)
	}
	database.DB = db

	t.Cleanup(func() {
		database.DB = oldDB
		testutil.CloseDB(t, db)
	})
}

func createTestAccessKey(t *testing.T, description string) *models.AccessKey {
	t.Helper()

	accessKey := &models.AccessKey{
		UserID:        1,
		Username:      "admin",
		AccessKeyHash: "test-hash",
		Description:   description,
	}
	if err := accessKey.Generate(); err != nil {
		t.Fatalf("create access key: %v", err)
	}
	return accessKey
}

func TestUpdateAccessKeyRenamesWithoutChangingSecret(t *testing.T) {
	setupAccessKeyAPITestDB(t)
	accessKey := createTestAccessKey(t, "old name")

	expiredAt := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	recorder := performJSONRequest(t, UpdateAccessKey, http.MethodPost, map[string]any{
		"id":          accessKey.ID,
		"description": "  renamed key  ",
		"expiredAt":   expiredAt,
	})
	response := decodeAPIResponse(t, recorder)
	if response.Code != 200 {
		t.Fatalf("update returned code %d (%s), want 200", response.Code, response.Msg)
	}

	var stored models.AccessKey
	if err := database.DB.First(&stored, accessKey.ID).Error; err != nil {
		t.Fatalf("reload access key: %v", err)
	}
	if stored.Description != "renamed key" {
		t.Fatalf("stored description = %q, want %q", stored.Description, "renamed key")
	}
	if stored.AccessKeyHash != "test-hash" {
		t.Fatalf("update changed the stored secret hash to %q", stored.AccessKeyHash)
	}
	if stored.ExpiredAt == nil || !stored.ExpiredAt.Equal(expiredAt) {
		t.Fatalf("stored expiry = %v, want %v", stored.ExpiredAt, expiredAt)
	}
}

func TestUpdateAccessKeyRejectsInvalidDescription(t *testing.T) {
	setupAccessKeyAPITestDB(t)
	accessKey := createTestAccessKey(t, "old name")

	blank := performJSONRequest(t, UpdateAccessKey, http.MethodPost, map[string]any{
		"id":          accessKey.ID,
		"description": "   ",
	})
	if response := decodeAPIResponse(t, blank); response.Code == 200 || response.I18nKey != "backend.accessKeys.descriptionRequired" {
		t.Fatalf("blank description returned code %d and i18nKey %q", response.Code, response.I18nKey)
	}

	tooLong := performJSONRequest(t, UpdateAccessKey, http.MethodPost, map[string]any{
		"id":          accessKey.ID,
		"description": strings.Repeat("a", models.AccessKeyDescriptionMaxLength+1),
	})
	if response := decodeAPIResponse(t, tooLong); response.Code == 200 || response.I18nKey != "backend.accessKeys.descriptionTooLong" {
		t.Fatalf("over-long description returned code %d and i18nKey %q", response.Code, response.I18nKey)
	}

	var stored models.AccessKey
	if err := database.DB.First(&stored, accessKey.ID).Error; err != nil {
		t.Fatalf("reload access key: %v", err)
	}
	if stored.Description != "old name" {
		t.Fatalf("stored description = %q, want %q", stored.Description, "old name")
	}
}

func TestUpdateAccessKeyReportsMissingKey(t *testing.T) {
	setupAccessKeyAPITestDB(t)

	recorder := performJSONRequest(t, UpdateAccessKey, http.MethodPost, map[string]any{
		"id":          999,
		"description": "ghost key",
	})
	if response := decodeAPIResponse(t, recorder); response.Code == 200 || response.I18nKey != "backend.accessKeys.notFound" {
		t.Fatalf("missing access key returned code %d and i18nKey %q", response.Code, response.I18nKey)
	}
}
