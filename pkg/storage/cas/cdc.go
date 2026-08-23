package cas

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
)

// CDC mode stores a workspace as a stream: the walk emits one manifest entry
// per path and hands each chunk straight to an uploader, so nothing that scales
// with the size of the workspace is ever held.
//
// What memory a sync does use is fixed by configuration:
//
//	uploads in flight   MaxConcurrency x Chunker.MaxSize   (default 10 x 8 MB)
//	the chunker         Chunker.MaxSize                    (default 8 MB)
//	the manifest frame  DefaultManifestFrameSize           (4 MB)
//	the dedup set       MaxSeenChunks x ~72 bytes          (default ~75 MB, capped)
//	one file's hashes   file size / Chunker.TargetSize x 64 bytes
//
// Only the last of these depends on the workspace at all, and it depends on the
// largest single file rather than on the tree: a 100 GB file costs 6.4 MB of
// hashes. Everything else is constant. The dedup set is the one structure that
// would otherwise grow without limit, so it stops growing at MaxSeenChunks and
// falls back to asking storage - slower, equally correct.

// walkTree calls fn for every entry under root in directory-walk order, with
// slash-separated paths relative to root. The root itself is skipped.
//
// The order matters twice over: a directory is visited before anything inside
// it, so a restore can create parents as it goes, and two walks of similar
// trees produce the same sequence, which is what lets an incremental sync merge
// a manifest against a walk instead of indexing it.
func walkTree(root string, fn func(rel string, entry fs.DirEntry) error) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		return fn(filepath.ToSlash(rel), entry)
	})
}

// comparePaths orders two slash-separated relative paths the way a directory
// walk visits them: component by component, with a directory immediately before
// everything inside it.
//
// This is not plain string order. "a.txt" sorts before "a/b" as a string, but a
// walk visits the directory "a" and everything under it first, because '.' and
// '/' only differ once the shared component "a" has been consumed.
func comparePaths(a, b string) int {
	for {
		ai := strings.IndexByte(a, '/')
		bi := strings.IndexByte(b, '/')

		ac, bc := a, b
		if ai >= 0 {
			ac = a[:ai]
		}
		if bi >= 0 {
			bc = b[:bi]
		}

		if ac != bc {
			if ac < bc {
				return -1
			}
			return 1
		}

		switch {
		case ai < 0 && bi < 0:
			return 0
		case ai < 0:
			// a ends here, so it is the directory b lives under.
			return -1
		case bi < 0:
			return 1
		}

		a, b = a[ai+1:], b[bi+1:]
	}
}

// =============================================================================
// Uploading
// =============================================================================

// chunkSink uploads chunks with a fixed memory budget.
//
// Slots are both the concurrency limit and the buffer pool: a chunk cannot be
// copied until a slot is free, so the bytes in flight are bounded by the number
// of slots times the largest chunk, with no accounting to get wrong.
type chunkSink struct {
	store    ChunkStore
	tenantID string

	slots chan []byte
	group *errgroup.Group
	gctx  context.Context

	mu       sync.Mutex
	seen     map[string]struct{}
	maxSeen  int
	distinct int
	uploaded int
	newBytes int64
}

func newChunkSink(ctx context.Context, store ChunkStore, tenantID string, cfg Config) *chunkSink {
	group, gctx := errgroup.WithContext(ctx)

	slots := make(chan []byte, cfg.MaxConcurrency)
	for i := 0; i < cfg.MaxConcurrency; i++ {
		slots <- make([]byte, 0, cfg.Chunker.MaxSize)
	}

	return &chunkSink{
		store:    store,
		tenantID: tenantID,
		slots:    slots,
		group:    group,
		gctx:     gctx,
		seen:     make(map[string]struct{}),
		maxSeen:  cfg.MaxSeenChunks,
	}
}

