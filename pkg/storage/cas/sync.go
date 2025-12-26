package cas

import (
	"archive/tar"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
// For small workspaces (<SingleChunkThreshold), uses single-chunk mode (tar.zst).
// For large workspaces, uses CDC with parallel chunk uploads.
func (s *Sync) Sync(ctx context.Context, workspaceID, tenantID, srcDir string) (string, error) {
	// Calculate total size to determine mode
	totalSize, err := calculateDirSize(srcDir)
	if err != nil {
		return "", fmt.Errorf("failed to calculate directory size: %w", err)
	}

	manifest := &Manifest{
		ID:          id.Manifest(),
		WorkspaceID: workspaceID,
		TenantID:    tenantID,
		CreatedAt:   time.Now(),
		TotalSize:   totalSize,
	}

	if totalSize < s.config.SingleChunkThreshold {
		// Single chunk mode
		if err := s.syncSingleChunk(ctx, manifest, srcDir); err != nil {
			return "", err
		}
	} else {
		// CDC mode
		if err := s.syncCDC(ctx, manifest, srcDir); err != nil {
			return "", err
		}
	}

	// Save manifest
	if err := s.manifestStore.SaveManifest(ctx, manifest); err != nil {
		return "", fmt.Errorf("failed to save manifest: %w", err)
	}

	return manifest.ID, nil
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

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
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

// syncCDC performs content-defined chunking and parallel upload.
func (s *Sync) syncCDC(ctx context.Context, manifest *Manifest, srcDir string) error {
	var files []ManifestFile
	var mu sync.Mutex

	// Track unique chunks to upload
	chunksToUpload := make(map[string][]byte)
	var chunkMu sync.Mutex

	// Walk directory and chunk each file
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Get relative path
		relPath, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}

		// Read file
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("failed to read file %s: %w", path, err)
		}

		// Chunk the file
		chunks, err := s.chunker.ChunkData(data)
		if err != nil {
			return fmt.Errorf("failed to chunk file %s: %w", path, err)
		}

		// Build file entry
		mf := ManifestFile{
			Path:    relPath,
			Mode:    info.Mode(),
			ModTime: info.ModTime(),
			Size:    info.Size(),
			Chunks:  make([]string, 0, len(chunks)),
		}

		for _, chunk := range chunks {
			mf.Chunks = append(mf.Chunks, chunk.Hash)

			// Track unique chunks
			chunkMu.Lock()
			if _, exists := chunksToUpload[chunk.Hash]; !exists {
				chunksToUpload[chunk.Hash] = chunk.Data
			}
			chunkMu.Unlock()
		}

		mu.Lock()
		files = append(files, mf)
		mu.Unlock()

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to walk directory: %w", err)
	}

	// Parallel upload of unique chunks
	if err := s.uploadChunks(ctx, manifest.TenantID, chunksToUpload); err != nil {
		return err
	}

	manifest.Files = files
	manifest.ChunkCount = len(chunksToUpload)

	return nil
}

// uploadChunks uploads chunks with bounded concurrency.
func (s *Sync) uploadChunks(ctx context.Context, tenantID string, chunks map[string][]byte) error {
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(s.config.MaxConcurrency)

	for hash, data := range chunks {
		hash := hash
		data := data

		g.Go(func() error {
			// Check if chunk already exists (dedup)
			exists, err := s.chunkStore.ChunkExists(ctx, tenantID, hash)
			if err != nil {
				return fmt.Errorf("failed to check chunk %s: %w", hash, err)
			}
			if exists {
				return nil // Skip existing chunk
			}

			// Upload chunk
			if _, err := s.chunkStore.StoreChunk(ctx, tenantID, hash, data); err != nil {
				return fmt.Errorf("failed to upload chunk %s: %w", hash, err)
			}
			return nil
		})
	}

	return g.Wait()
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
	// Load manifest using streaming API
	fileCh, header, err := s.manifestStore.StreamManifestFiles(ctx, tenantID, workspaceID, manifestID)
	if err != nil {
		return fmt.Errorf("failed to load manifest: %w", err)
	}

	// Create destination directory
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return fmt.Errorf("failed to create destination directory: %w", err)
	}

	if header.SingleChunk {
		// Single chunk mode: extract tar.zst
		return s.restoreSingleChunk(ctx, header.ChunkHash, tenantID, dstDir)
	}

	// CDC mode: restore files from chunks
	return s.restoreCDC(ctx, tenantID, dstDir, fileCh)
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
			if err := os.MkdirAll(target, os.FileMode(header.Mode)); err != nil {
				return fmt.Errorf("failed to create directory: %w", err)
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

// restoreCDC restores files from chunks using CDC mode.
func (s *Sync) restoreCDC(ctx context.Context, tenantID, dstDir string, fileCh <-chan ManifestFile) error {
	// Cache chunks to avoid re-downloading
	chunkCache := make(map[string][]byte)
	var cacheMu sync.Mutex

	for mf := range fileCh {
		target := filepath.Join(dstDir, mf.Path)

		// Security: prevent path traversal
		if !isValidRestorePath(dstDir, target) {
			return &PathTraversalError{Path: mf.Path}
		}

		// Create parent directory
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return fmt.Errorf("failed to create parent directory: %w", err)
		}

		// Create file
		f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mf.Mode)
		if err != nil {
			return fmt.Errorf("failed to create file %s: %w", mf.Path, err)
		}

		// Write chunks
		for _, hash := range mf.Chunks {
			// Check cache first
			cacheMu.Lock()
			data, ok := chunkCache[hash]
			cacheMu.Unlock()

			if !ok {
				// Download chunk
				var err error
				data, err = s.chunkStore.GetChunk(ctx, tenantID, hash)
				if err != nil {
					_ = f.Close()
					return fmt.Errorf("failed to get chunk %s for file %s: %w", hash, mf.Path, err)
				}

				// Cache for potential reuse
				cacheMu.Lock()
				chunkCache[hash] = data
				cacheMu.Unlock()
			}

			if _, err := f.Write(data); err != nil {
				_ = f.Close()
				return fmt.Errorf("failed to write chunk to file %s: %w", mf.Path, err)
			}
		}

		_ = f.Close()

		// Restore modification time and permissions (non-fatal if fails)
		_ = os.Chtimes(target, mf.ModTime, mf.ModTime)
		_ = os.Chmod(target, mf.Mode)
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

// isValidRestorePath checks that the target path doesn't escape the base directory.
func isValidRestorePath(baseDir, target string) bool {
	cleanTarget := filepath.Clean(target)
	cleanBase := filepath.Clean(baseDir)
	return len(cleanTarget) >= len(cleanBase) && cleanTarget[:len(cleanBase)] == cleanBase
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

// Compile-time interface check.
var _ Syncer = (*Sync)(nil)
