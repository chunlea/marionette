package core

import (
	"context"
	"errors"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Runner status constants.
const (
	StatusOffline = "offline"
	StatusIdle    = "idle"
	StatusBusy    = "busy"
	StatusPaused  = "paused"
)

// ErrInvalidStatusTransition is returned when an invalid status transition is attempted.
var ErrInvalidStatusTransition = errors.New("invalid status transition")

// ConnectionManagerInterface defines the interface for connection management.
// This is implemented by grpc.ConnectionManager.
type ConnectionManagerInterface interface {
	IsConnected(runnerID string) bool
	UpdateLastSeen(runnerID string) error
}

// RunnerManager handles runner lifecycle and status transitions.
type RunnerManager struct {
	store       store.Store
	connManager ConnectionManagerInterface
	logger      *zap.Logger
}

// NewRunnerManager creates a new RunnerManager.
func NewRunnerManager(store store.Store, connManager ConnectionManagerInterface, logger *zap.Logger) *RunnerManager {
	return &RunnerManager{
		store:       store,
		connManager: connManager,
		logger:      logger,
	}
}

// OnConnect is called when a runner connects to the server.
// Transitions the runner from offline to idle.
func (m *RunnerManager) OnConnect(ctx context.Context, runnerID string) error {
	m.logger.Info("runner connecting",
		zap.String("runner_id", runnerID),
	)

	// Get current runner status
	runner, err := m.store.GetRunner(ctx, runnerID)
	if err != nil {
		return err
	}

	// Validate transition
	if !isValidTransition(runner.Status, StatusIdle) {
		m.logger.Warn("invalid status transition on connect",
			zap.String("runner_id", runnerID),
			zap.String("from", runner.Status),
			zap.String("to", StatusIdle),
		)
		// Allow anyway for reconnection scenarios
	}

	// Update status to idle
	now := time.Now()
	updates := store.RunnerUpdates{
		Status:     stringPtr(StatusIdle),
		LastSeenAt: &now,
	}

	if err := m.store.UpdateRunner(ctx, runnerID, updates); err != nil {
		m.logger.Error("failed to update runner status on connect",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return err
	}

	m.logger.Info("runner connected",
		zap.String("runner_id", runnerID),
		zap.String("status", StatusIdle),
	)

	return nil
}

// OnDisconnect is called when a runner disconnects from the server.
// Transitions the runner to offline status.
func (m *RunnerManager) OnDisconnect(ctx context.Context, runnerID string) error {
	m.logger.Info("runner disconnecting",
		zap.String("runner_id", runnerID),
	)

	// Update status to offline
	updates := store.RunnerUpdates{
		Status: stringPtr(StatusOffline),
	}

	if err := m.store.UpdateRunner(ctx, runnerID, updates); err != nil {
		m.logger.Error("failed to update runner status on disconnect",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return err
	}

	// Handle any in-flight tasks (G3: mark as failed, check retry policy)
	if err := m.handleInFlightTasks(ctx, runnerID); err != nil {
		m.logger.Warn("failed to handle in-flight tasks",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
	}

	// Detach any sessions attached to this runner (G3)
	if err := m.detachSessions(ctx, runnerID); err != nil {
		m.logger.Warn("failed to detach sessions",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
	}

	m.logger.Info("runner disconnected",
		zap.String("runner_id", runnerID),
		zap.String("status", StatusOffline),
	)

	return nil
}

// OnHeartbeat is called when a heartbeat is received from a runner.
// Updates last_seen timestamp and optionally status from heartbeat.
func (m *RunnerManager) OnHeartbeat(ctx context.Context, runnerID string, hb *pb.Heartbeat) error {
	m.logger.Debug("heartbeat received",
		zap.String("runner_id", runnerID),
		zap.String("status", hb.GetStatus()),
	)

	now := time.Now()
	updates := store.RunnerUpdates{
		LastSeenAt: &now,
	}

	// If heartbeat includes status, update it (with validation)
	if hb.GetStatus() != "" {
		runner, err := m.store.GetRunner(ctx, runnerID)
		if err != nil {
			return err
		}

		if isValidTransition(runner.Status, hb.GetStatus()) {
			updates.Status = stringPtr(hb.GetStatus())
		} else {
			m.logger.Debug("ignoring invalid status from heartbeat",
				zap.String("runner_id", runnerID),
				zap.String("current", runner.Status),
				zap.String("requested", hb.GetStatus()),
			)
		}
	}

	if err := m.store.UpdateRunner(ctx, runnerID, updates); err != nil {
		m.logger.Error("failed to update runner on heartbeat",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return err
	}

	// Also update connection manager's last seen
	if m.connManager != nil {
		_ = m.connManager.UpdateLastSeen(runnerID)
	}

	return nil
}

// SetStatus updates a runner's status with validation.
func (m *RunnerManager) SetStatus(ctx context.Context, runnerID, status string) error {
	runner, err := m.store.GetRunner(ctx, runnerID)
	if err != nil {
		return err
	}

	if !isValidTransition(runner.Status, status) {
		return ErrInvalidStatusTransition
	}

	updates := store.RunnerUpdates{
		Status: &status,
	}

	if err := m.store.UpdateRunner(ctx, runnerID, updates); err != nil {
		return err
	}

	m.logger.Info("runner status changed",
		zap.String("runner_id", runnerID),
		zap.String("from", runner.Status),
		zap.String("to", status),
	)

	return nil
}

// handleInFlightTasks handles tasks that were running on a disconnected runner.
// G3: Will mark running tasks as failed and check retry policy.
//
//nolint:unparam // Stub always returns nil; will be implemented in G3
func (m *RunnerManager) handleInFlightTasks(_ context.Context, runnerID string) error {
	m.logger.Debug("handling in-flight tasks (stub)",
		zap.String("runner_id", runnerID),
	)
	// G3: Implement task failure handling
	// - Get all task_runs with status="running" and runner_id=runnerID
	// - Mark them as failed with error="runner disconnected"
	// - Check retry policy and requeue if applicable
	return nil
}

// detachSessions detaches all sessions attached to a disconnected runner.
// G3: Will update sessions to suspended status.
//
//nolint:unparam // Stub always returns nil; will be implemented in G3
func (m *RunnerManager) detachSessions(_ context.Context, runnerID string) error {
	m.logger.Debug("detaching sessions (stub)",
		zap.String("runner_id", runnerID),
	)
	// G3: Implement session detachment
	// - Get all sessions with runner_id=runnerID
	// - Set runner_id=NULL and status=suspended
	// - Trigger suspend strategy if configured
	return nil
}

// isValidTransition checks if a status transition is valid according to the state machine.
//
// Valid transitions:
//   - offline -> idle (on connect)
//   - idle -> busy (on task start, G3)
//   - busy -> idle (on task complete, G3)
//   - * -> offline (on disconnect)
//   - * -> paused (on pause command)
//   - paused -> idle (on unpause)
func isValidTransition(from, to string) bool {
	// Allow any transition to offline (disconnect)
	if to == StatusOffline {
		return true
	}

	// Allow any transition to paused
	if to == StatusPaused {
		return true
	}

	switch from {
	case StatusOffline:
		// Can only go to idle on connect
		return to == StatusIdle

	case StatusIdle:
		// Can go to busy (start task) or stay idle
		return to == StatusBusy || to == StatusIdle

	case StatusBusy:
		// Can go to idle (task complete) or stay busy
		return to == StatusIdle || to == StatusBusy

	case StatusPaused:
		// Can only go to idle on unpause
		return to == StatusIdle

	default:
		// Unknown status, allow any transition
		return true
	}
}

// IsValidTransition is exported for testing.
func IsValidTransition(from, to string) bool {
	return isValidTransition(from, to)
}

// stringPtr returns a pointer to a string.
func stringPtr(s string) *string {
	return &s
}
