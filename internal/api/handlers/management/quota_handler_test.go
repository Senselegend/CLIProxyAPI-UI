package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
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

func TestPostQuotaRecoveryReturnsTruthfulTriggerSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalQuotaTrigger := triggerQuotaRecovery
	triggerQuotaRecovery = func(context.Context) QuotaRecoveryTriggerResult {
		return QuotaRecoveryTriggerResult{
			Triggered:      true,
			AlreadyRunning: true,
			StartupState:   "syncing",
			MissingRuntime: true,
		}
	}
	defer func() { triggerQuotaRecovery = originalQuotaTrigger }()

	originalAuthTrigger := triggerEligibleAuthRechecks
	triggerEligibleAuthRechecks = func(_ *coreauth.Manager, _ context.Context) coreauth.AuthRecheckSummary {
		return coreauth.AuthRecheckSummary{
			Considered:         4,
			Triggered:          1,
			AlreadyInFlight:    2,
			SkippedRateLimited: 1,
			SkippedDisabled:    0,
			SkippedDeactivated: 0,
			SkippedNotEligible: 0,
		}
	}
	defer func() { triggerEligibleAuthRechecks = originalAuthTrigger }()

	h := &Handler{}
	h.SetAuthManager(coreauth.NewManager(nil, nil, nil))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/quotas/recover", nil)

	h.PostQuotaRecovery(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	quotaRuntime, ok := response["quota_runtime"].(map[string]any)
	if !ok {
		t.Fatalf("quota_runtime = %#v, want object", response["quota_runtime"])
	}
	if got := quotaRuntime["triggered"]; got != true {
		t.Fatalf("quota_runtime.triggered = %#v, want true", got)
	}
	if got := quotaRuntime["already_running"]; got != true {
		t.Fatalf("quota_runtime.already_running = %#v, want true", got)
	}
	if got := quotaRuntime["startup_state"]; got != "syncing" {
		t.Fatalf("quota_runtime.startup_state = %#v, want syncing", got)
	}
	if got := quotaRuntime["missing_runtime"]; got != true {
		t.Fatalf("quota_runtime.missing_runtime = %#v, want true", got)
	}

	authRecheck, ok := response["auth_recheck"].(map[string]any)
	if !ok {
		t.Fatalf("auth_recheck = %#v, want object", response["auth_recheck"])
	}
	if got := authRecheck["considered"]; got != float64(4) {
		t.Fatalf("auth_recheck.considered = %#v, want 4", got)
	}
	if got := authRecheck["triggered"]; got != float64(1) {
		t.Fatalf("auth_recheck.triggered = %#v, want 1", got)
	}
	if got := authRecheck["already_in_flight"]; got != float64(2) {
		t.Fatalf("auth_recheck.already_in_flight = %#v, want 2", got)
	}
	if got := authRecheck["skipped_rate_limited"]; got != float64(1) {
		t.Fatalf("auth_recheck.skipped_rate_limited = %#v, want 1", got)
	}
}

func TestPostQuotaRecoveryReportsMissingAuthRuntimeTruthfully(t *testing.T) {
	gin.SetMode(gin.TestMode)

	originalQuotaTrigger := triggerQuotaRecovery
	triggerQuotaRecovery = func(context.Context) QuotaRecoveryTriggerResult {
		return QuotaRecoveryTriggerResult{Triggered: true, StartupState: "ready"}
	}
	defer func() { triggerQuotaRecovery = originalQuotaTrigger }()

	h := &Handler{}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/quotas/recover", nil)

	h.PostQuotaRecovery(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	authRecheck, ok := response["auth_recheck"].(map[string]any)
	if !ok {
		t.Fatalf("auth_recheck = %#v, want object", response["auth_recheck"])
	}
	if got := authRecheck["missing_runtime"]; got != true {
		t.Fatalf("auth_recheck.missing_runtime = %#v, want true", got)
	}
	if got := authRecheck["triggered"]; got != float64(0) {
		t.Fatalf("auth_recheck.triggered = %#v, want 0", got)
	}
}

func TestPostQuotaRecoveryKeepsAuthRecheckEligibilityPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)

	manager := coreauth.NewManager(nil, nil, nil)

	for _, auth := range []*coreauth.Auth{
		{ID: "error-auth", Status: coreauth.StatusError, Unavailable: true, LastError: &coreauth.Error{HTTPStatus: 500, Message: "temporary failure"}},
		{ID: "unknown-auth", Status: coreauth.StatusUnknown, Unavailable: true},
		{ID: "rate-limited-auth", Status: coreauth.StatusRateLimited, Unavailable: true},
		{ID: "disabled-auth", Status: coreauth.StatusError, Disabled: true},
		{ID: "deactivated-auth", Status: coreauth.StatusDeactivated},
	} {
		if _, err := manager.Register(context.Background(), auth); err != nil {
			t.Fatalf("register auth %s: %v", auth.ID, err)
		}
	}

	originalQuotaTrigger := triggerQuotaRecovery
	triggerQuotaRecovery = func(context.Context) QuotaRecoveryTriggerResult {
		return QuotaRecoveryTriggerResult{Triggered: true, StartupState: "ready"}
	}
	defer func() { triggerQuotaRecovery = originalQuotaTrigger }()

	h := &Handler{}
	h.SetAuthManager(manager)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/quotas/recover", nil)

	h.PostQuotaRecovery(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	authRecheck, ok := response["auth_recheck"].(map[string]any)
	if !ok {
		t.Fatalf("auth_recheck = %#v, want object", response["auth_recheck"])
	}
	if got := authRecheck["considered"]; got != float64(5) {
		t.Fatalf("auth_recheck.considered = %#v, want 5", got)
	}
	if got := authRecheck["triggered"]; got != float64(2) {
		t.Fatalf("auth_recheck.triggered = %#v, want 2", got)
	}
	if got := authRecheck["skipped_rate_limited"]; got != float64(1) {
		t.Fatalf("auth_recheck.skipped_rate_limited = %#v, want 1", got)
	}
	if got := authRecheck["skipped_disabled"]; got != float64(1) {
		t.Fatalf("auth_recheck.skipped_disabled = %#v, want 1", got)
	}
	if got := authRecheck["skipped_deactivated"]; got != float64(1) {
		t.Fatalf("auth_recheck.skipped_deactivated = %#v, want 1", got)
	}
	if got := authRecheck["skipped_not_eligible"]; got != float64(0) {
		t.Fatalf("auth_recheck.skipped_not_eligible = %#v, want 0", got)
	}
}
