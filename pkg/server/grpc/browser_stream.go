package grpc

import (
	"context"
	"io"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/server/core"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TunnelManagerInterface defines the interface for tunnel management.
type TunnelManagerInterface interface {
	ValidateToken(ctx interface{}, token string) (interface{}, error)
	MarkConnected(tunnelID, runnerID, sessionID string)
	MarkDisconnected(tunnelID string)
}

// StreamBrowser handles the bidirectional stream for browser frames and input.
// This RPC is called by agents to stream browser content to the server.
func (s *RunnerService) StreamBrowser(stream grpc.BidiStreamingServer[pb.RunnerBrowserMessage, pb.ServerBrowserMessage]) error {
	ctx := stream.Context()

	// Extract tunnel token from metadata
	tunnelToken, err := s.extractTunnelToken(ctx)
	if err != nil {
		s.logger.Warn("failed to extract tunnel token", zap.Error(err))
		return status.Errorf(codes.Unauthenticated, "missing tunnel token: %v", err)
	}

	// Validate tunnel token
	if s.tunnelMgr == nil {
		s.logger.Error("tunnel manager not configured")
		return status.Error(codes.Internal, "tunnel manager not configured")
	}

	tunnel, err := s.tunnelMgr.ValidateToken(ctx, tunnelToken)
	if err != nil {
		s.logger.Warn("tunnel token validation failed", zap.Error(err))
		return status.Errorf(codes.Unauthenticated, "invalid tunnel token: %v", err)
	}

	// Wait for init message
	initMsg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive init message: %v", err)
	}

	init := initMsg.GetInit()
	if init == nil {
		return status.Error(codes.InvalidArgument, "first message must be init")
	}

	// Verify init matches tunnel
	if init.GetTunnelId() != tunnel.ID {
		return status.Error(codes.PermissionDenied, "tunnel ID mismatch")
	}

	s.logger.Info("browser stream started",
		zap.String("tunnel_id", tunnel.ID),
		zap.String("session_id", tunnel.SessionID),
		zap.String("runner_id", init.GetRunnerId()),
	)

	// Register stream with FrameHub
	if s.frameHub == nil {
		s.logger.Error("frame hub not configured")
		return status.Error(codes.Internal, "frame hub not configured")
	}

	// Create send functions for FrameHub
	sendInput := func(event *pb.BrowserInputEvent) error {
		return stream.Send(&pb.ServerBrowserMessage{
			Payload: &pb.ServerBrowserMessage_Input{
				Input: event,
			},
		})
	}

	sendControl := func(msg *pb.ServerBrowserMessage) error {
		return stream.Send(msg)
	}

	// Register stream with FrameHub
	inputCh, err := s.frameHub.RegisterStream(
		tunnel.ID,
		init.GetRunnerId(),
		tunnel.SessionID,
		sendInput,
		sendControl,
	)
	if err != nil {
		s.logger.Error("failed to register stream with frame hub",
			zap.String("tunnel_id", tunnel.ID),
			zap.Error(err),
		)
		return status.Errorf(codes.Internal, "failed to register stream: %v", err)
	}
	defer s.frameHub.UnregisterStream(tunnel.ID)

	// Mark tunnel as connected
	s.tunnelMgr.MarkConnected(tunnel.ID, init.GetRunnerId(), tunnel.SessionID)
	defer s.tunnelMgr.MarkDisconnected(tunnel.ID)

	// Start input forwarder goroutine (FrameHub → Agent)
	go s.forwardInputToAgent(ctx, tunnel.ID, inputCh, stream)

	// Receive frames from agent
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				s.logger.Debug("browser stream closed gracefully",
					zap.String("tunnel_id", tunnel.ID),
				)
				return nil
			}
			if ctx.Err() != nil {
				s.logger.Debug("browser stream context cancelled",
					zap.String("tunnel_id", tunnel.ID),
				)
				return nil
			}
			s.logger.Warn("browser stream error",
				zap.String("tunnel_id", tunnel.ID),
				zap.Error(err),
			)
			return err
		}

		// Handle frame
		if frame := msg.GetFrame(); frame != nil {
			s.frameHub.BroadcastFrame(tunnel.ID, frame)
		}

		// Handle state update
		if state := msg.GetState(); state != nil {
			s.logger.Debug("browser state update",
				zap.String("tunnel_id", tunnel.ID),
				zap.String("state", state.GetState()),
			)
			// TODO: Update tunnel state in DB or notify subscribers
		}

		// Handle stats
		if stats := msg.GetStats(); stats != nil {
			s.logger.Debug("browser stats received",
				zap.String("tunnel_id", tunnel.ID),
				zap.Uint64("frames_sent", stats.GetFramesSent()),
				zap.Float64("current_fps", stats.GetCurrentFps()),
			)
		}
	}
}

// forwardInputToAgent forwards input events from FrameHub to the agent stream.
func (s *RunnerService) forwardInputToAgent(
	ctx context.Context,
	tunnelID string,
	inputCh <-chan *pb.BrowserInputEvent,
	stream grpc.BidiStreamingServer[pb.RunnerBrowserMessage, pb.ServerBrowserMessage],
) {
	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-inputCh:
			if !ok {
				// Channel closed
				return
			}
			if err := stream.Send(&pb.ServerBrowserMessage{
				Payload: &pb.ServerBrowserMessage_Input{
					Input: event,
				},
			}); err != nil {
				s.logger.Error("failed to forward input to agent",
					zap.String("tunnel_id", tunnelID),
					zap.Error(err),
				)
				return
			}
		}
	}
}

// extractTunnelToken extracts the tunnel token from gRPC metadata.
func (s *RunnerService) extractTunnelToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.InvalidArgument, "missing metadata")
	}

	tokens := md.Get("x-tunnel-token")
	if len(tokens) == 0 {
		return "", status.Error(codes.InvalidArgument, "missing x-tunnel-token")
	}
	return tokens[0], nil
}

// WithTunnelManager sets the tunnel manager for the RunnerService.
func WithTunnelManager(tm *core.TunnelManager) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.tunnelMgr = tm
	}
}

// WithFrameHub sets the frame hub for the RunnerService.
func WithFrameHub(fh *core.FrameHub) RunnerServiceOption {
	return func(svc *RunnerService) {
		svc.frameHub = fh
	}
}
