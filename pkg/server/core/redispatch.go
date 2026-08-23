package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Redispatch defaults.
const (
	// DefaultRedispatchInterval is how often the sweeper looks for work that
	// nothing else noticed. It is the backstop, not the primary path, so it is
	// deliberately slow: every edge trigger fires long before this does.
	DefaultRedispatchInterval = 60 * time.Second

	// DefaultRedispatchMaxAttempts is how many consecutive failed dispatches a
	// task gets before automatic redispatch gives up on it.
	//
	// Giving up means leaving the task pending with a recorded reason - which
	// is exactly where tasks sat before any of this existed. The fallback is
	// the status quo, so the blast radius of a bad decision here is bounded by
	// construction.
	DefaultRedispatchMaxAttempts = 6
)

// redispatchBackoff is the delay before attempt n (1-based), capped.
//
// It lives on the task row rather than in memory: an in-memory schedule resets
// for every task at once on restart, which turns a restart into a stampede
// against whatever was failing in the first place.
func redispatchBackoff(attempt int) time.Duration {
	schedule := []time.Duration{
		5 * time.Second,
		15 * time.Second,
		45 * time.Second,
		2 * time.Minute,
		5 * time.Minute,
	}
	if attempt < 1 {
		attempt = 1
	}
	if attempt > len(schedule) {
		return schedule[len(schedule)-1]
	}
	return schedule[attempt-1]
}

// recordDispatchFailure advances a task's redispatch backoff, parking it once
// the budget is spent.
//
// This is the infrastructure budget and is deliberately separate from
// max_retries: unwindDispatch refunds the retry budget because a send failure
// is not the agent's fault, and a poisoned task must not be able to exhaust the
// user's retries by failing to reach a runner.
func (m *TaskManager) recordDispatchFailure(ctx context.Context, taskID string, cause error) {
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		m.logger.Warn("could not record a dispatch failure",
			zap.String("task_id", taskID), zap.Error(err))
		return
	}

	m.redispatchMetrics.dispatchFailed()

	attempts := task.DispatchAttempts + 1
	updates := store.TaskUpdates{DispatchAttempts: &attempts}

	if attempts >= m.redispatchMaxAttempts {
		reason := fmt.Sprintf("gave up after %d dispatch attempts: %v", attempts, cause)
		updates.DispatchParkedReason = &reason
		m.redispatchMetrics.taskParked()
		m.logger.Warn("parking task: automatic redispatch gave up",
			zap.String("task_id", taskID),
			zap.Int("attempts", attempts),
			zap.Error(cause),
		)
	} else {
		next := time.Now().Add(redispatchBackoff(attempts))
		updates.NextDispatchAfter = &next
		m.logger.Info("task dispatch failed; backing off",
			zap.String("task_id", taskID),
			zap.Int("attempt", attempts),
			zap.Time("next_attempt_after", next),
			zap.Error(cause),
		)
	}

	if err := m.store.UpdateTask(ctx, taskID, updates); err != nil {
		m.logger.Warn("could not persist the dispatch backoff",
			zap.String("task_id", taskID), zap.Error(err))
	}
}

// eligibleForRedispatch reports whether automatic redispatch may act on a task.
//
// Edge triggers pass ignoreBackoff: a runner genuinely becoming available is
// new information, so it earns one immediate attempt rather than waiting out a
// timer set by an unrelated failure. Nothing bypasses the parked state.
func eligibleForRedispatch(task *store.Task, ignoreBackoff bool, now time.Time) bool {
	if task.Status != TaskStatusPending {
		return false
	}
	if task.DispatchParkedReason != nil && *task.DispatchParkedReason != "" {
		return false
	}
	if ignoreBackoff || task.NextDispatchAfter == nil {
		return true
	}
	return !now.Before(*task.NextDispatchAfter)
}

// RedispatchSweeper is the backstop trigger.
//
// Every other trigger is an edge: a task was created, a runner was freed, a
// runner joined. Edges are missed - a server restarts and loses its in-memory
// state, a runner appears in a way nothing watches - and a task that nothing
// ever retries is the bug this whole mechanism exists to fix.
//
// It owns a timer and nothing else. The scan itself lives in DispatchWaker, so
// a tick that lands in the middle of a runner-freed pass coalesces into it
// instead of walking the same sessions a second time.
type RedispatchSweeper struct {
	waker    *DispatchWaker
	logger   *zap.Logger
	interval time.Duration

	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
	running bool
}

// RedispatchSweeperOption is a functional option for RedispatchSweeper.
type RedispatchSweeperOption func(*RedispatchSweeper)

// WithRedispatchInterval sets how often the sweeper runs.
func WithRedispatchInterval(d time.Duration) RedispatchSweeperOption {
	return func(s *RedispatchSweeper) {
		s.interval = d
	}
}

// NewRedispatchSweeper creates a RedispatchSweeper.
func NewRedispatchSweeper(
	waker *DispatchWaker,
	logger *zap.Logger,
	opts ...RedispatchSweeperOption,
) *RedispatchSweeper {
	if logger == nil {
		logger = zap.NewNop()
	}
	sweeper := &RedispatchSweeper{
		waker:    waker,
		logger:   logger,
		interval: DefaultRedispatchInterval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
	for _, opt := range opts {
		opt(sweeper)
	}
	return sweeper
}

// Start begins the sweep loop.
func (s *RedispatchSweeper) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	s.logger.Info("starting redispatch sweeper", zap.Duration("interval", s.interval))
	go s.run(ctx)
}

// Stop stops the sweeper and waits for the loop to finish.
func (s *RedispatchSweeper) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	<-s.doneCh
	s.logger.Info("redispatch sweeper stopped")
}

func (s *RedispatchSweeper) run(ctx context.Context) {
	defer close(s.doneCh)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			if err := s.Sweep(ctx); err != nil {
				s.logger.Error("redispatch sweep failed", zap.Error(err))
			}
		}
	}
}

// Sweep performs one pass. Exported so tests can drive it directly rather than
// waiting on a timer.
func (s *RedispatchSweeper) Sweep(ctx context.Context) error {
	if s.waker == nil {
		return nil
	}
	return s.waker.WakeAndWait(ctx, WakeTriggerSweep)
}
