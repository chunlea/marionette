package core

import (
	"context"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// testSessionStore extends testStoreWrapper with session-specific functionality.
type testSessionStore struct {
	*testStoreWrapper
	mu                 sync.RWMutex
	sessions           map[string]*store.Session
	workspaces         map[string]*store.Workspace
	runners            map[string]*store.Runner
	agentConfigs       map[string]*store.AgentConfig
	permissionRequests map[string]*store.PermissionRequest
	tasks              map[string]*store.Task
}

func newTestSessionStore() *testSessionStore {
	return &testSessionStore{
		testStoreWrapper: &testStoreWrapper{
			testStore: newTestRunnerStore(),
		},
		sessions:           make(map[string]*store.Session),
		workspaces:         make(map[string]*store.Workspace),
		runners:            make(map[string]*store.Runner),
		agentConfigs:       make(map[string]*store.AgentConfig),
		permissionRequests: make(map[string]*store.PermissionRequest),
		tasks:              make(map[string]*store.Task),
	}
}

func (s *testSessionStore) CreateSession(_ context.Context, session *store.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
	return nil
}

func (s *testSessionStore) GetSession(_ context.Context, id string) (*store.Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.sessions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	// Return a copy to avoid race conditions with concurrent reads/writes
	copy := *session
	return &copy, nil
}

func (s *testSessionStore) ListSessions(_ context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.Session, 0, len(s.sessions))
	for _, sess := range s.sessions {
		// Filter by status
		if len(opts.Status) > 0 {
			matched := false
			for _, st := range opts.Status {
				if sess.Status == st {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		// Filter by runner_id
		if opts.RunnerID != nil {
			if sess.RunnerID == nil || *sess.RunnerID != *opts.RunnerID {
				continue
			}
		}
		// Return copies to avoid race conditions
		copy := *sess
		items = append(items, &copy)
	}
	return &store.ListResult[store.Session]{Items: items}, nil
}

func (s *testSessionStore) UpdateSession(_ context.Context, id string, updates store.SessionUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	session, ok := s.sessions[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		session.Status = *updates.Status
	}
	if updates.RunnerID != nil {
		if *updates.RunnerID == "" {
			session.RunnerID = nil
		} else {
			session.RunnerID = updates.RunnerID
		}
	}
	if updates.SuspendedAt != nil {
		session.SuspendedAt = updates.SuspendedAt
	}
	if updates.SuspendStrategy != nil {
		session.SuspendStrategy = updates.SuspendStrategy
	}
	if updates.PreviousRunnerID != nil {
		session.PreviousRunnerID = updates.PreviousRunnerID
	}
	if updates.ResumedAt != nil {
		session.ResumedAt = updates.ResumedAt
	}
	if updates.LastActivityAt != nil {
		session.LastActivityAt = updates.LastActivityAt
	}
	if updates.ContextSnapshot != nil {
		session.ContextSnapshot = updates.ContextSnapshot
	}
	session.UpdatedAt = time.Now()
	return nil
}

func (s *testSessionStore) DeleteSession(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
	return nil
}

func (s *testSessionStore) CreateWorkspace(_ context.Context, workspace *store.Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaces[workspace.ID] = workspace
	return nil
}

func (s *testSessionStore) GetWorkspace(_ context.Context, id string) (*store.Workspace, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	workspace, ok := s.workspaces[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	// Return a copy to avoid race conditions
	copy := *workspace
	return &copy, nil
}

func (s *testSessionStore) GetRunner(_ context.Context, id string) (*store.Runner, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runner, ok := s.runners[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	// Return a copy to avoid race conditions
	copy := *runner
	return &copy, nil
}

func (s *testSessionStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	runner, ok := s.runners[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		runner.Status = *updates.Status
	}
	if updates.LastSeenAt != nil {
		runner.LastSeenAt = updates.LastSeenAt
	}
	return nil
}

func (s *testSessionStore) GetAgentConfig(_ context.Context, id string) (*store.AgentConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cfg, ok := s.agentConfigs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	// Return a copy to avoid race conditions
	copy := *cfg
	return &copy, nil
}

func (s *testSessionStore) ListPermissionRequests(_ context.Context, opts store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.PermissionRequest, 0)
	for _, req := range s.permissionRequests {
		// Filter by session ID
		if opts.SessionID != nil && req.SessionID != *opts.SessionID {
			continue
		}
		// Filter by status
		if len(opts.Status) > 0 {
			matched := false
			for _, st := range opts.Status {
				if req.Status == st {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		// Return copies to avoid race conditions
		copy := *req
		items = append(items, &copy)
	}
	return &store.ListResult[store.PermissionRequest]{Items: items}, nil
}

func (s *testSessionStore) ListTasks(_ context.Context, opts store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.Task, 0)
	for _, task := range s.tasks {
		// Filter by session ID
		if opts.SessionID != nil && task.SessionID != *opts.SessionID {
			continue
		}
		// Filter by status
		if len(opts.Status) > 0 {
			matched := false
			for _, st := range opts.Status {
				if task.Status == st {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		// Return copies to avoid race conditions
		copy := *task
		items = append(items, &copy)
	}
	return &store.ListResult[store.Task]{Items: items}, nil
}

// Thread-safe helper methods for test setup
func (s *testSessionStore) SetSession(session *store.Session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions[session.ID] = session
}

func (s *testSessionStore) SetRunner(runner *store.Runner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runners[runner.ID] = runner
}

func (s *testSessionStore) SetWorkspace(workspace *store.Workspace) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.workspaces[workspace.ID] = workspace
}

func (s *testSessionStore) SetAgentConfig(cfg *store.AgentConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.agentConfigs[cfg.ID] = cfg
}

func (s *testSessionStore) SetTask(task *store.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = task
}

func (s *testSessionStore) SetPermissionRequest(req *store.PermissionRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.permissionRequests[req.ID] = req
}

// Thread-safe getter methods for test verification
func (s *testSessionStore) GetSessionDirect(id string) *store.Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessions[id]
}

func (s *testSessionStore) GetRunnerDirect(id string) *store.Runner {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.runners[id]
}

func (s *testSessionStore) GetWorkspaceDirect(id string) *store.Workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.workspaces[id]
}

// mockConnManagerForSession implements ConnectionManagerInterface for testing.
type mockConnManagerForSession struct {
	connectedRunners map[string]bool
}

func (m *mockConnManagerForSession) IsConnected(runnerID string) bool {
	if m.connectedRunners != nil {
		return m.connectedRunners[runnerID]
	}
	return true
}

func (m *mockConnManagerForSession) UpdateLastSeen(_ string) error {
	return nil
}

// mockCommandSenderForSession implements CommandSender for testing.
type mockCommandSenderForSession struct {
	lastRunnerID string
	lastCommand  *pb.ServerCommand
	sendErr      error
}

func (m *mockCommandSenderForSession) SendCommand(runnerID string, cmd *pb.ServerCommand) error {
	m.lastRunnerID = runnerID
	m.lastCommand = cmd
	return m.sendErr
}

// Helper to create test setup
func setupSessionManagerTest() (*SessionManager, *testSessionStore) {
	manager, s, _ := setupSessionManagerTestFull(nil)
	return manager, s
}

func setupSessionManagerTestWithCmdSender(cmdSender CommandSender) (*SessionManager, *testSessionStore) {
	manager, s, _ := setupSessionManagerTestFull(cmdSender)
	return manager, s
}

func setupSessionManagerTestFull(cmdSender CommandSender) (*SessionManager, *testSessionStore, *mockConnManagerForSession) {
	s := newTestSessionStore()
	connMgr := &mockConnManagerForSession{}
	logger := zap.NewNop()
	manager := NewSessionManager(s, connMgr, cmdSender, logger)
	return manager, s, connMgr
}

// =============================================================================
// SessionManager Tests
// =============================================================================

func TestSessionManager_Create(t *testing.T) {
	manager, s := setupSessionManagerTest()

	// Create a workspace first
	s.SetWorkspace(&store.Workspace{
		ID:   "ws_123",
		Name: "test-workspace",
	})

	opts := CreateSessionOptions{
		WorkspaceID: "ws_123",
		Agent:       "claude",
	}

	session, err := manager.Create(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, session)

	assert.Equal(t, "ws_123", session.WorkspaceID)
	assert.Equal(t, "claude", session.Agent)
	assert.Equal(t, SessionStatusPending, session.Status)
	assert.Equal(t, LifecycleModeOnDemand, session.LifecycleMode)
	assert.Equal(t, NetworkPolicyAllowList, session.NetworkPolicy)
	assert.True(t, len(session.ID) > 0)
}

func TestSessionManager_Create_WithName(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})

	name := "my-session"
	opts := CreateSessionOptions{
		Name:        &name,
		WorkspaceID: "ws_123",
		Agent:       "claude",
	}

	session, err := manager.Create(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, session.Name)
	assert.Equal(t, "my-session", *session.Name)
}

func TestSessionManager_Create_WithLifecycleMode(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})

	opts := CreateSessionOptions{
		WorkspaceID:   "ws_123",
		Agent:         "claude",
		LifecycleMode: LifecycleModeAlwaysOn,
	}

	session, err := manager.Create(context.Background(), opts)
	require.NoError(t, err)
	assert.Equal(t, LifecycleModeAlwaysOn, session.LifecycleMode)
}

func TestSessionManager_Create_ScheduledWithoutCron(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})

	opts := CreateSessionOptions{
		WorkspaceID:   "ws_123",
		Agent:         "claude",
		LifecycleMode: LifecycleModeScheduled,
		// Missing ScheduleCron
	}

	_, err := manager.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrScheduleCronRequired)
}

