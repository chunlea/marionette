// Package jobs provides background job implementations for the marionette server.
package jobs

import (
	"context"
	"errors"
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
	tenants  TenantLister
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

// TenantLister enumerates the tenants that currently hold chunks.
// *postgres.Store implements it.
type TenantLister interface {
	ListChunkTenants(ctx context.Context) ([]string, error)
}

// ChunkGCJobConfig contains configuration for the GC job.
type ChunkGCJobConfig struct {
	// TenantID restricts collection to one tenant. Leave empty (or "*") to
	// collect every tenant, which requires Tenants.
	TenantID string

	// Tenants enumerates tenants when TenantID is not set.
	//
	// The previous version passed TenantID straight through to RunGC and
	// documented "" as meaning all tenants, which it never did: every GC
	// statement filters on tenant_id, so an empty id matched nothing and the
	// job collected nothing at all.
	Tenants TenantLister

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

	if config.TenantID == allTenants {
		config.TenantID = ""
	}

	return &ChunkGCJob{
		gc:       gc,
		tenantID: config.TenantID,
		tenants:  config.Tenants,
		interval: config.Interval,
		logger:   config.Logger,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// allTenants is the wildcard the config accepts for "every tenant".
const allTenants = "*"

// tenantsToCollect resolves which tenants this run covers.
func (j *ChunkGCJob) tenantsToCollect(ctx context.Context) ([]string, error) {
	if j.tenantID != "" {
		return []string{j.tenantID}, nil
	}
	if j.tenants == nil {
		return nil, fmt.Errorf("collecting all tenants requires a TenantLister")
	}
	return j.tenants.ListChunkTenants(ctx)
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
	start := time.Now()

	tenants, err := j.tenantsToCollect(ctx)
	if err != nil {
		j.logger.Error("GC run failed to resolve tenants", "error", err)
		return nil, err
	}

	j.logger.Info("starting GC run", "tenants", len(tenants))

	total := &cas.GCResult{}
	for _, tenant := range tenants {
		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
		}

		result, err := j.gc.RunGC(ctx, tenant)
		if err != nil {
			// One tenant's failure must not stop the others: a single broken
			// tenant would otherwise stall collection for the whole deployment.
			j.logger.Error("GC run failed", "tenant_id", tenant, "error", err)
			total.Errors = append(total.Errors, fmt.Errorf("tenant %s: %w", tenant, err))
			continue
		}

		total.ChunksMarked += result.ChunksMarked
		total.ChunksDeleted += result.ChunksDeleted
		total.BytesFreed += result.BytesFreed
		total.Errors = append(total.Errors, result.Errors...)

		j.logger.Debug("GC run completed for tenant",
			"tenant_id", tenant,
			"chunks_marked", result.ChunksMarked,
			"chunks_deleted", result.ChunksDeleted,
			"bytes_freed", result.BytesFreed,
		)
	}

	total.Duration = time.Since(start)

	j.mu.Lock()
	j.lastRun = time.Now()
	j.lastResult = total
	j.mu.Unlock()

	j.logger.Info("GC run completed",
		"tenants", len(tenants),
		"chunks_marked", total.ChunksMarked,
		"chunks_deleted", total.ChunksDeleted,
		"bytes_freed", total.BytesFreed,
		"errors", len(total.Errors),
		"duration", total.Duration,
	)

	if len(total.Errors) > 0 {
		return total, fmt.Errorf("GC completed with %d error(s): %w", len(total.Errors), errors.Join(total.Errors...))
	}

	return total, nil
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
