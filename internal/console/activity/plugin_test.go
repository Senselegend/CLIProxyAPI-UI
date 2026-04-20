package activity

import (
	"context"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/logging"
	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestUsagePluginEnrichesByContextRequestID(t *testing.T) {
	store := NewStore(10)
	plugin := NewUsagePlugin(store)
	ctx := logging.WithRequestID(context.Background(), "req_ctx")

	store.Start(StartEvent{ID: "req_ctx", Method: "POST", Path: "/v1/chat/completions", StartedAt: time.Now()})
	plugin.HandleUsage(ctx, coreusage.Record{
		Provider: "codex",
		Model:    "gpt-5.4-mini",
		Source:   "user@example.com",
		Detail:   coreusage.Detail{TotalTokens: 42},
	})

	got := store.Snapshot(SnapshotOptions{Limit: 1})[0]
	if got.Model != "gpt-5.4-mini" || got.Tokens.TotalTokens != 42 {
		t.Fatalf("entry not enriched: %+v", got)
	}
}
