package cas

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/storage"
)

func newStreamManifestStore(t *testing.T) (*BlobManifestStore, *MemoryProvider) {
	t.Helper()
	mem := NewMemoryProvider()
	return NewBlobManifestStoreWithTempDir(mem, NewNoOpEncryptor(), t.TempDir()), mem
}

// rawObject returns the bytes stored under key.
func rawObject(t *testing.T, mem *MemoryProvider, key string) []byte {
	t.Helper()
	reader, _, err := mem.Download(context.Background(), key)
	require.NoError(t, err)
	defer func() { _ = reader.Close() }()
	data, err := io.ReadAll(reader)
	require.NoError(t, err)
	return data
}

// putObject overwrites the bytes stored under key.
func putObject(t *testing.T, mem *MemoryProvider, key string, data []byte) {
	t.Helper()
	require.NoError(t, mem.Upload(context.Background(), key, bytes.NewReader(data), storage.UploadOptions{}))
}

func streamEntries(t *testing.T, store *BlobManifestStore, tenantID, workspaceID, manifestID string) (ManifestHeader, []ManifestFile) {
	t.Helper()

	cursor, err := store.OpenManifest(context.Background(), tenantID, workspaceID, manifestID)
	require.NoError(t, err)
	defer func() { _ = cursor.Close() }()

	var got []ManifestFile
	for {
		entry, err := cursor.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		require.NoError(t, err)
		got = append(got, entry)
	}
	return cursor.Header(), got
}

// A manifest big enough to need many frames must come back exactly as it went
// in, including the entries that straddle a frame boundary.
func TestManifestWriter_FramedRoundTrip(t *testing.T) {
	ctx := context.Background()
	store, mem := newStreamManifestStore(t)

	manifest := &Manifest{
		ID:          "mfst_framed",
		WorkspaceID: "ws-1",
		TenantID:    "tenant-1",
		CreatedAt:   time.Now().UTC().Truncate(time.Second),
		TotalSize:   4096,
		ChunkCount:  9,
		Ordered:     true,
	}

	writer, err := store.OpenManifestWriter(ctx, manifest)
	require.NoError(t, err)
	// Small frames so a modest test tree still exercises the framing.
	writer.frameSize = 4 * 1024

	const entries = 500
	want := make([]ManifestFile, 0, entries)
	for i := 0; i < entries; i++ {
		entry := ManifestFile{
			Path:    fmt.Sprintf("dir%03d/file%03d.txt", i%7, i),
			Mode:    0o644,
			ModTime: manifest.CreatedAt,
			Size:    int64(i),
			Chunks:  []string{HashData([]byte(fmt.Sprintf("c%d", i)))},
		}
		want = append(want, entry)
		require.NoError(t, writer.Append(ctx, entry))
	}
	require.NoError(t, writer.Commit(ctx))

	// The object must be framed, not the single-buffer encoding.
	key := manifestKey(manifest.TenantID, manifest.WorkspaceID, manifest.ID)
	raw := rawObject(t, mem, key)
	require.True(t, strings.HasPrefix(string(raw), string(framedMagic)),
		"a streamed manifest must use the framed encoding")

	header, got := streamEntries(t, store, manifest.TenantID, manifest.WorkspaceID, manifest.ID)
	assert.Equal(t, entries, header.FileCount)
	assert.True(t, header.Ordered)
	assert.Equal(t, 9, header.ChunkCount)
	assert.Equal(t, want, got)
}

// The header carries counts that are only known once the walk is done, so it
// has to be written last and still land first in the object.
func TestManifestWriter_HeaderCountsReflectTheWalk(t *testing.T) {
	ctx := context.Background()
	store, _ := newStreamManifestStore(t)

	manifest := &Manifest{ID: "mfst_counts", WorkspaceID: "ws-1", TenantID: "t"}
	writer, err := store.OpenManifestWriter(ctx, manifest)
	require.NoError(t, err)

	for i := 0; i < 3; i++ {
		require.NoError(t, writer.Append(ctx, ManifestFile{Path: fmt.Sprintf("f%d", i)}))
	}
	// Discovered during the walk, set before the commit.
	manifest.ChunkCount = 12
	manifest.TotalSize = 345
	require.NoError(t, writer.Commit(ctx))

	loaded, err := store.LoadManifest(ctx, "t", "ws-1", "mfst_counts")
	require.NoError(t, err)
	assert.Equal(t, 12, loaded.ChunkCount)
	assert.Equal(t, int64(345), loaded.TotalSize)
	assert.Equal(t, 3, loaded.FileCount)
	assert.Len(t, loaded.Files, 3)
	assert.Equal(t, 3, writer.Entries())
}

