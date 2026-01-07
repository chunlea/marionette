package api

import (
	"context"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/server/core"
)

// BrowserStreamAdapter implements BrowserStreamService by wrapping TunnelManager and FrameHub.
type BrowserStreamAdapter struct {
	tunnelMgr *core.TunnelManager
	frameHub  *core.FrameHub
}

// NewBrowserStreamAdapter creates a new BrowserStreamAdapter.
func NewBrowserStreamAdapter(tunnelMgr *core.TunnelManager, frameHub *core.FrameHub) *BrowserStreamAdapter {
	return &BrowserStreamAdapter{
		tunnelMgr: tunnelMgr,
		frameHub:  frameHub,
	}
}

// ValidateTunnelToken validates a tunnel token and returns tunnel info.
func (a *BrowserStreamAdapter) ValidateTunnelToken(ctx context.Context, token string) (*core.TunnelInfo, error) {
	tunnel, err := a.tunnelMgr.ValidateToken(ctx, token)
	if err != nil {
		return nil, err
	}

	return &core.TunnelInfo{
		TunnelID:  tunnel.ID,
		SessionID: tunnel.SessionID,
		Type:      tunnel.Type,
	}, nil
}

// Subscribe subscribes to frames for a tunnel.
func (a *BrowserStreamAdapter) Subscribe(subscriber *core.FrameSubscriber) {
	a.frameHub.Subscribe(subscriber)
}

// Unsubscribe removes a subscriber.
func (a *BrowserStreamAdapter) Unsubscribe(subscriber *core.FrameSubscriber) {
	a.frameHub.Unsubscribe(subscriber)
}

// SendInput sends an input event to the agent.
func (a *BrowserStreamAdapter) SendInput(ctx context.Context, tunnelID string, event *pb.BrowserInputEvent) error {
	return a.frameHub.SendInput(ctx, tunnelID, event)
}

// SendControl sends a control message to the agent.
func (a *BrowserStreamAdapter) SendControl(ctx context.Context, tunnelID string, msg *pb.ServerBrowserMessage) error {
	return a.frameHub.SendControl(ctx, tunnelID, msg)
}

// GetStats returns statistics for a tunnel.
func (a *BrowserStreamAdapter) GetStats(tunnelID string) *core.FrameHubStats {
	return a.frameHub.GetStats(tunnelID)
}

// IsConnected checks if a tunnel has an active stream.
func (a *BrowserStreamAdapter) IsConnected(tunnelID string) bool {
	return a.frameHub.GetStream(tunnelID) != nil
}

// Verify BrowserStreamAdapter implements BrowserStreamService.
var _ BrowserStreamService = (*BrowserStreamAdapter)(nil)
