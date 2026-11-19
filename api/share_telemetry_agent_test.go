package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"sublink/models"
	"sublink/services/scheduler"

	"github.com/gin-gonic/gin"
)

type shareSessionEventTestData struct {
	Event     string `json:"event"`
	SessionID string `json:"session_id"`
	Accepted  bool   `json:"accepted"`
	Duplicate bool   `json:"duplicate"`
	Ignored   bool   `json:"ignored"`
	Session   *struct {
		SessionKey string     `json:"session_key"`
		EndedAt    *time.Time `json:"ended_at"`
		UpBytes    int64      `json:"up_bytes"`
		DownBytes  int64      `json:"down_bytes"`
		Bytes      int64      `json:"bytes"`
		Open       bool       `json:"open"`
	} `json:"session"`
}

type shareNodesTestData struct {
	AcceptedRows int  `json:"accepted_rows"`
	IgnoredRows  int  `json:"ignored_rows"`
	SkippedRows  int  `json:"skipped_rows"`
	Duplicate    bool `json:"duplicate"`
	Ignored      bool `json:"ignored"`
}

type shareReliabilityTestData struct {
	AcceptedRows int  `json:"accepted_rows"`
	SkippedRows  int  `json:"skipped_rows"`
	Events       int  `json:"events"`
	Duplicate    bool `json:"duplicate"`
	Ignored      bool `json:"ignored"`
}

func performShareIngestRequest(t *testing.T, handler gin.HandlerFunc, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodPost, path, strings.NewReader(body))
	ginContext.Request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		ginContext.Request.Header.Set(key, value)
	}
	handler(ginContext)
	return recorder
}

