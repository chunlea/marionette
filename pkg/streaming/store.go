package streaming

import (
	"context"
	"time"
)

// CreateStreamParams contains parameters for creating a stream.
type CreateStreamParams struct {
	ID           string
	SessionID    string
	RunnerID     string
	TenantID     string
	Type         StreamType
	Resolution   Resolution
	FrameRate    int
	BitRate      int
	AudioEnabled bool
	InputEnabled bool
	ICEServers   []ICEServer
	ProviderName string
	Metadata     map[string]string
	ExpiresAt    *time.Time
}

// UpdateStreamParams contains parameters for updating a stream.
type UpdateStreamParams struct {
	State            *StreamState
	SignalingURL     *string
	ProviderStreamID *string
	Resolution       *Resolution
	FrameRate        *int
	BitRate          *int
	VideoCodec       *string
	AudioCodec       *string
	Error            *string
	StartedAt        *time.Time
	StoppedAt        *time.Time
	Metadata         map[string]string
}

// ListStreamsParams contains parameters for listing streams.
type ListStreamsParams struct {
	SessionID    string
	RunnerID     string
	TenantID     string
	Type         *StreamType
	State        *StreamState
	ActiveOnly   bool // If true, only return non-terminal streams
	Limit        int
	Offset       int
	OrderBy      string // created_at, updated_at
	OrderDesc    bool
}

// StreamStore defines the interface for stream persistence.
type StreamStore interface {
	// CreateStream creates a new stream record.
	CreateStream(ctx context.Context, params CreateStreamParams) (*Stream, error)

	// GetStream retrieves a stream by ID.
	// Returns ErrStreamNotFound if not found.
	GetStream(ctx context.Context, id string) (*Stream, error)

	// GetStreamBySession retrieves the active stream for a session and type.
	// Returns ErrStreamNotFound if not found.
	GetStreamBySession(ctx context.Context, sessionID string, streamType StreamType) (*Stream, error)

	// UpdateStream updates a stream record.
	// Returns ErrStreamNotFound if not found.
	UpdateStream(ctx context.Context, id string, params UpdateStreamParams) (*Stream, error)

	// DeleteStream deletes a stream record.
	// Returns ErrStreamNotFound if not found.
	DeleteStream(ctx context.Context, id string) error

	// ListStreams lists streams matching the given parameters.
	// Returns the streams and total count.
	ListStreams(ctx context.Context, params ListStreamsParams) ([]*Stream, int, error)

	// CleanupExpiredStreams marks expired streams as stopped.
	// Returns the number of streams cleaned up.
	CleanupExpiredStreams(ctx context.Context) (int, error)
}
