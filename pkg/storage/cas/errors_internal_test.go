package cas

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/storage"
)

// errorProvider is a storage provider that returns errors for testing.
type errorProvider struct {
	uploadErr   error
	downloadErr error
	deleteErr   error
	existsErr   error
	data        map[string][]byte
}

func newErrorProvider() *errorProvider {
	return &errorProvider{data: make(map[string][]byte)}
}

func (p *errorProvider) Name() string { return "error" }

func (p *errorProvider) Upload(_ context.Context, key string, r io.Reader, _ storage.UploadOptions) error {
	if p.uploadErr != nil {
		return p.uploadErr
	}
	data, _ := io.ReadAll(r)
	p.data[key] = data
	return nil
}

func (p *errorProvider) Download(_ context.Context, key string) (io.ReadCloser, int64, error) {
	if p.downloadErr != nil {
		return nil, 0, p.downloadErr
	}
	data, ok := p.data[key]
	if !ok {
		return nil, 0, storage.ErrNotFound
	}
	return io.NopCloser(bytes.NewReader(data)), int64(len(data)), nil
}

func (p *errorProvider) Delete(_ context.Context, key string) error {
	if p.deleteErr != nil {
		return p.deleteErr
	}
	delete(p.data, key)
	return nil
}

func (p *errorProvider) Exists(_ context.Context, key string) (bool, error) {
	if p.existsErr != nil {
		return false, p.existsErr
	}
	_, ok := p.data[key]
	return ok, nil
}

// errorEncryptor returns errors for testing.
type errorEncryptor struct {
	encryptErr error
	decryptErr error
}

func (e *errorEncryptor) Encrypt(_ context.Context, _ string, data []byte) ([]byte, error) {
	if e.encryptErr != nil {
		return nil, e.encryptErr
	}
	return data, nil
}

func (e *errorEncryptor) Decrypt(_ context.Context, _ string, data []byte) ([]byte, error) {
	if e.decryptErr != nil {
		return nil, e.decryptErr
	}
	return data, nil
}

func TestBlobChunkStore_StoreChunk_EncryptError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	encryptor := &errorEncryptor{encryptErr: errors.New("encrypt failed")}

	store := NewBlobChunkStore(provider, encryptor)

	_, err := store.StoreChunk(ctx, "tenant-1", "hash123", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "encrypt")
}

func TestBlobChunkStore_StoreChunk_UploadError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	provider.uploadErr = errors.New("upload failed")
	encryptor := &errorEncryptor{}

	store := NewBlobChunkStore(provider, encryptor)

	_, err := store.StoreChunk(ctx, "tenant-1", "hash123", []byte("data"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upload")
}

func TestBlobChunkStore_GetChunk_DownloadError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	provider.downloadErr = errors.New("download failed")
	encryptor := &errorEncryptor{}

	store := NewBlobChunkStore(provider, encryptor)

	_, err := store.GetChunk(ctx, "tenant-1", "hash123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "download")
}

func TestBlobChunkStore_GetChunk_DecryptError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	encryptor := &errorEncryptor{}

	store := NewBlobChunkStore(provider, encryptor)

	// Store a chunk first
	_, err := store.StoreChunk(ctx, "tenant-1", "hash123", []byte("data"))
	require.NoError(t, err)

	// Now make decrypt fail
	encryptor.decryptErr = errors.New("decrypt failed")

	_, err = store.GetChunk(ctx, "tenant-1", "hash123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt")
}

func TestBlobChunkStore_ChunkExists_Error(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	provider.existsErr = errors.New("exists check failed")
	encryptor := &errorEncryptor{}

	store := NewBlobChunkStore(provider, encryptor)

	_, err := store.ChunkExists(ctx, "tenant-1", "hash123")
	assert.Error(t, err)
}

func TestBlobManifestStore_SaveManifest_EncryptError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	encryptor := &errorEncryptor{encryptErr: errors.New("encrypt failed")}

	store := NewBlobManifestStore(provider, encryptor)

	manifest := &Manifest{
		ID:          "mfst_test",
		WorkspaceID: "ws_1",
		TenantID:    "tenant_1",
	}

	err := store.SaveManifest(ctx, manifest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "encrypt")
}

