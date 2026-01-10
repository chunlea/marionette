package grpc

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/tunnel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// mockTRConnectionManager is a mock for TunnelRouter tests.
type mockTRConnectionManager struct {
	mu       sync.Mutex
	commands map[string][]*pb.ServerCommand
	sendErr  error
}

func newMockTRConnectionManager() *mockTRConnectionManager {
	return &mockTRConnectionManager{
		commands: make(map[string][]*pb.ServerCommand),
	}
}

func (m *mockTRConnectionManager) SendCommand(runnerID string, cmd *pb.ServerCommand) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sendErr != nil {
		return m.sendErr
	}
	m.commands[runnerID] = append(m.commands[runnerID], cmd)
	return nil
}

func (m *mockTRConnectionManager) GetCommands(runnerID string) []*pb.ServerCommand {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.commands[runnerID]
}

func TestNewTunnelRouter(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tr := NewTunnelRouter(
		WithTRLogger(logger),
	)

	require.NotNil(t, tr)
	assert.NotNil(t, tr.logger)
	assert.NotNil(t, tr.connections)
	assert.NotNil(t, tr.tunnelRunners)
}

func TestTunnelRouter_RegisterTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Register tunnel
	tr.RegisterTunnel("tun_test123", "run_test456")

	// Verify
	runnerID, ok := tr.GetRunnerForTunnel("tun_test123")
	assert.True(t, ok)
	assert.Equal(t, "run_test456", runnerID)
}

func TestTunnelRouter_UnregisterTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Register and then unregister
	tr.RegisterTunnel("tun_test123", "run_test456")
	tr.UnregisterTunnel("tun_test123")

	// Verify removed
	_, ok := tr.GetRunnerForTunnel("tun_test123")
	assert.False(t, ok)
}

func TestTunnelRouter_SendRequest_NoTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Try to send without registering tunnel
	_, err := tr.SendRequest(context.Background(), "tun_unknown", "conn_123", []byte("test"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel not found")
}

func TestTunnelRouter_SendRequest_NoConnectionManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Register tunnel but no connection manager
	tr.RegisterTunnel("tun_test", "run_test")

	_, err := tr.SendRequest(context.Background(), "tun_test", "conn_123", []byte("test"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection manager not configured")
}

func TestTunnelRouter_HandleTunnelData(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Create a mock connection manually
	responseCh := make(chan *pb.TunnelData, 10)
	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = &tunnelConnection{
		tunnelID:     "tun_test",
		connectionID: "conn_test",
		runnerID:     "run_test",
		responseCh:   responseCh,
		createdAt:    time.Now(),
	}
	tr.connectionsMu.Unlock()

	// Handle incoming data
	err := tr.HandleTunnelData(context.Background(), "run_test", &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_test",
		Data:         []byte("response data"),
		Eof:          false,
	})
	require.NoError(t, err)

	// Verify data was routed
	select {
	case data := <-responseCh:
		assert.Equal(t, "tun_test", data.TunnelId)
		assert.Equal(t, "conn_test", data.ConnectionId)
		assert.Equal(t, []byte("response data"), data.Data)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for data")
	}
}

func TestTunnelRouter_HandleTunnelData_UnknownConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Handle data for unknown connection (should not error, just log)
	err := tr.HandleTunnelData(context.Background(), "run_test", &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_unknown",
		Data:         []byte("test"),
	})
	require.NoError(t, err)
}

func TestTunnelRouter_HandleTunnelData_WrongRunner(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Create connection
	responseCh := make(chan *pb.TunnelData, 10)
	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = &tunnelConnection{
		tunnelID:     "tun_test",
		connectionID: "conn_test",
		runnerID:     "run_correct",
		responseCh:   responseCh,
		createdAt:    time.Now(),
	}
	tr.connectionsMu.Unlock()

	// Handle data from wrong runner (should be ignored)
	err := tr.HandleTunnelData(context.Background(), "run_wrong", &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_test",
		Data:         []byte("test"),
	})
	require.NoError(t, err)

	// Verify no data was routed
	select {
	case <-responseCh:
		t.Fatal("unexpected data received")
	case <-time.After(100 * time.Millisecond):
		// Expected - no data should be routed
	}
}

