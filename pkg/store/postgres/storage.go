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

// Snapshot column list for SELECT queries.
const snapshotColumns = `id, runner_id, name, provider_snapshot_id, storage_key,
	tenant_id, size_bytes, labels, annotations, created_at, expires_at`

// Tunnel column list for SELECT queries.
//
//nolint:gosec // G101: This is a column list, not hardcoded credentials
const tunnelColumns = `id, session_id, runner_id, type, direction, local_port, public_url, is_public,
	token_hash, token_prefix, hash_version, tenant_id, created_at, updated_at, expires_at, closed_at`

// DataKey column list for SELECT queries.
const dataKeyColumns = `id, resource_type, resource_id, dek_encrypted, algorithm, kek_id,
	tenant_id, created_at, rotated_at, updated_at`

// =============================================================================
// Snapshot CRUD
// =============================================================================

// CreateSnapshot creates a new snapshot.
func (s *Store) CreateSnapshot(ctx context.Context, snapshot *store.Snapshot) error {
	return createSnapshot(ctx, s.pool, snapshot)
}

// CreateSnapshot creates a new snapshot within a transaction.
func (t *Tx) CreateSnapshot(ctx context.Context, snapshot *store.Snapshot) error {
	return createSnapshot(ctx, t.tx, snapshot)
}

func createSnapshot(ctx context.Context, q querier, snapshot *store.Snapshot) error {
	if snapshot.ID == "" {
		snapshot.ID = id.Snapshot()
	}

	query := `
		INSERT INTO snapshots (
			id, runner_id, name, provider_snapshot_id, storage_key,
			tenant_id, size_bytes, labels, annotations, created_at, expires_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, NOW(), $10
		)
		RETURNING created_at`

	err := q.QueryRow(ctx, query,
		snapshot.ID, snapshot.RunnerID, snapshot.Name, snapshot.ProviderSnapshotID, snapshot.StorageKey,
		snapshot.TenantID, snapshot.SizeBytes, emptyJSONObject(snapshot.Labels), emptyJSONObject(snapshot.Annotations),
		snapshot.ExpiresAt,
	).Scan(&snapshot.CreatedAt)

	if err != nil {
		return handlePgError(err, "snapshot", snapshot.Name)
	}
	return nil
}

// GetSnapshot retrieves a snapshot by ID.
func (s *Store) GetSnapshot(ctx context.Context, snapshotID string) (*store.Snapshot, error) {
	return getSnapshot(ctx, s.pool, snapshotID)
}

// GetSnapshot retrieves a snapshot by ID within a transaction.
func (t *Tx) GetSnapshot(ctx context.Context, snapshotID string) (*store.Snapshot, error) {
	return getSnapshot(ctx, t.tx, snapshotID)
}

func getSnapshot(ctx context.Context, q querier, snapshotID string) (*store.Snapshot, error) {
	query := fmt.Sprintf(`SELECT %s FROM snapshots WHERE id = $1`, snapshotColumns)
	row := q.QueryRow(ctx, query, snapshotID)
	return scanSnapshot(row, snapshotID)
}

// GetSnapshotByRunnerAndName retrieves a snapshot by runner ID and name.
func (s *Store) GetSnapshotByRunnerAndName(ctx context.Context, runnerID, name string) (*store.Snapshot, error) {
	return getSnapshotByRunnerAndName(ctx, s.pool, runnerID, name)
}

// GetSnapshotByRunnerAndName retrieves a snapshot within a transaction.
func (t *Tx) GetSnapshotByRunnerAndName(ctx context.Context, runnerID, name string) (*store.Snapshot, error) {
	return getSnapshotByRunnerAndName(ctx, t.tx, runnerID, name)
}

func getSnapshotByRunnerAndName(ctx context.Context, q querier, runnerID, name string) (*store.Snapshot, error) {
	query := fmt.Sprintf(`SELECT %s FROM snapshots WHERE runner_id = $1 AND name = $2`, snapshotColumns)
	row := q.QueryRow(ctx, query, runnerID, name)
	return scanSnapshot(row, fmt.Sprintf("%s/%s", runnerID, name))
}

