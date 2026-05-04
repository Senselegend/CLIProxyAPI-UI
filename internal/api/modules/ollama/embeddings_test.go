package ollama

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

type proxyRecorder struct {
	*httptest.ResponseRecorder
}

func (r *proxyRecorder) CloseNotify() <-chan bool {
	return make(chan bool)
}

func TestEmbeddingsPassthrough(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var gotBody string
	var gotAuth string
	var gotProxyAuth string
	var gotCookie string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embeddings" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotProxyAuth = r.Header.Get("Proxy-Authorization")
		gotCookie = r.Header.Get("Cookie")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.Header().Set("X-Upstream", "ok")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"embedding":[1,2,3]}`))
	}))
	defer upstream.Close()

	proxy, err := NewEmbeddingsProxy(upstream.URL)
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	rec := &proxyRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/embeddings", strings.NewReader(`{"model":"nomic","prompt":"hi"}`))
	c.Request.Header.Set("Authorization", "Bearer local-key")
	c.Request.Header.Set("Proxy-Authorization", "Basic proxy-key")
	c.Request.Header.Set("Cookie", "session=secret")
	EmbeddingsHandler(proxy)(c)

	if gotAuth != "" {
		t.Fatalf("authorization leaked upstream: %q", gotAuth)
	}
	if gotProxyAuth != "" {
		t.Fatalf("proxy authorization leaked upstream: %q", gotProxyAuth)
	}
	if gotCookie != "" {
		t.Fatalf("cookie leaked upstream: %q", gotCookie)
	}
	if gotBody != `{"model":"nomic","prompt":"hi"}` {
		t.Fatalf("body = %s", gotBody)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != `{"embedding":[1,2,3]}` {
		t.Fatalf("bad response code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-Upstream"); got != "ok" {
		t.Fatalf("upstream header = %q", got)
	}
}

func TestEmbeddingsMissingUpstream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := &proxyRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/embeddings", strings.NewReader(`{}`))
	EmbeddingsHandler(nil)(c)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
}

func TestEmbeddingsPropagatesUpstream5xx(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("bad upstream"))
	}))
	defer upstream.Close()

	proxy, err := NewEmbeddingsProxy(upstream.URL)
	if err != nil {
		t.Fatalf("proxy: %v", err)
	}
	rec := &proxyRecorder{ResponseRecorder: httptest.NewRecorder()}
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/embeddings", strings.NewReader(`{}`))
	EmbeddingsHandler(proxy)(c)

	if rec.Code != http.StatusBadGateway || rec.Body.String() != "bad upstream" {
		t.Fatalf("bad response code=%d body=%s", rec.Code, rec.Body.String())
	}
}
