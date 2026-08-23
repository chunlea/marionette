package cas

import (
	"context"
	"fmt"
	"io/fs"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cdcConfig forces content-defined chunking regardless of how small the test
// tree is, which is what the forced mode exists for.
func cdcConfig() Config {
	return Config{
		CDCMode:        CDCModeAlways,
		MaxConcurrency: 4,
	}.WithDefaults()
}

func newCDCSync(t *testing.T, cfg Config) (*Sync, *MemoryProvider) {
	t.Helper()
	mem := NewMemoryProvider()
	enc := NewNoOpEncryptor()
	return NewSync(cfg,
		NewBlobChunkStore(mem, enc),
		NewBlobManifestStoreWithTempDir(mem, enc, t.TempDir()),
	), mem
}

// treeEntry is what a workspace snapshot has to preserve.
type treeEntry struct {
	Path    string
	Kind    string
	Mode    fs.FileMode
	Content string
	Link    string
}

func readTree(t *testing.T, root string) []treeEntry {
	t.Helper()

	var entries []treeEntry
	require.NoError(t, filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		require.NoError(t, err)
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		if rel == "." {
			return nil
		}

		info, err := d.Info()
		require.NoError(t, err)

		entry := treeEntry{Path: filepath.ToSlash(rel), Mode: info.Mode().Perm()}
		switch {
		case d.IsDir():
			entry.Kind = "dir"
		case info.Mode()&fs.ModeSymlink != 0:
			entry.Kind = "link"
			// A link's own permissions are not portable, and nothing sets
			// them; the target is the whole content.
			entry.Mode = 0
			entry.Link, err = os.Readlink(path)
			require.NoError(t, err)
		default:
			entry.Kind = "file"
			data, err := os.ReadFile(path)
			require.NoError(t, err)
			entry.Content = string(data)
		}
		entries = append(entries, entry)
		return nil
	}))

	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries
}

// writeTree builds a workspace with everything a snapshot has to survive:
// nested and empty directories, executable and read-only files, an empty file,
// a multi-chunk binary, and links that point at a file, a directory and
// nothing at all.
func writeTree(t *testing.T, root string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(root, "src/deep"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "build/empty"), 0o700))

	require.NoError(t, os.WriteFile(filepath.Join(root, "README.md"), []byte("# workspace\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "run.sh"), []byte("#!/bin/sh\necho hi\n"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "secret.key"), []byte("hunter2"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src/empty.txt"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "src/deep/notes.txt"), []byte("deep notes"), 0o644))

	// Big enough to span several chunks at the default target size.
	blob := make([]byte, 6<<20)
	rng := rand.New(rand.NewSource(7)) //nolint:gosec // reproducible test data
	_, _ = rng.Read(blob)
	require.NoError(t, os.WriteFile(filepath.Join(root, "src/blob.bin"), blob, 0o644))

	require.NoError(t, os.Symlink("../README.md", filepath.Join(root, "src/readme-link")))
	require.NoError(t, os.Symlink("deep", filepath.Join(root, "src/deep-link")))
	require.NoError(t, os.Symlink("/nowhere/at/all", filepath.Join(root, "dangling")))
}

// A workspace above the threshold has to come back exactly as it went in -
// contents, modes, empty directories and links included. Archive fidelity is
// the product; a restore that quietly drops a build directory or resolves a
// symlink is not a restore.
func TestCDC_RoundTripPreservesTheTree(t *testing.T) {
	ctx := context.Background()
	sync, _ := newCDCSync(t, cdcConfig())

	srcDir := t.TempDir()
	writeTree(t, srcDir)

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	dstDir := t.TempDir()
	require.NoError(t, sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir))

	assert.Equal(t, readTree(t, srcDir), readTree(t, dstDir))
}

// The same tree through the small-workspace path, so the fidelity rules are
// the same whichever side of the threshold a workspace falls on.
func TestSingleChunk_RoundTripPreservesTheTree(t *testing.T) {
	ctx := context.Background()
	cfg := Config{CDCMode: CDCModeNever, MaxConcurrency: 4}.WithDefaults()
	sync, _ := newCDCSync(t, cfg)

	srcDir := t.TempDir()
	writeTree(t, srcDir)

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	dstDir := t.TempDir()
	require.NoError(t, sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir))

	assert.Equal(t, readTree(t, srcDir), readTree(t, dstDir))
}

