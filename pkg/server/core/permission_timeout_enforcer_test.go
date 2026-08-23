package core

import (
	"context"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// mockSessionMgrForTimeout implements SessionManagerInterface for testing.
type mockSessionMgrForTimeout struct {
	suspendCalls  []string // session IDs that had Suspend called
	suspendErr    error
	suspendReason string
}

func (m *mockSessionMgrForTimeout) Create(_ context.Context, _ CreateSessionOptions) (*store.Session, error) {
	return nil, nil
}

func (m *mockSessionMgrForTimeout) Get(_ context.Context, _ string) (*store.Session, error) {
	return nil, nil
}

func (m *mockSessionMgrForTimeout) List(_ context.Context, _ ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return nil, nil
}

func (m *mockSessionMgrForTimeout) Activate(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionMgrForTimeout) Suspend(_ context.Context, sessionID, strategy string) error {
	m.suspendCalls = append(m.suspendCalls, sessionID)
	m.suspendReason = strategy
	return m.suspendErr
}

func (m *mockSessionMgrForTimeout) Resume(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionMgrForTimeout) Terminate(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionMgrForTimeout) AttachRunner(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionMgrForTimeout) DetachRunner(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionMgrForTimeout) UpdateContextSnapshot(_ context.Context, _ string, _ *ContextSnapshot) error {
	return nil
}

func setupPermissionTimeoutTest(t *testing.T) (store.Store, *mockSessionMgrForTimeout, *PermissionTimeoutEnforcer) {
	t.Helper()
	logger := zaptest.NewLogger(t)
	s := newTestStore()

	sessionMgr := &mockSessionMgrForTimeout{}
	enforcer := NewPermissionTimeoutEnforcer(
		s,
		sessionMgr,
		logger,
		WithPermissionTimeoutCheckInterval(100*time.Millisecond),
	)

	return s, sessionMgr, enforcer
}

func TestPermissionTimeoutEnforcer_CheckTimeouts_SuspendSession(t *testing.T) {
	s, sessionMgr, enforcer := setupPermissionTimeoutTest(t)
	ctx := context.Background()

	// Create workspace
	ws := &store.Workspace{
		ID:        "ws_test",
		Name:      "test-workspace",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, s.CreateWorkspace(ctx, ws))

	// Create active session
	sess := &store.Session{
		ID:          "sess_test",
		WorkspaceID: ws.ID,
		Agent:       "claude",
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, s.CreateSession(ctx, sess))

	// Create a pending permission request that has exceeded its suspend timeout
	perm := &store.PermissionRequest{
		ID:                  "perm_test",
		SessionID:           sess.ID,
		TaskID:              "task_test",
		RunID:               "trun_test",
		Tool:                "bash",
		Action:              "rm -rf /",
		RiskLevel:           "high",
		Status:              "pending",
		SuspendAfterSeconds: 1,                                // 1 second timeout
		CreatedAt:           time.Now().Add(-5 * time.Second), // Created 5 seconds ago
		UpdatedAt:           time.Now().Add(-5 * time.Second),
	}
	require.NoError(t, s.CreatePermissionRequest(ctx, perm))

	// Run timeout check
	err := enforcer.CheckTimeouts(ctx)
	require.NoError(t, err)

	// Verify session was suspended
	assert.Len(t, sessionMgr.suspendCalls, 1)
	assert.Equal(t, sess.ID, sessionMgr.suspendCalls[0])
	assert.Equal(t, "terminate_preserve_storage", sessionMgr.suspendReason)
}

func TestPermissionTimeoutEnforcer_CheckTimeouts_NoSuspendIfNotExpired(t *testing.T) {
	s, sessionMgr, enforcer := setupPermissionTimeoutTest(t)
	ctx := context.Background()

	// Create workspace
	ws := &store.Workspace{
		ID:        "ws_test",
		Name:      "test-workspace",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, s.CreateWorkspace(ctx, ws))

	// Create active session
	sess := &store.Session{
		ID:          "sess_test",
		WorkspaceID: ws.ID,
		Agent:       "claude",
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, s.CreateSession(ctx, sess))

	// Create a pending permission request that has NOT exceeded its timeout
	perm := &store.PermissionRequest{
		ID:                  "perm_test",
		SessionID:           sess.ID,
		TaskID:              "task_test",
		RunID:               "trun_test",
		Tool:                "bash",
		Action:              "echo hello",
		RiskLevel:           "low",
		Status:              "pending",
		SuspendAfterSeconds: 3600,       // 1 hour timeout
		CreatedAt:           time.Now(), // Just created
		UpdatedAt:           time.Now(),
	}
	require.NoError(t, s.CreatePermissionRequest(ctx, perm))

	// Run timeout check
	err := enforcer.CheckTimeouts(ctx)
	require.NoError(t, err)

	// Verify session was NOT suspended
	assert.Empty(t, sessionMgr.suspendCalls)
}

func TestPermissionTimeoutEnforcer_CheckTimeouts_SkipAlreadySuspended(t *testing.T) {
	s, sessionMgr, enforcer := setupPermissionTimeoutTest(t)
	ctx := context.Background()

	// Create workspace
	ws := &store.Workspace{
		ID:        "ws_test",
		Name:      "test-workspace",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, s.CreateWorkspace(ctx, ws))

	// Create already suspended session
	sess := &store.Session{
		ID:          "sess_test",
		WorkspaceID: ws.ID,
		Agent:       "claude",
		Status:      "suspended", // Already suspended
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, s.CreateSession(ctx, sess))

	// Create a pending permission request that has exceeded its timeout
	perm := &store.PermissionRequest{
		ID:                  "perm_test",
		SessionID:           sess.ID,
		TaskID:              "task_test",
		RunID:               "trun_test",
		Tool:                "bash",
		Action:              "echo hello",
		RiskLevel:           "low",
		Status:              "pending",
		SuspendAfterSeconds: 1,                                // 1 second timeout
		CreatedAt:           time.Now().Add(-5 * time.Second), // Created 5 seconds ago
		UpdatedAt:           time.Now().Add(-5 * time.Second),
	}
	require.NoError(t, s.CreatePermissionRequest(ctx, perm))

	// Run timeout check
	err := enforcer.CheckTimeouts(ctx)
	require.NoError(t, err)

	// Verify session was NOT suspended (already suspended)
	assert.Empty(t, sessionMgr.suspendCalls)
}

func TestPermissionTimeoutEnforcer_CheckTimeouts_SkipRespondedPermissions(t *testing.T) {
	s, sessionMgr, enforcer := setupPermissionTimeoutTest(t)
	ctx := context.Background()

	// Create workspace
	ws := &store.Workspace{
		ID:        "ws_test",
		Name:      "test-workspace",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, s.CreateWorkspace(ctx, ws))

	// Create active session
	sess := &store.Session{
		ID:          "sess_test",
		WorkspaceID: ws.ID,
		Agent:       "claude",
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, s.CreateSession(ctx, sess))

	// Create an APPROVED permission request
	now := time.Now()
	respondedBy := "user1"
	perm := &store.PermissionRequest{
		ID:                  "perm_test",
		SessionID:           sess.ID,
		TaskID:              "task_test",
		RunID:               "trun_test",
		Tool:                "bash",
		Action:              "echo hello",
		RiskLevel:           "low",
		Status:              "approved", // Already responded
		SuspendAfterSeconds: 1,
		CreatedAt:           time.Now().Add(-5 * time.Second),
		UpdatedAt:           time.Now(),
		RespondedBy:         &respondedBy,
		RespondedAt:         &now,
	}
	require.NoError(t, s.CreatePermissionRequest(ctx, perm))

	// Run timeout check
	err := enforcer.CheckTimeouts(ctx)
	require.NoError(t, err)

	// Verify session was NOT suspended (permission already responded)
	assert.Empty(t, sessionMgr.suspendCalls)
}

func TestPermissionTimeoutEnforcer_CheckTimeouts_MultipleSessions(t *testing.T) {
	s, sessionMgr, enforcer := setupPermissionTimeoutTest(t)
	ctx := context.Background()

	// Create workspace
	ws := &store.Workspace{
		ID:        "ws_test",
		Name:      "test-workspace",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, s.CreateWorkspace(ctx, ws))

	// Create two active sessions
	sess1 := &store.Session{
		ID:          "sess_test1",
		WorkspaceID: ws.ID,
		Agent:       "claude",
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	sess2 := &store.Session{
		ID:          "sess_test2",
		WorkspaceID: ws.ID,
		Agent:       "claude",
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, s.CreateSession(ctx, sess1))
	require.NoError(t, s.CreateSession(ctx, sess2))

	// Create expired permission for session 1
	perm1 := &store.PermissionRequest{
		ID:                  "perm_test1",
		SessionID:           sess1.ID,
		TaskID:              "task_test1",
		RunID:               "trun_test1",
		Tool:                "bash",
		Action:              "action1",
		RiskLevel:           "medium",
		Status:              "pending",
		SuspendAfterSeconds: 1,
		CreatedAt:           time.Now().Add(-5 * time.Second),
		UpdatedAt:           time.Now().Add(-5 * time.Second),
	}
	require.NoError(t, s.CreatePermissionRequest(ctx, perm1))

	// Create expired permission for session 2
	perm2 := &store.PermissionRequest{
		ID:                  "perm_test2",
		SessionID:           sess2.ID,
		TaskID:              "task_test2",
		RunID:               "trun_test2",
		Tool:                "bash",
		Action:              "action2",
		RiskLevel:           "medium",
		Status:              "pending",
		SuspendAfterSeconds: 1,
		CreatedAt:           time.Now().Add(-5 * time.Second),
		UpdatedAt:           time.Now().Add(-5 * time.Second),
	}
	require.NoError(t, s.CreatePermissionRequest(ctx, perm2))

	// Run timeout check
	err := enforcer.CheckTimeouts(ctx)
	require.NoError(t, err)

	// Verify both sessions were suspended
	assert.Len(t, sessionMgr.suspendCalls, 2)
	assert.Contains(t, sessionMgr.suspendCalls, sess1.ID)
	assert.Contains(t, sessionMgr.suspendCalls, sess2.ID)
}

func TestPermissionTimeoutEnforcer_StartStop(t *testing.T) {
	_, _, enforcer := setupPermissionTimeoutTest(t)
	ctx := context.Background()

	// Start the enforcer
	enforcer.Start(ctx)

	// Wait a bit
	time.Sleep(50 * time.Millisecond)

	// Stop the enforcer
	enforcer.Stop()

	// Should complete without hanging
}

// EnsureRunner satisfies SessionManagerInterface. These fakes never allocate.
func (m *mockSessionMgrForTimeout) EnsureRunner(_ context.Context, _ string) (*store.Session, error) {
	return nil, ErrNoRunnerAvailable
}
