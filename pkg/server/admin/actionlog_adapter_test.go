package admin

import (
	"context"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockActionLogStore implements ActionLogStore for testing.
type mockActionLogStore struct {
	logs          map[string]*store.ActionLog
	listError     error
	getError      error
}

func newMockActionLogStore() *mockActionLogStore {
	return &mockActionLogStore{
		logs: make(map[string]*store.ActionLog),
	}
}

func (s *mockActionLogStore) AddLog(log *store.ActionLog) {
	s.logs[log.ID] = log
}

func (s *mockActionLogStore) SetListError(err error) {
	s.listError = err
}

func (s *mockActionLogStore) SetGetError(err error) {
	s.getError = err
}

func (s *mockActionLogStore) GetActionLog(_ context.Context, id string) (*store.ActionLog, error) {
	if s.getError != nil {
		return nil, s.getError
	}
	log, ok := s.logs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return log, nil
}

func (s *mockActionLogStore) ListActionLogs(_ context.Context, opts store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	if s.listError != nil {
		return nil, s.listError
	}

	// Collect all logs
	items := make([]*store.ActionLog, 0, len(s.logs))
	for _, log := range s.logs {
		// Apply filters
		if opts.ActorType != nil && log.ActorType != *opts.ActorType {
			continue
		}
		if opts.ActorID != nil && (log.ActorID == nil || *log.ActorID != *opts.ActorID) {
			continue
		}
		if opts.Action != nil && log.Action != *opts.Action {
			continue
		}
		if opts.ActionPrefix != nil && !strings.HasPrefix(log.Action, *opts.ActionPrefix) {
			continue
		}
		if opts.ResourceType != nil && log.ResourceType != *opts.ResourceType {
			continue
		}
		if opts.ResourceID != nil && log.ResourceID != *opts.ResourceID {
			continue
		}
		if opts.SessionID != nil && (log.SessionID == nil || *log.SessionID != *opts.SessionID) {
			continue
		}
		if opts.TaskID != nil && (log.TaskID == nil || *log.TaskID != *opts.TaskID) {
			continue
		}
		if opts.Success != nil && log.Success != *opts.Success {
			continue
		}
		if opts.From != nil && log.CreatedAt.Before(*opts.From) {
			continue
		}
		if opts.To != nil && log.CreatedAt.After(*opts.To) {
			continue
		}
		items = append(items, log)
	}

	// Sort by CreatedAt descending
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt.After(items[j].CreatedAt)
	})

	// Apply limit
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	nextCursor := ""
	if hasMore && len(items) > 0 {
		nextCursor = items[len(items)-1].ID
	}

	return &store.ListResult[store.ActionLog]{
		Items:      items,
		TotalCount: int64(len(s.logs)),
		NextCursor: nextCursor,
	}, nil
}

func TestNewActionLogStoreAdapter(t *testing.T) {
	st := newMockActionLogStore()
	adapter := NewActionLogStoreAdapter(st)

	assert.NotNil(t, adapter)
	assert.Equal(t, st, adapter.store)
}

func TestActionLogStoreAdapter_Get(t *testing.T) {
	st := newMockActionLogStore()
	adapter := NewActionLogStoreAdapter(st)
	ctx := context.Background()

	// Create a log entry
	log := &store.ActionLog{
		ID:           "alog_test1",
		ActorType:    "user",
		Action:       "permission.approved",
		ResourceType: "permission_request",
		ResourceID:   "perm_123",
		Success:      true,
		CreatedAt:    time.Now(),
	}
	st.AddLog(log)

	// Get the log
	got, err := adapter.Get(ctx, "alog_test1")
	require.NoError(t, err)
	assert.Equal(t, log.ID, got.ID)
	assert.Equal(t, log.Action, got.Action)
}

