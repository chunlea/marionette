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

// RunnerManagerInterface defines the interface for runner management.
// This is used for dependency injection in other components.
type RunnerManagerInterface interface {
	OnConnect(ctx context.Context, runnerID string) error
	OnDisconnect(ctx context.Context, runnerID string) error
	OnHeartbeat(ctx context.Context, runnerID string, hb *pb.Heartbeat) error
	SetStatus(ctx context.Context, runnerID, status string) error
}

// RunnerManager handles runner lifecycle and status transitions.
type RunnerManager struct {
	store       store.Store
	connManager ConnectionManagerInterface
	taskMgr     TaskManagerInterface
	sessionMgr  SessionManagerInterface
	logger      *zap.Logger
	webhooks    *WebhookIntegration
}

// RunnerManagerOption is a functional option for RunnerManager.
type RunnerManagerOption func(*RunnerManager)

// WithTaskManager sets the task manager for the runner manager.
func WithTaskManager(tm TaskManagerInterface) RunnerManagerOption {
	return func(m *RunnerManager) {
		m.taskMgr = tm
	}
}

// WithSessionManager sets the session manager for the runner manager.
func WithSessionManager(sm SessionManagerInterface) RunnerManagerOption {
	return func(m *RunnerManager) {
		m.sessionMgr = sm
	}
}

// SetWebhookIntegration sets the webhook integration for dispatching events.
func (m *RunnerManager) SetWebhookIntegration(wi *WebhookIntegration) {
	m.webhooks = wi
}

