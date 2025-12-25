package grpc

import (
	"context"
	"testing"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRunnerService_RegisterRunner(t *testing.T) {
	logger := zap.NewNop()
	svc := &runnerService{logger: logger}

	req := &pb.RegisterRunnerRequest{
		Name:     "test-runner",
		Hostname: "localhost",
		Token:    "test-token",
	}

	resp, err := svc.RegisterRunner(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.True(t, resp.Accepted)
	assert.Equal(t, "run_stub", resp.RunnerId)
	assert.Contains(t, resp.Message, "stub")
}

func TestRunnerService_GetRunnerStatus(t *testing.T) {
	logger := zap.NewNop()
	svc := &runnerService{logger: logger}

	req := &pb.GetRunnerStatusRequest{
		RunnerId: "run_123",
	}

	resp, err := svc.GetRunnerStatus(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "run_123", resp.RunnerId)
	assert.Equal(t, "unknown", resp.Status)
}
