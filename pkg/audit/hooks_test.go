package audit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventBuilder_Basic(t *testing.T) {
	event := NewEvent("test.action").Build()

	assert.Equal(t, "test.action", event.Action)
	assert.True(t, event.Success) // Default is true.
}

func TestEventBuilder_WithActor(t *testing.T) {
	event := NewEvent("test.action").
		WithActor(ActorTypeUser, "user-123", "John Doe").
		Build()

	assert.Equal(t, ActorTypeUser, event.Actor.Type)
	assert.Equal(t, "user-123", event.Actor.ID)
	assert.Equal(t, "John Doe", event.Actor.Name)
}

func TestEventBuilder_WithSystemActor(t *testing.T) {
	event := NewEvent("test.action").
		WithSystemActor().
		Build()

	assert.Equal(t, ActorTypeSystem, event.Actor.Type)
	assert.Empty(t, event.Actor.ID)
	assert.Empty(t, event.Actor.Name)
}

func TestEventBuilder_WithResource(t *testing.T) {
	event := NewEvent("test.action").
		WithResource("session", "sess_123").
		Build()

	assert.Equal(t, "session", event.ResourceType)
	assert.Equal(t, "sess_123", event.ResourceID)
}

func TestEventBuilder_WithSession(t *testing.T) {
	event := NewEvent("test.action").
		WithSession("sess_456").
		Build()

	assert.Equal(t, "sess_456", event.SessionID)
}

func TestEventBuilder_WithTask(t *testing.T) {
	event := NewEvent("test.action").
		WithTask("task_789").
		Build()

	assert.Equal(t, "task_789", event.TaskID)
}

func TestEventBuilder_WithDetails(t *testing.T) {
	details := map[string]any{
		"reason": "test reason",
		"count":  42,
	}

	event := NewEvent("test.action").
		WithDetails(details).
		Build()

	assert.JSONEq(t, `{"reason":"test reason","count":42}`, string(event.Details))
}

func TestEventBuilder_WithDetails_Nil(t *testing.T) {
	event := NewEvent("test.action").
		WithDetails(nil).
		Build()

	assert.Empty(t, event.Details)
}

func TestEventBuilder_WithRawDetails(t *testing.T) {
	raw := []byte(`{"key":"value"}`)

	event := NewEvent("test.action").
		WithRawDetails(raw).
		Build()

	assert.Equal(t, raw, []byte(event.Details))
}

func TestEventBuilder_WithClientInfo(t *testing.T) {
	event := NewEvent("test.action").
		WithClientInfo("192.168.1.1", "Mozilla/5.0").
		Build()

	assert.Equal(t, "192.168.1.1", event.IPAddress)
	assert.Equal(t, "Mozilla/5.0", event.UserAgent)
}

func TestEventBuilder_WithTenant(t *testing.T) {
	event := NewEvent("test.action").
		WithTenant("tenant-acme").
		Build()

	assert.Equal(t, "tenant-acme", event.TenantID)
}

func TestEventBuilder_WithSuccess(t *testing.T) {
	event := NewEvent("test.action").
		WithSuccess(false).
		Build()

	assert.False(t, event.Success)
}

func TestEventBuilder_WithError(t *testing.T) {
	event := NewEvent("test.action").
		WithError("something went wrong").
		Build()

	assert.False(t, event.Success)
	assert.Equal(t, "something went wrong", event.ErrorMessage)
}

func TestEventBuilder_Chained(t *testing.T) {
	event := NewEvent(ActionSessionCreated).
		WithActor(ActorTypeAPIKey, "key-123", "CI Key").
		WithResource(ResourceTypeSession, "sess_abc").
		WithSession("sess_abc").
		WithTenant("tenant-1").
		WithClientInfo("10.0.0.1", "curl/7.64").
		WithDetails(map[string]string{"mode": "test"}).
		Build()

	assert.Equal(t, ActionSessionCreated, event.Action)
	assert.Equal(t, ActorTypeAPIKey, event.Actor.Type)
	assert.Equal(t, "key-123", event.Actor.ID)
	assert.Equal(t, "CI Key", event.Actor.Name)
	assert.Equal(t, ResourceTypeSession, event.ResourceType)
	assert.Equal(t, "sess_abc", event.ResourceID)
	assert.Equal(t, "sess_abc", event.SessionID)
	assert.Equal(t, "tenant-1", event.TenantID)
	assert.Equal(t, "10.0.0.1", event.IPAddress)
	assert.Equal(t, "curl/7.64", event.UserAgent)
	assert.True(t, event.Success)
}

