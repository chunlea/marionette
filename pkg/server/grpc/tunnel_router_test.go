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
	registerForTest(t, tr, conn)

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
	registerForTest(t, tr, conn)

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
	registerForTest(t, tr, conn)

	// Handle EOF
	err := tr.HandleTunnelData(context.Background(), "run_test", &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_test",
		Eof:          true,
	})
	require.NoError(t, err)

	// The pump delivers EOF and then tears the connection down.
	waitPumpDone(t, conn)
	assertConnectionGone(t, tr, "conn_test")
}

func TestTunnelRouter_CloseConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Create connection
	conn := newTunnelConnection("tun_test", "conn_test", "run_test")
	responseCh := conn.responseCh
	registerForTest(t, tr, conn)

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
	registerForTest(t, tr, conn)

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

	registerForTest(t, tr, idle)
	registerForTest(t, tr, active)

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

	registerForTest(t, tr, conn)

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
	err := tr.SendEOF(context.Background(), "tun_test", "conn_123")
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

	err := tr.SendEOF(context.Background(), "tun_unknown", "conn_123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tunnel not found")
}

func TestTunnelRouter_SendEOF_NoConnectionManager(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	// Register tunnel but no connection manager
	tr.RegisterTunnel("tun_test", "run_test")

	err := tr.SendEOF(context.Background(), "tun_test", "conn_123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection manager not configured")
}

// The caller's context no longer gates delivery. Enqueueing is instant and the
// pump owns the wait, so a control stream whose context is already cancelled
// still hands the frame over rather than dropping it on the floor.
func TestTunnelRouter_HandleTunnelData_CallerContextDoesNotGateDelivery(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(WithTRLogger(logger))

	conn := newTunnelConnection("tun_test", "conn_test", "run_test")
	registerForTest(t, tr, conn)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := tr.HandleTunnelData(ctx, "run_test", &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_test",
		Data:         []byte("delivered anyway"),
	})
	require.NoError(t, err)

	select {
	case frame := <-conn.responseCh:
		assert.Equal(t, []byte("delivered anyway"), frame.GetData())
	case <-time.After(3 * time.Second):
		t.Fatal("frame was not delivered")
	}
}

// TestTunnelRouter_HandleTunnelData_StalledConsumer is the regression test for
// the old `default:` branch, which silently dropped bytes mid-body whenever a
// consumer fell behind. A stalled consumer must still fail loudly - the wait
// simply happens on the pump now instead of on the caller.
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

	registerForTest(t, tr, conn)

	// The caller is not held up by the stalled consumer.
	err := tr.HandleTunnelData(context.Background(), "run_test", &pb.TunnelData{
		TunnelId:     "tun_test",
		ConnectionId: "conn_test",
		Data:         []byte("must not be dropped silently"),
	})
	require.NoError(t, err)

	// The pump waits out the send timeout and then tears the connection down,
	// rather than leaking bytes.
	waitPumpDone(t, conn)
	assertConnectionGone(t, tr, "conn_test")
}

