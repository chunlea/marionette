package grpc

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockRunnerManager implements RunnerManagerInterface for testing.
type mockRunnerManager struct {
	onConnectCalled    bool
	onDisconnectCalled bool
	onHeartbeatCalled  bool
	setStatusCalled    bool
	lastHeartbeat      *pb.Heartbeat
	lastStatus         string
	setStatusErr       error
}

func (m *mockRunnerManager) OnConnect(_ context.Context, _ string) error {
	m.onConnectCalled = true
	return nil
}

func (m *mockRunnerManager) OnDisconnect(_ context.Context, _ string) error {
	m.onDisconnectCalled = true
	return nil
}

func (m *mockRunnerManager) OnHeartbeat(_ context.Context, _ string, hb *pb.Heartbeat) error {
	m.onHeartbeatCalled = true
	m.lastHeartbeat = hb
	return nil
}

func (m *mockRunnerManager) SetStatus(_ context.Context, _ string, status string) error {
	m.setStatusCalled = true
	m.lastStatus = status
	return m.setStatusErr
}

// mockTaskManager implements core.TaskManagerInterface for testing.
type mockTaskManager struct {
	onTaskAcceptedCalled  bool
	onTaskStartedCalled   bool
	onTaskProgressCalled  bool
	onTaskCompletedCalled bool

	lastRunID        string
	lastProgress     int
	lastCompletedRes *core.TaskCompletedResult

	onTaskAcceptedErr  error
	onTaskStartedErr   error
	onTaskProgressErr  error
	onTaskCompletedErr error
}

func (m *mockTaskManager) Create(_ context.Context, _ core.CreateTaskOptions) (*store.Task, error) {
	return nil, nil
}
func (m *mockTaskManager) Get(_ context.Context, _ string) (*store.Task, error) {
	return nil, nil
}
func (m *mockTaskManager) List(_ context.Context, _ core.ListTasksOptions) (*store.ListResult[store.Task], error) {
	return nil, nil
}
func (m *mockTaskManager) Cancel(_ context.Context, _ string) error  { return nil }
func (m *mockTaskManager) Execute(_ context.Context, _ string) error { return nil }
func (m *mockTaskManager) CreateRun(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, nil
}
func (m *mockTaskManager) OnTaskAccepted(_ context.Context, runID string) error {
	m.onTaskAcceptedCalled = true
	m.lastRunID = runID
	return m.onTaskAcceptedErr
}
func (m *mockTaskManager) OnTaskStarted(_ context.Context, runID string) error {
	m.onTaskStartedCalled = true
	m.lastRunID = runID
	return m.onTaskStartedErr
}
func (m *mockTaskManager) OnTaskProgress(_ context.Context, runID string, progress int) error {
	m.onTaskProgressCalled = true
	m.lastRunID = runID
	m.lastProgress = progress
	return m.onTaskProgressErr
}
func (m *mockTaskManager) OnTaskCompleted(_ context.Context, result *core.TaskCompletedResult) error {
	m.onTaskCompletedCalled = true
	m.lastCompletedRes = result
	return m.onTaskCompletedErr
}
func (m *mockTaskManager) FailRun(_ context.Context, _, _ string) error { return nil }
func (m *mockTaskManager) ShouldRetry(_ context.Context, _ string) (bool, error) {
	return false, nil
}
func (m *mockTaskManager) Retry(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, nil
}
func (m *mockTaskManager) ReExecute(_ context.Context, _ string) error { return nil }

func TestMessageRouter_HandleMessage_Heartbeat(t *testing.T) {
	logger := zap.NewNop()
	rm := &mockRunnerManager{}
	router := NewMessageRouter(logger, rm)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				Status: "idle",
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	assert.True(t, rm.onHeartbeatCalled)
	assert.Equal(t, "idle", rm.lastHeartbeat.GetStatus())
}

