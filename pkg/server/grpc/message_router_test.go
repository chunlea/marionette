package grpc

import (
	"context"
	"testing"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockRunnerManager implements RunnerManagerInterface for testing.
type mockRunnerManager struct {
	onConnectCalled    bool
	onDisconnectCalled bool
	onHeartbeatCalled  bool
	lastHeartbeat      *pb.Heartbeat
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