// ListSnapshots retrieves snapshots with optional filtering.
func (s *Store) ListSnapshots(ctx context.Context, opts store.ListSnapshotsOptions) (*store.ListResult[store.Snapshot], error) {
	return listSnapshots(ctx, s.pool, opts)
}

// ListSnapshots retrieves snapshots within a transaction.
func (t *Tx) ListSnapshots(ctx context.Context, opts store.ListSnapshotsOptions) (*store.ListResult[store.Snapshot], error) {
	return listSnapshots(ctx, t.tx, opts)
}

func listSnapshots(ctx context.Context, q querier, opts store.ListSnapshotsOptions) (*store.ListResult[store.Snapshot], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.RunnerID != nil {
		conditions = append(conditions, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, *opts.RunnerID)
		argNum++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	page, err := snapshotSortColumns.page(opts.BaseListOptions, argNum)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM snapshots %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting snapshots: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM snapshots %s
		ORDER BY %s
		LIMIT $%d`,
		snapshotColumns, page.where(whereClause), page.orderBy, page.limitArg(argNum))
	dataArgs := append(args, page.args...) //nolint:gocritic // intentionally creating new slice
	dataArgs = append(dataArgs, limit+1)

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []*store.Snapshot
	for rows.Next() {
		snapshot, err := scanSnapshotFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning snapshot: %w", err)
		}
		snapshots = append(snapshots, snapshot)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating snapshots: %w", err)
	}

	hasMore := len(snapshots) > limit
	if hasMore {
		snapshots = snapshots[:limit]
	}

	var nextCursor string
	if len(snapshots) > 0 {
		last := snapshots[len(snapshots)-1]
		nextCursor = page.nextTime(hasMore, last.CreatedAt, last.ID)
	}

	return &store.ListResult[store.Snapshot]{
		Items:      snapshots,
		TotalCount: totalCount,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// UpdateSnapshot updates snapshot fields.
func (s *Store) UpdateSnapshot(ctx context.Context, snapshotID string, updates store.SnapshotUpdates) error {
	return updateSnapshot(ctx, s.pool, snapshotID, updates)
}

// UpdateSnapshot updates snapshot fields within a transaction.
func (t *Tx) UpdateSnapshot(ctx context.Context, snapshotID string, updates store.SnapshotUpdates) error {
	return updateSnapshot(ctx, t.tx, snapshotID, updates)
}

func updateSnapshot(ctx context.Context, q querier, snapshotID string, updates store.SnapshotUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *updates.Name)
		argNum++
	}
	if updates.StorageKey != nil {
		setClauses = append(setClauses, fmt.Sprintf("storage_key = $%d", argNum))
		args = append(args, *updates.StorageKey)
		argNum++
	}
	if updates.SizeBytes != nil {
		setClauses = append(setClauses, fmt.Sprintf("size_bytes = $%d", argNum))
		args = append(args, *updates.SizeBytes)
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

	if len(setClauses) == 0 {
		return nil
	}

	query := fmt.Sprintf(`UPDATE snapshots SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, snapshotID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "snapshot", snapshotID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "snapshot", ID: snapshotID}
	}

	return nil
}

// DeleteSnapshot deletes a snapshot.
func (s *Store) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	return deleteSnapshot(ctx, s.pool, snapshotID)
}

// DeleteSnapshot deletes a snapshot within a transaction.
func (t *Tx) DeleteSnapshot(ctx context.Context, snapshotID string) error {
	return deleteSnapshot(ctx, t.tx, snapshotID)
}

func deleteSnapshot(ctx context.Context, q querier, snapshotID string) error {
	query := `DELETE FROM snapshots WHERE id = $1`
	result, err := q.Exec(ctx, query, snapshotID)
	if err != nil {
		return handlePgError(err, "snapshot", snapshotID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "snapshot", ID: snapshotID}
	}

	return nil
}

func scanSnapshot(row pgx.Row, identifier string) (*store.Snapshot, error) {
	var s store.Snapshot
	err := row.Scan(
		&s.ID, &s.RunnerID, &s.Name, &s.ProviderSnapshotID, &s.StorageKey,
		&s.TenantID, &s.SizeBytes, &s.Labels, &s.Annotations, &s.CreatedAt, &s.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "snapshot", ID: identifier}
		}
		return nil, fmt.Errorf("scanning snapshot: %w", err)
	}
	return &s, nil
}