// A consumer far enough behind to fill the inbox is rejected at enqueue, which
// is the one place a full queue is reported to the caller. It is still a
// teardown, never a silent drop.
func TestTunnelRouter_HandleTunnelData_InboxOverflowTearsDownConnection(t *testing.T) {
	logger := zaptest.NewLogger(t)
	tr := NewTunnelRouter(
		WithTRLogger(logger),
		WithTRSendTimeout(30*time.Second), // long: the pump must stay blocked
	)

	conn := newTunnelConnection("tun_test", "conn_test", "run_test")
	for i := 0; i < cap(conn.responseCh); i++ {
		conn.responseCh <- &pb.TunnelData{}
	}
	registerForTest(t, tr, conn)

	frame := func() *pb.TunnelData {
		return &pb.TunnelData{
			TunnelId:     "tun_test",
			ConnectionId: "conn_test",
			Data:         []byte("payload"),
		}
	}

	// Fill the inbox. One frame may be in flight on the pump, so allow for it.
	var err error
	for i := 0; i < cap(conn.inbox)+2; i++ {
		if err = tr.HandleTunnelData(context.Background(), "run_test", frame()); err != nil {
			break
		}
	}

	require.ErrorIs(t, err, errInboxFull)
	assertConnectionGone(t, tr, "conn_test")
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

	registerForTest(t, tr, conn)

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
		registerForTest(t, tr, conn)

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
		registerForTest(t, tr, conn)

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
	registerForTest(t, tr, conn)

	require.NoError(t, tr.HandleTunnelData(context.Background(), "run_eof", &pb.TunnelData{
		TunnelId:     "tun_eof",
		ConnectionId: connID,
		Eof:          true,
	}))

	// Let the pump deliver EOF and tear the connection down.
	waitPumpDone(t, conn)

	// Further closes must be no-ops, not panics.
	tr.CloseConnection(connID)
	conn.close()

	// The consumer still observes the EOF frame, then the close.
	frame, ok := <-conn.responseCh
	require.True(t, ok)
	assert.True(t, frame.GetEof())

	_, ok = <-conn.responseCh
	assert.False(t, ok, "channel must be closed after teardown")
}

// waitPumpDone blocks until a connection's pump goroutine has exited. Delivery
// is asynchronous now, so assertions about it have to wait for the pump rather
// than assume the work happened inline.
func waitPumpDone(t *testing.T, conn *tunnelConnection) {
	t.Helper()
	select {
	case <-conn.pumpDone:
	case <-time.After(5 * time.Second):
		t.Fatal("pump did not exit")
	}
}

// assertConnectionGone waits for a connection to leave the router's map. The
// pump tears down on its own goroutine, so removal is eventual.
func assertConnectionGone(t *testing.T, tr *TunnelRouter, connectionID string) {
	t.Helper()
	require.Eventually(t, func() bool {
		tr.connectionsMu.RLock()
		defer tr.connectionsMu.RUnlock()
		_, exists := tr.connections[connectionID]
		return !exists
	}, 5*time.Second, 5*time.Millisecond, "connection %s was not torn down", connectionID)
}

// TestTunnelRouter_StalledConsumerDoesNotBlockOtherTraffic is the regression
// test for the head-of-line blocking the bounded blocking send introduced.
//
// A runner's control stream dispatches messages one at a time. When the router
// waited for a stalled consumer inline, that wait held the whole stream: the
// runner's heartbeats and every other tunnel's frames queued behind one wedged
// HTTP client for up to the send timeout. The pump moves that wait onto the
// connection it belongs to.
//
// The stalled connection here can never drain, and the send timeout is long, so
// before the pump this test would take at least sendTimeout to get past the
// first frame. It now completes in milliseconds.
func TestTunnelRouter_StalledConsumerDoesNotBlockOtherTraffic(t *testing.T) {
	logger := zaptest.NewLogger(t)

	const sendTimeout = 30 * time.Second
	tr := NewTunnelRouter(
		WithTRLogger(logger),
		WithTRSendTimeout(sendTimeout),
	)

	// A consumer that never reads: its response channel starts full.
	stalled := newTunnelConnection("tun_stalled", "conn_stalled", "run_1")
	for i := 0; i < cap(stalled.responseCh); i++ {
		stalled.responseCh <- &pb.TunnelData{}
	}
	registerForTest(t, tr, stalled)

	// A healthy connection on a different tunnel, same runner.
	healthy := newTunnelConnection("tun_healthy", "conn_healthy", "run_1")
	registerForTest(t, tr, healthy)

	// Stand in for the control stream reader: it dispatches sequentially, so
	// anything it blocks on delays everything behind it.
	reader := func(connectionID, tunnelID string, payload []byte) error {
		return tr.HandleTunnelData(context.Background(), "run_1", &pb.TunnelData{
			TunnelId:     tunnelID,
			ConnectionId: connectionID,
			Data:         payload,
		})
	}

	start := time.Now()

	// One frame for the wedged consumer, which the pump will sit on.
	require.NoError(t, reader("conn_stalled", "tun_stalled", []byte("blocks the pump")))

	// The very next message on the same stream must not wait for it.
	require.NoError(t, reader("conn_healthy", "tun_healthy", []byte("must not wait")))

	select {
	case frame := <-healthy.responseCh:
		assert.Equal(t, []byte("must not wait"), frame.GetData())
	case <-time.After(5 * time.Second):
		t.Fatal("healthy tunnel starved behind the stalled one")
	}

	elapsed := time.Since(start)
	assert.Less(t, elapsed, sendTimeout/10,
		"other traffic waited on the stalled consumer (took %s)", elapsed)

	// And the reader keeps flowing for the rest of the stream.
	for i := 0; i < 20; i++ {
		require.NoError(t, reader("conn_healthy", "tun_healthy", []byte("still flowing")))
		select {
		case <-healthy.responseCh:
		case <-time.After(5 * time.Second):
			t.Fatalf("healthy tunnel stalled on frame %d", i)
		}
	}

	assert.Less(t, time.Since(start), sendTimeout/10,
		"sustained traffic was throttled by the stalled consumer")

	// The stalled connection is still pending its own teardown, unaffecting
	// everyone else. Clean it up so the pump goroutine does not outlive the test.
	tr.CloseConnection("conn_stalled")
	waitPumpDone(t, stalled)
}

