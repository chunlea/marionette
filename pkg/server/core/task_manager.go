package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/audit"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
)

// Task status constants.
const (
	TaskStatusPending   = "pending"
	TaskStatusRunning   = "running"
	TaskStatusCompleted = "completed"
	TaskStatusFailed    = "failed"
	TaskStatusCanceled  = "canceled"
)

// TaskRun status constants.
const (
	TaskRunStatusPending   = "pending"
	TaskRunStatusAssigned  = "assigned"
	TaskRunStatusRunning   = "running"
	TaskRunStatusCompleted = "completed"
	TaskRunStatusFailed    = "failed"
	TaskRunStatusTimeout   = "timeout"
	TaskRunStatusCanceled  = "canceled"
)

// Default task configuration.
const (
	DefaultTaskTimeoutSeconds = 3600 // 1 hour
	DefaultMaxRetries         = 0
)

// Task-related errors.
var (
	ErrTaskNotFound             = errors.New("task not found")
	ErrTaskRunNotFound          = errors.New("task run not found")
	ErrInvalidTaskTransition    = errors.New("invalid task status transition")
	ErrInvalidTaskRunTransition = errors.New("invalid task run status transition")
	ErrTaskAlreadyCompleted     = errors.New("task is already completed")
	ErrTaskAlreadyCanceled      = errors.New("task is already canceled")
	ErrSessionRequired          = errors.New("session_id is required")
	ErrPromptRequired           = errors.New("prompt is required")
	ErrNoRunnerAttached         = errors.New("no runner attached to session")
	ErrMaxRetriesExceeded       = errors.New("max retries exceeded")

	// ErrDispatchRaceLost means another dispatcher moved the task first.
	//
	// It is not a failure: the task is running, which is what the caller
	// wanted. Triggers that fire opportunistically swallow it; the explicit
	// execute endpoint surfaces it, because a user asking to run an
	// already-running task should be told so.
	ErrDispatchRaceLost = errors.New("task was dispatched by someone else")
)

// CommandSender defines the interface for sending commands to runners.
// This is implemented by grpc.ConnectionManager.
type CommandSender interface {
	SendCommand(runnerID string, cmd *pb.ServerCommand) error
}

// TaskManagerInterface defines the interface for task management.
// This is used for dependency injection in other components.
type TaskManagerInterface interface {
	Create(ctx context.Context, opts CreateTaskOptions) (*store.Task, error)
	Get(ctx context.Context, taskID string) (*store.Task, error)
	List(ctx context.Context, opts ListTasksOptions) (*store.ListResult[store.Task], error)
	Cancel(ctx context.Context, taskID string) error
	Execute(ctx context.Context, taskID string) error
	ReExecute(ctx context.Context, taskID string) error
	DispatchNext(ctx context.Context, sessionID string) error
	CreateRun(ctx context.Context, taskID string) (*store.TaskRun, error)
	ListRuns(ctx context.Context, taskID string, opts ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error)
	OnTaskAccepted(ctx context.Context, runID string) error
	OnTaskStarted(ctx context.Context, runID string) error
	OnTaskProgress(ctx context.Context, runID string, progress int) error
	OnTaskCompleted(ctx context.Context, result *TaskCompletedResult) error
	FailRun(ctx context.Context, runID, reason string) error
	ShouldRetry(ctx context.Context, taskID string) (bool, error)
	Retry(ctx context.Context, taskID string) (*store.TaskRun, error)
}

// TaskCompletedResult contains the result of a completed task run.
type TaskCompletedResult struct {
	RunID        string
	Success      bool
	Error        string
	ExitCode     *int
	TokensInput  int
	TokensOutput int
}

// TaskManager handles task lifecycle and execution.
type TaskManager struct {
	store      store.Store
	cmdSender  CommandSender
	sessionMgr SessionManagerInterface
	auditLog   audit.Logger
	webhooks   *WebhookIntegration
	background *backgroundTasks
	logger     *zap.Logger

	// dispatchLocks serialises DispatchNext per session.
	//
	// Creating a task both activates the session (which schedules a dispatch)
	// and dispatches directly, and a resume dispatches too. Without this,
	// two of those can each see "nothing running" and both send the same
	// pending task to the runner.
	dispatchLocks sync.Map // sessionID -> *sync.Mutex
}