func scanSnapshotFromRows(rows pgx.Rows) (*store.Snapshot, error) {
	var s store.Snapshot
	err := rows.Scan(
		&s.ID, &s.RunnerID, &s.Name, &s.ProviderSnapshotID, &s.StorageKey,
		&s.TenantID, &s.SizeBytes, &s.Labels, &s.Annotations, &s.CreatedAt, &s.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// =============================================================================
// Tunnel CRUD
// =============================================================================

// CreateTunnel creates a new tunnel.
func (s *Store) CreateTunnel(ctx context.Context, tunnel *store.Tunnel) error {
	return createTunnel(ctx, s.pool, tunnel)
}

// CreateTunnel creates a new tunnel within a transaction.
func (t *Tx) CreateTunnel(ctx context.Context, tunnel *store.Tunnel) error {
	return createTunnel(ctx, t.tx, tunnel)
}

func createTunnel(ctx context.Context, q querier, tunnel *store.Tunnel) error {
	if tunnel.ID == "" {
		tunnel.ID = id.Tunnel()
	}

	query := `
		INSERT INTO tunnels (
			id, session_id, runner_id, type, direction, local_port, public_url, is_public,
			token_hash, token_prefix, hash_version, tenant_id, created_at, updated_at, expires_at, closed_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW(), $13, $14
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		tunnel.ID, tunnel.SessionID, tunnel.RunnerID, tunnel.Type, tunnel.Direction,
		tunnel.LocalPort, tunnel.PublicURL, tunnel.IsPublic, tunnel.TokenHash, tunnel.TokenPrefix,
		tunnel.HashVersion, tunnel.TenantID, tunnel.ExpiresAt, tunnel.ClosedAt,
	).Scan(&tunnel.CreatedAt, &tunnel.UpdatedAt)

	if err != nil {
		return handlePgError(err, "tunnel", tunnel.ID)
	}
	return nil
}

// GetTunnel retrieves a tunnel by ID.
func (s *Store) GetTunnel(ctx context.Context, tunnelID string) (*store.Tunnel, error) {
	return getTunnel(ctx, s.pool, tunnelID)
}

// GetTunnel retrieves a tunnel by ID within a transaction.
func (t *Tx) GetTunnel(ctx context.Context, tunnelID string) (*store.Tunnel, error) {
	return getTunnel(ctx, t.tx, tunnelID)
}

func getTunnel(ctx context.Context, q querier, tunnelID string) (*store.Tunnel, error) {
	query := fmt.Sprintf(`SELECT %s FROM tunnels WHERE id = $1`, tunnelColumns)
	row := q.QueryRow(ctx, query, tunnelID)
	return scanTunnel(row, tunnelID)
}

// GetTunnelByTokenHash retrieves a tunnel by token hash.
func (s *Store) GetTunnelByTokenHash(ctx context.Context, hash string) (*store.Tunnel, error) {
	return getTunnelByTokenHash(ctx, s.pool, hash)
}

// GetTunnelByTokenHash retrieves a tunnel within a transaction.
func (t *Tx) GetTunnelByTokenHash(ctx context.Context, hash string) (*store.Tunnel, error) {
	return getTunnelByTokenHash(ctx, t.tx, hash)
}

func getTunnelByTokenHash(ctx context.Context, q querier, hash string) (*store.Tunnel, error) {
	query := fmt.Sprintf(`SELECT %s FROM tunnels WHERE token_hash = $1`, tunnelColumns)
	row := q.QueryRow(ctx, query, hash)
	return scanTunnel(row, hash)
}

// ListTunnels retrieves tunnels with optional filtering.
func (s *Store) ListTunnels(ctx context.Context, opts store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	return listTunnels(ctx, s.pool, opts)
}

// ListTunnels retrieves tunnels within a transaction.
func (t *Tx) ListTunnels(ctx context.Context, opts store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	return listTunnels(ctx, t.tx, opts)
}

func listTunnels(ctx context.Context, q querier, opts store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.SessionID != nil {
		conditions = append(conditions, fmt.Sprintf("session_id = $%d", argNum))
		args = append(args, *opts.SessionID)
		argNum++
	}
	if opts.RunnerID != nil {
		conditions = append(conditions, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, *opts.RunnerID)
		argNum++
	}
	if len(opts.Type) > 0 {
		conditions = append(conditions, fmt.Sprintf("type = ANY($%d)", argNum))
		args = append(args, opts.Type)
		argNum++
	}
	if opts.Direction != nil {
		conditions = append(conditions, fmt.Sprintf("direction = $%d", argNum))
		args = append(args, *opts.Direction)
		argNum++
	}
	if !opts.IncludeClosed {
		conditions = append(conditions, "closed_at IS NULL")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	page, err := tunnelSortColumns.page(opts.BaseListOptions, argNum)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM tunnels %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting tunnels: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM tunnels %s
		ORDER BY %s
		LIMIT $%d`,
		tunnelColumns, page.where(whereClause), page.orderBy, page.limitArg(argNum))
	dataArgs := append(args, page.args...) //nolint:gocritic // intentionally creating new slice
	dataArgs = append(dataArgs, limit+1)

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying tunnels: %w", err)
	}
	defer rows.Close()

	var tunnels []*store.Tunnel
	for rows.Next() {
		tunnel, err := scanTunnelFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning tunnel: %w", err)
		}
		tunnels = append(tunnels, tunnel)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tunnels: %w", err)
	}

	hasMore := len(tunnels) > limit
	if hasMore {
		tunnels = tunnels[:limit]
	}

	var nextCursor string
	if len(tunnels) > 0 {
		last := tunnels[len(tunnels)-1]
		nextCursor = page.nextTime(hasMore, last.CreatedAt, last.ID)
	}

	return &store.ListResult[store.Tunnel]{
		Items:      tunnels,
		TotalCount: totalCount,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// UpdateTunnel updates tunnel fields.
func (s *Store) UpdateTunnel(ctx context.Context, tunnelID string, updates store.TunnelUpdates) error {
	return updateTunnel(ctx, s.pool, tunnelID, updates)
}

// UpdateTunnel updates tunnel fields within a transaction.
func (t *Tx) UpdateTunnel(ctx context.Context, tunnelID string, updates store.TunnelUpdates) error {
	return updateTunnel(ctx, t.tx, tunnelID, updates)
}

func updateTunnel(ctx context.Context, q querier, tunnelID string, updates store.TunnelUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.RunnerID != nil {
		setClauses = append(setClauses, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, *updates.RunnerID)
		argNum++
	}
	if updates.PublicURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("public_url = $%d", argNum))
		args = append(args, *updates.PublicURL)
		argNum++
	}
	if updates.ClosedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("closed_at = $%d", argNum))
		args = append(args, *updates.ClosedAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE tunnels SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, tunnelID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "tunnel", tunnelID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "tunnel", ID: tunnelID}
	}

	return nil
}

