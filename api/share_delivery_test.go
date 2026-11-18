package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"sublink/models"
	"sublink/utils"

	"github.com/gin-gonic/gin"
)

func performClientRequestWithHeaders(t *testing.T, method, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), method, path, nil)
	for key, value := range headers {
		ginContext.Request.Header.Set(key, value)
	}
	GetClient(ginContext)
	return recorder
}

// createManagedShareFixture builds a node pool, a share group bound to it and a
// subscription link that inherited the group snapshot.
func createManagedShareFixture(t *testing.T, clashTemplatePath, surgeTemplatePath, subName, token, linkName string, group models.ShareGroup) (*models.Subcription, *models.SubscriptionShare, *models.ShareGroup) {
	t.Helper()

	createClientSubscriptionFixture(t, clashTemplatePath, surgeTemplatePath, subName, token, linkName)

	var sub models.Subcription
	sub.Name = subName
	if err := sub.Find(); err != nil {
		t.Fatalf("find subscription %s: %v", subName, err)
	}

	if group.Name == "" {
		group.Name = subName + " group"
	}
	group.SubscriptionID = sub.ID
	if group.Templates == "" {
		group.Templates = models.DefaultShareTemplates
	}
	group.MihomoSnkp = true
	group.AllowLegacyClients = true
	group.Enabled = true
	if err := group.Add(); err != nil {
		t.Fatalf("add share group for %s: %v", subName, err)
	}

	share, err := models.GetSubscriptionShareByToken(token)
	if err != nil {
		t.Fatalf("find share %s: %v", token, err)
	}
	share.ApplyGroupSnapshot(&group)
	if err := share.Update(); err != nil {
		t.Fatalf("attach share %s to group: %v", token, err)
	}
	share, err = models.GetSubscriptionShareByToken(token)
	if err != nil {
		t.Fatalf("reload share %s: %v", token, err)
	}
	return &sub, share, &group
}

func TestResolveShareClientTypeManagedGroup(t *testing.T) {
	gin.SetMode(gin.TestMode)

	group := &models.ShareGroup{Templates: models.DefaultShareTemplates, MihomoSnkp: true}
	hiddenGroup := &models.ShareGroup{Templates: models.DefaultShareTemplates, MihomoSnkp: true, HideConfig: true}

	tests := []struct {
		name   string
		group  *models.ShareGroup
		query  string
		ua     string
		marker string
		want   string
	}{
		{name: "auto unknown app falls back to snkp", group: group, want: models.ShareTemplateMihomoSnkp},
		{name: "auto clash ua", group: group, ua: "ClashMeta/1.19.0", want: models.ShareTemplateMihomo},
		{name: "auto surge ua", group: group, ua: "Surge iOS/2900", want: models.ShareTemplateSurge},
		{name: "auto v2ray ua", group: group, ua: "v2rayN/6.31", want: models.ShareTemplateV2Ray},
		{name: "auto snkp ua", group: group, ua: "SNKP/1.0 (Android)", want: models.ShareTemplateMihomoSnkp},
		{name: "auto snkp marker", group: group, marker: "snkp", want: models.ShareTemplateMihomoSnkp},
		{name: "mihomo request from snkp", group: group, query: "mihomo", ua: "SNKP/1.0", want: models.ShareTemplateMihomoSnkp},
		{name: "mihomo request from clash", group: group, query: "mihomo", ua: "ClashMeta/1.19.0", want: models.ShareTemplateMihomo},
		{name: "explicit surge", group: group, query: "surge", want: models.ShareTemplateSurge},
		{name: "hide config forces snkp", group: hiddenGroup, query: "surge", want: models.ShareTemplateMihomoSnkp},
		{name: "legacy link keeps v2ray fallback", group: nil, want: models.ShareTemplateV2Ray},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ginContext, _ := gin.CreateTestContext(recorder)
			path := "/c/?token=test"
			if tt.query != "" {
				path += "&client=" + tt.query
			}
			ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
			if tt.ua != "" {
				ginContext.Request.Header.Set("User-Agent", tt.ua)
			}
			if tt.marker != "" {
				ginContext.Request.Header.Set("X-Sublink-Client", tt.marker)
			}

			if got := resolveShareClientType(ginContext, tt.group); got != tt.want {
				t.Fatalf("resolveShareClientType()=%q, want %q", got, tt.want)
			}
		})
	}
}

