package cas

import (
	"context"
	"crypto/rand"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewStreamingProcessor(t *testing.T) {
	chunker := NewDefaultChunker()

	t.Run("default concurrency", func(t *testing.T) {
		sp := NewStreamingProcessor(chunker, 0)
		assert.NotNil(t, sp)
		assert.Equal(t, 4, sp.maxConcurrency)
	})

	t.Run("custom concurrency", func(t *testing.T) {
		sp := NewStreamingProcessor(chunker, 8)
		assert.NotNil(t, sp)
		assert.Equal(t, 8, sp.maxConcurrency)
	})

	t.Run("negative concurrency defaults", func(t *testing.T) {
		sp := NewStreamingProcessor(chunker, -1)
		assert.NotNil(t, sp)
		assert.Equal(t, 4, sp.maxConcurrency)
	})
}

func TestStreamingProcessor_ChunkFile(t *testing.T) {
	chunker := NewDefaultChunker()
	sp := NewStreamingProcessor(chunker, 4)

	t.Run("small file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		err := os.WriteFile(filePath, []byte("hello world"), 0644)
		require.NoError(t, err)

		chunks, err := sp.ChunkFile(filePath)
		require.NoError(t, err)
		assert.Len(t, chunks, 1)
		assert.Equal(t, "hello world", string(chunks[0].Data))
	})

	t.Run("empty file", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "empty.txt")
		_, err := os.Create(filePath)
		require.NoError(t, err)

		chunks, err := sp.ChunkFile(filePath)
		require.NoError(t, err)
		assert.Empty(t, chunks)
	})

	t.Run("nonexistent file", func(t *testing.T) {
		_, err := sp.ChunkFile("/nonexistent/file.txt")
		assert.Error(t, err)
	})

	t.Run("large file with multiple chunks", func(t *testing.T) {
		if testing.Short() {
			t.Skip("skipping in short mode")
		}

		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "large.bin")

		// Create 2MB file
		data := make([]byte, 2*1024*1024)
		_, err := rand.Read(data)
		require.NoError(t, err)
		err = os.WriteFile(filePath, data, 0644)
		require.NoError(t, err)

		chunks, err := sp.ChunkFile(filePath)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(chunks), 1)

		// Verify data integrity
		var reassembled []byte
		for _, chunk := range chunks {
			reassembled = append(reassembled, chunk.Data...)
		}
		assert.Equal(t, data, reassembled)
	})
}

func TestStreamingProcessor_ChunkFileStreaming(t *testing.T) {
	chunker := NewDefaultChunker()
	sp := NewStreamingProcessor(chunker, 4)

	t.Run("successful streaming", func(t *testing.T) {
		tmpDir := t.TempDir()
		filePath := filepath.Join(tmpDir, "test.txt")
		err := os.WriteFile(filePath, []byte("streaming test content"), 0644)
		require.NoError(t, err)

		chunkCh, errCh := sp.ChunkFileStreaming(filePath)

		var chunks []ChunkInfo
		for chunk := range chunkCh {
			chunks = append(chunks, chunk)
		}

		select {
		case err := <-errCh:
			require.NoError(t, err)
		default:
		}

		assert.Len(t, chunks, 1)
	})

	t.Run("nonexistent file returns error", func(t *testing.T) {
		chunkCh, errCh := sp.ChunkFileStreaming("/nonexistent/file.txt")

		// Drain chunks channel
		for range chunkCh {
		}

		err := <-errCh
		assert.Error(t, err)
	})
}

func TestStreamingProcessor_ProcessDirectory(t *testing.T) {
	chunker := NewDefaultChunker()
	sp := NewStreamingProcessor(chunker, 4)

	t.Run("process multiple files", func(t *testing.T) {
		tmpDir := t.TempDir()
		createTestFile(t, tmpDir, "file1.txt", "content1")
		createTestFile(t, tmpDir, "file2.txt", "content2")
		createTestFile(t, tmpDir, "sub/file3.txt", "content3")

		ctx := context.Background()
		resultCh, err := sp.ProcessDirectory(ctx, tmpDir)
		require.NoError(t, err)

		var results []FileChunkResult
		for r := range resultCh {
			results = append(results, r)
		}

		assert.Len(t, results, 3)

		// Check no errors
		for _, r := range results {
			assert.NoError(t, r.Err)
			assert.NotEmpty(t, r.Path)
		}
	})

	t.Run("empty directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		ctx := context.Background()
		resultCh, err := sp.ProcessDirectory(ctx, tmpDir)
		require.NoError(t, err)

		var results []FileChunkResult
		for r := range resultCh {
			results = append(results, r)
		}

		assert.Empty(t, results)
	})

	t.Run("context cancellation", func(t *testing.T) {
		tmpDir := t.TempDir()
		for i := 0; i < 10; i++ {
			createTestFile(t, tmpDir, "file"+string(rune('0'+i))+".txt", "content")
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // Cancel immediately

		resultCh, err := sp.ProcessDirectory(ctx, tmpDir)
		require.NoError(t, err)

		// Drain results
		for range resultCh {
		}
	})

	t.Run("nonexistent directory", func(t *testing.T) {
		ctx := context.Background()
		_, err := sp.ProcessDirectory(ctx, "/nonexistent/dir")
		assert.Error(t, err)
	})
}

