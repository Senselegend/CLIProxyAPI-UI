package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
	log "github.com/sirupsen/logrus"
)

// AccountUsageStore tracks usage statistics per account (by email/account ID).
// It complements RequestStatistics which tracks per API key.
type AccountUsageStore struct {
	mu         sync.RWMutex
	accounts   map[string]*accountUsage
	storageDir string
}

type accountUsage struct {
	TotalRequests int64            `json:"total_requests"`
	TotalTokens   int64            `json:"total_tokens"`
	FailedCount   int64            `json:"failed_count"`
	Models        map[string]int64 `json:"models"`
	LastSeen      time.Time        `json:"last_seen"`
}

var defaultAccountUsageStore = &AccountUsageStore{accounts: make(map[string]*accountUsage)}

const accountUsageStorageDirSuffix = ".usage"

func GetAccountUsageStore() *AccountUsageStore { return defaultAccountUsageStore }

// SetStorageDir configures the directory for persisting usage stats.
func (s *AccountUsageStore) SetStorageDir(dir string) { s.storageDir = dir }

func resolveUsageBaseDir(dir string) (string, error) {
	if dir == "" {
		return "", nil
	}
	if strings.HasPrefix(dir, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve usage storage dir: %w", err)
		}
		remainder := strings.TrimPrefix(dir, "~")
		remainder = strings.TrimLeft(remainder, "/\\")
		if remainder == "" {
			return filepath.Clean(home), nil
		}
		normalized := strings.ReplaceAll(remainder, "\\", "/")
		return filepath.Clean(filepath.Join(home, filepath.FromSlash(normalized))), nil
	}
	return filepath.Clean(os.ExpandEnv(dir)), nil
}

func resolveUsageStorageDir(dir string) (string, error) {
	resolved, err := resolveUsageBaseDir(dir)
	if err != nil || resolved == "" {
		return resolved, err
	}
	if strings.HasSuffix(filepath.Base(resolved), accountUsageStorageDirSuffix) {
		return resolved, nil
	}
	return resolved + accountUsageStorageDirSuffix, nil
}

func removeLegacyAccountUsageFiles(authDir string) {
	baseDir, err := resolveUsageBaseDir(authDir)
	if err != nil || baseDir == "" {
		return
	}
	targetDir, err := resolveUsageStorageDir(authDir)
	if err != nil || targetDir == "" {
		return
	}
	targetPath := filepath.Join(targetDir, "account_usage.json")
	targetHasData := fileHasUsageData(targetPath)
	legacyPaths := []string{
		filepath.Join(baseDir, "account_usage.json"),
		filepath.Join(baseDir, ".usage", "account_usage.json"),
	}
	legacyPaths = append(legacyPaths, globUsageFiles(filepath.Join(baseDir, "account_usage.json*"))...)
	legacyPaths = append(legacyPaths, globUsageFiles(filepath.Join(baseDir, ".usage", "account_usage.json*"))...)
	for _, legacyPath := range legacyPaths {
		if filepath.Clean(legacyPath) == filepath.Clean(targetPath) {
			continue
		}
		if _, errStat := os.Stat(legacyPath); errStat != nil {
			continue
		}
		if !targetHasData && fileHasUsageData(legacyPath) {
			if errMkdir := os.MkdirAll(targetDir, 0o755); errMkdir == nil {
				data, errRead := os.ReadFile(legacyPath)
				if errRead == nil {
					if errWrite := os.WriteFile(targetPath, data, 0o644); errWrite == nil {
						targetHasData = true
					}
				}
			}
		}
		archivePath := legacyPath + ".migrated"
		if errRename := os.Rename(legacyPath, archivePath); errRename != nil {
			log.Debugf("failed to archive legacy account usage file %s: %v", legacyPath, errRename)
		}
	}
}

func globUsageFiles(pattern string) []string {
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil
	}
	return matches
}