func TestShareGroupSnapshotInheritedByLink(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, group := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "snapshot-sub", "snapshot-token", "Snapshot Node", models.ShareGroup{
		MaxDevices:                2,
		TrafficLimitBytes:         5 * 1024 * 1024 * 1024,
		ExpireType:                models.ExpireTypeDays,
		ExpireDays:                14,
		ReportIntervalSessions:    600,
		ReportIntervalNodes:       1200,
		ReportIntervalReliability: 1800,
	})

	if share.GroupID == nil || *share.GroupID != group.ID {
		t.Fatalf("expected link to reference group %d, got %v", group.ID, share.GroupID)
	}
	if share.MaxDevices != 2 {
		t.Fatalf("expected snapshot max devices 2, got %d", share.MaxDevices)
	}
	if share.TrafficLimitBytes != 5*1024*1024*1024 {
		t.Fatalf("unexpected snapshot traffic limit %d", share.TrafficLimitBytes)
	}
	if share.ExpireType != models.ExpireTypeDays || share.ExpireDays != 14 {
		t.Fatalf("unexpected snapshot expiry type=%d days=%d", share.ExpireType, share.ExpireDays)
	}
	if share.ReportInterval(models.ShareTelemetryChannelSessions) != 600 ||
		share.ReportInterval(models.ShareTelemetryChannelNodes) != 1200 ||
		share.ReportInterval(models.ShareTelemetryChannelReliability) != 1800 {
		t.Fatalf("unexpected snapshot telemetry intervals: %+v", share)
	}
}

func TestDisabledShareGroupServesGroupDisabledStub(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, _, group := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "disabled-group-sub", "disabled-group-token", "Disabled Group Node", models.ShareGroup{})
	group.Enabled = false
	if err := group.Update(); err != nil {
		t.Fatalf("disable share group: %v", err)
	}

	recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=disabled-group-token&client=v2ray", map[string]string{
		"X-HWID":      "hwid-disabled",
		"X-Device-ID": "device-disabled",
	})

	if got := recorder.Header().Get("X-Sublink-Reason"); got != models.ShareStubCaseGroupDisabled {
		t.Fatalf("expected reason header %q, got %q", models.ShareStubCaseGroupDisabled, got)
	}
	decoded := strings.TrimSpace(utils.Base64Decode(recorder.Body.String()))
	if !strings.Contains(decoded, "temporarily disabled") {
		t.Fatalf("expected group-disabled stub in payload, got %q", decoded)
	}
}

func TestHideConfigGroupServesUnsupportedAppStubForNonSnkp(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "hidden-sub", "hidden-token", "Hidden Node", models.ShareGroup{
		HideConfig: true,
	})

	recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=hidden-token&client=clash", map[string]string{
		"User-Agent":  "ClashMeta/1.19.0",
		"X-HWID":      "hwid-hidden",
		"X-Device-ID": "device-hidden",
	})

	if got := recorder.Header().Get("X-Sublink-Reason"); got != models.ShareStubCaseUnsupportedApp {
		t.Fatalf("expected reason header %q, got %q", models.ShareStubCaseUnsupportedApp, got)
	}
	if got := recorder.Header().Get("X-Sublink-Template"); got != models.ShareTemplateMihomoSnkp {
		t.Fatalf("expected template header %q, got %q", models.ShareTemplateMihomoSnkp, got)
	}
	if strings.Contains(recorder.Body.String(), "Hidden Node") {
		t.Fatal("hide-config group leaked the node list to a non-SNKP client")
	}
}

