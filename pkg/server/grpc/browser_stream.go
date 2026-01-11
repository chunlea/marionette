package grpc

import (
	"context"
	"io"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/streaming"
	"github.com/chunlea/marionette/pkg/streaming/browser"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// BrowserStreamHandler handles browser streaming operations.
// It manages the bidirectional stream between agents and the server
// for browser frame streaming and input forwarding.
type BrowserStreamHandler struct {
	provider *browser.BrowserStreamProvider
	logger   *zap.Logger
}

// NewBrowserStreamHandler creates a new BrowserStreamHandler.
func NewBrowserStreamHandler(provider *browser.BrowserStreamProvider, logger *zap.Logger) *BrowserStreamHandler {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &BrowserStreamHandler{
		provider: provider,
		logger:   logger.Named("browser-stream"),
	}
}

// StreamBrowser handles the bidirectional stream for browser frames and input.
// This RPC is called by agents to stream browser content to the server.
func (h *BrowserStreamHandler) StreamBrowser(stream grpc.BidiStreamingServer[pb.RunnerBrowserMessage, pb.ServerBrowserMessage]) error {
	ctx := stream.Context()

	// Wait for init message
	initMsg, err := stream.Recv()
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "failed to receive init message: %v", err)
	}

	init := initMsg.GetInit()
	if init == nil {
		return status.Error(codes.InvalidArgument, "first message must be init")
	}

	streamID := init.GetStreamId()
	runnerID := init.GetRunnerId()
	sessionID := init.GetSessionId()

	// Validate stream via provider
	if err := h.provider.ValidateStream(streamID, runnerID, sessionID); err != nil {
		h.logger.Warn("stream validation failed",
			zap.String("stream_id", streamID),
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
		return status.Errorf(codes.PermissionDenied, "stream validation failed: %v", err)
	}

	h.logger.Info("browser stream started",
		zap.String("stream_id", streamID),
		zap.String("session_id", sessionID),
		zap.String("runner_id", runnerID),
		zap.String("browser_product", init.GetBrowserProduct()),
	)

	// Get FrameHub from provider
	frameHub := h.provider.FrameHub()
	if frameHub == nil {
		h.logger.Error("frame hub not available")
		return status.Error(codes.Internal, "frame hub not available")
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
	inputCh, err := frameHub.RegisterStream(
		streamID,
		runnerID,
		sessionID,
		sendInput,
		sendControl,
	)
	if err != nil {
		h.logger.Error("failed to register stream with frame hub",
			zap.String("stream_id", streamID),
			zap.Error(err),
		)
		return status.Errorf(codes.Internal, "failed to register stream: %v", err)
	}
	defer frameHub.UnregisterStream(streamID)

	// Update stream state to active
	if err := h.provider.SetStreamState(streamID, streaming.StreamStateActive, ""); err != nil {
		h.logger.Warn("failed to set stream state to active",
			zap.String("stream_id", streamID),
			zap.Error(err),
		)
	}

	// Ensure stream is marked stopped on exit
	defer func() {
		if err := h.provider.SetStreamState(streamID, streaming.StreamStateStopped, ""); err != nil {
			h.logger.Warn("failed to set stream state to stopped",
				zap.String("stream_id", streamID),
				zap.Error(err),
			)
		}
	}()

	// Start input forwarder goroutine (FrameHub → Agent)
	go h.forwardInputToAgent(ctx, streamID, inputCh, stream)

	// Receive frames from agent
	for {
		msg, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				h.logger.Debug("browser stream closed gracefully",
					zap.String("stream_id", streamID),
				)
				return nil
			}
			if ctx.Err() != nil {
				h.logger.Debug("browser stream context cancelled",
					zap.String("stream_id", streamID),
				)
				return nil
			}
			h.logger.Warn("browser stream error",
				zap.String("stream_id", streamID),
				zap.Error(err),
			)
			return err
		}

		// Handle frame
		if frame := msg.GetFrame(); frame != nil {
			frameHub.BroadcastFrame(streamID, frame)
		}

		// Handle state update
		if state := msg.GetState(); state != nil {
			h.logger.Debug("browser state update",
				zap.String("stream_id", streamID),
				zap.String("state", state.GetState()),
			)
			// Update provider state
			streamState := streaming.StreamState(state.GetState())
			if err := h.provider.SetStreamState(streamID, streamState, state.GetError()); err != nil {
				h.logger.Warn("failed to update stream state",
					zap.String("stream_id", streamID),
					zap.Error(err),
				)
			}
		}

		// Handle stats
		if stats := msg.GetStats(); stats != nil {
			h.logger.Debug("browser stats received",
				zap.String("stream_id", streamID),
				zap.Uint64("frames_sent", stats.GetFramesSent()),
				zap.Float64("current_fps", stats.GetCurrentFps()),
			)
		}
	}
}

// forwardInputToAgent forwards input events from FrameHub to the agent stream.
func (h *BrowserStreamHandler) forwardInputToAgent(
	ctx context.Context,
	streamID string,
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
				h.logger.Error("failed to forward input to agent",
					zap.String("stream_id", streamID),
					zap.Error(err),
				)
				return
			}
		}
	}
}

// extractStreamID extracts the stream ID from gRPC metadata (if provided).
// This is an alternative to getting stream_id from the init message.
func extractStreamID(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.InvalidArgument, "missing metadata")
	}

	ids := md.Get("x-stream-id")
	if len(ids) == 0 {
		return "", status.Error(codes.InvalidArgument, "missing x-stream-id")
	}
	return ids[0], nil
}

// StreamBrowserAdapter adapts BrowserStreamHandler to RunnerService.
// This allows the RunnerService to delegate browser streaming to the handler.
type StreamBrowserAdapter struct {
	handler *BrowserStreamHandler
}

// NewStreamBrowserAdapter creates a new adapter.
func NewStreamBrowserAdapter(handler *BrowserStreamHandler) *StreamBrowserAdapter {
	return &StreamBrowserAdapter{handler: handler}
}

// StreamBrowser delegates to the handler.
func (a *StreamBrowserAdapter) StreamBrowser(stream grpc.BidiStreamingServer[pb.RunnerBrowserMessage, pb.ServerBrowserMessage]) error {
	return a.handler.StreamBrowser(stream)
}
