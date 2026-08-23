package grpc

import (
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// Runner status constants.
const (
	RunnerStatusIdle = "idle"
	RunnerStatusBusy = "busy"
)

// commandBufferSize is the maximum number of commands that can be queued per connection.
const commandBufferSize = 100

// ErrRunnerAlreadyConnected is returned when trying to register a runner that is already connected.
var ErrRunnerAlreadyConnected = errors.New("runner already connected")

// ErrRunnerNotFound is returned when a runner is not found in the connection manager.
var ErrRunnerNotFound = errors.New("runner not found")

// RunnerConnection represents an active runner connection.
type RunnerConnection struct {
	RunnerID    string
	Name        string
	Hostname    string
	Status      string // "idle" or "busy"
	ConnectedAt time.Time
	LastSeen    time.Time
	mu          sync.RWMutex

	// commandCh is a buffered channel for outgoing commands to the runner.
	// Commands are sent by the server and consumed by the sendCommands goroutine.
	//
	// commandCh is NEVER closed. Closing it would race with SendCommand, which
	// necessarily publishes to it without holding a lock that the disconnect
	// path also takes, and a send on a closed channel panics the whole process.
	// Teardown is signalled through done instead.
	commandCh chan *pb.ServerCommand

	// done is closed exactly once when the connection is torn down. Both the
	// sender goroutine and SendCommand select on it, so no command is published
	// to a connection that is going away.
	done      chan struct{}
	closeOnce sync.Once

	// stream is the bidirectional gRPC stream for this connection.
	stream grpc.BidiStreamingServer[pb.RunnerMessage, pb.ServerCommand]
}

// newRunnerConnection creates a RunnerConnection with its channels initialized.
func newRunnerConnection(runnerID, name, hostname string, stream grpc.BidiStreamingServer[pb.RunnerMessage, pb.ServerCommand]) *RunnerConnection {
	now := time.Now()
	return &RunnerConnection{
		RunnerID:    runnerID,
		Name:        name,
		Hostname:    hostname,
		Status:      RunnerStatusIdle,
		ConnectedAt: now,
		LastSeen:    now,
		commandCh:   make(chan *pb.ServerCommand, commandBufferSize),
		done:        make(chan struct{}),
		stream:      stream,
	}
}

// Close signals that the connection is torn down. It is safe to call multiple
// times and from multiple goroutines. Queued but unsent commands are discarded.
func (rc *RunnerConnection) Close() {
	rc.closeOnce.Do(func() {
		if rc.done != nil {
			close(rc.done)
		}
	})
}

// Done returns a channel that is closed when the connection is torn down.
// A connection built without newRunnerConnection returns nil, which blocks
// forever in a select and therefore never reports teardown.
func (rc *RunnerConnection) Done() <-chan struct{} {
	return rc.done
}

// UpdateStatus updates the runner status.
func (rc *RunnerConnection) UpdateStatus(status string) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.Status = status
}

// UpdateLastSeen updates the last seen timestamp.
func (rc *RunnerConnection) UpdateLastSeen() {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	rc.LastSeen = time.Now()
}

// GetStatus returns the current status.
func (rc *RunnerConnection) GetStatus() string {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.Status
}

// GetLastSeen returns the last seen timestamp.
func (rc *RunnerConnection) GetLastSeen() time.Time {
	rc.mu.RLock()
	defer rc.mu.RUnlock()
	return rc.LastSeen
}

// ConnectionManager manages active runner connections.
type ConnectionManager struct {
	connections map[string]*RunnerConnection
	mu          sync.RWMutex
	logger      *zap.Logger

	// routeMu guards the cross-replica collaborators, which are attached after
	// construction: the connection manager is built before core.Wire (every
	// manager takes it as a dependency) and the locator comes out of core.Wire.
	routeMu   sync.RWMutex
	locator   RunnerLocator
	forwarder CommandForwarder
	metrics   *RoutingMetrics
}

// NewConnectionManager creates a new ConnectionManager.
func NewConnectionManager(logger *zap.Logger) *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*RunnerConnection),
		logger:      logger,
	}
}

