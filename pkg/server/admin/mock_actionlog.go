package admin

import (
	"context"
	"sync"

	"github.com/chunlea/marionette/pkg/store"
)

// MockActionLogService is a mock implementation of ActionLogService for testing.
type MockActionLogService struct {
	mu            sync.RWMutex
	logs          map[string]*store.ActionLog
	internalError error
}

// NewMockActionLogService creates a new mock action log service.
func NewMockActionLogService() *MockActionLogService {
	return &MockActionLogService{
		logs: make(map[string]*store.ActionLog),
	}
}

// Get retrieves an action log by ID.
func (m *MockActionLogService) Get(_ context.Context, id string) (*store.ActionLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		return nil, m.internalError
	}

	log, ok := m.logs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return log, nil
}

// List returns action logs matching the given options.
func (m *MockActionLogService) List(_ context.Context, opts ListActionLogsOptions) (*ListResult[store.ActionLog], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		return nil, m.internalError
	}

	items := make([]*store.ActionLog, 0, len(m.logs))
	for _, log := range m.logs {
		items = append(items, log)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}

	hasMore := false
	if len(items) > limit {
		items = items[:limit]
		hasMore = true
	}

	return &ListResult[store.ActionLog]{
		Items:      items,
		TotalCount: int64(len(m.logs)),
		NextCursor: func() string {
			if hasMore && len(items) > 0 {
				return items[len(items)-1].ID
			}
			return ""
		}(),
	}, nil
}

// AddLog adds a log entry for testing.
func (m *MockActionLogService) AddLog(log *store.ActionLog) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs[log.ID] = log
}

// SetInternalError sets an error to be returned by subsequent calls.
func (m *MockActionLogService) SetInternalError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = err
}

// Reset clears all logs and errors.
func (m *MockActionLogService) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.logs = make(map[string]*store.ActionLog)
	m.internalError = nil
}
