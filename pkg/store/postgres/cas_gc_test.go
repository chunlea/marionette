package postgres_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/storage/cas"
	"github.com/chunlea/marionette/pkg/store"
)

// memoryChunkStore is the blob half of the CAS, in memory. The GC tests here
// are about the metadata race, so the blobs only need to exist and disappear.
type memoryChunkStore struct {
	mu     sync.Mutex
	blobs  map[string][]byte
	failOn map[string]bool
}

func newMemoryChunkStore() *memoryChunkStore {
	return &memoryChunkStore{
		blobs:  make(map[string][]byte),
		failOn: make(map[string]bool),
	}
}

func (m *memoryChunkStore) key(tenantID, hash string) string { return tenantID + "/" + hash }

func (m *memoryChunkStore) StoreChunk(_ context.Context, tenantID, hash string, data []byte) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.blobs[m.key(tenantID, hash)] = data
	return int64(len(data)), nil
}

func (m *memoryChunkStore) GetChunk(_ context.Context, tenantID, hash string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.blobs[m.key(tenantID, hash)]
	if !ok {
		return nil, cas.ErrChunkNotFound
	}
	return data, nil
}

func (m *memoryChunkStore) DeleteChunk(_ context.Context, tenantID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOn[m.key(tenantID, hash)] {
		return fmt.Errorf("simulated blob delete failure")
	}
	delete(m.blobs, m.key(tenantID, hash))
	return nil
}

func (m *memoryChunkStore) ChunkExists(_ context.Context, tenantID, hash string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.blobs[m.key(tenantID, hash)]
	return ok, nil
}

func (m *memoryChunkStore) has(tenantID, hash string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.blobs[m.key(tenantID, hash)]
	return ok
}

func chunkHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// newTestGC builds the most hostile collector the config allows: a grace period
// of one nanosecond, so any chunk older than an instant is markable and any
// mark older than an instant is sweepable. Only the ref_count guards stand
// between a live chunk and deletion.
//
// Note this cannot be zero — NewGC reads a non-positive grace period as "unset"
// and substitutes the seven-day default.
func newTestGC(t *testing.T, blobs *memoryChunkStore) *cas.GC {
	t.Helper()
	return cas.NewGC(testStore, blobs, cas.GCConfig{
		GracePeriod: time.Nanosecond,
		BatchSize:   64,
	})
}

func newTenant(t *testing.T) string {
	t.Helper()

	tenant := "gc-tenant-" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		_, _ = testStore.Pool().Exec(context.Background(),
			"DELETE FROM chunks WHERE tenant_id = $1", tenant)
	})
	return tenant
}

func TestGCCollectsOnlyUnreferencedChunks(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	blobs := newMemoryChunkStore()
	gc := newTestGC(t, blobs)

	abandoned := chunkHash("abandoned")
	referenced := chunkHash("referenced")

	for _, hash := range []string{abandoned, referenced} {
		_, err := blobs.StoreChunk(ctx, tenant, hash, []byte("data"))
		require.NoError(t, err)
		require.NoError(t, testStore.RegisterChunk(ctx, tenant, hash, 4))
	}
	require.NoError(t, testStore.IncrementChunkRef(ctx, tenant, referenced))

	result, err := gc.RunGC(ctx, tenant)
	require.NoError(t, err)
	require.Empty(t, result.Errors)

	// The referenced chunk survives, metadata and blob.
	got, err := testStore.GetChunk(ctx, tenant, referenced)
	require.NoError(t, err, "a referenced chunk must not be collected")
	assert.Equal(t, 1, got.RefCount)
	assert.Nil(t, got.DeletedAt)
	assert.True(t, blobs.has(tenant, referenced), "blob of a referenced chunk was deleted")

	// The abandoned one is gone — otherwise this test would pass for a GC that
	// does nothing at all.
	_, err = testStore.GetChunk(ctx, tenant, abandoned)
	assert.Error(t, err, "an unreferenced chunk should have been collected")
	assert.False(t, blobs.has(tenant, abandoned), "blob of a collected chunk was left behind")
	assert.Equal(t, 1, result.ChunksDeleted)
}

