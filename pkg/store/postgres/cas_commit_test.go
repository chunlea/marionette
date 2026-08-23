package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/storage/cas"
	"github.com/chunlea/marionette/pkg/store"
)

func newCommitWorkspace(ctx context.Context, t *testing.T) *store.Workspace {
	t.Helper()

	ws := &store.Workspace{
		Name:        "commit-ws-" + time.Now().Format("150405.000000000"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	require.NoError(t, testStore.CreateWorkspace(ctx, ws))
	t.Cleanup(func() { _ = testStore.DeleteWorkspace(context.Background(), ws.ID) })
	return ws
}

func TestCommitManifestTakesReferences(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	ws := newCommitWorkspace(ctx, t)

	chunks := []*store.Chunk{
		{Hash: chunkHash("commit-a"), Size: 10},
		{Hash: chunkHash("commit-b"), Size: 20},
	}

	manifest := &store.Manifest{
		WorkspaceID: ws.ID,
		TenantID:    tenant,
		TotalSize:   30,
		SingleChunk: false,
		ChunkCount:  len(chunks),
	}

	require.NoError(t, testStore.CommitManifest(ctx, manifest, chunks))
	assert.NotEmpty(t, manifest.ID)

	for _, chunk := range chunks {
		got, err := testStore.GetChunk(ctx, tenant, chunk.Hash)
		require.NoErrorf(t, err, "chunk %s was not registered", chunk.Hash)
		assert.Equal(t, 1, got.RefCount)
		assert.Nil(t, got.DeletedAt)
		assert.Equal(t, chunk.Size, got.Size)
	}

	stored, err := testStore.GetManifest(ctx, manifest.ID)
	require.NoError(t, err)
	assert.Equal(t, ws.ID, stored.WorkspaceID)
}

// TestCommitManifestIsAtomic is the reason this exists: registering chunks and
// creating the manifest used to be separate calls, so a failure in between left
// chunks that nothing referenced and a workspace with no manifest.
func TestCommitManifestIsAtomic(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)

	chunks := []*store.Chunk{{Hash: chunkHash("atomic-a"), Size: 10}}

	// A workspace id that does not exist: the manifest insert violates its
	// foreign key, after the chunks have been registered and referenced.
	manifest := &store.Manifest{
		WorkspaceID: "ws_does_not_exist",
		TenantID:    tenant,
		TotalSize:   10,
		ChunkCount:  1,
	}

	err := testStore.CommitManifest(ctx, manifest, chunks)
	require.Error(t, err, "a manifest against a missing workspace must not commit")

	_, err = testStore.GetChunk(ctx, tenant, chunks[0].Hash)
	assert.ErrorIs(t, err, store.ErrNotFound,
		"the chunk registration should have rolled back with the manifest")
}

// TestCommitManifestSurvivesAnAggressiveCollector covers the gap the earlier
// two-step commit left: with a grace period of one nanosecond the collector is
// free to reclaim a chunk between registering it and referencing it. Inside one
// transaction there is no such gap.
func TestCommitManifestSurvivesAnAggressiveCollector(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	ws := newCommitWorkspace(ctx, t)
	blobs := newMemoryChunkStore()
	gc := newTestGC(t, blobs)

	const commits = 40

	gcCtx, stopGC := context.WithCancel(ctx)
	gcDone := make(chan struct{})
	go func() {
		defer close(gcDone)
		for {
			select {
			case <-gcCtx.Done():
				return
			default:
			}
			_, _ = gc.RunGC(gcCtx, tenant)
		}
	}()

	committed := make([]*store.Manifest, 0, commits)
	for i := 0; i < commits; i++ {
		hash := chunkHash("aggressive-" + time.Now().Format("150405.000000000") + string(rune('a'+i%26)))

		_, err := blobs.StoreChunk(ctx, tenant, hash, []byte("payload"))
		require.NoError(t, err)

		manifest := &store.Manifest{
			WorkspaceID: ws.ID,
			TenantID:    tenant,
			TotalSize:   7,
			SingleChunk: true,
			ChunkHash:   &hash,
			ChunkCount:  1,
		}
		require.NoErrorf(t, testStore.CommitManifest(ctx, manifest, []*store.Chunk{{Hash: hash, Size: 7}}),
			"commit %d lost its chunk to the collector", i)
		committed = append(committed, manifest)
	}

	stopGC()
	<-gcDone

	// Every committed manifest still has its chunk.
	for i, manifest := range committed {
		require.NotNil(t, manifest.ChunkHash)
		chunk, err := testStore.GetChunk(ctx, tenant, *manifest.ChunkHash)
		require.NoErrorf(t, err, "manifest %d points at a collected chunk", i)
		assert.Positive(t, chunk.RefCount)
		assert.Nil(t, chunk.DeletedAt)
	}
}

func TestCommitManifestDeduplicatesChunks(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	ws := newCommitWorkspace(ctx, t)

	hash := chunkHash("repeated")
	chunks := []*store.Chunk{
		{Hash: hash, Size: 5},
		{Hash: hash, Size: 5},
		{Hash: hash, Size: 5},
	}

	manifest := &store.Manifest{
		WorkspaceID: ws.ID,
		TenantID:    tenant,
		TotalSize:   15,
		ChunkCount:  3,
	}
	require.NoError(t, testStore.CommitManifest(ctx, manifest, chunks))

	got, err := testStore.GetChunk(ctx, tenant, hash)
	require.NoError(t, err)
	assert.Equal(t, 1, got.RefCount,
		"a chunk appearing several times in one manifest is still one reference")
}

func TestCommitManifestSharedChunkAcrossManifests(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	ws := newCommitWorkspace(ctx, t)

	hash := chunkHash("shared-across")
	chunks := []*store.Chunk{{Hash: hash, Size: 5}}

	first := &store.Manifest{WorkspaceID: ws.ID, TenantID: tenant, TotalSize: 5, ChunkCount: 1}
	second := &store.Manifest{WorkspaceID: ws.ID, TenantID: tenant, TotalSize: 5, ChunkCount: 1}

	require.NoError(t, testStore.CommitManifest(ctx, first, chunks))
	require.NoError(t, testStore.CommitManifest(ctx, second, chunks))

	got, err := testStore.GetChunk(ctx, tenant, hash)
	require.NoError(t, err)
	assert.Equal(t, 2, got.RefCount)

	// Releasing one manifest leaves the chunk alive for the other.
	require.NoError(t, testStore.ReleaseManifest(ctx, first.ID, chunks))

	got, err = testStore.GetChunk(ctx, tenant, hash)
	require.NoError(t, err)
	assert.Equal(t, 1, got.RefCount, "releasing one manifest must not orphan a shared chunk")

	_, err = testStore.GetManifest(ctx, first.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Releasing the last one drops it to zero, where the collector can take it.
	require.NoError(t, testStore.ReleaseManifest(ctx, second.ID, chunks))

	got, err = testStore.GetChunk(ctx, tenant, hash)
	require.NoError(t, err)
	assert.Zero(t, got.RefCount)

	blobs := newMemoryChunkStore()
	result, err := cas.NewGC(testStore, blobs, cas.GCConfig{GracePeriod: time.Nanosecond, BatchSize: 16}).
		RunGC(ctx, tenant)
	require.NoError(t, err)
	assert.Equal(t, 1, result.ChunksDeleted)

	_, err = testStore.GetChunk(ctx, tenant, hash)
	assert.ErrorIs(t, err, store.ErrNotFound, "a fully released chunk should be collectable")
}

func TestCommitManifestRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	ws := newCommitWorkspace(ctx, t)

	t.Run("nil manifest", func(t *testing.T) {
		err := testStore.CommitManifest(ctx, nil, nil)
		assert.ErrorIs(t, err, store.ErrInvalidInput)
	})

	t.Run("no tenant", func(t *testing.T) {
		err := testStore.CommitManifest(ctx, &store.Manifest{WorkspaceID: ws.ID}, nil)
		assert.ErrorIs(t, err, store.ErrInvalidInput)
	})

	t.Run("chunk with no hash", func(t *testing.T) {
		tenant := newTenant(t)
		err := testStore.CommitManifest(ctx,
			&store.Manifest{WorkspaceID: ws.ID, TenantID: tenant},
			[]*store.Chunk{{Size: 1}})
		assert.ErrorIs(t, err, store.ErrInvalidInput)
	})
}

// A content-defined manifest is the case the schema always described and
// nothing ever wrote: thousands of chunk references, the file list in the
// object store rather than the row, and all of it in one transaction.
func TestCommitManifestForAContentDefinedSnapshot(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	ws := newCommitWorkspace(ctx, t)

	// More than one batch of references, so the batching is exercised rather
	// than merely present.
	const count = 2500
	chunks := make([]*store.Chunk, 0, count)
	for i := 0; i < count; i++ {
		chunks = append(chunks, &store.Chunk{
			Hash: chunkHash(fmt.Sprintf("cdc-commit-%d-%s", i, tenant)),
			Size: int64(1024 + i),
		})
	}

	manifest := &store.Manifest{
		WorkspaceID: ws.ID,
		TenantID:    tenant,
		TotalSize:   1 << 30,
		SingleChunk: false,
		ChunkCount:  count,
		// The file list lives in the manifest object. A row holding a million
		// paths is the thing this mode exists to avoid.
		FilesJSON: nil,
	}

	require.NoError(t, testStore.CommitManifest(ctx, manifest, chunks))

	stored, err := testStore.GetManifest(ctx, manifest.ID)
	require.NoError(t, err)
	assert.False(t, stored.SingleChunk)
	assert.Equal(t, count, stored.ChunkCount)
	assert.Nil(t, stored.ChunkHash)
	assert.Empty(t, stored.FilesJSON, "a content-defined manifest keeps its file list in the object store")

	for _, sample := range []int{0, count / 2, count - 1} {
		got, err := testStore.GetChunk(ctx, tenant, chunks[sample].Hash)
		require.NoErrorf(t, err, "chunk %d was not registered", sample)
		assert.Equal(t, 1, got.RefCount)
		assert.Equal(t, chunks[sample].Size, got.Size)
		assert.Nil(t, got.DeletedAt)
	}

	// Releasing it drops every reference, leaving the chunks to the collector.
	require.NoError(t, testStore.ReleaseManifest(ctx, manifest.ID, chunks))

	_, err = testStore.GetManifest(ctx, manifest.ID)
	require.Error(t, err)

	for _, sample := range []int{0, count - 1} {
		got, err := testStore.GetChunk(ctx, tenant, chunks[sample].Hash)
		require.NoError(t, err)
		assert.Equal(t, 0, got.RefCount)
	}
}

// A manifest that names the same chunk in many files must reference it once,
// however many times it appears - which is the normal case for a workspace
// with a thousand copies of the same license file.
func TestCommitManifestDeduplicatesAcrossBatches(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	ws := newCommitWorkspace(ctx, t)

	repeated := chunkHash("cdc-repeated-" + tenant)
	chunks := make([]*store.Chunk, 0, 3000)
	for i := 0; i < 3000; i++ {
		chunks = append(chunks, &store.Chunk{Hash: repeated, Size: 42})
	}

	manifest := &store.Manifest{
		WorkspaceID: ws.ID,
		TenantID:    tenant,
		ChunkCount:  1,
	}
	require.NoError(t, testStore.CommitManifest(ctx, manifest, chunks))

	got, err := testStore.GetChunk(ctx, tenant, repeated)
	require.NoError(t, err)
	assert.Equal(t, 1, got.RefCount)
}