func TestTunnelRouter_HandleTunnelData_EOF(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Create connection
	responseCh := make(chan *pb.TunnelData, 10)
	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = &tunnelConnection{
		tunnelID:     "tun_test",
		connectionID: "conn_test",
		runnerID:     "run_test",
		responseCh:   responseCh,
		createdAt:    time.Now(),
	}
	tr.connectionsMu.Unlock()

	// Handle EOF
	err := tr.HandleTunnelData(context.Background(), "run_test", &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_test",
		Eof:          true,
	})
	require.NoError(t, err)

	// Verify connection was closed
	tr.connectionsMu.RLock()
	_, exists := tr.connections["conn_test"]
	tr.connectionsMu.RUnlock()
	assert.False(t, exists)
}

func TestTunnelRouter_CloseConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Create connection
	responseCh := make(chan *pb.TunnelData, 10)
	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = &tunnelConnection{
		tunnelID:     "tun_test",
		connectionID: "conn_test",
		runnerID:     "run_test",
		responseCh:   responseCh,
		createdAt:    time.Now(),
	}
	tr.connectionsMu.Unlock()

	// Close connection
	tr.CloseConnection("conn_test")

	// Verify removed
	tr.connectionsMu.RLock()
	_, exists := tr.connections["conn_test"]
	tr.connectionsMu.RUnlock()
	assert.False(t, exists)

	// Verify channel is closed
	select {
	case _, ok := <-responseCh:
		assert.False(t, ok, "channel should be closed")
	default:
		t.Fatal("channel should be closed")
	}
}

func TestTunnelRouter_HandleCloseTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Create tunnel registration and connections
	tr.RegisterTunnel("tun_test", "run_test")
	responseCh := make(chan *pb.TunnelData, 10)
	tr.connectionsMu.Lock()
	tr.connections["conn_1"] = &tunnelConnection{
		tunnelID:     "tun_test",
		connectionID: "conn_1",
		runnerID:     "run_test",
		responseCh:   responseCh,
		createdAt:    time.Now(),
	}
	tr.connectionsMu.Unlock()

	// Handle close
	err := tr.HandleCloseTunnel(context.Background(), "run_test", "tun_test", "test cleanup")
	require.NoError(t, err)

	// Verify tunnel unregistered
	_, ok := tr.GetRunnerForTunnel("tun_test")
	assert.False(t, ok)

	// Verify connections closed
	tr.connectionsMu.RLock()
	_, exists := tr.connections["conn_1"]
	tr.connectionsMu.RUnlock()
	assert.False(t, exists)
}

func TestTunnelRouter_CleanupStaleConnections(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Create old and new connections
	tr.connectionsMu.Lock()
	tr.connections["conn_old"] = &tunnelConnection{
		tunnelID:     "tun_test",
		connectionID: "conn_old",
		responseCh:   make(chan *pb.TunnelData, 10),
		createdAt:    time.Now().Add(-2 * time.Hour),
	}
	tr.connections["conn_new"] = &tunnelConnection{
		tunnelID:     "tun_test",
		connectionID: "conn_new",
		responseCh:   make(chan *pb.TunnelData, 10),
		createdAt:    time.Now(),
	}
	tr.connectionsMu.Unlock()

	// Cleanup connections older than 1 hour
	cleaned := tr.CleanupStaleConnections(time.Hour)
	assert.Equal(t, 1, cleaned)

	// Verify old connection removed, new one remains
	tr.connectionsMu.RLock()
	_, oldExists := tr.connections["conn_old"]
	_, newExists := tr.connections["conn_new"]
	tr.connectionsMu.RUnlock()

	assert.False(t, oldExists)
	assert.True(t, newExists)
}

func TestNewTunnelRouter_WithOptions(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cm := newMockTRConnectionManager()
	tm := tunnel.NewTunnelManager()

	tr := NewTunnelRouter(
		WithTRLogger(logger),
		WithTRConnectionManager(cm),
		WithTRTunnelManager(tm),
	)

	require.NotNil(t, tr)
	assert.Equal(t, cm, tr.connManager)
	assert.Equal(t, tm, tr.tm)
}

func TestTunnelRouter_SendRequest_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cm := newMockTRConnectionManager()

	tr := NewTunnelRouter(
		WithTRLogger(logger),
		WithTRConnectionManager(cm),
	)

	// Register tunnel
	tr.RegisterTunnel("tun_test", "run_test")

	// Send request
	responseCh, err := tr.SendRequest(context.Background(), "tun_test", "conn_123", []byte("hello"))
	require.NoError(t, err)
	require.NotNil(t, responseCh)

	// Verify command was sent
	commands := cm.GetCommands("run_test")
	require.Len(t, commands, 1)

	tunnelData := commands[0].GetTunnelData()
	require.NotNil(t, tunnelData)
	assert.Equal(t, "tun_test", tunnelData.TunnelId)
	assert.Equal(t, "conn_123", tunnelData.ConnectionId)
	assert.Equal(t, []byte("hello"), tunnelData.Data)
	assert.False(t, tunnelData.Eof)

	// Verify connection was registered
	tr.connectionsMu.RLock()
	_, exists := tr.connections["conn_123"]
	tr.connectionsMu.RUnlock()
	assert.True(t, exists)

	// Cleanup
	tr.CloseConnection("conn_123")
}

