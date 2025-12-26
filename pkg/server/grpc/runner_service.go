// Package grpc provides the gRPC server for runner communication.
package grpc

import (
	"context"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// RunnerManagerInterface defines the interface for runner lifecycle management.
// This interface is implemented by core.RunnerManager.
type RunnerManagerInterface interface {
	// OnConnect is called when a runner connects.
	OnConnect(ctx context.Context, runnerID string) error
	// OnDisconnect is called when a runner disconnects.
	OnDisconnect(ctx context.Context, runnerID string) error
	// OnHeartbeat is called when a heartbeat is received from a runner.
	OnHeartbeat(ctx context.Context, runnerID string, hb *pb.Heartbeat) error
}

// MessageRouterInterface defines the interface for routing runner messages.
type MessageRouterInterface interface {
	// HandleMessage routes a message from a runner to the appropriate handler.
	HandleMessage(ctx context.Context, runnerID string, msg *pb.RunnerMessage) error
}

// RunnerService implements the RunnerServiceServer interface.
type RunnerService struct {
	pb.UnimplementedRunnerServiceServer
	logger        *zap.Logger
	store         store.Store
	tokenSvc      *auth.RunnerTokenService
	connManager   *ConnectionManager
	runnerManager RunnerManagerInterface
	router        MessageRouterInterface
}

// RunnerServiceOption is a functional option for RunnerService.
type RunnerServiceOption func(*RunnerService)

// NewRunnerService creates a new RunnerService with the given options.
func NewRunnerService(logger *zap.Logger, opts ...RunnerServiceOption) *RunnerService {
	svc := &RunnerService{
		logger: logger,
	}
	for _, opt := range opts {
		opt(svc)
	}
	return svc
}

// WithStore sets the store for the RunnerService.
func WithStore(s store.Store) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.store = s
	}
}

// WithTokenService sets the token service for the RunnerService.
func WithTokenService(ts *auth.RunnerTokenService) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.tokenSvc = ts
	}
}

// WithConnectionManager sets the connection manager for the RunnerService.
func WithConnectionManager(cm *ConnectionManager) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.connManager = cm
	}
}

// WithRunnerManager sets the runner manager for the RunnerService.
func WithRunnerManager(rm RunnerManagerInterface) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.runnerManager = rm
	}
}

// WithRouter sets the message router for the RunnerService.
func WithRouter(r MessageRouterInterface) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.router = r
	}
}

// RegisterRunner handles runner registration.
// This is a stub that returns a mock response.
func (s *RunnerService) RegisterRunner(_ context.Context, req *pb.RegisterRunnerRequest) (*pb.RegisterRunnerResponse, error) {
	s.logger.Info("RegisterRunner called (stub)",
		zap.String("name", req.Name),
		zap.String("hostname", req.Hostname),
	)

	// Stub implementation - actual implementation will come in G2
	return &pb.RegisterRunnerResponse{
		RunnerId: "run_stub",
		Accepted: true,
		Message:  "stub: registration accepted",
	}, nil
}

// GetRunnerStatus returns the status of a runner.
// This is a stub that returns a mock response.
func (s *RunnerService) GetRunnerStatus(_ context.Context, req *pb.GetRunnerStatusRequest) (*pb.RunnerStatus, error) {
	s.logger.Info("GetRunnerStatus called (stub)",
		zap.String("runner_id", req.RunnerId),
	)

	// Stub implementation - actual implementation will come in G3
	return &pb.RunnerStatus{
		RunnerId: req.RunnerId,
		Status:   "unknown",
	}, nil
}

// createConnection creates a new RunnerConnection from a runner and stream.
func (s *RunnerService) createConnection(runner *store.Runner, stream grpc.BidiStreamingServer[pb.RunnerMessage, pb.ServerCommand]) *RunnerConnection {
	return &RunnerConnection{
		RunnerID:    runner.ID,
		Name:        runner.Name,
		Hostname:    runner.Hostname,
		Status:      RunnerStatusIdle,
		ConnectedAt: time.Now(),
		LastSeen:    time.Now(),
		commandCh:   make(chan *pb.ServerCommand, commandBufferSize),
		stream:      stream,
	}
}

// StreamLogs handles the log upload stream.
// This is a stub that counts messages and returns the count.
func (s *RunnerService) StreamLogs(stream grpc.ClientStreamingServer[pb.StreamLogsMessage, pb.StreamLogsResponse]) error {
	s.logger.Info("StreamLogs stream opened (stub)")
	var count int64
	for {
		_, err := stream.Recv()
		if err != nil {
			s.logger.Info("StreamLogs stream closed", zap.Int64("logs_received", count), zap.Error(err))
			return stream.SendAndClose(&pb.StreamLogsResponse{
				LogsReceived: count,
				LogsStored:   count,
				LogsDropped:  0,
			})
		}
		count++
	}
}
