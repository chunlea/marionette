package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// Chunk column list for SELECT queries.
const chunkColumns = `hash, tenant_id, size, ref_count, deleted_at, created_at`

// Manifest column list for SELECT queries.
const manifestColumns = `id, workspace_id, parent_id, total_size, single_chunk, chunk_hash,
	chunk_count, files_json, tenant_id, created_at`

// =============================================================================
// Chunk CRUD
// =============================================================================

// CreateChunk creates a new content chunk.
func (s *Store) CreateChunk(ctx context.Context, chunk *store.Chunk) error {
	return createChunk(ctx, s.pool, chunk)
}

// CreateChunk creates a new chunk within a transaction.
func (t *Tx) CreateChunk(ctx context.Context, chunk *store.Chunk) error {
	return createChunk(ctx, t.tx, chunk)
}

func createChunk(ctx context.Context, q querier, chunk *store.Chunk) error {
	query := `
		INSERT INTO chunks (
			hash, tenant_id, size, ref_count, deleted_at, created_at
		) VALUES (
			$1, $2, $3, $4, $5, NOW()
		)
		RETURNING created_at`

	err := q.QueryRow(ctx, query,
		chunk.Hash, chunk.TenantID, chunk.Size, chunk.RefCount, chunk.DeletedAt,
	).Scan(&chunk.CreatedAt)

	if err != nil {
		return handlePgError(err, "chunk", chunk.Hash)
	}
	return nil
}

// GetChunk retrieves a chunk by hash and tenant ID.
func (s *Store) GetChunk(ctx context.Context, tenantID, hash string) (*store.Chunk, error) {
	return getChunk(ctx, s.pool, tenantID, hash)
}

// GetChunk retrieves a chunk by hash and tenant ID within a transaction.
func (t *Tx) GetChunk(ctx context.Context, tenantID, hash string) (*store.Chunk, error) {
	return getChunk(ctx, t.tx, tenantID, hash)
}

func getChunk(ctx context.Context, q querier, tenantID, hash string) (*store.Chunk, error) {
	query := fmt.Sprintf(`SELECT %s FROM chunks WHERE tenant_id = $1 AND hash = $2`, chunkColumns)
	row := q.QueryRow(ctx, query, tenantID, hash)
	return scanChunk(row, fmt.Sprintf("%s/%s", tenantID, hash))
}

// IncrementChunkRef increments the reference count for a chunk.
func (s *Store) IncrementChunkRef(ctx context.Context, tenantID, hash string) error {
	return incrementChunkRef(ctx, s.pool, tenantID, hash)
}

// IncrementChunkRefCount increments the reference count (alias for IncrementChunkRef).
func (s *Store) IncrementChunkRefCount(ctx context.Context, tenantID, hash string) error {
	return incrementChunkRef(ctx, s.pool, tenantID, hash)
}

// IncrementChunkRef increments the reference count within a transaction.
func (t *Tx) IncrementChunkRef(ctx context.Context, tenantID, hash string) error {
	return incrementChunkRef(ctx, t.tx, tenantID, hash)
}

// IncrementChunkRefCount increments the reference count within a transaction (alias).
func (t *Tx) IncrementChunkRefCount(ctx context.Context, tenantID, hash string) error {
	return incrementChunkRef(ctx, t.tx, tenantID, hash)
}

func incrementChunkRef(ctx context.Context, q querier, tenantID, hash string) error {
	// Clearing deleted_at in the same statement is the resurrection path: a
	// chunk the GC marked while it was unreferenced becomes live again the
	// instant something references it, with no window in between. Doing it as a
	// separate call would reopen the race the mark/sweep guards close.
	query := `UPDATE chunks SET ref_count = ref_count + 1, deleted_at = NULL WHERE tenant_id = $1 AND hash = $2`
	result, err := q.Exec(ctx, query, tenantID, hash)
	if err != nil {
		return handlePgError(err, "chunk", hash)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "chunk", ID: hash}
	}

	return nil
}

