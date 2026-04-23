package usage

import (
	"context"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"
)

// AuthProvider defines interface for fetching access tokens.
type AuthProvider interface {
	GetAccessToken() string
	GetAccountID() string
	ID() string
}

// QuotaRefresher handles periodic quota refresh.
type QuotaRefresher struct {
	mu       sync.RWMutex
	running  bool
	stopCh   chan struct{}
	interval time.Duration
}

func NewQuotaRefresher(interval time.Duration) *QuotaRefresher {
	if interval == 0 {
		interval = 5 * time.Minute
	}
	return &QuotaRefresher{interval: interval}
}

func (r *QuotaRefresher) Start(accounts []AuthProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running {
		return
	}
	r.running = true
	r.stopCh = make(chan struct{})
	go r.runLoop(accounts)
}

func (r *QuotaRefresher) runLoop(accounts []AuthProvider) {
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	r.refreshAll(accounts)
	for {
		select {
		case <-ticker.C:
			r.refreshAll(accounts)
		case <-r.stopCh:
			return
		}
	}
}

func (r *QuotaRefresher) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running && r.stopCh != nil {
		close(r.stopCh)
		r.running = false
	}
}

func (r *QuotaRefresher) refreshAll(accounts []AuthProvider) {
	for _, account := range accounts {
		go r.refreshAccount(account)
	}
}

func (r *QuotaRefresher) refreshAccount(account AuthProvider) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	token := account.GetAccessToken()
	if token == "" {
		return
	}

	store := GetQuotaStore()
	prior := store.Get(account.ID())

	payload, err := FetchWhamUsage(ctx, token, account.GetAccountID())
	if err != nil {
		quota := PreserveQuotaWindows(&AccountQuota{
			AccountID:  account.ID(),
			Source:     "chatgpt",
			FetchedAt:  time.Now(),
			FetchError: err.Error(),
		}, prior)
		store.Set(account.ID(), quota)
		log.Printf("quota refresh failed for %s: %v", account.ID(), err)
		return
	}

	quota := PreserveQuotaWindows(PayloadToAccountQuota(account.ID(), payload), prior)
	store.Set(account.ID(), quota)
}

// PayloadToAccountQuota converts WhamUsagePayload to AccountQuota.
func PayloadToAccountQuota(accountID string, payload *WhamUsagePayload) *AccountQuota {
	quota := &AccountQuota{
		AccountID: accountID,
		Source:    "chatgpt",
		PlanType:  payload.PlanType,
		FetchedAt: time.Now(),
	}

	if payload.RateLimit != nil {
		if pw := payload.RateLimit.PrimaryWindow; pw != nil {
			quota.PrimaryWindow = &QuotaWindow{
				UsedPercent:   pw.UsedPercent,
				ResetAt:       pw.ResetAt,
				WindowMinutes: int(pw.LimitWindowSeconds / 60),
			}
		}
		if sw := payload.RateLimit.SecondaryWindow; sw != nil {
			quota.SecondaryWindow = &QuotaWindow{
				UsedPercent:   sw.UsedPercent,
				ResetAt:       sw.ResetAt,
				WindowMinutes: int(sw.LimitWindowSeconds / 60),
			}
		}
	}

	if payload.Credits != nil {
		has := payload.Credits.Has
		quota.CreditsHas = &has
		unlimited := payload.Credits.Unlimited
		quota.CreditsUnlimited = &unlimited
		if balance := parseFloat(payload.Credits.Balance); balance != nil {
			quota.CreditsBalance = balance
		}
	}

	return quota
}

func PreserveQuotaWindows(next *AccountQuota, prior *AccountQuota) *AccountQuota {
	if next == nil || prior == nil {
		return next
	}
	if next.PlanType == "" {
		next.PlanType = prior.PlanType
	}
	if next.PrimaryWindow != nil && next.PrimaryWindow.UsedPercent == nil && prior.PrimaryWindow != nil && prior.PrimaryWindow.UsedPercent != nil {
		next.PrimaryWindow = cloneQuotaWindow(prior.PrimaryWindow)
	}
	if next.SecondaryWindow != nil && next.SecondaryWindow.UsedPercent == nil && prior.SecondaryWindow != nil && prior.SecondaryWindow.UsedPercent != nil {
		next.SecondaryWindow = cloneQuotaWindow(prior.SecondaryWindow)
	}
	if next.PrimaryWindow == nil && prior.PrimaryWindow != nil && prior.PrimaryWindow.UsedPercent != nil {
		next.PrimaryWindow = cloneQuotaWindow(prior.PrimaryWindow)
	}
	if next.SecondaryWindow == nil && prior.SecondaryWindow != nil && prior.SecondaryWindow.UsedPercent != nil {
		next.SecondaryWindow = cloneQuotaWindow(prior.SecondaryWindow)
	}
	if next.CreditsHas == nil && prior.CreditsHas != nil {
		next.CreditsHas = cloneBool(prior.CreditsHas)
	}
	if next.CreditsUnlimited == nil && prior.CreditsUnlimited != nil {
		next.CreditsUnlimited = cloneBool(prior.CreditsUnlimited)
	}
	if next.CreditsBalance == nil && prior.CreditsBalance != nil {
		next.CreditsBalance = cloneFloat64(prior.CreditsBalance)
	}
	return next
}

func cloneQuotaWindow(window *QuotaWindow) *QuotaWindow {
	if window == nil {
		return nil
	}
	cloned := *window
	if window.UsedPercent != nil {
		used := *window.UsedPercent
		cloned.UsedPercent = &used
	}
	return &cloned
}

func cloneBool(v *bool) *bool {
	if v == nil {
		return nil
	}
	cloned := *v
	return &cloned
}

func cloneFloat64(v *float64) *float64 {
	if v == nil {
		return nil
	}
	cloned := *v
	return &cloned
}

func parseFloat(v any) *float64 {
	switch val := v.(type) {
	case float64:
		return &val
	case int:
		f := float64(val)
		return &f
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
			return &f
		}
	}
	return nil
}
