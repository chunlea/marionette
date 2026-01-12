package agent

import (
	"context"
	"testing"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestNewAndroidStreamManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewAndroidStreamManager(logger)

	require.NotNil(t, mgr)
	assert.NotNil(t, mgr.logger)
	assert.NotNil(t, mgr.streams)
	assert.Nil(t, mgr.provider)
	assert.Nil(t, mgr.sendMessage)
}

func TestAndroidStreamManager_SetMessageSender(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewAndroidStreamManager(logger)

	var called bool
	sender := func(msg *pb.RunnerMessage) error {
		called = true
		return nil
	}

	mgr.SetMessageSender(sender)

	// Verify sender is set
	require.NotNil(t, mgr.sendMessage)

	// Verify sender works
	err := mgr.sendMessage(&pb.RunnerMessage{})
	require.NoError(t, err)
	assert.True(t, called)
}

func TestAndroidStreamManager_StartStream_MissingOptions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewAndroidStreamManager(logger)

	ctx := context.Background()
	cmd := &pb.CreateTunnel{
		TunnelId: "tun_test",
		Type:     "android",
		// AndroidOptions is nil
	}

	err := mgr.StartStream(ctx, cmd)
	assert.ErrorIs(t, err, ErrInvalidRequest)
}

func TestAndroidStreamManager_StopStream_NotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewAndroidStreamManager(logger)

	err := mgr.StopStream("nonexistent")
	assert.ErrorIs(t, err, ErrStreamNotFound)
}

func TestAndroidStreamManager_GetStreamInfo_NotFound(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewAndroidStreamManager(logger)

	_, err := mgr.GetStreamInfo("nonexistent")
	assert.ErrorIs(t, err, ErrStreamNotFound)
}

func TestAndroidStreamManager_Close(t *testing.T) {
	logger := zaptest.NewLogger(t)
	mgr := NewAndroidStreamManager(logger)

	// Close should not error on empty manager
	err := mgr.Close()
	assert.NoError(t, err)
}

func TestAgentVideoSink_OnVideoData_NilSender(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sink := &agentVideoSink{
		tunnelID:    "tun_test",
		sendMessage: nil,
		logger:      logger,
	}

	// Should not panic or error with nil sender
	err := sink.OnVideoData([]byte{0x00, 0x00, 0x00, 0x01})
	assert.NoError(t, err)
}

func TestAgentVideoSink_OnVideoData(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var receivedMsg *pb.RunnerMessage
	sender := func(msg *pb.RunnerMessage) error {
		receivedMsg = msg
		return nil
	}

	sink := &agentVideoSink{
		tunnelID:    "tun_test",
		sendMessage: sender,
		logger:      logger,
	}

	testData := []byte{0x00, 0x00, 0x00, 0x01, 0x67}
	err := sink.OnVideoData(testData)
	require.NoError(t, err)

	require.NotNil(t, receivedMsg)
	streamData := receivedMsg.GetAndroidStreamData()
	require.NotNil(t, streamData)
	assert.Equal(t, "tun_test", streamData.TunnelId)
	assert.True(t, streamData.IsVideo)
	assert.Equal(t, testData, streamData.Data)
}

func TestAgentVideoSink_OnAudioData(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var receivedMsg *pb.RunnerMessage
	sender := func(msg *pb.RunnerMessage) error {
		receivedMsg = msg
		return nil
	}

	sink := &agentVideoSink{
		tunnelID:    "tun_test",
		sendMessage: sender,
		logger:      logger,
	}

	testData := []byte{0xFF, 0xF1}
	err := sink.OnAudioData(testData)
	require.NoError(t, err)

	require.NotNil(t, receivedMsg)
	streamData := receivedMsg.GetAndroidStreamData()
	require.NotNil(t, streamData)
	assert.Equal(t, "tun_test", streamData.TunnelId)
	assert.False(t, streamData.IsVideo)
	assert.Equal(t, testData, streamData.Data)
}

func TestAgentVideoSink_OnVideoConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var receivedMsg *pb.RunnerMessage
	sender := func(msg *pb.RunnerMessage) error {
		receivedMsg = msg
		return nil
	}

	sink := &agentVideoSink{
		tunnelID:    "tun_test",
		sendMessage: sender,
		logger:      logger,
	}

	configData := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42}
	err := sink.OnVideoConfig(1920, 1080, "h264", configData)
	require.NoError(t, err)

	require.NotNil(t, receivedMsg)
	streamStarted := receivedMsg.GetAndroidStreamStarted()
	require.NotNil(t, streamStarted)
	assert.Equal(t, "tun_test", streamStarted.TunnelId)
	assert.Equal(t, int32(1920), streamStarted.Width)
	assert.Equal(t, int32(1080), streamStarted.Height)
	assert.Equal(t, "h264", streamStarted.VideoCodec)
	assert.Equal(t, configData, streamStarted.VideoConfig)

	// Second call should not send (configSent = true)
	receivedMsg = nil
	err = sink.OnVideoConfig(1920, 1080, "h264", configData)
	require.NoError(t, err)
	assert.Nil(t, receivedMsg, "should not send config twice")
}

func TestAgentVideoSink_OnError(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var receivedMsg *pb.RunnerMessage
	sender := func(msg *pb.RunnerMessage) error {
		receivedMsg = msg
		return nil
	}

	sink := &agentVideoSink{
		tunnelID:    "tun_test",
		sendMessage: sender,
		logger:      logger,
	}

	testErr := assert.AnError
	sink.OnError(testErr)

	require.NotNil(t, receivedMsg)
	streamStopped := receivedMsg.GetAndroidStreamStopped()
	require.NotNil(t, streamStopped)
	assert.Equal(t, "tun_test", streamStopped.TunnelId)
	assert.Equal(t, "error", streamStopped.Reason)
	assert.Contains(t, streamStopped.Error, testErr.Error())
}

func TestAgentVideoSink_OnClose(t *testing.T) {
	logger := zaptest.NewLogger(t)

	var receivedMsg *pb.RunnerMessage
	sender := func(msg *pb.RunnerMessage) error {
		receivedMsg = msg
		return nil
	}

	sink := &agentVideoSink{
		tunnelID:    "tun_test",
		sendMessage: sender,
		logger:      logger,
	}

	sink.OnClose()

	require.NotNil(t, receivedMsg)
	streamStopped := receivedMsg.GetAndroidStreamStopped()
	require.NotNil(t, streamStopped)
	assert.Equal(t, "tun_test", streamStopped.TunnelId)
	assert.Equal(t, "closed", streamStopped.Reason)
}

func TestAgentVideoSink_OnAudioConfig(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sink := &agentVideoSink{
		tunnelID:    "tun_test",
		sendMessage: nil,
		logger:      logger,
	}

	// OnAudioConfig is a no-op, should not error
	err := sink.OnAudioConfig(48000, 2, "opus", nil)
	assert.NoError(t, err)
}
