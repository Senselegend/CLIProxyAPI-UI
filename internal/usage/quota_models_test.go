package usage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"testing"
	"time"
)

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

func TestUsageWindowJSONDistinguishesMissingUsedPercentFromExplicitZero(t *testing.T) {
	var missing UsageWindow
	if err := json.Unmarshal([]byte(`{"reset_at":1776695731,"limit_window_seconds":18000}`), &missing); err != nil {
		t.Fatalf("unmarshal missing used_percent: %v", err)
	}

	var explicitZero UsageWindow
	if err := json.Unmarshal([]byte(`{"used_percent":0,"reset_at":1776695731,"limit_window_seconds":18000}`), &explicitZero); err != nil {
		t.Fatalf("unmarshal explicit zero used_percent: %v", err)
	}

	missingValue, missingPresent := usedPercentFieldValue(t, missing)
	explicitZeroValue, explicitZeroPresent := usedPercentFieldValue(t, explicitZero)

	if missingPresent {
		t.Fatalf("missing used_percent unexpectedly decoded as present with value %v", missingValue)
	}
	if !explicitZeroPresent {
		t.Fatalf("explicit zero used_percent decoded as missing")
	}
	if explicitZeroValue != 0 {
		t.Fatalf("explicitZero used_percent = %v, want 0", explicitZeroValue)
	}
}

func TestPayloadToAccountQuotaDistinguishesMissingPrimaryUsedPercentFromExplicitZero(t *testing.T) {
	payloadMissing := &WhamUsagePayload{
		PlanType: "plus",
		RateLimit: &RateLimitData{
			PrimaryWindow:   &UsageWindow{ResetAt: 1776695731, LimitWindowSeconds: 18000},
			SecondaryWindow: &UsageWindow{UsedPercent: float64Ptr(38), ResetAt: 1777210276, LimitWindowSeconds: 604800},
		},
	}
	payloadZero := &WhamUsagePayload{
		PlanType: "plus",
		RateLimit: &RateLimitData{
			PrimaryWindow:   &UsageWindow{UsedPercent: float64Ptr(0), ResetAt: 1776695731, LimitWindowSeconds: 18000},
			SecondaryWindow: &UsageWindow{UsedPercent: float64Ptr(38), ResetAt: 1777210276, LimitWindowSeconds: 604800},
		},
	}

	quotaMissing := PayloadToAccountQuota("auth-1", payloadMissing)
	quotaZero := PayloadToAccountQuota("auth-1", payloadZero)

	if quotaMissing.PrimaryWindow == nil {
		t.Fatalf("quotaMissing.PrimaryWindow is nil")
	}
	if quotaZero.PrimaryWindow == nil {
		t.Fatalf("quotaZero.PrimaryWindow is nil")
	}

	missingValue, missingPresent := usedPercentFieldValue(t, quotaMissing.PrimaryWindow)
	zeroValue, zeroPresent := usedPercentFieldValue(t, quotaZero.PrimaryWindow)
	if missingPresent {
		t.Fatalf("mapped missing primary used_percent unexpectedly present with value %v", missingValue)
	}
	if !zeroPresent {
		t.Fatalf("mapped explicit zero primary used_percent is missing")
	}
	if zeroValue != 0 {
		t.Fatalf("mapped explicit zero primary used_percent = %v, want 0", zeroValue)
	}
}

