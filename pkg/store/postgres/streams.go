package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/streaming"
)

// CreateStream creates a new stream record.
func (s *Store) CreateStream(ctx context.Context, params streaming.CreateStreamParams) (*streaming.Stream, error) {
	streamID := params.ID
	if streamID == "" {
		streamID = id.Stream()
	}

	// Marshal ICE servers to JSON
	iceServersJSON, err := json.Marshal(params.ICEServers)
	if err != nil {
		return nil, fmt.Errorf("marshaling ice_servers: %w", err)
	}

	// Marshal metadata to JSON
	metadataJSON, err := json.Marshal(params.Metadata)
	if err != nil {
		return nil, fmt.Errorf("marshaling metadata: %w", err)
	}

	query := `
		INSERT INTO streams (
			id, session_id, runner_id, tenant_id, type, state,
			signaling_url, ice_servers,
			resolution_width, resolution_height, frame_rate, bitrate,
			audio_enabled, input_enabled, provider_name,
			metadata, expires_at, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8,
			$9, $10, $11, $12,
			$13, $14, $15,
			$16, $17, NOW(), NOW()
		)
		RETURNING id, session_id, runner_id, tenant_id, type, state,
			signaling_url, ice_servers,
			resolution_width, resolution_height, frame_rate, bitrate,
			audio_enabled, input_enabled, provider_name, provider_stream_id,
			error, metadata, created_at, updated_at, started_at, stopped_at, expires_at
	`

	row := s.pool.QueryRow(ctx, query,
		streamID,
		params.SessionID,
		nullString(params.RunnerID),
		nullString(params.TenantID),
		string(params.Type),
		string(streaming.StreamStatePending),
		nullString(params.SignalingURL),
		iceServersJSON,
		nullInt(params.Resolution.Width),
		nullInt(params.Resolution.Height),
		nullInt(params.FrameRate),
		nullInt(params.BitRate),
		params.AudioEnabled,
		params.InputEnabled,
		params.ProviderName,
		metadataJSON,
		nullTime(params.ExpiresAt),
	)

	stream, err := scanStream(row)
	if err != nil {
		return nil, handlePgError(err, "stream", streamID)
	}

	return stream, nil
}

// GetStream retrieves a stream by ID.
func (s *Store) GetStream(ctx context.Context, streamID string) (*streaming.Stream, error) {
	query := `
		SELECT id, session_id, runner_id, tenant_id, type, state,
			signaling_url, ice_servers,
			resolution_width, resolution_height, frame_rate, bitrate,
			audio_enabled, input_enabled, provider_name, provider_stream_id,
			error, metadata, created_at, updated_at, started_at, stopped_at, expires_at
		FROM streams
		WHERE id = $1
	`

	row := s.pool.QueryRow(ctx, query, streamID)
	stream, err := scanStream(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, handlePgError(err, "stream", streamID)
	}

	return stream, nil
}

// GetStreamBySession retrieves the active stream for a session.
func (s *Store) GetStreamBySession(ctx context.Context, sessionID string, streamType streaming.StreamType) (*streaming.Stream, error) {
	query := `
		SELECT id, session_id, runner_id, tenant_id, type, state,
			signaling_url, ice_servers,
			resolution_width, resolution_height, frame_rate, bitrate,
			audio_enabled, input_enabled, provider_name, provider_stream_id,
			error, metadata, created_at, updated_at, started_at, stopped_at, expires_at
		FROM streams
		WHERE session_id = $1 AND type = $2
			AND state NOT IN ('stopped', 'error')
		ORDER BY created_at DESC
		LIMIT 1
	`

	row := s.pool.QueryRow(ctx, query, sessionID, string(streamType))
	stream, err := scanStream(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, handlePgError(err, "stream", "")
	}

	return stream, nil
}

