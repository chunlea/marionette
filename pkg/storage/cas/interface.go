package cas

import (
	"context"
	"time"
)

// ChunkStore defines operations for storing and retrieving encrypted chunks.
type ChunkStore interface {
	// StoreChunk compresses, encrypts, and stores a chunk.
	// Returns the storage size (compressed + encrypted) and any error.
	StoreChunk(ctx context.Context, tenantID, hash string, data []byte) (int64, error)

	// GetChunk retrieves, decrypts, and decompresses a chunk.
	GetChunk(ctx context.Context, tenantID, hash string) ([]byte, error)

	// DeleteChunk removes a chunk from storage.
	DeleteChunk(ctx context.Context, tenantID, hash string) error

	// ChunkExists checks if a chunk exists in storage.
	ChunkExists(ctx context.Context, tenantID, hash string) (bool, error)
}

// ManifestStore defines operations for workspace manifests.
type ManifestStore interface {
	// SaveManifest stores a manifest held whole in memory.
	// Use OpenManifestWriter for manifests produced by a walk.
	SaveManifest(ctx context.Context, manifest *Manifest) error

	// OpenManifestWriter begins a streaming write. The caller appends entries
	// as it discovers them and then commits, which is what keeps a sync's
	// memory independent of how many files the workspace holds.
	OpenManifestWriter(ctx context.Context, manifest *Manifest) (*ManifestObjectWriter, error)

	// OpenManifest opens a manifest for streaming reads, holding at most one
	// frame rather than the whole entry list.
	OpenManifest(ctx context.Context, tenantID, workspaceID, manifestID string) (*ManifestEntries, error)

	// LoadManifest loads a complete manifest.
	LoadManifest(ctx context.Context, tenantID, workspaceID, manifestID string) (*Manifest, error)

	// StreamManifestFiles returns a channel for streaming manifest files.
	// Use for very large manifests (100k+ files).
	// The header is returned synchronously, files are streamed via channel.
	// The channel is closed when all files have been sent or an error occurs.
	StreamManifestFiles(ctx context.Context, tenantID, workspaceID, manifestID string) (<-chan ManifestFile, *ManifestHeader, error)

	// DeleteManifest removes a manifest from storage.
	DeleteManifest(ctx context.Context, tenantID, workspaceID, manifestID string) error
}

// Syncer defines the workspace sync/restore interface.
type Syncer interface {
	// Sync performs workspace sync.
	// Returns the created manifest ID.
	Sync(ctx context.Context, workspaceID, tenantID, srcDir string) (string, error)

	// SyncIncremental performs incremental sync based on a previous manifest.
	// Only uploads chunks that have changed since the previous manifest.
	// Returns the created manifest ID and a diff summary.
	SyncIncremental(ctx context.Context, workspaceID, tenantID, srcDir, previousManifestID string) (string, *DiffResult, error)

	// Restore reconstructs a workspace from the latest manifest.
	Restore(ctx context.Context, workspaceID, tenantID, dstDir string) error

	// RestoreFromManifest restores from a specific manifest.
	RestoreFromManifest(ctx context.Context, manifestID, tenantID, dstDir string) error

	// ValidateManifest checks that all chunks referenced by a manifest exist.
	ValidateManifest(ctx context.Context, manifest *Manifest) error

	// Diff compares the current directory against a manifest.
	Diff(ctx context.Context, manifest *Manifest, srcDir string) (*DiffResult, error)
}

// Encryptor provides encryption and decryption for CAS data.
type Encryptor interface {
	// Encrypt compresses and encrypts data for a tenant.
	Encrypt(ctx context.Context, tenantID string, data []byte) ([]byte, error)

	// Decrypt decrypts and decompresses data for a tenant.
	Decrypt(ctx context.Context, tenantID string, ciphertext []byte) ([]byte, error)
}

// GarbageCollector defines operations for cleaning up orphaned chunks.
type GarbageCollector interface {
	// Mark identifies unreferenced chunks and marks them for deletion.
	// Returns the number of chunks marked.
	Mark(ctx context.Context, tenantID string) (int, error)

	// Sweep permanently deletes chunks that have been marked for deletion
	// and have passed the grace period.
	// Returns the number of chunks deleted and total bytes freed.
	Sweep(ctx context.Context, tenantID string) (chunksDeleted int, bytesFreed int64, err error)

	// Resurrect clears the deletion mark on a chunk that has been re-referenced.
	// This prevents deletion of chunks that were marked but are now in use.
	Resurrect(ctx context.Context, tenantID, hash string) error

	// RunGC performs a full garbage collection cycle (mark + sweep).
	// Returns statistics about the GC run.
	RunGC(ctx context.Context, tenantID string) (*GCResult, error)
}

// GCResult contains statistics from a garbage collection run.
type GCResult struct {
	// ChunksMarked is the number of chunks marked for deletion in the mark phase.
	ChunksMarked int

	// ChunksDeleted is the number of chunks physically deleted in the sweep phase.
	ChunksDeleted int

	// ChunksResurrected is the number of chunks that were unmarked due to being re-referenced.
	ChunksResurrected int

	// BytesFreed is the total storage bytes freed by deletion.
	BytesFreed int64

	// Duration is how long the GC cycle took.
	Duration time.Duration

	// Errors contains any non-fatal errors encountered during GC.
	Errors []error
}

// GCConfig contains configuration for the garbage collector.
type GCConfig struct {
	// GracePeriod is how long to wait after marking before sweeping.
	// Default: 7 days
	GracePeriod time.Duration

	// BatchSize is the maximum number of chunks to process per batch.
	// Default: 1000
	BatchSize int

	// DryRun if true, reports what would be deleted without actually deleting.
	DryRun bool
}
