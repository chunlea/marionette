package core

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"text/template"
	"time"

	"github.com/robfig/cron/v3"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/audit"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// ScheduledTask status constants.
const (
	ScheduledTaskStatusActive   = "active"
	ScheduledTaskStatusPaused   = "paused"
	ScheduledTaskStatusDisabled = "disabled"
)

// ScheduledTask OnFailure policies.
const (
	OnFailureContinue         = "continue"
	OnFailurePauseOnFailure   = "pause_on_failure"
	OnFailureDisableOnFailure = "disable_on_failure"
)

// Default scheduled task configuration.
const (
	DefaultScheduledTaskTimeoutSeconds = 3600 // 1 hour
	DefaultScheduledTaskMaxRetries     = 0
	DefaultMaxConsecutiveFailures      = 3
	DefaultScheduledTaskTimezone       = "UTC"
)

// ScheduledTask-related errors.
var (
	ErrScheduledTaskNotFound       = errors.New("scheduled task not found")
	ErrInvalidCronExpression       = errors.New("invalid cron expression")
	ErrInvalidTimezone             = errors.New("invalid timezone")
	ErrScheduledTaskNameRequired   = errors.New("name is required")
	ErrScheduledTaskPromptRequired = errors.New("prompt_template is required")
	ErrScheduledTaskCronRequired   = errors.New("cron_expression is required")
	ErrScheduledTaskAlreadyActive  = errors.New("scheduled task is already active")
	ErrScheduledTaskAlreadyPaused  = errors.New("scheduled task is already paused")
	ErrScheduledTaskDisabled       = errors.New("scheduled task is disabled")

	// ErrScheduledTickTaken reports that another replica already claimed this
	// run.
	ErrScheduledTickTaken = errors.New("scheduled task tick already claimed")
)

// ScheduledTaskServiceInterface defines the interface for scheduled task management.
type ScheduledTaskServiceInterface interface {
	Create(ctx context.Context, opts CreateScheduledTaskOptions) (*store.ScheduledTask, error)
	Get(ctx context.Context, taskID string) (*store.ScheduledTask, error)
	List(ctx context.Context, opts ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error)
	Update(ctx context.Context, taskID string, opts UpdateScheduledTaskOptions) (*store.ScheduledTask, error)
	Delete(ctx context.Context, taskID string) error
	Pause(ctx context.Context, taskID string) error
	Resume(ctx context.Context, taskID string) error
	Trigger(ctx context.Context, taskID string) (*store.Task, error)
	GetDue(ctx context.Context, limit int) ([]*store.ScheduledTask, error)
	ExecuteScheduledTask(ctx context.Context, scheduledTask *store.ScheduledTask) (*store.Task, error)
	ExecuteDue(ctx context.Context, scheduledTask *store.ScheduledTask) (*store.Task, error)
	MarkTaskCompleted(ctx context.Context, scheduledTaskID string, success bool) error
	CalculateNextRunAt(cronExpr, timezone string, after time.Time) (*time.Time, error)
}

// ScheduledTaskService handles scheduled task lifecycle.
type ScheduledTaskService struct {
	store      store.Store
	taskMgr    TaskManagerInterface
	auditLog   audit.Logger
	logger     *zap.Logger
	cronParser cron.Parser
}

// NewScheduledTaskService creates a new ScheduledTaskService.
func NewScheduledTaskService(
	store store.Store,
	taskMgr TaskManagerInterface,
	auditLog audit.Logger,
	logger *zap.Logger,
) *ScheduledTaskService {
	return &ScheduledTaskService{
		store:      store,
		taskMgr:    taskMgr,
		auditLog:   auditLog,
		logger:     logger,
		cronParser: cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow),
	}
}

// CreateScheduledTaskOptions contains options for creating a scheduled task.
type CreateScheduledTaskOptions struct {
	SessionID              string            // Required
	Name                   string            // Required
	Description            string            // Optional
	CronExpression         string            // Required: e.g., "0 9 * * 1-5"
	Timezone               string            // Default: "UTC"
	PromptTemplate         string            // Required: may contain {{.Date}}, {{.RunNumber}}
	TimeoutSeconds         int               // Default: 3600
	MaxRetries             int               // Default: 0
	OnFailure              string            // Default: "continue"
	MaxConsecutiveFailures *int              // Default: 3
	TenantID               *string           // For multi-tenant deployments
	Labels                 map[string]string // Optional metadata labels
	Annotations            map[string]string // Optional metadata annotations
}

