// Package grpc provides the gRPC server for runner communication.
package grpc

import (
	"context"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	registry      *core.RunnerRegistry
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

// WithRegistry sets the runner registry for the RunnerService.
func WithRegistry(reg *core.RunnerRegistry) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.registry = reg
	}
}

// RegisterRunner handles runner registration.
// Validates the runner token and creates/updates the runner in the database.
func (s *RunnerService) RegisterRunner(ctx context.Context, req *pb.RegisterRunnerRequest) (*pb.RegisterRunnerResponse, error) {
	s.logger.Info("RegisterRunner called",
		zap.String("name", req.GetName()),
		zap.String("hostname", req.GetHostname()),
	)

	// Check if registry is configured
	if s.registry == nil {
		s.logger.Error("registry not configured")
		return &pb.RegisterRunnerResponse{
			Accepted: false,
			Message:  "server configuration error: registry not configured",
		}, status.Error(codes.Internal, "registry not configured")
	}

	// Build registration request
	regReq := &core.RegisterRequest{
		Token:        req.GetToken(),
		Name:         req.GetName(),
		Hostname:     req.GetHostname(),
		SandboxMode:  req.GetSandboxMode(),
		SandboxTypes: req.GetSandboxTypes(),
		Capabilities: req.GetCapabilities(),
		Labels:       req.GetLabels(),
	}

	// Register via registry
	result, err := s.registry.Register(ctx, regReq)
	if err != nil {
		s.logger.Warn("runner registration failed",
			zap.String("name", req.GetName()),
			zap.Error(err),
		)
		return &pb.RegisterRunnerResponse{
			Accepted: false,
			Message:  err.Error(),
		}, status.Errorf(codes.InvalidArgument, "registration failed: %v", err)
	}

	msg := "runner registered"
	if !result.IsNew {
		msg = "runner re-registered"
	}

	s.logger.Info(msg,
		zap.String("runner_id", result.RunnerID),
		zap.String("pool_name", result.PoolName),
		zap.Bool("is_new", result.IsNew),
	)

	return &pb.RegisterRunnerResponse{
		RunnerId: result.RunnerID,
		Accepted: true,
		Message:  msg,
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
