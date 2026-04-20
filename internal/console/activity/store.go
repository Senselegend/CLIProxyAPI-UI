package activity

import (
	"fmt"
	"strings"
	"sync"
	"time"

	coreusage "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/usage"
)

const defaultCapacity = 500

var defaultStore = NewStore(defaultCapacity)

type Store struct {
	mu       sync.RWMutex
	capacity int
	entries  []Entry
	index    map[string]int
}

func DefaultStore() *Store {
	return defaultStore
}

func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = defaultCapacity
	}
	return &Store{
		capacity: capacity,
		entries:  make([]Entry, 0, capacity),
		index:    make(map[string]int),
	}
}

func (s *Store) Reset() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = s.entries[:0]
	s.index = make(map[string]int)
}

func (s *Store) Start(event StartEvent) {
	if s == nil {
		return
	}
	id := strings.TrimSpace(event.ID)
	if id == "" {
		return
	}
	startedAt := event.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	transport := strings.TrimSpace(event.DownstreamTransport)
	if transport == "" {
		transport = "http"
	}
	entry := Entry{
		ID:                  id,
		Method:              strings.TrimSpace(event.Method),
		Path:                strings.TrimSpace(event.Path),
		Transport:           transport,
		DownstreamTransport: transport,
		Status:              "pending",
		RequestedAt:         startedAt,
		Message:             messageFor(event.Method, event.Path),
		Account:             "--",
		Model:               "--",
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.upsertLocked(entry)
}

func (s *Store) Finish(event FinishEvent) {
	if s == nil {
		return
	}
	id := strings.TrimSpace(event.ID)
	if id == "" {
		return
	}
	finishedAt := event.FinishedAt
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.index[id]
	if !ok {
		return
	}
	entry := &s.entries[idx]
	entry.HTTPStatus = event.HTTPStatus
	entry.LatencyMs = event.Latency.Milliseconds()
	entry.FinishedAt = &finishedAt
	entry.Status = statusFor(event.HTTPStatus, false)
}

func (s *Store) EnrichUsage(requestID string, record coreusage.Record) {
	if s == nil {
		return
	}
	id := strings.TrimSpace(requestID)

	s.mu.Lock()
	defer s.mu.Unlock()
	idx, ok := s.findUsageTargetLocked(id, record)
	if !ok {
		return
	}
	entry := &s.entries[idx]
	if provider := strings.TrimSpace(record.Provider); provider != "" {
		entry.Provider = provider
	}
	if model := strings.TrimSpace(record.Model); model != "" {
		entry.Model = model
	}
	if account := accountFromRecord(record); account != "" {
		entry.Account = account
	}
	entry.Tokens = TokenStats{
		InputTokens:     record.Detail.InputTokens,
		OutputTokens:    record.Detail.OutputTokens,
		ReasoningTokens: record.Detail.ReasoningTokens,
		CachedTokens:    record.Detail.CachedTokens,
		TotalTokens:     record.Detail.TotalTokens,
	}
	if record.Failed {
		entry.Status = "error"
	} else if entry.Status == "" || entry.Status == "pending" {
		entry.Status = statusFor(entry.HTTPStatus, false)
	}
}

func (s *Store) Snapshot(opts SnapshotOptions) []Entry {
	if s == nil {
		return []Entry{}
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	limit := opts.Limit
	if limit <= 0 || limit > len(s.entries) {
		limit = len(s.entries)
	}
	out := make([]Entry, 0, limit)
	for i := len(s.entries) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, s.entries[i])
	}
	return out
}

func (s *Store) upsertLocked(entry Entry) {
	if idx, ok := s.index[entry.ID]; ok {
		s.entries[idx] = entry
		return
	}
	if len(s.entries) >= s.capacity {
		old := s.entries[0]
		delete(s.index, old.ID)
		copy(s.entries, s.entries[1:])
		s.entries = s.entries[:len(s.entries)-1]
		for id, idx := range s.index {
			s.index[id] = idx - 1
		}
	}
	s.entries = append(s.entries, entry)
	s.index[entry.ID] = len(s.entries) - 1
}

func (s *Store) findUsageTargetLocked(id string, record coreusage.Record) (int, bool) {
	if id != "" {
		if idx, ok := s.index[id]; ok {
			return idx, true
		}
	}
	if !record.RequestedAt.IsZero() {
		for i := len(s.entries) - 1; i >= 0; i-- {
			entry := s.entries[i]
			if !entry.RequestedAt.IsZero() && entry.RequestedAt.Sub(record.RequestedAt).Abs() <= 5*time.Second {
				return i, true
			}
		}
	}
	return 0, false
}

func messageFor(method, path string) string {
	combined := strings.TrimSpace(strings.TrimSpace(method) + " " + strings.TrimSpace(path))
	if combined == "" {
		return "--"
	}
	return combined
}

func statusFor(httpStatus int, failed bool) string {
	if failed || httpStatus >= 500 {
		return "error"
	}
	if httpStatus >= 400 {
		return "warning"
	}
	if httpStatus == 0 {
		return "pending"
	}
	return "success"
}

func accountFromRecord(record coreusage.Record) string {
	for _, value := range []string{record.Source, record.AuthID, record.APIKey, record.AuthIndex} {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func (e Entry) String() string {
	return fmt.Sprintf("%s %s %s", e.ID, e.Method, e.Path)
}
