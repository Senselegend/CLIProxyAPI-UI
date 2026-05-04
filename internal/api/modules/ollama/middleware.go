package ollama

import (
	"net"
	"net/http"

	"github.com/gin-gonic/gin"
	log "github.com/sirupsen/logrus"
)

func (m *OllamaModule) enabledMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !m.isEnabled() {
			c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"error": "ollama module is disabled"})
			return
		}
		c.Next()
	}
}

func (m *OllamaModule) localhostOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !m.isRestrictedToLocalhost() {
			c.Next()
			return
		}
		host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
		if err != nil {
			host = c.Request.RemoteAddr
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			log.Warnf("ollama: non-localhost connection from %s attempted access, denying", c.Request.RemoteAddr)
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Access denied: Ollama routes restricted to localhost"})
			return
		}
		c.Next()
	}
}

func noCORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "")
		c.Header("Access-Control-Allow-Methods", "")
		c.Header("Access-Control-Allow-Headers", "")
		c.Header("Access-Control-Allow-Credentials", "")
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusForbidden)
			return
		}
		c.Next()
	}
}
