package cas

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sync/errgroup"
)

// StreamingProcessor provides memory-efficient file processing for large workspaces.
// It processes files using streaming I/O rather than loading entire files into memory.
type StreamingProcessor struct {
	chunker        *Chunker
	maxConcurrency int
}

// NewStreamingProcessor creates a new streaming processor.
func NewStreamingProcessor(chunker *Chunker, maxConcurrency int) *StreamingProcessor {
	if maxConcurrency <= 0 {
		maxConcurrency = 4
	}
	return &StreamingProcessor{
		chunker:        chunker,
		maxConcurrency: maxConcurrency,
	}
}

// FileChunkResult contains the result of chunking a single file.
type FileChunkResult struct {
	Path   string
	Chunks []ChunkInfo
	Size   int64
	Err    error
}

// ChunkFile processes a single file using streaming and returns its chunks.
// This is memory-efficient as it doesn't load the entire file at once.
func (sp *StreamingProcessor) ChunkFile(path string) ([]ChunkInfo, error) {
	f, err := os.Open(path) //nolint:gosec // path is validated by caller
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() { _ = f.Close() }()

	chunkCh, errCh := sp.chunker.ChunkReader(f)

	var chunks []ChunkInfo
	for chunk := range chunkCh {
		chunks = append(chunks, chunk)
	}

	// Check for errors
	select {
	case err := <-errCh:
		if err != nil {
			return nil, fmt.Errorf("failed to chunk file: %w", err)
		}
	default:
	}

	return chunks, nil
}

// ChunkFileStreaming processes a file and sends chunks to a channel.
// This allows the caller to process chunks as they arrive.
func (sp *StreamingProcessor) ChunkFileStreaming(path string) (<-chan ChunkInfo, <-chan error) {
	f, err := os.Open(path) //nolint:gosec // path is validated by caller
	if err != nil {
		errCh := make(chan error, 1)
		errCh <- fmt.Errorf("failed to open file: %w", err)
		close(errCh)

		chunkCh := make(chan ChunkInfo)
		close(chunkCh)
		return chunkCh, errCh
	}

	// Wrap to ensure file gets closed
	return sp.chunkReaderWithClose(f)
}

// chunkReaderWithClose wraps the chunker's ChunkReader and ensures the file is closed.
func (sp *StreamingProcessor) chunkReaderWithClose(f *os.File) (<-chan ChunkInfo, <-chan error) {
	chunkCh, errCh := sp.chunker.ChunkReader(f)

	// Create wrapper channels
	outChunks := make(chan ChunkInfo, 10)
	outErrs := make(chan error, 1)

	go func() {
		defer close(outChunks)
		defer close(outErrs)
		defer func() { _ = f.Close() }()

		for chunk := range chunkCh {
			outChunks <- chunk
		}

		select {
		case err := <-errCh:
			if err != nil {
				outErrs <- err
			}
		default:
		}
	}()

	return outChunks, outErrs
}

// ProcessDirectory walks a directory and processes files using streaming.
// It returns results via a channel for memory efficiency.
func (sp *StreamingProcessor) ProcessDirectory(ctx context.Context, srcDir string) (<-chan FileChunkResult, error) {
	// First collect all file paths
	var paths []string
	err := filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory: %w", err)
	}

	results := make(chan FileChunkResult, sp.maxConcurrency)

	go func() {
		defer close(results)

		g, ctx := errgroup.WithContext(ctx)
		g.SetLimit(sp.maxConcurrency)

		// Use mutex to protect channel writes since errgroup runs concurrently
		var mu sync.Mutex

		for _, path := range paths {
			path := path // capture loop variable

			g.Go(func() error {
				// Check for context cancellation
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				relPath, relErr := filepath.Rel(srcDir, path)
				if relErr != nil {
					mu.Lock()
					results <- FileChunkResult{Path: path, Err: relErr}
					mu.Unlock()
					return nil //nolint:nilerr // Intentional: collect errors in results channel
				}

				info, statErr := os.Stat(path)
				if statErr != nil {
					mu.Lock()
					results <- FileChunkResult{Path: relPath, Err: statErr}
					mu.Unlock()
					return nil //nolint:nilerr // Intentional: collect errors in results channel
				}

				chunks, err := sp.ChunkFile(path)

				mu.Lock()
				results <- FileChunkResult{
					Path:   relPath,
					Chunks: chunks,
					Size:   info.Size(),
					Err:    err,
				}
				mu.Unlock()

				return nil
			})
		}

		// Wait for all goroutines to complete
		_ = g.Wait()
	}()

	return results, nil
}

// StreamingDiffResult contains the diff result and new chunks for streaming processing.
type StreamingDiffResult struct {
	Diff      *DiffResult
	NewChunks map[string][]ChunkInfo
}

