package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

func TestPostAuthFilesRecheckReturnsSummary(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/recheck", nil)

	h := &Handler{}
	h.SetAuthManager(coreauth.NewManager(nil, nil, nil))

	want := coreauth.AuthRecheckSummary{
		Considered:         5,
		Triggered:          2,
		AlreadyInFlight:    1,
		SkippedRateLimited: 1,
		SkippedDisabled:    1,
	}

	original := triggerEligibleAuthRechecks
	triggerEligibleAuthRechecks = func(_ *coreauth.Manager, _ context.Context) coreauth.AuthRecheckSummary {
		return want
	}
	defer func() { triggerEligibleAuthRechecks = original }()

	h.PostAuthFilesRecheck(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got["considered"] != float64(want.Considered) {
		t.Fatalf("considered = %v, want %d", got["considered"], want.Considered)
	}
	if got["triggered"] != float64(want.Triggered) {
		t.Fatalf("triggered = %v, want %d", got["triggered"], want.Triggered)
	}
	if got["already_in_flight"] != float64(want.AlreadyInFlight) {
		t.Fatalf("already_in_flight = %v, want %d", got["already_in_flight"], want.AlreadyInFlight)
	}
	if got["skipped_rate_limited"] != float64(want.SkippedRateLimited) {
		t.Fatalf("skipped_rate_limited = %v, want %d", got["skipped_rate_limited"], want.SkippedRateLimited)
	}
	if got["skipped_disabled"] != float64(want.SkippedDisabled) {
		t.Fatalf("skipped_disabled = %v, want %d", got["skipped_disabled"], want.SkippedDisabled)
	}
	recovery, ok := got["recovery"].(map[string]any)
	if !ok {
		t.Fatalf("recovery = %#v, want object", got["recovery"])
	}
	if inFlightCount := recovery["in_flight_count"]; inFlightCount != float64(0) {
		t.Fatalf("recovery.in_flight_count = %v, want 0", inFlightCount)
	}
}

func TestPostAuthFilesRecheckReturnsSummaryWithRecoverySnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/recheck", nil)

	h := &Handler{}
	h.SetAuthManager(coreauth.NewManager(nil, nil, nil))

	wantSummary := coreauth.AuthRecheckSummary{
		Considered:         5,
		Triggered:          2,
		AlreadyInFlight:    1,
		SkippedRateLimited: 1,
		SkippedDisabled:    1,
	}
	lastRunAt := time.Date(2026, time.April, 22, 12, 34, 56, 0, time.UTC)

	originalTrigger := triggerEligibleAuthRechecks
	triggerEligibleAuthRechecks = func(_ *coreauth.Manager, _ context.Context) coreauth.AuthRecheckSummary {
		return wantSummary
	}
	defer func() { triggerEligibleAuthRechecks = originalTrigger }()

	originalSnapshot := authRecheckSnapshotGetter
	authRecheckSnapshotGetter = func(_ *coreauth.Manager) coreauth.RecheckSnapshot {
		return coreauth.RecheckSnapshot{
			InFlightCount: 1,
			InFlight: map[string]bool{
				"auth-a": true,
			},
			LastRunAt: map[string]time.Time{
				"auth-a": lastRunAt,
			},
		}
	}
	defer func() { authRecheckSnapshotGetter = originalSnapshot }()

	h.PostAuthFilesRecheck(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got["considered"] != float64(wantSummary.Considered) {
		t.Fatalf("considered = %v, want %d", got["considered"], wantSummary.Considered)
	}
	if got["triggered"] != float64(wantSummary.Triggered) {
		t.Fatalf("triggered = %v, want %d", got["triggered"], wantSummary.Triggered)
	}

	recovery, ok := got["recovery"].(map[string]any)
	if !ok {
		t.Fatalf("recovery = %#v, want object", got["recovery"])
	}
	if recovery["in_flight_count"] != float64(1) {
		t.Fatalf("recovery.in_flight_count = %#v, want 1", recovery["in_flight_count"])
	}
	inFlight, ok := recovery["in_flight"].(map[string]any)
	if !ok {
		t.Fatalf("recovery.in_flight = %#v, want object", recovery["in_flight"])
	}
	if inFlight["auth-a"] != true {
		t.Fatalf("recovery.in_flight[auth-a] = %#v, want true", inFlight["auth-a"])
	}
	lastRuns, ok := recovery["last_run_at"].(map[string]any)
	if !ok {
		t.Fatalf("recovery.last_run_at = %#v, want object", recovery["last_run_at"])
	}
	if lastRuns["auth-a"] != lastRunAt.Format(time.RFC3339) {
		t.Fatalf("recovery.last_run_at[auth-a] = %#v, want %s", lastRuns["auth-a"], lastRunAt.Format(time.RFC3339))
	}
}

func TestListAuthFilesIncludesRecoveryMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	filePath := filepath.Join(authDir, "codex-user.json")
	if err := os.WriteFile(filePath, []byte(`{"type":"codex","email":"user@example.com"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:       "auth-a",
		FileName: "codex-user.json",
		Provider: "codex",
		Status:   coreauth.StatusError,
		Attributes: map[string]string{
			"path": filePath,
		},
		Metadata: map[string]any{
			"email": "user@example.com",
		},
	}); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)
	lastRunAt := time.Date(2026, time.April, 22, 9, 8, 7, 0, time.UTC)

	originalSnapshot := authRecheckSnapshotGetter
	authRecheckSnapshotGetter = func(_ *coreauth.Manager) coreauth.RecheckSnapshot {
		return coreauth.RecheckSnapshot{
			InFlightCount: 1,
			InFlight: map[string]bool{
				"auth-a": true,
			},
			LastRunAt: map[string]time.Time{
				"auth-a": lastRunAt,
			},
		}
	}
	defer func() { authRecheckSnapshotGetter = originalSnapshot }()

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)

	h.ListAuthFiles(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	files, ok := payload["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("files = %#v, want single entry", payload["files"])
	}
	entry, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("file entry = %#v, want object", files[0])
	}
	if entry["status"] != string(coreauth.StatusError) {
		t.Fatalf("status = %#v, want %q", entry["status"], coreauth.StatusError)
	}
	recovery, ok := entry["recovery"].(map[string]any)
	if !ok {
		t.Fatalf("recovery = %#v, want object", entry["recovery"])
	}
	if recovery["in_flight"] != true {
		t.Fatalf("recovery.in_flight = %#v, want true", recovery["in_flight"])
	}
	if recovery["last_run_at"] != lastRunAt.Format(time.RFC3339) {
		t.Fatalf("recovery.last_run_at = %#v, want %s", recovery["last_run_at"], lastRunAt.Format(time.RFC3339))
	}
}

func TestListAuthFilesExposesDeactivatedStatusForInvalidatedAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	filePath := filepath.Join(authDir, "codex-user.json")
	if err := os.WriteFile(filePath, []byte(`{"type":"codex","email":"user@example.com"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	manager := coreauth.NewManager(nil, nil, nil)
	if _, err := manager.Register(context.Background(), &coreauth.Auth{
		ID:            "auth-a",
		FileName:      "codex-user.json",
		Provider:      "codex",
		Status:        coreauth.StatusDeactivated,
		StatusMessage: "token_invalidated",
		Unavailable:   true,
		LastError:     &coreauth.Error{HTTPStatus: http.StatusUnauthorized, Message: "token_invalidated"},
		Attributes: map[string]string{
			"path": filePath,
		},
		Metadata: map[string]any{
			"email": "user@example.com",
		},
	}); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, manager)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)

	h.ListAuthFiles(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	files, ok := payload["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("files = %#v, want single entry", payload["files"])
	}
	entry, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("file entry = %#v, want object", files[0])
	}
	if entry["status"] != string(coreauth.StatusDeactivated) {
		t.Fatalf("status = %#v, want %q", entry["status"], coreauth.StatusDeactivated)
	}
	if entry["status_message"] != "token_invalidated" {
		t.Fatalf("status_message = %#v, want %q", entry["status_message"], "token_invalidated")
	}
	if entry["unavailable"] != true {
		t.Fatalf("unavailable = %#v, want true", entry["unavailable"])
	}
}

func TestListAuthFilesFromDiskIncludesRecoveryShape(t *testing.T) {
	gin.SetMode(gin.TestMode)

	authDir := t.TempDir()
	filePath := filepath.Join(authDir, "codex-user.json")
	if err := os.WriteFile(filePath, []byte(`{"type":"codex","email":"user@example.com"}`), 0o600); err != nil {
		t.Fatalf("write auth file: %v", err)
	}

	h := NewHandlerWithoutConfigFilePath(&config.Config{AuthDir: authDir}, nil)

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/auth-files", nil)

	h.ListAuthFiles(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	files, ok := payload["files"].([]any)
	if !ok || len(files) != 1 {
		t.Fatalf("files = %#v, want single entry", payload["files"])
	}
	entry, ok := files[0].(map[string]any)
	if !ok {
		t.Fatalf("file entry = %#v, want object", files[0])
	}
	recovery, ok := entry["recovery"].(map[string]any)
	if !ok {
		t.Fatalf("recovery = %#v, want object", entry["recovery"])
	}
	if recovery["in_flight"] != false {
		t.Fatalf("recovery.in_flight = %#v, want false", recovery["in_flight"])
	}
	if _, exists := recovery["last_run_at"]; exists {
		t.Fatalf("recovery.last_run_at = %#v, want absent", recovery["last_run_at"])
	}
}

func TestPostAuthFilesRecheckWithNilManagerReturnsServiceUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/v0/management/auth-files/recheck", nil)

	h := &Handler{}
	h.PostAuthFilesRecheck(c)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
	if body := w.Body.String(); body != "{\"error\":\"core auth manager unavailable\"}" {
		t.Fatalf("body = %s, want core auth manager unavailable error", body)
	}
}
