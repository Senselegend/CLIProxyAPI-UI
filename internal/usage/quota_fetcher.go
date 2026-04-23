package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

const (
	whamUsageURL      = "/backend-api/wham/usage"
	quotaFetchTimeout = 10 * time.Second
)

// WhamUsagePayload represents the JSON response from /wham/usage.
type WhamUsagePayload struct {
	PlanType             string            `json:"plan_type,omitempty"`
	RateLimit            *RateLimitData    `json:"rate_limit,omitempty"`
	AdditionalRateLimits []json.RawMessage `json:"additional_rate_limits,omitempty"`
	Credits              *CreditsData      `json:"credits,omitempty"`
}

// RateLimitData contains primary/secondary window usage.
type RateLimitData struct {
	PrimaryWindow   *UsageWindow `json:"primary_window,omitempty"`
	SecondaryWindow *UsageWindow `json:"secondary_window,omitempty"`
}

// UsageWindow represents a single rate-limit window.
type UsageWindow struct {
	UsedPercent        *float64 `json:"used_percent,omitempty"`
	LimitWindowSeconds int      `json:"limit_window_seconds,omitempty"`
	ResetAt            int64    `json:"reset_at,omitempty"`
	ResetAfterSeconds  int      `json:"reset_after_seconds,omitempty"`
}

// CreditsData contains credit balance info.
type CreditsData struct {
	Has       bool `json:"has_credits,omitempty"`
	Unlimited bool `json:"unlimited,omitempty"`
	Balance   any  `json:"balance,omitempty"`
}

// FetchWhamUsage fetches quota from OpenAI internal API.
func FetchWhamUsage(ctx context.Context, accessToken, accountID string) (*WhamUsagePayload, error) {
	baseURL := "https://chatgpt.com"
	if url := os.Getenv("OPENAI_BASE_URL"); url != "" {
		baseURL = url
	}
	url := baseURL + whamUsageURL

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")

	if accountID != "" && !isEmailOrLocalAccount(accountID) {
		req.Header.Set("chatgpt-account-id", accountID)
	}

	client := &http.Client{Timeout: quotaFetchTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var payload WhamUsagePayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse JSON: %w", err)
	}
	return &payload, nil
}

func isEmailOrLocalAccount(accountID string) bool {
	return len(accountID) > 6 && (accountID[:6] == "email_" || accountID[:6] == "local_")
}
