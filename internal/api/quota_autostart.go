package api

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	managementHandlers "github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v6/sdk/auth"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

const (
	startupStateNotStarted = "not_started"
	startupStateSyncing    = "syncing"
	startupStateReady      = "ready"
	startupStateError      = "error"
)

var errQuotaTokenStoreUnavailable = errors.New("token store unavailable")

var (
	quotaRefresherStarted bool
	quotaRefresher        *usage.QuotaRefresher
	quotaRefresherMu      sync.Mutex

	quotaStartupStateMu sync.RWMutex
	quotaStartupState   = startupStateNotStarted
	quotaStartupMessage string

	quotaRecoveryMu      sync.Mutex
	quotaRecoveryRunning bool

	quotaAutostartMaxAttempts = 60
	quotaAutostartSleepFunc   = func() {
		time.Sleep(500 * time.Millisecond)
	}

	loadQuotaAccountsFunc   = loadQuotaAccounts
	runQuotaSyncFunc        = runQuotaSync
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
)

func init() {
	managementHandlers.SetQuotaStartupGetters(GetQuotaStartupState, GetQuotaStartupMessage)
	managementHandlers.SetQuotaRecoveryTrigger(func(ctx context.Context) managementHandlers.QuotaRecoveryTriggerResult {
		result := TriggerQuotaRecovery(ctx)
		return managementHandlers.QuotaRecoveryTriggerResult{
			Triggered:      result.Triggered,
			AlreadyRunning: result.AlreadyRunning,
			StartupState:   result.StartupState,
			MissingRuntime: result.MissingRuntime,
		}
	})
}

func GetQuotaStartupState() string {
	quotaStartupStateMu.RLock()
	defer quotaStartupStateMu.RUnlock()
	return quotaStartupState
}

func GetQuotaStartupMessage() string {
	quotaStartupStateMu.RLock()
	defer quotaStartupStateMu.RUnlock()
	return quotaStartupMessage
}

func setQuotaStartupState(state, message string) {
	quotaStartupStateMu.Lock()
	quotaStartupState = state
	quotaStartupMessage = message
	quotaStartupStateMu.Unlock()
}

func tryStartQuotaRefresher() {
	ctx := context.Background()
	for i := 0; i < quotaAutostartMaxAttempts; i++ {
		if _, err := loadQuotaAccountsFunc(ctx); err == nil {
			if err := RunInitialQuotaSync(ctx); err != nil {
				log.Printf("quota refresher: initial sync failed: %v", err)
			}
			return
		}
		quotaAutostartSleepFunc()
	}
	msg := "token store not available after 30s"
	setQuotaStartupState(startupStateError, msg)
	log.Printf("quota refresher: %s", msg)
}

type QuotaRecoveryTriggerResult struct {
	Triggered      bool   `json:"triggered"`
	AlreadyRunning bool   `json:"already_running"`
	StartupState   string `json:"startup_state"`
	MissingRuntime bool   `json:"missing_runtime"`
}

func RunStartupQuotaSync(ctx context.Context) error {
	_, err := runQuotaRecovery(ctx, false)
	return err
}

func RunInitialQuotaSync(ctx context.Context) error {
	_, err := runQuotaRecovery(ctx, false)
	return err
}

func TriggerQuotaRecovery(ctx context.Context) QuotaRecoveryTriggerResult {
	result, _ := runQuotaRecovery(ctx, true)
	return result
}

func runQuotaRecovery(ctx context.Context, forceRestartRefresher bool) (QuotaRecoveryTriggerResult, error) {
	quotaRecoveryMu.Lock()
	if quotaRecoveryRunning {
		quotaRecoveryMu.Unlock()
		return QuotaRecoveryTriggerResult{
			AlreadyRunning: true,
			StartupState:   GetQuotaStartupState(),
		}, nil
	}
	quotaRecoveryRunning = true
	quotaRecoveryMu.Unlock()

	defer func() {
		quotaRecoveryMu.Lock()
		quotaRecoveryRunning = false
		quotaRecoveryMu.Unlock()
	}()

	setQuotaStartupState(startupStateSyncing, "")

	accounts, err := loadQuotaAccountsFunc(ctx)
	if err != nil {
		msg := fmt.Sprintf("load quota accounts: %v", err)
		setQuotaStartupState(startupStateError, msg)
		return QuotaRecoveryTriggerResult{
			Triggered:      true,
			StartupState:   GetQuotaStartupState(),
			MissingRuntime: errors.Is(err, errQuotaTokenStoreUnavailable),
		}, fmt.Errorf("load quota accounts: %w", err)
	}

	if err := runQuotaSyncFunc(ctx, accounts); err != nil {
		msg := fmt.Sprintf("run quota sync: %v", err)
		setQuotaStartupState(startupStateError, msg)
		return QuotaRecoveryTriggerResult{
			Triggered:    true,
			StartupState: GetQuotaStartupState(),
		}, fmt.Errorf("run quota sync: %w", err)
	}

	startQuotaRefresher(accounts, forceRestartRefresher)
	setQuotaStartupState(startupStateReady, "")
	return QuotaRecoveryTriggerResult{
		Triggered:    true,
		StartupState: GetQuotaStartupState(),
	}, nil
}

