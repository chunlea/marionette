package core

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/provider"
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

// ListRunners lists the runners this store owns.
//
// Without it the embedded wrapper answered from a different map, so allocation
// could never see a runner set with SetRunner. The Status and Labels filters
// are honoured the way the real store applies them.
func (s *testSessionStore) ListRunners(_ context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	items := make([]*store.Runner, 0, len(s.runners))
	for _, runner := range s.runners {
		if len(opts.Status) > 0 && !matchesAnyStatus(opts.Status, runner.Status) {
			continue
		}
		if opts.PoolName != nil {
			if runner.PoolName == nil || *runner.PoolName != *opts.PoolName {
				continue
			}
		}
		if !runnerHasLabels(runner, opts.Labels) {
			continue
		}
		cp := *runner
		items = append(items, &cp)
	}

	// Deterministic order so selection does not depend on map iteration.
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })

	return &store.ListResult[store.Runner]{Items: items}, nil
}

// runnerHasLabels reports whether a runner carries every requested label.
func runnerHasLabels(runner *store.Runner, want map[string]string) bool {
	if len(want) == 0 {
		return true
	}
	have := map[string]string{}
	if len(runner.Labels) > 0 {
		if err := json.Unmarshal(runner.Labels, &have); err != nil {
			return false
		}
	}
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
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

// Webhook methods (stub)
func (s *testSessionStore) CreateWebhook(_ context.Context, _ *store.Webhook) error { return nil }
func (s *testSessionStore) GetWebhook(_ context.Context, _ string) (*store.Webhook, error) {
	return nil, store.ErrNotFound
}
func (s *testSessionStore) GetWebhookByName(_ context.Context, _ string, _ *string) (*store.Webhook, error) {
	return nil, store.ErrNotFound
}
func (s *testSessionStore) ListWebhooks(_ context.Context, _ store.ListWebhooksOptions) (*store.ListResult[store.Webhook], error) {
	return &store.ListResult[store.Webhook]{}, nil
}
func (s *testSessionStore) UpdateWebhook(_ context.Context, _ string, _ store.WebhookUpdates) error {
	return nil
}
func (s *testSessionStore) DeleteWebhook(_ context.Context, _ string) error { return nil }
func (s *testSessionStore) GetActiveWebhooksForEvent(_ context.Context, _ string, _ *string) ([]*store.Webhook, error) {
	return nil, nil
}

// WebhookEvent methods (stub)
func (s *testSessionStore) CreateWebhookEvent(_ context.Context, _ *store.WebhookEvent) error {
	return nil
}
func (s *testSessionStore) GetWebhookEvent(_ context.Context, _ string) (*store.WebhookEvent, error) {
	return nil, store.ErrNotFound
}
func (s *testSessionStore) ListWebhookEvents(_ context.Context, _ store.ListWebhookEventsOptions) (*store.ListResult[store.WebhookEvent], error) {
	return &store.ListResult[store.WebhookEvent]{}, nil
}
func (s *testSessionStore) UpdateWebhookEvent(_ context.Context, _ string, _ store.WebhookEventUpdates) error {
	return nil
}
func (s *testSessionStore) GetPendingWebhookEvents(_ context.Context, _ int) ([]*store.WebhookEvent, error) {
	return nil, nil
}
func (s *testSessionStore) CancelWebhookEventsByWebhook(_ context.Context, _ string) error {
	return nil
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
	inUse                bool
	inUseErr             error
	workspace            *store.Workspace
}

func (m *mockWorkspaceManagerForSession) Create(_ context.Context, _ CreateWorkspaceOptions) (*store.Workspace, error) {
	return nil, nil
}

func (m *mockWorkspaceManagerForSession) Get(_ context.Context, id string) (*store.Workspace, error) {
	if m.workspace != nil {
		return m.workspace, nil
	}
	return &store.Workspace{ID: id, Persist: true, Mobility: WorkspaceMobilityLocal}, nil
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
	return m.inUse, m.inUseErr
}

func TestSessionManager_SetWorkspaceManager(t *testing.T) {
	manager, _ := setupSessionManagerTest()

	// Initially nil
	assert.Nil(t, manager.workspaceManager)

	// Set workspace manager
	mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/test"}
	manager.workspaceManager = mockWM

	assert.NotNil(t, manager.workspaceManager)
	assert.Equal(t, mockWM, manager.workspaceManager)
}

func TestSessionManager_GetWorkspaceHostPath(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		manager, s := setupSessionManagerTest()
		mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/ws_123"}
		manager.workspaceManager = mockWM

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
		manager.workspaceManager = mockWM

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
	manager.workspaceManager = mockWM

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
	manager.workspaceManager = mockWM

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
	manager.workspaceManager = mockWM

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
	manager.workspaceManager = mockWM

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
	manager.workspaceManager = mockWM

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
	manager.workspaceManager = mockWM

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
	manager.workspaceManager = mockWM

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
	mu              sync.Mutex
	executedTasks   []string
	reExecutedTasks []string
	dispatched      []string
	executeErr      error
	reExecuteErr    error
	dispatchErr     error
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
	m.mu.Lock()
	defer m.mu.Unlock()
	m.executedTasks = append(m.executedTasks, taskID)
	return m.executeErr
}
func (m *mockTaskManagerForSession) ReExecute(_ context.Context, taskID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.reExecutedTasks = append(m.reExecutedTasks, taskID)
	return m.reExecuteErr
}

// executedTaskIDs returns a copy of the recorded Execute calls.
func (m *mockTaskManagerForSession) executedTaskIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.executedTasks...)
}