// dispatchLock returns the per-session dispatch mutex, creating it on first use.
func (m *TaskManager) dispatchLock(sessionID string) *sync.Mutex {
	lock, _ := m.dispatchLocks.LoadOrStore(sessionID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// TaskManagerOption is a functional option for TaskManager.
type TaskManagerOption func(*TaskManager)

// WithTaskBackground supplies the shared background worker pool.
// When unset, the manager creates its own.
func WithTaskBackground(b *backgroundTasks) TaskManagerOption {
	return func(m *TaskManager) {
		m.background = b
	}
}

// WithTaskWebhooks sets the webhook integration for dispatching task events.
func WithTaskWebhooks(wi *WebhookIntegration) TaskManagerOption {
	return func(m *TaskManager) {
		m.webhooks = wi
	}
}

// NewTaskManager creates a new TaskManager.
func NewTaskManager(
	store store.Store,
	cmdSender CommandSender,
	sessionMgr SessionManagerInterface,
	auditLog audit.Logger,
	logger *zap.Logger,
	opts ...TaskManagerOption,
) *TaskManager {
	m := &TaskManager{
		store:      store,
		cmdSender:  cmdSender,
		sessionMgr: sessionMgr,
		auditLog:   auditLog,
		logger:     logger,
	}
	for _, opt := range opts {
		opt(m)
	}
	if m.background == nil {
		m.background = newBackgroundTasks(context.Background(), 0, logger)
	}
	return m
}

// CreateTaskOptions contains options for creating a new task.
type CreateTaskOptions struct {
	SessionID      string            // Required
	Prompt         string            // Required
	MaxRetries     int               // Default: 0
	TimeoutSeconds int               // Default: 3600
	TenantID       *string           // For multi-tenant deployments
	Labels         map[string]string // Optional metadata labels
	Annotations    map[string]string // Optional metadata annotations
}

// ListTasksOptions wraps store.ListTasksOptions for convenience.
type ListTasksOptions = store.ListTasksOptions

// ListTaskRunsOptions wraps store.ListTaskRunsOptions for convenience.
type ListTaskRunsOptions = store.ListTaskRunsOptions

// Create creates a new task.
func (m *TaskManager) Create(ctx context.Context, opts CreateTaskOptions) (*store.Task, error) {
	// Validate required fields
	if opts.SessionID == "" {
		return nil, ErrSessionRequired
	}
	if opts.Prompt == "" {
		return nil, ErrPromptRequired
	}

	// Validate session exists
	session, err := m.store.GetSession(ctx, opts.SessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, err
	}

	// Set defaults
	timeoutSeconds := opts.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTaskTimeoutSeconds
	}

	maxRetries := opts.MaxRetries
	if maxRetries < 0 {
		maxRetries = DefaultMaxRetries
	}

	// Marshal labels and annotations
	labels, err := json.Marshal(opts.Labels)
	if err != nil {
		labels = []byte("{}")
	}
	annotations, err := json.Marshal(opts.Annotations)
	if err != nil {
		annotations = []byte("{}")
	}

	// A task belongs to its session's tenant. The request context is checked
	// against any explicit value first, so a caller cannot create a task in a
	// tenant it is not acting for.
	if _, err := tenantFor(ctx, opts.TenantID); err != nil {
		return nil, err
	}
	tenantID := session.TenantID

	// Create task
	task := &store.Task{
		ID:             id.Task(),
		SessionID:      opts.SessionID,
		Prompt:         opts.Prompt,
		Status:         TaskStatusPending,
		MaxRetries:     maxRetries,
		RetryCount:     0,
		TimeoutSeconds: timeoutSeconds,
		TenantID:       tenantID,
		Labels:         labels,
		Annotations:    annotations,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
	}

	if err := m.store.CreateTask(ctx, task); err != nil {
		return nil, err
	}

	m.logger.Info("task created",
		zap.String("task_id", task.ID),
		zap.String("session_id", task.SessionID),
		zap.Int("timeout_seconds", task.TimeoutSeconds),
		zap.Int("max_retries", task.MaxRetries),
	)

	// Log audit event
	if m.auditLog != nil {
		_ = audit.NewEvent(audit.ActionTaskCreated).
			WithSystemActor().
			WithResource(audit.ResourceTypeTask, task.ID).
			WithSession(task.SessionID).
			WithTask(task.ID).
			WithDetails(map[string]any{
				"timeout_seconds": task.TimeoutSeconds,
				"max_retries":     task.MaxRetries,
			}).
			WithSuccess(true).
			Log(ctx, m.auditLog)
	}

	// Dispatch webhook event
	if m.webhooks != nil {
		m.webhooks.DispatchTaskEvent(ctx, "task.created", task, nil)
	}

	// Creating a task is enough to run it. Before this, a task sat pending
	// until someone called POST /tasks/{id}/execute by hand.
	m.autoDispatch(ctx, task)

	return task, nil
}

// DispatchNext executes the oldest pending task of a session, if the session is
// free to take one.
//
// Tasks within a session are sequential, so a session with a task already in
// flight is left alone: the next one goes out when that finishes. Returns nil
// when there is nothing to dispatch - an idle session is not an error.
func (m *TaskManager) DispatchNext(ctx context.Context, sessionID string) error {
	// Held across the whole dispatch, including Execute: Execute commits the
	// task as running before it sends, so a caller that waits here then sees
	// the task in flight and correctly does nothing.
	lock := m.dispatchLock(sessionID)
	lock.Lock()
	defer lock.Unlock()

	running, err := m.store.ListTasks(ctx, ListTasksOptions{
		BaseListOptions: store.BaseListOptions{Limit: 1},
		SessionID:       &sessionID,
		Status:          []string{TaskStatusRunning},
	})
	if err != nil {
		return err
	}
	if len(running.Items) > 0 {
		m.logger.Debug("session already has a task in flight, not dispatching",
			zap.String("session_id", sessionID),
			zap.String("running_task_id", running.Items[0].ID),
		)
		return nil
	}

	pending, err := m.store.ListTasks(ctx, ListTasksOptions{
		BaseListOptions: store.BaseListOptions{Limit: dispatchScanLimit},
		SessionID:       &sessionID,
		Status:          []string{TaskStatusPending},
	})
	if err != nil {
		return err
	}

	next := oldestTask(pending.Items)
	if next == nil {
		return nil
	}

	m.logger.Info("dispatching pending task",
		zap.String("session_id", sessionID),
		zap.String("task_id", next.ID),
	)

	if err := m.Execute(ctx, next.ID); err != nil {
		if errors.Is(err, ErrDispatchRaceLost) {
			// Somebody else got there first, which is the outcome this call
			// wanted anyway.
			m.logger.Debug("another dispatcher won the race",
				zap.String("session_id", sessionID),
				zap.String("task_id", next.ID),
			)
			return nil
		}
		return err
	}
	return nil
}

// dispatchScanLimit bounds how many pending tasks are considered when picking
// the oldest. A session's backlog is small by construction - execution is
// sequential - so this is a safety valve, not a page size.
const dispatchScanLimit = 200

// oldestTask returns the earliest-created task, or nil for an empty list.
// The ordering is computed here rather than pushed into the query so the
// behaviour does not depend on a store's default sort.
func oldestTask(tasks []*store.Task) *store.Task {
	var oldest *store.Task
	for _, task := range tasks {
		if oldest == nil || task.CreatedAt.Before(oldest.CreatedAt) {
			oldest = task
		}
	}
	return oldest
}

// autoDispatch gets a newly created task moving without a second API call.
//
// It asks the session manager for a runner first, which activates a pending
// session by allocating one. When no runner can be had, the task and the
// session both stay pending and say so - that is the honest state, and the user
// re-triggers once capacity exists. A dispatch that reaches a runner and fails
// is NOT retried here: it parks as pending for manual re-trigger.
func (m *TaskManager) autoDispatch(ctx context.Context, task *store.Task) {
	if m.sessionMgr == nil {
		return
	}

	if _, err := m.sessionMgr.EnsureRunner(ctx, task.SessionID); err != nil {
		m.logger.Warn("task created but no runner is available; it stays pending",
			zap.String("task_id", task.ID),
			zap.String("session_id", task.SessionID),
			zap.Error(err),
		)
		return
	}

	if err := m.DispatchNext(ctx, task.SessionID); err != nil {
		m.logger.Warn("task created but dispatch failed; it stays pending",
			zap.String("task_id", task.ID),
			zap.String("session_id", task.SessionID),
			zap.Error(err),
		)
	}
}

// Get retrieves a task by ID.
func (m *TaskManager) Get(ctx context.Context, taskID string) (*store.Task, error) {
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}
	return task, nil
}

