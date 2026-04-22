package management

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
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

	var got coreauth.AuthRecheckSummary
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got != want {
		t.Fatalf("summary = %+v, want %+v", got, want)
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
