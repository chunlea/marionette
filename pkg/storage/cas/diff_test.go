package cas

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffResult_IsEmpty(t *testing.T) {
	tests := []struct {
		name     string
		diff     *DiffResult
		expected bool
	}{
		{
			name: "empty diff",
			diff: &DiffResult{
				Added:     []string{},
				Modified:  []string{},
				Deleted:   []string{},
				Unchanged: []string{"file1.txt"},
			},
			expected: true,
		},
		{
			name: "has added files",
			diff: &DiffResult{
				Added:     []string{"new.txt"},
				Modified:  []string{},
				Deleted:   []string{},
				Unchanged: []string{},
			},
			expected: false,
		},
		{
			name: "has modified files",
			diff: &DiffResult{
				Added:     []string{},
				Modified:  []string{"changed.txt"},
				Deleted:   []string{},
				Unchanged: []string{},
			},
			expected: false,
		},
		{
			name: "has deleted files",
			diff: &DiffResult{
				Added:     []string{},
				Modified:  []string{},
				Deleted:   []string{"removed.txt"},
				Unchanged: []string{},
			},
			expected: false,
		},
		{
			name: "has all types of changes",
			diff: &DiffResult{
				Added:     []string{"new.txt"},
				Modified:  []string{"changed.txt"},
				Deleted:   []string{"removed.txt"},
				Unchanged: []string{"same.txt"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.diff.IsEmpty()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDiffResult_TotalChanges(t *testing.T) {
	tests := []struct {
		name     string
		diff     *DiffResult
		expected int
	}{
		{
			name: "no changes",
			diff: &DiffResult{
				Added:     []string{},
				Modified:  []string{},
				Deleted:   []string{},
				Unchanged: []string{"file1.txt", "file2.txt"},
			},
			expected: 0,
		},
		{
			name: "only added",
			diff: &DiffResult{
				Added:     []string{"a.txt", "b.txt", "c.txt"},
				Modified:  []string{},
				Deleted:   []string{},
				Unchanged: []string{},
			},
			expected: 3,
		},
		{
			name: "mixed changes",
			diff: &DiffResult{
				Added:     []string{"new.txt"},
				Modified:  []string{"changed1.txt", "changed2.txt"},
				Deleted:   []string{"removed.txt"},
				Unchanged: []string{"same.txt"},
			},
			expected: 4,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.diff.TotalChanges()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDiffManifests_NilOldManifest(t *testing.T) {
	newManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "file1.txt", Chunks: []string{"hash1"}},
			{Path: "file2.txt", Chunks: []string{"hash2"}},
		},
	}

	result := DiffManifests(nil, newManifest)

	assert.Len(t, result.Added, 2)
	assert.Contains(t, result.Added, "file1.txt")
	assert.Contains(t, result.Added, "file2.txt")
	assert.Empty(t, result.Modified)
	assert.Empty(t, result.Deleted)
	assert.Empty(t, result.Unchanged)
}

func TestDiffManifests_SameManifests(t *testing.T) {
	files := []ManifestFile{
		{Path: "file1.txt", Chunks: []string{"hash1", "hash2"}},
		{Path: "file2.txt", Chunks: []string{"hash3"}},
	}

	oldManifest := &Manifest{Files: files}
	newManifest := &Manifest{Files: files}

	result := DiffManifests(oldManifest, newManifest)

	assert.Empty(t, result.Added)
	assert.Empty(t, result.Modified)
	assert.Empty(t, result.Deleted)
	assert.Len(t, result.Unchanged, 2)
}

func TestDiffManifests_AddedFiles(t *testing.T) {
	oldManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "existing.txt", Chunks: []string{"hash1"}},
		},
	}
	newManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "existing.txt", Chunks: []string{"hash1"}},
			{Path: "new1.txt", Chunks: []string{"hash2"}},
			{Path: "new2.txt", Chunks: []string{"hash3"}},
		},
	}

	result := DiffManifests(oldManifest, newManifest)

	assert.Len(t, result.Added, 2)
	assert.Contains(t, result.Added, "new1.txt")
	assert.Contains(t, result.Added, "new2.txt")
	assert.Empty(t, result.Modified)
	assert.Empty(t, result.Deleted)
	assert.Len(t, result.Unchanged, 1)
}

