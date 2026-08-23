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
	conn := newTunnelConnection("tun_test", "conn_test", "run_test")
	responseCh := conn.responseCh
	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = conn
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
	conn := newTunnelConnection("tun_test", "conn_test", "run_correct")
	responseCh := conn.responseCh
	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = conn
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
	conn := newTunnelConnection("tun_test", "conn_test", "run_test")
	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = conn
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
	conn := newTunnelConnection("tun_test", "conn_test", "run_test")
	responseCh := conn.responseCh
	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = conn
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
	conn := newTunnelConnection("tun_test", "conn_1", "run_test")
	tr.connectionsMu.Lock()
	tr.connections["conn_1"] = conn
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

func TestTunnelRouter_CleanupIdleConnections(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	idle := newTunnelConnection("tun_test", "conn_idle", "")
	idle.lastActivity.Store(time.Now().Add(-2 * time.Hour).UnixNano())

	active := newTunnelConnection("tun_test", "conn_active", "")

	tr.connectionsMu.Lock()
	tr.connections["conn_idle"] = idle
	tr.connections["conn_active"] = active
	tr.connectionsMu.Unlock()

	cleaned := tr.CleanupIdleConnections(time.Hour)
	assert.Equal(t, 1, cleaned)

	tr.connectionsMu.RLock()
	_, idleExists := tr.connections["conn_idle"]
	_, activeExists := tr.connections["conn_active"]
	tr.connectionsMu.RUnlock()

	assert.False(t, idleExists)
	assert.True(t, activeExists)
}

// TestTunnelRouter_CleanupIdleConnections_SparesOldButActive is the regression
// test for the age-based sweep, which killed healthy long-lived WebSockets.
func TestTunnelRouter_CleanupIdleConnections_SparesOldButActive(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Created hours ago but carrying traffic right now.
	conn := newTunnelConnection("tun_test", "conn_ws", "run_test")
	conn.createdAt = time.Now().Add(-6 * time.Hour)

	tr.connectionsMu.Lock()
	tr.connections["conn_ws"] = conn
	tr.connectionsMu.Unlock()

	assert.Equal(t, 0, tr.CleanupIdleConnections(time.Hour))

	tr.connectionsMu.RLock()
	_, exists := tr.connections["conn_ws"]
	tr.connectionsMu.RUnlock()
	assert.True(t, exists, "an actively used connection must survive the sweep")
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

	// Fill the response channel so the send has to block.
	conn := newTunnelConnection("tun_test", "conn_test", "run_test")
	for i := 0; i < cap(conn.responseCh); i++ {
		conn.responseCh <- &pb.TunnelData{}
	}
	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = conn
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

// TestTunnelRouter_HandleTunnelData_StalledConsumer is the regression test for
// the old `default:` branch, which silently dropped bytes mid-body whenever a
// consumer fell behind. A stalled consumer must now fail loudly instead.
func TestTunnelRouter_HandleTunnelData_StalledConsumer(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(
		WithTRLogger(logger),
		WithTRSendTimeout(50*time.Millisecond),
	)

	conn := newTunnelConnection("tun_test", "conn_test", "run_test")
	for i := 0; i < cap(conn.responseCh); i++ {
		conn.responseCh <- &pb.TunnelData{}
	}

	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = conn
	tr.connectionsMu.Unlock()

	err := tr.HandleTunnelData(context.Background(), "run_test", &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_test",
		Data:         []byte("must not be dropped silently"),
	})

	require.ErrorIs(t, err, errSendTimeout)

	// The connection is torn down rather than left leaking bytes.
	tr.connectionsMu.RLock()
	_, exists := tr.connections["conn_test"]
	tr.connectionsMu.RUnlock()
	assert.False(t, exists, "a stalled connection must be closed, not kept")
}

// TestTunnelRouter_HandleTunnelData_BlocksUntilDrained proves the send waits
// for back-pressure to clear instead of discarding the frame.
func TestTunnelRouter_HandleTunnelData_BlocksUntilDrained(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(
		WithTRLogger(logger),
		WithTRSendTimeout(5*time.Second),
	)

	conn := newTunnelConnection("tun_test", "conn_test", "run_test")
	for i := 0; i < cap(conn.responseCh); i++ {
		conn.responseCh <- &pb.TunnelData{}
	}

	tr.connectionsMu.Lock()
	tr.connections["conn_test"] = conn
	tr.connectionsMu.Unlock()

	payload := []byte("late but intact")

	errCh := make(chan error, 1)
	go func() {
		errCh <- tr.HandleTunnelData(context.Background(), "run_test", &pb.TunnelData{
			TunnelId:     "tun_test",
			ConnectionId: "conn_test",
			Data:         payload,
		})
	}()

	// Drain one slot so the blocked send can complete.
	time.Sleep(20 * time.Millisecond)
	<-conn.responseCh

	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("send did not complete after the consumer drained a slot")
	}

	// Every buffered frame plus the late one is still queued: nothing dropped.
	assert.Equal(t, cap(conn.responseCh), len(conn.responseCh))
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

