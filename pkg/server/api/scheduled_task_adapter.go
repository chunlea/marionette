package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
)

// ScheduledTaskAdapter adapts core.ScheduledTaskServiceInterface to the API layer.
type ScheduledTaskAdapter struct {
	svc core.ScheduledTaskServiceInterface
}

// NewScheduledTaskAdapter creates a new ScheduledTaskAdapter.
func NewScheduledTaskAdapter(svc core.ScheduledTaskServiceInterface) *ScheduledTaskAdapter {
	return &ScheduledTaskAdapter{svc: svc}
}

// Create creates a new scheduled task.
func (a *ScheduledTaskAdapter) Create(ctx context.Context, opts CreateScheduledTaskOptions) (*store.ScheduledTask, error) {
	return a.svc.Create(ctx, core.CreateScheduledTaskOptions{
		SessionID:              opts.SessionID,
		Name:                   opts.Name,
		Description:            opts.Description,
		CronExpression:         opts.CronExpression,
		Timezone:               opts.Timezone,
		PromptTemplate:         opts.PromptTemplate,
		TimeoutSeconds:         opts.TimeoutSeconds,
		MaxRetries:             opts.MaxRetries,
		OnFailure:              opts.OnFailure,
		MaxConsecutiveFailures: opts.MaxConsecutiveFailures,
		Labels:                 opts.Labels,
		Annotations:            opts.Annotations,
	})
}

// Get retrieves a scheduled task by ID.
func (a *ScheduledTaskAdapter) Get(ctx context.Context, id string) (*store.ScheduledTask, error) {
	return a.svc.Get(ctx, id)
}

// List returns scheduled tasks matching the filter options.
func (a *ScheduledTaskAdapter) List(ctx context.Context, opts ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	coreOpts := store.ListScheduledTasksOptions{
		BaseListOptions: store.BaseListOptions{
			Limit: opts.Limit,
		},
		Status: opts.Status,
	}
	if opts.SessionID != "" {
		coreOpts.SessionID = &opts.SessionID
	}
	return a.svc.List(ctx, coreOpts)
}

// Update updates a scheduled task.
func (a *ScheduledTaskAdapter) Update(ctx context.Context, id string, opts UpdateScheduledTaskOptions) (*store.ScheduledTask, error) {
	return a.svc.Update(ctx, id, core.UpdateScheduledTaskOptions{
		Name:                   opts.Name,
		Description:            opts.Description,
		CronExpression:         opts.CronExpression,
		Timezone:               opts.Timezone,
		PromptTemplate:         opts.PromptTemplate,
		TimeoutSeconds:         opts.TimeoutSeconds,
		MaxRetries:             opts.MaxRetries,
		OnFailure:              opts.OnFailure,
		MaxConsecutiveFailures: opts.MaxConsecutiveFailures,
		Labels:                 opts.Labels,
		Annotations:            opts.Annotations,
	})
}

// Delete deletes a scheduled task.
func (a *ScheduledTaskAdapter) Delete(ctx context.Context, id string) error {
	return a.svc.Delete(ctx, id)
}

// Pause pauses a scheduled task.
func (a *ScheduledTaskAdapter) Pause(ctx context.Context, id string) error {
	return a.svc.Pause(ctx, id)
}

// Resume resumes a paused scheduled task.
func (a *ScheduledTaskAdapter) Resume(ctx context.Context, id string) error {
	return a.svc.Resume(ctx, id)
}

// Trigger manually triggers a scheduled task immediately.
func (a *ScheduledTaskAdapter) Trigger(ctx context.Context, id string) (*store.Task, error) {
	return a.svc.Trigger(ctx, id)
}

// Ensure ScheduledTaskAdapter implements ScheduledTaskService.
var _ ScheduledTaskService = (*ScheduledTaskAdapter)(nil)
