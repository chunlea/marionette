package api

import (
	"context"
	"fmt"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/tunnel"
	"go.uber.org/zap"
)

// TunnelAdapter adapts TunnelManager to the TunnelService interface.
type TunnelAdapter struct {
	tunnelManager *tunnel.TunnelManager
	tunnelRouter  TunnelRouter
	store         store.Store
	logger        *zap.Logger
}

// TunnelAdapterOption is a functional option for TunnelAdapter.
type TunnelAdapterOption func(*TunnelAdapter)

// WithTALogger sets the logger.
func WithTALogger(logger *zap.Logger) TunnelAdapterOption {
	return func(a *TunnelAdapter) {
		a.logger = logger
	}
}

// WithTATunnelManager sets the tunnel manager.
func WithTATunnelManager(tm *tunnel.TunnelManager) TunnelAdapterOption {
	return func(a *TunnelAdapter) {
		a.tunnelManager = tm
	}
}

// WithTATunnelRouter sets the tunnel router for registering tunnels.
func WithTATunnelRouter(tr TunnelRouter) TunnelAdapterOption {
	return func(a *TunnelAdapter) {
		a.tunnelRouter = tr
	}
}

// WithTAStore sets the store.
func WithTAStore(s store.Store) TunnelAdapterOption {
	return func(a *TunnelAdapter) {
		a.store = s
	}
}

// NewTunnelAdapter creates a new TunnelAdapter.
func NewTunnelAdapter(opts ...TunnelAdapterOption) *TunnelAdapter {
	a := &TunnelAdapter{
		logger: zap.NewNop(),
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Create creates a new tunnel for a session.
func (a *TunnelAdapter) Create(ctx context.Context, opts CreateTunnelOptions) (*tunnel.Tunnel, error) {
	if a.tunnelManager == nil {
		return nil, fmt.Errorf("tunnel manager not configured")
	}

	// Get session to find the runner ID
	if a.store == nil {
		return nil, fmt.Errorf("store not configured")
	}

	session, err := a.store.GetSession(ctx, opts.SessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to get session: %w", err)
	}

	if session.RunnerID == nil || *session.RunnerID == "" {
		return nil, fmt.Errorf("session has no runner attached")
	}

	// Create the tunnel
	tunnelOpts := tunnel.CreateTunnelOptions{
		SessionID: opts.SessionID,
		RunnerID:  *session.RunnerID,
		Type:      opts.Type,
		LocalPort: opts.LocalPort,
		Public:    opts.Public,
	}

	tun, err := a.tunnelManager.Create(ctx, tunnelOpts)
	if err != nil {
		return nil, err
	}

	// Register tunnel with router for traffic routing
	if a.tunnelRouter != nil {
		a.tunnelRouter.RegisterTunnel(tun.ID, tun.RunnerID)
		a.logger.Debug("tunnel registered with router",
			zap.String("tunnel_id", tun.ID),
			zap.String("runner_id", tun.RunnerID),
		)

		// Notify the runner to start the relay
		if err := a.tunnelRouter.NotifyTunnelCreated(tun.ID, tun.RunnerID, tun.Type, int32(tun.LocalPort), tun.Direction); err != nil {
			a.logger.Error("failed to notify runner of tunnel creation",
				zap.String("tunnel_id", tun.ID),
				zap.String("runner_id", tun.RunnerID),
				zap.Error(err),
			)
			// Don't fail the tunnel creation, but log the error
			// The tunnel is created in the database, user can retry accessing it
		}
	}

	return tun, nil
}

// Get retrieves a tunnel by ID.
func (a *TunnelAdapter) Get(ctx context.Context, id string) (*tunnel.Tunnel, error) {
	if a.tunnelManager == nil {
		return nil, fmt.Errorf("tunnel manager not configured")
	}
	return a.tunnelManager.Get(ctx, id)
}

// ListBySession returns tunnels for a session.
func (a *TunnelAdapter) ListBySession(ctx context.Context, sessionID string) ([]*tunnel.Tunnel, error) {
	if a.tunnelManager == nil {
		return nil, fmt.Errorf("tunnel manager not configured")
	}
	return a.tunnelManager.GetBySession(ctx, sessionID)
}

// Close closes a tunnel.
func (a *TunnelAdapter) Close(ctx context.Context, id string) error {
	if a.tunnelManager == nil {
		return fmt.Errorf("tunnel manager not configured")
	}
	return a.tunnelManager.Close(ctx, id)
}

// Ensure TunnelAdapter implements TunnelService.
var _ TunnelService = (*TunnelAdapter)(nil)
