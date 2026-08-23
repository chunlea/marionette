package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// testTaskStore extends testSessionStore with task-specific functionality.
type testTaskStore struct {
	*testSessionStore
	mu       sync.RWMutex
	tasks    map[string]*store.Task
	taskRuns map[string]*store.TaskRun
}

func newTestTaskStore() *testTaskStore {
	return &testTaskStore{
		testSessionStore: newTestSessionStore(),
		tasks:            make(map[string]*store.Task),
		taskRuns:         make(map[string]*store.TaskRun),
	}
}

// copyTask returns a deep copy of a task to prevent race conditions.
func copyTask(t *store.Task) *store.Task {
	if t == nil {
		return nil
	}
	cp := *t
	if t.TenantID != nil {
		tenantID := *t.TenantID
		cp.TenantID = &tenantID
	}
	if t.Labels != nil {
		cp.Labels = make([]byte, len(t.Labels))
		copy(cp.Labels, t.Labels)
	}
	if t.Annotations != nil {
		cp.Annotations = make([]byte, len(t.Annotations))
		copy(cp.Annotations, t.Annotations)
	}
	return &cp
}

// copyTaskRun returns a deep copy of a task run to prevent race conditions.
func copyTaskRun(r *store.TaskRun) *store.TaskRun {
	if r == nil {
		return nil
	}
	cp := *r
	if r.RunnerID != nil {
		runnerID := *r.RunnerID
		cp.RunnerID = &runnerID
	}
	if r.Error != nil {
		errStr := *r.Error
		cp.Error = &errStr
	}
	if r.ExitCode != nil {
		code := *r.ExitCode
		cp.ExitCode = &code
	}
	if r.TokensInput != nil {
		v := *r.TokensInput
		cp.TokensInput = &v
	}
	if r.TokensOutput != nil {
		v := *r.TokensOutput
		cp.TokensOutput = &v
	}
	if r.TenantID != nil {
		tenantID := *r.TenantID
		cp.TenantID = &tenantID
	}
	if r.AssignedAt != nil {
		t := *r.AssignedAt
		cp.AssignedAt = &t
	}
	if r.StartedAt != nil {
		t := *r.StartedAt
		cp.StartedAt = &t
	}
	if r.EndedAt != nil {
		t := *r.EndedAt
		cp.EndedAt = &t
	}
	return &cp
}

func (s *testTaskStore) CreateTask(_ context.Context, task *store.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[task.ID] = copyTask(task)
	return nil
}