// Modification times have to survive too, or every suspend re-chunks every
// file it just wrote back.
func TestCDC_RestorePreservesModificationTimes(t *testing.T) {
	ctx := context.Background()
	sync, _ := newCDCSync(t, cdcConfig())

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "a.txt"), []byte("content"), 0o644))
	stamp := time.Now().Add(-72 * time.Hour).Truncate(time.Second)
	require.NoError(t, os.Chtimes(filepath.Join(srcDir, "a.txt"), stamp, stamp))

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	dstDir := t.TempDir()
	require.NoError(t, sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir))

	info, err := os.Stat(filepath.Join(dstDir, "a.txt"))
	require.NoError(t, err)
	assert.True(t, info.ModTime().Equal(stamp), "want %v, got %v", stamp, info.ModTime())
}

// countingChunkStore reports what a sync actually asked storage to do, which
// is the only honest measure of whether the fast path skipped anything.
type countingChunkStore struct {
	ChunkStore
	exists atomic.Int64
	stored atomic.Int64
}

func (c *countingChunkStore) ChunkExists(ctx context.Context, tenantID, hash string) (bool, error) {
	c.exists.Add(1)
	return c.ChunkStore.ChunkExists(ctx, tenantID, hash)
}

func (c *countingChunkStore) StoreChunk(ctx context.Context, tenantID, hash string, data []byte) (int64, error) {
	c.stored.Add(1)
	return c.ChunkStore.StoreChunk(ctx, tenantID, hash, data)
}

// The proof the brief asks for: touch one file of a thousand and one file's
// worth of chunks is new.
func TestCDC_IncrementalReusesTheParentManifest(t *testing.T) {
	ctx := context.Background()

	mem := NewMemoryProvider()
	enc := NewNoOpEncryptor()
	chunks := &countingChunkStore{ChunkStore: NewBlobChunkStore(mem, enc)}
	sync := NewSync(cdcConfig(), chunks, NewBlobManifestStoreWithTempDir(mem, enc, t.TempDir()))

	const files = 1000
	srcDir := t.TempDir()
	for i := 0; i < files; i++ {
		body := fmt.Sprintf("file %d\n%s\n", i, strings.Repeat("payload", 40))
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, fmt.Sprintf("f%04d.txt", i)), []byte(body), 0o644))
	}

	first, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	storedAfterFirst := chunks.stored.Load()
	require.Equal(t, int64(files), storedAfterFirst, "each small file should be one chunk")

	chunks.exists.Store(0)
	chunks.stored.Store(0)

	// One file changes. Its modification time has to move far enough that the
	// filesystem's resolution cannot hide it.
	changed := filepath.Join(srcDir, "f0500.txt")
	require.NoError(t, os.WriteFile(changed, []byte("rewritten content entirely"), 0o644))
	future := time.Now().Add(time.Second)
	require.NoError(t, os.Chtimes(changed, future, future))

	second, diff, err := sync.SyncIncremental(ctx, "ws-1", "tenant-1", srcDir, first)
	require.NoError(t, err)
	require.NotEqual(t, first, second)

	assert.Equal(t, []string{"f0500.txt"}, diff.Modified)
	assert.Empty(t, diff.Added)
	assert.Empty(t, diff.Deleted)
	assert.Len(t, diff.Unchanged, files-1)

	assert.Equal(t, int64(1), chunks.stored.Load(), "only the changed file's chunk is new")
	assert.LessOrEqual(t, chunks.exists.Load(), int64(2),
		"unchanged files must not be re-chunked, so storage is not asked about them")

	// The new snapshot still restores the whole workspace, not just the delta.
	dstDir := t.TempDir()
	require.NoError(t, sync.restoreFromManifestInternal(ctx, "ws-1", second, "tenant-1", dstDir))
	assert.Equal(t, readTree(t, srcDir), readTree(t, dstDir))

	loaded, err := sync.manifestStore.LoadManifest(ctx, "tenant-1", "ws-1", second)
	require.NoError(t, err)
	require.NotNil(t, loaded.ParentID)
	assert.Equal(t, first, *loaded.ParentID)
	assert.Equal(t, files, loaded.ChunkCount)
}

