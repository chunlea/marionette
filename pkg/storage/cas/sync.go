package cas

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/klauspost/compress/zstd"
	"golang.org/x/sync/errgroup"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/storage"
)

// BlobChunkStore implements ChunkStore using a blob storage provider.
type BlobChunkStore struct {
	storage   storage.StorageProvider
	encryptor Encryptor
}

// NewBlobChunkStore creates a new chunk store.
func NewBlobChunkStore(storageProvider storage.StorageProvider, encryptor Encryptor) *BlobChunkStore {
	return &BlobChunkStore{
		storage:   storageProvider,
		encryptor: encryptor,
	}
}

// chunkKey returns the storage key for a chunk.
// Layout: chunks/{tenant_id}/{hash[:2]}/{hash}.blob.enc
func chunkKey(tenantID, hash string) string {
	prefix := hash[:2]
	return fmt.Sprintf("chunks/%s/%s/%s.blob.enc", tenantID, prefix, hash)
}

// StoreChunk compresses, encrypts, and stores a chunk.
func (s *BlobChunkStore) StoreChunk(ctx context.Context, tenantID, hash string, data []byte) (int64, error) {
	// Encrypt (encryptor handles compression)
	encrypted, err := s.encryptor.Encrypt(ctx, tenantID, data)
	if err != nil {
		return 0, fmt.Errorf("failed to encrypt chunk: %w", err)
	}

	// Upload
	key := chunkKey(tenantID, hash)
	if err := s.storage.Upload(ctx, key, bytes.NewReader(encrypted), storage.UploadOptions{
		ContentType: "application/octet-stream",
	}); err != nil {
		return 0, fmt.Errorf("failed to upload chunk: %w", err)
	}

	return int64(len(encrypted)), nil
}

// GetChunk retrieves, decrypts, and decompresses a chunk.
func (s *BlobChunkStore) GetChunk(ctx context.Context, tenantID, hash string) ([]byte, error) {
	key := chunkKey(tenantID, hash)

	// Download
	reader, _, err := s.storage.Download(ctx, key)
	if err != nil {
		if err == storage.ErrNotFound {
			return nil, ErrChunkNotFound
		}
		return nil, fmt.Errorf("failed to download chunk: %w", err)
	}
	defer func() { _ = reader.Close() }()

	encrypted, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read chunk: %w", err)
	}

	// Decrypt (encryptor handles decompression)
	data, err := s.encryptor.Decrypt(ctx, tenantID, encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt chunk: %w", err)
	}

	// Verify hash
	if HashData(data) != hash {
		return nil, ErrChunkCorrupted
	}

	return data, nil
}

// DeleteChunk removes a chunk from storage.
func (s *BlobChunkStore) DeleteChunk(ctx context.Context, tenantID, hash string) error {
	key := chunkKey(tenantID, hash)
	return s.storage.Delete(ctx, key)
}

// ChunkExists checks if a chunk exists in storage.
func (s *BlobChunkStore) ChunkExists(ctx context.Context, tenantID, hash string) (bool, error) {
	key := chunkKey(tenantID, hash)
	return s.storage.Exists(ctx, key)
}

// Compile-time interface check.
var _ ChunkStore = (*BlobChunkStore)(nil)

// Sync implements the Syncer interface for workspace synchronization.
type Sync struct {
	config        Config
	chunkStore    ChunkStore
	manifestStore ManifestStore
	chunker       *Chunker
}

// NewSync creates a new sync orchestrator.
func NewSync(config Config, chunkStore ChunkStore, manifestStore ManifestStore) *Sync {
	cfg := config.WithDefaults()
	return &Sync{
		config:        cfg,
		chunkStore:    chunkStore,
		manifestStore: manifestStore,
		chunker:       NewChunker(cfg.Chunker),
	}
}

// Sync performs workspace synchronization.
// Below CDCThreshold the workspace is stored as one tar.zst chunk; at or above
// it, content-defined chunking streams the tree instead. CDCMode overrides the
// choice.
func (s *Sync) Sync(ctx context.Context, workspaceID, tenantID, srcDir string) (string, error) {
	manifestID, _, err := s.sync(ctx, workspaceID, tenantID, srcDir, "", nil)
	return manifestID, err
}

