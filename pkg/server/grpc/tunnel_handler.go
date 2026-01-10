package grpc

import (
	"context"
	"fmt"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/tunnel"
	"go.uber.org/zap"
)

// TunnelHandlerInterface defines the interface for handling tunnel requests from runners.
type TunnelHandlerInterface interface {
	// HandleCreateTunnelRequest handles a CreateTunnelRequest from a runner.
	// Returns the CreateTunnelResponse to send back to the runner.
	HandleCreateTunnelRequest(ctx context.Context, runnerID string, req *pb.CreateTunnelRequest) (*pb.CreateTunnelResponse, error)
}

// TunnelHandler handles tunnel requests from runners.
type TunnelHandler struct {
	logger        *zap.Logger
	tunnelManager *tunnel.TunnelManager
}

// TunnelHandlerOption is a functional option for TunnelHandler.
type TunnelHandlerOption func(*TunnelHandler)

// WithTHLogger sets the logger for the tunnel handler.
func WithTHLogger(logger *zap.Logger) TunnelHandlerOption {
	return func(h *TunnelHandler) {
		h.logger = logger
	}
}

// WithTHTunnelManager sets the tunnel manager for the tunnel handler.
func WithTHTunnelManager(tm *tunnel.TunnelManager) TunnelHandlerOption {
	return func(h *TunnelHandler) {
		h.tunnelManager = tm
	}
}

// NewTunnelHandler creates a new TunnelHandler.
func NewTunnelHandler(opts ...TunnelHandlerOption) *TunnelHandler {
	h := &TunnelHandler{
		logger: zap.NewNop(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// HandleCreateTunnelRequest handles a CreateTunnelRequest from a runner.
func (h *TunnelHandler) HandleCreateTunnelRequest(ctx context.Context, runnerID string, req *pb.CreateTunnelRequest) (*pb.CreateTunnelResponse, error) {
	h.logger.Info("handling create tunnel request",
		zap.String("runner_id", runnerID),
		zap.String("session_id", req.GetSessionId()),
		zap.String("type", req.GetType()),
		zap.Int32("local_port", req.GetLocalPort()),
	)

	requestID := req.GetRequestId()

	if h.tunnelManager == nil {
		return &pb.CreateTunnelResponse{
			RequestId: requestID,
			Success:   false,
			Error:     "tunnel manager not configured",
		}, nil
	}

	// Validate request
	sessionID := req.GetSessionId()
	if sessionID == "" {
		return &pb.CreateTunnelResponse{
			RequestId: requestID,
			Success:   false,
			Error:     "session_id is required",
		}, nil
	}

	tunnelType := req.GetType()
	if tunnelType == "" {
		tunnelType = "http" // Default to HTTP
	}

	localPort := int(req.GetLocalPort())
	if localPort <= 0 || localPort > 65535 {
		return &pb.CreateTunnelResponse{
			RequestId: requestID,
			Success:   false,
			Error:     "invalid local_port: must be between 1 and 65535",
		}, nil
	}

	// Create tunnel
	opts := tunnel.CreateTunnelOptions{
		SessionID: sessionID,
		RunnerID:  runnerID,
		Type:      tunnelType,
		LocalPort: localPort,
	}

	t, err := h.tunnelManager.Create(ctx, opts)
	if err != nil {
		h.logger.Error("failed to create tunnel",
			zap.String("runner_id", runnerID),
			zap.String("session_id", sessionID),
			zap.Error(err),
		)
		return &pb.CreateTunnelResponse{
			RequestId: requestID,
			Success:   false,
			Error:     fmt.Sprintf("failed to create tunnel: %v", err),
		}, nil
	}

	h.logger.Info("tunnel created successfully",
		zap.String("tunnel_id", t.ID),
		zap.String("runner_id", runnerID),
		zap.String("session_id", sessionID),
		zap.String("public_url", t.PublicURL),
	)

	return &pb.CreateTunnelResponse{
		RequestId:       requestID,
		Success:         true,
		TunnelId:        t.ID,
		Token:           t.Token,
		PublicUrl:       t.PublicURL,
		ExpiresAtUnixMs: t.ExpiresAt.UnixMilli(),
	}, nil
}