// TestGCReferenceAfterMarkResurrects covers the window the old mark/sweep lost
// data in: a chunk marked while unreferenced, then referenced before the sweep.
func TestGCReferenceAfterMarkResurrects(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	blobs := newMemoryChunkStore()
	gc := newTestGC(t, blobs)

	hash := chunkHash("marked-then-referenced")
	_, err := blobs.StoreChunk(ctx, tenant, hash, []byte("data"))
	require.NoError(t, err)
	require.NoError(t, testStore.RegisterChunk(ctx, tenant, hash, 4))

	marked, err := gc.Mark(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, 1, marked)

	before, err := testStore.GetChunk(ctx, tenant, hash)
	require.NoError(t, err)
	require.NotNil(t, before.DeletedAt, "chunk should be marked at this point")

	// Referencing it must clear the mark in the same statement.
	require.NoError(t, testStore.IncrementChunkRef(ctx, tenant, hash))

	after, err := testStore.GetChunk(ctx, tenant, hash)
	require.NoError(t, err)
	assert.Nil(t, after.DeletedAt, "referencing a chunk must resurrect it")

	deleted, _, err := gc.Sweep(ctx, tenant)
	require.NoError(t, err)
	assert.Zero(t, deleted, "the sweep deleted a chunk that had been referenced")

	_, err = testStore.GetChunk(ctx, tenant, hash)
	assert.NoError(t, err, "live chunk was collected")
	assert.True(t, blobs.has(tenant, hash))
}

// TestGCSweepSkipsChunksReferencedWhileStillMarked covers the other half: a row
// that is still marked but has been referenced again. The old sweep looked only
// at deleted_at and deleted it.
func TestGCSweepSkipsChunksReferencedWhileStillMarked(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	blobs := newMemoryChunkStore()
	gc := newTestGC(t, blobs)

	hash := chunkHash("marked-and-referenced")
	_, err := blobs.StoreChunk(ctx, tenant, hash, []byte("data"))
	require.NoError(t, err)
	require.NoError(t, testStore.RegisterChunk(ctx, tenant, hash, 4))

	// Reference it and re-apply the mark directly, producing the inconsistent
	// state a lost race leaves behind: ref_count > 0 with deleted_at set.
	require.NoError(t, testStore.IncrementChunkRef(ctx, tenant, hash))
	_, err = testStore.Pool().Exec(ctx,
		"UPDATE chunks SET deleted_at = NOW() - INTERVAL '1 day' WHERE tenant_id = $1 AND hash = $2",
		tenant, hash)
	require.NoError(t, err)

	deleted, _, err := gc.Sweep(ctx, tenant)
	require.NoError(t, err)
	assert.Zero(t, deleted, "a chunk with ref_count > 0 must never be swept")

	_, err = testStore.GetChunk(ctx, tenant, hash)
	assert.NoError(t, err, "live chunk was collected")
}

// TestGCGracePeriodProtectsUncommittedUploads covers the upload-then-commit
// window: a chunk that has been written but whose manifest has not been
// committed is legitimately unreferenced and must not be marked.
func TestGCGracePeriodProtectsUncommittedUploads(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	blobs := newMemoryChunkStore()

	gc := cas.NewGC(testStore, blobs, cas.GCConfig{
		GracePeriod: time.Hour,
		BatchSize:   64,
	})

	hash := chunkHash("just-uploaded")
	_, err := blobs.StoreChunk(ctx, tenant, hash, []byte("data"))
	require.NoError(t, err)
	require.NoError(t, testStore.RegisterChunk(ctx, tenant, hash, 4))

	marked, err := gc.Mark(ctx, tenant)
	require.NoError(t, err)
	assert.Zero(t, marked, "a chunk inside its commit window must not be marked")

	got, err := testStore.GetChunk(ctx, tenant, hash)
	require.NoError(t, err)
	assert.Nil(t, got.DeletedAt)
}

func TestGCReportsBlobDeleteFailures(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	blobs := newMemoryChunkStore()
	gc := newTestGC(t, blobs)

	hash := chunkHash("blob-delete-fails")
	_, err := blobs.StoreChunk(ctx, tenant, hash, []byte("data"))
	require.NoError(t, err)
	require.NoError(t, testStore.RegisterChunk(ctx, tenant, hash, 4))
	blobs.failOn[blobs.key(tenant, hash)] = true

	marked, err := gc.Mark(ctx, tenant)
	require.NoError(t, err)
	require.Equal(t, 1, marked)

	deleted, _, err := gc.Sweep(ctx, tenant)
	require.Error(t, err, "a failed blob delete leaves an orphan and must be reported")
	assert.Contains(t, err.Error(), hash)

	// The row goes first, so it is gone even though the blob delete failed.
	assert.Equal(t, 1, deleted)
	_, err = testStore.GetChunk(ctx, tenant, hash)
	assert.Error(t, err, "the database row should have been deleted first")
}