func TestBlobManifestStore_SaveManifest_UploadError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	provider.uploadErr = errors.New("upload failed")
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(provider, encryptor)

	manifest := &Manifest{
		ID:          "mfst_test",
		WorkspaceID: "ws_1",
		TenantID:    "tenant_1",
	}

	err := store.SaveManifest(ctx, manifest)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "upload")
}

func TestBlobManifestStore_LoadManifest_DecryptError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(provider, encryptor)

	// Save a manifest first
	manifest := &Manifest{
		ID:          "mfst_test",
		WorkspaceID: "ws_1",
		TenantID:    "tenant_1",
	}
	err := store.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Now use an encryptor that fails on decrypt
	store2 := NewBlobManifestStore(provider, &errorEncryptor{decryptErr: errors.New("decrypt failed")})

	_, err = store2.LoadManifest(ctx, "tenant_1", "ws_1", "mfst_test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt")
}

func TestBlobManifestStore_StreamManifestFiles_DecryptError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(provider, encryptor)

	// Save a manifest first
	manifest := &Manifest{
		ID:          "mfst_test",
		WorkspaceID: "ws_1",
		TenantID:    "tenant_1",
		Files: []ManifestFile{
			{Path: "a.txt", Chunks: []string{"h1"}},
		},
	}
	err := store.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Now use an encryptor that fails
	store2 := NewBlobManifestStore(provider, &errorEncryptor{decryptErr: errors.New("decrypt failed")})

	_, _, err = store2.StreamManifestFiles(ctx, "tenant_1", "ws_1", "mfst_test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decrypt")
}

func TestSync_SyncCDC_ChunkExistsError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(provider, encryptor)
	manifestStore := NewBlobManifestStore(NewMemoryProvider(), encryptor)

	config := Config{
		CDCThreshold:   10, // Force CDC
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create test dir
	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "file.txt", "some content here")

	// Make exists check fail during upload
	provider.existsErr = errors.New("exists check failed")

	_, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	assert.Error(t, err)
}

func TestSync_ValidateManifest_ExistsError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(provider, encryptor)

	config := DefaultConfig
	sync := NewSync(config, chunkStore, nil)

	manifest := &Manifest{
		TenantID: "tenant-1",
		Files: []ManifestFile{
			{Chunks: []string{"hash1"}},
		},
	}

	provider.existsErr = errors.New("exists check failed")

	err := sync.ValidateManifest(ctx, manifest)
	assert.Error(t, err)
}

func TestSync_RestoreCDC_GetChunkError(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		CDCThreshold:   10, // Force CDC
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create and sync a workspace
	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "file.txt", "test content")

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Delete the chunks
	manifest, _ := manifestStore.LoadManifest(ctx, "tenant-1", "ws-1", manifestID)
	for _, hash := range manifest.CollectChunkHashes() {
		_ = chunkStore.DeleteChunk(ctx, "tenant-1", hash)
	}

	// Now restore should fail
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get chunk")
}

func TestSync_RestoreSingleChunk_GetChunkError(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		CDCThreshold:   100 * 1024 * 1024, // Force single chunk
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create and sync
	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "file.txt", "test")

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Delete the chunk
	manifest, _ := manifestStore.LoadManifest(ctx, "tenant-1", "ws-1", manifestID)
	_ = chunkStore.DeleteChunk(ctx, "tenant-1", *manifest.ChunkHash)

	// Restore should fail
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get single chunk")
}

// createTestFileInternal is a helper to create test files (internal to avoid redeclaration)
func createTestFileInternal(t *testing.T, baseDir, relPath, content string) {
	t.Helper()
	fullPath := filepath.Join(baseDir, relPath)
	err := os.MkdirAll(filepath.Dir(fullPath), 0755)
	require.NoError(t, err)
	err = os.WriteFile(fullPath, []byte(content), 0600) //nolint:gosec // test file
	require.NoError(t, err)
}

func TestSync_Restore_RequiresManifestID(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	sync := NewSync(DefaultConfig, chunkStore, manifestStore)

	err := sync.Restore(ctx, "ws-1", "tenant-1", t.TempDir())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "requires manifest ID")
}