func TestSessionManager_Create_ScheduledWithCron(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})

	cron := "0 9 * * 1-5"
	opts := CreateSessionOptions{
		WorkspaceID:   "ws_123",
		Agent:         "claude",
		LifecycleMode: LifecycleModeScheduled,
		ScheduleCron:  &cron,
	}

	session, err := manager.Create(context.Background(), opts)
	require.NoError(t, err)
	assert.Equal(t, LifecycleModeScheduled, session.LifecycleMode)
	require.NotNil(t, session.ScheduleCron)
	assert.Equal(t, "0 9 * * 1-5", *session.ScheduleCron)
}

func TestSessionManager_Create_WorkspaceRequired(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	opts := CreateSessionOptions{
		Agent: "claude",
		// Missing WorkspaceID
	}

	_, err := manager.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrWorkspaceRequired)
}

func TestSessionManager_Create_AgentRequired(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})

	opts := CreateSessionOptions{
		WorkspaceID: "ws_123",
		// Missing Agent
	}

	_, err := manager.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrAgentRequired)
}

func TestSessionManager_Create_WorkspaceNotFound(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	opts := CreateSessionOptions{
		WorkspaceID: "ws_nonexistent",
		Agent:       "claude",
	}

	_, err := manager.Create(context.Background(), opts)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "workspace not found")
}

func TestSessionManager_Get(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusActive,
		Agent:  "claude",
	})

	session, err := manager.Get(context.Background(), "sess_123")
	require.NoError(t, err)
	assert.Equal(t, "sess_123", session.ID)
	assert.Equal(t, SessionStatusActive, session.Status)
}

func TestSessionManager_Get_NotFound(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	_, err := manager.Get(context.Background(), "sess_nonexistent")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManager_List(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{ID: "sess_1", Status: SessionStatusActive})
	s.SetSession(&store.Session{ID: "sess_2", Status: SessionStatusSuspended})
	s.SetSession(&store.Session{ID: "sess_3", Status: SessionStatusActive})

	result, err := manager.List(context.Background(), ListSessionsOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 3)
}

func TestSessionManager_List_FilterByStatus(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{ID: "sess_1", Status: SessionStatusActive})
	s.SetSession(&store.Session{ID: "sess_2", Status: SessionStatusSuspended})
	s.SetSession(&store.Session{ID: "sess_3", Status: SessionStatusActive})

	result, err := manager.List(context.Background(), ListSessionsOptions{
		Status: []string{SessionStatusActive},
	})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
}

func TestSessionManager_Activate(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
	})
	s.SetRunner(&store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	})

	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusActive, session.Status)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_123", *session.RunnerID)
}

func TestSessionManager_Activate_FromResuming(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusResuming,
	})
	s.SetRunner(&store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	})

	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusActive, session.Status)
	assert.NotNil(t, session.ResumedAt)
}

func TestSessionManager_Activate_SessionNotFound(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	err := manager.Activate(context.Background(), "sess_nonexistent", "run_123")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManager_Activate_InvalidTransition(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusSuspended, // Can't go directly to active
	})
	s.SetRunner(&store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	})

	err := manager.Activate(context.Background(), "sess_123", "run_123")
	assert.ErrorIs(t, err, ErrInvalidSessionTransition)
}

func TestSessionManager_Activate_RunnerNotIdle(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
	})
	s.SetRunner(&store.Runner{
		ID:     "run_123",
		Status: StatusBusy, // Not idle
	})

	err := manager.Activate(context.Background(), "sess_123", "run_123")
	assert.ErrorIs(t, err, ErrRunnerNotIdle)
}

