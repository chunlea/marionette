package pool

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

// RunnerStore is the subset of store.Store needed by the pool provider.
// This interface is used internally for dependency injection and testing.
type RunnerStore interface {
	GetRunner(ctx context.Context, id string) (*store.Runner, error)
	ListRunners(ctx context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error)
	UpdateRunner(ctx context.Context, id string, updates store.RunnerUpdates) error
}

// Provider implements the pool-based runner provider.
// Unlike managed providers (Docker, K8s), pool runners join the pool
// via token authentication rather than being spawned by the server.
type Provider struct {
	name          string
	config        *Config
	suspendConfig *provider.SuspendConfig
	store         RunnerStore
	selector      *Selector
	logger        *zap.Logger

	// Statistics
	mu         sync.RWMutex
	taskCounts map[string]int // runnerID -> task count
}

// Compile-time interface checks.
var (
	_ provider.Provider     = (*Provider)(nil)
	_ provider.PoolAcquirer = (*Provider)(nil)
)

// New creates a pool provider from a store.ProviderConfig.
func New(cfg *store.ProviderConfig, st RunnerStore, logger *zap.Logger) (*Provider, error) {
	poolCfg, err := ParseConfig(cfg.Config)
	if err != nil {
		return nil, fmt.Errorf("parsing pool config: %w", err)
	}

	if err := poolCfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating pool config: %w", err)
	}

	suspendCfg, err := provider.ParseSuspendConfig(cfg.SuspendConfig, defaultSuspendConfig())
	if err != nil {
		return nil, fmt.Errorf("parsing suspend config: %w", err)
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &Provider{
		name:          cfg.Name,
		config:        poolCfg,
		suspendConfig: suspendCfg,
		store:         st,
		selector:      NewSelector(st, poolCfg.PoolName, poolCfg.SelectionStrategy),
		logger:        logger.With(zap.String("provider", cfg.Name), zap.String("pool", poolCfg.PoolName)),
		taskCounts:    make(map[string]int),
	}, nil
}

// NewWithConfig creates a pool provider with explicit configuration.
func NewWithConfig(name string, cfg *Config, suspendCfg *provider.SuspendConfig, st RunnerStore, logger *zap.Logger) (*Provider, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validating pool config: %w", err)
	}

	if suspendCfg == nil {
		defaults := defaultSuspendConfig()
		suspendCfg = &defaults
	}

	if logger == nil {
		logger = zap.NewNop()
	}

	return &Provider{
		name:          name,
		config:        cfg,
		suspendConfig: suspendCfg,
		store:         st,
		selector:      NewSelector(st, cfg.PoolName, cfg.SelectionStrategy),
		logger:        logger.With(zap.String("provider", name), zap.String("pool", cfg.PoolName)),
		taskCounts:    make(map[string]int),
	}, nil
}

// Name returns the provider config name.
func (p *Provider) Name() string {
	return p.name
}

// Type returns the provider type (pool).
func (p *Provider) Type() provider.ProviderType {
	return provider.ProviderTypePool
}

// Capabilities returns the provider's capabilities.
func (p *Provider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{
		Pause:    false, // Pool providers don't support pause
		Snapshot: false, // Pool providers don't support snapshots
		// No suspend strategies: this provider implements neither Suspend nor
		// Resume, so advertising release_to_pool made callers believe a
		// capability that does not exist. ReleaseRunner/AcquireFromPool are
		// the pieces a real implementation would use.
		Suspend: provider.SuspendCapability{},
	}
}

// Spawn is not supported for pool providers.
// Pool runners join via token authentication, not server-initiated spawn.
func (p *Provider) Spawn(_ context.Context, opts provider.SpawnOptions) (*provider.RunnerInstance, error) {
	return nil, &provider.ErrSpawnNotSupported{
		Provider: p.name,
		Reason:   "pool providers do not spawn runners; runners join via token authentication",
	}
}

// Destroy removes a runner from the pool.
// For pool providers, this marks the runner as offline but doesn't destroy
// infrastructure, so there is no provider instance to address: opts is unused.
func (p *Provider) Destroy(ctx context.Context, runnerID string, _ provider.DestroyOptions) error {
	runner, err := p.store.GetRunner(ctx, runnerID)
	if err != nil {
		return &provider.ErrDestroyFailed{RunnerID: runnerID, Cause: err}
	}

	// Verify runner belongs to this pool
	if runner.PoolName == nil || *runner.PoolName != p.config.PoolName {
		return &provider.ErrRunnerNotFound{RunnerID: runnerID}
	}

	// Mark runner as offline (not actually destroying infrastructure)
	status := "offline"
	if err := p.store.UpdateRunner(ctx, runnerID, store.RunnerUpdates{
		Status: &status,
	}); err != nil {
		return &provider.ErrDestroyFailed{RunnerID: runnerID, Cause: err}
	}

	p.logger.Info("runner removed from pool",
		zap.String("runner_id", runnerID),
	)

	return nil
}