func TestSync_RestoreFromManifest_SingleChunk(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		CDCThreshold:   100 * 1024 * 1024, // Force single chunk
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create and sync
	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "hello.txt", "hello world")
	createTestFileInternal(t, srcDir, "subdir/nested.txt", "nested content")

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Restore using internal method with workspaceID
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify files
	content, err := os.ReadFile(filepath.Join(dstDir, "hello.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(content))

	content, err = os.ReadFile(filepath.Join(dstDir, "subdir/nested.txt"))
	require.NoError(t, err)
	assert.Equal(t, "nested content", string(content))
}

func TestSync_RestoreFromManifest_CDC(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		CDCThreshold:   10, // Force CDC
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create and sync
	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "file.txt", "some content here that is longer")

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Restore using internal method with workspaceID
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify file
	content, err := os.ReadFile(filepath.Join(dstDir, "file.txt"))
	require.NoError(t, err)
	assert.Equal(t, "some content here that is longer", string(content))
}

func TestSync_RestoreFromManifest_NotFound(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	sync := NewSync(DefaultConfig, chunkStore, manifestStore)

	dstDir := t.TempDir()
	err := sync.RestoreFromManifest(ctx, "nonexistent", "tenant-1", dstDir)
	assert.Error(t, err)
}

func TestBlobChunkStore_DeleteChunk_Error(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	provider.deleteErr = errors.New("delete failed")
	encryptor := NewNoOpEncryptor()

	store := NewBlobChunkStore(provider, encryptor)

	err := store.DeleteChunk(ctx, "tenant-1", "hash123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete")
}

func TestBlobManifestStore_DeleteManifest_Error(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(provider, encryptor)

	// Save a manifest first
	manifest := &Manifest{
		ID:          "mfst_test",
		WorkspaceID: "ws_1",
		TenantID:    "tenant_1",
	}
	err := store.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Now make delete fail
	provider.deleteErr = errors.New("delete failed")

	err = store.DeleteManifest(ctx, "tenant_1", "ws_1", "mfst_test")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "delete")
}

func TestBlobManifestStore_LoadManifest_DownloadError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	provider.downloadErr = errors.New("download failed")
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(provider, encryptor)

	_, err := store.LoadManifest(ctx, "tenant-1", "ws-1", "mfst_test")
	assert.Error(t, err)
}

func TestSync_Sync_EmptyDir(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	sync := NewSync(DefaultConfig, chunkStore, manifestStore)

	// Sync empty directory
	srcDir := t.TempDir()

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)
	assert.NotEmpty(t, manifestID)
}

func TestSync_RestoreFromManifest_PathTraversal(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		CDCThreshold:   10, // Force CDC
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create a manifest with path traversal in file path
	manifest := &Manifest{
		ID:          "mfst_evil",
		WorkspaceID: "ws-1",
		TenantID:    "tenant-1",
		Files: []ManifestFile{
			{
				Path:   "../../../etc/passwd",
				Size:   100,
				Chunks: []string{"hash1"},
			},
		},
	}
	err := manifestStore.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Store a fake chunk
	_, err = chunkStore.StoreChunk(ctx, "tenant-1", "hash1", []byte("fake content"))
	require.NoError(t, err)

	// Restore should fail with path traversal error
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", "mfst_evil", "tenant-1", dstDir)
	assert.Error(t, err)
	var ptErr *PathTraversalError
	assert.True(t, errors.As(err, &ptErr), "expected PathTraversalError, got: %v", err)
}

func TestSync_SyncSingleChunk_SaveManifestError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(provider, encryptor)

	// Use error provider for manifest store too
	manifestProvider := newErrorProvider()
	manifestStore := NewBlobManifestStore(manifestProvider, encryptor)

	config := Config{
		CDCThreshold:   100 * 1024 * 1024, // Force single chunk
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "file.txt", "content")

	// Make manifest save fail
	manifestProvider.uploadErr = errors.New("manifest upload failed")

	_, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "save manifest")
}

