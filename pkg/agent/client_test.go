package agent

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestClient_Connect_Success(t *testing.T) {
	// Start mock server
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	// Create client
	cfg := &Config{
		Server: ServerConfig{
			Address: server.Addr(),
		},
		Runner: RunnerConfig{
			Token: "rtok_test",
			Name:  "test-runner",
		},
		Sandbox: SandboxConfig{
			Mode: "runner-is-sandbox",
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	// Initial state should be disconnected
	assert.Equal(t, StateDisconnected, client.State())

	// Connect - use longer timeout for CI environments
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = client.Connect(ctx)
	require.NoError(t, err)

	assert.Equal(t, StateConnected, client.State())
	assert.Equal(t, "run_mock_test-runner", client.RunnerID())

	// Verify registration was called
	calls := server.GetRegisterCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "test-runner", calls[0].Name)
	assert.Equal(t, "rtok_test", calls[0].Token)
	assert.Equal(t, "runner-is-sandbox", calls[0].SandboxMode)

	// Cleanup
	require.NoError(t, client.Close())
	assert.Equal(t, StateStopped, client.State())
}

func TestClient_Connect_Rejected(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	// Configure server to reject registration
	server.RegisterFunc = func(_ *pb.RegisterRunnerRequest) (*pb.RegisterRunnerResponse, error) {
		return &pb.RegisterRunnerResponse{
			Accepted: false,
			Message:  "invalid token",
		}, nil
	}

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "bad-token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	err = client.Connect(ctx)
	require.Error(t, err)

	var rejErr *ErrRegistrationRejected
	if assert.ErrorAs(t, err, &rejErr) {
		assert.Contains(t, rejErr.Message, "invalid token")
	}
	assert.Equal(t, StateDisconnected, client.State())
}

func TestClient_Connect_ServerUnavailable(t *testing.T) {
	cfg := &Config{
		Server:  ServerConfig{Address: "localhost:59999"}, // Unlikely to be listening
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	require.Error(t, err)

	// With grpc.NewClient (lazy connection), connection errors may appear
	// during RPC rather than dial, so we just verify an error occurred
	// and contains connection-related information
	assert.Contains(t, err.Error(), "connection")
	assert.Equal(t, StateDisconnected, client.State())
}

func TestClient_Connect_AlreadyConnected(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	ctx := context.Background()

	// First connect
	err = client.Connect(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateConnected, client.State())

	// Second connect should fail
	err = client.Connect(ctx)
	assert.ErrorIs(t, err, ErrAlreadyConnected)

	require.NoError(t, client.Close())
}

func TestClient_Connect_AfterClose(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	ctx := context.Background()

	// Connect then close
	err = client.Connect(ctx)
	require.NoError(t, err)
	require.NoError(t, client.Close())
	assert.Equal(t, StateStopped, client.State())

	// Try to connect again after close
	err = client.Connect(ctx)
	assert.ErrorIs(t, err, ErrShuttingDown)
}

func TestClient_StateTransitions(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	// Collect state changes with proper synchronization
	var mu sync.Mutex
	states := make([]ConnState, 0)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for state := range client.StateC() {
			mu.Lock()
			states = append(states, state)
			mu.Unlock()
		}
	}()

	// Initial state
	assert.Equal(t, StateDisconnected, client.State())

	// Connect
	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)
	assert.Equal(t, StateConnected, client.State())

	// Close
	err = client.Close()
	require.NoError(t, err)
	assert.Equal(t, StateStopped, client.State())

	// Allow time for state channel to be read
	time.Sleep(50 * time.Millisecond)

	// Check collected states with mutex protection
	mu.Lock()
	stateCount := len(states)
	mu.Unlock()

	// Should have transitioned through: connecting -> registering -> connected -> stopped
	assert.GreaterOrEqual(t, stateCount, 2)
}

func TestClient_WithLabels(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server: ServerConfig{Address: server.Addr()},
		Runner: RunnerConfig{
			Token: "token",
			Name:  "test",
			Labels: map[string]string{
				"env":    "test",
				"region": "us-west-2",
			},
			Annotations: map[string]string{
				"version": "1.0.0",
			},
		},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	// Verify labels and annotations were sent
	calls := server.GetRegisterCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, map[string]string{"env": "test", "region": "us-west-2"}, calls[0].Labels)
	assert.Equal(t, map[string]string{"version": "1.0.0"}, calls[0].Annotations)

	require.NoError(t, client.Close())
}

func TestClient_WithPool(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server: ServerConfig{Address: server.Addr()},
		Runner: RunnerConfig{
			Token:    "token",
			Name:     "test",
			PoolName: "macos-pool",
		},
		Sandbox: SandboxConfig{Mode: "runner-creates-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	calls := server.GetRegisterCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, "macos-pool", calls[0].PoolName)
	assert.Equal(t, "runner-creates-sandbox", calls[0].SandboxMode)

	require.NoError(t, client.Close())
}

func TestConnState_String(t *testing.T) {
	tests := []struct {
		state ConnState
		want  string
	}{
		{StateDisconnected, "disconnected"},
		{StateConnecting, "connecting"},
		{StateRegistering, "registering"},
		{StateConnected, "connected"},
		{StateStopped, "stopped"},
		{ConnState(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.state.String())
		})
	}
}

func TestClient_GRPCClient(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	// Before connect, GRPCClient should be nil
	assert.Nil(t, client.GRPCClient())

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)

	// After connect, GRPCClient should not be nil
	assert.NotNil(t, client.GRPCClient())

	require.NoError(t, client.Close())

	// After close, GRPCClient should be nil
	assert.Nil(t, client.GRPCClient())
}

func TestClient_TLS_MissingCertFile(t *testing.T) {
	cfg := &Config{
		Server:  ServerConfig{Address: "localhost:9090"},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		TLS: TLSConfig{
			Enabled:  true,
			CertFile: "/nonexistent/cert.pem",
			KeyFile:  "/nonexistent/key.pem",
		},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading TLS")
}

func TestClient_TLS_MissingCAFile(t *testing.T) {
	cfg := &Config{
		Server:  ServerConfig{Address: "localhost:9090"},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		TLS: TLSConfig{
			Enabled: true,
			CAFile:  "/nonexistent/ca.pem",
		},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading CA")
}

func TestClient_TLS_InvalidCAFile(t *testing.T) {
	// Create a temporary file with invalid CA content
	tmpDir := t.TempDir()
	caFile := filepath.Join(tmpDir, "invalid-ca.pem")
	err := os.WriteFile(caFile, []byte("not a valid certificate"), 0600)
	require.NoError(t, err)

	cfg := &Config{
		Server:  ServerConfig{Address: "localhost:9090"},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		TLS: TLSConfig{
			Enabled: true,
			CAFile:  caFile,
		},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	err = client.Connect(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "append CA")
}

func TestClient_TLS_SkipVerify(t *testing.T) {
	cfg := &Config{
		Server:  ServerConfig{Address: "localhost:59999"},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
		TLS: TLSConfig{
			Enabled:    true,
			SkipVerify: true,
		},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	// This will fail to connect but should not fail on TLS config
	err := client.Connect(ctx)
	require.Error(t, err)
	// Error should be connection-related, not TLS config
	assert.Contains(t, err.Error(), "connection")
}

func TestClient_CloseNotConnected(t *testing.T) {
	cfg := &Config{
		Server:  ServerConfig{Address: "localhost:9090"},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	client := NewClient(cfg, zaptest.NewLogger(t))

	// Close without connecting should not error
	err := client.Close()
	require.NoError(t, err)
	assert.Equal(t, StateStopped, client.State())
}