func TestSessionManager_Suspend(t *testing.T) {
	manager, s := setupSessionManagerTest()
	runnerID := "run_123"
	s.SetSession(&store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	})

	err := manager.Suspend(context.Background(), "sess_123", "terminate")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusSuspended, session.Status)
	assert.Nil(t, session.RunnerID)
	assert.NotNil(t, session.SuspendedAt)
	require.NotNil(t, session.SuspendStrategy)
	assert.Equal(t, "terminate", *session.SuspendStrategy)
	require.NotNil(t, session.PreviousRunnerID)
	assert.Equal(t, "run_123", *session.PreviousRunnerID)
}

func TestSessionManager_Suspend_SessionNotFound(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	err := manager.Suspend(context.Background(), "sess_nonexistent", "terminate")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManager_Suspend_InvalidTransition(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending, // Can't suspend a pending session
	})

	err := manager.Suspend(context.Background(), "sess_123", "terminate")
	assert.ErrorIs(t, err, ErrInvalidSessionTransition)
}

func TestSessionManager_Suspend_ClearsContextSnapshotWithRunningTask(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)

	// Create session with context snapshot (simulating a conversation in progress)
	snapshot := &ContextSnapshot{
		WorkingDirectory: "/home/user/project",
		ConversationID:   "conv_123", // This would cause --resume to be used
	}
	snapshotJSON, _ := snapshot.ToJSON()

	runnerID := "run_123"
	s.SetSession(&store.Session{
		ID:              "sess_123",
		Status:          SessionStatusActive,
		RunnerID:        &runnerID,
		WorkspaceID:     "ws_123",
		Agent:           "claude",
		ContextSnapshot: snapshotJSON, // Has conversation_id
	})

	// Create a running task - this simulates suspension during task execution
	s.SetTask(&store.Task{
		ID:        "task_123",
		SessionID: "sess_123",
		Status:    TaskStatusRunning,
		Prompt:    "test prompt",
	})

	err := manager.Suspend(context.Background(), "sess_123", "permission_timeout")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusSuspended, session.Status)

	// Context snapshot should be cleared (new empty snapshot) because there was a running task.
	// This prevents Claude Code from trying to --resume a killed mid-task conversation.
	if len(session.ContextSnapshot) > 0 {
		parsed, err := ParseContextSnapshot(session.ContextSnapshot)
		require.NoError(t, err)
		// ConversationID should be empty (not the old one)
		assert.Empty(t, parsed.ConversationID, "ConversationID should be cleared when suspending with running task")
	}
}

func TestSessionManager_Suspend_PreservesContextSnapshotWithoutRunningTask(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)

	// Create session with context snapshot
	snapshot := &ContextSnapshot{
		WorkingDirectory: "/home/user/project",
		ConversationID:   "conv_123",
	}
	snapshotJSON, _ := snapshot.ToJSON()

	runnerID := "run_123"
	s.SetSession(&store.Session{
		ID:              "sess_123",
		Status:          SessionStatusActive,
		RunnerID:        &runnerID,
		WorkspaceID:     "ws_123",
		Agent:           "claude",
		ContextSnapshot: snapshotJSON,
	})

	// No running tasks - all completed
	s.SetTask(&store.Task{
		ID:        "task_123",
		SessionID: "sess_123",
		Status:    TaskStatusCompleted, // Not running
		Prompt:    "test prompt",
	})

	// Suspend with context snapshot option (simulating normal suspend)
	err := manager.SuspendWithOptions(context.Background(), "sess_123", SuspendOptions{
		Strategy:        "idle_timeout",
		ContextSnapshot: snapshot,
	})
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusSuspended, session.Status)

	// Context snapshot should be preserved since there's no running task
	if len(session.ContextSnapshot) > 0 {
		parsed, err := ParseContextSnapshot(session.ContextSnapshot)
		require.NoError(t, err)
		// ConversationID should be preserved
		assert.Equal(t, "conv_123", parsed.ConversationID, "ConversationID should be preserved when suspending without running task")
	}
}

func TestSessionManager_Resume(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusSuspended,
	})

	err := manager.Resume(context.Background(), "sess_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusResuming, session.Status)
}

func TestSessionManager_Resume_SessionNotFound(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	err := manager.Resume(context.Background(), "sess_nonexistent")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManager_Resume_InvalidTransition(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusActive, // Can't resume an active session
	})

	err := manager.Resume(context.Background(), "sess_123")
	assert.ErrorIs(t, err, ErrInvalidSessionTransition)
}

func TestSessionManager_Terminate(t *testing.T) {
	manager, s := setupSessionManagerTest()
	runnerID := "run_123"
	s.SetSession(&store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	})

	err := manager.Terminate(context.Background(), "sess_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusTerminated, session.Status)
	assert.Nil(t, session.RunnerID)
	require.NotNil(t, session.PreviousRunnerID)
	assert.Equal(t, "run_123", *session.PreviousRunnerID)
}

func TestSessionManager_Terminate_FromSuspended(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusSuspended,
	})

	err := manager.Terminate(context.Background(), "sess_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusTerminated, session.Status)
}

func TestSessionManager_Terminate_FromPending(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
	})

	err := manager.Terminate(context.Background(), "sess_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusTerminated, session.Status)
}

func TestSessionManager_Terminate_AlreadyTerminated(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusTerminated,
	})

	err := manager.Terminate(context.Background(), "sess_123")
	assert.ErrorIs(t, err, ErrSessionAlreadyTerminated)
}

func TestSessionManager_Terminate_SessionNotFound(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	err := manager.Terminate(context.Background(), "sess_nonexistent")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManager_AttachRunner(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
	})
	s.SetRunner(&store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	})

	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusActive, session.Status)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_123", *session.RunnerID)
}

func TestSessionManager_AttachRunner_Resuming(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusResuming,
	})
	s.SetRunner(&store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	})

	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusActive, session.Status)
}

func TestSessionManager_AttachRunner_SessionNotFound(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	err := manager.AttachRunner(context.Background(), "sess_nonexistent", "run_123")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManager_AttachRunner_ResumeWithBusyPreviousRunner(t *testing.T) {
	// Test that we can attach to a busy runner if it was the previous runner for a resuming session
	manager, s := setupSessionManagerTest()

	previousRunnerID := "run_123"
	s.SetSession(&store.Session{
		ID:               "sess_123",
		Status:           SessionStatusResuming,
		PreviousRunnerID: &previousRunnerID,
	})
	s.SetRunner(&store.Runner{
		ID:     "run_123",
		Status: StatusBusy, // Runner is busy - should still allow for resume
	})

	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusActive, session.Status)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_123", *session.RunnerID)
}