// DeleteTunnel deletes a tunnel.
func (s *Store) DeleteTunnel(ctx context.Context, tunnelID string) error {
	return deleteTunnel(ctx, s.pool, tunnelID)
}

// DeleteTunnel deletes a tunnel within a transaction.
func (t *Tx) DeleteTunnel(ctx context.Context, tunnelID string) error {
	return deleteTunnel(ctx, t.tx, tunnelID)
}

func deleteTunnel(ctx context.Context, q querier, tunnelID string) error {
	query := `DELETE FROM tunnels WHERE id = $1`
	result, err := q.Exec(ctx, query, tunnelID)
	if err != nil {
		return handlePgError(err, "tunnel", tunnelID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "tunnel", ID: tunnelID}
	}

	return nil
}

func scanTunnel(row pgx.Row, identifier string) (*store.Tunnel, error) {
	var t store.Tunnel
	err := row.Scan(
		&t.ID, &t.SessionID, &t.RunnerID, &t.Type, &t.Direction, &t.LocalPort, &t.PublicURL, &t.IsPublic,
		&t.TokenHash, &t.TokenPrefix, &t.HashVersion, &t.TenantID, &t.CreatedAt, &t.UpdatedAt,
		&t.ExpiresAt, &t.ClosedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "tunnel", ID: identifier}
		}
		return nil, fmt.Errorf("scanning tunnel: %w", err)
	}
	return &t, nil
}

