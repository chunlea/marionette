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

// Session column list for SELECT queries.
const sessionColumns = `id, name, status, runner_id, workspace_id, profile_id, agent, is_byok,
	agent_config_id, agent_config_metadata, context_snapshot, agent_version,
	suspend_strategy, suspend_snapshot_id, suspend_workspace_synced, workspace_manifest_id, previous_runner_id,
	network_policy, allowed_hosts, lifecycle_mode, idle_timeout_seconds, max_lifetime_seconds,
	schedule_cron, schedule_timezone, next_scheduled_at, tenant_id, labels, annotations,
	last_activity_at, suspended_at, resumed_at, created_at, updated_at`

// CreateSession creates a new session.
func (s *Store) CreateSession(ctx context.Context, session *store.Session) error {
	return createSession(ctx, s.db, session)
}

// CreateSession creates a new session within a transaction.
func (t *Tx) CreateSession(ctx context.Context, session *store.Session) error {
	return createSession(ctx, t.tx, session)
}

func createSession(ctx context.Context, q querier, session *store.Session) error {
	if session.ID == "" {
		session.ID = id.Session()
	}

	query := `
		INSERT INTO sessions (
			id, name, status, runner_id, workspace_id, profile_id, agent, is_byok,
			agent_config_id, agent_config_metadata, context_snapshot, agent_version,
			suspend_strategy, suspend_snapshot_id, suspend_workspace_synced, workspace_manifest_id, previous_runner_id,
			network_policy, allowed_hosts, lifecycle_mode, idle_timeout_seconds, max_lifetime_seconds,
			schedule_cron, schedule_timezone, next_scheduled_at, tenant_id, labels, annotations,
			last_activity_at, suspended_at, resumed_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16,
			$17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30, $31, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		session.ID, session.Name, session.Status, session.RunnerID, session.WorkspaceID,
		session.ProfileID, session.Agent, session.IsBYOK, session.AgentConfigID, session.AgentConfigMetadata,
		session.ContextSnapshot, session.AgentVersion, session.SuspendStrategy,
		session.SuspendSnapshotID, session.SuspendWorkspaceSynced, session.WorkspaceManifestID,
		session.PreviousRunnerID,
		session.NetworkPolicy, session.AllowedHosts, session.LifecycleMode,
		session.IdleTimeoutSeconds, session.MaxLifetimeSeconds, session.ScheduleCron,
		session.ScheduleTimezone, session.NextScheduledAt, session.TenantID,
		emptyJSONObject(session.Labels), emptyJSONObject(session.Annotations),
		session.LastActivityAt, session.SuspendedAt, session.ResumedAt,
	).Scan(&session.CreatedAt, &session.UpdatedAt)

	if err != nil {
		return handlePgError(err, "session", session.ID)
	}
	return nil
}

// GetSession retrieves a session by ID.
func (s *Store) GetSession(ctx context.Context, sessionID string) (*store.Session, error) {
	return getSession(ctx, s.db, sessionID)
}

// GetSession retrieves a session by ID within a transaction.
func (t *Tx) GetSession(ctx context.Context, sessionID string) (*store.Session, error) {
	return getSession(ctx, t.tx, sessionID)
}

func getSession(ctx context.Context, q querier, sessionID string) (*store.Session, error) {
	query := fmt.Sprintf(`SELECT %s FROM sessions WHERE id = $1`, sessionColumns)
	row := q.QueryRow(ctx, query, sessionID)
	return scanSession(row, sessionID)
}

// ListSessions retrieves sessions with optional filtering.
func (s *Store) ListSessions(ctx context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return listSessions(ctx, s.db, opts)
}

// ListSessions retrieves sessions within a transaction.
func (t *Tx) ListSessions(ctx context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return listSessions(ctx, t.tx, opts)
}

func listSessions(ctx context.Context, q querier, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	var conditions []string
	var args []any
	argNum := 1

	// Build WHERE clauses
	if len(opts.Status) > 0 {
		conditions = append(conditions, fmt.Sprintf("status = ANY($%d)", argNum))
		args = append(args, opts.Status)
		argNum++
	}
	if opts.RunnerID != nil {
		conditions = append(conditions, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, *opts.RunnerID)
		argNum++
	}
	if opts.WorkspaceID != nil {
		conditions = append(conditions, fmt.Sprintf("workspace_id = $%d", argNum))
		args = append(args, *opts.WorkspaceID)
		argNum++
	}
	if opts.Agent != nil {
		conditions = append(conditions, fmt.Sprintf("agent = $%d", argNum))
		args = append(args, *opts.Agent)
		argNum++
	}
	if opts.LifecycleMode != nil {
		conditions = append(conditions, fmt.Sprintf("lifecycle_mode = $%d", argNum))
		args = append(args, *opts.LifecycleMode)
		argNum++
	}
	// TODO: Add label filtering with JSONB operators

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	page, err := sessionSortColumns.page(opts.BaseListOptions, argNum)
	if err != nil {
		return nil, err
	}

	// Count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM sessions %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting sessions: %w", err)
	}

	// Data query - fetch one extra to determine HasMore
	dataQuery := fmt.Sprintf(`
		SELECT %s FROM sessions %s
		ORDER BY %s
		LIMIT $%d`,
		sessionColumns, page.where(whereClause), page.orderBy, page.limitArg(argNum))
	dataArgs := append(args, page.args...) //nolint:gocritic // intentionally creating new slice
	dataArgs = append(dataArgs, limit+1)

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*store.Session
	for rows.Next() {
		session, err := scanSessionFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning session: %w", err)
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sessions: %w", err)
	}

	hasMore := len(sessions) > limit
	if hasMore {
		sessions = sessions[:limit]
	}

	var nextCursor string
	if len(sessions) > 0 {
		last := sessions[len(sessions)-1]
		nextCursor = page.nextTime(hasMore, last.CreatedAt, last.ID)
	}

	return &store.ListResult[store.Session]{
		Items:      sessions,
		TotalCount: totalCount,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// UpdateSession updates session fields.
func (s *Store) UpdateSession(ctx context.Context, sessionID string, updates store.SessionUpdates) error {
	return updateSession(ctx, s.db, sessionID, updates)
}

// UpdateSession updates session fields within a transaction.
func (t *Tx) UpdateSession(ctx context.Context, sessionID string, updates store.SessionUpdates) error {
	return updateSession(ctx, t.tx, sessionID, updates)
}

func updateSession(ctx context.Context, q querier, sessionID string, updates store.SessionUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *updates.Name)
		argNum++
	}
	if updates.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argNum))
		args = append(args, *updates.Status)
		argNum++
	}
	if updates.RunnerID != nil {
		// Empty string means set to NULL (detach runner).
		// Foreign key constraint requires NULL or a valid runner ID, not empty string.
		if *updates.RunnerID == "" {
			setClauses = append(setClauses, "runner_id = NULL")
		} else {
			setClauses = append(setClauses, fmt.Sprintf("runner_id = $%d", argNum))
			args = append(args, *updates.RunnerID)
			argNum++
		}
	}
	if updates.ProfileID != nil {
		if *updates.ProfileID == "" {
			setClauses = append(setClauses, "profile_id = NULL")
		} else {
			setClauses = append(setClauses, fmt.Sprintf("profile_id = $%d", argNum))
			args = append(args, *updates.ProfileID)
			argNum++
		}
	}
	if updates.AgentConfigID != nil {
		setClauses = append(setClauses, fmt.Sprintf("agent_config_id = $%d", argNum))
		args = append(args, *updates.AgentConfigID)
		argNum++
	}
	if updates.AgentConfigMetadata != nil {
		setClauses = append(setClauses, fmt.Sprintf("agent_config_metadata = $%d", argNum))
		args = append(args, updates.AgentConfigMetadata)
		argNum++
	}
	if updates.ContextSnapshot != nil {
		setClauses = append(setClauses, fmt.Sprintf("context_snapshot = $%d", argNum))
		args = append(args, updates.ContextSnapshot)
		argNum++
	}
	if updates.AgentVersion != nil {
		setClauses = append(setClauses, fmt.Sprintf("agent_version = $%d", argNum))
		args = append(args, *updates.AgentVersion)
		argNum++
	}
	if updates.SuspendStrategy != nil {
		setClauses = append(setClauses, fmt.Sprintf("suspend_strategy = $%d", argNum))
		args = append(args, *updates.SuspendStrategy)
		argNum++
	}
	if updates.SuspendSnapshotID != nil {
		setClauses = append(setClauses, fmt.Sprintf("suspend_snapshot_id = $%d", argNum))
		args = append(args, *updates.SuspendSnapshotID)
		argNum++
	}
	if updates.SuspendWorkspaceSynced != nil {
		setClauses = append(setClauses, fmt.Sprintf("suspend_workspace_synced = $%d", argNum))
		args = append(args, *updates.SuspendWorkspaceSynced)
		argNum++
	}
	if updates.WorkspaceManifestID != nil {
		setClauses = append(setClauses, fmt.Sprintf("workspace_manifest_id = $%d", argNum))
		args = append(args, *updates.WorkspaceManifestID)
		argNum++
	}
	if updates.PreviousRunnerID != nil {
		setClauses = append(setClauses, fmt.Sprintf("previous_runner_id = $%d", argNum))
		args = append(args, *updates.PreviousRunnerID)
		argNum++
	}
	if updates.NetworkPolicy != nil {
		setClauses = append(setClauses, fmt.Sprintf("network_policy = $%d", argNum))
		args = append(args, *updates.NetworkPolicy)
		argNum++
	}
	if updates.AllowedHosts != nil {
		setClauses = append(setClauses, fmt.Sprintf("allowed_hosts = $%d", argNum))
		args = append(args, updates.AllowedHosts)
		argNum++
	}
	if updates.LifecycleMode != nil {
		setClauses = append(setClauses, fmt.Sprintf("lifecycle_mode = $%d", argNum))
		args = append(args, *updates.LifecycleMode)
		argNum++
	}
	if updates.IdleTimeoutSeconds != nil {
		setClauses = append(setClauses, fmt.Sprintf("idle_timeout_seconds = $%d", argNum))
		args = append(args, *updates.IdleTimeoutSeconds)
		argNum++
	}
	if updates.MaxLifetimeSeconds != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_lifetime_seconds = $%d", argNum))
		args = append(args, *updates.MaxLifetimeSeconds)
		argNum++
	}
	if updates.ScheduleCron != nil {
		setClauses = append(setClauses, fmt.Sprintf("schedule_cron = $%d", argNum))
		args = append(args, *updates.ScheduleCron)
		argNum++
	}
	if updates.ScheduleTimezone != nil {
		setClauses = append(setClauses, fmt.Sprintf("schedule_timezone = $%d", argNum))
		args = append(args, *updates.ScheduleTimezone)
		argNum++
	}
	if updates.NextScheduledAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("next_scheduled_at = $%d", argNum))
		args = append(args, *updates.NextScheduledAt)
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
	if updates.LastActivityAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_activity_at = $%d", argNum))
		args = append(args, *updates.LastActivityAt)
		argNum++
	}
	if updates.SuspendedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("suspended_at = $%d", argNum))
		args = append(args, *updates.SuspendedAt)
		argNum++
	}
	if updates.ResumedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("resumed_at = $%d", argNum))
		args = append(args, *updates.ResumedAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil // Nothing to update
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE sessions SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, sessionID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "session", sessionID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "session", ID: sessionID}
	}

	return nil
}

// DeleteSession deletes a session.
func (s *Store) DeleteSession(ctx context.Context, sessionID string) error {
	return deleteSession(ctx, s.db, sessionID)
}

// DeleteSession deletes a session within a transaction.
func (t *Tx) DeleteSession(ctx context.Context, sessionID string) error {
	return deleteSession(ctx, t.tx, sessionID)
}

func deleteSession(ctx context.Context, q querier, sessionID string) error {
	query := `DELETE FROM sessions WHERE id = $1`
	result, err := q.Exec(ctx, query, sessionID)
	if err != nil {
		return handlePgError(err, "session", sessionID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "session", ID: sessionID}
	}

	return nil
}

// scanSession scans a single row into a Session.
func scanSession(row pgx.Row, identifier string) (*store.Session, error) {
	var s store.Session
	err := row.Scan(
		&s.ID, &s.Name, &s.Status, &s.RunnerID, &s.WorkspaceID, &s.ProfileID, &s.Agent, &s.IsBYOK,
		&s.AgentConfigID, &s.AgentConfigMetadata, &s.ContextSnapshot, &s.AgentVersion,
		&s.SuspendStrategy, &s.SuspendSnapshotID, &s.SuspendWorkspaceSynced, &s.WorkspaceManifestID, &s.PreviousRunnerID,
		&s.NetworkPolicy, &s.AllowedHosts, &s.LifecycleMode, &s.IdleTimeoutSeconds, &s.MaxLifetimeSeconds,
		&s.ScheduleCron, &s.ScheduleTimezone, &s.NextScheduledAt, &s.TenantID, &s.Labels, &s.Annotations,
		&s.LastActivityAt, &s.SuspendedAt, &s.ResumedAt, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "session", ID: identifier}
		}
		return nil, fmt.Errorf("scanning session: %w", err)
	}
	return &s, nil
}

// scanSessionFromRows scans a rows iterator into a Session.
func scanSessionFromRows(rows pgx.Rows) (*store.Session, error) {
	var s store.Session
	err := rows.Scan(
		&s.ID, &s.Name, &s.Status, &s.RunnerID, &s.WorkspaceID, &s.ProfileID, &s.Agent, &s.IsBYOK,
		&s.AgentConfigID, &s.AgentConfigMetadata, &s.ContextSnapshot, &s.AgentVersion,
		&s.SuspendStrategy, &s.SuspendSnapshotID, &s.SuspendWorkspaceSynced, &s.WorkspaceManifestID, &s.PreviousRunnerID,
		&s.NetworkPolicy, &s.AllowedHosts, &s.LifecycleMode, &s.IdleTimeoutSeconds, &s.MaxLifetimeSeconds,
		&s.ScheduleCron, &s.ScheduleTimezone, &s.NextScheduledAt, &s.TenantID, &s.Labels, &s.Annotations,
		&s.LastActivityAt, &s.SuspendedAt, &s.ResumedAt, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// GetDueScheduledSessions retrieves sessions with lifecycle_mode='scheduled'
// that are suspended and have next_scheduled_at <= now.
func (s *Store) GetDueScheduledSessions(ctx context.Context, now time.Time, limit int) ([]*store.Session, error) {
	return getDueScheduledSessions(ctx, s.db, now, limit)
}

// GetDueScheduledSessions retrieves due scheduled sessions within a transaction.
func (t *Tx) GetDueScheduledSessions(ctx context.Context, now time.Time, limit int) ([]*store.Session, error) {
	return getDueScheduledSessions(ctx, t.tx, now, limit)
}

func getDueScheduledSessions(ctx context.Context, q querier, now time.Time, limit int) ([]*store.Session, error) {
	if limit <= 0 {
		limit = 100
	}

	query := fmt.Sprintf(`
		SELECT %s FROM sessions
		WHERE lifecycle_mode = 'scheduled'
		  AND status = 'suspended'
		  AND next_scheduled_at IS NOT NULL
		  AND next_scheduled_at <= $1
		ORDER BY next_scheduled_at ASC
		LIMIT $2
	`, sessionColumns)

	rows, err := q.Query(ctx, query, now, limit)
	if err != nil {
		return nil, fmt.Errorf("querying due scheduled sessions: %w", err)
	}
	defer rows.Close()

	var sessions []*store.Session
	for rows.Next() {
		session, err := scanSessionFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning scheduled session: %w", err)
		}
		sessions = append(sessions, session)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating scheduled sessions: %w", err)
	}

	return sessions, nil
}