// UpdateStream updates a stream record.
func (s *Store) UpdateStream(ctx context.Context, streamID string, params streaming.UpdateStreamParams) (*streaming.Stream, error) {
	// Build dynamic update query
	setClauses := []string{"updated_at = NOW()"}
	args := []any{streamID}
	argNum := 2

	if params.State != nil {
		setClauses = append(setClauses, fmt.Sprintf("state = $%d", argNum))
		args = append(args, string(*params.State))
		argNum++
	}
	if params.SignalingURL != nil {
		setClauses = append(setClauses, fmt.Sprintf("signaling_url = $%d", argNum))
		args = append(args, *params.SignalingURL)
		argNum++
	}
	if params.ProviderStreamID != nil {
		setClauses = append(setClauses, fmt.Sprintf("provider_stream_id = $%d", argNum))
		args = append(args, *params.ProviderStreamID)
		argNum++
	}
	if params.Resolution != nil {
		setClauses = append(setClauses, fmt.Sprintf("resolution_width = $%d", argNum))
		args = append(args, params.Resolution.Width)
		argNum++
		setClauses = append(setClauses, fmt.Sprintf("resolution_height = $%d", argNum))
		args = append(args, params.Resolution.Height)
		argNum++
	}
	if params.FrameRate != nil {
		setClauses = append(setClauses, fmt.Sprintf("frame_rate = $%d", argNum))
		args = append(args, *params.FrameRate)
		argNum++
	}
	if params.BitRate != nil {
		setClauses = append(setClauses, fmt.Sprintf("bitrate = $%d", argNum))
		args = append(args, *params.BitRate)
		argNum++
	}
	if params.Error != nil {
		setClauses = append(setClauses, fmt.Sprintf("error = $%d", argNum))
		args = append(args, *params.Error)
		argNum++
	}
	if params.StartedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("started_at = $%d", argNum))
		args = append(args, *params.StartedAt)
		argNum++
	}
	if params.StoppedAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("stopped_at = $%d", argNum))
		args = append(args, *params.StoppedAt)
		argNum++
	}
	if params.Metadata != nil {
		metadataJSON, err := json.Marshal(params.Metadata)
		if err != nil {
			return nil, fmt.Errorf("marshaling metadata: %w", err)
		}
		setClauses = append(setClauses, fmt.Sprintf("metadata = $%d", argNum))
		args = append(args, metadataJSON)
		argNum++
	}

	query := fmt.Sprintf(`
		UPDATE streams
		SET %s
		WHERE id = $1
		RETURNING id, session_id, runner_id, tenant_id, type, state,
			signaling_url, ice_servers,
			resolution_width, resolution_height, frame_rate, bitrate,
			audio_enabled, input_enabled, provider_name, provider_stream_id,
			error, metadata, created_at, updated_at, started_at, stopped_at, expires_at
	`, joinStrings(setClauses, ", "))

	row := s.pool.QueryRow(ctx, query, args...)
	stream, err := scanStream(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, &store.NotFoundError{Resource: "stream", ID: streamID}
		}
		return nil, handlePgError(err, "stream", streamID)
	}

	return stream, nil
}

// DeleteStream deletes a stream record.
func (s *Store) DeleteStream(ctx context.Context, streamID string) error {
	query := `DELETE FROM streams WHERE id = $1`
	result, err := s.pool.Exec(ctx, query, streamID)
	if err != nil {
		return handlePgError(err, "stream", streamID)
	}
	if result.RowsAffected() == 0 {
		return &store.NotFoundError{Resource: "stream", ID: streamID}
	}
	return nil
}

// ListStreams lists streams matching the given parameters.
func (s *Store) ListStreams(ctx context.Context, params streaming.ListStreamsParams) ([]*streaming.Stream, error) {
	whereClauses := []string{"1=1"}
	args := []any{}
	argNum := 1

	if params.SessionID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("session_id = $%d", argNum))
		args = append(args, params.SessionID)
		argNum++
	}
	if params.RunnerID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("runner_id = $%d", argNum))
		args = append(args, params.RunnerID)
		argNum++
	}
	if params.TenantID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("tenant_id = $%d", argNum))
		args = append(args, params.TenantID)
		argNum++
	}
	if params.Type != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("type = $%d", argNum))
		args = append(args, string(*params.Type))
		argNum++
	}
	if params.State != nil {
		whereClauses = append(whereClauses, fmt.Sprintf("state = $%d", argNum))
		args = append(args, string(*params.State))
		argNum++
	}
	if params.ActiveOnly {
		whereClauses = append(whereClauses, "state NOT IN ('stopped', 'error')")
	}

	query := fmt.Sprintf(`
		SELECT id, session_id, runner_id, tenant_id, type, state,
			signaling_url, ice_servers,
			resolution_width, resolution_height, frame_rate, bitrate,
			audio_enabled, input_enabled, provider_name, provider_stream_id,
			error, metadata, created_at, updated_at, started_at, stopped_at, expires_at
		FROM streams
		WHERE %s
		ORDER BY created_at DESC
	`, joinStrings(whereClauses, " AND "))

	if params.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", params.Limit)
	}
	if params.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", params.Offset)
	}

	rows, err := s.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, handlePgError(err, "streams", "")
	}
	defer rows.Close()

	var streams []*streaming.Stream
	for rows.Next() {
		stream, err := scanStreamFromRows(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning stream: %w", err)
		}
		streams = append(streams, stream)
	}

	if err := rows.Err(); err != nil {
		return nil, handlePgError(err, "streams", "")
	}

	return streams, nil
}

// CleanupExpiredStreams removes streams that have expired.
func (s *Store) CleanupExpiredStreams(ctx context.Context) (int, error) {
	query := `
		UPDATE streams
		SET state = 'stopped', stopped_at = NOW(), updated_at = NOW()
		WHERE expires_at IS NOT NULL
			AND expires_at < NOW()
			AND state NOT IN ('stopped', 'error')
	`

	result, err := s.pool.Exec(ctx, query)
	if err != nil {
		return 0, handlePgError(err, "streams", "")
	}

	return int(result.RowsAffected()), nil
}