// ListScheduledTasksOptions wraps store.ListScheduledTasksOptions.
type ListScheduledTasksOptions = store.ListScheduledTasksOptions

// UpdateScheduledTaskOptions contains options for updating a scheduled task.
type UpdateScheduledTaskOptions struct {
	Name                   *string
	Description            *string
	CronExpression         *string
	Timezone               *string
	PromptTemplate         *string
	TimeoutSeconds         *int
	MaxRetries             *int
	OnFailure              *string
	MaxConsecutiveFailures *int
	Labels                 map[string]string
	Annotations            map[string]string
}

// PromptTemplateData contains data for rendering prompt templates.
type PromptTemplateData struct {
	Date         string // Current date in YYYY-MM-DD format
	DateTime     string // Current date and time in RFC3339 format
	RunNumber    int    // Total number of runs including this one
	TaskName     string // Name of the scheduled task
	SessionID    string // Session ID
	ScheduledFor string // Scheduled run time in RFC3339 format
}

// Create creates a new scheduled task.
func (s *ScheduledTaskService) Create(ctx context.Context, opts CreateScheduledTaskOptions) (*store.ScheduledTask, error) {
	// Validate required fields
	if opts.SessionID == "" {
		return nil, ErrSessionRequired
	}
	if opts.Name == "" {
		return nil, ErrScheduledTaskNameRequired
	}
	if opts.CronExpression == "" {
		return nil, ErrScheduledTaskCronRequired
	}
	if opts.PromptTemplate == "" {
		return nil, ErrScheduledTaskPromptRequired
	}

	// Validate session exists
	_, err := s.store.GetSession(ctx, opts.SessionID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrSessionNotFound
		}
		return nil, fmt.Errorf("getting session: %w", err)
	}

	// Validate cron expression
	_, err = s.cronParser.Parse(opts.CronExpression)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCronExpression, err)
	}

	// Validate and set timezone
	tz := opts.Timezone
	if tz == "" {
		tz = DefaultScheduledTaskTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTimezone, err)
	}

	// Calculate next run time
	now := time.Now().In(loc)
	nextRunAt, err := s.CalculateNextRunAt(opts.CronExpression, tz, now)
	if err != nil {
		return nil, fmt.Errorf("calculating next run time: %w", err)
	}

	// Set defaults
	timeoutSeconds := opts.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultScheduledTaskTimeoutSeconds
	}
	onFailure := opts.OnFailure
	if onFailure == "" {
		onFailure = OnFailureContinue
	}
	maxConsecutiveFailures := DefaultMaxConsecutiveFailures
	if opts.MaxConsecutiveFailures != nil {
		maxConsecutiveFailures = *opts.MaxConsecutiveFailures
	}

	// Convert labels and annotations to JSON
	labelsJSON, err := mapToJSON(opts.Labels)
	if err != nil {
		return nil, fmt.Errorf("encoding labels: %w", err)
	}
	annotationsJSON, err := mapToJSON(opts.Annotations)
	if err != nil {
		return nil, fmt.Errorf("encoding annotations: %w", err)
	}

	// Handle optional description
	var description *string
	if opts.Description != "" {
		description = &opts.Description
	}

	// Create scheduled task
	task := &store.ScheduledTask{
		ID:                     id.ScheduledTask(),
		SessionID:              opts.SessionID,
		Name:                   opts.Name,
		Description:            description,
		CronExpression:         opts.CronExpression,
		Timezone:               tz,
		PromptTemplate:         opts.PromptTemplate,
		TimeoutSeconds:         timeoutSeconds,
		MaxRetries:             opts.MaxRetries,
		Status:                 ScheduledTaskStatusActive,
		NextRunAt:              nextRunAt,
		RunCount:               0,
		FailureCount:           0,
		OnFailure:              onFailure,
		MaxConsecutiveFailures: &maxConsecutiveFailures,
		ConsecutiveFailures:    0,
		TenantID:               opts.TenantID,
		Labels:                 labelsJSON,
		Annotations:            annotationsJSON,
	}

	if err := s.store.CreateScheduledTask(ctx, task); err != nil {
		return nil, fmt.Errorf("creating scheduled task: %w", err)
	}

	s.logger.Info("scheduled task created",
		zap.String("id", task.ID),
		zap.String("name", task.Name),
		zap.String("session_id", task.SessionID),
		zap.String("cron", task.CronExpression),
		zap.Timep("next_run_at", task.NextRunAt),
	)

	return task, nil
}