func TestSessionManager_AttachRunner_ResumeWithBusyDifferentRunner(t *testing.T) {
	// Test that we cannot attach to a busy runner if it was NOT the previous runner
	manager, s := setupSessionManagerTest()

	previousRunnerID := "run_original"
	s.SetSession(&store.Session{
		ID:               "sess_123",
		Status:           SessionStatusResuming,
		PreviousRunnerID: &previousRunnerID,
	})
	s.SetRunner(&store.Runner{
		ID:     "run_different",
		Status: StatusBusy, // Runner is busy and not the previous runner
	})

	err := manager.AttachRunner(context.Background(), "sess_123", "run_different")
	assert.ErrorIs(t, err, ErrRunnerNotIdle)
}

func TestSessionManager_AttachRunner_AlreadyHasRunner(t *testing.T) {
	manager, s := setupSessionManagerTest()
	runnerID := "run_existing"
	s.SetSession(&store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	})
	s.SetRunner(&store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	})

	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	assert.ErrorIs(t, err, ErrSessionAlreadyHasRunner)
}

func TestSessionManager_AttachRunner_InvalidStatus(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusSuspended, // Can't attach to suspended session directly
	})
	s.SetRunner(&store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	})

	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	assert.ErrorIs(t, err, ErrInvalidSessionTransition)
}

func TestSessionManager_DetachRunner(t *testing.T) {
	manager, s := setupSessionManagerTest()
	runnerID := "run_123"
	s.SetSession(&store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	})

	err := manager.DetachRunner(context.Background(), "sess_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Nil(t, session.RunnerID)
	require.NotNil(t, session.PreviousRunnerID)
	assert.Equal(t, "run_123", *session.PreviousRunnerID)
}

func TestSessionManager_DetachRunner_SessionNotFound(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	err := manager.DetachRunner(context.Background(), "sess_nonexistent")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManager_DetachRunner_NoRunner(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.SetSession(&store.Session{
		ID:       "sess_123",
		Status:   SessionStatusPending,
		RunnerID: nil,
	})

	err := manager.DetachRunner(context.Background(), "sess_123")
	assert.ErrorIs(t, err, ErrSessionNoRunner)
}

// =============================================================================
// IsValidSessionTransition Tests
// =============================================================================

func TestIsValidSessionTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		expected bool
	}{
		// Pending transitions
		{"pending to active", SessionStatusPending, SessionStatusActive, true},
		{"pending to suspended", SessionStatusPending, SessionStatusSuspended, false},
		{"pending to resuming", SessionStatusPending, SessionStatusResuming, false},
		{"pending to terminated", SessionStatusPending, SessionStatusTerminated, true},

		// Active transitions
		{"active to suspended", SessionStatusActive, SessionStatusSuspended, true},
		{"active to terminated", SessionStatusActive, SessionStatusTerminated, true},
		{"active to pending", SessionStatusActive, SessionStatusPending, false},
		{"active to resuming", SessionStatusActive, SessionStatusResuming, false},
		{"active to active", SessionStatusActive, SessionStatusActive, false},

		// Suspended transitions
		{"suspended to resuming", SessionStatusSuspended, SessionStatusResuming, true},
		{"suspended to terminated", SessionStatusSuspended, SessionStatusTerminated, true},
		{"suspended to active", SessionStatusSuspended, SessionStatusActive, false},
		{"suspended to pending", SessionStatusSuspended, SessionStatusPending, false},
		{"suspended to suspended", SessionStatusSuspended, SessionStatusSuspended, false},

		// Resuming transitions
		{"resuming to active", SessionStatusResuming, SessionStatusActive, true},
		{"resuming to suspended", SessionStatusResuming, SessionStatusSuspended, true},
		{"resuming to terminated", SessionStatusResuming, SessionStatusTerminated, true},
		{"resuming to pending", SessionStatusResuming, SessionStatusPending, false},
		{"resuming to resuming", SessionStatusResuming, SessionStatusResuming, false},

		// Terminated transitions (none allowed)
		{"terminated to pending", SessionStatusTerminated, SessionStatusPending, false},
		{"terminated to active", SessionStatusTerminated, SessionStatusActive, false},
		{"terminated to suspended", SessionStatusTerminated, SessionStatusSuspended, false},
		{"terminated to resuming", SessionStatusTerminated, SessionStatusResuming, false},
		{"terminated to terminated", SessionStatusTerminated, SessionStatusTerminated, false},

		// Unknown status
		{"unknown to active", "unknown", SessionStatusActive, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidSessionTransition(tt.from, tt.to)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// Integration-style Tests
// =============================================================================

func TestSessionManager_FullLifecycle(t *testing.T) {
	manager, s := setupSessionManagerTest()

	// Setup workspace and runner (use thread-safe methods)
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})
	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})

	// 1. Create session
	session, err := manager.Create(context.Background(), CreateSessionOptions{
		WorkspaceID: "ws_123",
		Agent:       "claude",
	})
	require.NoError(t, err)
	assert.Equal(t, SessionStatusPending, session.Status)

	// 2. Attach runner (activates session)
	err = manager.AttachRunner(context.Background(), session.ID, "run_123")
	require.NoError(t, err)

	session, _ = manager.Get(context.Background(), session.ID)
	assert.Equal(t, SessionStatusActive, session.Status)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_123", *session.RunnerID)

	// 3. Suspend session
	err = manager.Suspend(context.Background(), session.ID, "terminate_preserve_storage")
	require.NoError(t, err)

	session, _ = manager.Get(context.Background(), session.ID)
	assert.Equal(t, SessionStatusSuspended, session.Status)
	assert.Nil(t, session.RunnerID)

	// 4. Resume session
	err = manager.Resume(context.Background(), session.ID)
	require.NoError(t, err)

	session, _ = manager.Get(context.Background(), session.ID)
	assert.Equal(t, SessionStatusResuming, session.Status)

	// 5. Attach new runner (completes resume)
	s.SetRunner(&store.Runner{ID: "run_456", Status: StatusIdle})
	err = manager.Activate(context.Background(), session.ID, "run_456")
	require.NoError(t, err)

	session, _ = manager.Get(context.Background(), session.ID)
	assert.Equal(t, SessionStatusActive, session.Status)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_456", *session.RunnerID)

	// 6. Terminate session
	err = manager.Terminate(context.Background(), session.ID)
	require.NoError(t, err)

	session, _ = manager.Get(context.Background(), session.ID)
	assert.Equal(t, SessionStatusTerminated, session.Status)
	assert.Nil(t, session.RunnerID)
}

