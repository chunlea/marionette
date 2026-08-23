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

// Runner column list for SELECT queries.
const runnerColumns = `id, name, hostname, status, tainted, taint_reason,
	sandbox_mode, sandbox_types, provider_config_id, provider_instance_id,
	pool_name, profile_id, capabilities, tenant_id, labels, annotations,
	last_seen_at, created_at, updated_at`

// CreateRunner creates a new runner.
func (s *Store) CreateRunner(ctx context.Context, runner *store.Runner) error {
	return createRunner(ctx, s.pool, runner)
}

// CreateRunner creates a new runner within a transaction.
func (t *Tx) CreateRunner(ctx context.Context, runner *store.Runner) error {
	return createRunner(ctx, t.tx, runner)
}

func createRunner(ctx context.Context, q querier, runner *store.Runner) error {
	if runner.ID == "" {
		runner.ID = id.Runner()
	}

	query := `
		INSERT INTO runners (
			id, name, hostname, status, tainted, taint_reason,
			sandbox_mode, sandbox_types, provider_config_id, provider_instance_id,
			pool_name, profile_id, capabilities, tenant_id, labels, annotations,
			last_seen_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		runner.ID, runner.Name, runner.Hostname, runner.Status,
		runner.Tainted, runner.TaintReason, runner.SandboxMode,
		runner.SandboxTypes, runner.ProviderConfigID, runner.ProviderInstanceID,
		runner.PoolName, runner.ProfileID, runner.Capabilities,
		runner.TenantID, emptyJSONObject(runner.Labels), emptyJSONObject(runner.Annotations),
		runner.LastSeenAt,
	).Scan(&runner.CreatedAt, &runner.UpdatedAt)

	if err != nil {
		return handlePgError(err, "runner", runner.Name)
	}
	return nil
}

// GetRunner retrieves a runner by ID.
func (s *Store) GetRunner(ctx context.Context, runnerID string) (*store.Runner, error) {
	return getRunner(ctx, s.pool, runnerID)
}

// GetRunner retrieves a runner by ID within a transaction.
func (t *Tx) GetRunner(ctx context.Context, runnerID string) (*store.Runner, error) {
	return getRunner(ctx, t.tx, runnerID)
}

func getRunner(ctx context.Context, q querier, runnerID string) (*store.Runner, error) {
	query := fmt.Sprintf(`SELECT %s FROM runners WHERE id = $1`, runnerColumns)
	row := q.QueryRow(ctx, query, runnerID)
	return scanRunner(row, runnerID)
}

// GetRunnerByName retrieves a runner by name.
func (s *Store) GetRunnerByName(ctx context.Context, name string) (*store.Runner, error) {
	return getRunnerByName(ctx, s.pool, name)
}

// GetRunnerByName retrieves a runner by name within a transaction.
func (t *Tx) GetRunnerByName(ctx context.Context, name string) (*store.Runner, error) {
	return getRunnerByName(ctx, t.tx, name)
}

func getRunnerByName(ctx context.Context, q querier, name string) (*store.Runner, error) {
	query := fmt.Sprintf(`SELECT %s FROM runners WHERE name = $1`, runnerColumns)
	row := q.QueryRow(ctx, query, name)
	return scanRunner(row, name)
}

// ListRunners retrieves runners with optional filtering.
func (s *Store) ListRunners(ctx context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	return listRunners(ctx, s.pool, opts)
}

// ListRunners retrieves runners within a transaction.
func (t *Tx) ListRunners(ctx context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	return listRunners(ctx, t.tx, opts)
}

func listRunners(ctx context.Context, q querier, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	var conditions []string
	var args []any
	argNum := 1

	// Build WHERE clauses
	if len(opts.Status) > 0 {
		conditions = append(conditions, fmt.Sprintf("status = ANY($%d)", argNum))
		args = append(args, opts.Status)
		argNum++
	}
	if opts.PoolName != nil {
		conditions = append(conditions, fmt.Sprintf("pool_name = $%d", argNum))
		args = append(args, *opts.PoolName)
		argNum++
	}
	if opts.Tainted != nil {
		conditions = append(conditions, fmt.Sprintf("tainted = $%d", argNum))
		args = append(args, *opts.Tainted)
		argNum++
	}
	// TODO: Add label filtering with JSONB operators

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	page, err := runnerSortColumns.page(opts.BaseListOptions, argNum)
	if err != nil {
		return nil, err
	}

	// Count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM runners %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting runners: %w", err)
	}

	// Data query - fetch one extra to determine HasMore
	dataQuery := fmt.Sprintf(`
		SELECT %s FROM runners %s
		ORDER BY %s
		LIMIT $%d`,
		runnerColumns, page.where(whereClause), page.orderBy, page.limitArg(argNum))
	dataArgs := append(args, page.args...) //nolint:gocritic // intentionally creating new slice
	dataArgs = append(dataArgs, limit+1)

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying runners: %w", err)
	}
	defer rows.Close()

	var runners []*store.Runner
	for rows.Next() {
		runner, err := scanRunnerFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning runner: %w", err)
		}
		runners = append(runners, runner)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating runners: %w", err)
	}

	hasMore := len(runners) > limit
	if hasMore {
		runners = runners[:limit]
	}

	var nextCursor string
	if len(runners) > 0 {
		last := runners[len(runners)-1]
		nextCursor = page.nextTime(hasMore, last.CreatedAt, last.ID)
	}

	return &store.ListResult[store.Runner]{
		Items:      runners,
		TotalCount: totalCount,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// UpdateRunner updates runner fields.
func (s *Store) UpdateRunner(ctx context.Context, runnerID string, updates store.RunnerUpdates) error {
	return updateRunner(ctx, s.pool, runnerID, updates)
}

// UpdateRunner updates runner fields within a transaction.
func (t *Tx) UpdateRunner(ctx context.Context, runnerID string, updates store.RunnerUpdates) error {
	return updateRunner(ctx, t.tx, runnerID, updates)
}

func updateRunner(ctx context.Context, q querier, runnerID string, updates store.RunnerUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *updates.Name)
		argNum++
	}
	if updates.Hostname != nil {
		setClauses = append(setClauses, fmt.Sprintf("hostname = $%d", argNum))
		args = append(args, *updates.Hostname)
		argNum++
	}
	if updates.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argNum))
		args = append(args, *updates.Status)
		argNum++
	}
	if updates.Tainted != nil {
		setClauses = append(setClauses, fmt.Sprintf("tainted = $%d", argNum))
		args = append(args, *updates.Tainted)
		argNum++
	}
	if updates.TaintReason != nil {
		setClauses = append(setClauses, fmt.Sprintf("taint_reason = $%d", argNum))
		args = append(args, *updates.TaintReason)
		argNum++
	}
	if updates.SandboxMode != nil {
		setClauses = append(setClauses, fmt.Sprintf("sandbox_mode = $%d", argNum))
		args = append(args, *updates.SandboxMode)
		argNum++
	}
	if updates.SandboxTypes != nil {
		setClauses = append(setClauses, fmt.Sprintf("sandbox_types = $%d", argNum))
		args = append(args, updates.SandboxTypes)
		argNum++
	}
	if updates.ProviderConfigID != nil {
		setClauses = append(setClauses, fmt.Sprintf("provider_config_id = $%d", argNum))
		args = append(args, *updates.ProviderConfigID)
		argNum++
	}
	if updates.ProviderInstanceID != nil {
		setClauses = append(setClauses, fmt.Sprintf("provider_instance_id = $%d", argNum))
		args = append(args, *updates.ProviderInstanceID)
		argNum++
	}
	if updates.PoolName != nil {
		setClauses = append(setClauses, fmt.Sprintf("pool_name = $%d", argNum))
		args = append(args, *updates.PoolName)
		argNum++
	}
	if updates.ProfileID != nil {
		setClauses = append(setClauses, fmt.Sprintf("profile_id = $%d", argNum))
		args = append(args, *updates.ProfileID)
		argNum++
	}
	if updates.Capabilities != nil {
		setClauses = append(setClauses, fmt.Sprintf("capabilities = $%d", argNum))
		args = append(args, updates.Capabilities)
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
	if updates.LastSeenAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_seen_at = $%d", argNum))
		args = append(args, *updates.LastSeenAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil // Nothing to update
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE runners SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, runnerID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "runner", runnerID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "runner", ID: runnerID}
	}

	return nil
}

// DeleteRunner deletes a runner.
func (s *Store) DeleteRunner(ctx context.Context, runnerID string) error {
	return deleteRunner(ctx, s.pool, runnerID)
}

// DeleteRunner deletes a runner within a transaction.
func (t *Tx) DeleteRunner(ctx context.Context, runnerID string) error {
	return deleteRunner(ctx, t.tx, runnerID)
}

func deleteRunner(ctx context.Context, q querier, runnerID string) error {
	query := `DELETE FROM runners WHERE id = $1`
	result, err := q.Exec(ctx, query, runnerID)
	if err != nil {
		return handlePgError(err, "runner", runnerID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "runner", ID: runnerID}
	}

	return nil
}

// scanRunner scans a single row into a Runner.
func scanRunner(row pgx.Row, identifier string) (*store.Runner, error) {
	var r store.Runner
	err := row.Scan(
		&r.ID, &r.Name, &r.Hostname, &r.Status, &r.Tainted, &r.TaintReason,
		&r.SandboxMode, &r.SandboxTypes, &r.ProviderConfigID, &r.ProviderInstanceID,
		&r.PoolName, &r.ProfileID, &r.Capabilities, &r.TenantID,
		&r.Labels, &r.Annotations, &r.LastSeenAt, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "runner", ID: identifier}
		}
		return nil, fmt.Errorf("scanning runner: %w", err)
	}
	return &r, nil
}

// scanRunnerFromRows scans a rows iterator into a Runner.
func scanRunnerFromRows(rows pgx.Rows) (*store.Runner, error) {
	var r store.Runner
	err := rows.Scan(
		&r.ID, &r.Name, &r.Hostname, &r.Status, &r.Tainted, &r.TaintReason,
		&r.SandboxMode, &r.SandboxTypes, &r.ProviderConfigID, &r.ProviderInstanceID,
		&r.PoolName, &r.ProfileID, &r.Capabilities, &r.TenantID,
		&r.Labels, &r.Annotations, &r.LastSeenAt, &r.CreatedAt, &r.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &r, nil
}
