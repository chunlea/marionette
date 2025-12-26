package agent

import (
	"context"
	"testing"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestMockServer_GetRunnerStatus(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	// Connect to the server
	conn, err := grpc.NewClient(server.Addr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := pb.NewRunnerServiceClient(conn)

	// Test default behavior
	ctx := context.Background()
	resp, err := client.GetRunnerStatus(ctx, &pb.GetRunnerStatusRequest{RunnerId: "run_test123"})
	require.NoError(t, err)
	assert.Equal(t, "run_test123", resp.RunnerId)
	assert.Equal(t, "idle", resp.Status)
}

func TestMockServer_GetRunnerStatus_CustomFunc(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)

	// Configure custom behavior
	server.GetStatusFunc = func(req *pb.GetRunnerStatusRequest) (*pb.RunnerStatus, error) {
		return &pb.RunnerStatus{
			RunnerId: req.RunnerId,
			Status:   "busy",
		}, nil
	}

	server.Start()
	defer server.Stop()

	conn, err := grpc.NewClient(server.Addr(),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	client := pb.NewRunnerServiceClient(conn)

	ctx := context.Background()
	resp, err := client.GetRunnerStatus(ctx, &pb.GetRunnerStatusRequest{RunnerId: "run_test123"})
	require.NoError(t, err)
	assert.Equal(t, "busy", resp.Status)
}

func TestMockServer_Connect(t *testing.T) {
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

	// Send some heartbeats
	for i := 0; i < 3; i++ {
		err = stream.Send(&pb.RunnerMessage{
			Payload: &pb.RunnerMessage_Heartbeat{
				Heartbeat: &pb.Heartbeat{
					RunnerId: "run_test",
					Status:   "idle",
				},
			},
		})
		require.NoError(t, err)
	}

	// Close the send side
	err = stream.CloseSend()
	require.NoError(t, err)

	// Wait for server to process all messages by reading until EOF
	// (server doesn't send any commands in default mode, so this will just wait for stream end)
	for {
		_, err = stream.Recv()
		if err != nil {
			break
		}
	}

	// Heartbeat count should be 3
	assert.Equal(t, 3, server.GetHeartbeatCount())
}

func TestMockServer_StreamLogs(t *testing.T) {
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
	stream, err := client.StreamLogs(ctx)
	require.NoError(t, err)

	// Send init message first
	err = stream.Send(&pb.StreamLogsMessage{
		Payload: &pb.StreamLogsMessage_Init{
			Init: &pb.StreamLogsInit{
				SessionId: "sess_test",
				RunnerId:  "run_test",
			},
		},
	})
	require.NoError(t, err)

	// Send some log entries
	for i := 0; i < 5; i++ {
		err = stream.Send(&pb.StreamLogsMessage{
			Payload: &pb.StreamLogsMessage_LogEntry{
				LogEntry: &pb.LogEntry{
					RunId:   "trun_test",
					Stream:  "stdout",
					Content: "test log",
				},
			},
		})
		require.NoError(t, err)
	}

	// Close and get response
	resp, err := stream.CloseAndRecv()
	require.NoError(t, err)
	assert.Equal(t, int64(6), resp.LogsReceived) // 1 init + 5 logs
}

func TestMockServer_Reset(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	// Simulate some calls
	server.mu.Lock()
	server.RegisterCalls = append(server.RegisterCalls, &pb.RegisterRunnerRequest{Name: "test"})
	server.HeartbeatCount = 5
	server.mu.Unlock()

	// Reset should clear state
	server.Reset()

	assert.Empty(t, server.GetRegisterCalls())
	assert.Equal(t, 0, server.GetHeartbeatCount())
}

func TestMockServer_GetHeartbeatCount(t *testing.T) {
	server, err := NewMockServer()
	require.NoError(t, err)
	server.Start()
	defer server.Stop()

	// Initial count should be 0
	assert.Equal(t, 0, server.GetHeartbeatCount())

	// Simulate heartbeats
	server.mu.Lock()
	server.HeartbeatCount = 10
	server.mu.Unlock()

	assert.Equal(t, 10, server.GetHeartbeatCount())
}