// =============================================================================
// SendAttachSession Tests
// =============================================================================

func TestSessionManager_Activate_SendsAttachSession(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s := setupSessionManagerTestWithCmdSender(cmdSender)

	// Setup (use thread-safe methods)
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "/workspace/test"})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
	})
	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})

	// Activate
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	// Verify AttachSession was sent
	assert.Equal(t, "run_123", cmdSender.lastRunnerID)
	require.NotNil(t, cmdSender.lastCommand)

	attachCmd := cmdSender.lastCommand.GetAttachSession()
	require.NotNil(t, attachCmd)
	assert.Equal(t, "sess_123", attachCmd.SessionId)
	assert.Equal(t, "/workspace/test", attachCmd.WorkspacePath)
	assert.NotNil(t, attachCmd.AgentConfig)
	assert.Equal(t, "claude", attachCmd.AgentConfig.Agent)
}

func TestSessionManager_Activate_WithAgentConfig(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s := setupSessionManagerTestWithCmdSender(cmdSender)

	// Setup with agent config
	agentConfigID := "acfg_123"
	model := "claude-3-opus"
	baseURL := "https://api.anthropic.com"
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "/workspace/test"})
	s.SetAgentConfig(&store.AgentConfig{
		ID:      "acfg_123",
		Agent:   "claude",
		Model:   &model,
		BaseURL: &baseURL,
	})
	s.SetSession(&store.Session{
		ID:            "sess_123",
		Status:        SessionStatusPending,
		WorkspaceID:   "ws_123",
		Agent:         "claude",
		IsBYOK:        false,
		AgentConfigID: &agentConfigID,
	})
	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})

	// Activate
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	// Verify agent config was included
	attachCmd := cmdSender.lastCommand.GetAttachSession()
	require.NotNil(t, attachCmd)
	require.NotNil(t, attachCmd.AgentConfig)
	assert.Equal(t, "claude", attachCmd.AgentConfig.Agent)
	assert.Equal(t, "claude-3-opus", attachCmd.AgentConfig.Model)
	assert.Equal(t, "https://api.anthropic.com", attachCmd.AgentConfig.BaseUrl)
}

func TestSessionManager_Activate_WithContextSnapshot(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s := setupSessionManagerTestWithCmdSender(cmdSender)

	// Setup with context snapshot
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "/workspace/test"})
	s.SetSession(&store.Session{
		ID:              "sess_123",
		Status:          SessionStatusPending,
		WorkspaceID:     "ws_123",
		Agent:           "claude",
		IsBYOK:          true,
		ContextSnapshot: []byte(`{"working_dir": "/workspace/test/src"}`),
	})
	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})

	// Activate
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	// Verify context snapshot was included
	attachCmd := cmdSender.lastCommand.GetAttachSession()
	require.NotNil(t, attachCmd)
	assert.Equal(t, []byte(`{"working_dir": "/workspace/test/src"}`), attachCmd.ContextSnapshot)
}

func TestSessionManager_Activate_ResumingWithPendingPermissions(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s := setupSessionManagerTestWithCmdSender(cmdSender)

	// Setup resuming session with pending permissions
	suspendedAt := time.Now().Add(-1 * time.Hour)
	respondedAt := time.Now().Add(-30 * time.Minute) // Responded while suspended
	respondedBy := "user@example.com"
	reason := "approved for testing"

	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "/workspace/test"})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusResuming,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
		SuspendedAt: &suspendedAt,
	})
	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})
	s.SetPermissionRequest(&store.PermissionRequest{
		ID:             "perm_123",
		SessionID:      "sess_123",
		Status:         "approved",
		RespondedAt:    &respondedAt,
		RespondedBy:    &respondedBy,
		ResponseReason: &reason,
	})

	// Activate
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	// Verify pending permissions were included
	attachCmd := cmdSender.lastCommand.GetAttachSession()
	require.NotNil(t, attachCmd)
	require.Len(t, attachCmd.PendingPermissions, 1)
	assert.Equal(t, "perm_123", attachCmd.PendingPermissions[0].RequestId)
	assert.True(t, attachCmd.PendingPermissions[0].Approved)
	assert.Equal(t, "approved for testing", attachCmd.PendingPermissions[0].Reason)
	assert.Equal(t, "user@example.com", attachCmd.PendingPermissions[0].RespondedBy)
}

func TestSessionManager_Activate_NoCmdSender(t *testing.T) {
	// When cmdSender is nil, activation should still succeed
	manager, s := setupSessionManagerTest() // No cmdSender

	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "/workspace/test"})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
	})
	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})

	// Activate should succeed even without cmdSender
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusActive, session.Status)
}

func TestSessionManager_Activate_SendCommandError(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{
		sendErr: assert.AnError,
	}
	manager, s := setupSessionManagerTestWithCmdSender(cmdSender)

	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "/workspace/test"})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
	})
	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})

	// Activate should succeed even if SendCommand fails
	// (session is already activated in DB, command failure is logged but not fatal)
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusActive, session.Status)
}

func TestSessionManager_Activate_DetachesOldSession(t *testing.T) {
	manager, s := setupSessionManagerTest()

	runnerID := "run_123"
	s.SetWorkspace(&store.Workspace{ID: "ws_old", Name: "/workspace/old"})
	s.SetWorkspace(&store.Workspace{ID: "ws_new", Name: "/workspace/new"})

	// Existing active session attached to the runner
	s.SetSession(&store.Session{
		ID:          "sess_old",
		Status:      SessionStatusActive,
		WorkspaceID: "ws_old",
		Agent:       "claude",
		RunnerID:    &runnerID,
	})

	// New session to be activated
	s.SetSession(&store.Session{
		ID:          "sess_new",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_new",
		Agent:       "claude",
	})

	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})

	// Activate new session on the same runner
	err := manager.Activate(context.Background(), "sess_new", "run_123")
	require.NoError(t, err)

	// New session should be active
	newSession := s.GetSessionDirect("sess_new")
	assert.Equal(t, SessionStatusActive, newSession.Status)
	require.NotNil(t, newSession.RunnerID)
	assert.Equal(t, runnerID, *newSession.RunnerID)

	// Old session should be suspended
	oldSession := s.GetSessionDirect("sess_old")
	assert.Equal(t, SessionStatusSuspended, oldSession.Status)
	assert.Nil(t, oldSession.RunnerID)
}

