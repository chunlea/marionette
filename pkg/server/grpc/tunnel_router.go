package grpc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/tunnel"
	"go.uber.org/zap"
)

const (
	// defaultTunnelSendTimeout bounds how long a frame may wait for the HTTP
	// handler to drain responseCh before the connection is torn down. Sends
	// block rather than drop: a dropped frame silently truncates the HTTP body
	// or WebSocket frame the consumer is reassembling.
	//
	// This wait happens on the connection's own pump goroutine, so it costs
	// that connection only. The runner's control stream is never held by it.
	defaultTunnelSendTimeout = 30 * time.Second

	// tunnelResponseBuffer is the per-connection response channel buffer.
	tunnelResponseBuffer = 10

	// tunnelInboxBuffer bounds the queue between the control stream reader and
	// a connection's pump.
	//
	// Together with tunnelResponseBuffer this is the burst a briefly paused
	// consumer can absorb before the connection is torn down. It is kept small
	// deliberately: frames run to 64KB, so the queue is also the per-connection
	// memory a wedged consumer can pin.
	tunnelInboxBuffer = 64
)

var (
	// errConnectionClosed means the consumer went away before the frame could
	// be delivered.
	errConnectionClosed = errors.New("tunnel connection closed")

	// errSendTimeout means the consumer did not drain the channel in time.
	errSendTimeout = errors.New("tunnel consumer stalled")

	// errInboxFull means the consumer fell far enough behind to fill the
	// connection's queue.
	errInboxFull = errors.New("tunnel connection queue full")
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

	// sendTimeout bounds a blocked send to a stalled consumer.
	sendTimeout time.Duration
}

// tunnelConnection represents an active tunnel connection waiting for response.
type tunnelConnection struct {
	tunnelID     string
	connectionID string
	runnerID     string
	responseCh   chan *pb.TunnelData
	createdAt    time.Time

	// done is closed before responseCh so a sender can never observe an open
	// connection and then write to a channel that has since been closed.
	done      chan struct{}
	closeOnce sync.Once

	// sendMu serializes senders. close acquires it after closing done, so it
	// waits for any in-flight send to unwind before closing responseCh.
	sendMu sync.Mutex

	// lastActivity holds the Unix-nano timestamp of the most recent frame in
	// either direction. Accessed atomically so idle sweeps never block a
	// sender.
	lastActivity atomic.Int64

	// inbox queues frames from the runner's control stream for the pump. The
	// reader hands frames over here and moves on, so a stalled consumer costs
	// this connection only rather than every message from that runner.
	inbox chan *pb.TunnelData

	// pumpDone is closed when the pump goroutine has exited.
	pumpDone chan struct{}
}

// newTunnelConnection builds a connection with its close/activity bookkeeping
// initialised. tunnelConnection must not be constructed as a bare literal:
// close panics on a nil done channel.
//
// The connection is inert until TunnelRouter.register starts its pump.
func newTunnelConnection(tunnelID, connectionID, runnerID string) *tunnelConnection {
	c := &tunnelConnection{
		tunnelID:     tunnelID,
		connectionID: connectionID,
		runnerID:     runnerID,
		responseCh:   make(chan *pb.TunnelData, tunnelResponseBuffer),
		createdAt:    time.Now(),
		done:         make(chan struct{}),
		inbox:        make(chan *pb.TunnelData, tunnelInboxBuffer),
		pumpDone:     make(chan struct{}),
	}
	c.touch()
	return c
}

// touch records activity on the connection for idle-based cleanup.
func (c *tunnelConnection) touch() {
	c.lastActivity.Store(time.Now().UnixNano())
}

// idleSince reports when the connection last carried a frame.
func (c *tunnelConnection) idleSince() time.Time {
	return time.Unix(0, c.lastActivity.Load())
}

// close tears the connection down. It is safe to call concurrently and more
// than once. Callers must not hold connectionsMu: close waits for an in-flight
// send to unwind.
func (c *tunnelConnection) close() {
	c.closeOnce.Do(func() {
		// Wake any blocked sender first; no sender proceeds past done.
		close(c.done)
		// Wait for an in-flight send to release sendMu before closing the
		// channel it may still be writing to.
		c.sendMu.Lock()
		defer c.sendMu.Unlock()
		close(c.responseCh)
	})
}

// enqueue hands a frame to the pump. It never blocks on the consumer, so the
// caller — the runner's control stream reader — is never held up by one slow
// tunnel.
//
// The default case below is not the silent drop this file used to have. A full
// inbox means the pump has already spent the whole send timeout blocked on a
// consumer that is not draining; the caller responds by tearing the connection
// down, so the frame is never quietly lost.
func (c *tunnelConnection) enqueue(data *pb.TunnelData) error {
	select {
	case <-c.done:
		return errConnectionClosed
	default:
	}

	select {
	case c.inbox <- data:
		return nil
	case <-c.done:
		return errConnectionClosed
	default:
		return errInboxFull
	}
}

