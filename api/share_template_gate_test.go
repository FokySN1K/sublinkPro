package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sublink/models"

	"github.com/gin-gonic/gin"
)

func TestShareTemplateAllowlistEnforced(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "gate-sub", "gate-token", "Gate Node", models.ShareGroup{
		Templates: "mihomo,mihomo_snkp",
	})

	deviceHeaders := map[string]string{
		"X-HWID":      "hwid-gate",
		"X-Device-ID": "device-gate",
	}

	t.Run("explicit disallowed template is stubbed", func(t *testing.T) {
		headers := map[string]string{"X-HWID": deviceHeaders["X-HWID"], "X-Device-ID": deviceHeaders["X-Device-ID"]}
		recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=gate-token&client=surge", headers)
		if got := recorder.Header().Get("X-Sublink-Reason"); got != models.ShareStubCaseTemplateDisabled {
			t.Fatalf("expected reason %q, got %q", models.ShareStubCaseTemplateDisabled, got)
		}
		if got := recorder.Header().Get("X-Sublink-Template"); got != models.ShareTemplateSurge {
			t.Fatalf("expected template header %q, got %q", models.ShareTemplateSurge, got)
		}
		if strings.Contains(recorder.Body.String(), "Gate Node") {
			t.Fatal("template-disabled response leaked the node list")
		}
	})

	t.Run("auto detection of a disallowed app is stubbed", func(t *testing.T) {
		recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=gate-token", map[string]string{
			"User-Agent":  "Surge iOS/2900",
			"X-HWID":      deviceHeaders["X-HWID"],
			"X-Device-ID": "device-gate-auto",
		})
		if got := recorder.Header().Get("X-Sublink-Reason"); got != models.ShareStubCaseTemplateDisabled {
			t.Fatalf("expected reason %q, got %q", models.ShareStubCaseTemplateDisabled, got)
		}
	})

	t.Run("allowed template is served", func(t *testing.T) {
		recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=gate-token&client=mihomo", map[string]string{
			"User-Agent":  "ClashMeta/1.19.0",
			"X-HWID":      deviceHeaders["X-HWID"],
			"X-Device-ID": "device-gate-mihomo",
		})
		if got := recorder.Header().Get("X-Sublink-Reason"); got != "" {
			t.Fatalf("expected a served profile, got reason %q", got)
		}
		if !strings.Contains(recorder.Body.String(), "Gate Node") {
			t.Fatalf("expected the node list in the profile, got %q", recorder.Body.String())
		}
	})

	t.Run("substore targets are exempt", func(t *testing.T) {
		recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=gate-token&client=loon", map[string]string{
			"X-HWID":      deviceHeaders["X-HWID"],
			"X-Device-ID": "device-gate-loon",
		})
		if got := recorder.Header().Get("X-Sublink-Reason"); got == models.ShareStubCaseTemplateDisabled {
			t.Fatal("substore export targets must not be blocked by the template allowlist")
		}
		if got := recorder.Header().Get("X-Sublink-Template"); got != "loon" {
			t.Fatalf("expected template header %q, got %q", "loon", got)
		}
	})
}

func TestShareGroupTemplateFileNameResolvesAgainstTemplateDir(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	oldBaseTemplateDir := baseTemplateDir
	baseTemplateDir = t.TempDir()
	t.Cleanup(func() { baseTemplateDir = oldBaseTemplateDir })

	const bareFileName = "bare-group-template.yaml"
	bareTemplate := "port: 7999\n# bare-group-template\nproxies: []\nproxy-groups:\n  - name: bare\n    type: select\n    proxies: []\n"
	if err := os.WriteFile(filepath.Join(baseTemplateDir, bareFileName), []byte(bareTemplate), 0600); err != nil {
		t.Fatalf("write bare group template: %v", err)
	}
	if err := (&models.Template{Name: bareFileName, Category: "clash"}).Add(); err != nil {
		t.Fatalf("register bare group template: %v", err)
	}

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "bare-sub", "bare-token", "Bare Node", models.ShareGroup{
		Templates:     "mihomo",
		ClashTemplate: bareFileName,
	})

	recorder := performClientRequestWithHeaders(t, http.MethodGet, "/c/?token=bare-token&client=mihomo", map[string]string{
		"X-HWID":      "hwid-bare",
		"X-Device-ID": "device-bare",
	})
	if got := recorder.Header().Get("X-Sublink-Reason"); got != "" {
		t.Fatalf("expected a served profile, got reason %q body=%q", got, recorder.Body.String())
	}
	body := recorder.Body.String()
	if !strings.Contains(body, "port: 7999") || !strings.Contains(body, "Bare Node") {
		t.Fatalf("expected the group template file to be used, got %q", body)
	}
}

func TestShareStatusReportsTemplateDisabled(t *testing.T) {
	setupClientsAPITestDB(t)
	clashTemplatePath := writeTestClashTemplate(t)
	surgeTemplatePath := writeTestSurgeTemplate(t)

	createManagedShareFixture(t, clashTemplatePath, surgeTemplatePath, "status-gate-sub", "status-gate-token", "Status Gate Node", models.ShareGroup{
		Templates: "mihomo",
	})

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ginContext, _ := gin.CreateTestContext(recorder)
	ginContext.Request = httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/c/status?token=status-gate-token&client=surge", nil)
	ginContext.Request.Header.Set("X-HWID", "hwid-status-gate")
	ginContext.Request.Header.Set("X-Device-ID", "device-status-gate")
	ShareStatus(ginContext)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected 200 from /c/status, got %d body=%q", recorder.Code, recorder.Body.String())
	}
	var payload struct {
		Data struct {
			Valid  bool   `json:"valid"`
			Reason string `json:"reason"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode status payload: %v body=%q", err, recorder.Body.String())
	}
	if payload.Data.Valid {
		t.Fatal("expected an invalid status for a disallowed template")
	}
	if payload.Data.Reason != models.ShareStubCaseTemplateDisabled {
		t.Fatalf("expected reason %q, got %q", models.ShareStubCaseTemplateDisabled, payload.Data.Reason)
	}
}
