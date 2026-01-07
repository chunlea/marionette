package agent

import (
	"context"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockStreamProvider implements streaming.StreamProvider for testing.
type mockStreamProvider struct {
	name         string
	startErr     error
	stopErr      error
	startInfo    *streaming.StreamInfo
	startCalled  bool
	stopCalled   bool
	stopStreamID string
}

func (m *mockStreamProvider) Name() string {
	return m.name
}

func (m *mockStreamProvider) SupportedTypes() []streaming.StreamType {
	return []streaming.StreamType{streaming.StreamTypeDesktop}
}

func (m *mockStreamProvider) Start(_ context.Context, opts streaming.StreamOptions) (*streaming.StreamInfo, error) {
	m.startCalled = true
	if m.startErr != nil {
		return nil, m.startErr
	}
	if m.startInfo != nil {
		return m.startInfo, nil
	}
	return &streaming.StreamInfo{
		ID:           "test-stream",
		SignalingURL: "ws://localhost:8080/signal",
		Resolution:   opts.Resolution,
		FrameRate:    opts.FrameRate,
		BitRate:      opts.BitRate,
		AudioEnabled: opts.EnableAudio,
		InputEnabled: opts.EnableInput,
		Metadata:     map[string]string{"video_codec": "h264"},
	}, nil
}

func (m *mockStreamProvider) Stop(_ context.Context, streamID string) error {
	m.stopCalled = true
	m.stopStreamID = streamID
	return m.stopErr
}

func (m *mockStreamProvider) GetStatus(_ context.Context, streamID string) (*streaming.StreamInfo, error) {
	return &streaming.StreamInfo{
		ID:    streamID,
		State: streaming.StreamStateActive,
	}, nil
}

func (m *mockStreamProvider) UpdateOptions(_ context.Context, _ string, _ streaming.StreamOptions) error {
	return nil
}

func (m *mockStreamProvider) HealthCheck(_ context.Context) error {
	return nil
}

func TestDesktopStreamManager_NewManager(t *testing.T) {
	logger := zap.NewNop()

	t.Run("disabled config returns manager without provider", func(t *testing.T) {
		config := DesktopStreamConfig{
			Enabled: false,
		}
		mgr := NewDesktopStreamManager(config, logger)
		assert.NotNil(t, mgr)
		assert.Nil(t, mgr.provider)
	})

	t.Run("nil logger uses nop logger", func(t *testing.T) {
		config := DesktopStreamConfig{
			Enabled: false,
		}
		mgr := NewDesktopStreamManager(config, nil)
		assert.NotNil(t, mgr)
	})
}

func TestDesktopStreamManager_HandleStartDesktopStream(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	t.Run("provider not configured returns error", func(t *testing.T) {
		mgr := &DesktopStreamManager{
			provider: nil,
			logger:   logger.Named("test"),
			streams:  make(map[string]*StreamState),
		}

		cmd := &pb.StartDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
		}

		resp, err := mgr.HandleStartDesktopStream(ctx, cmd)
		// sendError returns both response and error
		require.Error(t, err)
		require.NotNil(t, resp)

		errPayload := resp.GetDesktopStreamError()
		require.NotNil(t, errPayload)
		assert.Equal(t, "PROVIDER_NOT_CONFIGURED", errPayload.ErrorCode)
	})

	t.Run("stream already exists returns error", func(t *testing.T) {
		mockProvider := &mockStreamProvider{name: "selkies"}
		mgr := &DesktopStreamManager{
			provider: mockProvider,
			logger:   logger.Named("test"),
			streams: map[string]*StreamState{
				"stream-1": {StreamID: "stream-1", Status: "running"},
			},
		}

		cmd := &pb.StartDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
		}

		resp, err := mgr.HandleStartDesktopStream(ctx, cmd)
		// sendError returns both response and error
		require.Error(t, err)
		require.NotNil(t, resp)

		errPayload := resp.GetDesktopStreamError()
		require.NotNil(t, errPayload)
		assert.Equal(t, "STREAM_EXISTS", errPayload.ErrorCode)
	})

	t.Run("successful start returns started response", func(t *testing.T) {
		mockProvider := &mockStreamProvider{
			name: "selkies",
			startInfo: &streaming.StreamInfo{
				ID:           "stream-1",
				SignalingURL: "ws://localhost:8080/signal",
				Resolution:   streaming.Resolution{Width: 1920, Height: 1080},
				FrameRate:    30,
				BitRate:      4000000,
				AudioEnabled: false,
				InputEnabled: true,
				Metadata:     map[string]string{"video_codec": "h264"},
			},
		}
		mgr := &DesktopStreamManager{
			provider: mockProvider,
			logger:   logger.Named("test"),
			streams:  make(map[string]*StreamState),
			config:   DefaultDesktopStreamConfig(),
		}

		cmd := &pb.StartDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
			Config: &pb.StreamConfig{
				Width:     1920,
				Height:    1080,
				FrameRate: 30,
			},
		}

		resp, err := mgr.HandleStartDesktopStream(ctx, cmd)
		require.NoError(t, err)
		require.NotNil(t, resp)

		started := resp.GetDesktopStreamStarted()
		require.NotNil(t, started)
		assert.Equal(t, "stream-1", started.StreamId)
		assert.Equal(t, "session-1", started.SessionId)
		assert.Equal(t, "selkies", started.Provider)
		assert.Equal(t, "ws://localhost:8080/signal", started.SignalingUrl)
		assert.NotNil(t, started.ActualConfig)
		assert.Equal(t, int32(1920), started.ActualConfig.Width)
		assert.Equal(t, int32(1080), started.ActualConfig.Height)
		assert.Equal(t, "h264", started.ActualConfig.VideoCodec)

		// Verify stream is tracked
		state, ok := mgr.GetStreamState("stream-1")
		require.True(t, ok)
		assert.Equal(t, "running", state.Status)
		assert.Equal(t, "selkies", state.Provider)

		assert.True(t, mockProvider.startCalled)
	})

	t.Run("start failure returns error", func(t *testing.T) {
		mockProvider := &mockStreamProvider{
			name:     "selkies",
			startErr: assert.AnError,
		}
		mgr := &DesktopStreamManager{
			provider: mockProvider,
			logger:   logger.Named("test"),
			streams:  make(map[string]*StreamState),
			config:   DefaultDesktopStreamConfig(),
		}

		cmd := &pb.StartDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
		}

		resp, err := mgr.HandleStartDesktopStream(ctx, cmd)
		require.Error(t, err)
		require.NotNil(t, resp)

		errPayload := resp.GetDesktopStreamError()
		require.NotNil(t, errPayload)
		assert.Equal(t, "START_FAILED", errPayload.ErrorCode)
		assert.True(t, errPayload.Recoverable)
	})
}

