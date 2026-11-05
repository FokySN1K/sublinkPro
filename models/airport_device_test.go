package models

import (
	"testing"

	"sublink/database"
	"sublink/utils"
)

func headerValue(headers AirportRequestHeaders, key string) string {
	for _, header := range headers {
		if header.Key == key {
			return header.Value
		}
	}
	return ""
}

func TestAirportEffectiveRequestHeadersAddsHWIDAndDeviceHeaders(t *testing.T) {
	airport := Airport{
		ID:            42,
		HWID:          "abcdef0123456789abcdef0123456789",
		DeviceProfile: AirportDeviceProfileAndroid,
		DeviceOS:      " Android ",
		OSVersion:     "14",
		DeviceModel:   "Pixel 7",
		AppName:       "v2rayTun",
	}

	headers := airport.EffectiveRequestHeaders()

	if got := headerValue(headers, "X-HWID"); got != airport.HWID {
		t.Fatalf("X-HWID = %q, want %q", got, airport.HWID)
	}
	// 设备头只取保存的字段值，并去除首尾空格。
	if got := headerValue(headers, "X-Device-OS"); got != "Android" {
		t.Fatalf("X-Device-OS = %q, want Android", got)
	}
	if got := headerValue(headers, "X-Ver-OS"); got != "14" {
		t.Fatalf("X-Ver-OS = %q, want 14", got)
	}
	if got := headerValue(headers, "X-Device-Model"); got != "Pixel 7" {
		t.Fatalf("X-Device-Model = %q, want Pixel 7", got)
	}
	if got := headerValue(headers, "X-App-Name"); got != "v2rayTun" {
		t.Fatalf("X-App-Name = %q, want v2rayTun", got)
	}
}

func TestAirportEffectiveRequestHeadersKeepsUserValues(t *testing.T) {
	airport := Airport{
		ID:            7,
		HWID:          "stored-hwid",
		DeviceProfile: AirportDeviceProfileIOS,
		RequestHeaders: AirportRequestHeaders{
			{Key: "X-HWID", Value: "custom-hwid"},
			{Key: "x-device-os", Value: "CustomOS"},
			{Key: "X-Custom", Value: "value"},
		},
	}

	headers := airport.EffectiveRequestHeaders()

	if got := headerValue(headers, "X-HWID"); got != "custom-hwid" {
		t.Fatalf("X-HWID = %q, want custom-hwid", got)
	}
	if got := headerValue(headers, "x-device-os"); got != "CustomOS" {
		t.Fatalf("x-device-os = %q, want CustomOS", got)
	}
	if got := headerValue(headers, "X-Custom"); got != "value" {
		t.Fatalf("X-Custom = %q, want value", got)
	}
	// 未填写的设备字段不会被预置值复活。
	if got := headerValue(headers, "X-App-Name"); got != "" {
		t.Fatalf("X-App-Name = %q, want empty without stored value", got)
	}
}

func TestAirportEffectiveRequestHeadersDisableHWID(t *testing.T) {
	airport := Airport{ID: 3, HWID: "stored-hwid", DisableHWID: true}

	if got := headerValue(airport.EffectiveRequestHeaders(), "X-HWID"); got != "" {
		t.Fatalf("X-HWID = %q, want empty when disabled", got)
	}
}

func TestAirportEffectiveRequestHeadersDerivesLegacyHWID(t *testing.T) {
	airport := Airport{ID: 99}

	headers := airport.EffectiveRequestHeaders()
	want := utils.DeriveHWID(99)
	if got := headerValue(headers, "X-HWID"); got != want {
		t.Fatalf("X-HWID = %q, want derived %q", got, want)
	}

	// 同一机场多次计算必须稳定，避免机场把每次拉取都当成新设备。
	if again := headerValue(airport.EffectiveRequestHeaders(), "X-HWID"); again != want {
		t.Fatalf("X-HWID is not deterministic: %q vs %q", again, want)
	}
}

func TestAirportEffectiveUserAgentPrefersExplicitValue(t *testing.T) {
	airport := Airport{DeviceProfile: AirportDeviceProfileAndroid}
	if got := airport.EffectiveUserAgent(); got != "v2rayTun/5.4.1 (Android 14; Pixel 7)" {
		t.Fatalf("EffectiveUserAgent() = %q", got)
	}

	airport.UserAgent = "clash.meta"
	if got := airport.EffectiveUserAgent(); got != "clash.meta" {
		t.Fatalf("EffectiveUserAgent() = %q, want explicit UA", got)
	}
}

