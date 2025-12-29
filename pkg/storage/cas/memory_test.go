package cas

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/storage"
)

func TestMemoryProvider_Name(t *testing.T) {
	p := NewMemoryProvider()
	assert.Equal(t, "memory", p.Name())
}

func TestMemoryProvider_UploadDownload(t *testing.T) {
	ctx := context.Background()
	p := NewMemoryProvider()

	key := "test/file.txt"
	data := []byte("hello world")

	// Upload
	err := p.Upload(ctx, key, bytes.NewReader(data), storage.UploadOptions{})
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

func TestMemoryProvider_DownloadNotFound(t *testing.T) {
	ctx := context.Background()
	p := NewMemoryProvider()

	_, _, err := p.Download(ctx, "nonexistent")
	assert.Equal(t, storage.ErrNotFound, err)
}

func TestMemoryProvider_Delete(t *testing.T) {
	ctx := context.Background()
	p := NewMemoryProvider()

	key := "test/file.txt"
	data := []byte("hello world")

	// Upload
	err := p.Upload(ctx, key, bytes.NewReader(data), storage.UploadOptions{})
	require.NoError(t, err)

	// Delete
	err = p.Delete(ctx, key)
	require.NoError(t, err)

	// Should not exist
	exists, err := p.Exists(ctx, key)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestMemoryProvider_DeleteNonexistent(t *testing.T) {
	ctx := context.Background()
	p := NewMemoryProvider()

	// Delete nonexistent should not error (idempotent)
	err := p.Delete(ctx, "nonexistent")
	assert.NoError(t, err)
}

func TestMemoryProvider_Exists(t *testing.T) {
	ctx := context.Background()
	p := NewMemoryProvider()

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

func TestMemoryProvider_DataIsolation(t *testing.T) {
	ctx := context.Background()
	p := NewMemoryProvider()

	key := "test/file.txt"
	original := []byte("original data")

	// Upload
	err := p.Upload(ctx, key, bytes.NewReader(original), storage.UploadOptions{})
	require.NoError(t, err)

	// Modify original slice
	original[0] = 'X'

	// Download should return unmodified data
	reader, _, err := p.Download(ctx, key)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()

	downloaded, err := io.ReadAll(reader)
	require.NoError(t, err)
	assert.Equal(t, []byte("original data"), downloaded)
}

func TestMemoryProvider_Keys(t *testing.T) {
	ctx := context.Background()
	p := NewMemoryProvider()

	// Upload some files
	_ = p.Upload(ctx, "a/b.txt", bytes.NewReader([]byte("1")), storage.UploadOptions{})
	_ = p.Upload(ctx, "c/d.txt", bytes.NewReader([]byte("2")), storage.UploadOptions{})

	keys := p.Keys()
	assert.Len(t, keys, 2)
	assert.Contains(t, keys, "a/b.txt")
	assert.Contains(t, keys, "c/d.txt")
}

func TestMemoryProvider_Size(t *testing.T) {
	ctx := context.Background()
	p := NewMemoryProvider()

	assert.Equal(t, int64(0), p.Size())

	_ = p.Upload(ctx, "a.txt", bytes.NewReader([]byte("hello")), storage.UploadOptions{})
	assert.Equal(t, int64(5), p.Size())

	_ = p.Upload(ctx, "b.txt", bytes.NewReader([]byte("world")), storage.UploadOptions{})
	assert.Equal(t, int64(10), p.Size())
}

func TestMemoryProvider_Clear(t *testing.T) {
	ctx := context.Background()
	p := NewMemoryProvider()

	_ = p.Upload(ctx, "a.txt", bytes.NewReader([]byte("hello")), storage.UploadOptions{})
	_ = p.Upload(ctx, "b.txt", bytes.NewReader([]byte("world")), storage.UploadOptions{})

	p.Clear()

	assert.Equal(t, int64(0), p.Size())
	assert.Empty(t, p.Keys())
}