func scanTunnelFromRows(rows pgx.Rows) (*store.Tunnel, error) {
	var t store.Tunnel
	err := rows.Scan(
		&t.ID, &t.SessionID, &t.RunnerID, &t.Type, &t.Direction, &t.LocalPort, &t.PublicURL, &t.IsPublic,
		&t.TokenHash, &t.TokenPrefix, &t.HashVersion, &t.TenantID, &t.CreatedAt, &t.UpdatedAt,
		&t.ExpiresAt, &t.ClosedAt,
	)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// =============================================================================
// Tunnel Extension Methods
// =============================================================================

// CloseSessionTunnels closes all active tunnels for a session by setting closed_at.
// Returns the number of tunnels closed.
func (s *Store) CloseSessionTunnels(ctx context.Context, sessionID string) (int64, error) {
	query := `
		UPDATE tunnels
		SET closed_at = NOW(), updated_at = NOW()
		WHERE session_id = $1 AND closed_at IS NULL`

	result, err := s.pool.Exec(ctx, query, sessionID)
	if err != nil {
		return 0, fmt.Errorf("closing session tunnels: %w", err)
	}

	return result.RowsAffected(), nil
}

// DeleteExpiredTunnels removes all tunnels that have expired.
// Returns the number of tunnels deleted.
func (s *Store) DeleteExpiredTunnels(ctx context.Context) (int64, error) {
	query := `DELETE FROM tunnels WHERE expires_at < NOW()`

	result, err := s.pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("deleting expired tunnels: %w", err)
	}

	return result.RowsAffected(), nil
}

// GetTunnelsByRunner returns all active tunnels for a runner.
func (s *Store) GetTunnelsByRunner(ctx context.Context, runnerID string) ([]*store.Tunnel, error) {
	query := fmt.Sprintf(`
		SELECT %s FROM tunnels
		WHERE runner_id = $1 AND closed_at IS NULL
		ORDER BY created_at ASC`,
		tunnelColumns)

	rows, err := s.pool.Query(ctx, query, runnerID)
	if err != nil {
		return nil, fmt.Errorf("querying tunnels by runner: %w", err)
	}
	defer rows.Close()

	var tunnels []*store.Tunnel
	for rows.Next() {
		tunnel, err := scanTunnelFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning tunnel: %w", err)
		}
		tunnels = append(tunnels, tunnel)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tunnels: %w", err)
	}

	return tunnels, nil
}

// GetActiveTunnelCount returns the count of active tunnels.
// Active means not closed and not expired.
func (s *Store) GetActiveTunnelCount(ctx context.Context) (int64, error) {
	query := `SELECT COUNT(*) FROM tunnels WHERE closed_at IS NULL AND expires_at > NOW()`

	var count int64
	if err := s.pool.QueryRow(ctx, query).Scan(&count); err != nil {
		return 0, fmt.Errorf("counting active tunnels: %w", err)
	}

	return count, nil
}

// =============================================================================
// DataKey CRUD
// =============================================================================

// CreateDataKey creates a new data key.
func (s *Store) CreateDataKey(ctx context.Context, key *store.DataKey) error {
	return createDataKey(ctx, s.pool, key)
}

// CreateDataKey creates a new data key within a transaction.
func (t *Tx) CreateDataKey(ctx context.Context, key *store.DataKey) error {
	return createDataKey(ctx, t.tx, key)
}

