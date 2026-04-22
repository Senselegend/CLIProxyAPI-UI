package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func TestClassifyPermanentAuthFailure_ExplicitSignalsOnly(t *testing.T) {
	cases := []struct {
		name string
		err  *Error
		want bool
	}{
		{name: "401 revoked token", err: &Error{HTTPStatus: 401, Message: "revoked token"}, want: true},
		{name: "403 banned account", err: &Error{HTTPStatus: 403, Message: "account banned"}, want: true},
		{name: "context canceled is transient", err: &Error{Message: "context canceled"}, want: false},
		{name: "generic unauthorized stays non-permanent", err: &Error{HTTPStatus: 401, Message: "unauthorized"}, want: false},
		{name: "402 payment required is not permanent", err: &Error{HTTPStatus: 402, Message: "payment required"}, want: false},
		{name: "403 payment_required is not permanent", err: &Error{HTTPStatus: 403, Message: "payment_required"}, want: false},
		{name: "429 quota exhausted is not permanent", err: &Error{HTTPStatus: 429, Message: "quota exhausted"}, want: false},
		{name: "503 transient upstream error is not permanent", err: &Error{HTTPStatus: 503, Message: "transient upstream error"}, want: false},
		{name: "403 generic forbidden is not permanent", err: &Error{HTTPStatus: 403, Message: "forbidden"}, want: false},
		{name: "403 forbidden with allowed permanent phrase is permanent", err: &Error{HTTPStatus: 403, Message: "account disabled by provider"}, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPermanentAuthFailure(tc.err); got != tc.want {
				t.Fatalf("isPermanentAuthFailure() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestApplyAuthFailureState_PermanentFailureBecomesDeactivated(t *testing.T) {
	now := time.Now()
	resultErr := &Error{HTTPStatus: 401, Message: "revoked token"}
	auth := &Auth{
		ID:             "auth-1",
		Provider:       "codex",
		Status:         StatusActive,
		StatusMessage:  "previous status",
		Unavailable:    false,
		NextRetryAfter: now.Add(10 * time.Minute),
		Quota: QuotaState{
			Exceeded:      true,
			Reason:        "quota exhausted",
			NextRecoverAt: now.Add(20 * time.Minute),
			BackoffLevel:  2,
		},
	}

	applyAuthFailureState(auth, resultErr, nil, now)

	if auth.Status != StatusDeactivated {
		t.Fatalf("Status = %q, want %q", auth.Status, StatusDeactivated)
	}
	if auth.StatusMessage != "revoked token" {
		t.Fatalf("StatusMessage = %q, want %q", auth.StatusMessage, "revoked token")
	}
	if auth.LastError == nil {
		t.Fatalf("LastError = nil, want preserved error")
	}
	if auth.LastError == resultErr {
		t.Fatalf("LastError should be cloned, but points to the original error")
	}
	if auth.LastError.Message != resultErr.Message {
		t.Fatalf("LastError.Message = %q, want %q", auth.LastError.Message, resultErr.Message)
	}
	if auth.LastError.HTTPStatus != resultErr.HTTPStatus {
		t.Fatalf("LastError.HTTPStatus = %d, want %d", auth.LastError.HTTPStatus, resultErr.HTTPStatus)
	}
	if !auth.NextRetryAfter.IsZero() {
		t.Fatalf("NextRetryAfter = %v, want zero value", auth.NextRetryAfter)
	}
	if auth.Quota != (QuotaState{}) {
		t.Fatalf("Quota = %#v, want zero value", auth.Quota)
	}
	if !auth.Unavailable {
		t.Fatalf("Unavailable = %v, want true", auth.Unavailable)
	}
}

func TestMaybeScheduleAuthRecheck_DeduplicatesByAuthID(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.recheckCooldown = 30 * time.Second

	release := make(chan struct{})
	startedFirst := make(chan struct{})
	finished := make(chan struct{})
	calls := make(chan string, 2)
	m.recheckRunner = func(_ context.Context, authID string) {
		calls <- authID
		select {
		case startedFirst <- struct{}{}:
		default:
		}
		<-release
		close(finished)
	}

	m.maybeScheduleAuthRecheck(context.Background(), &Auth{ID: "auth-1", Status: StatusError})

	deadline := time.Now().Add(time.Second)
	for {
		m.recheckMu.Lock()
		inFlight := len(m.recheckInFlight)
		m.recheckMu.Unlock()
		if inFlight == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("expected recheck to be marked in flight")
		}
		time.Sleep(time.Millisecond)
	}

	m.maybeScheduleAuthRecheck(context.Background(), &Auth{ID: "auth-1", Status: StatusError})

	select {
	case <-startedFirst:
	case <-time.After(time.Second):
		t.Fatalf("expected first recheck runner to start")
	}

	m.recheckMu.Lock()
	if len(m.recheckInFlight) != 1 {
		m.recheckMu.Unlock()
		t.Fatalf("len(recheckInFlight) = %d, want 1", len(m.recheckInFlight))
	}
	m.recheckMu.Unlock()

	select {
	case got := <-calls:
		if got != "auth-1" {
			t.Fatalf("runner authID = %q, want %q", got, "auth-1")
		}
	case <-time.After(time.Second):
		t.Fatalf("expected one runner invocation")
	}

	select {
	case got := <-calls:
		t.Fatalf("unexpected second runner invocation for %q", got)
	default:
	}

	close(release)

	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatalf("expected recheck runner to finish after release")
	}
}

func TestMaybeScheduleAuthRecheck_DetachesRunnerContext(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.recheckCooldown = 0

	ctxDone := make(chan (<-chan struct{}), 1)
	m.recheckRunner = func(ctx context.Context, _ string) {
		ctxDone <- ctx.Done()
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m.maybeScheduleAuthRecheck(ctx, &Auth{ID: "auth-1", Status: StatusError})

	select {
	case done := <-ctxDone:
		select {
		case <-done:
			t.Fatalf("expected detached runner context to remain uncanceled")
		default:
		}
	case <-time.After(time.Second):
		t.Fatalf("expected detached recheck runner to start")
	}
}

func TestMaybeScheduleAuthRecheck_SkipsQuotaCooldownAuth(t *testing.T) {
	m := NewManager(nil, nil, nil)
	called := false
	m.recheckRunner = func(context.Context, string) { called = true }

	m.maybeScheduleAuthRecheck(context.Background(), &Auth{
		ID:             "auth-1",
		Status:         StatusError,
		Unavailable:    true,
		NextRetryAfter: time.Now().Add(time.Minute),
		Quota:          QuotaState{Exceeded: true},
	})

	if called {
		t.Fatalf("recheck runner called for quota cooldown auth")
	}
}

func TestMaybeScheduleAuthRecheck_AllowsNonQuotaCooldownErrorAuth(t *testing.T) {
	m := NewManager(nil, nil, nil)
	triggered := make(chan string, 1)
	m.recheckRunner = func(_ context.Context, authID string) { triggered <- authID }

	m.maybeScheduleAuthRecheck(context.Background(), &Auth{
		ID:             "auth-1",
		Status:         StatusError,
		Unavailable:    true,
		NextRetryAfter: time.Now().Add(time.Minute),
		LastError:      &Error{HTTPStatus: 503, Message: "transient upstream error"},
	})

	select {
	case got := <-triggered:
		if got != "auth-1" {
			t.Fatalf("runner authID = %q, want %q", got, "auth-1")
		}
	case <-time.After(time.Second):
		t.Fatalf("expected non-quota unavailable auth to schedule recheck")
	}
}

func TestMaybeScheduleAuthRecheck_ClearsInFlightAfterRunnerReturns(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.recheckCooldown = 0

	done := make(chan struct{})
	m.recheckRunner = func(context.Context, string) {
		close(done)
	}

	m.maybeScheduleAuthRecheck(context.Background(), &Auth{ID: "auth-1", Status: StatusError})

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("expected recheck runner to finish")
	}

	m.recheckMu.Lock()
	defer m.recheckMu.Unlock()
	if _, exists := m.recheckInFlight["auth-1"]; exists {
		t.Fatalf("recheckInFlight still contains auth-1 after runner returned")
	}
}

func TestMarkResult_AuthErrorSchedulesBackgroundRecheck(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.recheckCooldown = 0
	triggered := make(chan string, 1)
	m.recheckRunner = func(ctx context.Context, authID string) { triggered <- authID }

	if _, err := m.Register(context.Background(), &Auth{ID: "auth-1", Provider: "codex", Status: StatusActive}); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	m.MarkResult(context.Background(), Result{AuthID: "auth-1", Provider: "codex", Success: false, Error: &Error{Message: "context canceled"}})

	select {
	case got := <-triggered:
		if got != "auth-1" {
			t.Fatalf("runner authID = %q, want %q", got, "auth-1")
		}
	case <-time.After(time.Second):
		t.Fatalf("expected recheck trigger")
	}
}

func TestManager_TriggerEligibleAuthRechecks_SkipsQuotaDisabledDeactivatedAndNotEligible(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.recheckCooldown = 0
	triggered := make(chan string, 10)
	m.recheckRunner = func(_ context.Context, authID string) { triggered <- authID }

	now := time.Now()
	m.auths = map[string]*Auth{
		"error-auth": {
			ID:          "error-auth",
			Status:      StatusError,
			Unavailable: true,
			LastError:   &Error{HTTPStatus: 500, Message: "temporary failure"},
		},
		"quota-auth": {
			ID:          "quota-auth",
			Status:      StatusError,
			Unavailable: true,
			Quota:       QuotaState{Exceeded: true},
			LastError:   &Error{HTTPStatus: 429, Message: "quota exhausted"},
		},
		"disabled-auth": {
			ID:       "disabled-auth",
			Status:   StatusError,
			Disabled: true,
		},
		"deactivated-auth": {
			ID:     "deactivated-auth",
			Status: StatusDeactivated,
		},
		"unknown-auth": {
			ID:          "unknown-auth",
			Status:      StatusUnknown,
			Unavailable: true,
			UpdatedAt:   now,
		},
		"not-found-auth": {
			ID:          "not-found-auth",
			Status:      StatusError,
			Unavailable: true,
			LastError:   &Error{HTTPStatus: 404, Message: "model not found"},
		},
		"unsupported-auth": {
			ID:          "unsupported-auth",
			Status:      StatusError,
			Unavailable: true,
			LastError:   &Error{HTTPStatus: 400, Message: "requested model is unsupported"},
		},
	}

	summary := m.TriggerEligibleAuthRechecks(context.Background())

	if summary.Considered != 7 {
		t.Fatalf("Considered = %d, want 7", summary.Considered)
	}
	if summary.Triggered != 2 {
		t.Fatalf("Triggered = %d, want 2", summary.Triggered)
	}
	if summary.SkippedRateLimited != 1 {
		t.Fatalf("SkippedRateLimited = %d, want 1", summary.SkippedRateLimited)
	}
	if summary.SkippedDisabled != 1 {
		t.Fatalf("SkippedDisabled = %d, want 1", summary.SkippedDisabled)
	}
	if summary.SkippedDeactivated != 1 {
		t.Fatalf("SkippedDeactivated = %d, want 1", summary.SkippedDeactivated)
	}
	if summary.SkippedNotEligible != 2 {
		t.Fatalf("SkippedNotEligible = %d, want 2", summary.SkippedNotEligible)
	}

	got := map[string]bool{}
	for range 2 {
		select {
		case authID := <-triggered:
			got[authID] = true
		case <-time.After(time.Second):
			t.Fatalf("expected two triggered auth rechecks")
		}
	}
	if !got["error-auth"] {
		t.Fatalf("expected error-auth to be rechecked")
	}
	if !got["unknown-auth"] {
		t.Fatalf("expected unknown-auth to be rechecked")
	}
}

func TestManager_TriggerEligibleAuthRechecks_ReportsAlreadyInFlight(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.recheckCooldown = 0
	m.recheckRunner = func(context.Context, string) {}
	m.auths = map[string]*Auth{
		"error-auth": {
			ID:          "error-auth",
			Status:      StatusError,
			Unavailable: true,
			LastError:   &Error{HTTPStatus: 500, Message: "temporary failure"},
		},
	}

	m.recheckMu.Lock()
	m.recheckInFlight["error-auth"] = struct{}{}
	m.recheckMu.Unlock()

	summary := m.TriggerEligibleAuthRechecks(context.Background())

	if summary.Considered != 1 {
		t.Fatalf("Considered = %d, want 1", summary.Considered)
	}
	if summary.Triggered != 0 {
		t.Fatalf("Triggered = %d, want 0", summary.Triggered)
	}
	if summary.AlreadyInFlight != 1 {
		t.Fatalf("AlreadyInFlight = %d, want 1", summary.AlreadyInFlight)
	}
}

func TestManager_TriggerEligibleAuthRechecks_CooldownCountsAsAlreadyInFlight(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.recheckCooldown = time.Minute
	m.recheckRunner = func(context.Context, string) {}
	m.auths = map[string]*Auth{
		"error-auth": {
			ID:          "error-auth",
			Status:      StatusError,
			Unavailable: true,
			LastError:   &Error{HTTPStatus: 500, Message: "temporary failure"},
		},
	}

	m.recheckMu.Lock()
	m.recheckLastRunAt["error-auth"] = time.Now()
	m.recheckMu.Unlock()

	summary := m.TriggerEligibleAuthRechecks(context.Background())

	if summary.Considered != 1 {
		t.Fatalf("Considered = %d, want 1", summary.Considered)
	}
	if summary.Triggered != 0 {
		t.Fatalf("Triggered = %d, want 0", summary.Triggered)
	}
	if summary.AlreadyInFlight != 1 {
		t.Fatalf("AlreadyInFlight = %d, want 1", summary.AlreadyInFlight)
	}
	if summary.SkippedNotEligible != 0 {
		t.Fatalf("SkippedNotEligible = %d, want 0", summary.SkippedNotEligible)
	}
}

func TestMarkResult_SuccessClearsErrorStatus(t *testing.T) {
	m := NewManager(nil, nil, nil)

	auth := &Auth{
		ID:             "auth-1",
		Provider:       "codex",
		Status:         StatusError,
		StatusMessage:  "context canceled",
		Unavailable:    true,
		NextRetryAfter: time.Now().Add(10 * time.Minute),
		LastError:      &Error{Message: "context canceled"},
	}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	m.MarkResult(context.Background(), Result{AuthID: auth.ID, Provider: auth.Provider, Success: true})

	m.mu.RLock()
	updated := m.auths[auth.ID].Clone()
	m.mu.RUnlock()

	if updated.Status != StatusActive {
		t.Fatalf("Status = %q, want %q", updated.Status, StatusActive)
	}
	if updated.StatusMessage != "" {
		t.Fatalf("StatusMessage = %q, want empty", updated.StatusMessage)
	}
	if updated.LastError != nil {
		t.Fatalf("LastError = %#v, want nil", updated.LastError)
	}
	if updated.Unavailable {
		t.Fatalf("Unavailable = %v, want false", updated.Unavailable)
	}
	if !updated.NextRetryAfter.IsZero() {
		t.Fatalf("NextRetryAfter = %v, want zero", updated.NextRetryAfter)
	}
}

func TestMarkResult_SuccessDoesNotClearDeactivatedStatus(t *testing.T) {
	m := NewManager(nil, nil, nil)

	auth := &Auth{
		ID:             "auth-1",
		Provider:       "codex",
		Status:         StatusDeactivated,
		StatusMessage:  "revoked token",
		Unavailable:    true,
		NextRetryAfter: time.Now().Add(10 * time.Minute),
		LastError:      &Error{HTTPStatus: http.StatusUnauthorized, Message: "revoked token"},
	}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	m.MarkResult(context.Background(), Result{AuthID: auth.ID, Provider: auth.Provider, Success: true})

	m.mu.RLock()
	updated := m.auths[auth.ID].Clone()
	m.mu.RUnlock()

	if updated.Status != StatusDeactivated {
		t.Fatalf("Status = %q, want %q", updated.Status, StatusDeactivated)
	}
	if updated.StatusMessage != "revoked token" {
		t.Fatalf("StatusMessage = %q, want %q", updated.StatusMessage, "revoked token")
	}
	if updated.LastError == nil || updated.LastError.Message != "revoked token" {
		t.Fatalf("LastError = %#v, want message %q", updated.LastError, "revoked token")
	}
	if !updated.Unavailable {
		t.Fatalf("Unavailable = %v, want true", updated.Unavailable)
	}
}

type authRecheckTestExecutor struct {
	identifier   string
	httpResponse *http.Response
	httpErr      error
	httpRequest  func(*http.Request)
}

func (e authRecheckTestExecutor) Identifier() string {
	if strings.TrimSpace(e.identifier) != "" {
		return e.identifier
	}
	return "codex"
}
func (e authRecheckTestExecutor) Execute(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}
func (e authRecheckTestExecutor) ExecuteStream(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (*cliproxyexecutor.StreamResult, error) {
	return nil, nil
}
func (e authRecheckTestExecutor) Refresh(context.Context, *Auth) (*Auth, error) { return nil, nil }
func (e authRecheckTestExecutor) CountTokens(context.Context, *Auth, cliproxyexecutor.Request, cliproxyexecutor.Options) (cliproxyexecutor.Response, error) {
	return cliproxyexecutor.Response{}, nil
}
func (e authRecheckTestExecutor) HttpRequest(_ context.Context, _ *Auth, req *http.Request) (*http.Response, error) {
	if e.httpRequest != nil && req != nil {
		e.httpRequest(req)
	}
	return e.httpResponse, e.httpErr
}

func TestAuthRecheckProbeRequestURL_UsesProviderSpecificEndpoint(t *testing.T) {
	cases := []struct {
		name string
		auth *Auth
		want string
	}{
		{
			name: "codex default responses endpoint",
			auth: &Auth{ID: "auth-codex", Provider: "codex"},
			want: "https://chatgpt.com/backend-api/codex/responses",
		},
		{
			name: "openai compat uses configured base url",
			auth: &Auth{ID: "auth-compat", Provider: "openai-compatibility", Attributes: map[string]string{"base_url": "https://pool.example.com/v1"}},
			want: "https://pool.example.com/v1/models",
		},
		{
			name: "claude uses anthropic models endpoint",
			auth: &Auth{ID: "auth-claude", Provider: "claude"},
			want: "https://api.anthropic.com/v1/models",
		},
		{
			name: "gemini uses generate content endpoint",
			auth: &Auth{ID: "auth-gemini", Provider: "gemini"},
			want: "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent",
		},
		{
			name: "gemini cli uses code assist endpoint",
			auth: &Auth{ID: "auth-gemini-cli", Provider: "gemini-cli"},
			want: "https://cloudcode-pa.googleapis.com/v1internal:countTokens",
		},
		{
			name: "vertex uses API key models endpoint when no location metadata",
			auth: &Auth{ID: "auth-vertex", Provider: "vertex"},
			want: "https://aiplatform.googleapis.com/v1/projects/-/locations/global/publishers/google/models",
		},
		{
			name: "vertex uses regional endpoint from metadata",
			auth: &Auth{ID: "auth-vertex-regional", Provider: "vertex", Metadata: map[string]any{"location": "europe-west4"}},
			want: "https://europe-west4-aiplatform.googleapis.com/v1/projects/-/locations/europe-west4/publishers/google/models",
		},
		{
			name: "kimi uses kimi models endpoint",
			auth: &Auth{ID: "auth-kimi", Provider: "kimi"},
			want: "https://api.moonshot.ai/v1/models",
		},
		{
			name: "antigravity uses custom base url when present",
			auth: &Auth{ID: "auth-antigravity", Provider: "antigravity", Attributes: map[string]string{"base_url": "https://daily-cloudcode-pa.googleapis.com"}},
			want: "https://daily-cloudcode-pa.googleapis.com/v1internal:countTokens",
		},
		{
			name: "unknown provider does not assume openai compatibility",
			auth: &Auth{ID: "auth-unknown", Provider: "custom"},
			want: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, err := authRecheckProbeRequest(context.Background(), tc.auth)
			if err != nil {
				t.Fatalf("authRecheckProbeRequest() error = %v", err)
			}
			got := ""
			if req != nil && req.URL != nil {
				got = req.URL.String()
			}
			if got != tc.want {
				t.Fatalf("authRecheckProbeRequest() url = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRunAuthRecheck_SuccessClearsTransientError(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.RegisterExecutor(authRecheckTestExecutor{
		httpResponse: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))},
	})

	auth := &Auth{
		ID:             "auth-1",
		Provider:       "codex",
		Status:         StatusError,
		StatusMessage:  "context canceled",
		Unavailable:    true,
		NextRetryAfter: time.Now().Add(10 * time.Minute),
		LastError:      &Error{Message: "context canceled"},
	}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	m.recheckMu.Lock()
	m.recheckInFlight[auth.ID] = struct{}{}
	m.recheckMu.Unlock()

	m.runAuthRecheck(context.Background(), auth.ID)

	m.mu.RLock()
	updated := m.auths[auth.ID].Clone()
	m.mu.RUnlock()

	if updated.Status != StatusActive {
		t.Fatalf("Status = %q, want %q", updated.Status, StatusActive)
	}
	if updated.StatusMessage != "" {
		t.Fatalf("StatusMessage = %q, want empty", updated.StatusMessage)
	}
	if updated.LastError != nil {
		t.Fatalf("LastError = %#v, want nil", updated.LastError)
	}
	if updated.Unavailable {
		t.Fatalf("Unavailable = %v, want false", updated.Unavailable)
	}
	if !updated.NextRetryAfter.IsZero() {
		t.Fatalf("NextRetryAfter = %v, want zero", updated.NextRetryAfter)
	}
}

func TestRunAuthRecheck_SuccessClearsDeactivatedStatus(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.RegisterExecutor(authRecheckTestExecutor{
		httpResponse: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))},
	})

	auth := &Auth{
		ID:             "auth-1",
		Provider:       "codex",
		Status:         StatusDeactivated,
		StatusMessage:  "revoked token",
		Unavailable:    true,
		NextRetryAfter: time.Now().Add(10 * time.Minute),
		LastError:      &Error{HTTPStatus: http.StatusUnauthorized, Message: "revoked token"},
	}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	m.recheckMu.Lock()
	m.recheckInFlight[auth.ID] = struct{}{}
	m.recheckMu.Unlock()

	m.runAuthRecheck(context.Background(), auth.ID)

	m.mu.RLock()
	updated := m.auths[auth.ID].Clone()
	m.mu.RUnlock()

	if updated.Status != StatusActive {
		t.Fatalf("Status = %q, want %q", updated.Status, StatusActive)
	}
	if updated.StatusMessage != "" {
		t.Fatalf("StatusMessage = %q, want empty", updated.StatusMessage)
	}
	if updated.LastError != nil {
		t.Fatalf("LastError = %#v, want nil", updated.LastError)
	}
	if updated.Unavailable {
		t.Fatalf("Unavailable = %v, want false", updated.Unavailable)
	}
}

