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
}

// NewConnectionManager creates a new ConnectionManager.
func NewConnectionManager(logger *zap.Logger) *ConnectionManager {
	return &ConnectionManager{
		connections: make(map[string]*RunnerConnection),
		logger:      logger,
	}
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

// IsConnected checks if a runner is connected.
func (cm *ConnectionManager) IsConnected(runnerID string) bool {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	_, exists := cm.connections[runnerID]
	return exists
}

// ErrCommandQueueFull is returned when the command queue is full.
var ErrCommandQueueFull = errors.New("command queue full")

// ErrRunnerDisconnected is returned when a command is dispatched to a
// connection that is being torn down.
var ErrRunnerDisconnected = errors.New("runner disconnected")

// SendCommand queues a command to be sent to a runner.
// Returns ErrRunnerNotFound if the runner is not connected.
// Returns ErrRunnerDisconnected if the connection is being torn down.
// Returns ErrCommandQueueFull if the command queue is full (backpressure).
func (cm *ConnectionManager) SendCommand(runnerID string, cmd *pb.ServerCommand) error {
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