func TestSync_SyncCDC_SaveManifestError(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)

	// Use error provider for manifest store
	manifestProvider := newErrorProvider()
	manifestStore := NewBlobManifestStore(manifestProvider, encryptor)

	config := Config{
		CDCThreshold:   10, // Force CDC
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "file.txt", "some content here")

	// Make manifest save fail
	manifestProvider.uploadErr = errors.New("manifest upload failed")

	_, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	assert.Error(t, err)
	// CDC mode streams the manifest straight to the object store rather than
	// handing a finished one to SaveManifest, so the upload is where it fails.
	assert.Contains(t, err.Error(), "upload manifest")
}

func TestSync_SyncSingleChunk_StoreChunkError(t *testing.T) {
	ctx := context.Background()
	provider := newErrorProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(provider, encryptor)
	manifestStore := NewBlobManifestStore(NewMemoryProvider(), encryptor)

	config := Config{
		CDCThreshold:   100 * 1024 * 1024, // Force single chunk
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "file.txt", "content")

	// Make upload fail
	provider.uploadErr = errors.New("upload failed")

	_, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "store single chunk")
}

func TestSync_Sync_InvalidSrcDir(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	sync := NewSync(DefaultConfig, chunkStore, manifestStore)

	// Sync non-existent directory
	_, err := sync.Sync(ctx, "ws-1", "tenant-1", "/nonexistent/dir")
	assert.Error(t, err)
}

func TestSync_RestoreSingleChunk_Directory(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		CDCThreshold:   100 * 1024 * 1024, // Force single chunk
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create directory structure
	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "dir1/file1.txt", "file1 content")
	createTestFileInternal(t, srcDir, "dir1/file2.txt", "file2 content")
	createTestFileInternal(t, srcDir, "dir2/nested/file3.txt", "file3 content")

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Restore
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify structure
	content1, err := os.ReadFile(filepath.Join(dstDir, "dir1/file1.txt"))
	require.NoError(t, err)
	assert.Equal(t, "file1 content", string(content1))

	content3, err := os.ReadFile(filepath.Join(dstDir, "dir2/nested/file3.txt"))
	require.NoError(t, err)
	assert.Equal(t, "file3 content", string(content3))
}

func TestSync_Restore_CreateDstDirError(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	sync := NewSync(DefaultConfig, chunkStore, manifestStore)

	// Create a manifest first
	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "file.txt", "content")
	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	// Try to restore to an invalid path (file exists where dir should be)
	tempFile := filepath.Join(t.TempDir(), "file")
	err = os.WriteFile(tempFile, []byte("blocking file"), 0600) //nolint:gosec // test file
	require.NoError(t, err)

	invalidDst := filepath.Join(tempFile, "subdir")
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", invalidDst)
	assert.Error(t, err)
}

func TestSync_SyncCDC_UploadChunkError(t *testing.T) {
	ctx := context.Background()

	// Create separate providers so we can control errors
	chunkProvider := newErrorProvider()
	manifestProvider := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(chunkProvider, encryptor)
	manifestStore := NewBlobManifestStore(manifestProvider, encryptor)

	config := Config{
		CDCThreshold:   10, // Force CDC
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	srcDir := t.TempDir()
	createTestFileInternal(t, srcDir, "file.txt", "some content here that is longer")

	// Make chunk upload fail
	chunkProvider.uploadErr = errors.New("chunk upload failed")

	_, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	assert.Error(t, err)
}

func TestBlobChunkStore_GetChunk_NotFound(t *testing.T) {
	ctx := context.Background()
	provider := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobChunkStore(provider, encryptor)

	_, err := store.GetChunk(ctx, "tenant-1", "nonexistent-hash")
	assert.Error(t, err)
}

func TestBlobChunkStore_GetChunk_Corrupted(t *testing.T) {
	ctx := context.Background()
	provider := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobChunkStore(provider, encryptor)

	// Store a chunk
	originalData := []byte("original content")
	hash := HashData(originalData)
	_, err := store.StoreChunk(ctx, "tenant-1", hash, originalData)
	require.NoError(t, err)

	// Manually corrupt the stored chunk by storing different data with same key
	key := "chunks/tenant-1/" + hash[:2] + "/" + hash + ".blob.enc"
	corruptedData := []byte("corrupted content")
	compressed, err := encryptor.Encrypt(ctx, "tenant-1", corruptedData)
	require.NoError(t, err)
	err = provider.Upload(ctx, key, bytes.NewReader(compressed), storage.UploadOptions{})
	require.NoError(t, err)

	// Now GetChunk should fail with corruption error
	_, err = store.GetChunk(ctx, "tenant-1", hash)
	assert.ErrorIs(t, err, ErrChunkCorrupted)
}

func TestSync_RestoreCDC_ChunkNotFound(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		CDCThreshold:   10, // Force CDC
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Create a manifest referencing non-existent chunks
	manifest := &Manifest{
		ID:          "mfst_test",
		WorkspaceID: "ws-1",
		TenantID:    "tenant-1",
		Files: []ManifestFile{
			{
				Path:   "file.txt",
				Size:   100,
				Chunks: []string{"nonexistent-hash"},
				Mode:   0644,
			},
		},
	}
	err := manifestStore.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Restore should fail because chunk doesn't exist
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", "mfst_test", "tenant-1", dstDir)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get chunk")
}