// The same property, stated as the interleaving that actually matters: a
// heartbeat dispatched behind a stalled tunnel frame is not delayed by it.
func TestTunnelRouter_StalledConsumerDoesNotDelayHeartbeats(t *testing.T) {
	logger := zaptest.NewLogger(t)

	const sendTimeout = 30 * time.Second
	tr := NewTunnelRouter(WithTRLogger(logger), WithTRSendTimeout(sendTimeout))

	stalled := newTunnelConnection("tun_stalled", "conn_stalled", "run_1")
	for i := 0; i < cap(stalled.responseCh); i++ {
		stalled.responseCh <- &pb.TunnelData{}
	}
	registerForTest(t, tr, stalled)

	heartbeats := make(chan time.Time, 8)

	// The reader loop: a tunnel frame for the wedged consumer, then a
	// heartbeat, repeatedly. Both run on the one goroutine, as they do in
	// RunnerService.
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 8; i++ {
			_ = tr.HandleTunnelData(context.Background(), "run_1", &pb.TunnelData{
				TunnelId:     "tun_stalled",
				ConnectionId: "conn_stalled",
				Data:         []byte("wedged"),
			})
			heartbeats <- time.Now()
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("reader loop blocked behind the stalled consumer")
	}

	require.Len(t, heartbeats, 8, "every heartbeat should have been dispatched")

	tr.CloseConnection("conn_stalled")
	waitPumpDone(t, stalled)
}

// registerForTest registers a connection and guarantees its pump has exited
// before the test ends.
//
// A pump outliving its test writes to a zaptest logger whose testing.T has
// already returned, which races the test framework. Closing here also keeps
// pump goroutines from leaking between tests.
func registerForTest(t *testing.T, tr *TunnelRouter, conn *tunnelConnection) {
	t.Helper()

	tr.register(conn)

	t.Cleanup(func() {
		tr.CloseConnection(conn.connectionID)
		select {
		case <-conn.pumpDone:
		case <-time.After(5 * time.Second):
			t.Error("pump did not exit before the test ended")
		}
	})
}

// restartTunnelStore is a tunnel.Store holding rows that outlived a server
// restart: the manager's cache and the router's registration map both start
// empty, exactly as they do in a freshly started process.
type restartTunnelStore struct {
	mu      sync.Mutex
	tunnels map[string]*tunnel.Tunnel
	gets    int
}

func newRestartTunnelStore(tunnels ...*tunnel.Tunnel) *restartTunnelStore {
	s := &restartTunnelStore{tunnels: make(map[string]*tunnel.Tunnel, len(tunnels))}
	for _, t := range tunnels {
		s.tunnels[t.ID] = t
	}
	return s
}

func (s *restartTunnelStore) CreateTunnel(_ context.Context, t *tunnel.Tunnel, _ string, _ int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tunnels[t.ID] = t
	return nil
}

