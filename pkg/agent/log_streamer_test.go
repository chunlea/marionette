package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
)

func TestDefaultLogStreamerConfig(t *testing.T) {
	cfg := DefaultLogStreamerConfig()

	assert.Equal(t, 100, cfg.BatchSize)
	assert.Equal(t, 100*time.Millisecond, cfg.FlushInterval)
	assert.Equal(t, 1000, cfg.BufferSize)
}

func TestLogStreamer_SetTask(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamer := NewLogStreamer(nil, "runner_1", "tenant_1", logger)

	streamer.SetTask("sess_123", "task_123", "run_123")

	streamer.mu.RLock()
	defer streamer.mu.RUnlock()
	assert.Equal(t, "sess_123", streamer.sessionID)
	assert.Equal(t, "task_123", streamer.taskID)
	assert.Equal(t, "run_123", streamer.runID)
}

func TestLogStreamer_ClearTask(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamer := NewLogStreamer(nil, "runner_1", "tenant_1", logger)

	streamer.SetTask("sess_123", "task_123", "run_123")
	streamer.ClearTask()

	streamer.mu.RLock()
	defer streamer.mu.RUnlock()
	assert.Empty(t, streamer.sessionID)
	assert.Empty(t, streamer.taskID)
	assert.Empty(t, streamer.runID)
}

func TestLogStreamer_HandleOutput_NoTask(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamer := NewLogStreamer(nil, "runner_1", "tenant_1", logger)

	// Without task set, output should be dropped
	streamer.HandleOutput("stdout", []byte("test output"))

	// Buffer should be empty
	select {
	case <-streamer.buffer:
		t.Fatal("expected buffer to be empty")
	default:
		// OK
	}
}

func TestLogStreamer_HandleOutput_WithTask(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamer := NewLogStreamer(nil, "runner_1", "tenant_1", logger)

	streamer.SetTask("sess_123", "task_123", "run_123")
	streamer.HandleOutput("stdout", []byte("test output\n"))

	// Should have entry in buffer
	select {
	case entry := <-streamer.buffer:
		assert.Equal(t, "sess_123", entry.SessionId)
		assert.Equal(t, "task_123", entry.TaskId)
		assert.Equal(t, "run_123", entry.RunId)
		assert.Equal(t, "stdout", entry.Stream)
		assert.Equal(t, "info", entry.Level)
		assert.Equal(t, "test output\n", entry.Content)
		assert.Equal(t, "runner_1", entry.RunnerId)
		assert.Equal(t, "tenant_1", entry.TenantId)
		assert.Equal(t, int64(1), entry.Sequence)
	default:
		t.Fatal("expected entry in buffer")
	}
}

func TestLogStreamer_HandleOutput_SequenceIncrement(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamer := NewLogStreamer(nil, "runner_1", "tenant_1", logger)

	streamer.SetTask("sess_123", "task_123", "run_123")

	// Send multiple outputs
	for i := 0; i < 3; i++ {
		streamer.HandleOutput("stdout", []byte("line\n"))
	}

	// Check sequence numbers
	for i := 0; i < 3; i++ {
		entry := <-streamer.buffer
		assert.Equal(t, int64(i+1), entry.Sequence)
	}
}

func TestLogStreamer_HandleOutput_BufferFull(t *testing.T) {
	logger := zaptest.NewLogger(t)
	config := LogStreamerConfig{
		BufferSize:    2, // Very small buffer
		BatchSize:     10,
		FlushInterval: time.Hour, // Long interval so we don't flush
	}
	streamer := NewLogStreamerWithConfig(nil, "runner_1", "tenant_1", config, logger)

	streamer.SetTask("sess_123", "task_123", "run_123")

	// Fill buffer
	streamer.HandleOutput("stdout", []byte("line1\n"))
	streamer.HandleOutput("stdout", []byte("line2\n"))

	// This should be dropped (buffer full)
	streamer.HandleOutput("stdout", []byte("line3\n"))

	// Only 2 entries should be in buffer
	count := 0
	for {
		select {
		case <-streamer.buffer:
			count++
		default:
			assert.Equal(t, 2, count)
			return
		}
	}
}

func TestLogStreamer_Log(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamer := NewLogStreamer(nil, "runner_1", "tenant_1", logger)

	streamer.SetTask("sess_123", "task_123", "run_123")
	streamer.Log("warn", "Something happened", map[string]string{"key": "value"})

	select {
	case entry := <-streamer.buffer:
		assert.Equal(t, "system", entry.Stream)
		assert.Equal(t, "warn", entry.Level)
		assert.Equal(t, "Something happened", entry.Content)
		assert.Equal(t, "value", entry.Metadata["key"])
	default:
		t.Fatal("expected entry in buffer")
	}
}

func TestLogStreamer_Log_NoTask(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamer := NewLogStreamer(nil, "runner_1", "tenant_1", logger)

	// Without task set, log should be dropped
	streamer.Log("info", "test", nil)

	select {
	case <-streamer.buffer:
		t.Fatal("expected buffer to be empty")
	default:
		// OK
	}
}