// SetRouter attaches cross-replica routing.
//
// Without it the manager behaves exactly as it did before routing existed:
// every lookup is answered from the local map. That is not a fallback, it is
// the single-process path, and it must stay free of database work.
func (cm *ConnectionManager) SetRouter(locator RunnerLocator, forwarder CommandForwarder, metrics *RoutingMetrics) {
	cm.routeMu.Lock()
	defer cm.routeMu.Unlock()

	cm.locator = locator
	cm.forwarder = forwarder
	cm.metrics = metrics
}

// router returns the attached collaborators, or nils.
func (cm *ConnectionManager) router() (RunnerLocator, CommandForwarder, *RoutingMetrics) {
	cm.routeMu.RLock()
	defer cm.routeMu.RUnlock()

	return cm.locator, cm.forwarder, cm.metrics
}

// Register adds a new runner connection.
// Returns ErrRunnerAlreadyConnected if the runner is already registered.
func (cm *ConnectionManager) Register(runnerID string, conn *RunnerConnection) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if _, exists := cm.connections[runnerID]; exists {
		return ErrRunnerAlreadyConnected
	}

	cm.connections[runnerID] = conn
	cm.logger.Info("runner connected",
		zap.String("runner_id", runnerID),
		zap.String("name", conn.Name),
		zap.String("hostname", conn.Hostname),
	)
	return nil
}

// Unregister removes a runner connection.
func (cm *ConnectionManager) Unregister(runnerID string) {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	if conn, exists := cm.connections[runnerID]; exists {
		delete(cm.connections, runnerID)
		cm.logger.Info("runner disconnected",
			zap.String("runner_id", runnerID),
			zap.String("name", conn.Name),
			zap.Duration("connected_duration", time.Since(conn.ConnectedAt)),
		)
	}
}

// Get retrieves a runner connection by ID.
func (cm *ConnectionManager) Get(runnerID string) (*RunnerConnection, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	conn, exists := cm.connections[runnerID]
	return conn, exists
}

// List returns all connected runners.
func (cm *ConnectionManager) List() []*RunnerConnection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	result := make([]*RunnerConnection, 0, len(cm.connections))
	for _, conn := range cm.connections {
		result = append(result, conn)
	}
	return result
}

// UpdateStatus updates a runner's status.
// Returns ErrRunnerNotFound if the runner is not connected.
func (cm *ConnectionManager) UpdateStatus(runnerID, status string) error {
	cm.mu.RLock()
	conn, exists := cm.connections[runnerID]
	cm.mu.RUnlock()

	if !exists {
		return ErrRunnerNotFound
	}

	conn.UpdateStatus(status)
	cm.logger.Debug("runner status updated",
		zap.String("runner_id", runnerID),
		zap.String("status", status),
	)
	return nil
}

// UpdateLastSeen updates a runner's last seen timestamp.
// Returns ErrRunnerNotFound if the runner is not connected.
func (cm *ConnectionManager) UpdateLastSeen(runnerID string) error {
	cm.mu.RLock()
	conn, exists := cm.connections[runnerID]
	cm.mu.RUnlock()

	if !exists {
		return ErrRunnerNotFound
	}

	conn.UpdateLastSeen()
	return nil
}

// GetByStatus returns all runners with a specific status.
func (cm *ConnectionManager) GetByStatus(status string) []*RunnerConnection {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	var result []*RunnerConnection
	for _, conn := range cm.connections {
		if conn.GetStatus() == status {
			result = append(result, conn)
		}
	}
	return result
}

// Count returns the number of connected runners.
func (cm *ConnectionManager) Count() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return len(cm.connections)
}

// IsConnected checks if a runner is connected, anywhere in the deployment.
//
// Local map first, so a single-process deployment never touches the database
// and a multi-replica one pays a lookup only for runners it does not hold.
// Without the second half, a runner attached to another replica is invisible
// to runner selection: each replica could allocate only from the slice of the
// fleet that happened to connect to it, and a parked session could never be
// served by an idle runner one hop away.
func (cm *ConnectionManager) IsConnected(runnerID string) bool {
	cm.mu.RLock()
	_, exists := cm.connections[runnerID]
	cm.mu.RUnlock()

	if exists {
		return true
	}

	locator, _, _ := cm.router()
	if locator == nil {
		return false
	}
	_, held := locator.Locate(runnerID)
	return held
}

// LocalCount returns how many runners this process is holding, as opposed to
// how many the deployment has.
func (cm *ConnectionManager) LocalCount() int {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	return len(cm.connections)
}