func TestDiffManifests_DeletedFiles(t *testing.T) {
	oldManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "kept.txt", Chunks: []string{"hash1"}},
			{Path: "deleted1.txt", Chunks: []string{"hash2"}},
			{Path: "deleted2.txt", Chunks: []string{"hash3"}},
		},
	}
	newManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "kept.txt", Chunks: []string{"hash1"}},
		},
	}

	result := DiffManifests(oldManifest, newManifest)

	assert.Empty(t, result.Added)
	assert.Empty(t, result.Modified)
	assert.Len(t, result.Deleted, 2)
	assert.Contains(t, result.Deleted, "deleted1.txt")
	assert.Contains(t, result.Deleted, "deleted2.txt")
	assert.Len(t, result.Unchanged, 1)
}

func TestDiffManifests_ModifiedFiles(t *testing.T) {
	oldManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "unchanged.txt", Chunks: []string{"hash1"}},
			{Path: "modified.txt", Chunks: []string{"old-hash"}},
		},
	}
	newManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "unchanged.txt", Chunks: []string{"hash1"}},
			{Path: "modified.txt", Chunks: []string{"new-hash"}},
		},
	}

	result := DiffManifests(oldManifest, newManifest)

	assert.Empty(t, result.Added)
	assert.Len(t, result.Modified, 1)
	assert.Contains(t, result.Modified, "modified.txt")
	assert.Empty(t, result.Deleted)
	assert.Len(t, result.Unchanged, 1)
}

func TestDiffManifests_MultipleChunkChanges(t *testing.T) {
	oldManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "file.txt", Chunks: []string{"hash1", "hash2", "hash3"}},
		},
	}
	newManifest := &Manifest{
		Files: []ManifestFile{
			// Same number of chunks, but one changed
			{Path: "file.txt", Chunks: []string{"hash1", "new-hash", "hash3"}},
		},
	}

	result := DiffManifests(oldManifest, newManifest)

	assert.Len(t, result.Modified, 1)
	assert.Contains(t, result.Modified, "file.txt")
}

func TestDiffManifests_DifferentChunkCount(t *testing.T) {
	oldManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "file.txt", Chunks: []string{"hash1", "hash2"}},
		},
	}
	newManifest := &Manifest{
		Files: []ManifestFile{
			// More chunks (file grew)
			{Path: "file.txt", Chunks: []string{"hash1", "hash2", "hash3"}},
		},
	}

	result := DiffManifests(oldManifest, newManifest)

	assert.Len(t, result.Modified, 1)
	assert.Contains(t, result.Modified, "file.txt")
}

func TestDiffManifests_ComplexChanges(t *testing.T) {
	oldManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "unchanged.txt", Chunks: []string{"hash1"}},
			{Path: "modified.txt", Chunks: []string{"old-hash"}},
			{Path: "deleted.txt", Chunks: []string{"hash2"}},
		},
	}
	newManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "unchanged.txt", Chunks: []string{"hash1"}},
			{Path: "modified.txt", Chunks: []string{"new-hash"}},
			{Path: "added.txt", Chunks: []string{"hash3"}},
		},
	}

	result := DiffManifests(oldManifest, newManifest)

	assert.Len(t, result.Added, 1)
	assert.Contains(t, result.Added, "added.txt")
	assert.Len(t, result.Modified, 1)
	assert.Contains(t, result.Modified, "modified.txt")
	assert.Len(t, result.Deleted, 1)
	assert.Contains(t, result.Deleted, "deleted.txt")
	assert.Len(t, result.Unchanged, 1)
	assert.Contains(t, result.Unchanged, "unchanged.txt")
}

func TestChunksEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "both empty",
			a:        []string{},
			b:        []string{},
			expected: true,
		},
		{
			name:     "both nil",
			a:        nil,
			b:        nil,
			expected: true,
		},
		{
			name:     "equal single element",
			a:        []string{"hash1"},
			b:        []string{"hash1"},
			expected: true,
		},
		{
			name:     "equal multiple elements",
			a:        []string{"hash1", "hash2", "hash3"},
			b:        []string{"hash1", "hash2", "hash3"},
			expected: true,
		},
		{
			name:     "different lengths",
			a:        []string{"hash1", "hash2"},
			b:        []string{"hash1"},
			expected: false,
		},
		{
			name:     "same length different content",
			a:        []string{"hash1", "hash2"},
			b:        []string{"hash1", "hash3"},
			expected: false,
		},
		{
			name:     "same elements different order",
			a:        []string{"hash1", "hash2"},
			b:        []string{"hash2", "hash1"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := chunksEqual(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDiffDirectory_NilManifest(t *testing.T) {
	srcDir := t.TempDir()
	createTestFile(t, srcDir, "file1.txt", "content1")
	createTestFile(t, srcDir, "file2.txt", "content2")

	chunker := NewDefaultChunker()
	result, newChunks, err := DiffDirectory(nil, srcDir, chunker)
	require.NoError(t, err)

	// All files should be added
	assert.Len(t, result.Added, 2)
	assert.Contains(t, result.Added, "file1.txt")
	assert.Contains(t, result.Added, "file2.txt")
	assert.Empty(t, result.Modified)
	assert.Empty(t, result.Deleted)
	assert.Empty(t, result.Unchanged)

	// Should have chunks for both files
	assert.Len(t, newChunks, 2)
	assert.Contains(t, newChunks, "file1.txt")
	assert.Contains(t, newChunks, "file2.txt")
}

func TestDiffDirectory_EmptyDirectory(t *testing.T) {
	srcDir := t.TempDir()

	manifest := &Manifest{
		Files: []ManifestFile{
			{Path: "deleted.txt", Chunks: []string{"hash1"}},
		},
	}

	chunker := NewDefaultChunker()
	result, newChunks, err := DiffDirectory(manifest, srcDir, chunker)
	require.NoError(t, err)

	assert.Empty(t, result.Added)
	assert.Empty(t, result.Modified)
	assert.Len(t, result.Deleted, 1)
	assert.Contains(t, result.Deleted, "deleted.txt")
	assert.Empty(t, result.Unchanged)
	assert.Empty(t, newChunks)
}

func TestDiffDirectory_UnchangedFiles(t *testing.T) {
	srcDir := t.TempDir()
	content := "unchanged content"
	createTestFile(t, srcDir, "file.txt", content)

	chunker := NewDefaultChunker()

	// First, chunk the file to get the hash
	data, _ := os.ReadFile(filepath.Join(srcDir, "file.txt"))
	chunks, _ := chunker.ChunkData(data)
	hashes := make([]string, len(chunks))
	for i, c := range chunks {
		hashes[i] = c.Hash
	}

	manifest := &Manifest{
		Files: []ManifestFile{
			{Path: "file.txt", Chunks: hashes},
		},
	}

	result, newChunks, err := DiffDirectory(manifest, srcDir, chunker)
	require.NoError(t, err)

	assert.Empty(t, result.Added)
	assert.Empty(t, result.Modified)
	assert.Empty(t, result.Deleted)
	assert.Len(t, result.Unchanged, 1)
	assert.Contains(t, result.Unchanged, "file.txt")
	assert.Empty(t, newChunks)
}

func TestDiffDirectory_ModifiedFile(t *testing.T) {
	srcDir := t.TempDir()
	createTestFile(t, srcDir, "file.txt", "new content")

	manifest := &Manifest{
		Files: []ManifestFile{
			{Path: "file.txt", Chunks: []string{"old-hash"}},
		},
	}

	chunker := NewDefaultChunker()
	result, newChunks, err := DiffDirectory(manifest, srcDir, chunker)
	require.NoError(t, err)

	assert.Empty(t, result.Added)
	assert.Len(t, result.Modified, 1)
	assert.Contains(t, result.Modified, "file.txt")
	assert.Empty(t, result.Deleted)
	assert.Empty(t, result.Unchanged)
	assert.Len(t, newChunks, 1)
	assert.Contains(t, newChunks, "file.txt")
}

func TestDiffDirectory_NestedFiles(t *testing.T) {
	srcDir := t.TempDir()
	createTestFile(t, srcDir, "root.txt", "root content")
	createTestFile(t, srcDir, "sub/file.txt", "sub content")
	createTestFile(t, srcDir, "sub/deep/file.txt", "deep content")

	chunker := NewDefaultChunker()
	result, newChunks, err := DiffDirectory(nil, srcDir, chunker)
	require.NoError(t, err)

	assert.Len(t, result.Added, 3)
	assert.Len(t, newChunks, 3)
}

func TestCollectNewChunks_Dedup(t *testing.T) {
	fileChunks := map[string][]ChunkInfo{
		"file1.txt": {
			{Hash: "hash1", Data: []byte("data1")},
			{Hash: "hash2", Data: []byte("data2")},
		},
		"file2.txt": {
			{Hash: "hash2", Data: []byte("data2")}, // Duplicate
			{Hash: "hash3", Data: []byte("data3")},
		},
	}

	result := CollectNewChunks(fileChunks)

	assert.Len(t, result, 3) // hash1, hash2, hash3 (deduplicated)
	assert.Contains(t, result, "hash1")
	assert.Contains(t, result, "hash2")
	assert.Contains(t, result, "hash3")
}

func TestCollectNewChunks_Empty(t *testing.T) {
	fileChunks := map[string][]ChunkInfo{}
	result := CollectNewChunks(fileChunks)
	assert.Empty(t, result)
}

func TestChunksToUpload(t *testing.T) {
	required := map[string][]byte{
		"hash1": []byte("data1"),
		"hash2": []byte("data2"),
		"hash3": []byte("data3"),
	}
	existing := map[string]bool{
		"hash1": true,  // Already exists
		"hash2": false, // Doesn't exist
		// hash3 not in map = doesn't exist
	}

	result := ChunksToUpload(required, existing)

	assert.Len(t, result, 2)
	assert.Contains(t, result, "hash2")
	assert.Contains(t, result, "hash3")
	assert.NotContains(t, result, "hash1")
}

func TestChunksToUpload_AllExist(t *testing.T) {
	required := map[string][]byte{
		"hash1": []byte("data1"),
		"hash2": []byte("data2"),
	}
	existing := map[string]bool{
		"hash1": true,
		"hash2": true,
	}

	result := ChunksToUpload(required, existing)
	assert.Empty(t, result)
}

func TestChunksToUpload_NoneExist(t *testing.T) {
	required := map[string][]byte{
		"hash1": []byte("data1"),
		"hash2": []byte("data2"),
	}
	existing := map[string]bool{}

	result := ChunksToUpload(required, existing)
	assert.Len(t, result, 2)
}

func TestDiffDirectory_WithSymlinks(t *testing.T) {
	srcDir := t.TempDir()
	createTestFile(t, srcDir, "real.txt", "real content")

	// Create symlink (if supported by OS)
	linkPath := filepath.Join(srcDir, "link.txt")
	realPath := filepath.Join(srcDir, "real.txt")
	err := os.Symlink(realPath, linkPath)
	if err != nil {
		t.Skip("symlinks not supported")
	}

	chunker := NewDefaultChunker()
	result, _, err := DiffDirectory(nil, srcDir, chunker)
	require.NoError(t, err)

	// Symlink should be treated as a regular file when followed
	assert.Len(t, result.Added, 2)
}

func TestDiffDirectory_ErrorHandling(t *testing.T) {
	srcDir := filepath.Join(t.TempDir(), "nonexistent")

	chunker := NewDefaultChunker()
	_, _, err := DiffDirectory(nil, srcDir, chunker)
	assert.Error(t, err)
}

// TestDiffManifests_EmptyManifests tests diffing two empty manifests.
func TestDiffManifests_EmptyManifests(t *testing.T) {
	oldManifest := &Manifest{Files: []ManifestFile{}}
	newManifest := &Manifest{Files: []ManifestFile{}}

	result := DiffManifests(oldManifest, newManifest)

	assert.Empty(t, result.Added)
	assert.Empty(t, result.Modified)
	assert.Empty(t, result.Deleted)
	assert.Empty(t, result.Unchanged)
}

// TestDiffManifests_EmptyFileContent tests files with no chunks (empty files).
func TestDiffManifests_EmptyFileContent(t *testing.T) {
	oldManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "empty.txt", Chunks: []string{}},
		},
	}
	newManifest := &Manifest{
		Files: []ManifestFile{
			{Path: "empty.txt", Chunks: []string{}},
		},
	}

	result := DiffManifests(oldManifest, newManifest)

	assert.Empty(t, result.Added)
	assert.Empty(t, result.Modified)
	assert.Empty(t, result.Deleted)
	assert.Len(t, result.Unchanged, 1)
}

