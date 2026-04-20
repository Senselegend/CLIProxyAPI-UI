package management

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

var (
	quotaStartupStateGetter   = func() string { return "" }
	quotaStartupMessageGetter = func() string { return "" }
)

func SetQuotaStartupGetters(stateGetter, messageGetter func() string) {
	if stateGetter != nil {
		quotaStartupStateGetter = stateGetter
	}
	if messageGetter != nil {
		quotaStartupMessageGetter = messageGetter
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