// An empty workspace still produces a readable manifest: a header and nothing
// else.
func TestManifestWriter_NoEntries(t *testing.T) {
	ctx := context.Background()
	store, _ := newStreamManifestStore(t)

	manifest := &Manifest{ID: "mfst_empty", WorkspaceID: "ws-1", TenantID: "t"}
	writer, err := store.OpenManifestWriter(ctx, manifest)
	require.NoError(t, err)
	require.NoError(t, writer.Commit(ctx))

	header, got := streamEntries(t, store, "t", "ws-1", "mfst_empty")
	assert.Equal(t, 0, header.FileCount)
	assert.Empty(t, got)
}

// Manifests written by the single-buffer encoder must keep reading. This is the
// small-workspace path, which SaveManifest still produces.
func TestManifestReader_ReadsTheUnframedEncoding(t *testing.T) {
	ctx := context.Background()
	store, mem := newStreamManifestStore(t)

	hash := HashData([]byte("tar"))
	manifest := &Manifest{
		ID:          "mfst_legacy",
		WorkspaceID: "ws-1",
		TenantID:    "t",
		SingleChunk: true,
		ChunkHash:   &hash,
		ChunkCount:  1,
		Files: []ManifestFile{
			{Path: "a.txt", Mode: 0o600, Size: 3, Chunks: []string{hash}},
		},
	}
	require.NoError(t, store.SaveManifest(ctx, manifest))

	raw := rawObject(t, mem, manifestKey("t", "ws-1", "mfst_legacy"))
	require.False(t, strings.HasPrefix(string(raw), string(framedMagic)),
		"SaveManifest must keep producing the original encoding")

	header, got := streamEntries(t, store, "t", "ws-1", "mfst_legacy")
	assert.True(t, header.SingleChunk)
	assert.Equal(t, hash, header.ChunkHash)
	require.Len(t, got, 1)
	assert.Equal(t, "a.txt", got[0].Path)
}

// Entries written before directories and symlinks were recorded have no type
// field, and must still read as regular files.
func TestManifestReader_UntypedEntriesAreFiles(t *testing.T) {
	ctx := context.Background()
	store, _ := newStreamManifestStore(t)

	manifest := &Manifest{
		ID: "mfst_untyped", WorkspaceID: "ws-1", TenantID: "t",
		Files: []ManifestFile{{Path: "a.txt", Mode: 0o644, Size: 1}},
	}
	require.NoError(t, store.SaveManifest(ctx, manifest))

	_, got := streamEntries(t, store, "t", "ws-1", "mfst_untyped")
	require.Len(t, got, 1)
	assert.True(t, got[0].IsRegular())
	assert.False(t, got[0].IsDir())
	assert.False(t, got[0].IsSymlink())
}

// A truncated object must be reported, not silently read as a short manifest:
// a restore that stops early looks exactly like a workspace that lost files.
func TestManifestReader_TruncatedFrameIsAnError(t *testing.T) {
	ctx := context.Background()
	store, mem := newStreamManifestStore(t)

	manifest := &Manifest{ID: "mfst_trunc", WorkspaceID: "ws-1", TenantID: "t"}
	writer, err := store.OpenManifestWriter(ctx, manifest)
	require.NoError(t, err)
	writer.frameSize = 128
	for i := 0; i < 50; i++ {
		require.NoError(t, writer.Append(ctx, ManifestFile{Path: fmt.Sprintf("f%03d", i)}))
	}
	require.NoError(t, writer.Commit(ctx))

	key := manifestKey("t", "ws-1", "mfst_trunc")
	raw := rawObject(t, mem, key)
	putObject(t, mem, key, raw[:len(raw)-8])

	cursor, err := store.OpenManifest(ctx, "t", "ws-1", "mfst_trunc")
	require.NoError(t, err)
	defer func() { _ = cursor.Close() }()

	var readErr error
	for {
		if _, err := cursor.Next(); err != nil {
			readErr = err
			break
		}
	}
	require.Error(t, readErr)
	assert.NotErrorIs(t, readErr, io.EOF, "a truncated manifest must not read as a complete one")
	assert.ErrorIs(t, readErr, ErrManifestCorrupt)
}

// An empty object is not a manifest with no files; it is not a manifest.
func TestManifestReader_EmptyObject(t *testing.T) {
	ctx := context.Background()
	store, mem := newStreamManifestStore(t)

	putObject(t, mem, manifestKey("t", "ws-1", "mfst_void"), nil)

	_, err := store.OpenManifest(ctx, "t", "ws-1", "mfst_void")
	require.Error(t, err)
}
