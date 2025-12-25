package store

import (
	"context"

	"github.com/chunlea/marionette/pkg/crypto"
)

// APIKeyStore defines operations for API key storage.
// This is a subset of Store for use by the auth package.
// The postgres.Store implements this interface.
type APIKeyStore interface {
	// CreateAPIKey stores a new API key.
	CreateAPIKey(ctx context.Context, key *APIKey) error

	// GetAPIKeyByHash retrieves an API key by its hash.
	// Returns ErrNotFound if the key does not exist.
	GetAPIKeyByHash(ctx context.Context, hash string) (*APIKey, error)

	// GetAPIKey retrieves an API key by its ID.
	// Returns ErrNotFound if the key does not exist.
	GetAPIKey(ctx context.Context, id string) (*APIKey, error)

	// ListAPIKeys returns API keys matching the given options.
	ListAPIKeys(ctx context.Context, opts ListAPIKeysOptions) (*ListResult[APIKey], error)

	// UpdateAPIKey updates specific fields of an API key.
	UpdateAPIKey(ctx context.Context, id string, updates APIKeyUpdates) error

	// DeleteAPIKey permanently deletes an API key.
	DeleteAPIKey(ctx context.Context, id string) error
}

// RunnerTokenStore defines operations for runner token storage.
// The postgres.Store implements this interface.
type RunnerTokenStore interface {
	// CreateRunnerToken stores a new runner token.
	CreateRunnerToken(ctx context.Context, token *RunnerToken) error

	// GetRunnerTokenByHash retrieves a runner token by its hash.
	// Also checks the previous hash if the token is in rotation.
	// Returns ErrNotFound if the token does not exist.
	GetRunnerTokenByHash(ctx context.Context, hash string) (*RunnerToken, error)

	// GetRunnerToken retrieves a runner token by its ID.
	// Returns ErrNotFound if the token does not exist.
	GetRunnerToken(ctx context.Context, id string) (*RunnerToken, error)

	// ListRunnerTokens returns runner tokens matching the given options.
	ListRunnerTokens(ctx context.Context, opts ListRunnerTokensOptions) (*ListResult[RunnerToken], error)

	// UpdateRunnerToken updates specific fields of a runner token.
	UpdateRunnerToken(ctx context.Context, id string, updates RunnerTokenUpdates) error

	// DeleteRunnerToken permanently deletes a runner token.
	DeleteRunnerToken(ctx context.Context, id string) error
}

// DEKStore defines operations for data encryption key storage.
// This interface is also defined in pkg/crypto but we re-export it here
// for consistency with other store interfaces.
type DEKStore = crypto.DEKStore