// List retrieves tasks matching the given options.
func (m *TaskManager) List(ctx context.Context, opts ListTasksOptions) (*store.ListResult[store.Task], error) {
	return m.store.ListTasks(ctx, opts)
}

// Cancel cancels a task.
// This can be called from pending or running state.
func (m *TaskManager) Cancel(ctx context.Context, taskID string) error {
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTaskNotFound
		}
		return err
	}

	// Check if already in terminal state
	if task.Status == TaskStatusCompleted {
		return ErrTaskAlreadyCompleted
	}
	if task.Status == TaskStatusCanceled {
		return ErrTaskAlreadyCanceled
	}

	// Update task status
	updates := store.TaskUpdates{
		Status: stringPtr(TaskStatusCanceled),
	}

	if err := m.store.UpdateTask(ctx, taskID, updates); err != nil {
		return err
	}

	// Cancel any running task runs
	runs, err := m.store.ListTaskRuns(ctx, store.ListTaskRunsOptions{
		TaskID: &taskID,
		Status: []string{TaskRunStatusPending, TaskRunStatusAssigned, TaskRunStatusRunning},
	})
	if err != nil {
		m.logger.Warn("failed to list task runs for cancellation",
			zap.String("task_id", taskID),
			zap.Error(err),
		)
	} else {
		for _, run := range runs.Items {
			if err := m.cancelRun(ctx, run.ID); err != nil {
				m.logger.Warn("failed to cancel task run",
					zap.String("run_id", run.ID),
					zap.Error(err),
				)
			}
		}
	}

	m.logger.Info("task canceled",
		zap.String("task_id", taskID),
		zap.String("from_status", task.Status),
	)

	// Log audit event
	if m.auditLog != nil {
		_ = audit.NewEvent(audit.ActionTaskCanceled).
			WithSystemActor().
			WithResource(audit.ResourceTypeTask, taskID).
			WithSession(task.SessionID).
			WithTask(taskID).
			WithDetails(map[string]any{
				"from_status": task.Status,
			}).
			WithSuccess(true).
			Log(ctx, m.auditLog)
	}

	return nil
}

// Execute sends a task to a runner for execution.
func (m *TaskManager) Execute(ctx context.Context, taskID string) error {
	_, err := m.dispatch(ctx, taskID, dispatchOptions{})
	return err
}

// dispatchOptions tunes a single dispatch attempt.
type dispatchOptions struct {
	// incrementRetry spends one unit of the task's retry budget. The budget is
	// only spent when the dispatch actually reaches a runner.
	incrementRetry bool
}

