package usage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

func TestAccountUsageStoreLoadExpandsTildeStorageDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	dir := filepath.Join(home, ".cli-proxy-api.usage")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir usage dir: %v", err)
	}
	data := `{"user@example.com":{"total_requests":3,"total_tokens":42,"failed_count":1,"models":{"gpt-5.4":3},"last_seen":"2026-04-20T13:07:19Z"}}`
	if err := os.WriteFile(filepath.Join(dir, "account_usage.json"), []byte(data), 0o644); err != nil {
		t.Fatalf("write usage file: %v", err)
	}

	store := &AccountUsageStore{accounts: make(map[string]*accountUsage)}
	store.SetStorageDir("~/.cli-proxy-api")

	if err := store.Load(); err != nil {
		t.Fatalf("load usage: %v", err)
	}

	got := store.Snapshot()["user@example.com"]
	if got.TotalRequests != 3 {
		t.Fatalf("total requests = %d, want 3", got.TotalRequests)
	}
}

func TestRemoveLegacyAccountUsageFilesArchivesFilesInsideAuthDir(t *testing.T) {
	authDir := filepath.Join(t.TempDir(), "auth")
	if err := os.MkdirAll(filepath.Join(authDir, ".usage"), 0o755); err != nil {
		t.Fatalf("mkdir legacy usage dir: %v", err)
	}
	targetDir := authDir + ".usage"
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatalf("mkdir target usage dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "account_usage.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatalf("write target usage file: %v", err)
	}

	legacyFiles := []string{
		filepath.Join(authDir, "account_usage.json"),
		filepath.Join(authDir, ".usage", "account_usage.json"),
	}
	for _, legacyFile := range legacyFiles {
		if err := os.WriteFile(legacyFile, []byte(`{"bad":true}`), 0o644); err != nil {
			t.Fatalf("write legacy usage file %s: %v", legacyFile, err)
		}
	}

	removeLegacyAccountUsageFiles(authDir)

	for _, legacyFile := range legacyFiles {
		if _, err := os.Stat(legacyFile); !os.IsNotExist(err) {
			t.Fatalf("legacy file %s still exists, stat err: %v", legacyFile, err)
		}
		if _, err := os.Stat(legacyFile + ".migrated"); err != nil {
			t.Fatalf("legacy archive %s missing: %v", legacyFile+".migrated", err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(targetDir, "account_usage.json")); err != nil {
		t.Fatalf("read target usage file: %v", err)
	} else if string(data) != `{}` {
		t.Fatalf("target usage file = %s, want {}", data)
	}
}

func TestGetRequestUsageStatsIncludesRollingTokenWindows(t *testing.T) {
	stats := NewRequestStatistics()
	now := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)

	stats.Record(context.Background(), coreusage.Record{
		Source:      "alpha@example.com",
		APIKey:      "alpha-key",
		Model:       "gpt-5.4",
		RequestedAt: now.Add(-2 * time.Hour),
		Detail: coreusage.Detail{TotalTokens: 100},
	})
	stats.Record(context.Background(), coreusage.Record{
		Source:      "alpha@example.com",
		APIKey:      "alpha-key",
		Model:       "gpt-5.4",
		RequestedAt: now.Add(-6 * time.Hour),
		Detail: coreusage.Detail{TotalTokens: 200},
	})
	stats.Record(context.Background(), coreusage.Record{
		Source:      "alpha@example.com",
		APIKey:      "alpha-key",
		Model:       "gpt-5.4",
		RequestedAt: now.Add(-8 * 24 * time.Hour),
		Detail: coreusage.Detail{TotalTokens: 300},
	})

	result := GetRequestUsageStatsAt(stats.Snapshot(), now)
	account := result.ByAccount["alpha@example.com"]

	if account.Last5Hours.TotalTokens != 100 {
		t.Fatalf("last_5_hours.total_tokens = %d, want 100", account.Last5Hours.TotalTokens)
	}
	if account.Last7Days.TotalTokens != 300 {
		t.Fatalf("last_7_days.total_tokens = %d, want 300", account.Last7Days.TotalTokens)
	}
}

func TestGetRequestUsageStatsSeparatesRollingTokenWindowsByAccount(t *testing.T) {
	stats := NewRequestStatistics()
	now := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)

	stats.Record(context.Background(), coreusage.Record{
		Source:      "alpha@example.com",
		APIKey:      "alpha-key",
		Model:       "gpt-5.4",
		RequestedAt: now.Add(-90 * time.Minute),
		Detail: coreusage.Detail{TotalTokens: 111},
	})
	stats.Record(context.Background(), coreusage.Record{
		Source:      "beta@example.com",
		APIKey:      "beta-key",
		Model:       "gpt-5.4",
		RequestedAt: now.Add(-3 * 24 * time.Hour),
		Detail: coreusage.Detail{TotalTokens: 222},
	})

	result := GetRequestUsageStatsAt(stats.Snapshot(), now)

	if result.ByAccount["alpha@example.com"].Last5Hours.TotalTokens != 111 {
		t.Fatalf("alpha 5h tokens = %d, want 111", result.ByAccount["alpha@example.com"].Last5Hours.TotalTokens)
	}
	if result.ByAccount["alpha@example.com"].Last7Days.TotalTokens != 111 {
		t.Fatalf("alpha 7d tokens = %d, want 111", result.ByAccount["alpha@example.com"].Last7Days.TotalTokens)
	}
	if result.ByAccount["beta@example.com"].Last5Hours.TotalTokens != 0 {
		t.Fatalf("beta 5h tokens = %d, want 0", result.ByAccount["beta@example.com"].Last5Hours.TotalTokens)
	}
	if result.ByAccount["beta@example.com"].Last7Days.TotalTokens != 222 {
		t.Fatalf("beta 7d tokens = %d, want 222", result.ByAccount["beta@example.com"].Last7Days.TotalTokens)
	}
}

func TestGetRequestUsageStatsUsesAPIKeyFallbackForRollingWindows(t *testing.T) {
	stats := NewRequestStatistics()
	now := time.Date(2026, 4, 22, 15, 0, 0, 0, time.UTC)

	stats.Record(context.Background(), coreusage.Record{
		Source:      "",
		APIKey:      "api-key-account",
		Model:       "gpt-5.4",
		RequestedAt: now.Add(-30 * time.Minute),
		Detail:      coreusage.Detail{TotalTokens: 77},
	})

	result := GetRequestUsageStatsAt(stats.Snapshot(), now)
	if result.ByAccount["api-key-account"].Last5Hours.TotalTokens != 77 {
		t.Fatalf("api-key 5h tokens = %d, want 77", result.ByAccount["api-key-account"].Last5Hours.TotalTokens)
	}
	if result.ByAccount["api-key-account"].Last7Days.TotalTokens != 77 {
		t.Fatalf("api-key 7d tokens = %d, want 77", result.ByAccount["api-key-account"].Last7Days.TotalTokens)
	}
}
