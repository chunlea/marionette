package cas

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/storage"
)

// TestBlobChunkStore tests the chunk store implementation.
func TestBlobChunkStore_StoreGetChunk(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobChunkStore(memStorage, encryptor)

	data := []byte("test chunk data")
	hash := HashData(data)
	tenantID := "tenant-1"

	// Store chunk
	size, err := store.StoreChunk(ctx, tenantID, hash, data)
	require.NoError(t, err)
	assert.Greater(t, size, int64(0))

	// Get chunk
	retrieved, err := store.GetChunk(ctx, tenantID, hash)
	require.NoError(t, err)
	assert.Equal(t, data, retrieved)
}

func TestBlobChunkStore_ChunkNotFound(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobChunkStore(memStorage, encryptor)

	_, err := store.GetChunk(ctx, "tenant-1", "nonexistent-hash")
	assert.Equal(t, ErrChunkNotFound, err)
}

func TestBlobChunkStore_ChunkExists(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobChunkStore(memStorage, encryptor)

	data := []byte("test chunk data")
	hash := HashData(data)
	tenantID := "tenant-1"

	// Should not exist initially
	exists, err := store.ChunkExists(ctx, tenantID, hash)
	require.NoError(t, err)
	assert.False(t, exists)

	// Store chunk
	_, err = store.StoreChunk(ctx, tenantID, hash, data)
	require.NoError(t, err)

	// Should exist now
	exists, err = store.ChunkExists(ctx, tenantID, hash)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestBlobChunkStore_DeleteChunk(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobChunkStore(memStorage, encryptor)

	data := []byte("test chunk data")
	hash := HashData(data)
	tenantID := "tenant-1"

	// Store chunk
	_, err := store.StoreChunk(ctx, tenantID, hash, data)
	require.NoError(t, err)

	// Delete chunk
	err = store.DeleteChunk(ctx, tenantID, hash)
	require.NoError(t, err)

	// Should not exist
	exists, err := store.ChunkExists(ctx, tenantID, hash)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestBlobChunkStore_CorruptedChunk(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobChunkStore(memStorage, encryptor)

	data := []byte("test chunk data")
	hash := HashData(data)
	tenantID := "tenant-1"

	// Store chunk
	_, err := store.StoreChunk(ctx, tenantID, hash, data)
	require.NoError(t, err)

	// Corrupt the stored data
	key := chunkKey(tenantID, hash)
	reader, _, _ := memStorage.Download(ctx, key)
	stored, _ := io.ReadAll(reader)
	_ = reader.Close()

	// Modify stored data - corrupting compressed data will cause decompression errors
	if len(stored) > 0 {
		stored[0] = ^stored[0]
	}
	_ = memStorage.Upload(ctx, key, bytes.NewReader(stored), storage.UploadOptions{})

	// Get should fail with some error (corruption manifests as decompression error or hash mismatch)
	_, err = store.GetChunk(ctx, tenantID, hash)
	assert.Error(t, err)
}

func TestBlobChunkStore_TenantIsolation(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobChunkStore(memStorage, encryptor)

	data := []byte("test chunk data")
	hash := HashData(data)

	// Store for tenant-1
	_, err := store.StoreChunk(ctx, "tenant-1", hash, data)
	require.NoError(t, err)

	// Should not exist for tenant-2
	exists, err := store.ChunkExists(ctx, "tenant-2", hash)
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestCASSync tests the sync/restore orchestration.
func TestCASSync_SyncSingleChunk(t *testing.T) {
	ctx := context.Background()

	// Setup
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 100 * 1024 * 1024, // 100 MB
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create test directory with small files (< threshold)
	srcDir := t.TempDir()
	createTestFile(t, srcDir, "file1.txt", "hello world")
	createTestFile(t, srcDir, "subdir/file2.txt", "nested content")

	// Sync
	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)
	assert.NotEmpty(t, manifestID)
	assert.True(t, hasPrefix(manifestID, "mfst_"))

	// Verify manifest was created
	manifest, err := manifestStore.LoadManifest(ctx, "tenant-1", "ws-1", manifestID)
	require.NoError(t, err)
	assert.True(t, manifest.SingleChunk)
	assert.NotNil(t, manifest.ChunkHash)
}

func TestCASSync_SyncRestore_SingleChunk(t *testing.T) {
	ctx := context.Background()

	// Setup
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 100 * 1024 * 1024, // 100 MB
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create test directory
	srcDir := t.TempDir()
	createTestFile(t, srcDir, "file1.txt", "hello world")
	createTestFile(t, srcDir, "subdir/file2.txt", "nested content")
	createTestFile(t, srcDir, "subdir/deep/file3.txt", "deep nested")

	// Sync
	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Restore to new directory
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify files
	assertFileContent(t, dstDir, "file1.txt", "hello world")
	assertFileContent(t, dstDir, "subdir/file2.txt", "nested content")
	assertFileContent(t, dstDir, "subdir/deep/file3.txt", "deep nested")
}

func TestCASSync_SyncRestore_CDC(t *testing.T) {
	ctx := context.Background()

	// Setup with small threshold to force CDC mode
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 100, // 100 bytes - force CDC mode
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create test directory with enough data to exceed threshold
	srcDir := t.TempDir()
	createTestFile(t, srcDir, "file1.txt", "hello world this is a test file with some content")
	createTestFile(t, srcDir, "file2.txt", "another file with different content that exceeds threshold")
	createTestFile(t, srcDir, "subdir/file3.txt", "nested content in a subdirectory for testing")

	// Sync
	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Verify manifest uses CDC mode
	manifest, err := manifestStore.LoadManifest(ctx, "tenant-1", "ws-1", manifestID)
	require.NoError(t, err)
	assert.False(t, manifest.SingleChunk)
	assert.Greater(t, len(manifest.Files), 0)

	// Restore to new directory
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify files
	assertFileContent(t, dstDir, "file1.txt", "hello world this is a test file with some content")
	assertFileContent(t, dstDir, "file2.txt", "another file with different content that exceeds threshold")
	assertFileContent(t, dstDir, "subdir/file3.txt", "nested content in a subdirectory for testing")
}

func TestCASSync_SyncRestore_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}

	ctx := context.Background()

	// Setup with small threshold to force CDC mode
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 1024, // 1 KB - force CDC mode
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create a large file that will be chunked
	srcDir := t.TempDir()
	largeData := make([]byte, 3*1024*1024) // 3 MB
	_, err := rand.Read(largeData)
	require.NoError(t, err)

	err = os.WriteFile(filepath.Join(srcDir, "large.bin"), largeData, 0644)
	require.NoError(t, err)

	// Sync
	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Restore to new directory
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify file content
	restored, err := os.ReadFile(filepath.Join(dstDir, "large.bin"))
	require.NoError(t, err)
	assert.Equal(t, largeData, restored)
}