// reExecutedTaskIDs returns a copy of the recorded re-execution calls.
func (m *mockTaskManagerForSession) reExecutedTaskIDs() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.reExecutedTasks...)
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
	manager.setTaskManager(mockTM)

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
	assert.Equal(t, []string{"task_123"}, mockTM.reExecutedTaskIDs())
	assert.Empty(t, mockTM.executedTaskIDs()) // Execute should not be called
}

func TestSessionManager_ReExecuteRunningTasks_NoRunningTasks(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)
	mockTM := &mockTaskManagerForSession{}
	manager.setTaskManager(mockTM)

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
	assert.Empty(t, mockTM.reExecutedTaskIDs())
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

// BeginTx shadows the embedded implementation so the transaction is bound to
// this store rather than the wrapper it embeds. See storeTx.
func (s *testSessionStore) BeginTx(_ context.Context) (store.Tx, error) {
	return &storeTx{Store: s}, nil
}

// =============================================================================
// Suspend / Activate ordering tests
// =============================================================================

// observingCmdSender records what the database looked like at the moment a
// command was published, which is how the suspend ordering is asserted.
type observingCmdSender struct {
	store         *testSessionStore
	sessionID     string
	statusAtSend  string
	runnerAtSend  *string
	commandsCount int
}

func (c *observingCmdSender) SendCommand(_ string, _ *pb.ServerCommand) error {
	c.commandsCount++
	if sess, err := c.store.GetSession(context.Background(), c.sessionID); err == nil {
		c.statusAtSend = sess.Status
		c.runnerAtSend = sess.RunnerID
	}
	return nil
}

// TestSessionManager_Suspend_PersistsBeforeDetaching locks in the ordering fix.
// DetachSession used to be sent before the database write, so a write that
// failed left the runner detached from a session the database still believed
// was active - two views of the world with no way to reconcile them.
func TestSessionManager_Suspend_PersistsBeforeDetaching(t *testing.T) {
	s := newTestSessionStore()
	sender := &observingCmdSender{store: s, sessionID: "sess_123"}
	manager := NewSessionManager(s, &mockConnManagerForSession{}, sender, zap.NewNop())

	runnerID := "run_123"
	s.SetSession(&store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	})

	require.NoError(t, manager.Suspend(context.Background(), "sess_123", "terminate"))

	require.Equal(t, 1, sender.commandsCount, "DetachSession must be sent exactly once")
	assert.Equal(t, SessionStatusSuspended, sender.statusAtSend,
		"the session must already be suspended in the database when DetachSession goes out")
	assert.True(t, sender.runnerAtSend == nil || *sender.runnerAtSend == "",
		"the runner must already be released in the database when DetachSession goes out")

	sess, err := s.GetSession(context.Background(), "sess_123")
	require.NoError(t, err)
	assert.Equal(t, SessionStatusSuspended, sess.Status)
	require.NotNil(t, sess.PreviousRunnerID)
	assert.Equal(t, runnerID, *sess.PreviousRunnerID)
}

// failingDetachStore fails the session update that suspends a session, so the
// suspend path can be observed when persistence is impossible.
type failingDetachStore struct {
	*testSessionStore
	failSessionID string
}

func (s *failingDetachStore) UpdateSession(ctx context.Context, id string, updates store.SessionUpdates) error {
	if id == s.failSessionID {
		return errors.New("database unavailable")
	}
	return s.testSessionStore.UpdateSession(ctx, id, updates)
}