func TestSync_RestoreCDC_MultipleChunks(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		CDCThreshold:   10, // Force CDC
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Store multiple chunks
	chunk1Data := []byte("chunk 1 data")
	chunk2Data := []byte("chunk 2 data")
	hash1 := HashData(chunk1Data)
	hash2 := HashData(chunk2Data)

	_, err := chunkStore.StoreChunk(ctx, "tenant-1", hash1, chunk1Data)
	require.NoError(t, err)
	_, err = chunkStore.StoreChunk(ctx, "tenant-1", hash2, chunk2Data)
	require.NoError(t, err)

	// Create manifest with multiple chunks per file
	manifest := &Manifest{
		ID:          "mfst_test",
		WorkspaceID: "ws-1",
		TenantID:    "tenant-1",
		Files: []ManifestFile{
			{
				Path:   "file.txt",
				Size:   int64(len(chunk1Data) + len(chunk2Data)),
				Chunks: []string{hash1, hash2},
				Mode:   0644,
			},
		},
	}
	err = manifestStore.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Restore should succeed
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", "mfst_test", "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify file content
	content, err := os.ReadFile(filepath.Join(dstDir, "file.txt"))
	require.NoError(t, err)
	expected := make([]byte, 0, len(chunk1Data)+len(chunk2Data))
	expected = append(expected, chunk1Data...)
	expected = append(expected, chunk2Data...)
	assert.Equal(t, expected, content)
}

func TestSync_RestoreCDC_ChunkReuse(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	manifestStore := NewBlobManifestStore(memStorage, encryptor)

	config := Config{
		CDCThreshold:   10, // Force CDC
		MaxConcurrency: 1,
	}.WithDefaults()

	sync := NewSync(config, chunkStore, manifestStore)

	// Store a single chunk
	chunkData := []byte("shared chunk data")
	hash := HashData(chunkData)
	_, err := chunkStore.StoreChunk(ctx, "tenant-1", hash, chunkData)
	require.NoError(t, err)

	// Create manifest with same chunk used in multiple files
	manifest := &Manifest{
		ID:          "mfst_test",
		WorkspaceID: "ws-1",
		TenantID:    "tenant-1",
		Files: []ManifestFile{
			{
				Path:   "file1.txt",
				Size:   int64(len(chunkData)),
				Chunks: []string{hash},
				Mode:   0644,
			},
			{
				Path:   "file2.txt",
				Size:   int64(len(chunkData)),
				Chunks: []string{hash},
				Mode:   0644,
			},
		},
	}
	err = manifestStore.SaveManifest(ctx, manifest)
	require.NoError(t, err)

	// Restore should succeed (chunk cached)
	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", "mfst_test", "tenant-1", dstDir)
	require.NoError(t, err)

	// Verify both files have same content
	content1, _ := os.ReadFile(filepath.Join(dstDir, "file1.txt"))
	content2, _ := os.ReadFile(filepath.Join(dstDir, "file2.txt"))
	assert.Equal(t, chunkData, content1)
	assert.Equal(t, chunkData, content2)
}

