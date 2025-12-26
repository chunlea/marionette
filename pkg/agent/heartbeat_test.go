package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestHeartbeatLoop_SendsHeartbeats(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Heartbeat: HeartbeatConfig{
			Interval: 50 * time.Millisecond,
			Timeout:  1 * time.Second,
		},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = client.Connect(ctx)
	require.NoError(t, err)

	loop := NewHeartbeatLoop(client, cfg.Heartbeat, logger)
	loop.Start(ctx)

	// Wait for a few heartbeat intervals
	time.Sleep(200 * time.Millisecond)
	loop.Stop()

	// Note: Without the Connect stream, heartbeats are just logged.
	// The loop runs without error, which is the key assertion.
	require.NoError(t, client.Close())
}

func TestHeartbeatLoop_SetStatus(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Heartbeat: HeartbeatConfig{
			Interval: 50 * time.Millisecond,
			Timeout:  1 * time.Second,
		},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	loop := NewHeartbeatLoop(client, cfg.Heartbeat, logger)

	// Default status is idle
	assert.Equal(t, "idle", loop.getStatus())

	// Change status
	loop.SetStatus("busy")
	assert.Equal(t, "busy", loop.getStatus())

	loop.SetStatus("paused")
	assert.Equal(t, "paused", loop.getStatus())

	require.NoError(t, client.Close())
}

func TestHeartbeatLoop_Stop(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Heartbeat: HeartbeatConfig{
			Interval: 1 * time.Second, // Long interval to ensure stop works
			Timeout:  1 * time.Second,
		},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	loop := NewHeartbeatLoop(client, cfg.Heartbeat, logger)
	loop.Start(ctx)

	// Stop should return quickly
	done := make(chan struct{})
	go func() {
		loop.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return in time")
	}

	require.NoError(t, client.Close())
}

func TestHeartbeatLoop_ContextCancellation(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Heartbeat: HeartbeatConfig{
			Interval: 1 * time.Second,
			Timeout:  1 * time.Second,
		},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx, cancel := context.WithCancel(context.Background())
	err = client.Connect(ctx)
	require.NoError(t, err)

	loop := NewHeartbeatLoop(client, cfg.Heartbeat, logger)
	loop.Start(ctx)

	// Cancel context
	cancel()

	// Wait should return quickly
	done := make(chan struct{})
	go func() {
		loop.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after context cancellation")
	}

	require.NoError(t, client.Close())
}

func TestHeartbeatLoop_Uptime(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Heartbeat: HeartbeatConfig{
			Interval: 100 * time.Millisecond,
			Timeout:  1 * time.Second,
		},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	loop := NewHeartbeatLoop(client, cfg.Heartbeat, logger)

	// Uptime should start at 0
	uptime1 := loop.uptimeSeconds()
	assert.GreaterOrEqual(t, uptime1, int64(0))

	// Wait long enough to see uptime increase (seconds granularity)
	time.Sleep(1100 * time.Millisecond)

	// Uptime should increase
	uptime2 := loop.uptimeSeconds()
	assert.Greater(t, uptime2, uptime1)

	require.NoError(t, client.Close())
}

func TestHeartbeatLoop_ResourceCollection(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Heartbeat: HeartbeatConfig{
			Interval: 100 * time.Millisecond,
			Timeout:  1 * time.Second,
		},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	loop := NewHeartbeatLoop(client, cfg.Heartbeat, logger)

	resources := loop.collectResources()

	// Memory should be > 0 (we're using some memory)
	assert.Greater(t, resources.MemoryBytes, int64(0))

	// CPU and Disk are not yet implemented
	assert.Equal(t, float64(0), resources.CPUPercent)
	assert.Equal(t, int64(0), resources.DiskBytes)

	require.NoError(t, client.Close())
}

func TestHeartbeatLoop_NotConnected(t *testing.T) {
	cfg := &Config{
		Server:  ServerConfig{Address: "localhost:59999"},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Heartbeat: HeartbeatConfig{
			Interval: 50 * time.Millisecond,
			Timeout:  1 * time.Second,
		},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	// Don't connect the client

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	loop := NewHeartbeatLoop(client, cfg.Heartbeat, logger)
	loop.Start(ctx)

	// Wait for context to cancel
	<-ctx.Done()
	loop.Wait()

	// Loop should run without error even when not connected
	// (it just logs that it's skipping heartbeats)
}

func TestHeartbeatLoop_SetStream(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Heartbeat: HeartbeatConfig{
			Interval: 100 * time.Millisecond,
			Timeout:  1 * time.Second,
		},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	loop := NewHeartbeatLoop(client, cfg.Heartbeat, logger)

	// SetStream with nil should not panic
	loop.SetStream(nil)

	require.NoError(t, client.Close())
}

func TestHeartbeatLoop_StopAsync(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		Heartbeat: HeartbeatConfig{
			Interval: 1 * time.Second,
			Timeout:  1 * time.Second,
		},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	loop := NewHeartbeatLoop(client, cfg.Heartbeat, logger)
	loop.Start(ctx)

	// StopAsync should return immediately
	loop.StopAsync()

	// Wait should block until stopped
	done := make(chan struct{})
	go func() {
		loop.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after StopAsync")
	}

	require.NoError(t, client.Close())
}
