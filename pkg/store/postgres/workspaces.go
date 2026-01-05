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

// Workspace column list for SELECT queries.
const workspaceColumns = `id, name, persist, storage_type, storage_config, mobility,
	storage_domain, storage_key, storage_size_bytes, last_synced_at,
	disk_quota_mb, tenant_id, labels, annotations,
	expires_at, created_at, updated_at, deleted_at`

// CreateWorkspace creates a new workspace.
func (s *Store) CreateWorkspace(ctx context.Context, workspace *store.Workspace) error {
	return createWorkspace(ctx, s.pool, workspace)
}

// CreateWorkspace creates a new workspace within a transaction.
func (t *Tx) CreateWorkspace(ctx context.Context, workspace *store.Workspace) error {
	return createWorkspace(ctx, t.tx, workspace)
}

func createWorkspace(ctx context.Context, q querier, workspace *store.Workspace) error {
	if workspace.ID == "" {
		workspace.ID = id.Workspace()
	}

	query := `
		INSERT INTO workspaces (
			id, name, persist, storage_type, storage_config, mobility,
			storage_domain, storage_key, storage_size_bytes, last_synced_at,
			disk_quota_mb, tenant_id, labels, annotations,
			expires_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		workspace.ID, workspace.Name, workspace.Persist,
		workspace.StorageType, emptyJSONObject(workspace.StorageConfig), workspace.Mobility,
		workspace.StorageDomain, workspace.StorageKey, workspace.StorageSizeBytes, workspace.LastSyncedAt,
		workspace.DiskQuotaMB, workspace.TenantID, emptyJSONObject(workspace.Labels), emptyJSONObject(workspace.Annotations),
		workspace.ExpiresAt,
	).Scan(&workspace.CreatedAt, &workspace.UpdatedAt)

	if err != nil {
		return handlePgError(err, "workspace", workspace.Name)
	}
	return nil
}

// GetWorkspace retrieves a workspace by ID.
func (s *Store) GetWorkspace(ctx context.Context, workspaceID string) (*store.Workspace, error) {
	return getWorkspace(ctx, s.pool, workspaceID)
}

// GetWorkspace retrieves a workspace by ID within a transaction.
func (t *Tx) GetWorkspace(ctx context.Context, workspaceID string) (*store.Workspace, error) {
	return getWorkspace(ctx, t.tx, workspaceID)
}

func getWorkspace(ctx context.Context, q querier, workspaceID string) (*store.Workspace, error) {
	query := fmt.Sprintf(`SELECT %s FROM workspaces WHERE id = $1`, workspaceColumns)
	row := q.QueryRow(ctx, query, workspaceID)
	return scanWorkspace(row, workspaceID)
}

// ListWorkspaces retrieves workspaces with optional filtering.
func (s *Store) ListWorkspaces(ctx context.Context, opts store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	return listWorkspaces(ctx, s.pool, opts)
}

// ListWorkspaces retrieves workspaces within a transaction.
func (t *Tx) ListWorkspaces(ctx context.Context, opts store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	return listWorkspaces(ctx, t.tx, opts)
}

func listWorkspaces(ctx context.Context, q querier, opts store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	var conditions []string
	var args []any
	argNum := 1

	// Build WHERE clauses
	if !opts.IncludeDeleted {
		conditions = append(conditions, "deleted_at IS NULL")
	}
	if opts.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argNum))
		args = append(args, *opts.TenantID)
		argNum++
	}
	// TODO: Add label filtering with JSONB operators

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy := "created_at"
	if opts.OrderBy != "" {
		orderBy = opts.OrderBy
	}
	orderDir := "ASC"
	if opts.OrderDesc {
		orderDir = "DESC"
	}

	// Count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM workspaces %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting workspaces: %w", err)
	}

	// Data query - fetch one extra to determine HasMore
	dataQuery := fmt.Sprintf(`
		SELECT %s FROM workspaces %s
		ORDER BY %s %s
		LIMIT $%d`,
		workspaceColumns, whereClause, orderBy, orderDir, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying workspaces: %w", err)
	}
	defer rows.Close()

	var workspaces []*store.Workspace
	for rows.Next() {
		workspace, err := scanWorkspaceFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning workspace: %w", err)
		}
		workspaces = append(workspaces, workspace)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating workspaces: %w", err)
	}

	hasMore := len(workspaces) > limit
	if hasMore {
		workspaces = workspaces[:limit]
	}

	return &store.ListResult[store.Workspace]{
		Items:      workspaces,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateWorkspace updates workspace fields.
func (s *Store) UpdateWorkspace(ctx context.Context, workspaceID string, updates store.WorkspaceUpdates) error {
	return updateWorkspace(ctx, s.pool, workspaceID, updates)
}

// UpdateWorkspace updates workspace fields within a transaction.
func (t *Tx) UpdateWorkspace(ctx context.Context, workspaceID string, updates store.WorkspaceUpdates) error {
	return updateWorkspace(ctx, t.tx, workspaceID, updates)
}

func updateWorkspace(ctx context.Context, q querier, workspaceID string, updates store.WorkspaceUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *updates.Name)
		argNum++
	}
	if updates.Persist != nil {
		setClauses = append(setClauses, fmt.Sprintf("persist = $%d", argNum))
		args = append(args, *updates.Persist)
		argNum++
	}
	if updates.StorageConfig != nil {
		setClauses = append(setClauses, fmt.Sprintf("storage_config = $%d", argNum))
		args = append(args, updates.StorageConfig)
		argNum++
	}
	if updates.Mobility != nil {
		setClauses = append(setClauses, fmt.Sprintf("mobility = $%d", argNum))
		args = append(args, *updates.Mobility)
		argNum++
	}
	if updates.StorageDomain != nil {
		setClauses = append(setClauses, fmt.Sprintf("storage_domain = $%d", argNum))
		args = append(args, *updates.StorageDomain)
		argNum++
	}
	if updates.StorageKey != nil {
		setClauses = append(setClauses, fmt.Sprintf("storage_key = $%d", argNum))
		args = append(args, *updates.StorageKey)
		argNum++
	}
	if updates.StorageSizeBytes != nil {
		setClauses = append(setClauses, fmt.Sprintf("storage_size_bytes = $%d", argNum))
		args = append(args, *updates.StorageSizeBytes)
		argNum++
	}
	if updates.LastSyncedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_synced_at = $%d", argNum))
		args = append(args, *updates.LastSyncedAt)
		argNum++
	}
	if updates.DiskQuotaMB != nil {
		setClauses = append(setClauses, fmt.Sprintf("disk_quota_mb = $%d", argNum))
		args = append(args, *updates.DiskQuotaMB)
		argNum++
	}
	if updates.Labels != nil {
		setClauses = append(setClauses, fmt.Sprintf("labels = $%d", argNum))
		args = append(args, updates.Labels)
		argNum++
	}
	if updates.Annotations != nil {
		setClauses = append(setClauses, fmt.Sprintf("annotations = $%d", argNum))
		args = append(args, updates.Annotations)
		argNum++
	}
	if updates.ExpiresAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("expires_at = $%d", argNum))
		args = append(args, *updates.ExpiresAt)
		argNum++
	}
	if updates.DeletedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("deleted_at = $%d", argNum))
		args = append(args, *updates.DeletedAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil // Nothing to update
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE workspaces SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, workspaceID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "workspace", workspaceID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "workspace", ID: workspaceID}
	}

	return nil
}

// DeleteWorkspace soft-deletes a workspace by setting deleted_at.
func (s *Store) DeleteWorkspace(ctx context.Context, workspaceID string) error {
	return deleteWorkspace(ctx, s.pool, workspaceID)
}

// DeleteWorkspace soft-deletes a workspace within a transaction.
func (t *Tx) DeleteWorkspace(ctx context.Context, workspaceID string) error {
	return deleteWorkspace(ctx, t.tx, workspaceID)
}

func deleteWorkspace(ctx context.Context, q querier, workspaceID string) error {
	// Soft delete by setting deleted_at
	query := `UPDATE workspaces SET deleted_at = NOW(), updated_at = NOW() WHERE id = $1 AND deleted_at IS NULL`
	result, err := q.Exec(ctx, query, workspaceID)
	if err != nil {
		return handlePgError(err, "workspace", workspaceID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "workspace", ID: workspaceID}
	}

	return nil
}

// scanWorkspace scans a single row into a Workspace.
func scanWorkspace(row pgx.Row, identifier string) (*store.Workspace, error) {
	var w store.Workspace
	err := row.Scan(
		&w.ID, &w.Name, &w.Persist, &w.StorageType, &w.StorageConfig, &w.Mobility,
		&w.StorageDomain, &w.StorageKey, &w.StorageSizeBytes, &w.LastSyncedAt,
		&w.DiskQuotaMB, &w.TenantID, &w.Labels, &w.Annotations,
		&w.ExpiresAt, &w.CreatedAt, &w.UpdatedAt, &w.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "workspace", ID: identifier}
		}
		return nil, fmt.Errorf("scanning workspace: %w", err)
	}
	return &w, nil
}

// scanWorkspaceFromRows scans a rows iterator into a Workspace.
func scanWorkspaceFromRows(rows pgx.Rows) (*store.Workspace, error) {
	var w store.Workspace
	err := rows.Scan(
		&w.ID, &w.Name, &w.Persist, &w.StorageType, &w.StorageConfig, &w.Mobility,
		&w.StorageDomain, &w.StorageKey, &w.StorageSizeBytes, &w.LastSyncedAt,
		&w.DiskQuotaMB, &w.TenantID, &w.Labels, &w.Annotations,
		&w.ExpiresAt, &w.CreatedAt, &w.UpdatedAt, &w.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &w, nil
}