func TestExpiredStubOverrideFromGroupIsRendered(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, _, group := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "stub-sub", "stub-token", "Stub Node", models.ShareGroup{})
	expireClientSubscriptionShare(t, "stub-token")

	stub := models.ShareStub{
		Name:    "Custom expired",
		CaseKey: models.ShareStubCaseExpired,
		Title:   "Custom expiry title",
		Content: "Custom expired text for {share_name}",
		Enabled: true,
	}
	if err := stub.Add(); err != nil {
		t.Fatalf("add custom stub: %v", err)
	}
	if err := models.SetShareGroupStubs(group.ID, map[string]int{models.ShareStubCaseExpired: stub.ID}); err != nil {
		t.Fatalf("map group stub: %v", err)
	}

	recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=stub-token&client=v2ray", map[string]string{
		"X-HWID":      "hwid-stub",
		"X-Device-ID": "device-stub",
	})

	if got := recorder.Header().Get("X-Sublink-Reason"); got != models.ShareStubCaseExpired {
		t.Fatalf("expected expired reason header, got %q", got)
	}
	decoded := strings.TrimSpace(utils.Base64Decode(recorder.Body.String()))
	if !strings.Contains(decoded, "Custom expired text for stub-sub share") {
		t.Fatalf("expected rendered group stub, got %q", decoded)
	}
	if !strings.Contains(recorder.Header().Get("Content-Disposition"), "stub-sub.txt") {
		t.Fatalf("expected expired link to keep the pool filename, got %q", recorder.Header().Get("Content-Disposition"))
	}
}

func TestShareDeviceAutoBindEnforcesLimit(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "device-sub", "device-token", "Device Node", models.ShareGroup{
		DeviceMode: models.ShareDeviceModeAutoBind,
		MaxDevices: 1,
	})

	first := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=device-token&client=v2ray", map[string]string{
		"X-HWID":            "hwid-owner",
		"X-Device-ID":       "device-one",
		"X-Device-Name":     "Pixel 8",
		"X-Device-Platform": "android",
	})
	if got := first.Header().Get("X-Sublink-Device-Status"); got != models.ShareDeviceStatusActive {
		t.Fatalf("expected first device to be active, got %q", got)
	}
	if first.Header().Get("X-Sublink-Device-Secret") == "" {
		t.Fatal("expected a device secret header for the bound device")
	}

	second := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=device-token&client=v2ray", map[string]string{
		"X-HWID":      "hwid-owner",
		"X-Device-ID": "device-two",
	})
	if got := second.Header().Get("X-Sublink-Reason"); got != models.ShareStubCaseDeviceLimit {
		t.Fatalf("expected device-limit stub, got reason %q", got)
	}

	active, err := models.CountActiveShareDevices(share.ID)
	if err != nil {
		t.Fatalf("count active devices: %v", err)
	}
	if active != 1 {
		t.Fatalf("expected exactly one active device, got %d", active)
	}

	devices, err := models.ListShareDevices(share.ID)
	if err != nil {
		t.Fatalf("list devices: %v", err)
	}
	if len(devices) != 1 {
		t.Fatalf("expected the rejected device not to be stored, got %d devices", len(devices))
	}
	if devices[0].Name != "Pixel 8" || devices[0].Platform != "android" {
		t.Fatalf("unexpected device metadata: %+v", devices[0])
	}
}