func TestCASSync_ValidateManifest(t *testing.T) {
	ctx := context.Background()

	// Setup
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 100 * 1024 * 1024,
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create and sync a workspace
	srcDir := t.TempDir()
	createTestFile(t, srcDir, "file.txt", "test content")

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Load manifest
	manifest, err := manifestStore.LoadManifest(ctx, "tenant-1", "ws-1", manifestID)
	require.NoError(t, err)

	// Validate should pass
	err = sync.ValidateManifest(ctx, manifest)
	require.NoError(t, err)
}

func TestCASSync_ValidateManifest_MissingChunks(t *testing.T) {
	ctx := context.Background()

	// Setup
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 100 * 1024 * 1024,
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create and sync a workspace
	srcDir := t.TempDir()
	createTestFile(t, srcDir, "file.txt", "test content")

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Load manifest
	manifest, err := manifestStore.LoadManifest(ctx, "tenant-1", "ws-1", manifestID)
	require.NoError(t, err)

	// Delete the chunk
	hashes := manifest.CollectChunkHashes()
	require.Greater(t, len(hashes), 0)
	err = chunkStore.DeleteChunk(ctx, "tenant-1", hashes[0])
	require.NoError(t, err)

	// Validate should fail
	err = sync.ValidateManifest(ctx, manifest)
	require.Error(t, err)

	var missingErr *ChunksMissingError
	assert.ErrorAs(t, err, &missingErr)
	assert.Contains(t, missingErr.Missing, hashes[0])
}

