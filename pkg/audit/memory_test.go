package audit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryStore_CreateActionLog(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	event := &StoredEvent{
		Event: Event{
			Actor: Actor{
				Type: ActorTypeUser,
				ID:   "user-123",
				Name: "John",
			},
			Action:       "session.created",
			ResourceType: "session",
			ResourceID:   "sess_123",
			Success:      true,
		},
	}

	err := store.CreateActionLog(ctx, event)
	require.NoError(t, err)
	assert.NotEmpty(t, event.ID)
	assert.False(t, event.Timestamp.IsZero())
	assert.Equal(t, 1, store.Count())
}

func TestMemoryStore_CreateActionLog_WithID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	event := &StoredEvent{
		ID: "alog_custom",
		Event: Event{
			Action:       "test.action",
			ResourceType: "test",
			ResourceID:   "test-1",
			Success:      true,
		},
	}

	err := store.CreateActionLog(ctx, event)
	require.NoError(t, err)
	assert.Equal(t, "alog_custom", event.ID)
}

func TestMemoryStore_CreateActionLog_WithTimestamp(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	customTime := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	event := &StoredEvent{
		Event: Event{
			Action:       "test.action",
			ResourceType: "test",
			ResourceID:   "test-1",
			Success:      true,
			Timestamp:    customTime,
		},
	}

	err := store.CreateActionLog(ctx, event)
	require.NoError(t, err)
	assert.Equal(t, customTime, event.Timestamp)
}

func TestMemoryStore_ListActionLogs_Empty(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	assert.Empty(t, result.Events)
	assert.Equal(t, 0, result.TotalCount)
	assert.False(t, result.HasMore)
}

func TestMemoryStore_ListActionLogs_FilterByActorType(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	// Create events with different actor types.
	for _, actorType := range []ActorType{ActorTypeUser, ActorTypeAPIKey, ActorTypeSystem} {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: actorType},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      true,
			},
		})
		require.NoError(t, err)
	}

	result, err := store.ListActionLogs(ctx, Filter{ActorType: ActorTypeUser})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
	assert.Equal(t, ActorTypeUser, result.Events[0].Actor.Type)
}

func TestMemoryStore_ListActionLogs_FilterByActorID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	for _, actorID := range []string{"user-1", "user-2", "user-3"} {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser, ID: actorID},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      true,
			},
		})
		require.NoError(t, err)
	}

	result, err := store.ListActionLogs(ctx, Filter{ActorID: "user-2"})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
	assert.Equal(t, "user-2", result.Events[0].Actor.ID)
}

func TestMemoryStore_ListActionLogs_FilterByAction(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	actions := []string{"session.created", "session.terminated", "permission.approved"}
	for _, action := range actions {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       action,
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      true,
			},
		})
		require.NoError(t, err)
	}

	result, err := store.ListActionLogs(ctx, Filter{Action: "session.created"})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
	assert.Equal(t, "session.created", result.Events[0].Action)
}

func TestMemoryStore_ListActionLogs_FilterByActionPrefix(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	actions := []string{"session.created", "session.terminated", "permission.approved"}
	for _, action := range actions {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       action,
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      true,
			},
		})
		require.NoError(t, err)
	}

	result, err := store.ListActionLogs(ctx, Filter{ActionPrefix: "session."})
	require.NoError(t, err)
	assert.Len(t, result.Events, 2)
}

func TestMemoryStore_ListActionLogs_FilterByResourceType(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	for _, resourceType := range []string{"session", "permission_request", "runner"} {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: resourceType,
				ResourceID:   "test-1",
				Success:      true,
			},
		})
		require.NoError(t, err)
	}

	result, err := store.ListActionLogs(ctx, Filter{ResourceType: "session"})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
	assert.Equal(t, "session", result.Events[0].ResourceType)
}

func TestMemoryStore_ListActionLogs_FilterByResourceID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	for _, resourceID := range []string{"sess_1", "sess_2", "sess_3"} {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "session",
				ResourceID:   resourceID,
				Success:      true,
			},
		})
		require.NoError(t, err)
	}

	result, err := store.ListActionLogs(ctx, Filter{ResourceID: "sess_2"})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
	assert.Equal(t, "sess_2", result.Events[0].ResourceID)
}

