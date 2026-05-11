package management

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteOAuthCallbackFileCreatesAuthDir(t *testing.T) {
	authDir := filepath.Join(t.TempDir(), "missing-auths")
	state := "test-state"

	path, err := WriteOAuthCallbackFile(authDir, "codex", state, "code-value", "")
	if err != nil {
		t.Fatalf("WriteOAuthCallbackFile: %v", err)
	}
	if _, err := os.Stat(authDir); err != nil {
		t.Fatalf("auth dir was not created: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("callback file was not created: %v", err)
	}
}