func createDataKey(ctx context.Context, q querier, key *store.DataKey) error {
	if key.ID == "" {
		key.ID = id.DataKey()
	}

	query := `
		INSERT INTO data_keys (
			id, resource_type, resource_id, dek_encrypted, algorithm, kek_id,
			tenant_id, created_at, rotated_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, NOW(), $8, NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		key.ID, key.ResourceType, key.ResourceID, key.DEKEncrypted, key.Algorithm,
		key.KEKID, key.TenantID, key.RotatedAt,
	).Scan(&key.CreatedAt, &key.UpdatedAt)

	if err != nil {
		return handlePgError(err, "data_key", key.ID)
	}
	return nil
}

// GetDataKey retrieves a data key by ID.
func (s *Store) GetDataKey(ctx context.Context, keyID string) (*store.DataKey, error) {
	return getDataKey(ctx, s.pool, keyID)
}

// GetDataKey retrieves a data key by ID within a transaction.
func (t *Tx) GetDataKey(ctx context.Context, keyID string) (*store.DataKey, error) {
	return getDataKey(ctx, t.tx, keyID)
}

func getDataKey(ctx context.Context, q querier, keyID string) (*store.DataKey, error) {
	query := fmt.Sprintf(`SELECT %s FROM data_keys WHERE id = $1`, dataKeyColumns)
	row := q.QueryRow(ctx, query, keyID)
	return scanDataKey(row, keyID)
}

// GetDataKeyByResource retrieves a data key by resource type and ID.
func (s *Store) GetDataKeyByResource(ctx context.Context, resourceType, resourceID string) (*store.DataKey, error) {
	return getDataKeyByResource(ctx, s.pool, resourceType, resourceID)
}

// GetDataKeyByResource retrieves a data key within a transaction.
func (t *Tx) GetDataKeyByResource(ctx context.Context, resourceType, resourceID string) (*store.DataKey, error) {
	return getDataKeyByResource(ctx, t.tx, resourceType, resourceID)
}

func getDataKeyByResource(ctx context.Context, q querier, resourceType, resourceID string) (*store.DataKey, error) {
	query := fmt.Sprintf(`SELECT %s FROM data_keys WHERE resource_type = $1 AND resource_id = $2`, dataKeyColumns)
	row := q.QueryRow(ctx, query, resourceType, resourceID)
	return scanDataKey(row, fmt.Sprintf("%s/%s", resourceType, resourceID))
}

// UpdateDataKey updates data key fields.
func (s *Store) UpdateDataKey(ctx context.Context, keyID string, updates store.DataKeyUpdates) error {
	return updateDataKey(ctx, s.pool, keyID, updates)
}

// UpdateDataKey updates data key fields within a transaction.
func (t *Tx) UpdateDataKey(ctx context.Context, keyID string, updates store.DataKeyUpdates) error {
	return updateDataKey(ctx, t.tx, keyID, updates)
}

func updateDataKey(ctx context.Context, q querier, keyID string, updates store.DataKeyUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.DEKEncrypted != nil {
		setClauses = append(setClauses, fmt.Sprintf("dek_encrypted = $%d", argNum))
		args = append(args, *updates.DEKEncrypted)
		argNum++
	}
	if updates.RotatedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("rotated_at = $%d", argNum))
		args = append(args, *updates.RotatedAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE data_keys SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, keyID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "data_key", keyID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "data_key", ID: keyID}
	}

	return nil
}

// DeleteDataKey deletes a data key.
func (s *Store) DeleteDataKey(ctx context.Context, keyID string) error {
	return deleteDataKey(ctx, s.pool, keyID)
}

// DeleteDataKey deletes a data key within a transaction.
func (t *Tx) DeleteDataKey(ctx context.Context, keyID string) error {
	return deleteDataKey(ctx, t.tx, keyID)
}

func deleteDataKey(ctx context.Context, q querier, keyID string) error {
	query := `DELETE FROM data_keys WHERE id = $1`
	result, err := q.Exec(ctx, query, keyID)
	if err != nil {
		return handlePgError(err, "data_key", keyID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "data_key", ID: keyID}
	}

	return nil
}

func scanDataKey(row pgx.Row, identifier string) (*store.DataKey, error) {
	var k store.DataKey
	err := row.Scan(
		&k.ID, &k.ResourceType, &k.ResourceID, &k.DEKEncrypted, &k.Algorithm, &k.KEKID,
		&k.TenantID, &k.CreatedAt, &k.RotatedAt, &k.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "data_key", ID: identifier}
		}
		return nil, fmt.Errorf("scanning data_key: %w", err)
	}
	return &k, nil
}