// scanStream scans a single stream row.
func scanStream(row pgx.Row) (*streaming.Stream, error) {
	var stream streaming.Stream
	var runnerID, tenantID, signalingURL, providerStreamID, errorStr *string
	var resWidth, resHeight, frameRate, bitrate *int
	var startedAt, stoppedAt, expiresAt *time.Time
	var iceServersJSON, metadataJSON []byte
	var streamType, state string

	err := row.Scan(
		&stream.ID,
		&stream.SessionID,
		&runnerID,
		&tenantID,
		&streamType,
		&state,
		&signalingURL,
		&iceServersJSON,
		&resWidth,
		&resHeight,
		&frameRate,
		&bitrate,
		&stream.AudioEnabled,
		&stream.InputEnabled,
		&stream.ProviderName,
		&providerStreamID,
		&errorStr,
		&metadataJSON,
		&stream.CreatedAt,
		&stream.UpdatedAt,
		&startedAt,
		&stoppedAt,
		&expiresAt,
	)
	if err != nil {
		return nil, err
	}

	// Set nullable fields
	if runnerID != nil {
		stream.RunnerID = *runnerID
	}
	if tenantID != nil {
		stream.TenantID = *tenantID
	}
	if signalingURL != nil {
		stream.SignalingURL = *signalingURL
	}
	if providerStreamID != nil {
		stream.ProviderStreamID = *providerStreamID
	}
	if errorStr != nil {
		stream.Error = *errorStr
	}
	if resWidth != nil {
		stream.Resolution.Width = *resWidth
	}
	if resHeight != nil {
		stream.Resolution.Height = *resHeight
	}
	if frameRate != nil {
		stream.FrameRate = *frameRate
	}
	if bitrate != nil {
		stream.BitRate = *bitrate
	}
	stream.StartedAt = startedAt
	stream.StoppedAt = stoppedAt
	stream.ExpiresAt = expiresAt

	stream.Type = streaming.StreamType(streamType)
	stream.State = streaming.StreamState(state)

	// Unmarshal JSON fields
	if len(iceServersJSON) > 0 {
		if err := json.Unmarshal(iceServersJSON, &stream.ICEServers); err != nil {
			return nil, fmt.Errorf("unmarshaling ice_servers: %w", err)
		}
	}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &stream.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshaling metadata: %w", err)
		}
	}

	return &stream, nil
}

// scanStreamFromRows scans a stream from rows iterator.
func scanStreamFromRows(rows pgx.Rows) (*streaming.Stream, error) {
	var stream streaming.Stream
	var runnerID, tenantID, signalingURL, providerStreamID, errorStr *string
	var resWidth, resHeight, frameRate, bitrate *int
	var startedAt, stoppedAt, expiresAt *time.Time
	var iceServersJSON, metadataJSON []byte
	var streamType, state string

	err := rows.Scan(
		&stream.ID,
		&stream.SessionID,
		&runnerID,
		&tenantID,
		&streamType,
		&state,
		&signalingURL,
		&iceServersJSON,
		&resWidth,
		&resHeight,
		&frameRate,
		&bitrate,
		&stream.AudioEnabled,
		&stream.InputEnabled,
		&stream.ProviderName,
		&providerStreamID,
		&errorStr,
		&metadataJSON,
		&stream.CreatedAt,
		&stream.UpdatedAt,
		&startedAt,
		&stoppedAt,
		&expiresAt,
	)
	if err != nil {
		return nil, err
	}

	// Set nullable fields
	if runnerID != nil {
		stream.RunnerID = *runnerID
	}
	if tenantID != nil {
		stream.TenantID = *tenantID
	}
	if signalingURL != nil {
		stream.SignalingURL = *signalingURL
	}
	if providerStreamID != nil {
		stream.ProviderStreamID = *providerStreamID
	}
	if errorStr != nil {
		stream.Error = *errorStr
	}
	if resWidth != nil {
		stream.Resolution.Width = *resWidth
	}
	if resHeight != nil {
		stream.Resolution.Height = *resHeight
	}
	if frameRate != nil {
		stream.FrameRate = *frameRate
	}
	if bitrate != nil {
		stream.BitRate = *bitrate
	}
	stream.StartedAt = startedAt
	stream.StoppedAt = stoppedAt
	stream.ExpiresAt = expiresAt

	stream.Type = streaming.StreamType(streamType)
	stream.State = streaming.StreamState(state)

	// Unmarshal JSON fields
	if len(iceServersJSON) > 0 {
		if err := json.Unmarshal(iceServersJSON, &stream.ICEServers); err != nil {
			return nil, fmt.Errorf("unmarshaling ice_servers: %w", err)
		}
	}
	if len(metadataJSON) > 0 {
		if err := json.Unmarshal(metadataJSON, &stream.Metadata); err != nil {
			return nil, fmt.Errorf("unmarshaling metadata: %w", err)
		}
	}

	return &stream, nil
}

// Helper functions for nullable values
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func nullInt(i int) *int {
	if i == 0 {
		return nil
	}
	return &i
}

func nullTime(t *time.Time) *time.Time {
	return t
}

func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}
