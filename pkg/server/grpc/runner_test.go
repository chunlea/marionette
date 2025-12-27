package grpc

import (
	"context"
	"testing"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRunnerService_RegisterRunner_NoRegistry(t *testing.T) {
	// Test that RegisterRunner fails gracefully when registry is not configured
	logger := zap.NewNop()
	svc := NewRunnerService(logger)

	req := &pb.RegisterRunnerRequest{
		Name:     "test-runner",
		Hostname: "localhost",
		Token:    "test-token",
	}

	resp, err := svc.RegisterRunner(context.Background(), req)
	require.Error(t, err)
	require.NotNil(t, resp)

	assert.False(t, resp.Accepted)
	assert.Contains(t, resp.Message, "registry not configured")
}

func TestRunnerService_GetRunnerStatus_NoConfig(t *testing.T) {
	logger := zap.NewNop()
	svc := NewRunnerService(logger)

	req := &pb.GetRunnerStatusRequest{
		RunnerId: "run_123",
	}

	resp, err := svc.GetRunnerStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "run_123", resp.RunnerId)
	assert.Equal(t, "unknown", resp.Status)
}

func TestRunnerService_GetRunnerStatus_ConnectedRunner(t *testing.T) {
	logger := zap.NewNop()
	connMgr := NewConnectionManager(logger)

	// Add a mock connection
	conn := &RunnerConnection{
		RunnerID: "run_123",
		Name:     "test-runner",
		Status:   RunnerStatusIdle,
	}
	connMgr.mu.Lock()
	connMgr.connections["run_123"] = conn
	connMgr.mu.Unlock()

	svc := NewRunnerService(logger, WithConnectionManager(connMgr))

	req := &pb.GetRunnerStatusRequest{
		RunnerId: "run_123",
	}

	resp, err := svc.GetRunnerStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "run_123", resp.RunnerId)
	assert.Equal(t, "idle", resp.Status)
}

func TestRunnerService_GetRunnerStatus_ConnectedRunnerBusy(t *testing.T) {
	logger := zap.NewNop()
	connMgr := NewConnectionManager(logger)

	// Add a busy connection
	conn := &RunnerConnection{
		RunnerID: "run_456",
		Name:     "busy-runner",
		Status:   RunnerStatusBusy,
	}
	connMgr.mu.Lock()
	connMgr.connections["run_456"] = conn
	connMgr.mu.Unlock()

	svc := NewRunnerService(logger, WithConnectionManager(connMgr))

	req := &pb.GetRunnerStatusRequest{
		RunnerId: "run_456",
	}

	resp, err := svc.GetRunnerStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "run_456", resp.RunnerId)
	assert.Equal(t, "busy", resp.Status)
}

func TestRunnerService_GetRunnerStatus_NotConnected_WithStore(t *testing.T) {
	logger := zap.NewNop()
	connMgr := NewConnectionManager(logger)
	testStore := newIntegrationTestStore()

	// Create a runner in the store (offline)
	runner := &store.Runner{
		ID:       "run_789",
		Name:     "offline-runner",
		Hostname: "localhost",
		Status:   "offline",
	}
	err := testStore.CreateRunner(context.Background(), runner)
	require.NoError(t, err)

	svc := NewRunnerService(logger,
		WithConnectionManager(connMgr),
		WithStore(testStore),
	)

	req := &pb.GetRunnerStatusRequest{
		RunnerId: "run_789",
	}

	resp, err := svc.GetRunnerStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "run_789", resp.RunnerId)
	assert.Equal(t, "offline", resp.Status)
}

func TestRunnerService_GetRunnerStatus_NotFound(t *testing.T) {
	logger := zap.NewNop()
	connMgr := NewConnectionManager(logger)
	testStore := newIntegrationTestStore()

	svc := NewRunnerService(logger,
		WithConnectionManager(connMgr),
		WithStore(testStore),
	)

	req := &pb.GetRunnerStatusRequest{
		RunnerId: "run_nonexistent",
	}

	resp, err := svc.GetRunnerStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "run_nonexistent", resp.RunnerId)
	assert.Equal(t, "unknown", resp.Status)
}

func TestNewRunnerService_WithOptions(t *testing.T) {
	logger := zap.NewNop()
	connMgr := NewConnectionManager(logger)

	svc := NewRunnerService(logger,
		WithConnectionManager(connMgr),
	)

	require.NotNil(t, svc)
	assert.Equal(t, connMgr, svc.connManager)
}
