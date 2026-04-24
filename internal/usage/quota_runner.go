package usage

import (
	"context"
	"sync"
)

const defaultQuotaSyncConcurrency = 4

func RunQuotaSyncWork(ctx context.Context, accounts []AuthProvider, fn func(context.Context, AuthProvider) error) error {
	return runQuotaWork(ctx, accounts, defaultQuotaSyncConcurrency, fn)
}

func runQuotaWork(ctx context.Context, accounts []AuthProvider, concurrency int, fn func(context.Context, AuthProvider) error) error {
	if len(accounts) == 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = defaultQuotaSyncConcurrency
	}
	if concurrency > len(accounts) {
		concurrency = len(accounts)
	}

	work := make(chan AuthProvider)
	var wg sync.WaitGroup
	var errMu sync.Mutex
	var firstErr error

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for account := range work {
				if err := fn(ctx, account); err != nil {
					errMu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					errMu.Unlock()
				}
			}
		}()
	}

	for _, account := range accounts {
		work <- account
	}
	close(work)
	wg.Wait()

	return firstErr
}
