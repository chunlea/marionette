package cas

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlobManifestStore_SaveLoadManifest(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(memStorage, encryptor)

	manifest := &Manifest{
		ID:          "mfst_test123",
		WorkspaceID: "ws_123",
		TenantID:    "tenant_1",
		CreatedAt:   time.Now(),
		TotalSize:   1024,
		ChunkCount:  2,
		Files: []ManifestFile{
			{
				Path:    "file1.txt",
				Mode:    0644,
				ModTime: time.Now(),
				Size:    512,
				Chunks:  []string{"hash1"},
			},
			{
				Path:    "file2.txt",
				Mode:    0755,
				ModTime: time.Now(),
				Size:    512,
				Chunks:  []string{"hash2"},
			},
		},
	}

	// Save
	err := store.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Load
	loaded, err := store.LoadManifest(ctx, "tenant_1", "ws_123", "mfst_test123")
	require.NoError(t, err)

	assert.Equal(t, manifest.ID, loaded.ID)
	assert.Equal(t, manifest.WorkspaceID, loaded.WorkspaceID)
	assert.Equal(t, manifest.TenantID, loaded.TenantID)
	assert.Equal(t, manifest.TotalSize, loaded.TotalSize)
	assert.Equal(t, manifest.ChunkCount, loaded.ChunkCount)
	assert.Len(t, loaded.Files, 2)
	assert.Equal(t, "file1.txt", loaded.Files[0].Path)
	assert.Equal(t, "file2.txt", loaded.Files[1].Path)
}

func TestBlobManifestStore_SaveLoadSingleChunk(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(memStorage, encryptor)

	chunkHash := "abc123"
	manifest := &Manifest{
		ID:          "mfst_single",
		WorkspaceID: "ws_123",
		TenantID:    "tenant_1",
		CreatedAt:   time.Now(),
		TotalSize:   1024,
		SingleChunk: true,
		ChunkHash:   &chunkHash,
		ChunkCount:  1,
	}

	// Save
	err := store.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Load
	loaded, err := store.LoadManifest(ctx, "tenant_1", "ws_123", "mfst_single")
	require.NoError(t, err)

	assert.True(t, loaded.SingleChunk)
	assert.NotNil(t, loaded.ChunkHash)
	assert.Equal(t, chunkHash, *loaded.ChunkHash)
}

func TestBlobManifestStore_LoadNotFound(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(memStorage, encryptor)

	_, err := store.LoadManifest(ctx, "tenant_1", "ws_123", "nonexistent")
	assert.Equal(t, ErrManifestNotFound, err)
}

func TestBlobManifestStore_StreamManifestFiles(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(memStorage, encryptor)

	manifest := &Manifest{
		ID:          "mfst_stream",
		WorkspaceID: "ws_123",
		TenantID:    "tenant_1",
		CreatedAt:   time.Now(),
		TotalSize:   1536,
		ChunkCount:  3,
		Files: []ManifestFile{
			{Path: "a.txt", Mode: 0644, Size: 512, Chunks: []string{"h1"}},
			{Path: "b.txt", Mode: 0644, Size: 512, Chunks: []string{"h2"}},
			{Path: "c.txt", Mode: 0644, Size: 512, Chunks: []string{"h3"}},
		},
	}

	err := store.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Stream
	fileCh, header, err := store.StreamManifestFiles(ctx, "tenant_1", "ws_123", "mfst_stream")
	require.NoError(t, err)
	require.NotNil(t, header)

	assert.Equal(t, "mfst_stream", header.ID)
	assert.Equal(t, int64(1536), header.TotalSize)

	// Collect files from channel
	var files []ManifestFile
	for f := range fileCh {
		files = append(files, f)
	}

	assert.Len(t, files, 3)
	assert.Equal(t, "a.txt", files[0].Path)
	assert.Equal(t, "b.txt", files[1].Path)
	assert.Equal(t, "c.txt", files[2].Path)
}

func TestBlobManifestStore_StreamNotFound(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(memStorage, encryptor)

	_, _, err := store.StreamManifestFiles(ctx, "tenant_1", "ws_123", "nonexistent")
	assert.Equal(t, ErrManifestNotFound, err)
}

func TestBlobManifestStore_DeleteManifest(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(memStorage, encryptor)

	manifest := &Manifest{
		ID:          "mfst_delete",
		WorkspaceID: "ws_123",
		TenantID:    "tenant_1",
		CreatedAt:   time.Now(),
		TotalSize:   100,
	}

	err := store.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Delete
	err = store.DeleteManifest(ctx, "tenant_1", "ws_123", "mfst_delete")
	require.NoError(t, err)

	// Should not exist
	_, err = store.LoadManifest(ctx, "tenant_1", "ws_123", "mfst_delete")
	assert.Equal(t, ErrManifestNotFound, err)
}

func TestManifest_CollectChunkHashes(t *testing.T) {
	// CDC mode
	m := &Manifest{
		Files: []ManifestFile{
			{Chunks: []string{"h1", "h2"}},
			{Chunks: []string{"h2", "h3"}}, // h2 is duplicate
			{Chunks: []string{"h1"}},       // h1 is duplicate
		},
	}
	hashes := m.CollectChunkHashes()
	assert.Len(t, hashes, 3) // h1, h2, h3 (deduplicated)

	// Single chunk mode
	hash := "single_hash"
	m2 := &Manifest{
		SingleChunk: true,
		ChunkHash:   &hash,
	}
	hashes2 := m2.CollectChunkHashes()
	assert.Len(t, hashes2, 1)
	assert.Equal(t, "single_hash", hashes2[0])
}

func TestManifest_ToFromHeader(t *testing.T) {
	chunkHash := "abc123"
	m := &Manifest{
		ID:          "mfst_123",
		WorkspaceID: "ws_456",
		TenantID:    "tenant_1",
		CreatedAt:   time.Now(),
		TotalSize:   1024,
		ChunkCount:  5,
		SingleChunk: true,
		ChunkHash:   &chunkHash,
	}

	header := m.ToHeader()
	assert.Equal(t, m.ID, header.ID)
	assert.Equal(t, m.WorkspaceID, header.WorkspaceID)
	assert.Equal(t, m.TenantID, header.TenantID)
	assert.Equal(t, m.TotalSize, header.TotalSize)
	assert.Equal(t, m.ChunkCount, header.ChunkCount)
	assert.True(t, header.SingleChunk)
	assert.Equal(t, chunkHash, header.ChunkHash)

	// Convert back
	m2 := FromHeader(header)
	assert.Equal(t, m.ID, m2.ID)
	assert.Equal(t, m.WorkspaceID, m2.WorkspaceID)
	assert.NotNil(t, m2.ChunkHash)
	assert.Equal(t, chunkHash, *m2.ChunkHash)
}

func TestFromHeader_NoChunkHash(t *testing.T) {
	header := ManifestHeader{
		ID:          "mfst_123",
		WorkspaceID: "ws_456",
		TenantID:    "tenant_1",
		SingleChunk: false,
		ChunkHash:   "", // empty
	}

	m := FromHeader(header)
	assert.Nil(t, m.ChunkHash)
}