func (s *failingDetachStore) BeginTx(_ context.Context) (store.Tx, error) {
	return &storeTx{Store: s}, nil
}

// TestSessionManager_Suspend_NoDetachWhenPersistenceFails is the other half of
// the ordering contract: if the database write fails, the runner is never told
// to detach, so the two views stay consistent.
func TestSessionManager_Suspend_NoDetachWhenPersistenceFails(t *testing.T) {
	inner := newTestSessionStore()
	s := &failingDetachStore{testSessionStore: inner, failSessionID: "sess_123"}
	sender := &mockCommandSenderForSession{}
	manager := NewSessionManager(s, &mockConnManagerForSession{}, sender, zap.NewNop())

	runnerID := "run_123"
	inner.SetSession(&store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	})

	err := manager.Suspend(context.Background(), "sess_123", "terminate")
	require.Error(t, err)
	assert.Nil(t, sender.lastCommand, "no DetachSession may be sent when the suspend did not persist")
}

// =============================================================================
// Provider release and workspace safety tests
// =============================================================================

// providerAwareSessionStore lets the suspend path resolve a provider for a
// runner, which the plain session test store cannot do.
type providerAwareSessionStore struct {
	*testSessionStore
	providerConfig *store.ProviderConfig
	profiles       map[string]*store.Profile
}

func newProviderAwareSessionStore() *providerAwareSessionStore {
	return &providerAwareSessionStore{
		testSessionStore: newTestSessionStore(),
		providerConfig: &store.ProviderConfig{
			ID:       "pcfg_1",
			Name:     "docker-default",
			Provider: "docker",
		},
	}
}

// SetProfile registers a profile the allocation path can resolve.
func (s *providerAwareSessionStore) SetProfile(profile *store.Profile) {
	if s.profiles == nil {
		s.profiles = make(map[string]*store.Profile)
	}
	s.profiles[profile.ID] = profile
}

func (s *providerAwareSessionStore) GetProfile(_ context.Context, id string) (*store.Profile, error) {
	profile, ok := s.profiles[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return profile, nil
}

func (s *providerAwareSessionStore) GetProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	if s.providerConfig == nil {
		return nil, store.ErrNotFound
	}
	return s.providerConfig, nil
}

func (s *providerAwareSessionStore) BeginTx(_ context.Context) (store.Tx, error) {
	return &storeTx{Store: s}, nil
}

// suspendableFakeProvider is a provider that implements SuspendableProvider.
type suspendableFakeProvider struct {
	fakeProvider
	suspended []string
	result    *provider.SuspendResult
	err       error
}

func (p *suspendableFakeProvider) Suspend(_ context.Context, runnerID string, _ provider.SuspendOptions) (*provider.SuspendResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	p.suspended = append(p.suspended, runnerID)
	return p.result, nil
}

func (p *suspendableFakeProvider) Resume(context.Context, string, provider.ResumeOptions) (*provider.RunnerInstance, error) {
	return nil, errors.New("not implemented")
}

func setupSuspendReleaseTest(prov provider.Provider) (*SessionManager, *providerAwareSessionStore) {
	s := newProviderAwareSessionStore()
	manager := NewSessionManagerWithConfig(SessionManagerConfig{
		Store:       s,
		ConnManager: &mockConnManagerForSession{},
		CmdSender:   &mockCommandSenderForSession{},
		// The default fixture is the ordinary Docker shape: the workspace is
		// mounted from the host, so it survives the runner being destroyed.
		WorkspaceManager: &mockWorkspaceManagerForSession{
			hostPath:  "/var/marionette/workspaces/ws_123",
			workspace: &store.Workspace{ID: "ws_123", Persist: true, Mobility: WorkspaceMobilityLocal},
		},
		ProviderRegistry: &fakeProviderRegistry{prov: prov},
		Logger:           zap.NewNop(),
	})

	runnerID := "run_123"
	cfgID := "pcfg_1"
	s.SetRunner(&store.Runner{
		ID:               runnerID,
		Name:             "runner-1",
		Status:           StatusBusy,
		ProviderConfigID: &cfgID,
	})
	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusActive,
		RunnerID:    &runnerID,
		WorkspaceID: "ws_123",
	})
	return manager, s
}

