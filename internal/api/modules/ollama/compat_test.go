package ollama

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestVersionHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/version", nil)
	VersionHandler()(c)

	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"version":"0.4.0"`) {
		t.Fatalf("bad version response code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestTagsHandlerListsMappedModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mapper := NewModelMapper([]config.OllamaModelMapping{{From: "gemma4:e2b", To: "kimi"}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	TagsHandler(mapper)(c)

	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"name":"gemma4:e2b"`) || !strings.Contains(body, `"models"`) {
		t.Fatalf("bad tags response code=%d body=%s", rec.Code, body)
	}
}

func TestShowHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mapper := NewModelMapper([]config.OllamaModelMapping{{From: "gemma4:e2b", To: "kimi"}})
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/show", strings.NewReader(`{"name":"gemma4:e2b"}`))
	ShowHandler(mapper)(c)

	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"details"`) || !strings.Contains(body, `"model_info"`) {
		t.Fatalf("bad show response code=%d body=%s", rec.Code, body)
	}
}

func TestShowHandlerUnknownModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/show", strings.NewReader(`{"name":"missing"}`))
	ShowHandler(NewModelMapper(nil))(c)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
