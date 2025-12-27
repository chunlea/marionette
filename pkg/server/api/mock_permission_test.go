package api

import (
	"context"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMockPermissionService_Get(t *testing.T) {
	svc := NewMockPermissionService()
	ctx := context.Background()

	// Add a permission request
	now := time.Now()
	perm := &store.PermissionRequest{
		ID:                  id.PermissionRequest(),
		SessionID:           "sess_xxx",
		TaskID:              "task_xxx",
		RunID:               "trun_xxx",
		Tool:                "bash",
		Action:              "rm -rf /tmp/test",
		RiskLevel:           "high",
		Status:              "pending",
		SuspendAfterSeconds: 1800,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
	svc.AddPermission(perm)

	// Get existing permission
	got, err := svc.Get(ctx, perm.ID)
	require.NoError(t, err)
	assert.Equal(t, perm.ID, got.ID)
	assert.Equal(t, "bash", got.Tool)
	assert.Equal(t, "pending", got.Status)

	// Get non-existent permission
	_, err = svc.Get(ctx, "perm_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockPermissionService_List(t *testing.T) {
	svc := NewMockPermissionService()
	ctx := context.Background()

	now := time.Now()

	// Add permission requests
	svc.AddPermission(&store.PermissionRequest{
		ID:        id.PermissionRequest(),
		SessionID: "sess_1",
		TaskID:    "task_1",
		RunID:     "trun_1",
		Tool:      "bash",
		Action:    "command 1",
		RiskLevel: "low",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	})
	svc.AddPermission(&store.PermissionRequest{
		ID:        id.PermissionRequest(),
		SessionID: "sess_1",
		TaskID:    "task_2",
		RunID:     "trun_2",
		Tool:      "edit",
		Action:    "modify file",
		RiskLevel: "medium",
		Status:    "approved",
		CreatedAt: now,
		UpdatedAt: now,
	})
	svc.AddPermission(&store.PermissionRequest{
		ID:        id.PermissionRequest(),
		SessionID: "sess_2",
		TaskID:    "task_3",
		RunID:     "trun_3",
		Tool:      "bash",
		Action:    "dangerous command",
		RiskLevel: "critical",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	})

	// List all
	result, err := svc.List(ctx, ListPermissionsOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 3)

	// List by session
	result, err = svc.List(ctx, ListPermissionsOptions{SessionID: "sess_1"})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)

	// List by task
	result, err = svc.List(ctx, ListPermissionsOptions{TaskID: "task_1"})
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)

	// List by status
	result, err = svc.List(ctx, ListPermissionsOptions{Status: []string{"pending"}})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)

	// List by risk level
	result, err = svc.List(ctx, ListPermissionsOptions{RiskLevel: []string{"critical", "high"}})
	require.NoError(t, err)
	assert.Len(t, result.Items, 1)
	assert.Equal(t, "critical", result.Items[0].RiskLevel)

	// List with limit
	result, err = svc.List(ctx, ListPermissionsOptions{Limit: 2})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
}

