package cas

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// mockMetadataStore is a mock implementation of ChunkMetadataStore for testing.
type mockMetadataStore struct {
	mu     sync.RWMutex
	chunks map[string]*store.Chunk // key: tenantID/hash
}

func newMockMetadataStore() *mockMetadataStore {
	return &mockMetadataStore{
		chunks: make(map[string]*store.Chunk),
	}
}

func (m *mockMetadataStore) key(tenantID, hash string) string {
	return tenantID + "/" + hash
}

func (m *mockMetadataStore) AddChunk(chunk *store.Chunk) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chunks[m.key(chunk.TenantID, chunk.Hash)] = chunk
}

func (m *mockMetadataStore) ListUnreferencedChunks(_ context.Context, tenantID string, limit int) ([]*store.Chunk, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*store.Chunk
	for _, chunk := range m.chunks {
		if chunk.TenantID == tenantID && chunk.RefCount == 0 && chunk.DeletedAt == nil {
			result = append(result, chunk)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockMetadataStore) ListSoftDeletedChunks(_ context.Context, tenantID string, olderThan time.Time, limit int) ([]*store.Chunk, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*store.Chunk
	for _, chunk := range m.chunks {
		if chunk.TenantID == tenantID && chunk.DeletedAt != nil && chunk.DeletedAt.Before(olderThan) {
			result = append(result, chunk)
			if len(result) >= limit {
				break
			}
		}
	}
	return result, nil
}

func (m *mockMetadataStore) MarkChunkDeleted(_ context.Context, tenantID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.key(tenantID, hash)
	chunk, exists := m.chunks[key]
	if !exists {
		return &store.NotFoundError{Resource: "chunk", ID: hash}
	}

	now := time.Now()
	chunk.DeletedAt = &now
	return nil
}

func (m *mockMetadataStore) ClearChunkDeleted(_ context.Context, tenantID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.key(tenantID, hash)
	chunk, exists := m.chunks[key]
	if !exists {
		return &store.NotFoundError{Resource: "chunk", ID: hash}
	}

	chunk.DeletedAt = nil
	return nil
}

func (m *mockMetadataStore) DeleteChunk(_ context.Context, tenantID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := m.key(tenantID, hash)
	if _, exists := m.chunks[key]; !exists {
		return &store.NotFoundError{Resource: "chunk", ID: hash}
	}

	delete(m.chunks, key)
	return nil
}

func (m *mockMetadataStore) GetChunk(_ context.Context, tenantID, hash string) (*store.Chunk, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := m.key(tenantID, hash)
	chunk, exists := m.chunks[key]
	if !exists {
		return nil, &store.NotFoundError{Resource: "chunk", ID: hash}
	}

	return chunk, nil
}

// mockChunkStore is a mock for ChunkStore used in GC.
type mockChunkStore struct {
	mu     sync.RWMutex
	chunks map[string][]byte // key: tenantID/hash
}

func newMockChunkStore() *mockChunkStore {
	return &mockChunkStore{
		chunks: make(map[string][]byte),
	}
}

func (m *mockChunkStore) key(tenantID, hash string) string {
	return tenantID + "/" + hash
}

func (m *mockChunkStore) StoreChunk(ctx context.Context, tenantID, hash string, data []byte) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.chunks[m.key(tenantID, hash)] = data
	return int64(len(data)), nil
}

func (m *mockChunkStore) GetChunk(ctx context.Context, tenantID, hash string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := m.key(tenantID, hash)
	data, exists := m.chunks[key]
	if !exists {
		return nil, ErrChunkNotFound
	}
	return data, nil
}

func (m *mockChunkStore) DeleteChunk(ctx context.Context, tenantID, hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.chunks, m.key(tenantID, hash))
	return nil
}

func (m *mockChunkStore) ChunkExists(ctx context.Context, tenantID, hash string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, exists := m.chunks[m.key(tenantID, hash)]
	return exists, nil
}

