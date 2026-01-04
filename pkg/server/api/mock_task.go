package api

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// MockTaskService is a mock implementation of TaskService for testing.
type MockTaskService struct {
	mu    sync.RWMutex
	tasks map[string]*store.Task
	logs  map[string][]*store.Log // taskID -> logs

	// Function stubs for custom behavior
	CreateFunc     func(ctx context.Context, opts CreateTaskOptions) (*store.Task, error)
	GetFunc        func(ctx context.Context, id string) (*store.Task, error)
	ListFunc       func(ctx context.Context, opts ListTasksOptions) (*store.ListResult[store.Task], error)
	ExecuteFunc    func(ctx context.Context, id string) error
	CancelFunc     func(ctx context.Context, id string) error
	RetryFunc      func(ctx context.Context, id string) error
	GetLogsFunc    func(ctx context.Context, taskID string, opts GetLogsOptions) (*store.ListResult[store.Log], error)
	StreamLogsFunc func(ctx context.Context, taskID string, opts StreamLogsOptions) (<-chan *store.Log, error)
}

// NewMockTaskService creates a new MockTaskService with an empty task store.
func NewMockTaskService() *MockTaskService {
	return &MockTaskService{
		tasks: make(map[string]*store.Task),
		logs:  make(map[string][]*store.Log),
	}
}

// Create creates a new task.
func (m *MockTaskService) Create(ctx context.Context, opts CreateTaskOptions) (*store.Task, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, opts)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	task := &store.Task{
		ID:             id.Task(),
		SessionID:      opts.SessionID,
		Prompt:         opts.Prompt,
		Status:         "pending",
		MaxRetries:     opts.MaxRetries,
		TimeoutSeconds: opts.TimeoutSeconds,
		CreatedAt:      now,
		UpdatedAt:      now,
		Labels:         json.RawMessage("{}"),
		Annotations:    json.RawMessage("{}"),
	}

	if task.TimeoutSeconds <= 0 {
		task.TimeoutSeconds = 3600
	}

	m.tasks[task.ID] = task
	return task, nil
}

// Get retrieves a task by ID.
func (m *MockTaskService) Get(ctx context.Context, taskID string) (*store.Task, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, taskID)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return task, nil
}

// List returns tasks matching the filter options.
func (m *MockTaskService) List(ctx context.Context, opts ListTasksOptions) (*store.ListResult[store.Task], error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, opts)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]*store.Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		// Apply filters
		if opts.SessionID != "" && task.SessionID != opts.SessionID {
			continue
		}
		if len(opts.Status) > 0 && !contains(opts.Status, task.Status) {
			continue
		}
		items = append(items, task)
	}

	// Apply limit
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(items) > limit {
		items = items[:limit]
	}

	return &store.ListResult[store.Task]{
		Items:      items,
		TotalCount: int64(len(items)),
		HasMore:    false,
	}, nil
}

// Execute starts execution of a pending task.
func (m *MockTaskService) Execute(ctx context.Context, taskID string) error {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, taskID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return store.ErrNotFound
	}

	if task.Status != "pending" {
		return &InvalidStateError{
			Resource: "task",
			ID:       taskID,
			Current:  task.Status,
			Expected: "pending",
		}
	}

	task.Status = "running"
	task.UpdatedAt = time.Now()
	return nil
}

