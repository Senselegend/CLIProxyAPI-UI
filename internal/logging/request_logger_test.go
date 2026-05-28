package logging

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func assertFileBodySourceCleaned(t *testing.T, partPaths []string) {
	t.Helper()

	dirs := make(map[string]struct{}, len(partPaths))
	for _, path := range partPaths {
		if _, errStat := os.Stat(path); !os.IsNotExist(errStat) {
			t.Fatalf("expected part %s to be removed, stat err=%v", path, errStat)
		}
		dirs[filepath.Dir(path)] = struct{}{}
	}
	for dir := range dirs {
		if _, errStat := os.Stat(dir); !os.IsNotExist(errStat) {
			t.Fatalf("expected part dir %s to be removed, stat err=%v", dir, errStat)
		}
	}
}

func TestFileBodySource_RecreatesPartDirAfterManualCleanup(t *testing.T) {
	logsDir := t.TempDir()
	source, errSource := NewFileBodySourceInDir(logsDir, "websocket-timeline-test")
	if errSource != nil {
		t.Fatalf("NewFileBodySourceInDir: %v", errSource)
	}
	if errAppend := source.AppendPart([]byte("before manual cleanup")); errAppend != nil {
		t.Fatalf("AppendPart before cleanup: %v", errAppend)
	}
	if errRemove := os.RemoveAll(logsDir); errRemove != nil {
		t.Fatalf("RemoveAll logs dir: %v", errRemove)
	}
	if errAppend := source.AppendPart([]byte("after manual cleanup")); errAppend != nil {
		t.Fatalf("AppendPart after cleanup: %v", errAppend)
	}

	raw, errBytes := source.Bytes()
	if errBytes != nil {
		t.Fatalf("Bytes after cleanup: %v", errBytes)
	}
	if bytes.Contains(raw, []byte("before manual cleanup")) {
		t.Fatalf("expected manually removed part to be skipped, got %q", string(raw))
	}
	if !bytes.Contains(raw, []byte("after manual cleanup")) {
		t.Fatalf("expected recreated part content, got %q", string(raw))
	}

	partPaths := source.Paths()
	if errCleanup := source.Cleanup(); errCleanup != nil {
		t.Fatalf("Cleanup: %v", errCleanup)
	}
	assertFileBodySourceCleaned(t, partPaths)
}
