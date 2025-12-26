package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	"github.com/chunlea/marionette/pkg/config"
	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

// Server is the gRPC server for runner communication.
type Server struct {
	server      *grpc.Server
	listener    net.Listener
	logger      *zap.Logger
	connManager *ConnectionManager
}

// Config holds configuration for the gRPC server.
type Config struct {
	Host string
	Port int
	TLS  *config.TLSConfig
}

// New creates a new gRPC server.
func New(cfg Config, logger *zap.Logger) (*Server, error) {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	// Build server options
	var opts []grpc.ServerOption

	// Configure TLS if enabled
	if cfg.TLS != nil && cfg.TLS.Enabled {
		tlsCreds, err := loadTLSCredentials(cfg.TLS, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
		}
		opts = append(opts, grpc.Creds(tlsCreds))
		logger.Info("TLS enabled for gRPC server",
			zap.Bool("verify_client", cfg.TLS.VerifyClient),
		)
	} else {
		logger.Warn("TLS disabled for gRPC server - this is not recommended for production")
	}

	s := grpc.NewServer(opts...)

	// Create connection manager
	connManager := NewConnectionManager(logger)

	// Register the RunnerService
	runnerSvc := NewRunnerService(logger, WithConnectionManager(connManager))
	pb.RegisterRunnerServiceServer(s, runnerSvc)

	// Enable reflection for grpcurl and debugging
	reflection.Register(s)

	return &Server{
		server:      s,
		listener:    lis,
		logger:      logger,
		connManager: connManager,
	}, nil
}

// ConnectionManager returns the server's connection manager.
func (s *Server) ConnectionManager() *ConnectionManager {
	return s.connManager
}

// loadTLSCredentials loads TLS certificates and returns gRPC transport credentials.
func loadTLSCredentials(cfg *config.TLSConfig, logger *zap.Logger) (credentials.TransportCredentials, error) {
	// Load server certificate and key
	serverCert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MinVersion:   tls.VersionTLS12,
	}

	// Load CA certificate for client verification (mTLS)
	if cfg.CAFile != "" && cfg.VerifyClient {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		logger.Info("mTLS enabled - client certificate verification required")
	} else if cfg.VerifyClient {
		// VerifyClient is true but no CA file - this is a configuration error
		return nil, fmt.Errorf("verify_client is true but no ca_file specified")
	}

	return credentials.NewTLS(tlsConfig), nil
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
