package usage

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunQuotaWorkCapsConcurrency(t *testing.T) {
	accounts := []AuthProvider{
		testAuthProvider{id: "account-1"},
		testAuthProvider{id: "account-2"},
		testAuthProvider{id: "account-3"},
		testAuthProvider{id: "account-4"},
		testAuthProvider{id: "account-5"},
	}

	const concurrency = 2
	started := make(chan struct{}, len(accounts))
	release := make(chan struct{})
	done := make(chan error, 1)

	released := false
	releaseAll := func() {
		if !released {
			close(release)
			released = true
		}
	}
	defer releaseAll()

	var inFlight int32
	var maxInFlight int32

	go func() {
		done <- runQuotaWork(context.Background(), accounts, concurrency, func(context.Context, AuthProvider) error {
			current := atomic.AddInt32(&inFlight, 1)
			for {
				max := atomic.LoadInt32(&maxInFlight)
				if current <= max || atomic.CompareAndSwapInt32(&maxInFlight, max, current) {
					break
				}
			}
			started <- struct{}{}
			<-release
			atomic.AddInt32(&inFlight, -1)
			return nil
		})
	}()

	waitForQuotaWorkStarts(t, started, concurrency)

	if got := atomic.LoadInt32(&maxInFlight); got != concurrency {
		t.Fatalf("max in-flight before release = %d, want %d", got, concurrency)
	}

	releaseAll()

	if err := waitForQuotaWorkDone(t, done); err != nil {
		t.Fatalf("runQuotaWork() error = %v", err)
	}
	if got := atomic.LoadInt32(&maxInFlight); got > concurrency {
		t.Fatalf("max in-flight = %d, want at most %d", got, concurrency)
	}
}

func TestRunQuotaWorkEmptyAccountsReturnsNil(t *testing.T) {
	called := false
	err := runQuotaWork(context.Background(), nil, 1, func(context.Context, AuthProvider) error {
		called = true
		return nil
	})
	if err != nil {
		t.Fatalf("runQuotaWork() error = %v, want nil", err)
	}
	if called {
		t.Fatalf("callback called for empty accounts")
	}
}

func TestRunQuotaWorkDefaultConcurrency(t *testing.T) {
	accounts := []AuthProvider{
		testAuthProvider{id: "account-1"},
		testAuthProvider{id: "account-2"},
		testAuthProvider{id: "account-3"},
		testAuthProvider{id: "account-4"},
		testAuthProvider{id: "account-5"},
	}
	if len(accounts) <= defaultQuotaSyncConcurrency {
		t.Fatalf("test requires more accounts than default concurrency")
	}

	started := make(chan struct{}, len(accounts))
	release := make(chan struct{})
	done := make(chan error, 1)

	released := false
	releaseAll := func() {
		if !released {
			close(release)
			released = true
		}
	}
	defer releaseAll()

	go func() {
		done <- runQuotaWork(context.Background(), accounts, 0, func(context.Context, AuthProvider) error {
			started <- struct{}{}
			<-release
			return nil
		})
	}()

	waitForQuotaWorkStarts(t, started, defaultQuotaSyncConcurrency)

	select {
	case <-started:
		t.Fatalf("runQuotaWork started more than default concurrency before release")
	default:
	}

	releaseAll()

	if err := waitForQuotaWorkDone(t, done); err != nil {
		t.Fatalf("runQuotaWork() error = %v", err)
	}
}

func TestRunQuotaWorkProcessesAllAccounts(t *testing.T) {
	accounts := []AuthProvider{
		testAuthProvider{id: "account-1"},
		testAuthProvider{id: "account-2"},
		testAuthProvider{id: "account-3"},
	}

	processed := make(map[string]bool)
	var mu sync.Mutex

	err := runQuotaWork(context.Background(), accounts, 2, func(_ context.Context, account AuthProvider) error {
		mu.Lock()
		defer mu.Unlock()
		processed[account.ID()] = true
		return nil
	})
	if err != nil {
		t.Fatalf("runQuotaWork() error = %v", err)
	}

	for _, account := range accounts {
		if !processed[account.ID()] {
			t.Fatalf("account %q was not processed", account.ID())
		}
	}
}

func TestRunQuotaWorkReturnsFirstErrorAfterCallbacksComplete(t *testing.T) {
	accounts := []AuthProvider{
		testAuthProvider{id: "account-1"},
		testAuthProvider{id: "account-2"},
	}
	errFirst := errors.New("first error")
	errSecond := errors.New("second error")
	blockedStarted := make(chan struct{}, 1)
	releaseBlocked := make(chan struct{})
	done := make(chan error, 1)

	released := false
	releaseAll := func() {
		if !released {
			close(releaseBlocked)
			released = true
		}
	}
	defer releaseAll()

	var completed int32
	go func() {
		done <- runQuotaWork(context.Background(), accounts, 2, func(_ context.Context, account AuthProvider) error {
			defer atomic.AddInt32(&completed, 1)
			if account.ID() == "account-1" {
				return errFirst
			}
			blockedStarted <- struct{}{}
			<-releaseBlocked
			return errSecond
		})
	}()

	waitForQuotaWorkStarts(t, blockedStarted, 1)

	select {
	case err := <-done:
		t.Fatalf("runQuotaWork() returned before all callbacks completed: %v", err)
	default:
	}

	releaseAll()

	if err := waitForQuotaWorkDone(t, done); !errors.Is(err, errFirst) {
		t.Fatalf("runQuotaWork() error = %v, want %v", err, errFirst)
	}
	if got := atomic.LoadInt32(&completed); got != int32(len(accounts)) {
		t.Fatalf("completed callbacks = %d, want %d", got, len(accounts))
	}
}

func waitForQuotaWorkStarts(t *testing.T, started <-chan struct{}, count int) {
	t.Helper()
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	for i := 0; i < count; i++ {
		select {
		case <-started:
		case <-timer.C:
			t.Fatalf("timed out waiting for quota work start %d of %d", i+1, count)
		}
	}
}

func waitForQuotaWorkDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for runQuotaWork to complete")
		return nil
	}
}
