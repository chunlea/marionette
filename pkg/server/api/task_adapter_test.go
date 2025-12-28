package api

import (
	"context"
	"errors"
	"testing"

	"github.com/chunlea/marionette/pkg/store"
)

// mockLogStore implements log listing for testing.
type mockLogStore struct {
	logs map[string][]*store.Log
}

func newMockLogStore() *mockLogStore {
	return &mockLogStore{
		logs: make(map[string][]*store.Log),
	}
}

func (s *mockLogStore) ListLogs(ctx context.Context, opts store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	if opts.TaskID == nil {
		return &store.ListResult[store.Log]{}, nil
	}
	logs := s.logs[*opts.TaskID]
	var items []*store.Log
	for _, log := range logs {
		items = append(items, log)
	}
	return &store.ListResult[store.Log]{Items: items}, nil
}

func TestNewTaskAdapter(t *testing.T) {
	adapter := NewTaskAdapter(nil, nil)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}

func TestTaskAdapter_GetLogs(t *testing.T) {
	st := newMockLogStore()
	adapter := &TaskAdapter{store: st}

	ctx := context.Background()
	taskID := "task_123"

	// Add some logs
	st.logs[taskID] = []*store.Log{
		{ID: "log_1", TaskID: taskID, Content: []byte("line 1")},
		{ID: "log_2", TaskID: taskID, Content: []byte("line 2")},
	}

	// Get logs
	result, err := adapter.GetLogs(ctx, taskID, GetLogsOptions{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 logs, got %d", len(result.Items))
	}
}

func TestTaskAdapter_GetLogs_Empty(t *testing.T) {
	st := newMockLogStore()
	adapter := &TaskAdapter{store: st}

	ctx := context.Background()

	// Get logs for task with no logs
	result, err := adapter.GetLogs(ctx, "task_empty", GetLogsOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 0 {
		t.Errorf("expected 0 logs, got %d", len(result.Items))
	}
}

func TestTaskAdapter_StreamLogs_NotImplemented(t *testing.T) {
	adapter := NewTaskAdapter(nil, nil)

	ctx := context.Background()

	// StreamLogs should return an error (not yet implemented)
	ch, err := adapter.StreamLogs(ctx, "task_123", StreamLogsOptions{})
	if err == nil {
		t.Fatal("expected error for unimplemented StreamLogs")
	}
	if ch != nil {
		t.Error("expected nil channel")
	}
	if !errors.Is(err, errors.New("log streaming not yet implemented")) {
		// Just verify it's an error
		if err.Error() != "log streaming not yet implemented" {
			t.Errorf("unexpected error message: %v", err)
		}
	}
}

// Verify TaskAdapter implements TaskService interface
var _ TaskService = (*TaskAdapter)(nil)
