package audit

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// mockActionLogStore implements ActionLogStore for testing.
type mockActionLogStore struct {
	logs []*store.ActionLog
}

func newMockActionLogStore() *mockActionLogStore {
	return &mockActionLogStore{
		logs: make([]*store.ActionLog, 0),
	}
}

func (m *mockActionLogStore) CreateActionLog(_ context.Context, log *store.ActionLog) error {
	m.logs = append(m.logs, log)
	return nil
}

func (m *mockActionLogStore) ListActionLogs(_ context.Context, opts store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	matched := make([]*store.ActionLog, 0, len(m.logs))

	for _, log := range m.logs {
		if !matchesListOptions(log, opts) {
			continue
		}
		matched = append(matched, log)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	hasMore := len(matched) > limit
	if hasMore {
		matched = matched[:limit]
	}

	return &store.ListResult[store.ActionLog]{
		Items:      matched,
		TotalCount: int64(len(matched)),
		HasMore:    hasMore,
	}, nil
}

func matchesListOptions(log *store.ActionLog, opts store.ListActionLogsOptions) bool {
	if opts.ActorType != nil && log.ActorType != *opts.ActorType {
		return false
	}
	if opts.ActorID != nil && (log.ActorID == nil || *log.ActorID != *opts.ActorID) {
		return false
	}
	if opts.Action != nil && log.Action != *opts.Action {
		return false
	}
	if opts.ResourceType != nil && log.ResourceType != *opts.ResourceType {
		return false
	}
	if opts.ResourceID != nil && log.ResourceID != *opts.ResourceID {
		return false
	}
	if opts.SessionID != nil && (log.SessionID == nil || *log.SessionID != *opts.SessionID) {
		return false
	}
	if opts.TaskID != nil && (log.TaskID == nil || *log.TaskID != *opts.TaskID) {
		return false
	}
	if opts.Success != nil && log.Success != *opts.Success {
		return false
	}
	return true
}

func TestNewStoreAdapter(t *testing.T) {
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)
	assert.NotNil(t, adapter)
}

func TestStoreAdapter_CreateActionLog(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	event := &StoredEvent{
		Event: Event{
			Actor: Actor{
				Type: ActorTypeUser,
				ID:   "user-123",
				Name: "Test User",
			},
			Action:       "session.created",
			ResourceType: "session",
			ResourceID:   "sess_456",
			SessionID:    "sess_456",
			IPAddress:    "192.168.1.1",
			UserAgent:    "Mozilla/5.0",
			Success:      true,
			TenantID:     "tenant-1",
		},
	}

	err := adapter.CreateActionLog(ctx, event)
	require.NoError(t, err)

	require.Len(t, mock.logs, 1)
	log := mock.logs[0]

	// Check ID was generated.
	assert.NotEmpty(t, log.ID)
	assert.True(t, log.ID[:5] == "alog_")

	// Check timestamp was set.
	assert.False(t, log.CreatedAt.IsZero())

	// Check fields were converted correctly.
	assert.Equal(t, "user", log.ActorType)
	assert.Equal(t, "user-123", *log.ActorID)
	assert.Equal(t, "Test User", *log.ActorName)
	assert.Equal(t, "session.created", log.Action)
	assert.Equal(t, "session", log.ResourceType)
	assert.Equal(t, "sess_456", log.ResourceID)
	assert.Equal(t, "sess_456", *log.SessionID)
	assert.Equal(t, "192.168.1.1", *log.IPAddress)
	assert.Equal(t, "Mozilla/5.0", *log.UserAgent)
	assert.True(t, log.Success)
	assert.Equal(t, "tenant-1", *log.TenantID)
}

func TestStoreAdapter_CreateActionLog_PreservesID(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	event := &StoredEvent{
		ID: "alog_custom123",
		Event: Event{
			Actor:        Actor{Type: ActorTypeSystem},
			Action:       "test.action",
			ResourceType: "test",
			ResourceID:   "test-1",
			Success:      true,
		},
	}

	err := adapter.CreateActionLog(ctx, event)
	require.NoError(t, err)

	require.Len(t, mock.logs, 1)
	assert.Equal(t, "alog_custom123", mock.logs[0].ID)
}

