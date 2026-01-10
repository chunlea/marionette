package agent

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/tunnel"
	"go.uber.org/zap"
)

// Default timeout for tunnel creation.
const defaultTunnelCreateTimeout = 30 * time.Second

// TunnelManager manages tunnel lifecycle on the agent side.
// It handles tunnel creation requests and data relay.
type TunnelManager struct {
	logger       *zap.Logger
	relayManager *tunnel.RelayManager
	sender       MessageSender

	// Pending tunnel requests waiting for response
	pendingRequests   map[string]chan *pb.CreateTunnelResponse
	pendingRequestsMu sync.Mutex

	// Active tunnels
	tunnels   map[string]*TunnelInfo
	tunnelsMu sync.RWMutex

	// Current session ID (set when session is attached)
	sessionID   string
	sessionIDMu sync.RWMutex
}

// Note: MessageSender is defined in task_runner.go

// TunnelInfo holds information about an active tunnel.
type TunnelInfo struct {
	ID        string
	Type      string
	LocalPort int
	Token     string
	PublicURL string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// TunnelManagerOption is a functional option for TunnelManager.
type TunnelManagerOption func(*TunnelManager)

// WithTMLogger sets the logger for the tunnel manager.
func WithTMLogger(logger *zap.Logger) TunnelManagerOption {
	return func(m *TunnelManager) {
		m.logger = logger
	}
}

// WithTMSender sets the message sender for the tunnel manager.
func WithTMSender(sender MessageSender) TunnelManagerOption {
	return func(m *TunnelManager) {
		m.sender = sender
	}
}

// NewTunnelManager creates a new TunnelManager.
func NewTunnelManager(opts ...TunnelManagerOption) *TunnelManager {
	m := &TunnelManager{
		logger:          zap.NewNop(),
		pendingRequests: make(map[string]chan *pb.CreateTunnelResponse),
		tunnels:         make(map[string]*TunnelInfo),
	}

	for _, opt := range opts {
		opt(m)
	}

	// Create relay manager for handling tunnel data
	// The sendFrame callback sends data back to the server
	m.relayManager = tunnel.NewRelayManager(
		m.logger.Named("relay"),
		m.sendFrame,
	)

	return m
}

// sendFrame sends a tunnel frame to the server.
func (m *TunnelManager) sendFrame(frame *tunnel.Frame) error {
	if m.sender == nil {
		return errors.New("message sender not configured")
	}

	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_TunnelData{
			TunnelData: &pb.TunnelData{
				TunnelId: frame.TunnelID,
				Data:     frame.Payload,
				Eof:      frame.Type == tunnel.FrameTypeClose,
			},
		},
	}
	m.sender.Send(msg)
	return nil
}

// SetSessionID sets the current session ID.
func (m *TunnelManager) SetSessionID(sessionID string) {
	m.sessionIDMu.Lock()
	defer m.sessionIDMu.Unlock()
	m.sessionID = sessionID
}

// GetSessionID returns the current session ID.
func (m *TunnelManager) GetSessionID() string {
	m.sessionIDMu.RLock()
	defer m.sessionIDMu.RUnlock()
	return m.sessionID
}

// CreateTunnelParams contains parameters for creating a tunnel.
type CreateTunnelParams struct {
	Type      string // "http" or "tcp"
	LocalPort int    // Port on the agent to tunnel
	Timeout   time.Duration
}

// CreateTunnelResult contains the result of tunnel creation.
type CreateTunnelResult struct {
	TunnelID  string
	Token     string
	PublicURL string
	ExpiresAt time.Time
}

// CreateTunnel requests a new tunnel from the server.
// This is a blocking call that waits for the server's response.
func (m *TunnelManager) CreateTunnel(ctx context.Context, params *CreateTunnelParams) (*CreateTunnelResult, error) {
	sessionID := m.GetSessionID()
	if sessionID == "" {
		return nil, errors.New("no session attached")
	}

	if m.sender == nil {
		return nil, errors.New("message sender not configured")
	}

	// Validate params
	if params.LocalPort <= 0 || params.LocalPort > 65535 {
		return nil, errors.New("invalid local_port: must be between 1 and 65535")
	}

	tunnelType := params.Type
	if tunnelType == "" {
		tunnelType = "http"
	}

	timeout := params.Timeout
	if timeout <= 0 {
		timeout = defaultTunnelCreateTimeout
	}

	// Create a unique request ID
	requestID := fmt.Sprintf("req_%d", time.Now().UnixNano())

	// Create channel for response
	responseCh := make(chan *pb.CreateTunnelResponse, 1)
	m.pendingRequestsMu.Lock()
	m.pendingRequests[requestID] = responseCh
	m.pendingRequestsMu.Unlock()

	// Clean up on exit
	defer func() {
		m.pendingRequestsMu.Lock()
		delete(m.pendingRequests, requestID)
		m.pendingRequestsMu.Unlock()
	}()

	m.logger.Info("requesting tunnel creation",
		zap.String("session_id", sessionID),
		zap.String("type", tunnelType),
		zap.Int("local_port", params.LocalPort),
		zap.String("request_id", requestID),
	)

	// Send request to server
	msg := &pb.RunnerMessage{
		Payload: &pb.RunnerMessage_CreateTunnelRequest{
			CreateTunnelRequest: &pb.CreateTunnelRequest{
				RequestId: requestID,
				SessionId: sessionID,
				Type:      tunnelType,
				LocalPort: int32(params.LocalPort),
				Direction: "outbound",
			},
		},
	}
	m.sender.Send(msg)

	// Wait for response with timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("tunnel creation timed out: %w", ctx.Err())
	case resp := <-responseCh:
		if !resp.Success {
			return nil, fmt.Errorf("tunnel creation failed: %s", resp.Error)
		}

		// Store tunnel info
		info := &TunnelInfo{
			ID:        resp.TunnelId,
			Type:      tunnelType,
			LocalPort: params.LocalPort,
			Token:     resp.Token,
			PublicURL: resp.PublicUrl,
			ExpiresAt: time.UnixMilli(resp.ExpiresAtUnixMs),
			CreatedAt: time.Now(),
		}

		m.tunnelsMu.Lock()
		m.tunnels[resp.TunnelId] = info
		m.tunnelsMu.Unlock()

		m.logger.Info("tunnel created successfully",
			zap.String("tunnel_id", resp.TunnelId),
			zap.String("public_url", resp.PublicUrl),
			zap.Time("expires_at", info.ExpiresAt),
		)

		// Start relay connection to local port
		if err := m.startRelay(resp.TunnelId, params.LocalPort); err != nil {
			m.logger.Warn("failed to start relay",
				zap.String("tunnel_id", resp.TunnelId),
				zap.Error(err),
			)
			// Don't fail - the tunnel is created, relay can be retried
		}

		return &CreateTunnelResult{
			TunnelID:  resp.TunnelId,
			Token:     resp.Token,
			PublicURL: resp.PublicUrl,
			ExpiresAt: info.ExpiresAt,
		}, nil
	}
}