func (s *testTaskStore) GetTask(_ context.Context, id string) (*store.Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.tasks[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return copyTask(task), nil
}

func (s *testTaskStore) ListTasks(_ context.Context, opts store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.Task, 0, len(s.tasks))
	for _, task := range s.tasks {
		// Filter by session_id
		if opts.SessionID != nil && task.SessionID != *opts.SessionID {
			continue
		}
		// Filter by status
		if len(opts.Status) > 0 {
			matched := false
			for _, st := range opts.Status {
				if task.Status == st {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		items = append(items, copyTask(task))
	}
	return &store.ListResult[store.Task]{Items: items}, nil
}

func (s *testTaskStore) UpdateTask(_ context.Context, id string, updates store.TaskUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.tasks[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		task.Status = *updates.Status
	}
	if updates.RetryCount != nil {
		task.RetryCount = *updates.RetryCount
	}
	task.UpdatedAt = time.Now()
	return nil
}

func (s *testTaskStore) DeleteTask(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tasks, id)
	return nil
}

func (s *testTaskStore) CreateTaskRun(_ context.Context, run *store.TaskRun) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskRuns[run.ID] = copyTaskRun(run)
	return nil
}

func (s *testTaskStore) GetTaskRun(_ context.Context, id string) (*store.TaskRun, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.taskRuns[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return copyTaskRun(run), nil
}

func (s *testTaskStore) ListTaskRuns(_ context.Context, opts store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	items := make([]*store.TaskRun, 0, len(s.taskRuns))
	for _, run := range s.taskRuns {
		// Filter by task_id
		if opts.TaskID != nil && run.TaskID != *opts.TaskID {
			continue
		}
		// Filter by runner_id
		if opts.RunnerID != nil && (run.RunnerID == nil || *run.RunnerID != *opts.RunnerID) {
			continue
		}
		// Filter by status
		if len(opts.Status) > 0 {
			matched := false
			for _, st := range opts.Status {
				if run.Status == st {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		items = append(items, copyTaskRun(run))
	}
	return &store.ListResult[store.TaskRun]{Items: items}, nil
}

func (s *testTaskStore) UpdateTaskRun(_ context.Context, id string, updates store.TaskRunUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	run, ok := s.taskRuns[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		run.Status = *updates.Status
	}
	if updates.Error != nil {
		run.Error = updates.Error
	}
	if updates.AssignedAt != nil {
		run.AssignedAt = updates.AssignedAt
	}
	if updates.StartedAt != nil {
		run.StartedAt = updates.StartedAt
	}
	if updates.EndedAt != nil {
		run.EndedAt = updates.EndedAt
	}
	if updates.TokensInput != nil {
		run.TokensInput = updates.TokensInput
	}
	if updates.TokensOutput != nil {
		run.TokensOutput = updates.TokensOutput
	}
	if updates.ExitCode != nil {
		run.ExitCode = updates.ExitCode
	}
	run.UpdatedAt = time.Now()
	return nil
}

// getTask is a helper for tests to safely read task state.
func (s *testTaskStore) getTask(id string) *store.Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyTask(s.tasks[id])
}

// getTaskRun is a helper for tests to safely read task run state.
func (s *testTaskStore) getTaskRun(id string) *store.TaskRun {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return copyTaskRun(s.taskRuns[id])
}

// setTask is a helper for tests to safely set task state.
func (s *testTaskStore) setTask(id string, task *store.Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks[id] = task
}

// setTaskRun is a helper for tests to safely set task run state.
func (s *testTaskStore) setTaskRun(id string, run *store.TaskRun) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.taskRuns[id] = run
}

// Webhook methods (stub)
func (s *testTaskStore) CreateWebhook(_ context.Context, _ *store.Webhook) error { return nil }
func (s *testTaskStore) GetWebhook(_ context.Context, _ string) (*store.Webhook, error) {
	return nil, store.ErrNotFound
}
func (s *testTaskStore) GetWebhookByName(_ context.Context, _ string, _ *string) (*store.Webhook, error) {
	return nil, store.ErrNotFound
}
func (s *testTaskStore) ListWebhooks(_ context.Context, _ store.ListWebhooksOptions) (*store.ListResult[store.Webhook], error) {
	return &store.ListResult[store.Webhook]{}, nil
}
func (s *testTaskStore) UpdateWebhook(_ context.Context, _ string, _ store.WebhookUpdates) error {
	return nil
}
func (s *testTaskStore) DeleteWebhook(_ context.Context, _ string) error { return nil }
func (s *testTaskStore) GetActiveWebhooksForEvent(_ context.Context, _ string, _ *string) ([]*store.Webhook, error) {
	return nil, nil
}

// WebhookEvent methods (stub)
func (s *testTaskStore) CreateWebhookEvent(_ context.Context, _ *store.WebhookEvent) error {
	return nil
}
func (s *testTaskStore) GetWebhookEvent(_ context.Context, _ string) (*store.WebhookEvent, error) {
	return nil, store.ErrNotFound
}
func (s *testTaskStore) ListWebhookEvents(_ context.Context, _ store.ListWebhookEventsOptions) (*store.ListResult[store.WebhookEvent], error) {
	return &store.ListResult[store.WebhookEvent]{}, nil
}
func (s *testTaskStore) UpdateWebhookEvent(_ context.Context, _ string, _ store.WebhookEventUpdates) error {
	return nil
}
func (s *testTaskStore) GetPendingWebhookEvents(_ context.Context, _ int) ([]*store.WebhookEvent, error) {
	return nil, nil
}
func (s *testTaskStore) CancelWebhookEventsByWebhook(_ context.Context, _ string) error { return nil }

// mockCommandSender implements CommandSender for testing.
type mockCommandSender struct {
	sentCommands []*pb.ServerCommand
	sendErr      error
}

func (m *mockCommandSender) SendCommand(_ string, cmd *pb.ServerCommand) error {
	if m.sendErr != nil {
		return m.sendErr
	}
	m.sentCommands = append(m.sentCommands, cmd)
	return nil
}

// mockSessionMgrForTask implements SessionManagerInterface for testing.
type mockSessionMgrForTask struct {
	// ensureRunnerErr, when set, makes EnsureRunner report that no runner
	// could be allocated. The zero value succeeds.
	ensureRunnerErr error
	ensureCalls     int
}

func (m *mockSessionMgrForTask) Create(_ context.Context, _ CreateSessionOptions) (*store.Session, error) {
	return nil, nil
}
func (m *mockSessionMgrForTask) Get(_ context.Context, _ string) (*store.Session, error) {
	return nil, nil
}
func (m *mockSessionMgrForTask) List(_ context.Context, _ ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return nil, nil
}
func (m *mockSessionMgrForTask) Activate(_ context.Context, _, _ string) error     { return nil }
func (m *mockSessionMgrForTask) Suspend(_ context.Context, _, _ string) error      { return nil }
func (m *mockSessionMgrForTask) Resume(_ context.Context, _ string) error          { return nil }
func (m *mockSessionMgrForTask) Terminate(_ context.Context, _ string) error       { return nil }
func (m *mockSessionMgrForTask) AttachRunner(_ context.Context, _, _ string) error { return nil }
func (m *mockSessionMgrForTask) DetachRunner(_ context.Context, _ string) error    { return nil }
func (m *mockSessionMgrForTask) UpdateContextSnapshot(_ context.Context, _ string, _ *ContextSnapshot) error {
	return nil
}

// Helper to create test setup
func setupTaskManagerTest() (*TaskManager, *testTaskStore, *mockCommandSender) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	sessionMgr := &mockSessionMgrForTask{}
	logger := zap.NewNop()
	manager := NewTaskManager(s, cmdSender, sessionMgr, nil, logger)
	return manager, s, cmdSender
}

// =============================================================================
// TaskManager Tests
// =============================================================================

func TestTaskManager_Create(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	// Setup session
	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusActive,
	}

	opts := CreateTaskOptions{
		SessionID: "sess_123",
		Prompt:    "Build a REST API",
	}

	task, err := manager.Create(context.Background(), opts)
	require.NoError(t, err)
	require.NotNil(t, task)

	assert.Equal(t, "sess_123", task.SessionID)
	assert.Equal(t, "Build a REST API", task.Prompt)
	assert.Equal(t, TaskStatusPending, task.Status)
	assert.Equal(t, DefaultTaskTimeoutSeconds, task.TimeoutSeconds)
	assert.Equal(t, DefaultMaxRetries, task.MaxRetries)
	assert.Equal(t, 0, task.RetryCount)
}

func TestTaskManager_Create_WithOptions(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.sessions["sess_123"] = &store.Session{ID: "sess_123", Status: SessionStatusActive}

	opts := CreateTaskOptions{
		SessionID:      "sess_123",
		Prompt:         "Build a REST API",
		MaxRetries:     3,
		TimeoutSeconds: 7200,
		Labels:         map[string]string{"env": "test"},
	}

	task, err := manager.Create(context.Background(), opts)
	require.NoError(t, err)

	assert.Equal(t, 3, task.MaxRetries)
	assert.Equal(t, 7200, task.TimeoutSeconds)
}

func TestTaskManager_Create_SessionRequired(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	opts := CreateTaskOptions{
		Prompt: "Build a REST API",
		// Missing SessionID
	}

	_, err := manager.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrSessionRequired)
}

func TestTaskManager_Create_PromptRequired(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.sessions["sess_123"] = &store.Session{ID: "sess_123", Status: SessionStatusActive}

	opts := CreateTaskOptions{
		SessionID: "sess_123",
		// Missing Prompt
	}

	_, err := manager.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrPromptRequired)
}

func TestTaskManager_Create_SessionNotFound(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	opts := CreateTaskOptions{
		SessionID: "sess_nonexistent",
		Prompt:    "Build a REST API",
	}

	_, err := manager.Create(context.Background(), opts)
	assert.ErrorIs(t, err, ErrSessionNotFound)
}

func TestTaskManager_Get(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:        "task_123",
		SessionID: "sess_123",
		Prompt:    "Test prompt",
		Status:    TaskStatusPending,
	}

	task, err := manager.Get(context.Background(), "task_123")
	require.NoError(t, err)
	assert.Equal(t, "task_123", task.ID)
	assert.Equal(t, "Test prompt", task.Prompt)
}

