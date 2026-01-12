package api

import (
	"context"

	"github.com/chunlea/marionette/pkg/streaming/browser"
)

// BrowserStreamAdapter adapts BrowserStreamProvider to the BrowserStreamService interface.
type BrowserStreamAdapter struct {
	provider *browser.BrowserStreamProvider
}

// NewBrowserStreamAdapter creates a new BrowserStreamAdapter.
func NewBrowserStreamAdapter(provider *browser.BrowserStreamProvider) *BrowserStreamAdapter {
	return &BrowserStreamAdapter{provider: provider}
}

// GetFrameHub returns the FrameHub for subscribing to frames.
func (a *BrowserStreamAdapter) GetFrameHub() *browser.FrameHub {
	return a.provider.FrameHub()
}

// ValidateStreamAccess validates that the caller can access the stream.
func (a *BrowserStreamAdapter) ValidateStreamAccess(ctx context.Context, streamID, token string) error {
	// For now, we just check if the stream exists
	// TODO: Add token validation when stream tokens are implemented
	_, err := a.provider.GetInfo(ctx, streamID)
	return err
}