// TestGC_Mark tests the mark phase of garbage collection.
func TestGC_Mark(t *testing.T) {
	ctx := context.Background()
	metadataStore := newMockMetadataStore()
	chunkStore := newMockChunkStore()

	// Add some chunks
	tenantID := "tenant1"

	// Referenced chunk (should not be marked)
	metadataStore.AddChunk(&store.Chunk{
		Hash:     "hash1",
		TenantID: tenantID,
		RefCount: 1,
		Size:     1000,
	})

	// Unreferenced chunks (should be marked)
	metadataStore.AddChunk(&store.Chunk{
		Hash:     "hash2",
		TenantID: tenantID,
		RefCount: 0,
		Size:     2000,
	})
	metadataStore.AddChunk(&store.Chunk{
		Hash:     "hash3",
		TenantID: tenantID,
		RefCount: 0,
		Size:     3000,
	})

	gc := NewGC(metadataStore, chunkStore, GCConfig{
		BatchSize: 100,
	})

	marked, err := gc.Mark(ctx, tenantID)
	if err != nil {
		t.Fatalf("Mark failed: %v", err)
	}

	if marked != 2 {
		t.Errorf("expected 2 chunks marked, got %d", marked)
	}

	// Verify the right chunks were marked
	chunk1, _ := metadataStore.GetChunk(ctx, tenantID, "hash1")
	if chunk1.DeletedAt != nil {
		t.Error("referenced chunk should not be marked")
	}

	chunk2, _ := metadataStore.GetChunk(ctx, tenantID, "hash2")
	if chunk2.DeletedAt == nil {
		t.Error("unreferenced chunk2 should be marked")
	}

	chunk3, _ := metadataStore.GetChunk(ctx, tenantID, "hash3")
	if chunk3.DeletedAt == nil {
		t.Error("unreferenced chunk3 should be marked")
	}
}

// TestGC_Sweep tests the sweep phase of garbage collection.
func TestGC_Sweep(t *testing.T) {
	ctx := context.Background()
	metadataStore := newMockMetadataStore()
	chunkStore := newMockChunkStore()

	tenantID := "tenant1"
	gracePeriod := 1 * time.Hour

	// Add chunks to blob storage
	_, _ = chunkStore.StoreChunk(ctx, tenantID, "hash1", []byte("data1"))
	_, _ = chunkStore.StoreChunk(ctx, tenantID, "hash2", []byte("data2"))
	_, _ = chunkStore.StoreChunk(ctx, tenantID, "hash3", []byte("data3"))

	// Chunk marked long ago (should be swept)
	oldTime := time.Now().Add(-2 * gracePeriod)
	metadataStore.AddChunk(&store.Chunk{
		Hash:      "hash1",
		TenantID:  tenantID,
		RefCount:  0,
		Size:      1000,
		DeletedAt: &oldTime,
	})

	// Chunk marked recently (should not be swept)
	recentTime := time.Now().Add(-30 * time.Minute)
	metadataStore.AddChunk(&store.Chunk{
		Hash:      "hash2",
		TenantID:  tenantID,
		RefCount:  0,
		Size:      2000,
		DeletedAt: &recentTime,
	})

	// Chunk not marked (should not be swept)
	metadataStore.AddChunk(&store.Chunk{
		Hash:     "hash3",
		TenantID: tenantID,
		RefCount: 0,
		Size:     3000,
	})

	gc := NewGC(metadataStore, chunkStore, GCConfig{
		GracePeriod: gracePeriod,
		BatchSize:   100,
	})

	deleted, bytesFreed, err := gc.Sweep(ctx, tenantID)
	if err != nil {
		t.Fatalf("Sweep failed: %v", err)
	}

	if deleted != 1 {
		t.Errorf("expected 1 chunk deleted, got %d", deleted)
	}

	if bytesFreed != 1000 {
		t.Errorf("expected 1000 bytes freed, got %d", bytesFreed)
	}

	// Verify hash1 was deleted
	_, err = metadataStore.GetChunk(ctx, tenantID, "hash1")
	if err == nil {
		t.Error("hash1 should have been deleted from metadata store")
	}

	exists, _ := chunkStore.ChunkExists(ctx, tenantID, "hash1")
	if exists {
		t.Error("hash1 should have been deleted from chunk store")
	}

	// Verify hash2 and hash3 still exist
	_, err = metadataStore.GetChunk(ctx, tenantID, "hash2")
	if err != nil {
		t.Error("hash2 should still exist")
	}

	_, err = metadataStore.GetChunk(ctx, tenantID, "hash3")
	if err != nil {
		t.Error("hash3 should still exist")
	}
}

// TestGC_Resurrect tests chunk resurrection.
func TestGC_Resurrect(t *testing.T) {
	ctx := context.Background()
	metadataStore := newMockMetadataStore()
	chunkStore := newMockChunkStore()

	tenantID := "tenant1"

	// Add a marked chunk
	deletedTime := time.Now().Add(-1 * time.Hour)
	metadataStore.AddChunk(&store.Chunk{
		Hash:      "hash1",
		TenantID:  tenantID,
		RefCount:  0,
		Size:      1000,
		DeletedAt: &deletedTime,
	})

	gc := NewGC(metadataStore, chunkStore, GCConfig{})

	// Resurrect the chunk
	if err := gc.Resurrect(ctx, tenantID, "hash1"); err != nil {
		t.Fatalf("Resurrect failed: %v", err)
	}

	// Verify it's no longer marked
	chunk, _ := metadataStore.GetChunk(ctx, tenantID, "hash1")
	if chunk.DeletedAt != nil {
		t.Error("chunk should no longer be marked for deletion")
	}
}