func TestRunAuthRecheck_SyncsSchedulerAfterSuccess(t *testing.T) {
	ctx := context.Background()
	m := NewManager(nil, &RoundRobinSelector{}, nil)
	m.RegisterExecutor(authRecheckTestExecutor{
		httpResponse: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))},
	})

	auth := &Auth{
		ID:             "auth-1",
		Provider:       "codex",
		Status:         StatusError,
		StatusMessage:  "context canceled",
		Unavailable:    true,
		NextRetryAfter: time.Now().Add(10 * time.Minute),
		LastError:      &Error{Message: "context canceled"},
	}
	if _, err := m.Register(ctx, auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	got, errPick := m.scheduler.pickSingle(ctx, "codex", "", cliproxyexecutor.Options{}, nil)
	var authErr *Error
	if !errors.As(errPick, &authErr) || authErr == nil {
		t.Fatalf("pickSingle() before recheck error = %v, want auth_unavailable", errPick)
	}
	if authErr.Code != "auth_unavailable" {
		t.Fatalf("pickSingle() before recheck code = %q, want %q", authErr.Code, "auth_unavailable")
	}
	if got != nil {
		t.Fatalf("pickSingle() before recheck auth = %v, want nil", got)
	}

	m.recheckMu.Lock()
	m.recheckInFlight[auth.ID] = struct{}{}
	m.recheckMu.Unlock()

	m.runAuthRecheck(ctx, auth.ID)

	got, errPick = m.scheduler.pickSingle(ctx, "codex", "", cliproxyexecutor.Options{}, nil)
	if errPick != nil {
		t.Fatalf("pickSingle() after recheck error = %v", errPick)
	}
	if got == nil || got.ID != auth.ID {
		t.Fatalf("pickSingle() after recheck auth = %v, want %q", got, auth.ID)
	}
}