// DecrementChunkRef decrements the reference count for a chunk.
func (s *Store) DecrementChunkRef(ctx context.Context, tenantID, hash string) error {
	return decrementChunkRef(ctx, s.pool, tenantID, hash)
}

// DecrementChunkRefCount decrements the reference count (alias for DecrementChunkRef).
func (s *Store) DecrementChunkRefCount(ctx context.Context, tenantID, hash string) error {
	return decrementChunkRef(ctx, s.pool, tenantID, hash)
}

// DecrementChunkRef decrements the reference count within a transaction.
func (t *Tx) DecrementChunkRef(ctx context.Context, tenantID, hash string) error {
	return decrementChunkRef(ctx, t.tx, tenantID, hash)
}

// DecrementChunkRefCount decrements the reference count within a transaction (alias).
func (t *Tx) DecrementChunkRefCount(ctx context.Context, tenantID, hash string) error {
	return decrementChunkRef(ctx, t.tx, tenantID, hash)
}

func decrementChunkRef(ctx context.Context, q querier, tenantID, hash string) error {
	query := `UPDATE chunks SET ref_count = ref_count - 1 WHERE tenant_id = $1 AND hash = $2 AND ref_count > 0`
	result, err := q.Exec(ctx, query, tenantID, hash)
	if err != nil {
		return handlePgError(err, "chunk", hash)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "chunk", ID: hash}
	}

	return nil
}

// UpdateChunk updates chunk fields.
func (s *Store) UpdateChunk(ctx context.Context, tenantID, hash string, updates store.ChunkUpdates) error {
	return updateChunk(ctx, s.pool, tenantID, hash, updates)
}

// UpdateChunk updates chunk fields within a transaction.
func (t *Tx) UpdateChunk(ctx context.Context, tenantID, hash string, updates store.ChunkUpdates) error {
	return updateChunk(ctx, t.tx, tenantID, hash, updates)
}

func updateChunk(ctx context.Context, q querier, tenantID, hash string, updates store.ChunkUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.RefCount != nil {
		setClauses = append(setClauses, fmt.Sprintf("ref_count = $%d", argNum))
		args = append(args, *updates.RefCount)
		argNum++
	}
	if updates.DeletedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("deleted_at = $%d", argNum))
		args = append(args, *updates.DeletedAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	query := fmt.Sprintf(`UPDATE chunks SET %s WHERE tenant_id = $%d AND hash = $%d`,
		strings.Join(setClauses, ", "), argNum, argNum+1)
	args = append(args, tenantID, hash)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "chunk", hash)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "chunk", ID: hash}
	}

	return nil
}

// DeleteChunk hard-deletes a chunk.
func (s *Store) DeleteChunk(ctx context.Context, tenantID, hash string) error {
	return deleteChunk(ctx, s.pool, tenantID, hash)
}

// DeleteChunk hard-deletes a chunk within a transaction.
func (t *Tx) DeleteChunk(ctx context.Context, tenantID, hash string) error {
	return deleteChunk(ctx, t.tx, tenantID, hash)
}

func deleteChunk(ctx context.Context, q querier, tenantID, hash string) error {
	query := `DELETE FROM chunks WHERE tenant_id = $1 AND hash = $2`
	result, err := q.Exec(ctx, query, tenantID, hash)
	if err != nil {
		return handlePgError(err, "chunk", hash)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "chunk", ID: hash}
	}

	return nil
}

// ListUnreferencedChunks lists chunks with ref_count = 0 that are candidates for GC.
func (s *Store) ListUnreferencedChunks(ctx context.Context, tenantID string, limit int) ([]*store.Chunk, error) {
	return listUnreferencedChunks(ctx, s.pool, tenantID, limit)
}

// ListUnreferencedChunks lists unreferenced chunks within a transaction.
func (t *Tx) ListUnreferencedChunks(ctx context.Context, tenantID string, limit int) ([]*store.Chunk, error) {
	return listUnreferencedChunks(ctx, t.tx, tenantID, limit)
}

