package api

import (
	"net/url"
	"strconv"
	"testing"

	"sublink/models"
)

func TestAddTempStoresTrimmedDescription(t *testing.T) {
	setupTemplateAPITestDB(t)
	templateDir := t.TempDir()
	baseTemplateDir = templateDir

	form := url.Values{}
	form.Set("filename", "desc.yaml")
	form.Set("text", "proxies: []\n")
	form.Set("category", "clash")
	form.Set("description", "  primary rule template  ")

	recorder := performSubcriptionFormRequest(t, AddTemp, form)
	if response := decodeAPIResponse(t, recorder); response.Code != 200 {
		t.Fatalf("AddTemp code = %d, body = %s", response.Code, recorder.Body.String())
	}

	var stored models.Template
	if err := stored.FindByName("desc.yaml"); err != nil {
		t.Fatalf("find template: %v", err)
	}
	if stored.Description != "primary rule template" {
		t.Fatalf("description = %q, want %q", stored.Description, "primary rule template")
	}
}

func TestUpdateTempPreservesDescriptionWhenOmitted(t *testing.T) {
	setupTemplateAPITestDB(t)
	templateDir := t.TempDir()
	baseTemplateDir = templateDir

	writeTemplateFileForTest(t, templateDir, "keep.yaml", "proxies: []\n")
	template := models.Template{Name: "keep.yaml", Description: "keep me", Category: "clash"}
	if err := template.Add(); err != nil {
		t.Fatalf("add template: %v", err)
	}

	form := url.Values{}
	form.Set("filename", "keep.yaml")
	form.Set("oldname", "keep.yaml")
	form.Set("text", "proxies: []\n# v2\n")
	form.Set("category", "clash")

	recorder := performSubcriptionFormRequest(t, UpdateTemp, form)
	if response := decodeAPIResponse(t, recorder); response.Code != 200 {
		t.Fatalf("UpdateTemp code = %d, body = %s", response.Code, recorder.Body.String())
	}

	var stored models.Template
	if err := stored.FindByName("keep.yaml"); err != nil {
		t.Fatalf("find template: %v", err)
	}
	if stored.Description != "keep me" {
		t.Fatalf("description = %q, want preserved value %q", stored.Description, "keep me")
	}
}

func TestUpdateTempClearsDescriptionWhenEmpty(t *testing.T) {
	setupTemplateAPITestDB(t)
	templateDir := t.TempDir()
	baseTemplateDir = templateDir

	writeTemplateFileForTest(t, templateDir, "clear.yaml", "proxies: []\n")
	template := models.Template{Name: "clear.yaml", Description: "remove me", Category: "clash"}
	if err := template.Add(); err != nil {
		t.Fatalf("add template: %v", err)
	}

	form := url.Values{}
	form.Set("filename", "clear.yaml")
	form.Set("oldname", "clear.yaml")
	form.Set("text", "proxies: []\n")
	form.Set("category", "clash")
	form.Set("description", "")

	recorder := performSubcriptionFormRequest(t, UpdateTemp, form)
	if response := decodeAPIResponse(t, recorder); response.Code != 200 {
		t.Fatalf("UpdateTemp code = %d, body = %s", response.Code, recorder.Body.String())
	}

	var stored models.Template
	if err := stored.FindByName("clear.yaml"); err != nil {
		t.Fatalf("find template: %v", err)
	}
	if stored.Description != "" {
		t.Fatalf("description = %q, want empty", stored.Description)
	}
}

func TestSubscriptionDescriptionRoundTrip(t *testing.T) {
	setupProxyRelayTestDB(t)

	airport := addRelayAirport(t, models.Airport{Name: "desc-airport", URL: "https://example.test/sub"})

	addForm := url.Values{}
	addForm.Set("name", "desc-sub")
	addForm.Set("type", models.SubcriptionTypeProxy)
	addForm.Set("AirportID", strconv.Itoa(airport.ID))
	addForm.Set("description", "  upstream relay  ")
	recorder := performSubcriptionFormRequest(t, SubAdd, addForm)
	if response := decodeAPIResponse(t, recorder); response.Code != 200 {
		t.Fatalf("SubAdd code = %d, body = %s", response.Code, recorder.Body.String())
	}

	var stored models.Subcription
	stored.Name = "desc-sub"
	if err := stored.Find(); err != nil {
		t.Fatalf("find subscription: %v", err)
	}
	if stored.Description != "upstream relay" {
		t.Fatalf("description = %q, want %q", stored.Description, "upstream relay")
	}

	updateForm := url.Values{}
	updateForm.Set("name", "desc-sub")
	updateForm.Set("oldname", "desc-sub")
	updateForm.Set("type", models.SubcriptionTypeProxy)
	updateForm.Set("AirportID", strconv.Itoa(airport.ID))
	recorder = performSubcriptionFormRequest(t, SubUpdate, updateForm)
	if response := decodeAPIResponse(t, recorder); response.Code != 200 {
		t.Fatalf("SubUpdate without description code = %d, body = %s", response.Code, recorder.Body.String())
	}

	stored = models.Subcription{Name: "desc-sub"}
	if err := stored.Find(); err != nil {
		t.Fatalf("find subscription after update: %v", err)
	}
	if stored.Description != "upstream relay" {
		t.Fatalf("description = %q, want preserved value %q", stored.Description, "upstream relay")
	}

	updateForm.Set("description", "")
	recorder = performSubcriptionFormRequest(t, SubUpdate, updateForm)
	if response := decodeAPIResponse(t, recorder); response.Code != 200 {
		t.Fatalf("SubUpdate with blank description code = %d, body = %s", response.Code, recorder.Body.String())
	}

	stored = models.Subcription{Name: "desc-sub"}
	if err := stored.Find(); err != nil {
		t.Fatalf("find subscription after clearing: %v", err)
	}
	if stored.Description != "" {
		t.Fatalf("description = %q, want empty", stored.Description)
	}
}