func TestTaskManager_Get_NotFound(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	_, err := manager.Get(context.Background(), "task_nonexistent")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestTaskManager_List(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_1"] = &store.Task{ID: "task_1", SessionID: "sess_123", Status: TaskStatusPending}
	s.tasks["task_2"] = &store.Task{ID: "task_2", SessionID: "sess_123", Status: TaskStatusCompleted}
	s.tasks["task_3"] = &store.Task{ID: "task_3", SessionID: "sess_456", Status: TaskStatusPending}

	result, err := manager.List(context.Background(), ListTasksOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Items, 3)
}

func TestTaskManager_List_FilterBySession(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_1"] = &store.Task{ID: "task_1", SessionID: "sess_123", Status: TaskStatusPending}
	s.tasks["task_2"] = &store.Task{ID: "task_2", SessionID: "sess_123", Status: TaskStatusCompleted}
	s.tasks["task_3"] = &store.Task{ID: "task_3", SessionID: "sess_456", Status: TaskStatusPending}

	sessionID := "sess_123"
	result, err := manager.List(context.Background(), ListTasksOptions{
		SessionID: &sessionID,
	})
	require.NoError(t, err)
	assert.Len(t, result.Items, 2)
}

func TestTaskManager_CreateRun(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	s.tasks["task_123"] = &store.Task{
		ID:         "task_123",
		SessionID:  "sess_123",
		RetryCount: 0,
	}

	run, err := manager.CreateRun(context.Background(), "task_123")
	require.NoError(t, err)
	require.NotNil(t, run)

	assert.Equal(t, "task_123", run.TaskID)
	assert.Equal(t, 1, run.Attempt)
	assert.Equal(t, TaskRunStatusPending, run.Status)
	require.NotNil(t, run.RunnerID)
	assert.Equal(t, "run_123", *run.RunnerID)
}

func TestTaskManager_CreateRun_TaskNotFound(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	_, err := manager.CreateRun(context.Background(), "task_nonexistent")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestTaskManager_Execute(t *testing.T) {
	manager, s, cmdSender := setupTaskManagerTest()

	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	s.tasks["task_123"] = &store.Task{
		ID:             "task_123",
		SessionID:      "sess_123",
		Prompt:         "Build a REST API",
		Status:         TaskStatusPending,
		TimeoutSeconds: 3600,
	}

	err := manager.Execute(context.Background(), "task_123")
	require.NoError(t, err)

	// Check task status updated
	task := s.tasks["task_123"]
	assert.Equal(t, TaskStatusRunning, task.Status)

	// Check command was sent
	assert.Len(t, cmdSender.sentCommands, 1)
	cmd := cmdSender.sentCommands[0]
	executeTask := cmd.GetExecuteTask()
	require.NotNil(t, executeTask)
	assert.Equal(t, "task_123", executeTask.GetTaskId())
	assert.Equal(t, "Build a REST API", executeTask.GetPrompt())
}

func TestTaskManager_Execute_NoRunnerAttached(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
		// No RunnerID
	}
	s.tasks["task_123"] = &store.Task{
		ID:        "task_123",
		SessionID: "sess_123",
		Status:    TaskStatusPending,
	}

	err := manager.Execute(context.Background(), "task_123")
	assert.ErrorIs(t, err, ErrNoRunnerAttached)
}

func TestTaskManager_Execute_TaskNotFound(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	err := manager.Execute(context.Background(), "task_nonexistent")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestTaskManager_ReExecute(t *testing.T) {
	manager, s, cmdSender := setupTaskManagerTest()

	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	s.tasks["task_123"] = &store.Task{
		ID:             "task_123",
		SessionID:      "sess_123",
		Prompt:         "Build a REST API",
		Status:         TaskStatusRunning, // Already running
		TimeoutSeconds: 3600,
	}
	s.taskRuns["trun_456"] = &store.TaskRun{
		ID:      "trun_456",
		TaskID:  "task_123",
		Attempt: 1,
		Status:  TaskRunStatusRunning,
	}

	err := manager.ReExecute(context.Background(), "task_123")
	require.NoError(t, err)

	// Check command was sent with existing run ID
	assert.Len(t, cmdSender.sentCommands, 1)
	cmd := cmdSender.sentCommands[0]
	executeTask := cmd.GetExecuteTask()
	require.NotNil(t, executeTask)
	assert.Equal(t, "task_123", executeTask.GetTaskId())
	assert.Equal(t, "trun_456", executeTask.GetRunId())
	assert.Equal(t, int32(1), executeTask.GetAttempt())
	assert.Equal(t, "Build a REST API", executeTask.GetPrompt())
}

func TestTaskManager_ReExecute_TaskNotRunning(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	s.tasks["task_123"] = &store.Task{
		ID:        "task_123",
		SessionID: "sess_123",
		Status:    TaskStatusPending, // Not running
	}

	err := manager.ReExecute(context.Background(), "task_123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "task is not running")
}

func TestTaskManager_ReExecute_NoRunningTaskRun(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	s.tasks["task_123"] = &store.Task{
		ID:        "task_123",
		SessionID: "sess_123",
		Status:    TaskStatusRunning,
	}
	// No running task_run exists

	err := manager.ReExecute(context.Background(), "task_123")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no running task_run found")
}

func TestTaskManager_ReExecute_TaskNotFound(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	err := manager.ReExecute(context.Background(), "task_nonexistent")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestTaskManager_OnTaskAccepted(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.taskRuns["trun_123"] = &store.TaskRun{
		ID:     "trun_123",
		TaskID: "task_123",
		Status: TaskRunStatusPending,
	}

	err := manager.OnTaskAccepted(context.Background(), "trun_123")
	require.NoError(t, err)

	run := s.taskRuns["trun_123"]
	assert.Equal(t, TaskRunStatusAssigned, run.Status)
	assert.NotNil(t, run.AssignedAt)
}

func TestTaskManager_OnTaskAccepted_InvalidTransition(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.taskRuns["trun_123"] = &store.TaskRun{
		ID:     "trun_123",
		TaskID: "task_123",
		Status: TaskRunStatusRunning, // Already running
	}

	err := manager.OnTaskAccepted(context.Background(), "trun_123")
	assert.ErrorIs(t, err, ErrInvalidTaskRunTransition)
}

func TestTaskManager_OnTaskAccepted_NotFound(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	err := manager.OnTaskAccepted(context.Background(), "trun_nonexistent")
	assert.ErrorIs(t, err, ErrTaskRunNotFound)
}

func TestTaskManager_OnTaskStarted(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.taskRuns["trun_123"] = &store.TaskRun{
		ID:     "trun_123",
		TaskID: "task_123",
		Status: TaskRunStatusAssigned,
	}

	err := manager.OnTaskStarted(context.Background(), "trun_123")
	require.NoError(t, err)

	run := s.taskRuns["trun_123"]
	assert.Equal(t, TaskRunStatusRunning, run.Status)
	assert.NotNil(t, run.StartedAt)
}

func TestTaskManager_OnTaskStarted_InvalidTransition(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.taskRuns["trun_123"] = &store.TaskRun{
		ID:     "trun_123",
		TaskID: "task_123",
		Status: TaskRunStatusPending, // Not assigned yet
	}

	err := manager.OnTaskStarted(context.Background(), "trun_123")
	assert.ErrorIs(t, err, ErrInvalidTaskRunTransition)
}

func TestTaskManager_OnTaskStarted_NotFound(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	err := manager.OnTaskStarted(context.Background(), "trun_nonexistent")
	assert.ErrorIs(t, err, ErrTaskRunNotFound)
}

func TestTaskManager_OnTaskProgress(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	// Progress is a no-op currently, just ensure it doesn't error
	err := manager.OnTaskProgress(context.Background(), "trun_123", 50)
	require.NoError(t, err)
}

func TestTaskManager_OnTaskCompleted_Success(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:         "task_123",
		SessionID:  "sess_123",
		Status:     TaskStatusRunning,
		MaxRetries: 0,
	}
	s.taskRuns["trun_123"] = &store.TaskRun{
		ID:     "trun_123",
		TaskID: "task_123",
		Status: TaskRunStatusRunning,
	}

	result := &TaskCompletedResult{
		RunID:        "trun_123",
		Success:      true,
		TokensInput:  100,
		TokensOutput: 200,
	}

	err := manager.OnTaskCompleted(context.Background(), result)
	require.NoError(t, err)

	run := s.taskRuns["trun_123"]
	assert.Equal(t, TaskRunStatusCompleted, run.Status)
	assert.NotNil(t, run.EndedAt)
	require.NotNil(t, run.TokensInput)
	assert.Equal(t, 100, *run.TokensInput)
	require.NotNil(t, run.TokensOutput)
	assert.Equal(t, 200, *run.TokensOutput)

	task := s.tasks["task_123"]
	assert.Equal(t, TaskStatusCompleted, task.Status)
}

func TestTaskManager_OnTaskCompleted_Failed(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:         "task_123",
		SessionID:  "sess_123",
		Status:     TaskStatusRunning,
		MaxRetries: 0,
	}
	s.taskRuns["trun_123"] = &store.TaskRun{
		ID:     "trun_123",
		TaskID: "task_123",
		Status: TaskRunStatusRunning,
	}

	result := &TaskCompletedResult{
		RunID:   "trun_123",
		Success: false,
		Error:   "execution failed",
	}

	err := manager.OnTaskCompleted(context.Background(), result)
	require.NoError(t, err)

	run := s.taskRuns["trun_123"]
	assert.Equal(t, TaskRunStatusFailed, run.Status)
	require.NotNil(t, run.Error)
	assert.Equal(t, "execution failed", *run.Error)

	task := s.tasks["task_123"]
	assert.Equal(t, TaskStatusFailed, task.Status)
}

func TestTaskManager_OnTaskCompleted_NotFound(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	result := &TaskCompletedResult{
		RunID:   "trun_nonexistent",
		Success: true,
	}

	err := manager.OnTaskCompleted(context.Background(), result)
	assert.ErrorIs(t, err, ErrTaskRunNotFound)
}

func TestTaskManager_FailRun(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.taskRuns["trun_123"] = &store.TaskRun{
		ID:     "trun_123",
		TaskID: "task_123",
		Status: TaskRunStatusRunning,
	}

	err := manager.FailRun(context.Background(), "trun_123", "runner disconnected")
	require.NoError(t, err)

	run := s.taskRuns["trun_123"]
	assert.Equal(t, TaskRunStatusFailed, run.Status)
	require.NotNil(t, run.Error)
	assert.Equal(t, "runner disconnected", *run.Error)
	assert.NotNil(t, run.EndedAt)
}

func TestTaskManager_Cancel(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:     "task_123",
		Status: TaskStatusPending,
	}

	err := manager.Cancel(context.Background(), "task_123")
	require.NoError(t, err)

	task := s.tasks["task_123"]
	assert.Equal(t, TaskStatusCanceled, task.Status)
}

func TestTaskManager_Cancel_Running(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:     "task_123",
		Status: TaskStatusRunning,
	}
	s.taskRuns["trun_123"] = &store.TaskRun{
		ID:     "trun_123",
		TaskID: "task_123",
		Status: TaskRunStatusRunning,
	}

	err := manager.Cancel(context.Background(), "task_123")
	require.NoError(t, err)

	task := s.tasks["task_123"]
	assert.Equal(t, TaskStatusCanceled, task.Status)

	run := s.taskRuns["trun_123"]
	assert.Equal(t, TaskRunStatusCanceled, run.Status)
}