func TestStoreAdapter_CreateActionLog_PreservesTimestamp(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	customTime := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)
	event := &StoredEvent{
		Event: Event{
			Actor:        Actor{Type: ActorTypeSystem},
			Action:       "test.action",
			ResourceType: "test",
			ResourceID:   "test-1",
			Success:      true,
			Timestamp:    customTime,
		},
	}

	err := adapter.CreateActionLog(ctx, event)
	require.NoError(t, err)

	require.Len(t, mock.logs, 1)
	assert.Equal(t, customTime, mock.logs[0].CreatedAt)
}

func TestStoreAdapter_CreateActionLog_WithDetails(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	details := json.RawMessage(`{"reason":"test","count":42}`)
	event := &StoredEvent{
		Event: Event{
			Actor:        Actor{Type: ActorTypeUser},
			Action:       "test.action",
			ResourceType: "test",
			ResourceID:   "test-1",
			Details:      details,
			Success:      true,
		},
	}

	err := adapter.CreateActionLog(ctx, event)
	require.NoError(t, err)

	require.Len(t, mock.logs, 1)
	assert.JSONEq(t, `{"reason":"test","count":42}`, string(mock.logs[0].Details))
}

func TestStoreAdapter_CreateActionLog_WithError(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	event := &StoredEvent{
		Event: Event{
			Actor:        Actor{Type: ActorTypeUser},
			Action:       "test.action",
			ResourceType: "test",
			ResourceID:   "test-1",
			Success:      false,
			ErrorMessage: "something went wrong",
		},
	}

	err := adapter.CreateActionLog(ctx, event)
	require.NoError(t, err)

	require.Len(t, mock.logs, 1)
	assert.False(t, mock.logs[0].Success)
	assert.Equal(t, "something went wrong", *mock.logs[0].ErrorMessage)
}

func TestStoreAdapter_ListActionLogs(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	// Create some logs directly in mock.
	userID := "user-1"
	sessionID := "sess_123"
	mock.logs = []*store.ActionLog{
		{
			ID:           "alog_1",
			ActorType:    "user",
			ActorID:      &userID,
			Action:       "session.created",
			ResourceType: "session",
			ResourceID:   "sess_123",
			SessionID:    &sessionID,
			Success:      true,
			CreatedAt:    time.Now(),
		},
		{
			ID:           "alog_2",
			ActorType:    "api_key",
			Action:       "task.created",
			ResourceType: "task",
			ResourceID:   "task_456",
			Success:      true,
			CreatedAt:    time.Now(),
		},
	}

	// List all.
	result, err := adapter.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	assert.Len(t, result.Events, 2)
	assert.Equal(t, 2, result.TotalCount)

	// Check first event was converted correctly.
	e := result.Events[0]
	assert.Equal(t, "alog_1", e.ID)
	assert.Equal(t, ActorTypeUser, e.Actor.Type)
	assert.Equal(t, "user-1", e.Actor.ID)
	assert.Equal(t, "session.created", e.Action)
	assert.Equal(t, "session", e.ResourceType)
	assert.Equal(t, "sess_123", e.ResourceID)
	assert.Equal(t, "sess_123", e.SessionID)
	assert.True(t, e.Success)
}

func TestStoreAdapter_ListActionLogs_FilterByActorType(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	mock.logs = []*store.ActionLog{
		{ID: "alog_1", ActorType: "user", Action: "test", ResourceType: "test", ResourceID: "1", Success: true},
		{ID: "alog_2", ActorType: "api_key", Action: "test", ResourceType: "test", ResourceID: "2", Success: true},
		{ID: "alog_3", ActorType: "user", Action: "test", ResourceType: "test", ResourceID: "3", Success: true},
	}

	result, err := adapter.ListActionLogs(ctx, Filter{ActorType: ActorTypeUser})
	require.NoError(t, err)
	assert.Len(t, result.Events, 2)
}