func listUnreferencedChunks(ctx context.Context, q querier, tenantID string, limit int) ([]*store.Chunk, error) {
	if limit <= 0 {
		limit = 100
	}

	query := fmt.Sprintf(`
		SELECT %s FROM chunks
		WHERE tenant_id = $1 AND ref_count = 0 AND deleted_at IS NULL
		ORDER BY created_at ASC
		LIMIT $2`,
		chunkColumns)

	rows, err := q.Query(ctx, query, tenantID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying unreferenced chunks: %w", err)
	}
	defer rows.Close()

	var chunks []*store.Chunk
	for rows.Next() {
		chunk, err := scanChunkFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating chunks: %w", err)
	}

	return chunks, nil
}

// ListSoftDeletedChunks lists chunks that have been soft-deleted and are past the grace period.
func (s *Store) ListSoftDeletedChunks(ctx context.Context, tenantID string, olderThan time.Time, limit int) ([]*store.Chunk, error) {
	return listSoftDeletedChunks(ctx, s.pool, tenantID, olderThan, limit)
}

// ListSoftDeletedChunks lists soft-deleted chunks within a transaction.
func (t *Tx) ListSoftDeletedChunks(ctx context.Context, tenantID string, olderThan time.Time, limit int) ([]*store.Chunk, error) {
	return listSoftDeletedChunks(ctx, t.tx, tenantID, olderThan, limit)
}

func listSoftDeletedChunks(ctx context.Context, q querier, tenantID string, olderThan time.Time, limit int) ([]*store.Chunk, error) {
	if limit <= 0 {
		limit = 100
	}

	query := fmt.Sprintf(`
		SELECT %s FROM chunks
		WHERE tenant_id = $1 AND deleted_at IS NOT NULL AND deleted_at < $2
		ORDER BY deleted_at ASC
		LIMIT $3`,
		chunkColumns)

	rows, err := q.Query(ctx, query, tenantID, olderThan, limit)
	if err != nil {
		return nil, fmt.Errorf("querying soft-deleted chunks: %w", err)
	}
	defer rows.Close()

	var chunks []*store.Chunk
	for rows.Next() {
		chunk, err := scanChunkFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating chunks: %w", err)
	}

	return chunks, nil
}

// MarkChunkDeleted sets the deleted_at timestamp on a chunk (soft delete).
func (s *Store) MarkChunkDeleted(ctx context.Context, tenantID, hash string) error {
	return markChunkDeleted(ctx, s.pool, tenantID, hash)
}

// MarkChunkDeleted sets deleted_at within a transaction.
func (t *Tx) MarkChunkDeleted(ctx context.Context, tenantID, hash string) error {
	return markChunkDeleted(ctx, t.tx, tenantID, hash)
}

func markChunkDeleted(ctx context.Context, q querier, tenantID, hash string) error {
	query := `UPDATE chunks SET deleted_at = NOW() WHERE tenant_id = $1 AND hash = $2 AND deleted_at IS NULL`
	result, err := q.Exec(ctx, query, tenantID, hash)
	if err != nil {
		return handlePgError(err, "chunk", hash)
	}

	if result.RowsAffected() == 0 {
		// Check if chunk exists
		checkQuery := `SELECT 1 FROM chunks WHERE tenant_id = $1 AND hash = $2`
		var exists int
		err := q.QueryRow(ctx, checkQuery, tenantID, hash).Scan(&exists)
		if errors.Is(err, pgx.ErrNoRows) {
			return &store.NotFoundError{Resource: "chunk", ID: hash}
		}
		// Chunk exists but already deleted - not an error
	}

	return nil
}

// ClearChunkDeleted clears the deleted_at timestamp (resurrects a chunk).
func (s *Store) ClearChunkDeleted(ctx context.Context, tenantID, hash string) error {
	return clearChunkDeleted(ctx, s.pool, tenantID, hash)
}

// ClearChunkDeleted clears deleted_at within a transaction.
func (t *Tx) ClearChunkDeleted(ctx context.Context, tenantID, hash string) error {
	return clearChunkDeleted(ctx, t.tx, tenantID, hash)
}