// NewRunnerManager creates a new RunnerManager.
func NewRunnerManager(store store.Store, connManager ConnectionManagerInterface, logger *zap.Logger, opts ...RunnerManagerOption) *RunnerManager {
	m := &RunnerManager{
		store:       store,
		connManager: connManager,
		logger:      logger,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

// OnConnect is called when a runner connects to the server.
// Transitions the runner from offline to idle.
// Also checks for resuming sessions that need a runner and auto-attaches.
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

	// Dispatch webhook event
	if m.webhooks != nil {
		// Fetch updated runner for webhook
		if updatedRunner, err := m.store.GetRunner(ctx, runnerID); err == nil {
			m.webhooks.DispatchRunnerEvent(ctx, "runner.connected", updatedRunner, nil)
		}
	}

	// Try to attach to a resuming session
	go m.tryAttachToResumingSession(ctx, runnerID)

	return nil
}

// tryAttachToResumingSession checks for sessions in "resuming" status and attaches the runner.
// This is called asynchronously after a runner connects.
func (m *RunnerManager) tryAttachToResumingSession(ctx context.Context, runnerID string) {
	if m.sessionMgr == nil {
		m.logger.Debug("skipping resuming session check: no session manager configured",
			zap.String("runner_id", runnerID),
		)
		return
	}

	// Look for sessions in "resuming" status that need a runner
	sessions, err := m.store.ListSessions(ctx, store.ListSessionsOptions{
		Status: []string{SessionStatusResuming},
	})
	if err != nil {
		m.logger.Warn("failed to list resuming sessions",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return
	}

	if len(sessions.Items) == 0 {
		m.logger.Debug("no resuming sessions to attach",
			zap.String("runner_id", runnerID),
		)
		return
	}

	// Find first session without a runner that we can attach to
	for _, sess := range sessions.Items {
		// Skip sessions that already have a runner attached
		if sess.RunnerID != nil && *sess.RunnerID != "" {
			continue
		}

		// Try to attach this runner to the session
		m.logger.Info("attaching runner to resuming session",
			zap.String("runner_id", runnerID),
			zap.String("session_id", sess.ID),
		)

		if err := m.sessionMgr.AttachRunner(ctx, sess.ID, runnerID); err != nil {
			m.logger.Warn("failed to attach runner to resuming session",
				zap.String("runner_id", runnerID),
				zap.String("session_id", sess.ID),
				zap.Error(err),
			)
			continue
		}

		m.logger.Info("runner attached to resuming session",
			zap.String("runner_id", runnerID),
			zap.String("session_id", sess.ID),
		)
		return
	}

	m.logger.Debug("no suitable resuming session found for runner",
		zap.String("runner_id", runnerID),
	)
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

	// Dispatch webhook event
	if m.webhooks != nil {
		// Fetch updated runner for webhook
		if runner, err := m.store.GetRunner(ctx, runnerID); err == nil {
			m.webhooks.DispatchRunnerEvent(ctx, "runner.disconnected", runner, nil)
		}
	}

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
// Marks running task runs as failed and triggers retry if applicable.
func (m *RunnerManager) handleInFlightTasks(ctx context.Context, runnerID string) error {
	if m.taskMgr == nil {
		m.logger.Debug("skipping in-flight task handling: no task manager configured",
			zap.String("runner_id", runnerID),
		)
		return nil
	}

	// Get all task runs that were running on this runner
	runs, err := m.store.ListTaskRuns(ctx, store.ListTaskRunsOptions{
		RunnerID: &runnerID,
		Status:   []string{TaskRunStatusAssigned, TaskRunStatusRunning},
	})
	if err != nil {
		return err
	}

	if len(runs.Items) == 0 {
		m.logger.Debug("no in-flight tasks to handle",
			zap.String("runner_id", runnerID),
		)
		return nil
	}

	m.logger.Info("handling in-flight tasks for disconnected runner",
		zap.String("runner_id", runnerID),
		zap.Int("task_runs", len(runs.Items)),
	)

	// Track tasks to check for retry
	tasksToRetry := make(map[string]bool)

	// Mark each run as failed
	for _, run := range runs.Items {
		if err := m.taskMgr.FailRun(ctx, run.ID, "runner disconnected"); err != nil {
			m.logger.Error("failed to mark task run as failed",
				zap.String("run_id", run.ID),
				zap.String("task_id", run.TaskID),
				zap.Error(err),
			)
			continue
		}
		tasksToRetry[run.TaskID] = true
	}

	// Check retry policy for each affected task
	for taskID := range tasksToRetry {
		shouldRetry, err := m.taskMgr.ShouldRetry(ctx, taskID)
		if err != nil {
			m.logger.Error("failed to check retry policy",
				zap.String("task_id", taskID),
				zap.Error(err),
			)
			continue
		}

		if shouldRetry {
			m.logger.Info("task will be retried after runner disconnect",
				zap.String("task_id", taskID),
			)
			// Note: The actual retry will be triggered when a new runner is available
			// and the session is resumed. For now, the task stays in running state.
		}
	}

	return nil
}

// detachSessions detaches all sessions attached to a disconnected runner.
// Suspends sessions and detaches them from the runner.
func (m *RunnerManager) detachSessions(ctx context.Context, runnerID string) error {
	if m.sessionMgr == nil {
		m.logger.Debug("skipping session detachment: no session manager configured",
			zap.String("runner_id", runnerID),
		)
		return nil
	}

	// Get all active sessions attached to this runner
	sessions, err := m.store.ListSessions(ctx, store.ListSessionsOptions{
		RunnerID: &runnerID,
		Status:   []string{SessionStatusActive},
	})
	if err != nil {
		return err
	}

	if len(sessions.Items) == 0 {
		m.logger.Debug("no sessions to detach",
			zap.String("runner_id", runnerID),
		)
		return nil
	}

	m.logger.Info("detaching sessions from disconnected runner",
		zap.String("runner_id", runnerID),
		zap.Int("sessions", len(sessions.Items)),
	)

	// Suspend each session
	for _, sess := range sessions.Items {
		// Use "terminate" strategy for unexpected disconnects
		// (more sophisticated strategies will be implemented in G5)
		if err := m.sessionMgr.Suspend(ctx, sess.ID, "terminate"); err != nil {
			m.logger.Error("failed to suspend session",
				zap.String("session_id", sess.ID),
				zap.String("runner_id", runnerID),
				zap.Error(err),
			)
			continue
		}
		m.logger.Info("session suspended due to runner disconnect",
			zap.String("session_id", sess.ID),
			zap.String("runner_id", runnerID),
		)
	}

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

// Default stale detection configuration.
const (
	DefaultCheckInterval  = 30 * time.Second // How often to check for stale runners
	DefaultStaleThreshold = 90 * time.Second // 3 missed heartbeats (30s interval)
)

// StaleDetector monitors runners and marks stale ones as offline.
type StaleDetector struct {
	store          store.Store
	connManager    ConnectionManagerInterface
	runnerManager  *RunnerManager
	checkInterval  time.Duration
	staleThreshold time.Duration
	logger         *zap.Logger

	stopCh chan struct{}
	doneCh chan struct{}
}

// StaleDetectorOption is a functional option for StaleDetector.
type StaleDetectorOption func(*StaleDetector)

// WithCheckInterval sets the check interval for the stale detector.
func WithCheckInterval(d time.Duration) StaleDetectorOption {
	return func(sd *StaleDetector) {
		sd.checkInterval = d
	}
}

// WithStaleThreshold sets the stale threshold for the stale detector.
func WithStaleThreshold(d time.Duration) StaleDetectorOption {
	return func(sd *StaleDetector) {
		sd.staleThreshold = d
	}
}

// NewStaleDetector creates a new StaleDetector.
func NewStaleDetector(
	store store.Store,
	connManager ConnectionManagerInterface,
	runnerManager *RunnerManager,
	logger *zap.Logger,
	opts ...StaleDetectorOption,
) *StaleDetector {
	sd := &StaleDetector{
		store:          store,
		connManager:    connManager,
		runnerManager:  runnerManager,
		checkInterval:  DefaultCheckInterval,
		staleThreshold: DefaultStaleThreshold,
		logger:         logger,
		stopCh:         make(chan struct{}),
		doneCh:         make(chan struct{}),
	}

	for _, opt := range opts {
		opt(sd)
	}

	return sd
}

// Start begins the background stale detection loop.
func (sd *StaleDetector) Start(ctx context.Context) {
	sd.logger.Info("starting stale detector",
		zap.Duration("check_interval", sd.checkInterval),
		zap.Duration("stale_threshold", sd.staleThreshold),
	)

	go sd.run(ctx)
}

// Stop stops the stale detector.
func (sd *StaleDetector) Stop() {
	sd.logger.Info("stopping stale detector")
	close(sd.stopCh)
	<-sd.doneCh
	sd.logger.Info("stale detector stopped")
}

// run is the main loop for the stale detector.
func (sd *StaleDetector) run(ctx context.Context) {
	defer close(sd.doneCh)

	ticker := time.NewTicker(sd.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-sd.stopCh:
			return
		case <-ticker.C:
			if err := sd.checkStaleRunners(ctx); err != nil {
				sd.logger.Error("failed to check stale runners", zap.Error(err))
			}
		}
	}
}

// checkStaleRunners checks all connected runners and marks stale ones as offline.
func (sd *StaleDetector) checkStaleRunners(ctx context.Context) error {
	// Get all runners that are online (idle, busy, or paused)
	runners, err := sd.store.ListRunners(ctx, store.ListRunnersOptions{
		Status: []string{StatusIdle, StatusBusy, StatusPaused},
	})
	if err != nil {
		return err
	}

	now := time.Now()
	staleCount := 0

	for _, runner := range runners.Items {
		// Check if runner has been seen recently
		if runner.LastSeenAt == nil {
			// No last_seen, assume stale if not connected
			if sd.connManager != nil && !sd.connManager.IsConnected(runner.ID) {
				if err := sd.markStale(ctx, runner.ID, "no heartbeat received"); err != nil {
					sd.logger.Error("failed to mark runner as stale",
						zap.String("runner_id", runner.ID),
						zap.Error(err),
					)
				} else {
					staleCount++
				}
			}
			continue
		}

		// Check if last seen is beyond threshold
		age := now.Sub(*runner.LastSeenAt)
		if age > sd.staleThreshold {
			sd.logger.Warn("runner is stale",
				zap.String("runner_id", runner.ID),
				zap.String("name", runner.Name),
				zap.Duration("age", age),
				zap.Time("last_seen", *runner.LastSeenAt),
			)

			if err := sd.markStale(ctx, runner.ID, "heartbeat timeout"); err != nil {
				sd.logger.Error("failed to mark runner as stale",
					zap.String("runner_id", runner.ID),
					zap.Error(err),
				)
			} else {
				staleCount++
			}
		}
	}

	if staleCount > 0 {
		sd.logger.Info("marked stale runners as offline",
			zap.Int("count", staleCount),
		)
	}

	return nil
}

// markStale marks a runner as offline due to staleness.
func (sd *StaleDetector) markStale(ctx context.Context, runnerID, reason string) error {
	sd.logger.Info("marking runner as stale",
		zap.String("runner_id", runnerID),
		zap.String("reason", reason),
	)

	// Use runner manager to handle disconnect (handles in-flight tasks, sessions, etc.)
	if sd.runnerManager != nil {
		return sd.runnerManager.OnDisconnect(ctx, runnerID)
	}

	// Fallback: directly update status
	updates := store.RunnerUpdates{
		Status: stringPtr(StatusOffline),
	}
	return sd.store.UpdateRunner(ctx, runnerID, updates)
}