// TestGC_ResurrectIfNeeded tests conditional resurrection.
func TestGC_ResurrectIfNeeded(t *testing.T) {
	ctx := context.Background()
	metadataStore := newMockMetadataStore()
	chunkStore := newMockChunkStore()

	tenantID := "tenant1"

	// Add a marked chunk
	deletedTime := time.Now().Add(-1 * time.Hour)
	metadataStore.AddChunk(&store.Chunk{
		Hash:      "hash1",
		TenantID:  tenantID,
		RefCount:  0,
		Size:      1000,
		DeletedAt: &deletedTime,
	})

	// Add a non-marked chunk
	metadataStore.AddChunk(&store.Chunk{
		Hash:     "hash2",
		TenantID: tenantID,
		RefCount: 1,
		Size:     2000,
	})

	gc := NewGC(metadataStore, chunkStore, GCConfig{})

	// Test resurrection of marked chunk
	resurrected, err := gc.ResurrectIfNeeded(ctx, tenantID, "hash1")
	if err != nil {
		t.Fatalf("ResurrectIfNeeded failed: %v", err)
	}
	if !resurrected {
		t.Error("expected marked chunk to be resurrected")
	}

	// Test non-resurrection of non-marked chunk
	resurrected, err = gc.ResurrectIfNeeded(ctx, tenantID, "hash2")
	if err != nil {
		t.Fatalf("ResurrectIfNeeded failed: %v", err)
	}
	if resurrected {
		t.Error("expected non-marked chunk to not be resurrected")
	}

	// Test non-existent chunk (should not error)
	resurrected, err = gc.ResurrectIfNeeded(ctx, tenantID, "nonexistent")
	if err != nil {
		t.Fatalf("ResurrectIfNeeded should not error for non-existent: %v", err)
	}
	if resurrected {
		t.Error("expected non-existent chunk to not be resurrected")
	}
}

// TestGC_RunGC tests the full GC cycle.
func TestGC_RunGC(t *testing.T) {
	ctx := context.Background()
	metadataStore := newMockMetadataStore()
	chunkStore := newMockChunkStore()

	tenantID := "tenant1"
	gracePeriod := 1 * time.Hour

	// Add chunk data
	_, _ = chunkStore.StoreChunk(ctx, tenantID, "old_unreferenced", []byte("old"))

	// Already marked and past grace period
	oldTime := time.Now().Add(-2 * gracePeriod)
	metadataStore.AddChunk(&store.Chunk{
		Hash:      "old_unreferenced",
		TenantID:  tenantID,
		RefCount:  0,
		Size:      100,
		DeletedAt: &oldTime,
	})

	// Unreferenced but not marked yet
	metadataStore.AddChunk(&store.Chunk{
		Hash:     "new_unreferenced",
		TenantID: tenantID,
		RefCount: 0,
		Size:     200,
	})

	// Referenced chunk
	metadataStore.AddChunk(&store.Chunk{
		Hash:     "referenced",
		TenantID: tenantID,
		RefCount: 5,
		Size:     300,
	})

	gc := NewGC(metadataStore, chunkStore, GCConfig{
		GracePeriod: gracePeriod,
		BatchSize:   100,
	})

	result, err := gc.RunGC(ctx, tenantID)
	if err != nil {
		t.Fatalf("RunGC failed: %v", err)
	}

	// Should mark 1 (new_unreferenced) and delete 1 (old_unreferenced)
	if result.ChunksMarked != 1 {
		t.Errorf("expected 1 chunk marked, got %d", result.ChunksMarked)
	}

	if result.ChunksDeleted != 1 {
		t.Errorf("expected 1 chunk deleted, got %d", result.ChunksDeleted)
	}

	if result.BytesFreed != 100 {
		t.Errorf("expected 100 bytes freed, got %d", result.BytesFreed)
	}

	if result.Duration <= 0 {
		t.Error("expected positive duration")
	}

	// Verify final state
	_, err = metadataStore.GetChunk(ctx, tenantID, "old_unreferenced")
	if err == nil {
		t.Error("old_unreferenced should be deleted")
	}

	chunk, _ := metadataStore.GetChunk(ctx, tenantID, "new_unreferenced")
	if chunk.DeletedAt == nil {
		t.Error("new_unreferenced should be marked")
	}

	chunk, _ = metadataStore.GetChunk(ctx, tenantID, "referenced")
	if chunk.DeletedAt != nil {
		t.Error("referenced chunk should not be marked")
	}
}