// TestSessionManager_Suspend_SuspendsThroughProvider covers the gap that made
// "suspended" mean nothing but a database row: SuspendWithOptions never
// resolved a provider, so every container and sandbox kept running and billing.
func TestSessionManager_Suspend_SuspendsThroughProvider(t *testing.T) {
	prov := &suspendableFakeProvider{
		fakeProvider: fakeProvider{name: "docker-default", kind: provider.ProviderTypeManaged},
		result: &provider.SuspendResult{
			Strategy:   provider.SuspendStrategyPause,
			SnapshotID: "snap_1",
		},
	}
	manager, s := setupSuspendReleaseTest(prov)

	require.NoError(t, manager.Suspend(context.Background(), "sess_123", "pause"))

	assert.Equal(t, []string{"run_123"}, prov.suspended)

	sess, err := s.GetSession(context.Background(), "sess_123")
	require.NoError(t, err)
	require.NotNil(t, sess.SuspendStrategy)
	assert.Equal(t, string(provider.SuspendStrategyPause), *sess.SuspendStrategy,
		"the strategy the provider actually used must be recorded for resume")

	runner, err := s.GetRunner(context.Background(), "run_123")
	require.NoError(t, err)
	assert.Equal(t, StatusPaused, runner.Status)
}

// TestSessionManager_Suspend_FallsBackToDestroy documents the fallback for
// providers that cannot suspend: terminate rather than keep paying.
func TestSessionManager_Suspend_FallsBackToDestroy(t *testing.T) {
	prov := &fakeProvider{name: "docker-default", kind: provider.ProviderTypeManaged}
	manager, s := setupSuspendReleaseTest(prov)

	require.NoError(t, manager.Suspend(context.Background(), "sess_123", "terminate_preserve_storage"))

	assert.Equal(t, []string{"run_123"}, prov.destroyed)

	sess, err := s.GetSession(context.Background(), "sess_123")
	require.NoError(t, err)
	require.NotNil(t, sess.SuspendStrategy)
	assert.Equal(t, string(provider.SuspendStrategyTerminate), *sess.SuspendStrategy)

	runner, err := s.GetRunner(context.Background(), "run_123")
	require.NoError(t, err)
	assert.Equal(t, StatusOffline, runner.Status)
}

// TestSessionManager_Suspend_ProviderFailureDoesNotFailSuspend: the session is
// already suspended in the database, and an orphaned runner is the reaper's
// problem, not the caller's.
func TestSessionManager_Suspend_ProviderFailureDoesNotFailSuspend(t *testing.T) {
	prov := &fakeProvider{
		name:       "docker-default",
		kind:       provider.ProviderTypeManaged,
		destroyErr: errors.New("docker daemon unreachable"),
	}
	manager, s := setupSuspendReleaseTest(prov)

	require.NoError(t, manager.Suspend(context.Background(), "sess_123", "terminate"))

	sess, err := s.GetSession(context.Background(), "sess_123")
	require.NoError(t, err)
	assert.Equal(t, SessionStatusSuspended, sess.Status)
}

// TestSessionManager_Terminate_KeepsSharedWorkspace is the data-loss guard:
// CleanupHostDirectory is an unconditional os.RemoveAll, so terminating one of
// N sessions sharing a workspace used to delete the other N-1's files.
func TestSessionManager_Terminate_KeepsSharedWorkspace(t *testing.T) {
	manager, s := setupSessionManagerTest()
	mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/ws_123", inUse: true}
	manager.workspaceManager = mockWM

	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusActive,
		WorkspaceID: "ws_123",
	})

	require.NoError(t, manager.Terminate(context.Background(), "sess_123"))
	assert.False(t, mockWM.cleanupHostDirCalled,
		"a workspace another session still uses must never be deleted")
}

// TestSessionManager_Terminate_KeepsWorkspaceOnUnknownUsage: an IsInUse failure
// means we do not know, and deleting a workspace is not reversible.
func TestSessionManager_Terminate_KeepsWorkspaceOnUnknownUsage(t *testing.T) {
	manager, s := setupSessionManagerTest()
	mockWM := &mockWorkspaceManagerForSession{
		hostPath: "/var/workspaces/ws_123",
		inUseErr: errors.New("database unavailable"),
	}
	manager.workspaceManager = mockWM

	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusActive,
		WorkspaceID: "ws_123",
	})

	require.NoError(t, manager.Terminate(context.Background(), "sess_123"))
	assert.False(t, mockWM.cleanupHostDirCalled)
}

