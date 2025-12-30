package cas

import (
	"context"
	"fmt"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// ChunkMetadataStore defines the database operations needed for GC.
// This is a subset of the store.Store interface focused on chunk metadata.
type ChunkMetadataStore interface {
	// ListUnreferencedChunks returns chunks with ref_count = 0 that haven't been marked.
	ListUnreferencedChunks(ctx context.Context, tenantID string, limit int) ([]*store.Chunk, error)

	// ListSoftDeletedChunks returns chunks marked for deletion past the grace period.
	ListSoftDeletedChunks(ctx context.Context, tenantID string, olderThan time.Time, limit int) ([]*store.Chunk, error)

	// MarkChunkDeleted sets the deleted_at timestamp on a chunk.
	MarkChunkDeleted(ctx context.Context, tenantID, hash string) error

	// ClearChunkDeleted clears the deleted_at timestamp (resurrection).
	ClearChunkDeleted(ctx context.Context, tenantID, hash string) error

	// DeleteChunk permanently removes a chunk from the database.
	DeleteChunk(ctx context.Context, tenantID, hash string) error

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
func (g *GC) Mark(ctx context.Context, tenantID string) (int, error) {
	totalMarked := 0

	for {
		select {
		case <-ctx.Done():
			return totalMarked, ctx.Err()
		default:
		}

		// Get a batch of unreferenced chunks
		chunks, err := g.metadataStore.ListUnreferencedChunks(ctx, tenantID, g.config.BatchSize)
		if err != nil {
			return totalMarked, fmt.Errorf("listing unreferenced chunks: %w", err)
		}

		if len(chunks) == 0 {
			break
		}

		// Mark each chunk for deletion
		for _, chunk := range chunks {
			if err := g.metadataStore.MarkChunkDeleted(ctx, tenantID, chunk.Hash); err != nil {
				return totalMarked, fmt.Errorf("marking chunk %s: %w", chunk.Hash, err)
			}
			totalMarked++
		}

		// If we got fewer than batch size, we're done
		if len(chunks) < g.config.BatchSize {
			break
		}
	}

	return totalMarked, nil
}

// Sweep permanently deletes chunks that have been marked and passed the grace period.
func (g *GC) Sweep(ctx context.Context, tenantID string) (int, int64, error) {
	totalDeleted := 0
	totalBytes := int64(0)

	// Calculate the cutoff time (chunks marked before this are eligible for deletion)
	cutoff := time.Now().Add(-g.config.GracePeriod)

	for {
		select {
		case <-ctx.Done():
			return totalDeleted, totalBytes, ctx.Err()
		default:
		}

		// Get a batch of chunks eligible for deletion
		chunks, err := g.metadataStore.ListSoftDeletedChunks(ctx, tenantID, cutoff, g.config.BatchSize)
		if err != nil {
			return totalDeleted, totalBytes, fmt.Errorf("listing soft-deleted chunks: %w", err)
		}

		if len(chunks) == 0 {
			break
		}

		// Delete each chunk
		for _, chunk := range chunks {
			// Skip if dry run
			if g.config.DryRun {
				totalDeleted++
				totalBytes += chunk.Size
				continue
			}

			// Delete from blob storage first
			// Ignore errors - chunk might already be deleted from storage
			// We still want to clean up the database record
			_ = g.chunkStore.DeleteChunk(ctx, tenantID, chunk.Hash)

			// Delete from database
			if err := g.metadataStore.DeleteChunk(ctx, tenantID, chunk.Hash); err != nil {
				return totalDeleted, totalBytes, fmt.Errorf("deleting chunk %s from database: %w", chunk.Hash, err)
			}

			totalDeleted++
			totalBytes += chunk.Size
		}

		// If we got fewer than batch size, we're done
		if len(chunks) < g.config.BatchSize {
			break
		}
	}

	return totalDeleted, totalBytes, nil
}

// Resurrect clears the deletion mark on a chunk that has been re-referenced.
func (g *GC) Resurrect(ctx context.Context, tenantID, hash string) error {
	// First check if the chunk exists and is marked for deletion
	chunk, err := g.metadataStore.GetChunk(ctx, tenantID, hash)
	if err != nil {
		return fmt.Errorf("getting chunk: %w", err)
	}

	// Only resurrect if actually marked for deletion
	if chunk.DeletedAt == nil {
		return nil // Already alive
	}

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
