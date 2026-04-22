package management

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

var triggerEligibleAuthRechecks = func(manager *coreauth.Manager, ctx context.Context) coreauth.AuthRecheckSummary {
	return manager.TriggerEligibleAuthRechecks(ctx)
}

func (h *Handler) PostAuthFilesRecheck(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	c.JSON(http.StatusOK, triggerEligibleAuthRechecks(h.authManager, c.Request.Context()))
}
