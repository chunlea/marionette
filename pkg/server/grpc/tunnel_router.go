package grpc

import (
	"context"
	"errors"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/tunnel"
	"go.uber.org/zap"
)

// TunnelRouter routes TunnelData between HTTP handlers and runners.
// It manages active connections and coordinates bidirectional data flow.
type TunnelRouter struct {
	logger      *zap.Logger
	connManager CommandSender
	tm          *tunnel.TunnelManager

	// Active connections waiting for responses
	// Key: connectionID, Value: response channel
	connections   map[string]*tunnelConnection
	connectionsMu sync.RWMutex

	// Tunnel to runner mapping
	// Key: tunnelID, Value: runnerID
	tunnelRunners   map[string]string
	tunnelRunnersMu sync.RWMutex
}

// tunnelConnection represents an active tunnel connection waiting for response.
type tunnelConnection struct {
	tunnelID     string
	connectionID string
	runnerID     string
	responseCh   chan *pb.TunnelData
	createdAt    time.Time
}

// TunnelRouterOption is a functional option for TunnelRouter.
type TunnelRouterOption func(*TunnelRouter)

// WithTRLogger sets the logger for the tunnel router.
func WithTRLogger(logger *zap.Logger) TunnelRouterOption {
	return func(r *TunnelRouter) {
		r.logger = logger
	}
}

// WithTRConnectionManager sets the connection manager for the tunnel router.
func WithTRConnectionManager(cm CommandSender) TunnelRouterOption {
	return func(r *TunnelRouter) {
		r.connManager = cm
	}
}

// WithTRTunnelManager sets the tunnel manager for the tunnel router.
func WithTRTunnelManager(tm *tunnel.TunnelManager) TunnelRouterOption {
	return func(r *TunnelRouter) {
		r.tm = tm
	}
}

