package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/tunnel"
)

// TunnelService defines operations for managing tunnels.
// This interface is implemented by the TunnelAdapter and
// can be mocked for testing HTTP handlers.
type TunnelService interface {
	// Create creates a new tunnel for a session.
	Create(ctx context.Context, opts CreateTunnelOptions) (*tunnel.Tunnel, error)

	// Get retrieves a tunnel by ID.
	Get(ctx context.Context, id string) (*tunnel.Tunnel, error)

	// ListBySession returns tunnels for a session.
	ListBySession(ctx context.Context, sessionID string) ([]*tunnel.Tunnel, error)

	// Close closes a tunnel.
	Close(ctx context.Context, id string) error
}

// CreateTunnelOptions contains options for creating a tunnel.
type CreateTunnelOptions struct {
	SessionID string `json:"session_id,omitempty"` // Set from URL path
	Type      string `json:"type"`
	LocalPort int    `json:"local_port"`
	Public    bool   `json:"public"` // If true, no authentication required
}
