package ollama

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/modules"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestOllamaRoutesEnabledAndVersionPublic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	m := New()
	err := m.Register(modules.Context{Engine: engine, Config: &config.Config{Ollama: config.Ollama{
		Enabled:       false,
		LocalhostOnly: false,
		ModelMappings: []config.OllamaModelMapping{{From: "m", To: "mapped"}},
	}}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	rootHead := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/", nil)
	engine.ServeHTTP(rootHead, req)
	if rootHead.Code != http.StatusOK {
		t.Fatalf("root HEAD status=%d body=%s", rootHead.Code, rootHead.Body.String())
	}

	rootGet := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	engine.ServeHTTP(rootGet, req)
	if rootGet.Code != http.StatusOK {
		t.Fatalf("root GET status=%d body=%s", rootGet.Code, rootGet.Body.String())
	}

	version := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/version", nil)
	engine.ServeHTTP(version, req)
	if version.Code != http.StatusOK {
		t.Fatalf("version status=%d body=%s", version.Code, version.Body.String())
	}

	chat := httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"model":"m"}`))
	engine.ServeHTTP(chat, req)
	if chat.Code != http.StatusNotFound {
		t.Fatalf("disabled chat status=%d body=%s", chat.Code, chat.Body.String())
	}
}

func TestOllamaRoutesLocalhostOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	m := New()
	err := m.Register(modules.Context{Engine: engine, Config: &config.Config{Ollama: config.Ollama{
		Enabled:       true,
		LocalhostOnly: true,
		ModelMappings: []config.OllamaModelMapping{{From: "m", To: "mapped"}},
	}}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
	req.RemoteAddr = "10.0.0.5:1234"
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOllamaRoutesNoCORSOptions(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	m := New()
	err := m.Register(modules.Context{Engine: engine, Config: &config.Config{Ollama: config.Ollama{Enabled: true}}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodOptions, "/api/chat", nil)
	engine.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOllamaConfigUpdateRefreshesMappings(t *testing.T) {
	m := New()
	engine := gin.New()
	err := m.Register(modules.Context{Engine: engine, Config: &config.Config{Ollama: config.Ollama{
		Enabled:       true,
		ModelMappings: []config.OllamaModelMapping{{From: "old", To: "mapped"}},
	}}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := m.OnConfigUpdated(&config.Config{Ollama: config.Ollama{Enabled: true, ModelMappings: []config.OllamaModelMapping{{From: "new", To: "mapped"}}}}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, ok := m.getModelMapper().Resolve("new"); !ok {
		t.Fatalf("new mapping not applied")
	}
}

func TestOllamaConfigUpdateConcurrentRequests(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	m := New()
	err := m.Register(modules.Context{Engine: engine, Config: &config.Config{Ollama: config.Ollama{
		Enabled:       true,
		LocalhostOnly: false,
		ModelMappings: []config.OllamaModelMapping{{From: "m", To: "mapped"}},
	}}})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_ = m.OnConfigUpdated(&config.Config{Ollama: config.Ollama{
				Enabled:       i%2 == 0,
				LocalhostOnly: false,
				ModelMappings: []config.OllamaModelMapping{{From: "m", To: "mapped"}},
			}})
		}(i)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "/api/tags", nil)
			engine.ServeHTTP(rec, req)
		}()
	}
	wg.Wait()
}
