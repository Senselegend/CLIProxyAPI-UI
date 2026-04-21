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

func TestApplyAuthFailureState_QuotaFailureBecomesRateLimited(t *testing.T) {
	t.Parallel()

	now := time.Now()
	retryAfter := 5 * time.Hour
	auth := &Auth{ID: "quota-auth"}

	applyAuthFailureState(auth, &Error{HTTPStatus: 429, Message: "quota exhausted"}, &retryAfter, now)

	if auth.Status != StatusRateLimited {
		t.Fatalf("auth.Status = %q, want %q", auth.Status, StatusRateLimited)
	}
	if !auth.Unavailable {
		t.Fatalf("auth.Unavailable = false, want true")
	}
	if !auth.Quota.Exceeded {
		t.Fatalf("auth.Quota.Exceeded = false, want true")
	}
	if auth.Quota.Reason != "quota" {
		t.Fatalf("auth.Quota.Reason = %q, want %q", auth.Quota.Reason, "quota")
	}
	want := now.Add(retryAfter)
	if !auth.NextRetryAfter.Equal(want) {
		t.Fatalf("auth.NextRetryAfter = %v, want %v", auth.NextRetryAfter, want)
	}
	if !auth.Quota.NextRecoverAt.Equal(want) {
		t.Fatalf("auth.Quota.NextRecoverAt = %v, want %v", auth.Quota.NextRecoverAt, want)
	}
}

func TestApplyAuthFailureState_TransientFailureStaysError(t *testing.T) {
	t.Parallel()

	now := time.Now()
	auth := &Auth{ID: "transient-auth"}

	applyAuthFailureState(auth, &Error{HTTPStatus: 503, Message: "upstream unavailable"}, nil, now)

	if auth.Status != StatusError {
		t.Fatalf("auth.Status = %q, want %q", auth.Status, StatusError)
	}
	if !auth.Unavailable {
		t.Fatalf("auth.Unavailable = false, want true")
	}
	if auth.Quota.Exceeded {
		t.Fatalf("auth.Quota.Exceeded = true, want false")
	}
	if !auth.Quota.NextRecoverAt.IsZero() {
		t.Fatalf("auth.Quota.NextRecoverAt = %v, want zero", auth.Quota.NextRecoverAt)
	}
	want := now.Add(1 * time.Minute)
	if !auth.NextRetryAfter.Equal(want) {
		t.Fatalf("auth.NextRetryAfter = %v, want %v", auth.NextRetryAfter, want)
	}
}

func TestUpdateAggregatedAvailability_QuotaRecoveryPrefersSecondaryWindow(t *testing.T) {
	t.Parallel()

	now := time.Now()
	primaryRecover := now.Add(5 * time.Hour)
	secondaryRecover := now.Add(7 * 24 * time.Hour)
	auth := &Auth{
		ID: "a",
		ModelStates: map[string]*ModelState{
			"primary": {
				Status:      StatusRateLimited,
				Unavailable: true,
				Quota: QuotaState{
					Exceeded:      true,
					Reason:        "quota",
					NextRecoverAt: primaryRecover,
					BackoffLevel:  1,
				},
			},
			"secondary": {
				Status:      StatusRateLimited,
				Unavailable: true,
				Quota: QuotaState{
					Exceeded:      true,
					Reason:        "quota",
					NextRecoverAt: secondaryRecover,
					BackoffLevel:  2,
				},
			},
		},
	}

	updateAggregatedAvailability(auth, now)

	if !auth.Quota.Exceeded {
		t.Fatalf("auth.Quota.Exceeded = false, want true")
	}
	if !auth.Quota.NextRecoverAt.Equal(secondaryRecover) {
		t.Fatalf("auth.Quota.NextRecoverAt = %v, want %v", auth.Quota.NextRecoverAt, secondaryRecover)
	}
	if auth.Quota.BackoffLevel != 2 {
		t.Fatalf("auth.Quota.BackoffLevel = %d, want %d", auth.Quota.BackoffLevel, 2)
	}
	if auth.NextRetryAfter != (time.Time{}) {
		t.Fatalf("auth.NextRetryAfter = %v, want zero", auth.NextRetryAfter)
	}
}

