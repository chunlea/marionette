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

// PermissionRequest column list for SELECT queries.
const permissionRequestColumns = `id, original_request_id, session_id, task_id, run_id, tool, action, context,
	risk_level, status, suspend_after_seconds, responded_by, response_reason, responded_at,
	tenant_id, created_at, updated_at`

// CreatePermissionRequest creates a new permission request.
func (s *Store) CreatePermissionRequest(ctx context.Context, req *store.PermissionRequest) error {
	return createPermissionRequest(ctx, s.pool, req)
}

// CreatePermissionRequest creates a new permission request within a transaction.
func (t *Tx) CreatePermissionRequest(ctx context.Context, req *store.PermissionRequest) error {
	return createPermissionRequest(ctx, t.tx, req)
}

func createPermissionRequest(ctx context.Context, q querier, req *store.PermissionRequest) error {
	if req.ID == "" {
		req.ID = id.PermissionRequest()
	}

	query := `
		INSERT INTO permission_requests (
			id, original_request_id, session_id, task_id, run_id, tool, action, context,
			risk_level, status, suspend_after_seconds, responded_by, response_reason, responded_at,
			tenant_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		req.ID, req.OriginalRequestID, req.SessionID, req.TaskID, req.RunID, req.Tool, req.Action, req.Context,
		req.RiskLevel, req.Status, req.SuspendAfterSeconds, req.RespondedBy, req.ResponseReason, req.RespondedAt,
		req.TenantID,
	).Scan(&req.CreatedAt, &req.UpdatedAt)

	if err != nil {
		return handlePgError(err, "permission_request", req.ID)
	}
	return nil
}

// GetPermissionRequest retrieves a permission request by ID.
func (s *Store) GetPermissionRequest(ctx context.Context, reqID string) (*store.PermissionRequest, error) {
	return getPermissionRequest(ctx, s.pool, reqID)
}

// GetPermissionRequest retrieves a permission request by ID within a transaction.
func (t *Tx) GetPermissionRequest(ctx context.Context, reqID string) (*store.PermissionRequest, error) {
	return getPermissionRequest(ctx, t.tx, reqID)
}

func getPermissionRequest(ctx context.Context, q querier, reqID string) (*store.PermissionRequest, error) {
	query := fmt.Sprintf(`SELECT %s FROM permission_requests WHERE id = $1`, permissionRequestColumns)
	row := q.QueryRow(ctx, query, reqID)
	return scanPermissionRequest(row, reqID)
}

// ListPermissionRequests retrieves permission requests with optional filtering.
func (s *Store) ListPermissionRequests(ctx context.Context, opts store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	return listPermissionRequests(ctx, s.pool, opts)
}

// ListPermissionRequests retrieves permission requests within a transaction.
func (t *Tx) ListPermissionRequests(ctx context.Context, opts store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	return listPermissionRequests(ctx, t.tx, opts)
}

func listPermissionRequests(ctx context.Context, q querier, opts store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
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
	if len(opts.Status) > 0 {
		conditions = append(conditions, fmt.Sprintf("status = ANY($%d)", argNum))
		args = append(args, opts.Status)
		argNum++
	}
	if len(opts.RiskLevel) > 0 {
		conditions = append(conditions, fmt.Sprintf("risk_level = ANY($%d)", argNum))
		args = append(args, opts.RiskLevel)
		argNum++
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	page, err := permissionSortColumns.page(opts.BaseListOptions, argNum)
	if err != nil {
		return nil, err
	}

	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM permission_requests %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting permission_requests: %w", err)
	}

	dataQuery := fmt.Sprintf(`
		SELECT %s FROM permission_requests %s
		ORDER BY %s
		LIMIT $%d`,
		permissionRequestColumns, page.where(whereClause), page.orderBy, page.limitArg(argNum))
	dataArgs := append(args, page.args...) //nolint:gocritic // intentionally creating new slice
	dataArgs = append(dataArgs, limit+1)

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying permission_requests: %w", err)
	}
	defer rows.Close()

	var requests []*store.PermissionRequest
	for rows.Next() {
		req, err := scanPermissionRequestFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning permission_request: %w", err)
		}
		requests = append(requests, req)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating permission_requests: %w", err)
	}

	hasMore := len(requests) > limit
	if hasMore {
		requests = requests[:limit]
	}

	var nextCursor string
	if len(requests) > 0 {
		last := requests[len(requests)-1]
		nextCursor = page.nextTime(hasMore, last.CreatedAt, last.ID)
	}

	return &store.ListResult[store.PermissionRequest]{
		Items:      requests,
		TotalCount: totalCount,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// UpdatePermissionRequest updates permission request fields.
func (s *Store) UpdatePermissionRequest(ctx context.Context, reqID string, updates store.PermissionRequestUpdates) error {
	return updatePermissionRequest(ctx, s.pool, reqID, updates)
}

// UpdatePermissionRequest updates permission request fields within a transaction.
func (t *Tx) UpdatePermissionRequest(ctx context.Context, reqID string, updates store.PermissionRequestUpdates) error {
	return updatePermissionRequest(ctx, t.tx, reqID, updates)
}

func updatePermissionRequest(ctx context.Context, q querier, reqID string, updates store.PermissionRequestUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argNum))
		args = append(args, *updates.Status)
		argNum++
	}
	if updates.RespondedBy != nil {
		setClauses = append(setClauses, fmt.Sprintf("responded_by = $%d", argNum))
		args = append(args, *updates.RespondedBy)
		argNum++
	}
	if updates.ResponseReason != nil {
		setClauses = append(setClauses, fmt.Sprintf("response_reason = $%d", argNum))
		args = append(args, *updates.ResponseReason)
		argNum++
	}
	if updates.RespondedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("responded_at = $%d", argNum))
		args = append(args, *updates.RespondedAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE permission_requests SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, reqID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "permission_request", reqID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "permission_request", ID: reqID}
	}

	return nil
}

func scanPermissionRequest(row pgx.Row, identifier string) (*store.PermissionRequest, error) {
	var p store.PermissionRequest
	err := row.Scan(
		&p.ID, &p.OriginalRequestID, &p.SessionID, &p.TaskID, &p.RunID, &p.Tool, &p.Action, &p.Context,
		&p.RiskLevel, &p.Status, &p.SuspendAfterSeconds, &p.RespondedBy, &p.ResponseReason, &p.RespondedAt,
		&p.TenantID, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "permission_request", ID: identifier}
		}
		return nil, fmt.Errorf("scanning permission_request: %w", err)
	}
	return &p, nil
}

func scanPermissionRequestFromRows(rows pgx.Rows) (*store.PermissionRequest, error) {
	var p store.PermissionRequest
	err := rows.Scan(
		&p.ID, &p.OriginalRequestID, &p.SessionID, &p.TaskID, &p.RunID, &p.Tool, &p.Action, &p.Context,
		&p.RiskLevel, &p.Status, &p.SuspendAfterSeconds, &p.RespondedBy, &p.ResponseReason, &p.RespondedAt,
		&p.TenantID, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
