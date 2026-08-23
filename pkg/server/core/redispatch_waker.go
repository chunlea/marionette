package core

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Wake pass bounds.
const (
	// redispatchBatch bounds how many sessions one page of a pass reads.
	redispatchBatch = 50

	// redispatchMaxPages bounds how many pages a pass reads. Without a bound a
	// deployment with a large backlog turns every idle runner into a full scan
	// of the sessions table; with only a single page, session 51 starves
	// forever. Hitting the cap is logged rather than silently truncated.
	redispatchMaxPages = 4

	// redispatchNoRunnerStreak stops a pass after this many consecutive
	// sessions failed to get a runner.
	//
	// The overwhelmingly common reason a parked session cannot be woken is that
	// there is no spare capacity at all, and in that case every remaining
	// session in the pass will fail the same way. Three in a row is enough
	// evidence to stop paying for the rest.
	redispatchNoRunnerStreak = 3
)

// sessionRunnerEnsurer is the slice of SessionManager a wake needs: it hands a
// parked session a runner, applying the profile selector, capability and tenant
// rules that live in the session manager. The waker deliberately does not
// select runners itself - two implementations of "which runner may this session
// have" is exactly how a tenant boundary gets crossed.
type sessionRunnerEnsurer interface {
	EnsureRunner(ctx context.Context, sessionID string) (*store.Session, error)
}

// RunnerAvailableNotifier is what the runner and session managers call when a
// runner becomes available. It exists so those managers depend on the wake, not
// on the whole waker.
type RunnerAvailableNotifier interface {
	RunnerAvailable(ctx context.Context, trigger string)
}

// DispatchWaker is the edge-trigger half of automatic redispatch.
//
// The dispatch CAS in TaskManager.dispatch is the correctness anchor: it makes
// the pending->running transition conditional, so the database arbitrates and a
// second dispatcher simply loses. Triggers therefore never need to be careful.
// They only need to be *cheap*, because everything that frees a runner fires
// one: a session suspending, a session terminating, a runner finishing a task,
// a runner reconnecting, and the sweep timer - often all around a single event.
//
// Cheapness comes from coalescing. At most one pass runs at a time; wakes that
// arrive during a pass set a flag that runs exactly one more pass afterwards,
// no matter how many arrived. A storm of triggers therefore costs two passes,
// and a pass that finds nothing is two queries.
type DispatchWaker struct {
	store      store.Store
	sessions   sessionRunnerEnsurer
	tasks      TaskManagerInterface
	background *backgroundTasks
	metrics    *RedispatchMetrics
	logger     *zap.Logger

	mu       sync.Mutex
	inFlight bool
	pending  bool
	// pendingTrigger attributes the coalesced pass to whatever asked for it
	// last, so the "sessions woken" counter is not credited to the trigger that
	// happened to arrive first.
	pendingTrigger string
}

// NewDispatchWaker creates a DispatchWaker. metrics may be nil.
func NewDispatchWaker(
	s store.Store,
	sessions sessionRunnerEnsurer,
	tasks TaskManagerInterface,
	background *backgroundTasks,
	metrics *RedispatchMetrics,
	logger *zap.Logger,
) *DispatchWaker {
	if logger == nil {
		logger = zap.NewNop()
	}
	if background == nil {
		background = newBackgroundTasks(context.Background(), 0, logger)
	}
	return &DispatchWaker{
		store:      s,
		sessions:   sessions,
		tasks:      tasks,
		background: background,
		metrics:    metrics,
		logger:     logger,
	}
}

