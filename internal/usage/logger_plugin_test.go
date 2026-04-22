package usage

import (
	"context"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestRequestStatisticsRecordIncludesLatency(t *testing.T) {
	stats := NewRequestStatistics()
	stats.Record(context.Background(), coreusage.Record{
		APIKey:      "test-key",
		Model:       "gpt-5.4",
		RequestedAt: time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC),
		Latency:     1500 * time.Millisecond,
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	snapshot := stats.Snapshot()
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].LatencyMs != 1500 {
		t.Fatalf("latency_ms = %d, want 1500", details[0].LatencyMs)
	}
}

func TestRequestStatisticsMergeSnapshotDedupIgnoresLatency(t *testing.T) {
	stats := NewRequestStatistics()
	timestamp := time.Date(2026, 3, 20, 12, 0, 0, 0, time.UTC)
	first := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 0,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}
	second := StatisticsSnapshot{
		APIs: map[string]APISnapshot{
			"test-key": {
				Models: map[string]ModelSnapshot{
					"gpt-5.4": {
						Details: []RequestDetail{{
							Timestamp: timestamp,
							LatencyMs: 2500,
							Source:    "user@example.com",
							AuthIndex: "0",
							Tokens: TokenStats{
								InputTokens:  10,
								OutputTokens: 20,
								TotalTokens:  30,
							},
						}},
					},
				},
			},
		},
	}

	result := stats.MergeSnapshot(first)
	if result.Added != 1 || result.Skipped != 0 {
		t.Fatalf("first merge = %+v, want added=1 skipped=0", result)
	}

	result = stats.MergeSnapshot(second)
	if result.Added != 0 || result.Skipped != 1 {
		t.Fatalf("second merge = %+v, want added=0 skipped=1", result)
	}

	snapshot := stats.Snapshot()
	details := snapshot.APIs["test-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
}

func TestRequestStatisticsPersistAndLoadRoundTrip(t *testing.T) {
	stats := NewRequestStatistics()
	storageDir := t.TempDir()
	now := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)

	stats.Record(context.Background(), coreusage.Record{
		Source:      "alpha@example.com",
		APIKey:      "alpha-key",
		Model:       "gpt-5.4",
		RequestedAt: now,
		Latency:     150 * time.Millisecond,
		Detail: coreusage.Detail{
			InputTokens:  10,
			OutputTokens: 20,
			TotalTokens:  30,
		},
	})

	if err := stats.Persist(storageDir); err != nil {
		t.Fatalf("persist request stats: %v", err)
	}

	reloaded := NewRequestStatistics()
	if err := reloaded.Load(storageDir); err != nil {
		t.Fatalf("load request stats: %v", err)
	}

	snapshot := reloaded.Snapshot()
	details := snapshot.APIs["alpha-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].Source != "alpha@example.com" {
		t.Fatalf("detail source = %q, want alpha@example.com", details[0].Source)
	}
	if details[0].Account != "alpha@example.com" {
		t.Fatalf("detail account = %q, want alpha@example.com", details[0].Account)
	}
	if details[0].Tokens.TotalTokens != 30 {
		t.Fatalf("detail total_tokens = %d, want 30", details[0].Tokens.TotalTokens)
	}
}

func TestRequestStatisticsPersistPrunesDetailsOlderThanSevenDays(t *testing.T) {
	stats := NewRequestStatistics()
	storageDir := t.TempDir()
	now := time.Now()

	stats.Record(context.Background(), coreusage.Record{
		Source:      "alpha@example.com",
		APIKey:      "alpha-key",
		Model:       "gpt-5.4",
		RequestedAt: now.Add(-8 * 24 * time.Hour),
		Detail:      coreusage.Detail{TotalTokens: 10},
	})
	stats.Record(context.Background(), coreusage.Record{
		Source:      "alpha@example.com",
		APIKey:      "alpha-key",
		Model:       "gpt-5.4",
		RequestedAt: now.Add(-2 * time.Hour),
		Detail:      coreusage.Detail{TotalTokens: 20},
	})

	if err := stats.Persist(storageDir); err != nil {
		t.Fatalf("persist request stats: %v", err)
	}

	reloaded := NewRequestStatistics()
	if err := reloaded.Load(storageDir); err != nil {
		t.Fatalf("load request stats: %v", err)
	}

	snapshot := reloaded.Snapshot()
	details := snapshot.APIs["alpha-key"].Models["gpt-5.4"].Details
	if len(details) != 1 {
		t.Fatalf("details len = %d, want 1", len(details))
	}
	if details[0].Tokens.TotalTokens != 20 {
		t.Fatalf("persisted detail total_tokens = %d, want 20", details[0].Tokens.TotalTokens)
	}
	if snapshot.TotalRequests != 2 {
		t.Fatalf("total requests = %d, want 2", snapshot.TotalRequests)
	}
	if snapshot.TotalTokens != 30 {
		t.Fatalf("total tokens = %d, want 30", snapshot.TotalTokens)
	}
}
