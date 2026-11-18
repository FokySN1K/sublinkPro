package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"sublink/models"

	"github.com/gin-gonic/gin"
)

type shareTelemetryTestData struct {
	ReportedTrafficBytes       int64      `json:"reported_traffic_bytes"`
	TrafficLimitBytes          int64      `json:"traffic_limit_bytes"`
	RemainingBytes             int64      `json:"remaining_bytes"`
	QuotaExhausted             bool       `json:"quota_exhausted"`
	LastUsageAt                *time.Time `json:"last_usage_at"`
	DeviceStatus               string     `json:"device_status"`
	DeviceReportedTrafficBytes int64      `json:"device_reported_traffic_bytes"`
	AcceptedBytes              int64      `json:"accepted_bytes"`
	Duplicate                  bool       `json:"duplicate"`
}

func performShareTelemetryRequest(t *testing.T, token, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	path := "/c/telemetry"
	if token != "" {
		path += "?token=" + token
	}
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(body))
	ginContext.Request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		ginContext.Request.Header.Set(key, value)
	}
	ShareReportTraffic(ginContext)
	return recorder
}

func decodeShareTelemetryResponse(t *testing.T, recorder *httptest.ResponseRecorder) shareTelemetryTestData {
	t.Helper()
	var payload struct {
		Code int                    `json:"code"`
		Msg  string                 `json:"msg"`
		Data shareTelemetryTestData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode telemetry response: %v body=%q", err, recorder.Body.String())
	}
	if payload.Code != http.StatusOK {
		t.Fatalf("expected code 200, got %d msg=%q", payload.Code, payload.Msg)
	}
	return payload.Data
}

func TestShareTelemetryAccumulatesReportedTraffic(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "telemetry-sub", "telemetry-token", "Telemetry Node", models.ShareGroup{
		TelemetryEnabled:  true,
		TrafficLimitBytes: 1000,
	})

	first := performShareTelemetryRequest(t, "telemetry-token", `{"up": 100, "down": 250}`, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", first.Code, first.Body.String())
	}
	data := decodeShareTelemetryResponse(t, first)
	if data.ReportedTrafficBytes != 350 || data.TrafficLimitBytes != 1000 || data.RemainingBytes != 650 {
		t.Fatalf("unexpected first report data: %+v", data)
	}
	if data.QuotaExhausted {
		t.Fatalf("quota must not be exhausted yet: %+v", data)
	}
	if data.DeviceStatus != "unbound" {
		t.Fatalf("expected unbound device status, got %q", data.DeviceStatus)
	}
	if data.LastUsageAt == nil {
		t.Fatal("expected last_usage_at to be set")
	}

	second := performShareTelemetryRequest(t, "telemetry-token", `{"down": 700}`, nil)
	if second.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", second.Code, second.Body.String())
	}
	data = decodeShareTelemetryResponse(t, second)
	if data.ReportedTrafficBytes != 1050 || data.RemainingBytes != 0 || !data.QuotaExhausted {
		t.Fatalf("unexpected report data after exceeding the limit: %+v", data)
	}

	stored, err := models.GetSubscriptionShareByToken("telemetry-token")
	if err != nil {
		t.Fatalf("reload share: %v", err)
	}
	if stored.ReportedTrafficBytes != 1050 {
		t.Fatalf("expected stored reported traffic 1050, got %d", stored.ReportedTrafficBytes)
	}
	if stored.LastUsageAt == nil {
		t.Fatal("expected stored last_usage_at to be set")
	}

	profile := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=telemetry-token&client=v2ray", map[string]string{
		"X-HWID":      "hwid-quota",
		"X-Device-ID": "device-quota",
	})
	if got := profile.Header().Get("X-Sublink-Reason"); got != models.ShareStubCaseQuotaExhausted {
		t.Fatalf("expected quota_exhausted stub after reporting past the limit, got %q", got)
	}
}

