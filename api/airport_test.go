package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sublink/database"
	"sublink/internal/testutil"
	"sublink/models"

	"github.com/gin-gonic/gin"
)

func setupAirportAPITestDB(t *testing.T) {
	t.Helper()

	oldDB := database.DB
	oldDialect := database.Dialect
	oldInitialized := database.IsInitialized

	db := testutil.OpenMemoryDB(t, "airport_api_test")
	if err := db.AutoMigrate(&models.Airport{}); err != nil {
		t.Fatalf("auto migrate airports: %v", err)
	}

	database.DB = db
	database.Dialect = database.DialectSQLite
	database.IsInitialized = false
	if err := models.InitAirportCache(); err != nil {
		t.Fatalf("init airport cache: %v", err)
	}

	t.Cleanup(func() {
		database.DB = oldDB
		database.Dialect = oldDialect
		database.IsInitialized = oldInitialized
		if oldDB != nil {
			_ = models.InitAirportCache()
		}
		testutil.CloseDB(t, db)
	})
}

func performAirportJSONRequest(t *testing.T, handler gin.HandlerFunc, method, path string, body any, params ...gin.Param) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	var requestBody []byte
	var err error
	if body != nil {
		requestBody, err = json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
	}

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), method, path, bytes.NewReader(requestBody))
	ginContext.Request.Header.Set("Content-Type", "application/json")
	ginContext.Params = params

	handler(ginContext)
	return recorder
}

func decodeAirportAPIResponse(t *testing.T, recorder *httptest.ResponseRecorder) apiJSONResponse {
	t.Helper()
	return decodeAPIResponse(t, recorder)
}

func TestAirportAddPersistsUpdateAfterDetectSettings(t *testing.T) {
	setupAirportAPITestDB(t)

	addBody := map[string]any{
		"name":                         "机场A",
		"url":                          "https://example.com/subscription",
		"cronExpr":                     "0 */12 * * *",
		"enabled":                      false,
		"group":                        "默认组",
		"downloadWithProxy":            false,
		"proxyLink":                    "",
		"userAgent":                    "",
		"requestHeaders":               []map[string]string{},
		"fetchUsageInfo":               false,
		"skipTLSVerify":                false,
		"updateAfterDetect":            true,
		"updateAfterDetectProfileId":   7,
		"updateAfterDetectChangedOnly": true,
		"remark":                       "",
		"logo":                         "",
		"nodeNameWhitelist":            "",
		"nodeNameBlacklist":            "",
		"protocolWhitelist":            "",
		"protocolBlacklist":            "",
		"nodeNamePreprocess":           "",
		"deduplicationRule":            "",
		"nodeNameUniquify":             false,
		"nodeNamePrefix":               "",
		"nodeNameIntraUniquify":        false,
	}

	addRecorder := performAirportJSONRequest(t, AirportAdd, http.MethodPost, "/api/v1/airports", addBody)
	if addRecorder.Code != http.StatusOK {
		t.Fatalf("add airport status = %d, body = %s", addRecorder.Code, addRecorder.Body.String())
	}
	addResponse := decodeAirportAPIResponse(t, addRecorder)
	if addResponse.Code != 200 {
		t.Fatalf("add airport response code = %d, msg = %s", addResponse.Code, addResponse.Msg)
	}

	var stored models.Airport
	if err := database.DB.Where("name = ?", "机场A").First(&stored).Error; err != nil {
		t.Fatalf("load added airport: %v", err)
	}
	if !stored.UpdateAfterDetect {
		t.Fatal("expected update_after_detect to be stored as true after add")
	}
	if stored.UpdateAfterDetectProfileID != 7 {
		t.Fatalf("expected update_after_detect_profile_id = 7 after add, got %d", stored.UpdateAfterDetectProfileID)
	}
	if !stored.UpdateAfterDetectChangedOnly {
		t.Fatal("expected update_after_detect_changed_only to be stored as true after add")
	}
}

func TestResolveAirportHWID(t *testing.T) {
	if got := resolveAirportHWID(" abc ", "old", false, false); got != "abc" {
		t.Fatalf("explicit hwid should win, got %q", got)
	}
	if got := resolveAirportHWID("", " old ", false, false); got != "old" {
		t.Fatalf("stored hwid should be kept, got %q", got)
	}
	if got := resolveAirportHWID("", "old", true, false); got != "old" {
		t.Fatalf("stored hwid should survive disabling, got %q", got)
	}
	if got := resolveAirportHWID("", "", true, true); got != "" {
		t.Fatalf("disabled hwid without stored value should stay empty, got %q", got)
	}
	// 编辑时（generate=false）不能凭空生成新设备，否则机场会把请求当成新设备。
	if got := resolveAirportHWID("", "", false, false); got != "" {
		t.Fatalf("edit without stored hwid should stay empty, got %q", got)
	}

	generated := resolveAirportHWID("", "", false, true)
	if len(generated) != 32 {
		t.Fatalf("generated hwid should be 32 characters, got %q", generated)
	}
	for _, char := range generated {
		if !strings.ContainsRune("0123456789abcdef", char) {
			t.Fatalf("generated hwid should be lowercase hex, got %q", generated)
		}
	}
}