// sync is the one implementation behind Sync and SyncIncremental.
//
// parentManifestID may be empty. diff may be nil, and is nil on the path a
// runner actually takes: a diff is a list of every path that changed, so
// collecting one costs memory proportional to the workspace, which is exactly
// what CDC mode exists to avoid.
func (s *Sync) sync(ctx context.Context, workspaceID, tenantID, srcDir, parentManifestID string, diff *DiffResult) (string, bool, error) {
	// The size pass is a second walk, so it is skipped when the mode is
	// already decided. CDC recomputes the total from the walk it does anyway.
	var totalSize int64
	if s.config.CDCMode != CDCModeAlways {
		var err error
		totalSize, err = calculateDirSize(srcDir)
		if err != nil {
			return "", false, fmt.Errorf("failed to calculate directory size: %w", err)
		}
	}

	manifest := &Manifest{
		ID:          id.Manifest(),
		WorkspaceID: workspaceID,
		TenantID:    tenantID,
		CreatedAt:   time.Now(),
		TotalSize:   totalSize,
		ParentID:    stringPtr(parentManifestID),
	}

	if !s.config.useCDC(totalSize) {
		if err := s.syncSingleChunk(ctx, manifest, srcDir); err != nil {
			return "", false, err
		}
		if err := s.manifestStore.SaveManifest(ctx, manifest); err != nil {
			return "", false, fmt.Errorf("failed to save manifest: %w", err)
		}
		return manifest.ID, false, nil
	}

	parent, err := s.openParent(ctx, workspaceID, tenantID, parentManifestID)
	if err != nil {
		return "", true, err
	}
	if parent != nil {
		defer func() { _ = parent.Close() }()
	}

	if err := s.syncCDC(ctx, manifest, srcDir, parent, diff); err != nil {
		return "", true, err
	}
	return manifest.ID, true, nil
}

// openParent opens the previous snapshot so unchanged files can carry their
// chunk lists over. A missing or single-chunk parent is not an error: it just
// means everything is re-chunked.
func (s *Sync) openParent(ctx context.Context, workspaceID, tenantID, manifestID string) (*ManifestEntries, error) {
	if manifestID == "" {
		return nil, nil
	}

	cursor, err := s.manifestStore.OpenManifest(ctx, tenantID, workspaceID, manifestID)
	if err != nil {
		return nil, fmt.Errorf("failed to load previous manifest: %w", err)
	}
	if cursor.Header().SingleChunk {
		// A tar archive has no per-file chunk lists to reuse.
		_ = cursor.Close()
		return nil, nil
	}
	return cursor, nil
}

// syncSingleChunk creates a tar.zst archive of the entire directory.
func (s *Sync) syncSingleChunk(ctx context.Context, manifest *Manifest, srcDir string) error {
	// Create tar archive in memory
	var buf bytes.Buffer

	// Create zstd writer
	encoder, err := zstd.NewWriter(&buf, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return fmt.Errorf("failed to create zstd encoder: %w", err)
	}

	// Create tar writer on top of zstd
	tw := tar.NewWriter(encoder)

	// Walk directory and add files
	err = filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// Skip root
		if relPath == "." {
			return nil
		}

		// Symlinks need their target, and tar.FileInfoHeader cannot read it:
		// passing "" records a link that points nowhere, which is how the
		// small-workspace path used to lose every symlink it archived.
		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}

		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// Write file content if regular file
		if info.Mode().IsRegular() {
			f, err := os.Open(path) //nolint:gosec // path is from filepath.Walk within srcDir
			if err != nil {
				return err
			}

			_, copyErr := io.Copy(tw, f)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		}

		return nil
	})
	if err != nil {
		_ = tw.Close()
		_ = encoder.Close()
		return fmt.Errorf("failed to create tar archive: %w", err)
	}

	if err := tw.Close(); err != nil {
		_ = encoder.Close()
		return fmt.Errorf("failed to close tar writer: %w", err)
	}
	if err := encoder.Close(); err != nil {
		return fmt.Errorf("failed to close zstd encoder: %w", err)
	}

	// Compute hash and store
	data := buf.Bytes()
	hash := HashData(data)

	// Store the chunk
	if _, err := s.chunkStore.StoreChunk(ctx, manifest.TenantID, hash, data); err != nil {
		return fmt.Errorf("failed to store single chunk: %w", err)
	}

	manifest.SingleChunk = true
	manifest.ChunkHash = &hash
	manifest.ChunkCount = 1

	return nil
}

// Restore reconstructs a workspace from the latest manifest.
func (s *Sync) Restore(_ context.Context, _, _, _ string) error {
	// Load the latest manifest
	// Note: In a real implementation, we'd query the manifest store for the latest manifest
	// For now, we require a specific manifest ID via RestoreFromManifest
	return fmt.Errorf("Restore requires manifest ID; use RestoreFromManifest instead")
}

// RestoreFromManifest restores from a specific manifest.
func (s *Sync) RestoreFromManifest(ctx context.Context, manifestID, tenantID, dstDir string) error {
	// We need workspaceID to load the manifest
	// This is a limitation of the current interface - we may need to adjust
	// For now, we'll scan for the manifest by ID across workspaces
	// In practice, the caller should provide the workspaceID
	return s.restoreFromManifestInternal(ctx, "", manifestID, tenantID, dstDir)
}

// restoreFromManifestInternal performs the actual restore.
func (s *Sync) restoreFromManifestInternal(ctx context.Context, workspaceID, manifestID, tenantID, dstDir string) error {
	entries, err := s.manifestStore.OpenManifest(ctx, tenantID, workspaceID, manifestID)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}
	defer func() { _ = entries.Close() }()

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if header := entries.Header(); header.SingleChunk {
		return s.restoreSingleChunk(ctx, header.ChunkHash, tenantID, dstDir)
	}

	return s.restoreCDC(ctx, tenantID, dstDir, entries)
}

