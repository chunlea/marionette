package core

import (
	"context"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/webhook"
	"go.uber.org/zap"
)

// WebhookDispatcher is the interface for dispatching webhook events.
type WebhookDispatcher interface {
	Dispatch(ctx context.Context, eventType string, resource webhook.ResourceInfo, data any, tenantID *string) error
}

// WebhookIntegration provides methods for dispatching webhook events from managers.
type WebhookIntegration struct {
	dispatcher WebhookDispatcher
	logger     *zap.Logger
}

// NewWebhookIntegration creates a new WebhookIntegration.
func NewWebhookIntegration(dispatcher WebhookDispatcher, logger *zap.Logger) *WebhookIntegration {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &WebhookIntegration{
		dispatcher: dispatcher,
		logger:     logger,
	}
}

// DispatchSessionEvent dispatches a session-related webhook event.
func (w *WebhookIntegration) DispatchSessionEvent(ctx context.Context, eventType string, session *store.Session) {
	if w.dispatcher == nil {
		return
	}

	resource := webhook.ResourceInfo{
		ID:   session.ID,
		Type: "session",
	}
	if session.Labels != nil {
		// Labels is json.RawMessage, decode if needed
		// For now, skip labels to keep it simple
	}

	data := webhook.SessionEventData{
		WorkspaceID:   session.WorkspaceID,
		Agent:         session.Agent,
		Status:        session.Status,
		LifecycleMode: session.LifecycleMode,
	}
	if session.RunnerID != nil {
		data.RunnerID = session.RunnerID
	}

	if err := w.dispatcher.Dispatch(ctx, eventType, resource, data, session.TenantID); err != nil {
		w.logger.Warn("failed to dispatch session webhook",
			zap.String("event_type", eventType),
			zap.String("session_id", session.ID),
			zap.Error(err),
		)
	}
}

// DispatchTaskEvent dispatches a task-related webhook event.
func (w *WebhookIntegration) DispatchTaskEvent(ctx context.Context, eventType string, task *store.Task, run *store.TaskRun) {
	if w.dispatcher == nil {
		return
	}

	resource := webhook.ResourceInfo{
		ID:   task.ID,
		Type: "task",
	}

	data := webhook.TaskEventData{
		SessionID: task.SessionID,
		Status:    task.Status,
	}

	// Truncate prompt for privacy
	prompt := task.Prompt
	if len(prompt) > 100 {
		prompt = prompt[:100] + "..."
	}
	data.Prompt = prompt

	// Include run details if available
	if run != nil {
		if run.StartedAt != nil && run.EndedAt != nil {
			duration := int64(run.EndedAt.Sub(*run.StartedAt).Seconds())
			data.DurationSeconds = &duration
		}
		data.ExitCode = run.ExitCode
		data.Error = run.Error
		data.TokensInput = run.TokensInput
		data.TokensOutput = run.TokensOutput
	}

	if err := w.dispatcher.Dispatch(ctx, eventType, resource, data, task.TenantID); err != nil {
		w.logger.Warn("failed to dispatch task webhook",
			zap.String("event_type", eventType),
			zap.String("task_id", task.ID),
			zap.Error(err),
		)
	}
}

// DispatchRunnerEvent dispatches a runner-related webhook event.
func (w *WebhookIntegration) DispatchRunnerEvent(ctx context.Context, eventType string, runner *store.Runner, sessionID *string) {
	if w.dispatcher == nil {
		return
	}

	resource := webhook.ResourceInfo{
		ID:   runner.ID,
		Type: "runner",
	}

	data := webhook.RunnerEventData{
		Name:        runner.Name,
		Status:      runner.Status,
		SandboxMode: runner.SandboxMode,
	}
	if runner.PoolName != nil {
		data.PoolName = runner.PoolName
	}
	if sessionID != nil {
		data.SessionID = sessionID
	}

	if err := w.dispatcher.Dispatch(ctx, eventType, resource, data, runner.TenantID); err != nil {
		w.logger.Warn("failed to dispatch runner webhook",
			zap.String("event_type", eventType),
			zap.String("runner_id", runner.ID),
			zap.Error(err),
		)
	}
}

// DispatchPermissionEvent dispatches a permission-related webhook event.
func (w *WebhookIntegration) DispatchPermissionEvent(ctx context.Context, eventType string, perm *store.PermissionRequest) {
	if w.dispatcher == nil {
		return
	}

	resource := webhook.ResourceInfo{
		ID:   perm.ID,
		Type: "permission_request",
	}

	data := webhook.PermissionEventData{
		SessionID: perm.SessionID,
		TaskID:    perm.TaskID,
		Tool:      perm.Tool,
		Action:    perm.Action,
		RiskLevel: perm.RiskLevel,
		Status:    perm.Status,
	}
	if perm.RespondedBy != nil {
		data.RespondedBy = perm.RespondedBy
	}
	if perm.ResponseReason != nil {
		data.ResponseReason = perm.ResponseReason
	}

	if err := w.dispatcher.Dispatch(ctx, eventType, resource, data, perm.TenantID); err != nil {
		w.logger.Warn("failed to dispatch permission webhook",
			zap.String("event_type", eventType),
			zap.String("perm_id", perm.ID),
			zap.Error(err),
		)
	}
}