// Status returns the current status of a runner.
func (p *Provider) Status(ctx context.Context, runnerID string) (*provider.RunnerStatus, error) {
	runner, err := p.store.GetRunner(ctx, runnerID)
	if err != nil {
		return nil, fmt.Errorf("getting runner: %w", err)
	}

	// Verify runner belongs to this pool
	if runner.PoolName == nil || *runner.PoolName != p.config.PoolName {
		return nil, &provider.ErrRunnerNotFound{RunnerID: runnerID}
	}

	return &provider.RunnerStatus{
		Status:    mapRunnerStatus(runner.Status),
		UpdatedAt: runner.UpdatedAt,
	}, nil
}

// List returns all runners in the pool.
func (p *Provider) List(ctx context.Context) ([]*provider.RunnerInstance, error) {
	poolName := p.config.PoolName
	result, err := p.store.ListRunners(ctx, store.ListRunnersOptions{
		PoolName: &poolName,
		BaseListOptions: store.BaseListOptions{
			Limit: 1000, // Get all runners
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing runners: %w", err)
	}

	instances := make([]*provider.RunnerInstance, len(result.Items))
	for i, r := range result.Items {
		instances[i] = runnerToInstance(r)
	}

	return instances, nil
}

// AcquireRunner acquires an idle runner from the pool for a session.
// This is the main entry point for pool-based runner assignment.
func (p *Provider) AcquireRunner(ctx context.Context, opts AcquireOptions) (*store.Runner, error) {
	criteria := SelectionCriteria{
		RequiredLabels:       p.config.RequiredLabels,
		RequiredCapabilities: p.config.RequiredCapabilities,
		PreferRunnerID:       opts.PreferRunnerID,
		ExcludeRunnerIDs:     opts.ExcludeRunnerIDs,
		ExcludeTainted:       true,
	}

	// Merge additional label requirements (from profile selector)
	if opts.RequiredLabels != nil {
		if criteria.RequiredLabels == nil {
			criteria.RequiredLabels = make(map[string]string)
		}
		for k, v := range opts.RequiredLabels {
			criteria.RequiredLabels[k] = v
		}
	}

	// Merge additional capability requirements (from profile)
	if len(opts.RequiredCapabilities) > 0 {
		capSet := make(map[string]bool)
		for _, c := range criteria.RequiredCapabilities {
			capSet[c] = true
		}
		for _, c := range opts.RequiredCapabilities {
			if !capSet[c] {
				criteria.RequiredCapabilities = append(criteria.RequiredCapabilities, c)
			}
		}
	}

	// Select a runner
	runner, err := p.selector.SelectRunner(ctx, criteria)
	if err != nil {
		return nil, fmt.Errorf("selecting runner: %w", err)
	}

	if runner == nil {
		return nil, &ErrNoIdleRunner{PoolName: p.config.PoolName}
	}

	// Mark runner as busy
	status := "busy"
	if err := p.store.UpdateRunner(ctx, runner.ID, store.RunnerUpdates{
		Status: &status,
	}); err != nil {
		return nil, fmt.Errorf("marking runner busy: %w", err)
	}

	p.logger.Info("runner acquired from pool",
		zap.String("runner_id", runner.ID),
		zap.String("runner_name", runner.Name),
	)

	return runner, nil
}

// ReleaseRunner releases a runner back to the pool.
func (p *Provider) ReleaseRunner(ctx context.Context, runnerID string, tainted bool, taintReason string) error {
	// suspendConfig.MinDuration is deliberately not consulted here. This used
	// to log "skipping release due to min duration" and then release anyway.
	// Debouncing a release is also wrong: a second task finishing on the same
	// runner is a real release, and suppressing it breaks the task counting
	// that drives max-tasks tainting. If MinDuration is ever enforced for
	// pools it belongs in acquisition, not release.

	updates := store.RunnerUpdates{}

	if tainted {
		updates.Tainted = &tainted
		updates.TaintReason = &taintReason
		status := "offline" // Tainted runners go offline
		updates.Status = &status
		p.logger.Warn("runner tainted",
			zap.String("runner_id", runnerID),
			zap.String("reason", taintReason),
		)
	} else {
		status := "idle"
		updates.Status = &status

		// Track task count
		p.mu.Lock()
		p.taskCounts[runnerID]++
		taskCount := p.taskCounts[runnerID]
		p.mu.Unlock()

		// Check if runner should be recycled
		if p.config.MaxTasksPerRunner > 0 && taskCount >= p.config.MaxTasksPerRunner {
			tainted = true
			reason := fmt.Sprintf("max tasks reached (%d)", p.config.MaxTasksPerRunner)
			updates.Tainted = &tainted
			updates.TaintReason = &reason
			status = "offline"
			updates.Status = &status
			p.logger.Info("runner recycled due to max tasks",
				zap.String("runner_id", runnerID),
				zap.Int("task_count", taskCount),
			)
		}
	}

	if err := p.store.UpdateRunner(ctx, runnerID, updates); err != nil {
		return fmt.Errorf("releasing runner: %w", err)
	}

	if !tainted {
		p.logger.Info("runner released to pool",
			zap.String("runner_id", runnerID),
		)
	}

	return nil
}

// AcquireFromPool implements provider.PoolAcquirer interface.
// It acquires an idle runner from the pool based on the provided options.
func (p *Provider) AcquireFromPool(ctx context.Context, opts provider.PoolAcquireOptions) (provider.RunnerInfo, error) {
	// Convert provider options to internal options
	acquireOpts := AcquireOptions{
		PreferRunnerID:       opts.PreferRunnerID,
		RequiredLabels:       opts.RequiredLabels,
		RequiredCapabilities: opts.RequiredCapabilities,
		ExcludeRunnerIDs:     opts.ExcludeRunnerIDs,
		SessionID:            opts.SessionID,
		ProfileID:            opts.ProfileID,
	}

	runner, err := p.AcquireRunner(ctx, acquireOpts)
	if err != nil {
		return provider.RunnerInfo{}, err
	}

	return provider.RunnerInfo{
		ID:   runner.ID,
		Name: runner.Name,
	}, nil
}

// ReleaseToPool implements provider.PoolAcquirer interface.
// It releases a runner back to the pool.
func (p *Provider) ReleaseToPool(ctx context.Context, runnerID string, tainted bool, taintReason string) error {
	return p.ReleaseRunner(ctx, runnerID, tainted, taintReason)
}

// PoolStats returns statistics about the pool.
func (p *Provider) PoolStats(ctx context.Context) (*PoolStats, error) {
	poolName := p.config.PoolName
	result, err := p.store.ListRunners(ctx, store.ListRunnersOptions{
		PoolName: &poolName,
		BaseListOptions: store.BaseListOptions{
			Limit: 1000,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("listing runners: %w", err)
	}

	stats := &PoolStats{
		PoolName: p.config.PoolName,
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

// Config returns the pool configuration.
func (p *Provider) Config() *Config {
	return p.config
}

// SuspendConfig returns the suspend configuration.
func (p *Provider) SuspendConfig() provider.SuspendConfig {
	return *p.suspendConfig
}

// AcquireOptions contains options for acquiring a runner.
type AcquireOptions struct {
	// PreferRunnerID prefers a specific runner if available.
	PreferRunnerID string

	// RequiredLabels are additional labels the runner must have.
	RequiredLabels map[string]string

	// RequiredCapabilities are capabilities the runner must have.
	RequiredCapabilities []string

	// ExcludeRunnerIDs excludes specific runners from selection.
	ExcludeRunnerIDs []string

	// SessionID is the session acquiring the runner.
	SessionID string

	// ProfileID is the profile ID for logging/tracking purposes.
	ProfileID string
}

// PoolStats contains pool statistics.
type PoolStats struct {
	PoolName string
	Total    int
	Idle     int
	Busy     int
	Offline  int
	Paused   int
	Tainted  int
}

// ErrNoIdleRunner is returned when no idle runner is available.
type ErrNoIdleRunner struct {
	PoolName string
}

func (e *ErrNoIdleRunner) Error() string {
	return fmt.Sprintf("no idle runner available in pool %s", e.PoolName)
}

// mapRunnerStatus maps database runner status to provider.InstanceStatus.
func mapRunnerStatus(status string) provider.InstanceStatus {
	switch status {
	case "idle", "busy":
		return provider.InstanceStatusRunning
	case "paused":
		return provider.InstanceStatusPaused
	case "offline":
		return provider.InstanceStatusStopped
	default:
		return provider.InstanceStatusFailed
	}
}

// runnerToInstance converts a store.Runner to provider.RunnerInstance.
func runnerToInstance(r *store.Runner) *provider.RunnerInstance {
	var labels map[string]string
	if len(r.Labels) > 0 {
		_ = json.Unmarshal(r.Labels, &labels)
	}

	var annotations map[string]string
	if len(r.Annotations) > 0 {
		_ = json.Unmarshal(r.Annotations, &annotations)
	}

	instance := &provider.RunnerInstance{
		ID:          r.ID,
		Name:        r.Name,
		Status:      mapRunnerStatus(r.Status),
		SandboxMode: r.SandboxMode,
		CreatedAt:   r.CreatedAt,
		Labels:      labels,
		Annotations: annotations,
		Metadata:    make(map[string]string),
	}

	if r.PoolName != nil {
		instance.Metadata["pool_name"] = *r.PoolName
	}
	if r.Tainted {
		instance.Metadata["tainted"] = "true"
		if r.TaintReason != nil {
			instance.Metadata["taint_reason"] = *r.TaintReason
		}
	}

	return instance
}
