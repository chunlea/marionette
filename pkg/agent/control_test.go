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
	"google.golang.org/grpc/credentials/insecure"
)

func TestControlChannel_Start(t *testing.T) {
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

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = client.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)
	control := NewControlChannel(client, handler, logger)

	err = control.Start(ctx)
	require.NoError(t, err)

	// Give control channel time to establish
	time.Sleep(100 * time.Millisecond)

	// Verify stream is available
	assert.NotNil(t, control.Stream())

	control.Stop()
}

func TestControlChannel_NotConnected(t *testing.T) {
	cfg := &Config{
		Server:  ServerConfig{Address: "localhost:59999"},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	// Don't connect client
	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)
	control := NewControlChannel(client, handler, logger)

	ctx := context.Background()
	err := control.Start(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "client not connected")
}

func TestControlChannel_Send(t *testing.T) {
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

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = client.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)
	control := NewControlChannel(client, handler, logger)

	err = control.Start(ctx)
	require.NoError(t, err)

	// Send a message
	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				RunnerId: "test",
				Status:   "idle",
			},
		},
	}
	control.Send(msg)

	// Give time for message to be sent
	time.Sleep(100 * time.Millisecond)

	control.Stop()
}

func TestControlChannel_StopAsync(t *testing.T) {
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

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx := context.Background()
	err = client.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	workspace := NewWorkspaceManager(t.TempDir(), logger)
	handler := NewDefaultCommandHandler(workspace, logger)
	control := NewControlChannel(client, handler, logger)

	err = control.Start(ctx)
	require.NoError(t, err)

	// StopAsync should return immediately
	control.StopAsync()

	// Wait should block until stopped
	done := make(chan struct{})
	go func() {
		control.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after StopAsync")
	}
}

func TestCommandType(t *testing.T) {
	tests := []struct {
		cmd      *pb.ServerCommand
		expected string
	}{
		{
			cmd:      &pb.ServerCommand{Payload: &pb.ServerCommand_ExecuteTask{}},
			expected: "ExecuteTask",
		},
		{
			cmd:      &pb.ServerCommand{Payload: &pb.ServerCommand_ApprovePermission{}},
			expected: "ApprovePermission",
		},
		{
			cmd:      &pb.ServerCommand{Payload: &pb.ServerCommand_KillTask{}},
			expected: "KillTask",
		},
		{
			cmd:      &pb.ServerCommand{Payload: &pb.ServerCommand_CreateTunnel{}},
			expected: "CreateTunnel",
		},
		{
			cmd:      &pb.ServerCommand{Payload: &pb.ServerCommand_TunnelData{}},
			expected: "TunnelData",
		},
		{
			cmd:      &pb.ServerCommand{Payload: &pb.ServerCommand_AttachSession{}},
			expected: "AttachSession",
		},
		{
			cmd:      &pb.ServerCommand{Payload: &pb.ServerCommand_DetachSession{}},
			expected: "DetachSession",
		},
		{
			cmd:      &pb.ServerCommand{},
			expected: "Unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, commandType(tt.cmd))
		})
	}
}

// MockCommandHandler is a test helper for command handling
type MockCommandHandler struct {
	mu                     sync.Mutex
	ExecuteTaskCalls       []*pb.ExecuteTask
	ApprovePermissionCalls []*pb.ApprovePermission
	KillTaskCalls          []*pb.KillTask
	CreateTunnelCalls      []*pb.CreateTunnel
	TunnelDataCalls        []*pb.TunnelData
	AttachSessionCalls     []*pb.AttachSession
	DetachSessionCalls     []*pb.DetachSession
}

func (m *MockCommandHandler) HandleExecuteTask(_ context.Context, cmd *pb.ExecuteTask) (*pb.RunnerMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ExecuteTaskCalls = append(m.ExecuteTaskCalls, cmd)
	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskAccepted{
			TaskAccepted: &pb.TaskAccepted{
				TaskId:  cmd.TaskId,
				RunId:   cmd.RunId,
				Attempt: cmd.Attempt,
			},
		},
	}, nil
}

func (m *MockCommandHandler) HandleApprovePermission(_ context.Context, cmd *pb.ApprovePermission) (*pb.RunnerMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ApprovePermissionCalls = append(m.ApprovePermissionCalls, cmd)
	return nil, nil
}