func TestStreamingProcessor_DiffDirectoryStreaming(t *testing.T) {
	chunker := NewDefaultChunker()
	sp := NewStreamingProcessor(chunker, 4)

	t.Run("nil manifest", func(t *testing.T) {
		tmpDir := t.TempDir()
		createTestFile(t, tmpDir, "file1.txt", "content1")
		createTestFile(t, tmpDir, "file2.txt", "content2")

		ctx := context.Background()
		result, err := sp.DiffDirectoryStreaming(ctx, nil, tmpDir)
		require.NoError(t, err)

		assert.Len(t, result.Diff.Added, 2)
		assert.Empty(t, result.Diff.Modified)
		assert.Empty(t, result.Diff.Deleted)
		assert.Empty(t, result.Diff.Unchanged)
		assert.Len(t, result.NewChunks, 2)
	})

	t.Run("unchanged files", func(t *testing.T) {
		tmpDir := t.TempDir()
		content := "unchanged content"
		createTestFile(t, tmpDir, "file.txt", content)

		// First pass to get chunks
		ctx := context.Background()
		result1, err := sp.DiffDirectoryStreaming(ctx, nil, tmpDir)
		require.NoError(t, err)

		// Build manifest from first pass
		var chunks []string
		for _, c := range result1.NewChunks["file.txt"] {
			chunks = append(chunks, c.Hash)
		}
		manifest := &Manifest{
			Files: []ManifestFile{
				{Path: "file.txt", Chunks: chunks},
			},
		}

		// Second pass should show unchanged
		result2, err := sp.DiffDirectoryStreaming(ctx, manifest, tmpDir)
		require.NoError(t, err)

		assert.Empty(t, result2.Diff.Added)
		assert.Empty(t, result2.Diff.Modified)
		assert.Empty(t, result2.Diff.Deleted)
		assert.Len(t, result2.Diff.Unchanged, 1)
		assert.Empty(t, result2.NewChunks)
	})

	t.Run("deleted files", func(t *testing.T) {
		tmpDir := t.TempDir()

		manifest := &Manifest{
			Files: []ManifestFile{
				{Path: "deleted.txt", Chunks: []string{"hash1"}},
			},
		}

		ctx := context.Background()
		result, err := sp.DiffDirectoryStreaming(ctx, manifest, tmpDir)
		require.NoError(t, err)

		assert.Empty(t, result.Diff.Added)
		assert.Empty(t, result.Diff.Modified)
		assert.Len(t, result.Diff.Deleted, 1)
		assert.Contains(t, result.Diff.Deleted, "deleted.txt")
	})

	t.Run("modified files", func(t *testing.T) {
		tmpDir := t.TempDir()
		createTestFile(t, tmpDir, "file.txt", "new content")

		manifest := &Manifest{
			Files: []ManifestFile{
				{Path: "file.txt", Chunks: []string{"old-hash"}},
			},
		}

		ctx := context.Background()
		result, err := sp.DiffDirectoryStreaming(ctx, manifest, tmpDir)
		require.NoError(t, err)

		assert.Empty(t, result.Diff.Added)
		assert.Len(t, result.Diff.Modified, 1)
		assert.Contains(t, result.Diff.Modified, "file.txt")
		assert.Empty(t, result.Diff.Deleted)
		assert.Len(t, result.NewChunks, 1)
	})
}

func TestNewChunkUploader(t *testing.T) {
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)

	t.Run("default concurrency", func(t *testing.T) {
		u := NewChunkUploader(chunkStore, "tenant-1", 0)
		assert.NotNil(t, u)
		assert.Equal(t, 4, u.maxConcurrency)
	})

	t.Run("custom concurrency", func(t *testing.T) {
		u := NewChunkUploader(chunkStore, "tenant-1", 8)
		assert.NotNil(t, u)
		assert.Equal(t, 8, u.maxConcurrency)
	})
}

