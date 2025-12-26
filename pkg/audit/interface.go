// Package audit provides audit logging capabilities for tracking sensitive actions.
package audit

import (
	"context"
	"encoding/json"
	"time"
)

// ActorType identifies who performed an action.
type ActorType string

const (
	// ActorTypeUser represents a human user.
	ActorTypeUser ActorType = "user"
	// ActorTypeAPIKey represents an API key.
	ActorTypeAPIKey ActorType = "api_key"
	// ActorTypeSystem represents the system itself.
	ActorTypeSystem ActorType = "system"
	// ActorTypeRunner represents a runner.
	ActorTypeRunner ActorType = "runner"
)

// String returns the string representation of ActorType.
func (t ActorType) String() string {
	return string(t)
}

// IsValid checks if the actor type is valid.
func (t ActorType) IsValid() bool {
	switch t {
	case ActorTypeUser, ActorTypeAPIKey, ActorTypeSystem, ActorTypeRunner:
		return true
	}
	return false
}

// Actor represents who performed an action.
type Actor struct {
	Type ActorType `json:"type"`
	ID   string    `json:"id,omitempty"`
	Name string    `json:"name,omitempty"`
}

// Event represents an audit log event.
type Event struct {
	// Actor identifies who performed the action.
	Actor Actor `json:"actor"`

	// Action is the action performed (e.g., "permission.approved", "session.created").
	Action string `json:"action"`

	// ResourceType is the type of resource affected (e.g., "permission_request", "session").
	ResourceType string `json:"resource_type"`

	// ResourceID is the ID of the affected resource.
	ResourceID string `json:"resource_id"`

	// SessionID is the associated session (if applicable).
	SessionID string `json:"session_id,omitempty"`

	// TaskID is the associated task (if applicable).
	TaskID string `json:"task_id,omitempty"`

	// Details contains action-specific details.
	Details json.RawMessage `json:"details,omitempty"`

	// IPAddress is the client IP address.
	IPAddress string `json:"ip_address,omitempty"`

	// UserAgent is the client user agent.
	UserAgent string `json:"user_agent,omitempty"`

	// Success indicates whether the action succeeded.
	Success bool `json:"success"`

	// ErrorMessage contains the error message if the action failed.
	ErrorMessage string `json:"error_message,omitempty"`

	// TenantID is the tenant ID for multi-tenant deployments.
	TenantID string `json:"tenant_id,omitempty"`

	// Timestamp is when the event occurred (set automatically if not provided).
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// Filter defines criteria for querying audit logs.
type Filter struct {
	// Actor filters
	ActorType ActorType `json:"actor_type,omitempty"`
	ActorID   string    `json:"actor_id,omitempty"`

	// Action filters
	Action       string `json:"action,omitempty"`
	ActionPrefix string `json:"action_prefix,omitempty"` // e.g., "permission." matches "permission.approved"

	// Resource filters
	ResourceType string `json:"resource_type,omitempty"`
	ResourceID   string `json:"resource_id,omitempty"`

	// Context filters
	SessionID string `json:"session_id,omitempty"`
	TaskID    string `json:"task_id,omitempty"`
	TenantID  string `json:"tenant_id,omitempty"`

	// Result filters
	SuccessOnly bool `json:"success_only,omitempty"`
	FailureOnly bool `json:"failure_only,omitempty"`

	// Time range
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`

	// Pagination
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// QueryResult contains the results of an audit log query.
type QueryResult struct {
	Events     []StoredEvent `json:"events"`
	TotalCount int           `json:"total_count"`
	HasMore    bool          `json:"has_more"`
}

// StoredEvent is an Event with an ID and guaranteed timestamp.
type StoredEvent struct {
	ID string `json:"id"`
	Event
}

// Logger defines the interface for audit logging.
type Logger interface {
	// Log records an audit event.
	// The event ID and timestamp are set automatically if not provided.
	Log(ctx context.Context, event Event) error

	// Query retrieves audit events matching the filter.
	Query(ctx context.Context, filter Filter) (*QueryResult, error)
}

// Store defines the storage interface for audit logs.
// This is implemented by the postgres store.
type Store interface {
	// CreateActionLog stores a new action log entry.
	CreateActionLog(ctx context.Context, log *StoredEvent) error

	// ListActionLogs retrieves action logs matching the filter.
	ListActionLogs(ctx context.Context, filter Filter) (*QueryResult, error)
}
