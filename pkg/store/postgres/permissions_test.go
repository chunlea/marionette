package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// =============================================================================
// PermissionRequest Tests
// =============================================================================

func TestPermissionRequestCRUD(t *testing.T) {
	ctx := context.Background()

	// Create workspace
	workspace := &store.Workspace{
		Name:        "perm-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	// Create session
	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	// Create task
	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Test dangerous operation",
		Status:         "running",
		MaxRetries:     0,
		TimeoutSeconds: 3600,
	}
	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)

	// Create task run
	taskRun := &store.TaskRun{
		TaskID:  task.ID,
		Attempt: 1,
		Status:  "running",
	}
	err = testStore.CreateTaskRun(ctx, taskRun)
	require.NoError(t, err)

	// Create permission request
	permReq := &store.PermissionRequest{
		SessionID:           session.ID,
		TaskID:              task.ID,
		RunID:               taskRun.ID,
		Tool:                "bash",
		Action:              "rm -rf /tmp/test",
		Context:             strPtr("User requested file cleanup"),
		RiskLevel:           "high",
		Status:              "pending",
		SuspendAfterSeconds: 1800,
	}

	err = testStore.CreatePermissionRequest(ctx, permReq)
	require.NoError(t, err)
	assert.NotEmpty(t, permReq.ID)
	assert.NotZero(t, permReq.CreatedAt)

	// Get
	got, err := testStore.GetPermissionRequest(ctx, permReq.ID)
	require.NoError(t, err)
	assert.Equal(t, "bash", got.Tool)
	assert.Equal(t, "rm -rf /tmp/test", got.Action)
	assert.Equal(t, "high", got.RiskLevel)
	assert.Equal(t, "pending", got.Status)

	// Update (approve)
	now := time.Now()
	newStatus := "approved"
	respondedBy := "test-user"
	reason := "Approved for testing"
	err = testStore.UpdatePermissionRequest(ctx, permReq.ID, store.PermissionRequestUpdates{
		Status:         &newStatus,
		RespondedBy:    &respondedBy,
		ResponseReason: &reason,
		RespondedAt:    &now,
	})
	require.NoError(t, err)

	got, err = testStore.GetPermissionRequest(ctx, permReq.ID)
	require.NoError(t, err)
	assert.Equal(t, "approved", got.Status)
	assert.Equal(t, "test-user", *got.RespondedBy)
	assert.Equal(t, "Approved for testing", *got.ResponseReason)
	assert.NotNil(t, got.RespondedAt)

	// List by session
	list, err := testStore.ListPermissionRequests(ctx, store.ListPermissionRequestsOptions{
		SessionID: &session.ID,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// List by status
	pendingStatus := []string{"pending"}
	listPending, err := testStore.ListPermissionRequests(ctx, store.ListPermissionRequestsOptions{
		SessionID: &session.ID,
		Status:    pendingStatus,
	})
	require.NoError(t, err)
	// Should be 0 since we approved the only permission request
	assert.Equal(t, 0, len(listPending.Items))

	// Cleanup
	_ = testStore.DeleteSession(ctx, session.ID)
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

func TestPermissionRequestDenyFlow(t *testing.T) {
	ctx := context.Background()

	// Create workspace, session, task, and task run
	workspace := &store.Workspace{
		Name:        "perm-deny-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Test denied operation",
		Status:         "running",
		MaxRetries:     0,
		TimeoutSeconds: 3600,
	}
	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)

	taskRun := &store.TaskRun{
		TaskID:  task.ID,
		Attempt: 1,
		Status:  "running",
	}
	err = testStore.CreateTaskRun(ctx, taskRun)
	require.NoError(t, err)

	// Create permission request
	permReq := &store.PermissionRequest{
		SessionID:           session.ID,
		TaskID:              task.ID,
		RunID:               taskRun.ID,
		Tool:                "edit",
		Action:              "modify production config",
		RiskLevel:           "critical",
		Status:              "pending",
		SuspendAfterSeconds: 1800,
	}
	err = testStore.CreatePermissionRequest(ctx, permReq)
	require.NoError(t, err)

	// Deny the request
	now := time.Now()
	deniedStatus := "denied"
	respondedBy := "admin"
	reason := "Too risky for production"
	err = testStore.UpdatePermissionRequest(ctx, permReq.ID, store.PermissionRequestUpdates{
		Status:         &deniedStatus,
		RespondedBy:    &respondedBy,
		ResponseReason: &reason,
		RespondedAt:    &now,
	})
	require.NoError(t, err)

	// Verify denial
	got, err := testStore.GetPermissionRequest(ctx, permReq.ID)
	require.NoError(t, err)
	assert.Equal(t, "denied", got.Status)
	assert.Equal(t, "admin", *got.RespondedBy)
	assert.Equal(t, "Too risky for production", *got.ResponseReason)

	// Cleanup
	_ = testStore.DeleteSession(ctx, session.ID)
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

// =============================================================================
// Helper Functions
// =============================================================================
