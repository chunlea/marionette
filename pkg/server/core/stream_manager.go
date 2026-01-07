package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/chunlea/marionette/pkg/streaming/sfu"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// StreamManagerInterface defines the stream manager operations.
type StreamManagerInterface interface {
	// StartDesktopStream starts a desktop stream for a session.
	StartDesktopStream(ctx context.Context, input *StartStreamInput) (*StreamInfo, error)

	// StopDesktopStream stops a desktop stream.
	StopDesktopStream(ctx context.Context, streamID string) error

	// GetStream returns stream information.
	GetStream(ctx context.Context, streamID string) (*StreamInfo, error)

	// ListSessionStreams lists all streams for a session.
	ListSessionStreams(ctx context.Context, sessionID string) ([]*StreamInfo, error)

	// OnStreamStarted handles stream started notification from runner.
	OnStreamStarted(ctx context.Context, msg *StreamStartedMessage) error

	// OnStreamStopped handles stream stopped notification from runner.
	OnStreamStopped(ctx context.Context, msg *StreamStoppedMessage) error

	// OnStreamError handles stream error notification from runner.
	OnStreamError(ctx context.Context, msg *StreamErrorMessage) error

	// GetSFU returns the SFU for WebRTC operations.
	GetSFU() *sfu.SFU

	// Close closes the stream manager.
	Close(ctx context.Context) error
}

// StartStreamInput contains input for starting a stream.
type StartStreamInput struct {
	SessionID string
	Config    *StreamConfig
	TenantID  *string
}

// StreamConfig contains stream configuration.
type StreamConfig struct {
	Width        int
	Height       int
	FrameRate    int
	Bitrate      int
	VideoCodec   string
	AudioEnabled bool
	InputEnabled bool
	Display      string
	HWAccel      string
}

// StreamInfo contains stream information.
type StreamInfo struct {
	ID           string
	SessionID    string
	RunnerID     string
	Status       string
	SignalingURL string
	Config       *StreamConfig
	Provider     string
	StartedAt    *time.Time
	StoppedAt    *time.Time
	TenantID     *string
}

// StreamStartedMessage contains stream started notification.
type StreamStartedMessage struct {
	StreamID        string
	SessionID       string
	ActualConfig    *StreamConfig
	SignalingURL    string
	Provider        string
	ProviderVersion string
	StartedAt       time.Time
}

// StreamStoppedMessage contains stream stopped notification.
type StreamStoppedMessage struct {
	StreamID  string
	SessionID string
	Reason    string
	StoppedAt time.Time
}

// StreamErrorMessage contains stream error notification.
type StreamErrorMessage struct {
	StreamID    string
	SessionID   string
	Error       string
	ErrorCode   string
	Recoverable bool
	Timestamp   time.Time
}

// StreamManager manages desktop streaming.
type StreamManager struct {
	logger *zap.Logger
	store  store.Store
	sfuMgr *sfu.SFU

	// Active streams
	mu      sync.RWMutex
	streams map[string]*activeStream // streamID -> activeStream

	// Configuration
	defaultICEServers []streaming.ICEServer
	signalingBaseURL  string
}

// activeStream tracks a running stream.
type activeStream struct {
	info      *StreamInfo
	room      *sfu.Room
	createdAt time.Time
}

// StreamManagerOption is a functional option for StreamManager.
type StreamManagerOption func(*StreamManager)

// WithSMStore sets the store for the stream manager.
func WithSMStore(s store.Store) StreamManagerOption {
	return func(m *StreamManager) {
		m.store = s
	}
}

// WithSMICEServers sets the default ICE servers.
func WithSMICEServers(servers []streaming.ICEServer) StreamManagerOption {
	return func(m *StreamManager) {
		m.defaultICEServers = servers
	}
}

// WithSMSignalingBaseURL sets the signaling server base URL.
func WithSMSignalingBaseURL(url string) StreamManagerOption {
	return func(m *StreamManager) {
		m.signalingBaseURL = url
	}
}

// NewStreamManager creates a new StreamManager.
func NewStreamManager(logger *zap.Logger, opts ...StreamManagerOption) (*StreamManager, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Create SFU
	sfuConfig := sfu.DefaultConfig()
	sfuMgr, err := sfu.New(sfuConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("creating SFU: %w", err)
	}

	m := &StreamManager{
		logger:  logger.Named("stream_manager"),
		sfuMgr:  sfuMgr,
		streams: make(map[string]*activeStream),
		defaultICEServers: []streaming.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	for _, opt := range opts {
		opt(m)
	}

	return m, nil
}

// StartDesktopStream starts a desktop stream for a session.
func (m *StreamManager) StartDesktopStream(ctx context.Context, input *StartStreamInput) (*StreamInfo, error) {
	// Generate stream ID
	streamID := id.New("strm")

	m.logger.Info("starting desktop stream",
		zap.String("stream_id", streamID),
		zap.String("session_id", input.SessionID),
	)

	// Create SFU room for this stream
	room, err := m.sfuMgr.CreateRoom(streamID)
	if err != nil {
		return nil, fmt.Errorf("creating SFU room: %w", err)
	}

	// Build stream info
	info := &StreamInfo{
		ID:           streamID,
		SessionID:    input.SessionID,
		Status:       "pending",
		SignalingURL: m.buildSignalingURL(streamID),
		Config:       input.Config,
		TenantID:     input.TenantID,
	}

	// Track active stream
	m.mu.Lock()
	m.streams[streamID] = &activeStream{
		info:      info,
		room:      room,
		createdAt: time.Now(),
	}
	m.mu.Unlock()

	m.logger.Info("desktop stream created",
		zap.String("stream_id", streamID),
		zap.String("session_id", input.SessionID),
	)

	return info, nil
}

// StopDesktopStream stops a desktop stream.
func (m *StreamManager) StopDesktopStream(ctx context.Context, streamID string) error {
	m.mu.Lock()
	stream, ok := m.streams[streamID]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("stream not found: %s", streamID)
	}
	delete(m.streams, streamID)
	m.mu.Unlock()

	// Close SFU room
	if stream.room != nil {
		if err := stream.room.Close(ctx); err != nil {
			m.logger.Warn("error closing SFU room",
				zap.String("stream_id", streamID),
				zap.Error(err),
			)
		}
	}

	// Remove room from SFU
	if err := m.sfuMgr.RemoveRoom(ctx, streamID); err != nil {
		m.logger.Warn("error removing SFU room",
			zap.String("stream_id", streamID),
			zap.Error(err),
		)
	}

	m.logger.Info("desktop stream stopped",
		zap.String("stream_id", streamID),
	)

	return nil
}

