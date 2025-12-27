package agent

import (
	"context"
	"io"
	"net"
	"sync"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"google.golang.org/grpc"
)

// MockServer is a mock gRPC server for testing the agent.
type MockServer struct {
	pb.UnimplementedRunnerServiceServer

	server   *grpc.Server
	listener net.Listener

	// Configurable behavior for registration.
	RegisterFunc func(req *pb.RegisterRunnerRequest) (*pb.RegisterRunnerResponse, error)

	// Configurable behavior for status.
	GetStatusFunc func(req *pb.GetRunnerStatusRequest) (*pb.RunnerStatus, error)

	// Configurable behavior for connect stream.
	ConnectFunc func(stream pb.RunnerService_ConnectServer) error

	// Recording of calls for assertions.
	mu             sync.Mutex
	RegisterCalls  []*pb.RegisterRunnerRequest
	HeartbeatCount int
	LogCount       int64
}

// NewMockServer creates a new mock server listening on a random port.
func NewMockServer() (*MockServer, error) {
	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return nil, err
	}

	s := &MockServer{
		listener:      lis,
		RegisterCalls: make([]*pb.RegisterRunnerRequest, 0),
	}

	s.server = grpc.NewServer()
	pb.RegisterRunnerServiceServer(s.server, s)

	return s, nil
}

// Start starts the mock server in a goroutine.
func (s *MockServer) Start() {
	go func() {
		_ = s.server.Serve(s.listener)
	}()
}

// Stop gracefully stops the mock server.
func (s *MockServer) Stop() {
	s.server.GracefulStop()
}

// Addr returns the server address (host:port).
func (s *MockServer) Addr() string {
	return s.listener.Addr().String()
}

// RegisterRunner implements the RegisterRunner RPC.
func (s *MockServer) RegisterRunner(_ context.Context, req *pb.RegisterRunnerRequest) (*pb.RegisterRunnerResponse, error) {
	s.mu.Lock()
	s.RegisterCalls = append(s.RegisterCalls, req)
	s.mu.Unlock()

	if s.RegisterFunc != nil {
		return s.RegisterFunc(req)
	}

	// Default behavior: accept and return a mock runner ID
	return &pb.RegisterRunnerResponse{
		RunnerId: "run_mock_" + req.Name,
		Accepted: true,
		Message:  "mock: registration accepted",
	}, nil
}

// GetRunnerStatus implements the GetRunnerStatus RPC.
func (s *MockServer) GetRunnerStatus(_ context.Context, req *pb.GetRunnerStatusRequest) (*pb.RunnerStatus, error) {
	if s.GetStatusFunc != nil {
		return s.GetStatusFunc(req)
	}

	return &pb.RunnerStatus{
		RunnerId: req.RunnerId,
		Status:   "idle",
	}, nil
}

// Connect implements the bidirectional Connect RPC.
func (s *MockServer) Connect(stream pb.RunnerService_ConnectServer) error {
	if s.ConnectFunc != nil {
		return s.ConnectFunc(stream)
	}

	// Default behavior: count heartbeats until stream ends
	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		if hb := msg.GetHeartbeat(); hb != nil {
			s.mu.Lock()
			s.HeartbeatCount++
			s.mu.Unlock()
		}
	}
}

// StreamLogs implements the StreamLogs RPC.
func (s *MockServer) StreamLogs(stream pb.RunnerService_StreamLogsServer) error {
	var count int64
	for {
		_, err := stream.Recv()
		if err == io.EOF {
			s.mu.Lock()
			s.LogCount += count
			s.mu.Unlock()
			return stream.SendAndClose(&pb.StreamLogsResponse{
				LogsReceived: count,
				LogsStored:   count,
			})
		}
		if err != nil {
			return err
		}
		count++
	}
}

// GetRegisterCalls returns a copy of all registration calls.
func (s *MockServer) GetRegisterCalls() []*pb.RegisterRunnerRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]*pb.RegisterRunnerRequest, len(s.RegisterCalls))
	copy(result, s.RegisterCalls)
	return result
}

// GetHeartbeatCount returns the number of heartbeats received.
func (s *MockServer) GetHeartbeatCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.HeartbeatCount
}

// Reset clears all recorded calls.
func (s *MockServer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.RegisterCalls = make([]*pb.RegisterRunnerRequest, 0)
	s.HeartbeatCount = 0
	s.LogCount = 0
}
