package api

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	sdkAuth "github.com/router-for-me/CLIProxyAPI/v6/sdk/auth"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

var (
	quotaRefresherStarted bool
	quotaRefresherMu      sync.Mutex
)

func init() {
	go tryStartQuotaRefresher()
}

func tryStartQuotaRefresher() {
	// Wait for token store to be registered
	for i := 0; i < 60; i++ {
		time.Sleep(500 * time.Millisecond)
		store := sdkAuth.GetTokenStore()
		if store != nil {
			startQuotaRefresher()
			return
		}
	}
	log.Printf("quota refresher: token store not available after 30s")
}

func startQuotaRefresher() {
	quotaRefresherMu.Lock()
	defer quotaRefresherMu.Unlock()
	if quotaRefresherStarted {
		return
	}
	quotaRefresherStarted = true

	ctx := context.Background()
	store := sdkAuth.GetTokenStore()
	if store == nil {
		return
	}

	auths, err := store.List(ctx)
	if err != nil || len(auths) == 0 {
		log.Printf("quota refresher: no accounts found")
		return
	}

	var accounts []usage.AuthProvider
	for _, a := range auths {
		if a.Provider == "codex" || a.Provider == "openai" {
			accounts = append(accounts, &authAdapter{auth: a})
		}
	}

	if len(accounts) == 0 {
		log.Printf("quota refresher: no codex/openai accounts")
		return
	}

	refresher := usage.NewQuotaRefresher(5 * time.Minute)
	refresher.Start(accounts)
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