// TestGC_DryRun tests that dry run doesn't delete anything.
func TestGC_DryRun(t *testing.T) {
	ctx := context.Background()
	metadataStore := newMockMetadataStore()
	chunkStore := newMockChunkStore()

	tenantID := "tenant1"
	gracePeriod := 1 * time.Hour

	// Add chunk data
	_, _ = chunkStore.StoreChunk(ctx, tenantID, "hash1", []byte("data"))

	// Marked and past grace period
	oldTime := time.Now().Add(-2 * gracePeriod)
	metadataStore.AddChunk(&store.Chunk{
		Hash:      "hash1",
		TenantID:  tenantID,
		RefCount:  0,
		Size:      1000,
		DeletedAt: &oldTime,
	})

	gc := NewGC(metadataStore, chunkStore, GCConfig{
		GracePeriod: gracePeriod,
		DryRun:      true,
	})

	deleted, bytesFreed, err := gc.Sweep(ctx, tenantID)
	if err != nil {
		t.Fatalf("Sweep failed: %v", err)
	}

	if deleted != 1 {
		t.Errorf("expected 1 chunk reported as deleted, got %d", deleted)
	}

	if bytesFreed != 1000 {
		t.Errorf("expected 1000 bytes reported as freed, got %d", bytesFreed)
	}

	// Verify chunk still exists
	_, err = metadataStore.GetChunk(ctx, tenantID, "hash1")
	if err != nil {
		t.Error("chunk should still exist in dry run mode")
	}

	exists, _ := chunkStore.ChunkExists(ctx, tenantID, "hash1")
	if !exists {
		t.Error("chunk should still exist in chunk store in dry run mode")
	}
}

// TestGC_ContextCancellation tests that GC respects context cancellation.
func TestGC_ContextCancellation(t *testing.T) {
	metadataStore := newMockMetadataStore()
	chunkStore := newMockChunkStore()

	tenantID := "tenant1"

	// Add many chunks
	for i := 0; i < 100; i++ {
		metadataStore.AddChunk(&store.Chunk{
			Hash:     "hash" + string(rune('0'+i%10)) + string(rune('0'+i/10)),
			TenantID: tenantID,
			RefCount: 0,
			Size:     1000,
		})
	}

	gc := NewGC(metadataStore, chunkStore, GCConfig{
		BatchSize: 10, // Small batch to allow cancellation between batches
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := gc.Mark(ctx, tenantID)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestGC_TenantIsolation tests that GC only affects the specified tenant.
func TestGC_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	metadataStore := newMockMetadataStore()
	chunkStore := newMockChunkStore()

	// Add unreferenced chunks for two tenants
	metadataStore.AddChunk(&store.Chunk{
		Hash:     "hash1",
		TenantID: "tenant1",
		RefCount: 0,
		Size:     1000,
	})
	metadataStore.AddChunk(&store.Chunk{
		Hash:     "hash2",
		TenantID: "tenant2",
		RefCount: 0,
		Size:     2000,
	})

	gc := NewGC(metadataStore, chunkStore, GCConfig{})

	// Mark only tenant1
	marked, err := gc.Mark(ctx, "tenant1")
	if err != nil {
		t.Fatalf("Mark failed: %v", err)
	}

	if marked != 1 {
		t.Errorf("expected 1 chunk marked, got %d", marked)
	}

	// Verify tenant1 chunk is marked
	chunk1, _ := metadataStore.GetChunk(ctx, "tenant1", "hash1")
	if chunk1.DeletedAt == nil {
		t.Error("tenant1 chunk should be marked")
	}

	// Verify tenant2 chunk is not marked
	chunk2, _ := metadataStore.GetChunk(ctx, "tenant2", "hash2")
	if chunk2.DeletedAt != nil {
		t.Error("tenant2 chunk should not be marked")
	}
}

// TestGC_BatchProcessing tests that GC processes chunks in batches.
func TestGC_BatchProcessing(t *testing.T) {
	ctx := context.Background()
	metadataStore := newMockMetadataStore()
	chunkStore := newMockChunkStore()

	tenantID := "tenant1"

	// Add more chunks than batch size
	for i := 0; i < 25; i++ {
		metadataStore.AddChunk(&store.Chunk{
			Hash:     "hash" + string(rune('a'+i)),
			TenantID: tenantID,
			RefCount: 0,
			Size:     1000,
		})
	}

	gc := NewGC(metadataStore, chunkStore, GCConfig{
		BatchSize: 10,
	})

	marked, err := gc.Mark(ctx, tenantID)
	if err != nil {
		t.Fatalf("Mark failed: %v", err)
	}

	if marked != 25 {
		t.Errorf("expected 25 chunks marked, got %d", marked)
	}
}
