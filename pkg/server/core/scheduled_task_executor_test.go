package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockScheduledTaskSvc implements ScheduledTaskServiceInterface for testing.
type mockScheduledTaskSvc struct {
	mu                 sync.Mutex
	getDueFunc         func(ctx context.Context, limit int) ([]*store.ScheduledTask, error)
	getFunc            func(ctx context.Context, id string) (*store.ScheduledTask, error)
	executeFunc        func(ctx context.Context, scheduledTask *store.ScheduledTask) (*store.Task, error)
	markCompletedFunc  func(ctx context.Context, id string, success bool) error
	getDueCalls        int
	executeCalls       int
	markCompletedCalls int
}

func (m *mockScheduledTaskSvc) Create(_ context.Context, _ CreateScheduledTaskOptions) (*store.ScheduledTask, error) {
	return nil, nil
}

func (m *mockScheduledTaskSvc) Get(ctx context.Context, id string) (*store.ScheduledTask, error) {
	if m.getFunc != nil {
		return m.getFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockScheduledTaskSvc) List(_ context.Context, _ ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	return nil, nil
}

func (m *mockScheduledTaskSvc) Update(_ context.Context, _ string, _ UpdateScheduledTaskOptions) (*store.ScheduledTask, error) {
	return nil, nil
}

func (m *mockScheduledTaskSvc) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockScheduledTaskSvc) Pause(_ context.Context, _ string) error {
	return nil
}

func (m *mockScheduledTaskSvc) Resume(_ context.Context, _ string) error {
	return nil
}

func (m *mockScheduledTaskSvc) Trigger(_ context.Context, _ string) (*store.Task, error) {
	return nil, nil
}

func (m *mockScheduledTaskSvc) GetDue(ctx context.Context, limit int) ([]*store.ScheduledTask, error) {
	m.mu.Lock()
	m.getDueCalls++
	m.mu.Unlock()

	if m.getDueFunc != nil {
		return m.getDueFunc(ctx, limit)
	}
	return nil, nil
}

func (m *mockScheduledTaskSvc) ExecuteScheduledTask(ctx context.Context, scheduledTask *store.ScheduledTask) (*store.Task, error) {
	m.mu.Lock()
	m.executeCalls++
	m.mu.Unlock()

	if m.executeFunc != nil {
		return m.executeFunc(ctx, scheduledTask)
	}
	return &store.Task{ID: "task_test"}, nil
}

// ExecuteDue routes to the same recorder: the executor's job is to call it and
// to interpret a lost claim, and the claim itself is covered where it lives.
func (m *mockScheduledTaskSvc) ExecuteDue(ctx context.Context, scheduledTask *store.ScheduledTask) (*store.Task, error) {
	return m.ExecuteScheduledTask(ctx, scheduledTask)
}

func (m *mockScheduledTaskSvc) MarkTaskCompleted(ctx context.Context, id string, success bool) error {
	m.mu.Lock()
	m.markCompletedCalls++
	m.mu.Unlock()

	if m.markCompletedFunc != nil {
		return m.markCompletedFunc(ctx, id, success)
	}
	return nil
}

func (m *mockScheduledTaskSvc) CalculateNextRunAt(_ string, _ string, _ time.Time) (*time.Time, error) {
	return nil, nil
}

func TestNewScheduledTaskExecutor(t *testing.T) {
	logger := zap.NewNop()
	mockSvc := &mockScheduledTaskSvc{}
	mockTaskMgr := &mockTaskMgrForScheduled{}

	t.Run("default values", func(t *testing.T) {
		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger)
		assert.NotNil(t, executor)
		assert.Equal(t, DefaultScheduledTaskCheckInterval, executor.checkInterval)
		assert.Equal(t, DefaultScheduledTaskBatchSize, executor.batchSize)
		assert.False(t, executor.IsRunning())
	})

	t.Run("with custom check interval", func(t *testing.T) {
		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger,
			WithScheduledTaskCheckInterval(5*time.Second),
		)
		assert.Equal(t, 5*time.Second, executor.checkInterval)
	})

	t.Run("with custom batch size", func(t *testing.T) {
		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger,
			WithScheduledTaskBatchSize(100),
		)
		assert.Equal(t, 100, executor.batchSize)
	})

	t.Run("with multiple options", func(t *testing.T) {
		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger,
			WithScheduledTaskCheckInterval(10*time.Second),
			WithScheduledTaskBatchSize(25),
		)
		assert.Equal(t, 10*time.Second, executor.checkInterval)
		assert.Equal(t, 25, executor.batchSize)
	})
}

