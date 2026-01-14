package pool

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// TaintReason represents reasons for tainting a runner.
type TaintReason string

const (
	TaintReasonScriptFailed      TaintReason = "script_failed"
	TaintReasonMaxTasksReached   TaintReason = "max_tasks_reached"
	TaintReasonSessionCrashed    TaintReason = "session_crashed"
	TaintReasonAgentCrashed      TaintReason = "agent_crashed"
	TaintReasonStateCorrupted    TaintReason = "state_corrupted"
	TaintReasonManualTaint       TaintReason = "manual"
	TaintReasonHealthCheckFailed TaintReason = "health_check_failed"
	TaintReasonStale             TaintReason = "stale"
)

// TaintStore is the subset of store.Store needed by the taint manager.
type TaintStore interface {
	ListRunners(ctx context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error)
	UpdateRunner(ctx context.Context, id string, updates store.RunnerUpdates) error
}

// TaintManager handles taint detection and cleanup for pool runners.
type TaintManager struct {
	store  TaintStore
	logger *zap.Logger
}

// NewTaintManager creates a new taint manager.
func NewTaintManager(st TaintStore, logger *zap.Logger) *TaintManager {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &TaintManager{
		store:  st,
		logger: logger,
	}
}

// TaintRunner marks a runner as tainted with a reason.
func (m *TaintManager) TaintRunner(ctx context.Context, runnerID string, reason TaintReason, details string) error {
	tainted := true
	reasonStr := string(reason)
	if details != "" {
		reasonStr = fmt.Sprintf("%s: %s", reason, details)
	}
	status := "offline"

	err := m.store.UpdateRunner(ctx, runnerID, store.RunnerUpdates{
		Tainted:     &tainted,
		TaintReason: &reasonStr,
		Status:      &status,
	})
	if err != nil {
		return fmt.Errorf("tainting runner: %w", err)
	}

	m.logger.Warn("runner tainted",
		zap.String("runner_id", runnerID),
		zap.String("reason", reasonStr),
	)

	return nil
}

// UntaintRunner removes the taint from a runner.
func (m *TaintManager) UntaintRunner(ctx context.Context, runnerID string) error {
	tainted := false
	emptyReason := ""

	err := m.store.UpdateRunner(ctx, runnerID, store.RunnerUpdates{
		Tainted:     &tainted,
		TaintReason: &emptyReason,
	})
	if err != nil {
		return fmt.Errorf("untainting runner: %w", err)
	}

	m.logger.Info("runner untainted",
		zap.String("runner_id", runnerID),
	)

	return nil
}

// ListTaintedRunners returns all tainted runners in a pool.
func (m *TaintManager) ListTaintedRunners(ctx context.Context, poolName string) ([]*store.Runner, error) {
	tainted := true
	result, err := m.store.ListRunners(ctx, store.ListRunnersOptions{
		PoolName: &poolName,
		Tainted:  &tainted,
	})
	if err != nil {
		return nil, fmt.Errorf("listing tainted runners: %w", err)
	}

	return result.Items, nil
}

// CleanupTaintedRunners removes tainted runners that have been offline for a while.
func (m *TaintManager) CleanupTaintedRunners(ctx context.Context, poolName string, offlineThreshold time.Duration) (int, error) {
	runners, err := m.ListTaintedRunners(ctx, poolName)
	if err != nil {
		return 0, err
	}

	now := time.Now()
	cleaned := 0

	for _, runner := range runners {
		// Check if runner has been offline long enough
		if runner.LastSeenAt != nil {
			if now.Sub(*runner.LastSeenAt) < offlineThreshold {
				continue
			}
		}

		// Mark runner as offline (soft delete)
		status := "offline"
		if err := m.store.UpdateRunner(ctx, runner.ID, store.RunnerUpdates{
			Status: &status,
		}); err != nil {
			m.logger.Warn("failed to cleanup tainted runner",
				zap.String("runner_id", runner.ID),
				zap.Error(err),
			)
			continue
		}

		m.logger.Info("cleaned up tainted runner",
			zap.String("runner_id", runner.ID),
			zap.String("runner_name", runner.Name),
			zap.String("taint_reason", pointerToString(runner.TaintReason)),
		)
		cleaned++
	}

	return cleaned, nil
}

// DetectScriptFailure taints a runner if script execution fails.
func (m *TaintManager) DetectScriptFailure(ctx context.Context, runnerID string, scriptType ScriptType, exitCode int, stderr string) error {
	details := fmt.Sprintf("%s script failed with exit code %d", scriptType, exitCode)
	if stderr != "" {
		// Truncate stderr for the taint reason
		if len(stderr) > 200 {
			stderr = stderr[:200] + "..."
		}
		details = fmt.Sprintf("%s: %s", details, stderr)
	}

	return m.TaintRunner(ctx, runnerID, TaintReasonScriptFailed, details)
}

// DetectMaxTasksReached taints a runner when it reaches max tasks.
func (m *TaintManager) DetectMaxTasksReached(ctx context.Context, runnerID string, taskCount, maxTasks int) error {
	details := fmt.Sprintf("reached %d/%d tasks", taskCount, maxTasks)
	return m.TaintRunner(ctx, runnerID, TaintReasonMaxTasksReached, details)
}

// DetectSessionCrash taints a runner when a session crashes unexpectedly.
func (m *TaintManager) DetectSessionCrash(ctx context.Context, runnerID, sessionID, reason string) error {
	details := fmt.Sprintf("session %s: %s", sessionID, reason)
	return m.TaintRunner(ctx, runnerID, TaintReasonSessionCrashed, details)
}

// DetectHealthCheckFailure taints a runner when health check fails.
func (m *TaintManager) DetectHealthCheckFailure(ctx context.Context, runnerID string, reason string) error {
	return m.TaintRunner(ctx, runnerID, TaintReasonHealthCheckFailed, reason)
}

// DetectStaleRunner taints a runner that hasn't sent heartbeat.
func (m *TaintManager) DetectStaleRunner(ctx context.Context, runnerID string, lastSeen time.Time) error {
	details := fmt.Sprintf("last seen at %s", lastSeen.Format(time.RFC3339))
	return m.TaintRunner(ctx, runnerID, TaintReasonStale, details)
}

// pointerToString safely converts a *string to string.
func pointerToString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
