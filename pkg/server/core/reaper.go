package core

import (
	"context"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/audit"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Reaper defaults.
const (
	// DefaultReapInterval is how often orphaned runners are looked for.
	DefaultReapInterval = 2 * time.Minute

	// DefaultReapMinAge is how long a runner must have existed before it can be
	// reaped. Without it the reaper races a session that has spawned a runner
	// but not yet attached it.
	DefaultReapMinAge = 5 * time.Minute

	// DefaultReapMaxAttempts is how many times a single runner's destroy is
	// retried before the reaper gives up and leaves it to an operator.
	DefaultReapMaxAttempts = 5

	// reapBatchSize bounds one sweep so a large fleet cannot stall the loop.
	reapBatchSize = 200
)

// liveSessionStatuses are the session states that still lay claim to a runner.
// A suspended session counts: it may resume onto the same instance.
var liveSessionStatuses = []string{
	SessionStatusPending,
	SessionStatusActive,
	SessionStatusResuming,
	SessionStatusSuspended,
}

// Reaper destroys runners that no session will ever come back to.
//
// Provider.Destroy previously had no caller anywhere in the codebase: when a
// session was terminated its container, pod or sandbox simply kept running, and
// kept billing. The reaper is the missing owner of that call.
//
// It reaps conservatively. A runner is destroyed only when it has a managed
// provider behind it, is old enough not to be mid-attach, and no pending,
// active, resuming or suspended session references it - directly or as a
// previous runner it might resume onto. Pool and external runners are never
// destroyed: we do not own that infrastructure.
type Reaper struct {
	store       store.Store
	providers   ProviderRegistryInterface
	auditLog    audit.Logger
	logger      *zap.Logger
	interval    time.Duration
	minAge      time.Duration
	maxAttempts int

	// attempts counts consecutive destroy failures per runner so a permanently
	// broken instance does not get retried forever.
	attempts map[string]int

	stopCh  chan struct{}
	doneCh  chan struct{}
	mu      sync.Mutex
	running bool
}

// ReaperOption is a functional option for Reaper.
type ReaperOption func(*Reaper)

// WithReapInterval sets how often the reaper sweeps.
func WithReapInterval(d time.Duration) ReaperOption {
	return func(r *Reaper) {
		r.interval = d
	}
}

// WithReapMinAge sets how old a runner must be before it can be reaped.
func WithReapMinAge(d time.Duration) ReaperOption {
	return func(r *Reaper) {
		r.minAge = d
	}
}

// WithReapMaxAttempts sets how many destroy failures are tolerated per runner.
func WithReapMaxAttempts(n int) ReaperOption {
	return func(r *Reaper) {
		r.maxAttempts = n
	}
}

// NewReaper creates a Reaper.
func NewReaper(
	s store.Store,
	providers ProviderRegistryInterface,
	auditLog audit.Logger,
	logger *zap.Logger,
	opts ...ReaperOption,
) *Reaper {
	r := &Reaper{
		store:       s,
		providers:   providers,
		auditLog:    auditLog,
		logger:      logger,
		interval:    DefaultReapInterval,
		minAge:      DefaultReapMinAge,
		maxAttempts: DefaultReapMaxAttempts,
		attempts:    make(map[string]int),
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Start begins the background reap loop.
func (r *Reaper) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.mu.Unlock()

	r.logger.Info("starting runner reaper",
		zap.Duration("interval", r.interval),
		zap.Duration("min_age", r.minAge),
	)
	go r.run(ctx)
}

// Stop stops the reaper and waits for the loop to finish.
func (r *Reaper) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	r.running = false
	close(r.stopCh)
	r.mu.Unlock()

	<-r.doneCh
	r.logger.Info("runner reaper stopped")
}

func (r *Reaper) run(ctx context.Context) {
	defer close(r.doneCh)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stopCh:
			return
		case <-ticker.C:
			if err := r.Sweep(ctx); err != nil {
				r.logger.Error("reaper sweep failed", zap.Error(err))
			}
		}
	}
}

// Sweep performs one reap pass. Exported so it can be driven directly by tests
// and by an operator-triggered cleanup.
func (r *Reaper) Sweep(ctx context.Context) error {
	runners, err := r.store.ListRunners(ctx, store.ListRunnersOptions{
		BaseListOptions: store.BaseListOptions{Limit: reapBatchSize},
	})
	if err != nil {
		return err
	}

	now := time.Now()
	reaped := 0

	for _, runner := range runners.Items {
		if !r.shouldReap(ctx, runner, now) {
			continue
		}
		if r.reap(ctx, runner) {
			reaped++
		}
	}

	if reaped > 0 {
		r.logger.Info("reaped orphaned runners", zap.Int("count", reaped))
	}
	return nil
}

