package grpc

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/config"
	"github.com/chunlea/marionette/pkg/crypto/certreloader"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

// Server is the gRPC server for runner communication.
type Server struct {
	server       *grpc.Server
	listener     net.Listener
	logger       *zap.Logger
	connManager  *ConnectionManager
	certReloader *certreloader.CertReloader
}

// Config holds configuration for the gRPC server.
type Config struct {
	Host  string
	Port  int
	TLS   *config.TLSConfig
	Store store.Store // Optional: enables full runner lifecycle management
}

// ServerOption is a functional option for configuring the gRPC server.
type ServerOption func(*serverOptions)

// serverOptions holds optional dependencies for the gRPC server.
type serverOptions struct {
	permissionManager core.PermissionManagerInterface
	sessionManager    core.SessionManagerInterface
	taskManager       core.TaskManagerInterface
	connManager       *ConnectionManager
}

// WithPermissionManager sets the permission manager for handling permission requests from runners.
func WithPermissionManager(pm core.PermissionManagerInterface) ServerOption {
	return func(o *serverOptions) {
		o.permissionManager = pm
	}
}

// WithSessionManager sets the session manager for session operations.
func WithSessionManager(sm core.SessionManagerInterface) ServerOption {
	return func(o *serverOptions) {
		o.sessionManager = sm
	}
}

// WithTaskManager sets the task manager for task operations.
func WithTaskManager(tm core.TaskManagerInterface) ServerOption {
	return func(o *serverOptions) {
		o.taskManager = tm
	}
}

// WithConnManager sets the connection manager for the server.
// If not provided, a new ConnectionManager will be created internally.
func WithConnManager(cm *ConnectionManager) ServerOption {
	return func(o *serverOptions) {
		o.connManager = cm
	}
}

// New creates a new gRPC server.
func New(cfg Config, logger *zap.Logger, opts ...ServerOption) (*Server, error) {
	// Apply options
	srvOpts := &serverOptions{}
	for _, opt := range opts {
		opt(srvOpts)
	}

	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", cfg.Host, cfg.Port))
	if err != nil {
		return nil, fmt.Errorf("failed to listen: %w", err)
	}

	// Build gRPC server options
	var grpcOpts []grpc.ServerOption

	// Configure TLS if enabled
	var certReloader *certreloader.CertReloader
	if cfg.TLS != nil && cfg.TLS.Enabled {
		tlsCreds, reloader, err := loadTLSCredentials(cfg.TLS, logger)
		if err != nil {
			return nil, fmt.Errorf("failed to load TLS credentials: %w", err)
		}
		certReloader = reloader
		grpcOpts = append(grpcOpts, grpc.Creds(tlsCreds))
		logger.Info("TLS enabled for gRPC server",
			zap.Bool("verify_client", cfg.TLS.VerifyClient),
			zap.Bool("hot_reload", certReloader != nil),
		)
	} else {
		logger.Warn("TLS disabled for gRPC server - this is not recommended for production")
	}

	// Add interceptors (order: recovery first to catch panics, then logging)
	grpcOpts = append(grpcOpts,
		grpc.ChainUnaryInterceptor(
			RecoveryUnaryInterceptor(logger),
			LoggingUnaryInterceptor(logger),
		),
		grpc.ChainStreamInterceptor(
			RecoveryStreamInterceptor(logger),
			LoggingStreamInterceptor(logger),
		),
	)

	s := grpc.NewServer(grpcOpts...)

	// Use provided connection manager or create a new one
	connManager := srvOpts.connManager
	if connManager == nil {
		connManager = NewConnectionManager(logger)
	}

	// Build RunnerService options
	svcOpts := []RunnerServiceOption{
		WithConnectionManager(connManager),
	}

	// If store is provided, wire up the full runner lifecycle
	if cfg.Store != nil {
		// Create token service for runner authentication
		tokenSvc := auth.NewRunnerTokenService(cfg.Store, id.RunnerToken)

		// Create runner registry for registration
		registry := core.NewRunnerRegistry(cfg.Store, tokenSvc, logger)

		// Create runner manager for lifecycle management
		runnerManager := core.NewRunnerManager(cfg.Store, connManager, logger)

		// Create message router with optional managers
		routerOpts := []MessageRouterOption{
			WithMRStore(cfg.Store), // Required for permission request handling
		}
		if srvOpts.permissionManager != nil {
			routerOpts = append(routerOpts, WithMRPermissionManager(srvOpts.permissionManager))
		}
		if srvOpts.taskManager != nil {
			routerOpts = append(routerOpts, WithMRTaskManager(srvOpts.taskManager))
		}
		router := NewMessageRouter(logger, runnerManager, routerOpts...)

		svcOpts = append(svcOpts,
			WithStore(cfg.Store),
			WithTokenService(tokenSvc),
			WithRegistry(registry),
			WithRunnerManager(runnerManager),
			WithRouter(router),
		)

		logger.Info("runner lifecycle services initialized")
	} else {
		logger.Warn("store not configured - runner registration will not work")
	}

	// Register the RunnerService
	runnerSvc := NewRunnerService(logger, svcOpts...)
	pb.RegisterRunnerServiceServer(s, runnerSvc)

	// Enable reflection for grpcurl and debugging
	reflection.Register(s)

	return &Server{
		server:       s,
		listener:     lis,
		logger:       logger,
		connManager:  connManager,
		certReloader: certReloader,
	}, nil
}

