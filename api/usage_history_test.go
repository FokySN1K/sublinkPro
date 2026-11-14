package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"sublink/database"
	"sublink/internal/testutil"
	"sublink/models"

	"github.com/gin-gonic/gin"
)

func setupUsageHistoryAPITestDB(t *testing.T) {
	t.Helper()

	oldDB := database.DB
	oldDialect := database.Dialect
	oldInitialized := database.IsInitialized

	db := testutil.OpenMemoryDB(t, "usage_history_api_test")
	if err := db.AutoMigrate(
		&models.Airport{},
		&models.AirportUsageSnapshot{},
		&models.SystemSetting{},
	); err != nil {
		t.Fatalf("auto migrate usage history tables: %v", err)
	}

	database.DB = db
	database.Dialect = database.DialectSQLite
	database.IsInitialized = false
	if err := models.InitAirportCache(); err != nil {
		t.Fatalf("init airport cache: %v", err)
	}
	if err := models.InitSettingCache(); err != nil {
		t.Fatalf("init setting cache: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Exec("DELETE FROM system_settings").Error
		database.DB = oldDB
		database.Dialect = oldDialect
		database.IsInitialized = oldInitialized
		if oldDB != nil {
			_ = models.InitAirportCache()
			_ = models.InitSettingCache()
		}
		testutil.CloseDB(t, db)
	})
}

func newUsageHistoryAPIAirport(t *testing.T, name string) *models.Airport {
	t.Helper()

	airport := &models.Airport{
		Name:           name,
		URL:            "https://example.com/" + name,
		CronExpr:       "0 */12 * * *",
		FetchUsageInfo: true,
	}
	if err := airport.Add(); err != nil {
		t.Fatalf("add airport %s: %v", name, err)
	}
	return airport
}

func decodeUsageHistoryData[T any](t *testing.T, recorder interface{ Bytes() []byte }) T {
	t.Helper()

	var body struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(recorder.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal usage history response: %v", err)
	}
	if body.Code != 200 {
		t.Fatalf("usage history response code = %d, msg = %s", body.Code, body.Msg)
	}

	var data T
	if err := json.Unmarshal(body.Data, &data); err != nil {
		t.Fatalf("unmarshal usage history data: %v", err)
	}
	return data
}

func TestGetAirportUsageHistoryReturnsPoints(t *testing.T) {
	setupUsageHistoryAPITestDB(t)
	airport := newUsageHistoryAPIAirport(t, "history-api")

	snapshots := []models.AirportUsageSnapshot{
		{AirportID: airport.ID, Upload: 10, Download: 20, Total: 500, CapturedAt: time.Now().Add(-2 * time.Hour)},
		{AirportID: airport.ID, Upload: 30, Download: 40, Total: 500, CapturedAt: time.Now().Add(-1 * time.Hour)},
	}
	if err := database.DB.Create(&snapshots).Error; err != nil {
		t.Fatalf("create usage snapshots: %v", err)
	}

	recorder := performAirportJSONRequest(
		t,
		GetAirportUsageHistory,
		http.MethodGet,
		"/api/v1/airports/"+strconv.Itoa(airport.ID)+"/usage-history?days=1&granularity=hour",
		nil,
		gin.Param{Key: "id", Value: strconv.Itoa(airport.ID)},
	)
	history := decodeUsageHistoryData[models.AirportUsageHistory](t, recorder.Body)

	if history.AirportID != airport.ID {
		t.Fatalf("airport id = %d, want %d", history.AirportID, airport.ID)
	}
	if history.Granularity != models.UsageGranularityHour {
		t.Fatalf("granularity = %s, want hour", history.Granularity)
	}
	if len(history.Points) != 2 {
		t.Fatalf("points = %d, want 2", len(history.Points))
	}
	if history.Points[1].Used != 70 {
		t.Fatalf("last point used = %d, want 70", history.Points[1].Used)
	}
}

func TestGetAirportUsageHistoryRejectsInvalidID(t *testing.T) {
	setupUsageHistoryAPITestDB(t)

	recorder := performAirportJSONRequest(
		t,
		GetAirportUsageHistory,
		http.MethodGet,
		"/api/v1/airports/abc/usage-history",
		nil,
		gin.Param{Key: "id", Value: "abc"},
	)
	response := decodeAirportAPIResponse(t, recorder)
	if response.Code == 200 {
		t.Fatalf("expected failure for invalid id, body = %s", recorder.Body.String())
	}
}

