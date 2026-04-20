package activity

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMiddlewareRecordsWebsocketTransport(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore(10)
	router := gin.New()
	router.Use(MiddlewareWithStore(store))
	router.GET("/v1/realtime", func(c *gin.Context) { c.Status(http.StatusSwitchingProtocols) })

	req := httptest.NewRequest(http.MethodGet, "/v1/realtime", nil)
	req.Header.Set("Upgrade", "websocket")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	entries := store.Snapshot(SnapshotOptions{Limit: 10})
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	if entries[0].DownstreamTransport != "websocket" {
		t.Fatalf("transport = %q, want websocket", entries[0].DownstreamTransport)
	}
	if entries[0].HTTPStatus != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", entries[0].HTTPStatus, http.StatusSwitchingProtocols)
	}
}

func TestMiddlewareSkipsManagementRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := NewStore(10)
	router := gin.New()
	router.Use(MiddlewareWithStore(store))
	router.GET("/v0/management/usage", func(c *gin.Context) { c.Status(http.StatusOK) })

	req := httptest.NewRequest(http.MethodGet, "/v0/management/usage", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	entries := store.Snapshot(SnapshotOptions{Limit: 10})
	if len(entries) != 0 {
		t.Fatalf("entries len = %d, want 0", len(entries))
	}
}
