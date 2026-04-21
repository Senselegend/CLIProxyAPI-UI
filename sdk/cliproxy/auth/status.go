package auth

// Status represents the lifecycle state of an Auth entry.
type Status string

const (
	// StatusUnknown means the auth state could not be determined.
	StatusUnknown Status = "unknown"
	// StatusActive indicates the auth is valid and ready for execution.
	StatusActive Status = "active"
	// StatusPending indicates the auth is waiting for an external action, such as MFA.
	StatusPending Status = "pending"
	// StatusRefreshing indicates the auth is undergoing a refresh flow.
	StatusRefreshing Status = "refreshing"
	// StatusError indicates the auth is temporarily unavailable due to errors.
	StatusError Status = "error"
	// StatusPaused indicates the auth is temporarily paused but may resume later.
	StatusPaused Status = "paused"
	// StatusRateLimited indicates the auth is temporarily unavailable due to rate limits.
	StatusRateLimited Status = "rate_limited"
	// StatusDisabled marks the auth as intentionally disabled.
	StatusDisabled Status = "disabled"
	// StatusDeactivated marks the auth as permanently unusable after terminal failures.
	StatusDeactivated Status = "deactivated"
)