func TestDiffDirectory_EmptyFile(t *testing.T) {
	srcDir := t.TempDir()
	// Create empty file
	emptyPath := filepath.Join(srcDir, "empty.txt")
	_, err := os.Create(emptyPath)
	require.NoError(t, err)

	chunker := NewDefaultChunker()
	result, newChunks, err := DiffDirectory(nil, srcDir, chunker)
	require.NoError(t, err)

	assert.Len(t, result.Added, 1)
	assert.Contains(t, result.Added, "empty.txt")
	assert.Len(t, newChunks, 1)
	// Empty file should produce empty chunk list
	assert.Empty(t, newChunks["empty.txt"])
}

func TestDiffDirectory_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large file test in short mode")
	}

	srcDir := t.TempDir()

	// Create a file large enough to produce multiple chunks
	data := make([]byte, 2*1024*1024) // 2 MB
	for i := range data {
		data[i] = byte(i % 256)
	}
	err := os.WriteFile(filepath.Join(srcDir, "large.bin"), data, 0644)
	require.NoError(t, err)

	chunker := NewDefaultChunker()
	result, newChunks, err := DiffDirectory(nil, srcDir, chunker)
	require.NoError(t, err)

	assert.Len(t, result.Added, 1)
	assert.Contains(t, result.Added, "large.bin")

	// Should have multiple chunks for large file
	assert.GreaterOrEqual(t, len(newChunks["large.bin"]), 1)
}

// Benchmark tests
func BenchmarkDiffManifests(b *testing.B) {
	// Create manifests with 1000 files
	files := make([]ManifestFile, 1000)
	for i := 0; i < 1000; i++ {
		files[i] = ManifestFile{
			Path:    filepath.Join("dir", "file"+string(rune('0'+i%10)), "subfile.txt"),
			Mode:    0644,
			ModTime: time.Now(),
			Size:    1024,
			Chunks:  []string{"hash" + string(rune('0'+i%10))},
		}
	}

	oldManifest := &Manifest{Files: files}

	// Modify 10% of files
	newFiles := make([]ManifestFile, len(files))
	copy(newFiles, files)
	for i := 0; i < 100; i++ {
		newFiles[i*10].Chunks = []string{"new-hash"}
	}
	newManifest := &Manifest{Files: newFiles}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		DiffManifests(oldManifest, newManifest)
	}
}
