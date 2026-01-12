package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// mockStreamProvider implements streaming.StreamProvider for testing.
type mockStreamProvider struct {
	name           string
	supportedTypes []streaming.StreamType
	startErr       error
	stopErr        error
	getInfoErr     error
	streamInfo     *streaming.StreamInfo
}

func (m *mockStreamProvider) Name() string {
	return m.name
}

func (m *mockStreamProvider) SupportedTypes() []streaming.StreamType {
	return m.supportedTypes
}

func (m *mockStreamProvider) Start(_ context.Context, opts streaming.StreamOptions) (*streaming.StreamInfo, error) {
	if m.startErr != nil {
		return nil, m.startErr
	}
	if m.streamInfo != nil {
		return m.streamInfo, nil
	}
	return &streaming.StreamInfo{
		ID:         "provider-stream-123",
		Resolution: opts.Resolution,
		FrameRate:  opts.FrameRate,
	}, nil
}

func (m *mockStreamProvider) Stop(_ context.Context, _ string) error {
	return m.stopErr
}

func (m *mockStreamProvider) GetInfo(_ context.Context, _ string) (*streaming.StreamInfo, error) {
	if m.getInfoErr != nil {
		return nil, m.getInfoErr
	}
	return m.streamInfo, nil
}

func TestDefaultDesktopStreamConfig(t *testing.T) {
	config := DefaultDesktopStreamConfig()

	assert.True(t, config.Enabled)
	assert.Equal(t, ":99", config.DefaultDisplay)
	assert.NotNil(t, config.ProviderConfig)
}

func TestNewDesktopStreamManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := DefaultDesktopStreamConfig()

	mgr := NewDesktopStreamManager(config, logger)

	assert.NotNil(t, mgr)
	assert.Equal(t, 0, mgr.ActiveStreamCount())
}

func TestNewDesktopStreamManager_NilLogger(t *testing.T) {
	config := DefaultDesktopStreamConfig()

	// Should not panic with nil logger
	mgr := NewDesktopStreamManager(config, nil)

	assert.NotNil(t, mgr)
}

func TestNewDesktopStreamManager_Disabled(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	config.Enabled = false

	mgr := NewDesktopStreamManager(config, nil)

	assert.NotNil(t, mgr)
	// Provider should be nil when disabled
	assert.Nil(t, mgr.provider)
}

func TestDesktopStreamManager_SetSendFunc(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	mgr := NewDesktopStreamManager(config, nil)

	mgr.SetSendFunc(func(_ *pb.RunnerMessage) error {
		return nil
	})

	// Verify sendFunc is set (we can't directly check, but it shouldn't panic)
	assert.NotNil(t, mgr)
}

func TestDesktopStreamManager_HandleStartDesktopStream_ProviderDisabled(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	config.Enabled = false
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	ctx := context.Background()
	cmd := &pb.StartDesktopStream{
		StreamId:  "stream_123",
		SessionId: "sess_123",
	}

	resp, err := mgr.HandleStartDesktopStream(ctx, cmd)

	// Should return error about provider not configured
	require.Error(t, err)
	require.NotNil(t, resp)
	errResp := resp.GetDesktopStreamError()
	require.NotNil(t, errResp)
	assert.Equal(t, "stream_123", errResp.StreamId)
	assert.Contains(t, errResp.Error, "not enabled")
	assert.Equal(t, "provider_not_configured", errResp.ErrorCode)
}

func TestDesktopStreamManager_HandleStartDesktopStream_Success(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	// Replace provider with mock
	mockProvider := &mockStreamProvider{
		name:           "mock",
		supportedTypes: []streaming.StreamType{streaming.StreamTypeDesktop},
		streamInfo: &streaming.StreamInfo{
			ID:         "provider-stream-123",
			VideoCodec: "h264",
			Resolution: streaming.Resolution{Width: 1920, Height: 1080},
			FrameRate:  30,
		},
	}
	mgr.provider = mockProvider

	ctx := context.Background()
	cmd := &pb.StartDesktopStream{
		StreamId:  "stream_123",
		SessionId: "sess_123",
		Config: &pb.StreamConfig{
			Width:     1920,
			Height:    1080,
			FrameRate: 30,
			Display:   ":0",
		},
	}

	resp, err := mgr.HandleStartDesktopStream(ctx, cmd)

	require.NoError(t, err)
	require.NotNil(t, resp)

	started := resp.GetDesktopStreamStarted()
	require.NotNil(t, started)
	assert.Equal(t, "stream_123", started.StreamId)
	assert.Equal(t, "sess_123", started.SessionId)
	assert.Equal(t, int32(1920), started.Width)
	assert.Equal(t, int32(1080), started.Height)
	assert.Equal(t, int32(30), started.FrameRate)
	assert.Equal(t, "h264", started.VideoCodec)
	assert.Equal(t, "mock", started.ProviderName)
	assert.Equal(t, "provider-stream-123", started.ProviderStreamId)

	// Verify stream is tracked
	assert.Equal(t, 1, mgr.ActiveStreamCount())
	state, exists := mgr.GetStreamState("stream_123")
	require.True(t, exists)
	assert.Equal(t, "stream_123", state.StreamID)
	assert.Equal(t, "sess_123", state.SessionID)
	assert.Equal(t, "running", state.Status)
	assert.Equal(t, "mock", state.Provider)
}

