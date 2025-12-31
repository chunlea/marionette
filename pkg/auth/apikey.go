package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/chunlea/marionette/pkg/cryptoutil"
	"github.com/chunlea/marionette/pkg/store"
)

// APIKeyService manages API key lifecycle.
type APIKeyService struct {
	store store.APIKeyStore
	idGen func() string
}

// NewAPIKeyService creates a new API key service.
//
// Parameters:
//   - store: The API key store for persistence
//   - idGen: Function to generate key IDs (e.g., id.APIKey)
func NewAPIKeyService(store store.APIKeyStore, idGen func() string) *APIKeyService {
	return &APIKeyService{
		store: store,
		idGen: idGen,
	}
}

// CreateAPIKeyOptions defines options for creating an API key.
type CreateAPIKeyOptions struct {
	Name        string            // Human-readable name (required)
	Scopes      []string          // Permission scopes
	TenantID    *string           // Tenant isolation
	Labels      map[string]string // Key-value labels
	Annotations map[string]string // Key-value annotations
	ExpiresAt   *time.Time        // Optional expiration time
	CreatedBy   *string           // Who created this key
}

// Create generates a new API key.
// Returns the created key record AND the plaintext token (shown once).
func (s *APIKeyService) Create(ctx context.Context, opts CreateAPIKeyOptions) (*store.APIKey, string, error) {
	// Validate input
	if strings.TrimSpace(opts.Name) == "" {
		return nil, "", ErrInvalidName
	}

	// Generate token
	token, displayPrefix, hash, version, err := cryptoutil.GenerateAPIKey()
	if err != nil {
		return nil, "", err
	}

	// Generate ID
	id := ""
	if s.idGen != nil {
		id = s.idGen()
	}

	// Marshal labels and annotations to JSON
	var labelsJSON, annotationsJSON json.RawMessage
	if opts.Labels != nil {
		labelsJSON, _ = json.Marshal(opts.Labels)
	} else {
		labelsJSON = json.RawMessage("{}")
	}
	if opts.Annotations != nil {
		annotationsJSON, _ = json.Marshal(opts.Annotations)
	} else {
		annotationsJSON = json.RawMessage("{}")
	}

	// Create key record
	key := &store.APIKey{
		ID:          id,
		Name:        strings.TrimSpace(opts.Name),
		KeyHash:     hash,
		KeyPrefix:   displayPrefix,
		HashVersion: version,
		Scopes:      opts.Scopes,
		TenantID:    opts.TenantID,
		Labels:      labelsJSON,
		Annotations: annotationsJSON,
		CreatedAt:   time.Now(),
		CreatedBy:   opts.CreatedBy,
		ExpiresAt:   opts.ExpiresAt,
	}

	// Initialize empty scopes if nil
	if key.Scopes == nil {
		key.Scopes = []string{}
	}

	// Store the key
	if err := s.store.CreateAPIKey(ctx, key); err != nil {
		return nil, "", err
	}

	return key, token, nil
}

// Validate checks a token and returns the API key info if valid.
func (s *APIKeyService) Validate(ctx context.Context, token string) (*store.APIKey, error) {
	// First check prefix to provide specific error for wrong token type
	prefix := cryptoutil.ExtractPrefix(token)
	if prefix != cryptoutil.PrefixAPIKey {
		// Check if it's a valid prefix but wrong type
		if prefix == cryptoutil.PrefixRunnerToken || prefix == cryptoutil.PrefixTunnelToken {
			return nil, ErrInvalidPrefix
		}
		return nil, ErrInvalidToken
	}

	// Validate token format (length, characters)
	if !cryptoutil.ValidateTokenFormat(token, cryptoutil.PrefixAPIKey) {
		return nil, ErrInvalidToken
	}

	// Hash the token
	hash := cryptoutil.HashToken(token)

	// Look up by hash
	key, err := s.store.GetAPIKeyByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}

	// Check if revoked
	if key.RevokedAt != nil {
		return nil, ErrTokenRevoked
	}

	// Check if expired
	if key.ExpiresAt != nil && time.Now().After(*key.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	return key, nil
}

// Get retrieves an API key by ID.
func (s *APIKeyService) Get(ctx context.Context, id string) (*store.APIKey, error) {
	key, err := s.store.GetAPIKey(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}
	return key, nil
}

// List returns API keys matching options.
func (s *APIKeyService) List(ctx context.Context, opts store.ListAPIKeysOptions) ([]*store.APIKey, error) {
	result, err := s.store.ListAPIKeys(ctx, opts)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// Revoke revokes an API key.
func (s *APIKeyService) Revoke(ctx context.Context, id, reason string) error {
	// Verify key exists
	_, err := s.store.GetAPIKey(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTokenNotFound
		}
		return err
	}

	now := time.Now()
	updates := store.APIKeyUpdates{
		RevokedAt: &now,
	}
	if reason != "" {
		updates.RevokeReason = &reason
	}

	return s.store.UpdateAPIKey(ctx, id, updates)
}

// UpdateLastUsed updates the last_used_at timestamp.
func (s *APIKeyService) UpdateLastUsed(ctx context.Context, id string) error {
	now := time.Now()
	return s.store.UpdateAPIKey(ctx, id, store.APIKeyUpdates{
		LastUsedAt: &now,
	})
}

// HasScope checks if an API key has a specific scope.
// Supports wildcard matching (e.g., "tasks:*" matches "tasks:read").
func HasScope(key *store.APIKey, requiredScope string) bool {
	if key == nil {
		return false
	}

	for _, scope := range key.Scopes {
		// Exact match
		if scope == requiredScope {
			return true
		}

		// Wildcard match: "tasks:*" matches "tasks:read", "tasks:write", etc.
		if strings.HasSuffix(scope, ":*") {
			prefix := strings.TrimSuffix(scope, "*")
			if strings.HasPrefix(requiredScope, prefix) {
				return true
			}
		}

		// Full wildcard
		if scope == "*" {
			return true
		}
	}

	return false
}

// HasAnyScope checks if an API key has any of the required scopes.
func HasAnyScope(key *store.APIKey, requiredScopes ...string) bool {
	for _, scope := range requiredScopes {
		if HasScope(key, scope) {
			return true
		}
	}
	return false
}

// HasAllScopes checks if an API key has all of the required scopes.
func HasAllScopes(key *store.APIKey, requiredScopes ...string) bool {
	for _, scope := range requiredScopes {
		if !HasScope(key, scope) {
			return false
		}
	}
	return true
}