func TestChunkUploader_UploadChunksStreaming(t *testing.T) {
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	uploader := NewChunkUploader(chunkStore, "tenant-1", 4)
	ctx := context.Background()

	t.Run("upload new chunks", func(t *testing.T) {
		chunks := make(chan ChunkInfo, 3)
		chunks <- ChunkInfo{Hash: "hash1", Data: []byte("data1")}
		chunks <- ChunkInfo{Hash: "hash2", Data: []byte("data2")}
		chunks <- ChunkInfo{Hash: "hash3", Data: []byte("data3")}
		close(chunks)

		progress := uploader.UploadChunksStreaming(ctx, chunks)

		var uploaded, skipped int
		for p := range progress {
			assert.NoError(t, p.Err)
			if p.Uploaded {
				uploaded++
			}
			if p.Skipped {
				skipped++
			}
		}

		assert.Equal(t, 3, uploaded)
		assert.Equal(t, 0, skipped)

		// Verify chunks exist
		exists, err := chunkStore.ChunkExists(ctx, "tenant-1", "hash1")
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("skip existing chunks", func(t *testing.T) {
		// Pre-store a chunk
		_, err := chunkStore.StoreChunk(ctx, "tenant-1", "existing", []byte("existing"))
		require.NoError(t, err)

		chunks := make(chan ChunkInfo, 2)
		chunks <- ChunkInfo{Hash: "existing", Data: []byte("existing")}
		chunks <- ChunkInfo{Hash: "new-hash", Data: []byte("new")}
		close(chunks)

		progress := uploader.UploadChunksStreaming(ctx, chunks)

		var uploaded, skipped int
		for p := range progress {
			assert.NoError(t, p.Err)
			if p.Uploaded {
				uploaded++
			}
			if p.Skipped {
				skipped++
			}
		}

		assert.Equal(t, 1, uploaded)
		assert.Equal(t, 1, skipped)
	})

	t.Run("dedupe within batch", func(t *testing.T) {
		chunks := make(chan ChunkInfo, 3)
		chunks <- ChunkInfo{Hash: "dup-hash", Data: []byte("data")}
		chunks <- ChunkInfo{Hash: "dup-hash", Data: []byte("data")} // Duplicate
		chunks <- ChunkInfo{Hash: "dup-hash", Data: []byte("data")} // Duplicate
		close(chunks)

		progress := uploader.UploadChunksStreaming(ctx, chunks)

		var total int
		for range progress {
			total++
		}

		// Should only process once
		assert.Equal(t, 1, total)
	})

	t.Run("context cancellation", func(_ *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		chunks := make(chan ChunkInfo, 1)
		chunks <- ChunkInfo{Hash: "cancel-test", Data: []byte("data")}
		close(chunks)

		progress := uploader.UploadChunksStreaming(ctx, chunks)

		// Drain
		for range progress {
		}
	})
}

func TestNewStreamingFileReader(t *testing.T) {
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)

	reader := NewStreamingFileReader(chunkStore, "tenant-1")
	assert.NotNil(t, reader)
	assert.Equal(t, "tenant-1", reader.tenantID)
}

func TestStreamingFileReader_RestoreFile(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)

	// Store some chunks
	data1 := []byte("chunk1 data")
	data2 := []byte("chunk2 data")
	hash1 := HashData(data1)
	hash2 := HashData(data2)

	_, err := chunkStore.StoreChunk(ctx, "tenant-1", hash1, data1)
	require.NoError(t, err)
	_, err = chunkStore.StoreChunk(ctx, "tenant-1", hash2, data2)
	require.NoError(t, err)

	reader := NewStreamingFileReader(chunkStore, "tenant-1")

	t.Run("restore single chunk file", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstPath := filepath.Join(tmpDir, "restored.txt")

		err := reader.RestoreFile(ctx, dstPath, []string{hash1}, 0644)
		require.NoError(t, err)

		content, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.Equal(t, data1, content)
	})

	t.Run("restore multi-chunk file", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstPath := filepath.Join(tmpDir, "multi.txt")

		err := reader.RestoreFile(ctx, dstPath, []string{hash1, hash2}, 0644)
		require.NoError(t, err)

		content, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		expected := append([]byte{}, data1...)
		expected = append(expected, data2...)
		assert.Equal(t, expected, content)
	})

	t.Run("restore with nested directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstPath := filepath.Join(tmpDir, "deep", "nested", "file.txt")

		err := reader.RestoreFile(ctx, dstPath, []string{hash1}, 0644)
		require.NoError(t, err)

		content, err := os.ReadFile(dstPath)
		require.NoError(t, err)
		assert.Equal(t, data1, content)
	})

	t.Run("restore empty file", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstPath := filepath.Join(tmpDir, "empty.txt")

		err := reader.RestoreFile(ctx, dstPath, []string{}, 0644)
		require.NoError(t, err)

		info, err := os.Stat(dstPath)
		require.NoError(t, err)
		assert.Equal(t, int64(0), info.Size())
	})

	t.Run("missing chunk", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstPath := filepath.Join(tmpDir, "missing.txt")

		err := reader.RestoreFile(ctx, dstPath, []string{"nonexistent-hash"}, 0644)
		assert.Error(t, err)
	})

	t.Run("file permissions", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstPath := filepath.Join(tmpDir, "exec.sh")

		err := reader.RestoreFile(ctx, dstPath, []string{hash1}, 0755)
		require.NoError(t, err)

		info, err := os.Stat(dstPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0755), info.Mode().Perm())
	})
}