// Get retrieves a scheduled task by ID.
func (s *ScheduledTaskService) Get(ctx context.Context, taskID string) (*store.ScheduledTask, error) {
	task, err := s.store.GetScheduledTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrScheduledTaskNotFound
		}
		return nil, fmt.Errorf("getting scheduled task: %w", err)
	}
	return task, nil
}

// List retrieves scheduled tasks with filtering.
func (s *ScheduledTaskService) List(ctx context.Context, opts ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	return s.store.ListScheduledTasks(ctx, opts)
}

// Update updates a scheduled task.
func (s *ScheduledTaskService) Update(ctx context.Context, taskID string, opts UpdateScheduledTaskOptions) (*store.ScheduledTask, error) {
	task, err := s.store.GetScheduledTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrScheduledTaskNotFound
		}
		return nil, fmt.Errorf("getting scheduled task: %w", err)
	}

	updates := store.ScheduledTaskUpdates{}
	needRecalcNextRun := false

	if opts.Name != nil {
		updates.Name = opts.Name
	}
	if opts.Description != nil {
		updates.Description = opts.Description
	}
	if opts.CronExpression != nil {
		// Validate cron expression
		_, err := s.cronParser.Parse(*opts.CronExpression)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidCronExpression, err)
		}
		updates.CronExpression = opts.CronExpression
		needRecalcNextRun = true
	}
	if opts.Timezone != nil {
		// Validate timezone
		_, err := time.LoadLocation(*opts.Timezone)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrInvalidTimezone, err)
		}
		updates.Timezone = opts.Timezone
		needRecalcNextRun = true
	}
	if opts.PromptTemplate != nil {
		updates.PromptTemplate = opts.PromptTemplate
	}
	if opts.TimeoutSeconds != nil {
		updates.TimeoutSeconds = opts.TimeoutSeconds
	}
	if opts.MaxRetries != nil {
		updates.MaxRetries = opts.MaxRetries
	}
	if opts.OnFailure != nil {
		updates.OnFailure = opts.OnFailure
	}
	if opts.MaxConsecutiveFailures != nil {
		updates.MaxConsecutiveFailures = opts.MaxConsecutiveFailures
	}
	if opts.Labels != nil {
		labelsJSON, err := mapToJSON(opts.Labels)
		if err != nil {
			return nil, fmt.Errorf("encoding labels: %w", err)
		}
		updates.Labels = labelsJSON
	}
	if opts.Annotations != nil {
		annotationsJSON, err := mapToJSON(opts.Annotations)
		if err != nil {
			return nil, fmt.Errorf("encoding annotations: %w", err)
		}
		updates.Annotations = annotationsJSON
	}

	// Recalculate next run time if cron or timezone changed
	if needRecalcNextRun && task.Status == ScheduledTaskStatusActive {
		cronExpr := task.CronExpression
		if opts.CronExpression != nil {
			cronExpr = *opts.CronExpression
		}
		tz := task.Timezone
		if opts.Timezone != nil {
			tz = *opts.Timezone
		}

		loc, _ := time.LoadLocation(tz)
		now := time.Now().In(loc)
		nextRunAt, err := s.CalculateNextRunAt(cronExpr, tz, now)
		if err != nil {
			return nil, fmt.Errorf("calculating next run time: %w", err)
		}
		updates.NextRunAt = nextRunAt
	}

	if err := s.store.UpdateScheduledTask(ctx, taskID, updates); err != nil {
		return nil, fmt.Errorf("updating scheduled task: %w", err)
	}

	// Fetch updated task
	return s.store.GetScheduledTask(ctx, taskID)
}

