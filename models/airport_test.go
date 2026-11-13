package models

import (
	"strconv"
	"testing"

	"sublink/cache"
	"sublink/database"
	"sublink/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func resetAirportCacheForTest() {
	airportCache = cache.NewMapCache(func(a Airport) int { return a.ID })
	airportCache.AddIndex("enabled", func(a Airport) string { return strconv.FormatBool(a.Enabled) })
	airportCache.AddIndex("name", func(a Airport) string { return a.Name })
}

func setupAirportTestDB(t *testing.T) {
	t.Helper()

	oldDB := database.DB
	oldDialect := database.Dialect
	oldInitialized := database.IsInitialized

	db, err := gorm.Open(sqlite.Open(testutil.UniqueMemoryDSN(t, "airport_model_test")), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if err := db.AutoMigrate(&Airport{}, &SubcriptionAirport{}, &GroupAirportSort{}); err != nil {
		t.Fatalf("auto migrate airports: %v", err)
	}

	database.DB = db
	database.Dialect = database.DialectSQLite
	database.IsInitialized = false
	resetAirportCacheForTest()

	t.Cleanup(func() {
		database.DB = oldDB
		database.Dialect = oldDialect
		database.IsInitialized = oldInitialized
		resetAirportCacheForTest()
		testutil.CloseDB(t, db)
	})
}

func TestAirportDeleteCleansSubscriptionAirportRelations(t *testing.T) {
	setupAirportTestDB(t)

	airport := &Airport{
		Name:     "relation-cleanup-airport",
		URL:      "https://example.com/subscription",
		CronExpr: "0 */12 * * *",
		Enabled:  true,
	}
	if err := airport.Add(); err != nil {
		t.Fatalf("add airport: %v", err)
	}
	if err := database.DB.Create(&SubcriptionAirport{SubcriptionID: 9, AirportID: airport.ID, Sort: 3}).Error; err != nil {
		t.Fatalf("add subscription airport relation: %v", err)
	}

	if err := airport.Del(); err != nil {
		t.Fatalf("delete airport: %v", err)
	}

	var count int64
	if err := database.DB.Model(&SubcriptionAirport{}).Where("airport_id = ?", airport.ID).Count(&count).Error; err != nil {
		t.Fatalf("count subscription airport relations: %v", err)
	}
	if count != 0 {
		t.Fatalf("subscription airport relation count = %d, want 0", count)
	}
}

func TestAirportUpdatePersistsUpdateAfterDetectSettings(t *testing.T) {
	setupAirportTestDB(t)

	airport := &Airport{
		Name:                         "机场A",
		URL:                          "https://example.com/subscription",
		CronExpr:                     "0 */12 * * *",
		Enabled:                      false,
		Group:                        "默认组",
		RequestHeaders:               AirportRequestHeaders{},
		UpdateAfterDetect:            true,
		UpdateAfterDetectProfileID:   7,
		UpdateAfterDetectChangedOnly: true,
	}

	if err := airport.Add(); err != nil {
		t.Fatalf("add airport: %v", err)
	}

	airport.Name = "机场A-更新"
	airport.URL = "https://example.com/subscription-updated"
	airport.CronExpr = "0 */6 * * *"
	airport.Group = "更新组"
	airport.UpdateAfterDetect = false
	airport.UpdateAfterDetectProfileID = 0
	airport.UpdateAfterDetectChangedOnly = false

	if err := airport.Update(); err != nil {
		t.Fatalf("update airport: %v", err)
	}

	var stored Airport
	if err := database.DB.First(&stored, airport.ID).Error; err != nil {
		t.Fatalf("reload airport: %v", err)
	}

	if stored.UpdateAfterDetect {
		t.Fatal("expected update_after_detect to persist false after update")
	}
	if stored.UpdateAfterDetectProfileID != 0 {
		t.Fatalf("expected update_after_detect_profile_id = 0 after update, got %d", stored.UpdateAfterDetectProfileID)
	}
	if stored.UpdateAfterDetectChangedOnly {
		t.Fatal("expected update_after_detect_changed_only to persist false after update")
	}
}

func TestAirportBackfillCountryImpliesAutoFillCountryOnSave(t *testing.T) {
	setupAirportTestDB(t)

	airport := &Airport{
		Name:                    "回填机场",
		URL:                     "https://example.com/subscription",
		CronExpr:                "0 */12 * * *",
		BackfillExistingCountry: true,
		AutoFillCountry:         false,
	}
	if err := airport.Add(); err != nil {
		t.Fatalf("add airport: %v", err)
	}

	var stored Airport
	if err := database.DB.First(&stored, airport.ID).Error; err != nil {
		t.Fatalf("reload airport after add: %v", err)
	}
	if !stored.AutoFillCountry {
		t.Fatal("expected auto_fill_country to be true when backfill_existing_country is true after add")
	}

	stored.AutoFillCountry = false
	stored.BackfillExistingCountry = true
	if err := stored.Update(); err != nil {
		t.Fatalf("update airport: %v", err)
	}

	var updated Airport
	if err := database.DB.First(&updated, airport.ID).Error; err != nil {
		t.Fatalf("reload airport after update: %v", err)
	}
	if !updated.AutoFillCountry {
		t.Fatal("expected auto_fill_country to be true when backfill_existing_country is true after update")
	}
}

func TestEnableAirportUsageFetchEnablesExistingAirports(t *testing.T) {
	setupAirportTestDB(t)

	disabled := &Airport{
		Name:           "用量关闭机场",
		URL:            "https://example.com/usage-disabled",
		CronExpr:       "0 */12 * * *",
		FetchUsageInfo: false,
	}
	enabled := &Airport{
		Name:           "用量开启机场",
		URL:            "https://example.com/usage-enabled",
		CronExpr:       "0 */12 * * *",
		FetchUsageInfo: true,
	}
	if err := disabled.Add(); err != nil {
		t.Fatalf("add disabled airport: %v", err)
	}
	if err := enabled.Add(); err != nil {
		t.Fatalf("add enabled airport: %v", err)
	}

	// 模拟历史数据中的 NULL 值
	if err := database.DB.Model(&Airport{}).Where("id = ?", disabled.ID).Update("fetch_usage_info", nil).Error; err != nil {
		t.Fatalf("set fetch_usage_info to null: %v", err)
	}

	affected, err := enableAirportUsageFetch(database.DB)
	if err != nil {
		t.Fatalf("enable airport usage fetch: %v", err)
	}
	if affected != 1 {
		t.Fatalf("affected = %d, want 1", affected)
	}

	var airports []Airport
	if err := database.DB.Order("id").Find(&airports).Error; err != nil {
		t.Fatalf("list airports: %v", err)
	}
	for _, airport := range airports {
		if !airport.FetchUsageInfo {
			t.Fatalf("expected fetch_usage_info true for airport %q", airport.Name)
		}
	}

	// 幂等：再次执行不应再更新任何行
	affected, err = enableAirportUsageFetch(database.DB)
	if err != nil {
		t.Fatalf("re-run enable airport usage fetch: %v", err)
	}
	if affected != 0 {
		t.Fatalf("affected on re-run = %d, want 0", affected)
	}
}