// TestSessionManager_Terminate_CleansUnusedWorkspace keeps the happy path
// honest: nothing else uses it, so the directory does get removed.
func TestSessionManager_Terminate_CleansUnusedWorkspace(t *testing.T) {
	manager, s := setupSessionManagerTest()
	mockWM := &mockWorkspaceManagerForSession{hostPath: "/var/workspaces/ws_123", inUse: false}
	manager.workspaceManager = mockWM

	s.SetSession(&store.Session{
		ID:          "sess_123",
		Status:      SessionStatusActive,
		WorkspaceID: "ws_123",
	})

	require.NoError(t, manager.Terminate(context.Background(), "sess_123"))
	assert.True(t, mockWM.cleanupHostDirCalled)
}

// TestSessionManager_Suspend_RefusesDestroyWhenWorkspaceIsRunnerLocal is the
// data-loss guard on the Destroy fallback. Sessions outlive runners by design,
// so destroying one is only acceptable while the workspace lives elsewhere.
func TestSessionManager_Suspend_RefusesDestroyWhenWorkspaceIsRunnerLocal(t *testing.T) {
	prov := &fakeProvider{name: "docker-default", kind: provider.ProviderTypeManaged}
	manager, s := setupSuspendReleaseTest(prov)
	manager.workspaceManager = &mockWorkspaceManagerForSession{
		// Persisted, no host mount, local mobility: the files exist only
		// inside the runner.
		hostPath:  "",
		workspace: &store.Workspace{ID: "ws_123", Persist: true, Mobility: WorkspaceMobilityLocal},
	}
	sess, err := s.GetSession(context.Background(), "sess_123")
	require.NoError(t, err)
	sess.WorkspaceID = "ws_123"
	s.SetSession(sess)

	require.NoError(t, manager.Suspend(context.Background(), "sess_123", "terminate"))

	assert.Empty(t, prov.destroyed,
		"a runner holding the only copy of the workspace must not be destroyed")

	// The session is still suspended: the database is authoritative and the
	// leaked runner is the reaper's and the operator's problem, not data loss.
	updated, err := s.GetSession(context.Background(), "sess_123")
	require.NoError(t, err)
	assert.Equal(t, SessionStatusSuspended, updated.Status)
}

func TestSessionManager_Suspend_DestroysWhenWorkspaceIsExternal(t *testing.T) {
	tests := []struct {
		name      string
		workspace *store.Workspace
		hostPath  string
	}{
		{
			name:      "host mounted",
			workspace: &store.Workspace{ID: "ws_123", Persist: true, Mobility: WorkspaceMobilityLocal},
			hostPath:  "/var/marionette/workspaces/ws_123",
		},
		{
			name:      "shared storage",
			workspace: &store.Workspace{ID: "ws_123", Persist: true, Mobility: WorkspaceMobilityShared},
		},
		{
			name:      "object synced",
			workspace: &store.Workspace{ID: "ws_123", Persist: true, Mobility: WorkspaceMobilityObjectSync},
		},
		{
			name:      "explicitly ephemeral",
			workspace: &store.Workspace{ID: "ws_123", Persist: false, Mobility: WorkspaceMobilityLocal},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := &fakeProvider{name: "docker-default", kind: provider.ProviderTypeManaged}
			manager, s := setupSuspendReleaseTest(prov)
			manager.workspaceManager = &mockWorkspaceManagerForSession{
				hostPath:  tt.hostPath,
				workspace: tt.workspace,
			}
			sess, err := s.GetSession(context.Background(), "sess_123")
			require.NoError(t, err)
			sess.WorkspaceID = "ws_123"
			s.SetSession(sess)

			require.NoError(t, manager.Suspend(context.Background(), "sess_123", "terminate"))
			assert.Equal(t, []string{"run_123"}, prov.destroyed)
		})
	}
}

// TestSessionManager_Suspend_RefusesDestroyWithoutWorkspaceManager: if we
// cannot establish where the workspace lives, the answer is no.
func TestSessionManager_Suspend_RefusesDestroyWithoutWorkspaceManager(t *testing.T) {
	prov := &fakeProvider{name: "docker-default", kind: provider.ProviderTypeManaged}
	manager, _ := setupSuspendReleaseTest(prov)
	manager.workspaceManager = nil

	require.NoError(t, manager.Suspend(context.Background(), "sess_123", "terminate"))
	assert.Empty(t, prov.destroyed)
}

