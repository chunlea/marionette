package core

import (
	"context"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// mockCommandSenderForPerm implements CommandSender for testing.
type mockCommandSenderForPerm struct {
	lastCommand  *pb.ServerCommand
	lastRunnerID string
	sendError    error
}

func (m *mockCommandSenderForPerm) SendCommand(runnerID string, cmd *pb.ServerCommand) error {
	m.lastRunnerID = runnerID
	m.lastCommand = cmd
	return m.sendError
}

// mockSessionMgrForPerm implements SessionManagerInterface for testing.
type mockSessionMgrForPerm struct {
	resumeCalled bool
	resumeErr    error
}

func (m *mockSessionMgrForPerm) Create(_ context.Context, _ CreateSessionOptions) (*store.Session, error) {
	return nil, nil
}

func (m *mockSessionMgrForPerm) Get(_ context.Context, _ string) (*store.Session, error) {
	return nil, nil
}

func (m *mockSessionMgrForPerm) List(_ context.Context, _ ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return nil, nil
}

func (m *mockSessionMgrForPerm) Activate(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionMgrForPerm) Suspend(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionMgrForPerm) Resume(_ context.Context, _ string) error {
	m.resumeCalled = true
	return m.resumeErr
}

func (m *mockSessionMgrForPerm) Terminate(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionMgrForPerm) AttachRunner(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionMgrForPerm) DetachRunner(_ context.Context, _ string) error {
	return nil
}

func setupPermissionManagerTest(t *testing.T) (*PermissionManager, store.Store, *mockCommandSenderForPerm, *mockSessionMgrForPerm) {
	t.Helper()
	logger := zaptest.NewLogger(t)
	s := newTestStore()

	cmdSender := &mockCommandSenderForPerm{}
	sessionMgr := &mockSessionMgrForPerm{}

	pm := NewPermissionManager(s, cmdSender, sessionMgr, nil, logger)

	// Create a test workspace
	ctx := context.Background()
	ws := &store.Workspace{
		ID:        "ws_test",
		Name:      "test-workspace",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, s.CreateWorkspace(ctx, ws))

	// Create a test session
	sess := &store.Session{
		ID:          "sess_test",
		WorkspaceID: ws.ID,
		Agent:       "claude",
		Status:      "active",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	require.NoError(t, s.CreateSession(ctx, sess))

	// Create a test task
	task := &store.Task{
		ID:        "task_test",
		SessionID: sess.ID,
		Prompt:    "test prompt",
		Status:    "running",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, s.CreateTask(ctx, task))

	// Create a test task run
	run := &store.TaskRun{
		ID:        "trun_test",
		TaskID:    task.ID,
		Attempt:   1,
		Status:    "running",
		QueuedAt:  time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, s.CreateTaskRun(ctx, run))

	return pm, s, cmdSender, sessionMgr
}

func TestPermissionManager_Create(t *testing.T) {
	pm, _, _, _ := setupPermissionManagerTest(t)
	ctx := context.Background()

	perm, err := pm.Create(ctx, &CreatePermissionRequestInput{
		SessionID:           "sess_test",
		TaskID:              "task_test",
		RunID:               "trun_test",
		Tool:                "bash",
		Action:              "rm -rf /tmp/test",
		RiskLevel:           "high",
		SuspendAfterSeconds: 1800,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, perm.ID)
	assert.Equal(t, "sess_test", perm.SessionID)
	assert.Equal(t, "bash", perm.Tool)
	assert.Equal(t, "high", perm.RiskLevel)
	assert.Equal(t, PermissionStatusPending, perm.Status)
	assert.Equal(t, 1800, perm.SuspendAfterSeconds)
}

func TestPermissionManager_Create_DefaultValues(t *testing.T) {
	pm, _, _, _ := setupPermissionManagerTest(t)
	ctx := context.Background()

	perm, err := pm.Create(ctx, &CreatePermissionRequestInput{
		SessionID: "sess_test",
		TaskID:    "task_test",
		RunID:     "trun_test",
		Tool:      "edit",
		Action:    "edit file.txt",
		// No RiskLevel or SuspendAfterSeconds
	})
	require.NoError(t, err)
	assert.Equal(t, RiskLevelMedium, perm.RiskLevel)
	assert.Equal(t, DefaultSuspendAfterSeconds, perm.SuspendAfterSeconds)
}

func TestPermissionManager_Respond_Approve(t *testing.T) {
	pm, s, cmdSender, sessionMgr := setupPermissionManagerTest(t)
	ctx := context.Background()

	// Update session with runner
	runnerID := "run_test"
	require.NoError(t, s.UpdateSession(ctx, "sess_test", store.SessionUpdates{
		RunnerID: &runnerID,
	}))

	// Create permission with original request ID (simulating Claude's tool_use_id)
	originalReqID := "toolu_test123"
	perm, err := pm.Create(ctx, &CreatePermissionRequestInput{
		OriginalRequestID: originalReqID,
		SessionID:         "sess_test",
		TaskID:            "task_test",
		RunID:             "trun_test",
		Tool:              "bash",
		Action:            "ls -la",
	})
	require.NoError(t, err)

	// Respond with approval
	err = pm.Respond(ctx, perm.ID, true, "looks safe", "user123")
	require.NoError(t, err)

	// Verify status updated
	updated, err := pm.Get(ctx, perm.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionStatusApproved, updated.Status)
	assert.NotNil(t, updated.RespondedAt)
	require.NotNil(t, updated.RespondedBy)
	assert.Equal(t, "user123", *updated.RespondedBy)

	// Verify command sent with original request ID (not perm.ID)
	assert.Equal(t, runnerID, cmdSender.lastRunnerID)
	assert.NotNil(t, cmdSender.lastCommand)
	approveCmd := cmdSender.lastCommand.GetApprovePermission()
	require.NotNil(t, approveCmd)
	assert.Equal(t, originalReqID, approveCmd.RequestId) // Uses original request ID from agent
	assert.True(t, approveCmd.Approved)

	// Session was active, so Resume should not be called
	assert.False(t, sessionMgr.resumeCalled)
}

func TestPermissionManager_Respond_Deny(t *testing.T) {
	pm, s, cmdSender, _ := setupPermissionManagerTest(t)
	ctx := context.Background()

	// Update session with runner
	runnerID := "run_test"
	require.NoError(t, s.UpdateSession(ctx, "sess_test", store.SessionUpdates{
		RunnerID: &runnerID,
	}))

	// Create permission
	perm, err := pm.Create(ctx, &CreatePermissionRequestInput{
		SessionID: "sess_test",
		TaskID:    "task_test",
		RunID:     "trun_test",
		Tool:      "bash",
		Action:    "rm -rf /",
	})
	require.NoError(t, err)

	// Respond with denial
	err = pm.Respond(ctx, perm.ID, false, "too dangerous", "admin")
	require.NoError(t, err)

	// Verify status updated
	updated, err := pm.Get(ctx, perm.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionStatusDenied, updated.Status)

	// Verify command sent
	approveCmd := cmdSender.lastCommand.GetApprovePermission()
	require.NotNil(t, approveCmd)
	assert.False(t, approveCmd.Approved)
}

func TestPermissionManager_Respond_AlreadyResponded(t *testing.T) {
	pm, s, _, _ := setupPermissionManagerTest(t)
	ctx := context.Background()

	// Update session with runner
	runnerID := "run_test"
	require.NoError(t, s.UpdateSession(ctx, "sess_test", store.SessionUpdates{
		RunnerID: &runnerID,
	}))

	// Create permission
	perm, err := pm.Create(ctx, &CreatePermissionRequestInput{
		SessionID: "sess_test",
		TaskID:    "task_test",
		RunID:     "trun_test",
		Tool:      "bash",
		Action:    "echo hello",
	})
	require.NoError(t, err)

	// Respond once
	err = pm.Respond(ctx, perm.ID, true, "", "user1")
	require.NoError(t, err)

	// Try to respond again
	err = pm.Respond(ctx, perm.ID, false, "", "user2")
	assert.ErrorIs(t, err, ErrPermissionAlreadyResponded)
}

func TestPermissionManager_Respond_SessionSuspended(t *testing.T) {
	pm, s, _, sessionMgr := setupPermissionManagerTest(t)
	ctx := context.Background()

	// Update session to suspended status (no runner)
	suspended := "suspended"
	require.NoError(t, s.UpdateSession(ctx, "sess_test", store.SessionUpdates{
		Status: &suspended,
	}))

	// Create permission
	perm, err := pm.Create(ctx, &CreatePermissionRequestInput{
		SessionID: "sess_test",
		TaskID:    "task_test",
		RunID:     "trun_test",
		Tool:      "bash",
		Action:    "echo hello",
	})
	require.NoError(t, err)

	// Respond - should trigger resume
	err = pm.Respond(ctx, perm.ID, true, "", "user1")
	require.NoError(t, err)

	// Verify resume was called
	assert.True(t, sessionMgr.resumeCalled)
}

func TestPermissionManager_Cancel(t *testing.T) {
	pm, _, _, _ := setupPermissionManagerTest(t)
	ctx := context.Background()

	// Create permission
	perm, err := pm.Create(ctx, &CreatePermissionRequestInput{
		SessionID: "sess_test",
		TaskID:    "task_test",
		RunID:     "trun_test",
		Tool:      "bash",
		Action:    "echo hello",
	})
	require.NoError(t, err)

	// Cancel
	err = pm.Cancel(ctx, perm.ID)
	require.NoError(t, err)

	// Verify status
	updated, err := pm.Get(ctx, perm.ID)
	require.NoError(t, err)
	assert.Equal(t, PermissionStatusCanceled, updated.Status)
}

func TestPermissionManager_Cancel_NotPending(t *testing.T) {
	pm, s, _, _ := setupPermissionManagerTest(t)
	ctx := context.Background()

	// Update session with runner
	runnerID := "run_test"
	require.NoError(t, s.UpdateSession(ctx, "sess_test", store.SessionUpdates{
		RunnerID: &runnerID,
	}))

	// Create permission
	perm, err := pm.Create(ctx, &CreatePermissionRequestInput{
		SessionID: "sess_test",
		TaskID:    "task_test",
		RunID:     "trun_test",
		Tool:      "bash",
		Action:    "echo hello",
	})
	require.NoError(t, err)

	// Respond first
	err = pm.Respond(ctx, perm.ID, true, "", "user1")
	require.NoError(t, err)

	// Try to cancel
	err = pm.Cancel(ctx, perm.ID)
	assert.ErrorIs(t, err, ErrPermissionNotPending)
}

func TestPermissionManager_Get_NotFound(t *testing.T) {
	pm, _, _, _ := setupPermissionManagerTest(t)
	ctx := context.Background()

	_, err := pm.Get(ctx, "perm_nonexistent")
	assert.ErrorIs(t, err, ErrPermissionNotFound)
}

func TestPermissionManager_List(t *testing.T) {
	pm, _, _, _ := setupPermissionManagerTest(t)
	ctx := context.Background()

	// Create multiple permissions
	for i := 0; i < 3; i++ {
		_, err := pm.Create(ctx, &CreatePermissionRequestInput{
			SessionID: "sess_test",
			TaskID:    "task_test",
			RunID:     "trun_test",
			Tool:      "bash",
			Action:    "action",
		})
		require.NoError(t, err)
	}

	// List all
	result, err := pm.List(ctx, ListPermissionRequestsOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 3)
}

func TestPermissionManager_List_FilterByStatus(t *testing.T) {
	pm, s, _, _ := setupPermissionManagerTest(t)
	ctx := context.Background()

	// Update session with runner
	runnerID := "run_test"
	require.NoError(t, s.UpdateSession(ctx, "sess_test", store.SessionUpdates{
		RunnerID: &runnerID,
	}))

	// Create 3 permissions
	var perms []*store.PermissionRequest
	for i := 0; i < 3; i++ {
		perm, err := pm.Create(ctx, &CreatePermissionRequestInput{
			SessionID: "sess_test",
			TaskID:    "task_test",
			RunID:     "trun_test",
			Tool:      "bash",
			Action:    "action",
		})
		require.NoError(t, err)
		perms = append(perms, perm)
	}

	// Approve one
	err := pm.Respond(ctx, perms[0].ID, true, "", "user1")
	require.NoError(t, err)

	// List only pending
	result, err := pm.List(ctx, ListPermissionRequestsOptions{
		Status: []string{PermissionStatusPending},
	})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
}
