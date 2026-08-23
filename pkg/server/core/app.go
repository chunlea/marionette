package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/audit"
	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/config"
	"github.com/chunlea/marionette/pkg/jobs"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/webhook"
	"go.uber.org/zap"
)

// Wire errors. Every required dependency is checked up front so a
// misconfigured binary fails at startup instead of silently no-opping later.
var (
	ErrStoreRequired            = errors.New("core: store is required")
	ErrConnManagerRequired      = errors.New("core: connection manager is required")
	ErrCmdSenderRequired        = errors.New("core: command sender is required")
	ErrRunnerTokenSvcRequired   = errors.New("core: runner token service is required")
	ErrProviderRegistryRequired = errors.New("core: provider registry is required")
	ErrLoggerRequired           = errors.New("core: logger is required")
)

// JobsConfig tunes the background jobs App.Start runs. Zero values mean
// "use the package default"; set Disable* to leave a job unstarted.
type JobsConfig struct {
	StaleCheckInterval   time.Duration
	StaleThreshold       time.Duration
	DisableStaleDetector bool

	TaskTimeoutCheckInterval time.Duration
	DisableTaskTimeout       bool

	DisablePermissionTimeout bool

	ScheduledTaskCheckInterval time.Duration
	DisableScheduledTasks      bool

	ScheduledSessionCheckInterval time.Duration
	DisableScheduledSessions      bool

	ReapInterval  time.Duration
	ReapMinAge    time.Duration
	DisableReaper bool

	// BackgroundWorkers bounds fire-and-forget work (task retries, post-resume
	// re-execution, runner attach). Zero uses the package default.
	BackgroundWorkers int

	PartitionInterval          time.Duration
	PartitionDaysAhead         int
	DisablePartitionMaintainer bool

	// LogRetentionDays drops log partitions older than this many days.
	//
	// It MUST stay zero (the default, meaning "never drop") until log archiving
	// is wired: the partitions are the only copy of the logs, so a non-zero
	// value here deletes them outright rather than ageing them out of hot
	// storage. See docs/storage.md.
	LogRetentionDays int
}

// WireDeps are the external dependencies App needs. They are struct fields
// rather than options on purpose: every one of them is required, and Wire
// refuses to build a half-connected App.
type WireDeps struct {
	// Store is the database.
	Store store.Store
	// ConnManager reports runner connectivity (grpc.ConnectionManager).
	ConnManager ConnectionManagerInterface
	// CmdSender publishes commands to runners (grpc.ConnectionManager).
	CmdSender CommandSender
	// RunnerTokenService authenticates runners during registration.
	RunnerTokenService *auth.RunnerTokenService
	// ProviderRegistry resolves runner providers for spawn/suspend/destroy.
	ProviderRegistry ProviderRegistryInterface
	// AuditLog records sensitive actions. Optional but strongly recommended.
	AuditLog audit.Logger
	// Logger is the root logger.
	Logger *zap.Logger

	// WorkspaceConfig configures on-host workspace directories.
	WorkspaceConfig config.WorkspaceStorageConfig
	// WebhookConfig configures webhook delivery.
	WebhookConfig webhook.Config
	// Jobs tunes the background jobs.
	Jobs JobsConfig
}

// App owns every core manager and every background job. It is the single
// production wiring point: nothing in cmd/server constructs a manager directly,
// so production wiring cannot drift away from what the tests exercise.
type App struct {
	Store  store.Store
	Logger *zap.Logger

	Sessions       *SessionManager
	Tasks          *TaskManager
	Permissions    *PermissionManager
	Runners        *RunnerManager
	RunnerRegistry *RunnerRegistry
	Workspaces     *WorkspaceManager
	ScheduledTasks *ScheduledTaskService
	Webhooks       *WebhookManager
	LogSubscribers *LogSubscriberManager
	Events         *EventBus
	Reaper         *Reaper

	// Background jobs, started by Start and drained by Stop.
	jobs []backgroundJob

	// ctx is the lifetime of every background operation. It is derived from
	// context.Background(), never from a request context: work that outlives a
	// request must not be cancelled when that request returns.
	ctx    context.Context
	cancel context.CancelFunc

	// background is the shared bounded pool the managers spawn onto, drained
	// by Stop so no retry outlives the process it belongs to.
	background *backgroundTasks

	mu      sync.Mutex
	started bool
	stopped bool
}

