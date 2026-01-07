package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/chunlea/marionette/pkg/streaming/desktop"
	"go.uber.org/zap"
)

// DesktopStreamConfig contains configuration for desktop streaming.
type DesktopStreamConfig struct {
	// Provider configuration
	ProviderConfig desktop.SelkiesConfig

	// Enable desktop streaming
	Enabled bool

	// Default display (e.g., ":99")
	DefaultDisplay string
}

// DefaultDesktopStreamConfig returns default configuration.
func DefaultDesktopStreamConfig() DesktopStreamConfig {
	return DesktopStreamConfig{
		ProviderConfig: desktop.DefaultSelkiesConfig(),
		Enabled:        true,
		DefaultDisplay: ":99",
	}
}

// StreamState tracks state of an active stream.
type StreamState struct {
	StreamID   string
	SessionID  string
	Status     string
	Provider   string
	Config     *streaming.StreamOptions
	StartedAt  time.Time
	StoppedAt  *time.Time
}

// DesktopStreamManager manages desktop streaming on the agent.
type DesktopStreamManager struct {
	config   DesktopStreamConfig
	provider streaming.StreamProvider
	logger   *zap.Logger
	sendFunc func(*pb.RunnerMessage) error

	mu      sync.RWMutex
	streams map[string]*StreamState // streamID -> StreamState
}

// NewDesktopStreamManager creates a new DesktopStreamManager.
func NewDesktopStreamManager(config DesktopStreamConfig, logger *zap.Logger) *DesktopStreamManager {
	if logger == nil {
		logger = zap.NewNop()
	}

	// Create Selkies provider
	var provider streaming.StreamProvider
	if config.Enabled {
		provider = desktop.NewSelkiesProvider(config.ProviderConfig, logger)
	}

	return &DesktopStreamManager{
		config:   config,
		provider: provider,
		logger:   logger.Named("desktop_stream"),
		streams:  make(map[string]*StreamState),
	}
}

// SetSendFunc sets the function used to send messages to the server.
func (m *DesktopStreamManager) SetSendFunc(fn func(*pb.RunnerMessage) error) {
	m.sendFunc = fn
}

// HandleStartDesktopStream handles the StartDesktopStream command.
func (m *DesktopStreamManager) HandleStartDesktopStream(ctx context.Context, cmd *pb.StartDesktopStream) (*pb.RunnerMessage, error) {
	if m.provider == nil {
		return m.sendError(cmd.StreamId, cmd.SessionId, "desktop streaming not enabled", "PROVIDER_NOT_CONFIGURED", false)
	}

	m.logger.Info("starting desktop stream",
		zap.String("stream_id", cmd.StreamId),
		zap.String("session_id", cmd.SessionId),
		zap.String("signaling_url", cmd.SignalingUrl),
	)

	// Check if stream already exists
	m.mu.RLock()
	if _, exists := m.streams[cmd.StreamId]; exists {
		m.mu.RUnlock()
		return m.sendError(cmd.StreamId, cmd.SessionId, "stream already exists", "STREAM_EXISTS", false)
	}
	m.mu.RUnlock()

	// Build stream options from command
	opts := m.buildStreamOptions(cmd)

	// Start the stream provider
	info, err := m.provider.Start(ctx, opts)
	if err != nil {
		m.logger.Error("failed to start desktop stream",
			zap.String("stream_id", cmd.StreamId),
			zap.Error(err),
		)
		return m.sendError(cmd.StreamId, cmd.SessionId, err.Error(), "START_FAILED", true)
	}

	// Track the stream
	providerName := m.provider.Name()
	m.mu.Lock()
	m.streams[cmd.StreamId] = &StreamState{
		StreamID:  cmd.StreamId,
		SessionID: cmd.SessionId,
		Status:    "running",
		Provider:  providerName,
		Config:    &opts,
		StartedAt: time.Now(),
	}
	m.mu.Unlock()

	m.logger.Info("desktop stream started",
		zap.String("stream_id", cmd.StreamId),
		zap.String("provider", providerName),
	)

	// Send started notification
	return m.buildStartedResponse(cmd, info), nil
}

// HandleStopDesktopStream handles the StopDesktopStream command.
func (m *DesktopStreamManager) HandleStopDesktopStream(ctx context.Context, cmd *pb.StopDesktopStream) (*pb.RunnerMessage, error) {
	if m.provider == nil {
		return nil, nil
	}

	m.logger.Info("stopping desktop stream",
		zap.String("stream_id", cmd.StreamId),
		zap.String("session_id", cmd.SessionId),
		zap.String("reason", cmd.Reason),
	)

	// Check if stream exists
	m.mu.RLock()
	state, exists := m.streams[cmd.StreamId]
	m.mu.RUnlock()

	if !exists {
		m.logger.Warn("stream not found",
			zap.String("stream_id", cmd.StreamId),
		)
		return m.buildStoppedResponse(cmd.StreamId, cmd.SessionId, "stream not found"), nil
	}

	// Stop the stream provider
	if err := m.provider.Stop(ctx, state.StreamID); err != nil {
		m.logger.Error("failed to stop desktop stream",
			zap.String("stream_id", cmd.StreamId),
			zap.Error(err),
		)
		// Still continue to clean up
	}

	// Update state
	m.mu.Lock()
	if state, ok := m.streams[cmd.StreamId]; ok {
		now := time.Now()
		state.Status = "stopped"
		state.StoppedAt = &now
	}
	delete(m.streams, cmd.StreamId)
	m.mu.Unlock()

	m.logger.Info("desktop stream stopped",
		zap.String("stream_id", cmd.StreamId),
	)

	return m.buildStoppedResponse(cmd.StreamId, cmd.SessionId, cmd.Reason), nil
}