// shouldReap decides whether a runner is an orphan safe to destroy.
func (r *Reaper) shouldReap(ctx context.Context, runner *store.Runner, now time.Time) bool {
	// Only managed instances: pool and external runners belong to someone else.
	if runner.ProviderConfigID == nil || *runner.ProviderConfigID == "" {
		return false
	}
	if runner.PoolName != nil && *runner.PoolName != "" {
		return false
	}
	if now.Sub(runner.CreatedAt) < r.minAge {
		return false
	}

	r.mu.Lock()
	attempts := r.attempts[runner.ID]
	r.mu.Unlock()
	if attempts >= r.maxAttempts {
		return false
	}

	claimed, err := r.hasLiveSession(ctx, runner.ID)
	if err != nil {
		// Never destroy on incomplete information.
		r.logger.Warn("could not determine whether runner is claimed; leaving it alone",
			zap.String("runner_id", runner.ID),
			zap.Error(err),
		)
		return false
	}
	return !claimed
}

// hasLiveSession reports whether any session still lays claim to the runner,
// either as its current runner or as a previous runner it may resume onto.
func (r *Reaper) hasLiveSession(ctx context.Context, runnerID string) (bool, error) {
	byRunner, err := r.store.ListSessions(ctx, store.ListSessionsOptions{
		BaseListOptions: store.BaseListOptions{Limit: 1},
		RunnerID:        &runnerID,
		Status:          liveSessionStatuses,
	})
	if err != nil {
		return false, err
	}
	if len(byRunner.Items) > 0 {
		return true, nil
	}

	// A suspended session releases runner_id but remembers the runner in
	// previous_runner_id, and resume prefers that instance. There is no index
	// on previous_runner_id, so scan the suspended set instead - it is small by
	// construction, and this is a background sweep.
	suspended, err := r.store.ListSessions(ctx, store.ListSessionsOptions{
		BaseListOptions: store.BaseListOptions{Limit: reapBatchSize},
		Status:          []string{SessionStatusSuspended, SessionStatusResuming},
	})
	if err != nil {
		return false, err
	}
	for _, sess := range suspended.Items {
		if sess.PreviousRunnerID != nil && *sess.PreviousRunnerID == runnerID {
			return true, nil
		}
	}

	return false, nil
}

// reap destroys one runner and records the outcome. It reports whether the
// runner was actually destroyed.
func (r *Reaper) reap(ctx context.Context, runner *store.Runner) bool {
	prov, err := r.providerFor(ctx, runner)
	if err != nil || prov == nil {
		if err != nil {
			r.logger.Warn("could not resolve provider for orphaned runner",
				zap.String("runner_id", runner.ID),
				zap.Error(err),
			)
		}
		return false
	}
	if prov.Type() != provider.ProviderTypeManaged {
		return false
	}

	if err := prov.Destroy(ctx, runner.ID, provider.DestroyOptions{
		ProviderInstanceID: runnerInstanceID(runner),
	}); err != nil {
		r.mu.Lock()
		r.attempts[runner.ID]++
		attempts := r.attempts[runner.ID]
		r.mu.Unlock()

		r.logger.Warn("failed to destroy orphaned runner",
			zap.String("runner_id", runner.ID),
			zap.String("provider", prov.Name()),
			zap.Int("attempt", attempts),
			zap.Int("max_attempts", r.maxAttempts),
			zap.Error(err),
		)
		if attempts >= r.maxAttempts {
			r.logger.Error("giving up on orphaned runner; manual cleanup required",
				zap.String("runner_id", runner.ID),
				zap.String("provider", prov.Name()),
			)
			r.audit(ctx, runner, prov.Name(), false, err.Error())
		}
		return false
	}

	r.mu.Lock()
	delete(r.attempts, runner.ID)
	r.mu.Unlock()

	// The instance is gone; the row must say so.
	if err := r.store.UpdateRunner(ctx, runner.ID, store.RunnerUpdates{
		Status: stringPtr(StatusOffline),
	}); err != nil {
		r.logger.Warn("destroyed runner but failed to update its status",
			zap.String("runner_id", runner.ID),
			zap.Error(err),
		)
	}

	r.logger.Info("destroyed orphaned runner",
		zap.String("runner_id", runner.ID),
		zap.String("name", runner.Name),
		zap.String("provider", prov.Name()),
	)
	r.audit(ctx, runner, prov.Name(), true, "")
	return true
}

func (r *Reaper) providerFor(ctx context.Context, runner *store.Runner) (provider.Provider, error) {
	provConfig, err := r.store.GetProviderConfig(ctx, *runner.ProviderConfigID)
	if err != nil {
		return nil, err
	}
	return r.providers.Get(ctx, provConfig.Name)
}

func (r *Reaper) audit(ctx context.Context, runner *store.Runner, providerName string, success bool, errMsg string) {
	if r.auditLog == nil {
		return
	}
	event := audit.NewEvent(audit.ActionRunnerReleased).
		WithSystemActor().
		WithResource(audit.ResourceTypeRunner, runner.ID).
		WithDetails(map[string]any{
			"reason":   "orphaned",
			"provider": providerName,
			"name":     runner.Name,
		}).
		WithSuccess(success)
	if errMsg != "" {
		event = event.WithError(errMsg)
	}
	_ = event.Log(ctx, r.auditLog)
}