// backgroundJob adapts the varying Start/Stop shapes of the core jobs to one
// interface App can drive. stop receives the shutdown context so a job that
// can block on teardown honours the same deadline as everything else.
type backgroundJob struct {
	name  string
	start func(ctx context.Context) error
	stop  func(ctx context.Context)
}

// Wire builds every core manager and background job from deps.
// It returns an error rather than a partially-wired App.
func Wire(deps WireDeps) (*App, error) {
	if err := deps.validate(); err != nil {
		return nil, err
	}

	logger := deps.Logger

	// Events first: managers dispatch through it, so it must exist before them.
	events := NewEventBus(logger.Named("events"))
	webhookMgr := NewWebhookManager(deps.Store, deps.WebhookConfig, logger.Named("webhook"))
	webhooks := NewWebhookIntegration(
		NewEventDispatcher(webhookMgr, events),
		logger.Named("webhook-integration"),
	)

	workspaces := NewWorkspaceManager(deps.Store, deps.WorkspaceConfig, logger.Named("workspace"))

	appCtx, cancel := context.WithCancel(context.Background())
	background := newBackgroundTasks(appCtx, deps.Jobs.BackgroundWorkers, logger.Named("background"))

	sessions := NewSessionManagerWithConfig(SessionManagerConfig{
		Store:            deps.Store,
		ConnManager:      deps.ConnManager,
		CmdSender:        deps.CmdSender,
		WorkspaceManager: workspaces,
		AuditLog:         deps.AuditLog,
		ProviderRegistry: deps.ProviderRegistry,
		Webhooks:         webhooks,
		Background:       background,
		Logger:           logger.Named("session"),
	})

	tasks := NewTaskManager(
		deps.Store,
		deps.CmdSender,
		sessions,
		deps.AuditLog,
		logger.Named("task"),
		WithTaskWebhooks(webhooks),
		WithTaskBackground(background),
	)
	// Close the session <-> task cycle. See setTaskManager.
	sessions.setTaskManager(tasks)

	permissions := NewPermissionManager(
		deps.Store,
		deps.CmdSender,
		sessions,
		deps.AuditLog,
		logger.Named("permission"),
		WithPermissionWebhooks(webhooks),
	)

	runners := NewRunnerManager(
		deps.Store,
		deps.ConnManager,
		logger.Named("runner"),
		// Without the task manager a dead runner's tasks stay "running"
		// forever: handleInFlightTasks silently no-ops. This wiring is the
		// whole reason RunnerManager construction moved out of grpc.New.
		WithTaskManager(tasks),
		WithSessionManager(sessions),
		WithRunnerWebhooks(webhooks),
		WithRunnerBackground(background),
	)

	registry := NewRunnerRegistry(deps.Store, deps.RunnerTokenService, logger.Named("registry"))
	scheduledTasks := NewScheduledTaskService(deps.Store, tasks, deps.AuditLog, logger.Named("scheduled-task"))
	logSubscribers := NewLogSubscriberManager(logger.Named("log-subscriber"))

	app := &App{
		Store:          deps.Store,
		Logger:         logger,
		Sessions:       sessions,
		Tasks:          tasks,
		Permissions:    permissions,
		Runners:        runners,
		RunnerRegistry: registry,
		Workspaces:     workspaces,
		ScheduledTasks: scheduledTasks,
		Webhooks:       webhookMgr,
		LogSubscribers: logSubscribers,
		Events:         events,
		ctx:            appCtx,
		cancel:         cancel,
		background:     background,
	}

	app.buildJobs(deps)

	return app, nil
}

