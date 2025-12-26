package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

func TestExtractRunnerID(t *testing.T) {
	logger := zap.NewNop()
	svc := NewRunnerService(logger)

	tests := []struct {
		name      string
		ctx       context.Context
		wantID    string
		wantError bool
	}{
		{
			name: "valid runner ID",
			ctx: metadata.NewIncomingContext(
				context.Background(),
				metadata.Pairs("x-runner-id", "run_123"),
			),
			wantID:    "run_123",
			wantError: false,
		},
		{
			name:      "missing metadata",
			ctx:       context.Background(),
			wantID:    "",
			wantError: true,
		},
		{
			name: "missing x-runner-id",
			ctx: metadata.NewIncomingContext(
				context.Background(),
				metadata.Pairs("other-key", "value"),
			),
			wantID:    "",
			wantError: true,
		},
		{
			name: "multiple runner IDs (returns first)",
			ctx: metadata.NewIncomingContext(
				context.Background(),
				metadata.Pairs("x-runner-id", "run_first", "x-runner-id", "run_second"),
			),
			wantID:    "run_first",
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := svc.extractRunnerID(tt.ctx)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantID, id)
			}
		})
	}
}

func TestExtractRunnerToken(t *testing.T) {
	logger := zap.NewNop()
	svc := NewRunnerService(logger)

	tests := []struct {
		name      string
		ctx       context.Context
		wantToken string
		wantError bool
	}{
		{
			name: "valid token",
			ctx: metadata.NewIncomingContext(
				context.Background(),
				metadata.Pairs("x-runner-token", "rtok_abc123"),
			),
			wantToken: "rtok_abc123",
			wantError: false,
		},
		{
			name:      "missing metadata",
			ctx:       context.Background(),
			wantToken: "",
			wantError: true,
		},
		{
			name: "missing x-runner-token",
			ctx: metadata.NewIncomingContext(
				context.Background(),
				metadata.Pairs("x-runner-id", "run_123"),
			),
			wantToken: "",
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			token, err := svc.extractRunnerToken(tt.ctx)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantToken, token)
			}
		})
	}
}

func TestValidateRunnerAuth_NoTokenService(t *testing.T) {
	logger := zap.NewNop()
	svc := NewRunnerService(logger)
	// tokenSvc is nil

	ctx := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("x-runner-token", "rtok_abc123"),
	)

	// Should succeed when tokenSvc is nil (skip validation)
	err := svc.validateRunnerAuth(ctx, "run_123")
	require.NoError(t, err)
}

func TestConnectionManager_SendCommand(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	// Test error case when runner is not found
	err := cm.SendCommand("run_nonexistent", nil)
	assert.ErrorIs(t, err, ErrRunnerNotFound)
}

func TestConnectionManager_RegisterAndIsConnected(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	// Initially not connected
	assert.False(t, cm.IsConnected("run_123"))

	// Register a connection
	conn := &RunnerConnection{
		RunnerID: "run_123",
		Name:     "test-runner",
	}
	err := cm.Register("run_123", conn)
	require.NoError(t, err)

	// Now connected
	assert.True(t, cm.IsConnected("run_123"))

	// Unregister
	cm.Unregister("run_123")

	// No longer connected
	assert.False(t, cm.IsConnected("run_123"))
}