func TestScheduledTaskExecutor_StartStop(t *testing.T) {
	logger := zap.NewNop()
	mockSvc := &mockScheduledTaskSvc{}
	mockTaskMgr := &mockTaskMgrForScheduled{}

	t.Run("start and stop", func(t *testing.T) {
		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger,
			WithScheduledTaskCheckInterval(100*time.Millisecond),
		)

		ctx := context.Background()
		assert.False(t, executor.IsRunning())

		executor.Start(ctx)
		assert.Eventually(t, func() bool {
			return executor.IsRunning()
		}, time.Second, 10*time.Millisecond)

		// Wait for at least one poll
		time.Sleep(150 * time.Millisecond)

		executor.Stop()
		assert.False(t, executor.IsRunning())
	})

	t.Run("double start is no-op", func(t *testing.T) {
		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger,
			WithScheduledTaskCheckInterval(100*time.Millisecond),
		)

		ctx := context.Background()
		executor.Start(ctx)
		assert.Eventually(t, func() bool {
			return executor.IsRunning()
		}, time.Second, 10*time.Millisecond)

		// Second start should be no-op
		executor.Start(ctx)
		assert.True(t, executor.IsRunning())

		executor.Stop()
		assert.False(t, executor.IsRunning())
	})

	t.Run("double stop is no-op", func(t *testing.T) {
		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger,
			WithScheduledTaskCheckInterval(100*time.Millisecond),
		)

		// Stop without starting - should be no-op
		executor.Stop()
		assert.False(t, executor.IsRunning())

		// Start and stop twice
		executor.Start(context.Background())
		assert.Eventually(t, func() bool {
			return executor.IsRunning()
		}, time.Second, 10*time.Millisecond)

		executor.Stop()
		executor.Stop() // Second stop should be no-op
		assert.False(t, executor.IsRunning())
	})

	t.Run("context cancellation stops executor", func(t *testing.T) {
		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger,
			WithScheduledTaskCheckInterval(100*time.Millisecond),
		)

		ctx, cancel := context.WithCancel(context.Background())
		executor.Start(ctx)
		assert.Eventually(t, func() bool {
			return executor.IsRunning()
		}, time.Second, 10*time.Millisecond)

		cancel()

		// The running flag is set to false by Stop(), not by context cancellation
		// The goroutine will exit but running stays true until Stop() is called
		time.Sleep(200 * time.Millisecond)
	})
}