func TestUpdateAggregatedAvailability_IgnoresExpiredQuotaRecovery(t *testing.T) {
	t.Parallel()

	now := time.Now()
	expiredRecover := now.Add(-5 * time.Minute)
	activeRecover := now.Add(2 * time.Hour)
	auth := &Auth{
		ID: "a",
		ModelStates: map[string]*ModelState{
			"expired": {
				Status:      StatusRateLimited,
				Unavailable: false,
				Quota: QuotaState{
					Exceeded:      true,
					Reason:        "quota",
					NextRecoverAt: expiredRecover,
					BackoffLevel:  4,
				},
			},
			"active": {
				Status:      StatusRateLimited,
				Unavailable: true,
				Quota: QuotaState{
					Exceeded:      true,
					Reason:        "quota",
					NextRecoverAt: activeRecover,
					BackoffLevel:  2,
				},
			},
		},
	}

	updateAggregatedAvailability(auth, now)

	if !auth.Quota.Exceeded {
		t.Fatalf("auth.Quota.Exceeded = false, want true")
	}
	if !auth.Quota.NextRecoverAt.Equal(activeRecover) {
		t.Fatalf("auth.Quota.NextRecoverAt = %v, want %v", auth.Quota.NextRecoverAt, activeRecover)
	}
	if auth.Quota.BackoffLevel != 2 {
		t.Fatalf("auth.Quota.BackoffLevel = %d, want %d", auth.Quota.BackoffLevel, 2)
	}
	expired := auth.ModelStates["expired"]
	if expired == nil {
		t.Fatalf("expired model state missing")
	}
	if expired.Quota.Exceeded {
		t.Fatalf("expired.Quota.Exceeded = true, want false")
	}
	if !expired.Quota.NextRecoverAt.IsZero() {
		t.Fatalf("expired.Quota.NextRecoverAt = %v, want zero", expired.Quota.NextRecoverAt)
	}
	if expired.Quota.BackoffLevel != 0 {
		t.Fatalf("expired.Quota.BackoffLevel = %d, want 0", expired.Quota.BackoffLevel)
	}
}

func TestUpdateAggregatedAvailability_IgnoresZeroQuotaRecovery(t *testing.T) {
	t.Parallel()

	now := time.Now()
	activeRecover := now.Add(2 * time.Hour)
	auth := &Auth{
		ID: "a",
		ModelStates: map[string]*ModelState{
			"zero": {
				Status:      StatusRateLimited,
				Unavailable: false,
				Quota: QuotaState{
					Exceeded:     true,
					Reason:       "quota",
					BackoffLevel: 4,
				},
			},
			"active": {
				Status:      StatusRateLimited,
				Unavailable: true,
				Quota: QuotaState{
					Exceeded:      true,
					Reason:        "quota",
					NextRecoverAt: activeRecover,
					BackoffLevel:  2,
				},
			},
		},
	}

	updateAggregatedAvailability(auth, now)

	if !auth.Quota.Exceeded {
		t.Fatalf("auth.Quota.Exceeded = false, want true")
	}
	if !auth.Quota.NextRecoverAt.Equal(activeRecover) {
		t.Fatalf("auth.Quota.NextRecoverAt = %v, want %v", auth.Quota.NextRecoverAt, activeRecover)
	}
	if auth.Quota.BackoffLevel != 2 {
		t.Fatalf("auth.Quota.BackoffLevel = %d, want %d", auth.Quota.BackoffLevel, 2)
	}
	zero := auth.ModelStates["zero"]
	if zero == nil {
		t.Fatalf("zero model state missing")
	}
	if zero.Quota.Exceeded {
		t.Fatalf("zero.Quota.Exceeded = true, want false")
	}
	if !zero.Quota.NextRecoverAt.IsZero() {
		t.Fatalf("zero.Quota.NextRecoverAt = %v, want zero", zero.Quota.NextRecoverAt)
	}
	if zero.Quota.BackoffLevel != 0 {
		t.Fatalf("zero.Quota.BackoffLevel = %d, want 0", zero.Quota.BackoffLevel)
	}
}