func (s *restartTunnelStore) GetTunnel(_ context.Context, id string) (*tunnel.Tunnel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	t, ok := s.tunnels[id]
	if !ok {
		return nil, tunnel.ErrTunnelNotFound
	}
	return t, nil
}

func (s *restartTunnelStore) GetTunnelByHash(_ context.Context, _ string) (*tunnel.Tunnel, error) {
	return nil, tunnel.ErrTunnelNotFound
}

func (s *restartTunnelStore) ListTunnels(_ context.Context, _ tunnel.ListOptions) ([]*tunnel.Tunnel, error) {
	return nil, nil
}

func (s *restartTunnelStore) UpdateTunnel(_ context.Context, id string, updates tunnel.Updates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tunnels[id]
	if !ok {
		return tunnel.ErrTunnelNotFound
	}
	if updates.ClosedAt != nil {
		t.ClosedAt = updates.ClosedAt
	}
	return nil
}

func (s *restartTunnelStore) DeleteExpiredTunnels(_ context.Context) (int64, error) {
	return 0, nil
}

func (s *restartTunnelStore) getCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gets
}

// storedTunnel builds a tunnel row as the store would hold it.
func storedTunnel(id, runnerID string, expiresIn time.Duration) *tunnel.Tunnel {
	return &tunnel.Tunnel{
		ID:        id,
		SessionID: "ses_restart",
		RunnerID:  runnerID,
		Type:      tunnel.TypeHTTP,
		Direction: tunnel.DirectionOutbound,
		LocalPort: 3000,
		ExpiresAt: time.Now().Add(expiresIn),
		CreatedAt: time.Now().Add(-time.Minute),
	}
}

// restartedRouter builds the post-restart pair: a manager whose cache is empty
// but whose store is warm, and a router that has never seen a RegisterTunnel
// call.
func restartedRouter(t *testing.T, store *restartTunnelStore) (*TunnelRouter, *mockTRConnectionManager) {
	t.Helper()

	logger := zaptest.NewLogger(t)
	tm := tunnel.NewTunnelManager(
		tunnel.WithLogger(logger),
		tunnel.WithStore(store),
	)
	cm := newMockTRConnectionManager()

	tr := NewTunnelRouter(
		WithTRLogger(logger),
		WithTRConnectionManager(cm),
		WithTRTunnelManager(tm),
	)

	return tr, cm
}

// TestTunnelRouter_RoutesTunnelCreatedBeforeRestart is the regression test for
// the read-through residual. The router's tunnelRunners map is filled by
// RegisterTunnel at creation time only, so after a restart it knew no tunnels
// and every send failed with "tunnel not found" — even though the proxy in
// front of it had already resolved and authenticated the same tunnel through
// the store.
func TestTunnelRouter_RoutesTunnelCreatedBeforeRestart(t *testing.T) {
	const (
		tunnelID = "tun_before_restart"
		runnerID = "run_reconnected"
		connID   = "conn_after_restart"
	)

	store := newRestartTunnelStore(storedTunnel(tunnelID, runnerID, time.Hour))
	tr, cm := restartedRouter(t, store)

	// Nothing registered this tunnel: this is a cold router.
	_, cached := tr.GetRunnerForTunnel(tunnelID)
	require.False(t, cached, "precondition: the router must start with no registration")

	responseCh, err := tr.SendRequest(context.Background(), tunnelID, connID, []byte("GET / HTTP/1.1"))
	require.NoError(t, err, "a tunnel that outlived the restart must still be routable")
	require.NotNil(t, responseCh)
	closeConnForTest(t, tr, connID)

	// The remaining two routing paths must resolve the same way.
	require.NoError(t, tr.SendData(context.Background(), tunnelID, connID, []byte("more"), false))
	require.NoError(t, tr.SendEOF(context.Background(), tunnelID, connID))

	commands := cm.GetCommands(runnerID)
	require.Len(t, commands, 3, "all three sends must reach the runner the store names")
	assert.Equal(t, tunnelID, commands[0].GetTunnelData().GetTunnelId())
	assert.True(t, commands[2].GetTunnelData().GetEof())

	// Resolution is cached back, so the store is read once per tunnel rather
	// than once per frame.
	gotRunner, cached := tr.GetRunnerForTunnel(tunnelID)
	assert.True(t, cached, "the resolved tunnel must be registered for later frames")
	assert.Equal(t, runnerID, gotRunner)
	assert.Equal(t, 1, store.getCount(), "the store should be consulted once, not per send")
}