func TestMessageRouter_HandleMessage_TaskAccepted(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskAccepted{
			TaskAccepted: &pb.TaskAccepted{
				TaskId: "task_123",
				RunId:  "trun_123",
			},
		},
	}

	// Should not error (stub handler)
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_HandleMessage_TaskStarted(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskStarted{
			TaskStarted: &pb.TaskStarted{
				TaskId: "task_123",
				RunId:  "trun_123",
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_HandleMessage_TaskProgress(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskProgress{
			TaskProgress: &pb.TaskProgress{
				TaskId:          "task_123",
				RunId:           "trun_123",
				ProgressPercent: 50,
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_HandleMessage_TaskCompleted(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskCompleted{
			TaskCompleted: &pb.TaskCompleted{
				TaskId:  "task_123",
				RunId:   "trun_123",
				Success: true,
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_HandleMessage_PermissionRequest(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_PermissionRequest{
			PermissionRequest: &pb.PermissionRequest{
				RequestId: "perm_123",
				Tool:      "bash",
				Action:    "rm -rf /tmp/test",
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_HandleMessage_Nil(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	err := router.HandleMessage(context.Background(), "run_123", nil)
	require.NoError(t, err)
}

func TestMessageRouter_HandleMessage_UnknownType(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	// Empty message with no payload
	msg := &pb.RunnerMessage{}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err) // Should handle gracefully
}

func TestMessageRouter_HandleMessage_SessionAttached(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_SessionAttached{
			SessionAttached: &pb.SessionAttached{
				SessionId: "sess_123",
			},
		},
	}

	// Should not error (stub handler)
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_HandleMessage_SessionSuspended(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_SessionSuspended{
			SessionSuspended: &pb.SessionSuspended{
				SessionId: "sess_123",
			},
		},
	}

	// Should not error (stub handler)
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_HandleHeartbeat_NilManager(t *testing.T) {
	logger := zap.NewNop()
	// Router with nil runnerManager
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_Heartbeat{
			Heartbeat: &pb.Heartbeat{
				Status: "idle",
			},
		},
	}

	// Should not error when runnerManager is nil
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

// =============================================================================
// Task Handler Tests with MockTaskManager
// =============================================================================

func TestMessageRouter_HandleMessage_TaskAccepted_WithTaskManager(t *testing.T) {
	logger := zap.NewNop()
	tm := &mockTaskManager{}
	router := NewMessageRouter(logger, nil, WithMRTaskManager(tm))

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskAccepted{
			TaskAccepted: &pb.TaskAccepted{
				TaskId: "task_123",
				RunId:  "trun_123",
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	assert.True(t, tm.onTaskAcceptedCalled)
	assert.Equal(t, "trun_123", tm.lastRunID)
}

func TestMessageRouter_HandleMessage_TaskAccepted_Error(t *testing.T) {
	logger := zap.NewNop()
	tm := &mockTaskManager{
		onTaskAcceptedErr: errors.New("task not found"),
	}
	router := NewMessageRouter(logger, nil, WithMRTaskManager(tm))

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskAccepted{
			TaskAccepted: &pb.TaskAccepted{
				TaskId: "task_123",
				RunId:  "trun_123",
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "task not found")
}

func TestMessageRouter_HandleMessage_TaskStarted_WithTaskManager(t *testing.T) {
	logger := zap.NewNop()
	rm := &mockRunnerManager{}
	tm := &mockTaskManager{}
	router := NewMessageRouter(logger, rm, WithMRTaskManager(tm))

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskStarted{
			TaskStarted: &pb.TaskStarted{
				TaskId: "task_123",
				RunId:  "trun_123",
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	// TaskManager should be called
	assert.True(t, tm.onTaskStartedCalled)
	assert.Equal(t, "trun_123", tm.lastRunID)

	// RunnerManager.SetStatus should be called with "busy"
	assert.True(t, rm.setStatusCalled)
	assert.Equal(t, "busy", rm.lastStatus)
}

func TestMessageRouter_HandleMessage_TaskStarted_TaskManagerError(t *testing.T) {
	logger := zap.NewNop()
	rm := &mockRunnerManager{}
	tm := &mockTaskManager{
		onTaskStartedErr: errors.New("invalid transition"),
	}
	router := NewMessageRouter(logger, rm, WithMRTaskManager(tm))

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskStarted{
			TaskStarted: &pb.TaskStarted{
				TaskId: "task_123",
				RunId:  "trun_123",
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid transition")

	// SetStatus should NOT be called when TaskManager fails
	assert.False(t, rm.setStatusCalled)
}

func TestMessageRouter_HandleMessage_TaskProgress_WithTaskManager(t *testing.T) {
	logger := zap.NewNop()
	tm := &mockTaskManager{}
	router := NewMessageRouter(logger, nil, WithMRTaskManager(tm))

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskProgress{
			TaskProgress: &pb.TaskProgress{
				TaskId:          "task_123",
				RunId:           "trun_123",
				ProgressPercent: 75,
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	assert.True(t, tm.onTaskProgressCalled)
	assert.Equal(t, "trun_123", tm.lastRunID)
	assert.Equal(t, 75, tm.lastProgress)
}

func TestMessageRouter_HandleMessage_TaskCompleted_WithTaskManager(t *testing.T) {
	logger := zap.NewNop()
	rm := &mockRunnerManager{}
	tm := &mockTaskManager{}
	router := NewMessageRouter(logger, rm, WithMRTaskManager(tm))

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskCompleted{
			TaskCompleted: &pb.TaskCompleted{
				TaskId:       "task_123",
				RunId:        "trun_123",
				Success:      true,
				TokensInput:  100,
				TokensOutput: 200,
				ExitCode:     0,
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	// TaskManager should be called
	assert.True(t, tm.onTaskCompletedCalled)
	require.NotNil(t, tm.lastCompletedRes)
	assert.Equal(t, "trun_123", tm.lastCompletedRes.RunID)
	assert.True(t, tm.lastCompletedRes.Success)
	assert.Equal(t, 100, tm.lastCompletedRes.TokensInput)
	assert.Equal(t, 200, tm.lastCompletedRes.TokensOutput)

	// RunnerManager.SetStatus should be called with "idle"
	assert.True(t, rm.setStatusCalled)
	assert.Equal(t, "idle", rm.lastStatus)
}

func TestMessageRouter_HandleMessage_TaskCompleted_Failed(t *testing.T) {
	logger := zap.NewNop()
	rm := &mockRunnerManager{}
	tm := &mockTaskManager{}
	router := NewMessageRouter(logger, rm, WithMRTaskManager(tm))

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskCompleted{
			TaskCompleted: &pb.TaskCompleted{
				TaskId:   "task_123",
				RunId:    "trun_123",
				Success:  false,
				Error:    "execution failed",
				ExitCode: 1,
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	assert.True(t, tm.onTaskCompletedCalled)
	require.NotNil(t, tm.lastCompletedRes)
	assert.False(t, tm.lastCompletedRes.Success)
	assert.Equal(t, "execution failed", tm.lastCompletedRes.Error)
	require.NotNil(t, tm.lastCompletedRes.ExitCode)
	assert.Equal(t, 1, *tm.lastCompletedRes.ExitCode)

	// Runner should still be set to idle
	assert.True(t, rm.setStatusCalled)
	assert.Equal(t, "idle", rm.lastStatus)
}

func TestMessageRouter_HandleMessage_TaskCompleted_TaskManagerError(t *testing.T) {
	logger := zap.NewNop()
	rm := &mockRunnerManager{}
	tm := &mockTaskManager{
		onTaskCompletedErr: errors.New("failed to update"),
	}
	router := NewMessageRouter(logger, rm, WithMRTaskManager(tm))

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskCompleted{
			TaskCompleted: &pb.TaskCompleted{
				TaskId:  "task_123",
				RunId:   "trun_123",
				Success: true,
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update")

	// SetStatus should NOT be called when TaskManager fails
	assert.False(t, rm.setStatusCalled)
}

func TestMessageRouter_HandleMessage_TaskCompleted_NilTaskManager(t *testing.T) {
	logger := zap.NewNop()
	rm := &mockRunnerManager{}
	// No TaskManager configured
	router := NewMessageRouter(logger, rm)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskCompleted{
			TaskCompleted: &pb.TaskCompleted{
				TaskId:  "task_123",
				RunId:   "trun_123",
				Success: true,
			},
		},
	}

	// Should not error when taskManager is nil (logs warning)
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_HandleMessage_TaskStarted_NilRunnerManager(t *testing.T) {
	logger := zap.NewNop()
	tm := &mockTaskManager{}
	// No RunnerManager configured
	router := NewMessageRouter(logger, nil, WithMRTaskManager(tm))

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TaskStarted{
			TaskStarted: &pb.TaskStarted{
				TaskId: "task_123",
				RunId:  "trun_123",
			},
		},
	}

	// Should not error, just skip SetStatus
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	assert.True(t, tm.onTaskStartedCalled)
}

func TestMessageRouter_WithMRTaskManager(t *testing.T) {
	logger := zap.NewNop()
	tm := &mockTaskManager{}
	router := NewMessageRouter(logger, nil, WithMRTaskManager(tm))

	assert.NotNil(t, router.taskManager)
}

// =============================================================================
// Permission Request Tests with MockPermissionManager
// =============================================================================

// mockPermissionManager implements core.PermissionManagerInterface for testing.
type mockPermissionManager struct {
	createCalled bool
	lastInput    *core.CreatePermissionRequestInput
	createErr    error
}

func (m *mockPermissionManager) Create(_ context.Context, input *core.CreatePermissionRequestInput) (*store.PermissionRequest, error) {
	m.createCalled = true
	m.lastInput = input
	if m.createErr != nil {
		return nil, m.createErr
	}
	return &store.PermissionRequest{
		ID:        "perm_test",
		SessionID: input.SessionID,
		TaskID:    input.TaskID,
		RunID:     input.RunID,
		Tool:      input.Tool,
		Action:    input.Action,
		Status:    "pending",
	}, nil
}

func (m *mockPermissionManager) Respond(_ context.Context, _ string, _ bool, _, _ string) error {
	return nil
}

func (m *mockPermissionManager) Get(_ context.Context, _ string) (*store.PermissionRequest, error) {
	return nil, nil
}

func (m *mockPermissionManager) List(_ context.Context, _ core.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	return nil, nil
}

func (m *mockPermissionManager) Cancel(_ context.Context, _ string) error {
	return nil
}

// mockStoreForRouter implements store.Store for message router tests.
type mockStoreForRouter struct {
	sessions []*store.Session
	tasks    []*store.Task
	taskRuns []*store.TaskRun
}

func (m *mockStoreForRouter) ListSessions(_ context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	items := make([]*store.Session, 0, len(m.sessions))
	for _, sess := range m.sessions {
		if opts.RunnerID != nil && (sess.RunnerID == nil || *sess.RunnerID != *opts.RunnerID) {
			continue
		}
		if len(opts.Status) > 0 {
			found := false
			for _, status := range opts.Status {
				if sess.Status == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		items = append(items, sess)
	}
	return &store.ListResult[store.Session]{Items: items}, nil
}

func (m *mockStoreForRouter) ListTasks(_ context.Context, opts store.ListTasksOptions) (*store.ListResult[store.Task], error) {
	items := make([]*store.Task, 0, len(m.tasks))
	for _, task := range m.tasks {
		if opts.SessionID != nil && task.SessionID != *opts.SessionID {
			continue
		}
		if len(opts.Status) > 0 {
			found := false
			for _, status := range opts.Status {
				if task.Status == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		items = append(items, task)
	}
	return &store.ListResult[store.Task]{Items: items}, nil
}

func (m *mockStoreForRouter) ListTaskRuns(_ context.Context, opts store.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	items := make([]*store.TaskRun, 0, len(m.taskRuns))
	for _, run := range m.taskRuns {
		if opts.TaskID != nil && run.TaskID != *opts.TaskID {
			continue
		}
		if len(opts.Status) > 0 {
			found := false
			for _, status := range opts.Status {
				if run.Status == status {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		items = append(items, run)
	}
	return &store.ListResult[store.TaskRun]{Items: items}, nil
}

// Stub implementations for unused methods
func (m *mockStoreForRouter) CreateRunner(_ context.Context, _ *store.Runner) error { return nil }
func (m *mockStoreForRouter) GetRunner(_ context.Context, _ string) (*store.Runner, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetRunnerByName(_ context.Context, _ string) (*store.Runner, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListRunners(_ context.Context, _ store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	return &store.ListResult[store.Runner]{}, nil
}
func (m *mockStoreForRouter) UpdateRunner(_ context.Context, _ string, _ store.RunnerUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteRunner(_ context.Context, _ string) error { return nil }
func (m *mockStoreForRouter) CreateWorkspace(_ context.Context, _ *store.Workspace) error {
	return nil
}
func (m *mockStoreForRouter) GetWorkspace(_ context.Context, _ string) (*store.Workspace, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListWorkspaces(_ context.Context, _ store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	return &store.ListResult[store.Workspace]{}, nil
}
func (m *mockStoreForRouter) UpdateWorkspace(_ context.Context, _ string, _ store.WorkspaceUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteWorkspace(_ context.Context, _ string) error { return nil }
func (m *mockStoreForRouter) CreateSession(_ context.Context, _ *store.Session) error {
	return nil
}
func (m *mockStoreForRouter) GetSession(_ context.Context, _ string) (*store.Session, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) UpdateSession(_ context.Context, _ string, _ store.SessionUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteSession(_ context.Context, _ string) error   { return nil }
func (m *mockStoreForRouter) CreateTask(_ context.Context, _ *store.Task) error { return nil }
func (m *mockStoreForRouter) GetTask(_ context.Context, id string) (*store.Task, error) {
	for _, task := range m.tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) UpdateTask(_ context.Context, _ string, _ store.TaskUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteTask(_ context.Context, _ string) error { return nil }
func (m *mockStoreForRouter) CreateTaskRun(_ context.Context, _ *store.TaskRun) error {
	return nil
}
func (m *mockStoreForRouter) GetTaskRun(_ context.Context, _ string) (*store.TaskRun, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetTaskRunByTaskAndAttempt(_ context.Context, _ string, _ int) (*store.TaskRun, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) UpdateTaskRun(_ context.Context, _ string, _ store.TaskRunUpdates) error {
	return nil
}
func (m *mockStoreForRouter) CreateScheduledTask(_ context.Context, _ *store.ScheduledTask) error {
	return nil
}
func (m *mockStoreForRouter) GetScheduledTask(_ context.Context, _ string) (*store.ScheduledTask, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListScheduledTasks(_ context.Context, _ store.ListScheduledTasksOptions) (*store.ListResult[store.ScheduledTask], error) {
	return &store.ListResult[store.ScheduledTask]{}, nil
}
func (m *mockStoreForRouter) UpdateScheduledTask(_ context.Context, _ string, _ store.ScheduledTaskUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteScheduledTask(_ context.Context, _ string) error { return nil }
func (m *mockStoreForRouter) CreatePermissionRequest(_ context.Context, _ *store.PermissionRequest) error {
	return nil
}
func (m *mockStoreForRouter) GetPermissionRequest(_ context.Context, _ string) (*store.PermissionRequest, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListPermissionRequests(_ context.Context, _ store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	return &store.ListResult[store.PermissionRequest]{}, nil
}
func (m *mockStoreForRouter) UpdatePermissionRequest(_ context.Context, _ string, _ store.PermissionRequestUpdates) error {
	return nil
}
func (m *mockStoreForRouter) CreateAPIKey(_ context.Context, _ *store.APIKey) error { return nil }
func (m *mockStoreForRouter) GetAPIKey(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetAPIKeyByHash(_ context.Context, _ string) (*store.APIKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListAPIKeys(_ context.Context, _ store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	return &store.ListResult[store.APIKey]{}, nil
}
func (m *mockStoreForRouter) UpdateAPIKey(_ context.Context, _ string, _ store.APIKeyUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteAPIKey(_ context.Context, _ string) error { return nil }
func (m *mockStoreForRouter) CreateRunnerToken(_ context.Context, _ *store.RunnerToken) error {
	return nil
}
func (m *mockStoreForRouter) GetRunnerToken(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetRunnerTokenByHash(_ context.Context, _ string) (*store.RunnerToken, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListRunnerTokens(_ context.Context, _ store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	return &store.ListResult[store.RunnerToken]{}, nil
}
func (m *mockStoreForRouter) UpdateRunnerToken(_ context.Context, _ string, _ store.RunnerTokenUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteRunnerToken(_ context.Context, _ string) error { return nil }
func (m *mockStoreForRouter) CreateAgentConfig(_ context.Context, _ *store.AgentConfig) error {
	return nil
}
func (m *mockStoreForRouter) GetAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetAgentConfigByName(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetDefaultAgentConfig(_ context.Context, _ string) (*store.AgentConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListAgentConfigs(_ context.Context, _ store.ListAgentConfigsOptions) (*store.ListResult[store.AgentConfig], error) {
	return &store.ListResult[store.AgentConfig]{}, nil
}
func (m *mockStoreForRouter) UpdateAgentConfig(_ context.Context, _ string, _ store.AgentConfigUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteAgentConfig(_ context.Context, _ string) error { return nil }
func (m *mockStoreForRouter) CreateProviderConfig(_ context.Context, _ *store.ProviderConfig) error {
	return nil
}
func (m *mockStoreForRouter) GetProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetProviderConfigByName(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetDefaultProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListProviderConfigs(_ context.Context, _ store.ListProviderConfigsOptions) (*store.ListResult[store.ProviderConfig], error) {
	return &store.ListResult[store.ProviderConfig]{}, nil
}
func (m *mockStoreForRouter) UpdateProviderConfig(_ context.Context, _ string, _ store.ProviderConfigUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteProviderConfig(_ context.Context, _ string) error  { return nil }
func (m *mockStoreForRouter) CreateProfile(_ context.Context, _ *store.Profile) error { return nil }
func (m *mockStoreForRouter) GetProfile(_ context.Context, _ string) (*store.Profile, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetProfileByName(_ context.Context, _ string) (*store.Profile, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListProfiles(_ context.Context, _ store.ListProfilesOptions) (*store.ListResult[store.Profile], error) {
	return &store.ListResult[store.Profile]{}, nil
}
func (m *mockStoreForRouter) UpdateProfile(_ context.Context, _ string, _ store.ProfileUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteProfile(_ context.Context, _ string) error { return nil }
func (m *mockStoreForRouter) CreateSnapshot(_ context.Context, _ *store.Snapshot) error {
	return nil
}
func (m *mockStoreForRouter) GetSnapshot(_ context.Context, _ string) (*store.Snapshot, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetSnapshotByRunnerAndName(_ context.Context, _, _ string) (*store.Snapshot, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListSnapshots(_ context.Context, _ store.ListSnapshotsOptions) (*store.ListResult[store.Snapshot], error) {
	return &store.ListResult[store.Snapshot]{}, nil
}
func (m *mockStoreForRouter) UpdateSnapshot(_ context.Context, _ string, _ store.SnapshotUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteSnapshot(_ context.Context, _ string) error      { return nil }
func (m *mockStoreForRouter) CreateTunnel(_ context.Context, _ *store.Tunnel) error { return nil }
func (m *mockStoreForRouter) GetTunnel(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetTunnelByTokenHash(_ context.Context, _ string) (*store.Tunnel, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListTunnels(_ context.Context, _ store.ListTunnelsOptions) (*store.ListResult[store.Tunnel], error) {
	return &store.ListResult[store.Tunnel]{}, nil
}
func (m *mockStoreForRouter) UpdateTunnel(_ context.Context, _ string, _ store.TunnelUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteTunnel(_ context.Context, _ string) error { return nil }
func (m *mockStoreForRouter) CreateActionLog(_ context.Context, _ *store.ActionLog) error {
	return nil
}
func (m *mockStoreForRouter) GetActionLog(_ context.Context, _ string) (*store.ActionLog, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListActionLogs(_ context.Context, _ store.ListActionLogsOptions) (*store.ListResult[store.ActionLog], error) {
	return &store.ListResult[store.ActionLog]{}, nil
}
func (m *mockStoreForRouter) CreateLog(_ context.Context, _ *store.Log) error    { return nil }
func (m *mockStoreForRouter) CreateLogs(_ context.Context, _ []*store.Log) error { return nil }
func (m *mockStoreForRouter) ListLogs(_ context.Context, _ store.ListLogsOptions) (*store.ListResult[store.Log], error) {
	return &store.ListResult[store.Log]{}, nil
}
func (m *mockStoreForRouter) CreateLogArchive(_ context.Context, _ *store.LogArchive) error {
	return nil
}
func (m *mockStoreForRouter) GetLogArchive(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetLogArchiveBySession(_ context.Context, _ string) (*store.LogArchive, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListLogArchives(_ context.Context, _ store.ListLogArchivesOptions) (*store.ListResult[store.LogArchive], error) {
	return &store.ListResult[store.LogArchive]{}, nil
}
func (m *mockStoreForRouter) UpdateLogArchive(_ context.Context, _ string, _ store.LogArchiveUpdates) error {
	return nil
}
func (m *mockStoreForRouter) CreateDataKey(_ context.Context, _ *store.DataKey) error { return nil }
func (m *mockStoreForRouter) GetDataKey(_ context.Context, _ string) (*store.DataKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetDataKeyByResource(_ context.Context, _, _ string) (*store.DataKey, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) UpdateDataKey(_ context.Context, _ string, _ store.DataKeyUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteDataKey(_ context.Context, _ string) error     { return nil }
func (m *mockStoreForRouter) CreateChunk(_ context.Context, _ *store.Chunk) error { return nil }
func (m *mockStoreForRouter) GetChunk(_ context.Context, _, _ string) (*store.Chunk, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) UpdateChunk(_ context.Context, _, _ string, _ store.ChunkUpdates) error {
	return nil
}
func (m *mockStoreForRouter) IncrementChunkRefCount(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockStoreForRouter) DecrementChunkRefCount(_ context.Context, _, _ string) error {
	return nil
}
func (m *mockStoreForRouter) DeleteChunk(_ context.Context, _, _ string) error { return nil }
func (m *mockStoreForRouter) ListUnreferencedChunks(_ context.Context, _ string, _ int) ([]*store.Chunk, error) {
	return nil, nil
}
func (m *mockStoreForRouter) ListSoftDeletedChunks(_ context.Context, _ string, _ time.Time, _ int) ([]*store.Chunk, error) {
	return nil, nil
}
func (m *mockStoreForRouter) MarkChunkDeleted(_ context.Context, _, _ string) error  { return nil }
func (m *mockStoreForRouter) ClearChunkDeleted(_ context.Context, _, _ string) error { return nil }
func (m *mockStoreForRouter) CreateManifest(_ context.Context, _ *store.Manifest) error {
	return nil
}
func (m *mockStoreForRouter) GetManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetLatestManifest(_ context.Context, _ string) (*store.Manifest, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) DeleteManifest(_ context.Context, _ string) error { return nil }

// Stream methods (stub)
func (m *mockStoreForRouter) CreateStream(_ context.Context, _ *store.Stream) error { return nil }
func (m *mockStoreForRouter) GetStream(_ context.Context, _ string) (*store.Stream, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) GetStreamBySessionAndType(_ context.Context, _, _ string, _ bool) (*store.Stream, error) {
	return nil, store.ErrNotFound
}
func (m *mockStoreForRouter) ListStreams(_ context.Context, _ store.ListStreamsOptions) (*store.ListResult[store.Stream], error) {
	return &store.ListResult[store.Stream]{}, nil
}
func (m *mockStoreForRouter) UpdateStream(_ context.Context, _ string, _ store.StreamUpdates) error {
	return nil
}
func (m *mockStoreForRouter) DeleteStream(_ context.Context, _ string) error { return nil }
func (m *mockStoreForRouter) CleanupExpiredStreams(_ context.Context) (int, error) {
	return 0, nil
}

func (m *mockStoreForRouter) BeginTx(_ context.Context) (store.Tx, error)     { return nil, nil }
func (m *mockStoreForRouter) Ping(_ context.Context) error                    { return nil }
func (m *mockStoreForRouter) Close() error                                    { return nil }
func (m *mockStoreForRouter) DeleteTaskRun(_ context.Context, _ string) error { return nil }

func (m *mockStoreForRouter) GetDueScheduledSessions(_ context.Context, _ time.Time, _ int) ([]*store.Session, error) {
	return nil, nil
}

func (m *mockStoreForRouter) GetDueScheduledTasks(_ context.Context, _ time.Time, _ int) ([]*store.ScheduledTask, error) {
	return nil, nil
}

// Webhook methods (stub)
func (m *mockStoreForRouter) CreateWebhook(_ context.Context, _ *store.Webhook) error {
	panic("not implemented")
}
func (m *mockStoreForRouter) GetWebhook(_ context.Context, _ string) (*store.Webhook, error) {
	panic("not implemented")
}
func (m *mockStoreForRouter) GetWebhookByName(_ context.Context, _ string, _ *string) (*store.Webhook, error) {
	panic("not implemented")
}
func (m *mockStoreForRouter) ListWebhooks(_ context.Context, _ store.ListWebhooksOptions) (*store.ListResult[store.Webhook], error) {
	panic("not implemented")
}
func (m *mockStoreForRouter) UpdateWebhook(_ context.Context, _ string, _ store.WebhookUpdates) error {
	panic("not implemented")
}
func (m *mockStoreForRouter) DeleteWebhook(_ context.Context, _ string) error {
	panic("not implemented")
}
func (m *mockStoreForRouter) GetActiveWebhooksForEvent(_ context.Context, _ string, _ *string) ([]*store.Webhook, error) {
	panic("not implemented")
}

// WebhookEvent methods (stub)
func (m *mockStoreForRouter) CreateWebhookEvent(_ context.Context, _ *store.WebhookEvent) error {
	panic("not implemented")
}
func (m *mockStoreForRouter) GetWebhookEvent(_ context.Context, _ string) (*store.WebhookEvent, error) {
	panic("not implemented")
}
func (m *mockStoreForRouter) ListWebhookEvents(_ context.Context, _ store.ListWebhookEventsOptions) (*store.ListResult[store.WebhookEvent], error) {
	panic("not implemented")
}
func (m *mockStoreForRouter) UpdateWebhookEvent(_ context.Context, _ string, _ store.WebhookEventUpdates) error {
	panic("not implemented")
}
func (m *mockStoreForRouter) GetPendingWebhookEvents(_ context.Context, _ int) ([]*store.WebhookEvent, error) {
	panic("not implemented")
}
func (m *mockStoreForRouter) CancelWebhookEventsByWebhook(_ context.Context, _ string) error {
	panic("not implemented")
}

func TestMessageRouter_HandleMessage_PermissionRequest_WithManager(t *testing.T) {
	logger := zap.NewNop()
	pm := &mockPermissionManager{}
	runnerID := "run_123"

	mockStore := &mockStoreForRouter{
		sessions: []*store.Session{
			{
				ID:       "sess_123",
				Status:   "active",
				RunnerID: &runnerID,
			},
		},
		tasks: []*store.Task{
			{
				ID:        "task_123",
				SessionID: "sess_123",
				Status:    "running",
			},
		},
		taskRuns: []*store.TaskRun{
			{
				ID:     "trun_123",
				TaskID: "task_123",
				Status: "running",
			},
		},
	}

	router := NewMessageRouter(logger, nil,
		WithMRPermissionManager(pm),
		WithMRStore(mockStore),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_PermissionRequest{
			PermissionRequest: &pb.PermissionRequest{
				RequestId:           "perm_123",
				TaskId:              "task_123",
				RunId:               "trun_123",
				Tool:                "bash",
				Action:              "rm -rf /tmp/test",
				RiskLevel:           "high",
				SuspendAfterSeconds: 1800,
			},
		},
	}

	err := router.HandleMessage(context.Background(), runnerID, msg)
	require.NoError(t, err)

	// Verify permission manager was called
	assert.True(t, pm.createCalled)
	require.NotNil(t, pm.lastInput)
	assert.Equal(t, "sess_123", pm.lastInput.SessionID)
	assert.Equal(t, "task_123", pm.lastInput.TaskID)
	assert.Equal(t, "trun_123", pm.lastInput.RunID)
	assert.Equal(t, "bash", pm.lastInput.Tool)
	assert.Equal(t, "rm -rf /tmp/test", pm.lastInput.Action)
	assert.Equal(t, "high", pm.lastInput.RiskLevel)
	assert.Equal(t, 1800, pm.lastInput.SuspendAfterSeconds)
}

func TestMessageRouter_HandleMessage_PermissionRequest_NoSession(t *testing.T) {
	logger := zap.NewNop()
	pm := &mockPermissionManager{}

	// Empty store - no sessions
	mockStore := &mockStoreForRouter{}

	router := NewMessageRouter(logger, nil,
		WithMRPermissionManager(pm),
		WithMRStore(mockStore),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_PermissionRequest{
			PermissionRequest: &pb.PermissionRequest{
				RequestId: "perm_123",
				TaskId:    "task_123",
				RunId:     "trun_123",
				Tool:      "bash",
				Action:    "echo hello",
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active session found")

	// Permission manager should not have been called
	assert.False(t, pm.createCalled)
}

func TestMessageRouter_HandleMessage_PermissionRequest_NoTask(t *testing.T) {
	logger := zap.NewNop()
	pm := &mockPermissionManager{}
	runnerID := "run_123"

	// Store with session but no tasks
	mockStore := &mockStoreForRouter{
		sessions: []*store.Session{
			{
				ID:       "sess_123",
				Status:   "active",
				RunnerID: &runnerID,
			},
		},
	}

	router := NewMessageRouter(logger, nil,
		WithMRPermissionManager(pm),
		WithMRStore(mockStore),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_PermissionRequest{
			PermissionRequest: &pb.PermissionRequest{
				RequestId: "perm_123",
				TaskId:    "task_nonexistent",
				RunId:     "trun_123",
				Tool:      "bash",
				Action:    "echo hello",
			},
		},
	}

	err := router.HandleMessage(context.Background(), runnerID, msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")

	// Permission manager should not have been called
	assert.False(t, pm.createCalled)
}

func TestMessageRouter_HandleMessage_PermissionRequest_CreateError(t *testing.T) {
	logger := zap.NewNop()
	pm := &mockPermissionManager{
		createErr: errors.New("database error"),
	}
	runnerID := "run_123"

	mockStore := &mockStoreForRouter{
		sessions: []*store.Session{
			{
				ID:       "sess_123",
				Status:   "active",
				RunnerID: &runnerID,
			},
		},
		tasks: []*store.Task{
			{
				ID:        "task_123",
				SessionID: "sess_123",
				Status:    "running",
			},
		},
		taskRuns: []*store.TaskRun{
			{
				ID:     "trun_123",
				TaskID: "task_123",
				Status: "running",
			},
		},
	}

	router := NewMessageRouter(logger, nil,
		WithMRPermissionManager(pm),
		WithMRStore(mockStore),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_PermissionRequest{
			PermissionRequest: &pb.PermissionRequest{
				RequestId: "perm_123",
				TaskId:    "task_123",
				RunId:     "trun_123",
				Tool:      "bash",
				Action:    "echo hello",
			},
		},
	}

	err := router.HandleMessage(context.Background(), runnerID, msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "database error")
}

func TestMessageRouter_HandleMessage_PermissionRequest_TaskNotInSession(t *testing.T) {
	logger := zap.NewNop()
	pm := &mockPermissionManager{}
	runnerID := "run_123"

	// Task belongs to a different session
	mockStore := &mockStoreForRouter{
		sessions: []*store.Session{
			{
				ID:       "sess_123",
				Status:   "active",
				RunnerID: &runnerID,
			},
		},
		tasks: []*store.Task{
			{
				ID:        "task_123",
				SessionID: "sess_different", // Different session
				Status:    "running",
			},
		},
	}

	router := NewMessageRouter(logger, nil,
		WithMRPermissionManager(pm),
		WithMRStore(mockStore),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_PermissionRequest{
			PermissionRequest: &pb.PermissionRequest{
				RequestId: "perm_123",
				TaskId:    "task_123",
				RunId:     "trun_123",
				Tool:      "bash",
				Action:    "echo hello",
			},
		},
	}

	err := router.HandleMessage(context.Background(), runnerID, msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not belong to session")

	// Permission manager should not have been called
	assert.False(t, pm.createCalled)
}

func TestMessageRouter_HandleMessage_PermissionRequest_MissingTaskOrRunID(t *testing.T) {
	logger := zap.NewNop()
	pm := &mockPermissionManager{}
	runnerID := "run_123"

	mockStore := &mockStoreForRouter{
		sessions: []*store.Session{
			{
				ID:       "sess_123",
				Status:   "active",
				RunnerID: &runnerID,
			},
		},
	}

	router := NewMessageRouter(logger, nil,
		WithMRPermissionManager(pm),
		WithMRStore(mockStore),
	)

	// Missing both task_id and run_id
	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_PermissionRequest{
			PermissionRequest: &pb.PermissionRequest{
				RequestId: "perm_123",
				Tool:      "bash",
				Action:    "echo hello",
			},
		},
	}

	err := router.HandleMessage(context.Background(), runnerID, msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing task_id or run_id")

	// Permission manager should not have been called
	assert.False(t, pm.createCalled)
}

// =============================================================================
// Tunnel Handler Tests
// =============================================================================

// mockTunnelHandler implements TunnelHandlerInterface for testing.
type mockTunnelHandler struct {
	handleCalled bool
	lastReq      *pb.CreateTunnelRequest
	lastRunnerID string
	resp         *pb.CreateTunnelResponse
	err          error
}

func (m *mockTunnelHandler) HandleCreateTunnelRequest(_ context.Context, runnerID string, req *pb.CreateTunnelRequest) (*pb.CreateTunnelResponse, error) {
	m.handleCalled = true
	m.lastRunnerID = runnerID
	m.lastReq = req
	if m.err != nil {
		return nil, m.err
	}
	if m.resp != nil {
		return m.resp, nil
	}
	return &pb.CreateTunnelResponse{
		RequestId: req.GetRequestId(),
		Success:   true,
		TunnelId:  "tun_test123",
		Token:     "ttok_test",
		PublicUrl: "http://localhost:8080/tunnels/tun_test123",
	}, nil
}

// mockConnectionManager implements a minimal mock for sending commands.
type mockConnectionManager struct {
	sendCalled   bool
	lastRunnerID string
	lastCmd      *pb.ServerCommand
	sendErr      error
}

func (m *mockConnectionManager) SendCommand(runnerID string, cmd *pb.ServerCommand) error {
	m.sendCalled = true
	m.lastRunnerID = runnerID
	m.lastCmd = cmd
	return m.sendErr
}

func TestMessageRouter_HandleMessage_CreateTunnelRequest(t *testing.T) {
	logger := zap.NewNop()
	th := &mockTunnelHandler{}
	cm := &mockConnectionManager{}
	router := NewMessageRouter(logger, nil,
		WithMRTunnelHandler(th),
		WithMRConnectionManager(cm),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_CreateTunnelRequest{
			CreateTunnelRequest: &pb.CreateTunnelRequest{
				RequestId: "req_123",
				SessionId: "sess_123",
				Type:      "http",
				LocalPort: 8000,
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	// Verify tunnel handler was called
	assert.True(t, th.handleCalled)
	assert.Equal(t, "run_123", th.lastRunnerID)
	assert.Equal(t, "req_123", th.lastReq.GetRequestId())
	assert.Equal(t, "sess_123", th.lastReq.GetSessionId())
	assert.Equal(t, "http", th.lastReq.GetType())
	assert.Equal(t, int32(8000), th.lastReq.GetLocalPort())

	// Verify response was sent
	assert.True(t, cm.sendCalled)
	assert.Equal(t, "run_123", cm.lastRunnerID)
	require.NotNil(t, cm.lastCmd)
	resp := cm.lastCmd.GetCreateTunnelResponse()
	require.NotNil(t, resp)
	assert.Equal(t, "req_123", resp.GetRequestId())
	assert.True(t, resp.GetSuccess())
	assert.Equal(t, "tun_test123", resp.GetTunnelId())
}

func TestMessageRouter_HandleMessage_CreateTunnelRequest_NoHandler(t *testing.T) {
	logger := zap.NewNop()
	cm := &mockConnectionManager{}
	// No tunnel handler configured
	router := NewMessageRouter(logger, nil,
		WithMRConnectionManager(cm),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_CreateTunnelRequest{
			CreateTunnelRequest: &pb.CreateTunnelRequest{
				RequestId: "req_123",
				SessionId: "sess_123",
				Type:      "http",
				LocalPort: 8000,
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	// Verify error response was sent
	assert.True(t, cm.sendCalled)
	resp := cm.lastCmd.GetCreateTunnelResponse()
	require.NotNil(t, resp)
	assert.Equal(t, "req_123", resp.GetRequestId())
	assert.False(t, resp.GetSuccess())
	assert.Contains(t, resp.GetError(), "tunnel handler not configured")
}

func TestMessageRouter_HandleMessage_CreateTunnelRequest_HandlerError(t *testing.T) {
	logger := zap.NewNop()
	th := &mockTunnelHandler{
		err: errors.New("internal error"),
	}
	cm := &mockConnectionManager{}
	router := NewMessageRouter(logger, nil,
		WithMRTunnelHandler(th),
		WithMRConnectionManager(cm),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_CreateTunnelRequest{
			CreateTunnelRequest: &pb.CreateTunnelRequest{
				RequestId: "req_123",
				SessionId: "sess_123",
				Type:      "http",
				LocalPort: 8000,
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	// Verify error response was sent
	assert.True(t, cm.sendCalled)
	resp := cm.lastCmd.GetCreateTunnelResponse()
	require.NotNil(t, resp)
	assert.Equal(t, "req_123", resp.GetRequestId())
	assert.False(t, resp.GetSuccess())
	assert.Contains(t, resp.GetError(), "internal error")
}

func TestMessageRouter_HandleMessage_CreateTunnelRequest_NoConnectionManager(t *testing.T) {
	logger := zap.NewNop()
	th := &mockTunnelHandler{}
	// No connection manager configured
	router := NewMessageRouter(logger, nil,
		WithMRTunnelHandler(th),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_CreateTunnelRequest{
			CreateTunnelRequest: &pb.CreateTunnelRequest{
				RequestId: "req_123",
				SessionId: "sess_123",
				Type:      "http",
				LocalPort: 8000,
			},
		},
	}

	// Should not error (logs warning but continues)
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	// Handler should still have been called
	assert.True(t, th.handleCalled)
}

func TestMessageRouter_HandleMessage_TunnelData(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TunnelData{
			TunnelData: &pb.TunnelData{
				TunnelId:     "tun_123",
				ConnectionId: "conn_456",
				Data:         []byte("test data"),
				Eof:          false,
			},
		},
	}

	// Should not error (stub implementation)
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_HandleMessage_CloseTunnel(t *testing.T) {
	logger := zap.NewNop()
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_CloseTunnel{
			CloseTunnel: &pb.CloseTunnel{
				TunnelId: "tun_123",
				Reason:   "user requested",
			},
		},
	}

	// Should not error (stub implementation)
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_WithMRTunnelHandler(t *testing.T) {
	logger := zap.NewNop()
	th := &mockTunnelHandler{}
	router := NewMessageRouter(logger, nil, WithMRTunnelHandler(th))

	assert.NotNil(t, router.tunnelHandler)
}

func TestMessageRouter_WithMRConnectionManager(t *testing.T) {
	logger := zap.NewNop()
	cm := &ConnectionManager{}
	router := NewMessageRouter(logger, nil, WithMRConnectionManager(cm))

	assert.NotNil(t, router.connManager)
}

func TestMessageRouter_HandleMessage_CreateTunnelRequest_SendError(t *testing.T) {
	logger := zap.NewNop()
	th := &mockTunnelHandler{}
	cm := &mockConnectionManager{
		sendErr: errors.New("connection failed"),
	}
	router := NewMessageRouter(logger, nil,
		WithMRTunnelHandler(th),
		WithMRConnectionManager(cm),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_CreateTunnelRequest{
			CreateTunnelRequest: &pb.CreateTunnelRequest{
				RequestId: "req_123",
				SessionId: "sess_123",
				Type:      "http",
				LocalPort: 8000,
			},
		},
	}

	// Should return the send error
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")

	// Handler should have been called
	assert.True(t, th.handleCalled)

	// Connection manager should have been called
	assert.True(t, cm.sendCalled)
}

// =============================================================================
// Context Update Tests
// =============================================================================

// mockSessionManager implements core.SessionManagerInterface for testing.
type mockSessionManager struct {
	updateContextCalled bool
	lastSessionID       string
	lastContextSnapshot *core.ContextSnapshot
	updateContextErr    error
}

func (m *mockSessionManager) Create(_ context.Context, _ core.CreateSessionOptions) (*store.Session, error) {
	return nil, nil
}

func (m *mockSessionManager) Get(_ context.Context, _ string) (*store.Session, error) {
	return nil, nil
}

func (m *mockSessionManager) List(_ context.Context, _ store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return nil, nil
}

func (m *mockSessionManager) Activate(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionManager) Suspend(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionManager) Resume(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionManager) Terminate(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionManager) AttachRunner(_ context.Context, _, _ string) error {
	return nil
}

func (m *mockSessionManager) DetachRunner(_ context.Context, _ string) error {
	return nil
}

func (m *mockSessionManager) UpdateContextSnapshot(_ context.Context, sessionID string, snapshot *core.ContextSnapshot) error {
	m.updateContextCalled = true
	m.lastSessionID = sessionID
	m.lastContextSnapshot = snapshot
	return m.updateContextErr
}

func TestMessageRouter_HandleMessage_ContextUpdate(t *testing.T) {
	logger := zap.NewNop()
	sm := &mockSessionManager{}
	router := NewMessageRouter(logger, nil,
		WithMRSessionManager(sm),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_ContextUpdate{
			ContextUpdate: &pb.ContextUpdate{
				SessionId:       "sess_123",
				TaskId:          "task_456",
				ContextSnapshot: []byte(`{"conversation_id": "conv_789"}`),
			},
		},
	}

	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	// Verify session manager was called
	assert.True(t, sm.updateContextCalled)
	assert.Equal(t, "sess_123", sm.lastSessionID)
	assert.NotNil(t, sm.lastContextSnapshot)
	assert.Equal(t, "conv_789", sm.lastContextSnapshot.ConversationID)
}

func TestMessageRouter_HandleMessage_ContextUpdate_NoSessionManager(t *testing.T) {
	logger := zap.NewNop()
	// No session manager configured
	router := NewMessageRouter(logger, nil)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_ContextUpdate{
			ContextUpdate: &pb.ContextUpdate{
				SessionId:       "sess_123",
				TaskId:          "task_456",
				ContextSnapshot: []byte(`{"conversation_id": "conv_789"}`),
			},
		},
	}

	// Should not error even without session manager
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)
}

func TestMessageRouter_HandleMessage_ContextUpdate_InvalidJSON(t *testing.T) {
	logger := zap.NewNop()
	sm := &mockSessionManager{}
	router := NewMessageRouter(logger, nil,
		WithMRSessionManager(sm),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_ContextUpdate{
			ContextUpdate: &pb.ContextUpdate{
				SessionId:       "sess_123",
				TaskId:          "task_456",
				ContextSnapshot: []byte(`{invalid json`),
			},
		},
	}

	// Should not error, just log warning
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	// Session manager should not have been called
	assert.False(t, sm.updateContextCalled)
}

func TestMessageRouter_HandleMessage_ContextUpdate_UpdateError(t *testing.T) {
	logger := zap.NewNop()
	sm := &mockSessionManager{
		updateContextErr: errors.New("update failed"),
	}
	router := NewMessageRouter(logger, nil,
		WithMRSessionManager(sm),
	)

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_ContextUpdate{
			ContextUpdate: &pb.ContextUpdate{
				SessionId:       "sess_123",
				TaskId:          "task_456",
				ContextSnapshot: []byte(`{"conversation_id": "conv_789"}`),
			},
		},
	}

	// Should not error (logs warning but continues)
	err := router.HandleMessage(context.Background(), "run_123", msg)
	require.NoError(t, err)

	// Session manager should have been called
	assert.True(t, sm.updateContextCalled)
}

func TestMessageRouter_WithMRSessionManager(t *testing.T) {
	logger := zap.NewNop()
	sm := &mockSessionManager{}
	router := NewMessageRouter(logger, nil, WithMRSessionManager(sm))

	assert.NotNil(t, router.sessionManager)
}

// EnsureRunner satisfies core.SessionManagerInterface. This fake never allocates.
func (m *mockSessionManager) EnsureRunner(_ context.Context, _ string) (*store.Session, error) {
	return nil, core.ErrNoRunnerAvailable
}

// DispatchNext satisfies TaskManagerInterface. These fakes dispatch nothing.
func (m *mockTaskManager) DispatchNext(_ context.Context, _ string) error { return nil }

// ListRuns satisfies TaskManagerInterface. These fakes keep no run history.
func (m *mockTaskManager) ListRuns(_ context.Context, _ string, _ core.ListTaskRunsOptions) (*store.ListResult[store.TaskRun], error) {
	return &store.ListResult[store.TaskRun]{}, nil
}

// DispatchNextNow satisfies TaskManagerInterface. These fakes dispatch nothing.
func (m *mockTaskManager) DispatchNextNow(_ context.Context, _ string) error { return nil }