func TestTaskManager_Cancel_AlreadyCompleted(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:     "task_123",
		Status: TaskStatusCompleted,
	}

	err := manager.Cancel(context.Background(), "task_123")
	assert.ErrorIs(t, err, ErrTaskAlreadyCompleted)
}

func TestTaskManager_Cancel_AlreadyCanceled(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:     "task_123",
		Status: TaskStatusCanceled,
	}

	err := manager.Cancel(context.Background(), "task_123")
	assert.ErrorIs(t, err, ErrTaskAlreadyCanceled)
}

func TestTaskManager_Cancel_NotFound(t *testing.T) {
	manager, _, _ := setupTaskManagerTest()

	err := manager.Cancel(context.Background(), "task_nonexistent")
	assert.ErrorIs(t, err, ErrTaskNotFound)
}

func TestTaskManager_ShouldRetry(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:         "task_123",
		Status:     TaskStatusRunning,
		MaxRetries: 3,
		RetryCount: 0,
	}

	shouldRetry, err := manager.ShouldRetry(context.Background(), "task_123")
	require.NoError(t, err)
	assert.True(t, shouldRetry)
}

func TestTaskManager_ShouldRetry_MaxRetriesExceeded(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:         "task_123",
		Status:     TaskStatusRunning,
		MaxRetries: 3,
		RetryCount: 3, // Already at max
	}

	shouldRetry, err := manager.ShouldRetry(context.Background(), "task_123")
	require.NoError(t, err)
	assert.False(t, shouldRetry)
}

func TestTaskManager_ShouldRetry_TaskCompleted(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:         "task_123",
		Status:     TaskStatusCompleted,
		MaxRetries: 3,
		RetryCount: 0,
	}

	shouldRetry, err := manager.ShouldRetry(context.Background(), "task_123")
	require.NoError(t, err)
	assert.False(t, shouldRetry)
}

func TestTaskManager_ShouldRetry_TaskCanceled(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:         "task_123",
		Status:     TaskStatusCanceled,
		MaxRetries: 3,
		RetryCount: 0,
	}

	shouldRetry, err := manager.ShouldRetry(context.Background(), "task_123")
	require.NoError(t, err)
	assert.False(t, shouldRetry)
}

func TestTaskManager_Retry(t *testing.T) {
	manager, s, cmdSender := setupTaskManagerTest()

	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	s.tasks["task_123"] = &store.Task{
		ID:             "task_123",
		SessionID:      "sess_123",
		Prompt:         "Build a REST API",
		Status:         TaskStatusRunning,
		MaxRetries:     3,
		RetryCount:     0,
		TimeoutSeconds: 3600,
	}

	run, err := manager.Retry(context.Background(), "task_123")
	require.NoError(t, err)
	require.NotNil(t, run)

	// Check retry count was incremented
	task := s.tasks["task_123"]
	assert.Equal(t, 1, task.RetryCount)

	// Check new run was created
	assert.Equal(t, "task_123", run.TaskID)
	assert.Equal(t, 2, run.Attempt) // First retry is attempt 2

	// Check command was sent
	assert.Len(t, cmdSender.sentCommands, 1)
}

