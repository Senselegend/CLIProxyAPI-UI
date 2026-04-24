package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func TestRunInitialQuotaSyncCanRecoverAfterPriorError(t *testing.T) {
	resetQuotaAutostartTestState(t)
	setQuotaStartupState(startupStateError, "old failure")

	accounts := []usage.AuthProvider{stubQuotaAuthProvider{id: "auth-1", token: "token-1", accountID: "acct-1"}}
	loadCalls := 0
	loadQuotaAccountsFunc = func(context.Context) ([]usage.AuthProvider, error) {
		loadCalls++
		return accounts, nil
	}

	syncCalls := 0
	runQuotaSyncFunc = func(context.Context, []usage.AuthProvider) error {
		syncCalls++
		return nil
	}

	started := 0
	startQuotaRefresherFunc = func(got []usage.AuthProvider) *usage.QuotaRefresher {
		started++
		if len(got) != 1 || got[0].ID() != "auth-1" {
			t.Fatalf("startQuotaRefresherFunc accounts = %#v", got)
		}
		return nil
	}

	if err := RunInitialQuotaSync(context.Background()); err != nil {
		t.Fatalf("RunInitialQuotaSync() error = %v", err)
	}

	if state := GetQuotaStartupState(); state != startupStateReady {
		t.Fatalf("GetQuotaStartupState() = %q, want %q", state, startupStateReady)
	}
	if msg := GetQuotaStartupMessage(); msg != "" {
		t.Fatalf("GetQuotaStartupMessage() = %q, want empty", msg)
	}
	if loadCalls != 1 {
		t.Fatalf("loadQuotaAccountsFunc called %d times, want 1", loadCalls)
	}
	if syncCalls != 1 {
		t.Fatalf("runQuotaSyncFunc called %d times, want 1", syncCalls)
	}
	if started != 1 {
		t.Fatalf("startQuotaRefresherFunc called %d times, want 1", started)
	}
}

func TestTriggerQuotaRecoveryRearmsQuotaRefresherAfterPriorFailure(t *testing.T) {
	resetQuotaAutostartTestState(t)
	setQuotaStartupState(startupStateError, "stale failure")
	quotaRefresherStarted = true
	quotaRefresher = usage.NewQuotaRefresher(time.Minute)

	accounts := []usage.AuthProvider{stubQuotaAuthProvider{id: "auth-1", token: "token-1", accountID: "acct-1"}}
	loadQuotaAccountsFunc = func(context.Context) ([]usage.AuthProvider, error) {
		return accounts, nil
	}
	var synced []usage.AuthProvider
	runQuotaSyncFunc = func(context.Context, []usage.AuthProvider) error {
		synced = accounts
		return nil
	}

	stopped := 0
	stopQuotaRefresherFunc = func(refresher *usage.QuotaRefresher) {
		stopped++
		if refresher == nil {
			t.Fatalf("stopQuotaRefresherFunc refresher = nil")
		}
	}

	started := 0
	startQuotaRefresherFunc = func(got []usage.AuthProvider) *usage.QuotaRefresher {
		started++
		if len(got) != 1 || got[0].ID() != "auth-1" {
			t.Fatalf("startQuotaRefresherFunc accounts = %#v", got)
		}
		return usage.NewQuotaRefresher(time.Minute)
	}

	result := TriggerQuotaRecovery(context.Background())

	if !result.Triggered {
		t.Fatalf("TriggerQuotaRecovery().Triggered = false, want true")
	}
	if result.AlreadyRunning {
		t.Fatalf("TriggerQuotaRecovery().AlreadyRunning = true, want false")
	}
	if result.StartupState != startupStateReady {
		t.Fatalf("TriggerQuotaRecovery().StartupState = %q, want %q", result.StartupState, startupStateReady)
	}
	if result.MissingRuntime {
		t.Fatalf("TriggerQuotaRecovery().MissingRuntime = true, want false")
	}
	if state := GetQuotaStartupState(); state != startupStateReady {
		t.Fatalf("GetQuotaStartupState() = %q, want %q", state, startupStateReady)
	}
	if len(synced) != 1 || synced[0].ID() != "auth-1" {
		t.Fatalf("runQuotaSyncFunc accounts = %#v", synced)
	}
	if stopped != 1 {
		t.Fatalf("stopQuotaRefresherFunc called %d times, want 1", stopped)
	}
	if started != 1 {
		t.Fatalf("startQuotaRefresherFunc called %d times, want 1", started)
	}
}

