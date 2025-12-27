package core

import (
	"context"
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
	sessions           map[string]*store.Session
	workspaces         map[string]*store.Workspace
	runners            map[string]*store.Runner
	agentConfigs       map[string]*store.AgentConfig
	permissionRequests map[string]*store.PermissionRequest
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
	}
}

func (s *testSessionStore) CreateSession(_ context.Context, session *store.Session) error {
	s.sessions[session.ID] = session
	return nil
}

func (s *testSessionStore) GetSession(_ context.Context, id string) (*store.Session, error) {
	session, ok := s.sessions[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return session, nil
}

func (s *testSessionStore) ListSessions(_ context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
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
		items = append(items, sess)
	}
	return &store.ListResult[store.Session]{Items: items}, nil
}

func (s *testSessionStore) UpdateSession(_ context.Context, id string, updates store.SessionUpdates) error {
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
	session.UpdatedAt = time.Now()
	return nil
}

func (s *testSessionStore) DeleteSession(_ context.Context, id string) error {
	delete(s.sessions, id)
	return nil
}

func (s *testSessionStore) CreateWorkspace(_ context.Context, workspace *store.Workspace) error {
	s.workspaces[workspace.ID] = workspace
	return nil
}

func (s *testSessionStore) GetWorkspace(_ context.Context, id string) (*store.Workspace, error) {
	workspace, ok := s.workspaces[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return workspace, nil
}

func (s *testSessionStore) GetRunner(_ context.Context, id string) (*store.Runner, error) {
	runner, ok := s.runners[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return runner, nil
}

func (s *testSessionStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
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
	cfg, ok := s.agentConfigs[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cfg, nil
}

func (s *testSessionStore) ListPermissionRequests(_ context.Context, opts store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
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
		items = append(items, req)
	}
	return &store.ListResult[store.PermissionRequest]{Items: items}, nil
}

// mockConnManagerForSession implements ConnectionManagerInterface for testing.
type mockConnManagerForSession struct{}

func (m *mockConnManagerForSession) IsConnected(_ string) bool {
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
	return setupSessionManagerTestWithCmdSender(nil)
}

func setupSessionManagerTestWithCmdSender(cmdSender CommandSender) (*SessionManager, *testSessionStore) {
	s := newTestSessionStore()
	connMgr := &mockConnManagerForSession{}
	logger := zap.NewNop()
	manager := NewSessionManager(s, connMgr, cmdSender, logger)
	return manager, s
}

// =============================================================================
// SessionManager Tests
// =============================================================================

func TestSessionManager_Create(t *testing.T) {
	manager, s := setupSessionManagerTest()

	// Create a workspace first
	s.workspaces["ws_123"] = &store.Workspace{
		ID:   "ws_123",
		Name: "test-workspace",
	}

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
	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "test-workspace"}

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
	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "test-workspace"}

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
	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "test-workspace"}

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
	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "test-workspace"}

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
	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "test-workspace"}

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
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusActive,
		Agent:  "claude",
	}

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
	s.sessions["sess_1"] = &store.Session{ID: "sess_1", Status: SessionStatusActive}
	s.sessions["sess_2"] = &store.Session{ID: "sess_2", Status: SessionStatusSuspended}
	s.sessions["sess_3"] = &store.Session{ID: "sess_3", Status: SessionStatusActive}

	result, err := manager.List(context.Background(), ListSessionsOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 3)
}

func TestSessionManager_List_FilterByStatus(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_1"] = &store.Session{ID: "sess_1", Status: SessionStatusActive}
	s.sessions["sess_2"] = &store.Session{ID: "sess_2", Status: SessionStatusSuspended}
	s.sessions["sess_3"] = &store.Session{ID: "sess_3", Status: SessionStatusActive}

	result, err := manager.List(context.Background(), ListSessionsOptions{
		Status: []string{SessionStatusActive},
	})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
}

func TestSessionManager_Activate(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
	}
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	}

	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
	assert.Equal(t, SessionStatusActive, session.Status)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_123", *session.RunnerID)
}

func TestSessionManager_Activate_FromResuming(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusResuming,
	}
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	}

	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
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
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusSuspended, // Can't go directly to active
	}
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	}

	err := manager.Activate(context.Background(), "sess_123", "run_123")
	assert.ErrorIs(t, err, ErrInvalidSessionTransition)
}

func TestSessionManager_Activate_RunnerNotIdle(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
	}
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Status: StatusBusy, // Not idle
	}

	err := manager.Activate(context.Background(), "sess_123", "run_123")
	assert.ErrorIs(t, err, ErrRunnerNotIdle)
}

func TestSessionManager_Suspend(t *testing.T) {
	manager, s := setupSessionManagerTest()
	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}

	err := manager.Suspend(context.Background(), "sess_123", "terminate")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
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
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending, // Can't suspend a pending session
	}

	err := manager.Suspend(context.Background(), "sess_123", "terminate")
	assert.ErrorIs(t, err, ErrInvalidSessionTransition)
}

func TestSessionManager_Resume(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusSuspended,
	}

	err := manager.Resume(context.Background(), "sess_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
	assert.Equal(t, SessionStatusResuming, session.Status)
}

func TestSessionManager_Resume_SessionNotFound(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	err := manager.Resume(context.Background(), "sess_nonexistent")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManager_Resume_InvalidTransition(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusActive, // Can't resume an active session
	}

	err := manager.Resume(context.Background(), "sess_123")
	assert.ErrorIs(t, err, ErrInvalidSessionTransition)
}

