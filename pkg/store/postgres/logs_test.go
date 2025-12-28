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
// Log Tests
// =============================================================================

func TestLogCRUD(t *testing.T) {
	ctx := context.Background()

	// Create prerequisites: Workspace, Session, Task, TaskRun, Runner
	workspace := &store.Workspace{
		Name:        "log-test-ws-" + time.Now().Format("150405"),
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

	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Test prompt",
		Status:         "running",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}
	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)

	runner := &store.Runner{
		Name:         "log-test-runner-" + time.Now().Format("150405"),
		Hostname:     "localhost",
		Status:       "busy",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	err = testStore.CreateRunner(ctx, runner)
	require.NoError(t, err)

	taskRun := &store.TaskRun{
		TaskID:   task.ID,
		Attempt:  1,
		Status:   "running",
		RunnerID: &runner.ID,
	}
	err = testStore.CreateTaskRun(ctx, taskRun)
	require.NoError(t, err)

	// Create Log
	log := &store.Log{
		SessionID: session.ID,
		TaskID:    task.ID,
		RunID:     taskRun.ID,
		RunnerID:  runner.ID,
		Stream:    "stdout",
		Content:   []byte("Test log message"),
		Sequence:  1,
	}

	err = testStore.CreateLog(ctx, log)
	require.NoError(t, err)
	assert.NotEmpty(t, log.ID)
	assert.NotZero(t, log.CreatedAt)

	// Get Log
	got, err := testStore.GetLog(ctx, log.ID)
	require.NoError(t, err)
	assert.Equal(t, log.SessionID, got.SessionID)
	assert.Equal(t, log.TaskID, got.TaskID)
	assert.Equal(t, log.RunID, got.RunID)
	assert.Equal(t, "stdout", got.Stream)
	assert.Equal(t, []byte("Test log message"), got.Content)
	assert.Equal(t, int64(1), got.Sequence)

	// List Logs
	list, err := testStore.ListLogs(ctx, store.ListLogsOptions{
		RunID: &taskRun.ID,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// List Logs by session
	listBySession, err := testStore.ListLogs(ctx, store.ListLogsOptions{
		SessionID: &session.ID,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listBySession.Items), 1)

	// List Logs by stream
	listByStream, err := testStore.ListLogs(ctx, store.ListLogsOptions{
		Stream: []string{"stdout"},
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listByStream.Items), 1)

	// DeleteLogsByRun
	err = testStore.DeleteLogsByRun(ctx, taskRun.ID)
	require.NoError(t, err)

	_, err = testStore.GetLog(ctx, log.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup
	_ = testStore.DeleteRunner(ctx, runner.ID)
}

func TestLogBatch(t *testing.T) {
	ctx := context.Background()

	// Create prerequisites
	workspace := &store.Workspace{
		Name:        "log-batch-ws-" + time.Now().Format("150405"),
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

	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Test prompt",
		Status:         "running",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}
	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)

	runner := &store.Runner{
		Name:         "log-batch-runner-" + time.Now().Format("150405"),
		Hostname:     "localhost",
		Status:       "busy",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	err = testStore.CreateRunner(ctx, runner)
	require.NoError(t, err)

	taskRun := &store.TaskRun{
		TaskID:   task.ID,
		Attempt:  1,
		Status:   "running",
		RunnerID: &runner.ID,
	}
	err = testStore.CreateTaskRun(ctx, taskRun)
	require.NoError(t, err)

	// Create batch of logs
	logs := []*store.Log{
		{
			SessionID: session.ID,
			TaskID:    task.ID,
			RunID:     taskRun.ID,
			RunnerID:  runner.ID,
			Stream:    "stdout",
			Content:   []byte("Log message 1"),
			Sequence:  1,
		},
		{
			SessionID: session.ID,
			TaskID:    task.ID,
			RunID:     taskRun.ID,
			RunnerID:  runner.ID,
			Stream:    "stdout",
			Content:   []byte("Log message 2"),
			Sequence:  2,
		},
		{
			SessionID: session.ID,
			TaskID:    task.ID,
			RunID:     taskRun.ID,
			RunnerID:  runner.ID,
			Stream:    "stderr",
			Content:   []byte("Error message"),
			Sequence:  3,
		},
	}

	err = testStore.CreateLogBatch(ctx, logs)
	require.NoError(t, err)
	for _, log := range logs {
		assert.NotEmpty(t, log.ID)
	}

	// List all logs for this run
	list, err := testStore.ListLogs(ctx, store.ListLogsOptions{
		RunID: &taskRun.ID,
	})
	require.NoError(t, err)
	assert.Equal(t, 3, len(list.Items))

	// Filter by stream
	listByStream, err := testStore.ListLogs(ctx, store.ListLogsOptions{
		RunID:  &taskRun.ID,
		Stream: []string{"stderr"},
	})
	require.NoError(t, err)
	assert.Equal(t, 1, len(listByStream.Items))
	assert.Equal(t, []byte("Error message"), listByStream.Items[0].Content)

	// Cleanup
	_ = testStore.DeleteLogsByRun(ctx, taskRun.ID)
	_ = testStore.DeleteRunner(ctx, runner.ID)
}

// =============================================================================
// LogArchive Tests
// =============================================================================

func TestLogArchiveCRUD(t *testing.T) {
	ctx := context.Background()

	// Create session for archive
	workspace := &store.Workspace{
		Name:        "archive-test-ws-" + time.Now().Format("150405"),
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

	// Create LogArchive
	now := time.Now()
	archive := &store.LogArchive{
		SessionID:        session.ID,
		StorageKey:       "s3://bucket/logs/" + session.ID + ".zst",
		StorageSizeBytes: int64Ptr(1024000),
		LogCount:         500,
		FirstLogAt:       &now,
		LastLogAt:        &now,
	}

	err = testStore.CreateLogArchive(ctx, archive)
	require.NoError(t, err)
	assert.NotEmpty(t, archive.ID)
	assert.NotZero(t, archive.ArchivedAt)

	// Get LogArchive
	got, err := testStore.GetLogArchive(ctx, archive.ID)
	require.NoError(t, err)
	assert.Equal(t, session.ID, got.SessionID)
	assert.Equal(t, "s3://bucket/logs/"+session.ID+".zst", got.StorageKey)
	assert.Equal(t, int64(1024000), *got.StorageSizeBytes)
	assert.Equal(t, int64(500), got.LogCount)

	// Get LogArchive by session
	gotBySession, err := testStore.GetLogArchiveBySession(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, archive.ID, gotBySession.ID)

	// List LogArchives
	list, err := testStore.ListLogArchives(ctx, store.ListLogArchivesOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// Update LogArchive (soft delete)
	deletedAt := time.Now()
	err = testStore.UpdateLogArchive(ctx, archive.ID, store.LogArchiveUpdates{
		DeletedAt: &deletedAt,
	})
	require.NoError(t, err)

	// Should not appear in list without IncludeDeleted
	list, err = testStore.ListLogArchives(ctx, store.ListLogArchivesOptions{})
	require.NoError(t, err)
	for _, a := range list.Items {
		assert.NotEqual(t, archive.ID, a.ID)
	}

	// Should appear with IncludeDeleted
	listDeleted, err := testStore.ListLogArchives(ctx, store.ListLogArchivesOptions{
		IncludeDeleted: true,
	})
	require.NoError(t, err)
	found := false
	for _, a := range listDeleted.Items {
		if a.ID == archive.ID {
			found = true
			assert.NotNil(t, a.DeletedAt)
		}
	}
	assert.True(t, found, "deleted archive should appear with IncludeDeleted")

	// Delete LogArchive
	err = testStore.DeleteLogArchive(ctx, archive.ID)
	require.NoError(t, err)

	_, err = testStore.GetLogArchive(ctx, archive.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// =============================================================================
// ActionLog Tests
// =============================================================================

func TestActionLogCRUD(t *testing.T) {
	ctx := context.Background()

	// Create session for action log context
	workspace := &store.Workspace{
		Name:        "action-log-ws-" + time.Now().Format("150405"),
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

	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "Test prompt",
		Status:         "running",
		MaxRetries:     3,
		TimeoutSeconds: 3600,
	}
	err = testStore.CreateTask(ctx, task)
	require.NoError(t, err)

	// Create ActionLog
	actionLog := &store.ActionLog{
		ActorType:    "api_key",
		ActorID:      strPtr("key_test123"),
		ActorName:    strPtr("test-api-key"),
		Action:       "permission.approved",
		ResourceType: "permission_request",
		ResourceID:   "perm_test123",
		SessionID:    &session.ID,
		TaskID:       &task.ID,
		IPAddress:    strPtr("192.168.1.1"),
		UserAgent:    strPtr("test-client/1.0"),
		Success:      true,
	}

	err = testStore.CreateActionLog(ctx, actionLog)
	require.NoError(t, err)
	assert.NotEmpty(t, actionLog.ID)
	assert.NotZero(t, actionLog.CreatedAt)

	// Get ActionLog
	got, err := testStore.GetActionLog(ctx, actionLog.ID)
	require.NoError(t, err)
	assert.Equal(t, "api_key", got.ActorType)
	assert.Equal(t, "key_test123", *got.ActorID)
	assert.Equal(t, "test-api-key", *got.ActorName)
	assert.Equal(t, "permission.approved", got.Action)
	assert.Equal(t, "permission_request", got.ResourceType)
	assert.Equal(t, "perm_test123", got.ResourceID)
	assert.Equal(t, session.ID, *got.SessionID)
	assert.Equal(t, task.ID, *got.TaskID)
	assert.True(t, got.Success)

	// List ActionLogs
	list, err := testStore.ListActionLogs(ctx, store.ListActionLogsOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// List by ActorType
	listByActor, err := testStore.ListActionLogs(ctx, store.ListActionLogsOptions{
		ActorType: strPtr("api_key"),
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listByActor.Items), 1)

	// List by Action
	listByAction, err := testStore.ListActionLogs(ctx, store.ListActionLogsOptions{
		Action: strPtr("permission.approved"),
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listByAction.Items), 1)

	// List by ResourceType
	listByResource, err := testStore.ListActionLogs(ctx, store.ListActionLogsOptions{
		ResourceType: strPtr("permission_request"),
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listByResource.Items), 1)

	// List by SessionID
	listBySession, err := testStore.ListActionLogs(ctx, store.ListActionLogsOptions{
		SessionID: &session.ID,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listBySession.Items), 1)

	// List by Success
	success := true
	listBySuccess, err := testStore.ListActionLogs(ctx, store.ListActionLogsOptions{
		Success: &success,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listBySuccess.Items), 1)
	for _, log := range listBySuccess.Items {
		assert.True(t, log.Success)
	}
}

func TestActionLogFilters(t *testing.T) {
	ctx := context.Background()

	// Create multiple action logs with different attributes
	actionLogs := []*store.ActionLog{
		{
			ActorType:    "api_key",
			ActorID:      strPtr("key_123"),
			Action:       "session.created",
			ResourceType: "session",
			ResourceID:   "sess_123",
			Success:      true,
		},
		{
			ActorType:    "system",
			Action:       "session.suspended",
			ResourceType: "session",
			ResourceID:   "sess_456",
			Success:      true,
		},
		{
			ActorType:    "api_key",
			ActorID:      strPtr("key_456"),
			Action:       "task.created",
			ResourceType: "task",
			ResourceID:   "task_789",
			Success:      false,
			ErrorMessage: strPtr("validation failed"),
		},
	}

	for _, log := range actionLogs {
		err := testStore.CreateActionLog(ctx, log)
		require.NoError(t, err)
	}

	// Filter by multiple criteria
	listSystemActions, err := testStore.ListActionLogs(ctx, store.ListActionLogsOptions{
		ActorType: strPtr("system"),
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listSystemActions.Items), 1)
	for _, log := range listSystemActions.Items {
		assert.Equal(t, "system", log.ActorType)
	}

	// Filter by failure
	failure := false
	listFailures, err := testStore.ListActionLogs(ctx, store.ListActionLogsOptions{
		Success: &failure,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(listFailures.Items), 1)
	for _, log := range listFailures.Items {
		assert.False(t, log.Success)
	}
}

// =============================================================================
// Helper Functions
// =============================================================================


