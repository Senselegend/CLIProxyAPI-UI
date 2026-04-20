package activity

import "time"

type TokenStats struct {
	InputTokens     int64 `json:"input_tokens"`
	OutputTokens    int64 `json:"output_tokens"`
	ReasoningTokens int64 `json:"reasoning_tokens"`
	CachedTokens    int64 `json:"cached_tokens"`
	TotalTokens     int64 `json:"total_tokens"`
}

type Entry struct {
	ID                  string     `json:"id"`
	Account             string     `json:"account"`
	Provider            string     `json:"provider"`
	Model               string     `json:"model"`
	Method              string     `json:"method"`
	Path                string     `json:"path"`
	Transport           string     `json:"transport"`
	DownstreamTransport string     `json:"downstream_transport"`
	UpstreamTransport   string     `json:"upstream_transport"`
	HTTPStatus          int        `json:"http_status"`
	Status              string     `json:"status"`
	LatencyMs           int64      `json:"latency_ms"`
	RequestedAt         time.Time  `json:"requested_at"`
	FinishedAt          *time.Time `json:"finished_at,omitempty"`
	Tokens              TokenStats `json:"tokens"`
	Message             string     `json:"message"`
}

type SnapshotOptions struct {
	Limit int
}

type StartEvent struct {
	ID                  string
	Method              string
	Path                string
	DownstreamTransport string
	StartedAt           time.Time
}

type FinishEvent struct {
	ID         string
	HTTPStatus int
	Latency    time.Duration
	FinishedAt time.Time
}
