package api

import (
	"fmt"
	"net/http"
	"testing"

	"sublink/models"
)

// TestShareUsageHeaderReportsShareQuota proves that a managed link answers
// subscription-userinfo from its own quota snapshot, not from airports.
func TestShareUsageHeaderReportsShareQuota(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "usage-sub", "usage-token", "Usage Node", models.ShareGroup{
		TrafficLimitBytes: 5000,
		ExpireType:        models.ExpireTypeDays,
		ExpireDays:        14,
	})
	if err := share.AddReportedTraffic(1234); err != nil {
		t.Fatalf("report traffic: %v", err)
	}
	share, err := models.GetSubscriptionShareByToken("usage-token")
	if err != nil {
		t.Fatalf("reload share: %v", err)
	}

	wantExpire := share.CreatedAt.AddDate(0, 0, 14).Unix()
	wantHeader := fmt.Sprintf("upload=1234; download=0; total=5000; expire=%d", wantExpire)

	for _, method := range []string{http.MethodGet, http.MethodHead} {
		recorder := performClientRequestWithHeaders(t, method, "/c/?token=usage-token&client=v2ray", map[string]string{
			"X-HWID":      "hwid-usage",
			"X-Device-ID": "device-usage",
		})
		if got := recorder.Header().Get("subscription-userinfo"); got != wantHeader {
			t.Fatalf("%s subscription-userinfo=%q, want %q", method, got, wantHeader)
		}
	}
}

// TestShareUsageHeaderFallsBackToAirports covers links without a limit and
// without an expiry: they keep the legacy airport aggregate header.
func TestShareUsageHeaderFallsBackToAirports(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "free-sub", "free-token", "Free Node", models.ShareGroup{})
	createClientSubscriptionFixture(t, clashTemplatePath, surgeTemplatePath, "legacy-usage-sub", "legacy-usage-token", "Legacy Usage Node")

	wantHeader := "upload=0; download=0; total=0; expire=0"
	for _, token := range []string{"free-token", "legacy-usage-token"} {
		recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token="+token+"&client=v2ray", map[string]string{
			"X-HWID":      "hwid-free",
			"X-Device-ID": "device-free",
		})
		if got := recorder.Header().Get("subscription-userinfo"); got != wantHeader {
			t.Fatalf("%s subscription-userinfo=%q, want %q", token, got, wantHeader)
		}
	}
}

// TestShareUsageHeaderOnQuotaStub proves that stub responses keep the share
// quota header so SNKP can show the reason together with real numbers.
func TestShareUsageHeaderOnQuotaStub(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "stub-usage-sub", "stub-usage-token", "Stub Usage Node", models.ShareGroup{
		TrafficLimitBytes: 5000,
	})
	if err := share.AddReportedTraffic(6000); err != nil {
		t.Fatalf("report traffic: %v", err)
	}

	recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=stub-usage-token&client=v2ray", map[string]string{
		"X-HWID":      "hwid-stub-usage",
		"X-Device-ID": "device-stub-usage",
	})
	if got := recorder.Header().Get("X-Sublink-Reason"); got != models.ShareStubCaseQuotaExhausted {
		t.Fatalf("expected quota stub reason, got %q", got)
	}
	wantHeader := "upload=6000; download=0; total=5000; expire=0"
	if got := recorder.Header().Get("subscription-userinfo"); got != wantHeader {
		t.Fatalf("stub subscription-userinfo=%q, want %q", got, wantHeader)
	}
}
