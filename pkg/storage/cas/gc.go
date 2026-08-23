package cas

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// ChunkMetadataStore defines the database operations needed for GC.
// *postgres.Store implements it.
//
// Mark and sweep are single statements that re-check ref_count as they write,
// rather than a read followed by a write. The read-then-write version could
// mark a chunk that had been referenced in between, and the sweep then deleted
// it because it only looked at deleted_at.
type ChunkMetadataStore interface {
	// MarkUnreferencedChunks marks up to limit unreferenced chunks older than
	// minAge, and returns the rows it actually marked.
	//
	// Ages are measured against the store's own clock, not the caller's: these
	// timestamps are written by the store, and comparing them against an
	// application clock that drifts makes a short grace period behave
	// erratically.
	MarkUnreferencedChunks(ctx context.Context, tenantID string, minAge time.Duration, limit int) ([]*store.Chunk, error)

	// ListSweepableChunks returns chunks that are still unreferenced and have
	// been marked for at least minAge.
	ListSweepableChunks(ctx context.Context, tenantID string, minAge time.Duration, limit int) ([]*store.Chunk, error)

	// DeleteChunkIfUnreferenced removes a chunk only while it is still
	// unreferenced and has been marked for at least minAge, reporting whether
	// it did.
	DeleteChunkIfUnreferenced(ctx context.Context, tenantID, hash string, minAge time.Duration) (bool, error)

	// ListUnreferencedChunks returns chunks with ref_count = 0 that haven't been
	// marked. Used by dry runs, which must not write.
	ListUnreferencedChunks(ctx context.Context, tenantID string, limit int) ([]*store.Chunk, error)

	// ClearChunkDeleted clears the deleted_at timestamp (resurrection).
	ClearChunkDeleted(ctx context.Context, tenantID, hash string) error

	// GetChunk retrieves chunk metadata.
	GetChunk(ctx context.Context, tenantID, hash string) (*store.Chunk, error)
}

// DefaultGracePeriod is the default time to wait before sweeping marked chunks.
const DefaultGracePeriod = 7 * 24 * time.Hour // 7 days

// DefaultBatchSize is the default number of chunks to process per batch.
const DefaultBatchSize = 1000

// GC implements the GarbageCollector interface using mark-and-sweep.
type GC struct {
	metadataStore ChunkMetadataStore
	chunkStore    ChunkStore
	config        GCConfig
}

// NewGC creates a new garbage collector.
func NewGC(metadataStore ChunkMetadataStore, chunkStore ChunkStore, config GCConfig) *GC {
	if config.GracePeriod <= 0 {
		config.GracePeriod = DefaultGracePeriod
	}
	if config.BatchSize <= 0 {
		config.BatchSize = DefaultBatchSize
	}

	return &GC{
		metadataStore: metadataStore,
		chunkStore:    chunkStore,
		config:        config,
	}
}

// Mark identifies unreferenced chunks and marks them for deletion.
//
// Chunks created within the grace period are left alone: a chunk that has been
// uploaded but whose manifest has not been committed yet is legitimately
// unreferenced, and marking it immediately would put the commit in a race with
// the sweep.
func (g *GC) Mark(ctx context.Context, tenantID string) (int, error) {
	totalMarked := 0

	for {
		select {
		case <-ctx.Done():
			return totalMarked, ctx.Err()
		default:
		}

		if g.config.DryRun {
			// A dry run must not write, so it reports the candidate set instead.
			chunks, err := g.metadataStore.ListUnreferencedChunks(ctx, tenantID, g.config.BatchSize)
			if err != nil {
				return totalMarked, fmt.Errorf("listing unreferenced chunks: %w", err)
			}
			totalMarked += len(chunks)
			return totalMarked, nil
		}

		marked, err := g.metadataStore.MarkUnreferencedChunks(ctx, tenantID, g.config.GracePeriod, g.config.BatchSize)
		if err != nil {
			return totalMarked, fmt.Errorf("marking unreferenced chunks: %w", err)
		}

		totalMarked += len(marked)

		// A short batch means the candidate set is exhausted. Marked rows leave
		// that set, so this terminates.
		if len(marked) < g.config.BatchSize {
			break
		}
	}

	return totalMarked, nil
}