func (m *MockCommandHandler) HandleKillTask(_ context.Context, cmd *pb.KillTask) (*pb.RunnerMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.KillTaskCalls = append(m.KillTaskCalls, cmd)
	return nil, nil
}

func (m *MockCommandHandler) HandleCreateTunnel(_ context.Context, cmd *pb.CreateTunnel) (*pb.RunnerMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CreateTunnelCalls = append(m.CreateTunnelCalls, cmd)
	return nil, nil
}

func (m *MockCommandHandler) HandleTunnelData(_ context.Context, cmd *pb.TunnelData) (*pb.RunnerMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TunnelDataCalls = append(m.TunnelDataCalls, cmd)
	return nil, nil
}

func (m *MockCommandHandler) HandleAttachSession(_ context.Context, cmd *pb.AttachSession) (*pb.RunnerMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.AttachSessionCalls = append(m.AttachSessionCalls, cmd)
	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_SessionAttached{
			SessionAttached: &pb.SessionAttached{
				SessionId: cmd.SessionId,
				Restored:  len(cmd.ContextSnapshot) > 0,
			},
		},
	}, nil
}

func (m *MockCommandHandler) HandleDetachSession(_ context.Context, cmd *pb.DetachSession) (*pb.RunnerMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DetachSessionCalls = append(m.DetachSessionCalls, cmd)
	return &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_SessionSuspended{
			SessionSuspended: &pb.SessionSuspended{
				SessionId: cmd.SessionId,
				Success:   true,
			},
		},
	}, nil
}

func TestControlChannel_ReceiveCommands(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)

	// Set up mock to send commands when Connect is called
	var sentCommands bool
	server.ConnectFunc = func(stream pb.RunnerService_ConnectServer) error {
		if !sentCommands {
			sentCommands = true
			// Send an AttachSession command
			err := stream.Send(&pb.ServerCommand{
				Payload: &pb.ServerCommand_AttachSession{
					AttachSession: &pb.AttachSession{
						SessionId:     "sess_test",
						WorkspacePath: t.TempDir(),
					},
				},
			})
			if err != nil {
				return err
			}
		}

		// Keep connection open briefly
		time.Sleep(200 * time.Millisecond)
		return nil
	}

	server.Start()
	defer server.Stop()

	cfg := &Config{
		Server:  ServerConfig{Address: server.Addr()},
		Runner:  RunnerConfig{Token: "token", Name: "test"},
		Sandbox: SandboxConfig{Mode: "runner-is-sandbox"},
		Logging: LoggingConfig{Level: "info", Format: "json"},
	}

	logger := zaptest.NewLogger(t)
	client := NewClient(cfg, logger)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err = client.Connect(ctx)
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	mockHandler := &MockCommandHandler{}
	control := NewControlChannel(client, mockHandler, logger)

	err = control.Start(ctx)
	require.NoError(t, err)

	// Wait for command to be processed
	time.Sleep(300 * time.Millisecond)

	control.Stop()

	// Verify command was received
	mockHandler.mu.Lock()
	defer mockHandler.mu.Unlock()
	assert.Len(t, mockHandler.AttachSessionCalls, 1)
	if len(mockHandler.AttachSessionCalls) > 0 {
		assert.Equal(t, "sess_test", mockHandler.AttachSessionCalls[0].SessionId)
	}
}

func TestMockServer_SendCommand(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	conn, err := grpc.NewClient(server.Addr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := pb.NewRunnerServiceClient(conn)

	ctx := context.Background()
	stream, err := client.Connect(ctx)
	require.NoError(t, err)

	// Send a heartbeat
	err = stream.Send(&pb.RunnerMessage{
		Payload: &pb.RunnerMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				RunnerId: "test",
				Status:   "idle",
			},
		},
	})
	require.NoError(t, err)

	// Close send
	err = stream.CloseSend()
	require.NoError(t, err)

	// Wait for stream to close
	for {
		_, err = stream.Recv()
		if err != nil {
			break
		}
	}

	// Verify heartbeat was counted
	assert.Equal(t, 1, server.GetHeartbeatCount())
}
