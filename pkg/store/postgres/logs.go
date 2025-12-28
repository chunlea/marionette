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

// RawLog column list for SELECT queries.
const rawLogColumns = `id, session_id, conversation_id, task_id, run_id, runner_id, stream, content,
	sequence, processed, tenant_id, created_at`

// LogArchive column list for SELECT queries.
const logArchiveColumns = `id, session_id, tenant_id, storage_key, storage_size_bytes,
	log_count, first_log_at, last_log_at, archived_at, expires_at, deleted_at`

// ActionLog column list for SELECT queries.
const actionLogColumns = `id, actor_type, actor_id, actor_name, action, resource_type, resource_id,
	session_id, task_id, details, ip_address, user_agent, success, error_message,
	tenant_id, created_at`

// =============================================================================
// Log CRUD
// =============================================================================

// CreateLog creates a new log entry.
func (s *Store) CreateLog(ctx context.Context, log *store.Log) error {
	return createLog(ctx, s.pool, log)
}

// CreateLog creates a new log entry within a transaction.
func (t *Tx) CreateLog(ctx context.Context, log *store.Log) error {
	return createLog(ctx, t.tx, log)
}

func createLog(ctx context.Context, q querier, log *store.Log) error {
	if log.ID == "" {
		log.ID = id.RawLog()
	}

	query := `
		INSERT INTO raw_logs (
			id, session_id, conversation_id, task_id, run_id, runner_id, stream, content,
			sequence, processed, tenant_id, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW()
		)
		RETURNING created_at`

	err := q.QueryRow(ctx, query,
		log.ID, log.SessionID, log.ConversationID, log.TaskID, log.RunID, log.RunnerID,
		log.Stream, log.Content, log.Sequence, log.Processed, log.TenantID,
	).Scan(&log.CreatedAt)

	if err != nil {
		return handlePgError(err, "raw_log", log.ID)
	}
	return nil
}

// CreateLogBatch creates multiple log entries efficiently.
func (s *Store) CreateLogBatch(ctx context.Context, logs []*store.Log) error {
	return createLogBatch(ctx, s.pool, logs)
}

// CreateLogs creates multiple log entries (alias for CreateLogBatch).
func (s *Store) CreateLogs(ctx context.Context, logs []*store.Log) error {
	return createLogBatch(ctx, s.pool, logs)
}

// CreateLogBatch creates multiple log entries within a transaction.
func (t *Tx) CreateLogBatch(ctx context.Context, logs []*store.Log) error {
	return createLogBatch(ctx, t.tx, logs)
}

// CreateLogs creates multiple log entries within a transaction (alias for CreateLogBatch).
func (t *Tx) CreateLogs(ctx context.Context, logs []*store.Log) error {
	return createLogBatch(ctx, t.tx, logs)
}

func createLogBatch(ctx context.Context, q querier, logs []*store.Log) error {
	if len(logs) == 0 {
		return nil
	}

	// Generate IDs for logs without them
	for _, log := range logs {
		if log.ID == "" {
			log.ID = id.RawLog()
		}
	}

	// Build batch insert
	valueStrings := make([]string, 0, len(logs))
	valueArgs := make([]any, 0, len(logs)*11)
	for i, log := range logs {
		offset := i * 11
		valueStrings = append(valueStrings, fmt.Sprintf(
			"($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, NOW())",
			offset+1, offset+2, offset+3, offset+4, offset+5, offset+6,
			offset+7, offset+8, offset+9, offset+10, offset+11,
		))
		valueArgs = append(valueArgs,
			log.ID, log.SessionID, log.ConversationID, log.TaskID, log.RunID, log.RunnerID,
			log.Stream, log.Content, log.Sequence, log.Processed, log.TenantID,
		)
	}

	query := fmt.Sprintf(`
		INSERT INTO raw_logs (
			id, session_id, conversation_id, task_id, run_id, runner_id, stream, content,
			sequence, processed, tenant_id, created_at
		) VALUES %s`, strings.Join(valueStrings, ", "))

	_, err := q.Exec(ctx, query, valueArgs...)
	if err != nil {
		return handlePgError(err, "raw_log", "batch")
	}
	return nil
}

// GetLog retrieves a log entry by ID.
func (s *Store) GetLog(ctx context.Context, logID string) (*store.Log, error) {
	return getLog(ctx, s.pool, logID)
}

