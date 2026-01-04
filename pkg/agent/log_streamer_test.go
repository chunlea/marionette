package agent

import (
	"context"
	"sync"
	"testing"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
)

// mockStreamLogsClient implements pb.RunnerService_StreamLogsClient for testing.
type mockStreamLogsClient struct {
	grpc.ClientStream
	mu       sync.Mutex
	messages []*pb.StreamLogsMessage
	closed   bool
	response *pb.StreamLogsResponse
	sendErr  error
	closeErr error
}

func (m *mockStreamLogsClient) Send(msg *pb.StreamLogsMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, msg)
	return nil
}

func (m *mockStreamLogsClient) CloseAndRecv() (*pb.StreamLogsResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	if m.closeErr != nil {
		return nil, m.closeErr
	}
	if m.response == nil {
		m.response = &pb.StreamLogsResponse{}
	}
	return m.response, nil
}

func (m *mockStreamLogsClient) Messages() []*pb.StreamLogsMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.messages
}

// mockRunnerServiceClientForLogs implements pb.RunnerServiceClient for testing.
type mockRunnerServiceClientForLogs struct {
	pb.RunnerServiceClient
	streamClient *mockStreamLogsClient
	streamErr    error
}

func (m *mockRunnerServiceClientForLogs) StreamLogs(ctx context.Context, opts ...grpc.CallOption) (pb.RunnerService_StreamLogsClient, error) {
	if m.streamErr != nil {
		return nil, m.streamErr
	}
	return m.streamClient, nil
}

func TestNewGRPCLogStreamer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client := &mockRunnerServiceClientForLogs{}

	streamer := NewGRPCLogStreamer(client, "run_123", logger)

	assert.NotNil(t, streamer)
	assert.Equal(t, client, streamer.client)
	assert.Equal(t, "run_123", streamer.runnerID)
	assert.False(t, streamer.IsActive())
}

func TestGRPCLogStreamer_Start_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamClient := &mockStreamLogsClient{
		response: &pb.StreamLogsResponse{
			LogsReceived: 10,
			LogsStored:   10,
		},
	}
	client := &mockRunnerServiceClientForLogs{
		streamClient: streamClient,
	}

	streamer := NewGRPCLogStreamer(client, "run_123", logger)

	init := &pb.StreamLogsInit{
		SessionId: "sess_123",
		TaskId:    "task_456",
		RunId:     "trun_789",
	}

	err := streamer.Start(context.Background(), init)

	require.NoError(t, err)
	assert.True(t, streamer.IsActive())

	// Verify init message was sent
	messages := streamClient.Messages()
	require.Len(t, messages, 1)
	initMsg := messages[0].GetInit()
	require.NotNil(t, initMsg)
	assert.Equal(t, "sess_123", initMsg.SessionId)
	assert.Equal(t, "task_456", initMsg.TaskId)
	assert.Equal(t, "trun_789", initMsg.RunId)
	assert.Equal(t, "run_123", initMsg.RunnerId)
}

func TestGRPCLogStreamer_Start_AlreadyActive(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamClient := &mockStreamLogsClient{}
	client := &mockRunnerServiceClientForLogs{
		streamClient: streamClient,
	}

	streamer := NewGRPCLogStreamer(client, "run_123", logger)

	init := &pb.StreamLogsInit{
		SessionId: "sess_123",
		TaskId:    "task_456",
		RunId:     "trun_789",
	}

	// Start first time
	err := streamer.Start(context.Background(), init)
	require.NoError(t, err)

	// Start second time - should fail
	err = streamer.Start(context.Background(), init)
	assert.ErrorIs(t, err, ErrStreamAlreadyActive)
}

func TestGRPCLogStreamer_Send_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamClient := &mockStreamLogsClient{}
	client := &mockRunnerServiceClientForLogs{
		streamClient: streamClient,
	}

	streamer := NewGRPCLogStreamer(client, "run_123", logger)

	// Start stream first
	init := &pb.StreamLogsInit{
		SessionId: "sess_123",
		TaskId:    "task_456",
		RunId:     "trun_789",
	}
	err := streamer.Start(context.Background(), init)
	require.NoError(t, err)

	// Send log entry
	entry := &pb.LogEntry{
		TaskId:    "task_456",
		RunId:     "trun_789",
		SessionId: "sess_123",
		Stream:    "stdout",
		Level:     "info",
		Content:   "test log message",
	}
	err = streamer.Send(entry)
	require.NoError(t, err)

	// Verify log entry was sent
	messages := streamClient.Messages()
	require.Len(t, messages, 2) // init + log entry
	logMsg := messages[1].GetLogEntry()
	require.NotNil(t, logMsg)
	assert.Equal(t, "test log message", logMsg.Content)
	assert.Equal(t, "run_123", logMsg.RunnerId)
	assert.Equal(t, int64(1), logMsg.Sequence)
	assert.Greater(t, logMsg.TimestampUnixMs, int64(0))
}

func TestGRPCLogStreamer_Send_NotActive(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client := &mockRunnerServiceClientForLogs{}

	streamer := NewGRPCLogStreamer(client, "run_123", logger)

	entry := &pb.LogEntry{
		Content: "test",
	}
	err := streamer.Send(entry)

	assert.ErrorIs(t, err, ErrStreamNotActive)
}