func TestGetAirportUsageSparklinesParsesIDs(t *testing.T) {
	setupUsageHistoryAPITestDB(t)
	first := newUsageHistoryAPIAirport(t, "sparkline-api-one")
	second := newUsageHistoryAPIAirport(t, "sparkline-api-two")

	snapshots := []models.AirportUsageSnapshot{
		{AirportID: first.ID, Upload: 1, Download: 2, Total: 10, CapturedAt: time.Now().Add(-time.Hour)},
		{AirportID: second.ID, Upload: 3, Download: 4, Total: 20, CapturedAt: time.Now().Add(-time.Hour)},
	}
	if err := database.DB.Create(&snapshots).Error; err != nil {
		t.Fatalf("create sparkline snapshots: %v", err)
	}

	ids := strconv.Itoa(first.ID) + "," + strconv.Itoa(second.ID) + "," + strconv.Itoa(first.ID)
	recorder := performAirportJSONRequest(
		t,
		GetAirportUsageSparklines,
		http.MethodGet,
		"/api/v1/airports/usage-sparklines?ids="+ids+"&days=7",
		nil,
	)
	payload := decodeUsageHistoryData[struct {
		Items []models.AirportUsageSparkline `json:"items"`
	}](t, recorder.Body)

	if len(payload.Items) != 2 {
		t.Fatalf("sparkline items = %d, want 2", len(payload.Items))
	}
	for _, item := range payload.Items {
		if len(item.Points) == 0 {
			t.Fatalf("airport %d has no sparkline points", item.AirportID)
		}
	}
}

func TestGetAirportUsageSparklinesRejectsBadQueries(t *testing.T) {
	setupUsageHistoryAPITestDB(t)

	cases := []string{
		"/api/v1/airports/usage-sparklines",
		"/api/v1/airports/usage-sparklines?ids=abc",
		"/api/v1/airports/usage-sparklines?ids=0",
	}
	for _, path := range cases {
		recorder := performAirportJSONRequest(t, GetAirportUsageSparklines, http.MethodGet, path, nil)
		response := decodeAirportAPIResponse(t, recorder)
		if response.Code == 200 {
			t.Fatalf("expected failure for path %s, body = %s", path, recorder.Body.String())
		}
	}
}

func TestUsageHistoryConfigHandlersRoundTrip(t *testing.T) {
	setupUsageHistoryAPITestDB(t)

	getRecorder := performAirportJSONRequest(t, GetUsageHistoryConfig, http.MethodGet, "/api/v1/settings/usage-history", nil)
	initial := decodeUsageHistoryData[struct {
		RetentionDays    int   `json:"retentionDays"`
		MinRetentionDays int   `json:"minRetentionDays"`
		MaxRetentionDays int   `json:"maxRetentionDays"`
		SnapshotCount    int64 `json:"snapshotCount"`
	}](t, getRecorder.Body)

	if initial.RetentionDays != models.DefaultAirportUsageRetentionDays {
		t.Fatalf("initial retention days = %d, want default %d", initial.RetentionDays, models.DefaultAirportUsageRetentionDays)
	}
	if initial.MinRetentionDays != models.MinAirportUsageRetentionDays || initial.MaxRetentionDays != models.MaxAirportUsageRetentionDays {
		t.Fatalf("retention bounds = %d..%d, want %d..%d",
			initial.MinRetentionDays, initial.MaxRetentionDays,
			models.MinAirportUsageRetentionDays, models.MaxAirportUsageRetentionDays)
	}

	updateRecorder := performAirportJSONRequest(
		t,
		UpdateUsageHistoryConfig,
		http.MethodPost,
		"/api/v1/settings/usage-history",
		map[string]any{"retentionDays": 7},
	)
	updateResponse := decodeAirportAPIResponse(t, updateRecorder)
	if updateResponse.Code != 200 {
		t.Fatalf("update retention days response code = %d, msg = %s", updateResponse.Code, updateResponse.Msg)
	}
	if got := models.GetAirportUsageRetentionDays(); got != 7 {
		t.Fatalf("stored retention days = %d, want 7", got)
	}
}

func TestUpdateUsageHistoryConfigValidatesInput(t *testing.T) {
	setupUsageHistoryAPITestDB(t)

	cases := []struct {
		name string
		body map[string]any
	}{
		{name: "missing", body: map[string]any{}},
		{name: "below minimum", body: map[string]any{"retentionDays": models.MinAirportUsageRetentionDays - 1}},
		{name: "above maximum", body: map[string]any{"retentionDays": models.MaxAirportUsageRetentionDays + 1}},
	}
	for _, tc := range cases {
		recorder := performAirportJSONRequest(t, UpdateUsageHistoryConfig, http.MethodPost, "/api/v1/settings/usage-history", tc.body)
		response := decodeAirportAPIResponse(t, recorder)
		if response.Code == 200 {
			t.Fatalf("case %s: expected failure, body = %s", tc.name, recorder.Body.String())
		}
	}
}