// RunnerAvailable asks for a pass and returns immediately.
//
// Callers are on the path of something else - a suspend, a gRPC status report -
// and must not wait for a scan of the session table. A wake that arrives while
// a pass is running is folded into a single follow-up pass.
func (w *DispatchWaker) RunnerAvailable(_ context.Context, trigger string) {
	w.metrics.wakeRequested(trigger)

	w.mu.Lock()
	if w.inFlight {
		w.pending = true
		w.pendingTrigger = trigger
		w.mu.Unlock()
		w.metrics.wakeCoalesced(trigger)
		return
	}
	w.inFlight = true
	w.mu.Unlock()

	// Deliberately not on the caller's context: the pass outlives the request
	// that freed the runner, and cancelling it when that request returns is a
	// race the pass usually loses.
	if !w.background.Go("redispatch-wake", func(bgCtx context.Context) {
		// drain logs every failure it sees; there is nobody here to return one
		// to, which is what "fire and forget" means for an edge trigger.
		_ = w.drain(bgCtx, trigger)
	}) {
		// A saturated or shutting-down pool must not leave the gate closed:
		// nothing would ever reopen it and every later wake would be swallowed.
		w.mu.Lock()
		w.inFlight = false
		w.pending = false
		w.mu.Unlock()
		w.metrics.passCompleted(passOutcomeFailed)
	}
}

// WakeAndWait runs a pass on the calling goroutine, coalescing with any pass
// already in flight. It is what the sweeper's timer calls: the sweeper has its
// own goroutine and wants the error.
//
// Returning nil when another pass is already running is the point, not a
// weakness: that pass covers the same ground, and the follow-up flag guarantees
// one more after it.
func (w *DispatchWaker) WakeAndWait(ctx context.Context, trigger string) error {
	w.metrics.wakeRequested(trigger)

	w.mu.Lock()
	if w.inFlight {
		w.pending = true
		w.pendingTrigger = trigger
		w.mu.Unlock()
		w.metrics.wakeCoalesced(trigger)
		return nil
	}
	w.inFlight = true
	w.mu.Unlock()

	return w.drain(ctx, trigger)
}

// drain runs passes until no wake arrived during the last one.
func (w *DispatchWaker) drain(ctx context.Context, trigger string) error {
	var lastErr error
	for {
		woken, err := w.pass(ctx, trigger)
		switch {
		case err != nil:
			lastErr = err
			w.metrics.passCompleted(passOutcomeFailed)
			w.logger.Warn("redispatch pass failed",
				zap.String("trigger", trigger), zap.Error(err))
		case woken > 0:
			w.metrics.passCompleted(passOutcomeDispatched)
			w.logger.Info("redispatch woke parked work",
				zap.String("trigger", trigger), zap.Int("sessions", woken))
		default:
			// The common case, and the one that has to stay free: every
			// trigger fires optimistically and most find nothing.
			w.metrics.passCompleted(passOutcomeEmpty)
		}

		w.mu.Lock()
		if !w.pending || ctx.Err() != nil {
			w.inFlight = false
			w.pending = false
			w.mu.Unlock()
			return lastErr
		}
		w.pending = false
		trigger = w.pendingTrigger
		w.mu.Unlock()
	}
}

// pass performs one scan and reports how many sessions it moved.
//
// It looks at sessions rather than at tasks: a session is where the
// one-task-in-flight invariant lives, so scanning sessions bounds the work per
// tick for free and cannot dispatch two tasks into the same workspace.
//
// Suspended sessions are deliberately absent. Waking one costs a runner
// acquisition and possibly a workspace restore, and that stays an explicit user
// action - the proposal's non-goal, restated as a query.
func (w *DispatchWaker) pass(ctx context.Context, trigger string) (int, error) {
	// The sweep is a timer, so it honours the backoff a failure recorded. Edge
	// triggers do not: a runner genuinely becoming available is new
	// information, and it earns one immediate attempt.
	ignoreBackoff := trigger != WakeTriggerSweep

	woken := 0
	noRunnerStreak := 0
	cursor := ""

	for page := 0; page < redispatchMaxPages; page++ {
		sessions, err := w.store.ListSessions(ctx, store.ListSessionsOptions{
			BaseListOptions: store.BaseListOptions{Limit: redispatchBatch, Cursor: cursor},
			Status:          []string{SessionStatusPending, SessionStatusActive},
		})
		if err != nil {
			return woken, err
		}

		for _, session := range sessions.Items {
			if ctx.Err() != nil {
				return woken, ctx.Err()
			}

			next, ok := w.dueTask(ctx, session.ID)
			if !ok || !eligibleForRedispatch(next, ignoreBackoff, time.Now()) {
				continue
			}

			runnerID, err := w.ensureRunner(ctx, session)
			if err != nil {
				if errors.Is(err, ErrNoRunnerAvailable) {
					noRunnerStreak++
					if noRunnerStreak >= redispatchNoRunnerStreak {
						w.logger.Debug("stopping redispatch pass: no capacity",
							zap.String("trigger", trigger),
							zap.Int("sessions_woken", woken),
						)
						return woken, nil
					}
				}
				continue
			}
			if runnerID == "" {
				// Nothing to dispatch to and nothing to complain about: the
				// session is mid-resume, or an allocation is already running.
				continue
			}
			noRunnerStreak = 0

			if w.dispatch(ctx, session.ID, ignoreBackoff) {
				woken++
				w.metrics.sessionWoken(trigger)
			}
		}

		if !sessions.HasMore || sessions.NextCursor == "" {
			return woken, nil
		}
		cursor = sessions.NextCursor
	}

	w.logger.Info("redispatch pass hit its page budget; the rest waits for the next wake",
		zap.String("trigger", trigger),
		zap.Int("pages", redispatchMaxPages),
		zap.Int("sessions_woken", woken),
	)
	return woken, nil
}