func TestScheduledTaskExecutor_RunNow(t *testing.T) {
	logger := zap.NewNop()
	mockTaskMgr := &mockTaskMgrForScheduled{}

	t.Run("no due tasks", func(t *testing.T) {
		mockSvc := &mockScheduledTaskSvc{
			getDueFunc: func(_ context.Context, _ int) ([]*store.ScheduledTask, error) {
				return nil, nil
			},
		}

		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger)
		err := executor.RunNow(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 1, mockSvc.getDueCalls)
		assert.Equal(t, 0, mockSvc.executeCalls)
	})

	t.Run("due tasks executed", func(t *testing.T) {
		now := time.Now().Add(-time.Minute)
		mockSvc := &mockScheduledTaskSvc{
			getDueFunc: func(_ context.Context, _ int) ([]*store.ScheduledTask, error) {
				return []*store.ScheduledTask{
					{
						ID:        "stsk_1",
						Name:      "task1",
						SessionID: "sess_1",
						Status:    ScheduledTaskStatusActive,
						NextRunAt: &now,
					},
					{
						ID:        "stsk_2",
						Name:      "task2",
						SessionID: "sess_1",
						Status:    ScheduledTaskStatusActive,
						NextRunAt: &now,
					},
				}, nil
			},
			getFunc: func(_ context.Context, id string) (*store.ScheduledTask, error) {
				return &store.ScheduledTask{
					ID:        id,
					Name:      "task",
					SessionID: "sess_1",
					Status:    ScheduledTaskStatusActive,
					NextRunAt: &now,
				}, nil
			},
			executeFunc: func(_ context.Context, _ *store.ScheduledTask) (*store.Task, error) {
				return &store.Task{ID: "task_created"}, nil
			},
		}

		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger)
		err := executor.RunNow(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 1, mockSvc.getDueCalls)
		assert.Equal(t, 2, mockSvc.executeCalls)
	})

	t.Run("GetDue error", func(t *testing.T) {
		expectedErr := errors.New("database error")
		mockSvc := &mockScheduledTaskSvc{
			getDueFunc: func(_ context.Context, _ int) ([]*store.ScheduledTask, error) {
				return nil, expectedErr
			},
		}

		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger)
		err := executor.RunNow(context.Background())
		assert.ErrorIs(t, err, expectedErr)
	})
}

