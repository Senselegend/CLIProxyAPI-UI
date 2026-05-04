package ollama

import (
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

func NewEmbeddingsProxy(upstream string) (*httputil.ReverseProxy, error) {
	raw := strings.TrimSpace(upstream)
	if raw == "" {
		return nil, nil
	}
	target, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	originalDirector := proxy.Director
	proxy.Director = func(r *http.Request) {
		originalDirector(r)
		r.URL.Path = "/api/embeddings"
		r.Header.Del("Authorization")
		r.Header.Del("Proxy-Authorization")
		r.Header.Del("Cookie")
	}
	return proxy, nil
}

func EmbeddingsHandler(proxy *httputil.ReverseProxy) gin.HandlerFunc {
	return func(c *gin.Context) {
		if proxy == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "ollama embeddings upstream is not configured"})
			return
		}
		proxy.ServeHTTP(c.Writer, c.Request)
	}
}