// dispatch records a task run and sends it to the session's runner.
//
// All database work happens in one transaction, and the command is sent only
// after that transaction commits: a command must never be published for a run
// the database has not durably recorded. Previously the run creation, the task
// status update and the send were three independent steps, so a failed send
// left the task "running" forever with a run nobody would ever finish, and
// Retry burned a retry before it knew whether the dispatch would work at all.
//
// If the send fails, a compensating transaction unwinds the whole attempt: the
// run is marked failed for the audit trail, the retry budget is handed back,
// and the task returns to exactly the state it was in before the dispatch.
func (m *TaskManager) dispatch(ctx context.Context, taskID string, opts dispatchOptions) (*store.TaskRun, error) {
	var (
		run      *store.TaskRun
		runnerID string
		cmd      *pb.ServerCommand
	)

	err := store.WithTx(ctx, m.store, func(tx store.Tx) error {
		task, err := tx.GetTask(ctx, taskID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return ErrTaskNotFound
			}
			return err
		}

		if opts.incrementRetry && task.RetryCount >= task.MaxRetries {
			return ErrMaxRetriesExceeded
		}

		session, err := tx.GetSession(ctx, task.SessionID)
		if err != nil {
			return err
		}
		if session.RunnerID == nil || *session.RunnerID == "" {
			return ErrNoRunnerAttached
		}
		runnerID = *session.RunnerID

		retryCount := task.RetryCount
		if opts.incrementRetry {
			retryCount++
		}

		now := time.Now()
		run = &store.TaskRun{
			ID:        id.TaskRun(),
			TaskID:    taskID,
			Attempt:   retryCount + 1,
			RunnerID:  session.RunnerID,
			Status:    TaskRunStatusPending,
			TenantID:  task.TenantID,
			QueuedAt:  now,
			UpdatedAt: now,
		}
		if err := tx.CreateTaskRun(ctx, run); err != nil {
			// UNIQUE(task_id, attempt) is the second guard, and the only one on
			// the retry path: two racing retries compute the same attempt from
			// the same retry_count, so the loser trips the constraint.
			if errors.Is(err, store.ErrAlreadyExists) {
				return ErrDispatchRaceLost
			}
			return err
		}

		updates := store.TaskUpdates{Status: stringPtr(TaskStatusRunning)}
		if opts.incrementRetry {
			updates.RetryCount = &retryCount
		} else {
			// Compare-and-set on the status. Two dispatchers can both read
			// "pending" under READ COMMITTED; only one can write past this,
			// and the loser's whole transaction rolls back. The in-memory
			// per-session lock is now a contention optimisation rather than
			// the thing correctness rests on - which matters the moment there
			// is a second server process.
			updates.ExpectedStatus = stringPtr(TaskStatusPending)
		}
		if err := tx.UpdateTask(ctx, taskID, updates); err != nil {
			if errors.Is(err, store.ErrConflict) {
				return ErrDispatchRaceLost
			}
			return err
		}

		// Note: Attempt is bounded by MaxRetries (small value), so the int32
		// conversion is safe.
		cmd = &pb.ServerCommand{
			Payload: &pb.ServerCommand_ExecuteTask{
				ExecuteTask: &pb.ExecuteTask{
					TaskId:    task.ID,
					RunId:     run.ID,
					Attempt:   int32(run.Attempt), //nolint:gosec // Attempt is bounded by MaxRetries
					SessionId: session.ID,
					Prompt:    task.Prompt,
					Sandbox: &pb.SandboxConfig{
						TimeoutSeconds: int64(task.TimeoutSeconds),
					},
				},
			},
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if sendErr := m.cmdSender.SendCommand(runnerID, cmd); sendErr != nil {
		m.unwindDispatch(ctx, taskID, run.ID, opts, sendErr)
		return nil, sendErr
	}

	m.logger.Info("task execution started",
		zap.String("task_id", taskID),
		zap.String("run_id", run.ID),
		zap.String("runner_id", runnerID),
		zap.Int("attempt", run.Attempt),
	)

	return run, nil
}

// unwindDispatch compensates for a dispatch whose command never reached a
// runner. Best-effort: if the compensating write fails too, the stale
// "running" task is picked up by the task timeout enforcer.
func (m *TaskManager) unwindDispatch(ctx context.Context, taskID, runID string, opts dispatchOptions, cause error) {
	reason := "failed to send command to runner: " + cause.Error()

	err := store.WithTx(ctx, m.store, func(tx store.Tx) error {
		now := time.Now()
		if err := tx.UpdateTaskRun(ctx, runID, store.TaskRunUpdates{
			Status:  stringPtr(TaskRunStatusFailed),
			Error:   &reason,
			EndedAt: &now,
		}); err != nil {
			return err
		}

		task, err := tx.GetTask(ctx, taskID)
		if err != nil {
			return err
		}

		// The task never reached a runner, so put it back exactly where it was:
		// pending, with its retry budget intact.
		updates := store.TaskUpdates{Status: stringPtr(TaskStatusPending)}
		if opts.incrementRetry && task.RetryCount > 0 {
			restored := task.RetryCount - 1
			updates.RetryCount = &restored
		}
		return tx.UpdateTask(ctx, taskID, updates)
	})

	if err != nil {
		m.logger.Error("failed to unwind task dispatch; task may be stuck running",
			zap.String("task_id", taskID),
			zap.String("run_id", runID),
			zap.Error(err),
		)
		return
	}

	m.logger.Warn("task dispatch unwound after send failure",
		zap.String("task_id", taskID),
		zap.String("run_id", runID),
		zap.String("reason", reason),
	)
}

// ReExecute re-sends a running task to a runner after session resume.
// Unlike Execute, this reuses the existing task_run instead of creating a new one.
// This is used when a session resumes and needs to continue a task that was
// interrupted by suspend.
func (m *TaskManager) ReExecute(ctx context.Context, taskID string) error {
	// Get task
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTaskNotFound
		}
		return err
	}

	// Task must be in running status
	if task.Status != TaskStatusRunning {
		return fmt.Errorf("task is not running: status=%s", task.Status)
	}

	// Get session
	session, err := m.store.GetSession(ctx, task.SessionID)
	if err != nil {
		return err
	}

	// Check if session has a runner
	if session.RunnerID == nil || *session.RunnerID == "" {
		return ErrNoRunnerAttached
	}

	// Find the existing running task_run
	runs, err := m.store.ListTaskRuns(ctx, store.ListTaskRunsOptions{
		TaskID: &taskID,
		Status: []string{TaskRunStatusPending, TaskRunStatusAssigned, TaskRunStatusRunning},
	})
	if err != nil {
		return err
	}

	if len(runs.Items) == 0 {
		return fmt.Errorf("no running task_run found for task %s", taskID)
	}

	// Use the most recent running task_run
	run := runs.Items[0]

	// Update runner_id in case it changed (e.g., different runner after resume)
	if err := m.store.UpdateTaskRun(ctx, run.ID, store.TaskRunUpdates{
		RunnerID: session.RunnerID,
	}); err != nil {
		return err
	}

	// Send ExecuteTask command to runner
	cmd := &pb.ServerCommand{
		Payload: &pb.ServerCommand_ExecuteTask{
			ExecuteTask: &pb.ExecuteTask{
				TaskId:    task.ID,
				RunId:     run.ID,
				Attempt:   int32(run.Attempt), //nolint:gosec // Attempt is bounded by MaxRetries
				SessionId: session.ID,
				Prompt:    task.Prompt,
				Sandbox: &pb.SandboxConfig{
					TimeoutSeconds: int64(task.TimeoutSeconds),
				},
			},
		},
	}

	if err := m.cmdSender.SendCommand(*session.RunnerID, cmd); err != nil {
		return err
	}

	m.logger.Info("task re-execution started after resume",
		zap.String("task_id", taskID),
		zap.String("run_id", run.ID),
		zap.String("runner_id", *session.RunnerID),
	)

	return nil
}