func TestSessionManager_Activate_DetachesMultipleOldSessions(t *testing.T) {
	manager, s := setupSessionManagerTest()

	runnerID := "run_123"
	s.SetWorkspace(&store.Workspace{ID: "ws_1", Name: "/workspace/1"})
	s.SetWorkspace(&store.Workspace{ID: "ws_2", Name: "/workspace/2"})
	s.SetWorkspace(&store.Workspace{ID: "ws_new", Name: "/workspace/new"})

	// Multiple active sessions attached to the runner (shouldn't happen normally, but test the cleanup)
	s.SetSession(&store.Session{
		ID:          "sess_1",
		Status:      SessionStatusActive,
		WorkspaceID: "ws_1",
		Agent:       "claude",
		RunnerID:    &runnerID,
	})
	s.SetSession(&store.Session{
		ID:          "sess_2",
		Status:      SessionStatusActive,
		WorkspaceID: "ws_2",
		Agent:       "claude",
		RunnerID:    &runnerID,
	})

	// New session to be activated
	s.SetSession(&store.Session{
		ID:          "sess_new",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_new",
		Agent:       "claude",
	})

	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})

	// Activate new session on the same runner
	err := manager.Activate(context.Background(), "sess_new", "run_123")
	require.NoError(t, err)

	// New session should be active
	newSession := s.GetSessionDirect("sess_new")
	assert.Equal(t, SessionStatusActive, newSession.Status)

	// Both old sessions should be suspended
	assert.Equal(t, SessionStatusSuspended, s.GetSessionDirect("sess_1").Status)
	assert.Equal(t, SessionStatusSuspended, s.GetSessionDirect("sess_2").Status)
}

// =============================================================================
// Workspace-related Tests
// =============================================================================

// mockWorkspaceManagerForSession implements WorkspaceManagerInterface for testing.
type mockWorkspaceManagerForSession struct {
	hostPath             string
	ensureHostDirCalled  bool
	cleanupHostDirCalled bool
	cleanupHostDirErr    error
	ensureHostDirErr     error
}

func (m *mockWorkspaceManagerForSession) Create(_ context.Context, _ CreateWorkspaceOptions) (*store.Workspace, error) {
	return nil, nil
}

func (m *mockWorkspaceManagerForSession) Get(_ context.Context, _ string) (*store.Workspace, error) {
	return nil, nil
}

func (m *mockWorkspaceManagerForSession) List(_ context.Context, _ ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	return nil, nil
}

func (m *mockWorkspaceManagerForSession) Update(_ context.Context, _ string, _ store.WorkspaceUpdates) (*store.Workspace, error) {
	return nil, nil
}

func (m *mockWorkspaceManagerForSession) Delete(_ context.Context, _ string) error {
	return nil
}

func (m *mockWorkspaceManagerForSession) GetHostPath(_ context.Context, _ string) (string, error) {
	return m.hostPath, nil
}

func (m *mockWorkspaceManagerForSession) EnsureHostDirectory(_ context.Context, _ string) (string, error) {
	m.ensureHostDirCalled = true
	if m.ensureHostDirErr != nil {
		return "", m.ensureHostDirErr
	}
	return m.hostPath, nil
}

func (m *mockWorkspaceManagerForSession) CleanupHostDirectory(_ context.Context, _ string) error {
	m.cleanupHostDirCalled = true
	return m.cleanupHostDirErr
}

func (m *mockWorkspaceManagerForSession) IsInUse(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func TestSessionManager_SetWorkspaceManager(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	// Initially nil
	assert.Nil(t, manager.workspaceManager)

	// Set workspace manager
	mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/test"}
	manager.SetWorkspaceManager(mockWM)

	assert.NotNil(t, manager.workspaceManager)
	assert.Equal(t, mockWM, manager.workspaceManager)
}

func TestSessionManager_GetWorkspaceHostPath(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		manager, s := setupSessionManagerTest()
		mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/ws_123"}
		manager.SetWorkspaceManager(mockWM)

		s.SetSession(&store.Session{
			ID:          "sess_123",
			Status:      SessionStatusActive,
			WorkspaceID: "ws_123",
		})

		path, err := manager.GetWorkspaceHostPath(context.Background(), "sess_123")
		require.NoError(t, err)
		assert.Equal(t, "/var/workspaces/ws_123", path)
	})

	t.Run("session not found", func(t *testing.T) {
		manager, _ := setupSessionManagerTest()
		mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces"}
		manager.SetWorkspaceManager(mockWM)

		_, err := manager.GetWorkspaceHostPath(context.Background(), "sess_nonexistent")
		assert.ErrorIs(t, err, ErrSessionNotFound)
	})

	t.Run("no workspace manager", func(t *testing.T) {
		manager, s := setupSessionManagerTest()
		// No workspace manager set

		s.SetSession(&store.Session{
			ID:          "sess_123",
			Status:      SessionStatusActive,
			WorkspaceID: "ws_123",
		})

		path, err := manager.GetWorkspaceHostPath(context.Background(), "sess_123")
		require.NoError(t, err)
		assert.Empty(t, path)
	})
}

func TestSessionManager_Activate_WorkspacePathForDockerRunner(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s := setupSessionManagerTestWithCmdSender(cmdSender)
	mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/ws_123"}
	manager.SetWorkspaceManager(mockWM)

	// Setup with Docker runner (runner-is-sandbox mode)
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
	})
	s.SetRunner(&store.Runner{
		ID:          "run_123",
		Status:      StatusIdle,
		SandboxMode: "runner-is-sandbox", // Docker mode
	})

	// Activate
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	// Verify workspace path is /workspace (container mount point)
	attachCmd := cmdSender.lastCommand.GetAttachSession()
	require.NotNil(t, attachCmd)
	assert.Equal(t, "/workspace", attachCmd.WorkspacePath)
}

func TestSessionManager_Activate_WorkspacePathForLocalRunner(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s := setupSessionManagerTestWithCmdSender(cmdSender)
	mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/ws_123"}
	manager.SetWorkspaceManager(mockWM)

	// Setup with local runner (no sandbox mode or "none")
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
	})
	s.SetRunner(&store.Runner{
		ID:          "run_123",
		Status:      StatusIdle,
		SandboxMode: "none", // Local mode
	})

	// Activate
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	// Verify workspace path is the actual host path
	attachCmd := cmdSender.lastCommand.GetAttachSession()
	require.NotNil(t, attachCmd)
	assert.Equal(t, "/var/workspaces/ws_123", attachCmd.WorkspacePath)
}

