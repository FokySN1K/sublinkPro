package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"sublink/models"

	"github.com/gin-gonic/gin"
)

// requestShareStatus performs GET /c/status for one token as an SNKP client.
func requestShareStatus(t *testing.T, token string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/c/status?token="+token, nil)
	ginContext.Request.Header.Set("X-HWID", "hwid-status-share")
	ginContext.Request.Header.Set("X-Device-ID", "device-status-share")
	ginContext.Request.Header.Set("User-Agent", "SNKP/1.0")
	ShareStatus(ginContext)
	return recorder
}

type shareStatusTestPayload struct {
	Code int `json:"code"`
	Data struct {
		Valid bool `json:"valid"`
		Share struct {
			TrafficLimitBytes    int64  `json:"traffic_limit_bytes"`
			ReportedTrafficBytes int64  `json:"reported_traffic_bytes"`
			RemainingBytes       int64  `json:"remaining_bytes"`
			QuotaExhausted       bool   `json:"quota_exhausted"`
			ExpireAtUnix         int64  `json:"expire_at_unix"`
			CreatedAt            string `json:"created_at"`
		} `json:"share"`
		Telemetry struct {
			Enabled               bool `json:"enabled"`
			Sessions              int  `json:"sessions"`
			ReportIntervalSeconds int  `json:"report_interval_seconds"`
		} `json:"telemetry"`
	} `json:"data"`
}

func decodeShareStatus(t *testing.T, recorder *httptest.ResponseRecorder) shareStatusTestPayload {
	t.Helper()
	var payload shareStatusTestPayload
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status payload: %v body=%q", err, recorder.Body.String())
	}
	if payload.Code != http.StatusOK {
		t.Fatalf("expected code 200, got %d body=%q", payload.Code, recorder.Body.String())
	}
	return payload
}

// TestShareStatusReportsShareQuota covers the quota fields SNKP reads from
// /c/status together with the telemetry reporting interval.
func TestShareStatusReportsShareQuota(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	expireAt := time.Now().Add(72 * time.Hour).UTC().Truncate(time.Second)
	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "status-share-sub", "status-share-token", "Status Share Node", models.ShareGroup{
		TelemetryEnabled:       true,
		TrafficLimitBytes:      5000,
		ExpireType:             models.ExpireTypeDateTime,
		ExpireAt:               &expireAt,
		ReportIntervalSessions: 450,
	})
	if err := share.AddReportedTraffic(1500); err != nil {
		t.Fatalf("report traffic: %v", err)
	}

	payload := decodeShareStatus(t, requestShareStatus(t, "status-share-token"))
	if !payload.Data.Valid {
		t.Fatalf("expected a valid status, got %+v", payload.Data)
	}
	if payload.Data.Share.TrafficLimitBytes != 5000 || payload.Data.Share.ReportedTrafficBytes != 1500 {
		t.Fatalf("unexpected quota snapshot: %+v", payload.Data.Share)
	}
	if payload.Data.Share.RemainingBytes != 3500 || payload.Data.Share.QuotaExhausted {
		t.Fatalf("unexpected remaining quota: %+v", payload.Data.Share)
	}
	if payload.Data.Share.ExpireAtUnix != expireAt.Unix() {
		t.Fatalf("expire_at_unix=%d, want %d", payload.Data.Share.ExpireAtUnix, expireAt.Unix())
	}
	if payload.Data.Share.CreatedAt == "" {
		t.Fatal("expected created_at to be present")
	}
	if !payload.Data.Telemetry.Enabled || payload.Data.Telemetry.Sessions != 450 || payload.Data.Telemetry.ReportIntervalSeconds != 450 {
		t.Fatalf("unexpected telemetry snapshot: %+v", payload.Data.Telemetry)
	}

	if err := share.AddReportedTraffic(4000); err != nil {
		t.Fatalf("report traffic past the limit: %v", err)
	}
	exhausted := decodeShareStatus(t, requestShareStatus(t, "status-share-token"))
	if !exhausted.Data.Share.QuotaExhausted || exhausted.Data.Share.RemainingBytes != 0 {
		t.Fatalf("expected an exhausted quota, got %+v", exhausted.Data.Share)
	}
}