// recordSend meters one send attempt. Nil metrics is the norm in tests and in
// a deployment with observability off.
func (cm *ConnectionManager) recordSend(path string, err error) {
	_, _, metrics := cm.router()
	metrics.send(path, err)
}

// recordHop meters one cross-replica delivery.
func (cm *ConnectionManager) recordHop(d time.Duration) {
	_, _, metrics := cm.router()
	metrics.hop(d)
}

// ErrCommandQueueFull is returned when the command queue is full.
var ErrCommandQueueFull = errors.New("command queue full")

// ErrRunnerDisconnected is returned when a command is dispatched to a
// connection that is being torn down.
var ErrRunnerDisconnected = errors.New("runner disconnected")

// SendCommand queues a command to be sent to a runner, wherever it is attached.
//
// The rule is local first:
//
//  1. this process holds the stream        -> write to it. Nothing else happens.
//  2. a live peer holds it                 -> one hop, and its outcome is the answer.
//  3. otherwise                            -> ErrRunnerNotFound, as before.
//
// Local-first is what keeps a single-process deployment at exactly the cost it
// had before routing existed - no query, no round trip - and what keeps
// TunnelData, which flows per chunk, off the database.
//
// It accepts one window: a runner connected to two replicas at once (a
// partition where this process has not yet noticed its stream is dead) is sent
// to locally although the registry names someone else. If the stream is truly
// dead the write fails, the connection tears down, and the next attempt routes
// correctly; if it is alive the agent is holding two streams, which this layer
// cannot fix and should not paper over. The case is metered.
//
// Returns ErrRunnerNotFound if nothing holds the runner.
// Returns ErrRunnerDisconnected if the connection is being torn down.
// Returns ErrCommandQueueFull if the command queue is full (backpressure).
// Returns ErrReplicaUnreachable if the holder could not be reached.
func (cm *ConnectionManager) SendCommand(runnerID string, cmd *pb.ServerCommand) error {
	err := cm.sendLocal(runnerID, cmd)
	if !errors.Is(err, ErrRunnerNotFound) {
		cm.recordSend(routePathLocal, err)
		return err
	}

	locator, forwarder, _ := cm.router()
	if locator == nil || forwarder == nil {
		cm.recordSend(routePathLocal, err)
		return err
	}

	peer, held := locator.Locate(runnerID)
	if !held {
		cm.recordSend(routePathLocal, err)
		return err
	}

	started := time.Now()
	remoteErr := forwarder.Forward(peer, runnerID, cmd)
	cm.recordHop(time.Since(started))
	cm.recordSend(routePathRemote, remoteErr)
	if remoteErr != nil {
		cm.logger.Warn("could not deliver a command to the replica holding the runner",
			zap.String("runner_id", runnerID),
			zap.String("replica_id", peer.ReplicaID),
			zap.String("addr", peer.Addr),
			zap.Error(remoteErr),
		)
		return remoteErr
	}

	cm.logger.Debug("command forwarded to the replica holding the runner",
		zap.String("runner_id", runnerID),
		zap.String("replica_id", peer.ReplicaID),
	)
	return nil
}

// sendLocal writes to a stream this process holds, and is the whole of the
// old SendCommand. The internal router service calls it directly: a forwarded
// command has already been routed and must not be routed again.
func (cm *ConnectionManager) sendLocal(runnerID string, cmd *pb.ServerCommand) error {
	cm.mu.RLock()
	conn, exists := cm.connections[runnerID]
	cm.mu.RUnlock()

	if !exists {
		return ErrRunnerNotFound
	}

	// The connection can be torn down between the map lookup above and the send
	// below; the read lock is intentionally not held across the send. That is
	// safe because commandCh is never closed - the worst case is a command
	// queued onto a connection whose sender has already exited, which is
	// equivalent to the runner vanishing mid-flight.
	select {
	case <-conn.done:
		return ErrRunnerDisconnected
	default:
	}

	// Non-blocking send with backpressure
	select {
	case conn.commandCh <- cmd:
		cm.logger.Debug("command queued",
			zap.String("runner_id", runnerID),
			zap.String("command_type", fmt.Sprintf("%T", cmd.Payload)),
		)
		return nil
	default:
		cm.logger.Warn("command queue full, applying backpressure",
			zap.String("runner_id", runnerID),
		)
		return ErrCommandQueueFull
	}
}