// TestSessionManager_Activate_DoesNotReleaseRunnerBeingHandedOver guards a trap
// created by making suspend actually release infrastructure: Activate detaches
// whichever session still holds the runner, and if that detach released the
// runner to the provider it would pause, release or destroy the very instance
// the new session is about to attach to.
func TestSessionManager_Activate_DoesNotReleaseRunnerBeingHandedOver(t *testing.T) {
	prov := &fakeProvider{name: "docker-default", kind: provider.ProviderTypeManaged}
	manager, s := setupSuspendReleaseTest(prov)

	// sess_123 (from the fixture) currently holds run_123. sess_456 is resuming
	// and is about to take it over.
	runner, err := s.GetRunner(context.Background(), "run_123")
	require.NoError(t, err)
	runner.Status = StatusIdle
	s.SetRunner(runner)

	previous := "run_123"
	s.SetSession(&store.Session{
		ID:               "sess_456",
		Status:           SessionStatusResuming,
		PreviousRunnerID: &previous,
		WorkspaceID:      "ws_123",
	})

	require.NoError(t, manager.Activate(context.Background(), "sess_456", "run_123"))

	assert.Empty(t, prov.destroyed,
		"the runner being handed to another session must not be destroyed")

	// The old session is suspended, the new one owns the runner.
	old, err := s.GetSession(context.Background(), "sess_123")
	require.NoError(t, err)
	assert.Equal(t, SessionStatusSuspended, old.Status)

	next, err := s.GetSession(context.Background(), "sess_456")
	require.NoError(t, err)
	assert.Equal(t, SessionStatusActive, next.Status)
	require.NotNil(t, next.RunnerID)
	assert.Equal(t, "run_123", *next.RunnerID)
}

// DispatchNext records which sessions were asked to dispatch their backlog.
func (m *mockTaskManagerForSession) DispatchNext(_ context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dispatched = append(m.dispatched, sessionID)
	return m.dispatchErr
}

// dispatchedSessions returns a copy of the recorded dispatch calls.
func (m *mockTaskManagerForSession) dispatchedSessions() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.dispatched...)
}

// =============================================================================
// G1: allocating a runner for a pending session
// =============================================================================

func setupAllocationTest(t *testing.T) (*SessionManager, *providerAwareSessionStore, *mockConnManagerForSession) {
	t.Helper()
	s := newProviderAwareSessionStore()
	connMgr := &mockConnManagerForSession{}
	manager := NewSessionManagerWithConfig(SessionManagerConfig{
		Store:       s,
		ConnManager: connMgr,
		CmdSender:   &mockCommandSenderForSession{},
		Logger:      zap.NewNop(),
	})
	s.SetSession(&store.Session{
		ID:          "sess_pending",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_1",
	})
	return manager, s, connMgr
}

func idleRunner(id, name string) *store.Runner {
	return &store.Runner{
		ID:       id,
		Name:     name,
		Status:   StatusIdle,
		PoolName: stringPtr("default"),
	}
}

// TestSessionManager_EnsureRunner_ActivatesPendingSession is G1: creating a task
// for a pending session must not require an operator to look up a runner ID and
// call the admin activate endpoint by hand.
func TestSessionManager_EnsureRunner_ActivatesPendingSession(t *testing.T) {
	manager, s, _ := setupAllocationTest(t)
	s.SetRunner(idleRunner("run_1", "smoke-runner"))

	session, err := manager.EnsureRunner(context.Background(), "sess_pending")
	require.NoError(t, err)
	require.NotNil(t, session)

	assert.Equal(t, SessionStatusActive, session.Status)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_1", *session.RunnerID)
}

func TestSessionManager_EnsureRunner_KeepsExistingRunner(t *testing.T) {
	manager, s, _ := setupAllocationTest(t)
	s.SetRunner(idleRunner("run_1", "one"))
	s.SetRunner(idleRunner("run_2", "two"))
	runnerID := "run_2"
	s.SetSession(&store.Session{
		ID:          "sess_pending",
		Status:      SessionStatusActive,
		RunnerID:    &runnerID,
		WorkspaceID: "ws_1",
	})

	session, err := manager.EnsureRunner(context.Background(), "sess_pending")
	require.NoError(t, err)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_2", *session.RunnerID, "an attached runner must not be swapped")
}

// TestSessionManager_EnsureRunner_NoRunnerAvailable is the "surface a clear
// status" half of G1: the session stays pending and says why.
func TestSessionManager_EnsureRunner_NoRunnerAvailable(t *testing.T) {
	manager, _, _ := setupAllocationTest(t)

	session, err := manager.EnsureRunner(context.Background(), "sess_pending")
	require.ErrorIs(t, err, ErrNoRunnerAvailable)
	assert.Nil(t, session)
}

