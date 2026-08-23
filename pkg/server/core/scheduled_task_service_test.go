package core

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockTaskMgrForScheduled implements TaskManagerInterface for testing.
type mockTaskMgrForScheduled struct {
	createFunc   func(ctx context.Context, opts CreateTaskOptions) (*store.Task, error)
	createdTasks []*store.Task
}

func (m *mockTaskMgrForScheduled) Create(ctx context.Context, opts CreateTaskOptions) (*store.Task, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, opts)
	}
	task := &store.Task{
		ID:             "task_test_123",
		SessionID:      opts.SessionID,
		Prompt:         opts.Prompt,
		Status:         TaskStatusPending,
		MaxRetries:     opts.MaxRetries,
		TimeoutSeconds: opts.TimeoutSeconds,
		TenantID:       opts.TenantID,
	}
	m.createdTasks = append(m.createdTasks, task)
	return task, nil
}

func (m *mockTaskMgrForScheduled) Get(_ context.Context, _ string) (*store.Task, error) {
	return nil, nil
}

func (m *mockTaskMgrForScheduled) List(_ context.Context, _ ListTasksOptions) (*store.ListResult[store.Task], error) {
	return nil, nil
}

func (m *mockTaskMgrForScheduled) CreateRun(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, nil
}

func (m *mockTaskMgrForScheduled) Execute(_ context.Context, _ string) error {
	return nil
}

func (m *mockTaskMgrForScheduled) ReExecute(_ context.Context, _ string) error {
	return nil
}

func (m *mockTaskMgrForScheduled) Cancel(_ context.Context, _ string) error {
	return nil
}

func (m *mockTaskMgrForScheduled) Retry(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, nil
}

