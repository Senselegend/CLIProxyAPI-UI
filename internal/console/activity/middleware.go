package activity

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
)

func Middleware() gin.HandlerFunc {
	return MiddlewareWithStore(DefaultStore())
}

func MiddlewareWithStore(store *Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if store == nil || shouldSkipActivity(c.Request) {
			c.Next()
			return
		}

		startedAt := time.Now()
		id := logging.GetGinRequestID(c)
		if id == "" {
			id = fmt.Sprintf("activity_%d", startedAt.UnixNano())
		}
		path := ""
		method := ""
		transport := "http"
		if c.Request != nil {
			method = c.Request.Method
			if c.Request.URL != nil {
				path = c.Request.URL.Path
			}
			transport = downstreamTransport(c.Request)
		}

		store.Start(StartEvent{
			ID:                  id,
			Method:              method,
			Path:                path,
			DownstreamTransport: transport,
			StartedAt:           startedAt,
		})

		c.Next()

		finishedAt := time.Now()
		store.Finish(FinishEvent{
			ID:         id,
			HTTPStatus: c.Writer.Status(),
			Latency:    finishedAt.Sub(startedAt),
			FinishedAt: finishedAt,
		})
	}
}

func shouldSkipActivity(req *http.Request) bool {
	if req == nil || req.URL == nil {
		return true
	}
	path := req.URL.Path
	return strings.HasPrefix(path, "/v0/management") || strings.HasPrefix(path, "/management")
}

func downstreamTransport(req *http.Request) string {
	if req == nil {
		return "http"
	}
	for _, value := range req.Header.Values("Upgrade") {
		if strings.EqualFold(strings.TrimSpace(value), "websocket") {
			return "websocket"
		}
	}
	return "http"
}
