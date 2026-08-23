package main

import (
	"context"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/agent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

// testConfig builds an agent config pointed at addr with a brisk heartbeat so
// tests do not have to wait a production interval.
func testConfig(addr string) *agent.Config {
	return &agent.Config{
		Server:    agent.ServerConfig{Address: addr},
		Runner:    agent.RunnerConfig{Token: "rtok_test", Name: "test-runner"},
		Sandbox:   agent.SandboxConfig{Mode: "runner-is-sandbox"},
		Logging:   agent.LoggingConfig{Level: "info", Format: "json"},
		Heartbeat: agent.HeartbeatConfig{Interval: 20 * time.Millisecond},
		Workspace: agent.WorkspaceConfig{BasePath: "."},
	}
}

// fastBackoff keeps reconnect tests quick without removing the pacing itself.
func fastBackoff() *agent.ExponentialBackoff {
	return agent.NewExponentialBackoff(agent.BackoffConfig{
		InitialDelay: 5 * time.Millisecond,
		MaxDelay:     50 * time.Millisecond,
		Multiplier:   2.0,
		MaxRetries:   -1,
	})
}

// newTestDeps assembles the same wiring main() uses.
func newTestDeps(t *testing.T, addr string) (connectionDeps, *agent.Client) {
	t.Helper()

	logger := zaptest.NewLogger(t)
	cfg := testConfig(addr)
	client := agent.NewClient(cfg, logger)
	workspaceMgr := agent.NewWorkspaceManager(t.TempDir(), logger)

	return connectionDeps{
		client:      client,
		handler:     agent.NewDefaultCommandHandler(workspaceMgr, logger),
		sender:      newCurrentChannel(logger),
		hbLoop:      agent.NewHeartbeatLoop(client, cfg.Heartbeat, logger),
		logStreamer: agent.NewGRPCLogStreamer(nil, "", logger),
		logger:      logger,
		backoff:     fastBackoff(),
	}, client
}

// TestSuperviseConnection_ReconnectsAfterStreamClose is the regression test for
// the bug that bricked the agent: the control channel returned on any stream
// error, clean EOF included, and nothing restarted it. One server restart was
// enough to take the agent out permanently.
func TestSuperviseConnection_ReconnectsAfterStreamClose(t *testing.T) {
	server, err := agent.NewMockServer()
	require.NoError(t, err)

	var mu sync.Mutex
	connects := 0

	// Drop the first two control streams the way a restarting server would,
	// then hold the third open.
	server.ConnectFunc = func(stream pb.RunnerService_ConnectServer) error {
		mu.Lock()
		connects++
		drop := connects <= 2
		mu.Unlock()

		if drop {
			return nil // clean EOF
		}
		for {
			// Any receive error means the agent hung up; that ends the mock
			// handler cleanly rather than being a server-side failure.
			if _, err := stream.Recv(); err != nil {
				return nil //nolint:nilerr // intentional: client disconnect is not a server error
			}
		}
	}

	server.Start()
	defer server.Stop()

	deps, _ := newTestDeps(t, server.Addr())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		superviseConnection(ctx, deps)
	}()

	require.Eventually(t, func() bool {
		return len(server.GetRegisterCalls()) >= 3
	}, 20*time.Second, 20*time.Millisecond,
		"the agent must re-register after the server drops its control stream")

	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("superviseConnection did not return after context cancellation")
	}
}

// TestSuperviseConnection_AttachesHeartbeatStream covers the other silent
// failure: SetStream had no production caller, so every heartbeat the loop
// produced was discarded before it reached the wire.
func TestSuperviseConnection_AttachesHeartbeatStream(t *testing.T) {
	server, err := agent.NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	deps, client := newTestDeps(t, server.Addr())

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	deps.hbLoop.Start(ctx)
	defer deps.hbLoop.Stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		superviseConnection(ctx, deps)
	}()

	require.Eventually(t, func() bool {
		return server.GetHeartbeatCount() > 0
	}, 20*time.Second, 20*time.Millisecond,
		"heartbeats must reach the server once the control stream is attached")

	// The log streamer must be bound to the live connection too, not to a
	// handle captured before the connection existed.
	assert.NotEmpty(t, client.RunnerID())
	require.NoError(t, deps.logStreamer.Start(ctx, &pb.StreamLogsInit{
		SessionId: "sess_test",
		TaskId:    "task_test",
		RunId:     "trun_test",
	}))
	require.NoError(t, deps.logStreamer.Send(&pb.LogEntry{
		SessionId: "sess_test",
		TaskId:    "task_test",
		RunId:     "trun_test",
		Stream:    "stdout",
		Content:   "hello",
	}))
	_, err = deps.logStreamer.Close()
	require.NoError(t, err)

	cancel()
	<-done
}

// TestSuperviseConnection_RetriesUnreachableServer proves the supervisor keeps
// trying instead of exiting when the server is not up yet.
func TestSuperviseConnection_RetriesUnreachableServer(t *testing.T) {
	// Port 1 has nothing listening.
	deps, _ := newTestDeps(t, "127.0.0.1:1")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		superviseConnection(ctx, deps)
	}()

	select {
	case <-done:
		// Returned because ctx expired, which is the only legal exit.
		assert.Error(t, ctx.Err())
	case <-time.After(20 * time.Second):
		t.Fatal("superviseConnection never returned")
	}
}

// TestCurrentChannel_Send covers the indirection that lets long-lived
// components survive a control channel being replaced.
func TestCurrentChannel_Send(t *testing.T) {
	logger := zaptest.NewLogger(t)
	sender := newCurrentChannel(logger)

	// No connection: the message is dropped, not panicked on.
	assert.NotPanics(t, func() {
		sender.Send(&pb.RunnerMessage{})
	})

	server, err := agent.NewMockServer()
	require.NoError(t, err)

	received := make(chan struct{}, 1)
	server.ConnectFunc = func(stream pb.RunnerService_ConnectServer) error {
		for {
			msg, err := stream.Recv()
			if err != nil {
				// EOF or a transport error both mean the agent is done.
				return nil //nolint:nilerr // intentional: client disconnect is not a server error
			}
			if msg.GetTaskAccepted() != nil {
				select {
				case received <- struct{}{}:
				default:
				}
			}
		}
	}
	server.Start()
	defer server.Stop()

	cfg := testConfig(server.Addr())
	client := agent.NewClient(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	require.NoError(t, client.Connect(ctx))
	defer func() { _ = client.Close() }()

	workspaceMgr := agent.NewWorkspaceManager(t.TempDir(), logger)
	cc := agent.NewControlChannel(client, agent.NewDefaultCommandHandler(workspaceMgr, logger), logger)
	require.NoError(t, cc.Start(ctx))
	defer cc.Stop()

	sender.set(cc)
	sender.Send(&pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskAccepted{
			TaskAccepted: &pb.TaskAccepted{TaskId: "task_test"},
		},
	})

	select {
	case <-received:
	case <-time.After(10 * time.Second):
		t.Fatal("message never reached the server through the sender indirection")
	}

	// After the channel goes away the sender must degrade, not panic.
	sender.set(nil)
	assert.NotPanics(t, func() {
		sender.Send(&pb.RunnerMessage{})
	})
}