func TestRefreshAccountPreservesPriorGoodPrimaryWindowWhenPayloadDropsUsedPercent(t *testing.T) {
	store := swapQuotaStoreForTest(t)
	account := testAuthProvider{id: "auth-1", token: "token-1"}
	priorPrimary := &QuotaWindow{UsedPercent: float64Ptr(42), ResetAt: 1776695000, WindowMinutes: 300}
	store.Set(account.ID(), &AccountQuota{
		AccountID:       account.ID(),
		Source:          "chatgpt",
		PlanType:        "plus",
		PrimaryWindow:   priorPrimary,
		SecondaryWindow: &QuotaWindow{UsedPercent: float64Ptr(10), ResetAt: 1777210000, WindowMinutes: 10080},
		FetchedAt:       time.Unix(100, 0),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"reset_at":1776695731,"limit_window_seconds":18000},"secondary_window":{"used_percent":38,"reset_at":1777210276,"limit_window_seconds":604800}}}`))
	}))
	defer server.Close()
	os.Setenv("OPENAI_BASE_URL", server.URL)
	defer os.Unsetenv("OPENAI_BASE_URL")

	r := NewQuotaRefresher(time.Minute)
	r.refreshAccount(account)

	got := store.Get(account.ID())
	if got == nil {
		t.Fatalf("store.Get(%q) returned nil", account.ID())
	}
	if got.PrimaryWindow == nil {
		t.Fatalf("got.PrimaryWindow is nil")
	}
	gotPrimary, gotPrimaryPresent := usedPercentFieldValue(t, got.PrimaryWindow)
	if !gotPrimaryPresent {
		t.Fatalf("preserved primary used_percent is missing")
	}
	if gotPrimary != 42 {
		t.Fatalf("primary used_percent = %v, want preserved 42", gotPrimary)
	}
	if got.SecondaryWindow == nil {
		t.Fatalf("got.SecondaryWindow is nil")
	}
	gotSecondary, gotSecondaryPresent := usedPercentFieldValue(t, got.SecondaryWindow)
	if !gotSecondaryPresent || gotSecondary != 38 {
		t.Fatalf("secondary used_percent = (%v, present=%v), want (38, true)", gotSecondary, gotSecondaryPresent)
	}
}

func TestRefreshAccountPreservesPriorGoodWindowsOnTransientFetchFailure(t *testing.T) {
	store := swapQuotaStoreForTest(t)
	account := testAuthProvider{id: "auth-2", token: "token-2"}
	priorPrimary := &QuotaWindow{UsedPercent: float64Ptr(21), ResetAt: 1776695000, WindowMinutes: 300}
	priorSecondary := &QuotaWindow{UsedPercent: float64Ptr(54), ResetAt: 1777210000, WindowMinutes: 10080}
	priorHas := true
	priorUnlimited := false
	priorBalance := 17.5
	store.Set(account.ID(), &AccountQuota{
		AccountID:        account.ID(),
		Source:           "chatgpt",
		PlanType:         "plus",
		PrimaryWindow:    priorPrimary,
		SecondaryWindow:  priorSecondary,
		CreditsHas:       &priorHas,
		CreditsUnlimited: &priorUnlimited,
		CreditsBalance:   &priorBalance,
		FetchedAt:        time.Unix(100, 0),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}))
	defer server.Close()
	os.Setenv("OPENAI_BASE_URL", server.URL)
	defer os.Unsetenv("OPENAI_BASE_URL")

	r := NewQuotaRefresher(time.Minute)
	r.refreshAccount(account)

	got := store.Get(account.ID())
	if got == nil {
		t.Fatalf("store.Get(%q) returned nil", account.ID())
	}
	if got.FetchError == "" {
		t.Fatalf("FetchError is empty after transient failure")
	}
	if !reflect.DeepEqual(got.PrimaryWindow, priorPrimary) {
		t.Fatalf("primary window = %+v, want preserved %+v", got.PrimaryWindow, priorPrimary)
	}
	if !reflect.DeepEqual(got.SecondaryWindow, priorSecondary) {
		t.Fatalf("secondary window = %+v, want preserved %+v", got.SecondaryWindow, priorSecondary)
	}
	if got.CreditsHas == nil || *got.CreditsHas != priorHas {
		t.Fatalf("credits has = %v, want %v", got.CreditsHas, priorHas)
	}
	if got.CreditsUnlimited == nil || *got.CreditsUnlimited != priorUnlimited {
		t.Fatalf("credits unlimited = %v, want %v", got.CreditsUnlimited, priorUnlimited)
	}
	if got.CreditsBalance == nil || *got.CreditsBalance != priorBalance {
		t.Fatalf("credits balance = %v, want %v", got.CreditsBalance, priorBalance)
	}
}

func TestRefreshAccountTreatsExplicitZeroPrimaryUsageAsValidData(t *testing.T) {
	store := swapQuotaStoreForTest(t)
	account := testAuthProvider{id: "auth-3", token: "token-3"}
	store.Set(account.ID(), &AccountQuota{
		AccountID:       account.ID(),
		Source:          "chatgpt",
		PlanType:        "plus",
		PrimaryWindow:   &QuotaWindow{UsedPercent: float64Ptr(42), ResetAt: 1776695000, WindowMinutes: 300},
		SecondaryWindow: &QuotaWindow{UsedPercent: float64Ptr(10), ResetAt: 1777210000, WindowMinutes: 10080},
		FetchedAt:       time.Unix(100, 0),
	})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"plan_type":"plus","rate_limit":{"primary_window":{"used_percent":0,"reset_at":1776695731,"limit_window_seconds":18000},"secondary_window":{"used_percent":38,"reset_at":1777210276,"limit_window_seconds":604800}}}`))
	}))
	defer server.Close()
	os.Setenv("OPENAI_BASE_URL", server.URL)
	defer os.Unsetenv("OPENAI_BASE_URL")

	r := NewQuotaRefresher(time.Minute)
	r.refreshAccount(account)

	got := store.Get(account.ID())
	if got == nil || got.PrimaryWindow == nil {
		t.Fatalf("stored quota/primary window missing: %+v", got)
	}
	gotPrimary, gotPrimaryPresent := usedPercentFieldValue(t, got.PrimaryWindow)
	if !gotPrimaryPresent {
		t.Fatalf("explicit zero primary used_percent decoded as missing")
	}
	if gotPrimary != 0 {
		t.Fatalf("primary used_percent = %v, want explicit zero", gotPrimary)
	}
}

