// Package grpc provides the gRPC server for runner communication.
package grpc

import (
	"context"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// runnerService implements the RunnerServiceServer interface.
type runnerService struct {
	pb.UnimplementedRunnerServiceServer
	logger *zap.Logger
}

// RegisterRunner handles runner registration.
// This is a stub that returns Unimplemented.
func (s *runnerService) RegisterRunner(_ context.Context, req *pb.RegisterRunnerRequest) (*pb.RegisterRunnerResponse, error) {
	s.logger.Info("RegisterRunner called (stub)",
		zap.String("name", req.Name),
		zap.String("hostname", req.Hostname),
	)

	// Stub implementation - actual implementation will come later
	return &pb.RegisterRunnerResponse{
		RunnerId: "run_stub",
		Accepted: true,
		Message:  "stub: registration accepted",
	}, nil
}

// GetRunnerStatus returns the status of a runner.
// This is a stub that returns Unimplemented.
func (s *runnerService) GetRunnerStatus(_ context.Context, req *pb.GetRunnerStatusRequest) (*pb.RunnerStatus, error) {
	s.logger.Info("GetRunnerStatus called (stub)",
		zap.String("runner_id", req.RunnerId),
	)

	// Stub implementation
	return &pb.RunnerStatus{
		RunnerId: req.RunnerId,
		Status:   "unknown",
	}, nil
}

// Connect handles the bidirectional stream for control messages.
// This is a stub that returns Unimplemented.
func (s *runnerService) Connect(stream grpc.BidiStreamingServer[pb.RunnerMessage, pb.ServerCommand]) error {
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
// This is a stub that returns Unimplemented.
func (s *runnerService) StreamLogs(stream grpc.ClientStreamingServer[pb.StreamLogsMessage, pb.StreamLogsResponse]) error {
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
