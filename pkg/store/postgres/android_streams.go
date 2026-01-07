package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// Android stream column list for SELECT queries.
const androidStreamColumns = `id, session_id, runner_id, device_serial, state, error_message,
	options, width, height, video_codec, audio_codec, local_port, tenant_id,
	created_at, started_at, closed_at, updated_at`

// CreateAndroidStream creates a new Android stream.
func (s *Store) CreateAndroidStream(ctx context.Context, stream *store.AndroidStream) error {
	return createAndroidStream(ctx, s.pool, stream)
}

// CreateAndroidStream creates a new Android stream within a transaction.
func (t *Tx) CreateAndroidStream(ctx context.Context, stream *store.AndroidStream) error {
	return createAndroidStream(ctx, t.tx, stream)
}

func createAndroidStream(ctx context.Context, q querier, stream *store.AndroidStream) error {
	if stream.ID == "" {
		stream.ID = id.New("astr")
	}

	query := `
		INSERT INTO android_streams (
			id, session_id, runner_id, device_serial, state, error_message,
			options, width, height, video_codec, audio_codec, local_port, tenant_id,
			created_at, started_at, closed_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13,
			NOW(), $14, $15, NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		stream.ID, stream.SessionID, stream.RunnerID, stream.DeviceSerial,
		stream.State, stream.ErrorMessage, emptyJSONObject(stream.Options),
		stream.Width, stream.Height, stream.VideoCodec, stream.AudioCodec,
		stream.LocalPort, stream.TenantID, stream.StartedAt, stream.ClosedAt,
	).Scan(&stream.CreatedAt, &stream.UpdatedAt)

	if err != nil {
		return handlePgError(err, "android_stream", stream.ID)
	}
	return nil
}

// GetAndroidStream retrieves an Android stream by ID.
func (s *Store) GetAndroidStream(ctx context.Context, streamID string) (*store.AndroidStream, error) {
	return getAndroidStream(ctx, s.pool, streamID)
}

// GetAndroidStream retrieves an Android stream by ID within a transaction.
func (t *Tx) GetAndroidStream(ctx context.Context, streamID string) (*store.AndroidStream, error) {
	return getAndroidStream(ctx, t.tx, streamID)
}

func getAndroidStream(ctx context.Context, q querier, streamID string) (*store.AndroidStream, error) {
	query := fmt.Sprintf(`SELECT %s FROM android_streams WHERE id = $1`, androidStreamColumns)
	row := q.QueryRow(ctx, query, streamID)
	return scanAndroidStream(row, streamID)
}

// ListAndroidStreams retrieves Android streams with optional filtering.
func (s *Store) ListAndroidStreams(ctx context.Context, opts store.ListAndroidStreamsOptions) (*store.ListResult[store.AndroidStream], error) {
	return listAndroidStreams(ctx, s.pool, opts)
}

// ListAndroidStreams retrieves Android streams within a transaction.
func (t *Tx) ListAndroidStreams(ctx context.Context, opts store.ListAndroidStreamsOptions) (*store.ListResult[store.AndroidStream], error) {
	return listAndroidStreams(ctx, t.tx, opts)
}

func listAndroidStreams(ctx context.Context, q querier, opts store.ListAndroidStreamsOptions) (*store.ListResult[store.AndroidStream], error) {
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

	if opts.DeviceSerial != nil {
		conditions = append(conditions, fmt.Sprintf("device_serial = $%d", argNum))
		args = append(args, *opts.DeviceSerial)
		argNum++
	}

	if len(opts.State) > 0 {
		placeholders := make([]string, len(opts.State))
		for i, s := range opts.State {
			placeholders[i] = fmt.Sprintf("$%d", argNum)
			args = append(args, s)
			argNum++
		}
		conditions = append(conditions, fmt.Sprintf("state IN (%s)", strings.Join(placeholders, ", ")))
	}

	if !opts.IncludeClosed {
		conditions = append(conditions, "closed_at IS NULL")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Count total
	countQuery := fmt.Sprintf(`SELECT COUNT(*) FROM android_streams %s`, whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, err
	}

	// Apply cursor-based pagination
	limit := opts.Limit
	if limit <= 0 || limit > 1000 {
		limit = 50
	}

	// Build query with ordering
	orderBy := "created_at"
	if opts.OrderBy != "" {
		orderBy = opts.OrderBy
	}
	orderDir := "ASC"
	if opts.OrderDesc {
		orderDir = "DESC"
	}

	// Handle cursor
	if opts.Cursor != "" {
		cursorID, cursorTime, err := decodeCursor(opts.Cursor)
		if err == nil {
			if opts.OrderDesc {
				conditions = append(conditions, fmt.Sprintf("(created_at, id) < ($%d, $%d)", argNum, argNum+1))
			} else {
				conditions = append(conditions, fmt.Sprintf("(created_at, id) > ($%d, $%d)", argNum, argNum+1))
			}
			args = append(args, cursorTime, cursorID)
			argNum += 2
		}
	}

	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT %s FROM android_streams
		%s
		ORDER BY %s %s, id %s
		LIMIT $%d`,
		androidStreamColumns, whereClause, orderBy, orderDir, orderDir, argNum)
	args = append(args, limit+1) // Fetch one extra to check if there's more

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var streams []*store.AndroidStream
	for rows.Next() {
		stream, err := scanAndroidStreamRow(rows)
		if err != nil {
			return nil, err
		}
		streams = append(streams, stream)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	hasMore := len(streams) > limit
	if hasMore {
		streams = streams[:limit]
	}

	var nextCursor string
	if hasMore && len(streams) > 0 {
		last := streams[len(streams)-1]
		nextCursor = encodeCursor(last.CreatedAt, last.ID)
	}

	return &store.ListResult[store.AndroidStream]{
		Items:      streams,
		TotalCount: totalCount,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// UpdateAndroidStream updates an Android stream.
func (s *Store) UpdateAndroidStream(ctx context.Context, streamID string, updates store.AndroidStreamUpdates) error {
	return updateAndroidStream(ctx, s.pool, streamID, updates)
}

// UpdateAndroidStream updates an Android stream within a transaction.
func (t *Tx) UpdateAndroidStream(ctx context.Context, streamID string, updates store.AndroidStreamUpdates) error {
	return updateAndroidStream(ctx, t.tx, streamID, updates)
}

func updateAndroidStream(ctx context.Context, q querier, streamID string, updates store.AndroidStreamUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.RunnerID != nil {
		setClauses = append(setClauses, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, *updates.RunnerID)
		argNum++
	}

	if updates.State != nil {
		setClauses = append(setClauses, fmt.Sprintf("state = $%d", argNum))
		args = append(args, *updates.State)
		argNum++
	}

	if updates.ErrorMessage != nil {
		setClauses = append(setClauses, fmt.Sprintf("error_message = $%d", argNum))
		args = append(args, *updates.ErrorMessage)
		argNum++
	}

	if updates.Width != nil {
		setClauses = append(setClauses, fmt.Sprintf("width = $%d", argNum))
		args = append(args, *updates.Width)
		argNum++
	}

	if updates.Height != nil {
		setClauses = append(setClauses, fmt.Sprintf("height = $%d", argNum))
		args = append(args, *updates.Height)
		argNum++
	}

	if updates.VideoCodec != nil {
		setClauses = append(setClauses, fmt.Sprintf("video_codec = $%d", argNum))
		args = append(args, *updates.VideoCodec)
		argNum++
	}

	if updates.AudioCodec != nil {
		setClauses = append(setClauses, fmt.Sprintf("audio_codec = $%d", argNum))
		args = append(args, *updates.AudioCodec)
		argNum++
	}

	if updates.LocalPort != nil {
		setClauses = append(setClauses, fmt.Sprintf("local_port = $%d", argNum))
		args = append(args, *updates.LocalPort)
		argNum++
	}

	if updates.StartedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("started_at = $%d", argNum))
		args = append(args, *updates.StartedAt)
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

	query := fmt.Sprintf(`UPDATE android_streams SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, streamID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "android_stream", streamID)
	}

	if result.RowsAffected() == 0 {
		return store.ErrNotFound
	}

	return nil
}

// DeleteAndroidStream deletes an Android stream.
func (s *Store) DeleteAndroidStream(ctx context.Context, streamID string) error {
	return deleteAndroidStream(ctx, s.pool, streamID)
}

// DeleteAndroidStream deletes an Android stream within a transaction.
func (t *Tx) DeleteAndroidStream(ctx context.Context, streamID string) error {
	return deleteAndroidStream(ctx, t.tx, streamID)
}

func deleteAndroidStream(ctx context.Context, q querier, streamID string) error {
	query := `DELETE FROM android_streams WHERE id = $1`
	result, err := q.Exec(ctx, query, streamID)
	if err != nil {
		return handlePgError(err, "android_stream", streamID)
	}

	if result.RowsAffected() == 0 {
		return store.ErrNotFound
	}

	return nil
}

func scanAndroidStream(row pgx.Row, streamID string) (*store.AndroidStream, error) {
	var stream store.AndroidStream
	err := row.Scan(
		&stream.ID, &stream.SessionID, &stream.RunnerID, &stream.DeviceSerial,
		&stream.State, &stream.ErrorMessage, &stream.Options,
		&stream.Width, &stream.Height, &stream.VideoCodec, &stream.AudioCodec,
		&stream.LocalPort, &stream.TenantID, &stream.CreatedAt, &stream.StartedAt,
		&stream.ClosedAt, &stream.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, handlePgError(err, "android_stream", streamID)
	}
	return &stream, nil
}

func scanAndroidStreamRow(rows pgx.Rows) (*store.AndroidStream, error) {
	var stream store.AndroidStream
	err := rows.Scan(
		&stream.ID, &stream.SessionID, &stream.RunnerID, &stream.DeviceSerial,
		&stream.State, &stream.ErrorMessage, &stream.Options,
		&stream.Width, &stream.Height, &stream.VideoCodec, &stream.AudioCodec,
		&stream.LocalPort, &stream.TenantID, &stream.CreatedAt, &stream.StartedAt,
		&stream.ClosedAt, &stream.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &stream, nil
}