// StopAllStreams stops all active streams.
func (m *DesktopStreamManager) StopAllStreams(ctx context.Context) error {
	m.mu.RLock()
	streamIDs := make([]string, 0, len(m.streams))
	for id := range m.streams {
		streamIDs = append(streamIDs, id)
	}
	m.mu.RUnlock()

	for _, id := range streamIDs {
		m.mu.RLock()
		state := m.streams[id]
		m.mu.RUnlock()

		if state != nil {
			_, _ = m.HandleStopDesktopStream(ctx, &pb.StopDesktopStream{
				StreamId:  id,
				SessionId: state.SessionID,
				Reason:    "agent shutdown",
			})
		}
	}

	return nil
}

// GetStreamState returns the state of a stream.
func (m *DesktopStreamManager) GetStreamState(streamID string) (*StreamState, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	state, ok := m.streams[streamID]
	return state, ok
}

// ActiveStreamCount returns the number of active streams.
func (m *DesktopStreamManager) ActiveStreamCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.streams)
}

// buildStreamOptions converts protobuf config to StreamOptions.
func (m *DesktopStreamManager) buildStreamOptions(cmd *pb.StartDesktopStream) streaming.StreamOptions {
	opts := streaming.StreamOptions{
		Type:        streaming.StreamTypeDesktop,
		EnableAudio: false,
		EnableInput: true,
	}

	if cfg := cmd.Config; cfg != nil {
		if cfg.Width > 0 && cfg.Height > 0 {
			opts.Resolution = streaming.Resolution{
				Width:  int(cfg.Width),
				Height: int(cfg.Height),
			}
		}
		if cfg.FrameRate > 0 {
			opts.FrameRate = int(cfg.FrameRate)
		}
		if cfg.Bitrate > 0 {
			opts.BitRate = int(cfg.Bitrate)
		}
		if cfg.Display != "" {
			opts.Display = cfg.Display
		} else {
			opts.Display = m.config.DefaultDisplay
		}
		opts.EnableAudio = cfg.AudioEnabled
		opts.EnableInput = cfg.InputEnabled
	}

	// Add ICE servers
	if len(cmd.IceServers) > 0 {
		opts.ICEServers = make([]streaming.ICEServer, len(cmd.IceServers))
		for i, server := range cmd.IceServers {
			opts.ICEServers[i] = streaming.ICEServer{
				URLs:       server.Urls,
				Username:   server.Username,
				Credential: server.Credential,
			}
		}
	}

	return opts
}

// buildStartedResponse builds the DesktopStreamStarted response.
func (m *DesktopStreamManager) buildStartedResponse(cmd *pb.StartDesktopStream, info *streaming.StreamInfo) *pb.RunnerMessage {
	var actualConfig *pb.StreamConfig
	if info.Resolution.Width > 0 {
		// Get video codec from metadata if available
		videoCodec := ""
		if info.Metadata != nil {
			videoCodec = info.Metadata["video_codec"]
		}
		actualConfig = &pb.StreamConfig{
			Width:        int32(info.Resolution.Width),
			Height:       int32(info.Resolution.Height),
			FrameRate:    int32(info.FrameRate),
			Bitrate:      int32(info.BitRate),
			VideoCodec:   videoCodec,
			AudioEnabled: info.AudioEnabled,
			InputEnabled: true,
		}
	}

	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_DesktopStreamStarted{
			DesktopStreamStarted: &pb.DesktopStreamStarted{
				StreamId:        cmd.StreamId,
				SessionId:       cmd.SessionId,
				ActualConfig:    actualConfig,
				SignalingUrl:    info.SignalingURL,
				Provider:        m.provider.Name(),
				ProviderVersion: "1.0.0",
				StartedAtUnixMs: time.Now().UnixMilli(),
			},
		},
	}
}

// buildStoppedResponse builds the DesktopStreamStopped response.
func (m *DesktopStreamManager) buildStoppedResponse(streamID, sessionID, reason string) *pb.RunnerMessage {
	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_DesktopStreamStopped{
			DesktopStreamStopped: &pb.DesktopStreamStopped{
				StreamId:        streamID,
				SessionId:       sessionID,
				Reason:          reason,
				StoppedAtUnixMs: time.Now().UnixMilli(),
			},
		},
	}
}

// sendError sends an error notification to the server.
func (m *DesktopStreamManager) sendError(streamID, sessionID, errMsg, errCode string, recoverable bool) (*pb.RunnerMessage, error) {
	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_DesktopStreamError{
			DesktopStreamError: &pb.DesktopStreamError{
				StreamId:        streamID,
				SessionId:       sessionID,
				Error:           errMsg,
				ErrorCode:       errCode,
				Recoverable:     recoverable,
				TimestampUnixMs: time.Now().UnixMilli(),
			},
		},
	}, fmt.Errorf("%s: %s", errCode, errMsg)
}