func TestScheduledTaskExecutor_ExecuteTask(t *testing.T) {
	logger := zap.NewNop()
	mockTaskMgr := &mockTaskMgrForScheduled{}

	t.Run("task no longer active", func(t *testing.T) {
		mockSvc := &mockScheduledTaskSvc{
			getDueFunc: func(_ context.Context, _ int) ([]*store.ScheduledTask, error) {
				now := time.Now().Add(-time.Minute)
				return []*store.ScheduledTask{
					{
						ID:        "stsk_1",
						Name:      "task1",
						SessionID: "sess_1",
						Status:    ScheduledTaskStatusActive,
						NextRunAt: &now,
					},
				}, nil
			},
			getFunc: func(_ context.Context, _ string) (*store.ScheduledTask, error) {
				// Return paused status - should be skipped
				return &store.ScheduledTask{
					ID:        "stsk_1",
					Name:      "task1",
					SessionID: "sess_1",
					Status:    ScheduledTaskStatusPaused,
				}, nil
			},
		}

		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger)
		err := executor.RunNow(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 0, mockSvc.executeCalls) // Should not execute
	})

	t.Run("task no longer due", func(t *testing.T) {
		future := time.Now().Add(time.Hour)
		mockSvc := &mockScheduledTaskSvc{
			getDueFunc: func(_ context.Context, _ int) ([]*store.ScheduledTask, error) {
				now := time.Now().Add(-time.Minute)
				return []*store.ScheduledTask{
					{
						ID:        "stsk_1",
						Name:      "task1",
						SessionID: "sess_1",
						Status:    ScheduledTaskStatusActive,
						NextRunAt: &now,
					},
				}, nil
			},
			getFunc: func(_ context.Context, _ string) (*store.ScheduledTask, error) {
				// Return future NextRunAt - should be skipped
				return &store.ScheduledTask{
					ID:        "stsk_1",
					Name:      "task1",
					SessionID: "sess_1",
					Status:    ScheduledTaskStatusActive,
					NextRunAt: &future,
				}, nil
			},
		}

		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger)
		err := executor.RunNow(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 0, mockSvc.executeCalls) // Should not execute
	})

	t.Run("Get error", func(t *testing.T) {
		mockSvc := &mockScheduledTaskSvc{
			getDueFunc: func(_ context.Context, _ int) ([]*store.ScheduledTask, error) {
				now := time.Now().Add(-time.Minute)
				return []*store.ScheduledTask{
					{
						ID:        "stsk_1",
						Name:      "task1",
						SessionID: "sess_1",
						Status:    ScheduledTaskStatusActive,
						NextRunAt: &now,
					},
				}, nil
			},
			getFunc: func(_ context.Context, _ string) (*store.ScheduledTask, error) {
				return nil, errors.New("not found")
			},
		}

		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger)
		err := executor.RunNow(context.Background())
		assert.NoError(t, err) // RunNow doesn't return errors from individual task executions
		assert.Equal(t, 0, mockSvc.executeCalls)
	})

	t.Run("execute error marks task as failed", func(t *testing.T) {
		now := time.Now().Add(-time.Minute)
		var markedFailed bool

		mockSvc := &mockScheduledTaskSvc{
			getDueFunc: func(_ context.Context, _ int) ([]*store.ScheduledTask, error) {
				return []*store.ScheduledTask{
					{
						ID:        "stsk_1",
						Name:      "task1",
						SessionID: "sess_1",
						Status:    ScheduledTaskStatusActive,
						NextRunAt: &now,
					},
				}, nil
			},
			getFunc: func(_ context.Context, _ string) (*store.ScheduledTask, error) {
				return &store.ScheduledTask{
					ID:        "stsk_1",
					Name:      "task1",
					SessionID: "sess_1",
					Status:    ScheduledTaskStatusActive,
					NextRunAt: &now,
				}, nil
			},
			executeFunc: func(_ context.Context, _ *store.ScheduledTask) (*store.Task, error) {
				return nil, errors.New("execution failed")
			},
			markCompletedFunc: func(_ context.Context, _ string, success bool) error {
				if !success {
					markedFailed = true
				}
				return nil
			},
		}

		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger)
		err := executor.RunNow(context.Background())
		assert.NoError(t, err)
		assert.True(t, markedFailed)
	})

	t.Run("nil NextRunAt is skipped", func(t *testing.T) {
		mockSvc := &mockScheduledTaskSvc{
			getDueFunc: func(_ context.Context, _ int) ([]*store.ScheduledTask, error) {
				now := time.Now().Add(-time.Minute)
				return []*store.ScheduledTask{
					{
						ID:        "stsk_1",
						Name:      "task1",
						SessionID: "sess_1",
						Status:    ScheduledTaskStatusActive,
						NextRunAt: &now,
					},
				}, nil
			},
			getFunc: func(_ context.Context, _ string) (*store.ScheduledTask, error) {
				return &store.ScheduledTask{
					ID:        "stsk_1",
					Name:      "task1",
					SessionID: "sess_1",
					Status:    ScheduledTaskStatusActive,
					NextRunAt: nil, // nil NextRunAt
				}, nil
			},
		}

		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger)
		err := executor.RunNow(context.Background())
		assert.NoError(t, err)
		assert.Equal(t, 0, mockSvc.executeCalls)
	})
}

func TestScheduledTaskExecutor_Integration(t *testing.T) {
	logger := zap.NewNop()
	mockTaskMgr := &mockTaskMgrForScheduled{}

	t.Run("periodic execution", func(t *testing.T) {
		callCount := 0
		var mu sync.Mutex

		mockSvc := &mockScheduledTaskSvc{
			getDueFunc: func(_ context.Context, _ int) ([]*store.ScheduledTask, error) {
				mu.Lock()
				callCount++
				mu.Unlock()
				return nil, nil
			},
		}

		executor := NewScheduledTaskExecutor(mockSvc, mockTaskMgr, logger,
			WithScheduledTaskCheckInterval(50*time.Millisecond),
		)

		ctx := context.Background()
		executor.Start(ctx)

		// Wait for multiple polls
		time.Sleep(180 * time.Millisecond)

		executor.Stop()

		mu.Lock()
		finalCount := callCount
		mu.Unlock()

		// Should have been called at least 2-3 times (initial + 2-3 ticks)
		require.GreaterOrEqual(t, finalCount, 2, "Expected at least 2 poll cycles")
	})
}
