package grpc

import (
	"context"
	"errors"
	"sync"
	"testing"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/auth"
	mockstore "github.com/chunlea/marionette/pkg/store/mock"
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

func TestValidateRunnerAuth_WithTokenService(t *testing.T) {
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a token
	token, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	// Bind token to a runner
	err = tokenSvc.BindRunner(ctx, token.ID, "run_123")
	require.NoError(t, err)

	svc := NewRunnerService(logger, WithTokenService(tokenSvc))

	// Test with valid token and matching runner ID
	ctxWithToken := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("x-runner-token", plaintext),
	)
	err = svc.validateRunnerAuth(ctxWithToken, "run_123")
	require.NoError(t, err)
}

func TestValidateRunnerAuth_InvalidToken(t *testing.T) {
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	svc := NewRunnerService(logger, WithTokenService(tokenSvc))

	// Test with invalid token
	ctxWithToken := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("x-runner-token", "invalid_token"),
	)
	err := svc.validateRunnerAuth(ctxWithToken, "run_123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid token")
}

func TestValidateRunnerAuth_TokenBoundToDifferentRunner(t *testing.T) {
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create a token and bind to a different runner
	token, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	err = tokenSvc.BindRunner(ctx, token.ID, "run_other")
	require.NoError(t, err)

	svc := NewRunnerService(logger, WithTokenService(tokenSvc))

	// Test with token bound to different runner
	ctxWithToken := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("x-runner-token", plaintext),
	)
	err = svc.validateRunnerAuth(ctxWithToken, "run_123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bound to different runner")
}

func TestValidateRunnerAuth_UnboundToken(t *testing.T) {
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	ctx := context.Background()

	// Create an unbound token
	_, plaintext, err := tokenSvc.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: "test-pool",
	})
	require.NoError(t, err)

	svc := NewRunnerService(logger, WithTokenService(tokenSvc))

	// Test with unbound token (should work for any runner)
	ctxWithToken := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("x-runner-token", plaintext),
	)
	err = svc.validateRunnerAuth(ctxWithToken, "run_123")
	require.NoError(t, err)
}

func TestValidateRunnerAuth_MissingToken(t *testing.T) {
	tokenStore := mockstore.NewRunnerTokenStore()
	tokenSvc := auth.NewRunnerTokenService(tokenStore, func() string { return "rtok_test1" })
	logger := zap.NewNop()

	svc := NewRunnerService(logger, WithTokenService(tokenSvc))

	// Test with no token in metadata
	ctxWithoutToken := metadata.NewIncomingContext(
		context.Background(),
		metadata.Pairs("x-runner-id", "run_123"),
	)
	err := svc.validateRunnerAuth(ctxWithoutToken, "run_123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "x-runner-token")
}

func TestConnectionManager_SendCommand_Success(t *testing.T) {
	logger := zap.NewNop()
	cm := NewConnectionManager(logger)

	// Create a connection with a command channel
	commandCh := make(chan *pb.ServerCommand, 10)
	conn := &RunnerConnection{
		RunnerID:  "run_123",
		Name:      "test-runner",
		commandCh: commandCh,
	}
	err := cm.Register("run_123", conn)
	require.NoError(t, err)

	// Send a command (using KillTask as an example command)
	cmd := &pb.ServerCommand{
		Payload: &pb.ServerCommand_KillTask{
			KillTask: &pb.KillTask{
				TaskId: "task_123",
			},
		},
	}
	err = cm.SendCommand("run_123", cmd)
	require.NoError(t, err)

	// Verify command was sent to channel
	select {
	case received := <-commandCh:
		assert.NotNil(t, received.GetKillTask())
		assert.Equal(t, "task_123", received.GetKillTask().GetTaskId())
	default:
		t.Fatal("expected command in channel")
	}
}

// TestSendCommandVsDisconnect_NoPanic is the regression test for the
// send-on-closed-channel panic. handleDisconnect used to close commandCh while
// SendCommand had already released the connection map's read lock but had not
// yet published its command; losing that race killed the whole process.
// Run with -race.
func TestSendCommandVsDisconnect_NoPanic(t *testing.T) {
	const (
		rounds          = 50
		senders         = 8
		sendsPerRoutine = 100
	)

	logger := zap.NewNop()

	for round := 0; round < rounds; round++ {
		cm := NewConnectionManager(logger)
		svc := NewRunnerService(logger, WithConnectionManager(cm))

		conn := newRunnerConnection("run_race", "race-runner", "localhost", nil)
		require.NoError(t, cm.Register("run_race", conn))

		// Drain queued commands so senders keep hitting the publish path
		// instead of bouncing off a full buffer.
		drained := make(chan struct{})
		go func() {
			defer close(drained)
			for {
				select {
				case <-conn.Done():
					return
				case <-conn.commandCh:
				}
			}
		}()

		var wg sync.WaitGroup
		start := make(chan struct{})

		for i := 0; i < senders; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for j := 0; j < sendsPerRoutine; j++ {
					err := cm.SendCommand("run_race", &pb.ServerCommand{
						Payload: &pb.ServerCommand_KillTask{
							KillTask: &pb.KillTask{TaskId: "task_race"},
						},
					})
					switch {
					case err == nil,
						errors.Is(err, ErrRunnerNotFound),
						errors.Is(err, ErrRunnerDisconnected),
						errors.Is(err, ErrCommandQueueFull):
					default:
						assert.NoError(t, err, "unexpected SendCommand error")
						return
					}
				}
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			svc.handleDisconnect(context.Background(), "run_race")
		}()

		close(start)
		wg.Wait()
		<-drained
	}
}

// TestRunnerConnectionClose_Idempotent guards the once-semantics of Close: the
// disconnect path can be reached from both the deferred cleanup and an explicit
// teardown, and a double close would panic.
func TestRunnerConnectionClose_Idempotent(t *testing.T) {
	conn := newRunnerConnection("run_123", "test-runner", "localhost", nil)

	select {
	case <-conn.Done():
		t.Fatal("connection reported done before Close")
	default:
	}

	conn.Close()
	conn.Close()

	select {
	case <-conn.Done():
	default:
		t.Fatal("connection did not report done after Close")
	}

	// A connection built without the constructor has a nil done channel; Close
	// must not panic on it.
	bare := &RunnerConnection{RunnerID: "run_bare"}
	bare.Close()
	assert.Nil(t, bare.Done())
}