func (m *mockTaskMgrForScheduled) ShouldRetry(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (m *mockTaskMgrForScheduled) OnTaskAccepted(_ context.Context, _ string) error {
	return nil
}

func (m *mockTaskMgrForScheduled) OnTaskStarted(_ context.Context, _ string) error {
	return nil
}

func (m *mockTaskMgrForScheduled) OnTaskProgress(_ context.Context, _ string, _ int) error {
	return nil
}

func (m *mockTaskMgrForScheduled) OnTaskCompleted(_ context.Context, _ *TaskCompletedResult) error {
	return nil
}

func (m *mockTaskMgrForScheduled) FailRun(_ context.Context, _ string, _ string) error {
	return nil
}

// scheduledTaskTestStore extends testSessionStore with scheduled task functionality.
type scheduledTaskTestStore struct {
	*testSessionStore
	scheduledTasks map[string]*store.ScheduledTask
}

func newScheduledTaskTestStore() *scheduledTaskTestStore {
	return &scheduledTaskTestStore{
		testSessionStore: newTestSessionStore(),
		scheduledTasks:   make(map[string]*store.ScheduledTask),
	}
}

func (s *scheduledTaskTestStore) CreateScheduledTask(_ context.Context, task *store.ScheduledTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scheduledTasks[task.ID] = task
	return nil
}

func (s *scheduledTaskTestStore) GetScheduledTask(_ context.Context, id string) (*store.ScheduledTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.scheduledTasks[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	copy := *task
	return &copy, nil
}

func (s *scheduledTaskTestStore) ListScheduledTasks(_ context.Context, opts store.ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.ScheduledTask, 0, len(s.scheduledTasks))
	for _, task := range s.scheduledTasks {
		if opts.SessionID != nil && task.SessionID != *opts.SessionID {
			continue
		}
		if len(opts.Status) > 0 && !slices.Contains(opts.Status, task.Status) {
			continue
		}
		copy := *task
		items = append(items, &copy)
	}
	return &store.ListResult[store.ScheduledTask]{Items: items}, nil
}

func (s *scheduledTaskTestStore) UpdateScheduledTask(_ context.Context, id string, updates store.ScheduledTaskUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.scheduledTasks[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Name != nil {
		task.Name = *updates.Name
	}
	if updates.Description != nil {
		task.Description = updates.Description
	}
	if updates.CronExpression != nil {
		task.CronExpression = *updates.CronExpression
	}
	if updates.Timezone != nil {
		task.Timezone = *updates.Timezone
	}
	if updates.PromptTemplate != nil {
		task.PromptTemplate = *updates.PromptTemplate
	}
	if updates.Status != nil {
		task.Status = *updates.Status
	}
	if updates.NextRunAt != nil {
		task.NextRunAt = updates.NextRunAt
	}
	if updates.LastRunAt != nil {
		task.LastRunAt = updates.LastRunAt
	}
	if updates.LastTaskID != nil {
		task.LastTaskID = updates.LastTaskID
	}
	if updates.RunCount != nil {
		task.RunCount = *updates.RunCount
	}
	if updates.FailureCount != nil {
		task.FailureCount = *updates.FailureCount
	}
	if updates.ConsecutiveFailures != nil {
		task.ConsecutiveFailures = *updates.ConsecutiveFailures
	}
	if updates.TimeoutSeconds != nil {
		task.TimeoutSeconds = *updates.TimeoutSeconds
	}
	if updates.MaxRetries != nil {
		task.MaxRetries = *updates.MaxRetries
	}
	if updates.OnFailure != nil {
		task.OnFailure = *updates.OnFailure
	}
	if updates.MaxConsecutiveFailures != nil {
		task.MaxConsecutiveFailures = updates.MaxConsecutiveFailures
	}
	task.UpdatedAt = time.Now()
	return nil
}

func (s *scheduledTaskTestStore) DeleteScheduledTask(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.scheduledTasks[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.scheduledTasks, id)
	return nil
}

func (s *scheduledTaskTestStore) GetDueScheduledTasks(_ context.Context, now time.Time, limit int) ([]*store.ScheduledTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.ScheduledTask, 0)
	for _, task := range s.scheduledTasks {
		if task.Status != ScheduledTaskStatusActive {
			continue
		}
		if task.NextRunAt == nil || task.NextRunAt.After(now) {
			continue
		}
		copy := *task
		items = append(items, &copy)
		if len(items) >= limit {
			break
		}
	}
	return items, nil
}

func setupScheduledTaskServiceTest() (*ScheduledTaskService, *scheduledTaskTestStore, *mockTaskMgrForScheduled) {
	s := newScheduledTaskTestStore()
	taskMgr := &mockTaskMgrForScheduled{}
	logger := zap.NewNop()
	service := NewScheduledTaskService(s, taskMgr, nil, logger)
	return service, s, taskMgr
}

// =============================================================================
// ScheduledTaskService Tests
// =============================================================================

func TestScheduledTaskService_Create(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	// Setup session
	s.SetSession(&store.Session{
		ID:     "sess_123",
		Status: SessionStatusActive,
	})

	opts := CreateScheduledTaskOptions{
		SessionID:      "sess_123",
		Name:           "daily-summary",
		CronExpression: "0 9 * * *",
		PromptTemplate: "Generate daily summary for {{.Date}}",
	}

	task, err := service.Create(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, task)

	assert.Equal(t, "sess_123", task.SessionID)
	assert.Equal(t, "daily-summary", task.Name)
	assert.Equal(t, "0 9 * * *", task.CronExpression)
	assert.Equal(t, "Generate daily summary for {{.Date}}", task.PromptTemplate)
	assert.Equal(t, ScheduledTaskStatusActive, task.Status)
	assert.Equal(t, "UTC", task.Timezone)
	assert.Equal(t, DefaultScheduledTaskTimeoutSeconds, task.TimeoutSeconds)
	assert.Equal(t, OnFailureContinue, task.OnFailure)
	assert.NotNil(t, task.NextRunAt)
}

func TestScheduledTaskService_Create_WithOptions(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.SetSession(&store.Session{ID: "sess_123", Status: SessionStatusActive})

	maxFailures := 5
	opts := CreateScheduledTaskOptions{
		SessionID:              "sess_123",
		Name:                   "weekly-report",
		Description:            "Weekly status report",
		CronExpression:         "0 9 * * 1",
		Timezone:               "America/New_York",
		PromptTemplate:         "Generate weekly report",
		TimeoutSeconds:         7200,
		MaxRetries:             3,
		OnFailure:              OnFailurePauseOnFailure,
		MaxConsecutiveFailures: &maxFailures,
		Labels:                 map[string]string{"env": "prod"},
	}

	task, err := service.Create(context.Background(), opts)
	require.NoError(t, err)

	assert.Equal(t, "America/New_York", task.Timezone)
	assert.Equal(t, 7200, task.TimeoutSeconds)
	assert.Equal(t, 3, task.MaxRetries)
	assert.Equal(t, OnFailurePauseOnFailure, task.OnFailure)
	require.NotNil(t, task.MaxConsecutiveFailures)
	assert.Equal(t, 5, *task.MaxConsecutiveFailures)
	assert.NotNil(t, task.Description)
	assert.Equal(t, "Weekly status report", *task.Description)
}

func TestScheduledTaskService_Create_SessionRequired(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	opts := CreateScheduledTaskOptions{
		Name:           "test",
		CronExpression: "0 9 * * *",
		PromptTemplate: "test",
	}

	_, err := service.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrSessionRequired)
}

