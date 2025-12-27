package agent

import (
	"context"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestDefaultCommandHandler_AttachSession(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()
	workspace := NewWorkspaceManager(tmpDir, logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	ctx := context.Background()

	// Attach session
	cmd := &pb.AttachSession{
		SessionId:     "sess_test123",
		WorkspacePath: tmpDir + "/workspace1",
		AgentConfig: &pb.AgentConfig{
			Agent:  "claude",
			Model:  "claude-sonnet-4-20250514",
			ApiKey: "test-key",
		},
		ContextSnapshot: []byte(`{"state": "test"}`),
	}

	resp, err := handler.HandleAttachSession(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Check response
	attached := resp.GetSessionAttached()
	require.NotNil(t, attached)
	assert.Equal(t, "sess_test123", attached.SessionId)
	assert.True(t, attached.Restored) // Context snapshot was provided

	// Verify session is tracked
	session, exists := handler.GetSession("sess_test123")
	require.True(t, exists)
	assert.Equal(t, "sess_test123", session.SessionID)
	assert.Equal(t, tmpDir+"/workspace1", session.WorkspacePath)
	assert.Equal(t, "claude", session.AgentConfig.Agent)
	assert.NotZero(t, session.AttachedAt)

	// Verify workspace was created
	assert.True(t, workspace.Exists(tmpDir+"/workspace1"))
}

func TestDefaultCommandHandler_DetachSession(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()
	workspace := NewWorkspaceManager(tmpDir, logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	ctx := context.Background()

	// First attach a session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_test123",
		WorkspacePath: tmpDir + "/workspace1",
	}
	_, err := handler.HandleAttachSession(ctx, attachCmd)
	require.NoError(t, err)

	// Verify session exists
	assert.Equal(t, 1, handler.ActiveSessionCount())

	// Detach session
	detachCmd := &pb.DetachSession{
		SessionId:   "sess_test123",
		SaveContext: true,
		Suspend: &pb.SuspendConfig{
			Strategy:      "terminate",
			SyncWorkspace: false,
			Reason:        "test detach",
		},
	}

	resp, err := handler.HandleDetachSession(ctx, detachCmd)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Check response
	suspended := resp.GetSessionSuspended()
	require.NotNil(t, suspended)
	assert.Equal(t, "sess_test123", suspended.SessionId)
	assert.Equal(t, "terminate", suspended.Strategy)
	assert.True(t, suspended.Success)
	assert.True(t, suspended.ContextSaved)

	// Verify session is no longer tracked
	assert.Equal(t, 0, handler.ActiveSessionCount())
	_, exists := handler.GetSession("sess_test123")
	assert.False(t, exists)
}

func TestDefaultCommandHandler_DetachNonExistent(t *testing.T) {
	logger := zaptest.NewLogger(t)
	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	ctx := context.Background()

	// Try to detach non-existent session
	cmd := &pb.DetachSession{
		SessionId: "sess_nonexistent",
	}

	resp, err := handler.HandleDetachSession(ctx, cmd)
	require.NoError(t, err) // Should not error, just log warning
	assert.Nil(t, resp)     // No response for non-existent session
}

func TestDefaultCommandHandler_ExecuteTask_NoSession(t *testing.T) {
	logger := zaptest.NewLogger(t)
	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	ctx := context.Background()

	// Try to execute task without attached session
	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_123",
		SessionId: "sess_nonexistent",
		Attempt:   1,
		Prompt:    "Test prompt",
	}

	resp, err := handler.HandleExecuteTask(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should return task failed
	completed := resp.GetTaskCompleted()
	require.NotNil(t, completed)
	assert.Equal(t, "task_123", completed.TaskId)
	assert.False(t, completed.Success)
	assert.Contains(t, completed.Error, "session not attached")
}

func TestDefaultCommandHandler_ExecuteTask_WithSession(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()
	workspace := NewWorkspaceManager(tmpDir, logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	ctx := context.Background()

	// First attach a session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_test123",
		WorkspacePath: tmpDir + "/workspace1",
	}
	_, err := handler.HandleAttachSession(ctx, attachCmd)
	require.NoError(t, err)

	// Execute task
	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_123",
		SessionId: "sess_test123",
		Attempt:   1,
		Prompt:    "Test prompt",
	}

	resp, err := handler.HandleExecuteTask(ctx, cmd)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Should return task accepted (no callback set)
	accepted := resp.GetTaskAccepted()
	require.NotNil(t, accepted)
	assert.Equal(t, "task_123", accepted.TaskId)
	assert.Equal(t, "trun_123", accepted.RunId)
	assert.Equal(t, int32(1), accepted.Attempt)
}

func TestDefaultCommandHandler_ExecuteTask_WithCallback(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()
	workspace := NewWorkspaceManager(tmpDir, logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	var callbackCalled bool
	var receivedCmd *pb.ExecuteTask

	// Set callback
	handler.OnExecuteTask = func(_ context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error) {
		callbackCalled = true
		receivedCmd = cmd
		return &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_TaskStarted{
				TaskStarted: &pb.TaskStarted{
					TaskId:  cmd.TaskId,
					RunId:   cmd.RunId,
					Attempt: cmd.Attempt,
				},
			},
		}, nil
	}

	ctx := context.Background()

	// Attach session
	attachCmd := &pb.AttachSession{
		SessionId:     "sess_test123",
		WorkspacePath: tmpDir + "/workspace1",
	}
	_, err := handler.HandleAttachSession(ctx, attachCmd)
	require.NoError(t, err)

	// Execute task
	cmd := &pb.ExecuteTask{
		TaskId:    "task_123",
		RunId:     "trun_123",
		SessionId: "sess_test123",
		Attempt:   1,
		Prompt:    "Test prompt",
	}

	resp, err := handler.HandleExecuteTask(ctx, cmd)
	require.NoError(t, err)

	assert.True(t, callbackCalled)
	assert.Equal(t, "task_123", receivedCmd.TaskId)

	// Should return custom response from callback
	started := resp.GetTaskStarted()
	require.NotNil(t, started)
	assert.Equal(t, "task_123", started.TaskId)
}

func TestDefaultCommandHandler_ApprovePermission(t *testing.T) {
	logger := zaptest.NewLogger(t)
	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	ctx := context.Background()

	cmd := &pb.ApprovePermission{
		RequestId:   "perm_123",
		Approved:    true,
		Reason:      "User approved",
		FromCache:   false,
		RespondedBy: "user@example.com",
	}

	resp, err := handler.HandleApprovePermission(ctx, cmd)
	require.NoError(t, err)
	assert.Nil(t, resp) // No response for permission approval
}

func TestDefaultCommandHandler_ApprovePermission_WithCallback(t *testing.T) {
	logger := zaptest.NewLogger(t)
	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	var callbackCalled bool
	var receivedCmd *pb.ApprovePermission

	handler.OnApprovePermission = func(_ context.Context, cmd *pb.ApprovePermission) error {
		callbackCalled = true
		receivedCmd = cmd
		return nil
	}

	ctx := context.Background()

	cmd := &pb.ApprovePermission{
		RequestId: "perm_123",
		Approved:  true,
	}

	_, err := handler.HandleApprovePermission(ctx, cmd)
	require.NoError(t, err)

	assert.True(t, callbackCalled)
	assert.Equal(t, "perm_123", receivedCmd.RequestId)
}

func TestDefaultCommandHandler_KillTask(t *testing.T) {
	logger := zaptest.NewLogger(t)
	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	ctx := context.Background()

	cmd := &pb.KillTask{
		TaskId: "task_123",
		RunId:  "trun_123",
		Reason: "User canceled",
	}

	resp, err := handler.HandleKillTask(ctx, cmd)
	require.NoError(t, err)
	assert.Nil(t, resp) // No response for kill task
}

func TestDefaultCommandHandler_KillTask_WithCallback(t *testing.T) {
	logger := zaptest.NewLogger(t)
	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	var callbackCalled bool
	var receivedCmd *pb.KillTask

	handler.OnKillTask = func(_ context.Context, cmd *pb.KillTask) error {
		callbackCalled = true
		receivedCmd = cmd
		return nil
	}

	ctx := context.Background()

	cmd := &pb.KillTask{
		TaskId: "task_123",
		RunId:  "trun_123",
		Reason: "timeout",
	}

	_, err := handler.HandleKillTask(ctx, cmd)
	require.NoError(t, err)

	assert.True(t, callbackCalled)
	assert.Equal(t, "task_123", receivedCmd.TaskId)
}

func TestDefaultCommandHandler_CreateTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	ctx := context.Background()

	cmd := &pb.CreateTunnel{
		TunnelId:  "tun_123",
		Type:      "http",
		LocalPort: 8080,
		Direction: "outbound",
	}

	resp, err := handler.HandleCreateTunnel(ctx, cmd)
	require.NoError(t, err)
	assert.Nil(t, resp) // Stub implementation returns nil
}

func TestDefaultCommandHandler_PendingPermissions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()
	workspace := NewWorkspaceManager(tmpDir, logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	var approvedPerms []*pb.ApprovePermission
	handler.OnApprovePermission = func(_ context.Context, cmd *pb.ApprovePermission) error {
		approvedPerms = append(approvedPerms, cmd)
		return nil
	}

	ctx := context.Background()

	// Attach session with pending permissions
	cmd := &pb.AttachSession{
		SessionId:     "sess_test123",
		WorkspacePath: tmpDir + "/workspace1",
		PendingPermissions: []*pb.PendingPermissionResponse{
			{
				RequestId:         "perm_1",
				Approved:          true,
				Reason:            "Approved while suspended",
				RespondedBy:       "user@example.com",
				RespondedAtUnixMs: time.Now().UnixMilli(),
			},
			{
				RequestId:         "perm_2",
				Approved:          false,
				Reason:            "Denied",
				RespondedBy:       "admin@example.com",
				RespondedAtUnixMs: time.Now().UnixMilli(),
			},
		},
	}

	_, err := handler.HandleAttachSession(ctx, cmd)
	require.NoError(t, err)

	// Verify pending permissions were processed
	require.Len(t, approvedPerms, 2)
	assert.Equal(t, "perm_1", approvedPerms[0].RequestId)
	assert.True(t, approvedPerms[0].Approved)
	assert.True(t, approvedPerms[0].FromCache)
	assert.Equal(t, "perm_2", approvedPerms[1].RequestId)
	assert.False(t, approvedPerms[1].Approved)
}

func TestDefaultCommandHandler_ActiveSessionCount(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()
	workspace := NewWorkspaceManager(tmpDir, logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	ctx := context.Background()

	assert.Equal(t, 0, handler.ActiveSessionCount())

	// Attach first session
	_, err := handler.HandleAttachSession(ctx, &pb.AttachSession{
		SessionId:     "sess_1",
		WorkspacePath: tmpDir + "/ws1",
	})
	require.NoError(t, err)
	assert.Equal(t, 1, handler.ActiveSessionCount())

	// Attach second session
	_, err = handler.HandleAttachSession(ctx, &pb.AttachSession{
		SessionId:     "sess_2",
		WorkspacePath: tmpDir + "/ws2",
	})
	require.NoError(t, err)
	assert.Equal(t, 2, handler.ActiveSessionCount())

	// Detach one
	_, err = handler.HandleDetachSession(ctx, &pb.DetachSession{SessionId: "sess_1"})
	require.NoError(t, err)
	assert.Equal(t, 1, handler.ActiveSessionCount())
}