func TestMemoryStore_ListActionLogs_FilterBySessionID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	for _, sessionID := range []string{"sess_1", "sess_2", ""} {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				SessionID:    sessionID,
				Success:      true,
			},
		})
		require.NoError(t, err)
	}

	result, err := store.ListActionLogs(ctx, Filter{SessionID: "sess_1"})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
	assert.Equal(t, "sess_1", result.Events[0].SessionID)
}

func TestMemoryStore_ListActionLogs_FilterByTaskID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	for _, taskID := range []string{"task_1", "task_2", ""} {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				TaskID:       taskID,
				Success:      true,
			},
		})
		require.NoError(t, err)
	}

	result, err := store.ListActionLogs(ctx, Filter{TaskID: "task_1"})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
	assert.Equal(t, "task_1", result.Events[0].TaskID)
}

func TestMemoryStore_ListActionLogs_FilterByTenantID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	for _, tenantID := range []string{"tenant-1", "tenant-2", ""} {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				TenantID:     tenantID,
				Success:      true,
			},
		})
		require.NoError(t, err)
	}

	result, err := store.ListActionLogs(ctx, Filter{TenantID: "tenant-1"})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
	assert.Equal(t, "tenant-1", result.Events[0].TenantID)
}

func TestMemoryStore_ListActionLogs_FilterBySuccess(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	// Create 3 successful and 2 failed events.
	for i := 0; i < 3; i++ {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      true,
			},
		})
		require.NoError(t, err)
	}
	for i := 0; i < 2; i++ {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      false,
			},
		})
		require.NoError(t, err)
	}

	// Filter success only.
	result, err := store.ListActionLogs(ctx, Filter{SuccessOnly: true})
	require.NoError(t, err)
	assert.Len(t, result.Events, 3)

	// Filter failure only.
	result, err = store.ListActionLogs(ctx, Filter{FailureOnly: true})
	require.NoError(t, err)
	assert.Len(t, result.Events, 2)
}

func TestMemoryStore_ListActionLogs_FilterByTimeRange(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	now := time.Now().UTC()
	timestamps := []time.Time{
		now.Add(-2 * time.Hour),
		now.Add(-1 * time.Hour),
		now,
	}

	for _, ts := range timestamps {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      true,
				Timestamp:    ts,
			},
		})
		require.NoError(t, err)
	}

	// Filter by start time.
	result, err := store.ListActionLogs(ctx, Filter{
		StartTime: now.Add(-90 * time.Minute),
	})
	require.NoError(t, err)
	assert.Len(t, result.Events, 2)

	// Filter by end time.
	result, err = store.ListActionLogs(ctx, Filter{
		EndTime: now.Add(-90 * time.Minute),
	})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)

	// Filter by time range.
	result, err = store.ListActionLogs(ctx, Filter{
		StartTime: now.Add(-90 * time.Minute),
		EndTime:   now.Add(-30 * time.Minute),
	})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
}

func TestMemoryStore_ListActionLogs_Pagination(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	// Create 15 events.
	for i := 0; i < 15; i++ {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      true,
				Timestamp:    time.Now().Add(time.Duration(i) * time.Second),
			},
		})
		require.NoError(t, err)
	}

	// First page.
	result, err := store.ListActionLogs(ctx, Filter{Limit: 5})
	require.NoError(t, err)
	assert.Len(t, result.Events, 5)
	assert.Equal(t, 15, result.TotalCount)
	assert.True(t, result.HasMore)

	// Second page.
	result, err = store.ListActionLogs(ctx, Filter{Limit: 5, Offset: 5})
	require.NoError(t, err)
	assert.Len(t, result.Events, 5)
	assert.True(t, result.HasMore)

	// Third page.
	result, err = store.ListActionLogs(ctx, Filter{Limit: 5, Offset: 10})
	require.NoError(t, err)
	assert.Len(t, result.Events, 5)
	assert.False(t, result.HasMore)

	// Beyond data.
	result, err = store.ListActionLogs(ctx, Filter{Limit: 5, Offset: 15})
	require.NoError(t, err)
	assert.Empty(t, result.Events)
	assert.False(t, result.HasMore)
}

