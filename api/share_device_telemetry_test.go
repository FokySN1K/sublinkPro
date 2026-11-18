package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"sublink/models"

	"github.com/gin-gonic/gin"
)

type shareDeviceOverviewResponse struct {
	Code int `json:"code"`
	Data struct {
		Items []struct {
			HWID                 string `json:"hwid"`
			DeviceID             string `json:"device_id"`
			Status               string `json:"status"`
			ShareCount           int    `json:"share_count"`
			ReportedTrafficBytes int64  `json:"reported_traffic_bytes"`
			WindowTrafficBytes   int64  `json:"window_traffic_bytes"`
			Shares               []struct {
				ShareID          int    `json:"share_id"`
				ShareName        string `json:"share_name"`
				SubscriptionName string `json:"subscription_name"`
			} `json:"shares"`
		} `json:"items"`
		Total   int `json:"total"`
		Summary struct {
			Total  int `json:"total"`
			Active int `json:"active"`
		} `json:"summary"`
		WindowDays int `json:"window_days"`
	} `json:"data"`
}

func performShareDeviceOverviewRequest(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/shares/devices/overview?"+query, nil)
	ShareDeviceOverview(ginContext)
	return recorder
}

func decodeShareDeviceOverview(t *testing.T, recorder *httptest.ResponseRecorder) shareDeviceOverviewResponse {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	var payload shareDeviceOverviewResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode overview response: %v body=%q", err, recorder.Body.String())
	}
	return payload
}

func TestShareDeviceTelemetryAttributesTraffic(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "device-traffic-sub", "device-traffic-token", "Device Traffic Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})

	profile := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=device-traffic-token&client=v2ray", map[string]string{
		"X-HWID":      "hwid-traffic",
		"X-Device-ID": "device-traffic",
	})
	secret := profile.Header().Get("X-Sublink-Device-Secret")
	if secret == "" {
		t.Fatal("expected a device secret after auto-binding")
	}

	report := performShareTelemetryRequest(t, "device-traffic-token", `{"up": 100, "down": 200}`, map[string]string{
		"X-Sublink-Device-Secret": secret,
	})
	if report.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", report.Code, report.Body.String())
	}

	device, err := models.FindShareDeviceBySecret(share.ID, secret)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if device.ReportedTrafficBytes != 300 {
		t.Fatalf("expected 300 device bytes, got %d", device.ReportedTrafficBytes)
	}
	if device.LastReportAt == nil {
		t.Fatal("expected last_report_at to be recorded")
	}

	// Two reports inside the same hour merge into one bucket, a report in a
	// later hour opens a new bucket.
	base := time.Now().UTC().Add(-3 * time.Hour).Truncate(time.Hour)
	for _, report := range []struct {
		up, down int64
		at       time.Time
	}{
		{10, 20, base.Add(10 * time.Minute)},
		{5, 5, base.Add(40 * time.Minute)},
		{1, 1, base.Add(2 * time.Hour)},
	} {
		if err := models.RecordShareDeviceTraffic(device, report.up, report.down, report.at); err != nil {
			t.Fatalf("record device traffic: %v", err)
		}
	}

	sums, err := models.SumShareDeviceUsage([]int{device.ID}, time.Time{})
	if err != nil {
		t.Fatalf("sum device usage: %v", err)
	}
	if sums[device.ID] != 342 {
		t.Fatalf("expected 342 window bytes, got %d", sums[device.ID])
	}

	buckets, err := models.ListShareDeviceUsage([]int{device.ID}, time.Time{})
	if err != nil {
		t.Fatalf("list device usage: %v", err)
	}
	if len(buckets) != 3 {
		t.Fatalf("expected 3 hourly buckets, got %d: %+v", len(buckets), buckets)
	}
	if !buckets[0].WindowStart.Equal(base) || buckets[0].Bytes != 40 {
		t.Fatalf("expected the same-hour reports to merge into 40 bytes, got %+v", buckets[0])
	}
}

