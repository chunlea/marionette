package core

import (
	"context"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Default scheduled task executor configuration.
const (
	DefaultScheduledTaskCheckInterval = 30 * time.Second
	DefaultScheduledTaskBatchSize     = 50
)

// ScheduledTaskExecutor polls for due scheduled tasks and executes them.
type ScheduledTaskExecutor struct {
	scheduledTaskSvc ScheduledTaskServiceInterface
	taskMgr          TaskManagerInterface
	checkInterval    time.Duration
	batchSize        int
	logger           *zap.Logger

	stopCh  chan struct{}
	doneCh  chan struct{}
	running bool
	mu      sync.RWMutex
}

// ScheduledTaskExecutorOption is a functional option for ScheduledTaskExecutor.
type ScheduledTaskExecutorOption func(*ScheduledTaskExecutor)

// WithScheduledTaskCheckInterval sets the polling interval.
func WithScheduledTaskCheckInterval(d time.Duration) ScheduledTaskExecutorOption {
	return func(e *ScheduledTaskExecutor) {
		e.checkInterval = d
	}
}

// WithScheduledTaskBatchSize sets the number of tasks to process per poll.
func WithScheduledTaskBatchSize(size int) ScheduledTaskExecutorOption {
	return func(e *ScheduledTaskExecutor) {
		e.batchSize = size
	}
}

// NewScheduledTaskExecutor creates a new ScheduledTaskExecutor.
func NewScheduledTaskExecutor(
	scheduledTaskSvc ScheduledTaskServiceInterface,
	taskMgr TaskManagerInterface,
	logger *zap.Logger,
	opts ...ScheduledTaskExecutorOption,
) *ScheduledTaskExecutor {
	e := &ScheduledTaskExecutor{
		scheduledTaskSvc: scheduledTaskSvc,
		taskMgr:          taskMgr,
		checkInterval:    DefaultScheduledTaskCheckInterval,
		batchSize:        DefaultScheduledTaskBatchSize,
		logger:           logger,
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Start begins the background scheduled task execution loop.
func (e *ScheduledTaskExecutor) Start(ctx context.Context) {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.mu.Unlock()

	e.logger.Info("starting scheduled task executor",
		zap.Duration("check_interval", e.checkInterval),
		zap.Int("batch_size", e.batchSize),
	)

	go e.run(ctx)
}

// Stop stops the scheduled task executor.
func (e *ScheduledTaskExecutor) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	e.logger.Info("stopping scheduled task executor")
	close(e.stopCh)
	<-e.doneCh

	e.mu.Lock()
	e.running = false
	e.mu.Unlock()

	e.logger.Info("scheduled task executor stopped")
}

// IsRunning returns whether the executor is running.
func (e *ScheduledTaskExecutor) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// run is the main loop for the scheduled task executor.
func (e *ScheduledTaskExecutor) run(ctx context.Context) {
	defer close(e.doneCh)

	// Run immediately on start
	if err := e.processDueTasks(ctx); err != nil {
		e.logger.Error("failed to process due scheduled tasks", zap.Error(err))
	}

	ticker := time.NewTicker(e.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			if err := e.processDueTasks(ctx); err != nil {
				e.logger.Error("failed to process due scheduled tasks", zap.Error(err))
			}
		}
	}
}

// processDueTasks finds and executes all due scheduled tasks.
func (e *ScheduledTaskExecutor) processDueTasks(ctx context.Context) error {
	dueTasks, err := e.scheduledTaskSvc.GetDue(ctx, e.batchSize)
	if err != nil {
		return err
	}

	if len(dueTasks) == 0 {
		return nil
	}

	e.logger.Debug("found due scheduled tasks",
		zap.Int("count", len(dueTasks)),
	)

	var wg sync.WaitGroup
	for _, scheduledTask := range dueTasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.executeTask(ctx, scheduledTask.ID, scheduledTask.Name)
		}()
	}

	wg.Wait()
	return nil
}

// executeTask executes a single scheduled task.
func (e *ScheduledTaskExecutor) executeTask(ctx context.Context, scheduledTaskID, name string) {
	logger := e.logger.With(
		zap.String("scheduled_task_id", scheduledTaskID),
		zap.String("name", name),
	)

	// Get the scheduled task fresh (it may have been modified)
	scheduledTask, err := e.scheduledTaskSvc.Get(ctx, scheduledTaskID)
	if err != nil {
		logger.Error("failed to get scheduled task", zap.Error(err))
		return
	}

	// Double-check it's still active and due
	if scheduledTask.Status != ScheduledTaskStatusActive {
		logger.Debug("scheduled task no longer active, skipping")
		return
	}
	if scheduledTask.NextRunAt == nil || scheduledTask.NextRunAt.After(time.Now()) {
		logger.Debug("scheduled task no longer due, skipping")
		return
	}

	// Execute the scheduled task
	task, err := e.scheduledTaskSvc.ExecuteScheduledTask(ctx, scheduledTask)
	if err != nil {
		logger.Error("failed to execute scheduled task", zap.Error(err))
		// Mark as failed
		if markErr := e.scheduledTaskSvc.MarkTaskCompleted(ctx, scheduledTaskID, false); markErr != nil {
			logger.Error("failed to mark scheduled task as failed", zap.Error(markErr))
		}
		return
	}

	logger.Info("scheduled task executed successfully",
		zap.String("task_id", task.ID),
	)

	// Note: The actual task success/failure will be determined later when the task completes.
	// This could be hooked into the task completion handler in the future.
}

// RunNow immediately processes due tasks (for testing).
func (e *ScheduledTaskExecutor) RunNow(ctx context.Context) error {
	return e.processDueTasks(ctx)
}