func (d WireDeps) validate() error {
	switch {
	case d.Store == nil:
		return ErrStoreRequired
	case d.ConnManager == nil:
		return ErrConnManagerRequired
	case d.CmdSender == nil:
		return ErrCmdSenderRequired
	case d.RunnerTokenService == nil:
		return ErrRunnerTokenSvcRequired
	case d.ProviderRegistry == nil:
		return ErrProviderRegistryRequired
	case d.Logger == nil:
		return ErrLoggerRequired
	}
	return nil
}

// buildJobs instantiates the background jobs. Each one used to have zero
// production constructor calls; they are wired here so that "built" and
// "running" are the same thing.
func (a *App) buildJobs(deps WireDeps) {
	cfg := deps.Jobs

	if !cfg.DisableStaleDetector {
		var opts []StaleDetectorOption
		if cfg.StaleCheckInterval > 0 {
			opts = append(opts, WithCheckInterval(cfg.StaleCheckInterval))
		}
		if cfg.StaleThreshold > 0 {
			opts = append(opts, WithStaleThreshold(cfg.StaleThreshold))
		}
		sd := NewStaleDetector(a.Store, deps.ConnManager, a.Runners, a.Logger.Named("stale-detector"), opts...)
		a.jobs = append(a.jobs, backgroundJob{
			name:  "stale-detector",
			start: func(ctx context.Context) error { sd.Start(ctx); return nil },
			stop:  func(context.Context) { sd.Stop() },
		})
	}

	if !cfg.DisableTaskTimeout {
		var opts []TaskTimeoutEnforcerOption
		if cfg.TaskTimeoutCheckInterval > 0 {
			opts = append(opts, WithTimeoutCheckInterval(cfg.TaskTimeoutCheckInterval))
		}
		tte := NewTaskTimeoutEnforcer(a.Store, a.Tasks, deps.CmdSender, a.Logger.Named("task-timeout"), opts...)
		a.jobs = append(a.jobs, backgroundJob{
			name:  "task-timeout-enforcer",
			start: func(ctx context.Context) error { tte.Start(ctx); return nil },
			stop:  func(context.Context) { tte.Stop() },
		})
	}

	if !cfg.DisablePermissionTimeout {
		pte := NewPermissionTimeoutEnforcer(a.Store, a.Sessions, a.Logger.Named("permission-timeout"))
		a.jobs = append(a.jobs, backgroundJob{
			name:  "permission-timeout-enforcer",
			start: func(ctx context.Context) error { pte.Start(ctx); return nil },
			stop:  func(context.Context) { pte.Stop() },
		})
	}

	if !cfg.DisableScheduledTasks {
		var opts []ScheduledTaskExecutorOption
		if cfg.ScheduledTaskCheckInterval > 0 {
			opts = append(opts, WithScheduledTaskCheckInterval(cfg.ScheduledTaskCheckInterval))
		}
		ste := NewScheduledTaskExecutor(a.ScheduledTasks, a.Tasks, a.Logger.Named("scheduled-task-executor"), opts...)
		a.jobs = append(a.jobs, backgroundJob{
			name:  "scheduled-task-executor",
			start: func(ctx context.Context) error { ste.Start(ctx); return nil },
			stop:  func(context.Context) { ste.Stop() },
		})
	}

	// The logs table is partitioned by day and the initial migration created a
	// fixed set once. Without this job the partitions run out and every log
	// insert fails; migration 007 added a default partition so writes survive,
	// but retention only works while the daily partitions keep coming.
	if !cfg.DisablePartitionMaintainer {
		if partitioner, ok := a.Store.(jobs.LogPartitioner); ok {
			pm := jobs.NewPartitionMaintainer(partitioner, jobs.PartitionMaintainerConfig{
				Interval:      cfg.PartitionInterval,
				DaysAhead:     cfg.PartitionDaysAhead,
				RetentionDays: cfg.LogRetentionDays,
				Logger:        a.Logger.Named("partition-maintainer"),
			})
			a.jobs = append(a.jobs, backgroundJob{
				name:  "log-partition-maintainer",
				start: pm.Start,
				stop: func(ctx context.Context) {
					if err := pm.Stop(ctx); err != nil {
						a.Logger.Warn("partition maintainer did not stop cleanly", zap.Error(err))
					}
				},
			})
		} else {
			a.Logger.Warn("store does not maintain log partitions; log inserts will fail once the existing partitions run out")
		}
	}

	if !cfg.DisableReaper {
		var opts []ReaperOption
		if cfg.ReapInterval > 0 {
			opts = append(opts, WithReapInterval(cfg.ReapInterval))
		}
		if cfg.ReapMinAge > 0 {
			opts = append(opts, WithReapMinAge(cfg.ReapMinAge))
		}
		a.Reaper = NewReaper(a.Store, deps.ProviderRegistry, deps.AuditLog, a.Logger.Named("reaper"), opts...)
		reaper := a.Reaper
		a.jobs = append(a.jobs, backgroundJob{
			name:  "runner-reaper",
			start: func(ctx context.Context) error { reaper.Start(ctx); return nil },
			stop:  func(context.Context) { reaper.Stop() },
		})
	}

	if !cfg.DisableScheduledSessions {
		ssa := NewScheduledSessionActivator(ScheduledSessionActivatorConfig{
			Store:         a.Store,
			SessionMgr:    a.Sessions,
			CheckInterval: cfg.ScheduledSessionCheckInterval,
			Logger:        a.Logger,
		})
		a.jobs = append(a.jobs, backgroundJob{
			name:  "scheduled-session-activator",
			start: func(_ context.Context) error { return ssa.Start() },
			stop:  func(context.Context) { ssa.Stop() },
		})
	}
}