// TestTunnelRouter_CloseRace_NoSendOnClosedChannel is the regression test for
// the close race: HandleTunnelData read the connection under RLock, released
// it, and then sent, while CloseConnection closed responseCh outside the lock.
// Before the fix this panicked with "send on closed channel" under -race.
func TestTunnelRouter_CloseRace_NoSendOnClosedChannel(t *testing.T) {
	logger := zaptest.NewLogger(t)

	for iteration := 0; iteration < 200; iteration++ {
		tr := NewTunnelRouter(
			WithTRLogger(logger),
			WithTRSendTimeout(time.Second),
		)

		const connID = "conn_race"
		conn := newTunnelConnection("tun_race", connID, "run_race")
		tr.connectionsMu.Lock()
		tr.connections[connID] = conn
		tr.connectionsMu.Unlock()

		var wg sync.WaitGroup

		// Senders racing against the closer.
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = tr.HandleTunnelData(context.Background(), "run_race", &pb.TunnelData{
					TunnelId:     "tun_race",
					ConnectionId: connID,
					Data:         []byte("frame"),
				})
			}()
		}

		// Concurrent closers, exercising every teardown path.
		wg.Add(3)
		go func() { defer wg.Done(); tr.CloseConnection(connID) }()
		go func() { defer wg.Done(); tr.CloseConnection(connID) }()
		go func() { defer wg.Done(); tr.CleanupIdleConnections(0) }()

		// A consumer, so sends can also succeed rather than only time out.
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range conn.responseCh { //nolint:revive // drain until closed
			}
		}()

		wg.Wait()
	}
}

// TestTunnelRouter_HandleCloseTunnel_Race covers the same shape via the
// tunnel-wide teardown path.
func TestTunnelRouter_HandleCloseTunnel_Race(t *testing.T) {
	logger := zaptest.NewLogger(t)

	for iteration := 0; iteration < 200; iteration++ {
		tr := NewTunnelRouter(
			WithTRLogger(logger),
			WithTRSendTimeout(time.Second),
		)

		const connID = "conn_race"
		conn := newTunnelConnection("tun_race", connID, "run_race")
		tr.connectionsMu.Lock()
		tr.connections[connID] = conn
		tr.connectionsMu.Unlock()

		var wg sync.WaitGroup

		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = tr.HandleTunnelData(context.Background(), "run_race", &pb.TunnelData{
					TunnelId:     "tun_race",
					ConnectionId: connID,
					Data:         []byte("frame"),
				})
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = tr.HandleCloseTunnel(context.Background(), "run_race", "tun_race", "race")
		}()

		wg.Add(1)
		go func() {
			defer wg.Done()
			for range conn.responseCh { //nolint:revive // drain until closed
			}
		}()

		wg.Wait()
	}
}

// TestTunnelRouter_EOFAndClose_DoubleCloseSafe verifies the EOF path and an
// explicit close can both fire without a double-close panic.
func TestTunnelRouter_EOFAndClose_DoubleCloseSafe(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	const connID = "conn_eof"
	conn := newTunnelConnection("tun_eof", connID, "run_eof")
	tr.connectionsMu.Lock()
	tr.connections[connID] = conn
	tr.connectionsMu.Unlock()

	require.NoError(t, tr.HandleTunnelData(context.Background(), "run_eof", &pb.TunnelData{
		TunnelId:     "tun_eof",
		ConnectionId: connID,
		Eof:          true,
	}))

	// Second close must be a no-op, not a panic.
	tr.CloseConnection(connID)
	conn.close()

	// The consumer still observes the EOF frame, then the close.
	frame, ok := <-conn.responseCh
	require.True(t, ok)
	assert.True(t, frame.GetEof())

	_, ok = <-conn.responseCh
	assert.False(t, ok, "channel must be closed after teardown")
}
