package grpc

import (
	"errors"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Runner status constants.
const (
	RunnerStatusIdle = "idle"
	RunnerStatusBusy = "busy"
)

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
