package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

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
	return createRunner(ctx, s.db, runner)
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
	return getRunner(ctx, s.db, runnerID)
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
	return getRunnerByName(ctx, s.db, name)
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
	return listRunners(ctx, s.db, opts)
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
	if len(opts.Labels) > 0 {
		// Containment, so the filter means "has at least these labels" rather
		// than "has exactly these" - the Kubernetes-style semantics every
		// caller assumes.
		//
		// This was a TODO for the life of the project, and callers had already
		// been written against it: runner selection passes a profile's os and
		// arch selectors here, and with the filter missing they were silently
		// ignored, so a profile that asked for linux/arm64 would be served the
		// first idle runner of any shape.
		encoded, err := json.Marshal(opts.Labels)
		if err != nil {
			return nil, fmt.Errorf("encoding label filter: %w", err)
		}
		conditions = append(conditions, fmt.Sprintf("labels @> $%d::jsonb", argNum))
		args = append(args, string(encoded))
		argNum++
	}

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
	return updateRunner(ctx, s.db, runnerID, updates)
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
	return deleteRunner(ctx, s.db, runnerID)
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

// ClaimRunner takes an exclusive, leased claim on a runner for a session.
//
// The claim is a single conditional UPDATE, so the database decides the winner:
// concurrent callers serialise on the row lock and exactly one sees a row
// affected. Everything else - listing candidates, checking whether a live
// session already owns the runner - is advisory, because it reads state that
// can change before the caller acts on it.
//
// A claim held longer than lease is taken over. Without that, a server that
// died between claiming and activating would strand the runner permanently.
func (s *Store) ClaimRunner(ctx context.Context, runnerID, sessionID string, lease time.Duration) (bool, error) {
	return claimRunner(ctx, s.db, runnerID, sessionID, lease)
}

// ClaimRunner takes a runner claim within a transaction.
func (t *Tx) ClaimRunner(ctx context.Context, runnerID, sessionID string, lease time.Duration) (bool, error) {
	return claimRunner(ctx, t.tx, runnerID, sessionID, lease)
}

func claimRunner(ctx context.Context, q querier, runnerID, sessionID string, lease time.Duration) (bool, error) {
	if lease <= 0 {
		lease = store.DefaultRunnerClaimLease
	}

	query := `
		UPDATE runners
		   SET claim_session_id = $2,
		       claimed_at = NOW(),
		       updated_at = NOW()
		 WHERE id = $1
		   AND (claim_session_id IS NULL
		        OR claim_session_id = $2
		        OR claimed_at < NOW() - make_interval(secs => $3))`

	result, err := q.Exec(ctx, query, runnerID, sessionID, lease.Seconds())
	if err != nil {
		return false, handlePgError(err, "runner", runnerID)
	}

	return result.RowsAffected() > 0, nil
}

// ReleaseRunnerClaim drops a claim held by sessionID.
func (s *Store) ReleaseRunnerClaim(ctx context.Context, runnerID, sessionID string) error {
	return releaseRunnerClaim(ctx, s.db, runnerID, sessionID)
}

// ReleaseRunnerClaim drops a claim within a transaction.
func (t *Tx) ReleaseRunnerClaim(ctx context.Context, runnerID, sessionID string) error {
	return releaseRunnerClaim(ctx, t.tx, runnerID, sessionID)
}

func releaseRunnerClaim(ctx context.Context, q querier, runnerID, sessionID string) error {
	// Scoped to the holder on purpose. A release that matched any claim would
	// let a caller whose lease had already expired and been taken over hand
	// somebody else's runner away.
	query := `
		UPDATE runners
		   SET claim_session_id = NULL,
		       claimed_at = NULL,
		       updated_at = NOW()
		 WHERE id = $1 AND claim_session_id = $2`

	if _, err := q.Exec(ctx, query, runnerID, sessionID); err != nil {
		return handlePgError(err, "runner", runnerID)
	}
	return nil
}