func TestScheduledTaskService_Create_NameRequired(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()
	s.SetSession(&store.Session{ID: "sess_123", Status: SessionStatusActive})

	opts := CreateScheduledTaskOptions{
		SessionID:      "sess_123",
		CronExpression: "0 9 * * *",
		PromptTemplate: "test",
	}

	_, err := service.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrScheduledTaskNameRequired)
}

func TestScheduledTaskService_Create_CronRequired(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()
	s.SetSession(&store.Session{ID: "sess_123", Status: SessionStatusActive})

	opts := CreateScheduledTaskOptions{
		SessionID:      "sess_123",
		Name:           "test",
		PromptTemplate: "test",
	}

	_, err := service.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrScheduledTaskCronRequired)
}

func TestScheduledTaskService_Create_PromptRequired(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()
	s.SetSession(&store.Session{ID: "sess_123", Status: SessionStatusActive})

	opts := CreateScheduledTaskOptions{
		SessionID:      "sess_123",
		Name:           "test",
		CronExpression: "0 9 * * *",
	}

	_, err := service.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrScheduledTaskPromptRequired)
}

func TestScheduledTaskService_Create_SessionNotFound(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	opts := CreateScheduledTaskOptions{
		SessionID:      "sess_nonexistent",
		Name:           "test",
		CronExpression: "0 9 * * *",
		PromptTemplate: "test",
	}

	_, err := service.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestScheduledTaskService_Create_InvalidCron(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()
	s.SetSession(&store.Session{ID: "sess_123", Status: SessionStatusActive})

	opts := CreateScheduledTaskOptions{
		SessionID:      "sess_123",
		Name:           "test",
		CronExpression: "invalid cron",
		PromptTemplate: "test",
	}

	_, err := service.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrInvalidCronExpression)
}

func TestScheduledTaskService_Create_InvalidTimezone(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()
	s.SetSession(&store.Session{ID: "sess_123", Status: SessionStatusActive})

	opts := CreateScheduledTaskOptions{
		SessionID:      "sess_123",
		Name:           "test",
		CronExpression: "0 9 * * *",
		Timezone:       "Invalid/Timezone",
		PromptTemplate: "test",
	}

	_, err := service.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrInvalidTimezone)
}