func TestAirportEffectiveRequestHeadersCustomProfile(t *testing.T) {
	airport := Airport{
		ID:            11,
		HWID:          "custom-hwid",
		DeviceProfile: AirportDeviceProfileCustom,
		DeviceOS:      "HarmonyOS",
		OSVersion:     "4.2",
		DeviceModel:   "Mate 60",
		AppName:       "v2rayTun",
	}

	headers := airport.EffectiveRequestHeaders()
	tests := map[string]string{
		"X-Device-OS":    "HarmonyOS",
		"X-Ver-OS":       "4.2",
		"X-Device-Model": "Mate 60",
		"X-App-Name":     "v2rayTun",
	}
	for key, want := range tests {
		if got := headerValue(headers, key); got != want {
			t.Fatalf("%s = %q, want %q", key, got, want)
		}
	}

	// 自定义档位下留空的字段不应发送。
	partial := Airport{ID: 12, DeviceProfile: AirportDeviceProfileCustom, DeviceOS: "HarmonyOS"}
	partialHeaders := partial.EffectiveRequestHeaders()
	if got := headerValue(partialHeaders, "X-Device-OS"); got != "HarmonyOS" {
		t.Fatalf("X-Device-OS = %q, want HarmonyOS", got)
	}
	if got := headerValue(partialHeaders, "X-Ver-OS"); got != "" {
		t.Fatalf("empty X-Ver-OS should not be sent, got %q", got)
	}
}

func TestAirportEffectiveRequestHeadersPresetDoesNotRefillEmptyFields(t *testing.T) {
	airport := Airport{
		ID:            21,
		DeviceProfile: AirportDeviceProfileAndroid,
		DeviceOS:      " HarmonyOS ",
		DeviceModel:   "Mate 60",
	}

	headers := airport.EffectiveRequestHeaders()
	if got := headerValue(headers, "X-Device-OS"); got != "HarmonyOS" {
		t.Fatalf("X-Device-OS = %q, want HarmonyOS", got)
	}
	if got := headerValue(headers, "X-Device-Model"); got != "Mate 60" {
		t.Fatalf("X-Device-Model = %q, want Mate 60", got)
	}
	// 清空的字段不能被档位预置值回填。
	if got := headerValue(headers, "X-Ver-OS"); got != "" {
		t.Fatalf("empty X-Ver-OS must not be sent, got %q", got)
	}
	if got := headerValue(headers, "X-App-Name"); got != "" {
		t.Fatalf("empty X-App-Name must not be sent, got %q", got)
	}

	// 设备字段全空时只保留 X-HWID。
	blank := Airport{ID: 22, HWID: "hwid", DeviceProfile: AirportDeviceProfileIOS}
	if got := headerValue(blank.EffectiveRequestHeaders(), "X-Device-OS"); got != "" {
		t.Fatalf("blank preset profile must not send X-Device-OS, got %q", got)
	}
}

func TestBackfillAirportDevicePresetFields(t *testing.T) {
	setupAirportTestDB(t)

	legacy := Airport{Name: "legacy-preset", URL: "https://example.com/a", CronExpr: "0 */12 * * *", DeviceProfile: AirportDeviceProfileAndroid}
	partial := Airport{Name: "partial-preset", URL: "https://example.com/b", CronExpr: "0 */12 * * *", DeviceProfile: AirportDeviceProfileAndroid, DeviceOS: "HarmonyOS"}
	custom := Airport{Name: "custom-profile", URL: "https://example.com/c", CronExpr: "0 */12 * * *", DeviceProfile: AirportDeviceProfileCustom}
	for _, airport := range []*Airport{&legacy, &partial, &custom} {
		if err := database.DB.Create(airport).Error; err != nil {
			t.Fatalf("create airport %s: %v", airport.Name, err)
		}
	}

	if err := backfillAirportDevicePresetFields(database.DB); err != nil {
		t.Fatalf("backfill preset fields: %v", err)
	}

	var gotLegacy Airport
	if err := database.DB.First(&gotLegacy, legacy.ID).Error; err != nil {
		t.Fatalf("reload legacy airport: %v", err)
	}
	if gotLegacy.DeviceOS != "Android" || gotLegacy.OSVersion != "14" || gotLegacy.DeviceModel != "Pixel 7" || gotLegacy.AppName != "v2rayTun" {
		t.Fatalf("legacy airport not backfilled: %+v", gotLegacy)
	}

	var gotPartial Airport
	if err := database.DB.First(&gotPartial, partial.ID).Error; err != nil {
		t.Fatalf("reload partial airport: %v", err)
	}
	// 已填写的字段保持不变，只补空字段。
	if gotPartial.DeviceOS != "HarmonyOS" {
		t.Fatalf("existing DeviceOS overwritten: %q", gotPartial.DeviceOS)
	}
	if gotPartial.OSVersion != "14" {
		t.Fatalf("empty OSVersion not backfilled: %q", gotPartial.OSVersion)
	}

	var gotCustom Airport
	if err := database.DB.First(&gotCustom, custom.ID).Error; err != nil {
		t.Fatalf("reload custom airport: %v", err)
	}
	if gotCustom.DeviceOS != "" || gotCustom.AppName != "" {
		t.Fatalf("custom profile must not be backfilled: %+v", gotCustom)
	}
}

func TestNormalizeAirportDeviceProfile(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Android", AirportDeviceProfileAndroid},
		{" ios ", AirportDeviceProfileIOS},
		{"windows", AirportDeviceProfileWindows},
		{" Custom ", AirportDeviceProfileCustom},
		{"unknown", AirportDeviceProfileNone},
		{"", AirportDeviceProfileNone},
	}
	for _, tt := range tests {
		if got := NormalizeAirportDeviceProfile(tt.input); got != tt.want {
			t.Fatalf("NormalizeAirportDeviceProfile(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
