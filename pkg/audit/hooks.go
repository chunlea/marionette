package audit

import (
	"context"
	"encoding/json"

	"github.com/chunlea/marionette/pkg/store"
)

// Common action constants for audit logging.
const (
	// Permission actions.
	ActionPermissionApproved = "permission.approved"
	ActionPermissionDenied   = "permission.denied"
	ActionPermissionCanceled = "permission.canceled"

	// Session actions.
	ActionSessionCreated    = "session.created"
	ActionSessionResumed    = "session.resumed"
	ActionSessionSuspended  = "session.suspended"
	ActionSessionTerminated = "session.terminated"

	// Task actions.
	ActionTaskCreated   = "task.created"
	ActionTaskStarted   = "task.started"
	ActionTaskCompleted = "task.completed"
	ActionTaskFailed    = "task.failed"
	ActionTaskCanceled  = "task.canceled"

	// Runner actions.
	ActionRunnerConnected    = "runner.connected"
	ActionRunnerDisconnected = "runner.disconnected"
	ActionRunnerAssigned     = "runner.assigned"
	ActionRunnerReleased     = "runner.released"

	// Config actions.
	ActionConfigCreated = "config.created"
	ActionConfigUpdated = "config.updated"
	ActionConfigDeleted = "config.deleted"

	// API key actions.
	ActionAPIKeyCreated = "api_key.created"
	ActionAPIKeyRevoked = "api_key.revoked"
	ActionAPIKeyUsed    = "api_key.used"
)

// Common resource type constants.
const (
	ResourceTypePermissionRequest = "permission_request"
	ResourceTypeSession           = "session"
	ResourceTypeTask              = "task"
	ResourceTypeTaskRun           = "task_run"
	ResourceTypeRunner            = "runner"
	ResourceTypeProviderConfig    = "provider_config"
	ResourceTypeAgentConfig       = "agent_config"
	ResourceTypeAPIKey            = "api_key"
	ResourceTypeRunnerToken       = "runner_token"
)

// EventBuilder helps construct audit events with a fluent API.
type EventBuilder struct {
	event Event
}

// NewEvent creates a new event builder with the given action.
func NewEvent(action string) *EventBuilder {
	return &EventBuilder{
		event: Event{
			Action:  action,
			Success: true, // Default to success.
		},
	}
}

// WithActor sets the actor for the event.
func (b *EventBuilder) WithActor(actorType ActorType, actorID, actorName string) *EventBuilder {
	b.event.Actor = Actor{
		Type: actorType,
		ID:   actorID,
		Name: actorName,
	}
	return b
}

// WithSystemActor sets the actor as the system.
func (b *EventBuilder) WithSystemActor() *EventBuilder {
	b.event.Actor = Actor{Type: ActorTypeSystem}
	return b
}

// WithResource sets the resource type and ID.
func (b *EventBuilder) WithResource(resourceType, resourceID string) *EventBuilder {
	b.event.ResourceType = resourceType
	b.event.ResourceID = resourceID
	return b
}

// WithSession sets the session ID.
func (b *EventBuilder) WithSession(sessionID string) *EventBuilder {
	b.event.SessionID = sessionID
	return b
}

// WithTask sets the task ID.
func (b *EventBuilder) WithTask(taskID string) *EventBuilder {
	b.event.TaskID = taskID
	return b
}

// WithDetails sets the event details as JSON.
func (b *EventBuilder) WithDetails(details any) *EventBuilder {
	if details != nil {
		data, err := json.Marshal(details)
		if err == nil {
			b.event.Details = data
		}
	}
	return b
}

// WithRawDetails sets the event details from raw JSON.
func (b *EventBuilder) WithRawDetails(details json.RawMessage) *EventBuilder {
	b.event.Details = details
	return b
}

// WithClientInfo sets the client IP and user agent.
func (b *EventBuilder) WithClientInfo(ipAddress, userAgent string) *EventBuilder {
	b.event.IPAddress = ipAddress
	b.event.UserAgent = userAgent
	return b
}

// WithTenant sets the tenant ID.
func (b *EventBuilder) WithTenant(tenantID string) *EventBuilder {
	b.event.TenantID = tenantID
	return b
}

// WithSuccess sets the success status.
func (b *EventBuilder) WithSuccess(success bool) *EventBuilder {
	b.event.Success = success
	return b
}

// WithError sets the event as failed with an error message.
func (b *EventBuilder) WithError(errMsg string) *EventBuilder {
	b.event.Success = false
	b.event.ErrorMessage = errMsg
	return b
}

// Build returns the constructed event.
func (b *EventBuilder) Build() Event {
	return b.event
}

// Log logs the event using the provided logger.
func (b *EventBuilder) Log(ctx context.Context, logger Logger) error {
	// Stamp the tenant from the request unless the caller named one. An audit
	// trail that cannot say which tenant an action belonged to is not much of
	// an audit trail, and every call site would otherwise have to remember.
	if b.event.TenantID == "" {
		if tenantID, ok := store.TenantFromContext(ctx); ok {
			b.event.TenantID = tenantID
		}
	}
	return logger.Log(ctx, b.event)
}

