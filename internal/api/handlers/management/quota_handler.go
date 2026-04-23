package management

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

type QuotaRecoveryTriggerResult struct {
	Triggered      bool   `json:"triggered"`
	AlreadyRunning bool   `json:"already_running"`
	StartupState   string `json:"startup_state"`
	MissingRuntime bool   `json:"missing_runtime"`
}

var (
	quotaStartupStateGetter   = func() string { return "" }
	quotaStartupMessageGetter = func() string { return "" }
	triggerQuotaRecovery      = func(context.Context) QuotaRecoveryTriggerResult { return QuotaRecoveryTriggerResult{} }
)

func SetQuotaStartupGetters(stateGetter, messageGetter func() string) {
	if stateGetter != nil {
		quotaStartupStateGetter = stateGetter
	}
	if messageGetter != nil {
		quotaStartupMessageGetter = messageGetter
	}
}

func SetQuotaRecoveryTrigger(trigger func(context.Context) QuotaRecoveryTriggerResult) {
	if trigger != nil {
		triggerQuotaRecovery = trigger
	}
}

// GetQuotas returns all account quotas plus aggregate summaries.
func (h *Handler) GetQuotas(c *gin.Context) {
	quotas := usage.GetQuotaStore().Snapshot()
	summary := usage.AggregateQuotaSummary{
		PrimaryUsedPercent:   usage.AggregateQuotas(quotas, "primary"),
		SecondaryUsedPercent: usage.AggregateQuotas(quotas, "secondary"),
	}
	c.JSON(http.StatusOK, gin.H{
		"quotas":  quotas,
		"summary": summary,
		"startup_sync": gin.H{
			"state":   quotaStartupStateGetter(),
			"message": quotaStartupMessageGetter(),
		},
	})
}

// GetQuota returns quota for a specific account.
func (h *Handler) GetQuota(c *gin.Context) {
	accountID := c.Param("accountId")
	if accountID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "account_id required"})
		return
	}

	quota := usage.GetQuotaStore().Get(accountID)
	if quota == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "quota not found"})
		return
	}
	c.JSON(http.StatusOK, quota)
}

func (h *Handler) PostQuotaRecovery(c *gin.Context) {
	quotaRuntime := triggerQuotaRecovery(c.Request.Context())

	authRecheck := gin.H{
		"missing_runtime":      h == nil || h.authManager == nil,
		"considered":           0,
		"triggered":            0,
		"already_in_flight":    0,
		"skipped_rate_limited": 0,
		"skipped_disabled":     0,
		"skipped_deactivated":  0,
		"skipped_not_eligible": 0,
	}
	if h != nil && h.authManager != nil {
		authSummary := triggerEligibleAuthRechecks(h.authManager, c.Request.Context())
		authRecheck = gin.H{
			"missing_runtime":      false,
			"considered":           authSummary.Considered,
			"triggered":            authSummary.Triggered,
			"already_in_flight":    authSummary.AlreadyInFlight,
			"skipped_rate_limited": authSummary.SkippedRateLimited,
			"skipped_disabled":     authSummary.SkippedDisabled,
			"skipped_deactivated":  authSummary.SkippedDeactivated,
			"skipped_not_eligible": authSummary.SkippedNotEligible,
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"quota_runtime": quotaRuntime,
		"auth_recheck":  authRecheck,
	})
}