func TestStoreAdapter_ListActionLogs_FilterByAction(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	mock.logs = []*store.ActionLog{
		{ID: "alog_1", ActorType: "user", Action: "session.created", ResourceType: "session", ResourceID: "1", Success: true},
		{ID: "alog_2", ActorType: "user", Action: "session.terminated", ResourceType: "session", ResourceID: "2", Success: true},
		{ID: "alog_3", ActorType: "user", Action: "session.created", ResourceType: "session", ResourceID: "3", Success: true},
	}

	result, err := adapter.ListActionLogs(ctx, Filter{Action: "session.created"})
	require.NoError(t, err)
	assert.Len(t, result.Events, 2)
}

func TestStoreAdapter_ListActionLogs_FilterBySuccess(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	mock.logs = []*store.ActionLog{
		{ID: "alog_1", ActorType: "user", Action: "test", ResourceType: "test", ResourceID: "1", Success: true},
		{ID: "alog_2", ActorType: "user", Action: "test", ResourceType: "test", ResourceID: "2", Success: false},
		{ID: "alog_3", ActorType: "user", Action: "test", ResourceType: "test", ResourceID: "3", Success: true},
	}

	// Success only.
	result, err := adapter.ListActionLogs(ctx, Filter{SuccessOnly: true})
	require.NoError(t, err)
	assert.Len(t, result.Events, 2)

	// Failure only.
	result, err = adapter.ListActionLogs(ctx, Filter{FailureOnly: true})
	require.NoError(t, err)
	assert.Len(t, result.Events, 1)
}

func TestStoreAdapter_ListActionLogs_Pagination(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	// Create 15 logs.
	for i := 0; i < 15; i++ {
		mock.logs = append(mock.logs, &store.ActionLog{
			ID:           "alog_" + string(rune('a'+i)),
			ActorType:    "user",
			Action:       "test",
			ResourceType: "test",
			ResourceID:   "1",
			Success:      true,
		})
	}

	result, err := adapter.ListActionLogs(ctx, Filter{Limit: 5})
	require.NoError(t, err)
	assert.Len(t, result.Events, 5)
	assert.True(t, result.HasMore)
}

func TestStoreAdapter_ListActionLogs_DefaultLimit(t *testing.T) {
	ctx := context.Background()
	mock := newMockActionLogStore()
	adapter := NewStoreAdapter(mock)

	// Create 150 logs.
	for i := 0; i < 150; i++ {
		mock.logs = append(mock.logs, &store.ActionLog{
			ID:           "alog_" + string(rune('a'+i%26)),
			ActorType:    "user",
			Action:       "test",
			ResourceType: "test",
			ResourceID:   "1",
			Success:      true,
		})
	}

	result, err := adapter.ListActionLogs(ctx, Filter{})
	require.NoError(t, err)
	assert.Len(t, result.Events, 100)
	assert.True(t, result.HasMore)
}

func TestEventToActionLog(t *testing.T) {
	event := &StoredEvent{
		ID: "alog_123",
		Event: Event{
			Actor: Actor{
				Type: ActorTypeAPIKey,
				ID:   "key-456",
				Name: "CI Key",
			},
			Action:       "permission.approved",
			ResourceType: "permission_request",
			ResourceID:   "perm_789",
			SessionID:    "sess_abc",
			TaskID:       "task_def",
			Details:      json.RawMessage(`{"tool":"bash"}`),
			IPAddress:    "10.0.0.1",
			UserAgent:    "curl/7.64",
			Success:      true,
			ErrorMessage: "",
			TenantID:     "tenant-1",
			Timestamp:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		},
	}

	log := eventToActionLog(event)

	assert.Equal(t, "alog_123", log.ID)
	assert.Equal(t, "api_key", log.ActorType)
	assert.Equal(t, "key-456", *log.ActorID)
	assert.Equal(t, "CI Key", *log.ActorName)
	assert.Equal(t, "permission.approved", log.Action)
	assert.Equal(t, "permission_request", log.ResourceType)
	assert.Equal(t, "perm_789", log.ResourceID)
	assert.Equal(t, "sess_abc", *log.SessionID)
	assert.Equal(t, "task_def", *log.TaskID)
	assert.JSONEq(t, `{"tool":"bash"}`, string(log.Details))
	assert.Equal(t, "10.0.0.1", *log.IPAddress)
	assert.Equal(t, "curl/7.64", *log.UserAgent)
	assert.True(t, log.Success)
	assert.Nil(t, log.ErrorMessage)
	assert.Equal(t, "tenant-1", *log.TenantID)
	assert.Equal(t, time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC), log.CreatedAt)
}