func TestSessionManager_EnsureRunner_SkipsUnusableRunners(t *testing.T) {
	t.Run("busy runner", func(t *testing.T) {
		manager, s, _ := setupAllocationTest(t)
		busy := idleRunner("run_busy", "busy")
		busy.Status = StatusBusy
		s.SetRunner(busy)

		_, err := manager.EnsureRunner(context.Background(), "sess_pending")
		assert.ErrorIs(t, err, ErrNoRunnerAvailable)
	})

	t.Run("tainted runner", func(t *testing.T) {
		manager, s, _ := setupAllocationTest(t)
		tainted := idleRunner("run_tainted", "tainted")
		tainted.Tainted = true
		s.SetRunner(tainted)

		_, err := manager.EnsureRunner(context.Background(), "sess_pending")
		assert.ErrorIs(t, err, ErrNoRunnerAvailable)
	})

	t.Run("disconnected runner", func(t *testing.T) {
		manager, s, connMgr := setupAllocationTest(t)
		connMgr.connectedRunners = map[string]bool{"run_1": false}
		s.SetRunner(idleRunner("run_1", "gone"))

		_, err := manager.EnsureRunner(context.Background(), "sess_pending")
		assert.ErrorIs(t, err, ErrNoRunnerAvailable)
	})

	t.Run("runner already claimed by a live session", func(t *testing.T) {
		manager, s, _ := setupAllocationTest(t)
		s.SetRunner(idleRunner("run_1", "claimed"))
		claimed := "run_1"
		s.SetSession(&store.Session{
			ID:       "sess_other",
			Status:   SessionStatusActive,
			RunnerID: &claimed,
		})

		_, err := manager.EnsureRunner(context.Background(), "sess_pending")
		assert.ErrorIs(t, err, ErrNoRunnerAvailable)
	})
}

// TestSessionManager_EnsureRunner_RespectsProfileCapabilities keeps allocation
// selector-aware: a profile that demands a capability must not be handed a
// runner without it.
func TestSessionManager_EnsureRunner_RespectsProfileCapabilities(t *testing.T) {
	manager, s, _ := setupAllocationTest(t)

	s.SetProfile(&store.Profile{
		ID:       "prof_gpu",
		Name:     "gpu",
		Selector: []byte(`{"capabilities":["gpu"]}`),
	})
	profileID := "prof_gpu"
	s.SetSession(&store.Session{
		ID:          "sess_pending",
		Status:      SessionStatusPending,
		WorkspaceID: "ws_1",
		ProfileID:   &profileID,
	})

	plain := idleRunner("run_plain", "plain")
	s.SetRunner(plain)

	_, err := manager.EnsureRunner(context.Background(), "sess_pending")
	require.ErrorIs(t, err, ErrNoRunnerAvailable)

	gpu := idleRunner("run_gpu", "gpu")
	gpu.Capabilities = []string{"gpu", "xcode"}
	s.SetRunner(gpu)

	session, err := manager.EnsureRunner(context.Background(), "sess_pending")
	require.NoError(t, err)
	require.NotNil(t, session.RunnerID)
	assert.Equal(t, "run_gpu", *session.RunnerID)
}

func TestSessionManager_EnsureRunner_RefusesNonPendingStates(t *testing.T) {
	tests := []struct {
		status  string
		wantErr error
	}{
		{SessionStatusSuspended, ErrSessionNotActive},
		{SessionStatusResuming, ErrNoRunnerAvailable},
		{SessionStatusTerminated, ErrSessionAlreadyTerminated},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			manager, s, _ := setupAllocationTest(t)
			s.SetRunner(idleRunner("run_1", "free"))
			s.SetSession(&store.Session{
				ID:          "sess_pending",
				Status:      tt.status,
				WorkspaceID: "ws_1",
			})

			_, err := manager.EnsureRunner(context.Background(), "sess_pending")
			assert.ErrorIs(t, err, tt.wantErr)
		})
	}
}

// =============================================================================
// G2: dispatching the backlog when a session becomes active
// =============================================================================

// TestSessionManager_Activate_DispatchesPendingBacklog covers the resume half of
// G2: a session that comes back should pick up its backlog rather than wait for
// another POST /execute.
func TestSessionManager_Activate_DispatchesPendingBacklog(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)
	mockTM := &mockTaskManagerForSession{}
	manager.setTaskManager(mockTM)

	s.SetRunner(&store.Runner{ID: "run_123", Name: "r", Status: StatusIdle})
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
	})

	require.NoError(t, manager.Activate(context.Background(), "sess_123", "run_123"))

	require.Eventually(t, func() bool {
		return len(mockTM.dispatchedSessions()) == 1
	}, 5*time.Second, 20*time.Millisecond, "activation must dispatch the session backlog")
	assert.Equal(t, []string{"sess_123"}, mockTM.dispatchedSessions())
}