// Cancel cancels a pending or running task.
func (m *MockTaskService) Cancel(ctx context.Context, taskID string) error {
	if m.CancelFunc != nil {
		return m.CancelFunc(ctx, taskID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return store.ErrNotFound
	}

	if task.Status != "pending" && task.Status != "running" {
		return &InvalidStateError{
			Resource: "task",
			ID:       taskID,
			Current:  task.Status,
			Expected: "pending or running",
		}
	}

	task.Status = "canceled"
	task.UpdatedAt = time.Now()
	return nil
}

// Retry retries a failed task.
func (m *MockTaskService) Retry(ctx context.Context, taskID string) error {
	if m.RetryFunc != nil {
		return m.RetryFunc(ctx, taskID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	task, ok := m.tasks[taskID]
	if !ok {
		return store.ErrNotFound
	}

	if task.Status != "failed" {
		return &InvalidStateError{
			Resource: "task",
			ID:       taskID,
			Current:  task.Status,
			Expected: "failed",
		}
	}

	if task.RetryCount >= task.MaxRetries {
		return &MaxRetriesExceededError{
			TaskID:     taskID,
			RetryCount: task.RetryCount,
			MaxRetries: task.MaxRetries,
		}
	}

	task.Status = "pending"
	task.RetryCount++
	task.UpdatedAt = time.Now()
	return nil
}

// GetLogs returns logs for a task.
func (m *MockTaskService) GetLogs(ctx context.Context, taskID string, opts GetLogsOptions) (*store.ListResult[store.Log], error) {
	if m.GetLogsFunc != nil {
		return m.GetLogsFunc(ctx, taskID, opts)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if task exists
	if _, ok := m.tasks[taskID]; !ok {
		return nil, store.ErrNotFound
	}

	logs := m.logs[taskID]
	items := make([]*store.Log, 0, len(logs))

	for _, log := range logs {
		// Apply filters
		if len(opts.Level) > 0 && !contains(opts.Level, log.Level) {
			continue
		}
		if len(opts.Stream) > 0 && !contains(opts.Stream, log.Stream) {
			continue
		}
		items = append(items, log)
	}

	// Apply limit
	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}
	if len(items) > limit {
		items = items[:limit]
	}

	return &store.ListResult[store.Log]{
		Items:      items,
		TotalCount: int64(len(items)),
		HasMore:    false,
	}, nil
}

// StreamLogs streams logs for a task in real-time.
func (m *MockTaskService) StreamLogs(ctx context.Context, taskID string, opts StreamLogsOptions) (<-chan *store.Log, error) {
	if m.StreamLogsFunc != nil {
		return m.StreamLogsFunc(ctx, taskID, opts)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	// Check if task exists
	if _, ok := m.tasks[taskID]; !ok {
		return nil, store.ErrNotFound
	}

	ch := make(chan *store.Log, 100)

	go func() {
		defer close(ch)

		m.mu.RLock()
		logs := m.logs[taskID]
		m.mu.RUnlock()

		// Send existing logs
		start := 0
		if opts.Tail > 0 && len(logs) > opts.Tail {
			start = len(logs) - opts.Tail
		}

		for i := start; i < len(logs); i++ {
			log := logs[i]
			// Apply filters
			if len(opts.Level) > 0 && !contains(opts.Level, log.Level) {
				continue
			}
			if len(opts.Stream) > 0 && !contains(opts.Stream, log.Stream) {
				continue
			}
			select {
			case ch <- log:
			case <-ctx.Done():
				return
			}
		}
	}()

	return ch, nil
}

// AddTask adds a task directly to the mock store (for testing).
func (m *MockTaskService) AddTask(task *store.Task) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks[task.ID] = task
}

// AddLog adds a log entry for a task (for testing).
func (m *MockTaskService) AddLog(taskID string, log *store.Log) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs[taskID] = append(m.logs[taskID], log)
}

// GetAllTasks returns all tasks in the mock store (for testing).
func (m *MockTaskService) GetAllTasks() []*store.Task {
	m.mu.RLock()
	defer m.mu.RUnlock()

	tasks := make([]*store.Task, 0, len(m.tasks))
	for _, t := range m.tasks {
		tasks = append(tasks, t)
	}
	return tasks
}

// Reset clears all tasks and logs from the mock store.
func (m *MockTaskService) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tasks = make(map[string]*store.Task)
	m.logs = make(map[string][]*store.Log)
}

// MockLogStream is a mock implementation of LogStream for testing.
type MockLogStream struct {
	logs    []*store.Log
	index   int
	mu      sync.Mutex
	closed  bool
	closeCh chan struct{}
}

// NewMockLogStream creates a new MockLogStream with the given logs.
func NewMockLogStream(logs []*store.Log) *MockLogStream {
	return &MockLogStream{
		logs:    logs,
		closeCh: make(chan struct{}),
	}
}

// Next returns the next log entry.
func (s *MockLogStream) Next() (*store.Log, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil, io.EOF
	}

	if s.index >= len(s.logs) {
		return nil, io.EOF
	}

	log := s.logs[s.index]
	s.index++
	return log, nil
}

// Close closes the stream.
func (s *MockLogStream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.closed {
		s.closed = true
		close(s.closeCh)
	}
	return nil
}

// Verify MockTaskService implements TaskService.
var _ TaskService = (*MockTaskService)(nil)

// Verify MockLogStream implements LogStream.
var _ LogStream = (*MockLogStream)(nil)