// ListRuns returns the execution attempts of a task, oldest attempt first.
//
// A task's history is its runs: which runner took it, how it ended, what it
// cost. The task row only carries the latest status, so this is the only way to
// see a retry that failed before the one that succeeded.
func (m *TaskManager) ListRuns(ctx context.Context, taskID string, opts ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	// Confirm the task exists so an unknown ID is a 404 rather than an empty
	// list that looks like a task which has never run.
	if _, err := m.store.GetTask(ctx, taskID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}

	opts.TaskID = &taskID
	result, err := m.store.ListTaskRuns(ctx, opts)
	if err != nil {
		return nil, err
	}

	// Attempt order is the meaningful one for a history, and it does not depend
	// on a store's default sort.
	sort.SliceStable(result.Items, func(i, j int) bool {
		return result.Items[i].Attempt < result.Items[j].Attempt
	})

	return result, nil
}

// CreateRun creates a new task run.
func (m *TaskManager) CreateRun(ctx context.Context, taskID string) (*store.TaskRun, error) {
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrTaskNotFound
		}
		return nil, err
	}

	// Get session for runner ID
	session, err := m.store.GetSession(ctx, task.SessionID)
	if err != nil {
		return nil, err
	}

	// Calculate attempt number
	attempt := task.RetryCount + 1

	now := time.Now()
	run := &store.TaskRun{
		ID:        id.TaskRun(),
		TaskID:    taskID,
		Attempt:   attempt,
		RunnerID:  session.RunnerID,
		Status:    TaskRunStatusPending,
		TenantID:  task.TenantID,
		QueuedAt:  now,
		UpdatedAt: now,
	}

	if err := m.store.CreateTaskRun(ctx, run); err != nil {
		return nil, err
	}

	m.logger.Debug("task run created",
		zap.String("run_id", run.ID),
		zap.String("task_id", taskID),
		zap.Int("attempt", attempt),
	)

	return run, nil
}

// OnTaskAccepted is called when a runner accepts a task (pending → assigned).
func (m *TaskManager) OnTaskAccepted(ctx context.Context, runID string) error {
	run, err := m.store.GetTaskRun(ctx, runID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTaskRunNotFound
		}
		return err
	}

	// Idempotent: if already assigned (e.g., after session resume), just return success
	if run.Status == TaskRunStatusAssigned {
		m.logger.Debug("task run already accepted (idempotent)",
			zap.String("run_id", runID),
		)
		return nil
	}

	if !isValidTaskRunTransition(run.Status, TaskRunStatusAssigned) {
		m.logger.Warn("invalid task run transition",
			zap.String("run_id", runID),
			zap.String("from", run.Status),
			zap.String("to", TaskRunStatusAssigned),
		)
		return ErrInvalidTaskRunTransition
	}

	now := time.Now()
	updates := store.TaskRunUpdates{
		Status:     stringPtr(TaskRunStatusAssigned),
		AssignedAt: &now,
	}

	if err := m.store.UpdateTaskRun(ctx, runID, updates); err != nil {
		return err
	}

	m.logger.Debug("task run accepted",
		zap.String("run_id", runID),
		zap.String("task_id", run.TaskID),
	)

	return nil
}