func fileHasUsageData(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var loaded map[string]*accountUsage
	if err := json.Unmarshal(data, &loaded); err != nil {
		return false
	}
	return len(loaded) > 0
}

// Load restores persisted usage stats from disk.
func (s *AccountUsageStore) Load() error {
	if s.storageDir == "" {
		return nil
	}
	dir, err := resolveUsageStorageDir(s.storageDir)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "account_usage.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var loaded map[string]*accountUsage
	if err := json.Unmarshal(data, &loaded); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.accounts = loaded
	return nil
}

// Persist saves usage stats to disk.
func (s *AccountUsageStore) Persist() error {
	if s.storageDir == "" {
		return nil
	}
	dir, err := resolveUsageStorageDir(s.storageDir)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "account_usage.json")
	s.mu.RLock()
	data, err := json.Marshal(s.accounts)
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// AccountUsagePlugin implements coreusage.Plugin to receive usage records.
type AccountUsagePlugin struct{}

func (p *AccountUsagePlugin) HandleUsage(ctx context.Context, record coreusage.Record) {
	store := GetAccountUsageStore()
	store.Record(record)
}

func (s *AccountUsageStore) Record(record coreusage.Record) {
	// Use Source (email for OAuth) as primary key, fallback to APIKey
	key := record.Source
	if key == "" {
		key = record.APIKey
	}
	if key == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	acc, ok := s.accounts[key]
	if !ok {
		acc = &accountUsage{Models: make(map[string]int64)}
		s.accounts[key] = acc
	}

	acc.TotalRequests++
	if record.Failed {
		acc.FailedCount++
	} else {
		acc.TotalTokens += record.Detail.TotalTokens
	}
	acc.Models[record.Model]++
	acc.LastSeen = time.Now()
}

// Snapshot returns a copy of the usage data.
func (s *AccountUsageStore) Snapshot() map[string]accountUsage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]accountUsage, len(s.accounts))
	for k, v := range s.accounts {
		copied := *v
		copied.Models = make(map[string]int64, len(v.Models))
		for model, tokens := range v.Models {
			copied.Models[model] = tokens
		}
		result[k] = copied
	}
	return result
}

func init() {
	coreusage.RegisterPlugin(&AccountUsagePlugin{})
}

// RequestUsageStats combines per-account usage with the standard API key stats.
type RequestUsageStats struct {
	ByAccount map[string]AccountUsageData `json:"by_account"`
	ByAPIKey  map[string]APISnapshot      `json:"by_api_key"`
}

type AccountUsageData struct {
	TotalRequests int64            `json:"total_requests"`
	TotalTokens   int64            `json:"total_tokens"`
	FailedCount   int64            `json:"failed_count"`
	Models        map[string]int64 `json:"models"`
}

// GetRequestUsageStats returns combined usage data for the dashboard.
func GetRequestUsageStats() RequestUsageStats {
	accountStore := GetAccountUsageStore()
	apiStats := GetRequestStatistics().Snapshot()

	result := RequestUsageStats{
		ByAccount: make(map[string]AccountUsageData),
		ByAPIKey:  apiStats.APIs,
	}

	for email, acc := range accountStore.Snapshot() {
		result.ByAccount[email] = AccountUsageData{
			TotalRequests: acc.TotalRequests,
			TotalTokens:   acc.TotalTokens,
			FailedCount:   acc.FailedCount,
			Models:        acc.Models,
		}
	}

	return result
}

// StartUsagePersistence starts a background goroutine that periodically saves usage stats.
func StartUsagePersistence(authDir string, interval time.Duration) {
	store := GetAccountUsageStore()
	removeLegacyAccountUsageFiles(authDir)
	store.SetStorageDir(authDir)

	if err := store.Load(); err != nil {
		log.Debugf("No persisted usage stats found: %v", err)
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			if err := store.Persist(); err != nil {
				log.Warnf("Failed to persist usage stats: %v", err)
			}
		}
	}()
}