// GetStream returns stream information.
func (m *StreamManager) GetStream(_ context.Context, streamID string) (*StreamInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stream, ok := m.streams[streamID]
	if !ok {
		return nil, fmt.Errorf("stream not found: %s", streamID)
	}

	return stream.info, nil
}

// ListSessionStreams lists all streams for a session.
func (m *StreamManager) ListSessionStreams(_ context.Context, sessionID string) ([]*StreamInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var streams []*StreamInfo
	for _, s := range m.streams {
		if s.info.SessionID == sessionID {
			streams = append(streams, s.info)
		}
	}

	return streams, nil
}

// OnStreamStarted handles stream started notification from runner.
func (m *StreamManager) OnStreamStarted(_ context.Context, msg *StreamStartedMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	stream, ok := m.streams[msg.StreamID]
	if !ok {
		return fmt.Errorf("stream not found: %s", msg.StreamID)
	}

	stream.info.Status = "running"
	stream.info.Provider = msg.Provider
	stream.info.StartedAt = &msg.StartedAt

	if msg.ActualConfig != nil {
		stream.info.Config = msg.ActualConfig
	}

	m.logger.Info("stream started",
		zap.String("stream_id", msg.StreamID),
		zap.String("provider", msg.Provider),
	)

	return nil
}

// OnStreamStopped handles stream stopped notification from runner.
func (m *StreamManager) OnStreamStopped(ctx context.Context, msg *StreamStoppedMessage) error {
	m.mu.Lock()
	stream, ok := m.streams[msg.StreamID]
	if !ok {
		m.mu.Unlock()
		return nil // Already removed
	}

	stream.info.Status = "stopped"
	stream.info.StoppedAt = &msg.StoppedAt
	m.mu.Unlock()

	m.logger.Info("stream stopped notification received",
		zap.String("stream_id", msg.StreamID),
		zap.String("reason", msg.Reason),
	)

	// Clean up the stream
	return m.StopDesktopStream(ctx, msg.StreamID)
}

// OnStreamError handles stream error notification from runner.
func (m *StreamManager) OnStreamError(ctx context.Context, msg *StreamErrorMessage) error {
	m.mu.Lock()
	stream, ok := m.streams[msg.StreamID]
	if ok {
		stream.info.Status = "error"
	}
	m.mu.Unlock()

	m.logger.Error("stream error",
		zap.String("stream_id", msg.StreamID),
		zap.String("error", msg.Error),
		zap.String("error_code", msg.ErrorCode),
		zap.Bool("recoverable", msg.Recoverable),
	)

	// If not recoverable, stop the stream
	if !msg.Recoverable {
		return m.StopDesktopStream(ctx, msg.StreamID)
	}

	return nil
}

// GetSFU returns the SFU for WebRTC operations.
func (m *StreamManager) GetSFU() *sfu.SFU {
	return m.sfuMgr
}

// GetRoom returns the SFU room for a stream.
func (m *StreamManager) GetRoom(streamID string) (*sfu.Room, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stream, ok := m.streams[streamID]
	if !ok {
		return nil, false
	}

	return stream.room, true
}

// Close closes the stream manager.
func (m *StreamManager) Close(ctx context.Context) error {
	m.mu.Lock()
	streams := make([]string, 0, len(m.streams))
	for id := range m.streams {
		streams = append(streams, id)
	}
	m.mu.Unlock()

	// Stop all streams
	for _, id := range streams {
		if err := m.StopDesktopStream(ctx, id); err != nil {
			m.logger.Warn("error stopping stream during close",
				zap.String("stream_id", id),
				zap.Error(err),
			)
		}
	}

	// Close SFU
	if err := m.sfuMgr.Close(ctx); err != nil {
		return fmt.Errorf("closing SFU: %w", err)
	}

	m.logger.Info("stream manager closed")
	return nil
}

// buildSignalingURL constructs the signaling WebSocket URL for a stream.
func (m *StreamManager) buildSignalingURL(streamID string) string {
	if m.signalingBaseURL == "" {
		return fmt.Sprintf("/admin/api/v1/streams/%s/signaling", streamID)
	}
	return fmt.Sprintf("%s/streams/%s/signaling", m.signalingBaseURL, streamID)
}

// GetDefaultICEServers returns the default ICE servers.
func (m *StreamManager) GetDefaultICEServers() []streaming.ICEServer {
	return m.defaultICEServers
}