func TestDesktopStreamManager_HandleStopDesktopStream(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	t.Run("provider not configured returns nil", func(t *testing.T) {
		mgr := &DesktopStreamManager{
			provider: nil,
			logger:   logger.Named("test"),
			streams:  make(map[string]*StreamState),
		}

		cmd := &pb.StopDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
			Reason:    "user requested",
		}

		resp, err := mgr.HandleStopDesktopStream(ctx, cmd)
		require.NoError(t, err)
		assert.Nil(t, resp)
	})

	t.Run("stream not found returns stopped response", func(t *testing.T) {
		mockProvider := &mockStreamProvider{name: "selkies"}
		mgr := &DesktopStreamManager{
			provider: mockProvider,
			logger:   logger.Named("test"),
			streams:  make(map[string]*StreamState),
		}

		cmd := &pb.StopDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
			Reason:    "user requested",
		}

		resp, err := mgr.HandleStopDesktopStream(ctx, cmd)
		require.NoError(t, err)
		require.NotNil(t, resp)

		stopped := resp.GetDesktopStreamStopped()
		require.NotNil(t, stopped)
		assert.Equal(t, "stream-1", stopped.StreamId)
		assert.Equal(t, "stream not found", stopped.Reason)
	})

	t.Run("successful stop removes stream from tracking", func(t *testing.T) {
		mockProvider := &mockStreamProvider{name: "selkies"}
		mgr := &DesktopStreamManager{
			provider: mockProvider,
			logger:   logger.Named("test"),
			streams: map[string]*StreamState{
				"stream-1": {
					StreamID:  "stream-1",
					SessionID: "session-1",
					Status:    "running",
					Provider:  "selkies",
					StartedAt: time.Now(),
				},
			},
		}

		cmd := &pb.StopDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
			Reason:    "user requested",
		}

		resp, err := mgr.HandleStopDesktopStream(ctx, cmd)
		require.NoError(t, err)
		require.NotNil(t, resp)

		stopped := resp.GetDesktopStreamStopped()
		require.NotNil(t, stopped)
		assert.Equal(t, "stream-1", stopped.StreamId)
		assert.Equal(t, "session-1", stopped.SessionId)
		assert.Equal(t, "user requested", stopped.Reason)

		// Verify stream is removed
		_, ok := mgr.GetStreamState("stream-1")
		assert.False(t, ok)

		assert.True(t, mockProvider.stopCalled)
		assert.Equal(t, "stream-1", mockProvider.stopStreamID)
	})
}

