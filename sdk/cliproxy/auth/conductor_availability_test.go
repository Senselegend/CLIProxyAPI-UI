package auth

import (
	"testing"
	"time"
)

func TestUpdateAggregatedAvailability_UnavailableWithoutNextRetryDoesNotBlockAuth(t *testing.T) {
	t.Parallel()

	now := time.Now()
	model := "test-model"
	auth := &Auth{
		ID: "a",
		ModelStates: map[string]*ModelState{
			model: {
				Status:      StatusError,
				Unavailable: true,
			},
		},
	}

	updateAggregatedAvailability(auth, now)

	if auth.Unavailable {
		t.Fatalf("auth.Unavailable = true, want false")
	}
	if !auth.NextRetryAfter.IsZero() {
		t.Fatalf("auth.NextRetryAfter = %v, want zero", auth.NextRetryAfter)
	}
}

func TestUpdateAggregatedAvailability_FutureNextRetryBlocksAuth(t *testing.T) {
	t.Parallel()

	now := time.Now()
	model := "test-model"
	next := now.Add(5 * time.Minute)
	auth := &Auth{
		ID: "a",
		ModelStates: map[string]*ModelState{
			model: {
				Status:         StatusError,
				Unavailable:    true,
				NextRetryAfter: next,
			},
		},
	}

	updateAggregatedAvailability(auth, now)

	if !auth.Unavailable {
		t.Fatalf("auth.Unavailable = false, want true")
	}
	if auth.NextRetryAfter.IsZero() {
		t.Fatalf("auth.NextRetryAfter = zero, want %v", next)
	}
	if auth.NextRetryAfter.Sub(next) > time.Second || next.Sub(auth.NextRetryAfter) > time.Second {
		t.Fatalf("auth.NextRetryAfter = %v, want %v", auth.NextRetryAfter, next)
	}
}

func TestUpdateAggregatedAvailability_DoesNotClearDeactivatedStatus(t *testing.T) {
	t.Parallel()

	now := time.Now()
	auth := &Auth{
		ID:            "a",
		Status:        StatusDeactivated,
		StatusMessage: "revoked token",
		Unavailable:   true,
		LastError:     &Error{Message: "revoked token"},
		ModelStates: map[string]*ModelState{
			"test-model": {
				Status:         StatusError,
				Unavailable:    true,
				NextRetryAfter: now.Add(5 * time.Minute),
			},
		},
	}

	updateAggregatedAvailability(auth, now)

	if auth.Status != StatusDeactivated {
		t.Fatalf("auth.Status = %q, want %q", auth.Status, StatusDeactivated)
	}
	if auth.StatusMessage != "revoked token" {
		t.Fatalf("auth.StatusMessage = %q, want %q", auth.StatusMessage, "revoked token")
	}
	if auth.LastError == nil || auth.LastError.Message != "revoked token" {
		t.Fatalf("auth.LastError = %#v, want message %q", auth.LastError, "revoked token")
	}
}