func clearChunkDeleted(ctx context.Context, q querier, tenantID, hash string) error {
	query := `UPDATE chunks SET deleted_at = NULL WHERE tenant_id = $1 AND hash = $2`
	result, err := q.Exec(ctx, query, tenantID, hash)
	if err != nil {
		return handlePgError(err, "chunk", hash)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "chunk", ID: hash}
	}

	return nil
}

func scanChunk(row pgx.Row, identifier string) (*store.Chunk, error) {
	var c store.Chunk
	err := row.Scan(
		&c.Hash, &c.TenantID, &c.Size, &c.RefCount, &c.DeletedAt, &c.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "chunk", ID: identifier}
		}
		return nil, fmt.Errorf("scanning chunk: %w", err)
	}
	return &c, nil
}

// =============================================================================
// Garbage collection primitives
//
// Mark and sweep are expressed as single statements that re-check ref_count at
// the moment they write. The previous read-then-write pair could mark a chunk
// that had been referenced between the SELECT and the UPDATE, and the sweep
// then deleted it because it only looked at deleted_at — losing live data.
// =============================================================================

// RegisterChunk records a chunk that has just been written to blob storage, or
// revives an existing row that the collector had marked.
//
// ref_count starts at 0: the chunk exists but nothing points at it yet. That is
// the upload-then-commit window, and it is why Mark ignores chunks younger than
// the grace period.
func (s *Store) RegisterChunk(ctx context.Context, tenantID, hash string, size int64) error {
	return registerChunk(ctx, s.pool, tenantID, hash, size)
}

// RegisterChunk records a chunk within a transaction.
func (t *Tx) RegisterChunk(ctx context.Context, tenantID, hash string, size int64) error {
	return registerChunk(ctx, t.tx, tenantID, hash, size)
}

func registerChunk(ctx context.Context, q querier, tenantID, hash string, size int64) error {
	// Content-addressed: the same hash is the same bytes, so a conflict means
	// someone stored this chunk already and the row only needs reviving.
	query := `
		INSERT INTO chunks (hash, tenant_id, size, ref_count, created_at)
		VALUES ($1, $2, $3, 0, NOW())
		ON CONFLICT (tenant_id, hash) DO UPDATE
		SET deleted_at = NULL`

	if _, err := q.Exec(ctx, query, hash, tenantID, size); err != nil {
		return handlePgError(err, "chunk", hash)
	}
	return nil
}

// MarkUnreferencedChunks marks up to limit unreferenced chunks for deletion and
// returns the rows it actually marked.
//
// Only chunks older than minAge are considered, so a chunk that has been
// uploaded but not yet referenced survives its commit window.
//
// Ages are measured against the database clock rather than the caller's. The
// timestamps being compared were written by the database, and an application
// clock that runs a few milliseconds behind would otherwise make a short grace
// period collect nothing at all.
func (s *Store) MarkUnreferencedChunks(ctx context.Context, tenantID string, minAge time.Duration, limit int) ([]*store.Chunk, error) {
	return markUnreferencedChunks(ctx, s.pool, tenantID, minAge, limit)
}

// MarkUnreferencedChunks marks unreferenced chunks within a transaction.
func (t *Tx) MarkUnreferencedChunks(ctx context.Context, tenantID string, minAge time.Duration, limit int) ([]*store.Chunk, error) {
	return markUnreferencedChunks(ctx, t.tx, tenantID, minAge, limit)
}

