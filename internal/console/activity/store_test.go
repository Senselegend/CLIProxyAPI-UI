package activity

import (
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestStoreEnrichesEntryByRequestID(t *testing.T) {
	store := NewStore(10)
	now := time.Date(2026, 4, 20, 14, 30, 0, 0, time.UTC)

	store.Start(StartEvent{
		ID:                  "req_123",
		Method:              "POST",
		Path:                "/v1/responses",
		DownstreamTransport: "http",
		StartedAt:           now,
	})
	store.Finish(FinishEvent{
		ID:         "req_123",
		HTTPStatus: 200,
		Latency:    250 * time.Millisecond,
		FinishedAt: now.Add(250 * time.Millisecond),
	})
	store.EnrichUsage("req_123", coreusage.Record{
		Provider: "codex",
		Model:    "gpt-5.4",
		Source:   "user@example.com",
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	entries := store.Snapshot(SnapshotOptions{Limit: 10})
	if len(entries) != 1 {
		t.Fatalf("entries len = %d, want 1", len(entries))
	}
	got := entries[0]
	if got.Account != "user@example.com" || got.Model != "gpt-5.4" || got.Provider != "codex" {
		t.Fatalf("unexpected enrichment: %+v", got)
	}
	if got.Transport != "http" || got.Status != "success" || got.LatencyMs != 250 {
		t.Fatalf("unexpected request fields: %+v", got)
	}
	if got.Tokens.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want 30", got.Tokens.TotalTokens)
	}
}
