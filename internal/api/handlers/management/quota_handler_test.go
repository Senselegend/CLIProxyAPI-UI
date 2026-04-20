package management

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestGetQuotasIncludesStartupSyncMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	SetQuotaStartupGetters(
		func() string { return "ready" },
		func() string { return "startup sync complete" },
	)
	defer SetQuotaStartupGetters(
		func() string { return "" },
		func() string { return "" },
	)

	h := NewHandler(&config.Config{}, "", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/quotas", nil)

	h.GetQuotas(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if _, ok := response["quotas"]; !ok {
		t.Fatalf("response missing quotas: %s", w.Body.String())
	}
	if _, ok := response["summary"]; !ok {
		t.Fatalf("response missing summary: %s", w.Body.String())
	}

	startupSync, ok := response["startup_sync"].(map[string]any)
	if !ok {
		t.Fatalf("response missing startup_sync object: %s", w.Body.String())
	}

	if got := startupSync["state"]; got != "ready" {
		t.Fatalf("startup_sync.state = %#v, want %q", got, "ready")
	}
	if got := startupSync["message"]; got != "startup sync complete" {
		t.Fatalf("startup_sync.message = %#v, want %q", got, "startup sync complete")
	}
}