func TestDesktopStreamManager_HandleStartDesktopStream_AlreadyExists(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	mockProvider := &mockStreamProvider{
		name:           "mock",
		supportedTypes: []streaming.StreamType{streaming.StreamTypeDesktop},
	}
	mgr.provider = mockProvider

	ctx := context.Background()
	cmd := &pb.StartDesktopStream{
		StreamId:  "stream_123",
		SessionId: "sess_123",
	}

	// Start first stream
	_, err := mgr.HandleStartDesktopStream(ctx, cmd)
	require.NoError(t, err)

	// Try to start same stream again
	resp, err := mgr.HandleStartDesktopStream(ctx, cmd)

	require.Error(t, err)
	require.NotNil(t, resp)
	errResp := resp.GetDesktopStreamError()
	require.NotNil(t, errResp)
	assert.Equal(t, "stream_exists", errResp.ErrorCode)
}

func TestDesktopStreamManager_HandleStartDesktopStream_ProviderError(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	mockProvider := &mockStreamProvider{
		name:           "mock",
		supportedTypes: []streaming.StreamType{streaming.StreamTypeDesktop},
		startErr:       errors.New("display not found"),
	}
	mgr.provider = mockProvider

	ctx := context.Background()
	cmd := &pb.StartDesktopStream{
		StreamId:  "stream_123",
		SessionId: "sess_123",
	}

	resp, err := mgr.HandleStartDesktopStream(ctx, cmd)

	require.Error(t, err)
	require.NotNil(t, resp)
	errResp := resp.GetDesktopStreamError()
	require.NotNil(t, errResp)
	assert.Equal(t, "start_failed", errResp.ErrorCode)
	assert.Contains(t, errResp.Error, "display not found")
	assert.True(t, errResp.Recoverable)
}

func TestDesktopStreamManager_HandleStopDesktopStream_Success(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	mockProvider := &mockStreamProvider{
		name:           "mock",
		supportedTypes: []streaming.StreamType{streaming.StreamTypeDesktop},
	}
	mgr.provider = mockProvider

	ctx := context.Background()

	// First start a stream
	startCmd := &pb.StartDesktopStream{
		StreamId:  "stream_123",
		SessionId: "sess_123",
	}
	_, err := mgr.HandleStartDesktopStream(ctx, startCmd)
	require.NoError(t, err)
	assert.Equal(t, 1, mgr.ActiveStreamCount())

	// Now stop it
	stopCmd := &pb.StopDesktopStream{
		StreamId: "stream_123",
		Reason:   "user requested",
	}
	resp, err := mgr.HandleStopDesktopStream(ctx, stopCmd)

	require.NoError(t, err)
	require.NotNil(t, resp)

	stopped := resp.GetDesktopStreamStopped()
	require.NotNil(t, stopped)
	assert.Equal(t, "stream_123", stopped.StreamId)
	assert.Equal(t, "sess_123", stopped.SessionId)
	assert.Equal(t, "user requested", stopped.Reason)

	// Verify stream is no longer tracked
	assert.Equal(t, 0, mgr.ActiveStreamCount())
	_, exists := mgr.GetStreamState("stream_123")
	assert.False(t, exists)
}

func TestDesktopStreamManager_HandleStopDesktopStream_NotFound(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	mockProvider := &mockStreamProvider{
		name:           "mock",
		supportedTypes: []streaming.StreamType{streaming.StreamTypeDesktop},
	}
	mgr.provider = mockProvider

	ctx := context.Background()

	cmd := &pb.StopDesktopStream{
		StreamId: "stream_nonexistent",
		Reason:   "test",
	}

	resp, err := mgr.HandleStopDesktopStream(ctx, cmd)

	require.NoError(t, err)
	require.NotNil(t, resp)

	stopped := resp.GetDesktopStreamStopped()
	require.NotNil(t, stopped)
	assert.Equal(t, "stream_nonexistent", stopped.StreamId)
	assert.Contains(t, stopped.Reason, "stream not found")
}