func TestTunnelRouter_SendRequest_SendError(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cm := newMockTRConnectionManager()
	cm.sendErr = errors.New("connection lost")

	tr := NewTunnelRouter(
		WithTRLogger(logger),
		WithTRConnectionManager(cm),
	)

	// Register tunnel
	tr.RegisterTunnel("tun_test", "run_test")

	// Send request should fail
	responseCh, err := tr.SendRequest(context.Background(), "tun_test", "conn_123", []byte("hello"))
	require.Error(t, err)
	assert.Nil(t, responseCh)
	assert.Contains(t, err.Error(), "connection lost")

	// Verify connection was not registered (cleaned up)
	tr.connectionsMu.RLock()
	_, exists := tr.connections["conn_123"]
	tr.connectionsMu.RUnlock()
	assert.False(t, exists)
}

func TestTunnelRouter_SendEOF_Success(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cm := newMockTRConnectionManager()

	tr := NewTunnelRouter(
		WithTRLogger(logger),
		WithTRConnectionManager(cm),
	)

	// Register tunnel
	tr.RegisterTunnel("tun_test", "run_test")

	// Send EOF
	err := tr.SendEOF("tun_test", "conn_123")
	require.NoError(t, err)

	// Verify EOF command was sent
	commands := cm.GetCommands("run_test")
	require.Len(t, commands, 1)

	tunnelData := commands[0].GetTunnelData()
	require.NotNil(t, tunnelData)
	assert.Equal(t, "tun_test", tunnelData.TunnelId)
	assert.Equal(t, "conn_123", tunnelData.ConnectionId)
	assert.True(t, tunnelData.Eof)
}

func TestTunnelRouter_SendEOF_NoTunnel(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	err := tr.SendEOF("tun_unknown", "conn_123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel not found")
}

func TestTunnelRouter_SendEOF_NoConnectionManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Register tunnel but no connection manager
	tr.RegisterTunnel("tun_test", "run_test")

	err := tr.SendEOF("tun_test", "conn_123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection manager not configured")
}

func TestTunnelRouter_HandleTunnelData_ContextCancelled(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Create connection with full channel (buffer size 0)
	responseCh := make(chan *pb.TunnelData) // unbuffered
	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = &tunnelConnection{
		tunnelID:     "tun_test",
		connectionID: "conn_test",
		runnerID:     "run_test",
		responseCh:   responseCh,
		createdAt:    time.Now(),
	}
	tr.connectionsMu.Unlock()

	// Use cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Handle data with cancelled context
	err := tr.HandleTunnelData(ctx, "run_test", &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_test",
		Data:         []byte("test"),
	})

	// Should return context error
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestTunnelRouter_HandleTunnelData_ChannelFull(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Create connection with full channel
	responseCh := make(chan *pb.TunnelData, 1)
	responseCh <- &pb.TunnelData{} // fill the channel

	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = &tunnelConnection{
		tunnelID:     "tun_test",
		connectionID: "conn_test",
		runnerID:     "run_test",
		responseCh:   responseCh,
		createdAt:    time.Now(),
	}
	tr.connectionsMu.Unlock()

	// Handle data when channel is full (should drop and not block)
	err := tr.HandleTunnelData(context.Background(), "run_test", &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_test",
		Data:         []byte("dropped"),
	})

	// Should not error, just log warning and drop
	require.NoError(t, err)
}

func TestTunnelRouter_CloseConnection_NotExists(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Close non-existent connection (should not panic)
	tr.CloseConnection("conn_nonexistent")
}

func TestTunnelRouter_HandleCloseTunnel_WithTunnelManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tm := tunnel.NewTunnelManager(tunnel.WithLogger(logger))

	tr := NewTunnelRouter(
		WithTRLogger(logger),
		WithTRTunnelManager(tm),
	)

	// Register tunnel
	tr.RegisterTunnel("tun_test", "run_test")

	// Handle close (will call tm.Close which may return error for non-existent tunnel)
	err := tr.HandleCloseTunnel(context.Background(), "run_test", "tun_test", "test")

	// The error from tm.Close is returned (tunnel doesn't exist in tm)
	// This is expected behavior
	assert.Error(t, err)
}
