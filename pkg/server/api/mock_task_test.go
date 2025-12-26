package api

import (
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockTaskService_Create(t *testing.T) {
	svc := NewMockTaskService()
	ctx := context.Background()

	tests := []struct {
		name    string
		opts    CreateTaskOptions
		check   func(*testing.T, *store.Task)
		wantErr bool
	}{
		{
			name: "create basic task",
			opts: CreateTaskOptions{
				SessionID: "sess_xxx",
				Prompt:    "Build an API",
			},
			check: func(t *testing.T, task *store.Task) {
				assert.NotEmpty(t, task.ID)
				assert.Equal(t, "sess_xxx", task.SessionID)
				assert.Equal(t, "Build an API", task.Prompt)
				assert.Equal(t, "pending", task.Status)
				assert.Equal(t, 3600, task.TimeoutSeconds)
			},
		},
		{
			name: "create task with custom timeout",
			opts: CreateTaskOptions{
				SessionID:      "sess_xxx",
				Prompt:         "Quick task",
				TimeoutSeconds: 300,
			},
			check: func(t *testing.T, task *store.Task) {
				assert.Equal(t, 300, task.TimeoutSeconds)
			},
		},
		{
			name: "create task with retries",
			opts: CreateTaskOptions{
				SessionID:  "sess_xxx",
				Prompt:     "Retry task",
				MaxRetries: 3,
			},
			check: func(t *testing.T, task *store.Task) {
				assert.Equal(t, 3, task.MaxRetries)
				assert.Equal(t, 0, task.RetryCount)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task, err := svc.Create(ctx, tt.opts)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, task)
			tt.check(t, task)
		})
	}
}

