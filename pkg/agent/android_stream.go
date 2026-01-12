package agent

import (
	"context"
	"sync"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/streaming/android"
	"github.com/chunlea/marionette/pkg/streaming/android/scrcpy"
	"go.uber.org/zap"
)

// AndroidStreamManager manages Android screen streaming for the agent.
type AndroidStreamManager struct {
	provider *scrcpy.Provider
	logger   *zap.Logger

	streams   map[string]*androidStream
	streamsMu sync.RWMutex

	// Callback to send messages to server
	sendMessage func(*pb.RunnerMessage) error
}

// androidStream represents an active Android stream.
type androidStream struct {
	tunnelID     string
	streamID     string
	deviceSerial string
	cancel       context.CancelFunc
}

// NewAndroidStreamManager creates a new Android stream manager.
func NewAndroidStreamManager(logger *zap.Logger) *AndroidStreamManager {
	return &AndroidStreamManager{
		logger:  logger.Named("android-stream"),
		streams: make(map[string]*androidStream),
	}
}

// SetMessageSender sets the callback for sending messages to the server.
func (m *AndroidStreamManager) SetMessageSender(sender func(*pb.RunnerMessage) error) {
	m.sendMessage = sender
}

// StartStream starts an Android screen stream for the given tunnel.
func (m *AndroidStreamManager) StartStream(ctx context.Context, cmd *pb.CreateTunnel) error {
	if cmd.AndroidOptions == nil {
		return ErrInvalidRequest
	}

	m.logger.Info("starting android stream",
		zap.String("tunnel_id", cmd.TunnelId),
		zap.String("device", cmd.AndroidOptions.DeviceSerial),
	)

	// Initialize provider if not already done
	if m.provider == nil {
		cfg := scrcpy.Config{
			Logger: m.logger,
		}
		cfg = cfg.WithDefaults()
		provider, err := scrcpy.New(cfg)
		if err != nil {
			m.logger.Error("failed to create scrcpy provider", zap.Error(err))
			return err
		}
		m.provider = provider
	}

	// Build stream options
	opts := android.StreamOptions{
		DeviceSerial: cmd.AndroidOptions.DeviceSerial,
		MaxWidth:     int(cmd.AndroidOptions.MaxWidth),
		MaxHeight:    int(cmd.AndroidOptions.MaxHeight),
		MaxFPS:       int(cmd.AndroidOptions.MaxFps),
		Bitrate:      int(cmd.AndroidOptions.Bitrate),
		AudioEnabled: cmd.AndroidOptions.AudioEnabled,
	}

	// Create cancellable context for this stream
	streamCtx, cancel := context.WithCancel(ctx)

	// Start the stream
	info, err := m.provider.StartStream(streamCtx, opts)
	if err != nil {
		cancel()
		m.logger.Error("failed to start android stream",
			zap.String("tunnel_id", cmd.TunnelId),
			zap.Error(err),
		)
		return err
	}

	// Store stream reference
	m.streamsMu.Lock()
	m.streams[cmd.TunnelId] = &androidStream{
		tunnelID:     cmd.TunnelId,
		streamID:     info.ID,
		deviceSerial: cmd.AndroidOptions.DeviceSerial,
		cancel:       cancel,
	}
	m.streamsMu.Unlock()

	// Set up a video sink to forward data to the server
	sink := &agentVideoSink{
		tunnelID:    cmd.TunnelId,
		sendMessage: m.sendMessage,
		logger:      m.logger,
	}

	if err := m.provider.SetVideoSink(info.ID, sink); err != nil {
		m.logger.Warn("failed to set video sink", zap.Error(err))
	}

	// Get video config for the initial message
	videoConfig := make([]byte, 0)
	videoCodec := "h264"
	audioCodec := ""

	// Send stream started message
	if m.sendMessage != nil {
		startedMsg := &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_AndroidStreamStarted{
				AndroidStreamStarted: &pb.AndroidStreamStarted{
					TunnelId:     cmd.TunnelId,
					DeviceSerial: cmd.AndroidOptions.DeviceSerial,
					Width:        int32(info.Width),
					Height:       int32(info.Height),
					VideoCodec:   videoCodec,
					AudioCodec:   audioCodec,
					VideoConfig:  videoConfig,
				},
			},
		}
		if err := m.sendMessage(startedMsg); err != nil {
			m.logger.Warn("failed to send stream started message", zap.Error(err))
		}
	}

	m.logger.Info("android stream started",
		zap.String("tunnel_id", cmd.TunnelId),
		zap.String("stream_id", info.ID),
		zap.Int("width", info.Width),
		zap.Int("height", info.Height),
	)

	return nil
}