// Delete deletes a scheduled task.
func (s *ScheduledTaskService) Delete(ctx context.Context, taskID string) error {
	if err := s.store.DeleteScheduledTask(ctx, taskID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrScheduledTaskNotFound
		}
		return fmt.Errorf("deleting scheduled task: %w", err)
	}

	s.logger.Info("scheduled task deleted", zap.String("id", taskID))
	return nil
}

// Pause pauses a scheduled task.
func (s *ScheduledTaskService) Pause(ctx context.Context, taskID string) error {
	task, err := s.store.GetScheduledTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrScheduledTaskNotFound
		}
		return fmt.Errorf("getting scheduled task: %w", err)
	}

	if task.Status == ScheduledTaskStatusPaused {
		return ErrScheduledTaskAlreadyPaused
	}
	if task.Status == ScheduledTaskStatusDisabled {
		return ErrScheduledTaskDisabled
	}

	status := ScheduledTaskStatusPaused
	updates := store.ScheduledTaskUpdates{
		Status: &status,
	}

	if err := s.store.UpdateScheduledTask(ctx, taskID, updates); err != nil {
		return fmt.Errorf("updating scheduled task: %w", err)
	}

	s.logger.Info("scheduled task paused",
		zap.String("id", taskID),
		zap.String("name", task.Name),
	)

	return nil
}

// Resume resumes a paused scheduled task.
func (s *ScheduledTaskService) Resume(ctx context.Context, taskID string) error {
	task, err := s.store.GetScheduledTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrScheduledTaskNotFound
		}
		return fmt.Errorf("getting scheduled task: %w", err)
	}

	if task.Status == ScheduledTaskStatusActive {
		return ErrScheduledTaskAlreadyActive
	}
	if task.Status == ScheduledTaskStatusDisabled {
		return ErrScheduledTaskDisabled
	}

	// Calculate next run time
	loc, err := time.LoadLocation(task.Timezone)
	if err != nil {
		return fmt.Errorf("loading timezone: %w", err)
	}
	now := time.Now().In(loc)
	nextRunAt, err := s.CalculateNextRunAt(task.CronExpression, task.Timezone, now)
	if err != nil {
		return fmt.Errorf("calculating next run time: %w", err)
	}

	// Reset consecutive failures on resume
	status := ScheduledTaskStatusActive
	zero := 0
	updates := store.ScheduledTaskUpdates{
		Status:              &status,
		NextRunAt:           nextRunAt,
		ConsecutiveFailures: &zero,
	}

	if err := s.store.UpdateScheduledTask(ctx, taskID, updates); err != nil {
		return fmt.Errorf("updating scheduled task: %w", err)
	}

	s.logger.Info("scheduled task resumed",
		zap.String("id", taskID),
		zap.String("name", task.Name),
		zap.Timep("next_run_at", nextRunAt),
	)

	return nil
}

// Trigger manually triggers a scheduled task immediately.
func (s *ScheduledTaskService) Trigger(ctx context.Context, taskID string) (*store.Task, error) {
	scheduledTask, err := s.store.GetScheduledTask(ctx, taskID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrScheduledTaskNotFound
		}
		return nil, fmt.Errorf("getting scheduled task: %w", err)
	}

	if scheduledTask.Status == ScheduledTaskStatusDisabled {
		return nil, ErrScheduledTaskDisabled
	}

	return s.ExecuteScheduledTask(ctx, scheduledTask)
}

// GetDue retrieves scheduled tasks that are due to run.
func (s *ScheduledTaskService) GetDue(ctx context.Context, limit int) ([]*store.ScheduledTask, error) {
	return s.store.GetDueScheduledTasks(ctx, time.Now(), limit)
}