// OnTaskStarted is called when a runner starts executing a task (assigned → running).
func (m *TaskManager) OnTaskStarted(ctx context.Context, runID string) error {
	run, err := m.store.GetTaskRun(ctx, runID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTaskRunNotFound
		}
		return err
	}

	// Idempotent: if already running (e.g., after session resume), just return success
	if run.Status == TaskRunStatusRunning {
		m.logger.Debug("task run already started (idempotent)",
			zap.String("run_id", runID),
		)
		return nil
	}

	if !isValidTaskRunTransition(run.Status, TaskRunStatusRunning) {
		m.logger.Warn("invalid task run transition",
			zap.String("run_id", runID),
			zap.String("from", run.Status),
			zap.String("to", TaskRunStatusRunning),
		)
		return ErrInvalidTaskRunTransition
	}

	now := time.Now()
	updates := store.TaskRunUpdates{
		Status:    stringPtr(TaskRunStatusRunning),
		StartedAt: &now,
	}

	if err := m.store.UpdateTaskRun(ctx, runID, updates); err != nil {
		return err
	}

	m.logger.Debug("task run started",
		zap.String("run_id", runID),
		zap.String("task_id", run.TaskID),
	)

	return nil
}

// OnTaskProgress is called when a runner reports progress.
// Currently a no-op since we don't store progress in DB, but could be used for real-time updates.
func (m *TaskManager) OnTaskProgress(_ context.Context, runID string, progress int) error {
	m.logger.Debug("task run progress",
		zap.String("run_id", runID),
		zap.Int("progress", progress),
	)
	// Future: emit to websocket subscribers
	return nil
}

// OnTaskCompleted is called when a task run completes (running → completed/failed).
func (m *TaskManager) OnTaskCompleted(ctx context.Context, result *TaskCompletedResult) error {
	run, err := m.store.GetTaskRun(ctx, result.RunID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTaskRunNotFound
		}
		return err
	}

	now := time.Now()
	var runStatus string
	if result.Success {
		runStatus = TaskRunStatusCompleted
	} else {
		runStatus = TaskRunStatusFailed
	}

	updates := store.TaskRunUpdates{
		Status:       stringPtr(runStatus),
		EndedAt:      &now,
		TokensInput:  &result.TokensInput,
		TokensOutput: &result.TokensOutput,
		ExitCode:     result.ExitCode,
	}

	if result.Error != "" {
		updates.Error = &result.Error
	}

	if err := m.store.UpdateTaskRun(ctx, result.RunID, updates); err != nil {
		return err
	}

	// Update task status
	var taskStatus string
	switch {
	case result.Success:
		taskStatus = TaskStatusCompleted
	default:
		// Check if we should retry
		shouldRetry, retryErr := m.ShouldRetry(ctx, run.TaskID)
		switch {
		case retryErr != nil:
			m.logger.Warn("failed to check retry policy",
				zap.String("task_id", run.TaskID),
				zap.Error(retryErr),
			)
			taskStatus = TaskStatusFailed
		case shouldRetry:
			// Will retry - keep status as running
			m.logger.Info("task will be retried",
				zap.String("task_id", run.TaskID),
				zap.String("run_id", result.RunID),
			)
			// Trigger the retry on the bounded background pool. This used to
			// be a bare goroutine per failed run: unbounded, immediate, with
			// no backoff, no recover and nothing draining it on shutdown.
			m.scheduleRetry(run.TaskID)
			return nil
		default:
			taskStatus = TaskStatusFailed
		}
	}

	if err := m.store.UpdateTask(ctx, run.TaskID, store.TaskUpdates{
		Status: stringPtr(taskStatus),
	}); err != nil {
		return err
	}

	m.logger.Info("task run completed",
		zap.String("run_id", result.RunID),
		zap.String("task_id", run.TaskID),
		zap.Bool("success", result.Success),
		zap.String("task_status", taskStatus),
	)

	// Dispatch webhook event
	if m.webhooks != nil {
		if task, err := m.store.GetTask(ctx, run.TaskID); err == nil {
			eventType := "task.completed"
			if taskStatus == TaskStatusFailed {
				eventType = "task.failed"
			}
			m.webhooks.DispatchTaskEvent(ctx, eventType, task, run)
		}
	}

	return nil
}

// FailRun marks a task run as failed.
func (m *TaskManager) FailRun(ctx context.Context, runID, reason string) error {
	now := time.Now()
	updates := store.TaskRunUpdates{
		Status:  stringPtr(TaskRunStatusFailed),
		Error:   &reason,
		EndedAt: &now,
	}

	if err := m.store.UpdateTaskRun(ctx, runID, updates); err != nil {
		return err
	}

	m.logger.Info("task run failed",
		zap.String("run_id", runID),
		zap.String("reason", reason),
	)

	return nil
}

// ShouldRetry checks if a task should be retried.
func (m *TaskManager) ShouldRetry(ctx context.Context, taskID string) (bool, error) {
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return false, err
	}

	// Check if we've exceeded max retries
	if task.RetryCount >= task.MaxRetries {
		return false, nil
	}

	// Check if task is in a state that allows retry
	if task.Status == TaskStatusCompleted || task.Status == TaskStatusCanceled {
		return false, nil
	}

	return true, nil
}