// NewTunnelRouter creates a new TunnelRouter.
func NewTunnelRouter(opts ...TunnelRouterOption) *TunnelRouter {
	r := &TunnelRouter{
		logger:        zap.NewNop(),
		connections:   make(map[string]*tunnelConnection),
		tunnelRunners: make(map[string]string),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// RegisterTunnel registers a tunnel with its associated runner.
// This is called when a tunnel is created to establish the routing.
func (r *TunnelRouter) RegisterTunnel(tunnelID, runnerID string) {
	r.tunnelRunnersMu.Lock()
	defer r.tunnelRunnersMu.Unlock()
	r.tunnelRunners[tunnelID] = runnerID

	r.logger.Debug("tunnel registered",
		zap.String("tunnel_id", tunnelID),
		zap.String("runner_id", runnerID),
	)
}

// UnregisterTunnel removes a tunnel registration.
func (r *TunnelRouter) UnregisterTunnel(tunnelID string) {
	r.tunnelRunnersMu.Lock()
	defer r.tunnelRunnersMu.Unlock()
	delete(r.tunnelRunners, tunnelID)

	r.logger.Debug("tunnel unregistered",
		zap.String("tunnel_id", tunnelID),
	)
}

// GetRunnerForTunnel returns the runner ID associated with a tunnel.
func (r *TunnelRouter) GetRunnerForTunnel(tunnelID string) (string, bool) {
	r.tunnelRunnersMu.RLock()
	defer r.tunnelRunnersMu.RUnlock()
	runnerID, ok := r.tunnelRunners[tunnelID]
	return runnerID, ok
}

// SendRequest sends a TunnelData request to a runner and waits for response.
// This is the main entry point for HTTP proxy handlers.
func (r *TunnelRouter) SendRequest(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan *pb.TunnelData, error) {
	// Get runner for this tunnel
	runnerID, ok := r.GetRunnerForTunnel(tunnelID)
	if !ok {
		return nil, errors.New("tunnel not found or not registered")
	}

	if r.connManager == nil {
		return nil, errors.New("connection manager not configured")
	}

	// Create response channel
	responseCh := make(chan *pb.TunnelData, 10)
	conn := &tunnelConnection{
		tunnelID:     tunnelID,
		connectionID: connectionID,
		runnerID:     runnerID,
		responseCh:   responseCh,
		createdAt:    time.Now(),
	}

	// Register connection
	r.connectionsMu.Lock()
	r.connections[connectionID] = conn
	r.connectionsMu.Unlock()

	// Send data to runner
	cmd := &pb.ServerCommand{
		Payload: &pb.ServerCommand_TunnelData{
			TunnelData: &pb.TunnelData{
				TunnelId:     tunnelID,
				ConnectionId: connectionID,
				Data:         data,
				Eof:          false,
			},
		},
	}

	if err := r.connManager.SendCommand(runnerID, cmd); err != nil {
		// Clean up on failure
		r.connectionsMu.Lock()
		delete(r.connections, connectionID)
		r.connectionsMu.Unlock()
		close(responseCh)
		return nil, err
	}

	r.logger.Debug("sent tunnel request to runner",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.String("runner_id", runnerID),
		zap.Int("data_len", len(data)),
	)

	return responseCh, nil
}

// CloseConnection closes a tunnel connection and cleans up resources.
func (r *TunnelRouter) CloseConnection(connectionID string) {
	r.connectionsMu.Lock()
	conn, ok := r.connections[connectionID]
	if ok {
		delete(r.connections, connectionID)
	}
	r.connectionsMu.Unlock()

	if ok && conn.responseCh != nil {
		close(conn.responseCh)
		r.logger.Debug("closed tunnel connection",
			zap.String("connection_id", connectionID),
			zap.String("tunnel_id", conn.tunnelID),
		)
	}
}

// SendEOF sends an EOF signal to a runner for a connection.
func (r *TunnelRouter) SendEOF(tunnelID, connectionID string) error {
	runnerID, ok := r.GetRunnerForTunnel(tunnelID)
	if !ok {
		return errors.New("tunnel not found")
	}

	if r.connManager == nil {
		return errors.New("connection manager not configured")
	}

	cmd := &pb.ServerCommand{
		Payload: &pb.ServerCommand_TunnelData{
			TunnelData: &pb.TunnelData{
				TunnelId:     tunnelID,
				ConnectionId: connectionID,
				Eof:          true,
			},
		},
	}

	return r.connManager.SendCommand(runnerID, cmd)
}

// HandleTunnelData handles incoming TunnelData from a runner.
// Routes the data to the appropriate HTTP response handler.
func (r *TunnelRouter) HandleTunnelData(ctx context.Context, runnerID string, data *pb.TunnelData) error {
	connectionID := data.GetConnectionId()

	r.connectionsMu.RLock()
	conn, ok := r.connections[connectionID]
	r.connectionsMu.RUnlock()

	if !ok {
		r.logger.Warn("received data for unknown connection",
			zap.String("connection_id", connectionID),
			zap.String("tunnel_id", data.GetTunnelId()),
			zap.String("runner_id", runnerID),
		)
		return nil
	}

	// Verify the data is from the correct runner
	if conn.runnerID != runnerID {
		r.logger.Warn("received data from unexpected runner",
			zap.String("connection_id", connectionID),
			zap.String("expected_runner", conn.runnerID),
			zap.String("actual_runner", runnerID),
		)
		return nil
	}

	// Send to response channel (non-blocking with timeout)
	select {
	case conn.responseCh <- data:
		r.logger.Debug("routed tunnel data to handler",
			zap.String("connection_id", connectionID),
			zap.String("tunnel_id", data.GetTunnelId()),
			zap.Int("data_len", len(data.GetData())),
			zap.Bool("eof", data.GetEof()),
		)
	case <-ctx.Done():
		r.logger.Warn("context cancelled while routing data",
			zap.String("connection_id", connectionID),
		)
		return ctx.Err()
	default:
		r.logger.Warn("response channel full, dropping data",
			zap.String("connection_id", connectionID),
			zap.String("tunnel_id", data.GetTunnelId()),
		)
	}

	// If EOF, close the connection
	if data.GetEof() {
		r.CloseConnection(connectionID)
	}

	return nil
}

// HandleCloseTunnel handles a tunnel close request from a runner.
func (r *TunnelRouter) HandleCloseTunnel(ctx context.Context, runnerID string, tunnelID string, reason string) error {
	r.logger.Info("handling tunnel close",
		zap.String("runner_id", runnerID),
		zap.String("tunnel_id", tunnelID),
		zap.String("reason", reason),
	)

	// Close all connections for this tunnel
	r.connectionsMu.Lock()
	for connID, conn := range r.connections {
		if conn.tunnelID == tunnelID {
			if conn.responseCh != nil {
				close(conn.responseCh)
			}
			delete(r.connections, connID)
		}
	}
	r.connectionsMu.Unlock()

	// Unregister the tunnel
	r.UnregisterTunnel(tunnelID)

	// Close in TunnelManager if configured
	if r.tm != nil {
		return r.tm.Close(ctx, tunnelID)
	}

	return nil
}

// CleanupStaleConnections removes connections older than the given duration.
// Should be called periodically to prevent memory leaks.
func (r *TunnelRouter) CleanupStaleConnections(maxAge time.Duration) int {
	r.connectionsMu.Lock()
	defer r.connectionsMu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	cleaned := 0

	for connID, conn := range r.connections {
		if conn.createdAt.Before(cutoff) {
			if conn.responseCh != nil {
				close(conn.responseCh)
			}
			delete(r.connections, connID)
			cleaned++
			r.logger.Debug("cleaned up stale connection",
				zap.String("connection_id", connID),
				zap.String("tunnel_id", conn.tunnelID),
				zap.Duration("age", time.Since(conn.createdAt)),
			)
		}
	}

	return cleaned
}