// markSeen records a hash and reports whether this sync had already seen it.
//
// Past MaxSeenChunks the set stops growing. A hash that is not remembered is
// simply asked about again, so the cost of the cap is a storage round trip, not
// a wrong answer.
func (s *chunkSink) markSeen(hash string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.seen[hash]; ok {
		return true
	}
	s.distinct++
	if len(s.seen) < s.maxSeen {
		s.seen[hash] = struct{}{}
	}
	return false
}

// Known records a chunk this sync did not have to read, because an unchanged
// file carried it over from the parent manifest. It counts towards the
// manifest, and it must not be uploaded.
func (s *chunkSink) Known(hash string) { s.markSeen(hash) }

// Add offers a chunk. The data is only read during the call.
func (s *chunkSink) Add(hash string, data []byte) error {
	if s.markSeen(hash) {
		return nil
	}

	var buf []byte
	select {
	case buf = <-s.slots:
	case <-s.gctx.Done():
		return s.gctx.Err()
	}

	buf = append(buf[:0], data...)

	s.group.Go(func() error {
		defer func() { s.slots <- buf[:0] }()

		exists, err := s.store.ChunkExists(s.gctx, s.tenantID, hash)
		if err != nil {
			return fmt.Errorf("failed to check chunk %s: %w", hash, err)
		}
		if exists {
			return nil
		}

		size, err := s.store.StoreChunk(s.gctx, s.tenantID, hash, buf)
		if err != nil {
			return fmt.Errorf("failed to upload chunk %s: %w", hash, err)
		}

		s.mu.Lock()
		s.uploaded++
		s.newBytes += size
		s.mu.Unlock()
		return nil
	})

	return nil
}

// Wait blocks until every offered chunk is durably stored.
func (s *chunkSink) Wait() error { return s.group.Wait() }

// Distinct returns how many distinct chunks the manifest references.
func (s *chunkSink) Distinct() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.distinct
}

// =============================================================================
// The parent manifest
// =============================================================================

// parentLookup answers what the previous manifest said about a path.
//
// An ordered manifest is merged against the walk, which costs one entry of
// memory; anything else is indexed, which costs the manifest. Manifests written
// by this package are ordered, so the indexed path only exists for snapshots
// taken before the walk order was recorded.
type parentLookup struct {
	cursor *ManifestEntries
	byPath map[string]ManifestFile

	held     ManifestFile
	haveHeld bool
	done     bool
	err      error

	// onSkip is told about parent entries the walk passed over, which is how
	// a merge notices deletions without holding either side.
	onSkip func(ManifestFile)
}

// newParentLookup prepares a lookup over a parent manifest cursor.
// A nil cursor produces a lookup that never matches.
func newParentLookup(cursor *ManifestEntries) (*parentLookup, error) {
	if cursor == nil {
		return &parentLookup{done: true}, nil
	}

	lookup := &parentLookup{cursor: cursor}
	if cursor.Header().Ordered {
		return lookup, nil
	}

	lookup.byPath = make(map[string]ManifestFile)
	for {
		entry, err := cursor.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		lookup.byPath[entry.Path] = entry
	}
	lookup.done = true
	return lookup, nil
}

// Lookup returns the parent's entry for path, if it had one.
// Paths must be offered in walk order.
func (l *parentLookup) Lookup(path string) (ManifestFile, bool) {
	if l.byPath != nil {
		entry, ok := l.byPath[path]
		return entry, ok
	}

	for {
		if !l.haveHeld {
			if l.done || l.err != nil {
				return ManifestFile{}, false
			}
			entry, err := l.cursor.Next()
			if errors.Is(err, io.EOF) {
				l.done = true
				return ManifestFile{}, false
			}
			if err != nil {
				l.err = err
				return ManifestFile{}, false
			}
			l.held, l.haveHeld = entry, true
		}

		switch comparePaths(l.held.Path, path) {
		case 0:
			l.haveHeld = false
			return l.held, true
		case 1:
			// The parent is already past this path, so it never had it.
			return ManifestFile{}, false
		default:
			// The parent had a path this walk does not: it was deleted.
			if l.onSkip != nil {
				l.onSkip(l.held)
			}
			l.haveHeld = false
		}
	}
}