// pump delivers queued frames to the consumer, in order, on the connection's
// own goroutine. It owns the blocking send: the deadline and teardown
// semantics are unchanged, they simply no longer run on the shared reader.
//
// It exits when the connection closes, when a send fails, or after delivering
// EOF, and calls onTeardown so the router drops the connection with it.
func (c *tunnelConnection) pump(logger *zap.Logger, sendTimeout time.Duration, onTeardown func()) {
	defer close(c.pumpDone)

	for {
		select {
		case <-c.done:
			return

		case data := <-c.inbox:
			if err := c.send(context.Background(), data, sendTimeout); err != nil {
				if errors.Is(err, errSendTimeout) {
					logger.Warn("tunnel consumer stalled, closing connection",
						zap.String("connection_id", c.connectionID),
						zap.String("tunnel_id", c.tunnelID),
						zap.Duration("timeout", sendTimeout),
					)
				}
				onTeardown()
				return
			}

			logger.Debug("routed tunnel data to handler",
				zap.String("connection_id", c.connectionID),
				zap.String("tunnel_id", c.tunnelID),
				zap.Int("data_len", len(data.GetData())),
				zap.Bool("eof", data.GetEof()),
			)

			if data.GetEof() {
				// Delivered first, then torn down, so the consumer sees the
				// final frame before responseCh closes.
				onTeardown()
				return
			}
		}
	}
}

// send delivers a frame to the consumer, blocking until the consumer drains
// the channel, the connection closes, ctx is cancelled, or timeout expires.
//
// There is deliberately no default case: this is a data path, and a
// non-blocking send truncates whatever the consumer is reassembling.
func (c *tunnelConnection) send(ctx context.Context, data *pb.TunnelData, timeout time.Duration) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	// close may have fired while we waited for sendMu.
	select {
	case <-c.done:
		return errConnectionClosed
	default:
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case c.responseCh <- data:
		c.touch()
		return nil
	case <-c.done:
		return errConnectionClosed
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errSendTimeout
	}
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

// WithTRSendTimeout sets how long a frame may wait for a stalled consumer
// before the connection is torn down.
func WithTRSendTimeout(d time.Duration) TunnelRouterOption {
	return func(r *TunnelRouter) {
		r.sendTimeout = d
	}
}

