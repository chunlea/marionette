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
//
// The runner lifecycle components are supplied by the caller, not built here.
// They used to be constructed inside New, which meant production got a
// RunnerManager with no TaskManager attached (so a dead runner's tasks stayed
// "running" forever) while tests injected a complete one and passed. core.Wire
// is now the single place these are built.
type Config struct {
	Host  string
	Port  int
	TLS   *config.TLSConfig
	Store store.Store // Optional: enables full runner lifecycle management

	// RunnerManager handles runner connect/disconnect/heartbeat.
	// Required when Store is set.
	RunnerManager core.RunnerManagerInterface
	// RunnerRegistry handles runner registration. Required when Store is set.
	RunnerRegistry *core.RunnerRegistry
	// RunnerTokenService authenticates runners. Required when Store is set.
	RunnerTokenService *auth.RunnerTokenService
	// MessageRouter routes inbound runner messages. Required when Store is set.
	MessageRouter MessageRouterInterface
	// LogSubscribers receives streamed logs for real-time subscribers.
	// Optional.
	LogSubscribers core.LogSubscriberManagerInterface

	// ConnectionBinder records which process holds each runner's control
	// stream, so commands sent from another replica can be routed to it.
	// Optional: unset is a single-process deployment.
	ConnectionBinder ConnectionBinder

	// PeerCredential authenticates a peer replica on the internal router
	// method. Derived from the master key, which every replica already shares.
	// Empty leaves the method refusing to serve, which a single-process
	// deployment never reaches because it never hops.
	PeerCredential PeerCredential

	// RoutingMetrics counts what routing does. Optional.
	RoutingMetrics *RoutingMetrics
}

// validate checks that the runner lifecycle components are present whenever a
// store is configured. A nil component here is exactly the failure mode this
// wiring pass exists to remove, so it is an error, not a warning.
func (c Config) validate() error {
	if c.Store == nil {
		return nil
	}
	switch {
	case c.RunnerManager == nil:
		return fmt.Errorf("grpc: RunnerManager is required when Store is set")
	case c.RunnerRegistry == nil:
		return fmt.Errorf("grpc: RunnerRegistry is required when Store is set")
	case c.RunnerTokenService == nil:
		return fmt.Errorf("grpc: RunnerTokenService is required when Store is set")
	case c.MessageRouter == nil:
		return fmt.Errorf("grpc: MessageRouter is required when Store is set")
	}
	return nil
}

// ServerOption is a functional option for configuring the gRPC server.
type ServerOption func(*serverOptions)

// serverOptions holds optional dependencies for the gRPC server.
type serverOptions struct {
	connManager          *ConnectionManager
	browserStreamHandler BrowserStreamHandlerInterface

	// Interceptors for metrics, tracing, etc.
	unaryInterceptors  []grpc.UnaryServerInterceptor
	streamInterceptors []grpc.StreamServerInterceptor
}

// WithConnManager sets the connection manager for the server.
// If not provided, a new ConnectionManager will be created internally.
func WithConnManager(cm *ConnectionManager) ServerOption {
	return func(o *serverOptions) {
		o.connManager = cm
	}
}

// WithBrowserStream sets the browser stream handler for browser frame streaming.
func WithBrowserStream(bsh BrowserStreamHandlerInterface) ServerOption {
	return func(o *serverOptions) {
		o.browserStreamHandler = bsh
	}
}

// WithUnaryInterceptor adds a unary server interceptor.
// Interceptors are applied in the order they are added.
func WithUnaryInterceptor(i grpc.UnaryServerInterceptor) ServerOption {
	return func(o *serverOptions) {
		o.unaryInterceptors = append(o.unaryInterceptors, i)
	}
}

// WithStreamInterceptor adds a stream server interceptor.
// Interceptors are applied in the order they are added.
func WithStreamInterceptor(i grpc.StreamServerInterceptor) ServerOption {
	return func(o *serverOptions) {
		o.streamInterceptors = append(o.streamInterceptors, i)
	}
}

// New creates a new gRPC server.
func New(cfg Config, logger *zap.Logger, opts ...ServerOption) (*Server, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

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

	// Build interceptor chains (order: recovery first to catch panics, then logging, then custom)
	unaryInterceptors := []grpc.UnaryServerInterceptor{
		RecoveryUnaryInterceptor(logger),
		LoggingUnaryInterceptor(logger),
	}
	unaryInterceptors = append(unaryInterceptors, srvOpts.unaryInterceptors...)

	streamInterceptors := []grpc.StreamServerInterceptor{
		RecoveryStreamInterceptor(logger),
		LoggingStreamInterceptor(logger),
	}
	streamInterceptors = append(streamInterceptors, srvOpts.streamInterceptors...)

	grpcOpts = append(grpcOpts,
		grpc.ChainUnaryInterceptor(unaryInterceptors...),
		grpc.ChainStreamInterceptor(streamInterceptors...),
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

	// Attach the runner lifecycle components built by core.Wire.
	if cfg.Store != nil {
		svcOpts = append(svcOpts,
			WithStore(cfg.Store),
			WithTokenService(cfg.RunnerTokenService),
			WithRegistry(cfg.RunnerRegistry),
			WithRunnerManager(cfg.RunnerManager),
			WithRouter(cfg.MessageRouter),
		)

		if cfg.LogSubscribers != nil {
			svcOpts = append(svcOpts, WithLogSubscriberManager(cfg.LogSubscribers))
		}

		if cfg.ConnectionBinder != nil {
			svcOpts = append(svcOpts, WithConnectionBinder(cfg.ConnectionBinder))
			logger.Info("cross-replica connection registry attached")
		}

		logger.Info("runner lifecycle services attached")
	} else {
		logger.Warn("store not configured - runner registration will not work")
	}

	// Add browser stream handler if configured
	if srvOpts.browserStreamHandler != nil {
		svcOpts = append(svcOpts, WithBrowserStreamHandler(srvOpts.browserStreamHandler))
		logger.Info("browser stream handler configured")
	}

	// Register the RunnerService
	runnerSvc := NewRunnerService(logger, svcOpts...)
	pb.RegisterRunnerServiceServer(s, runnerSvc)

	// The internal router shares this listener with RunnerService: no new
	// port, no new firewall rule, and mTLS - where configured - already covers
	// it. Because runners dial this port, the method authenticates a peer
	// credential a runner never holds, and refuses outright when none is
	// configured.
	pb.RegisterInternalRouterServiceServer(s,
		NewInternalRouterService(connManager, cfg.PeerCredential, cfg.RoutingMetrics, logger))

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