func TestSessionManager_Activate_DispatchesBacklogAfterResume(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)
	mockTM := &mockTaskManagerForSession{}
	manager.setTaskManager(mockTM)

	previous := "run_123"
	s.SetRunner(&store.Runner{ID: "run_123", Name: "r", Status: StatusIdle})
	s.SetSession(&store.Session{
		ID:               "sess_123",
		Status:           SessionStatusResuming,
		PreviousRunnerID: &previous,
	})

	require.NoError(t, manager.Activate(context.Background(), "sess_123", "run_123"))

	require.Eventually(t, func() bool {
		return len(mockTM.dispatchedSessions()) == 1
	}, 5*time.Second, 20*time.Millisecond, "a resumed session must dispatch its backlog")
}

// ListRuns satisfies TaskManagerInterface. These fakes keep no run history.
func (m *mockTaskManagerForSession) ListRuns(_ context.Context, _ string, _ ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	return &store.ListResult[store.TaskRun]{}, nil
}

// TestSessionManager_AttachSession_SendsWorkspaceIdentity is the fix for a CAS
// key collision A1 would otherwise inherit: workspace_path is "/workspace" for
// every container-mode session, so identity has to travel separately.
func TestSessionManager_AttachSession_SendsWorkspaceIdentity(t *testing.T) {
	sender := &mockCommandSenderForSession{}
	s := newTestSessionStore()
	tenant := "tenant_a"
	manager := NewSessionManagerWithConfig(SessionManagerConfig{
		Store:       s,
		ConnManager: &mockConnManagerForSession{},
		CmdSender:   sender,
		WorkspaceManager: &mockWorkspaceManagerForSession{
			hostPath: "/var/marionette/workspaces/ws_123",
		},
		Logger: zap.NewNop(),
	})

	s.SetWorkspace(&store.Workspace{ID: "ws_123", Name: "mine", TenantID: &tenant})
	s.SetRunner(&store.Runner{
		ID: "run_123", Name: "r", Status: StatusIdle,
		SandboxMode: "runner-is-sandbox", TenantID: &tenant,
	})
	session := &store.Session{
		ID: "sess_123", Status: SessionStatusPending,
		WorkspaceID: "ws_123", TenantID: &tenant,
	}
	s.SetSession(session)

	require.NoError(t, manager.sendAttachSession(context.Background(), session, "run_123"))

	require.NotNil(t, sender.lastCommand)
	attach := sender.lastCommand.GetAttachSession()
	require.NotNil(t, attach)

	assert.Equal(t, "/workspace", attach.GetWorkspacePath(),
		"a container still mounts the workspace at /workspace")
	assert.Equal(t, "ws_123", attach.GetWorkspaceId(),
		"identity must travel separately from the mount point")
	assert.Equal(t, "tenant_a", attach.GetTenantId(),
		"chunks dedupe per tenant, so the tenant is part of the CAS key")
}

// TestSessionManager_AttachSession_TenantIsEmptyForSingleTenant keeps the
// zero-config path honest: no tenant means an empty string, not a placeholder.
func TestSessionManager_AttachSession_TenantIsEmptyForSingleTenant(t *testing.T) {
	sender := &mockCommandSenderForSession{}
	s := newTestSessionStore()
	manager := NewSessionManagerWithConfig(SessionManagerConfig{
		Store:            s,
		ConnManager:      &mockConnManagerForSession{},
		CmdSender:        sender,
		WorkspaceManager: &mockWorkspaceManagerForSession{hostPath: "/var/ws/ws_1"},
		Logger:           zap.NewNop(),
	})

	s.SetWorkspace(&store.Workspace{ID: "ws_1", Name: "mine"})
	s.SetRunner(&store.Runner{ID: "run_1", Name: "r", Status: StatusIdle, SandboxMode: "none"})
	session := &store.Session{ID: "sess_1", Status: SessionStatusPending, WorkspaceID: "ws_1"}
	s.SetSession(session)

	require.NoError(t, manager.sendAttachSession(context.Background(), session, "run_1"))

	attach := sender.lastCommand.GetAttachSession()
	require.NotNil(t, attach)
	assert.Equal(t, "ws_1", attach.GetWorkspaceId())
	assert.Empty(t, attach.GetTenantId())
	assert.Equal(t, "/var/ws/ws_1", attach.GetWorkspacePath(),
		"a native runner gets the real host path")
}