func markUnreferencedChunks(ctx context.Context, q querier, tenantID string, minAge time.Duration, limit int) ([]*store.Chunk, error) {
	if limit <= 0 {
		limit = 100
	}

	// FOR UPDATE makes a concurrent IncrementChunkRef wait; the ref_count = 0
	// predicate is repeated on the UPDATE so that a reference committed between
	// the snapshot and the lock drops the row instead of marking a live chunk.
	query := `
		WITH candidates AS (
			SELECT hash FROM chunks
			WHERE tenant_id = $1
			  AND ref_count = 0
			  AND deleted_at IS NULL
			  AND created_at <= NOW() - ($2::double precision * INTERVAL '1 second')
			ORDER BY created_at ASC
			LIMIT $3
			FOR UPDATE SKIP LOCKED
		)
		UPDATE chunks c
		SET deleted_at = NOW()
		FROM candidates cd
		WHERE c.tenant_id = $1
		  AND c.hash = cd.hash
		  AND c.ref_count = 0
		  AND c.deleted_at IS NULL
		RETURNING c.hash, c.tenant_id, c.size, c.ref_count, c.deleted_at, c.created_at`

	rows, err := q.Query(ctx, query, tenantID, minAge.Seconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("marking unreferenced chunks: %w", err)
	}
	defer rows.Close()

	var chunks []*store.Chunk
	for rows.Next() {
		chunk, err := scanChunkFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating marked chunks: %w", err)
	}

	return chunks, nil
}

// ListChunkTenants returns every tenant that currently holds chunks.
//
// Garbage collection is per-tenant because chunks are: the primary key is
// (tenant_id, hash) and every GC statement filters on it. A collector that only
// knows one tenant id silently leaves every other tenant's garbage in place.
func (s *Store) ListChunkTenants(ctx context.Context) ([]string, error) {
	return listChunkTenants(ctx, s.pool)
}

// ListChunkTenants returns the tenants holding chunks, within a transaction.
func (t *Tx) ListChunkTenants(ctx context.Context) ([]string, error) {
	return listChunkTenants(ctx, t.tx)
}

