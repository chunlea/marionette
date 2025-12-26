package cas

import (
	"os"
	"path/filepath"
)

// DiffResult represents the differences between two workspace states.
type DiffResult struct {
	// Added contains files that exist in the new state but not in the old state.
	Added []string

	// Modified contains files that exist in both states but have different content.
	Modified []string

	// Deleted contains files that exist in the old state but not in the new state.
	Deleted []string

	// Unchanged contains files that exist in both states with the same content.
	Unchanged []string
}

// IsEmpty returns true if there are no changes.
func (d *DiffResult) IsEmpty() bool {
	return len(d.Added) == 0 && len(d.Modified) == 0 && len(d.Deleted) == 0
}

// TotalChanges returns the total number of changed files.
func (d *DiffResult) TotalChanges() int {
	return len(d.Added) + len(d.Modified) + len(d.Deleted)
}

// DiffManifests compares two manifests and returns the differences.
// oldManifest can be nil, in which case all files in newManifest are considered added.
func DiffManifests(oldManifest, newManifest *Manifest) *DiffResult {
	result := &DiffResult{
		Added:     make([]string, 0),
		Modified:  make([]string, 0),
		Deleted:   make([]string, 0),
		Unchanged: make([]string, 0),
	}

	// Build maps for quick lookup
	oldFiles := make(map[string]*ManifestFile)
	newFiles := make(map[string]*ManifestFile)

	if oldManifest != nil {
		for i := range oldManifest.Files {
			oldFiles[oldManifest.Files[i].Path] = &oldManifest.Files[i]
		}
	}

	for i := range newManifest.Files {
		newFiles[newManifest.Files[i].Path] = &newManifest.Files[i]
	}

	// Find added and modified files
	for path, newFile := range newFiles {
		oldFile, exists := oldFiles[path]
		switch {
		case !exists:
			result.Added = append(result.Added, path)
		case !chunksEqual(oldFile.Chunks, newFile.Chunks):
			result.Modified = append(result.Modified, path)
		default:
			result.Unchanged = append(result.Unchanged, path)
		}
	}

	// Find deleted files
	for path := range oldFiles {
		if _, exists := newFiles[path]; !exists {
			result.Deleted = append(result.Deleted, path)
		}
	}

	return result
}

// DiffDirectory compares a manifest against the current filesystem state.
// Returns which files need to be synced.
func DiffDirectory(manifest *Manifest, srcDir string, chunker *Chunker) (*DiffResult, map[string][]ChunkInfo, error) {
	result := &DiffResult{
		Added:     make([]string, 0),
		Modified:  make([]string, 0),
		Deleted:   make([]string, 0),
		Unchanged: make([]string, 0),
	}

	// Map to store new chunks for modified/added files
	newChunks := make(map[string][]ChunkInfo)

	// Build map of manifest files
	manifestFiles := make(map[string]*ManifestFile)
	if manifest != nil {
		for i := range manifest.Files {
			manifestFiles[manifest.Files[i].Path] = &manifest.Files[i]
		}
	}

	// Track which manifest files we've seen
	seenFiles := make(map[string]bool)

	// Walk the directory
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

		seenFiles[relPath] = true

		// Read file and compute chunks
		data, err := os.ReadFile(path) //nolint:gosec // path is validated relative to srcDir
		if err != nil {
			return err
		}

		chunks, err := chunker.ChunkData(data)
		if err != nil {
			return err
		}

		// Check against manifest
		manifestFile, exists := manifestFiles[relPath]
		if !exists {
			result.Added = append(result.Added, relPath)
			newChunks[relPath] = chunks
		} else {
			// Compare chunk hashes
			newHashes := make([]string, len(chunks))
			for i, c := range chunks {
				newHashes[i] = c.Hash
			}

			if !chunksEqual(manifestFile.Chunks, newHashes) {
				result.Modified = append(result.Modified, relPath)
				newChunks[relPath] = chunks
			} else {
				result.Unchanged = append(result.Unchanged, relPath)
			}
		}

		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	// Find deleted files
	for path := range manifestFiles {
		if !seenFiles[path] {
			result.Deleted = append(result.Deleted, path)
		}
	}

	return result, newChunks, nil
}

// chunksEqual compares two slices of chunk hashes for equality.
func chunksEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// CollectNewChunks returns a deduplicated map of chunks that need to be uploaded.
func CollectNewChunks(fileChunks map[string][]ChunkInfo) map[string][]byte {
	result := make(map[string][]byte)
	for _, chunks := range fileChunks {
		for _, chunk := range chunks {
			if _, exists := result[chunk.Hash]; !exists {
				result[chunk.Hash] = chunk.Data
			}
		}
	}
	return result
}

// ChunksToUpload determines which chunks need to be uploaded based on what's missing.
// existingChunks is a set of chunk hashes that already exist in storage.
func ChunksToUpload(required map[string][]byte, existingChunks map[string]bool) map[string][]byte {
	result := make(map[string][]byte)
	for hash, data := range required {
		if !existingChunks[hash] {
			result[hash] = data
		}
	}
	return result
}
