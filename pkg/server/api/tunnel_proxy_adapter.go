package api

import (
	"context"
	"errors"
	"time"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/tunnel"
	"go.uber.org/zap"
)

// TunnelRouter defines the interface for tunnel data routing.
// This is implemented by grpc.TunnelRouter.
type TunnelRouter interface {
	SendRequest(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan *pb.TunnelData, error)
	CloseConnection(connectionID string)
	RegisterTunnel(tunnelID, runnerID string)
	UnregisterTunnel(tunnelID string)
	NotifyTunnelCreated(tunnelID, runnerID, tunnelType string, localPort int32, direction string) error
}

// TunnelProxyAdapter implements TunnelProxyService by integrating with
// TunnelManager and TunnelRouter.
type TunnelProxyAdapter struct {
	logger        *zap.Logger
	tunnelManager *tunnel.TunnelManager
	tunnelRouter  TunnelRouter
}

// TunnelProxyAdapterOption is a functional option for TunnelProxyAdapter.
type TunnelProxyAdapterOption func(*TunnelProxyAdapter)

// WithTPALogger sets the logger for the adapter.
func WithTPALogger(logger *zap.Logger) TunnelProxyAdapterOption {
	return func(a *TunnelProxyAdapter) {
		a.logger = logger
	}
}

// WithTPATunnelManager sets the tunnel manager.
func WithTPATunnelManager(tm *tunnel.TunnelManager) TunnelProxyAdapterOption {
	return func(a *TunnelProxyAdapter) {
		a.tunnelManager = tm
	}
}

// WithTPATunnelRouter sets the tunnel router.
func WithTPATunnelRouter(tr TunnelRouter) TunnelProxyAdapterOption {
	return func(a *TunnelProxyAdapter) {
		a.tunnelRouter = tr
	}
}

// NewTunnelProxyAdapter creates a new TunnelProxyAdapter.
func NewTunnelProxyAdapter(opts ...TunnelProxyAdapterOption) *TunnelProxyAdapter {
	a := &TunnelProxyAdapter{
		logger: zap.NewNop(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// ValidateTunnel validates a tunnel exists and is not expired.
func (a *TunnelProxyAdapter) ValidateTunnel(ctx context.Context, tunnelID string) (*TunnelInfo, error) {
	if a.tunnelManager == nil {
		return nil, errors.New("tunnel manager not configured")
	}

	t, err := a.tunnelManager.Get(ctx, tunnelID)
	if err != nil {
		return nil, err
	}

	if t.ClosedAt != nil {
		return nil, errors.New("tunnel is closed")
	}

	return &TunnelInfo{
		ID:        t.ID,
		Type:      t.Type,
		RunnerID:  t.RunnerID,
		SessionID: t.SessionID,
		IsPublic:  t.IsPublic,
		ExpiresAt: t.ExpiresAt,
	}, nil
}

// ValidateTunnelToken validates a tunnel token.
func (a *TunnelProxyAdapter) ValidateTunnelToken(ctx context.Context, tunnelID, token string) (bool, error) {
	if a.tunnelManager == nil {
		return false, errors.New("tunnel manager not configured")
	}

	// TunnelManager.ValidateToken checks both existence and token validity
	_, err := a.tunnelManager.ValidateToken(ctx, tunnelID, token)
	if err != nil {
		a.logger.Debug("tunnel token validation failed",
			zap.String("tunnel_id", tunnelID),
			zap.Error(err),
		)
		return false, nil
	}

	return true, nil
}

// SendRequest sends a serialized HTTP request through the tunnel.
func (a *TunnelProxyAdapter) SendRequest(ctx context.Context, tunnelID, connectionID string, data []byte) (<-chan []byte, error) {
	if a.tunnelRouter == nil {
		return nil, errors.New("tunnel router not configured")
	}

	// Send through tunnel router
	responseCh, err := a.tunnelRouter.SendRequest(ctx, tunnelID, connectionID, data)
	if err != nil {
		return nil, err
	}

	// Convert pb.TunnelData channel to []byte channel
	bytesCh := make(chan []byte, 10)
	go func() {
		defer close(bytesCh)
		for data := range responseCh {
			if data == nil {
				continue
			}
			if len(data.Data) > 0 {
				bytesCh <- data.Data
			}
			if data.Eof {
				return
			}
		}
	}()

	return bytesCh, nil
}

// CloseConnection closes a tunnel connection.
func (a *TunnelProxyAdapter) CloseConnection(connectionID string) {
	if a.tunnelRouter != nil {
		a.tunnelRouter.CloseConnection(connectionID)
	}
}

// Ensure TunnelProxyAdapter implements TunnelProxyService.
var _ TunnelProxyService = (*TunnelProxyAdapter)(nil)

// CreateAPIKeyValidator creates an API key validation function for tunnel proxy.
// This allows tunnels to be accessed with API keys in addition to tunnel tokens.
func CreateAPIKeyValidator(validateKey func(ctx context.Context, key string) (bool, error)) func(r *context.Context, header string) (bool, error) {
	return func(ctx *context.Context, header string) (bool, error) {
		return validateKey(*ctx, header)
	}
}

// TunnelCleanupConfig holds configuration for tunnel cleanup.
type TunnelCleanupConfig struct {
	Interval time.Duration // How often to run cleanup
	MaxAge   time.Duration // Max age for stale connections
}

// DefaultTunnelCleanupConfig returns the default cleanup configuration.
func DefaultTunnelCleanupConfig() TunnelCleanupConfig {
	return TunnelCleanupConfig{
		Interval: 5 * time.Minute,
		MaxAge:   10 * time.Minute,
	}
}