func listChunkTenants(ctx context.Context, q querier) ([]string, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT tenant_id FROM chunks ORDER BY tenant_id`)
	if err != nil {
		return nil, fmt.Errorf("listing chunk tenants: %w", err)
	}
	defer rows.Close()

	var tenants []string
	for rows.Next() {
		var tenant string
		if err := rows.Scan(&tenant); err != nil {
			return nil, fmt.Errorf("scanning tenant: %w", err)
		}
		tenants = append(tenants, tenant)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tenants: %w", err)
	}

	return tenants, nil
}

// ListSweepableChunks returns chunks that are still unreferenced and whose
// grace period has expired.
//
// Unlike ListSoftDeletedChunks it excludes chunks that were referenced again
// after being marked, which is the set the old sweep happily deleted.
func (s *Store) ListSweepableChunks(ctx context.Context, tenantID string, minAge time.Duration, limit int) ([]*store.Chunk, error) {
	return listSweepableChunks(ctx, s.pool, tenantID, minAge, limit)
}

// ListSweepableChunks lists sweepable chunks within a transaction.
func (t *Tx) ListSweepableChunks(ctx context.Context, tenantID string, minAge time.Duration, limit int) ([]*store.Chunk, error) {
	return listSweepableChunks(ctx, t.tx, tenantID, minAge, limit)
}

func listSweepableChunks(ctx context.Context, q querier, tenantID string, minAge time.Duration, limit int) ([]*store.Chunk, error) {
	if limit <= 0 {
		limit = 100
	}

	query := fmt.Sprintf(`
		SELECT %s FROM chunks
		WHERE tenant_id = $1
		  AND ref_count = 0
		  AND deleted_at IS NOT NULL
		  AND deleted_at < NOW() - ($2::double precision * INTERVAL '1 second')
		ORDER BY deleted_at ASC
		LIMIT $3`,
		chunkColumns)

	rows, err := q.Query(ctx, query, tenantID, minAge.Seconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("querying sweepable chunks: %w", err)
	}
	defer rows.Close()

	var chunks []*store.Chunk
	for rows.Next() {
		chunk, err := scanChunkFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating chunks: %w", err)
	}

	return chunks, nil
}

// DeleteChunkIfUnreferenced deletes a chunk only while it is still unreferenced
// and still marked. It reports whether the row was actually removed, so the
// caller knows not to delete the blob of a chunk that was resurrected under it.
func (s *Store) DeleteChunkIfUnreferenced(ctx context.Context, tenantID, hash string, minAge time.Duration) (bool, error) {
	return deleteChunkIfUnreferenced(ctx, s.pool, tenantID, hash, minAge)
}

// DeleteChunkIfUnreferenced deletes a chunk within a transaction.
func (t *Tx) DeleteChunkIfUnreferenced(ctx context.Context, tenantID, hash string, minAge time.Duration) (bool, error) {
	return deleteChunkIfUnreferenced(ctx, t.tx, tenantID, hash, minAge)
}

func deleteChunkIfUnreferenced(ctx context.Context, q querier, tenantID, hash string, minAge time.Duration) (bool, error) {
	query := `
		DELETE FROM chunks
		WHERE tenant_id = $1
		  AND hash = $2
		  AND ref_count = 0
		  AND deleted_at IS NOT NULL
		  AND deleted_at < NOW() - ($3::double precision * INTERVAL '1 second')`

	result, err := q.Exec(ctx, query, tenantID, hash, minAge.Seconds())
	if err != nil {
		return false, handlePgError(err, "chunk", hash)
	}

	return result.RowsAffected() > 0, nil
}

func scanChunkFromRows(rows pgx.Rows) (*store.Chunk, error) {
	var c store.Chunk
	err := rows.Scan(
		&c.Hash, &c.TenantID, &c.Size, &c.RefCount, &c.DeletedAt, &c.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// =============================================================================
// Manifest CRUD
// =============================================================================

// CreateManifest creates a new workspace manifest.
func (s *Store) CreateManifest(ctx context.Context, manifest *store.Manifest) error {
	return createManifest(ctx, s.pool, manifest)
}

// CreateManifest creates a new manifest within a transaction.
func (t *Tx) CreateManifest(ctx context.Context, manifest *store.Manifest) error {
	return createManifest(ctx, t.tx, manifest)
}

func createManifest(ctx context.Context, q querier, manifest *store.Manifest) error {
	if manifest.ID == "" {
		manifest.ID = id.Manifest()
	}

	query := `
		INSERT INTO manifests (
			id, workspace_id, parent_id, total_size, single_chunk, chunk_hash,
			chunk_count, files_json, tenant_id, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, NOW()
		)
		RETURNING created_at`

	err := q.QueryRow(ctx, query,
		manifest.ID, manifest.WorkspaceID, manifest.ParentID, manifest.TotalSize,
		manifest.SingleChunk, manifest.ChunkHash, manifest.ChunkCount,
		manifest.FilesJSON, manifest.TenantID,
	).Scan(&manifest.CreatedAt)

	if err != nil {
		return handlePgError(err, "manifest", manifest.ID)
	}
	return nil
}

// GetManifest retrieves a manifest by ID.
func (s *Store) GetManifest(ctx context.Context, manifestID string) (*store.Manifest, error) {
	return getManifest(ctx, s.pool, manifestID)
}

// GetManifest retrieves a manifest by ID within a transaction.
func (t *Tx) GetManifest(ctx context.Context, manifestID string) (*store.Manifest, error) {
	return getManifest(ctx, t.tx, manifestID)
}

func getManifest(ctx context.Context, q querier, manifestID string) (*store.Manifest, error) {
	query := fmt.Sprintf(`SELECT %s FROM manifests WHERE id = $1`, manifestColumns)
	row := q.QueryRow(ctx, query, manifestID)
	return scanManifest(row, manifestID)
}

// GetLatestManifest retrieves the most recent manifest for a workspace.
func (s *Store) GetLatestManifest(ctx context.Context, workspaceID string) (*store.Manifest, error) {
	return getLatestManifest(ctx, s.pool, workspaceID)
}

// GetLatestManifest retrieves the most recent manifest within a transaction.
func (t *Tx) GetLatestManifest(ctx context.Context, workspaceID string) (*store.Manifest, error) {
	return getLatestManifest(ctx, t.tx, workspaceID)
}

func getLatestManifest(ctx context.Context, q querier, workspaceID string) (*store.Manifest, error) {
	query := fmt.Sprintf(`
		SELECT %s FROM manifests
		WHERE workspace_id = $1
		ORDER BY created_at DESC
		LIMIT 1`,
		manifestColumns)
	row := q.QueryRow(ctx, query, workspaceID)
	return scanManifest(row, workspaceID)
}

// ListManifests retrieves manifests for a workspace.
func (s *Store) ListManifests(ctx context.Context, workspaceID string, limit int) ([]*store.Manifest, error) {
	return listManifests(ctx, s.pool, workspaceID, limit)
}

// ListManifests retrieves manifests within a transaction.
func (t *Tx) ListManifests(ctx context.Context, workspaceID string, limit int) ([]*store.Manifest, error) {
	return listManifests(ctx, t.tx, workspaceID, limit)
}

func listManifests(ctx context.Context, q querier, workspaceID string, limit int) ([]*store.Manifest, error) {
	if limit <= 0 {
		limit = 50
	}

	query := fmt.Sprintf(`
		SELECT %s FROM manifests
		WHERE workspace_id = $1
		ORDER BY created_at DESC
		LIMIT $2`,
		manifestColumns)

	rows, err := q.Query(ctx, query, workspaceID, limit)
	if err != nil {
		return nil, fmt.Errorf("querying manifests: %w", err)
	}
	defer rows.Close()

	var manifests []*store.Manifest
	for rows.Next() {
		manifest, err := scanManifestFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning manifest: %w", err)
		}
		manifests = append(manifests, manifest)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating manifests: %w", err)
	}

	return manifests, nil
}

// DeleteManifest deletes a manifest.
func (s *Store) DeleteManifest(ctx context.Context, manifestID string) error {
	return deleteManifest(ctx, s.pool, manifestID)
}

// DeleteManifest deletes a manifest within a transaction.
func (t *Tx) DeleteManifest(ctx context.Context, manifestID string) error {
	return deleteManifest(ctx, t.tx, manifestID)
}

func deleteManifest(ctx context.Context, q querier, manifestID string) error {
	query := `DELETE FROM manifests WHERE id = $1`
	result, err := q.Exec(ctx, query, manifestID)
	if err != nil {
		return handlePgError(err, "manifest", manifestID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "manifest", ID: manifestID}
	}

	return nil
}

// DeleteManifestsByWorkspace deletes all manifests for a workspace.
func (s *Store) DeleteManifestsByWorkspace(ctx context.Context, workspaceID string) error {
	return deleteManifestsByWorkspace(ctx, s.pool, workspaceID)
}

// DeleteManifestsByWorkspace deletes manifests within a transaction.
func (t *Tx) DeleteManifestsByWorkspace(ctx context.Context, workspaceID string) error {
	return deleteManifestsByWorkspace(ctx, t.tx, workspaceID)
}

func deleteManifestsByWorkspace(ctx context.Context, q querier, workspaceID string) error {
	query := `DELETE FROM manifests WHERE workspace_id = $1`
	_, err := q.Exec(ctx, query, workspaceID)
	if err != nil {
		return handlePgError(err, "manifest", workspaceID)
	}
	return nil
}

func scanManifest(row pgx.Row, identifier string) (*store.Manifest, error) {
	var m store.Manifest
	err := row.Scan(
		&m.ID, &m.WorkspaceID, &m.ParentID, &m.TotalSize, &m.SingleChunk, &m.ChunkHash,
		&m.ChunkCount, &m.FilesJSON, &m.TenantID, &m.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "manifest", ID: identifier}
		}
		return nil, fmt.Errorf("scanning manifest: %w", err)
	}
	return &m, nil
}

func scanManifestFromRows(rows pgx.Rows) (*store.Manifest, error) {
	var m store.Manifest
	err := rows.Scan(
		&m.ID, &m.WorkspaceID, &m.ParentID, &m.TotalSize, &m.SingleChunk, &m.ChunkHash,
		&m.ChunkCount, &m.FilesJSON, &m.TenantID, &m.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &m, nil
}