// HandleCreateTunnelResponse handles a CreateTunnelResponse from the server.
func (m *TunnelManager) HandleCreateTunnelResponse(resp *pb.CreateTunnelResponse) {
	requestID := resp.GetRequestId()
	m.logger.Debug("received tunnel response",
		zap.String("request_id", requestID),
		zap.String("tunnel_id", resp.GetTunnelId()),
		zap.Bool("success", resp.GetSuccess()),
		zap.String("error", resp.GetError()),
	)

	m.pendingRequestsMu.Lock()
	defer m.pendingRequestsMu.Unlock()

	// Match response to pending request using request_id
	ch, ok := m.pendingRequests[requestID]
	if !ok {
		m.logger.Warn("no pending request for tunnel response",
			zap.String("request_id", requestID),
			zap.String("tunnel_id", resp.GetTunnelId()),
		)
		return
	}

	select {
	case ch <- resp:
	default:
		m.logger.Warn("response channel full, dropping response")
	}
}

// HandleTunnelData handles incoming tunnel data from the server.
func (m *TunnelManager) HandleTunnelData(ctx context.Context, data *pb.TunnelData) error {
	m.logger.Debug("received tunnel data",
		zap.String("tunnel_id", data.GetTunnelId()),
		zap.String("connection_id", data.GetConnectionId()),
		zap.Int("data_len", len(data.GetData())),
		zap.Bool("eof", data.GetEof()),
	)

	// Route to relay manager
	return m.relayManager.HandleFrame(&tunnel.Frame{
		Type:     tunnel.FrameTypeData,
		TunnelID: data.GetTunnelId(),
		Payload:  data.GetData(),
	})
}

// startRelay starts the relay connection to the local port.
func (m *TunnelManager) startRelay(tunnelID string, localPort int) error {
	return m.relayManager.HandleConnect(tunnelID, localPort)
}

// GetTunnel returns information about an active tunnel.
func (m *TunnelManager) GetTunnel(tunnelID string) (*TunnelInfo, bool) {
	m.tunnelsMu.RLock()
	defer m.tunnelsMu.RUnlock()
	info, ok := m.tunnels[tunnelID]
	return info, ok
}

// ListTunnels returns all active tunnels.
func (m *TunnelManager) ListTunnels() []*TunnelInfo {
	m.tunnelsMu.RLock()
	defer m.tunnelsMu.RUnlock()

	result := make([]*TunnelInfo, 0, len(m.tunnels))
	for _, info := range m.tunnels {
		result = append(result, info)
	}
	return result
}

// CloseTunnel closes a tunnel.
func (m *TunnelManager) CloseTunnel(tunnelID string, reason string) error {
	m.tunnelsMu.Lock()
	_, exists := m.tunnels[tunnelID]
	if exists {
		delete(m.tunnels, tunnelID)
	}
	m.tunnelsMu.Unlock()

	if !exists {
		return errors.New("tunnel not found")
	}

	// Close relay
	if err := m.relayManager.HandleClose(tunnelID); err != nil {
		m.logger.Warn("failed to close relay",
			zap.String("tunnel_id", tunnelID),
			zap.Error(err),
		)
	}

	// Notify server
	if m.sender != nil {
		msg := &pb.RunnerMessage{
			Payload: &pb.RunnerMessage_CloseTunnel{
				CloseTunnel: &pb.CloseTunnel{
					TunnelId: tunnelID,
					Reason:   reason,
				},
			},
		}
		m.sender.Send(msg)
	}

	m.logger.Info("tunnel closed",
		zap.String("tunnel_id", tunnelID),
		zap.String("reason", reason),
	)

	return nil
}
