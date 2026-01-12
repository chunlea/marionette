package streaming

import (
	"context"
	"encoding/json"

	"github.com/chunlea/marionette/pkg/store"
)

// StoreAdapter adapts store.Store to StreamStore interface.
// It handles type conversion between store.Stream and streaming.Stream.
type StoreAdapter struct {
	store store.Store
}

// NewStoreAdapter creates a new store adapter.
func NewStoreAdapter(s store.Store) *StoreAdapter {
	return &StoreAdapter{store: s}
}

// CreateStream creates a new stream record.
func (a *StoreAdapter) CreateStream(ctx context.Context, params CreateStreamParams) (*Stream, error) {
	// Convert ICEServers to JSON
	iceServersJSON, err := json.Marshal(params.ICEServers)
	if err != nil {
		return nil, err
	}

	// Convert Metadata to JSON
	var metadataJSON json.RawMessage
	if params.Metadata != nil {
		metadataJSON, err = json.Marshal(params.Metadata)
		if err != nil {
			return nil, err
		}
	}

	// Build store.Stream
	storeStream := &store.Stream{
		ID:           params.ID,
		SessionID:    params.SessionID,
		Type:         string(params.Type),
		State:        string(StreamStatePending),
		ICEServers:   iceServersJSON,
		AudioEnabled: params.AudioEnabled,
		InputEnabled: params.InputEnabled,
		ProviderName: params.ProviderName,
		Metadata:     metadataJSON,
	}

	// Set optional fields
	if params.RunnerID != "" {
		storeStream.RunnerID = &params.RunnerID
	}
	if params.TenantID != "" {
		storeStream.TenantID = &params.TenantID
	}
	if params.Resolution.Width > 0 {
		storeStream.ResolutionWidth = &params.Resolution.Width
	}
	if params.Resolution.Height > 0 {
		storeStream.ResolutionHeight = &params.Resolution.Height
	}
	if params.FrameRate > 0 {
		storeStream.FrameRate = &params.FrameRate
	}
	if params.BitRate > 0 {
		storeStream.BitRate = &params.BitRate
	}
	if params.ExpiresAt != nil {
		storeStream.ExpiresAt = params.ExpiresAt
	}

	// Create in store
	if err := a.store.CreateStream(ctx, storeStream); err != nil {
		return nil, err
	}

	// Convert back to streaming.Stream
	return storeStreamToStreaming(storeStream), nil
}

// GetStream retrieves a stream by ID.
func (a *StoreAdapter) GetStream(ctx context.Context, id string) (*Stream, error) {
	storeStream, err := a.store.GetStream(ctx, id)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, ErrStreamNotFound
		}
		return nil, err
	}
	return storeStreamToStreaming(storeStream), nil
}

// GetStreamBySession retrieves the active stream for a session and type.
func (a *StoreAdapter) GetStreamBySession(ctx context.Context, sessionID string, streamType StreamType) (*Stream, error) {
	storeStream, err := a.store.GetStreamBySessionAndType(ctx, sessionID, string(streamType), true)
	if err != nil {
		if err == store.ErrNotFound {
			return nil, ErrStreamNotFound
		}
		return nil, err
	}
	return storeStreamToStreaming(storeStream), nil
}

// UpdateStream updates a stream record.
func (a *StoreAdapter) UpdateStream(ctx context.Context, id string, params UpdateStreamParams) (*Stream, error) {
	// Build store.StreamUpdates
	updates := store.StreamUpdates{}

	if params.State != nil {
		state := string(*params.State)
		updates.State = &state
	}
	if params.SignalingURL != nil {
		updates.SignalingURL = params.SignalingURL
	}
	if params.Resolution != nil {
		updates.ResolutionWidth = &params.Resolution.Width
		updates.ResolutionHeight = &params.Resolution.Height
	}
	if params.FrameRate != nil {
		updates.FrameRate = params.FrameRate
	}
	if params.BitRate != nil {
		updates.BitRate = params.BitRate
	}
	if params.VideoCodec != nil {
		updates.VideoCodec = params.VideoCodec
	}
	if params.AudioCodec != nil {
		updates.AudioCodec = params.AudioCodec
	}
	if params.ProviderStreamID != nil {
		updates.ProviderStreamID = params.ProviderStreamID
	}
	if params.Error != nil {
		updates.Error = params.Error
	}
	if params.Metadata != nil {
		metadataJSON, err := json.Marshal(params.Metadata)
		if err != nil {
			return nil, err
		}
		updates.Metadata = metadataJSON
	}
	if params.StartedAt != nil {
		updates.StartedAt = params.StartedAt
	}
	if params.StoppedAt != nil {
		updates.StoppedAt = params.StoppedAt
	}

	// Update in store
	if err := a.store.UpdateStream(ctx, id, updates); err != nil {
		if err == store.ErrNotFound {
			return nil, ErrStreamNotFound
		}
		return nil, err
	}

	// Get the updated stream
	return a.GetStream(ctx, id)
}