func TestShareDeviceOverviewAggregatesInstallations(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	sub, firstShare, group := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "overview-sub", "overview-token-1", "Overview Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})

	secondShare := models.SubscriptionShare{
		SubscriptionID: sub.ID,
		Token:          "overview-token-2",
		Name:           "overview second share",
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

	for _, token := range []string{"overview-token-1", "overview-token-2"} {
		profile := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token="+token+"&client=v2ray", map[string]string{
			"X-HWID":            "hwid-overview",
			"X-Device-ID":       "device-overview",
			"X-Device-Name":     "Pixel 9",
			"X-Device-Platform": "android",
		})
		if profile.Code != http.StatusOK {
			t.Fatalf("expected 200 from %s, got %d", token, profile.Code)
		}
	}

	device, err := models.FindShareDevice(firstShare.ID, "hwid-overview", "device-overview")
	if err != nil {
		t.Fatalf("find device on first share: %v", err)
	}
	if err := models.RecordShareDeviceTraffic(device, 120, 80, time.Now()); err != nil {
		t.Fatalf("record device traffic: %v", err)
	}

	payload := decodeShareDeviceOverview(t, performShareDeviceOverviewRequest(t, "subId="+strconv.Itoa(sub.ID)))
	if payload.Data.Total != 1 || len(payload.Data.Items) != 1 {
		t.Fatalf("expected one aggregated device, got %+v", payload.Data)
	}
	item := payload.Data.Items[0]
	if item.HWID != "hwid-overview" || item.DeviceID != "device-overview" {
		t.Fatalf("unexpected identity: %+v", item)
	}
	if item.ShareCount != 2 || len(item.Shares) != 2 {
		t.Fatalf("expected two link bindings, got %+v", item)
	}
	for _, link := range item.Shares {
		if link.SubscriptionName != "overview-sub" {
			t.Fatalf("expected node pool name on the link entry, got %q", link.SubscriptionName)
		}
	}
	if item.ReportedTrafficBytes != 200 || item.WindowTrafficBytes != 200 {
		t.Fatalf("expected 200 attributed bytes, got %+v", item)
	}
	if payload.Data.WindowDays != models.ShareDeviceUsageWindowDays {
		t.Fatalf("expected window days %d, got %d", models.ShareDeviceUsageWindowDays, payload.Data.WindowDays)
	}

	pending := decodeShareDeviceOverview(t, performShareDeviceOverviewRequest(t, "subId="+strconv.Itoa(sub.ID)+"&status=pending"))
	if pending.Data.Total != 0 {
		t.Fatalf("expected no pending devices, got %d", pending.Data.Total)
	}
	keyword := decodeShareDeviceOverview(t, performShareDeviceOverviewRequest(t, "subId="+strconv.Itoa(sub.ID)+"&keyword=second"))
	if keyword.Data.Total != 1 {
		t.Fatalf("expected the keyword to match the second link, got %d", keyword.Data.Total)
	}
}

func TestShareDeviceDetailReturnsHourlyAndActivity(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "detail-sub", "detail-token", "Detail Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})

	profile := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=detail-token&client=v2ray", map[string]string{
		"X-HWID":      "hwid-detail",
		"X-Device-ID": "device-detail",
	})
	secret := profile.Header().Get("X-Sublink-Device-Secret")
	if secret == "" {
		t.Fatal("expected a device secret after auto-binding")
	}
	report := performShareTelemetryRequest(t, "detail-token", `{"up": 5, "down": 7}`, map[string]string{
		"X-Sublink-Device-Secret": secret,
	})
	if report.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", report.Code, report.Body.String())
	}
	models.WaitForPendingShareAccessLogs()

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/shares/devices/detail?hwid=hwid-detail&deviceId=device-detail", nil)
	ShareDeviceDetail(ginContext)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Device struct {
				HWID       string `json:"hwid"`
				ShareCount int    `json:"share_count"`
				Shares     []struct {
					ShareName        string `json:"share_name"`
					SubscriptionName string `json:"subscription_name"`
				} `json:"shares"`
			} `json:"device"`
			Hourly []struct {
				Bytes int64 `json:"bytes"`
			} `json:"hourly"`
			Activity []struct {
				Action string `json:"action"`
			} `json:"activity"`
			WindowDays int `json:"window_days"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode detail response: %v body=%q", err, recorder.Body.String())
	}
	if payload.Data.Device.HWID != "hwid-detail" || payload.Data.Device.ShareCount != 1 {
		t.Fatalf("unexpected device payload: %+v", payload.Data.Device)
	}
	if link := payload.Data.Device.Shares[0]; link.SubscriptionName != "detail-sub" || link.ShareName == "" {
		t.Fatalf("expected link metadata, got %+v", link)
	}
	if len(payload.Data.Hourly) != 1 || payload.Data.Hourly[0].Bytes != 12 {
		t.Fatalf("expected one hourly bucket with 12 bytes, got %+v", payload.Data.Hourly)
	}
	if len(payload.Data.Activity) == 0 || payload.Data.Activity[0].Action != models.ShareAccessActionTelemetry {
		t.Fatalf("expected telemetry activity, got %+v", payload.Data.Activity)
	}

	missing := httptest.NewRecorder()
	missingContext, _ := gin.CreateTestContext(missing)
	missingContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/shares/devices/detail?hwid=missing&deviceId=missing", nil)
	ShareDeviceDetail(missingContext)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for an unknown device, got %d", missing.Code)
	}
}