func TestSessionManager_Activate_WorkspacePathForRunnerCreatesSandbox(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s := setupSessionManagerTestWithCmdSender(cmdSender)
	mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/ws_123"}
	manager.SetWorkspaceManager(mockWM)

	// Setup with runner-creates-sandbox mode
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
	})
	s.SetRunner(&store.Runner{
		ID:          "run_123",
		Status:      StatusIdle,
		SandboxMode: "runner-creates-sandbox", // macOS/GPU pool mode
	})

	// Activate
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	// Verify workspace path is the actual host path (not /workspace)
	attachCmd := cmdSender.lastCommand.GetAttachSession()
	require.NotNil(t, attachCmd)
	assert.Equal(t, "/var/workspaces/ws_123", attachCmd.WorkspacePath)
}

func TestSessionManager_Activate_WorkspacePathWithoutWorkspaceManager(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s := setupSessionManagerTestWithCmdSender(cmdSender)
	// No workspace manager set

	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
	})
	s.SetRunner(&store.Runner{
		ID:          "run_123",
		Status:      StatusIdle,
		SandboxMode: "runner-is-sandbox",
	})

	// Activate
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	// Without workspace manager, should fall back to workspace name
	attachCmd := cmdSender.lastCommand.GetAttachSession()
	require.NotNil(t, attachCmd)
	assert.Equal(t, "test-workspace", attachCmd.WorkspacePath)
}

func TestSessionManager_AttachRunner_EnsuresHostDirectory(t *testing.T) {
	manager, s := setupSessionManagerTest()
	mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/ws_123"}
	manager.SetWorkspaceManager(mockWM)

	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
	})
	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})

	// Attach runner
	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	// Verify EnsureHostDirectory was called
	assert.True(t, mockWM.ensureHostDirCalled)
}

func TestSessionManager_AttachRunner_EnsureHostDirectoryError(t *testing.T) {
	manager, s := setupSessionManagerTest()
	mockWM := &mockWorkspaceManagerForSession{
		hostPath:         "/var/workspaces/ws_123",
		ensureHostDirErr: assert.AnError,
	}
	manager.SetWorkspaceManager(mockWM)

	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
	})
	s.SetRunner(&store.Runner{ID: "run_123", Status: StatusIdle})

	// Attach runner - should fail if EnsureHostDirectory fails
	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	require.Error(t, err)

	// Session should still be in pending status (not activated)
	session := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusPending, session.Status)
}

// =============================================================================
// requestRunnerForResume Tests
// =============================================================================

func TestSessionManager_RequestRunnerForResume_ExternalRunnerConnected(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, connMgr := setupSessionManagerTestFull(cmdSender)
	mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/ws_123"}
	manager.SetWorkspaceManager(mockWM)

	// Configure connection manager to show runner as connected
	connMgr.connectedRunners = map[string]bool{
		"run_external": true,
	}

	// Create workspace
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})

	// Create external runner (no ProviderConfigID)
	s.SetRunner(&store.Runner{
		ID:               "run_external",
		Status:           StatusIdle,
		ProviderConfigID: nil, // External runner
	})

	// Create resuming session with previous runner
	prevRunnerID := "run_external"
	s.SetSession(&store.Session{
		ID:               "sess_123",
		Status:           SessionStatusResuming,
		WorkspaceID:      "ws_123",
		Agent:            "claude",
		PreviousRunnerID: &prevRunnerID,
	})

	// Call requestRunnerForResume
	session := s.GetSessionDirect("sess_123")
	manager.requestRunnerForResume(context.Background(), session)

	// Verify session was attached to the runner
	updatedSession := s.GetSessionDirect("sess_123")
	require.NotNil(t, updatedSession.RunnerID)
	assert.Equal(t, "run_external", *updatedSession.RunnerID)
	assert.Equal(t, SessionStatusActive, updatedSession.Status)
}

func TestSessionManager_RequestRunnerForResume_ExternalRunnerNotConnected(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, connMgr := setupSessionManagerTestFull(cmdSender)
	mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/ws_123"}
	manager.SetWorkspaceManager(mockWM)

	// Configure connection manager to show runner as NOT connected
	connMgr.connectedRunners = map[string]bool{
		"run_external": false,
	}

	// Create workspace
	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "test-workspace"})

	// Create external runner (no ProviderConfigID)
	s.SetRunner(&store.Runner{
		ID:               "run_external",
		Status:           StatusOffline,
		ProviderConfigID: nil, // External runner
	})

	// Create resuming session with previous runner
	prevRunnerID := "run_external"
	s.SetSession(&store.Session{
		ID:               "sess_123",
		Status:           SessionStatusResuming,
		WorkspaceID:      "ws_123",
		Agent:            "claude",
		PreviousRunnerID: &prevRunnerID,
	})

	// Call requestRunnerForResume
	session := s.GetSessionDirect("sess_123")
	manager.requestRunnerForResume(context.Background(), session)

	// Verify session was NOT attached (still resuming, waiting for reconnect)
	updatedSession := s.GetSessionDirect("sess_123")
	assert.Nil(t, updatedSession.RunnerID)
	assert.Equal(t, SessionStatusResuming, updatedSession.Status)
}

func TestSessionManager_RequestRunnerForResume_NoPreviousRunner(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)

	// Create resuming session WITHOUT previous runner
	s.SetSession(&store.Session{
		ID:               "sess_123",
		Status:           SessionStatusResuming,
		WorkspaceID:      "ws_123",
		Agent:            "claude",
		PreviousRunnerID: nil, // No previous runner
	})

	// Call requestRunnerForResume - should return early without error
	session := s.GetSessionDirect("sess_123")
	manager.requestRunnerForResume(context.Background(), session)

	// Verify session status unchanged
	updatedSession := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusResuming, updatedSession.Status)
}

func TestSessionManager_RequestRunnerForResume_PreviousRunnerNotFound(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)

	// Create resuming session with non-existent previous runner
	prevRunnerID := "run_nonexistent"
	s.SetSession(&store.Session{
		ID:               "sess_123",
		Status:           SessionStatusResuming,
		WorkspaceID:      "ws_123",
		Agent:            "claude",
		PreviousRunnerID: &prevRunnerID,
	})

	// Call requestRunnerForResume - should return early without error
	session := s.GetSessionDirect("sess_123")
	manager.requestRunnerForResume(context.Background(), session)

	// Verify session status unchanged
	updatedSession := s.GetSessionDirect("sess_123")
	assert.Equal(t, SessionStatusResuming, updatedSession.Status)
}

// =============================================================================
// reExecuteRunningTasks Tests
// =============================================================================

// mockTaskManagerForSession implements TaskManagerInterface for testing.
type mockTaskManagerForSession struct {
	executedTasks   []string
	reExecutedTasks []string
	executeErr      error
	reExecuteErr    error
}