// DeleteStream deletes a stream record.
func (a *StoreAdapter) DeleteStream(ctx context.Context, id string) error {
	if err := a.store.DeleteStream(ctx, id); err != nil {
		if err == store.ErrNotFound {
			return ErrStreamNotFound
		}
		return err
	}
	return nil
}

// ListStreams lists streams matching the given parameters.
func (a *StoreAdapter) ListStreams(ctx context.Context, params ListStreamsParams) ([]*Stream, int, error) {
	// Build store.ListStreamsOptions
	opts := store.ListStreamsOptions{
		ActiveOnly: params.ActiveOnly,
	}
	opts.Limit = params.Limit
	opts.OrderBy = params.OrderBy
	opts.OrderDesc = params.OrderDesc

	if params.SessionID != "" {
		opts.SessionID = &params.SessionID
	}
	if params.RunnerID != "" {
		opts.RunnerID = &params.RunnerID
	}
	if params.Type != nil {
		opts.Type = []string{string(*params.Type)}
	}
	if params.State != nil {
		opts.State = []string{string(*params.State)}
	}

	// List from store
	result, err := a.store.ListStreams(ctx, opts)
	if err != nil {
		return nil, 0, err
	}

	// Convert results
	streams := make([]*Stream, len(result.Items))
	for i, item := range result.Items {
		streams[i] = storeStreamToStreaming(item)
	}

	return streams, int(result.TotalCount), nil
}

// CleanupExpiredStreams marks expired streams as stopped.
func (a *StoreAdapter) CleanupExpiredStreams(ctx context.Context) (int, error) {
	return a.store.CleanupExpiredStreams(ctx)
}

// storeStreamToStreaming converts a store.Stream to streaming.Stream.
func storeStreamToStreaming(s *store.Stream) *Stream {
	stream := &Stream{
		ID:           s.ID,
		SessionID:    s.SessionID,
		Type:         StreamType(s.Type),
		State:        StreamState(s.State),
		AudioEnabled: s.AudioEnabled,
		InputEnabled: s.InputEnabled,
		ProviderName: s.ProviderName,
		CreatedAt:    s.CreatedAt,
		UpdatedAt:    s.UpdatedAt,
		StartedAt:    s.StartedAt,
		StoppedAt:    s.StoppedAt,
		ExpiresAt:    s.ExpiresAt,
	}

	// Convert pointer fields
	if s.RunnerID != nil {
		stream.RunnerID = *s.RunnerID
	}
	if s.TenantID != nil {
		stream.TenantID = *s.TenantID
	}
	if s.SignalingURL != nil {
		stream.SignalingURL = *s.SignalingURL
	}
	if s.ResolutionWidth != nil && s.ResolutionHeight != nil {
		stream.Resolution = Resolution{
			Width:  *s.ResolutionWidth,
			Height: *s.ResolutionHeight,
		}
	}
	if s.FrameRate != nil {
		stream.FrameRate = *s.FrameRate
	}
	if s.BitRate != nil {
		stream.BitRate = *s.BitRate
	}
	if s.VideoCodec != nil {
		stream.VideoCodec = *s.VideoCodec
	}
	if s.AudioCodec != nil {
		stream.AudioCodec = *s.AudioCodec
	}
	if s.ProviderStreamID != nil {
		stream.ProviderStreamID = *s.ProviderStreamID
	}
	if s.Error != nil {
		stream.Error = *s.Error
	}

	// Parse ICEServers from JSON
	if len(s.ICEServers) > 0 {
		var iceServers []ICEServer
		if err := json.Unmarshal(s.ICEServers, &iceServers); err == nil {
			stream.ICEServers = iceServers
		}
	}

	// Parse Metadata from JSON
	if len(s.Metadata) > 0 {
		var metadata map[string]string
		if err := json.Unmarshal(s.Metadata, &metadata); err == nil {
			stream.Metadata = metadata
		}
	}

	return stream
}
