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

	conn       *grpc.ClientConn
	grpcClient pb.RunnerServiceClient

	runnerID string
	hostname string

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
	c.conn = conn
	c.grpcClient = pb.NewRunnerServiceClient(conn)

	// Register with server
	c.setState(StateRegistering)
	if err := c.register(ctx); err != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.grpcClient = nil
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

	// Wait for connection to be ready with timeout
	dialCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Check connection state by making a blocking call
	conn.Connect()
	if !conn.WaitForStateChange(dialCtx, conn.GetState()) {
		// If the state didn't change and we're not connected, we've timed out
		state := conn.GetState()
		if state != connectivity.Ready {
			_ = conn.Close()
			return nil, fmt.Errorf("connection not ready, state: %s", state)
		}
	}

	return conn, nil
}

func (c *Client) loadTLSCredentials() (credentials.TransportCredentials, error) {
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
	}

	// Load client certificate if provided
	if c.cfg.TLS.CertFile != "" && c.cfg.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.cfg.TLS.CertFile, c.cfg.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("loading client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate if provided
	if c.cfg.TLS.CAFile != "" {
		caCert, err := os.ReadFile(c.cfg.TLS.CAFile)
		if err != nil {
			return nil, fmt.Errorf("reading CA certificate: %w", err)
		}
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(caCert) {
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
	resp, err := c.grpcClient.RegisterRunner(ctx, req)
	if err != nil {
		return fmt.Errorf("register RPC failed: %w", err)
	}

	if !resp.Accepted {
		return &ErrRegistrationRejected{Message: resp.Message}
	}

	c.runnerID = resp.RunnerId
	c.logger.Info("registered with server",
		zap.String("runner_id", c.runnerID),
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
	if c.runnerID != "" {
		md.Set("x-runner-id", c.runnerID)
	}
	return metadata.NewOutgoingContext(ctx, md)
}

// RunnerID returns the assigned runner ID.
func (c *Client) RunnerID() string {
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
func (c *Client) GRPCClient() pb.RunnerServiceClient {
	return c.grpcClient
}

// Close closes the client connection.
func (c *Client) Close() error {
	c.setState(StateStopped)
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		c.grpcClient = nil
		return err
	}
	return nil
}