func TestRunAuthRecheck_PermanentFailureBecomesDeactivated(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.RegisterExecutor(authRecheckTestExecutor{
		httpErr: &Error{HTTPStatus: http.StatusUnauthorized, Message: "revoked token"},
	})

	auth := &Auth{ID: "auth-1", Provider: "codex", Status: StatusError, StatusMessage: "temporary failure", Unavailable: true}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	m.recheckMu.Lock()
	m.recheckInFlight[auth.ID] = struct{}{}
	m.recheckMu.Unlock()

	m.runAuthRecheck(context.Background(), auth.ID)

	m.mu.RLock()
	updated := m.auths[auth.ID].Clone()
	m.mu.RUnlock()

	if updated.Status != StatusDeactivated {
		t.Fatalf("Status = %q, want %q", updated.Status, StatusDeactivated)
	}
	if updated.StatusMessage != "revoked token" {
		t.Fatalf("StatusMessage = %q, want %q", updated.StatusMessage, "revoked token")
	}
}

func TestRunAuthRecheck_TransientFailureStaysError(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.RegisterExecutor(authRecheckTestExecutor{
		httpErr: errors.New("context canceled"),
	})

	auth := &Auth{ID: "auth-1", Provider: "codex", Status: StatusError, StatusMessage: "old", Unavailable: true}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	m.recheckMu.Lock()
	m.recheckInFlight[auth.ID] = struct{}{}
	m.recheckMu.Unlock()

	m.runAuthRecheck(context.Background(), auth.ID)

	m.mu.RLock()
	updated := m.auths[auth.ID].Clone()
	m.mu.RUnlock()

	if updated.Status != StatusError {
		t.Fatalf("Status = %q, want %q", updated.Status, StatusError)
	}
	if updated.LastError == nil || updated.LastError.Message != "context canceled" {
		t.Fatalf("LastError = %#v, want message %q", updated.LastError, "context canceled")
	}
}

