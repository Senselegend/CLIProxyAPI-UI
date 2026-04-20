package usage

import (
	"strings"
	"time"
)

// QuotaWindow represents a single usage window (primary/secondary).
type QuotaWindow struct {
	UsedPercent   float64 `json:"used_percent"`
	Capacity      float64 `json:"capacity,omitempty"`
	UsedCredits   float64 `json:"used_credits,omitempty"`
	ResetAt       int64   `json:"reset_at,omitempty"`
	WindowMinutes int     `json:"window_minutes,omitempty"`
}

// Plan credit capacities for weighted-average aggregation.
const (
	// Primary (5h window) capacities by plan type.
	CapacityPrimaryPlus = 225.0
	CapacityPrimaryPro  = 1500.0
	// Secondary (7d window) = primary × 30 roughly.
	CapacitySecondaryPlus = 7560.0
	CapacitySecondaryPro  = 50400.0
)

// PlanCapacity returns the credit capacity for a given plan type and window.
// Returns nil when used_percent is also nil (no data).
func (w *QuotaWindow) capacity(window string, planType string) *float64 {
	if w == nil {
		return nil
	}
	if w.Capacity > 0 {
		return &w.Capacity
	}
	var cap float64
	switch window {
	case "primary":
		switch strings.ToLower(planType) {
		case "plus", "business", "team":
			cap = CapacityPrimaryPlus
		case "pro":
			cap = CapacityPrimaryPro
		default:
			cap = CapacityPrimaryPlus // conservative default
		}
	case "secondary":
		switch strings.ToLower(planType) {
		case "plus", "business", "team":
			cap = CapacitySecondaryPlus
		case "pro":
			cap = CapacitySecondaryPro
		default:
			cap = CapacitySecondaryPlus
		}
	}
	return &cap
}

func (w *QuotaWindow) hasData() bool {
	if w == nil {
		return false
	}
	return w.UsedPercent != 0 ||
		w.Capacity != 0 ||
		w.UsedCredits != 0 ||
		w.ResetAt != 0 ||
		w.WindowMinutes != 0
}

// AggregateQuotaSummary holds weighted-average used_percent across all accounts.
type AggregateQuotaSummary struct {
	PrimaryUsedPercent   float64 `json:"primary_used_percent"`
	SecondaryUsedPercent float64 `json:"secondary_used_percent"`
}

// AggregateQuotas computes weighted-average used_percent from a snapshot.
func AggregateQuotas(quotas []AccountQuota, window string) (avg float64) {
	var totalCap, totalUsed float64
	for _, q := range quotas {
		var w *QuotaWindow
		if window == "primary" {
			w = q.PrimaryWindow
		} else {
			w = q.SecondaryWindow
		}
		if !w.hasData() {
			continue
		}
		cap := w.capacity(window, q.PlanType)
		if cap == nil {
			continue
		}
		totalCap += *cap
		totalUsed += (*cap * w.UsedPercent) / 100.0
	}
	if totalCap == 0 {
		return 0
	}
	return (totalUsed / totalCap) * 100.0
}

// AccountQuota stores quota state for a single account.
type AccountQuota struct {
	AccountID        string       `json:"account_id"`
	Source           string       `json:"source"`
	PlanType         string       `json:"plan_type,omitempty"`
	PrimaryWindow    *QuotaWindow `json:"primary_window,omitempty"`
	SecondaryWindow  *QuotaWindow `json:"secondary_window,omitempty"`
	CreditsHas       *bool        `json:"credits_has,omitempty"`
	CreditsUnlimited *bool        `json:"credits_unlimited,omitempty"`
	CreditsBalance   *float64     `json:"credits_balance,omitempty"`
	FetchedAt        time.Time    `json:"fetched_at"`
	FetchError       string       `json:"fetch_error,omitempty"`
}