// Retry creates a new run for a failed task.
//
// The retry budget is spent in the same transaction that records the new run,
// and handed back if the dispatch never reaches a runner - so a runner that is
// simply unreachable can no longer burn a task's entire retry budget.
func (m *TaskManager) Retry(ctx context.Context, taskID string) (*store.TaskRun, error) {
	run, err := m.dispatch(ctx, taskID, dispatchOptions{incrementRetry: true})
	if err != nil {
		return nil, err
	}

	m.logger.Info("retrying task",
		zap.String("task_id", taskID),
		zap.String("run_id", run.ID),
		zap.Int("attempt", run.Attempt),
	)

	return run, nil
}

// scheduleRetry queues a retry on the bounded background pool, after a jittered
// backoff. A rejected schedule (pool saturated or shutting down) is logged: the
// task stays running and the task timeout enforcer will pick it up.
func (m *TaskManager) scheduleRetry(taskID string) {
	delay := m.retryDelayFor(taskID)

	accepted := m.background.Go("task-retry", func(ctx context.Context) {
		if !sleepCtx(ctx, delay) {
			m.logger.Debug("retry abandoned during shutdown", zap.String("task_id", taskID))
			return
		}
		if _, err := m.Retry(ctx, taskID); err != nil {
			m.logger.Error("failed to retry task",
				zap.String("task_id", taskID),
				zap.Error(err),
			)
		}
	})

	if !accepted {
		m.logger.Warn("retry not scheduled; task will be picked up by the timeout enforcer",
			zap.String("task_id", taskID),
		)
	}
}

// retryDelayFor derives the backoff from how many retries the task has already
// spent, so repeated failures back off instead of hammering the runner.
func (m *TaskManager) retryDelayFor(taskID string) time.Duration {
	task, err := m.store.GetTask(context.Background(), taskID)
	if err != nil {
		return retryDelay(0)
	}
	return retryDelay(task.RetryCount)
}

// cancelRun marks a task run as canceled.
func (m *TaskManager) cancelRun(ctx context.Context, runID string) error {
	now := time.Now()
	updates := store.TaskRunUpdates{
		Status:  stringPtr(TaskRunStatusCanceled),
		EndedAt: &now,
	}

	if err := m.store.UpdateTaskRun(ctx, runID, updates); err != nil {
		return err
	}

	m.logger.Debug("task run canceled",
		zap.String("run_id", runID),
	)

	return nil
}

// isValidTaskTransition checks if a task status transition is valid.
//
// Valid transitions:
//   - pending → running (execution started)
//   - running → completed (success)
//   - running → failed (error)
//   - pending → canceled
//   - running → canceled
func isValidTaskTransition(from, to string) bool {
	switch from {
	case TaskStatusPending:
		return to == TaskStatusRunning || to == TaskStatusCanceled
	case TaskStatusRunning:
		return to == TaskStatusCompleted || to == TaskStatusFailed || to == TaskStatusCanceled
	case TaskStatusCompleted, TaskStatusFailed, TaskStatusCanceled:
		return false // Terminal states
	default:
		return false
	}
}

// IsValidTaskTransition is exported for testing.
func IsValidTaskTransition(from, to string) bool {
	return isValidTaskTransition(from, to)
}

// isValidTaskRunTransition checks if a task run status transition is valid.
//
// Valid transitions:
//   - pending → assigned (runner accepts)
//   - assigned → running (execution starts)
//   - running → completed/failed/timeout (execution ends)
//   - pending/assigned/running → canceled
func isValidTaskRunTransition(from, to string) bool {
	// Cancel is always allowed from non-terminal states
	if to == TaskRunStatusCanceled {
		return from == TaskRunStatusPending ||
			from == TaskRunStatusAssigned ||
			from == TaskRunStatusRunning
	}

	switch from {
	case TaskRunStatusPending:
		return to == TaskRunStatusAssigned
	case TaskRunStatusAssigned:
		return to == TaskRunStatusRunning
	case TaskRunStatusRunning:
		return to == TaskRunStatusCompleted ||
			to == TaskRunStatusFailed ||
			to == TaskRunStatusTimeout
	case TaskRunStatusCompleted, TaskRunStatusFailed, TaskRunStatusTimeout, TaskRunStatusCanceled:
		return false // Terminal states
	default:
		return false
	}
}

// IsValidTaskRunTransition is exported for testing.
func IsValidTaskRunTransition(from, to string) bool {
	return isValidTaskRunTransition(from, to)
}

// Default timeout enforcement configuration.
const (
	DefaultTimeoutCheckInterval = 30 * time.Second
)

// TaskTimeoutEnforcer monitors running tasks and enforces timeouts.
type TaskTimeoutEnforcer struct {
	store         store.Store
	taskMgr       *TaskManager
	cmdSender     CommandSender
	checkInterval time.Duration
	logger        *zap.Logger

	stopCh chan struct{}
	doneCh chan struct{}
}

// TaskTimeoutEnforcerOption is a functional option for TaskTimeoutEnforcer.
type TaskTimeoutEnforcerOption func(*TaskTimeoutEnforcer)

// WithTimeoutCheckInterval sets the check interval for the timeout enforcer.
func WithTimeoutCheckInterval(d time.Duration) TaskTimeoutEnforcerOption {
	return func(e *TaskTimeoutEnforcer) {
		e.checkInterval = d
	}
}