type testAuthProvider struct {
	id        string
	token     string
	accountID string
}

func (a testAuthProvider) GetAccessToken() string { return a.token }
func (a testAuthProvider) GetAccountID() string   { return a.accountID }
func (a testAuthProvider) ID() string             { return a.id }

func swapQuotaStoreForTest(t *testing.T) *QuotaStore {
	t.Helper()
	original := defaultQuotaStore
	store := NewQuotaStore()
	defaultQuotaStore = store
	t.Cleanup(func() {
		defaultQuotaStore = original
	})
	return store
}

func usedPercentFieldValue(t *testing.T, v any) (float64, bool) {
	t.Helper()
	field := reflect.ValueOf(v)
	if field.Kind() == reflect.Pointer {
		field = field.Elem()
	}
	usedPercent := field.FieldByName("UsedPercent")
	if !usedPercent.IsValid() {
		t.Fatalf("%T does not have UsedPercent field", v)
	}
	if usedPercent.Kind() == reflect.Pointer {
		if usedPercent.IsNil() {
			return 0, false
		}
		return usedPercent.Elem().Float(), true
	}
	return usedPercent.Float(), true
}

func quotaWithPrimaryUsage(planType string, usedPercent float64) AccountQuota {
	return AccountQuota{
		PlanType: planType,
		PrimaryWindow: &QuotaWindow{
			UsedPercent:   float64Ptr(usedPercent),
			ResetAt:       1776695731,
			WindowMinutes: 300,
		},
	}
}

func quotaWithSecondaryUsage(planType string, usedPercent float64) AccountQuota {
	return AccountQuota{
		PlanType: planType,
		SecondaryWindow: &QuotaWindow{
			UsedPercent:   float64Ptr(usedPercent),
			ResetAt:       1777210276,
			WindowMinutes: 10080,
		},
	}
}

func float64Ptr(v float64) *float64 {
	return &v
}