// dueTask returns the task a dispatch would pick for a session, and whether
// there is one to pick.
//
// Pending and running are read in one query on purpose. A session with a task
// in flight is skipped, and finding that out with a second query would double
// the cost of the case that dominates: a wake that has nothing to do.
func (w *DispatchWaker) dueTask(ctx context.Context, sessionID string) (*store.Task, bool) {
	tasks, err := w.store.ListTasks(ctx, store.ListTasksOptions{
		BaseListOptions: store.BaseListOptions{Limit: dispatchScanLimit},
		SessionID:       &sessionID,
		Status:          []string{TaskStatusPending, TaskStatusRunning},
	})
	if err != nil {
		w.logger.Warn("could not read a session's backlog",
			zap.String("session_id", sessionID), zap.Error(err))
		return nil, false
	}

	pending := make([]*store.Task, 0, len(tasks.Items))
	for _, task := range tasks.Items {
		if task.Status == TaskStatusRunning {
			// Tasks within a session are sequential; the next one goes out
			// when this finishes.
			return nil, false
		}
		pending = append(pending, task)
	}

	// The oldest pending task is what DispatchNext will pick, so it is the one
	// eligibility has to be judged on. Asking "is any task due" instead would
	// wake a session whose oldest task is parked, every single tick, and the
	// dispatch would then correctly do nothing - which is the trigger noise
	// these counters exist to expose.
	next := oldestTask(pending)
	return next, next != nil
}

// ensureRunner gives a session a runner if it has none, and reports the runner
// it ended up with.
func (w *DispatchWaker) ensureRunner(ctx context.Context, session *store.Session) (string, error) {
	if session.RunnerID != nil && *session.RunnerID != "" {
		return *session.RunnerID, nil
	}
	if w.sessions == nil {
		return "", nil
	}

	updated, err := w.sessions.EnsureRunner(ctx, session.ID)
	if err != nil {
		w.metrics.allocationFailed()
		w.logger.Debug("no runner for a parked session",
			zap.String("session_id", session.ID), zap.Error(err))
		return "", err
	}
	if updated == nil || updated.RunnerID == nil || *updated.RunnerID == "" {
		return "", nil
	}

	w.metrics.runnerAllocated()
	w.logger.Info("parked session woken onto a runner",
		zap.String("session_id", session.ID),
		zap.String("runner_id", *updated.RunnerID),
	)
	return *updated.RunnerID, nil
}

// dispatch sends the session's next task, reporting whether it went out.
func (w *DispatchWaker) dispatch(ctx context.Context, sessionID string, ignoreBackoff bool) bool {
	var err error
	if ignoreBackoff {
		err = w.tasks.DispatchNextNow(ctx, sessionID)
	} else {
		err = w.tasks.DispatchNext(ctx, sessionID)
	}
	if err != nil {
		w.logger.Warn("redispatch failed",
			zap.String("session_id", sessionID), zap.Error(err))
		return false
	}
	return true
}
