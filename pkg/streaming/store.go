package streaming

import (
	"context"
	"time"
)

// Stream represents a desktop/browser/mobile stream record stored in the database.
// This extends the tunnel concept for streaming purposes.
type Stream struct {
	// ID is the unique identifier for this stream (prefix: strm_).
	ID string

	// SessionID is the session this stream belongs to.
	SessionID string

	// RunnerID is the runner providing this stream.
	RunnerID string

	// TenantID is the tenant this stream belongs to.
	TenantID string

	// Type specifies the stream type (desktop, browser, ios, android).
	Type StreamType

	// State is the current state of the stream.
	State StreamState

	// SignalingURL is the WebSocket URL for WebRTC signaling.
	SignalingURL string

	// ICEServers are the STUN/TURN servers configuration (JSON).
	ICEServers []ICEServer

	// Resolution is the stream resolution.
	Resolution Resolution

	// FrameRate is the stream frame rate.
	FrameRate int

	// BitRate is the stream bitrate in kbps.
	BitRate int

	// AudioEnabled indicates if audio streaming is enabled.
	AudioEnabled bool

	// InputEnabled indicates if input forwarding is enabled.
	InputEnabled bool

	// ProviderName is the name of the stream provider (e.g., "selkies").
	ProviderName string

	// ProviderStreamID is the provider's internal stream identifier.
	ProviderStreamID string

	// Error contains error information if State is error.
	Error string

	// Metadata contains additional provider-specific information.
	Metadata map[string]string

	// CreatedAt is when the stream was created.
	CreatedAt time.Time

	// UpdatedAt is when the stream was last updated.
	UpdatedAt time.Time

	// StartedAt is when the stream became active.
	StartedAt *time.Time

	// StoppedAt is when the stream was stopped.
	StoppedAt *time.Time

	// ExpiresAt is when the stream should automatically expire.
	ExpiresAt *time.Time
}

// CreateStreamParams contains parameters for creating a new stream.
type CreateStreamParams struct {
	ID           string
	SessionID    string
	RunnerID     string
	TenantID     string
	Type         StreamType
	ProviderName string
	SignalingURL string
	ICEServers   []ICEServer
	Resolution   Resolution
	FrameRate    int
	BitRate      int
	AudioEnabled bool
	InputEnabled bool
	ExpiresAt    *time.Time
	Metadata     map[string]string
}

// UpdateStreamParams contains parameters for updating a stream.
type UpdateStreamParams struct {
	State            *StreamState
	SignalingURL     *string
	ProviderStreamID *string
	Resolution       *Resolution
	FrameRate        *int
	BitRate          *int
	Error            *string
	StartedAt        *time.Time
	StoppedAt        *time.Time
	Metadata         map[string]string
}

// ListStreamsParams contains parameters for listing streams.
type ListStreamsParams struct {
	// SessionID filters by session.
	SessionID string

	// RunnerID filters by runner.
	RunnerID string

	// TenantID filters by tenant.
	TenantID string

	// Type filters by stream type.
	Type *StreamType

	// State filters by stream state.
	State *StreamState

	// ActiveOnly returns only active streams (state not stopped/error).
	ActiveOnly bool

	// Limit limits the number of results.
	Limit int

	// Offset for pagination.
	Offset int
}

// StreamStore defines the interface for stream persistence operations.
type StreamStore interface {
	// CreateStream creates a new stream record.
	CreateStream(ctx context.Context, params CreateStreamParams) (*Stream, error)

	// GetStream retrieves a stream by ID.
	GetStream(ctx context.Context, id string) (*Stream, error)

	// GetStreamBySession retrieves the active stream for a session.
	// Returns nil if no active stream exists.
	GetStreamBySession(ctx context.Context, sessionID string, streamType StreamType) (*Stream, error)

	// UpdateStream updates a stream record.
	UpdateStream(ctx context.Context, id string, params UpdateStreamParams) (*Stream, error)

	// DeleteStream deletes a stream record.
	DeleteStream(ctx context.Context, id string) error

	// ListStreams lists streams matching the given parameters.
	ListStreams(ctx context.Context, params ListStreamsParams) ([]*Stream, error)

	// CleanupExpiredStreams removes streams that have expired.
	// Returns the number of streams cleaned up.
	CleanupExpiredStreams(ctx context.Context) (int, error)
}
