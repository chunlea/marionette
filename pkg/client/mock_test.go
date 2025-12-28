package client

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockClient_Sessions(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateSession", func(t *testing.T) {
		client := &MockClient{}
		expectedSession := &Session{ID: "sess_test123"}
		client.CreateSessionFunc = func(ctx context.Context, opts CreateSessionOptions) (*Session, error) {
			return expectedSession, nil
		}

		session, err := client.CreateSession(ctx, CreateSessionOptions{Agent: "claude"})
		require.NoError(t, err)
		assert.Equal(t, expectedSession.ID, session.ID)

		// Verify call was recorded
		calls := client.GetCalls()
		assert.Len(t, calls, 1)
		assert.Equal(t, "CreateSession", calls[0].Method)
	})

	t.Run("CreateSession error", func(t *testing.T) {
		client := &MockClient{}
		expectedErr := errors.New("create failed")
		client.CreateSessionFunc = func(ctx context.Context, opts CreateSessionOptions) (*Session, error) {
			return nil, expectedErr
		}

		_, err := client.CreateSession(ctx, CreateSessionOptions{})
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("CreateSession nil func returns ErrNotFound", func(t *testing.T) {
		client := &MockClient{}
		_, err := client.CreateSession(ctx, CreateSessionOptions{})
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("GetSession", func(t *testing.T) {
		client := &MockClient{}
		expectedSession := &Session{ID: "sess_test456"}
		client.GetSessionFunc = func(ctx context.Context, id string) (*Session, error) {
			assert.Equal(t, "sess_test456", id)
			return expectedSession, nil
		}

		session, err := client.GetSession(ctx, "sess_test456")
		require.NoError(t, err)
		assert.Equal(t, expectedSession.ID, session.ID)
	})

	t.Run("GetSession nil func returns ErrNotFound", func(t *testing.T) {
		client := &MockClient{}
		_, err := client.GetSession(ctx, "sess_xxx")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("ListSessions", func(t *testing.T) {
		client := &MockClient{}
		expectedResult := &ListResult[Session]{
			Items:      []*Session{{ID: "sess_1"}, {ID: "sess_2"}},
			TotalCount: 2,
		}
		client.ListSessionsFunc = func(ctx context.Context, opts ListSessionsOptions) (*ListResult[Session], error) {
			return expectedResult, nil
		}

		result, err := client.ListSessions(ctx, ListSessionsOptions{})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
	})

	t.Run("ListSessions nil func returns empty list", func(t *testing.T) {
		client := &MockClient{}
		result, err := client.ListSessions(ctx, ListSessionsOptions{})
		require.NoError(t, err)
		assert.Empty(t, result.Items)
	})

	t.Run("SuspendSession", func(t *testing.T) {
		client := &MockClient{}
		client.SuspendSessionFunc = func(ctx context.Context, id string) error {
			assert.Equal(t, "sess_suspend", id)
			return nil
		}

		err := client.SuspendSession(ctx, "sess_suspend")
		assert.NoError(t, err)
	})

	t.Run("SuspendSession nil func returns nil", func(t *testing.T) {
		client := &MockClient{}
		err := client.SuspendSession(ctx, "sess_xxx")
		assert.NoError(t, err)
	})

	t.Run("ResumeSession", func(t *testing.T) {
		client := &MockClient{}
		client.ResumeSessionFunc = func(ctx context.Context, id string) error {
			assert.Equal(t, "sess_resume", id)
			return nil
		}

		err := client.ResumeSession(ctx, "sess_resume")
		assert.NoError(t, err)
	})

	t.Run("ResumeSession nil func returns nil", func(t *testing.T) {
		client := &MockClient{}
		err := client.ResumeSession(ctx, "sess_xxx")
		assert.NoError(t, err)
	})

	t.Run("TerminateSession", func(t *testing.T) {
		client := &MockClient{}
		client.TerminateSessionFunc = func(ctx context.Context, id string) error {
			assert.Equal(t, "sess_terminate", id)
			return nil
		}

		err := client.TerminateSession(ctx, "sess_terminate")
		assert.NoError(t, err)
	})

	t.Run("TerminateSession nil func returns nil", func(t *testing.T) {
		client := &MockClient{}
		err := client.TerminateSession(ctx, "sess_xxx")
		assert.NoError(t, err)
	})
}

func TestMockClient_Tasks(t *testing.T) {
	ctx := context.Background()

	t.Run("CreateTask", func(t *testing.T) {
		client := &MockClient{}
		expectedTask := &Task{ID: "task_test123"}
		client.CreateTaskFunc = func(ctx context.Context, opts CreateTaskOptions) (*Task, error) {
			assert.Equal(t, "sess_xxx", opts.SessionID)
			assert.Equal(t, "Build an API", opts.Prompt)
			return expectedTask, nil
		}

		task, err := client.CreateTask(ctx, CreateTaskOptions{
			SessionID: "sess_xxx",
			Prompt:    "Build an API",
		})
		require.NoError(t, err)
		assert.Equal(t, expectedTask.ID, task.ID)
	})

	t.Run("CreateTask nil func returns ErrNotFound", func(t *testing.T) {
		client := &MockClient{}
		_, err := client.CreateTask(ctx, CreateTaskOptions{})
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("GetTask", func(t *testing.T) {
		client := &MockClient{}
		expectedTask := &Task{ID: "task_get123"}
		client.GetTaskFunc = func(ctx context.Context, id string) (*Task, error) {
			return expectedTask, nil
		}

		task, err := client.GetTask(ctx, "task_get123")
		require.NoError(t, err)
		assert.Equal(t, expectedTask.ID, task.ID)
	})

	t.Run("GetTask nil func returns ErrNotFound", func(t *testing.T) {
		client := &MockClient{}
		_, err := client.GetTask(ctx, "task_xxx")
		assert.ErrorIs(t, err, ErrNotFound)
	})

	t.Run("ListTasks", func(t *testing.T) {
		client := &MockClient{}
		expectedResult := &ListResult[Task]{
			Items:      []*Task{{ID: "task_1"}, {ID: "task_2"}},
			TotalCount: 2,
		}
		client.ListTasksFunc = func(ctx context.Context, opts ListTasksOptions) (*ListResult[Task], error) {
			return expectedResult, nil
		}

		result, err := client.ListTasks(ctx, ListTasksOptions{})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
	})

	t.Run("ListTasks nil func returns empty list", func(t *testing.T) {
		client := &MockClient{}
		result, err := client.ListTasks(ctx, ListTasksOptions{})
		require.NoError(t, err)
		assert.Empty(t, result.Items)
	})

	t.Run("CancelTask", func(t *testing.T) {
		client := &MockClient{}
		client.CancelTaskFunc = func(ctx context.Context, id string) error {
			assert.Equal(t, "task_cancel", id)
			return nil
		}

		err := client.CancelTask(ctx, "task_cancel")
		assert.NoError(t, err)
	})

	t.Run("CancelTask nil func returns nil", func(t *testing.T) {
		client := &MockClient{}
		err := client.CancelTask(ctx, "task_xxx")
		assert.NoError(t, err)
	})

	t.Run("GetTaskLogs", func(t *testing.T) {
		client := &MockClient{}
		logs := []*Log{
			{ID: "log_1", Content: []byte("First log")},
			{ID: "log_2", Content: []byte("Second log")},
		}
		client.GetTaskLogsFunc = func(ctx context.Context, id string, opts GetLogsOptions) (LogIterator, error) {
			return &MockLogIterator{Logs: logs}, nil
		}

		iter, err := client.GetTaskLogs(ctx, "task_logs", GetLogsOptions{})
		require.NoError(t, err)

		// Read all logs
		log1, err := iter.Next()
		require.NoError(t, err)
		assert.Equal(t, "log_1", log1.ID)

		log2, err := iter.Next()
		require.NoError(t, err)
		assert.Equal(t, "log_2", log2.ID)

		// Should return EOF at end
		_, err = iter.Next()
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("GetTaskLogs nil func returns empty iterator", func(t *testing.T) {
		client := &MockClient{}
		iter, err := client.GetTaskLogs(ctx, "task_xxx", GetLogsOptions{})
		require.NoError(t, err)

		// Empty iterator returns EOF immediately
		_, err = iter.Next()
		assert.ErrorIs(t, err, io.EOF)
	})
}

func TestMockClient_CallTracking(t *testing.T) {
	ctx := context.Background()
	client := &MockClient{}

	client.CreateSessionFunc = func(ctx context.Context, opts CreateSessionOptions) (*Session, error) {
		return &Session{ID: "sess_1"}, nil
	}
	client.GetSessionFunc = func(ctx context.Context, id string) (*Session, error) {
		return &Session{ID: id}, nil
	}

	// Make several calls
	_, _ = client.CreateSession(ctx, CreateSessionOptions{Agent: "claude"})
	_, _ = client.CreateSession(ctx, CreateSessionOptions{Agent: "codex"})
	_, _ = client.GetSession(ctx, "sess_1")

	// Verify all calls recorded
	calls := client.GetCalls()
	assert.Len(t, calls, 3)

	// Verify call details
	assert.Equal(t, "CreateSession", calls[0].Method)
	assert.Equal(t, CreateSessionOptions{Agent: "claude"}, calls[0].Args[0])

	assert.Equal(t, "CreateSession", calls[1].Method)
	assert.Equal(t, CreateSessionOptions{Agent: "codex"}, calls[1].Args[0])

	assert.Equal(t, "GetSession", calls[2].Method)
	assert.Equal(t, "sess_1", calls[2].Args[0])

	// Reset and verify
	client.Reset()
	assert.Len(t, client.GetCalls(), 0)
}

func TestMockLogIterator(t *testing.T) {
	t.Run("iterate all logs", func(t *testing.T) {
		logs := []*Log{
			{ID: "log_1", Content: []byte("First")},
			{ID: "log_2", Content: []byte("Second")},
			{ID: "log_3", Content: []byte("Third")},
		}
		iter := &MockLogIterator{Logs: logs}

		// Read all logs
		for i, expected := range logs {
			log, err := iter.Next()
			require.NoError(t, err, "iteration %d", i)
			assert.Equal(t, expected.ID, log.ID)
		}

		// Should return EOF after all logs consumed
		_, err := iter.Next()
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("empty iterator", func(t *testing.T) {
		iter := &MockLogIterator{}

		_, err := iter.Next()
		assert.ErrorIs(t, err, io.EOF)
	})

	t.Run("close iterator", func(t *testing.T) {
		logs := []*Log{{ID: "log_1"}, {ID: "log_2"}}
		iter := &MockLogIterator{Logs: logs}

		// Read one log
		_, _ = iter.Next()

		// Close returns nil
		err := iter.Close()
		assert.NoError(t, err)
	})
}