func TestDesktopStreamManager_StopAllStreams(t *testing.T) {
	logger := zap.NewNop()
	ctx := context.Background()

	mockProvider := &mockStreamProvider{name: "selkies"}
	mgr := &DesktopStreamManager{
		provider: mockProvider,
		logger:   logger.Named("test"),
		streams: map[string]*StreamState{
			"stream-1": {StreamID: "stream-1", SessionID: "session-1", Status: "running"},
			"stream-2": {StreamID: "stream-2", SessionID: "session-2", Status: "running"},
		},
	}

	err := mgr.StopAllStreams(ctx)
	require.NoError(t, err)

	// All streams should be removed
	assert.Equal(t, 0, mgr.ActiveStreamCount())
}

func TestDesktopStreamManager_ActiveStreamCount(t *testing.T) {
	logger := zap.NewNop()

	mgr := &DesktopStreamManager{
		logger: logger.Named("test"),
		streams: map[string]*StreamState{
			"stream-1": {StreamID: "stream-1", Status: "running"},
			"stream-2": {StreamID: "stream-2", Status: "running"},
			"stream-3": {StreamID: "stream-3", Status: "running"},
		},
	}

	assert.Equal(t, 3, mgr.ActiveStreamCount())
}

func TestDesktopStreamManager_BuildStreamOptions(t *testing.T) {
	logger := zap.NewNop()

	mgr := &DesktopStreamManager{
		logger: logger.Named("test"),
		config: DesktopStreamConfig{
			DefaultDisplay: ":99",
		},
	}

	t.Run("default options", func(t *testing.T) {
		cmd := &pb.StartDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
		}

		opts := mgr.buildStreamOptions(cmd)
		assert.Equal(t, streaming.StreamTypeDesktop, opts.Type)
		assert.False(t, opts.EnableAudio)
		assert.True(t, opts.EnableInput)
	})

	t.Run("with config", func(t *testing.T) {
		cmd := &pb.StartDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
			Config: &pb.StreamConfig{
				Width:        1920,
				Height:       1080,
				FrameRate:    60,
				Bitrate:      8000000,
				AudioEnabled: true,
				InputEnabled: false,
				Display:      ":0",
			},
		}

		opts := mgr.buildStreamOptions(cmd)
		assert.Equal(t, 1920, opts.Resolution.Width)
		assert.Equal(t, 1080, opts.Resolution.Height)
		assert.Equal(t, 60, opts.FrameRate)
		assert.Equal(t, 8000000, opts.BitRate)
		assert.True(t, opts.EnableAudio)
		assert.False(t, opts.EnableInput)
		assert.Equal(t, ":0", opts.Display)
	})

	t.Run("uses default display when not specified", func(t *testing.T) {
		cmd := &pb.StartDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
			Config: &pb.StreamConfig{
				Width:  1280,
				Height: 720,
			},
		}

		opts := mgr.buildStreamOptions(cmd)
		assert.Equal(t, ":99", opts.Display)
	})

	t.Run("with ICE servers", func(t *testing.T) {
		cmd := &pb.StartDesktopStream{
			StreamId:  "stream-1",
			SessionId: "session-1",
			IceServers: []*pb.ICEServer{
				{
					Urls:       []string{"stun:stun.example.com:3478"},
					Username:   "user",
					Credential: "pass",
				},
			},
		}

		opts := mgr.buildStreamOptions(cmd)
		require.Len(t, opts.ICEServers, 1)
		assert.Equal(t, []string{"stun:stun.example.com:3478"}, opts.ICEServers[0].URLs)
		assert.Equal(t, "user", opts.ICEServers[0].Username)
		assert.Equal(t, "pass", opts.ICEServers[0].Credential)
	})
}

func TestDefaultDesktopStreamConfig(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	assert.True(t, config.Enabled)
	assert.Equal(t, ":99", config.DefaultDisplay)
}