func TestActionLogStoreAdapter_Get_NotFound(t *testing.T) {
	st := newMockActionLogStore()
	adapter := NewActionLogStoreAdapter(st)
	ctx := context.Background()

	_, err := adapter.Get(ctx, "nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestActionLogStoreAdapter_List(t *testing.T) {
	st := newMockActionLogStore()
	adapter := NewActionLogStoreAdapter(st)
	ctx := context.Background()

	// Create some log entries
	now := time.Now()
	logs := []*store.ActionLog{
		{
			ID:           "alog_1",
			ActorType:    "user",
			Action:       "permission.approved",
			ResourceType: "permission_request",
			ResourceID:   "perm_1",
			Success:      true,
			CreatedAt:    now.Add(-2 * time.Hour),
		},
		{
			ID:           "alog_2",
			ActorType:    "api_key",
			Action:       "session.created",
			ResourceType: "session",
			ResourceID:   "sess_1",
			Success:      true,
			CreatedAt:    now.Add(-1 * time.Hour),
		},
		{
			ID:           "alog_3",
			ActorType:    "user",
			Action:       "task.canceled",
			ResourceType: "task",
			ResourceID:   "task_1",
			Success:      false,
			CreatedAt:    now,
		},
	}
	for _, log := range logs {
		st.AddLog(log)
	}

	// List all
	result, err := adapter.List(ctx, ListActionLogsOptions{Limit: 10})
	require.NoError(t, err)
	assert.Len(t, result.Items, 3)
	assert.Equal(t, int64(3), result.TotalCount)
}

func TestActionLogStoreAdapter_List_WithFilters(t *testing.T) {
	st := newMockActionLogStore()
	adapter := NewActionLogStoreAdapter(st)
	ctx := context.Background()

	now := time.Now()
	sessionID := "sess_filter"
	taskID := "task_filter"
	actorID := "user_123"

	logs := []*store.ActionLog{
		{
			ID:           "alog_a",
			ActorType:    "user",
			ActorID:      &actorID,
			Action:       "permission.approved",
			ResourceType: "permission_request",
			ResourceID:   "perm_a",
			SessionID:    &sessionID,
			Success:      true,
			CreatedAt:    now.Add(-2 * time.Hour),
		},
		{
			ID:           "alog_b",
			ActorType:    "api_key",
			Action:       "session.created",
			ResourceType: "session",
			ResourceID:   "sess_b",
			Success:      true,
			CreatedAt:    now.Add(-1 * time.Hour),
		},
		{
			ID:           "alog_c",
			ActorType:    "user",
			Action:       "task.canceled",
			ResourceType: "task",
			ResourceID:   "task_c",
			TaskID:       &taskID,
			Success:      false,
			CreatedAt:    now,
		},
	}
	for _, log := range logs {
		st.AddLog(log)
	}

	t.Run("filter by actor_type", func(t *testing.T) {
		result, err := adapter.List(ctx, ListActionLogsOptions{ActorType: "user"})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
	})

	t.Run("filter by actor_id", func(t *testing.T) {
		result, err := adapter.List(ctx, ListActionLogsOptions{ActorID: actorID})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "alog_a", result.Items[0].ID)
	})

	t.Run("filter by action", func(t *testing.T) {
		result, err := adapter.List(ctx, ListActionLogsOptions{Action: "session.created"})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "alog_b", result.Items[0].ID)
	})

	t.Run("filter by action_prefix", func(t *testing.T) {
		result, err := adapter.List(ctx, ListActionLogsOptions{ActionPrefix: "permission."})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "alog_a", result.Items[0].ID)
	})

	t.Run("filter by resource_type", func(t *testing.T) {
		result, err := adapter.List(ctx, ListActionLogsOptions{ResourceType: "task"})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "alog_c", result.Items[0].ID)
	})

	t.Run("filter by resource_id", func(t *testing.T) {
		result, err := adapter.List(ctx, ListActionLogsOptions{ResourceID: "perm_a"})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "alog_a", result.Items[0].ID)
	})

	t.Run("filter by session_id", func(t *testing.T) {
		result, err := adapter.List(ctx, ListActionLogsOptions{SessionID: sessionID})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "alog_a", result.Items[0].ID)
	})

	t.Run("filter by task_id", func(t *testing.T) {
		result, err := adapter.List(ctx, ListActionLogsOptions{TaskID: taskID})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "alog_c", result.Items[0].ID)
	})

	t.Run("filter by success", func(t *testing.T) {
		success := false
		result, err := adapter.List(ctx, ListActionLogsOptions{Success: &success})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "alog_c", result.Items[0].ID)
	})

	t.Run("filter by time range", func(t *testing.T) {
		from := now.Add(-90 * time.Minute)
		to := now.Add(-30 * time.Minute)
		result, err := adapter.List(ctx, ListActionLogsOptions{From: &from, To: &to})
		require.NoError(t, err)
		assert.Len(t, result.Items, 1)
		assert.Equal(t, "alog_b", result.Items[0].ID)
	})
}

func TestActionLogStoreAdapter_List_Error(t *testing.T) {
	st := newMockActionLogStore()
	adapter := NewActionLogStoreAdapter(st)
	ctx := context.Background()

	// Set error
	expectedErr := assert.AnError
	st.SetListError(expectedErr)

	// List should return error
	_, err := adapter.List(ctx, ListActionLogsOptions{})
	assert.ErrorIs(t, err, expectedErr)
}

func TestActionLogStoreAdapter_List_Pagination(t *testing.T) {
	st := newMockActionLogStore()
	adapter := NewActionLogStoreAdapter(st)
	ctx := context.Background()

	// Create 5 log entries
	now := time.Now()
	for i := 0; i < 5; i++ {
		log := &store.ActionLog{
			ID:           "alog_" + string(rune('a'+i)),
			ActorType:    "user",
			Action:       "test.action",
			ResourceType: "test",
			ResourceID:   "test_" + string(rune('a'+i)),
			Success:      true,
			CreatedAt:    now.Add(time.Duration(i) * time.Hour),
		}
		st.AddLog(log)
	}

	// List with limit
	result, err := adapter.List(ctx, ListActionLogsOptions{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
	assert.NotEmpty(t, result.NextCursor)
	assert.Equal(t, int64(5), result.TotalCount)
}

// Ensure ActionLogStoreAdapter implements ActionLogService interface.
var _ ActionLogService = (*ActionLogStoreAdapter)(nil)