// ConnectionManager returns the server's connection manager.
func (s *Server) ConnectionManager() *ConnectionManager {
	return s.connManager
}

// loadTLSCredentials loads TLS certificates and returns gRPC transport credentials.
// It returns a CertReloader for hot-reloading support.
func loadTLSCredentials(cfg *config.TLSConfig, logger *zap.Logger) (credentials.TransportCredentials, *certreloader.CertReloader, error) {
	// Create certificate reloader for hot-reload support
	reloader, err := certreloader.New(cfg.CertFile, cfg.KeyFile, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate reloader: %w", err)
	}

	// Build TLS config using the reloader
	tlsConfig := reloader.NewTLSConfig()

	// Load CA certificate for client verification (mTLS)
	if cfg.CAFile != "" && cfg.VerifyClient {
		caCert, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			_ = reloader.Close()
			return nil, nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			_ = reloader.Close()
			return nil, nil, fmt.Errorf("failed to parse CA certificate")
		}

		tlsConfig.ClientCAs = certPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		logger.Info("mTLS enabled - client certificate verification required")
	} else if cfg.VerifyClient {
		// VerifyClient is true but no CA file - this is a configuration error
		_ = reloader.Close()
		return nil, nil, fmt.Errorf("verify_client is true but no ca_file specified")
	}

	return credentials.NewTLS(tlsConfig), reloader, nil
}

// Start starts the gRPC server.
func (s *Server) Start() error {
	s.logger.Info("starting gRPC server", zap.String("addr", s.listener.Addr().String()))

	// Start certificate watcher in background if enabled
	if s.certReloader != nil {
		go func() {
			ctx := context.Background()
			if err := s.certReloader.Watch(ctx); err != nil && err != context.Canceled {
				s.logger.Error("certificate watcher stopped unexpectedly", zap.Error(err))
			}
		}()
	}

	if err := s.server.Serve(s.listener); err != nil {
		return fmt.Errorf("grpc server error: %w", err)
	}
	return nil
}

// Shutdown gracefully shuts down the server.
func (s *Server) Shutdown(_ context.Context) error {
	s.logger.Info("shutting down gRPC server")

	// Close certificate reloader first to stop the watcher
	if s.certReloader != nil {
		if err := s.certReloader.Close(); err != nil {
			s.logger.Error("failed to close certificate reloader", zap.Error(err))
		}
	}

	s.server.GracefulStop()
	return nil
}