func TestScheduledTaskService_Get(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:             "stsk_123",
		SessionID:      "sess_123",
		Name:           "test-task",
		CronExpression: "0 9 * * *",
		Status:         ScheduledTaskStatusActive,
	}

	task, err := service.Get(context.Background(), "stsk_123")
	require.NoError(t, err)
	assert.Equal(t, "stsk_123", task.ID)
	assert.Equal(t, "test-task", task.Name)
}

func TestScheduledTaskService_Get_NotFound(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	_, err := service.Get(context.Background(), "stsk_nonexistent")
	assert.ErrorIs(t, err, ErrScheduledTaskNotFound)
}

func TestScheduledTaskService_List(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_1"] = &store.ScheduledTask{ID: "stsk_1", SessionID: "sess_123", Status: ScheduledTaskStatusActive}
	s.scheduledTasks["stsk_2"] = &store.ScheduledTask{ID: "stsk_2", SessionID: "sess_123", Status: ScheduledTaskStatusPaused}
	s.scheduledTasks["stsk_3"] = &store.ScheduledTask{ID: "stsk_3", SessionID: "sess_456", Status: ScheduledTaskStatusActive}

	result, err := service.List(context.Background(), ListScheduledTasksOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 3)
}

func TestScheduledTaskService_List_FilterBySession(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_1"] = &store.ScheduledTask{ID: "stsk_1", SessionID: "sess_123", Status: ScheduledTaskStatusActive}
	s.scheduledTasks["stsk_2"] = &store.ScheduledTask{ID: "stsk_2", SessionID: "sess_123", Status: ScheduledTaskStatusPaused}
	s.scheduledTasks["stsk_3"] = &store.ScheduledTask{ID: "stsk_3", SessionID: "sess_456", Status: ScheduledTaskStatusActive}

	sessionID := "sess_123"
	result, err := service.List(context.Background(), ListScheduledTasksOptions{
		SessionID: &sessionID,
	})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
}

func TestScheduledTaskService_Update(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:             "stsk_123",
		SessionID:      "sess_123",
		Name:           "old-name",
		CronExpression: "0 9 * * *",
		Timezone:       "UTC",
		PromptTemplate: "old prompt",
		Status:         ScheduledTaskStatusActive,
	}

	newName := "new-name"
	newPrompt := "new prompt"
	opts := UpdateScheduledTaskOptions{
		Name:           &newName,
		PromptTemplate: &newPrompt,
	}

	task, err := service.Update(context.Background(), "stsk_123", opts)
	require.NoError(t, err)
	assert.Equal(t, "new-name", task.Name)
	assert.Equal(t, "new prompt", task.PromptTemplate)
}

func TestScheduledTaskService_Update_CronRecalculatesNextRun(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	oldNextRun := time.Now().Add(24 * time.Hour)
	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:             "stsk_123",
		SessionID:      "sess_123",
		Name:           "test",
		CronExpression: "0 9 * * *",
		Timezone:       "UTC",
		PromptTemplate: "test",
		Status:         ScheduledTaskStatusActive,
		NextRunAt:      &oldNextRun,
	}

	newCron := "0 10 * * *"
	opts := UpdateScheduledTaskOptions{
		CronExpression: &newCron,
	}

	task, err := service.Update(context.Background(), "stsk_123", opts)
	require.NoError(t, err)
	assert.Equal(t, "0 10 * * *", task.CronExpression)
	// NextRunAt should be recalculated
	assert.NotEqual(t, oldNextRun, *task.NextRunAt)
}

func TestScheduledTaskService_Update_NotFound(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	newName := "new-name"
	_, err := service.Update(context.Background(), "stsk_nonexistent", UpdateScheduledTaskOptions{
		Name: &newName,
	})
	assert.ErrorIs(t, err, ErrScheduledTaskNotFound)
}

func TestScheduledTaskService_Delete(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:        "stsk_123",
		SessionID: "sess_123",
		Name:      "test",
	}

	err := service.Delete(context.Background(), "stsk_123")
	require.NoError(t, err)

	_, ok := s.scheduledTasks["stsk_123"]
	assert.False(t, ok)
}