// TestGCConcurrentWritersKeepTheirChunks is the proof the brief asks for: many
// writers registering and referencing chunks while the collector runs flat out
// with no grace period. Every chunk a writer committed must survive.
func TestGCConcurrentWritersKeepTheirChunks(t *testing.T) {
	ctx := context.Background()
	tenant := newTenant(t)
	blobs := newMemoryChunkStore()
	gc := newTestGC(t, blobs)

	const (
		writers          = 8
		chunksPerWriter  = 25
		abandonedPerCall = 4
	)

	var (
		mu         sync.Mutex
		committed  []string
		reclaimed  int
		abandoned  []string
		writeGroup sync.WaitGroup
	)

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
			// Errors here are not fatal to the test: what matters is that no
			// committed chunk goes missing, which is asserted at the end.
			_, _ = gc.RunGC(gcCtx, tenant)
		}
	}()

	for w := 0; w < writers; w++ {
		writeGroup.Add(1)
		go func(writer int) {
			defer writeGroup.Done()

			for i := 0; i < chunksPerWriter; i++ {
				hash := chunkHash(fmt.Sprintf("w%d-c%d-%s", writer, i, tenant))

				if _, err := blobs.StoreChunk(ctx, tenant, hash, []byte("payload")); err != nil {
					t.Errorf("StoreChunk: %v", err)
					return
				}
				if err := testStore.RegisterChunk(ctx, tenant, hash, 7); err != nil {
					t.Errorf("RegisterChunk: %v", err)
					return
				}

				// The commit. Between the register above and this call the
				// collector is free to mark and sweep, which is the race.
				err := testStore.IncrementChunkRef(ctx, tenant, hash)

				mu.Lock()
				switch {
				case err == nil:
					committed = append(committed, hash)
				case errors.Is(err, store.ErrNotFound):
					// Collected before this writer committed. With a grace
					// period of one nanosecond that is the correct outcome —
					// the window really is that short — and what matters is
					// that the writer is told, rather than ending up with a
					// manifest pointing at a chunk that is gone.
					reclaimed++
				default:
					t.Errorf("IncrementChunkRef(%s): %v", hash, err)
				}
				mu.Unlock()

				if err != nil && !errors.Is(err, store.ErrNotFound) {
					return
				}
			}

			// Chunks that are uploaded and then never committed, so the test
			// cannot pass by the collector simply doing nothing.
			for i := 0; i < abandonedPerCall; i++ {
				hash := chunkHash(fmt.Sprintf("w%d-abandoned%d-%s", writer, i, tenant))
				if _, err := blobs.StoreChunk(ctx, tenant, hash, []byte("payload")); err != nil {
					t.Errorf("StoreChunk: %v", err)
					return
				}
				if err := testStore.RegisterChunk(ctx, tenant, hash, 7); err != nil {
					t.Errorf("RegisterChunk: %v", err)
					return
				}
				mu.Lock()
				abandoned = append(abandoned, hash)
				mu.Unlock()
			}
		}(w)
	}

	writeGroup.Wait()
	stopGC()
	<-gcDone

	// Every attempt either committed or was reclaimed before it could; nothing
	// is unaccounted for.
	require.Equal(t, writers*chunksPerWriter, len(committed)+reclaimed)
	require.NotEmpty(t, committed, "no commit won its race — the test proves nothing")
	t.Logf("%d committed, %d reclaimed before commit", len(committed), reclaimed)

	var lost []string
	for _, hash := range committed {
		chunk, err := testStore.GetChunk(ctx, tenant, hash)
		if err != nil {
			lost = append(lost, hash)
			continue
		}
		assert.Positivef(t, chunk.RefCount, "chunk %s lost its reference", hash)
		assert.Nilf(t, chunk.DeletedAt, "chunk %s is still marked for deletion", hash)
		assert.Truef(t, blobs.has(tenant, hash), "chunk %s lost its blob", hash)
	}
	assert.Emptyf(t, lost, "%d committed chunk(s) were collected while live", len(lost))

	// One last pass now that the writers have stopped, so the abandoned chunks
	// are certain to have been offered to the collector.
	_, err := gc.RunGC(ctx, tenant)
	require.NoError(t, err)

	stillThere := 0
	for _, hash := range abandoned {
		if _, err := testStore.GetChunk(ctx, tenant, hash); err == nil {
			stillThere++
		}
	}
	assert.Zerof(t, stillThere, "%d abandoned chunk(s) were never collected", stillThere)
}
