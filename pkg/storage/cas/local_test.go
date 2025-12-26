package cas

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/storage"
)

func TestLocalProvider_Name(t *testing.T) {
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)
	assert.Equal(t, "local", p.Name())
}

func TestLocalProvider_UploadDownload(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)

	key := "test/file.txt"
	data := []byte("hello world")

	// Upload
	err = p.Upload(ctx, key, bytes.NewReader(data), storage.UploadOptions{})
	require.NoError(t, err)

	// Verify file exists on disk
	fullPath := filepath.Join(dir, key)
	_, err = os.Stat(fullPath)
	require.NoError(t, err)

	// Download
	reader, size, err := p.Download(ctx, key)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	assert.Equal(t, int64(len(data)), size)

	downloaded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, data, downloaded)
}

func TestLocalProvider_UploadCreatesDirectories(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)

	key := "deep/nested/path/file.txt"
	data := []byte("nested content")

	err = p.Upload(ctx, key, bytes.NewReader(data), storage.UploadOptions{})
	require.NoError(t, err)

	// Verify file exists
	fullPath := filepath.Join(dir, key)
	content, err := os.ReadFile(fullPath)
	require.NoError(t, err)
	assert.Equal(t, data, content)
}

func TestLocalProvider_DownloadNotFound(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)

	_, _, err = p.Download(ctx, "nonexistent")
	assert.Equal(t, storage.ErrNotFound, err)
}

func TestLocalProvider_Delete(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)

	key := "test/file.txt"
	data := []byte("hello world")

	// Upload
	err = p.Upload(ctx, key, bytes.NewReader(data), storage.UploadOptions{})
	require.NoError(t, err)

	// Delete
	err = p.Delete(ctx, key)
	require.NoError(t, err)

	// Should not exist
	exists, err := p.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestLocalProvider_DeleteNonexistent(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)

	// Delete nonexistent should not error (idempotent)
	err = p.Delete(ctx, "nonexistent")
	assert.NoError(t, err)
}

func TestLocalProvider_Exists(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)

	key := "test/file.txt"

	// Should not exist initially
	exists, err := p.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists)

	// Upload
	err = p.Upload(ctx, key, bytes.NewReader([]byte("data")), storage.UploadOptions{})
	require.NoError(t, err)

	// Should exist now
	exists, err = p.Exists(ctx, key)
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestLocalProvider_PathTraversal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)

	// Attempt path traversal
	key := "../../../etc/passwd"
	err = p.Upload(ctx, key, bytes.NewReader([]byte("data")), storage.UploadOptions{})
	assert.Equal(t, storage.ErrInvalidKey, err)

	_, _, err = p.Download(ctx, key)
	assert.Equal(t, storage.ErrInvalidKey, err)
}

func TestLocalProvider_AtomicWrite(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)

	key := "test/file.txt"

	// Write initial content
	err = p.Upload(ctx, key, bytes.NewReader([]byte("initial")), storage.UploadOptions{})
	require.NoError(t, err)

	// Overwrite with new content
	err = p.Upload(ctx, key, bytes.NewReader([]byte("updated")), storage.UploadOptions{})
	require.NoError(t, err)

	// Read back
	reader, _, err := p.Download(ctx, key)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	content, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("updated"), content)
}

func TestLocalProvider_CreatesBasePath(t *testing.T) {
	dir := t.TempDir()
	basePath := filepath.Join(dir, "new", "nested", "path")

	p, err := NewLocalProvider(basePath)
	require.NoError(t, err)
	require.NotNil(t, p)

	// Verify directory was created
	info, err := os.Stat(basePath)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestLocalProvider_ExistsError(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)

	// Path traversal should return error
	_, err = p.Exists(ctx, "../../../etc/passwd")
	assert.Equal(t, storage.ErrInvalidKey, err)
}

func TestLocalProvider_DeletePathTraversal(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p, err := NewLocalProvider(dir)
	require.NoError(t, err)

	err = p.Delete(ctx, "../../../etc/passwd")
	assert.Equal(t, storage.ErrInvalidKey, err)
}