// decodeShareIngestPayload decodes the utils.Response envelope into dst and
// fails the test on a non 200 code.
func decodeShareIngestPayload[T any](t *testing.T, recorder *httptest.ResponseRecorder, dst *T) {
	t.Helper()
	var payload struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data T      `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode ingest response: %v body=%q", err, recorder.Body.String())
	}
	if payload.Code != http.StatusOK {
		t.Fatalf("expected code 200, got %d msg=%q", payload.Code, payload.Msg)
	}
	*dst = payload.Data
}

// bindShareDeviceForTest downloads the profile once so the auto-bind mode
// creates the device row and returns its secret.
func bindShareDeviceForTest(t *testing.T, token, hwid, deviceID string) (string, *models.ShareDevice) {
	t.Helper()
	profile := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token="+token+"&client=v2ray", map[string]string{
		"X-HWID":      hwid,
		"X-Device-ID": deviceID,
	})
	if profile.Code != http.StatusOK {
		t.Fatalf("expected 200 from profile, got %d body=%q", profile.Code, profile.Body.String())
	}
	secret := profile.Header().Get("X-Sublink-Device-Secret")
	if secret == "" {
		t.Fatal("expected a device secret after auto-binding")
	}
	share, err := models.GetSubscriptionShareByToken(token)
	if err != nil {
		t.Fatalf("load share %s: %v", token, err)
	}
	device, err := models.FindShareDeviceBySecret(share.ID, secret)
	if err != nil {
		t.Fatalf("find bound device: %v", err)
	}
	return secret, device
}

func TestShareSessionsMirrorAcceptedTraffic(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "session-sub", "session-token", "Session Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})
	secret, device := bindShareDeviceForTest(t, "session-token", "hwid-session", "device-session")
	headers := map[string]string{"X-Sublink-Device-Secret": secret}

	start := performShareIngestRequest(t, ShareReportSession, "/c/sessions?token=session-token",
		`{"event":"session_start","session_id":"sess-1"}`, headers)
	var startData shareSessionEventTestData
	decodeShareIngestPayload(t, start, &startData)
	if !startData.Accepted || startData.Ignored || startData.Session == nil || !startData.Session.Open {
		t.Fatalf("expected an open session after session_start, got %+v", startData)
	}

	for _, body := range []string{
		`{"up":100,"down":200,"session_id":"sess-1"}`,
		`{"down":50,"session_id":"sess-1"}`,
	} {
		report := performShareTelemetryRequest(t, "session-token", body, headers)
		if report.Code != http.StatusOK {
			t.Fatalf("expected 200 from telemetry, got %d body=%q", report.Code, report.Body.String())
		}
	}

	sessions, err := models.ListShareSessions([]int{share.ID}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected one session, got %+v", sessions)
	}
	session := sessions[0]
	if session.UpBytes != 100 || session.DownBytes != 250 || session.Bytes != 350 {
		t.Fatalf("session must mirror accepted deltas once, got %+v", session)
	}
	if session.Closed || session.EndedAt != nil {
		t.Fatalf("session must stay open before session_stop, got %+v", session)
	}

	// No double counting: the session mirrors the same bytes that already
	// moved the link and device counters.
	storedShare, err := models.GetSubscriptionShareByToken("session-token")
	if err != nil {
		t.Fatalf("reload share: %v", err)
	}
	storedDevice, err := models.GetShareDeviceByID(device.ID)
	if err != nil {
		t.Fatalf("reload device: %v", err)
	}
	if storedShare.ReportedTrafficBytes != 350 || storedDevice.ReportedTrafficBytes != 350 {
		t.Fatalf("expected share and device counters to stay at 350, got share=%d device=%d",
			storedShare.ReportedTrafficBytes, storedDevice.ReportedTrafficBytes)
	}

	stop := performShareIngestRequest(t, ShareReportSession, "/c/sessions?token=session-token",
		`{"event":"session_stop","session_id":"sess-1"}`, headers)
	var stopData shareSessionEventTestData
	decodeShareIngestPayload(t, stop, &stopData)
	if !stopData.Accepted || stopData.Duplicate {
		t.Fatalf("expected the first stop to close the session, got %+v", stopData)
	}

	replay := performShareIngestRequest(t, ShareReportSession, "/c/sessions?token=session-token",
		`{"event":"session_stop","session_id":"sess-1"}`, headers)
	var replayData shareSessionEventTestData
	decodeShareIngestPayload(t, replay, &replayData)
	if !replayData.Duplicate || replayData.Accepted {
		t.Fatalf("expected a retried stop to be reported as a duplicate, got %+v", replayData)
	}

	sessions, err = models.ListShareSessions([]int{share.ID}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("reload sessions: %v", err)
	}
	if len(sessions) != 1 || !sessions[0].Closed || sessions[0].EndedAt == nil {
		t.Fatalf("expected the session to be closed, got %+v", sessions)
	}
}

func TestShareSessionsRequireInstallationIdentity(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "anonymous-session-sub", "anonymous-session-token", "Anonymous Session Node", models.ShareGroup{
		TelemetryEnabled: true,
	})

	recorder := performShareIngestRequest(t, ShareReportSession, "/c/sessions?token=anonymous-session-token",
		`{"event":"session_start","session_id":"anon-1"}`, nil)
	var data shareSessionEventTestData
	decodeShareIngestPayload(t, recorder, &data)
	if !data.Ignored || data.Accepted {
		t.Fatalf("expected a session without identity to be ignored, got %+v", data)
	}
	sessions, err := models.ListShareSessions([]int{share.ID}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Fatalf("expected no session rows, got %+v", sessions)
	}

	invalid := performShareIngestRequest(t, ShareReportSession, "/c/sessions?token=anonymous-session-token",
		`{"event":"session_start"}`, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a missing session_id, got %d", invalid.Code)
	}
	invalid = performShareIngestRequest(t, ShareReportSession, "/c/sessions?token=anonymous-session-token",
		`{"event":"session_pause","session_id":"anon-1"}`, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an unknown event, got %d", invalid.Code)
	}
}

func TestShareNodesDedupesReportsAndMergesKeys(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "nodes-sub", "nodes-token", "Nodes Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})
	secret, _ := bindShareDeviceForTest(t, "nodes-token", "hwid-nodes", "device-nodes")
	headers := map[string]string{"X-Sublink-Device-Secret": secret}

	keyA := strings.Repeat("a", 32)
	keyB := strings.Repeat("b", 32)
	body := fmt.Sprintf(`{"report_id":"nodes-r1","nodes":[`+
		`{"key":%q,"up":10,"down":20},`+
		`{"key":%q,"up":1,"down":2},`+
		`{"key":"","up":5},`+
		`{"key":%q,"up":-1,"down":4}]}`, strings.ToUpper(keyA), keyA, keyB)
	recorder := performShareIngestRequest(t, ShareReportNodes, "/c/nodes?token=nodes-token", body, headers)
	var data shareNodesTestData
	decodeShareIngestPayload(t, recorder, &data)
	if data.AcceptedRows != 1 || data.SkippedRows != 1 || data.IgnoredRows != 2 {
		t.Fatalf("unexpected node report counters: %+v", data)
	}

	replay := performShareIngestRequest(t, ShareReportNodes, "/c/nodes?token=nodes-token", body, headers)
	var replayData shareNodesTestData
	decodeShareIngestPayload(t, replay, &replayData)
	if !replayData.Duplicate || replayData.AcceptedRows != 0 {
		t.Fatalf("expected the retried report to be a duplicate, got %+v", replayData)
	}

	sums, err := models.SumShareNodeUsage([]int{share.ID}, nil, time.Time{})
	if err != nil {
		t.Fatalf("sum node usage: %v", err)
	}
	if len(sums) != 1 || sums[0].NodeKey != keyA || sums[0].Bytes != 33 {
		t.Fatalf("expected one merged node key with 33 bytes, got %+v", sums)
	}

	tooMany := strings.Builder{}
	tooMany.WriteString(`{"nodes":[`)
	for i := 0; i < shareNodeReportMaxRows+1; i++ {
		if i > 0 {
			tooMany.WriteString(",")
		}
		fmt.Fprintf(&tooMany, `{"key":%q,"up":1}`, keyA)
	}
	tooMany.WriteString(`]}`)
	overflow := performShareIngestRequest(t, ShareReportNodes, "/c/nodes?token=nodes-token", tooMany.String(), headers)
	if overflow.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an oversized node report, got %d", overflow.Code)
	}
}

func TestShareNodesIgnoreUnboundInstallations(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "unbound-nodes-sub", "unbound-nodes-token", "Unbound Nodes Node", models.ShareGroup{
		TelemetryEnabled: true,
	})

	recorder := performShareIngestRequest(t, ShareReportNodes, "/c/nodes?token=unbound-nodes-token",
		fmt.Sprintf(`{"nodes":[{"key":%q,"up":10,"down":20}]}`, strings.Repeat("c", 32)), nil)
	var data shareNodesTestData
	decodeShareIngestPayload(t, recorder, &data)
	if !data.Ignored || data.AcceptedRows != 0 {
		t.Fatalf("expected an unbound report to be ignored, got %+v", data)
	}
}

func TestShareReliabilityTracksTransitions(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "reliability-sub", "reliability-token", "Reliability Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})
	secret, _ := bindShareDeviceForTest(t, "reliability-token", "hwid-reliability", "device-reliability")
	headers := map[string]string{"X-Sublink-Device-Secret": secret}
	key := strings.Repeat("d", 32)

	first := performShareIngestRequest(t, ShareReportReliability, "/c/reliability?token=reliability-token",
		fmt.Sprintf(`{"report_id":"rel-1","nodes":[{"key":%q,"score":-50,"fails":3,"streak":2}]}`, key), headers)
	var firstData shareReliabilityTestData
	decodeShareIngestPayload(t, first, &firstData)
	if firstData.AcceptedRows != 1 || firstData.Events != 1 {
		t.Fatalf("expected one accepted row and one transition, got %+v", firstData)
	}

	snapshot, err := models.ListProblemShareNodeReliability([]int{share.ID}, nil)
	if err != nil {
		t.Fatalf("list problem reliability: %v", err)
	}
	if len(snapshot) != 1 || snapshot[0].State != models.ShareReliabilityStateSuspect || snapshot[0].Score != -50 {
		t.Fatalf("expected one suspect snapshot, got %+v", snapshot)
	}

	same := performShareIngestRequest(t, ShareReportReliability, "/c/reliability?token=reliability-token",
		fmt.Sprintf(`{"nodes":[{"key":%q,"score":-50,"fails":3,"streak":2}]}`, key), headers)
	var sameData shareReliabilityTestData
	decodeShareIngestPayload(t, same, &sameData)
	if sameData.AcceptedRows != 1 || sameData.Events != 0 {
		t.Fatalf("expected a repeated snapshot without a transition, got %+v", sameData)
	}

	recovered := performShareIngestRequest(t, ShareReportReliability, "/c/reliability?token=reliability-token",
		fmt.Sprintf(`{"nodes":[{"key":%q,"score":90,"fails":0,"streak":0,"recovered":true}]}`, key), headers)
	var recoveredData shareReliabilityTestData
	decodeShareIngestPayload(t, recovered, &recoveredData)
	if recoveredData.AcceptedRows != 1 || recoveredData.Events != 1 {
		t.Fatalf("expected a recovery transition, got %+v", recoveredData)
	}

	problem, err := models.ListProblemShareNodeReliability([]int{share.ID}, nil)
	if err != nil {
		t.Fatalf("reload problem reliability: %v", err)
	}
	if len(problem) != 0 {
		t.Fatalf("recovered node must leave the problem list, got %+v", problem)
	}
	events, err := models.ListShareReliabilityEvents([]int{share.ID}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("list reliability events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected two transition rows, got %+v", events)
	}
	if events[0].Reason != "recovered" || events[1].ToState != models.ShareReliabilityStateSuspect {
		t.Fatalf("unexpected transition order: %+v", events)
	}
}

func TestShareReliabilityCapsRows(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "reliability-cap-sub", "reliability-cap-token", "Reliability Cap Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})
	secret, _ := bindShareDeviceForTest(t, "reliability-cap-token", "hwid-rel-cap", "device-rel-cap")

	builder := strings.Builder{}
	builder.WriteString(`{"nodes":[`)
	for i := 0; i < shareReliabilityReportMaxRows+1; i++ {
		if i > 0 {
			builder.WriteString(",")
		}
		fmt.Fprintf(&builder, `{"key":%q,"score":-40}`, strings.Repeat("e", 32))
	}
	builder.WriteString(`]}`)
	recorder := performShareIngestRequest(t, ShareReportReliability, "/c/reliability?token=reliability-cap-token",
		builder.String(), map[string]string{"X-Sublink-Device-Secret": secret})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for an oversized reliability report, got %d", recorder.Code)
	}
}

func TestShareTelemetryMaintenanceClosesStaleSessions(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "stale-sub", "stale-token", "Stale Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})
	_, device := bindShareDeviceForTest(t, "stale-token", "hwid-stale", "device-stale")

	staleAt := time.Now().Add(-models.ShareSessionStaleAfter - time.Minute)
	if _, err := models.StartShareSession(share.ID, device, "hwid-stale", "device-stale", "stale-session", staleAt); err != nil {
		t.Fatalf("start stale session: %v", err)
	}
	freshAt := time.Now()
	if _, err := models.StartShareSession(share.ID, device, "hwid-stale", "device-stale", "fresh-session", freshAt); err != nil {
		t.Fatalf("start fresh session: %v", err)
	}

	scheduler.ExecuteShareTelemetryMaintenanceTask()

	sessions, err := models.ListShareSessions([]int{share.ID}, time.Time{}, 10)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected two sessions, got %+v", sessions)
	}
	byKey := make(map[string]models.ShareSession, len(sessions))
	for _, session := range sessions {
		byKey[session.SessionKey] = session
	}
	stale := byKey["stale-session"]
	if !stale.Closed || stale.EndedAt == nil {
		t.Fatalf("expected the stale session to be closed, got %+v", stale)
	}
	if stale.EndedAt.Unix() != stale.LastReportAt.Unix() {
		t.Fatalf("stale session must end at its last report, ended=%v last=%v", stale.EndedAt, stale.LastReportAt)
	}
	fresh := byKey["fresh-session"]
	if fresh.Closed {
		t.Fatalf("expected the fresh session to stay open, got %+v", fresh)
	}
}

type shareSubscriptionOverviewTestData struct {
	Items []struct {
		ShareID       int                     `json:"share_id"`
		ShareName     string                  `json:"share_name"`
		ClientID      *int                    `json:"client_id"`
		ClientName    string                  `json:"client_name"`
		Status        string                  `json:"status"`
		WindowBytes   int64                   `json:"window_bytes"`
		SessionsToday int64                   `json:"sessions_today"`
		OnlineNow     bool                    `json:"online_now"`
		ProblemNodes  int                     `json:"problem_nodes"`
		TopNodes      []shareNodeTrafficEntry `json:"top_nodes"`
		Devices       struct {
			Total  int `json:"total"`
			Active int `json:"active"`
		} `json:"devices"`
	} `json:"items"`
	Daily      []shareDailyBucket `json:"daily"`
	Total      int                `json:"total"`
	WindowDays int                `json:"window_days"`
	Summary    struct {
		Total        int   `json:"total"`
		Online       int   `json:"online"`
		ProblemNodes int   `json:"problem_nodes"`
		WindowBytes  int64 `json:"window_traffic_bytes"`
	} `json:"summary"`
}

type shareSubscriptionDetailTestData struct {
	Share struct {
		ShareID    int    `json:"share_id"`
		ClientName string `json:"client_name"`
	} `json:"share"`
	Devices []struct {
		ID            int   `json:"id"`
		SessionsCount int64 `json:"sessions_count"`
	} `json:"devices"`
	Daily       []shareDailyBucket           `json:"daily"`
	Sessions    []shareSessionDetailEntry    `json:"sessions"`
	Nodes       []shareNodeTrafficEntry      `json:"nodes"`
	Reliability []shareReliabilityEntry      `json:"reliability"`
	Events      []shareReliabilityEventEntry `json:"reliability_events"`
	WindowDays  int                          `json:"window_days"`
}

func performShareSubscriptionRequest(t *testing.T, handler gin.HandlerFunc, query string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)

	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/shares/subscriptions/?"+query, nil)
	handler(ginContext)
	return recorder
}

func decodeShareSubscriptionOverview(t *testing.T, recorder *httptest.ResponseRecorder) shareSubscriptionOverviewTestData {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 from overview, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Code int                               `json:"code"`
		Data shareSubscriptionOverviewTestData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode overview: %v body=%q", err, recorder.Body.String())
	}
	return payload.Data
}

func TestShareSubscriptionOverviewAggregatesTelemetry(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	sub, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "overview-telemetry-sub", "overview-telemetry-token", "Overview Telemetry Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})
	secret, _ := bindShareDeviceForTest(t, "overview-telemetry-token", "hwid-overview-tel", "device-overview-tel")
	headers := map[string]string{"X-Sublink-Device-Secret": secret}

	start := performShareIngestRequest(t, ShareReportSession, "/c/sessions?token=overview-telemetry-token",
		`{"event":"session_start","session_id":"overview-sess"}`, headers)
	if start.Code != http.StatusOK {
		t.Fatalf("expected 200 from session_start, got %d body=%q", start.Code, start.Body.String())
	}
	report := performShareTelemetryRequest(t, "overview-telemetry-token",
		`{"up":120,"down":180,"session_id":"overview-sess"}`, headers)
	if report.Code != http.StatusOK {
		t.Fatalf("expected 200 from telemetry, got %d body=%q", report.Code, report.Body.String())
	}
	key := strings.Repeat("f", 32)
	secondKey := strings.Repeat("e", 32)
	thirdKey := strings.Repeat("d", 32)
	fourthKey := strings.Repeat("c", 32)
	nodes := performShareIngestRequest(t, ShareReportNodes, "/c/nodes?token=overview-telemetry-token",
		fmt.Sprintf(`{"nodes":[{"key":%q,"up":50,"down":50},{"key":%q,"up":30,"down":30},{"key":%q,"up":20,"down":20},{"key":%q,"up":10,"down":10}]}`,
			key, secondKey, thirdKey, fourthKey), headers)
	if nodes.Code != http.StatusOK {
		t.Fatalf("expected 200 from nodes, got %d body=%q", nodes.Code, nodes.Body.String())
	}
	reliability := performShareIngestRequest(t, ShareReportReliability, "/c/reliability?token=overview-telemetry-token",
		fmt.Sprintf(`{"nodes":[{"key":%q,"score":-70,"fails":4,"streak":3}]}`, key), headers)
	if reliability.Code != http.StatusOK {
		t.Fatalf("expected 200 from reliability, got %d body=%q", reliability.Code, reliability.Body.String())
	}

	overview := decodeShareSubscriptionOverview(t,
		performShareSubscriptionRequest(t, ShareSubscriptionOverview, "subId="+strconv.Itoa(sub.ID)))
	if overview.Total != 1 || len(overview.Items) != 1 {
		t.Fatalf("expected one share row, got %+v", overview)
	}
	item := overview.Items[0]
	if item.ShareID != share.ID || item.WindowBytes != 300 {
		t.Fatalf("unexpected overview row: %+v", item)
	}
	if item.SessionsToday != 1 || !item.OnlineNow || item.Devices.Total != 1 || item.Devices.Active != 1 {
		t.Fatalf("expected an online row with one session and one device, got %+v", item)
	}
	if item.ProblemNodes != 1 {
		t.Fatalf("expected one problem node, got %+v", item)
	}
	if len(item.TopNodes) != shareSubscriptionTopNodeLimit {
		t.Fatalf("expected %d top nodes, got %+v", shareSubscriptionTopNodeLimit, item.TopNodes)
	}
	if item.TopNodes[0].Bytes != 100 || item.TopNodes[0].Key != key {
		t.Fatalf("expected the busiest node first, got %+v", item.TopNodes)
	}
	for _, entry := range item.TopNodes {
		if entry.Key == fourthKey {
			t.Fatalf("the smallest node must be trimmed from the top list, got %+v", item.TopNodes)
		}
	}
	if overview.Summary.Online != 1 || overview.Summary.ProblemNodes != 1 || overview.Summary.WindowBytes != 300 {
		t.Fatalf("unexpected overview summary: %+v", overview.Summary)
	}
	if overview.WindowDays != models.ShareDeviceUsageWindowDays {
		t.Fatalf("expected the default window of %d days, got %d", models.ShareDeviceUsageWindowDays, overview.WindowDays)
	}
	if len(overview.Daily) != models.ShareDeviceUsageWindowDays {
		t.Fatalf("expected %d daily buckets, got %+v", models.ShareDeviceUsageWindowDays, overview.Daily)
	}
	var dailyTotal int64
	for _, bucket := range overview.Daily {
		dailyTotal += bucket.Bytes
	}
	if dailyTotal != 300 {
		t.Fatalf("expected the daily buckets to sum the window traffic, got %d (%+v)", dailyTotal, overview.Daily)
	}

	client := models.ShareClient{Name: "Acme"}
	if err := client.Add(); err != nil {
		t.Fatalf("add client: %v", err)
	}
	if err := models.AssignShareClients([]int{share.ID}, &client.ID); err != nil {
		t.Fatalf("assign client: %v", err)
	}
	assigned := decodeShareSubscriptionOverview(t,
		performShareSubscriptionRequest(t, ShareSubscriptionOverview, "subId="+strconv.Itoa(sub.ID)))
	if assigned.Items[0].ClientName != "Acme" || assigned.Items[0].ClientID == nil {
		t.Fatalf("expected the client on the overview row, got %+v", assigned.Items[0])
	}

	filtered := decodeShareSubscriptionOverview(t,
		performShareSubscriptionRequest(t, ShareSubscriptionOverview, "subId="+strconv.Itoa(sub.ID)+"&clientId="+strconv.Itoa(client.ID)))
	if filtered.Total != 1 {
		t.Fatalf("expected the client filter to keep the row, got %+v", filtered)
	}
	unassigned := decodeShareSubscriptionOverview(t,
		performShareSubscriptionRequest(t, ShareSubscriptionOverview, "subId="+strconv.Itoa(sub.ID)+"&clientId=none"))
	if unassigned.Total != 0 {
		t.Fatalf("expected no unassigned rows, got %+v", unassigned)
	}

	keywordMiss := decodeShareSubscriptionOverview(t,
		performShareSubscriptionRequest(t, ShareSubscriptionOverview, "subId="+strconv.Itoa(sub.ID)+"&keyword=zzz"))
	if keywordMiss.Total != 0 {
		t.Fatalf("expected the keyword filter to drop the row, got %+v", keywordMiss)
	}

	// Deleting the client must keep the card and unassign it.
	if err := client.Delete(); err != nil {
		t.Fatalf("delete client: %v", err)
	}
	afterDelete := decodeShareSubscriptionOverview(t,
		performShareSubscriptionRequest(t, ShareSubscriptionOverview, "subId="+strconv.Itoa(sub.ID)))
	if afterDelete.Total != 1 || afterDelete.Items[0].ClientID != nil {
		t.Fatalf("expected the card to stay unassigned after client deletion, got %+v", afterDelete)
	}
}

func TestShareSubscriptionDetailReturnsTelemetryBlocks(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "detail-telemetry-sub", "detail-telemetry-token", "Detail Telemetry Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})
	secret, _ := bindShareDeviceForTest(t, "detail-telemetry-token", "hwid-detail-tel", "device-detail-tel")
	headers := map[string]string{"X-Sublink-Device-Secret": secret}

	if recorder := performShareIngestRequest(t, ShareReportSession, "/c/sessions?token=detail-telemetry-token",
		`{"event":"session_start","session_id":"detail-sess"}`, headers); recorder.Code != http.StatusOK {
		t.Fatalf("start session failed: %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder := performShareTelemetryRequest(t, "detail-telemetry-token",
		`{"up":10,"down":90,"session_id":"detail-sess"}`, headers); recorder.Code != http.StatusOK {
		t.Fatalf("telemetry report failed: %d %q", recorder.Code, recorder.Body.String())
	}
	key := strings.Repeat("9", 32)
	if recorder := performShareIngestRequest(t, ShareReportNodes, "/c/nodes?token=detail-telemetry-token",
		fmt.Sprintf(`{"nodes":[{"key":%q,"up":30,"down":70}]}`, key), headers); recorder.Code != http.StatusOK {
		t.Fatalf("node report failed: %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder := performShareIngestRequest(t, ShareReportReliability, "/c/reliability?token=detail-telemetry-token",
		fmt.Sprintf(`{"nodes":[{"key":%q,"score":-60,"fails":2,"streak":2,"silent":false}]}`, key), headers); recorder.Code != http.StatusOK {
		t.Fatalf("reliability report failed: %d %q", recorder.Code, recorder.Body.String())
	}

	recorder := performShareSubscriptionRequest(t, ShareSubscriptionDetail,
		"shareId="+strconv.Itoa(share.ID)+"&days=7")
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 from detail, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Code int                             `json:"code"`
		Data shareSubscriptionDetailTestData `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode detail: %v body=%q", err, recorder.Body.String())
	}
	data := payload.Data
	if data.Share.ShareID != share.ID || data.WindowDays != 7 {
		t.Fatalf("unexpected share meta: %+v", data)
	}
	if len(data.Daily) != 7 {
		t.Fatalf("expected seven daily buckets, got %+v", data.Daily)
	}
	if len(data.Sessions) != 1 || data.Sessions[0].SessionKey != "detail-sess" || data.Sessions[0].Bytes != 100 {
		t.Fatalf("expected the session row, got %+v", data.Sessions)
	}
	if len(data.Devices) != 1 || data.Devices[0].SessionsCount != 1 {
		t.Fatalf("expected one device with one session, got %+v", data.Devices)
	}
	if len(data.Nodes) != 1 || data.Nodes[0].Key != key || data.Nodes[0].Bytes != 100 {
		t.Fatalf("expected one node row with 100 bytes, got %+v", data.Nodes)
	}
	if len(data.Reliability) != 1 || data.Reliability[0].State != models.ShareReliabilityStateSuspect {
		t.Fatalf("expected one suspect node, got %+v", data.Reliability)
	}
	if len(data.Events) != 1 || data.Events[0].NodeKey != key {
		t.Fatalf("expected one reliability event, got %+v", data.Events)
	}
}