func TestTaskManager_Retry_MaxRetriesExceeded(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	s.tasks["task_123"] = &store.Task{
		ID:         "task_123",
		SessionID:  "sess_123",
		MaxRetries: 3,
		RetryCount: 3, // Already at max
	}

	_, err := manager.Retry(context.Background(), "task_123")
	assert.ErrorIs(t, err, ErrMaxRetriesExceeded)
}

// =============================================================================
// IsValidTaskTransition Tests
// =============================================================================

func TestIsValidTaskTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		expected bool
	}{
		// Pending transitions
		{"pending to running", TaskStatusPending, TaskStatusRunning, true},
		{"pending to canceled", TaskStatusPending, TaskStatusCanceled, true},
		{"pending to completed", TaskStatusPending, TaskStatusCompleted, false},
		{"pending to failed", TaskStatusPending, TaskStatusFailed, false},

		// Running transitions
		{"running to completed", TaskStatusRunning, TaskStatusCompleted, true},
		{"running to failed", TaskStatusRunning, TaskStatusFailed, true},
		{"running to canceled", TaskStatusRunning, TaskStatusCanceled, true},
		{"running to pending", TaskStatusRunning, TaskStatusPending, false},

		// Completed transitions (terminal)
		{"completed to pending", TaskStatusCompleted, TaskStatusPending, false},
		{"completed to running", TaskStatusCompleted, TaskStatusRunning, false},
		{"completed to failed", TaskStatusCompleted, TaskStatusFailed, false},

		// Failed transitions (terminal)
		{"failed to pending", TaskStatusFailed, TaskStatusPending, false},
		{"failed to running", TaskStatusFailed, TaskStatusRunning, false},
		{"failed to completed", TaskStatusFailed, TaskStatusCompleted, false},

		// Canceled transitions (terminal)
		{"canceled to pending", TaskStatusCanceled, TaskStatusPending, false},
		{"canceled to running", TaskStatusCanceled, TaskStatusRunning, false},

		// Unknown status
		{"unknown to running", "unknown", TaskStatusRunning, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidTaskTransition(tt.from, tt.to)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// IsValidTaskRunTransition Tests
// =============================================================================

func TestIsValidTaskRunTransition(t *testing.T) {
	tests := []struct {
		name     string
		from     string
		to       string
		expected bool
	}{
		// Pending transitions
		{"pending to assigned", TaskRunStatusPending, TaskRunStatusAssigned, true},
		{"pending to canceled", TaskRunStatusPending, TaskRunStatusCanceled, true},
		{"pending to running", TaskRunStatusPending, TaskRunStatusRunning, false},
		{"pending to completed", TaskRunStatusPending, TaskRunStatusCompleted, false},

		// Assigned transitions
		{"assigned to running", TaskRunStatusAssigned, TaskRunStatusRunning, true},
		{"assigned to canceled", TaskRunStatusAssigned, TaskRunStatusCanceled, true},
		{"assigned to pending", TaskRunStatusAssigned, TaskRunStatusPending, false},
		{"assigned to completed", TaskRunStatusAssigned, TaskRunStatusCompleted, false},

		// Running transitions
		{"running to completed", TaskRunStatusRunning, TaskRunStatusCompleted, true},
		{"running to failed", TaskRunStatusRunning, TaskRunStatusFailed, true},
		{"running to timeout", TaskRunStatusRunning, TaskRunStatusTimeout, true},
		{"running to canceled", TaskRunStatusRunning, TaskRunStatusCanceled, true},
		{"running to pending", TaskRunStatusRunning, TaskRunStatusPending, false},
		{"running to assigned", TaskRunStatusRunning, TaskRunStatusAssigned, false},

		// Completed transitions (terminal)
		{"completed to pending", TaskRunStatusCompleted, TaskRunStatusPending, false},
		{"completed to running", TaskRunStatusCompleted, TaskRunStatusRunning, false},
		{"completed to canceled", TaskRunStatusCompleted, TaskRunStatusCanceled, false},

		// Failed transitions (terminal)
		{"failed to pending", TaskRunStatusFailed, TaskRunStatusPending, false},
		{"failed to running", TaskRunStatusFailed, TaskRunStatusRunning, false},
		{"failed to canceled", TaskRunStatusFailed, TaskRunStatusCanceled, false},

		// Timeout transitions (terminal)
		{"timeout to pending", TaskRunStatusTimeout, TaskRunStatusPending, false},
		{"timeout to running", TaskRunStatusTimeout, TaskRunStatusRunning, false},

		// Canceled transitions (terminal)
		{"canceled to pending", TaskRunStatusCanceled, TaskRunStatusPending, false},
		{"canceled to running", TaskRunStatusCanceled, TaskRunStatusRunning, false},

		// Unknown status
		{"unknown to running", "unknown", TaskRunStatusRunning, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidTaskRunTransition(tt.from, tt.to)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// =============================================================================
// TaskTimeoutEnforcer Tests
// =============================================================================

func TestTaskTimeoutEnforcer_StartStop(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
		WithTimeoutCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	enforcer.Start(ctx)

	// Let it run briefly
	time.Sleep(100 * time.Millisecond)

	// Stop should complete without hanging
	done := make(chan struct{})
	go func() {
		enforcer.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Fatal("Stop() did not complete in time")
	}
}

func TestTaskTimeoutEnforcer_ContextCancellation(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
		WithTimeoutCheckInterval(50*time.Millisecond),
	)

	ctx, cancel := context.WithCancel(context.Background())
	enforcer.Start(ctx)

	// Cancel context
	cancel()

	// doneCh should be closed
	select {
	case <-enforcer.doneCh:
		// Success
	case <-time.After(time.Second):
		t.Fatal("enforcer did not stop after context cancellation")
	}
}

func TestTaskTimeoutEnforcer_WithCheckInterval(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
		WithTimeoutCheckInterval(10*time.Second),
	)

	assert.Equal(t, 10*time.Second, enforcer.checkInterval)
}

// =============================================================================
// TaskTimeoutEnforcer CheckTimeouts Tests
// =============================================================================

func TestTaskTimeoutEnforcer_CheckTimeouts_NoRunningTasks(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
	)

	// No running task runs
	err := enforcer.checkTimeouts(context.Background())
	require.NoError(t, err)
}

func TestTaskTimeoutEnforcer_CheckTimeouts_NotTimedOut(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
	)

	// Create a task with 1 hour timeout
	s.tasks["task_1"] = &store.Task{
		ID:             "task_1",
		SessionID:      "sess_1",
		Status:         TaskStatusRunning,
		TimeoutSeconds: 3600,
	}

	// Create a running task run that just started
	startedAt := time.Now()
	s.taskRuns["trun_1"] = &store.TaskRun{
		ID:        "trun_1",
		TaskID:    "task_1",
		Status:    TaskRunStatusRunning,
		StartedAt: &startedAt,
	}

	err := enforcer.checkTimeouts(context.Background())
	require.NoError(t, err)

	// Task run should still be running
	assert.Equal(t, TaskRunStatusRunning, s.taskRuns["trun_1"].Status)
}

func TestTaskTimeoutEnforcer_CheckTimeouts_TimedOut(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
	)

	// Create a task with 1 second timeout
	s.tasks["task_1"] = &store.Task{
		ID:             "task_1",
		SessionID:      "sess_1",
		Status:         TaskStatusRunning,
		TimeoutSeconds: 1,
		MaxRetries:     0,
	}

	// Create a running task run that started 2 seconds ago
	startedAt := time.Now().Add(-2 * time.Second)
	runnerID := "run_1"
	s.taskRuns["trun_1"] = &store.TaskRun{
		ID:        "trun_1",
		TaskID:    "task_1",
		Status:    TaskRunStatusRunning,
		StartedAt: &startedAt,
		RunnerID:  &runnerID,
	}

	err := enforcer.checkTimeouts(context.Background())
	require.NoError(t, err)

	// Task run should be marked as timeout
	assert.Equal(t, TaskRunStatusTimeout, s.taskRuns["trun_1"].Status)
	assert.NotNil(t, s.taskRuns["trun_1"].Error)
	assert.Equal(t, "task execution timed out", *s.taskRuns["trun_1"].Error)

	// Kill command should have been sent
	require.Len(t, cmdSender.sentCommands, 1)
	killCmd := cmdSender.sentCommands[0].GetKillTask()
	require.NotNil(t, killCmd)
	assert.Equal(t, "task_1", killCmd.TaskId)
	assert.Equal(t, "trun_1", killCmd.RunId)
	assert.Equal(t, "timeout", killCmd.Reason)
}