func TestBlobManifestStore_LoadManifest_InvalidCompressedData(t *testing.T) {
	ctx := context.Background()
	provider := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(provider, encryptor)

	// Store invalid compressed data (not valid zstd)
	key := "manifests/tenant-1/ws-1/mfst_test.jsonl.zst.enc"
	// This is not valid zstd data
	invalidData, _ := encryptor.Encrypt(ctx, "tenant-1", []byte("not valid jsonl"))
	err := provider.Upload(ctx, key, bytes.NewReader(invalidData), storage.UploadOptions{})
	require.NoError(t, err)

	// LoadManifest should fail
	_, err = store.LoadManifest(ctx, "tenant-1", "ws-1", "mfst_test")
	assert.Error(t, err)
}

func TestBlobManifestStore_StreamManifestFiles_InvalidCompressedData(t *testing.T) {
	ctx := context.Background()
	provider := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()

	store := NewBlobManifestStore(provider, encryptor)

	// Store invalid data
	key := "manifests/tenant-1/ws-1/mfst_test.jsonl.zst.enc"
	invalidData, _ := encryptor.Encrypt(ctx, "tenant-1", []byte("invalid"))
	err := provider.Upload(ctx, key, bytes.NewReader(invalidData), storage.UploadOptions{})
	require.NoError(t, err)

	// StreamManifestFiles should fail
	_, _, err = store.StreamManifestFiles(ctx, "tenant-1", "ws-1", "mfst_test")
	assert.Error(t, err)
}

func TestManifest_ToHeader(t *testing.T) {
	manifest := &Manifest{
		ID:          "mfst_123",
		WorkspaceID: "ws_456",
		TenantID:    "tenant_789",
		TotalSize:   12345,
		SingleChunk: true,
		ChunkHash:   strPtr("abc123"),
		ChunkCount:  1,
	}

	header := manifest.ToHeader()
	assert.Equal(t, manifest.ID, header.ID)
	assert.Equal(t, manifest.WorkspaceID, header.WorkspaceID)
	assert.Equal(t, manifest.TenantID, header.TenantID)
	assert.Equal(t, manifest.TotalSize, header.TotalSize)
	assert.Equal(t, manifest.SingleChunk, header.SingleChunk)
	assert.Equal(t, "abc123", header.ChunkHash)
	assert.Equal(t, manifest.ChunkCount, header.ChunkCount)
}

func strPtr(s string) *string {
	return &s
}

func TestManifest_CollectChunkHashes_SingleChunk(t *testing.T) {
	manifest := &Manifest{
		SingleChunk: true,
		ChunkHash:   strPtr("single-hash"),
	}

	hashes := manifest.CollectChunkHashes()
	assert.Equal(t, []string{"single-hash"}, hashes)
}

func TestManifest_CollectChunkHashes_CDCMode(t *testing.T) {
	manifest := &Manifest{
		SingleChunk: false,
		Files: []ManifestFile{
			{Chunks: []string{"hash1", "hash2"}},
			{Chunks: []string{"hash2", "hash3"}}, // hash2 is duplicate
		},
	}

	hashes := manifest.CollectChunkHashes()
	assert.Len(t, hashes, 3) // Unique hashes
	assert.Contains(t, hashes, "hash1")
	assert.Contains(t, hashes, "hash2")
	assert.Contains(t, hashes, "hash3")
}

func TestChunker_ChunkData_SmallData(t *testing.T) {
	chunker := NewChunker(DefaultChunkerConfig)

	// Data smaller than MinSize should produce a single chunk
	smallData := make([]byte, 100)
	for i := range smallData {
		smallData[i] = byte(i)
	}

	chunks, err := chunker.ChunkData(smallData)
	require.NoError(t, err)
	assert.Len(t, chunks, 1)
	assert.Equal(t, smallData, chunks[0].Data)
}

func TestChunker_ChunkReader_Error(t *testing.T) {
	chunkerInst := NewChunker(DefaultChunkerConfig)

	// Create a reader that returns an error
	errReader := &errorReaderImpl{err: errors.New("read error")}

	chunks, errs := chunkerInst.ChunkReader(errReader)

	// Drain chunks channel
	for range chunks { //nolint:revive // intentionally empty - draining the channel
	}

	// Check for error
	err := <-errs
	assert.Error(t, err)
}

// errorReaderImpl is a reader that always returns an error.
type errorReaderImpl struct {
	err error
}

func (r *errorReaderImpl) Read(_ []byte) (int, error) {
	return 0, r.err
}