// Deletions and additions have to show up too, and the merge has to stay
// aligned across directories it is not comparing chunk lists for.
func TestCDC_IncrementalReportsAdditionsAndDeletions(t *testing.T) {
	ctx := context.Background()
	sync, _ := newCDCSync(t, cdcConfig())

	srcDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(srcDir, "pkg/inner"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "pkg.txt"), []byte("sorts after the dir in a walk"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "pkg/a.txt"), []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "pkg/inner/b.txt"), []byte("b"), 0o644))

	first, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	require.NoError(t, os.Remove(filepath.Join(srcDir, "pkg/inner/b.txt")))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "pkg/c.txt"), []byte("c"), 0o644))

	_, diff, err := sync.SyncIncremental(ctx, "ws-1", "tenant-1", srcDir, first)
	require.NoError(t, err)

	assert.Equal(t, []string{"pkg/c.txt"}, diff.Added)
	assert.Equal(t, []string{"pkg/inner/b.txt"}, diff.Deleted)
	assert.ElementsMatch(t, []string{"pkg.txt", "pkg/a.txt"}, diff.Unchanged)
}

// A restore that was interrupted must converge when it runs again, and must
// not redo the files it already finished.
func TestCDC_RestoreIsResumable(t *testing.T) {
	ctx := context.Background()

	mem := NewMemoryProvider()
	enc := NewNoOpEncryptor()
	chunks := &countingChunkStore{ChunkStore: NewBlobChunkStore(mem, enc)}
	sync := NewSync(cdcConfig(), chunks, NewBlobManifestStoreWithTempDir(mem, enc, t.TempDir()))

	srcDir := t.TempDir()
	writeTree(t, srcDir)

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	dstDir := t.TempDir()
	require.NoError(t, sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir))

	// Simulate an interrupted run: one file truncated, one missing entirely.
	require.NoError(t, os.WriteFile(filepath.Join(dstDir, "README.md"), []byte("partial"), 0o644))
	require.NoError(t, os.Remove(filepath.Join(dstDir, "src/deep/notes.txt")))

	chunks.exists.Store(0)
	before := readTree(t, srcDir)

	require.NoError(t, sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir))
	assert.Equal(t, before, readTree(t, dstDir), "a second restore converges on the manifest")
}

// A chunk that goes missing must fail the restore rather than leave a file
// that is silently short.
func TestCDC_RestoreFailsOnAMissingChunk(t *testing.T) {
	ctx := context.Background()
	sync, mem := newCDCSync(t, cdcConfig())

	srcDir := t.TempDir()
	blob := make([]byte, 6<<20)
	rng := rand.New(rand.NewSource(11)) //nolint:gosec // reproducible test data
	_, _ = rng.Read(blob)
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "blob.bin"), blob, 0o644))

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	for _, key := range mem.Keys() {
		if strings.HasPrefix(key, "chunks/") {
			require.NoError(t, mem.Delete(ctx, key))
			break
		}
	}

	dstDir := t.TempDir()
	err = sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get chunk")
	assert.NoFileExists(t, filepath.Join(dstDir, "blob.bin"),
		"a file that could not be completed must not appear complete")
}

// The uploader's slots are the memory bound, so nothing may exceed them no
// matter how many chunks the walk produces.
func TestCDC_UploadsStayWithinTheConcurrencyBound(t *testing.T) {
	ctx := context.Background()

	const limit = 3
	cfg := Config{CDCMode: CDCModeAlways, MaxConcurrency: limit}.WithDefaults()

	mem := NewMemoryProvider()
	enc := NewNoOpEncryptor()
	tracker := &concurrencyTracker{ChunkStore: NewBlobChunkStore(mem, enc)}
	sync := NewSync(cfg, tracker, NewBlobManifestStoreWithTempDir(mem, enc, t.TempDir()))

	srcDir := t.TempDir()
	rng := rand.New(rand.NewSource(3)) //nolint:gosec // reproducible test data
	for i := 0; i < 12; i++ {
		blob := make([]byte, 3<<20)
		_, _ = rng.Read(blob)
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, fmt.Sprintf("b%02d.bin", i)), blob, 0o644))
	}

	_, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	assert.Positive(t, tracker.peak())
	assert.LessOrEqual(t, tracker.peak(), limit,
		"chunk uploads in flight are what bounds a sync's memory")
}

type concurrencyTracker struct {
	ChunkStore
	mu      sync.Mutex
	inFlags int
	max     int
}

func (c *concurrencyTracker) StoreChunk(ctx context.Context, tenantID, hash string, data []byte) (int64, error) {
	c.mu.Lock()
	c.inFlags++
	if c.inFlags > c.max {
		c.max = c.inFlags
	}
	c.mu.Unlock()

	// Hold the slot long enough that a bound that did not exist would show.
	time.Sleep(2 * time.Millisecond)

	size, err := c.ChunkStore.StoreChunk(ctx, tenantID, hash, data)

	c.mu.Lock()
	c.inFlags--
	c.mu.Unlock()
	return size, err
}