func TestAirportAddStoresDeviceIdentity(t *testing.T) {
	setupAirportAPITestDB(t)

	addBody := map[string]any{
		"name":          "机场Device",
		"url":           "https://example.com/subscription-device",
		"cronExpr":      "0 */12 * * *",
		"enabled":       false,
		"deviceProfile": "Android",
		"deviceOS":      "Android",
		"osVersion":     "14",
		"deviceModel":   "Pixel 7",
		"appName":       "v2rayTun",
	}

	recorder := performAirportJSONRequest(t, AirportAdd, http.MethodPost, "/api/v1/airports", addBody)
	if recorder.Code != http.StatusOK {
		t.Fatalf("add airport status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var stored models.Airport
	if err := database.DB.Where("name = ?", "机场Device").First(&stored).Error; err != nil {
		t.Fatalf("load added airport: %v", err)
	}
	if len(stored.HWID) != 32 {
		t.Fatalf("expected generated 32-char hwid, got %q", stored.HWID)
	}
	if stored.DisableHWID {
		t.Fatal("expected hwid to be enabled by default")
	}
	if stored.DeviceProfile != models.AirportDeviceProfileAndroid {
		t.Fatalf("expected normalized device profile %q, got %q", models.AirportDeviceProfileAndroid, stored.DeviceProfile)
	}

	headers := stored.EffectiveRequestHeaders()
	var gotHWID, gotDeviceOS string
	for _, header := range headers {
		switch header.Key {
		case "X-HWID":
			gotHWID = header.Value
		case "X-Device-OS":
			gotDeviceOS = header.Value
		}
	}
	if gotHWID != stored.HWID {
		t.Fatalf("X-HWID header = %q, want %q", gotHWID, stored.HWID)
	}
	if gotDeviceOS != "Android" {
		t.Fatalf("X-Device-OS header = %q, want Android", gotDeviceOS)
	}
}

func TestAirportAddDoesNotImplicitlyApplyDevicePreset(t *testing.T) {
	setupAirportAPITestDB(t)

	// 档位预置只在 UI 填表时使用；只传 deviceProfile 时不应凭空发送设备头。
	addBody := map[string]any{
		"name":          "机场PresetOnly",
		"url":           "https://example.com/subscription-preset-only",
		"cronExpr":      "0 */12 * * *",
		"enabled":       false,
		"deviceProfile": "android",
	}

	recorder := performAirportJSONRequest(t, AirportAdd, http.MethodPost, "/api/v1/airports", addBody)
	if recorder.Code != http.StatusOK {
		t.Fatalf("add airport status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var stored models.Airport
	if err := database.DB.Where("name = ?", "机场PresetOnly").First(&stored).Error; err != nil {
		t.Fatalf("load added airport: %v", err)
	}
	if stored.DeviceProfile != models.AirportDeviceProfileAndroid {
		t.Fatalf("expected device profile %q, got %q", models.AirportDeviceProfileAndroid, stored.DeviceProfile)
	}
	if stored.DeviceOS != "" || stored.AppName != "" {
		t.Fatalf("preset values must not be stored implicitly: %+v", stored)
	}

	for _, header := range stored.EffectiveRequestHeaders() {
		if header.Key == "X-Device-OS" || header.Key == "X-App-Name" {
			t.Fatalf("unexpected device header %s = %q", header.Key, header.Value)
		}
	}
}

func TestAirportAddStoresCustomDeviceIdentity(t *testing.T) {
	setupAirportAPITestDB(t)

	addBody := map[string]any{
		"name":          "机场CustomDevice",
		"url":           "https://example.com/subscription-custom-device",
		"cronExpr":      "0 */12 * * *",
		"enabled":       false,
		"deviceProfile": "Custom",
		"deviceOS":      " HarmonyOS ",
		"osVersion":     "4.2",
		"deviceModel":   "Mate 60",
		"appName":       "v2rayTun",
	}

	recorder := performAirportJSONRequest(t, AirportAdd, http.MethodPost, "/api/v1/airports", addBody)
	if recorder.Code != http.StatusOK {
		t.Fatalf("add airport status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var stored models.Airport
	if err := database.DB.Where("name = ?", "机场CustomDevice").First(&stored).Error; err != nil {
		t.Fatalf("load added airport: %v", err)
	}
	if stored.DeviceProfile != models.AirportDeviceProfileCustom {
		t.Fatalf("expected device profile %q, got %q", models.AirportDeviceProfileCustom, stored.DeviceProfile)
	}
	if stored.DeviceOS != "HarmonyOS" {
		t.Fatalf("device OS should be trimmed and stored, got %q", stored.DeviceOS)
	}
	if stored.DeviceModel != "Mate 60" {
		t.Fatalf("device model = %q", stored.DeviceModel)
	}

	headers := stored.EffectiveRequestHeaders()
	want := map[string]string{
		"X-Device-OS":    "HarmonyOS",
		"X-Ver-OS":       "4.2",
		"X-Device-Model": "Mate 60",
		"X-App-Name":     "v2rayTun",
	}
	for _, header := range headers {
		if expected, ok := want[header.Key]; ok {
			if header.Value != expected {
				t.Fatalf("%s = %q, want %q", header.Key, header.Value, expected)
			}
			delete(want, header.Key)
		}
	}
	if len(want) > 0 {
		t.Fatalf("missing custom device headers: %v", want)
	}
}