func TestScheduledTaskService_Delete_NotFound(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	err := service.Delete(context.Background(), "stsk_nonexistent")
	assert.ErrorIs(t, err, ErrScheduledTaskNotFound)
}

func TestScheduledTaskService_Pause(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:     "stsk_123",
		Name:   "test",
		Status: ScheduledTaskStatusActive,
	}

	err := service.Pause(context.Background(), "stsk_123")
	require.NoError(t, err)

	task := s.scheduledTasks["stsk_123"]
	assert.Equal(t, ScheduledTaskStatusPaused, task.Status)
}

func TestScheduledTaskService_Pause_AlreadyPaused(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:     "stsk_123",
		Name:   "test",
		Status: ScheduledTaskStatusPaused,
	}

	err := service.Pause(context.Background(), "stsk_123")
	assert.ErrorIs(t, err, ErrScheduledTaskAlreadyPaused)
}

func TestScheduledTaskService_Pause_Disabled(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:     "stsk_123",
		Name:   "test",
		Status: ScheduledTaskStatusDisabled,
	}

	err := service.Pause(context.Background(), "stsk_123")
	assert.ErrorIs(t, err, ErrScheduledTaskDisabled)
}

func TestScheduledTaskService_Pause_NotFound(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	err := service.Pause(context.Background(), "stsk_nonexistent")
	assert.ErrorIs(t, err, ErrScheduledTaskNotFound)
}

func TestScheduledTaskService_Resume(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:                  "stsk_123",
		Name:                "test",
		Status:              ScheduledTaskStatusPaused,
		CronExpression:      "0 9 * * *",
		Timezone:            "UTC",
		ConsecutiveFailures: 3,
	}

	err := service.Resume(context.Background(), "stsk_123")
	require.NoError(t, err)

	task := s.scheduledTasks["stsk_123"]
	assert.Equal(t, ScheduledTaskStatusActive, task.Status)
	assert.NotNil(t, task.NextRunAt)
	assert.Equal(t, 0, task.ConsecutiveFailures)
}

func TestScheduledTaskService_Resume_AlreadyActive(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:     "stsk_123",
		Name:   "test",
		Status: ScheduledTaskStatusActive,
	}

	err := service.Resume(context.Background(), "stsk_123")
	assert.ErrorIs(t, err, ErrScheduledTaskAlreadyActive)
}

func TestScheduledTaskService_Resume_Disabled(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:     "stsk_123",
		Name:   "test",
		Status: ScheduledTaskStatusDisabled,
	}

	err := service.Resume(context.Background(), "stsk_123")
	assert.ErrorIs(t, err, ErrScheduledTaskDisabled)
}

func TestScheduledTaskService_Resume_NotFound(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	err := service.Resume(context.Background(), "stsk_nonexistent")
	assert.ErrorIs(t, err, ErrScheduledTaskNotFound)
}

func TestScheduledTaskService_Trigger(t *testing.T) {
	service, s, taskMgr := setupScheduledTaskServiceTest()

	s.SetSession(&store.Session{ID: "sess_123", Status: SessionStatusActive})
	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:             "stsk_123",
		SessionID:      "sess_123",
		Name:           "test",
		Status:         ScheduledTaskStatusActive,
		CronExpression: "0 9 * * *",
		Timezone:       "UTC",
		PromptTemplate: "Test prompt",
		TimeoutSeconds: 3600,
		RunCount:       5,
	}

	task, err := service.Trigger(context.Background(), "stsk_123")
	require.NoError(t, err)
	require.NotNil(t, task)

	assert.Equal(t, "sess_123", task.SessionID)
	assert.Len(t, taskMgr.createdTasks, 1)
}

func TestScheduledTaskService_Trigger_Disabled(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:     "stsk_123",
		Name:   "test",
		Status: ScheduledTaskStatusDisabled,
	}

	_, err := service.Trigger(context.Background(), "stsk_123")
	assert.ErrorIs(t, err, ErrScheduledTaskDisabled)
}