func TestMockPermissionService_Approve(t *testing.T) {
	svc := NewMockPermissionService()
	ctx := context.Background()

	// Add a pending permission
	now := time.Now()
	perm := &store.PermissionRequest{
		ID:        id.PermissionRequest(),
		SessionID: "sess_xxx",
		TaskID:    "task_xxx",
		RunID:     "trun_xxx",
		Tool:      "bash",
		Action:    "rm -rf /tmp/test",
		RiskLevel: "high",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	svc.AddPermission(perm)

	// Approve without reason
	err := svc.Approve(ctx, perm.ID, ApproveOptions{})
	require.NoError(t, err)

	got, err := svc.Get(ctx, perm.ID)
	require.NoError(t, err)
	assert.Equal(t, "approved", got.Status)
	assert.NotNil(t, got.RespondedAt)
	assert.Nil(t, got.ResponseReason)

	// Try to approve again - should fail
	err = svc.Approve(ctx, perm.ID, ApproveOptions{})
	require.Error(t, err)
	assert.True(t, IsInvalidState(err))

	// Approve with reason
	perm2 := &store.PermissionRequest{
		ID:        id.PermissionRequest(),
		SessionID: "sess_xxx",
		TaskID:    "task_xxx",
		RunID:     "trun_xxx",
		Tool:      "edit",
		Action:    "modify config",
		RiskLevel: "medium",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	svc.AddPermission(perm2)

	err = svc.Approve(ctx, perm2.ID, ApproveOptions{Reason: "Looks safe"})
	require.NoError(t, err)

	got, err = svc.Get(ctx, perm2.ID)
	require.NoError(t, err)
	assert.Equal(t, "approved", got.Status)
	require.NotNil(t, got.ResponseReason)
	assert.Equal(t, "Looks safe", *got.ResponseReason)

	// Approve non-existent permission
	err = svc.Approve(ctx, "perm_nonexistent", ApproveOptions{})
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockPermissionService_Deny(t *testing.T) {
	svc := NewMockPermissionService()
	ctx := context.Background()

	// Add a pending permission
	now := time.Now()
	perm := &store.PermissionRequest{
		ID:        id.PermissionRequest(),
		SessionID: "sess_xxx",
		TaskID:    "task_xxx",
		RunID:     "trun_xxx",
		Tool:      "bash",
		Action:    "rm -rf /",
		RiskLevel: "critical",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	svc.AddPermission(perm)

	// Deny without reason
	err := svc.Deny(ctx, perm.ID, DenyOptions{})
	require.NoError(t, err)

	got, err := svc.Get(ctx, perm.ID)
	require.NoError(t, err)
	assert.Equal(t, "denied", got.Status)
	assert.NotNil(t, got.RespondedAt)
	assert.Nil(t, got.ResponseReason)

	// Try to deny again - should fail
	err = svc.Deny(ctx, perm.ID, DenyOptions{})
	require.Error(t, err)
	assert.True(t, IsInvalidState(err))

	// Deny with reason
	perm2 := &store.PermissionRequest{
		ID:        id.PermissionRequest(),
		SessionID: "sess_xxx",
		TaskID:    "task_xxx",
		RunID:     "trun_xxx",
		Tool:      "bash",
		Action:    "sudo something",
		RiskLevel: "high",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	svc.AddPermission(perm2)

	err = svc.Deny(ctx, perm2.ID, DenyOptions{Reason: "Too dangerous"})
	require.NoError(t, err)

	got, err = svc.Get(ctx, perm2.ID)
	require.NoError(t, err)
	assert.Equal(t, "denied", got.Status)
	require.NotNil(t, got.ResponseReason)
	assert.Equal(t, "Too dangerous", *got.ResponseReason)

	// Deny non-existent permission
	err = svc.Deny(ctx, "perm_nonexistent", DenyOptions{})
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestMockPermissionService_FunctionStubs(t *testing.T) {
	svc := NewMockPermissionService()
	ctx := context.Background()

	// Test custom GetFunc
	customPerm := &store.PermissionRequest{ID: "perm_custom", Tool: "custom"}
	svc.GetFunc = func(_ context.Context, _ string) (*store.PermissionRequest, error) {
		return customPerm, nil
	}

	perm, err := svc.Get(ctx, "any-id")
	require.NoError(t, err)
	assert.Equal(t, "perm_custom", perm.ID)

	// Test custom ApproveFunc
	approvedID := ""
	svc.ApproveFunc = func(_ context.Context, id string, _ ApproveOptions) error {
		approvedID = id
		return nil
	}

	err = svc.Approve(ctx, "test-id", ApproveOptions{})
	require.NoError(t, err)
	assert.Equal(t, "test-id", approvedID)
}

func TestMockPermissionService_Reset(t *testing.T) {
	svc := NewMockPermissionService()

	now := time.Now()
	// Add permissions
	svc.AddPermission(&store.PermissionRequest{
		ID:        id.PermissionRequest(),
		CreatedAt: now,
		UpdatedAt: now,
	})
	svc.AddPermission(&store.PermissionRequest{
		ID:        id.PermissionRequest(),
		CreatedAt: now,
		UpdatedAt: now,
	})

	assert.Len(t, svc.GetAllPermissions(), 2)

	// Reset
	svc.Reset()
	assert.Len(t, svc.GetAllPermissions(), 0)
}
