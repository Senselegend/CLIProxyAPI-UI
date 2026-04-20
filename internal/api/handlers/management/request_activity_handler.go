package management

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/console/activity"
)

func (h *Handler) GetRequestActivity(c *gin.Context) {
	limit := parseActivityLimit(c.Query("limit"), 100)
	c.JSON(http.StatusOK, gin.H{
		"entries": activity.DefaultStore().Snapshot(activity.SnapshotOptions{Limit: limit}),
	})
}

func parseActivityLimit(raw string, fallback int) int {
	value := strings.TrimSpace(raw)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}