// Context returns the App lifetime context. Background work started outside
// App must derive from this, never from a request context.
func (a *App) Context() context.Context {
	return a.ctx
}

// Start launches every background job. It is safe to call once; a second call
// is a no-op. The passed context bounds startup only - the jobs themselves run
// under the App context.
func (a *App) Start(_ context.Context) error {
	a.mu.Lock()
	if a.started || a.stopped {
		a.mu.Unlock()
		return nil
	}
	a.started = true
	a.mu.Unlock()

	for _, job := range a.jobs {
		if err := job.start(a.ctx); err != nil {
			return fmt.Errorf("failed to start %s: %w", job.name, err)
		}
		a.Logger.Info("background job started", zap.String("job", job.name))
	}
	return nil
}

// Stop cancels the App context and drains every background job, giving up when
// ctx expires so a wedged job cannot hold shutdown open forever.
func (a *App) Stop(ctx context.Context) error {
	a.mu.Lock()
	if a.stopped || !a.started {
		a.stopped = true
		a.mu.Unlock()
		a.cancel()
		return nil
	}
	a.stopped = true
	a.mu.Unlock()

	// Cancel first so jobs mid-iteration see the cancellation while we drain.
	a.cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		// Stop in reverse start order.
		for i := len(a.jobs) - 1; i >= 0; i-- {
			job := a.jobs[i]
			job.stop(ctx)
			a.Logger.Info("background job stopped", zap.String("job", job.name))
		}
	}()

	select {
	case <-done:
	case <-ctx.Done():
		return fmt.Errorf("core: background jobs did not drain: %w", ctx.Err())
	}

	// Then the fire-and-forget pool: retries and post-resume re-execution used
	// to be bare goroutines nothing waited for, so shutdown raced them to the
	// database handle.
	if err := a.background.Wait(ctx); err != nil {
		return fmt.Errorf("core: background work did not drain: %w", err)
	}
	return nil
}