func TestLogStreamer_HandlePermissionRequest(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamer := NewLogStreamer(nil, "runner_1", "tenant_1", logger)

	// Placeholder implementation always returns true
	approved, err := streamer.HandlePermissionRequest(context.Background(), nil)
	assert.NoError(t, err)
	assert.True(t, approved)
}

func TestLogStreamer_StartStop(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamer := NewLogStreamer(nil, "runner_1", "tenant_1", logger)

	ctx := context.Background()
	err := streamer.Start(ctx)
	require.NoError(t, err)

	// Starting again should be a no-op
	err = streamer.Start(ctx)
	require.NoError(t, err)

	streamer.Stop()

	// Stopping again should be a no-op
	streamer.Stop()
}

// MockStreamLogsClient is a test helper for StreamLogs.
type MockStreamLogsClient struct {
	grpc.ClientStream
	mu       sync.Mutex
	messages []*pb.StreamLogsMessage
	response *pb.StreamLogsResponse
	sendErr  error
	closeErr error
}

func (m *MockStreamLogsClient) Send(msg *pb.StreamLogsMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.messages = append(m.messages, msg)
	return nil
}

func (m *MockStreamLogsClient) CloseAndRecv() (*pb.StreamLogsResponse, error) {
	if m.closeErr != nil {
		return nil, m.closeErr
	}
	if m.response == nil {
		return &pb.StreamLogsResponse{}, nil
	}
	return m.response, nil
}

func (m *MockStreamLogsClient) GetMessages() []*pb.StreamLogsMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]*pb.StreamLogsMessage, len(m.messages))
	copy(result, m.messages)
	return result
}

func TestLogStreamer_FlushEmpty(t *testing.T) {
	logger := zaptest.NewLogger(t)
	streamer := NewLogStreamer(nil, "runner_1", "tenant_1", logger)

	// Flush with no task should not error
	err := streamer.Flush(context.Background())
	assert.NoError(t, err)

	// Set task but don't add any logs
	streamer.SetTask("sess_123", "task_123", "run_123")
	err = streamer.Flush(context.Background())
	assert.NoError(t, err)
}

func TestLogStreamer_Integration(t *testing.T) {
	// This test verifies the full streaming flow with a mock server
	mockServer, err := NewMockServer()
	require.NoError(t, err)
	mockServer.Start()
	defer mockServer.Stop()

	logger := zaptest.NewLogger(t)

	// Create client connected to mock server
	cfg := &Config{
		Server: ServerConfig{
			Address: mockServer.Addr(),
		},
		Runner: RunnerConfig{
			Token: "test_token",
		},
	}
	client := NewClient(cfg, logger)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	// Create log streamer with the connected client
	streamer := NewLogStreamer(client, "runner_1", "tenant_1", logger)

	// Set task context
	streamer.SetTask("sess_123", "task_123", "run_123")

	// Add some logs
	streamer.HandleOutput("stdout", []byte("line 1\n"))
	streamer.HandleOutput("stdout", []byte("line 2\n"))
	streamer.HandleOutput("stderr", []byte("error line\n"))
	streamer.Log("info", "test message", map[string]string{"key": "value"})

	// Flush logs to server
	err = streamer.Flush(ctx)
	require.NoError(t, err)

	// Verify logs were sent (4 logs + 1 init message = 5 messages)
	mockServer.mu.Lock()
	logCount := mockServer.LogCount
	mockServer.mu.Unlock()

	assert.Equal(t, int64(5), logCount)
}

func TestLogStreamer_StreamLoop(t *testing.T) {
	// Test the streaming loop with automatic flushing
	mockServer, err := NewMockServer()
	require.NoError(t, err)
	mockServer.Start()
	defer mockServer.Stop()

	logger := zaptest.NewLogger(t)

	// Create client connected to mock server
	cfg := &Config{
		Server: ServerConfig{
			Address: mockServer.Addr(),
		},
		Runner: RunnerConfig{
			Token: "test_token",
		},
	}
	client := NewClient(cfg, logger)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	// Create log streamer with short flush interval
	config := LogStreamerConfig{
		BatchSize:     2,
		FlushInterval: 50 * time.Millisecond,
		BufferSize:    100,
	}
	streamer := NewLogStreamerWithConfig(client, "runner_1", "tenant_1", config, logger)

	// Start the stream loop
	err = streamer.Start(ctx)
	require.NoError(t, err)

	// Set task context
	streamer.SetTask("sess_123", "task_123", "run_123")

	// Add some logs (should trigger batch send at 2)
	streamer.HandleOutput("stdout", []byte("line 1\n"))
	streamer.HandleOutput("stdout", []byte("line 2\n"))

	// Wait for auto-flush
	time.Sleep(100 * time.Millisecond)

	// Add one more (will be flushed on stop)
	streamer.HandleOutput("stdout", []byte("line 3\n"))

	// Stop the streamer (should flush remaining)
	streamer.Stop()

	// Verify logs were sent
	// Batch 1: init + 2 logs = 3 messages (triggered when batch hits size 2)
	// Batch 2: init + 1 log = 2 messages (flushed on stop)
	// Total: 5 messages
	mockServer.mu.Lock()
	logCount := mockServer.LogCount
	mockServer.mu.Unlock()

	assert.Equal(t, int64(5), logCount)
}
