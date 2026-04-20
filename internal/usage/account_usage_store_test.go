package usage

import (
	"os"
	"path/filepath"
	"testing"
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
