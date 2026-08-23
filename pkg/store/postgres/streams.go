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

// Stream column list for SELECT queries.
const streamColumns = `id, session_id, runner_id, tenant_id, type, state,
	signaling_url, ice_servers, resolution_width, resolution_height,
	frame_rate, bitrate, video_codec, audio_codec, audio_enabled, input_enabled,
	provider_name, provider_stream_id, error, metadata,
	created_at, updated_at, started_at, stopped_at, expires_at`

// CreateStream creates a new stream.
func (s *Store) CreateStream(ctx context.Context, stream *store.Stream) error {
	return createStream(ctx, s.db, stream)
}

// CreateStream creates a new stream within a transaction.
func (t *Tx) CreateStream(ctx context.Context, stream *store.Stream) error {
	return createStream(ctx, t.tx, stream)
}

func createStream(ctx context.Context, q querier, stream *store.Stream) error {
	if stream.ID == "" {
		stream.ID = id.Stream()
	}

	query := `
		INSERT INTO streams (
			id, session_id, runner_id, tenant_id, type, state,
			signaling_url, ice_servers, resolution_width, resolution_height,
			frame_rate, bitrate, video_codec, audio_codec, audio_enabled, input_enabled,
			provider_name, provider_stream_id, error, metadata,
			created_at, updated_at, started_at, stopped_at, expires_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
			NOW(), NOW(), $21, $22, $23
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		stream.ID, stream.SessionID, stream.RunnerID, stream.TenantID,
		stream.Type, stream.State, stream.SignalingURL,
		emptyJSONArray(stream.ICEServers),
		stream.ResolutionWidth, stream.ResolutionHeight,
		stream.FrameRate, stream.BitRate, stream.VideoCodec, stream.AudioCodec,
		stream.AudioEnabled, stream.InputEnabled,
		stream.ProviderName, stream.ProviderStreamID, stream.Error,
		emptyJSONObject(stream.Metadata),
		stream.StartedAt, stream.StoppedAt, stream.ExpiresAt,
	).Scan(&stream.CreatedAt, &stream.UpdatedAt)

	if err != nil {
		return handlePgError(err, "stream", stream.ID)
	}
	return nil
}

// GetStream retrieves a stream by ID.
func (s *Store) GetStream(ctx context.Context, streamID string) (*store.Stream, error) {
	return getStream(ctx, s.db, streamID)
}

// GetStream retrieves a stream by ID within a transaction.
func (t *Tx) GetStream(ctx context.Context, streamID string) (*store.Stream, error) {
	return getStream(ctx, t.tx, streamID)
}

func getStream(ctx context.Context, q querier, streamID string) (*store.Stream, error) {
	query := fmt.Sprintf(`SELECT %s FROM streams WHERE id = $1`, streamColumns)
	row := q.QueryRow(ctx, query, streamID)
	return scanStream(row, streamID)
}

// GetStreamBySessionAndType retrieves a stream by session ID and type.
// If activeOnly is true, only non-terminal streams are returned.
func (s *Store) GetStreamBySessionAndType(ctx context.Context, sessionID, streamType string, activeOnly bool) (*store.Stream, error) {
	return getStreamBySessionAndType(ctx, s.db, sessionID, streamType, activeOnly)
}

// GetStreamBySessionAndType retrieves a stream by session ID and type within a transaction.
func (t *Tx) GetStreamBySessionAndType(ctx context.Context, sessionID, streamType string, activeOnly bool) (*store.Stream, error) {
	return getStreamBySessionAndType(ctx, t.tx, sessionID, streamType, activeOnly)
}

func getStreamBySessionAndType(ctx context.Context, q querier, sessionID, streamType string, activeOnly bool) (*store.Stream, error) {
	query := fmt.Sprintf(`SELECT %s FROM streams WHERE session_id = $1 AND type = $2`, streamColumns)
	if activeOnly {
		query += ` AND state NOT IN ('stopped', 'error')`
	}
	query += ` ORDER BY created_at DESC LIMIT 1`

	row := q.QueryRow(ctx, query, sessionID, streamType)
	return scanStream(row, sessionID+"/"+streamType)
}

// ListStreams retrieves streams with optional filtering.
func (s *Store) ListStreams(ctx context.Context, opts store.ListStreamsOptions) (*store.ListResult[store.Stream], error) {
	return listStreams(ctx, s.db, opts)
}

// ListStreams retrieves streams within a transaction.
func (t *Tx) ListStreams(ctx context.Context, opts store.ListStreamsOptions) (*store.ListResult[store.Stream], error) {
	return listStreams(ctx, t.tx, opts)
}

func listStreams(ctx context.Context, q querier, opts store.ListStreamsOptions) (*store.ListResult[store.Stream], error) {
	var conditions []string
	var args []any
	argNum := 1

	// Build WHERE clauses
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
	if len(opts.State) > 0 {
		conditions = append(conditions, fmt.Sprintf("state = ANY($%d)", argNum))
		args = append(args, opts.State)
		argNum++
	}
	if opts.ActiveOnly {
		conditions = append(conditions, "state NOT IN ('stopped', 'error')")
	}

	whereClause := ""
	if len(conditions) > 0 {
		whereClause = "WHERE " + strings.Join(conditions, " AND ")
	}

	limit := defaultLimit(opts.Limit)
	page, err := streamSortColumns.page(opts.BaseListOptions, argNum)
	if err != nil {
		return nil, err
	}

	// Count query
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM streams %s", whereClause)
	var totalCount int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&totalCount); err != nil {
		return nil, fmt.Errorf("counting streams: %w", err)
	}

	// Data query - fetch one extra to determine HasMore
	dataQuery := fmt.Sprintf(`
		SELECT %s FROM streams %s
		ORDER BY %s
		LIMIT $%d`,
		streamColumns, page.where(whereClause), page.orderBy, page.limitArg(argNum))
	dataArgs := append(args, page.args...) //nolint:gocritic // intentionally creating new slice
	dataArgs = append(dataArgs, limit+1)

	rows, err := q.Query(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, fmt.Errorf("querying streams: %w", err)
	}
	defer rows.Close()

	var streams []*store.Stream
	for rows.Next() {
		stream, err := scanStreamFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning stream: %w", err)
		}
		streams = append(streams, stream)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating streams: %w", err)
	}

	hasMore := len(streams) > limit
	if hasMore {
		streams = streams[:limit]
	}

	var nextCursor string
	if len(streams) > 0 {
		last := streams[len(streams)-1]
		nextCursor = page.nextTime(hasMore, last.CreatedAt, last.ID)
	}

	return &store.ListResult[store.Stream]{
		Items:      streams,
		TotalCount: totalCount,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}, nil
}

// UpdateStream updates stream fields.
func (s *Store) UpdateStream(ctx context.Context, streamID string, updates store.StreamUpdates) error {
	return updateStream(ctx, s.db, streamID, updates)
}

// UpdateStream updates stream fields within a transaction.
func (t *Tx) UpdateStream(ctx context.Context, streamID string, updates store.StreamUpdates) error {
	return updateStream(ctx, t.tx, streamID, updates)
}

func updateStream(ctx context.Context, q querier, streamID string, updates store.StreamUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.State != nil {
		setClauses = append(setClauses, fmt.Sprintf("state = $%d", argNum))
		args = append(args, *updates.State)
		argNum++
	}
	if updates.SignalingURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("signaling_url = $%d", argNum))
		args = append(args, *updates.SignalingURL)
		argNum++
	}
	if updates.ResolutionWidth != nil {
		setClauses = append(setClauses, fmt.Sprintf("resolution_width = $%d", argNum))
		args = append(args, *updates.ResolutionWidth)
		argNum++
	}
	if updates.ResolutionHeight != nil {
		setClauses = append(setClauses, fmt.Sprintf("resolution_height = $%d", argNum))
		args = append(args, *updates.ResolutionHeight)
		argNum++
	}
	if updates.FrameRate != nil {
		setClauses = append(setClauses, fmt.Sprintf("frame_rate = $%d", argNum))
		args = append(args, *updates.FrameRate)
		argNum++
	}
	if updates.BitRate != nil {
		setClauses = append(setClauses, fmt.Sprintf("bitrate = $%d", argNum))
		args = append(args, *updates.BitRate)
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
	if updates.ProviderStreamID != nil {
		setClauses = append(setClauses, fmt.Sprintf("provider_stream_id = $%d", argNum))
		args = append(args, *updates.ProviderStreamID)
		argNum++
	}
	if updates.Error != nil {
		setClauses = append(setClauses, fmt.Sprintf("error = $%d", argNum))
		args = append(args, *updates.Error)
		argNum++
	}
	if updates.Metadata != nil {
		setClauses = append(setClauses, fmt.Sprintf("metadata = $%d", argNum))
		args = append(args, updates.Metadata)
		argNum++
	}
	if updates.StartedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("started_at = $%d", argNum))
		args = append(args, *updates.StartedAt)
		argNum++
	}
	if updates.StoppedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("stopped_at = $%d", argNum))
		args = append(args, *updates.StoppedAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil // Nothing to update
	}

	setClauses = append(setClauses, "updated_at = NOW()")

	query := fmt.Sprintf(`UPDATE streams SET %s WHERE id = $%d`,
		strings.Join(setClauses, ", "), argNum)
	args = append(args, streamID)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "stream", streamID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "stream", ID: streamID}
	}

	return nil
}

// DeleteStream deletes a stream.
func (s *Store) DeleteStream(ctx context.Context, streamID string) error {
	return deleteStream(ctx, s.db, streamID)
}

// DeleteStream deletes a stream within a transaction.
func (t *Tx) DeleteStream(ctx context.Context, streamID string) error {
	return deleteStream(ctx, t.tx, streamID)
}

func deleteStream(ctx context.Context, q querier, streamID string) error {
	query := `DELETE FROM streams WHERE id = $1`
	result, err := q.Exec(ctx, query, streamID)
	if err != nil {
		return handlePgError(err, "stream", streamID)
	}

	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "stream", ID: streamID}
	}

	return nil
}

// CleanupExpiredStreams marks expired streams as stopped.
func (s *Store) CleanupExpiredStreams(ctx context.Context) (int, error) {
	return cleanupExpiredStreams(ctx, s.db)
}

// CleanupExpiredStreams marks expired streams as stopped within a transaction.
func (t *Tx) CleanupExpiredStreams(ctx context.Context) (int, error) {
	return cleanupExpiredStreams(ctx, t.tx)
}

func cleanupExpiredStreams(ctx context.Context, q querier) (int, error) {
	query := `
		UPDATE streams
		SET state = 'stopped', stopped_at = NOW(), updated_at = NOW()
		WHERE expires_at IS NOT NULL
		AND expires_at < NOW()
		AND state NOT IN ('stopped', 'error')`

	result, err := q.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("cleaning up expired streams: %w", err)
	}

	return int(result.RowsAffected()), nil
}

// scanStream scans a single row into a Stream.
func scanStream(row pgx.Row, identifier string) (*store.Stream, error) {
	var s store.Stream
	err := row.Scan(
		&s.ID, &s.SessionID, &s.RunnerID, &s.TenantID, &s.Type, &s.State,
		&s.SignalingURL, &s.ICEServers, &s.ResolutionWidth, &s.ResolutionHeight,
		&s.FrameRate, &s.BitRate, &s.VideoCodec, &s.AudioCodec,
		&s.AudioEnabled, &s.InputEnabled,
		&s.ProviderName, &s.ProviderStreamID, &s.Error, &s.Metadata,
		&s.CreatedAt, &s.UpdatedAt, &s.StartedAt, &s.StoppedAt, &s.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, &store.NotFoundError{Resource: "stream", ID: identifier}
		}
		return nil, fmt.Errorf("scanning stream: %w", err)
	}
	return &s, nil
}

// scanStreamFromRows scans a rows iterator into a Stream.
func scanStreamFromRows(rows pgx.Rows) (*store.Stream, error) {
	var s store.Stream
	err := rows.Scan(
		&s.ID, &s.SessionID, &s.RunnerID, &s.TenantID, &s.Type, &s.State,
		&s.SignalingURL, &s.ICEServers, &s.ResolutionWidth, &s.ResolutionHeight,
		&s.FrameRate, &s.BitRate, &s.VideoCodec, &s.AudioCodec,
		&s.AudioEnabled, &s.InputEnabled,
		&s.ProviderName, &s.ProviderStreamID, &s.Error, &s.Metadata,
		&s.CreatedAt, &s.UpdatedAt, &s.StartedAt, &s.StoppedAt, &s.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}