// NewTunnelRouter creates a new TunnelRouter.
func NewTunnelRouter(opts ...TunnelRouterOption) *TunnelRouter {
	r := &TunnelRouter{
		logger:        zap.NewNop(),
		connections:   make(map[string]*tunnelConnection),
		tunnelRunners: make(map[string]string),
		sendTimeout:   defaultTunnelSendTimeout,
	}
	for _, opt := range opts {
		opt(r)
	}
	if r.sendTimeout <= 0 {
		r.sendTimeout = defaultTunnelSendTimeout
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

// NotifyTunnelCreated sends a CreateTunnel command to the runner.
// This is called after a tunnel is created via API to tell the runner to start the relay.
func (r *TunnelRouter) NotifyTunnelCreated(tunnelID, runnerID, tunnelType string, localPort int32, direction string) error {
	if r.connManager == nil {
		return errors.New("connection manager not configured")
	}

	cmd := &pb.ServerCommand{
		Payload: &pb.ServerCommand_CreateTunnel{
			CreateTunnel: &pb.CreateTunnel{
				TunnelId:  tunnelID,
				Type:      tunnelType,
				LocalPort: localPort,
				Direction: direction,
			},
		},
	}

	if err := r.connManager.SendCommand(runnerID, cmd); err != nil {
		r.logger.Error("failed to send CreateTunnel command to runner",
			zap.String("tunnel_id", tunnelID),
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return err
	}

	r.logger.Debug("sent CreateTunnel command to runner",
		zap.String("tunnel_id", tunnelID),
		zap.String("runner_id", runnerID),
		zap.String("type", tunnelType),
		zap.Int32("local_port", localPort),
	)

	return nil
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

	conn := newTunnelConnection(tunnelID, connectionID, runnerID)
	r.register(conn)

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
		conn.close()
		return nil, err
	}

	r.logger.Debug("sent tunnel request to runner",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.String("runner_id", runnerID),
		zap.Int("data_len", len(data)),
	)

	return conn.responseCh, nil
}

// register adds a connection to the router and starts its pump. This is the
// only sanctioned way to make a connection live: without a pump nothing drains
// its inbox.
func (r *TunnelRouter) register(conn *tunnelConnection) {
	r.connectionsMu.Lock()
	r.connections[conn.connectionID] = conn
	r.connectionsMu.Unlock()

	go conn.pump(r.logger, r.sendTimeout, func() {
		r.CloseConnection(conn.connectionID)
	})
}

// CloseConnection closes a tunnel connection and cleans up resources.
// Safe to call concurrently and more than once for the same connection.
func (r *TunnelRouter) CloseConnection(connectionID string) {
	r.connectionsMu.Lock()
	conn, ok := r.connections[connectionID]
	if ok {
		delete(r.connections, connectionID)
	}
	r.connectionsMu.Unlock()

	if !ok {
		return
	}

	// Closed outside connectionsMu: close waits for an in-flight send, and
	// holding the map lock through that would stall all routing.
	conn.close()

	r.logger.Debug("closed tunnel connection",
		zap.String("connection_id", connectionID),
		zap.String("tunnel_id", conn.tunnelID),
	)
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

// SendData sends data through the tunnel for bidirectional streaming (e.g., WebSocket).
// This is used to send data from the HTTP handler back to the runner.
func (r *TunnelRouter) SendData(ctx context.Context, tunnelID, connectionID string, data []byte, eof bool) error {
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
				Data:         data,
				Eof:          eof,
			},
		},
	}

	if err := r.connManager.SendCommand(runnerID, cmd); err != nil {
		return err
	}

	// Traffic in this direction also keeps the connection alive for idle
	// sweeps: a WebSocket may be mostly client-to-runner.
	r.connectionsMu.RLock()
	conn, ok := r.connections[connectionID]
	r.connectionsMu.RUnlock()
	if ok {
		conn.touch()
	}

	r.logger.Debug("sent data to tunnel",
		zap.String("tunnel_id", tunnelID),
		zap.String("connection_id", connectionID),
		zap.String("runner_id", runnerID),
		zap.Int("data_len", len(data)),
		zap.Bool("eof", eof),
	)

	return nil
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

	// Hand the frame to this connection's pump and return. The delivery wait,
	// including the EOF close, happens on the pump so that a stalled tunnel
	// consumer cannot delay this runner's heartbeats or any other tunnel.
	if err := conn.enqueue(data); err != nil {
		switch {
		case errors.Is(err, errConnectionClosed):
			// Consumer went away; nothing left to deliver to.
			r.logger.Debug("dropping data for closed connection",
				zap.String("connection_id", connectionID),
				zap.String("tunnel_id", data.GetTunnelId()),
			)
			return nil

		default:
			// Queue full: the consumer is far enough behind that the pump has
			// already waited out the send timeout. Tear this connection down
			// and keep reading for everyone else.
			r.logger.Warn("tunnel connection queue full, closing connection",
				zap.String("connection_id", connectionID),
				zap.String("tunnel_id", data.GetTunnelId()),
				zap.Int("queue_size", cap(conn.inbox)),
			)
			r.CloseConnection(connectionID)
			return err
		}
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

	// Detach all connections for this tunnel under the lock, then close them
	// outside it: close waits for in-flight sends.
	r.connectionsMu.Lock()
	closing := make([]*tunnelConnection, 0, len(r.connections))
	for connID, conn := range r.connections {
		if conn.tunnelID == tunnelID {
			closing = append(closing, conn)
			delete(r.connections, connID)
		}
	}
	r.connectionsMu.Unlock()

	for _, conn := range closing {
		conn.close()
	}

	// Unregister the tunnel
	r.UnregisterTunnel(tunnelID)

	// Close in TunnelManager if configured
	if r.tm != nil {
		return r.tm.Close(ctx, tunnelID)
	}

	return nil
}

// CleanupIdleConnections closes connections that have carried no data for
// longer than idleFor and returns how many were closed.
//
// Idle time is measured from the last frame, not from creation: an age-based
// sweep kills healthy long-lived WebSocket connections, which is what the
// previous CleanupStaleConnections did.
func (r *TunnelRouter) CleanupIdleConnections(idleFor time.Duration) int {
	cutoff := time.Now().Add(-idleFor)

	r.connectionsMu.Lock()
	closing := make([]*tunnelConnection, 0)
	for connID, conn := range r.connections {
		if conn.idleSince().Before(cutoff) {
			closing = append(closing, conn)
			delete(r.connections, connID)
		}
	}
	r.connectionsMu.Unlock()

	for _, conn := range closing {
		conn.close()
		r.logger.Debug("cleaned up idle tunnel connection",
			zap.String("connection_id", conn.connectionID),
			zap.String("tunnel_id", conn.tunnelID),
			zap.Duration("idle", time.Since(conn.idleSince())),
		)
	}

	return len(closing)
}
