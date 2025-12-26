// Package grpc provides the gRPC server for runner communication.
package grpc

import (
	"context"

	"github.com/chunlea/marionette/pkg/auth"
	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// RunnerService implements the RunnerServiceServer interface.
type RunnerService struct {
	pb.UnimplementedRunnerServiceServer
	logger      *zap.Logger
	store       store.Store
	tokenSvc    *auth.RunnerTokenService
	connManager *ConnectionManager
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

	// Stub implementation - actual implementation will come in G2
	return &pb.RunnerStatus{
		RunnerId: req.RunnerId,
		Status:   "unknown",
	}, nil
}

// Connect handles the bidirectional stream for control messages.
// This is a stub that just waits for the stream to close.
func (s *RunnerService) Connect(stream grpc.BidiStreamingServer[pb.RunnerMessage, pb.ServerCommand]) error {
	s.logger.Info("Connect stream opened (stub)")
	// Stub - just wait for the stream to close
	for {
		_, err := stream.Recv()
		if err != nil {
			s.logger.Info("Connect stream closed", zap.Error(err))
			return nil
		}
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