// restoreSingleChunk extracts a tar.zst archive.
func (s *Sync) restoreSingleChunk(ctx context.Context, hash, tenantID, dstDir string) error {
	// Get the single chunk
	data, err := s.chunkStore.GetChunk(ctx, tenantID, hash)
	if err != nil {
		return fmt.Errorf("failed to get single chunk: %w", err)
	}

	// Decompress zstd
	decoder, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("failed to create zstd decoder: %w", err)
	}
	defer decoder.Close()

	// Extract tar
	tr := tar.NewReader(decoder)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read tar header: %w", err)
		}

		target := filepath.Join(dstDir, header.Name)

		// Security: prevent path traversal
		if !isValidRestorePath(dstDir, target) {
			return &PathTraversalError{Path: header.Name}
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
			}
			// MkdirAll applies the umask, and does nothing at all when the
			// directory was already created as some file's parent.
			if err := os.Chmod(target, header.FileInfo().Mode().Perm()); err != nil {
				return fmt.Errorf("failed to set directory mode: %w", err)
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}
			if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("failed to replace symlink: %w", err)
			}
			if err := os.Symlink(header.Linkname, target); err != nil {
				return fmt.Errorf("failed to create symlink: %w", err)
			}
		case tar.TypeReg:
			// Create parent directory
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return fmt.Errorf("failed to create parent directory: %w", err)
			}

			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return fmt.Errorf("failed to create file: %w", err)
			}

			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return fmt.Errorf("failed to write file: %w", err)
			}
			_ = f.Close()

			// Restore modification time (non-fatal if fails)
			_ = os.Chtimes(target, header.ModTime, header.ModTime)
		}
	}

	return nil
}

// ValidateManifest checks that all chunks referenced by a manifest exist.
func (s *Sync) ValidateManifest(ctx context.Context, manifest *Manifest) error {
	hashes := manifest.CollectChunkHashes()

	var missing []string
	var mu sync.Mutex

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(s.config.MaxConcurrency)

	for _, hash := range hashes {
		hash := hash
		g.Go(func() error {
			exists, err := s.chunkStore.ChunkExists(ctx, manifest.TenantID, hash)
			if err != nil {
				return fmt.Errorf("failed to check chunk %s: %w", hash, err)
			}
			if !exists {
				mu.Lock()
				missing = append(missing, hash)
				mu.Unlock()
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return err
	}

	if len(missing) > 0 {
		return &ChunksMissingError{Missing: missing, InStorage: true}
	}

	return nil
}

// isValidRestorePath checks that the target path doesn't escape the base
// directory.
//
// The comparison is on path components, not on the string: "/tmp/ws" is not a
// prefix of "/tmp/ws-elsewhere" in any sense a restore should accept, and the
// string compare it used to do said otherwise.
func isValidRestorePath(baseDir, target string) bool {
	rel, err := filepath.Rel(filepath.Clean(baseDir), filepath.Clean(target))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// calculateDirSize calculates the total size of all files in a directory.
func calculateDirSize(dir string) (int64, error) {
	var size int64
	err := filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	return size, err
}

// SyncIncremental performs incremental sync based on a previous manifest.
//
// It is the same walk as Sync with the parent manifest merged in, plus a record
// of what changed. That record names every path, so it is bounded by the
// workspace rather than by a chunk: callers that only need the snapshot should
// use Sync.
func (s *Sync) SyncIncremental(ctx context.Context, workspaceID, tenantID, srcDir, previousManifestID string) (string, *DiffResult, error) {
	diff := &DiffResult{
		Added:     make([]string, 0),
		Modified:  make([]string, 0),
		Deleted:   make([]string, 0),
		Unchanged: make([]string, 0),
	}

	manifestID, usedCDC, err := s.sync(ctx, workspaceID, tenantID, srcDir, previousManifestID, diff)
	if err != nil {
		return "", nil, err
	}

	if !usedCDC {
		// One chunk holds the whole workspace, so there is nothing to compare
		// file by file and nothing was reused. Saying so is better than
		// reporting an empty diff that reads as "nothing changed".
		return manifestID, &DiffResult{Modified: []string{"(single-chunk-mode)"}}, nil
	}

	return manifestID, diff, nil
}

// Diff compares the current directory against a manifest.
func (s *Sync) Diff(_ context.Context, manifest *Manifest, srcDir string) (*DiffResult, error) {
	diff, _, err := DiffDirectory(manifest, srcDir, s.chunker)
	if err != nil {
		return nil, fmt.Errorf("failed to diff directory: %w", err)
	}
	return diff, nil
}

// stringPtr returns a pointer to a string, or nil if the string is empty.
func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Compile-time interface check.
var _ Syncer = (*Sync)(nil)