func TestMockTaskService_Get(t *testing.T) {
	svc := NewMockTaskService()
	ctx := context.Background()

	// Create a task
	task, err := svc.Create(ctx, CreateTaskOptions{
		SessionID: "sess_xxx",
		Prompt:    "Test task",
	})
	require.NoError(t, err)

	// Get existing task
	got, err := svc.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, task.ID, got.ID)

	// Get non-existent task
	_, err = svc.Get(ctx, "task_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockTaskService_List(t *testing.T) {
	svc := NewMockTaskService()
	ctx := context.Background()

	// Create tasks
	_, err := svc.Create(ctx, CreateTaskOptions{SessionID: "sess_1", Prompt: "Task 1"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, CreateTaskOptions{SessionID: "sess_1", Prompt: "Task 2"})
	require.NoError(t, err)
	_, err = svc.Create(ctx, CreateTaskOptions{SessionID: "sess_2", Prompt: "Task 3"})
	require.NoError(t, err)

	// List all
	result, err := svc.List(ctx, ListTasksOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 3)

	// List by session
	result, err = svc.List(ctx, ListTasksOptions{SessionID: "sess_1"})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)

	// List by status
	result, err = svc.List(ctx, ListTasksOptions{Status: []string{"pending"}})
	require.NoError(t, err)
	assert.Len(t, result.Items, 3)

	// List with limit
	result, err = svc.List(ctx, ListTasksOptions{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
}

func TestMockTaskService_Cancel(t *testing.T) {
	svc := NewMockTaskService()
	ctx := context.Background()

	// Create a task
	task, err := svc.Create(ctx, CreateTaskOptions{
		SessionID: "sess_xxx",
		Prompt:    "Test task",
	})
	require.NoError(t, err)

	// Cancel pending task
	err = svc.Cancel(ctx, task.ID)
	require.NoError(t, err)

	got, err := svc.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "canceled", got.Status)

	// Try to cancel again - should fail
	err = svc.Cancel(ctx, task.ID)
	require.Error(t, err)
	assert.True(t, IsInvalidState(err))

	// Cancel non-existent task
	err = svc.Cancel(ctx, "task_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockTaskService_Retry(t *testing.T) {
	svc := NewMockTaskService()
	ctx := context.Background()

	// Create a failed task with retries
	task := &store.Task{
		ID:          id.Task(),
		SessionID:   "sess_xxx",
		Prompt:      "Failed task",
		Status:      "failed",
		MaxRetries:  3,
		RetryCount:  0,
		Labels:      json.RawMessage("{}"),
		Annotations: json.RawMessage("{}"),
	}
	svc.AddTask(task)

	// Retry the task
	err := svc.Retry(ctx, task.ID)
	require.NoError(t, err)

	got, err := svc.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "pending", got.Status)
	assert.Equal(t, 1, got.RetryCount)

	// Fail and retry again
	got.Status = "failed"
	svc.AddTask(got)

	err = svc.Retry(ctx, task.ID)
	require.NoError(t, err)

	got, err = svc.Get(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, got.RetryCount)

	// Exceed max retries
	got.Status = "failed"
	got.RetryCount = 3
	svc.AddTask(got)

	err = svc.Retry(ctx, task.ID)
	require.Error(t, err)
	assert.True(t, IsMaxRetriesExceeded(err))

	// Retry non-failed task
	got.Status = "pending"
	svc.AddTask(got)

	err = svc.Retry(ctx, task.ID)
	require.Error(t, err)
	assert.True(t, IsInvalidState(err))

	// Retry non-existent task
	err = svc.Retry(ctx, "task_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockTaskService_GetLogs(t *testing.T) {
	svc := NewMockTaskService()
	ctx := context.Background()

	// Create a task
	task, err := svc.Create(ctx, CreateTaskOptions{
		SessionID: "sess_xxx",
		Prompt:    "Test task",
	})
	require.NoError(t, err)

	// Add logs
	now := time.Now()
	svc.AddLog(task.ID, &store.Log{
		ID:        id.Log(),
		TaskID:    task.ID,
		Stream:    "stdout",
		Level:     "info",
		Content:   "Log 1",
		Sequence:  1,
		CreatedAt: now,
	})
	svc.AddLog(task.ID, &store.Log{
		ID:        id.Log(),
		TaskID:    task.ID,
		Stream:    "stderr",
		Level:     "error",
		Content:   "Error log",
		Sequence:  2,
		CreatedAt: now,
	})

	// Get all logs
	result, err := svc.GetLogs(ctx, task.ID, GetLogsOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)

	// Filter by stream
	result, err = svc.GetLogs(ctx, task.ID, GetLogsOptions{Stream: []string{"stdout"}})
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)
	assert.Equal(t, "stdout", result.Items[0].Stream)

	// Filter by level
	result, err = svc.GetLogs(ctx, task.ID, GetLogsOptions{Level: []string{"error"}})
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)
	assert.Equal(t, "error", result.Items[0].Level)

	// Get logs for non-existent task
	_, err = svc.GetLogs(ctx, "task_nonexistent", GetLogsOptions{})
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockTaskService_StreamLogs(t *testing.T) {
	svc := NewMockTaskService()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create a task
	task, err := svc.Create(ctx, CreateTaskOptions{
		SessionID: "sess_xxx",
		Prompt:    "Test task",
	})
	require.NoError(t, err)

	// Add logs
	now := time.Now()
	for i := 1; i <= 5; i++ {
		svc.AddLog(task.ID, &store.Log{
			ID:        id.Log(),
			TaskID:    task.ID,
			Stream:    "stdout",
			Level:     "info",
			Content:   "Log message",
			Sequence:  int64(i),
			CreatedAt: now,
		})
	}

	// Stream all logs
	ch, err := svc.StreamLogs(ctx, task.ID, StreamLogsOptions{})
	require.NoError(t, err)

	logs := make([]*store.Log, 0, 5)
	for log := range ch {
		logs = append(logs, log)
	}
	assert.Len(t, logs, 5)

	// Stream with tail
	ch, err = svc.StreamLogs(ctx, task.ID, StreamLogsOptions{Tail: 2})
	require.NoError(t, err)

	logs = nil
	for log := range ch {
		logs = append(logs, log)
	}
	assert.Len(t, logs, 2)

	// Stream non-existent task
	_, err = svc.StreamLogs(ctx, "task_nonexistent", StreamLogsOptions{})
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockLogStream(t *testing.T) {
	logs := []*store.Log{
		{ID: "log_1", Content: "First"},
		{ID: "log_2", Content: "Second"},
		{ID: "log_3", Content: "Third"},
	}

	stream := NewMockLogStream(logs)

	// Read all logs
	log, err := stream.Next()
	require.NoError(t, err)
	assert.Equal(t, "log_1", log.ID)

	log, err = stream.Next()
	require.NoError(t, err)
	assert.Equal(t, "log_2", log.ID)

	log, err = stream.Next()
	require.NoError(t, err)
	assert.Equal(t, "log_3", log.ID)

	// EOF after all logs read
	_, err = stream.Next()
	assert.Equal(t, io.EOF, err)

	// Close stream
	err = stream.Close()
	require.NoError(t, err)

	// Read after close returns EOF
	_, err = stream.Next()
	assert.Equal(t, io.EOF, err)
}

func TestMockTaskService_Reset(t *testing.T) {
	svc := NewMockTaskService()
	ctx := context.Background()

	// Create tasks and add logs
	task, err := svc.Create(ctx, CreateTaskOptions{
		SessionID: "sess_xxx",
		Prompt:    "Test task",
	})
	require.NoError(t, err)

	svc.AddLog(task.ID, &store.Log{ID: id.Log()})

	assert.Len(t, svc.GetAllTasks(), 1)

	// Reset
	svc.Reset()
	assert.Len(t, svc.GetAllTasks(), 0)

	// Verify logs are also cleared
	logs, err := svc.GetLogs(ctx, task.ID, GetLogsOptions{})
	assert.ErrorIs(t, err, store.ErrNotFound)
	assert.Nil(t, logs)
}