// TestTunnelRouter_RestartFallback_RejectsUnusableTunnels checks the fallback
// does not resurrect tunnels the manager considers dead, and reports a tunnel
// with no runner as such.
func TestTunnelRouter_RestartFallback_RejectsUnusableTunnels(t *testing.T) {
	closedAt := time.Now().Add(-time.Minute)

	closed := storedTunnel("tun_closed", "run_a", time.Hour)
	closed.ClosedAt = &closedAt
	expired := storedTunnel("tun_expired", "run_b", -time.Minute)
	orphan := storedTunnel("tun_orphan", "", time.Hour)

	tests := []struct {
		name     string
		tunnelID string
		wantErr  error
	}{
		{"closed", closed.ID, tunnel.ErrTunnelClosed},
		{"expired", expired.ID, tunnel.ErrTunnelExpired},
		{"no runner attached", orphan.ID, tunnel.ErrRunnerNotConnected},
		{"absent from the store", "tun_never_existed", tunnel.ErrTunnelNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newRestartTunnelStore(closed, expired, orphan)
			tr, cm := restartedRouter(t, store)

			_, err := tr.SendRequest(context.Background(), tt.tunnelID, "conn_x", []byte("hi"))
			require.Error(t, err)
			assert.ErrorIs(t, err, tt.wantErr)

			assert.Empty(t, cm.GetCommands("run_a"))
			assert.Empty(t, cm.GetCommands("run_b"))
			_, cached := tr.GetRunnerForTunnel(tt.tunnelID)
			assert.False(t, cached, "an unusable tunnel must not be registered")
		})
	}
}

// TestTunnelRouter_HandleCloseTunnel_DoesNotReregister covers the ordering in
// HandleCloseTunnel: with a store fallback in place, unregistering before the
// manager marks the tunnel closed leaves a window where a concurrent send
// re-registers the entry being removed.
func TestTunnelRouter_HandleCloseTunnel_DoesNotReregister(t *testing.T) {
	const (
		tunnelID = "tun_closing"
		runnerID = "run_closing"
	)

	store := newRestartTunnelStore(storedTunnel(tunnelID, runnerID, time.Hour))
	tr, _ := restartedRouter(t, store)

	// Warm both caches the way a live tunnel would be.
	tr.RegisterTunnel(tunnelID, runnerID)
	_, err := tr.tm.Get(context.Background(), tunnelID)
	require.NoError(t, err)

	require.NoError(t, tr.HandleCloseTunnel(context.Background(), runnerID, tunnelID, "runner closed it"))

	_, cached := tr.GetRunnerForTunnel(tunnelID)
	require.False(t, cached, "the registration must be gone after a close")

	// The store row is closed too, so the fallback refuses to bring it back.
	err = tr.SendData(context.Background(), tunnelID, "conn_late", []byte("late frame"), false)
	require.Error(t, err)
	assert.ErrorIs(t, err, tunnel.ErrTunnelClosed)
}

// closeConnForTest tears a router-owned connection down and waits for its pump.
func closeConnForTest(t *testing.T, tr *TunnelRouter, connectionID string) {
	t.Helper()

	tr.connectionsMu.RLock()
	conn, ok := tr.connections[connectionID]
	tr.connectionsMu.RUnlock()
	require.True(t, ok, "connection %s should be registered", connectionID)

	t.Cleanup(func() {
		tr.CloseConnection(connectionID)
		select {
		case <-conn.pumpDone:
		case <-time.After(5 * time.Second):
			t.Error("pump did not exit before the test ended")
		}
	})
}
