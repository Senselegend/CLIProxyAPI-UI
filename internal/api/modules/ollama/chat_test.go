package ollama

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestChatHandlerDispatchesMappedStreamingRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mapper := NewModelMapper([]config.OllamaModelMapping{{From: "ollama-model", To: "openai-model"}})
	var downstreamBody string
	handler := ChatHandler(mapper, func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		downstreamBody = string(body)
		if c.Request.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path = %s", c.Request.URL.Path)
		}
		c.Writer.WriteHeader(http.StatusOK)
		c.Writer.Write([]byte(`data: {"choices":[{"delta":{"content":"hi"}}]}` + "\n\n"))
		c.Writer.Write([]byte("data: [DONE]\n\n"))
	})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"model":"ollama-model","messages":[{"role":"user","content":"hello"}]}`))
	handler(c)

	if !strings.Contains(downstreamBody, `"model":"openai-model"`) || !strings.Contains(downstreamBody, `"stream":true`) {
		t.Fatalf("downstream body not translated: %s", downstreamBody)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"content":"hi"`) || !strings.Contains(body, `"done":true`) {
		t.Fatalf("response not translated to Ollama stream:\n%s", body)
	}
}

func TestChatHandlerUnknownModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := ChatHandler(NewModelMapper(nil), func(c *gin.Context) {
		t.Fatal("downstream should not be called")
	})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"model":"missing"}`))
	handler(c)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestChatHandlerDispatchesBufferedRequest(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mapper := NewModelMapper([]config.OllamaModelMapping{{From: "ollama-model", To: "openai-model"}})
	handler := ChatHandler(mapper, func(c *gin.Context) {
		c.Writer.WriteHeader(http.StatusOK)
		c.Writer.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hello"}}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`))
	})

	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"model":"ollama-model","stream":false,"messages":[{"role":"user","content":"hello"}]}`))
	handler(c)

	body := rec.Body.String()
	for _, want := range []string{`"model":"ollama-model"`, `"content":"hello"`, `"done":true`, `"prompt_eval_count":4`, `"eval_count":2`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s in body:\n%s", want, body)
		}
	}
}