// ExecuteDue runs one due cron tick, claiming it first.
//
// GetDue is a plain SELECT, so every replica polling the same database sees the
// same due task. Without a claim they all execute it: an agent with shell
// access runs the user's prompt once per replica, against one workspace. The
// claim is a compare-and-set that advances next_run_at away from the due time -
// exactly one replica moves it, and the rest get ErrScheduledTickTaken.
//
// The schedule therefore advances BEFORE the task is created, which means a
// failure to create the task skips that tick rather than repeating it. That is
// the right way round: a skipped report is a nuisance, a prompt executed twice
// concurrently by an agent with shell access is a hazard.
func (s *ScheduledTaskService) ExecuteDue(ctx context.Context, scheduledTask *store.ScheduledTask) (*store.Task, error) {
	if scheduledTask.NextRunAt == nil {
		// Nothing to claim: this is not a scheduled tick.
		return s.ExecuteScheduledTask(ctx, scheduledTask)
	}

	now := time.Now()
	loc, _ := time.LoadLocation(scheduledTask.Timezone)
	nextRunAt, _ := s.CalculateNextRunAt(scheduledTask.CronExpression, scheduledTask.Timezone, now.In(loc))

	if err := s.store.UpdateScheduledTask(ctx, scheduledTask.ID, store.ScheduledTaskUpdates{
		NextRunAt:         nextRunAt,
		ExpectedNextRunAt: scheduledTask.NextRunAt,
	}); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrScheduledTickTaken
		}
		return nil, fmt.Errorf("claiming scheduled tick: %w", err)
	}

	// The schedule is already advanced, so the execution below must not
	// advance it again.
	claimed := *scheduledTask
	claimed.NextRunAt = nil
	return s.executeScheduledTask(ctx, &claimed, false)
}

// ExecuteScheduledTask executes a scheduled task by creating a regular task.
//
// This is the manual path (Trigger): it claims no tick, because there is none
// to claim - the user asked for a run now.
func (s *ScheduledTaskService) ExecuteScheduledTask(ctx context.Context, scheduledTask *store.ScheduledTask) (*store.Task, error) {
	return s.executeScheduledTask(ctx, scheduledTask, true)
}

// executeScheduledTask creates the task and records the run.
//
// advanceSchedule is false when the caller already advanced next_run_at to
// claim the tick; advancing again there would skip a run.
func (s *ScheduledTaskService) executeScheduledTask(
	ctx context.Context,
	scheduledTask *store.ScheduledTask,
	advanceSchedule bool,
) (*store.Task, error) {
	// Render prompt template
	prompt, err := s.renderPromptTemplate(scheduledTask)
	if err != nil {
		return nil, fmt.Errorf("rendering prompt template: %w", err)
	}

	// Create the task via TaskManager
	task, err := s.taskMgr.Create(ctx, CreateTaskOptions{
		SessionID:      scheduledTask.SessionID,
		Prompt:         prompt,
		MaxRetries:     scheduledTask.MaxRetries,
		TimeoutSeconds: scheduledTask.TimeoutSeconds,
		TenantID:       scheduledTask.TenantID,
		Labels:         jsonToMap(scheduledTask.Labels),
		Annotations:    jsonToMap(scheduledTask.Annotations),
	})
	if err != nil {
		return nil, fmt.Errorf("creating task: %w", err)
	}

	// Update scheduled task metadata
	now := time.Now()
	runCount := scheduledTask.RunCount + 1

	updates := store.ScheduledTaskUpdates{
		LastRunAt:  &now,
		LastTaskID: &task.ID,
		RunCount:   &runCount,
	}

	if advanceSchedule {
		loc, _ := time.LoadLocation(scheduledTask.Timezone)
		updates.NextRunAt, _ = s.CalculateNextRunAt(scheduledTask.CronExpression, scheduledTask.Timezone, now.In(loc))
	}

	if err := s.store.UpdateScheduledTask(ctx, scheduledTask.ID, updates); err != nil {
		s.logger.Warn("failed to update scheduled task after execution",
			zap.String("scheduled_task_id", scheduledTask.ID),
			zap.String("task_id", task.ID),
			zap.Error(err),
		)
	}

	s.logger.Info("scheduled task executed",
		zap.String("scheduled_task_id", scheduledTask.ID),
		zap.String("name", scheduledTask.Name),
		zap.String("task_id", task.ID),
		zap.Int("run_count", runCount),
		zap.Timep("next_run_at", updates.NextRunAt),
	)

	return task, nil
}

