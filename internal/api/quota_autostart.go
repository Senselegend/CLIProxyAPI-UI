package api

import (
	"context"
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

var (
	quotaRefresherStarted bool
	quotaRefresherMu      sync.Mutex

	quotaStartupStateMu sync.RWMutex
	quotaStartupState   = startupStateNotStarted
	quotaStartupMessage string

	quotaAutostartMaxAttempts = 60
	quotaAutostartSleepFunc   = func() {
		time.Sleep(500 * time.Millisecond)
	}

	loadQuotaAccountsFunc   = loadQuotaAccounts
	runQuotaSyncFunc        = runQuotaSync
	startQuotaRefresherFunc = func(accounts []usage.AuthProvider) {
		usage.NewQuotaRefresher(5 * time.Minute).Start(accounts)
	}
)

func init() {
	managementHandlers.SetQuotaStartupGetters(GetQuotaStartupState, GetQuotaStartupMessage)
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

func RunStartupQuotaSync(ctx context.Context) error {
	return RunInitialQuotaSync(ctx)
}

func RunInitialQuotaSync(ctx context.Context) error {
	setQuotaStartupState(startupStateSyncing, "")

	accounts, err := loadQuotaAccountsFunc(ctx)
	if err != nil {
		msg := fmt.Sprintf("load quota accounts: %v", err)
		setQuotaStartupState(startupStateError, msg)
		return fmt.Errorf("load quota accounts: %w", err)
	}

	if err := runQuotaSyncFunc(ctx, accounts); err != nil {
		msg := fmt.Sprintf("run quota sync: %v", err)
		setQuotaStartupState(startupStateError, msg)
		return fmt.Errorf("run quota sync: %w", err)
	}

	startQuotaRefresherOnce(accounts)
	setQuotaStartupState(startupStateReady, "")
	return nil
}

func loadQuotaAccounts(ctx context.Context) ([]usage.AuthProvider, error) {
	store := sdkAuth.GetTokenStore()
	if store == nil {
		return nil, fmt.Errorf("token store unavailable")
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
	var firstErr error

	for _, account := range accounts {
		token := account.GetAccessToken()
		if token == "" {
			continue
		}

		payload, err := usage.FetchWhamUsage(ctx, token, account.GetAccountID())
		if err != nil {
			quota := &usage.AccountQuota{
				AccountID:  account.ID(),
				Source:     "chatgpt",
				FetchedAt:  time.Now(),
				FetchError: err.Error(),
			}
			usage.GetQuotaStore().Set(account.ID(), quota)
			if firstErr == nil {
				firstErr = fmt.Errorf("refresh %s: %w", account.ID(), err)
			}
			continue
		}

		usage.GetQuotaStore().Set(account.ID(), usage.PayloadToAccountQuota(account.ID(), payload))
	}
	return firstErr
}

func startQuotaRefresherOnce(accounts []usage.AuthProvider) {
	quotaRefresherMu.Lock()
	defer quotaRefresherMu.Unlock()
	if quotaRefresherStarted {
		return
	}
	quotaRefresherStarted = true
	startQuotaRefresherFunc(accounts)
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
	return a.auth.ID
}

func (a *authAdapter) ID() string {
	if a.auth == nil {
		return ""
	}
	return a.auth.ID
}
