package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// MockSessionService is a mock implementation of SessionService for testing.
type MockSessionService struct {
	mu       sync.RWMutex
	sessions map[string]*store.Session

	// Function stubs for custom behavior
	CreateFunc    func(ctx context.Context, opts CreateSessionOptions) (*store.Session, error)
	GetFunc       func(ctx context.Context, id string) (*store.Session, error)
	ListFunc      func(ctx context.Context, opts ListSessionsOptions) (*store.ListResult[store.Session], error)
	SuspendFunc   func(ctx context.Context, id string) error
	ResumeFunc    func(ctx context.Context, id string) error
	TerminateFunc func(ctx context.Context, id string) error
	GetLogsFunc   func(ctx context.Context, id string, opts GetLogsOptions) (*store.ListResult[store.Log], error)
}

// GetLogs returns the session's logs.
func (m *MockSessionService) GetLogs(
	ctx context.Context, sessionID string, opts GetLogsOptions,
) (*store.ListResult[store.Log], error) {
	if m.GetLogsFunc != nil {
		return m.GetLogsFunc(ctx, sessionID, opts)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	if _, ok := m.sessions[sessionID]; !ok {
		return nil, store.ErrNotFound
	}
	return &store.ListResult[store.Log]{}, nil
}

// NewMockSessionService creates a new MockSessionService with an empty session store.
func NewMockSessionService() *MockSessionService {
	return &MockSessionService{
		sessions: make(map[string]*store.Session),
	}
}

// Create creates a new session.
func (m *MockSessionService) Create(ctx context.Context, opts CreateSessionOptions) (*store.Session, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, opts)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	session := &store.Session{
		ID:            id.Session(),
		Status:        "pending",
		Agent:         opts.Agent,
		WorkspaceID:   id.Workspace(),
		IsBYOK:        opts.APIKey != "",
		NetworkPolicy: opts.NetworkPolicy,
		AllowedHosts:  opts.AllowedHosts,
		LifecycleMode: opts.LifecycleMode,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if opts.Name != "" {
		session.Name = &opts.Name
	}

	if opts.AgentConfigID != "" {
		session.AgentConfigID = &opts.AgentConfigID
	}

	if opts.IdleTimeoutSeconds > 0 {
		session.IdleTimeoutSeconds = &opts.IdleTimeoutSeconds
	}

	if opts.NetworkPolicy == "" {
		session.NetworkPolicy = "allow_list"
	}

	if opts.LifecycleMode == "" {
		session.LifecycleMode = "on_demand"
	}

	// Initialize labels
	if opts.Labels != nil {
		labelsJSON, _ := json.Marshal(opts.Labels)
		session.Labels = labelsJSON
	} else {
		session.Labels = json.RawMessage("{}")
	}
	session.Annotations = json.RawMessage("{}")

	m.sessions[session.ID] = session
	return session, nil
}

// Get retrieves a session by ID.
func (m *MockSessionService) Get(ctx context.Context, sessionID string) (*store.Session, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, sessionID)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return session, nil
}

// List returns sessions matching the filter options.
func (m *MockSessionService) List(ctx context.Context, opts ListSessionsOptions) (*store.ListResult[store.Session], error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, opts)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]*store.Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		// Apply filters
		if len(opts.Status) > 0 && !contains(opts.Status, session.Status) {
			continue
		}
		if opts.Agent != "" && session.Agent != opts.Agent {
			continue
		}
		if opts.LifecycleMode != "" && session.LifecycleMode != opts.LifecycleMode {
			continue
		}
		items = append(items, session)
	}

	// Apply limit
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(items) > limit {
		items = items[:limit]
	}

	return &store.ListResult[store.Session]{
		Items:      items,
		TotalCount: int64(len(items)),
		HasMore:    false,
	}, nil
}

// Suspend suspends an active session.
func (m *MockSessionService) Suspend(ctx context.Context, sessionID string) error {
	if m.SuspendFunc != nil {
		return m.SuspendFunc(ctx, sessionID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return store.ErrNotFound
	}

	if session.Status != "active" {
		return &InvalidStateError{
			Resource: "session",
			ID:       sessionID,
			Current:  session.Status,
			Expected: "active",
		}
	}

	now := time.Now()
	session.Status = "suspended"
	session.SuspendedAt = &now
	session.UpdatedAt = now
	return nil
}

// Resume resumes a suspended session.
func (m *MockSessionService) Resume(ctx context.Context, sessionID string) error {
	if m.ResumeFunc != nil {
		return m.ResumeFunc(ctx, sessionID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return store.ErrNotFound
	}

	if session.Status != "suspended" {
		return &InvalidStateError{
			Resource: "session",
			ID:       sessionID,
			Current:  session.Status,
			Expected: "suspended",
		}
	}

	now := time.Now()
	session.Status = "resuming"
	session.ResumedAt = &now
	session.UpdatedAt = now
	return nil
}

// Terminate terminates a session.
func (m *MockSessionService) Terminate(ctx context.Context, sessionID string) error {
	if m.TerminateFunc != nil {
		return m.TerminateFunc(ctx, sessionID)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	session, ok := m.sessions[sessionID]
	if !ok {
		return store.ErrNotFound
	}

	if session.Status == "terminated" {
		return &InvalidStateError{
			Resource: "session",
			ID:       sessionID,
			Current:  session.Status,
			Expected: "not terminated",
		}
	}

	session.Status = "terminated"
	session.UpdatedAt = time.Now()
	return nil
}

// AddSession adds a session directly to the mock store (for testing).
func (m *MockSessionService) AddSession(session *store.Session) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions[session.ID] = session
}

// GetAllSessions returns all sessions in the mock store (for testing).
func (m *MockSessionService) GetAllSessions() []*store.Session {
	m.mu.RLock()
	defer m.mu.RUnlock()

	sessions := make([]*store.Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	return sessions
}

// Reset clears all sessions from the mock store.
func (m *MockSessionService) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = make(map[string]*store.Session)
}

// Verify MockSessionService implements SessionService.
var _ SessionService = (*MockSessionService)(nil)