// Drain reports the parent entries after the last path the walk offered. They
// are deletions too: the walk simply ended before reaching them.
func (l *parentLookup) Drain() {
	if l.onSkip == nil || l.byPath != nil {
		return
	}
	if l.haveHeld {
		l.onSkip(l.held)
		l.haveHeld = false
	}
	for !l.done && l.err == nil {
		entry, err := l.cursor.Next()
		if errors.Is(err, io.EOF) {
			l.done = true
			return
		}
		if err != nil {
			l.err = err
			return
		}
		l.onSkip(entry)
	}
}

// Err reports a failure reading the parent manifest. A parent that cannot be
// read is not fatal - every file is simply re-chunked - but the caller should
// know it happened.
func (l *parentLookup) Err() error { return l.err }

// reusable reports whether a parent entry can stand in for the file on disk.
//
// Size, modification time and mode together are the fast path every backup tool
// uses. It is a heuristic: a file rewritten within the timestamp's resolution
// with the same length and permissions is missed. The alternative is reading
// every byte of every file on every suspend, which is the cost this mode exists
// to avoid.
func reusable(parent ManifestFile, info fs.FileInfo) bool {
	return parent.IsRegular() &&
		parent.Size == info.Size() &&
		parent.Mode == info.Mode() &&
		parent.ModTime.Equal(info.ModTime())
}

// =============================================================================
// Sync
// =============================================================================

// syncCDC walks srcDir, chunking as it goes, and commits the manifest object.
//
// parent may be nil. When it is not, unchanged files carry their chunk lists
// over untouched, which is what makes the second sync of a workspace cost the
// files that changed rather than the files that exist.
func (s *Sync) syncCDC(ctx context.Context, manifest *Manifest, srcDir string, parent *ManifestEntries, diff *DiffResult) error {
	lookup, err := newParentLookup(parent)
	if err != nil {
		return fmt.Errorf("failed to read parent manifest: %w", err)
	}
	if diff != nil {
		lookup.onSkip = func(entry ManifestFile) {
			if entry.IsRegular() {
				diff.Deleted = append(diff.Deleted, entry.Path)
			}
		}
	}

	writer, err := s.manifestStore.OpenManifestWriter(ctx, manifest)
	if err != nil {
		return err
	}
	defer writer.Abort()

	sink := newChunkSink(ctx, s.chunkStore, manifest.TenantID, s.config)
	chunkIter := s.chunker.NewIterator()

	var totalSize int64

	walkErr := walkTree(srcDir, func(rel string, entry fs.DirEntry) error {
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("failed to stat %s: %w", rel, err)
		}

		record := ManifestFile{
			Path:    rel,
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
		}

		// Every path is offered to the parent, not just the ones that can
		// reuse chunks: the merge only stays aligned if both sides advance
		// together, and a directory skipped here would read as a deletion.
		prev, hadPrev := lookup.Lookup(rel)

		switch {
		case entry.IsDir():
			record.Type = EntryDir

		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(filepath.Join(srcDir, filepath.FromSlash(rel)))
			if err != nil {
				return fmt.Errorf("failed to read link %s: %w", rel, err)
			}
			record.Type = EntrySymlink
			record.Link = target

		case info.Mode().IsRegular():
			record.Size = info.Size()
			totalSize += info.Size()

			if hadPrev && reusable(prev, info) {
				record.Chunks = prev.Chunks
				for _, hash := range prev.Chunks {
					sink.Known(hash)
				}
				if diff != nil {
					diff.Unchanged = append(diff.Unchanged, rel)
				}
				break
			}

			chunks, err := s.chunkFile(filepath.Join(srcDir, filepath.FromSlash(rel)), chunkIter, sink)
			if err != nil {
				return err
			}
			record.Chunks = chunks

			if diff != nil {
				switch {
				case !hadPrev:
					diff.Added = append(diff.Added, rel)
				case chunksEqual(prev.Chunks, chunks):
					// Touched but not changed: the timestamp moved and the
					// content did not, so nothing was uploaded either.
					diff.Unchanged = append(diff.Unchanged, rel)
				default:
					diff.Modified = append(diff.Modified, rel)
				}
			}

		default:
			// Sockets, fifos and devices have no content a snapshot can carry
			// and no meaning on another machine. Recording them as empty files
			// would be worse than leaving them out.
			return nil
		}

		return writer.Append(ctx, record)
	})
	if walkErr != nil {
		// Chunks already in flight must be allowed to finish before the
		// buffers they hold go out of scope.
		_ = sink.Wait()
		return fmt.Errorf("failed to walk directory: %w", walkErr)
	}

	lookup.Drain()

	// Every chunk the manifest names has to be in storage before the manifest
	// that names it. The reverse order publishes a snapshot that cannot be
	// restored.
	if err := sink.Wait(); err != nil {
		return err
	}
	if err := lookup.Err(); err != nil {
		return fmt.Errorf("failed to read parent manifest: %w", err)
	}

	manifest.TotalSize = totalSize
	manifest.ChunkCount = sink.Distinct()
	manifest.Ordered = true

	return writer.Commit(ctx)
}

