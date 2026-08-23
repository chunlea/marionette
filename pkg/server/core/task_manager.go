package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
	CreateRun(ctx context.Context, taskID string) (*store.TaskRun, error)
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
	logger     *zap.Logger
}

// TaskManagerOption is a functional option for TaskManager.
type TaskManagerOption func(*TaskManager)

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

	// Use session's tenant ID if not specified
	tenantID := opts.TenantID
	if tenantID == nil {
		tenantID = session.TenantID
	}

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

	return task, nil
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
	// Get task
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTaskNotFound
		}
		return err
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

	// Create a new task run
	run, err := m.CreateRun(ctx, taskID)
	if err != nil {
		return err
	}

	// Update task status to running
	if err := m.store.UpdateTask(ctx, taskID, store.TaskUpdates{
		Status: stringPtr(TaskStatusRunning),
	}); err != nil {
		return err
	}

	// Send ExecuteTask command to runner
	// Note: Attempt is bounded by MaxRetries (small value), so int32 conversion is safe
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
		// If we can't send the command, fail the run
		_ = m.FailRun(ctx, run.ID, "failed to send command to runner: "+err.Error())
		return err
	}

	m.logger.Info("task execution started",
		zap.String("task_id", taskID),
		zap.String("run_id", run.ID),
		zap.String("runner_id", *session.RunnerID),
	)

	return nil
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
			// Trigger retry asynchronously
			go func() {
				retryCtx := context.Background()
				if _, err := m.Retry(retryCtx, run.TaskID); err != nil {
					m.logger.Error("failed to retry task",
						zap.String("task_id", run.TaskID),
						zap.Error(err),
					)
				}
			}()
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
func (m *TaskManager) Retry(ctx context.Context, taskID string) (*store.TaskRun, error) {
	task, err := m.store.GetTask(ctx, taskID)
	if err != nil {
		return nil, err
	}

	// Validate retry is allowed
	if task.RetryCount >= task.MaxRetries {
		return nil, ErrMaxRetriesExceeded
	}

	// Increment retry count
	newRetryCount := task.RetryCount + 1
	if err := m.store.UpdateTask(ctx, taskID, store.TaskUpdates{
		RetryCount: &newRetryCount,
	}); err != nil {
		return nil, err
	}

	m.logger.Info("retrying task",
		zap.String("task_id", taskID),
		zap.Int("attempt", newRetryCount+1),
		zap.Int("max_retries", task.MaxRetries),
	)

	// Execute the task again
	if err := m.Execute(ctx, taskID); err != nil {
		return nil, err
	}

	// Get the newly created run
	runs, err := m.store.ListTaskRuns(ctx, store.ListTaskRunsOptions{
		TaskID: &taskID,
	})
	if err != nil {
		return nil, err
	}

	if len(runs.Items) == 0 {
		return nil, errors.New("no runs found after retry")
	}

	// Return the most recent run (last in list by default ordering)
	return runs.Items[len(runs.Items)-1], nil
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
			// Trigger retry asynchronously
			go func() {
				retryCtx := context.Background()
				if _, err := e.taskMgr.Retry(retryCtx, task.ID); err != nil {
					e.logger.Error("failed to retry timed out task",
						zap.String("task_id", task.ID),
						zap.Error(err),
					)
				}
			}()
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
