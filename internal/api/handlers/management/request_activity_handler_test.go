package management

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/console/activity"
)

func TestGetRequestActivityReturnsEntries(t *testing.T) {
	gin.SetMode(gin.TestMode)
	activity.DefaultStore().Reset()
	activity.DefaultStore().Start(activity.StartEvent{
		ID:        "req_1",
		Method:    "GET",
		Path:      "/v1/models",
		StartedAt: time.Now(),
	})

	h := NewHandler(&config.Config{}, "", nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/v0/management/request-activity?limit=10", nil)

	h.GetRequestActivity(c)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"req_1"`) {
		t.Fatalf("response missing entry: %s", w.Body.String())
	}
}
