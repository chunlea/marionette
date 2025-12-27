package grpc

import (
	"context"
	"errors"
	"testing"

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