func TestTaskTimeoutEnforcer_CheckTimeouts_TimedOutWithRetry(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
	)

	// Create a task with 1 second timeout and 1 retry
	s.tasks["task_1"] = &store.Task{
		ID:             "task_1",
		SessionID:      "sess_1",
		Status:         TaskStatusRunning,
		TimeoutSeconds: 1,
		MaxRetries:     1,
		RetryCount:     0,
	}

	// Create an active session
	s.sessions["sess_1"] = &store.Session{
		ID:     "sess_1",
		Status: SessionStatusActive,
	}

	// Create a running task run that started 2 seconds ago
	startedAt := time.Now().Add(-2 * time.Second)
	s.taskRuns["trun_1"] = &store.TaskRun{
		ID:        "trun_1",
		TaskID:    "task_1",
		Status:    TaskRunStatusRunning,
		StartedAt: &startedAt,
		Attempt:   1,
	}

	err := enforcer.checkTimeouts(context.Background())
	require.NoError(t, err)

	// Task run should be marked as timeout
	assert.Equal(t, TaskRunStatusTimeout, s.taskRuns["trun_1"].Status)

	// Give goroutine time to retry (async retry)
	time.Sleep(100 * time.Millisecond)

	// Task should still be running (retry creates new run)
	assert.Equal(t, TaskStatusRunning, s.tasks["task_1"].Status)
}

func TestTaskTimeoutEnforcer_CheckTimeouts_NoStartedAt(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
	)

	// Create a task
	s.tasks["task_1"] = &store.Task{
		ID:             "task_1",
		SessionID:      "sess_1",
		Status:         TaskStatusRunning,
		TimeoutSeconds: 1,
	}

	// Create a running task run with no started_at (edge case)
	s.taskRuns["trun_1"] = &store.TaskRun{
		ID:        "trun_1",
		TaskID:    "task_1",
		Status:    TaskRunStatusRunning,
		StartedAt: nil, // No started_at
	}

	err := enforcer.checkTimeouts(context.Background())
	require.NoError(t, err)

	// Task run should still be running (skipped due to no started_at)
	assert.Equal(t, TaskRunStatusRunning, s.taskRuns["trun_1"].Status)
}

func TestTaskTimeoutEnforcer_CheckTimeouts_TaskNotFound(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
	)

	// Create a running task run without corresponding task
	startedAt := time.Now().Add(-2 * time.Second)
	s.taskRuns["trun_1"] = &store.TaskRun{
		ID:        "trun_1",
		TaskID:    "task_nonexistent",
		Status:    TaskRunStatusRunning,
		StartedAt: &startedAt,
	}

	// Should not error, just log warning
	err := enforcer.checkTimeouts(context.Background())
	require.NoError(t, err)
}

func TestTaskTimeoutEnforcer_CheckTimeouts_NoRunnerID(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
	)

	// Create a task with 1 second timeout
	s.tasks["task_1"] = &store.Task{
		ID:             "task_1",
		SessionID:      "sess_1",
		Status:         TaskStatusRunning,
		TimeoutSeconds: 1,
		MaxRetries:     0,
	}

	// Create a running task run with no runner_id
	startedAt := time.Now().Add(-2 * time.Second)
	s.taskRuns["trun_1"] = &store.TaskRun{
		ID:        "trun_1",
		TaskID:    "task_1",
		Status:    TaskRunStatusRunning,
		StartedAt: &startedAt,
		RunnerID:  nil, // No runner
	}

	err := enforcer.checkTimeouts(context.Background())
	require.NoError(t, err)

	// Task run should be marked as timeout even without runner
	assert.Equal(t, TaskRunStatusTimeout, s.taskRuns["trun_1"].Status)

	// No kill command sent (no runner to kill)
	assert.Len(t, cmdSender.sentCommands, 0)
}

func TestTaskTimeoutEnforcer_CheckTimeouts_NoMoreRetries(t *testing.T) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	logger := zap.NewNop()
	taskMgr := NewTaskManager(s, cmdSender, nil, nil, logger)

	enforcer := NewTaskTimeoutEnforcer(
		s,
		taskMgr,
		cmdSender,
		logger,
	)

	// Create a task with 1 second timeout and max retries exhausted
	s.tasks["task_1"] = &store.Task{
		ID:             "task_1",
		SessionID:      "sess_1",
		Status:         TaskStatusRunning,
		TimeoutSeconds: 1,
		MaxRetries:     2,
		RetryCount:     2, // Already retried twice
	}

	// Create a running task run that started 2 seconds ago
	startedAt := time.Now().Add(-2 * time.Second)
	s.taskRuns["trun_1"] = &store.TaskRun{
		ID:        "trun_1",
		TaskID:    "task_1",
		Status:    TaskRunStatusRunning,
		StartedAt: &startedAt,
		Attempt:   3,
	}

	err := enforcer.checkTimeouts(context.Background())
	require.NoError(t, err)

	// Task run should be marked as timeout
	assert.Equal(t, TaskRunStatusTimeout, s.taskRuns["trun_1"].Status)

	// Task should be marked as failed (no more retries)
	assert.Equal(t, TaskStatusFailed, s.tasks["task_1"].Status)
}