func TestScheduledTaskService_Trigger_NotFound(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	_, err := service.Trigger(context.Background(), "stsk_nonexistent")
	assert.ErrorIs(t, err, ErrScheduledTaskNotFound)
}

func TestScheduledTaskService_GetDue(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	pastTime := time.Now().Add(-1 * time.Hour)
	futureTime := time.Now().Add(1 * time.Hour)

	s.scheduledTasks["stsk_1"] = &store.ScheduledTask{ID: "stsk_1", Status: ScheduledTaskStatusActive, NextRunAt: &pastTime}
	s.scheduledTasks["stsk_2"] = &store.ScheduledTask{ID: "stsk_2", Status: ScheduledTaskStatusActive, NextRunAt: &futureTime}
	s.scheduledTasks["stsk_3"] = &store.ScheduledTask{ID: "stsk_3", Status: ScheduledTaskStatusPaused, NextRunAt: &pastTime}

	tasks, err := service.GetDue(context.Background(), 10)
	require.NoError(t, err)
	assert.Len(t, tasks, 1)
	assert.Equal(t, "stsk_1", tasks[0].ID)
}

func TestScheduledTaskService_ExecuteScheduledTask(t *testing.T) {
	service, s, taskMgr := setupScheduledTaskServiceTest()

	s.SetSession(&store.Session{ID: "sess_123", Status: SessionStatusActive})
	scheduledTask := &store.ScheduledTask{
		ID:             "stsk_123",
		SessionID:      "sess_123",
		Name:           "daily-summary",
		Status:         ScheduledTaskStatusActive,
		CronExpression: "0 9 * * *",
		Timezone:       "UTC",
		PromptTemplate: "Generate summary for {{.Date}}",
		TimeoutSeconds: 3600,
		MaxRetries:     2,
		RunCount:       0,
	}
	s.scheduledTasks["stsk_123"] = scheduledTask

	task, err := service.ExecuteScheduledTask(context.Background(), scheduledTask)
	require.NoError(t, err)
	require.NotNil(t, task)

	// Check task was created with rendered prompt
	require.Len(t, taskMgr.createdTasks, 1)
	createdTask := taskMgr.createdTasks[0]
	assert.Contains(t, createdTask.Prompt, "Generate summary for")
	assert.Equal(t, 3600, createdTask.TimeoutSeconds)
	assert.Equal(t, 2, createdTask.MaxRetries)

	// Check scheduled task was updated
	updatedScheduledTask := s.scheduledTasks["stsk_123"]
	assert.Equal(t, 1, updatedScheduledTask.RunCount)
	assert.NotNil(t, updatedScheduledTask.LastRunAt)
	assert.NotNil(t, updatedScheduledTask.LastTaskID)
	assert.NotNil(t, updatedScheduledTask.NextRunAt)
}

func TestScheduledTaskService_MarkTaskCompleted_Success(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:                  "stsk_123",
		Name:                "test",
		Status:              ScheduledTaskStatusActive,
		ConsecutiveFailures: 2,
	}

	err := service.MarkTaskCompleted(context.Background(), "stsk_123", true)
	require.NoError(t, err)

	task := s.scheduledTasks["stsk_123"]
	assert.Equal(t, 0, task.ConsecutiveFailures)
}

func TestScheduledTaskService_MarkTaskCompleted_Failure_Continue(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:                  "stsk_123",
		Name:                "test",
		Status:              ScheduledTaskStatusActive,
		OnFailure:           OnFailureContinue,
		FailureCount:        1,
		ConsecutiveFailures: 1,
	}

	err := service.MarkTaskCompleted(context.Background(), "stsk_123", false)
	require.NoError(t, err)

	task := s.scheduledTasks["stsk_123"]
	assert.Equal(t, ScheduledTaskStatusActive, task.Status)
	assert.Equal(t, 2, task.FailureCount)
	assert.Equal(t, 2, task.ConsecutiveFailures)
}

