package agent

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/crypto/certreloader"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

// ConnState represents the connection state of the client.
type ConnState int32

const (
	// StateDisconnected means the client is not connected.
	StateDisconnected ConnState = iota
	// StateConnecting means the client is establishing a connection.
	StateConnecting
	// StateRegistering means the client is registering with the server.
	StateRegistering
	// StateConnected means the client is connected and registered.
	StateConnected
	// StateStopped means the client has been stopped.
	StateStopped
)

// String returns the string representation of the connection state.
func (s ConnState) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateRegistering:
		return "registering"
	case StateConnected:
		return "connected"
	case StateStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// Client manages the gRPC connection to the Marionette server.
type Client struct {
	cfg    *Config
	logger *zap.Logger

	// connMu guards everything that Connect/Close swap out underneath the
	// goroutines that keep reading it: the control channel, the heartbeat
	// loop and the log streamer all call GRPCClient/RunnerID concurrently.
	connMu     sync.RWMutex
	conn       *grpc.ClientConn
	grpcClient pb.RunnerServiceClient
	runnerID   string

	hostname     string
	certReloader *certreloader.CertReloader

	state   atomic.Int32
	stateMu sync.RWMutex
	stateC  chan ConnState
}

// NewClient creates a new agent client.
func NewClient(cfg *Config, logger *zap.Logger) *Client {
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}

	c := &Client{
		cfg:      cfg,
		logger:   logger.Named("client"),
		hostname: hostname,
		stateC:   make(chan ConnState, 10),
	}
	c.state.Store(int32(StateDisconnected))
	return c
}

// Connect establishes a connection to the server and registers the runner.
func (c *Client) Connect(ctx context.Context) error {
	c.stateMu.Lock()
	currentState := ConnState(c.state.Load())
	if currentState == StateConnected {
		c.stateMu.Unlock()
		return ErrAlreadyConnected
	}
	if currentState == StateStopped {
		c.stateMu.Unlock()
		return ErrShuttingDown
	}
	c.stateMu.Unlock()

	c.setState(StateConnecting)

	// Establish gRPC connection
	conn, err := c.dial(ctx)
	if err != nil {
		c.setState(StateDisconnected)
		return &ErrConnectionFailed{Addr: c.cfg.Server.Address, Cause: err}
	}
	c.connMu.Lock()
	c.conn = conn
	c.grpcClient = pb.NewRunnerServiceClient(conn)
	c.connMu.Unlock()

	// Register with server
	c.setState(StateRegistering)
	if err := c.register(ctx); err != nil {
		c.connMu.Lock()
		c.conn = nil
		c.grpcClient = nil
		c.connMu.Unlock()
		_ = conn.Close()
		c.setState(StateDisconnected)
		return err
	}

	c.setState(StateConnected)
	return nil
}

func (c *Client) dial(ctx context.Context) (*grpc.ClientConn, error) {
	var opts []grpc.DialOption

	if c.cfg.TLS.Enabled {
		creds, err := c.loadTLSCredentials()
		if err != nil {
			return nil, fmt.Errorf("loading TLS credentials: %w", err)
		}
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}

	c.logger.Debug("dialing server",
		zap.String("address", c.cfg.Server.Address),
		zap.Bool("tls", c.cfg.TLS.Enabled),
	)

	// Create client and establish connection
	conn, err := grpc.NewClient(c.cfg.Server.Address, opts...)
	if err != nil {
		return nil, err
	}

	// Wait for the connection to actually be ready.
	//
	// WaitForStateChange returns after the FIRST transition, which for a fresh
	// client is IDLE -> CONNECTING: returning there declared success while
	// nothing was connected yet. Loop until Ready, the context expires, or the
	// connection shuts down.
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	conn.Connect()
	for {
		state := conn.GetState()
		switch state {
		case connectivity.Ready:
			return conn, nil
		case connectivity.Shutdown:
			_ = conn.Close()
			return nil, fmt.Errorf("connection shut down before becoming ready")
		case connectivity.Idle, connectivity.Connecting, connectivity.TransientFailure:
			// Keep waiting; gRPC retries transient failures on its own.
		}

		if !conn.WaitForStateChange(dialCtx, state) {
			_ = conn.Close()
			return nil, fmt.Errorf("connection not ready, state: %s", conn.GetState())
		}
	}
}