func TestActionLogToEvent(t *testing.T) {
	actorID := "key-456"
	actorName := "CI Key"
	sessionID := "sess_abc"
	taskID := "task_def"
	ipAddress := "10.0.0.1"
	userAgent := "curl/7.64"
	tenantID := "tenant-1"

	log := &store.ActionLog{
		ID:           "alog_123",
		ActorType:    "api_key",
		ActorID:      &actorID,
		ActorName:    &actorName,
		Action:       "permission.approved",
		ResourceType: "permission_request",
		ResourceID:   "perm_789",
		SessionID:    &sessionID,
		TaskID:       &taskID,
		Details:      json.RawMessage(`{"tool":"bash"}`),
		IPAddress:    &ipAddress,
		UserAgent:    &userAgent,
		Success:      true,
		ErrorMessage: nil,
		TenantID:     &tenantID,
		CreatedAt:    time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	event := actionLogToEvent(log)

	assert.Equal(t, "alog_123", event.ID)
	assert.Equal(t, ActorTypeAPIKey, event.Actor.Type)
	assert.Equal(t, "key-456", event.Actor.ID)
	assert.Equal(t, "CI Key", event.Actor.Name)
	assert.Equal(t, "permission.approved", event.Action)
	assert.Equal(t, "permission_request", event.ResourceType)
	assert.Equal(t, "perm_789", event.ResourceID)
	assert.Equal(t, "sess_abc", event.SessionID)
	assert.Equal(t, "task_def", event.TaskID)
	assert.JSONEq(t, `{"tool":"bash"}`, string(event.Details))
	assert.Equal(t, "10.0.0.1", event.IPAddress)
	assert.Equal(t, "curl/7.64", event.UserAgent)
	assert.True(t, event.Success)
	assert.Empty(t, event.ErrorMessage)
	assert.Equal(t, "tenant-1", event.TenantID)
	assert.Equal(t, time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC), event.Timestamp)
}

func TestFilterToOptions(t *testing.T) {
	filter := Filter{
		ActorType:    ActorTypeUser,
		ActorID:      "user-123",
		Action:       "session.created",
		ResourceType: "session",
		ResourceID:   "sess_456",
		SessionID:    "sess_456",
		TaskID:       "task_789",
		SuccessOnly:  true,
		Limit:        50,
	}

	opts := filterToOptions(filter)

	assert.Equal(t, 50, opts.Limit)
	assert.True(t, opts.OrderDesc)
	assert.Equal(t, "user", *opts.ActorType)
	assert.Equal(t, "user-123", *opts.ActorID)
	assert.Equal(t, "session.created", *opts.Action)
	assert.Equal(t, "session", *opts.ResourceType)
	assert.Equal(t, "sess_456", *opts.ResourceID)
	assert.Equal(t, "sess_456", *opts.SessionID)
	assert.Equal(t, "task_789", *opts.TaskID)
	assert.True(t, *opts.Success)
}

func TestFilterToOptions_FailureOnly(t *testing.T) {
	filter := Filter{
		FailureOnly: true,
	}

	opts := filterToOptions(filter)

	assert.False(t, *opts.Success)
}

func TestFilterToOptions_DefaultLimit(t *testing.T) {
	filter := Filter{}

	opts := filterToOptions(filter)

	assert.Equal(t, 100, opts.Limit)
}

func TestDerefString(t *testing.T) {
	s := "test"
	assert.Equal(t, "test", derefString(&s))
	assert.Equal(t, "", derefString(nil))
}

// Verify StoreAdapter implements Store.
func TestStoreAdapter_ImplementsStore(_ *testing.T) {
	var _ Store = (*StoreAdapter)(nil)
}