func TestGRPCLogStreamer_Send_SequenceIncrement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamClient := &mockStreamLogsClient{}
	client := &mockRunnerServiceClientForLogs{
		streamClient: streamClient,
	}

	streamer := NewGRPCLogStreamer(client, "run_123", logger)

	// Start stream
	init := &pb.StreamLogsInit{
		SessionId: "sess_123",
	}
	err := streamer.Start(context.Background(), init)
	require.NoError(t, err)

	// Send multiple entries
	for i := 0; i < 5; i++ {
		entry := &pb.LogEntry{
			Content: "test",
		}
		err := streamer.Send(entry)
		require.NoError(t, err)
	}

	// Verify sequences are incrementing
	messages := streamClient.Messages()
	require.Len(t, messages, 6) // 1 init + 5 entries
	for i := 1; i <= 5; i++ {
		logMsg := messages[i].GetLogEntry()
		require.NotNil(t, logMsg)
		assert.Equal(t, int64(i), logMsg.Sequence)
	}
}

func TestGRPCLogStreamer_Close_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamClient := &mockStreamLogsClient{
		response: &pb.StreamLogsResponse{
			LogsReceived: 10,
			LogsStored:   8,
			LogsDropped:  2,
		},
	}
	client := &mockRunnerServiceClientForLogs{
		streamClient: streamClient,
	}

	streamer := NewGRPCLogStreamer(client, "run_123", logger)

	// Start stream
	init := &pb.StreamLogsInit{
		SessionId: "sess_123",
	}
	err := streamer.Start(context.Background(), init)
	require.NoError(t, err)
	assert.True(t, streamer.IsActive())

	// Close stream
	resp, err := streamer.Close()
	require.NoError(t, err)
	assert.False(t, streamer.IsActive())
	assert.Equal(t, int64(10), resp.LogsReceived)
	assert.Equal(t, int64(8), resp.LogsStored)
	assert.Equal(t, int64(2), resp.LogsDropped)
}

func TestGRPCLogStreamer_Close_NotActive(t *testing.T) {
	logger := zaptest.NewLogger(t)
	client := &mockRunnerServiceClientForLogs{}

	streamer := NewGRPCLogStreamer(client, "run_123", logger)

	// Close without starting - should return empty response
	resp, err := streamer.Close()
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestGRPCLogStreamer_Close_Twice(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamClient := &mockStreamLogsClient{
		response: &pb.StreamLogsResponse{
			LogsReceived: 5,
		},
	}
	client := &mockRunnerServiceClientForLogs{
		streamClient: streamClient,
	}

	streamer := NewGRPCLogStreamer(client, "run_123", logger)

	// Start stream
	init := &pb.StreamLogsInit{
		SessionId: "sess_123",
	}
	err := streamer.Start(context.Background(), init)
	require.NoError(t, err)

	// Close first time
	resp, err := streamer.Close()
	require.NoError(t, err)
	assert.Equal(t, int64(5), resp.LogsReceived)

	// Close second time - should return empty response
	resp, err = streamer.Close()
	require.NoError(t, err)
	assert.Equal(t, int64(0), resp.LogsReceived)
}

func TestGRPCLogStreamer_FullLifecycle(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamClient := &mockStreamLogsClient{
		response: &pb.StreamLogsResponse{
			LogsReceived: 3,
			LogsStored:   3,
		},
	}
	client := &mockRunnerServiceClientForLogs{
		streamClient: streamClient,
	}

	streamer := NewGRPCLogStreamer(client, "run_123", logger)

	// Start
	init := &pb.StreamLogsInit{
		SessionId: "sess_123",
		TaskId:    "task_456",
		RunId:     "trun_789",
	}
	err := streamer.Start(context.Background(), init)
	require.NoError(t, err)
	assert.True(t, streamer.IsActive())

	// Send multiple log entries
	streams := []string{"stdout", "stderr", "system"}
	for _, stream := range streams {
		entry := &pb.LogEntry{
			TaskId:    "task_456",
			RunId:     "trun_789",
			SessionId: "sess_123",
			Stream:    stream,
			Level:     "info",
			Content:   "log from " + stream,
		}
		err := streamer.Send(entry)
		require.NoError(t, err)
	}

	// Close
	resp, err := streamer.Close()
	require.NoError(t, err)
	assert.False(t, streamer.IsActive())
	assert.Equal(t, int64(3), resp.LogsReceived)

	// Verify all messages
	messages := streamClient.Messages()
	require.Len(t, messages, 4) // 1 init + 3 log entries

	// Verify init
	initMsg := messages[0].GetInit()
	require.NotNil(t, initMsg)
	assert.Equal(t, "sess_123", initMsg.SessionId)

	// Verify log entries
	for i, stream := range streams {
		logMsg := messages[i+1].GetLogEntry()
		require.NotNil(t, logMsg)
		assert.Equal(t, stream, logMsg.Stream)
		assert.Equal(t, "log from "+stream, logMsg.Content)
		assert.Equal(t, int64(i+1), logMsg.Sequence)
	}
}