// chunkFile streams one file through the chunker, offering every chunk to the
// sink and keeping only the hashes.
func (s *Sync) chunkFile(path string, iter *Iterator, sink *chunkSink) ([]string, error) {
	f, err := os.Open(path) //nolint:gosec // path is a walk result under srcDir
	if err != nil {
		return nil, fmt.Errorf("failed to open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()

	hashes := make([]string, 0, 1)
	err = iter.Iterate(f, func(hash string, data []byte) error {
		hashes = append(hashes, hash)
		return sink.Add(hash, data)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to chunk %s: %w", path, err)
	}
	return hashes, nil
}

// =============================================================================
// Restore
// =============================================================================

// restoreCDC materializes a workspace from a streaming manifest cursor.
//
// Re-running a restore that was interrupted converges: entries already on disk
// with the recorded size, mode and modification time are left alone, and every
// file is written to a scratch name and renamed into place, so a file that
// exists is a file that is complete.
func (s *Sync) restoreCDC(ctx context.Context, tenantID, dstDir string, entries *ManifestEntries) error {
	fetch := s.config.MaxConcurrency

	for {
		entry, err := entries.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		target := filepath.Join(dstDir, filepath.FromSlash(entry.Path))
		if !isValidRestorePath(dstDir, target) {
			return &PathTraversalError{Path: entry.Path}
		}

		switch {
		case entry.IsDir():
			if err := restoreDir(target, entry); err != nil {
				return err
			}

		case entry.IsSymlink():
			if err := restoreSymlink(target, entry); err != nil {
				return err
			}

		default:
			if err := s.restoreFile(ctx, tenantID, target, entry, fetch); err != nil {
				return err
			}
		}
	}
}

// restoreDir creates a directory, replacing anything else found at that path.
//
// A manifest names a directory; if the destination holds a symlink there, every
// file restored underneath it would be written wherever that link points. The
// entries arrive in walk order, so replacing it here happens before anything is
// written inside.
func restoreDir(target string, entry ManifestFile) error {
	if info, err := os.Lstat(target); err == nil && !info.IsDir() {
		if err := os.Remove(target); err != nil {
			return fmt.Errorf("failed to replace %s with a directory: %w", entry.Path, err)
		}
	}

	if err := os.MkdirAll(target, entry.Mode.Perm()); err != nil {
		return fmt.Errorf("failed to create directory %s: %w", entry.Path, err)
	}
	// MkdirAll applies the umask and does nothing at all when the directory
	// already exists, so the recorded permissions are set explicitly.
	if err := os.Chmod(target, entry.Mode.Perm()); err != nil {
		return fmt.Errorf("failed to set mode on %s: %w", entry.Path, err)
	}
	return nil
}

// restoreSymlink recreates a link, target text and all. The target is never
// resolved: a link that dangles in the workspace it came from must dangle in
// the one it is restored into.
func restoreSymlink(target string, entry ManifestFile) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory for %s: %w", entry.Path, err)
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to replace %s: %w", entry.Path, err)
	}
	if err := os.Symlink(entry.Link, target); err != nil {
		return fmt.Errorf("failed to create symlink %s: %w", entry.Path, err)
	}
	return nil
}

// restoreFile writes one file from its chunks.
func (s *Sync) restoreFile(ctx context.Context, tenantID, target string, entry ManifestFile, window int) error {
	if upToDate(target, entry) {
		return nil
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// A scratch name in the same directory, so the rename is atomic and a
	// half-written file never carries the name of a complete one.
	scratch := filepath.Join(filepath.Dir(target), "."+filepath.Base(target)+".mrpart")
	if err := os.Remove(scratch); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear scratch file for %s: %w", entry.Path, err)
	}

	f, err := os.OpenFile(scratch, os.O_CREATE|os.O_EXCL|os.O_WRONLY, entry.Mode.Perm()) //nolint:gosec // mode comes from the manifest
	if err != nil {
		return fmt.Errorf("failed to create file %s: %w", entry.Path, err)
	}

	writeErr := s.writeChunks(ctx, tenantID, f, entry, window)
	closeErr := f.Close()
	if writeErr != nil {
		_ = os.Remove(scratch)
		return writeErr
	}
	if closeErr != nil {
		_ = os.Remove(scratch)
		return fmt.Errorf("failed to close file %s: %w", entry.Path, closeErr)
	}

	if err := os.Chmod(scratch, entry.Mode.Perm()); err != nil {
		_ = os.Remove(scratch)
		return fmt.Errorf("failed to set mode on %s: %w", entry.Path, err)
	}
	if !entry.ModTime.IsZero() {
		// Non-fatal: the content is right either way, and the timestamp only
		// costs the next sync a re-chunk of this file.
		_ = os.Chtimes(scratch, entry.ModTime, entry.ModTime)
	}

	if err := os.Rename(scratch, target); err != nil {
		_ = os.Remove(scratch)
		return fmt.Errorf("failed to publish file %s: %w", entry.Path, err)
	}
	return nil
}

// writeChunks streams a file's chunks to w, fetching ahead by up to window
// chunks so a slow object store does not serialise behind the write.
func (s *Sync) writeChunks(ctx context.Context, tenantID string, w io.Writer, entry ManifestFile, window int) error {
	if window < 1 {
		window = 1
	}

	fetchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type fetched struct {
		data []byte
		err  error
	}

	pipeline := make(chan chan fetched, window)

	go func() {
		defer close(pipeline)
		for _, hash := range entry.Chunks {
			slot := make(chan fetched, 1)
			select {
			case pipeline <- slot:
			case <-fetchCtx.Done():
				return
			}

			go func(hash string, slot chan fetched) {
				data, err := s.chunkStore.GetChunk(fetchCtx, tenantID, hash)
				slot <- fetched{data: data, err: err}
			}(hash, slot)
		}
	}()

	index := 0
	for slot := range pipeline {
		result := <-slot
		if result.err != nil {
			return fmt.Errorf("failed to get chunk %s for file %s: %w", entry.Chunks[index], entry.Path, result.err)
		}
		if _, err := w.Write(result.data); err != nil {
			return fmt.Errorf("failed to write chunk to file %s: %w", entry.Path, err)
		}
		index++
	}

	if err := fetchCtx.Err(); err != nil {
		return err
	}
	return nil
}

// upToDate reports whether the file already on disk is the one the manifest
// describes. It is the same heuristic the sync fast path uses, and it is what
// makes re-running an interrupted restore cheap instead of pointless.
func upToDate(target string, entry ManifestFile) bool {
	info, err := os.Lstat(target)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular() &&
		info.Size() == entry.Size &&
		info.Mode().Perm() == entry.Mode.Perm() &&
		!entry.ModTime.IsZero() &&
		info.ModTime().Equal(entry.ModTime)
}