// LogPermissionApproved logs a permission approval event.
func LogPermissionApproved(ctx context.Context, logger Logger, actor Actor, permissionID, sessionID, taskID string, details any) error {
	return NewEvent(ActionPermissionApproved).
		WithActor(actor.Type, actor.ID, actor.Name).
		WithResource(ResourceTypePermissionRequest, permissionID).
		WithSession(sessionID).
		WithTask(taskID).
		WithDetails(details).
		Log(ctx, logger)
}

// LogPermissionDenied logs a permission denial event.
func LogPermissionDenied(ctx context.Context, logger Logger, actor Actor, permissionID, sessionID, taskID string, details any) error {
	return NewEvent(ActionPermissionDenied).
		WithActor(actor.Type, actor.ID, actor.Name).
		WithResource(ResourceTypePermissionRequest, permissionID).
		WithSession(sessionID).
		WithTask(taskID).
		WithDetails(details).
		Log(ctx, logger)
}

// LogSessionCreated logs a session creation event.
func LogSessionCreated(ctx context.Context, logger Logger, actor Actor, sessionID, tenantID string, details any) error {
	return NewEvent(ActionSessionCreated).
		WithActor(actor.Type, actor.ID, actor.Name).
		WithResource(ResourceTypeSession, sessionID).
		WithSession(sessionID).
		WithTenant(tenantID).
		WithDetails(details).
		Log(ctx, logger)
}

// LogSessionTerminated logs a session termination event.
func LogSessionTerminated(ctx context.Context, logger Logger, actor Actor, sessionID, tenantID string, details any) error {
	return NewEvent(ActionSessionTerminated).
		WithActor(actor.Type, actor.ID, actor.Name).
		WithResource(ResourceTypeSession, sessionID).
		WithSession(sessionID).
		WithTenant(tenantID).
		WithDetails(details).
		Log(ctx, logger)
}

// LogTaskCreated logs a task creation event.
func LogTaskCreated(ctx context.Context, logger Logger, actor Actor, taskID, sessionID, tenantID string, details any) error {
	return NewEvent(ActionTaskCreated).
		WithActor(actor.Type, actor.ID, actor.Name).
		WithResource(ResourceTypeTask, taskID).
		WithSession(sessionID).
		WithTask(taskID).
		WithTenant(tenantID).
		WithDetails(details).
		Log(ctx, logger)
}

// LogRunnerConnected logs a runner connection event.
func LogRunnerConnected(ctx context.Context, logger Logger, runnerID, runnerName, tenantID string, details any) error {
	return NewEvent(ActionRunnerConnected).
		WithActor(ActorTypeRunner, runnerID, runnerName).
		WithResource(ResourceTypeRunner, runnerID).
		WithTenant(tenantID).
		WithDetails(details).
		Log(ctx, logger)
}

// LogAPIKeyCreated logs an API key creation event.
func LogAPIKeyCreated(ctx context.Context, logger Logger, actor Actor, keyID, tenantID string, details any) error {
	return NewEvent(ActionAPIKeyCreated).
		WithActor(actor.Type, actor.ID, actor.Name).
		WithResource(ResourceTypeAPIKey, keyID).
		WithTenant(tenantID).
		WithDetails(details).
		Log(ctx, logger)
}

// LogAPIKeyRevoked logs an API key revocation event.
func LogAPIKeyRevoked(ctx context.Context, logger Logger, actor Actor, keyID, tenantID string, details any) error {
	return NewEvent(ActionAPIKeyRevoked).
		WithActor(actor.Type, actor.ID, actor.Name).
		WithResource(ResourceTypeAPIKey, keyID).
		WithTenant(tenantID).
		WithDetails(details).
		Log(ctx, logger)
}

// LogConfigCreated logs a configuration creation event.
func LogConfigCreated(ctx context.Context, logger Logger, actor Actor, resourceType, resourceID, tenantID string, details any) error {
	return NewEvent(ActionConfigCreated).
		WithActor(actor.Type, actor.ID, actor.Name).
		WithResource(resourceType, resourceID).
		WithTenant(tenantID).
		WithDetails(details).
		Log(ctx, logger)
}

// LogConfigUpdated logs a configuration update event.
func LogConfigUpdated(ctx context.Context, logger Logger, actor Actor, resourceType, resourceID, tenantID string, details any) error {
	return NewEvent(ActionConfigUpdated).
		WithActor(actor.Type, actor.ID, actor.Name).
		WithResource(resourceType, resourceID).
		WithTenant(tenantID).
		WithDetails(details).
		Log(ctx, logger)
}

// LogConfigDeleted logs a configuration deletion event.
func LogConfigDeleted(ctx context.Context, logger Logger, actor Actor, resourceType, resourceID, tenantID string, details any) error {
	return NewEvent(ActionConfigDeleted).
		WithActor(actor.Type, actor.ID, actor.Name).
		WithResource(resourceType, resourceID).
		WithTenant(tenantID).
		WithDetails(details).
		Log(ctx, logger)
}
