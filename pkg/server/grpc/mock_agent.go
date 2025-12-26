package grpc

import (
	"context"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// MockRunnerClient simulates a runner connecting to the server.
// It is used for testing gRPC interactions.
type MockRunnerClient struct {
	conn   *grpc.ClientConn
	client pb.RunnerServiceClient
}

// NewMockRunnerClient creates a new mock runner client.
func NewMockRunnerClient(addr string, opts ...grpc.DialOption) (*MockRunnerClient, error) {
	// Default to insecure connection for testing
	if len(opts) == 0 {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	conn, err := grpc.NewClient(addr, opts...)
	if err != nil {
		return nil, err
	}

	return &MockRunnerClient{
		conn:   conn,
		client: pb.NewRunnerServiceClient(conn),
	}, nil
}

// Register registers the mock runner with the server.
func (m *MockRunnerClient) Register(ctx context.Context, req *pb.RegisterRunnerRequest) (*pb.RegisterRunnerResponse, error) {
	return m.client.RegisterRunner(ctx, req)
}

// GetStatus retrieves the runner status from the server.
func (m *MockRunnerClient) GetStatus(ctx context.Context, req *pb.GetRunnerStatusRequest) (*pb.RunnerStatus, error) {
	return m.client.GetRunnerStatus(ctx, req)
}

// Connect establishes a bidirectional control stream.
func (m *MockRunnerClient) Connect(ctx context.Context) (pb.RunnerService_ConnectClient, error) {
	return m.client.Connect(ctx)
}

// StreamLogs establishes a log streaming connection.
func (m *MockRunnerClient) StreamLogs(ctx context.Context) (pb.RunnerService_StreamLogsClient, error) {
	return m.client.StreamLogs(ctx)
}

// Close closes the client connection.
func (m *MockRunnerClient) Close() error {
	return m.conn.Close()
}