func TestCASSync_EmptyDirectory(t *testing.T) {
	ctx := context.Background()

	// Setup
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 100 * 1024 * 1024,
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create empty directory
	srcDir := t.TempDir()

	// Sync empty directory
	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Restore to new directory
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.NoError(t, err)

	// Directory should exist and be empty
	entries, err := os.ReadDir(dstDir)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestCASSync_FilePermissions(t *testing.T) {
	ctx := context.Background()

	// Setup with small threshold to force CDC mode
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 100, // Force CDC mode
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create test directory with specific permissions
	srcDir := t.TempDir()
	filePath := filepath.Join(srcDir, "executable.sh")
	err := os.WriteFile(filePath, []byte("#!/bin/bash\necho hello\n"), 0755)
	require.NoError(t, err)

	// Sync
	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Restore to new directory
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify permissions (on Unix systems)
	info, err := os.Stat(filepath.Join(dstDir, "executable.sh"))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
}

func TestCASSync_ChunkDedup(t *testing.T) {
	ctx := context.Background()

	// Setup
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 100, // Force CDC mode
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create test directory with duplicate content
	srcDir := t.TempDir()
	content := "this is repeated content that should be deduplicated"
	createTestFile(t, srcDir, "file1.txt", content)
	createTestFile(t, srcDir, "file2.txt", content) // Same content

	// Sync
	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Load manifest and check chunk count
	manifest, err := manifestStore.LoadManifest(ctx, "tenant-1", "ws-1", manifestID)
	require.NoError(t, err)

	// Both files should reference the same chunk
	require.Len(t, manifest.Files, 2)
	assert.Equal(t, manifest.Files[0].Chunks, manifest.Files[1].Chunks)
}

// Helper functions

func createTestFile(t *testing.T, baseDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, relPath)
	err := os.MkdirAll(filepath.Dir(fullPath), 0755)
	require.NoError(t, err)
	err = os.WriteFile(fullPath, []byte(content), 0644)
	require.NoError(t, err)
}

func assertFileContent(t *testing.T, baseDir, relPath, expected string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, relPath)
	content, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, expected, string(content))
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

func TestSync_RestoreRequiresManifestID(t *testing.T) {
	ctx := context.Background()

	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := DefaultConfig
	sync := NewSync(config, chunkStore, manifestStore)

	// Restore without manifest ID should error
	err := sync.Restore(ctx, "ws-1", "tenant-1", "/tmp/test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires manifest ID")
}

func TestBlobChunkStore_StoreChunkDedup(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobChunkStore(memStorage, encryptor)

	data := []byte("test chunk data for dedup")
	hash := HashData(data)
	tenantID := "tenant-1"

	// Store first time
	size1, err := store.StoreChunk(ctx, tenantID, hash, data)
	require.NoError(t, err)

	// Store again (should work, overwrites)
	size2, err := store.StoreChunk(ctx, tenantID, hash, data)
	require.NoError(t, err)

	// Sizes should be the same
	assert.Equal(t, size1, size2)
}

func TestSync_StreamManifestCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	// Create a manifest with many files
	manifest := &Manifest{
		ID:          "mfst_cancel",
		WorkspaceID: "ws_123",
		TenantID:    "tenant_1",
		CreatedAt:   time.Now(),
		TotalSize:   1000,
		ChunkCount:  10,
	}
	for i := 0; i < 100; i++ {
		manifest.Files = append(manifest.Files, ManifestFile{
			Path:   fmt.Sprintf("file%d.txt", i),
			Mode:   0644,
			Size:   10,
			Chunks: []string{fmt.Sprintf("hash%d", i)},
		})
	}

	err := manifestStore.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Stream with cancellation
	fileCh, header, err := manifestStore.StreamManifestFiles(ctx, "tenant_1", "ws_123", "mfst_cancel")
	require.NoError(t, err)
	require.NotNil(t, header)

	// Read a few files then cancel
	count := 0
	for range fileCh {
		count++
		if count == 5 {
			cancel()
			break
		}
	}

	// Drain the channel
	for range fileCh {
	}
}

func TestSync_BinaryFiles(t *testing.T) {
	ctx := context.Background()

	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 100, // Force CDC mode
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create binary files
	srcDir := t.TempDir()

	// Binary file with null bytes
	binaryData := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0x00, 0x00}
	err := os.WriteFile(filepath.Join(srcDir, "binary.bin"), binaryData, 0644)
	require.NoError(t, err)

	// Sync
	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Restore
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify binary content
	restored, err := os.ReadFile(filepath.Join(dstDir, "binary.bin"))
	require.NoError(t, err)
	assert.Equal(t, binaryData, restored)
}

func TestSync_NestedEmptyDirs(t *testing.T) {
	ctx := context.Background()

	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		SingleChunkThreshold: 100 * 1024 * 1024, // Force single chunk mode
		MaxConcurrency:       4,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create directory structure with some empty directories
	srcDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(srcDir, "empty1"), 0755)
	_ = os.MkdirAll(filepath.Join(srcDir, "parent/empty2"), 0755)
	createTestFile(t, srcDir, "parent/file.txt", "content")

	// Sync
	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Restore
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify file exists
	assertFileContent(t, dstDir, "parent/file.txt", "content")
}