func TestScheduledTaskService_MarkTaskCompleted_Failure_PauseOnFailure(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:                  "stsk_123",
		Name:                "test",
		Status:              ScheduledTaskStatusActive,
		OnFailure:           OnFailurePauseOnFailure,
		FailureCount:        0,
		ConsecutiveFailures: 0,
	}

	err := service.MarkTaskCompleted(context.Background(), "stsk_123", false)
	require.NoError(t, err)

	task := s.scheduledTasks["stsk_123"]
	assert.Equal(t, ScheduledTaskStatusPaused, task.Status)
}

func TestScheduledTaskService_MarkTaskCompleted_Failure_DisableOnFailure(t *testing.T) {
	service, s, _ := setupScheduledTaskServiceTest()

	maxFailures := 3
	s.scheduledTasks["stsk_123"] = &store.ScheduledTask{
		ID:                     "stsk_123",
		Name:                   "test",
		Status:                 ScheduledTaskStatusActive,
		OnFailure:              OnFailureDisableOnFailure,
		MaxConsecutiveFailures: &maxFailures,
		FailureCount:           2,
		ConsecutiveFailures:    2, // This will become 3
	}

	err := service.MarkTaskCompleted(context.Background(), "stsk_123", false)
	require.NoError(t, err)

	task := s.scheduledTasks["stsk_123"]
	assert.Equal(t, ScheduledTaskStatusDisabled, task.Status)
	assert.Equal(t, 3, task.ConsecutiveFailures)
}

func TestScheduledTaskService_CalculateNextRunAt(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	// Test at 8:00 UTC, next should be 9:00 UTC
	after := time.Date(2025, 1, 15, 8, 0, 0, 0, time.UTC)
	nextRunAt, err := service.CalculateNextRunAt("0 9 * * *", "UTC", after)
	require.NoError(t, err)
	require.NotNil(t, nextRunAt)

	expected := time.Date(2025, 1, 15, 9, 0, 0, 0, time.UTC)
	assert.Equal(t, expected, *nextRunAt)
}

func TestScheduledTaskService_CalculateNextRunAt_InvalidCron(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	_, err := service.CalculateNextRunAt("invalid", "UTC", time.Now())
	assert.ErrorIs(t, err, ErrInvalidCronExpression)
}

func TestScheduledTaskService_CalculateNextRunAt_InvalidTimezone(t *testing.T) {
	service, _, _ := setupScheduledTaskServiceTest()

	_, err := service.CalculateNextRunAt("0 9 * * *", "Invalid/Timezone", time.Now())
	assert.ErrorIs(t, err, ErrInvalidTimezone)
}

func TestMapToJSON(t *testing.T) {
	// Test nil map
	result, err := mapToJSON(nil)
	require.NoError(t, err)
	assert.Equal(t, "{}", string(result))

	// Test non-nil map
	m := map[string]string{"key": "value"}
	result, err = mapToJSON(m)
	require.NoError(t, err)
	assert.Equal(t, `{"key":"value"}`, string(result))
}

func TestJSONToMap(t *testing.T) {
	// Test empty JSON
	result := jsonToMap([]byte("{}"))
	assert.Nil(t, result)

	// Test null JSON
	result = jsonToMap([]byte("null"))
	assert.Nil(t, result)

	// Test empty slice
	result = jsonToMap([]byte{})
	assert.Nil(t, result)

	// Test valid JSON
	result = jsonToMap([]byte(`{"key":"value"}`))
	require.NotNil(t, result)
	assert.Equal(t, "value", result["key"])
}

// DispatchNext satisfies TaskManagerInterface. These fakes dispatch nothing.
func (m *mockTaskMgrForScheduled) DispatchNext(_ context.Context, _ string) error { return nil }