// NewTaskTimeoutEnforcer creates a new TaskTimeoutEnforcer.
func NewTaskTimeoutEnforcer(
	store store.Store,
	taskMgr *TaskManager,
	cmdSender CommandSender,
	logger *zap.Logger,
	opts ...TaskTimeoutEnforcerOption,
) *TaskTimeoutEnforcer {
	e := &TaskTimeoutEnforcer{
		store:         store,
		taskMgr:       taskMgr,
		cmdSender:     cmdSender,
		checkInterval: DefaultTimeoutCheckInterval,
		logger:        logger,
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
	}

	for _, opt := range opts {
		opt(e)
	}

	return e
}

// Start begins the background timeout enforcement loop.
func (e *TaskTimeoutEnforcer) Start(ctx context.Context) {
	e.logger.Info("starting task timeout enforcer",
		zap.Duration("check_interval", e.checkInterval),
	)

	go e.run(ctx)
}

// Stop stops the timeout enforcer.
func (e *TaskTimeoutEnforcer) Stop() {
	e.logger.Info("stopping task timeout enforcer")
	close(e.stopCh)
	<-e.doneCh
	e.logger.Info("task timeout enforcer stopped")
}

// run is the main loop for the timeout enforcer.
func (e *TaskTimeoutEnforcer) run(ctx context.Context) {
	defer close(e.doneCh)

	ticker := time.NewTicker(e.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-e.stopCh:
			return
		case <-ticker.C:
			if err := e.checkTimeouts(ctx); err != nil {
				e.logger.Error("failed to check task timeouts", zap.Error(err))
			}
		}
	}
}

// checkTimeouts checks all running task runs for timeout.
func (e *TaskTimeoutEnforcer) checkTimeouts(ctx context.Context) error {
	// Get all running task runs
	runs, err := e.store.ListTaskRuns(ctx, store.ListTaskRunsOptions{
		Status: []string{TaskRunStatusRunning},
	})
	if err != nil {
		return err
	}

	if len(runs.Items) == 0 {
		return nil
	}

	now := time.Now()
	timeoutCount := 0

	for _, run := range runs.Items {
		// Get the task to check timeout
		task, err := e.store.GetTask(ctx, run.TaskID)
		if err != nil {
			e.logger.Warn("failed to get task for timeout check",
				zap.String("run_id", run.ID),
				zap.String("task_id", run.TaskID),
				zap.Error(err),
			)
			continue
		}

		// Check if task has exceeded timeout
		if run.StartedAt == nil {
			continue
		}

		elapsed := now.Sub(*run.StartedAt)
		timeout := time.Duration(task.TimeoutSeconds) * time.Second

		if elapsed > timeout {
			e.logger.Warn("task run exceeded timeout",
				zap.String("run_id", run.ID),
				zap.String("task_id", run.TaskID),
				zap.Duration("elapsed", elapsed),
				zap.Duration("timeout", timeout),
			)

			// Mark run as timed out
			if err := e.timeoutRun(ctx, run, task); err != nil {
				e.logger.Error("failed to timeout task run",
					zap.String("run_id", run.ID),
					zap.Error(err),
				)
				continue
			}
			timeoutCount++
		}
	}

	if timeoutCount > 0 {
		e.logger.Info("timed out task runs",
			zap.Int("count", timeoutCount),
		)
	}

	return nil
}

// timeoutRun marks a task run as timed out and handles cleanup.
func (e *TaskTimeoutEnforcer) timeoutRun(ctx context.Context, run *store.TaskRun, task *store.Task) error {
	// Update task run status to timeout
	now := time.Now()
	updates := store.TaskRunUpdates{
		Status:  stringPtr(TaskRunStatusTimeout),
		Error:   stringPtr("task execution timed out"),
		EndedAt: &now,
	}

	if err := e.store.UpdateTaskRun(ctx, run.ID, updates); err != nil {
		return err
	}

	// Send kill command to runner if connected
	if run.RunnerID != nil && e.cmdSender != nil {
		killCmd := &pb.ServerCommand{
			Payload: &pb.ServerCommand_KillTask{
				KillTask: &pb.KillTask{
					TaskId: task.ID,
					RunId:  run.ID,
					Reason: "timeout",
				},
			},
		}

		if err := e.cmdSender.SendCommand(*run.RunnerID, killCmd); err != nil {
			e.logger.Warn("failed to send kill command to runner",
				zap.String("run_id", run.ID),
				zap.String("runner_id", *run.RunnerID),
				zap.Error(err),
			)
			// Continue - the timeout is still recorded
		}
	}

	// Check if we should retry
	if e.taskMgr != nil {
		shouldRetry, retryErr := e.taskMgr.ShouldRetry(ctx, task.ID)
		switch {
		case retryErr != nil:
			e.logger.Warn("failed to check retry policy after timeout",
				zap.String("task_id", task.ID),
				zap.Error(retryErr),
			)
		case shouldRetry:
			e.logger.Info("task will be retried after timeout",
				zap.String("task_id", task.ID),
			)
			// Same bounded path as a failed run: see TaskManager.scheduleRetry.
			e.taskMgr.scheduleRetry(task.ID)
		default:
			// No more retries - mark task as failed
			if err := e.store.UpdateTask(ctx, task.ID, store.TaskUpdates{
				Status: stringPtr(TaskStatusFailed),
			}); err != nil {
				e.logger.Warn("failed to mark task as failed after timeout",
					zap.String("task_id", task.ID),
					zap.Error(err),
				)
			}
		}
	}

	e.logger.Info("task run timed out",
		zap.String("run_id", run.ID),
		zap.String("task_id", task.ID),
	)

	return nil
}
