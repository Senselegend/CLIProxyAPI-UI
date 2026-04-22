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

// FlushDefaultAccountUsage persists the shared account usage store immediately.
func FlushDefaultAccountUsage() error { return GetAccountUsageStore().Persist() }

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

type DashboardSummaryWindow struct {
	Requests int64   `json:"requests"`
	Tokens   int64   `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
	Errors   int64   `json:"errors"`
}

type DashboardTopSummary struct {
	Lifetime   DashboardSummaryWindow `json:"lifetime"`
	Today      DashboardSummaryWindow `json:"today"`
	Last7Days  DashboardSummaryWindow `json:"last_7_days"`
	Last30Days DashboardSummaryWindow `json:"last_30_days"`
}

// RequestUsageStats combines per-account usage with the standard API key stats.
type RequestUsageStats struct {
	ByAccount map[string]AccountUsageData `json:"by_account"`
	ByAPIKey  map[string]APISnapshot      `json:"by_api_key"`
	Summary   DashboardTopSummary         `json:"summary"`
}

type AccountUsageWindowData struct {
	TotalTokens int64 `json:"total_tokens"`
}

type AccountUsageData struct {
	TotalRequests int64                 `json:"total_requests"`
	TotalTokens   int64                 `json:"total_tokens"`
	FailedCount   int64                 `json:"failed_count"`
	Models        map[string]int64      `json:"models"`
	Last5Hours    AccountUsageWindowData `json:"last_5_hours"`
	Last7Days     AccountUsageWindowData `json:"last_7_days"`
}

func BuildDashboardSummaryAt(accounts map[string]accountUsage, snapshot StatisticsSnapshot, now time.Time) DashboardTopSummary {
	const usdPerToken = 0.00001
	result := DashboardTopSummary{}

	for _, acc := range accounts {
		result.Lifetime.Requests += acc.TotalRequests
		result.Lifetime.Tokens += acc.TotalTokens
		result.Lifetime.Errors += acc.FailedCount
	}
	result.Lifetime.CostUSD = float64(result.Lifetime.Tokens) * usdPerToken

	todayKey := now.Format("2006-01-02")
	sevenDayCutoff := now.AddDate(0, 0, -6)
	thirtyDayCutoff := now.AddDate(0, 0, -29)

	for day, requests := range snapshot.RequestsByDay {
		dayTime, err := time.ParseInLocation("2006-01-02", day, now.Location())
		if err != nil {
			continue
		}
		tokens := snapshot.TokensByDay[day]
		errors := snapshot.FailuresByDay[day]
		if day == todayKey {
			result.Today.Requests += requests
			result.Today.Tokens += tokens
			result.Today.Errors += errors
		}
		if !dayTime.Before(sevenDayCutoff) {
			result.Last7Days.Requests += requests
			result.Last7Days.Tokens += tokens
			result.Last7Days.Errors += errors
		}
		if !dayTime.Before(thirtyDayCutoff) {
			result.Last30Days.Requests += requests
			result.Last30Days.Tokens += tokens
			result.Last30Days.Errors += errors
		}
	}

	result.Today.CostUSD = float64(result.Today.Tokens) * usdPerToken
	result.Last7Days.CostUSD = float64(result.Last7Days.Tokens) * usdPerToken
	result.Last30Days.CostUSD = float64(result.Last30Days.Tokens) * usdPerToken
	return result
}

func GetRequestUsageStatsAt(snapshot StatisticsSnapshot, now time.Time) RequestUsageStats {
	result := RequestUsageStats{
		ByAccount: make(map[string]AccountUsageData),
		ByAPIKey:  snapshot.APIs,
	}

	fiveHourCutoff := now.Add(-5 * time.Hour)
	sevenDayCutoff := now.Add(-7 * 24 * time.Hour)

	for _, apiSnapshot := range snapshot.APIs {
		for _, modelSnapshot := range apiSnapshot.Models {
			for _, detail := range modelSnapshot.Details {
				accountKey := strings.TrimSpace(detail.Account)
				if accountKey == "" {
					accountKey = strings.TrimSpace(detail.Source)
				}
				if accountKey == "" {
					continue
				}
				entry := result.ByAccount[accountKey]
				if !detail.Timestamp.IsZero() {
					if !detail.Timestamp.Before(fiveHourCutoff) {
						entry.Last5Hours.TotalTokens += detail.Tokens.TotalTokens
					}
					if !detail.Timestamp.Before(sevenDayCutoff) {
						entry.Last7Days.TotalTokens += detail.Tokens.TotalTokens
					}
				}
				result.ByAccount[accountKey] = entry
			}
		}
	}

	return result
}

// GetRequestUsageStats returns combined usage data for the dashboard.
func GetRequestUsageStats() RequestUsageStats {
	accountStore := GetAccountUsageStore()
	apiStats := GetRequestStatistics().Snapshot()
	now := time.Now()
	accounts := accountStore.Snapshot()
	result := GetRequestUsageStatsAt(apiStats, now)
	result.Summary = BuildDashboardSummaryAt(accounts, apiStats, now)

	for email, acc := range accounts {
		entry := result.ByAccount[email]
		entry.TotalRequests = acc.TotalRequests
		entry.TotalTokens = acc.TotalTokens
		entry.FailedCount = acc.FailedCount
		entry.Models = acc.Models
		result.ByAccount[email] = entry
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