// =============================================================================
// OnTaskCompleted Additional Tests
// =============================================================================

func TestTaskManager_OnTaskCompleted_WithTokens(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	// Create task
	s.tasks["task_1"] = &store.Task{
		ID:        "task_1",
		SessionID: "sess_1",
		Status:    TaskStatusRunning,
	}

	// Create running task run
	s.taskRuns["trun_1"] = &store.TaskRun{
		ID:     "trun_1",
		TaskID: "task_1",
		Status: TaskRunStatusRunning,
	}

	result := &TaskCompletedResult{
		RunID:        "trun_1",
		Success:      true,
		TokensInput:  100,
		TokensOutput: 200,
	}

	err := manager.OnTaskCompleted(context.Background(), result)
	require.NoError(t, err)

	// Check token values were saved
	run := s.taskRuns["trun_1"]
	require.NotNil(t, run.TokensInput)
	require.NotNil(t, run.TokensOutput)
	assert.Equal(t, 100, *run.TokensInput)
	assert.Equal(t, 200, *run.TokensOutput)
}

func TestTaskManager_OnTaskCompleted_FailedWithRetry(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	// Create task with retries available
	s.setTask("task_1", &store.Task{
		ID:             "task_1",
		SessionID:      "sess_1",
		Status:         TaskStatusRunning,
		MaxRetries:     2,
		RetryCount:     0,
		TimeoutSeconds: 3600,
	})

	// Create active session with a runner attached
	runnerID := "run_1"
	s.sessions["sess_1"] = &store.Session{
		ID:       "sess_1",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}

	// Create running task run
	s.setTaskRun("trun_1", &store.TaskRun{
		ID:      "trun_1",
		TaskID:  "task_1",
		Status:  TaskRunStatusRunning,
		Attempt: 1,
	})

	result := &TaskCompletedResult{
		RunID:   "trun_1",
		Success: false,
		Error:   "execution failed",
	}

	err := manager.OnTaskCompleted(context.Background(), result)
	require.NoError(t, err)

	// Task run should be failed
	assert.Equal(t, TaskRunStatusFailed, s.getTaskRun("trun_1").Status)

	// The retry is scheduled on the background pool behind a jittered backoff,
	// so wait for the effect rather than for a fixed duration.
	require.Eventually(t, func() bool {
		return s.getTask("task_1").RetryCount == 1
	}, 10*time.Second, 20*time.Millisecond, "the failed run must be retried")

	// Task should still be running (retry pending or in progress)
	task := s.getTask("task_1")
	assert.Equal(t, TaskStatusRunning, task.Status)
}

func TestTaskManager_OnTaskCompleted_FailedNoRetry(t *testing.T) {
	manager, s, _ := setupTaskManagerTest()

	// Create task with no retries
	s.tasks["task_1"] = &store.Task{
		ID:         "task_1",
		SessionID:  "sess_1",
		Status:     TaskStatusRunning,
		MaxRetries: 0,
		RetryCount: 0,
	}

	// Create running task run
	s.taskRuns["trun_1"] = &store.TaskRun{
		ID:      "trun_1",
		TaskID:  "task_1",
		Status:  TaskRunStatusRunning,
		Attempt: 1,
	}

	result := &TaskCompletedResult{
		RunID:   "trun_1",
		Success: false,
		Error:   "execution failed",
	}

	err := manager.OnTaskCompleted(context.Background(), result)
	require.NoError(t, err)

	// Task run should be failed
	assert.Equal(t, TaskRunStatusFailed, s.taskRuns["trun_1"].Status)

	// Task should also be failed (no retries)
	assert.Equal(t, TaskStatusFailed, s.tasks["task_1"].Status)
}

// BeginTx shadows the embedded implementation so the transaction is bound to
// this store rather than the store it embeds. See storeTx.
func (s *testTaskStore) BeginTx(_ context.Context) (store.Tx, error) {
	return &storeTx{Store: s}, nil
}

// =============================================================================
// Dispatch atomicity tests
// =============================================================================

// TestTaskManager_Execute_SendFailureUnwinds covers the invariant that a task
// which never reached a runner is not left "running" forever. Execute used to
// update the task status, then send, then only fail the run on error - leaving
// the task running with nothing on the other end to finish it.
func TestTaskManager_Execute_SendFailureUnwinds(t *testing.T) {
	manager, s, cmdSender := setupTaskManagerTest()
	cmdSender.sendErr = errors.New("runner disconnected")

	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	s.tasks["task_123"] = &store.Task{
		ID:             "task_123",
		SessionID:      "sess_123",
		Prompt:         "Build a REST API",
		Status:         TaskStatusPending,
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}

	err := manager.Execute(context.Background(), "task_123")
	require.Error(t, err)

	task, err := s.GetTask(context.Background(), "task_123")
	require.NoError(t, err)
	assert.Equal(t, TaskStatusPending, task.Status,
		"a task whose command never reached a runner must not stay running")
	assert.Equal(t, 0, task.RetryCount, "a failed dispatch must not spend the retry budget")

	runs, err := s.ListTaskRuns(context.Background(), store.ListTaskRunsOptions{})
	require.NoError(t, err)
	require.Len(t, runs.Items, 1, "the attempt must still be recorded for the audit trail")
	assert.Equal(t, TaskRunStatusFailed, runs.Items[0].Status)
	require.NotNil(t, runs.Items[0].Error)
	assert.Contains(t, *runs.Items[0].Error, "runner disconnected")
}

// TestTaskManager_Retry_SendFailureRestoresBudget is the retry half of the same
// invariant: an unreachable runner must not be able to burn a task's retries.
func TestTaskManager_Retry_SendFailureRestoresBudget(t *testing.T) {
	manager, s, cmdSender := setupTaskManagerTest()
	cmdSender.sendErr = errors.New("runner disconnected")

	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	s.tasks["task_123"] = &store.Task{
		ID:             "task_123",
		SessionID:      "sess_123",
		Prompt:         "Build a REST API",
		Status:         TaskStatusRunning,
		MaxRetries:     3,
		RetryCount:     1,
		TimeoutSeconds: 3600,
	}

	_, err := manager.Retry(context.Background(), "task_123")
	require.Error(t, err)

	task, err := s.GetTask(context.Background(), "task_123")
	require.NoError(t, err)
	assert.Equal(t, 1, task.RetryCount, "retry budget must be handed back on a failed dispatch")
}