// GetLog retrieves a log entry by ID within a transaction.
func (t *Tx) GetLog(ctx context.Context, logID string) (*store.Log, error) {
	return getLog(ctx, t.tx, logID)
}

func getLog(ctx context.Context, q querier, logID string) (*store.Log, error) {
	query := fmt.Sprintf(`SELECT %s FROM raw_logs WHERE id = $1`, rawLogColumns)
	row := q.QueryRow(ctx, query, logID)
	return scanLog(row, logID)
}

// ListLogs retrieves logs with optional filtering.
func (s *Store) ListLogs(ctx context.Context, opts store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	return listLogs(ctx, s.pool, opts)
}

// ListLogs retrieves logs within a transaction.
func (t *Tx) ListLogs(ctx context.Context, opts store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	return listLogs(ctx, t.tx, opts)
}

func listLogs(ctx context.Context, q querier, opts store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.SessionID != nil {
		conditions = append(conditions, fmt.Sprintf("session_id = $%d", argNum))
		args = append(args, *opts.SessionID)
		argNum++
	}
	if opts.TaskID != nil {
		conditions = append(conditions, fmt.Sprintf("task_id = $%d", argNum))
		args = append(args, *opts.TaskID)
		argNum++
	}
	if opts.RunID != nil {
		conditions = append(conditions, fmt.Sprintf("run_id = $%d", argNum))
		args = append(args, *opts.RunID)
		argNum++
	}
	if opts.RunnerID != nil {
		conditions = append(conditions, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, *opts.RunnerID)
		argNum++
	}
	if len(opts.Stream) > 0 {
		conditions = append(conditions, fmt.Sprintf("stream = ANY($%d)", argNum))
		args = append(args, opts.Stream)
		argNum++
	}
	// Note: Level filter removed - RawLog doesn't have level field

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy := "sequence"
	if opts.OrderBy != "" {
		orderBy = opts.OrderBy
	}
	orderDir := "ASC"
	if opts.OrderDesc {
		orderDir = "DESC"
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM raw_logs %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting raw_logs: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM raw_logs %s
		ORDER BY %s %s
		LIMIT $%d`,
		rawLogColumns, whereClause, orderBy, orderDir, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying raw_logs: %w", err)
	}
	defer rows.Close()

	var logs []*store.Log
	for rows.Next() {
		log, err := scanLogFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning raw_log: %w", err)
		}
		logs = append(logs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating raw_logs: %w", err)
	}

	hasMore := len(logs) > limit
	if hasMore {
		logs = logs[:limit]
	}

	return &store.ListResult[store.Log]{
		Items:      logs,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// DeleteLogsByRun deletes all logs for a specific run.
func (s *Store) DeleteLogsByRun(ctx context.Context, runID string) error {
	return deleteLogsByRun(ctx, s.pool, runID)
}

// DeleteLogsByRun deletes all logs for a run within a transaction.
func (t *Tx) DeleteLogsByRun(ctx context.Context, runID string) error {
	return deleteLogsByRun(ctx, t.tx, runID)
}

func deleteLogsByRun(ctx context.Context, q querier, runID string) error {
	query := `DELETE FROM raw_logs WHERE run_id = $1`
	_, err := q.Exec(ctx, query, runID)
	if err != nil {
		return handlePgError(err, "raw_log", runID)
	}
	return nil
}

func scanLog(row pgx.Row, identifier string) (*store.Log, error) {
	var l store.Log
	err := row.Scan(
		&l.ID, &l.SessionID, &l.ConversationID, &l.TaskID, &l.RunID, &l.RunnerID,
		&l.Stream, &l.Content, &l.Sequence, &l.Processed, &l.TenantID, &l.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "raw_log", ID: identifier}
		}
		return nil, fmt.Errorf("scanning raw_log: %w", err)
	}
	return &l, nil
}

func scanLogFromRows(rows pgx.Rows) (*store.Log, error) {
	var l store.Log
	err := rows.Scan(
		&l.ID, &l.SessionID, &l.ConversationID, &l.TaskID, &l.RunID, &l.RunnerID,
		&l.Stream, &l.Content, &l.Sequence, &l.Processed, &l.TenantID, &l.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// =============================================================================
// LogArchive CRUD
// =============================================================================

// CreateLogArchive creates a new log archive.
func (s *Store) CreateLogArchive(ctx context.Context, archive *store.LogArchive) error {
	return createLogArchive(ctx, s.pool, archive)
}

// CreateLogArchive creates a new log archive within a transaction.
func (t *Tx) CreateLogArchive(ctx context.Context, archive *store.LogArchive) error {
	return createLogArchive(ctx, t.tx, archive)
}

func createLogArchive(ctx context.Context, q querier, archive *store.LogArchive) error {
	if archive.ID == "" {
		archive.ID = id.LogArchive()
	}

	query := `
		INSERT INTO log_archives (
			id, session_id, tenant_id, storage_key, storage_size_bytes,
			log_count, first_log_at, last_log_at, archived_at, expires_at, deleted_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, NOW(), $9, $10
		)
		RETURNING archived_at`

	err := q.QueryRow(ctx, query,
		archive.ID, archive.SessionID, archive.TenantID, archive.StorageKey, archive.StorageSizeBytes,
		archive.LogCount, archive.FirstLogAt, archive.LastLogAt, archive.ExpiresAt, archive.DeletedAt,
	).Scan(&archive.ArchivedAt)

	if err != nil {
		return handlePgError(err, "log_archive", archive.ID)
	}
	return nil
}

// GetLogArchive retrieves a log archive by ID.
func (s *Store) GetLogArchive(ctx context.Context, archiveID string) (*store.LogArchive, error) {
	return getLogArchive(ctx, s.pool, archiveID)
}

// GetLogArchive retrieves a log archive by ID within a transaction.
func (t *Tx) GetLogArchive(ctx context.Context, archiveID string) (*store.LogArchive, error) {
	return getLogArchive(ctx, t.tx, archiveID)
}

func getLogArchive(ctx context.Context, q querier, archiveID string) (*store.LogArchive, error) {
	query := fmt.Sprintf(`SELECT %s FROM log_archives WHERE id = $1`, logArchiveColumns)
	row := q.QueryRow(ctx, query, archiveID)
	return scanLogArchive(row, archiveID)
}

// GetLogArchiveBySession retrieves a log archive by session ID.
func (s *Store) GetLogArchiveBySession(ctx context.Context, sessionID string) (*store.LogArchive, error) {
	return getLogArchiveBySession(ctx, s.pool, sessionID)
}

// GetLogArchiveBySession retrieves a log archive within a transaction.
func (t *Tx) GetLogArchiveBySession(ctx context.Context, sessionID string) (*store.LogArchive, error) {
	return getLogArchiveBySession(ctx, t.tx, sessionID)
}

func getLogArchiveBySession(ctx context.Context, q querier, sessionID string) (*store.LogArchive, error) {
	query := fmt.Sprintf(`SELECT %s FROM log_archives WHERE session_id = $1`, logArchiveColumns)
	row := q.QueryRow(ctx, query, sessionID)
	return scanLogArchive(row, sessionID)
}

// ListLogArchives retrieves log archives with optional filtering.
func (s *Store) ListLogArchives(ctx context.Context, opts store.ListLogArchivesOptions) (*store.ListResult[store.LogArchive], error) {
	return listLogArchives(ctx, s.pool, opts)
}

// ListLogArchives retrieves log archives within a transaction.
func (t *Tx) ListLogArchives(ctx context.Context, opts store.ListLogArchivesOptions) (*store.ListResult[store.LogArchive], error) {
	return listLogArchives(ctx, t.tx, opts)
}

func listLogArchives(ctx context.Context, q querier, opts store.ListLogArchivesOptions) (*store.ListResult[store.LogArchive], error) {
	var conditions []string
	var args []any
	argNum := 1

	if !opts.IncludeDeleted {
		conditions = append(conditions, "deleted_at IS NULL")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy := "archived_at"
	if opts.OrderBy != "" {
		orderBy = opts.OrderBy
	}
	orderDir := "DESC"
	if !opts.OrderDesc {
		orderDir = "ASC"
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM log_archives %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting log_archives: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM log_archives %s
		ORDER BY %s %s
		LIMIT $%d`,
		logArchiveColumns, whereClause, orderBy, orderDir, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying log_archives: %w", err)
	}
	defer rows.Close()

	var archives []*store.LogArchive
	for rows.Next() {
		archive, err := scanLogArchiveFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning log_archive: %w", err)
		}
		archives = append(archives, archive)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating log_archives: %w", err)
	}

	hasMore := len(archives) > limit
	if hasMore {
		archives = archives[:limit]
	}

	return &store.ListResult[store.LogArchive]{
		Items:      archives,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateLogArchive updates log archive fields.
func (s *Store) UpdateLogArchive(ctx context.Context, archiveID string, updates store.LogArchiveUpdates) error {
	return updateLogArchive(ctx, s.pool, archiveID, updates)
}

// UpdateLogArchive updates log archive fields within a transaction.
func (t *Tx) UpdateLogArchive(ctx context.Context, archiveID string, updates store.LogArchiveUpdates) error {
	return updateLogArchive(ctx, t.tx, archiveID, updates)
}

func updateLogArchive(ctx context.Context, q querier, archiveID string, updates store.LogArchiveUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.DeletedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("deleted_at = $%d", argNum))
		args = append(args, *updates.DeletedAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	query := fmt.Sprintf(`UPDATE log_archives SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, archiveID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "log_archive", archiveID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "log_archive", ID: archiveID}
	}

	return nil
}

// DeleteLogArchive deletes a log archive.
func (s *Store) DeleteLogArchive(ctx context.Context, archiveID string) error {
	return deleteLogArchive(ctx, s.pool, archiveID)
}

// DeleteLogArchive deletes a log archive within a transaction.
func (t *Tx) DeleteLogArchive(ctx context.Context, archiveID string) error {
	return deleteLogArchive(ctx, t.tx, archiveID)
}

func deleteLogArchive(ctx context.Context, q querier, archiveID string) error {
	query := `DELETE FROM log_archives WHERE id = $1`
	result, err := q.Exec(ctx, query, archiveID)
	if err != nil {
		return handlePgError(err, "log_archive", archiveID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "log_archive", ID: archiveID}
	}

	return nil
}

func scanLogArchive(row pgx.Row, identifier string) (*store.LogArchive, error) {
	var a store.LogArchive
	err := row.Scan(
		&a.ID, &a.SessionID, &a.TenantID, &a.StorageKey, &a.StorageSizeBytes,
		&a.LogCount, &a.FirstLogAt, &a.LastLogAt, &a.ArchivedAt, &a.ExpiresAt, &a.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "log_archive", ID: identifier}
		}
		return nil, fmt.Errorf("scanning log_archive: %w", err)
	}
	return &a, nil
}

func scanLogArchiveFromRows(rows pgx.Rows) (*store.LogArchive, error) {
	var a store.LogArchive
	err := rows.Scan(
		&a.ID, &a.SessionID, &a.TenantID, &a.StorageKey, &a.StorageSizeBytes,
		&a.LogCount, &a.FirstLogAt, &a.LastLogAt, &a.ArchivedAt, &a.ExpiresAt, &a.DeletedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// =============================================================================
// ActionLog CRUD
// =============================================================================

// CreateActionLog creates a new action log entry.
func (s *Store) CreateActionLog(ctx context.Context, log *store.ActionLog) error {
	return createActionLog(ctx, s.pool, log)
}

// CreateActionLog creates a new action log entry within a transaction.
func (t *Tx) CreateActionLog(ctx context.Context, log *store.ActionLog) error {
	return createActionLog(ctx, t.tx, log)
}

func createActionLog(ctx context.Context, q querier, log *store.ActionLog) error {
	if log.ID == "" {
		log.ID = id.ActionLog()
	}

	query := `
		INSERT INTO action_logs (
			id, actor_type, actor_id, actor_name, action, resource_type, resource_id,
			session_id, task_id, details, ip_address, user_agent, success, error_message,
			tenant_id, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW()
		)
		RETURNING created_at`

	err := q.QueryRow(ctx, query,
		log.ID, log.ActorType, log.ActorID, log.ActorName, log.Action,
		log.ResourceType, log.ResourceID, log.SessionID, log.TaskID,
		emptyJSONObject(log.Details), log.IPAddress, log.UserAgent,
		log.Success, log.ErrorMessage, log.TenantID,
	).Scan(&log.CreatedAt)

	if err != nil {
		return handlePgError(err, "action_log", log.ID)
	}
	return nil
}

// GetActionLog retrieves an action log by ID.
func (s *Store) GetActionLog(ctx context.Context, logID string) (*store.ActionLog, error) {
	return getActionLog(ctx, s.pool, logID)
}

// GetActionLog retrieves an action log by ID within a transaction.
func (t *Tx) GetActionLog(ctx context.Context, logID string) (*store.ActionLog, error) {
	return getActionLog(ctx, t.tx, logID)
}

func getActionLog(ctx context.Context, q querier, logID string) (*store.ActionLog, error) {
	query := fmt.Sprintf(`SELECT %s FROM action_logs WHERE id = $1`, actionLogColumns)
	row := q.QueryRow(ctx, query, logID)
	return scanActionLog(row, logID)
}

// ListActionLogs retrieves action logs with optional filtering.
func (s *Store) ListActionLogs(ctx context.Context, opts store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	return listActionLogs(ctx, s.pool, opts)
}

// ListActionLogs retrieves action logs within a transaction.
func (t *Tx) ListActionLogs(ctx context.Context, opts store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	return listActionLogs(ctx, t.tx, opts)
}

func listActionLogs(ctx context.Context, q querier, opts store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.ActorType != nil {
		conditions = append(conditions, fmt.Sprintf("actor_type = $%d", argNum))
		args = append(args, *opts.ActorType)
		argNum++
	}
	if opts.ActorID != nil {
		conditions = append(conditions, fmt.Sprintf("actor_id = $%d", argNum))
		args = append(args, *opts.ActorID)
		argNum++
	}
	if opts.Action != nil {
		conditions = append(conditions, fmt.Sprintf("action = $%d", argNum))
		args = append(args, *opts.Action)
		argNum++
	}
	if opts.ResourceType != nil {
		conditions = append(conditions, fmt.Sprintf("resource_type = $%d", argNum))
		args = append(args, *opts.ResourceType)
		argNum++
	}
	if opts.ResourceID != nil {
		conditions = append(conditions, fmt.Sprintf("resource_id = $%d", argNum))
		args = append(args, *opts.ResourceID)
		argNum++
	}
	if opts.SessionID != nil {
		conditions = append(conditions, fmt.Sprintf("session_id = $%d", argNum))
		args = append(args, *opts.SessionID)
		argNum++
	}
	if opts.TaskID != nil {
		conditions = append(conditions, fmt.Sprintf("task_id = $%d", argNum))
		args = append(args, *opts.TaskID)
		argNum++
	}
	if opts.Success != nil {
		conditions = append(conditions, fmt.Sprintf("success = $%d", argNum))
		args = append(args, *opts.Success)
		argNum++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	orderBy := "created_at"
	if opts.OrderBy != "" {
		orderBy = opts.OrderBy
	}
	orderDir := "DESC"
	if !opts.OrderDesc {
		orderDir = "ASC"
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM action_logs %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting action_logs: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM action_logs %s
		ORDER BY %s %s
		LIMIT $%d`,
		actionLogColumns, whereClause, orderBy, orderDir, argNum)
	dataArgs := append(args, limit+1) //nolint:gocritic // intentionally creating new slice

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying action_logs: %w", err)
	}
	defer rows.Close()

	var logs []*store.ActionLog
	for rows.Next() {
		log, err := scanActionLogFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning action_log: %w", err)
		}
		logs = append(logs, log)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating action_logs: %w", err)
	}

	hasMore := len(logs) > limit
	if hasMore {
		logs = logs[:limit]
	}

	return &store.ListResult[store.ActionLog]{
		Items:      logs,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

func scanActionLog(row pgx.Row, identifier string) (*store.ActionLog, error) {
	var a store.ActionLog
	err := row.Scan(
		&a.ID, &a.ActorType, &a.ActorID, &a.ActorName, &a.Action,
		&a.ResourceType, &a.ResourceID, &a.SessionID, &a.TaskID,
		&a.Details, &a.IPAddress, &a.UserAgent, &a.Success, &a.ErrorMessage,
		&a.TenantID, &a.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "action_log", ID: identifier}
		}
		return nil, fmt.Errorf("scanning action_log: %w", err)
	}
	return &a, nil
}

func scanActionLogFromRows(rows pgx.Rows) (*store.ActionLog, error) {
	var a store.ActionLog
	err := rows.Scan(
		&a.ID, &a.ActorType, &a.ActorID, &a.ActorName, &a.Action,
		&a.ResourceType, &a.ResourceID, &a.SessionID, &a.TaskID,
		&a.Details, &a.IPAddress, &a.UserAgent, &a.Success, &a.ErrorMessage,
		&a.TenantID, &a.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &a, nil
}