func (m *mockTaskManagerForSession) Create(_ context.Context, _ CreateTaskOptions) (*store.Task, error) {
	return nil, nil
}
func (m *mockTaskManagerForSession) Get(_ context.Context, _ string) (*store.Task, error) {
	return nil, nil
}
func (m *mockTaskManagerForSession) List(_ context.Context, _ ListTasksOptions) (*store.ListResult[store.Task], error) {
	return nil, nil
}
func (m *mockTaskManagerForSession) Cancel(_ context.Context, _ string) error { return nil }
func (m *mockTaskManagerForSession) Execute(_ context.Context, taskID string) error {
	m.executedTasks = append(m.executedTasks, taskID)
	return m.executeErr
}
func (m *mockTaskManagerForSession) ReExecute(_ context.Context, taskID string) error {
	m.reExecutedTasks = append(m.reExecutedTasks, taskID)
	return m.reExecuteErr
}
func (m *mockTaskManagerForSession) CreateRun(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, nil
}
func (m *mockTaskManagerForSession) OnTaskAccepted(_ context.Context, _ string) error { return nil }
func (m *mockTaskManagerForSession) OnTaskStarted(_ context.Context, _ string) error  { return nil }
func (m *mockTaskManagerForSession) OnTaskProgress(_ context.Context, _ string, _ int) error {
	return nil
}
func (m *mockTaskManagerForSession) OnTaskCompleted(_ context.Context, _ *TaskCompletedResult) error {
	return nil
}
func (m *mockTaskManagerForSession) FailRun(_ context.Context, _, _ string) error { return nil }
func (m *mockTaskManagerForSession) ShouldRetry(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *mockTaskManagerForSession) Retry(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, nil
}

func TestSessionManager_ReExecuteRunningTasks_WithRunningTask(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)
	mockTM := &mockTaskManagerForSession{}
	manager.SetTaskManager(mockTM)

	// Create a running task
	s.SetTask(&store.Task{
		ID:        "task_123",
		SessionID: "sess_123",
		Status:    TaskStatusRunning,
		Prompt:    "test prompt",
	})

	// Call reExecuteRunningTasks
	manager.reExecuteRunningTasks(context.Background(), "sess_123", "run_123")

	// Verify task was re-executed using ReExecute (not Execute)
	assert.Len(t, mockTM.reExecutedTasks, 1)
	assert.Equal(t, "task_123", mockTM.reExecutedTasks[0])
	assert.Empty(t, mockTM.executedTasks) // Execute should not be called
}

func TestSessionManager_ReExecuteRunningTasks_NoRunningTasks(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)
	mockTM := &mockTaskManagerForSession{}
	manager.SetTaskManager(mockTM)

	// Create a completed task (not running)
	s.SetTask(&store.Task{
		ID:        "task_123",
		SessionID: "sess_123",
		Status:    TaskStatusCompleted,
		Prompt:    "test prompt",
	})

	// Call reExecuteRunningTasks
	manager.reExecuteRunningTasks(context.Background(), "sess_123", "run_123")

	// Verify no tasks were re-executed
	assert.Empty(t, mockTM.reExecutedTasks)
}

func TestSessionManager_ReExecuteRunningTasks_NoTaskManager(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)
	// Don't set task manager

	// Create a running task
	s.SetTask(&store.Task{
		ID:        "task_123",
		SessionID: "sess_123",
		Status:    TaskStatusRunning,
		Prompt:    "test prompt",
	})

	// Call reExecuteRunningTasks - should not panic
	manager.reExecuteRunningTasks(context.Background(), "sess_123", "run_123")
}

// =============================================================================
// GetContextSnapshot Tests
// =============================================================================

func TestSessionManager_GetContextSnapshot_Success(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)

	// Create session with context snapshot
	snapshot := &ContextSnapshot{
		WorkingDirectory: "/home/user/project",
		ConversationID:   "conv_123",
		Environment: map[string]string{
			"PATH": "/usr/bin",
		},
	}
	snapshotJSON, _ := snapshot.ToJSON()

	s.SetSession(&store.Session{
		ID:              "sess_123",
		Status:          SessionStatusActive,
		WorkspaceID:     "ws_123",
		Agent:           "claude",
		ContextSnapshot: snapshotJSON,
	})

	result, err := manager.GetContextSnapshot(context.Background(), "sess_123")
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, "/home/user/project", result.WorkingDirectory)
	assert.Equal(t, "conv_123", result.ConversationID)
	assert.Equal(t, "/usr/bin", result.Environment["PATH"])
}

func TestSessionManager_GetContextSnapshot_NotFound(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, _, _ := setupSessionManagerTestFull(cmdSender)

	_, err := manager.GetContextSnapshot(context.Background(), "nonexistent")
	assert.Equal(t, ErrSessionNotFound, err)
}

func TestSessionManager_GetContextSnapshot_EmptySnapshot(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)

	// Create session without context snapshot
	s.SetSession(&store.Session{
		ID:              "sess_123",
		Status:          SessionStatusActive,
		WorkspaceID:     "ws_123",
		Agent:           "claude",
		ContextSnapshot: nil, // No snapshot
	})

	result, err := manager.GetContextSnapshot(context.Background(), "sess_123")
	require.NoError(t, err)
	assert.Nil(t, result)
}

// =============================================================================
// UpdateContextSnapshot Tests
// =============================================================================

func TestSessionManager_UpdateContextSnapshot_Success(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)

	// Create session
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusActive,
		WorkspaceID: "ws_123",
		Agent:       "claude",
	})

	snapshot := &ContextSnapshot{
		WorkingDirectory: "/home/user/project",
		ConversationID:   "conv_456",
	}

	err := manager.UpdateContextSnapshot(context.Background(), "sess_123", snapshot)
	require.NoError(t, err)

	// Verify session was updated
	updatedSession := s.GetSessionDirect("sess_123")
	require.NotNil(t, updatedSession.ContextSnapshot)

	// Parse and verify
	result, err := ParseContextSnapshot(updatedSession.ContextSnapshot)
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project", result.WorkingDirectory)
	assert.Equal(t, "conv_456", result.ConversationID)
}

func TestSessionManager_UpdateContextSnapshot_NilSnapshot(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)

	// Create session
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusActive,
		WorkspaceID: "ws_123",
		Agent:       "claude",
	})

	// Update with nil snapshot should do nothing
	err := manager.UpdateContextSnapshot(context.Background(), "sess_123", nil)
	require.NoError(t, err)

	// Verify session was NOT updated
	updatedSession := s.GetSessionDirect("sess_123")
	assert.Nil(t, updatedSession.ContextSnapshot)
}
