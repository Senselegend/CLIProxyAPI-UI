package management

import (
	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

// GetAccountUsageStatistics returns usage statistics grouped by account (email).
func (h *Handler) GetAccountUsageStatistics(c *gin.Context) {
	c.JSON(200, usage.GetRequestUsageStats())
}
