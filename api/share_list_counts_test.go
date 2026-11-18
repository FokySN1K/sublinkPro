package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"sublink/models"

	"github.com/gin-gonic/gin"
)

type shareListCountsResponse struct {
	Code int `json:"code"`
	Data []struct {
		ID                   int    `json:"id"`
		Name                 string `json:"name"`
		ActiveDeviceCount    int    `json:"active_device_count"`
		PendingDeviceCount   int    `json:"pending_device_count"`
		ReportedTrafficBytes int64  `json:"reported_traffic_bytes"`
		TrafficLimitBytes    int64  `json:"traffic_limit_bytes"`
	} `json:"data"`
}

type shareListPageCountsResponse struct {
	Code int `json:"code"`
	Data struct {
		Items []struct {
			ID                 int   `json:"id"`
			ActiveDeviceCount  int   `json:"active_device_count"`
			PendingDeviceCount int   `json:"pending_device_count"`
			TrafficLimitBytes  int64 `json:"traffic_limit_bytes"`
		} `json:"items"`
		Total int `json:"total"`
	} `json:"data"`
}

func performShareListRequest(t *testing.T, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/v1/shares/get?"+query, nil)
	ShareGet(ginContext)
	return recorder
}

func decodeShareListCounts(t *testing.T, recorder *httptest.ResponseRecorder) shareListCountsResponse {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	var payload shareListCountsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode share list: %v body=%q", err, recorder.Body.String())
	}
	return payload
}

// TestShareListIncludesDeviceCounts proves that /shares/get decorates every
// link with active/pending device counters while keeping the stored fields.
func TestShareListIncludesDeviceCounts(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	sub, firstShare, group := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "counts-sub", "counts-token-1", "Counts Node", models.ShareGroup{
		TrafficLimitBytes: 4096,
		DeviceMode:        models.ShareDeviceModeOpen,
	})

	secondShare := models.SubscriptionShare{
		SubscriptionID: sub.ID,
		Token:          "counts-token-2",
		Name:           "counts second link",
		ExpireType:     models.ExpireTypeNever,
		Enabled:        true,
	}
	if err := secondShare.Add(); err != nil {
		t.Fatalf("add second share: %v", err)
	}

	for _, fixture := range []struct {
		shareID  int
		hwid     string
		deviceID string
		status   string
	}{
		{firstShare.ID, "hwid-count-active-1", "device-active-1", models.ShareDeviceStatusActive},
		{firstShare.ID, "hwid-count-active-2", "device-active-2", models.ShareDeviceStatusActive},
		{firstShare.ID, "hwid-count-pending", "device-pending", models.ShareDeviceStatusPending},
		{firstShare.ID, "hwid-count-blocked", "device-blocked", models.ShareDeviceStatusBlocked},
		{secondShare.ID, "hwid-count-second", "device-second", models.ShareDeviceStatusActive},
	} {
		if _, err := models.EnsureShareDevice(fixture.shareID, fixture.hwid, fixture.deviceID, "", "", "", fixture.status); err != nil {
			t.Fatalf("bind device %s: %v", fixture.deviceID, err)
		}
	}
	if err := firstShare.AddReportedTraffic(1024); err != nil {
		t.Fatalf("report traffic: %v", err)
	}

	payload := decodeShareListCounts(t, performShareListRequest(t, "subId="+strconv.Itoa(sub.ID)))
	if len(payload.Data) != 2 {
		t.Fatalf("expected two links, got %+v", payload.Data)
	}
	positions := make(map[int]int, len(payload.Data))
	for i, item := range payload.Data {
		positions[item.ID] = i
	}

	first := payload.Data[positions[firstShare.ID]]
	if first.ActiveDeviceCount != 2 || first.PendingDeviceCount != 1 {
		t.Fatalf("first link counts = %d active / %d pending, want 2/1", first.ActiveDeviceCount, first.PendingDeviceCount)
	}
	if first.ReportedTrafficBytes != 1024 || first.TrafficLimitBytes != 4096 {
		t.Fatalf("first link telemetry = %d / %d, want 1024 / 4096", first.ReportedTrafficBytes, first.TrafficLimitBytes)
	}
	if first.Name == "" {
		t.Fatal("expected stored share fields to stay in the payload")
	}

	second := payload.Data[positions[secondShare.ID]]
	if second.ActiveDeviceCount != 1 || second.PendingDeviceCount != 0 {
		t.Fatalf("second link counts = %d active / %d pending, want 1/0", second.ActiveDeviceCount, second.PendingDeviceCount)
	}

	// The paginated group view uses the same decoration.
	recorder := performShareListRequest(t, "subId="+strconv.Itoa(sub.ID)+"&groupId="+strconv.Itoa(group.ID)+"&page=1&pageSize=10")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 from paginated list, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	var page shareListPageCountsResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &page); err != nil {
		t.Fatalf("decode paginated share list: %v body=%q", err, recorder.Body.String())
	}
	if page.Data.Total != 1 || len(page.Data.Items) != 1 {
		t.Fatalf("expected one grouped link, got %+v", page.Data)
	}
	item := page.Data.Items[0]
	if item.ID != firstShare.ID || item.ActiveDeviceCount != 2 || item.PendingDeviceCount != 1 {
		t.Fatalf("paginated item = %+v, want link %d with 2 active / 1 pending", item, firstShare.ID)
	}
	if item.TrafficLimitBytes != 4096 {
		t.Fatalf("paginated item limit = %d, want 4096", item.TrafficLimitBytes)
	}
}