// StopStream stops an Android stream.
func (m *AndroidStreamManager) StopStream(tunnelID string) error {
	m.streamsMu.Lock()
	stream, exists := m.streams[tunnelID]
	if exists {
		delete(m.streams, tunnelID)
	}
	m.streamsMu.Unlock()

	if !exists {
		return ErrStreamNotFound
	}

	// Cancel the stream context
	stream.cancel()

	// Stop the stream via provider
	if m.provider != nil {
		if err := m.provider.StopStream(context.Background(), stream.streamID); err != nil {
			m.logger.Warn("error stopping stream", zap.String("tunnel_id", tunnelID), zap.Error(err))
		}
	}

	// Send stream stopped message
	if m.sendMessage != nil {
		stoppedMsg := &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_AndroidStreamStopped{
				AndroidStreamStopped: &pb.AndroidStreamStopped{
					TunnelId: tunnelID,
					Reason:   "closed",
				},
			},
		}
		if err := m.sendMessage(stoppedMsg); err != nil {
			m.logger.Warn("failed to send stream stopped message", zap.Error(err))
		}
	}

	m.logger.Info("android stream stopped", zap.String("tunnel_id", tunnelID))
	return nil
}

// Close stops all streams and releases resources.
func (m *AndroidStreamManager) Close() error {
	m.streamsMu.Lock()
	streams := make([]*androidStream, 0, len(m.streams))
	for _, s := range m.streams {
		streams = append(streams, s)
	}
	m.streams = make(map[string]*androidStream)
	m.streamsMu.Unlock()

	for _, stream := range streams {
		stream.cancel()
	}

	if m.provider != nil {
		return m.provider.Close()
	}

	return nil
}

// ListDevices returns the list of connected Android devices.
func (m *AndroidStreamManager) ListDevices(ctx context.Context) ([]android.Device, error) {
	if m.provider == nil {
		cfg := scrcpy.Config{
			Logger: m.logger,
		}
		cfg = cfg.WithDefaults()
		provider, err := scrcpy.New(cfg)
		if err != nil {
			return nil, err
		}
		m.provider = provider
	}

	return m.provider.ListDevices(ctx)
}

// GetStreamInfo returns info about a stream by tunnel ID.
func (m *AndroidStreamManager) GetStreamInfo(tunnelID string) (*android.StreamInfo, error) {
	m.streamsMu.RLock()
	stream, exists := m.streams[tunnelID]
	m.streamsMu.RUnlock()

	if !exists {
		return nil, ErrStreamNotFound
	}

	if m.provider == nil {
		return nil, ErrStreamNotFound
	}

	return m.provider.GetStream(context.Background(), stream.streamID)
}

// agentVideoSink implements android.VideoSink to forward data to the server.
type agentVideoSink struct {
	tunnelID    string
	sendMessage func(*pb.RunnerMessage) error
	logger      *zap.Logger

	// Track config state
	configSent bool
	mu         sync.Mutex
}

func (s *agentVideoSink) OnVideoData(data []byte) error {
	if s.sendMessage == nil {
		return nil
	}

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_AndroidStreamData{
			AndroidStreamData: &pb.AndroidStreamData{
				TunnelId: s.tunnelID,
				IsVideo:  true,
				Data:     data,
				// PTS and keyframe detection would require parsing the NAL units
			},
		},
	}

	return s.sendMessage(msg)
}

func (s *agentVideoSink) OnVideoConfig(width, height int, codec string, config []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Send updated stream info if config changes
	if s.sendMessage != nil && !s.configSent {
		msg := &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_AndroidStreamStarted{
				AndroidStreamStarted: &pb.AndroidStreamStarted{
					TunnelId:    s.tunnelID,
					Width:       int32(width),
					Height:      int32(height),
					VideoCodec:  codec,
					VideoConfig: config,
				},
			},
		}
		if err := s.sendMessage(msg); err != nil {
			s.logger.Warn("failed to send video config", zap.Error(err))
		}
		s.configSent = true
	}

	return nil
}

func (s *agentVideoSink) OnAudioData(data []byte) error {
	if s.sendMessage == nil {
		return nil
	}

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_AndroidStreamData{
			AndroidStreamData: &pb.AndroidStreamData{
				TunnelId: s.tunnelID,
				IsVideo:  false,
				Data:     data,
			},
		},
	}

	return s.sendMessage(msg)
}

func (s *agentVideoSink) OnAudioConfig(sampleRate, channels int, codec string, config []byte) error {
	// Audio config is included in the stream started message
	return nil
}

func (s *agentVideoSink) OnError(err error) {
	s.logger.Error("stream error", zap.String("tunnel_id", s.tunnelID), zap.Error(err))

	if s.sendMessage != nil {
		msg := &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_AndroidStreamStopped{
				AndroidStreamStopped: &pb.AndroidStreamStopped{
					TunnelId: s.tunnelID,
					Reason:   "error",
					Error:    err.Error(),
				},
			},
		}
		_ = s.sendMessage(msg)
	}
}

func (s *agentVideoSink) OnClose() {
	s.logger.Info("stream closed", zap.String("tunnel_id", s.tunnelID))

	if s.sendMessage != nil {
		msg := &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_AndroidStreamStopped{
				AndroidStreamStopped: &pb.AndroidStreamStopped{
					TunnelId: s.tunnelID,
					Reason:   "closed",
				},
			},
		}
		_ = s.sendMessage(msg)
	}
}

// Ensure agentVideoSink implements android.VideoSink.
var _ android.VideoSink = (*agentVideoSink)(nil)