func TestEventBuilder_Log(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	err := NewEvent("test.action").
		WithActor(ActorTypeUser, "user-1", "Test").
		WithResource("test", "test-1").
		Log(ctx, logger)

	require.NoError(t, err)
	assert.Equal(t, 1, store.Count())
}

func TestLogPermissionApproved(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	actor := Actor{Type: ActorTypeUser, ID: "user-1", Name: "Admin"}
	details := map[string]string{"tool": "bash", "command": "ls -la"}

	err := LogPermissionApproved(ctx, logger, actor, "perm_123", "sess_456", "task_789", details)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionPermissionApproved, e.Action)
	assert.Equal(t, ActorTypeUser, e.Actor.Type)
	assert.Equal(t, "user-1", e.Actor.ID)
	assert.Equal(t, ResourceTypePermissionRequest, e.ResourceType)
	assert.Equal(t, "perm_123", e.ResourceID)
	assert.Equal(t, "sess_456", e.SessionID)
	assert.Equal(t, "task_789", e.TaskID)
	assert.True(t, e.Success)
}

func TestLogPermissionDenied(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	actor := Actor{Type: ActorTypeAPIKey, ID: "key-1", Name: "CI Key"}
	details := map[string]string{"reason": "policy violation"}

	err := LogPermissionDenied(ctx, logger, actor, "perm_abc", "sess_def", "task_ghi", details)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionPermissionDenied, e.Action)
	assert.Equal(t, ResourceTypePermissionRequest, e.ResourceType)
}

func TestLogSessionCreated(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	actor := Actor{Type: ActorTypeUser, ID: "user-1"}
	details := map[string]string{"agent": "claude"}

	err := LogSessionCreated(ctx, logger, actor, "sess_new", "tenant-1", details)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionSessionCreated, e.Action)
	assert.Equal(t, ResourceTypeSession, e.ResourceType)
	assert.Equal(t, "sess_new", e.ResourceID)
	assert.Equal(t, "sess_new", e.SessionID)
	assert.Equal(t, "tenant-1", e.TenantID)
}

func TestLogSessionTerminated(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	actor := Actor{Type: ActorTypeSystem}
	details := map[string]string{"reason": "idle timeout"}

	err := LogSessionTerminated(ctx, logger, actor, "sess_old", "tenant-1", details)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionSessionTerminated, e.Action)
	assert.Equal(t, ResourceTypeSession, e.ResourceType)
}

func TestLogTaskCreated(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	actor := Actor{Type: ActorTypeAPIKey, ID: "key-1"}
	details := map[string]string{"prompt_length": "500"}

	err := LogTaskCreated(ctx, logger, actor, "task_new", "sess_123", "tenant-1", details)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionTaskCreated, e.Action)
	assert.Equal(t, ResourceTypeTask, e.ResourceType)
	assert.Equal(t, "task_new", e.ResourceID)
	assert.Equal(t, "sess_123", e.SessionID)
	assert.Equal(t, "task_new", e.TaskID)
}

func TestLogRunnerConnected(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	details := map[string]string{"hostname": "runner-01"}

	err := LogRunnerConnected(ctx, logger, "run_123", "runner-01", "tenant-1", details)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionRunnerConnected, e.Action)
	assert.Equal(t, ActorTypeRunner, e.Actor.Type)
	assert.Equal(t, "run_123", e.Actor.ID)
	assert.Equal(t, "runner-01", e.Actor.Name)
	assert.Equal(t, ResourceTypeRunner, e.ResourceType)
}

func TestLogAPIKeyCreated(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	actor := Actor{Type: ActorTypeUser, ID: "admin-1", Name: "Admin"}
	details := map[string]string{"scopes": "sessions:*", "name": "CI Pipeline"}

	err := LogAPIKeyCreated(ctx, logger, actor, "key_new", "tenant-1", details)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionAPIKeyCreated, e.Action)
	assert.Equal(t, ResourceTypeAPIKey, e.ResourceType)
	assert.Equal(t, "key_new", e.ResourceID)
}

