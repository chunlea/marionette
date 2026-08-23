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
)

// testClientConfig builds a client config pointed at addr.
func testClientConfig(addr string) *Config {
	return &Config{
		Server:  ServerConfig{Address: addr},
		Runner:  RunnerConfig{Token: "rtok_test", Name: "test-runner"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}
}

// TestControlChannel_StopBeforeStart pins two crashes: Stop closed stopC
// unconditionally, so a second call panicked, and it then waited on stoppedC,
// which nothing closes when run() never started.
func TestControlChannel_StopBeforeStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cc := NewControlChannel(NewClient(testClientConfig("127.0.0.1:1"), logger), nil, logger)

	done := make(chan struct{})
	go func() {
		defer close(done)
		cc.Stop()
		cc.Stop() // second call must not panic
		cc.StopAsync()
		cc.Wait()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop deadlocked when the control channel was never started")
	}
}

// TestControlChannel_StopAfterFailedStart covers the same path when Start ran
// but failed, which leaves no run() goroutine behind either.
func TestControlChannel_StopAfterFailedStart(t *testing.T) {
	logger := zaptest.NewLogger(t)
	cc := NewControlChannel(NewClient(testClientConfig("127.0.0.1:1"), logger), nil, logger)

	require.Error(t, cc.Start(context.Background()))

	done := make(chan struct{})
	go func() {
		defer close(done)
		cc.Stop()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop deadlocked after a failed Start")
	}
}

// TestClient_ConcurrentConnectionAccess exercises the fields that Connect and
// Close swap while the control channel, heartbeat loop and log streamer read
// them. Run with -race; before the mutex this was an unguarded write.
func TestClient_ConcurrentConnectionAccess(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	logger := zaptest.NewLogger(t)
	client := NewClient(testClientConfig(server.Addr()), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Readers: what every long-lived agent goroutine does.
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = client.GRPCClient()
				_ = client.RunnerID()
				_ = client.State()
				_ = client.AttachMetadata(context.Background())
			}
		}()
	}

	// Writer: reconnect churn.
	for range 3 {
		require.NoError(t, client.Connect(ctx))
		assert.Equal(t, StateConnected, client.State())
		assert.NotEmpty(t, client.RunnerID())

		require.NoError(t, client.Disconnect())
		assert.Equal(t, StateDisconnected, client.State())
		assert.Empty(t, client.RunnerID())
		assert.Nil(t, client.GRPCClient())
	}

	close(stop)
	wg.Wait()

	require.NoError(t, client.Close())
}

// TestClient_DisconnectAllowsReconnect is the property the supervisor loop
// depends on: Disconnect must leave the client reusable, unlike Close.
func TestClient_DisconnectAllowsReconnect(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	logger := zaptest.NewLogger(t)
	client := NewClient(testClientConfig(server.Addr()), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, client.Connect(ctx))
	first := client.GRPCClient()
	require.NotNil(t, first)

	require.NoError(t, client.Disconnect())
	require.NoError(t, client.Connect(ctx))

	second := client.GRPCClient()
	require.NotNil(t, second)
	assert.NotSame(t, first, second, "reconnect must hand out a fresh client")

	require.NoError(t, client.Close())

	// Close is terminal: reconnecting after it must be refused.
	assert.ErrorIs(t, client.Connect(ctx), ErrShuttingDown)
}

// TestClient_DialWaitsForReady pins the dial fix. WaitForStateChange returns
// after the first transition (IDLE -> CONNECTING), so the old readiness check
// returned before anything was connected and RegisterRunner was the thing that
// actually discovered the failure.
func TestClient_DialWaitsForReady(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	logger := zaptest.NewLogger(t)
	client := NewClient(testClientConfig(server.Addr()), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	conn, err := client.dial(ctx)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	assert.Equal(t, "READY", conn.GetState().String())
}

// TestClient_DialGivesUpWhenUnreachable makes sure the readiness loop still
// terminates instead of spinning to its 30s ceiling.
func TestClient_DialGivesUpWhenUnreachable(t *testing.T) {
	logger := zaptest.NewLogger(t)
	// Port 1 is not listening; gRPC will churn IDLE/CONNECTING/TRANSIENT_FAILURE.
	client := NewClient(testClientConfig("127.0.0.1:1"), logger)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	conn, err := client.dial(ctx)
	require.Error(t, err)
	assert.Nil(t, conn)
}

// TestControlChannel_StopInterruptsBlockedReceive pins that Stop returns
// promptly while the receive loop is parked in stream.Recv. Signalling stopC
// alone cannot interrupt a blocking Recv, so Stop used to hang until the
// connection happened to break on its own.
func TestControlChannel_StopInterruptsBlockedReceive(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)

	// Hold the stream open and never send anything.
	serverDone := make(chan struct{})
	server.ConnectFunc = func(stream pb.RunnerService_ConnectServer) error {
		defer close(serverDone)
		for {
			if _, err := stream.Recv(); err != nil {
				return nil
			}
		}
	}
	server.Start()
	defer server.Stop()

	logger := zaptest.NewLogger(t)
	client := NewClient(testClientConfig(server.Addr()), logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, client.Connect(ctx))
	defer func() { _ = client.Close() }()

	cc := NewControlChannel(client, NewDefaultCommandHandler(NewWorkspaceManager(t.TempDir(), logger), logger), logger)
	require.NoError(t, cc.Start(ctx))

	// Give the receive loop time to park inside Recv.
	time.Sleep(100 * time.Millisecond)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		cc.Stop()
	}()

	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not interrupt a receive blocked in stream.Recv")
	}
}

// TestControlChannel_ExitsWhenServerClosesStream is the other half of the
// bricked-agent bug: the receive loop returned on a clean EOF but the send
// loop kept running, so run() never finished, stoppedC never closed, and the
// supervisor waiting on Wait() hung behind a connection that was already gone.
func TestControlChannel_ExitsWhenServerClosesStream(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)

	// Close the control stream immediately, the way a restarting server does.
	server.ConnectFunc = func(pb.RunnerService_ConnectServer) error { return nil }
	server.Start()
	defer server.Stop()

	logger := zaptest.NewLogger(t)
	client := NewClient(testClientConfig(server.Addr()), logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	require.NoError(t, client.Connect(ctx))
	defer func() { _ = client.Close() }()

	cc := NewControlChannel(client, NewDefaultCommandHandler(NewWorkspaceManager(t.TempDir(), logger), logger), logger)
	require.NoError(t, cc.Start(ctx))

	waited := make(chan struct{})
	go func() {
		defer close(waited)
		cc.Wait()
	}()

	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		t.Fatal("control channel did not finish after the server closed the stream")
	}
}
