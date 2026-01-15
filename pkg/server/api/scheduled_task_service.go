package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
)

// ScheduledTaskService defines operations for managing scheduled tasks.
type ScheduledTaskService interface {
	// Create creates a new scheduled task.
	Create(ctx context.Context, opts CreateScheduledTaskOptions) (*store.ScheduledTask, error)

	// Get retrieves a scheduled task by ID.
	Get(ctx context.Context, id string) (*store.ScheduledTask, error)

	// List returns scheduled tasks matching the filter options.
	List(ctx context.Context, opts ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error)

	// Update updates a scheduled task.
	Update(ctx context.Context, id string, opts UpdateScheduledTaskOptions) (*store.ScheduledTask, error)

	// Delete deletes a scheduled task.
	Delete(ctx context.Context, id string) error

	// Pause pauses a scheduled task.
	Pause(ctx context.Context, id string) error

	// Resume resumes a paused scheduled task.
	Resume(ctx context.Context, id string) error

	// Trigger manually triggers a scheduled task immediately.
	Trigger(ctx context.Context, id string) (*store.Task, error)
}

// CreateScheduledTaskOptions contains options for creating a scheduled task.
type CreateScheduledTaskOptions struct {
	SessionID              string            `json:"session_id"`
	Name                   string            `json:"name"`
	Description            string            `json:"description,omitempty"`
	CronExpression         string            `json:"cron_expression"`
	Timezone               string            `json:"timezone,omitempty"`
	PromptTemplate         string            `json:"prompt_template"`
	TimeoutSeconds         int               `json:"timeout_seconds,omitempty"`
	MaxRetries             int               `json:"max_retries,omitempty"`
	OnFailure              string            `json:"on_failure,omitempty"`
	MaxConsecutiveFailures *int              `json:"max_consecutive_failures,omitempty"`
	Labels                 map[string]string `json:"labels,omitempty"`
	Annotations            map[string]string `json:"annotations,omitempty"`
}

// ListScheduledTasksOptions contains options for listing scheduled tasks.
type ListScheduledTasksOptions struct {
	Limit     int      `json:"limit,omitempty"`
	Cursor    string   `json:"cursor,omitempty"`
	SessionID string   `json:"session_id,omitempty"`
	Status    []string `json:"status,omitempty"`
}

// UpdateScheduledTaskOptions contains options for updating a scheduled task.
type UpdateScheduledTaskOptions struct {
	Name                   *string           `json:"name,omitempty"`
	Description            *string           `json:"description,omitempty"`
	CronExpression         *string           `json:"cron_expression,omitempty"`
	Timezone               *string           `json:"timezone,omitempty"`
	PromptTemplate         *string           `json:"prompt_template,omitempty"`
	TimeoutSeconds         *int              `json:"timeout_seconds,omitempty"`
	MaxRetries             *int              `json:"max_retries,omitempty"`
	OnFailure              *string           `json:"on_failure,omitempty"`
	MaxConsecutiveFailures *int              `json:"max_consecutive_failures,omitempty"`
	Labels                 map[string]string `json:"labels,omitempty"`
	Annotations            map[string]string `json:"annotations,omitempty"`
}