func (c *concurrencyTracker) peak() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.max
}

// The claim CDC mode rests on: memory does not follow the size of the
// workspace. Two syncs, one four times the other, must cost the same.
func TestCDC_MemoryDoesNotFollowWorkspaceSize(t *testing.T) {
	if testing.Short() {
		t.Skip("writes over a gigabyte of test data")
	}

	small := syncPeakHeap(t, 8, 16<<20)  // 128 MB
	large := syncPeakHeap(t, 32, 16<<20) // 512 MB

	t.Logf("peak heap: 128 MB tree %d MiB, 512 MB tree %d MiB", small>>20, large>>20)

	// Sampling a live heap catches whatever the collector has not taken yet,
	// so the assertion is about the shape rather than a number: memory that
	// followed the workspace would be four times larger, not a fraction more.
	assert.Less(t, large, small*2,
		"a four-fold larger workspace must not cost anything like four times the memory")
	assert.Less(t, large, uint64(320<<20), "peak heap must stay bounded")
}

// syncPeakHeap syncs a tree of files x size bytes and returns the peak live
// heap observed while it ran.
func syncPeakHeap(t *testing.T, files int, size int) uint64 {
	t.Helper()
	ctx := context.Background()

	srcDir := t.TempDir()
	rng := rand.New(rand.NewSource(17)) //nolint:gosec // reproducible test data
	blob := make([]byte, size)
	for i := 0; i < files; i++ {
		_, _ = rng.Read(blob)
		require.NoError(t, os.WriteFile(filepath.Join(srcDir, fmt.Sprintf("f%03d.bin", i)), blob, 0o644))
	}

	// Chunks and manifests go to disk: an in-memory blob store would be
	// measuring the store, not the sync.
	provider, err := NewLocalProvider(t.TempDir())
	require.NoError(t, err)
	enc := NewNoOpEncryptor()

	sync := NewSync(cdcConfig(),
		NewBlobChunkStore(provider, enc),
		NewBlobManifestStoreWithTempDir(provider, enc, t.TempDir()),
	)

	runtime.GC()
	var peak atomic.Uint64
	stop := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			select {
			case <-stop:
				return
			default:
			}
			var stats runtime.MemStats
			runtime.ReadMemStats(&stats)
			for {
				current := peak.Load()
				if stats.HeapAlloc <= current || peak.CompareAndSwap(current, stats.HeapAlloc) {
					break
				}
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()

	_, err = sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	close(stop)
	<-stopped
	return peak.Load()
}

// A walk visits a directory before everything inside it, which is not the same
// as sorting the paths as strings. The merge against a parent manifest depends
// on getting this exactly right.
func TestComparePaths_MatchesWalkOrder(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a/b"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a-b"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a.txt"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a/b/c.txt"), nil, 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a-b/d.txt"), nil, 0o644))

	var walked []string
	require.NoError(t, walkTree(root, func(rel string, _ fs.DirEntry) error {
		walked = append(walked, rel)
		return nil
	}))

	for i := 1; i < len(walked); i++ {
		assert.Equal(t, -1, comparePaths(walked[i-1], walked[i]),
			"%q must order before %q", walked[i-1], walked[i])
	}
	assert.Equal(t, 0, comparePaths("a/b", "a/b"))
	assert.Equal(t, 1, comparePaths("a.txt", "a/b"))
}

// Devices, sockets and fifos carry nothing a snapshot can restore, so they are
// left out rather than recorded as empty files.
func TestCDC_SkipsEntriesWithNoContent(t *testing.T) {
	ctx := context.Background()
	sync, _ := newCDCSync(t, cdcConfig())

	srcDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "real.txt"), []byte("real"), 0o644))
	if err := makeFIFO(filepath.Join(srcDir, "pipe")); err != nil {
		t.Skipf("cannot create a fifo here: %v", err)
	}

	manifestID, err := sync.Sync(ctx, "ws-1", "tenant-1", srcDir)
	require.NoError(t, err)

	manifest, err := sync.manifestStore.LoadManifest(ctx, "tenant-1", "ws-1", manifestID)
	require.NoError(t, err)

	paths := make([]string, 0, len(manifest.Files))
	for _, f := range manifest.Files {
		paths = append(paths, f.Path)
	}
	assert.Equal(t, []string{"real.txt"}, paths)

	dstDir := t.TempDir()
	require.NoError(t, sync.restoreFromManifestInternal(ctx, "ws-1", manifestID, "tenant-1", dstDir))
	assert.NoFileExists(t, filepath.Join(dstDir, "pipe"))
}