func loadQuotaAccounts(ctx context.Context) ([]usage.AuthProvider, error) {
	store := sdkAuth.GetTokenStore()
	if store == nil {
		return nil, errQuotaTokenStoreUnavailable
	}

	auths, err := store.List(ctx)
	if err != nil {
		return nil, err
	}
	if len(auths) == 0 {
		return nil, fmt.Errorf("no accounts found")
	}

	accounts := make([]usage.AuthProvider, 0, len(auths))
	for _, a := range auths {
		if a.Provider == "codex" || a.Provider == "openai" {
			accounts = append(accounts, &authAdapter{auth: a})
		}
	}
	if len(accounts) == 0 {
		return nil, fmt.Errorf("no codex/openai accounts")
	}
	return accounts, nil
}

func runQuotaSync(ctx context.Context, accounts []usage.AuthProvider) error {
	errs := make([]error, len(accounts))
	accountIndexes := make(map[string]int, len(accounts))
	for index, account := range accounts {
		accountIndexes[account.ID()] = index
	}

	var errMu sync.Mutex
	_ = usage.RunQuotaSyncWork(ctx, accounts, func(ctx context.Context, account usage.AuthProvider) error {
		token := account.GetAccessToken()
		if token == "" {
			return nil
		}

		store := usage.GetQuotaStore()
		prior := store.Get(account.ID())

		payload, err := usage.FetchWhamUsage(ctx, token, account.GetAccountID())
		if err != nil {
			quota := usage.PreserveQuotaWindows(&usage.AccountQuota{
				AccountID:  account.ID(),
				Source:     "chatgpt",
				FetchedAt:  time.Now(),
				FetchError: err.Error(),
			}, prior)
			store.Set(account.ID(), quota)
			refreshErr := fmt.Errorf("refresh %s: %w", account.ID(), err)
			errMu.Lock()
			errs[accountIndexes[account.ID()]] = refreshErr
			errMu.Unlock()
			return refreshErr
		}

		store.Set(account.ID(), usage.PreserveQuotaWindows(usage.PayloadToAccountQuota(account.ID(), payload), prior))
		return nil
	})

	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

func startQuotaRefresher(accounts []usage.AuthProvider, forceRestart bool) {
	quotaRefresherMu.Lock()
	defer quotaRefresherMu.Unlock()
	if quotaRefresherStarted && !forceRestart {
		return
	}
	if forceRestart && quotaRefresher != nil {
		stopQuotaRefresherFunc(quotaRefresher)
		quotaRefresher = nil
		quotaRefresherStarted = false
	}
	quotaRefresher = startQuotaRefresherFunc(accounts)
	quotaRefresherStarted = quotaRefresher != nil
	log.Printf("Started quota refresher for %d accounts", len(accounts))
}

type authAdapter struct {
	auth *cliproxyauth.Auth
}

func (a *authAdapter) GetAccessToken() string {
	if a.auth == nil || a.auth.Metadata == nil {
		return ""
	}
	if at, ok := a.auth.Metadata["access_token"].(string); ok {
		return at
	}
	return ""
}

func (a *authAdapter) GetAccountID() string {
	if a.auth == nil {
		return ""
	}
	if a.auth.Metadata != nil {
		if accountID, ok := a.auth.Metadata["account_id"].(string); ok && accountID != "" {
			return accountID
		}
	}
	return a.auth.ID
}

func (a *authAdapter) ID() string {
	if a.auth == nil {
		return ""
	}
	return a.auth.ID
}