func TestShareDeviceDetailIncludesTelemetryBlocks(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, _, _ = createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "device-detail-sub", "device-detail-token", "Device Detail Node", models.ShareGroup{
		TelemetryEnabled: true,
		DeviceMode:       models.ShareDeviceModeAutoBind,
	})
	secret, _ := bindShareDeviceForTest(t, "device-detail-token", "hwid-dev-detail", "device-dev-detail")
	headers := map[string]string{"X-Sublink-Device-Secret": secret}

	if recorder := performShareIngestRequest(t, ShareReportSession, "/c/sessions?token=device-detail-token",
		`{"event":"session_start","session_id":"device-detail-sess"}`, headers); recorder.Code != http.StatusOK {
		t.Fatalf("start session failed: %d %q", recorder.Code, recorder.Body.String())
	}
	if recorder := performShareTelemetryRequest(t, "device-detail-token",
		`{"up":5,"down":15,"session_id":"device-detail-sess"}`, headers); recorder.Code != http.StatusOK {
		t.Fatalf("telemetry report failed: %d %q", recorder.Code, recorder.Body.String())
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet,
		"/api/v1/shares/devices/detail?hwid=hwid-dev-detail&deviceId=device-dev-detail&days=7", nil)
	ShareDeviceDetail(ginContext)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 from device detail, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Code int `json:"code"`
		Data struct {
			Daily    []shareDailyBucket         `json:"daily"`
			Sessions []shareSessionDetailEntry  `json:"sessions"`
			Nodes    []shareNodeTrafficEntry    `json:"nodes"`
			Activity []shareDeviceActivityEntry `json:"activity"`
			Window   int                        `json:"window_days"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode device detail: %v body=%q", err, recorder.Body.String())
	}
	if payload.Data.Window != 7 || len(payload.Data.Daily) != 7 {
		t.Fatalf("expected a seven day window, got %+v", payload.Data)
	}
	if len(payload.Data.Sessions) != 1 || payload.Data.Sessions[0].Bytes != 20 {
		t.Fatalf("expected the device session, got %+v", payload.Data.Sessions)
	}
}

func TestShareClientCRUDKeepsCards(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	_, share, _ := createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "client-crud-sub", "client-crud-token", "Client CRUD Node", models.ShareGroup{
		TelemetryEnabled: true,
	})

	added := performJSONRequest(t, ShareClientAdd, http.MethodPost, map[string]any{"name": "Bob", "note": "paid"})
	var addedPayload struct {
		Code int                `json:"code"`
		Data models.ShareClient `json:"data"`
	}
	if err := json.Unmarshal(added.Body.Bytes(), &addedPayload); err != nil {
		t.Fatalf("decode added client: %v body=%q", err, added.Body.String())
	}
	if addedPayload.Code != http.StatusOK || addedPayload.Data.ID == 0 {
		t.Fatalf("expected a created client, got %+v body=%q", addedPayload, added.Body.String())
	}
	clientID := addedPayload.Data.ID

	assign := performJSONRequest(t, ShareClientAssign, http.MethodPost, map[string]any{
		"share_ids": []int{share.ID},
		"client_id": clientID,
	})
	if decoded := decodeAPIResponse(t, assign); decoded.Code != http.StatusOK {
		t.Fatalf("assign failed: %+v", decoded)
	}
	reloaded, err := models.GetSubscriptionShareByToken("client-crud-token")
	if err != nil {
		t.Fatalf("reload share: %v", err)
	}
	if reloaded.ClientID == nil || *reloaded.ClientID != clientID {
		t.Fatalf("expected client_id %d, got %v", clientID, reloaded.ClientID)
	}

	list := performJSONRequest(t, ShareClientList, http.MethodGet, nil)
	var listPayload struct {
		Data struct {
			Items []shareClientItem `json:"items"`
			Total int               `json:"total"`
		} `json:"data"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listPayload); err != nil {
		t.Fatalf("decode client list: %v body=%q", err, list.Body.String())
	}
	if listPayload.Data.Total != 1 || listPayload.Data.Items[0].ShareCount != 1 {
		t.Fatalf("expected one client owning one card, got %+v", listPayload.Data)
	}

	gin.SetMode(gin.TestMode)
	deleteRecorder := httptest.NewRecorder()
	deleteContext, _ := gin.CreateTestContext(deleteRecorder)
	deleteContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodDelete,
		"/api/v1/shares/clients/delete?id="+strconv.Itoa(clientID), nil)
	ShareClientDelete(deleteContext)
	if decoded := decodeAPIResponse(t, deleteRecorder); decoded.Code != http.StatusOK {
		t.Fatalf("delete failed: %+v", decoded)
	}

	if _, err := models.GetShareClientByID(clientID); err == nil {
		t.Fatal("expected the client row to be deleted")
	}
	reloaded, err = models.GetSubscriptionShareByToken("client-crud-token")
	if err != nil {
		t.Fatalf("reload share after delete: %v", err)
	}
	if reloaded.ClientID != nil {
		t.Fatalf("expected the card to become unassigned, got %v", reloaded.ClientID)
	}
	stored, err := models.GetSubscriptionShareByToken("client-crud-token")
	if err != nil || stored == nil {
		t.Fatalf("expected the subscription card to survive the client deletion: %v", err)
	}
}