func TestSessionManager_Terminate(t *testing.T) {
	manager, s := setupSessionManagerTest()
	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}

	err := manager.Terminate(context.Background(), "sess_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
	assert.Equal(t, SessionStatusTerminated, session.Status)
	assert.Nil(t, session.RunnerID)
	require.NotNil(t, session.PreviousRunnerID)
	assert.Equal(t, "run_123", *session.PreviousRunnerID)
}

func TestSessionManager_Terminate_FromSuspended(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusSuspended,
	}

	err := manager.Terminate(context.Background(), "sess_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
	assert.Equal(t, SessionStatusTerminated, session.Status)
}

func TestSessionManager_Terminate_FromPending(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
	}

	err := manager.Terminate(context.Background(), "sess_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
	assert.Equal(t, SessionStatusTerminated, session.Status)
}

func TestSessionManager_Terminate_AlreadyTerminated(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusTerminated,
	}

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
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
	}
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	}

	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
	assert.Equal(t, SessionStatusActive, session.Status)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_123", *session.RunnerID)
}

func TestSessionManager_AttachRunner_Resuming(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusResuming,
	}
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	}

	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
	assert.Equal(t, SessionStatusActive, session.Status)
}

func TestSessionManager_AttachRunner_SessionNotFound(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	err := manager.AttachRunner(context.Background(), "sess_nonexistent", "run_123")
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestSessionManager_AttachRunner_AlreadyHasRunner(t *testing.T) {
	manager, s := setupSessionManagerTest()
	runnerID := "run_existing"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	}

	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	assert.ErrorIs(t, err, ErrSessionAlreadyHasRunner)
}

func TestSessionManager_AttachRunner_InvalidStatus(t *testing.T) {
	manager, s := setupSessionManagerTest()
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusSuspended, // Can't attach to suspended session directly
	}
	s.runners["run_123"] = &store.Runner{
		ID:     "run_123",
		Status: StatusIdle,
	}

	err := manager.AttachRunner(context.Background(), "sess_123", "run_123")
	assert.ErrorIs(t, err, ErrInvalidSessionTransition)
}

func TestSessionManager_DetachRunner(t *testing.T) {
	manager, s := setupSessionManagerTest()
	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}

	err := manager.DetachRunner(context.Background(), "sess_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
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
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusPending,
		RunnerID: nil,
	}

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

	// Setup workspace and runner
	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "test-workspace"}
	s.runners["run_123"] = &store.Runner{ID: "run_123", Status: StatusIdle}

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
	s.runners["run_456"] = &store.Runner{ID: "run_456", Status: StatusIdle}
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

	// Setup
	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "/workspace/test"}
	s.sessions["sess_123"] = &store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
	}
	s.runners["run_123"] = &store.Runner{ID: "run_123", Status: StatusIdle}

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
	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "/workspace/test"}
	s.agentConfigs["acfg_123"] = &store.AgentConfig{
		ID:      "acfg_123",
		Agent:   "claude",
		Model:   &model,
		BaseURL: &baseURL,
	}
	s.sessions["sess_123"] = &store.Session{
		ID:            "sess_123",
		Status:        SessionStatusPending,
		WorkspaceID:   "ws_123",
		Agent:         "claude",
		IsBYOK:        false,
		AgentConfigID: &agentConfigID,
	}
	s.runners["run_123"] = &store.Runner{ID: "run_123", Status: StatusIdle}

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
	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "/workspace/test"}
	s.sessions["sess_123"] = &store.Session{
		ID:              "sess_123",
		Status:          SessionStatusPending,
		WorkspaceID:     "ws_123",
		Agent:           "claude",
		IsBYOK:          true,
		ContextSnapshot: []byte(`{"working_dir": "/workspace/test/src"}`),
	}
	s.runners["run_123"] = &store.Runner{ID: "run_123", Status: StatusIdle}

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

	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "/workspace/test"}
	s.sessions["sess_123"] = &store.Session{
		ID:          "sess_123",
		Status:      SessionStatusResuming,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
		SuspendedAt: &suspendedAt,
	}
	s.runners["run_123"] = &store.Runner{ID: "run_123", Status: StatusIdle}
	s.permissionRequests["perm_123"] = &store.PermissionRequest{
		ID:             "perm_123",
		SessionID:      "sess_123",
		Status:         "approved",
		RespondedAt:    &respondedAt,
		RespondedBy:    &respondedBy,
		ResponseReason: &reason,
	}

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

	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "/workspace/test"}
	s.sessions["sess_123"] = &store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
	}
	s.runners["run_123"] = &store.Runner{ID: "run_123", Status: StatusIdle}

	// Activate should succeed even without cmdSender
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
	assert.Equal(t, SessionStatusActive, session.Status)
}

func TestSessionManager_Activate_SendCommandError(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{
		sendErr: assert.AnError,
	}
	manager, s := setupSessionManagerTestWithCmdSender(cmdSender)

	s.workspaces["ws_123"] = &store.Workspace{ID: "ws_123", Name: "/workspace/test"}
	s.sessions["sess_123"] = &store.Session{
		ID:          "sess_123",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_123",
		Agent:       "claude",
		IsBYOK:      true,
	}
	s.runners["run_123"] = &store.Runner{ID: "run_123", Status: StatusIdle}

	// Activate should succeed even if SendCommand fails
	// (session is already activated in DB, command failure is logged but not fatal)
	err := manager.Activate(context.Background(), "sess_123", "run_123")
	require.NoError(t, err)

	session := s.sessions["sess_123"]
	assert.Equal(t, SessionStatusActive, session.Status)
}
