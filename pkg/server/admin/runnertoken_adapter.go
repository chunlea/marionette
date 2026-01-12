package admin

import (
	"context"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/store"
)

// RunnerTokenAdapter adapts auth.RunnerTokenService to admin.RunnerTokenAdminService.
type RunnerTokenAdapter struct {
	service *auth.RunnerTokenService
}

// NewRunnerTokenAdapter creates a new RunnerTokenAdapter.
func NewRunnerTokenAdapter(service *auth.RunnerTokenService) *RunnerTokenAdapter {
	return &RunnerTokenAdapter{
		service: service,
	}
}

// Create creates a new runner token and returns the token and plaintext secret.
func (a *RunnerTokenAdapter) Create(ctx context.Context, opts CreateRunnerTokenOptions) (*store.RunnerToken, string, error) {
	authOpts := auth.CreateRunnerTokenOptions{
		PoolName:  opts.PoolName,
		Labels:    opts.Labels,
		ExpiresAt: opts.ExpiresAt,
	}
	return a.service.Create(ctx, authOpts)
}

// Get retrieves a runner token by ID.
func (a *RunnerTokenAdapter) Get(ctx context.Context, id string) (*store.RunnerToken, error) {
	return a.service.Get(ctx, id)
}

// List returns runner tokens matching the given options.
func (a *RunnerTokenAdapter) List(ctx context.Context, opts ListRunnerTokensOptions) (*ListResult[store.RunnerToken], error) {
	// Convert admin options to store options
	storeOpts := store.ListRunnerTokensOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Status:         opts.Status,
		IncludeRevoked: opts.IncludeRevoked,
	}

	// Convert pool_name filter
	if opts.PoolName != "" {
		storeOpts.PoolName = &opts.PoolName
	}

	tokens, err := a.service.List(ctx, storeOpts)
	if err != nil {
		return nil, err
	}

	return &ListResult[store.RunnerToken]{
		Items: tokens,
	}, nil
}

// Revoke revokes a runner token with an optional reason.
func (a *RunnerTokenAdapter) Revoke(ctx context.Context, id, reason string) error {
	return a.service.Revoke(ctx, id, reason)
}

// Rotate rotates a runner token and returns the new plaintext secret.
func (a *RunnerTokenAdapter) Rotate(ctx context.Context, id string) (*store.RunnerToken, string, error) {
	newToken, err := a.service.Rotate(ctx, id)
	if err != nil {
		return nil, "", err
	}

	// Get the updated token record
	token, err := a.service.Get(ctx, id)
	if err != nil {
		return nil, "", err
	}

	return token, newToken, nil
}

// Ensure RunnerTokenAdapter implements RunnerTokenAdminService.
var _ RunnerTokenAdminService = (*RunnerTokenAdapter)(nil)
