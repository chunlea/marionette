package audit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLogger(t *testing.T) {
	store := NewMemoryStore()
	logger := NewLogger(store)
	assert.NotNil(t, logger)
}

func TestDefaultLogger_Log(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	event := Event{
		Actor: Actor{
			Type: ActorTypeUser,
			ID:   "user-123",
			Name: "Test User",
		},
		Action:       "session.created",
		ResourceType: "session",
		ResourceID:   "sess_456",
		Success:      true,
	}

	err := logger.Log(ctx, event)
	require.NoError(t, err)

	// Verify event was stored.
	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	stored := result.Events[0]
	assert.NotEmpty(t, stored.ID)
	assert.True(t, stored.ID[:5] == "alog_")
	assert.False(t, stored.Timestamp.IsZero())
	assert.Equal(t, ActorTypeUser, stored.Actor.Type)
	assert.Equal(t, "user-123", stored.Actor.ID)
	assert.Equal(t, "session.created", stored.Action)
	assert.Equal(t, "session", stored.ResourceType)
	assert.Equal(t, "sess_456", stored.ResourceID)
	assert.True(t, stored.Success)
}

func TestDefaultLogger_Log_PreservesTimestamp(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	customTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	event := Event{
		Actor:        Actor{Type: ActorTypeSystem},
		Action:       "system.startup",
		ResourceType: "system",
		ResourceID:   "main",
		Success:      true,
		Timestamp:    customTime,
	}

	err := logger.Log(ctx, event)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	// Timestamp should be preserved.
	assert.Equal(t, customTime, result.Events[0].Timestamp)
}

func TestDefaultLogger_Log_SetsTimestamp(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	before := time.Now().UTC().Add(-time.Second)
	event := Event{
		Actor:        Actor{Type: ActorTypeSystem},
		Action:       "system.startup",
		ResourceType: "system",
		ResourceID:   "main",
		Success:      true,
		// No timestamp set.
	}

	err := logger.Log(ctx, event)
	require.NoError(t, err)

	after := time.Now().UTC().Add(time.Second)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	// Timestamp should have been set automatically.
	ts := result.Events[0].Timestamp
	assert.True(t, ts.After(before) || ts.Equal(before))
	assert.True(t, ts.Before(after) || ts.Equal(after))
}

func TestDefaultLogger_Log_WithAllFields(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	event := Event{
		Actor: Actor{
			Type: ActorTypeAPIKey,
			ID:   "key-789",
			Name: "CI Pipeline Key",
		},
		Action:       "permission.denied",
		ResourceType: "permission_request",
		ResourceID:   "perm_abc",
		SessionID:    "sess_123",
		TaskID:       "task_456",
		Details:      []byte(`{"reason":"policy violation"}`),
		IPAddress:    "10.0.0.1",
		UserAgent:    "curl/7.64.1",
		Success:      false,
		ErrorMessage: "Access denied by policy",
		TenantID:     "tenant-acme",
	}

	err := logger.Log(ctx, event)
	require.NoError(t, err)

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)

	stored := result.Events[0]
	assert.Equal(t, ActorTypeAPIKey, stored.Actor.Type)
	assert.Equal(t, "key-789", stored.Actor.ID)
	assert.Equal(t, "CI Pipeline Key", stored.Actor.Name)
	assert.Equal(t, "permission.denied", stored.Action)
	assert.Equal(t, "permission_request", stored.ResourceType)
	assert.Equal(t, "perm_abc", stored.ResourceID)
	assert.Equal(t, "sess_123", stored.SessionID)
	assert.Equal(t, "task_456", stored.TaskID)
	assert.JSONEq(t, `{"reason":"policy violation"}`, string(stored.Details))
	assert.Equal(t, "10.0.0.1", stored.IPAddress)
	assert.Equal(t, "curl/7.64.1", stored.UserAgent)
	assert.False(t, stored.Success)
	assert.Equal(t, "Access denied by policy", stored.ErrorMessage)
	assert.Equal(t, "tenant-acme", stored.TenantID)
}

func TestDefaultLogger_Query(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	// Add some events.
	events := []Event{
		{Actor: Actor{Type: ActorTypeUser}, Action: "session.created", ResourceType: "session", ResourceID: "sess_1", Success: true},
		{Actor: Actor{Type: ActorTypeAPIKey}, Action: "session.terminated", ResourceType: "session", ResourceID: "sess_2", Success: true},
		{Actor: Actor{Type: ActorTypeSystem}, Action: "runner.connected", ResourceType: "runner", ResourceID: "run_1", Success: true},
	}

	for _, e := range events {
		err := logger.Log(ctx, e)
		require.NoError(t, err)
	}

	// Query all.
	result, err := logger.Query(ctx, Filter{})
	require.NoError(t, err)
	assert.Len(t, result.Events, 3)
	assert.Equal(t, 3, result.TotalCount)

	// Query by resource type.
	result, err = logger.Query(ctx, Filter{ResourceType: "session"})
	require.NoError(t, err)
	assert.Len(t, result.Events, 2)

	// Query by actor type.
	result, err = logger.Query(ctx, Filter{ActorType: ActorTypeUser})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
}

func TestDefaultLogger_QueryWithPagination(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	// Add 10 events.
	for i := 0; i < 10; i++ {
		err := logger.Log(ctx, Event{
			Actor:        Actor{Type: ActorTypeUser},
			Action:       "test.action",
			ResourceType: "test",
			ResourceID:   "test-1",
			Success:      true,
		})
		require.NoError(t, err)
	}

	// First page.
	result, err := logger.Query(ctx, Filter{Limit: 3})
	require.NoError(t, err)
	assert.Len(t, result.Events, 3)
	assert.Equal(t, 10, result.TotalCount)
	assert.True(t, result.HasMore)

	// Second page.
	result, err = logger.Query(ctx, Filter{Limit: 3, Offset: 3})
	require.NoError(t, err)
	assert.Len(t, result.Events, 3)
	assert.True(t, result.HasMore)
}

func TestDefaultLogger_MultipleEvents(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()
	logger := NewLogger(store)

	// Log multiple events rapidly.
	for i := 0; i < 100; i++ {
		err := logger.Log(ctx, Event{
			Actor:        Actor{Type: ActorTypeSystem},
			Action:       "test.event",
			ResourceType: "test",
			ResourceID:   "test-1",
			Success:      true,
		})
		require.NoError(t, err)
	}

	result, err := logger.Query(ctx, Filter{})
	require.NoError(t, err)
	assert.Equal(t, 100, result.TotalCount)
}

// Verify Logger interface is implemented.
func TestDefaultLogger_ImplementsInterface(_ *testing.T) {
	var _ Logger = (*DefaultLogger)(nil)
}