func TestRunAuthRecheck_DoesNotClearDeactivatedWithoutSuccess(t *testing.T) {
	m := NewManager(nil, nil, nil)
	m.RegisterExecutor(authRecheckTestExecutor{
		httpErr: errors.New("context canceled"),
	})

	auth := &Auth{
		ID:            "auth-1",
		Provider:      "codex",
		Status:        StatusDeactivated,
		StatusMessage: "revoked token",
		Unavailable:   true,
		LastError:     &Error{HTTPStatus: http.StatusUnauthorized, Message: "revoked token"},
	}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	m.recheckMu.Lock()
	m.recheckInFlight[auth.ID] = struct{}{}
	m.recheckMu.Unlock()

	m.runAuthRecheck(context.Background(), auth.ID)

	m.mu.RLock()
	updated := m.auths[auth.ID].Clone()
	m.mu.RUnlock()

	if updated.Status != StatusDeactivated {
		t.Fatalf("Status = %q, want %q", updated.Status, StatusDeactivated)
	}
	if updated.StatusMessage != "revoked token" {
		t.Fatalf("StatusMessage = %q, want %q", updated.StatusMessage, "revoked token")
	}
	if updated.LastError == nil || updated.LastError.Message != "revoked token" {
		t.Fatalf("LastError = %#v, want message %q", updated.LastError, "revoked token")
	}
	if !updated.Unavailable {
		t.Fatalf("Unavailable = %v, want true", updated.Unavailable)
	}
}

func TestRunAuthRecheck_UsesProviderSpecificProbeURL(t *testing.T) {
	m := NewManager(nil, nil, nil)
	requestedURL := make(chan string, 1)
	m.RegisterExecutor(authRecheckTestExecutor{
		identifier: "claude",
		httpRequest: func(req *http.Request) {
			requestedURL <- req.URL.String()
		},
		httpResponse: &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(""))},
	})

	auth := &Auth{
		ID:            "auth-claude",
		Provider:      "claude",
		Status:        StatusError,
		StatusMessage: "temporary failure",
		Unavailable:   true,
	}
	if _, err := m.Register(context.Background(), auth); err != nil {
		t.Fatalf("register auth: %v", err)
	}

	m.recheckMu.Lock()
	m.recheckInFlight[auth.ID] = struct{}{}
	m.recheckMu.Unlock()

	m.runAuthRecheck(context.Background(), auth.ID)

	select {
	case got := <-requestedURL:
		want := "https://api.anthropic.com/v1/models"
		if got != want {
			t.Fatalf("recheck request url = %q, want %q", got, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("expected recheck request URL")
	}
}