// TestTaskManager_Execute_NoRunRecordedWhenNoRunner proves the run and the task
// status move together: a rejected dispatch leaves no orphan run behind.
func TestTaskManager_Execute_NoRunRecordedWhenNoRunner(t *testing.T) {
	manager, s, cmdSender := setupTaskManagerTest()

	s.sessions["sess_123"] = &store.Session{
		ID:     "sess_123",
		Status: SessionStatusPending,
	}
	s.tasks["task_123"] = &store.Task{
		ID:        "task_123",
		SessionID: "sess_123",
		Status:    TaskStatusPending,
	}

	err := manager.Execute(context.Background(), "task_123")
	require.ErrorIs(t, err, ErrNoRunnerAttached)

	runs, err := s.ListTaskRuns(context.Background(), store.ListTaskRunsOptions{})
	require.NoError(t, err)
	assert.Empty(t, runs.Items)

	task, err := s.GetTask(context.Background(), "task_123")
	require.NoError(t, err)
	assert.Equal(t, TaskStatusPending, task.Status)
	assert.Empty(t, cmdSender.sentCommands)
}

// EnsureRunner satisfies SessionManagerInterface. These fakes never allocate.
func (m *mockSessionMgrForTask) EnsureRunner(_ context.Context, sessionID string) (*store.Session, error) {
	m.ensureCalls++
	if m.ensureRunnerErr != nil {
		return nil, m.ensureRunnerErr
	}
	return &store.Session{ID: sessionID, Status: SessionStatusActive}, nil
}

// =============================================================================
// G2: automatic dispatch
// =============================================================================

func setupAutoDispatchTest() (*TaskManager, *testTaskStore, *mockCommandSender, *mockSessionMgrForTask) {
	s := newTestTaskStore()
	cmdSender := &mockCommandSender{}
	sessionMgr := &mockSessionMgrForTask{}
	manager := NewTaskManager(s, cmdSender, sessionMgr, nil, zap.NewNop())

	runnerID := "run_123"
	s.sessions["sess_123"] = &store.Session{
		ID:       "sess_123",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
	}
	return manager, s, cmdSender, sessionMgr
}

// TestTaskManager_Create_AutoDispatches is G2: creating a task on an active
// session with an idle runner executes it, with no separate POST /execute.
func TestTaskManager_Create_AutoDispatches(t *testing.T) {
	manager, s, cmdSender, sessionMgr := setupAutoDispatchTest()

	task, err := manager.Create(context.Background(), CreateTaskOptions{
		SessionID: "sess_123",
		Prompt:    "echo marionette",
	})
	require.NoError(t, err)

	assert.Equal(t, 1, sessionMgr.ensureCalls, "creation must ensure the session has a runner")
	require.Len(t, cmdSender.sentCommands, 1, "the task must be dispatched on creation")
	assert.Equal(t, task.ID, cmdSender.sentCommands[0].GetExecuteTask().GetTaskId())

	stored, err := s.GetTask(context.Background(), task.ID)
	require.NoError(t, err)
	assert.Equal(t, TaskStatusRunning, stored.Status)
}

// TestTaskManager_Create_StaysPendingWithoutRunner is the other half of G1:
// with no runner to be had, the task is created and parks as pending.
func TestTaskManager_Create_StaysPendingWithoutRunner(t *testing.T) {
	manager, s, cmdSender, sessionMgr := setupAutoDispatchTest()
	sessionMgr.ensureRunnerErr = ErrNoRunnerAvailable

	task, err := manager.Create(context.Background(), CreateTaskOptions{
		SessionID: "sess_123",
		Prompt:    "echo marionette",
	})
	require.NoError(t, err, "creation must succeed even when nothing can run it")

	assert.Empty(t, cmdSender.sentCommands)
	stored, err := s.GetTask(context.Background(), task.ID)
	require.NoError(t, err)
	assert.Equal(t, TaskStatusPending, stored.Status,
		"a task with nowhere to run must say so, not look running")
}

// TestTaskManager_Create_DoesNotJumpTheQueue keeps execution sequential: a
// session already running a task must not have a second one dispatched into it.
func TestTaskManager_Create_DoesNotJumpTheQueue(t *testing.T) {
	manager, s, cmdSender, _ := setupAutoDispatchTest()

	s.tasks["task_running"] = &store.Task{
		ID:        "task_running",
		SessionID: "sess_123",
		Status:    TaskStatusRunning,
		Prompt:    "first",
	}

	_, err := manager.Create(context.Background(), CreateTaskOptions{
		SessionID: "sess_123",
		Prompt:    "second",
	})
	require.NoError(t, err)

	assert.Empty(t, cmdSender.sentCommands,
		"a session with a task in flight must not dispatch another")
}

// TestTaskManager_Create_FailedDispatchParksPending holds coordinator verdict
// 3b: a dispatch that reached a runner and failed is not retried in a loop, it
// parks as pending for manual re-trigger.
func TestTaskManager_Create_FailedDispatchParksPending(t *testing.T) {
	manager, s, cmdSender, _ := setupAutoDispatchTest()
	cmdSender.sendErr = errors.New("runner disconnected")

	task, err := manager.Create(context.Background(), CreateTaskOptions{
		SessionID: "sess_123",
		Prompt:    "echo marionette",
	})
	require.NoError(t, err)

	stored, err := s.GetTask(context.Background(), task.ID)
	require.NoError(t, err)
	assert.Equal(t, TaskStatusPending, stored.Status)
	assert.Equal(t, 0, stored.RetryCount, "a failed dispatch must not spend the retry budget")
}

func TestTaskManager_DispatchNext_PicksOldestPendingTask(t *testing.T) {
	manager, s, cmdSender, _ := setupAutoDispatchTest()

	base := time.Now()
	s.tasks["task_new"] = &store.Task{
		ID: "task_new", SessionID: "sess_123", Status: TaskStatusPending,
		Prompt: "second", CreatedAt: base,
	}
	s.tasks["task_old"] = &store.Task{
		ID: "task_old", SessionID: "sess_123", Status: TaskStatusPending,
		Prompt: "first", CreatedAt: base.Add(-time.Hour),
	}

	require.NoError(t, manager.DispatchNext(context.Background(), "sess_123"))

	require.Len(t, cmdSender.sentCommands, 1)
	assert.Equal(t, "task_old", cmdSender.sentCommands[0].GetExecuteTask().GetTaskId(),
		"tasks are sequential, so the oldest pending goes first")
}

func TestTaskManager_DispatchNext_NothingPending(t *testing.T) {
	manager, _, cmdSender, _ := setupAutoDispatchTest()

	require.NoError(t, manager.DispatchNext(context.Background(), "sess_123"),
		"an idle session is not an error")
	assert.Empty(t, cmdSender.sentCommands)
}