func TestShareTelemetryVerifiesDeviceSecret(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "secret-sub", "secret-token", "Secret Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})

	profile := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=secret-token&client=v2ray", map[string]string{
		"X-HWID":      "hwid-secret",
		"X-Device-ID": "device-secret",
	})
	secret := profile.Header().Get("X-Sublink-Device-Secret")
	if secret == "" {
		t.Fatal("expected a device secret after auto-binding")
	}

	wrong := performShareTelemetryRequest(t, "secret-token", `{"up": 10}`, map[string]string{
		"X-Sublink-Device-Secret": "not-the-secret",
	})
	if wrong.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for an unknown device secret, got %d body=%q", wrong.Code, wrong.Body.String())
	}

	accepted := performShareTelemetryRequest(t, "secret-token", `{"up": 60, "down": 60}`, map[string]string{
		"X-Sublink-Device-Secret": secret,
	})
	if accepted.Code != http.StatusOK {
		t.Fatalf("expected 200 for a valid device secret, got %d body=%q", accepted.Code, accepted.Body.String())
	}
	data := decodeShareTelemetryResponse(t, accepted)
	if data.DeviceStatus != models.ShareDeviceStatusActive {
		t.Fatalf("expected active device status, got %q", data.DeviceStatus)
	}
	if data.ReportedTrafficBytes != 120 {
		t.Fatalf("expected the rejected report not to be counted, got %d bytes", data.ReportedTrafficBytes)
	}

	device, err := models.FindShareDeviceBySecret(share.ID, secret)
	if err != nil {
		t.Fatalf("find bound device: %v", err)
	}
	device.Status = models.ShareDeviceStatusBlocked
	if err := device.Update(); err != nil {
		t.Fatalf("block device: %v", err)
	}
	blocked := performShareTelemetryRequest(t, "secret-token", `{"up": 5}`, map[string]string{
		"X-Sublink-Device-Secret": secret,
	})
	if blocked.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for a blocked device, got %d body=%q", blocked.Code, blocked.Body.String())
	}
}

func TestShareTelemetryRejectsInvalidReports(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "valid-telemetry-sub", "valid-telemetry-token", "Valid Node", models.ShareGroup{
		TelemetryEnabled: true,
	})
	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "off-telemetry-sub", "off-telemetry-token", "Off Node", models.ShareGroup{
		TelemetryEnabled: false,
	})
	createClientSubscriptionFixture(t, clashTemplatePath, surgeTemplatePath, "legacy-telemetry-sub", "legacy-telemetry-token", "Legacy Telemetry Node")

	tests := []struct {
		name      string
		token     string
		body      string
		wantCode  int
		errorText string
	}{
		{name: "unknown token", token: "missing-token", body: `{"up": 1}`, wantCode: http.StatusNotFound, errorText: "Invalid share token"},
		{name: "empty token", token: "", body: `{"up": 1}`, wantCode: http.StatusNotFound, errorText: "Invalid share token"},
		{name: "telemetry disabled", token: "off-telemetry-token", body: `{"up": 1}`, wantCode: http.StatusForbidden, errorText: "Telemetry is not enabled"},
		{name: "legacy link", token: "legacy-telemetry-token", body: `{"up": 1}`, wantCode: http.StatusForbidden, errorText: "Telemetry is not enabled"},
		{name: "negative up", token: "valid-telemetry-token", body: `{"up": -1}`, wantCode: http.StatusBadRequest, errorText: "non-negative"},
		{name: "negative down", token: "valid-telemetry-token", body: `{"down": -5}`, wantCode: http.StatusBadRequest, errorText: "non-negative"},
		{name: "empty body", token: "valid-telemetry-token", body: `{}`, wantCode: http.StatusBadRequest, errorText: "at least one"},
		{name: "malformed value", token: "valid-telemetry-token", body: `{"up": "lots"}`, wantCode: http.StatusBadRequest, errorText: "Invalid telemetry payload"},
		{name: "too large", token: "valid-telemetry-token", body: `{"up": 1125899906842625}`, wantCode: http.StatusBadRequest, errorText: "too large"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := performShareTelemetryRequest(t, tt.token, tt.body, nil)
			if recorder.Code != tt.wantCode {
				t.Fatalf("expected status %d, got %d body=%q", tt.wantCode, recorder.Code, recorder.Body.String())
			}
			var payload struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode error response: %v body=%q", err, recorder.Body.String())
			}
			if payload.Code != tt.wantCode {
				t.Fatalf("expected envelope code %d, got %d msg=%q", tt.wantCode, payload.Code, payload.Msg)
			}
			if !strings.Contains(payload.Msg, tt.errorText) {
				t.Fatalf("expected message %q to contain %q", payload.Msg, tt.errorText)
			}
		})
	}

	share, err := models.GetSubscriptionShareByToken("valid-telemetry-token")
	if err != nil {
		t.Fatalf("reload share: %v", err)
	}
	if share.ReportedTrafficBytes != 0 || share.LastUsageAt != nil {
		t.Fatalf("rejected reports must not change the counter: %+v", share)
	}
}