func TestLogAPIKeyRevoked(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	actor := Actor{Type: ActorTypeUser, ID: "admin-1"}
	details := map[string]string{"reason": "compromised"}

	err := LogAPIKeyRevoked(ctx, logger, actor, "key_old", "tenant-1", details)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionAPIKeyRevoked, e.Action)
	assert.Equal(t, ResourceTypeAPIKey, e.ResourceType)
}

func TestLogConfigCreated(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	actor := Actor{Type: ActorTypeUser, ID: "admin-1"}
	details := map[string]string{"provider": "docker"}

	err := LogConfigCreated(ctx, logger, actor, ResourceTypeProviderConfig, "pcfg_123", "tenant-1", details)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionConfigCreated, e.Action)
	assert.Equal(t, ResourceTypeProviderConfig, e.ResourceType)
}

func TestLogConfigUpdated(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	actor := Actor{Type: ActorTypeAPIKey, ID: "key-1"}
	details := map[string]string{"changed": "model"}

	err := LogConfigUpdated(ctx, logger, actor, ResourceTypeAgentConfig, "acfg_123", "tenant-1", details)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionConfigUpdated, e.Action)
	assert.Equal(t, ResourceTypeAgentConfig, e.ResourceType)
}

func TestLogConfigDeleted(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	actor := Actor{Type: ActorTypeUser, ID: "admin-1"}

	err := LogConfigDeleted(ctx, logger, actor, ResourceTypeProviderConfig, "pcfg_old", "tenant-1", nil)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	e := result.Events[0]
	assert.Equal(t, ActionConfigDeleted, e.Action)
	assert.Equal(t, ResourceTypeProviderConfig, e.ResourceType)
}

func TestActionConstants(t *testing.T) {
	// Verify action constants are properly defined.
	assert.Equal(t, "permission.approved", ActionPermissionApproved)
	assert.Equal(t, "permission.denied", ActionPermissionDenied)
	assert.Equal(t, "permission.canceled", ActionPermissionCanceled)
	assert.Equal(t, "session.created", ActionSessionCreated)
	assert.Equal(t, "session.resumed", ActionSessionResumed)
	assert.Equal(t, "session.suspended", ActionSessionSuspended)
	assert.Equal(t, "session.terminated", ActionSessionTerminated)
	assert.Equal(t, "task.created", ActionTaskCreated)
	assert.Equal(t, "task.started", ActionTaskStarted)
	assert.Equal(t, "task.completed", ActionTaskCompleted)
	assert.Equal(t, "task.failed", ActionTaskFailed)
	assert.Equal(t, "task.canceled", ActionTaskCanceled)
	assert.Equal(t, "runner.connected", ActionRunnerConnected)
	assert.Equal(t, "runner.disconnected", ActionRunnerDisconnected)
	assert.Equal(t, "runner.assigned", ActionRunnerAssigned)
	assert.Equal(t, "runner.released", ActionRunnerReleased)
	assert.Equal(t, "config.created", ActionConfigCreated)
	assert.Equal(t, "config.updated", ActionConfigUpdated)
	assert.Equal(t, "config.deleted", ActionConfigDeleted)
	assert.Equal(t, "api_key.created", ActionAPIKeyCreated)
	assert.Equal(t, "api_key.revoked", ActionAPIKeyRevoked)
	assert.Equal(t, "api_key.used", ActionAPIKeyUsed)
}

func TestResourceTypeConstants(t *testing.T) {
	// Verify resource type constants are properly defined.
	assert.Equal(t, "permission_request", ResourceTypePermissionRequest)
	assert.Equal(t, "session", ResourceTypeSession)
	assert.Equal(t, "task", ResourceTypeTask)
	assert.Equal(t, "task_run", ResourceTypeTaskRun)
	assert.Equal(t, "runner", ResourceTypeRunner)
	assert.Equal(t, "provider_config", ResourceTypeProviderConfig)
	assert.Equal(t, "agent_config", ResourceTypeAgentConfig)
	assert.Equal(t, "api_key", ResourceTypeAPIKey)
	assert.Equal(t, "runner_token", ResourceTypeRunnerToken)
}
