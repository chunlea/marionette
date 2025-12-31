package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// =============================================================================
// Task Tests
// =============================================================================

func TestTaskCRUD(t *testing.T) {
	ctx := context.Background()

	// Create workspace and session first
	workspace := &store.Workspace{
		Name:        "task-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{}, // Required, NOT NULL
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	// Create task
	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Test prompt",
		Status:         "pending",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}

	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)
	assert.NotEmpty(t, task.ID)

	// Get
	got, err := testStore.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "Test prompt", got.Prompt)

	// Create task run
	taskRun := &store.TaskRun{
		TaskID:  task.ID,
		Attempt: 1,
		Status:  "pending",
	}

	err = testStore.CreateTaskRun(ctx, taskRun)
	require.NoError(t, err)
	assert.NotEmpty(t, taskRun.ID)

	// Get task run
	gotRun, err := testStore.GetTaskRun(ctx, taskRun.ID)
	require.NoError(t, err)
	assert.Equal(t, task.ID, gotRun.TaskID)
	assert.Equal(t, 1, gotRun.Attempt)

	// List task runs
	runs, err := testStore.ListTaskRuns(ctx, store.ListTaskRunsOptions{
		TaskID: &task.ID,
	})
	require.NoError(t, err)
	assert.Len(t, runs.Items, 1)
}

func TestGetTaskRunByTaskAndAttempt(t *testing.T) {
	ctx := context.Background()

	// Create workspace and session first
	workspace := &store.Workspace{
		Name:        "task-run-attempt-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	// Create task
	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Test prompt for attempt lookup",
		Status:         "pending",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}
	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)

	// Create task run (attempt=1)
	taskRun := &store.TaskRun{
		TaskID:  task.ID,
		Attempt: 1,
		Status:  "pending",
	}
	err = testStore.CreateTaskRun(ctx, taskRun)
	require.NoError(t, err)

	// Test: Get task run by task ID and attempt
	gotRun, err := testStore.GetTaskRunByTaskAndAttempt(ctx, task.ID, 1)
	require.NoError(t, err)
	assert.Equal(t, taskRun.ID, gotRun.ID)
	assert.Equal(t, task.ID, gotRun.TaskID)
	assert.Equal(t, 1, gotRun.Attempt)

	// Test: Non-existent attempt returns ErrNotFound
	_, err = testStore.GetTaskRunByTaskAndAttempt(ctx, task.ID, 99)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup
	_ = testStore.DeleteSession(ctx, session.ID)
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

