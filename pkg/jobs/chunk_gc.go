// Package jobs provides background job implementations for the marionette server.
package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/storage/cas"
)

// ChunkGCJob manages periodic garbage collection of orphaned chunks.
type ChunkGCJob struct {
	gc       cas.GarbageCollector
	tenantID string
	interval time.Duration
	logger   *slog.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	// Metrics
	lastRun    time.Time
	lastResult *cas.GCResult
}

// ChunkGCJobConfig contains configuration for the GC job.
type ChunkGCJobConfig struct {
	// TenantID is the tenant to run GC for.
	// Use "*" or empty string for all tenants.
	TenantID string

	// Interval is how often to run GC. Default: 24 hours
	Interval time.Duration

	// Logger is the structured logger to use.
	Logger *slog.Logger
}

// NewChunkGCJob creates a new GC job.
func NewChunkGCJob(gc cas.GarbageCollector, config ChunkGCJobConfig) *ChunkGCJob {
	if config.Interval <= 0 {
		config.Interval = 24 * time.Hour
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}

	return &ChunkGCJob{
		gc:       gc,
		tenantID: config.TenantID,
		interval: config.Interval,
		logger:   config.Logger,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start begins the periodic GC job.
func (j *ChunkGCJob) Start(ctx context.Context) error {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return fmt.Errorf("GC job already running")
	}
	j.running = true
	j.stopCh = make(chan struct{})
	j.doneCh = make(chan struct{})
	j.mu.Unlock()

	go j.run(ctx)
	return nil
}

// Stop stops the GC job gracefully.
func (j *ChunkGCJob) Stop(ctx context.Context) error {
	j.mu.Lock()
	if !j.running {
		j.mu.Unlock()
		return nil
	}
	close(j.stopCh)
	doneCh := j.doneCh
	j.mu.Unlock()

	select {
	case <-doneCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsRunning returns whether the job is currently running.
func (j *ChunkGCJob) IsRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

// LastRun returns the time of the last GC run.
func (j *ChunkGCJob) LastRun() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastRun
}

// LastResult returns the result of the last GC run.
func (j *ChunkGCJob) LastResult() *cas.GCResult {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastResult
}

// RunNow triggers an immediate GC run.
func (j *ChunkGCJob) RunNow(ctx context.Context) (*cas.GCResult, error) {
	return j.runGC(ctx)
}

func (j *ChunkGCJob) run(ctx context.Context) {
	defer func() {
		j.mu.Lock()
		j.running = false
		close(j.doneCh)
		j.mu.Unlock()
	}()

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	// Run immediately on start
	if _, err := j.runGC(ctx); err != nil {
		j.logger.Error("initial GC run failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-j.stopCh:
			return
		case <-ticker.C:
			if _, err := j.runGC(ctx); err != nil {
				j.logger.Error("periodic GC run failed", "error", err)
			}
		}
	}
}

func (j *ChunkGCJob) runGC(ctx context.Context) (*cas.GCResult, error) {
	j.logger.Info("starting GC run", "tenant_id", j.tenantID)

	result, err := j.gc.RunGC(ctx, j.tenantID)
	if err != nil {
		j.logger.Error("GC run failed",
			"tenant_id", j.tenantID,
			"error", err,
		)
		return result, err
	}

	j.mu.Lock()
	j.lastRun = time.Now()
	j.lastResult = result
	j.mu.Unlock()

	j.logger.Info("GC run completed",
		"tenant_id", j.tenantID,
		"chunks_marked", result.ChunksMarked,
		"chunks_deleted", result.ChunksDeleted,
		"bytes_freed", result.BytesFreed,
		"duration", result.Duration,
	)

	return result, nil
}

// ChunkGCScheduler manages GC jobs for multiple tenants.
type ChunkGCScheduler struct {
	gcFactory func(tenantID string) cas.GarbageCollector
	interval  time.Duration
	logger    *slog.Logger

	mu   sync.RWMutex
	jobs map[string]*ChunkGCJob
}

// NewChunkGCScheduler creates a scheduler for managing multiple tenant GC jobs.
func NewChunkGCScheduler(gcFactory func(tenantID string) cas.GarbageCollector, interval time.Duration, logger *slog.Logger) *ChunkGCScheduler {
	if logger == nil {
		logger = slog.Default()
	}
	return &ChunkGCScheduler{
		gcFactory: gcFactory,
		interval:  interval,
		logger:    logger,
		jobs:      make(map[string]*ChunkGCJob),
	}
}

// AddTenant adds a tenant to the scheduler.
func (s *ChunkGCScheduler) AddTenant(ctx context.Context, tenantID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.jobs[tenantID]; exists {
		return fmt.Errorf("tenant %s already scheduled", tenantID)
	}

	gc := s.gcFactory(tenantID)
	job := NewChunkGCJob(gc, ChunkGCJobConfig{
		TenantID: tenantID,
		Interval: s.interval,
		Logger:   s.logger.With("tenant_id", tenantID),
	})

	if err := job.Start(ctx); err != nil {
		return fmt.Errorf("starting GC job: %w", err)
	}

	s.jobs[tenantID] = job
	return nil
}

// RemoveTenant removes a tenant from the scheduler.
func (s *ChunkGCScheduler) RemoveTenant(ctx context.Context, tenantID string) error {
	s.mu.Lock()
	job, exists := s.jobs[tenantID]
	if !exists {
		s.mu.Unlock()
		return nil
	}
	delete(s.jobs, tenantID)
	s.mu.Unlock()

	return job.Stop(ctx)
}

// Stop stops all GC jobs.
func (s *ChunkGCScheduler) Stop(ctx context.Context) error {
	s.mu.Lock()
	jobs := make([]*ChunkGCJob, 0, len(s.jobs))
	for _, job := range s.jobs {
		jobs = append(jobs, job)
	}
	s.jobs = make(map[string]*ChunkGCJob)
	s.mu.Unlock()

	var lastErr error
	for _, job := range jobs {
		if err := job.Stop(ctx); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// RunGCForTenant triggers an immediate GC run for a specific tenant.
func (s *ChunkGCScheduler) RunGCForTenant(ctx context.Context, tenantID string) (*cas.GCResult, error) {
	s.mu.RLock()
	job, exists := s.jobs[tenantID]
	s.mu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("tenant %s not scheduled", tenantID)
	}

	return job.RunNow(ctx)
}

// ListTenants returns a list of all scheduled tenants.
func (s *ChunkGCScheduler) ListTenants() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tenants := make([]string, 0, len(s.jobs))
	for tenantID := range s.jobs {
		tenants = append(tenants, tenantID)
	}
	return tenants
}
