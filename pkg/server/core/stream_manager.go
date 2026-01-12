package core

import (
	"context"
	"net/http"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/chunlea/marionette/pkg/streaming/manager"
	"github.com/chunlea/marionette/pkg/streaming/sfu"
)

// StreamManager integrates the streaming manager with the server.
// It provides an interface for HTTP handlers and WebSocket connections
// to interact with the streaming infrastructure.
type StreamManager struct {
	manager  *manager.Manager
	store    streaming.StreamStore
	logger   *zap.Logger
	upgrader websocket.Upgrader
}

// StreamManagerConfig contains configuration for the StreamManager.
type StreamManagerConfig struct {
	// Manager configuration
	Manager manager.Config

	// WebSocket configuration
	WebSocketReadBufferSize  int
	WebSocketWriteBufferSize int
	AllowedOrigins           []string
}

// DefaultStreamManagerConfig returns a default configuration.
func DefaultStreamManagerConfig() StreamManagerConfig {
	return StreamManagerConfig{
		Manager:                  manager.DefaultConfig(),
		WebSocketReadBufferSize:  1024,
		WebSocketWriteBufferSize: 1024,
		AllowedOrigins:           []string{"*"},
	}
}

// NewStreamManager creates a new StreamManager.
func NewStreamManager(config StreamManagerConfig, s store.Store, logger *zap.Logger) (*StreamManager, error) {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Create store adapter to bridge store.Store to streaming.StreamStore
	storeAdapter := streaming.NewStoreAdapter(s)

	// Create streaming manager
	mgr, err := manager.New(config.Manager, storeAdapter, logger)
	if err != nil {
		return nil, err
	}

	// Configure WebSocket upgrader
	upgrader := websocket.Upgrader{
		ReadBufferSize:  config.WebSocketReadBufferSize,
		WriteBufferSize: config.WebSocketWriteBufferSize,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			for _, allowed := range config.AllowedOrigins {
				if allowed == "*" || allowed == origin {
					return true
				}
			}
			return false
		},
	}

	return &StreamManager{
		manager:  mgr,
		store:    storeAdapter,
		logger:   logger.Named("stream_manager"),
		upgrader: upgrader,
	}, nil
}

// Start starts the stream manager.
func (m *StreamManager) Start(ctx context.Context) error {
	return m.manager.Start(ctx)
}

// Stop stops the stream manager.
func (m *StreamManager) Stop(ctx context.Context) error {
	return m.manager.Stop(ctx)
}

// RegisterProvider registers a stream provider.
func (m *StreamManager) RegisterProvider(provider streaming.StreamProvider) error {
	return m.manager.RegisterProvider(provider)
}

// UnregisterProvider unregisters a stream provider.
func (m *StreamManager) UnregisterProvider(name string) bool {
	return m.manager.UnregisterProvider(name)
}

// StartStream starts a new stream.
func (m *StreamManager) StartStream(ctx context.Context, opts streaming.StreamOptions) (*streaming.Stream, error) {
	return m.manager.StartStream(ctx, opts)
}

// StopStream stops a stream.
func (m *StreamManager) StopStream(ctx context.Context, streamID string) error {
	return m.manager.StopStream(ctx, streamID)
}

// GetStream returns a stream by ID.
func (m *StreamManager) GetStream(ctx context.Context, streamID string) (*streaming.Stream, error) {
	return m.manager.GetStream(ctx, streamID)
}

// GetStreamBySession returns the active stream for a session and type.
func (m *StreamManager) GetStreamBySession(ctx context.Context, sessionID string, streamType streaming.StreamType) (*streaming.Stream, error) {
	return m.manager.GetStreamBySession(ctx, sessionID, streamType)
}

// ListStreams lists streams matching the given parameters.
func (m *StreamManager) ListStreams(ctx context.Context, params streaming.ListStreamsParams) ([]*streaming.Stream, int, error) {
	return m.manager.ListStreams(ctx, params)
}

// ListSessionStreams lists all streams for a session.
func (m *StreamManager) ListSessionStreams(ctx context.Context, sessionID string) ([]*streaming.Stream, error) {
	return m.manager.ListSessionStreams(ctx, sessionID)
}

// GetSFU returns the SFU instance.
func (m *StreamManager) GetSFU() *sfu.SFU {
	return m.manager.GetSFU()
}

// GetSignalingHandler returns the signaling handler.
func (m *StreamManager) GetSignalingHandler() *sfu.SignalingHandler {
	return m.manager.GetSignalingHandler()
}

// Stats returns manager statistics.
func (m *StreamManager) Stats() manager.Stats {
	return m.manager.Stats()
}

// UpgradeWebSocket upgrades an HTTP connection to WebSocket.
func (m *StreamManager) UpgradeWebSocket(w http.ResponseWriter, r *http.Request) (*websocket.Conn, error) {
	return m.upgrader.Upgrade(w, r, nil)
}