func TestDesktopStreamManager_HandleStopDesktopStream_ProviderDisabled(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	config.Enabled = false
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	ctx := context.Background()
	cmd := &pb.StopDesktopStream{
		StreamId: "stream_123",
		Reason:   "test",
	}

	resp, err := mgr.HandleStopDesktopStream(ctx, cmd)

	require.NoError(t, err)
	assert.Nil(t, resp) // No provider, nothing to do
}

func TestDesktopStreamManager_HandleStopDesktopStream_ProviderError(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	mockProvider := &mockStreamProvider{
		name:           "mock",
		supportedTypes: []streaming.StreamType{streaming.StreamTypeDesktop},
		stopErr:        errors.New("process not found"),
	}
	mgr.provider = mockProvider

	ctx := context.Background()

	// First start a stream
	startCmd := &pb.StartDesktopStream{
		StreamId:  "stream_123",
		SessionId: "sess_123",
	}
	_, err := mgr.HandleStartDesktopStream(ctx, startCmd)
	require.NoError(t, err)

	// Now stop it - should still succeed but log error
	stopCmd := &pb.StopDesktopStream{
		StreamId: "stream_123",
		Reason:   "test",
	}
	resp, err := mgr.HandleStopDesktopStream(ctx, stopCmd)

	require.NoError(t, err)
	require.NotNil(t, resp)

	// Stream should still be removed from tracking
	assert.Equal(t, 0, mgr.ActiveStreamCount())
}

func TestDesktopStreamManager_StopAllStreams(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	mockProvider := &mockStreamProvider{
		name:           "mock",
		supportedTypes: []streaming.StreamType{streaming.StreamTypeDesktop},
	}
	mgr.provider = mockProvider

	ctx := context.Background()

	// Start multiple streams
	for i := 0; i < 3; i++ {
		cmd := &pb.StartDesktopStream{
			StreamId:  "stream_" + string(rune('A'+i)),
			SessionId: "sess_123",
		}
		_, err := mgr.HandleStartDesktopStream(ctx, cmd)
		require.NoError(t, err)
	}
	assert.Equal(t, 3, mgr.ActiveStreamCount())

	// Stop all
	err := mgr.StopAllStreams(ctx)
	require.NoError(t, err)

	// All streams should be stopped
	assert.Equal(t, 0, mgr.ActiveStreamCount())
}

func TestDesktopStreamManager_GetStreamState(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	mockProvider := &mockStreamProvider{
		name:           "mock",
		supportedTypes: []streaming.StreamType{streaming.StreamTypeDesktop},
	}
	mgr.provider = mockProvider

	ctx := context.Background()

	// Non-existent stream
	state, exists := mgr.GetStreamState("nonexistent")
	assert.False(t, exists)
	assert.Nil(t, state)

	// Start a stream
	cmd := &pb.StartDesktopStream{
		StreamId:  "stream_123",
		SessionId: "sess_123",
	}
	_, err := mgr.HandleStartDesktopStream(ctx, cmd)
	require.NoError(t, err)

	// Now it should exist
	state, exists = mgr.GetStreamState("stream_123")
	assert.True(t, exists)
	assert.NotNil(t, state)
	assert.Equal(t, "stream_123", state.StreamID)
	assert.Equal(t, "running", state.Status)
	assert.NotZero(t, state.StartedAt)
	assert.Nil(t, state.StoppedAt)
}

func TestDesktopStreamManager_BuildStreamOptions(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	config.DefaultDisplay = ":99"
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	tests := []struct {
		name     string
		cmd      *pb.StartDesktopStream
		wantRes  streaming.Resolution
		wantFPS  int
		wantDisp string
	}{
		{
			name: "with config",
			cmd: &pb.StartDesktopStream{
				StreamId:  "s1",
				SessionId: "sess_1",
				Config: &pb.StreamConfig{
					Width:        1920,
					Height:       1080,
					FrameRate:    60,
					Bitrate:      5000000,
					Display:      ":0",
					AudioEnabled: true,
					InputEnabled: false,
				},
			},
			wantRes:  streaming.Resolution{Width: 1920, Height: 1080},
			wantFPS:  60,
			wantDisp: ":0",
		},
		{
			name: "without config - uses defaults",
			cmd: &pb.StartDesktopStream{
				StreamId:  "s2",
				SessionId: "sess_2",
			},
			wantRes:  streaming.Resolution{},
			wantFPS:  0,
			wantDisp: "",
		},
		{
			name: "partial config",
			cmd: &pb.StartDesktopStream{
				StreamId:  "s3",
				SessionId: "sess_3",
				Config: &pb.StreamConfig{
					Width:  1280,
					Height: 720,
					// No display - should use default
				},
			},
			wantRes:  streaming.Resolution{Width: 1280, Height: 720},
			wantFPS:  0,
			wantDisp: ":99", // Default
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := mgr.buildStreamOptions(tt.cmd)

			assert.Equal(t, streaming.StreamTypeDesktop, opts.Type)
			assert.Equal(t, tt.wantRes, opts.Resolution)
			assert.Equal(t, tt.wantFPS, opts.FrameRate)

			if tt.wantDisp != "" {
				assert.Equal(t, tt.wantDisp, opts.Metadata["display"])
			}
		})
	}
}

