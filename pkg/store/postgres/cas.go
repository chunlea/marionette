package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	query := `UPDATE chunks SET ref_count = ref_count + 1 WHERE tenant_id = $1 AND hash = $2`
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