// MarkTaskCompleted marks a scheduled task's last run as completed (success or failure).
// This should be called by the task completion handler.
func (s *ScheduledTaskService) MarkTaskCompleted(ctx context.Context, scheduledTaskID string, success bool) error {
	scheduledTask, err := s.store.GetScheduledTask(ctx, scheduledTaskID)
	if err != nil {
		return fmt.Errorf("getting scheduled task: %w", err)
	}

	updates := store.ScheduledTaskUpdates{}

	if success {
		// Reset consecutive failures on success
		zero := 0
		updates.ConsecutiveFailures = &zero
	} else {
		// Increment failure counters
		failureCount := scheduledTask.FailureCount + 1
		consecutiveFailures := scheduledTask.ConsecutiveFailures + 1
		updates.FailureCount = &failureCount
		updates.ConsecutiveFailures = &consecutiveFailures

		// Handle error policy
		switch scheduledTask.OnFailure {
		case OnFailurePauseOnFailure:
			status := ScheduledTaskStatusPaused
			updates.Status = &status
			s.logger.Warn("scheduled task paused due to failure",
				zap.String("id", scheduledTaskID),
				zap.String("name", scheduledTask.Name),
			)

		case OnFailureDisableOnFailure:
			maxFailures := DefaultMaxConsecutiveFailures
			if scheduledTask.MaxConsecutiveFailures != nil {
				maxFailures = *scheduledTask.MaxConsecutiveFailures
			}
			if consecutiveFailures >= maxFailures {
				status := ScheduledTaskStatusDisabled
				updates.Status = &status
				s.logger.Error("scheduled task disabled due to consecutive failures",
					zap.String("id", scheduledTaskID),
					zap.String("name", scheduledTask.Name),
					zap.Int("consecutive_failures", consecutiveFailures),
					zap.Int("max_consecutive_failures", maxFailures),
				)
			}
		}
	}

	return s.store.UpdateScheduledTask(ctx, scheduledTaskID, updates)
}

// CalculateNextRunAt calculates the next run time for a cron expression.
func (s *ScheduledTaskService) CalculateNextRunAt(cronExpr, timezone string, after time.Time) (*time.Time, error) {
	schedule, err := s.cronParser.Parse(cronExpr)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCronExpression, err)
	}

	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidTimezone, err)
	}

	// Convert to the target timezone for calculation
	afterInTZ := after.In(loc)
	nextTime := schedule.Next(afterInTZ)

	// Convert back to UTC for storage
	nextTimeUTC := nextTime.UTC()
	return &nextTimeUTC, nil
}

// renderPromptTemplate renders the prompt template with current data.
func (s *ScheduledTaskService) renderPromptTemplate(scheduledTask *store.ScheduledTask) (string, error) {
	now := time.Now()

	data := PromptTemplateData{
		Date:         now.Format("2006-01-02"),
		DateTime:     now.Format(time.RFC3339),
		RunNumber:    scheduledTask.RunCount + 1,
		TaskName:     scheduledTask.Name,
		SessionID:    scheduledTask.SessionID,
		ScheduledFor: now.Format(time.RFC3339),
	}
	if scheduledTask.NextRunAt != nil {
		data.ScheduledFor = scheduledTask.NextRunAt.Format(time.RFC3339)
	}

	tmpl, err := template.New("prompt").Parse(scheduledTask.PromptTemplate)
	if err != nil {
		return "", fmt.Errorf("parsing prompt template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("executing prompt template: %w", err)
	}

	return buf.String(), nil
}

// mapToJSON converts a map to json.RawMessage, handling nil maps.
func mapToJSON(m map[string]string) (json.RawMessage, error) {
	if m == nil {
		return json.RawMessage("{}"), nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(data), nil
}

// jsonToMap converts json.RawMessage to map[string]string, handling empty JSON.
func jsonToMap(raw json.RawMessage) map[string]string {
	if len(raw) == 0 || string(raw) == "{}" || string(raw) == "null" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
