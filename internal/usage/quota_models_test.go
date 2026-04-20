package usage

import "testing"

func TestAggregateQuotasIncludesValidZeroUsageWindows(t *testing.T) {
	quotas := []AccountQuota{
		quotaWithPrimaryUsage("plus", 0),
		quotaWithPrimaryUsage("plus", 0),
		quotaWithPrimaryUsage("plus", 0),
		quotaWithPrimaryUsage("plus", 80),
		quotaWithPrimaryUsage("plus", 0),
	}

	got := AggregateQuotas(quotas, "primary")
	if got != 16 {
		t.Fatalf("AggregateQuotas() = %v, want 16", got)
	}
}

func TestAggregateQuotasIncludesValidZeroUsageSecondaryWindows(t *testing.T) {
	quotas := []AccountQuota{
		quotaWithSecondaryUsage("plus", 0),
		quotaWithSecondaryUsage("plus", 0),
		quotaWithSecondaryUsage("plus", 75),
		quotaWithSecondaryUsage("plus", 0),
	}

	got := AggregateQuotas(quotas, "secondary")
	if got != 18.75 {
		t.Fatalf("AggregateQuotas() = %v, want 18.75", got)
	}
}

func quotaWithPrimaryUsage(planType string, usedPercent float64) AccountQuota {
	return AccountQuota{
		PlanType: planType,
		PrimaryWindow: &QuotaWindow{
			UsedPercent:   usedPercent,
			ResetAt:       1776695731,
			WindowMinutes: 300,
		},
	}
}

func quotaWithSecondaryUsage(planType string, usedPercent float64) AccountQuota {
	return AccountQuota{
		PlanType: planType,
		SecondaryWindow: &QuotaWindow{
			UsedPercent:   usedPercent,
			ResetAt:       1777210276,
			WindowMinutes: 10080,
		},
	}
}