func TestMemoryStore_ListActionLogs_DefaultLimit(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	// Create 150 events.
	for i := 0; i < 150; i++ {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      true,
			},
		})
		require.NoError(t, err)
	}

	// No limit specified, should use default (100).
	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	assert.Len(t, result.Events, 100)
	assert.Equal(t, 150, result.TotalCount)
	assert.True(t, result.HasMore)
}

func TestMemoryStore_ListActionLogs_SortByTimestamp(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	times := []time.Time{
		time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		time.Date(2025, 1, 1, 11, 0, 0, 0, time.UTC),
	}

	for _, ts := range times {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      true,
				Timestamp:    ts,
			},
		})
		require.NoError(t, err)
	}

	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 3)

	// Should be sorted by timestamp descending.
	assert.Equal(t, times[1], result.Events[0].Timestamp) // 12:00
	assert.Equal(t, times[2], result.Events[1].Timestamp) // 11:00
	assert.Equal(t, times[0], result.Events[2].Timestamp) // 10:00
}

func TestMemoryStore_Clear(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	// Add some events.
	for i := 0; i < 5; i++ {
		err := store.CreateActionLog(ctx, &StoredEvent{
			Event: Event{
				Actor:        Actor{Type: ActorTypeUser},
				Action:       "test.action",
				ResourceType: "test",
				ResourceID:   "test-1",
				Success:      true,
			},
		})
		require.NoError(t, err)
	}
	assert.Equal(t, 5, store.Count())

	store.Clear()
	assert.Equal(t, 0, store.Count())
}

func TestMemoryStore_CombinedFilters(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	// Create various events.
	events := []Event{
		{Actor: Actor{Type: ActorTypeUser, ID: "user-1"}, Action: "session.created", ResourceType: "session", ResourceID: "sess_1", Success: true, TenantID: "tenant-1"},
		{Actor: Actor{Type: ActorTypeUser, ID: "user-1"}, Action: "session.terminated", ResourceType: "session", ResourceID: "sess_1", Success: true, TenantID: "tenant-1"},
		{Actor: Actor{Type: ActorTypeAPIKey, ID: "key-1"}, Action: "session.created", ResourceType: "session", ResourceID: "sess_2", Success: true, TenantID: "tenant-1"},
		{Actor: Actor{Type: ActorTypeUser, ID: "user-1"}, Action: "session.created", ResourceType: "session", ResourceID: "sess_3", Success: false, TenantID: "tenant-2"},
	}

	for _, e := range events {
		err := store.CreateActionLog(ctx, &StoredEvent{Event: e})
		require.NoError(t, err)
	}

	// Combined filter: user-1, session., tenant-1, success only.
	result, err := store.ListActionLogs(ctx, Filter{
		ActorID:      "user-1",
		ActionPrefix: "session.",
		TenantID:     "tenant-1",
		SuccessOnly:  true,
	})
	require.NoError(t, err)
	assert.Len(t, result.Events, 2)
}

func TestMemoryStore_ImmutableStorage(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryStore()

	event := &StoredEvent{
		Event: Event{
			Actor:        Actor{Type: ActorTypeUser, ID: "user-1"},
			Action:       "test.action",
			ResourceType: "test",
			ResourceID:   "test-1",
			Success:      true,
		},
	}

	err := store.CreateActionLog(ctx, event)
	require.NoError(t, err)
	originalID := event.ID

	// Modify the original event.
	event.Actor.ID = "user-modified"

	// Verify stored event is not affected.
	result, err := store.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	require.Len(t, result.Events, 1)
	assert.Equal(t, originalID, result.Events[0].ID)
	assert.Equal(t, "user-1", result.Events[0].Actor.ID)
}
