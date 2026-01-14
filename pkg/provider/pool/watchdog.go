package pool

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// WatchdogStore is the subset of store.Store needed by the watchdog.
type WatchdogStore interface {
	ListRunners(ctx context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error)
	UpdateRunner(ctx context.Context, id string, updates store.RunnerUpdates) error
}

// Watchdog monitors pool health and performs periodic maintenance.
type Watchdog struct {
	store        WatchdogStore
	poolName     string
	config       *WatchdogConfig
	taintManager *TaintManager
	logger       *zap.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}
}

// WatchdogConfig contains watchdog configuration.
type WatchdogConfig struct {
	// HealthCheckInterval is how often to check runner health.
	HealthCheckInterval time.Duration

	// StaleThreshold is how long before a runner is considered stale.
	StaleThreshold time.Duration

	// MinRunners is the minimum number of runners (alert if below).
	MinRunners int

	// TaintedCleanupThreshold is how long tainted runners stay before cleanup.
	TaintedCleanupThreshold time.Duration

	// AlertCallback is called when alerts are generated.
	AlertCallback func(alert *PoolAlert)
}

// DefaultWatchdogConfig returns default watchdog configuration.
func DefaultWatchdogConfig() *WatchdogConfig {
	return &WatchdogConfig{
		HealthCheckInterval:     30 * time.Second,
		StaleThreshold:          90 * time.Second,
		MinRunners:              0,
		TaintedCleanupThreshold: 1 * time.Hour,
	}
}

// PoolAlert represents a pool health alert.
type PoolAlert struct {
	PoolName  string
	Type      AlertType
	Message   string
	RunnerID  string
	Timestamp time.Time
}

// AlertType represents the type of alert.
type AlertType string

const (
	AlertTypeRunnerStale     AlertType = "runner_stale"
	AlertTypeMinRunnersBelow AlertType = "min_runners_below"
	AlertTypeTaintedCleaned  AlertType = "tainted_cleaned"
	AlertTypeHealthCheckFail AlertType = "health_check_fail"
)

// NewWatchdog creates a new pool watchdog.
func NewWatchdog(st WatchdogStore, poolName string, config *WatchdogConfig, logger *zap.Logger) *Watchdog {
	if config == nil {
		config = DefaultWatchdogConfig()
	}
	if logger == nil {
		logger = zap.NewNop()
	}

	return &Watchdog{
		store:        st,
		poolName:     poolName,
		config:       config,
		taintManager: NewTaintManager(st, logger),
		logger:       logger.With(zap.String("pool", poolName)),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

// Start begins the watchdog monitoring loop.
func (w *Watchdog) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = true
	w.stopCh = make(chan struct{})
	w.doneCh = make(chan struct{})
	w.mu.Unlock()

	w.logger.Info("watchdog started",
		zap.Duration("health_check_interval", w.config.HealthCheckInterval),
		zap.Duration("stale_threshold", w.config.StaleThreshold),
	)

	go w.run(ctx)
	return nil
}

// Stop stops the watchdog monitoring loop.
func (w *Watchdog) Stop() {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return
	}
	w.running = false
	close(w.stopCh)
	w.mu.Unlock()

	<-w.doneCh
	w.logger.Info("watchdog stopped")
}

// run is the main watchdog loop.
func (w *Watchdog) run(ctx context.Context) {
	defer close(w.doneCh)

	ticker := time.NewTicker(w.config.HealthCheckInterval)
	defer ticker.Stop()

	// Run initial check
	w.runHealthCheck(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.runHealthCheck(ctx)
		}
	}
}