func TestStreamingFileReader_RestoreFileWithProgress(t *testing.T) {
	ctx := context.Background()
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)

	// Store some chunks
	data1 := []byte("chunk1 data")
	data2 := []byte("chunk2 data")
	hash1 := HashData(data1)
	hash2 := HashData(data2)

	_, err := chunkStore.StoreChunk(ctx, "tenant-1", hash1, data1)
	require.NoError(t, err)
	_, err = chunkStore.StoreChunk(ctx, "tenant-1", hash2, data2)
	require.NoError(t, err)

	reader := NewStreamingFileReader(chunkStore, "tenant-1")

	t.Run("track progress", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstPath := filepath.Join(tmpDir, "progress.txt")

		progress := make(chan int64, 10)
		var wg sync.WaitGroup
		var totalBytes int64

		wg.Add(1)
		go func() {
			defer wg.Done()
			for bytes := range progress {
				totalBytes += bytes
			}
		}()

		err := reader.RestoreFileWithProgress(ctx, dstPath, []string{hash1, hash2}, 0644, progress)
		close(progress)
		wg.Wait()

		require.NoError(t, err)
		assert.Equal(t, int64(len(data1)+len(data2)), totalBytes)
	})

	t.Run("nil progress channel", func(t *testing.T) {
		tmpDir := t.TempDir()
		dstPath := filepath.Join(tmpDir, "noprogress.txt")

		err := reader.RestoreFileWithProgress(ctx, dstPath, []string{hash1}, 0644, nil)
		require.NoError(t, err)
	})

	t.Run("context cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		tmpDir := t.TempDir()
		dstPath := filepath.Join(tmpDir, "cancelled.txt")

		err := reader.RestoreFileWithProgress(ctx, dstPath, []string{hash1}, 0644, nil)
		assert.Error(t, err)
	})
}

// Benchmark tests
func BenchmarkStreamingProcessor_ProcessDirectory(b *testing.B) {
	tmpDir := b.TempDir()

	// Create 100 files
	for i := 0; i < 100; i++ {
		path := filepath.Join(tmpDir, "file"+string(rune('0'+i/10))+string(rune('0'+i%10))+".txt")
		_ = os.WriteFile(path, []byte("benchmark content"), 0644)
	}

	chunker := NewDefaultChunker()
	sp := NewStreamingProcessor(chunker, 8)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		resultCh, _ := sp.ProcessDirectory(ctx, tmpDir)
		for range resultCh {
		}
	}
}

func BenchmarkChunkUploader_UploadChunksStreaming(b *testing.B) {
	memStorage := NewMemoryProvider()
	encryptor := NewNoOpEncryptor()
	chunkStore := NewBlobChunkStore(memStorage, encryptor)
	uploader := NewChunkUploader(chunkStore, "tenant-1", 8)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		chunks := make(chan ChunkInfo, 100)
		for j := 0; j < 100; j++ {
			chunks <- ChunkInfo{
				Hash: HashData([]byte("data" + string(rune('0'+j)))),
				Data: []byte("data" + string(rune('0'+j))),
			}
		}
		close(chunks)

		progress := uploader.UploadChunksStreaming(ctx, chunks)
		for range progress {
		}
	}
}