func TestTriggerQuotaRecoveryMarksMissingRuntimeOnlyWhenTokenStoreUnavailable(t *testing.T) {
	resetQuotaAutostartTestState(t)
	loadQuotaAccountsFunc = func(context.Context) ([]usage.AuthProvider, error) {
		return nil, context.DeadlineExceeded
	}

	result := TriggerQuotaRecovery(context.Background())
	if result.MissingRuntime {
		t.Fatalf("TriggerQuotaRecovery().MissingRuntime = true, want false for generic load failure")
	}

	loadQuotaAccountsFunc = func(context.Context) ([]usage.AuthProvider, error) {
		return nil, errQuotaTokenStoreUnavailable
	}

	result = TriggerQuotaRecovery(context.Background())
	if !result.MissingRuntime {
		t.Fatalf("TriggerQuotaRecovery().MissingRuntime = false, want true for token store unavailable")
	}
}

func TestRunQuotaSyncProcessesAllAccountsAndReturnsFirstError(t *testing.T) {
	resetQuotaAutostartTestState(t)
	withIsolatedQuotaStore(t)

	primaryUsed := 42.5
	secondaryUsed := 12.5
	usage.GetQuotaStore().Set("auth-error", &usage.AccountQuota{
		AccountID: "auth-error",
		Source:    "chatgpt",
		PlanType:  "plus",
		PrimaryWindow: &usage.QuotaWindow{
			UsedPercent:   &primaryUsed,
			WindowMinutes: 180,
			ResetAt:       1710000000,
		},
		SecondaryWindow: &usage.QuotaWindow{
			UsedPercent:   &secondaryUsed,
			WindowMinutes: 60,
			ResetAt:       1710000100,
		},
	})

	accounts := []usage.AuthProvider{
		stubQuotaAuthProvider{id: "auth-first-error", token: "token-first-error", accountID: "acct-first-error"},
		stubQuotaAuthProvider{id: "auth-success", token: "token-success", accountID: "acct-success"},
		stubQuotaAuthProvider{id: "auth-error", token: "token-error", accountID: "acct-error"},
	}
	requests := make(chan string, len(accounts))
	releaseFirstError := make(chan struct{})
	var releaseFirstErrorOnce sync.Once
	defer releaseFirstErrorOnce.Do(func() { close(releaseFirstError) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		requests <- token
		switch token {
		case "token-first-error":
			<-releaseFirstError
			http.Error(w, "first boom", http.StatusBadGateway)
		case "token-success":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"plan_type":"team","rate_limit":{"primary_window":{"used_percent":7.5,"limit_window_seconds":10800,"reset_at":1720000000}}}`))
		case "token-error":
			http.Error(w, "second boom", http.StatusTooManyRequests)
		default:
			http.Error(w, fmt.Sprintf("unexpected token %q", token), http.StatusUnauthorized)
		}
	}))
	defer server.Close()
	setOpenAIBaseURLForQuotaTest(t, server.URL)

	done := make(chan error, 1)
	go func() {
		done <- runQuotaSync(context.Background(), accounts)
	}()

	gotRequests, gotAllRequests := waitForQuotaSyncRequests(t, requests, len(accounts))
	if !gotAllRequests {
		releaseFirstErrorOnce.Do(func() { close(releaseFirstError) })
		_ = waitForRunQuotaSyncDone(t, done)
		t.Fatalf("runQuotaSync did not request all eligible accounts concurrently; got requests for %v", gotRequests)
	}
	select {
	case err := <-done:
		t.Fatalf("runQuotaSync returned before all eligible accounts were requested: %v", err)
	default:
	}
	releaseFirstErrorOnce.Do(func() { close(releaseFirstError) })

	err := waitForRunQuotaSyncDone(t, done)
	if err == nil {
		t.Fatalf("runQuotaSync() error = nil, want first account refresh error")
	}
	if !strings.Contains(err.Error(), "refresh auth-first-error") || !strings.Contains(err.Error(), "first boom") {
		t.Fatalf("runQuotaSync() error = %v, want first account refresh error", err)
	}

	successQuota := usage.GetQuotaStore().Get("auth-success")
	if successQuota == nil {
		t.Fatalf("successful account quota was not stored")
	}
	if successQuota.FetchError != "" {
		t.Fatalf("successful account FetchError = %q, want empty", successQuota.FetchError)
	}
	if successQuota.PlanType != "team" {
		t.Fatalf("successful account PlanType = %q, want team", successQuota.PlanType)
	}
	if successQuota.PrimaryWindow == nil || successQuota.PrimaryWindow.UsedPercent == nil || *successQuota.PrimaryWindow.UsedPercent != 7.5 {
		t.Fatalf("successful account primary window = %#v, want used_percent 7.5", successQuota.PrimaryWindow)
	}

	errorQuota := usage.GetQuotaStore().Get("auth-error")
	if errorQuota == nil {
		t.Fatalf("error account quota was not stored")
	}
	if !strings.Contains(errorQuota.FetchError, "second boom") {
		t.Fatalf("error account FetchError = %q, want second boom", errorQuota.FetchError)
	}
	if errorQuota.PlanType != "plus" {
		t.Fatalf("error account PlanType = %q, want prior plus", errorQuota.PlanType)
	}
	if errorQuota.PrimaryWindow == nil || errorQuota.PrimaryWindow.UsedPercent == nil || *errorQuota.PrimaryWindow.UsedPercent != primaryUsed {
		t.Fatalf("error account primary window = %#v, want preserved prior", errorQuota.PrimaryWindow)
	}
	if errorQuota.SecondaryWindow == nil || errorQuota.SecondaryWindow.UsedPercent == nil || *errorQuota.SecondaryWindow.UsedPercent != secondaryUsed {
		t.Fatalf("error account secondary window = %#v, want preserved prior", errorQuota.SecondaryWindow)
	}
}

type stubQuotaAuthProvider struct {
	id        string
	token     string
	accountID string
}

func (s stubQuotaAuthProvider) GetAccessToken() string { return s.token }
func (s stubQuotaAuthProvider) GetAccountID() string   { return s.accountID }
func (s stubQuotaAuthProvider) ID() string             { return s.id }

func withIsolatedQuotaStore(t *testing.T) {
	t.Helper()
	previousStore := usage.GetQuotaStore()
	usage.SetQuotaStoreForTest(usage.NewQuotaStore())
	t.Cleanup(func() {
		usage.SetQuotaStoreForTest(previousStore)
	})
}

func setOpenAIBaseURLForQuotaTest(t *testing.T, baseURL string) {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", baseURL)
}

func waitForQuotaSyncRequests(t *testing.T, requests <-chan string, count int) ([]string, bool) {
	t.Helper()
	got := make([]string, 0, count)
	timer := time.NewTimer(250 * time.Millisecond)
	defer timer.Stop()
	for len(got) < count {
		select {
		case token := <-requests:
			got = append(got, token)
		case <-timer.C:
			return got, false
		}
	}
	return got, true
}

func waitForRunQuotaSyncDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for runQuotaSync to complete")
		return nil
	}
}

func resetQuotaAutostartTestState(t *testing.T) {
	t.Helper()

	previousLoad := loadQuotaAccountsFunc
	previousSync := runQuotaSyncFunc
	previousStart := startQuotaRefresherFunc
	previousStop := stopQuotaRefresherFunc
	previousState := GetQuotaStartupState()
	previousMessage := GetQuotaStartupMessage()
	previousRefresherStarted := quotaRefresherStarted
	previousRefresher := quotaRefresher
	previousRecoveryRunning := quotaRecoveryRunning

	loadQuotaAccountsFunc = loadQuotaAccounts
	runQuotaSyncFunc = runQuotaSync
	startQuotaRefresherFunc = func(accounts []usage.AuthProvider) *usage.QuotaRefresher {
		refresher := usage.NewQuotaRefresher(5 * time.Minute)
		refresher.Start(accounts)
		return refresher
	}
	stopQuotaRefresherFunc = func(refresher *usage.QuotaRefresher) {
		if refresher != nil {
			refresher.Stop()
		}
	}
	quotaRefresherStarted = false
	quotaRefresher = nil
	quotaRecoveryRunning = false
	setQuotaStartupState(startupStateNotStarted, "")

	t.Cleanup(func() {
		loadQuotaAccountsFunc = previousLoad
		runQuotaSyncFunc = previousSync
		startQuotaRefresherFunc = previousStart
		stopQuotaRefresherFunc = previousStop
		quotaRefresherStarted = previousRefresherStarted
		quotaRefresher = previousRefresher
		quotaRecoveryRunning = previousRecoveryRunning
		setQuotaStartupState(previousState, previousMessage)
	})
}