func TestShareDeviceApproveModeKeepsDevicePending(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "approve-sub", "approve-token", "Approve Node", models.ShareGroup{
		DeviceMode: models.ShareDeviceModeApprove,
		MaxDevices: 1,
	})

	recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=approve-token&client=v2ray", map[string]string{
		"X-HWID":      "hwid-approve",
		"X-Device-ID": "device-approve",
	})

	if got := recorder.Header().Get("X-Sublink-Reason"); got != models.ShareStubCaseDevicePending {
		t.Fatalf("expected device-pending stub, got reason %q", got)
	}
	if got := recorder.Header().Get("X-Sublink-Device-Status"); got != models.ShareDeviceStatusPending {
		t.Fatalf("expected pending device status header, got %q", got)
	}

	active, err := models.CountActiveShareDevices(share.ID)
	if err != nil {
		t.Fatalf("count active devices: %v", err)
	}
	if active != 0 {
		t.Fatalf("pending devices must not consume active slots, got %d", active)
	}
}

func TestLegacyLinkIgnoresGroupPolicy(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createClientSubscriptionFixture(t, clashTemplatePath, surgeTemplatePath, "legacy-sub", "legacy-token", "Legacy Node")

	recorder := performClientRequest(t, http.MethodGet, "/c/?token=legacy-token&client=v2ray")

	if recorder.Body.String() == "" {
		t.Fatal("expected a rendered legacy subscription")
	}
	if got := recorder.Header().Get("X-Sublink-Device-Status"); got != "" {
		t.Fatalf("legacy links must not report device status, got %q", got)
	}
	decoded := strings.TrimSpace(utils.Base64Decode(recorder.Body.String()))
	if !strings.Contains(decoded, "Legacy Node") {
		t.Fatalf("expected the real node list for a legacy link, got %q", decoded)
	}
}

func TestShareStatusReportsPolicyAndDeviceState(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, _, _ = createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "status-sub", "status-token", "Status Node", models.ShareGroup{
		DeviceMode:                models.ShareDeviceModeAutoBind,
		MaxDevices:                3,
		TelemetryEnabled:          true,
		ReportIntervalSessions:    900,
		ReportIntervalNodes:       1800,
		ReportIntervalReliability: 2700,
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/c/status?token=status-token", nil)
	ginContext.Request.Header.Set("X-HWID", "hwid-status")
	ginContext.Request.Header.Set("X-Device-ID", "device-status")
	ginContext.Request.Header.Set("User-Agent", "SNKP/1.0")
	ShareStatus(ginContext)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 from /c/status, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Valid    bool   `json:"valid"`
			Reason   string `json:"reason"`
			Template string `json:"template"`
			Share    struct {
				MaxDevices int `json:"max_devices"`
			} `json:"share"`
			Group struct {
				ID          int  `json:"id"`
				HideConfig  bool `json:"hide_config"`
				TelemetryOn bool `json:"telemetry_enabled"`
			} `json:"group"`
			Device struct {
				Status string `json:"status"`
			} `json:"device"`
			Telemetry struct {
				Enabled     bool `json:"enabled"`
				Sessions    int  `json:"sessions"`
				Nodes       int  `json:"nodes"`
				Reliability int  `json:"reliability"`
			} `json:"telemetry"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status payload: %v body=%q", err, recorder.Body.String())
	}
	if !payload.Data.Valid {
		t.Fatalf("expected a valid status, got reason %q", payload.Data.Reason)
	}
	if payload.Data.Template != models.ShareTemplateMihomoSnkp {
		t.Fatalf("expected snkp template for a snkp client, got %q", payload.Data.Template)
	}
	if payload.Data.Group.ID == 0 || payload.Data.Share.MaxDevices != 3 {
		t.Fatalf("unexpected status snapshot: %+v", payload.Data)
	}
	if !payload.Data.Telemetry.Enabled || payload.Data.Telemetry.Sessions != 900 ||
		payload.Data.Telemetry.Nodes != 1800 || payload.Data.Telemetry.Reliability != 2700 {
		t.Fatalf("unexpected telemetry snapshot: %+v", payload.Data.Telemetry)
	}
	if payload.Data.Device.Status != "new" {
		t.Fatalf("status must not bind devices, got status %q", payload.Data.Device.Status)
	}
}