// runHealthCheck performs a health check on all runners in the pool.
func (w *Watchdog) runHealthCheck(ctx context.Context) {
	// List all runners in the pool
	poolName := w.poolName
	result, err := w.store.ListRunners(ctx, store.ListRunnersOptions{
		PoolName: &poolName,
		BaseListOptions: store.BaseListOptions{
			Limit: 1000,
		},
	})
	if err != nil {
		w.logger.Error("failed to list runners for health check", zap.Error(err))
		return
	}

	now := time.Now()
	stats := &healthCheckStats{}

	for _, runner := range result.Items {
		stats.total++

		// Skip already tainted runners
		if runner.Tainted {
			stats.tainted++
			continue
		}

		// Skip offline runners
		if runner.Status == "offline" {
			stats.offline++
			continue
		}

		// Check for stale runners
		if runner.LastSeenAt != nil {
			staleDuration := now.Sub(*runner.LastSeenAt)
			if staleDuration > w.config.StaleThreshold {
				w.logger.Warn("runner is stale",
					zap.String("runner_id", runner.ID),
					zap.String("runner_name", runner.Name),
					zap.Duration("since_last_seen", staleDuration),
				)

				// Taint the stale runner
				if err := w.taintManager.DetectStaleRunner(ctx, runner.ID, *runner.LastSeenAt); err != nil {
					w.logger.Error("failed to taint stale runner",
						zap.String("runner_id", runner.ID),
						zap.Error(err),
					)
				} else {
					stats.newlyTainted++
					w.emitAlert(&PoolAlert{
						PoolName:  w.poolName,
						Type:      AlertTypeRunnerStale,
						Message:   "Runner became stale",
						RunnerID:  runner.ID,
						Timestamp: now,
					})
				}
				continue
			}
		}

		switch runner.Status {
		case "idle":
			stats.idle++
		case "busy":
			stats.busy++
		}
	}

	// Check min runners
	availableRunners := stats.idle + stats.busy
	if w.config.MinRunners > 0 && availableRunners < w.config.MinRunners {
		w.logger.Warn("pool below minimum runners",
			zap.Int("available", availableRunners),
			zap.Int("minimum", w.config.MinRunners),
		)
		w.emitAlert(&PoolAlert{
			PoolName:  w.poolName,
			Type:      AlertTypeMinRunnersBelow,
			Message:   "Pool below minimum runners",
			Timestamp: now,
		})
	}

	// Cleanup old tainted runners
	if w.config.TaintedCleanupThreshold > 0 {
		cleaned, err := w.taintManager.CleanupTaintedRunners(ctx, w.poolName, w.config.TaintedCleanupThreshold)
		if err != nil {
			w.logger.Error("failed to cleanup tainted runners", zap.Error(err))
		} else if cleaned > 0 {
			w.logger.Info("cleaned up tainted runners", zap.Int("count", cleaned))
			w.emitAlert(&PoolAlert{
				PoolName:  w.poolName,
				Type:      AlertTypeTaintedCleaned,
				Message:   "Tainted runners cleaned up",
				Timestamp: now,
			})
		}
	}

	w.logger.Debug("health check completed",
		zap.Int("total", stats.total),
		zap.Int("idle", stats.idle),
		zap.Int("busy", stats.busy),
		zap.Int("offline", stats.offline),
		zap.Int("tainted", stats.tainted),
		zap.Int("newly_tainted", stats.newlyTainted),
	)
}

// healthCheckStats holds statistics from a health check run.
type healthCheckStats struct {
	total        int
	idle         int
	busy         int
	offline      int
	tainted      int
	newlyTainted int
}

// emitAlert sends an alert if a callback is configured.
func (w *Watchdog) emitAlert(alert *PoolAlert) {
	if w.config.AlertCallback != nil {
		w.config.AlertCallback(alert)
	}
}

// GetStats returns current pool statistics.
func (w *Watchdog) GetStats(ctx context.Context) (*PoolStats, error) {
	poolName := w.poolName
	result, err := w.store.ListRunners(ctx, store.ListRunnersOptions{
		PoolName: &poolName,
		BaseListOptions: store.BaseListOptions{
			Limit: 1000,
		},
	})
	if err != nil {
		return nil, err
	}

	stats := &PoolStats{
		PoolName: w.poolName,
	}

	for _, r := range result.Items {
		stats.Total++
		switch r.Status {
		case "idle":
			stats.Idle++
		case "busy":
			stats.Busy++
		case "offline":
			stats.Offline++
		case "paused":
			stats.Paused++
		}
		if r.Tainted {
			stats.Tainted++
		}
	}

	return stats, nil
}
