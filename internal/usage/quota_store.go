package usage

import "sync"

// QuotaStore holds quota data for all accounts.
type QuotaStore struct {
	mu     sync.RWMutex
	quotas map[string]*AccountQuota
}

var defaultQuotaStore = NewQuotaStore()

func NewQuotaStore() *QuotaStore {
	return &QuotaStore{quotas: make(map[string]*AccountQuota)}
}

func (s *QuotaStore) Get(accountID string) *AccountQuota {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.quotas[accountID]
}

func (s *QuotaStore) Set(accountID string, quota *AccountQuota) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.quotas[accountID] = quota
}

func (s *QuotaStore) Snapshot() []AccountQuota {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]AccountQuota, 0, len(s.quotas))
	for _, q := range s.quotas {
		copied := *q
		result = append(result, copied)
	}
	return result
}

func GetQuotaStore() *QuotaStore { return defaultQuotaStore }