func TestDesktopStreamManager_BuildStreamOptions_ICEServers(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	cmd := &pb.StartDesktopStream{
		StreamId:  "stream_123",
		SessionId: "sess_123",
		IceServers: []*pb.ICEServer{
			{
				Urls:       []string{"stun:stun.example.com:3478"},
				Username:   "",
				Credential: "",
			},
			{
				Urls:       []string{"turn:turn.example.com:3478"},
				Username:   "user",
				Credential: "pass",
			},
		},
	}

	opts := mgr.buildStreamOptions(cmd)

	require.Len(t, opts.ICEServers, 2)
	assert.Equal(t, []string{"stun:stun.example.com:3478"}, opts.ICEServers[0].URLs)
	assert.Equal(t, []string{"turn:turn.example.com:3478"}, opts.ICEServers[1].URLs)
	assert.Equal(t, "user", opts.ICEServers[1].Username)
	assert.Equal(t, "pass", opts.ICEServers[1].Credential)
}

func TestDesktopStreamManager_CommandHandler_Integration(t *testing.T) {
	// Test integration with DefaultCommandHandler
	logger := zaptest.NewLogger(t)
	tmpDir := t.TempDir()
	workspace := NewWorkspaceManager(tmpDir, logger)
	handler := NewDefaultCommandHandler(workspace, logger)

	streamConfig := DefaultDesktopStreamConfig()
	streamMgr := NewDesktopStreamManager(streamConfig, logger)

	// Replace provider with mock
	mockProvider := &mockStreamProvider{
		name:           "mock",
		supportedTypes: []streaming.StreamType{streaming.StreamTypeDesktop},
	}
	streamMgr.provider = mockProvider

	// Wire up the callbacks
	handler.OnStartDesktopStream = streamMgr.HandleStartDesktopStream
	handler.OnStopDesktopStream = streamMgr.HandleStopDesktopStream

	ctx := context.Background()

	// First attach a session
	_, err := handler.HandleAttachSession(ctx, &pb.AttachSession{
		SessionId:     "sess_123",
		WorkspacePath: tmpDir + "/ws1",
	})
	require.NoError(t, err)

	// Start desktop stream via command handler
	startCmd := &pb.StartDesktopStream{
		StreamId:  "stream_123",
		SessionId: "sess_123",
	}
	resp, err := handler.HandleStartDesktopStream(ctx, startCmd)
	require.NoError(t, err)
	require.NotNil(t, resp.GetDesktopStreamStarted())

	// Verify stream is tracked
	assert.Equal(t, 1, streamMgr.ActiveStreamCount())

	// Stop via command handler
	stopCmd := &pb.StopDesktopStream{
		StreamId: "stream_123",
		Reason:   "test complete",
	}
	resp, err = handler.HandleStopDesktopStream(ctx, stopCmd)
	require.NoError(t, err)
	require.NotNil(t, resp.GetDesktopStreamStopped())

	assert.Equal(t, 0, streamMgr.ActiveStreamCount())
}

func TestDesktopStreamManager_ConcurrentAccess(t *testing.T) {
	config := DefaultDesktopStreamConfig()
	logger := zaptest.NewLogger(t)

	mgr := NewDesktopStreamManager(config, logger)

	mockProvider := &mockStreamProvider{
		name:           "mock",
		supportedTypes: []streaming.StreamType{streaming.StreamTypeDesktop},
	}
	mgr.provider = mockProvider

	ctx := context.Background()

	// Start multiple streams concurrently
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			cmd := &pb.StartDesktopStream{
				StreamId:  "stream_" + string(rune('0'+idx)),
				SessionId: "sess_123",
			}
			_, _ = mgr.HandleStartDesktopStream(ctx, cmd)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// All should have succeeded
	assert.Equal(t, 10, mgr.ActiveStreamCount())

	// Now stop them all concurrently
	for i := 0; i < 10; i++ {
		go func(idx int) {
			cmd := &pb.StopDesktopStream{
				StreamId: "stream_" + string(rune('0'+idx)),
				Reason:   "test",
			}
			_, _ = mgr.HandleStopDesktopStream(ctx, cmd)
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	// Give some time for all to complete
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, 0, mgr.ActiveStreamCount())
}
