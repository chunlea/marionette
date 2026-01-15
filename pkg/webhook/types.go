// Package webhook provides webhook delivery functionality for external system integration.
package webhook

import (
	"encoding/json"
	"time"
)

// Event types for webhook subscriptions.
// Patterns support wildcards (e.g., "task.*" matches "task.created", "task.completed", etc.)
const (
	// Session events
	EventSessionCreated    = "session.created"
	EventSessionSuspended  = "session.suspended"
	EventSessionResumed    = "session.resumed"
	EventSessionTerminated = "session.terminated"

	// Task events
	EventTaskCreated   = "task.created"
	EventTaskStarted   = "task.started"
	EventTaskCompleted = "task.completed"
	EventTaskFailed    = "task.failed"
	EventTaskCanceled  = "task.canceled"

	// Runner events
	EventRunnerConnected    = "runner.connected"
	EventRunnerDisconnected = "runner.disconnected"
	EventRunnerAssigned     = "runner.assigned"
	EventRunnerReleased     = "runner.released"

	// Permission events
	EventPermissionRequested = "permission.requested"
	EventPermissionApproved  = "permission.approved"
	EventPermissionDenied    = "permission.denied"
	EventPermissionCanceled  = "permission.canceled"
)

// Payload is the webhook request payload sent to subscribers.
type Payload struct {
	Event     string          `json:"event"`
	Timestamp time.Time       `json:"timestamp"`
	Resource  ResourceInfo    `json:"resource"`
	Data      json.RawMessage `json:"data"`
}

// ResourceInfo contains metadata about the resource that triggered the event.
type ResourceInfo struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// SessionEventData contains event-specific data for session events.
type SessionEventData struct {
	WorkspaceID   string  `json:"workspace_id"`
	Agent         string  `json:"agent"`
	Status        string  `json:"status"`
	RunnerID      *string `json:"runner_id,omitempty"`
	LifecycleMode string  `json:"lifecycle_mode,omitempty"`
}

// TaskEventData contains event-specific data for task events.
type TaskEventData struct {
	SessionID       string  `json:"session_id"`
	Prompt          string  `json:"prompt,omitempty"` // Truncated for privacy
	Status          string  `json:"status"`
	DurationSeconds *int64  `json:"duration_seconds,omitempty"`
	ExitCode        *int    `json:"exit_code,omitempty"`
	Error           *string `json:"error,omitempty"`
	TokensInput     *int    `json:"tokens_input,omitempty"`
	TokensOutput    *int    `json:"tokens_output,omitempty"`
}

// RunnerEventData contains event-specific data for runner events.
type RunnerEventData struct {
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	PoolName    *string `json:"pool_name,omitempty"`
	SessionID   *string `json:"session_id,omitempty"`
	Provider    *string `json:"provider,omitempty"`
	SandboxMode string  `json:"sandbox_mode,omitempty"`
}

// PermissionEventData contains event-specific data for permission events.
type PermissionEventData struct {
	SessionID      string  `json:"session_id"`
	TaskID         string  `json:"task_id"`
	Tool           string  `json:"tool"`
	Action         string  `json:"action"`
	RiskLevel      string  `json:"risk_level"`
	Status         string  `json:"status"`
	RespondedBy    *string `json:"responded_by,omitempty"`
	ResponseReason *string `json:"response_reason,omitempty"`
}

// DeliveryResult contains the result of a webhook delivery attempt.
type DeliveryResult struct {
	Success    bool
	StatusCode int
	Error      error
	Duration   time.Duration
}

// Config contains default configuration for webhook delivery.
type Config struct {
	// DefaultMaxRetries is the default number of retry attempts for failed deliveries.
	DefaultMaxRetries int

	// DefaultRetryDelaySeconds is the default delay between retry attempts in seconds.
	DefaultRetryDelaySeconds int

	// DefaultTimeoutSeconds is the default HTTP timeout for webhook requests.
	DefaultTimeoutSeconds int

	// MaxPayloadSize is the maximum payload size in bytes (default: 10MB).
	MaxPayloadSize int

	// UserAgent is the User-Agent header sent with webhook requests.
	UserAgent string

	// WorkerCount is the number of concurrent delivery workers.
	WorkerCount int

	// BatchSize is the number of events to process in each batch.
	BatchSize int
}

// DefaultConfig returns the default webhook configuration.
func DefaultConfig() Config {
	return Config{
		DefaultMaxRetries:        3,
		DefaultRetryDelaySeconds: 60,
		DefaultTimeoutSeconds:    30,
		MaxPayloadSize:           10 * 1024 * 1024, // 10MB
		UserAgent:                "Marionette-Webhook/1.0",
		WorkerCount:              4,
		BatchSize:                100,
	}
}
