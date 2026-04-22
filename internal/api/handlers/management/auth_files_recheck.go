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

var authRecheckSnapshotGetter = func(manager *coreauth.Manager) coreauth.RecheckSnapshot {
	return manager.AuthRecheckSnapshot()
}

func (h *Handler) PostAuthFilesRecheck(c *gin.Context) {
	if h == nil || h.authManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "core auth manager unavailable"})
		return
	}
	summary := triggerEligibleAuthRechecks(h.authManager, c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"considered":           summary.Considered,
		"triggered":            summary.Triggered,
		"already_in_flight":    summary.AlreadyInFlight,
		"skipped_rate_limited": summary.SkippedRateLimited,
		"skipped_disabled":     summary.SkippedDisabled,
		"skipped_deactivated":  summary.SkippedDeactivated,
		"skipped_not_eligible": summary.SkippedNotEligible,
		"recovery":             authRecheckSnapshotGetter(h.authManager),
	})
}
