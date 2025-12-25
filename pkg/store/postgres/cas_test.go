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
// CAS Tests
// =============================================================================

func TestChunkCRUD(t *testing.T) {
	ctx := context.Background()
	tenantID := "test-tenant"

	// Create
	chunk := &store.Chunk{
		Hash:     "sha256-test-chunk-" + time.Now().Format("150405"),
		TenantID: tenantID,
		Size:     1024,
		RefCount: 1,
	}

	err := testStore.CreateChunk(ctx, chunk)
	require.NoError(t, err)
	assert.NotZero(t, chunk.CreatedAt)

	// Get
	got, err := testStore.GetChunk(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)
	assert.Equal(t, chunk.Hash, got.Hash)
	assert.Equal(t, chunk.TenantID, got.TenantID)
	assert.Equal(t, int64(1024), got.Size)
	assert.Equal(t, 1, got.RefCount)

	// IncrementChunkRef
	err = testStore.IncrementChunkRef(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)

	got, err = testStore.GetChunk(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)
	assert.Equal(t, 2, got.RefCount)

	// DecrementChunkRef
	err = testStore.DecrementChunkRef(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)

	got, err = testStore.GetChunk(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)
	assert.Equal(t, 1, got.RefCount)

	// Update
	newRefCount := 5
	err = testStore.UpdateChunk(ctx, tenantID, chunk.Hash, store.ChunkUpdates{
		RefCount: &newRefCount,
	})
	require.NoError(t, err)

	got, err = testStore.GetChunk(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)
	assert.Equal(t, 5, got.RefCount)

	// Delete
	err = testStore.DeleteChunk(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)

	_, err = testStore.GetChunk(ctx, tenantID, chunk.Hash)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestChunkNotFound(t *testing.T) {
	ctx := context.Background()

	_, err := testStore.GetChunk(ctx, "test-tenant", "sha256-nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)

	var notFoundErr *store.NotFoundError
	assert.ErrorAs(t, err, &notFoundErr)
	assert.Equal(t, "chunk", notFoundErr.Resource)
}

func TestChunkRefCountOperations(t *testing.T) {
	ctx := context.Background()
	tenantID := "test-tenant"

	// Create chunk with RefCount = 0
	chunk := &store.Chunk{
		Hash:     "sha256-refcount-test-" + time.Now().Format("150405"),
		TenantID: tenantID,
		Size:     2048,
		RefCount: 0,
	}

	err := testStore.CreateChunk(ctx, chunk)
	require.NoError(t, err)

	// Increment from 0 to 1
	err = testStore.IncrementChunkRef(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)

	got, err := testStore.GetChunk(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)
	assert.Equal(t, 1, got.RefCount)

	// Decrement back to 0
	err = testStore.DecrementChunkRef(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)

	got, err = testStore.GetChunk(ctx, tenantID, chunk.Hash)
	require.NoError(t, err)
	assert.Equal(t, 0, got.RefCount)

	// Try to decrement when already 0 (should fail)
	err = testStore.DecrementChunkRef(ctx, tenantID, chunk.Hash)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup
	_ = testStore.DeleteChunk(ctx, tenantID, chunk.Hash)
}

func TestListUnreferencedChunks(t *testing.T) {
	ctx := context.Background()
	tenantID := "test-tenant-gc"

	// Create chunks with RefCount = 0 (unreferenced)
	var createdHashes []string
	for i := 0; i < 3; i++ {
		chunk := &store.Chunk{
			Hash:     "sha256-gc-test-" + time.Now().Format("150405.000") + "-" + string(rune('a'+i)),
			TenantID: tenantID,
			Size:     int64(1024 * (i + 1)),
			RefCount: 0,
		}
		err := testStore.CreateChunk(ctx, chunk)
		require.NoError(t, err)
		createdHashes = append(createdHashes, chunk.Hash)
		time.Sleep(time.Millisecond) // Ensure different creation times
	}

	// Create a chunk with RefCount = 1 (referenced, should not appear)
	referencedChunk := &store.Chunk{
		Hash:     "sha256-referenced-" + time.Now().Format("150405"),
		TenantID: tenantID,
		Size:     512,
		RefCount: 1,
	}
	err := testStore.CreateChunk(ctx, referencedChunk)
	require.NoError(t, err)

	// List unreferenced chunks
	chunks, err := testStore.ListUnreferencedChunks(ctx, tenantID, 10)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(chunks), 3)

	// Verify only unreferenced chunks are returned
	for _, chunk := range chunks {
		if chunk.TenantID == tenantID {
			assert.Equal(t, 0, chunk.RefCount)
		}
	}

	// Cleanup
	for _, hash := range createdHashes {
		_ = testStore.DeleteChunk(ctx, tenantID, hash)
	}
	_ = testStore.DeleteChunk(ctx, tenantID, referencedChunk.Hash)
}

func TestManifestCRUD(t *testing.T) {
	ctx := context.Background()
	tenantID := "test-tenant"

	// Create workspace first (required for manifest)
	workspace := &store.Workspace{
		Name:        "manifest-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	// Create manifest
	manifest := &store.Manifest{
		WorkspaceID: workspace.ID,
		TotalSize:   10240,
		SingleChunk: true,
		ChunkHash:   strPtr("sha256-manifest-chunk"),
		ChunkCount:  1,
		TenantID:    tenantID,
	}

	err = testStore.CreateManifest(ctx, manifest)
	require.NoError(t, err)
	assert.NotEmpty(t, manifest.ID)
	assert.NotZero(t, manifest.CreatedAt)

	// Get
	got, err := testStore.GetManifest(ctx, manifest.ID)
	require.NoError(t, err)
	assert.Equal(t, manifest.WorkspaceID, got.WorkspaceID)
	assert.Equal(t, int64(10240), got.TotalSize)
	assert.True(t, got.SingleChunk)
	assert.NotNil(t, got.ChunkHash)
	assert.Equal(t, "sha256-manifest-chunk", *got.ChunkHash)
	assert.Equal(t, 1, got.ChunkCount)
	assert.Equal(t, tenantID, got.TenantID)

	// Delete
	err = testStore.DeleteManifest(ctx, manifest.ID)
	require.NoError(t, err)

	_, err = testStore.GetManifest(ctx, manifest.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup workspace
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

func TestManifestNotFound(t *testing.T) {
	ctx := context.Background()

	_, err := testStore.GetManifest(ctx, "mfst_nonexistent12345")
	assert.ErrorIs(t, err, store.ErrNotFound)

	var notFoundErr *store.NotFoundError
	assert.ErrorAs(t, err, &notFoundErr)
	assert.Equal(t, "manifest", notFoundErr.Resource)
}

func TestGetLatestManifest(t *testing.T) {
	ctx := context.Background()
	tenantID := "test-tenant"

	// Create workspace
	workspace := &store.Workspace{
		Name:        "latest-manifest-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	// Create multiple manifests
	var manifestIDs []string
	for i := 0; i < 3; i++ {
		manifest := &store.Manifest{
			WorkspaceID: workspace.ID,
			TotalSize:   int64(1024 * (i + 1)),
			SingleChunk: false,
			ChunkCount:  i + 1,
			TenantID:    tenantID,
		}
		err := testStore.CreateManifest(ctx, manifest)
		require.NoError(t, err)
		manifestIDs = append(manifestIDs, manifest.ID)
		time.Sleep(time.Millisecond) // Ensure different creation times
	}

	// Get latest manifest (should be the last one created)
	latest, err := testStore.GetLatestManifest(ctx, workspace.ID)
	require.NoError(t, err)
	assert.Equal(t, manifestIDs[2], latest.ID)
	assert.Equal(t, int64(3072), latest.TotalSize)
	assert.Equal(t, 3, latest.ChunkCount)

	// Cleanup
	for _, id := range manifestIDs {
		_ = testStore.DeleteManifest(ctx, id)
	}
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

func TestListManifests(t *testing.T) {
	ctx := context.Background()
	tenantID := "test-tenant"

	// Create workspace
	workspace := &store.Workspace{
		Name:        "list-manifest-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	// Create multiple manifests
	var manifestIDs []string
	for i := 0; i < 5; i++ {
		manifest := &store.Manifest{
			WorkspaceID: workspace.ID,
			TotalSize:   int64(512 * (i + 1)),
			SingleChunk: false,
			ChunkCount:  i + 1,
			TenantID:    tenantID,
		}
		err := testStore.CreateManifest(ctx, manifest)
		require.NoError(t, err)
		manifestIDs = append(manifestIDs, manifest.ID)
		time.Sleep(time.Millisecond) // Ensure different creation times
	}

	// List with limit
	manifests, err := testStore.ListManifests(ctx, workspace.ID, 3)
	require.NoError(t, err)
	assert.Len(t, manifests, 3)

	// Verify they are ordered by created_at DESC (newest first)
	assert.Equal(t, manifestIDs[4], manifests[0].ID)
	assert.Equal(t, manifestIDs[3], manifests[1].ID)
	assert.Equal(t, manifestIDs[2], manifests[2].ID)

	// List all
	manifests, err = testStore.ListManifests(ctx, workspace.ID, 100)
	require.NoError(t, err)
	assert.Len(t, manifests, 5)

	// Cleanup
	for _, id := range manifestIDs {
		_ = testStore.DeleteManifest(ctx, id)
	}
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

func TestDeleteManifestsByWorkspace(t *testing.T) {
	ctx := context.Background()
	tenantID := "test-tenant"

	// Create workspace
	workspace := &store.Workspace{
		Name:        "delete-manifests-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	// Create multiple manifests
	for i := 0; i < 3; i++ {
		manifest := &store.Manifest{
			WorkspaceID: workspace.ID,
			TotalSize:   int64(1024 * (i + 1)),
			SingleChunk: false,
			ChunkCount:  i + 1,
			TenantID:    tenantID,
		}
		err := testStore.CreateManifest(ctx, manifest)
		require.NoError(t, err)
	}

	// Verify manifests exist
	manifests, err := testStore.ListManifests(ctx, workspace.ID, 100)
	require.NoError(t, err)
	assert.Len(t, manifests, 3)

	// Delete all manifests for workspace
	err = testStore.DeleteManifestsByWorkspace(ctx, workspace.ID)
	require.NoError(t, err)

	// Verify all deleted
	manifests, err = testStore.ListManifests(ctx, workspace.ID, 100)
	require.NoError(t, err)
	assert.Len(t, manifests, 0)

	// Cleanup workspace
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

func TestManifestWithParent(t *testing.T) {
	ctx := context.Background()
	tenantID := "test-tenant"

	// Create workspace
	workspace := &store.Workspace{
		Name:        "parent-manifest-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	// Create parent manifest
	parentManifest := &store.Manifest{
		WorkspaceID: workspace.ID,
		TotalSize:   5120,
		SingleChunk: false,
		ChunkCount:  2,
		TenantID:    tenantID,
	}
	err = testStore.CreateManifest(ctx, parentManifest)
	require.NoError(t, err)

	// Create child manifest with parent reference
	childManifest := &store.Manifest{
		WorkspaceID: workspace.ID,
		ParentID:    &parentManifest.ID,
		TotalSize:   6144,
		SingleChunk: false,
		ChunkCount:  3,
		TenantID:    tenantID,
	}
	err = testStore.CreateManifest(ctx, childManifest)
	require.NoError(t, err)

	// Get child and verify parent reference
	got, err := testStore.GetManifest(ctx, childManifest.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.ParentID)
	assert.Equal(t, parentManifest.ID, *got.ParentID)

	// Cleanup
	_ = testStore.DeleteManifest(ctx, childManifest.ID)
	_ = testStore.DeleteManifest(ctx, parentManifest.ID)
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

// =============================================================================
// Helper Functions
// =============================================================================