func (c *Client) loadTLSCredentials() (credentials.TransportCredentials, error) {
	var tlsConfig *tls.Config

	// Load client certificate if provided using CertReloader for hot-reload
	if c.cfg.TLS.CertFile != "" && c.cfg.TLS.KeyFile != "" {
		reloader, err := certreloader.New(c.cfg.TLS.CertFile, c.cfg.TLS.KeyFile, c.logger)
		if err != nil {
			return nil, fmt.Errorf("creating certificate reloader: %w", err)
		}
		c.certReloader = reloader
		tlsConfig = reloader.NewTLSConfig()
	} else {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	// Load CA certificate if provided
	if c.cfg.TLS.CAFile != "" {
		caCert, err := os.ReadFile(c.cfg.TLS.CAFile)
		if err != nil {
			if c.certReloader != nil {
				_ = c.certReloader.Close()
				c.certReloader = nil
			}
			return nil, fmt.Errorf("reading CA certificate: %w", err)
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
			if c.certReloader != nil {
				_ = c.certReloader.Close()
				c.certReloader = nil
			}
			return nil, fmt.Errorf("failed to append CA certificate")
		}
		tlsConfig.RootCAs = certPool
	}

	// Skip verification if requested (insecure, dev only)
	if c.cfg.TLS.SkipVerify {
		tlsConfig.InsecureSkipVerify = true
	}

	return credentials.NewTLS(tlsConfig), nil
}

func (c *Client) register(ctx context.Context) error {
	// Detect sandbox capabilities if not specified
	sandboxTypes := c.cfg.Sandbox.Types
	if len(sandboxTypes) == 0 {
		caps := DetectSandboxCapabilities()
		sandboxTypes = caps.Types
	}

	runnerName := c.cfg.Runner.Name
	if runnerName == "" {
		runnerName = c.hostname
	}

	req := &pb.RegisterRunnerRequest{
		Name:         runnerName,
		Hostname:     c.hostname,
		Token:        c.cfg.Runner.Token,
		Capabilities: []string{}, // Future: detect additional capabilities
		SandboxMode:  c.cfg.Sandbox.Mode,
		SandboxTypes: sandboxTypes,
		PoolName:     c.cfg.Runner.PoolName,
		Labels:       c.cfg.Runner.Labels,
		Annotations:  c.cfg.Runner.Annotations,
	}

	c.logger.Debug("registering with server",
		zap.String("name", req.Name),
		zap.String("hostname", req.Hostname),
		zap.String("pool", req.PoolName),
		zap.String("sandbox_mode", req.SandboxMode),
		zap.Strings("sandbox_types", req.SandboxTypes),
	)

	ctx = c.attachMetadata(ctx)

	grpcClient := c.GRPCClient()
	if grpcClient == nil {
		return ErrNotConnected
	}

	resp, err := grpcClient.RegisterRunner(ctx, req)
	if err != nil {
		return fmt.Errorf("register RPC failed: %w", err)
	}

	if !resp.Accepted {
		return &ErrRegistrationRejected{Message: resp.Message}
	}

	c.connMu.Lock()
	c.runnerID = resp.RunnerId
	c.connMu.Unlock()

	c.logger.Info("registered with server",
		zap.String("runner_id", resp.RunnerId),
		zap.String("message", resp.Message),
	)

	return nil
}

// AttachMetadata adds authentication headers to outgoing requests.
func (c *Client) AttachMetadata(ctx context.Context) context.Context {
	return c.attachMetadata(ctx)
}

// attachMetadata adds authentication headers to outgoing requests.
func (c *Client) attachMetadata(ctx context.Context) context.Context {
	md := metadata.New(map[string]string{
		"x-runner-token": c.cfg.Runner.Token,
	})
	if runnerID := c.RunnerID(); runnerID != "" {
		md.Set("x-runner-id", runnerID)
	}
	return metadata.NewOutgoingContext(ctx, md)
}

// RunnerID returns the assigned runner ID.
func (c *Client) RunnerID() string {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.runnerID
}

// State returns the current connection state.
func (c *Client) State() ConnState {
	return ConnState(c.state.Load())
}

func (c *Client) setState(s ConnState) {
	old := ConnState(c.state.Swap(int32(s)))
	if old != s {
		c.logger.Debug("state changed",
			zap.String("from", old.String()),
			zap.String("to", s.String()),
		)
		select {
		case c.stateC <- s:
		default:
			// Non-blocking send; drop if channel is full
		}
	}
}

// StateC returns a channel that receives state changes.
func (c *Client) StateC() <-chan ConnState {
	return c.stateC
}

// GRPCClient returns the underlying gRPC client for advanced operations.
// Returns nil if not connected.
//
// Callers must re-read this after every reconnect: a value captured once is a
// handle to a connection that may since have been closed.
func (c *Client) GRPCClient() pb.RunnerServiceClient {
	c.connMu.RLock()
	defer c.connMu.RUnlock()
	return c.grpcClient
}

// Disconnect tears down the current connection without stopping the client, so
// a supervisor can reconnect. Unlike Close it does not move the client to the
// stopped state.
func (c *Client) Disconnect() error {
	c.connMu.Lock()
	conn := c.conn
	c.conn = nil
	c.grpcClient = nil
	c.runnerID = ""
	c.connMu.Unlock()

	c.setState(StateDisconnected)

	if conn != nil {
		return conn.Close()
	}
	return nil
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.setState(StateStopped)

	// Close certificate reloader if present
	if c.certReloader != nil {
		if err := c.certReloader.Close(); err != nil {
			c.logger.Error("failed to close certificate reloader", zap.Error(err))
		}
		c.certReloader = nil
	}

	c.connMu.Lock()
	conn := c.conn
	c.conn = nil
	c.grpcClient = nil
	c.connMu.Unlock()

	if conn != nil {
		return conn.Close()
	}
	return nil
}
