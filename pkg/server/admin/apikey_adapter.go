package admin

import (
	"context"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/store"
)

// APIKeyAdapter adapts auth.APIKeyService to admin.APIKeyService.
type APIKeyAdapter struct {
	service *auth.APIKeyService
}

// NewAPIKeyAdapter creates a new APIKeyAdapter.
func NewAPIKeyAdapter(service *auth.APIKeyService) *APIKeyAdapter {
	return &APIKeyAdapter{
		service: service,
	}
}

// Create creates a new API key and returns the key and plaintext token.
func (a *APIKeyAdapter) Create(ctx context.Context, opts CreateAPIKeyOptions) (*store.APIKey, string, error) {
	authOpts := auth.CreateAPIKeyOptions{
		Name:        opts.Name,
		Scopes:      opts.Scopes,
		Labels:      opts.Labels,
		Annotations: opts.Annotations,
		ExpiresAt:   opts.ExpiresAt,
	}
	return a.service.Create(ctx, authOpts)
}

// Get retrieves an API key by ID.
func (a *APIKeyAdapter) Get(ctx context.Context, id string) (*store.APIKey, error) {
	return a.service.Get(ctx, id)
}

// List returns API keys matching the given options.
func (a *APIKeyAdapter) List(ctx context.Context, opts ListAPIKeysOptions) (*ListResult[store.APIKey], error) {
	storeOpts := store.ListAPIKeysOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Labels: opts.Labels,
	}

	keys, err := a.service.List(ctx, storeOpts)
	if err != nil {
		return nil, err
	}

	return &ListResult[store.APIKey]{
		Items: keys,
	}, nil
}

// Revoke revokes an API key with an optional reason.
func (a *APIKeyAdapter) Revoke(ctx context.Context, id, reason string) error {
	return a.service.Revoke(ctx, id, reason)
}

// Ensure APIKeyAdapter implements APIKeyService.
var _ APIKeyService = (*APIKeyAdapter)(nil)
