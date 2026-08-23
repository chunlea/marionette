package grpc

import (
	"context"
	"io"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/store"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// disconnectTimeout is the timeout for cleanup operations during disconnect.
const disconnectTimeout = 5 * time.Second

// Connect handles the bidirectional stream for control messages.
func (s *RunnerService) Connect(stream grpc.BidiStreamingServer[pb.RunnerMessage, pb.ServerCommand]) error {
	ctx := stream.Context()

	// Extract runner credentials from metadata
	runnerID, err := s.extractRunnerID(ctx)
	if err != nil {
		s.logger.Warn("failed to extract runner ID", zap.Error(err))
		return status.Errorf(codes.Unauthenticated, "missing runner credentials: %v", err)
	}

	// Validate runner auth token and bind the tenant it belongs to.
	ctx, err = s.validateRunnerAuth(ctx, runnerID)
	if err != nil {
		s.logger.Warn("runner auth validation failed",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return status.Errorf(codes.Unauthenticated, "invalid runner credentials: %v", err)
	}

	// Get runner from database to verify it exists
	runner, err := s.store.GetRunner(ctx, runnerID)
	if err != nil {
		s.logger.Warn("runner not found in database",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return status.Errorf(codes.NotFound, "runner not found: %v", err)
	}

	// Create connection with command channel
	conn := s.createConnection(runner, stream)

	// Register connection (fails if already connected)
	if err := s.connManager.Register(runnerID, conn); err != nil {
		s.logger.Warn("failed to register connection",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return status.Errorf(codes.AlreadyExists, "runner already connected: %v", err)
	}
	defer s.handleDisconnect(ctx, runnerID)

	s.logger.Info("runner connected",
		zap.String("runner_id", runnerID),
		zap.String("name", runner.Name),
		zap.String("hostname", runner.Hostname),
	)

	// Notify runner manager of connect (updates DB status)
	if s.runnerManager != nil {
		if err := s.runnerManager.OnConnect(ctx, runnerID); err != nil {
			s.logger.Error("failed to process runner connect", zap.Error(err))
		}
	}

	// Start command sender goroutine. A send failure tears the connection down
	// via conn.Close(), which is what the receive loop below observes; there is
	// no separate error channel to read.
	go s.sendCommands(ctx, conn)

	// Receive messages from runner
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				s.logger.Debug("runner stream closed gracefully",
					zap.String("runner_id", runnerID),
				)
				return nil
			}
			// Check if context was cancelled
			if ctx.Err() != nil {
				s.logger.Debug("runner stream context cancelled",
					zap.String("runner_id", runnerID),
				)
				return nil
			}
			s.logger.Warn("runner stream error",
				zap.String("runner_id", runnerID),
				zap.Error(err),
			)
			return err
		}

		// Route message to handler
		if s.router != nil {
			if err := s.router.HandleMessage(ctx, runnerID, msg); err != nil {
				s.logger.Error("failed to handle runner message",
					zap.String("runner_id", runnerID),
					zap.Error(err),
				)
			}
		}

		// Update last seen on any message
		conn.UpdateLastSeen()
	}
}

// sendCommands sends queued commands to the runner via the stream.
//
// It used to report failures on an error channel that nobody ever read, so a
// broken stream was noticed only when the receive side happened to fail too.
// Now a failed send closes the connection, which stops SendCommand from
// queueing into a stream that is already gone.
func (s *RunnerService) sendCommands(ctx context.Context, conn *RunnerConnection) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-conn.Done():
			// Connection torn down; drop anything still queued.
			return
		case cmd := <-conn.commandCh:
			if err := conn.stream.Send(cmd); err != nil {
				s.logger.Error("failed to send command",
					zap.String("runner_id", conn.RunnerID),
					zap.Error(err),
				)
				conn.Close()
				return
			}
		}
	}
}

// handleDisconnect cleans up when a runner disconnects.
// Note: We use a background context here because the stream context may be canceled.
func (s *RunnerService) handleDisconnect(_ context.Context, runnerID string) {
	s.logger.Info("runner disconnecting", zap.String("runner_id", runnerID))

	// Signal the sender goroutine to stop. The command channel is deliberately
	// never closed: SendCommand publishes to it without holding a lock that this
	// path takes, so closing it would let a send race into a closed channel and
	// panic the process.
	if conn, ok := s.connManager.Get(runnerID); ok {
		conn.Close()
	}

	// Unregister from connection manager
	s.connManager.Unregister(runnerID)

	// Notify runner manager (updates DB status to offline)
	// Use a fresh background context since the stream context may be canceled
	if s.runnerManager != nil {
		ctx, cancel := context.WithTimeout(context.Background(), disconnectTimeout)
		defer cancel()
		if err := s.runnerManager.OnDisconnect(ctx, runnerID); err != nil {
			s.logger.Error("failed to process runner disconnect", zap.Error(err))
		}
	}
}

// extractRunnerID extracts the runner ID from gRPC metadata.
func (s *RunnerService) extractRunnerID(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.InvalidArgument, "missing metadata")
	}

	runnerIDs := md.Get("x-runner-id")
	if len(runnerIDs) == 0 {
		return "", status.Error(codes.InvalidArgument, "missing x-runner-id")
	}
	return runnerIDs[0], nil
}

// extractRunnerToken extracts the runner token from gRPC metadata.
func (s *RunnerService) extractRunnerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.InvalidArgument, "missing metadata")
	}

	tokens := md.Get("x-runner-token")
	if len(tokens) == 0 {
		return "", status.Error(codes.InvalidArgument, "missing x-runner-token")
	}
	return tokens[0], nil
}

// validateRunnerAuth validates the runner's authentication credentials and
// returns a context bound to the token's tenant.
//
// This is the runner-side counterpart of the API key middleware: a runner acts
// for exactly the tenant that issued its token, and everything the stream does
// afterwards - status updates, task runs, logs - is filtered by it.
func (s *RunnerService) validateRunnerAuth(ctx context.Context, runnerID string) (context.Context, error) {
	if s.tokenSvc == nil {
		// Token service not configured, skip validation
		return ctx, nil
	}

	token, err := s.extractRunnerToken(ctx)
	if err != nil {
		return ctx, err
	}

	// Validate token
	tokenInfo, err := s.tokenSvc.Validate(ctx, token)
	if err != nil {
		return ctx, status.Errorf(codes.Unauthenticated, "invalid token: %v", err)
	}

	// Verify token is bound to this runner (or unbound)
	if tokenInfo.RunnerID != nil && *tokenInfo.RunnerID != runnerID {
		return ctx, status.Error(codes.PermissionDenied, "token bound to different runner")
	}

	// Update last used timestamp
	_ = s.tokenSvc.UpdateLastUsed(ctx, tokenInfo.ID)

	if tokenInfo.TenantID != nil && *tokenInfo.TenantID != "" {
		ctx = store.WithTenant(ctx, *tokenInfo.TenantID)
	}

	return ctx, nil
}
