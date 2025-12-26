package grpc

import (
	"context"
	"testing"

	pb "github.com/chunlea/marionette/gen/proto/v1"
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

func TestRunnerService_GetRunnerStatus(t *testing.T) {
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

func TestNewRunnerService_WithOptions(t *testing.T) {
	logger := zap.NewNop()
	connMgr := NewConnectionManager(logger)

	svc := NewRunnerService(logger,
		WithConnectionManager(connMgr),
	)

	require.NotNil(t, svc)
	assert.Equal(t, connMgr, svc.connManager)
}
