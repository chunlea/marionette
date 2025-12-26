package client

import (
	"context"
	"io"
	"sync"
)

// MockClient is a mock implementation of Client for testing.
type MockClient struct {
	mu sync.Mutex

	// Calls tracks all method calls for assertions.
	Calls []MockCall

	// Function stubs - set these to customize behavior
	CreateSessionFunc    func(ctx context.Context, opts CreateSessionOptions) (*Session, error)
	GetSessionFunc       func(ctx context.Context, id string) (*Session, error)
	ListSessionsFunc     func(ctx context.Context, opts ListSessionsOptions) (*ListResult[Session], error)
	SuspendSessionFunc   func(ctx context.Context, id string) error
	ResumeSessionFunc    func(ctx context.Context, id string) error
	TerminateSessionFunc func(ctx context.Context, id string) error

	CreateTaskFunc  func(ctx context.Context, opts CreateTaskOptions) (*Task, error)
	GetTaskFunc     func(ctx context.Context, id string) (*Task, error)
	ListTasksFunc   func(ctx context.Context, opts ListTasksOptions) (*ListResult[Task], error)
	CancelTaskFunc  func(ctx context.Context, id string) error
	GetTaskLogsFunc func(ctx context.Context, id string, opts GetLogsOptions) (LogIterator, error)
}

// MockCall represents a recorded method call.
type MockCall struct {
	Method string
	Args   []any
}

// Ensure MockClient implements Client.
var _ Client = (*MockClient)(nil)

// recordCall records a method call for later assertions.
func (m *MockClient) recordCall(method string, args ...any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = append(m.Calls, MockCall{Method: method, Args: args})
}

// Reset clears all recorded calls.
func (m *MockClient) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Calls = nil
}

// GetCalls returns a copy of recorded calls.
func (m *MockClient) GetCalls() []MockCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	calls := make([]MockCall, len(m.Calls))
	copy(calls, m.Calls)
	return calls
}

// CreateSession implements Client.
func (m *MockClient) CreateSession(ctx context.Context, opts CreateSessionOptions) (*Session, error) {
	m.recordCall("CreateSession", opts)
	if m.CreateSessionFunc != nil {
		return m.CreateSessionFunc(ctx, opts)
	}
	return nil, ErrNotFound
}

// GetSession implements Client.
func (m *MockClient) GetSession(ctx context.Context, id string) (*Session, error) {
	m.recordCall("GetSession", id)
	if m.GetSessionFunc != nil {
		return m.GetSessionFunc(ctx, id)
	}
	return nil, ErrNotFound
}

// ListSessions implements Client.
func (m *MockClient) ListSessions(ctx context.Context, opts ListSessionsOptions) (*ListResult[Session], error) {
	m.recordCall("ListSessions", opts)
	if m.ListSessionsFunc != nil {
		return m.ListSessionsFunc(ctx, opts)
	}
	return &ListResult[Session]{Items: []*Session{}}, nil
}

// SuspendSession implements Client.
func (m *MockClient) SuspendSession(ctx context.Context, id string) error {
	m.recordCall("SuspendSession", id)
	if m.SuspendSessionFunc != nil {
		return m.SuspendSessionFunc(ctx, id)
	}
	return nil
}

// ResumeSession implements Client.
func (m *MockClient) ResumeSession(ctx context.Context, id string) error {
	m.recordCall("ResumeSession", id)
	if m.ResumeSessionFunc != nil {
		return m.ResumeSessionFunc(ctx, id)
	}
	return nil
}

// TerminateSession implements Client.
func (m *MockClient) TerminateSession(ctx context.Context, id string) error {
	m.recordCall("TerminateSession", id)
	if m.TerminateSessionFunc != nil {
		return m.TerminateSessionFunc(ctx, id)
	}
	return nil
}

// CreateTask implements Client.
func (m *MockClient) CreateTask(ctx context.Context, opts CreateTaskOptions) (*Task, error) {
	m.recordCall("CreateTask", opts)
	if m.CreateTaskFunc != nil {
		return m.CreateTaskFunc(ctx, opts)
	}
	return nil, ErrNotFound
}

// GetTask implements Client.
func (m *MockClient) GetTask(ctx context.Context, id string) (*Task, error) {
	m.recordCall("GetTask", id)
	if m.GetTaskFunc != nil {
		return m.GetTaskFunc(ctx, id)
	}
	return nil, ErrNotFound
}

// ListTasks implements Client.
func (m *MockClient) ListTasks(ctx context.Context, opts ListTasksOptions) (*ListResult[Task], error) {
	m.recordCall("ListTasks", opts)
	if m.ListTasksFunc != nil {
		return m.ListTasksFunc(ctx, opts)
	}
	return &ListResult[Task]{Items: []*Task{}}, nil
}

// CancelTask implements Client.
func (m *MockClient) CancelTask(ctx context.Context, id string) error {
	m.recordCall("CancelTask", id)
	if m.CancelTaskFunc != nil {
		return m.CancelTaskFunc(ctx, id)
	}
	return nil
}

// GetTaskLogs implements Client.
func (m *MockClient) GetTaskLogs(ctx context.Context, id string, opts GetLogsOptions) (LogIterator, error) {
	m.recordCall("GetTaskLogs", id, opts)
	if m.GetTaskLogsFunc != nil {
		return m.GetTaskLogsFunc(ctx, id, opts)
	}
	return &MockLogIterator{}, nil
}

// MockLogIterator is a mock implementation of LogIterator.
type MockLogIterator struct {
	Logs  []*Log
	index int
}

// Next implements LogIterator.
func (m *MockLogIterator) Next() (*Log, error) {
	if m.index >= len(m.Logs) {
		return nil, io.EOF
	}
	log := m.Logs[m.index]
	m.index++
	return log, nil
}

// Close implements LogIterator.
func (m *MockLogIterator) Close() error {
	return nil
}