// Sweep permanently deletes chunks that have been marked and passed the grace
// period and are still unreferenced.
//
// Ordering is deliberate: the database row goes first. A crash between the two
// deletes then leaves an orphaned blob, which wastes space and can be swept
// later. The reverse order leaves metadata claiming a chunk whose bytes are
// gone, which is silent corruption for anything that restores from it.
func (g *GC) Sweep(ctx context.Context, tenantID string) (int, int64, error) {
	totalDeleted := 0
	totalBytes := int64(0)
	var blobErrs []error

	for {
		select {
		case <-ctx.Done():
			return totalDeleted, totalBytes, ctx.Err()
		default:
		}

		chunks, err := g.metadataStore.ListSweepableChunks(ctx, tenantID, g.config.GracePeriod, g.config.BatchSize)
		if err != nil {
			return totalDeleted, totalBytes, fmt.Errorf("listing sweepable chunks: %w", err)
		}

		if len(chunks) == 0 {
			break
		}

		for _, chunk := range chunks {
			if g.config.DryRun {
				totalDeleted++
				totalBytes += chunk.Size
				continue
			}

			// The same age bound is re-applied by the delete, so a chunk that
			// was re-marked in the meantime is not caught out.
			deleted, err := g.metadataStore.DeleteChunkIfUnreferenced(ctx, tenantID, chunk.Hash, g.config.GracePeriod)
			if err != nil {
				return totalDeleted, totalBytes, fmt.Errorf("deleting chunk %s from database: %w", chunk.Hash, err)
			}
			if !deleted {
				// Referenced again between the list and the delete. Its blob
				// must stay.
				continue
			}

			if err := g.chunkStore.DeleteChunk(ctx, tenantID, chunk.Hash); err != nil {
				// The row is already gone, so the accounting stands; report the
				// orphaned blob rather than aborting the rest of the sweep.
				blobErrs = append(blobErrs, fmt.Errorf("deleting blob for chunk %s: %w", chunk.Hash, err))
			}

			totalDeleted++
			totalBytes += chunk.Size
		}

		if len(chunks) < g.config.BatchSize {
			break
		}
	}

	return totalDeleted, totalBytes, errors.Join(blobErrs...)
}

// Resurrect clears the deletion mark on a chunk that has been re-referenced.
//
// Note that referencing a chunk already resurrects it: IncrementChunkRef clears
// deleted_at in the same statement. This exists for the upload-then-commit
// window, where a chunk is rewritten before anything references it.
func (g *GC) Resurrect(ctx context.Context, tenantID, hash string) error {
	if err := g.metadataStore.ClearChunkDeleted(ctx, tenantID, hash); err != nil {
		return fmt.Errorf("clearing deleted_at: %w", err)
	}
	return nil
}

// RunGC performs a full garbage collection cycle.
func (g *GC) RunGC(ctx context.Context, tenantID string) (*GCResult, error) {
	start := time.Now()
	result := &GCResult{}

	// Mark phase
	marked, err := g.Mark(ctx, tenantID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("mark phase: %w", err))
		// Continue to sweep phase even if mark had errors
	}
	result.ChunksMarked = marked

	// Sweep phase
	deleted, bytes, err := g.Sweep(ctx, tenantID)
	if err != nil {
		result.Errors = append(result.Errors, fmt.Errorf("sweep phase: %w", err))
	}
	result.ChunksDeleted = deleted
	result.BytesFreed = bytes

	result.Duration = time.Since(start)

	// Return error only if both phases failed
	if len(result.Errors) == 2 {
		return result, fmt.Errorf("GC failed: %v", result.Errors)
	}

	return result, nil
}

// ResurrectIfNeeded checks if a chunk is marked for deletion and resurrects it.
// This should be called when a chunk is being referenced by a new manifest.
// Returns true if the chunk was resurrected.
func (g *GC) ResurrectIfNeeded(ctx context.Context, tenantID, hash string) (bool, error) {
	chunk, err := g.metadataStore.GetChunk(ctx, tenantID, hash)
	if err != nil {
		// Chunk doesn't exist - that's fine, it will be created
		return false, nil //nolint:nilerr // Intentional: missing chunk is not an error for resurrection check
	}

	if chunk.DeletedAt == nil {
		return false, nil // Not marked for deletion
	}

	// Resurrect the chunk
	if err := g.metadataStore.ClearChunkDeleted(ctx, tenantID, hash); err != nil {
		return false, fmt.Errorf("resurrecting chunk: %w", err)
	}

	return true, nil
}

// GCStats contains statistics about the current GC state for a tenant.
type GCStats struct {
	// UnreferencedChunks is the number of chunks with ref_count = 0.
	UnreferencedChunks int

	// MarkedChunks is the number of chunks marked for deletion.
	MarkedChunks int

	// EligibleForSweep is the number of chunks past the grace period.
	EligibleForSweep int

	// TotalChunks is the total number of chunks for this tenant.
	TotalChunks int

	// TotalBytes is the total storage used by chunks.
	TotalBytes int64
}
