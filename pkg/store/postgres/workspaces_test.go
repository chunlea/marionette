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
// Workspace Tests
// =============================================================================

func TestWorkspaceCRUD(t *testing.T) {
	ctx := context.Background()

	// Create
	workspace := &store.Workspace{
		Name:        "test-workspace-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}

	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)
	assert.NotEmpty(t, workspace.ID)

	// Get
	got, err := testStore.GetWorkspace(ctx, workspace.ID)
	require.NoError(t, err)
	assert.Equal(t, workspace.Name, got.Name)

	// Update
	newMobility := "shared"
	err = testStore.UpdateWorkspace(ctx, workspace.ID, store.WorkspaceUpdates{
		Mobility: &newMobility,
	})
	require.NoError(t, err)

	got, err = testStore.GetWorkspace(ctx, workspace.ID)
	require.NoError(t, err)
	assert.Equal(t, "shared", got.Mobility)

	// Soft delete
	err = testStore.DeleteWorkspace(ctx, workspace.ID)
	require.NoError(t, err)

	// Order newest-first: the default is created_at ASC with a 50-row page, so
	// this workspace falls off the first page as soon as the suite has created
	// enough workspaces, and both assertions below would pass vacuously.
	newestFirst := store.BaseListOptions{OrderDesc: true}

	// Should not appear in list without IncludeDeleted
	list, err := testStore.ListWorkspaces(ctx, store.ListWorkspacesOptions{
		BaseListOptions: newestFirst,
	})
	require.NoError(t, err)
	for _, w := range list.Items {
		assert.NotEqual(t, workspace.ID, w.ID)
	}

	// Should appear with IncludeDeleted
	list, err = testStore.ListWorkspaces(ctx, store.ListWorkspacesOptions{
		BaseListOptions: newestFirst,
		IncludeDeleted:  true,
	})
	require.NoError(t, err)
	found := false
	for _, w := range list.Items {
		if w.ID == workspace.ID {
			found = true
			assert.NotNil(t, w.DeletedAt)
		}
	}
	assert.True(t, found, "deleted workspace should appear with IncludeDeleted")
}