// TestShareGroupListReportsTelemetryAggregates covers the group-level telemetry
// fields surfaced by the share groups page (used / limit, last report, quota).
func TestShareGroupListReportsTelemetryAggregates(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	sub, firstShare, group := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "agg-sub", "agg-token-1", "Agg Node", models.ShareGroup{
		TelemetryEnabled:  true,
		TrafficLimitBytes: 1000,
	})

	secondShare := models.SubscriptionShare{
		SubscriptionID: sub.ID,
		Token:          "agg-token-2",
		Name:           "agg share 2",
		ExpireType:     models.ExpireTypeNever,
		Enabled:        true,
	}
	if err := secondShare.Add(); err != nil {
		t.Fatalf("add second share: %v", err)
	}
	secondShare.ApplyGroupSnapshot(group)
	if err := secondShare.Update(); err != nil {
		t.Fatalf("attach second share to group: %v", err)
	}

	if err := firstShare.AddReportedTraffic(600); err != nil {
		t.Fatalf("report traffic on first share: %v", err)
	}
	if err := secondShare.AddReportedTraffic(1500); err != nil {
		t.Fatalf("report traffic on second share: %v", err)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/share-groups/list", nil)
	ShareGroupList(ginContext)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 from group list, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data []struct {
			ID                   int        `json:"id"`
			ReportedTrafficBytes int64      `json:"reported_traffic_bytes"`
			LastUsageAt          *time.Time `json:"last_usage_at"`
			ExhaustedShareCount  int        `json:"exhausted_share_count"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode group list: %v body=%q", err, recorder.Body.String())
	}
	var item *struct {
		ID                   int        `json:"id"`
		ReportedTrafficBytes int64      `json:"reported_traffic_bytes"`
		LastUsageAt          *time.Time `json:"last_usage_at"`
		ExhaustedShareCount  int        `json:"exhausted_share_count"`
	}
	for i := range payload.Data {
		if payload.Data[i].ID == group.ID {
			item = &payload.Data[i]
			break
		}
	}
	if item == nil {
		t.Fatalf("group %d missing from list response", group.ID)
	}
	if item.ReportedTrafficBytes != 2100 {
		t.Fatalf("expected aggregated reported traffic 2100, got %d", item.ReportedTrafficBytes)
	}
	if item.LastUsageAt == nil {
		t.Fatal("expected last_usage_at to be aggregated")
	}
	if item.ExhaustedShareCount != 1 {
		t.Fatalf("expected one exhausted share, got %d", item.ExhaustedShareCount)
	}
}

// TestShareTelemetryAbsoluteCounter covers the reported_total_bytes mode:
// the server accepts only the growth of the installation counter, treats
// replays as duplicates and restarts the baseline when install_id changes.
func TestShareTelemetryAbsoluteCounter(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "abs-sub", "abs-token", "Abs Node", models.ShareGroup{
		TelemetryEnabled:  true,
		TrafficLimitBytes: 100000,
	})
	device, err := models.EnsureShareDevice(share.ID, "hwid-abs", "device-abs", "", "android", "1.0", models.ShareDeviceStatusActive)
	if err != nil {
		t.Fatalf("bind device: %v", err)
	}
	headers := map[string]string{
		"X-Sublink-Device-Secret": device.Secret,
		"X-HWID":                  "hwid-abs",
		"X-Device-ID":             "device-abs",
	}

	first := decodeShareTelemetryResponse(t, performShareTelemetryRequest(t, "abs-token",
		`{"reported_total_bytes": 100, "install_id": "inst-1", "up": 40, "down": 60}`, headers))
	if first.Duplicate || first.AcceptedBytes != 100 || first.ReportedTrafficBytes != 100 || first.DeviceReportedTrafficBytes != 100 {
		t.Fatalf("unexpected first absolute report: %+v", first)
	}

	replay := decodeShareTelemetryResponse(t, performShareTelemetryRequest(t, "abs-token",
		`{"reported_total_bytes": 100, "install_id": "inst-1"}`, headers))
	if !replay.Duplicate || replay.AcceptedBytes != 0 || replay.ReportedTrafficBytes != 100 {
		t.Fatalf("expected a duplicate replay, got %+v", replay)
	}

	growth := decodeShareTelemetryResponse(t, performShareTelemetryRequest(t, "abs-token",
		`{"reported_total_bytes": 250, "install_id": "inst-1", "up": 40}`, headers))
	if growth.Duplicate || growth.AcceptedBytes != 150 || growth.ReportedTrafficBytes != 250 || growth.DeviceReportedTrafficBytes != 250 {
		t.Fatalf("unexpected growth report: %+v", growth)
	}

	// A smaller total with the same install_id is a late/duplicated report,
	// not a reinstall: the baseline must stay at 250.
	stale := decodeShareTelemetryResponse(t, performShareTelemetryRequest(t, "abs-token",
		`{"reported_total_bytes": 10, "install_id": "inst-1"}`, headers))
	if !stale.Duplicate || stale.AcceptedBytes != 0 || stale.ReportedTrafficBytes != 250 {
		t.Fatalf("expected a stale duplicate, got %+v", stale)
	}

	// A new install_id restarts the client counter from zero.
	reinstall := decodeShareTelemetryResponse(t, performShareTelemetryRequest(t, "abs-token",
		`{"reported_total_bytes": 10, "install_id": "inst-2"}`, headers))
	if reinstall.Duplicate || reinstall.AcceptedBytes != 10 || reinstall.ReportedTrafficBytes != 260 || reinstall.DeviceReportedTrafficBytes != 260 {
		t.Fatalf("unexpected reinstall report: %+v", reinstall)
	}

	stored, err := models.GetShareDeviceByID(device.ID)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if stored.ReportBaselineBytes != 10 || stored.ReportBaselineEpoch != "inst-2" {
		t.Fatalf("unexpected baseline state: %+v", stored)
	}

	// The hourly buckets keep the up/down split of every accepted delta:
	// 40/60 + 40/110 + 0/10.
	buckets, err := models.ListShareDeviceUsage([]int{device.ID}, time.Time{})
	if err != nil {
		t.Fatalf("list device usage: %v", err)
	}
	var upSum, downSum int64
	for _, bucket := range buckets {
		upSum += bucket.UpBytes
		downSum += bucket.DownBytes
	}
	if upSum != 80 || downSum != 180 {
		t.Fatalf("unexpected device usage split: up=%d down=%d", upSum, downSum)
	}
}

// TestShareTelemetryAbsoluteWithoutDeviceFallsBackToDiff proves that the
// absolute counter needs a resolved device; otherwise the diff semantics and
// report_id dedup apply.
func TestShareTelemetryAbsoluteWithoutDeviceFallsBackToDiff(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "abs-free-sub", "abs-free-token", "Abs Free Node", models.ShareGroup{
		TelemetryEnabled: true,
	})

	data := decodeShareTelemetryResponse(t, performShareTelemetryRequest(t, "abs-free-token",
		`{"reported_total_bytes": 500, "install_id": "inst-1", "up": 10, "down": 20}`, nil))
	if data.Duplicate || data.AcceptedBytes != 30 || data.ReportedTrafficBytes != 30 || data.DeviceStatus != "unbound" {
		t.Fatalf("expected diff semantics without a device, got %+v", data)
	}
	if data.DeviceReportedTrafficBytes != 0 {
		t.Fatalf("expected no device counter, got %d", data.DeviceReportedTrafficBytes)
	}
}

// TestShareTelemetryReportIDDeduplicatesDiffReports covers report_id retries
// in the diff mode: the same id inside the TTL window is a duplicate, a new
// id is accepted.
func TestShareTelemetryReportIDDeduplicatesDiffReports(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "report-id-sub", "report-id-token", "Report ID Node", models.ShareGroup{
		TelemetryEnabled: true,
	})

	first := decodeShareTelemetryResponse(t, performShareTelemetryRequest(t, "report-id-token",
		`{"up": 5, "down": 5, "report_id": "r-1"}`, nil))
	if first.Duplicate || first.AcceptedBytes != 10 || first.ReportedTrafficBytes != 10 {
		t.Fatalf("unexpected first report: %+v", first)
	}

	replay := decodeShareTelemetryResponse(t, performShareTelemetryRequest(t, "report-id-token",
		`{"up": 5, "down": 5, "report_id": "r-1"}`, nil))
	if !replay.Duplicate || replay.AcceptedBytes != 0 || replay.ReportedTrafficBytes != 10 {
		t.Fatalf("expected a duplicate report_id, got %+v", replay)
	}

	next := decodeShareTelemetryResponse(t, performShareTelemetryRequest(t, "report-id-token",
		`{"up": 5, "down": 5, "report_id": "r-2"}`, nil))
	if next.Duplicate || next.AcceptedBytes != 10 || next.ReportedTrafficBytes != 20 {
		t.Fatalf("expected a fresh report id to be accepted, got %+v", next)
	}
}