// DiffDirectoryStreaming compares a manifest against a directory using streaming.
// This is more memory-efficient than DiffDirectory for large workspaces.
func (sp *StreamingProcessor) DiffDirectoryStreaming(ctx context.Context, manifest *Manifest, srcDir string) (*StreamingDiffResult, error) {
	result := &DiffResult{
		Added:     make([]string, 0),
		Modified:  make([]string, 0),
		Deleted:   make([]string, 0),
		Unchanged: make([]string, 0),
	}

	newChunks := make(map[string][]ChunkInfo)
	var mu sync.Mutex

	// Build map of manifest files
	manifestFiles := make(map[string]*ManifestFile)
	if manifest != nil {
		for i := range manifest.Files {
			manifestFiles[manifest.Files[i].Path] = &manifest.Files[i]
		}
	}

	seenFiles := make(map[string]bool)

	// Process directory using streaming
	resultCh, err := sp.ProcessDirectory(ctx, srcDir)
	if err != nil {
		return nil, err
	}

	// Collect results
	for res := range resultCh {
		if res.Err != nil {
			return nil, fmt.Errorf("failed to process file %s: %w", res.Path, res.Err)
		}

		mu.Lock()
		seenFiles[res.Path] = true

		// Build hash list for comparison
		newHashes := make([]string, len(res.Chunks))
		for i, c := range res.Chunks {
			newHashes[i] = c.Hash
		}

		// Check against manifest
		manifestFile, exists := manifestFiles[res.Path]
		switch {
		case !exists:
			result.Added = append(result.Added, res.Path)
			newChunks[res.Path] = res.Chunks
		case !chunksEqual(manifestFile.Chunks, newHashes):
			result.Modified = append(result.Modified, res.Path)
			newChunks[res.Path] = res.Chunks
		default:
			result.Unchanged = append(result.Unchanged, res.Path)
		}
		mu.Unlock()
	}

	// Find deleted files
	for path := range manifestFiles {
		if !seenFiles[path] {
			result.Deleted = append(result.Deleted, path)
		}
	}

	return &StreamingDiffResult{
		Diff:      result,
		NewChunks: newChunks,
	}, nil
}

// ChunkUploader provides streaming upload of chunks.
type ChunkUploader struct {
	chunkStore     ChunkStore
	tenantID       string
	maxConcurrency int
}

// NewChunkUploader creates a new chunk uploader.
func NewChunkUploader(chunkStore ChunkStore, tenantID string, maxConcurrency int) *ChunkUploader {
	if maxConcurrency <= 0 {
		maxConcurrency = 4
	}
	return &ChunkUploader{
		chunkStore:     chunkStore,
		tenantID:       tenantID,
		maxConcurrency: maxConcurrency,
	}
}

// UploadProgress contains progress information for chunk uploads.
type UploadProgress struct {
	Hash      string
	Uploaded  bool
	Skipped   bool
	BytesSize int64
	Err       error
}

// UploadChunksStreaming uploads chunks as they arrive via channel.
// Returns a progress channel for monitoring upload status.
func (u *ChunkUploader) UploadChunksStreaming(ctx context.Context, chunks <-chan ChunkInfo) <-chan UploadProgress {
	progress := make(chan UploadProgress, u.maxConcurrency)

	go func() {
		defer close(progress)

		g, ctx := errgroup.WithContext(ctx)
		g.SetLimit(u.maxConcurrency)

		// Track seen hashes for dedup within this batch
		seen := make(map[string]bool)
		var seenMu sync.Mutex

		for chunk := range chunks {
			chunk := chunk

			// Skip duplicates within this batch
			seenMu.Lock()
			if seen[chunk.Hash] {
				seenMu.Unlock()
				continue
			}
			seen[chunk.Hash] = true
			seenMu.Unlock()

			g.Go(func() error {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}

				// Check if chunk already exists
				exists, existsErr := u.chunkStore.ChunkExists(ctx, u.tenantID, chunk.Hash)
				if existsErr != nil {
					progress <- UploadProgress{Hash: chunk.Hash, Err: existsErr}
					return nil //nolint:nilerr // Intentional: report error via progress channel
				}

				if exists {
					progress <- UploadProgress{Hash: chunk.Hash, Skipped: true}
					return nil
				}

				// Upload chunk
				size, storeErr := u.chunkStore.StoreChunk(ctx, u.tenantID, chunk.Hash, chunk.Data)
				if storeErr != nil {
					progress <- UploadProgress{Hash: chunk.Hash, Err: storeErr}
					return nil //nolint:nilerr // Intentional: report error via progress channel
				}

				progress <- UploadProgress{Hash: chunk.Hash, Uploaded: true, BytesSize: size}
				return nil
			})
		}

		_ = g.Wait()
	}()

	return progress
}

// StreamingFileReader provides streaming read of a file during restore.
type StreamingFileReader struct {
	chunkStore ChunkStore
	tenantID   string
}

// NewStreamingFileReader creates a new streaming file reader.
func NewStreamingFileReader(chunkStore ChunkStore, tenantID string) *StreamingFileReader {
	return &StreamingFileReader{
		chunkStore: chunkStore,
		tenantID:   tenantID,
	}
}

// RestoreFile restores a file by streaming chunks directly to disk.
// This is memory-efficient as it doesn't hold all chunks in memory.
func (r *StreamingFileReader) RestoreFile(ctx context.Context, dstPath string, chunks []string, mode os.FileMode) error {
	// Create parent directory
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	// Create file
	f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = f.Close() }()

	// Write chunks sequentially
	for _, hash := range chunks {
		data, err := r.chunkStore.GetChunk(ctx, r.tenantID, hash)
		if err != nil {
			return fmt.Errorf("failed to get chunk %s: %w", hash, err)
		}

		if _, err := f.Write(data); err != nil {
			return fmt.Errorf("failed to write chunk: %w", err)
		}
	}

	return nil
}

// RestoreFileWithProgress restores a file and reports progress via channel.
func (r *StreamingFileReader) RestoreFileWithProgress(ctx context.Context, dstPath string, chunks []string, mode os.FileMode, progress chan<- int64) error {
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		return fmt.Errorf("failed to create parent directory: %w", err)
	}

	f, err := os.OpenFile(dstPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer func() { _ = f.Close() }()

	for _, hash := range chunks {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		data, err := r.chunkStore.GetChunk(ctx, r.tenantID, hash)
		if err != nil {
			return fmt.Errorf("failed to get chunk %s: %w", hash, err)
		}

		n, err := f.Write(data)
		if err != nil {
			return fmt.Errorf("failed to write chunk: %w", err)
		}

		if progress != nil {
			progress <- int64(n)
		}
	}

	return nil
}