func TestUpdateTaskRun(t *testing.T) {
	ctx := context.Background()

	// Create workspace and session first
	workspace := &store.Workspace{
		Name:        "task-run-update-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	// Create task
	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Test prompt for task run updates",
		Status:         "pending",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}
	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)

	// Create task run
	taskRun := &store.TaskRun{
		TaskID:  task.ID,
		Attempt: 1,
		Status:  "pending",
	}
	err = testStore.CreateTaskRun(ctx, taskRun)
	require.NoError(t, err)

	// Update status, error, and exit_code
	newStatus := "failed"
	errorMsg := "task execution failed"
	exitCode := 1
	err = testStore.UpdateTaskRun(ctx, taskRun.ID, store.TaskRunUpdates{
		Status:   &newStatus,
		Error:    &errorMsg,
		ExitCode: &exitCode,
	})
	require.NoError(t, err)

	// Verify the updates were applied
	updated, err := testStore.GetTaskRun(ctx, taskRun.ID)
	require.NoError(t, err)
	assert.Equal(t, "failed", updated.Status)
	assert.NotNil(t, updated.Error)
	assert.Equal(t, "task execution failed", *updated.Error)
	assert.NotNil(t, updated.ExitCode)
	assert.Equal(t, 1, *updated.ExitCode)

	// Update status to completed with no error
	completedStatus := "completed"
	exitCodeZero := 0
	err = testStore.UpdateTaskRun(ctx, taskRun.ID, store.TaskRunUpdates{
		Status:   &completedStatus,
		ExitCode: &exitCodeZero,
	})
	require.NoError(t, err)

	// Verify second update
	updated, err = testStore.GetTaskRun(ctx, taskRun.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", updated.Status)
	assert.NotNil(t, updated.ExitCode)
	assert.Equal(t, 0, *updated.ExitCode)
	// Error should still be there from previous update
	assert.NotNil(t, updated.Error)

	// Cleanup
	_ = testStore.DeleteSession(ctx, session.ID)
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

// =============================================================================
// ScheduledTask Tests
// =============================================================================

func TestScheduledTaskCRUD(t *testing.T) {
	ctx := context.Background()

	// Create workspace and session first
	workspace := &store.Workspace{
		Name:        "scheduled-task-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "scheduled",
		ScheduleCron:  strPtr("0 9 * * 1-5"),
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	// Create scheduled task
	scheduledTask := &store.ScheduledTask{
		SessionID:              session.ID,
		Name:                   "daily-standup-" + time.Now().Format("150405"),
		Description:            strPtr("Daily standup summary"),
		CronExpression:         "0 9 * * 1-5",
		Timezone:               "America/Los_Angeles",
		PromptTemplate:         "Generate standup summary for today",
		TimeoutSeconds:         3600,
		MaxRetries:             2,
		Status:                 "active",
		OnFailure:              "pause_on_failure",
		MaxConsecutiveFailures: intPtr(3),
	}

	err = testStore.CreateScheduledTask(ctx, scheduledTask)
	require.NoError(t, err)
	assert.NotEmpty(t, scheduledTask.ID)
	assert.NotZero(t, scheduledTask.CreatedAt)

	// Get
	got, err := testStore.GetScheduledTask(ctx, scheduledTask.ID)
	require.NoError(t, err)
	assert.Equal(t, scheduledTask.Name, got.Name)
	assert.Equal(t, "0 9 * * 1-5", got.CronExpression)
	assert.Equal(t, "America/Los_Angeles", got.Timezone)
	assert.Equal(t, "active", got.Status)

	// Update
	newStatus := "paused"
	err = testStore.UpdateScheduledTask(ctx, scheduledTask.ID, store.ScheduledTaskUpdates{
		Status: &newStatus,
	})
	require.NoError(t, err)

	got, err = testStore.GetScheduledTask(ctx, scheduledTask.ID)
	require.NoError(t, err)
	assert.Equal(t, "paused", got.Status)

	// List
	list, err := testStore.ListScheduledTasks(ctx, store.ListScheduledTasksOptions{
		SessionID: &session.ID,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// Delete
	err = testStore.DeleteScheduledTask(ctx, scheduledTask.ID)
	require.NoError(t, err)

	_, err = testStore.GetScheduledTask(ctx, scheduledTask.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup
	_ = testStore.DeleteSession(ctx, session.ID)
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

func TestUpdateTask(t *testing.T) {
	ctx := context.Background()

	// Create workspace and session first
	workspace := &store.Workspace{
		Name:        "update-task-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	// Create task
	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Original prompt",
		Status:         "pending",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}
	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)

	// Update status
	newStatus := "running"
	err = testStore.UpdateTask(ctx, task.ID, store.TaskUpdates{
		Status: &newStatus,
	})
	require.NoError(t, err)

	// Verify update
	got, err := testStore.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "running", got.Status)
	assert.Equal(t, "Original prompt", got.Prompt) // Unchanged

	// Update retry count
	newRetryCount := 1
	err = testStore.UpdateTask(ctx, task.ID, store.TaskUpdates{
		RetryCount: &newRetryCount,
	})
	require.NoError(t, err)

	got, err = testStore.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, got.RetryCount)

	// Update multiple fields
	completedStatus := "completed"
	finalRetryCount := 2
	err = testStore.UpdateTask(ctx, task.ID, store.TaskUpdates{
		Status:     &completedStatus,
		RetryCount: &finalRetryCount,
	})
	require.NoError(t, err)

	got, err = testStore.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, "completed", got.Status)
	assert.Equal(t, 2, got.RetryCount)

	// Cleanup
	_ = testStore.DeleteSession(ctx, session.ID)
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

func TestDeleteTask(t *testing.T) {
	ctx := context.Background()

	// Create workspace and session first
	workspace := &store.Workspace{
		Name:        "delete-task-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	// Create task
	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Task to be deleted",
		Status:         "pending",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}
	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)

	// Verify task exists
	got, err := testStore.GetTask(ctx, task.ID)
	require.NoError(t, err)
	assert.Equal(t, task.ID, got.ID)

	// Delete task
	err = testStore.DeleteTask(ctx, task.ID)
	require.NoError(t, err)

	// Verify task is deleted
	_, err = testStore.GetTask(ctx, task.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Delete non-existent task should return ErrNotFound
	err = testStore.DeleteTask(ctx, task.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup
	_ = testStore.DeleteSession(ctx, session.ID)
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

func TestListTasks(t *testing.T) {
	ctx := context.Background()

	// Create workspace and session first
	workspace := &store.Workspace{
		Name:        "list-tasks-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	// Create multiple tasks with different statuses
	tasks := []*store.Task{
		{
			SessionID:      session.ID,
			Prompt:         "Task 1",
			Status:         "pending",
			MaxRetries:     3,
			TimeoutSeconds: 3600,
		},
		{
			SessionID:      session.ID,
			Prompt:         "Task 2",
			Status:         "running",
			MaxRetries:     3,
			TimeoutSeconds: 3600,
		},
		{
			SessionID:      session.ID,
			Prompt:         "Task 3",
			Status:         "completed",
			MaxRetries:     3,
			TimeoutSeconds: 3600,
		},
		{
			SessionID:      session.ID,
			Prompt:         "Task 4",
			Status:         "pending",
			MaxRetries:     3,
			TimeoutSeconds: 3600,
		},
	}

	for _, task := range tasks {
		err := testStore.CreateTask(ctx, task)
		require.NoError(t, err)
	}

	// Test: List all tasks for the session
	result, err := testStore.ListTasks(ctx, store.ListTasksOptions{
		SessionID: &session.ID,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Items), 4)

	// Test: Filter by status (pending)
	result, err = testStore.ListTasks(ctx, store.ListTasksOptions{
		SessionID: &session.ID,
		Status:    []string{"pending"},
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Items), 2)
	for _, task := range result.Items {
		assert.Equal(t, "pending", task.Status)
	}

	// Test: Filter by status (running)
	result, err = testStore.ListTasks(ctx, store.ListTasksOptions{
		SessionID: &session.ID,
		Status:    []string{"running"},
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Items), 1)
	for _, task := range result.Items {
		assert.Equal(t, "running", task.Status)
	}

	// Test: Filter by multiple statuses
	result, err = testStore.ListTasks(ctx, store.ListTasksOptions{
		SessionID: &session.ID,
		Status:    []string{"pending", "running"},
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Items), 3)
	for _, task := range result.Items {
		assert.Contains(t, []string{"pending", "running"}, task.Status)
	}

	// Cleanup
	_ = testStore.DeleteSession(ctx, session.ID)
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

// =============================================================================
// Helper Functions
// =============================================================================
