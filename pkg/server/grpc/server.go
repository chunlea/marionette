package grpc

import (
	"context"
	"fmt"
	"net"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// Server is the gRPC server for runner communication.
type Server struct {
	server   *grpc.Server
	listener net.Listener
	logger   *zap.Logger
}

// Config holds configuration for the gRPC server.
type Config struct {
	Port int
}

// New creates a new gRPC server.
func New(cfg Config, logger *zap.Logger) (*Server, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	s := grpc.NewServer()

	// Register the RunnerService
	runnerSvc := &runnerService{logger: logger}
	pb.RegisterRunnerServiceServer(s, runnerSvc)

	// Enable reflection for grpcurl and debugging
	reflection.Register(s)

	return &Server{
		server:   s,
		listener: lis,
		logger:   logger,
	}, nil
}

// Start starts the gRPC server.
func (s *Server) Start() error {
	s.logger.Info("starting gRPC server", zap.String("addr", s.listener.Addr().String()))
	if err := s.server.Serve(s.listener); err != nil {
		return fmt.Errorf("grpc server error: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(_ context.Context) error {
	s.logger.Info("shutting down gRPC server")
	s.server.GracefulStop()
	return nil
}
