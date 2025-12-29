package cas

import (
	"errors"
	"fmt"
)

// CAS-specific errors.
var (
	// ErrChunkNotFound is returned when a chunk does not exist.
	ErrChunkNotFound = errors.New("chunk not found")

	// ErrManifestNotFound is returned when a manifest does not exist.
	ErrManifestNotFound = errors.New("manifest not found")

	// ErrChunkCorrupted is returned when a chunk's hash doesn't match.
	ErrChunkCorrupted = errors.New("chunk corrupted: hash mismatch")

	// ErrInvalidManifest is returned when a manifest has an invalid format.
	ErrInvalidManifest = errors.New("invalid manifest format")

	// ErrWorkspaceEmpty is returned when attempting to sync an empty workspace.
	ErrWorkspaceEmpty = errors.New("workspace is empty")

	// ErrRestoreFailed is returned when workspace restoration fails.
	ErrRestoreFailed = errors.New("restore failed")
)

// ChunksMissingError is returned when a manifest references chunks that don't exist.
type ChunksMissingError struct {
	ManifestID string
	Missing    []string
	InStorage  bool // True if missing from storage, false if missing from DB
}

func (e *ChunksMissingError) Error() string {
	loc := "database"
	if e.InStorage {
		loc = "object storage"
	}
	return fmt.Sprintf("manifest %s references %d chunks missing from %s", e.ManifestID, len(e.Missing), loc)
}

// PathTraversalError is returned when a file path attempts to escape the workspace.
type PathTraversalError struct {
	Path string
}

func (e *PathTraversalError) Error() string {
	return fmt.Sprintf("invalid path: path traversal attempt: %s", e.Path)
}
