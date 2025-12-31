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

// DefaultRotationWindow is the default duration during which both old and new tokens are valid.
const DefaultRotationWindow = 1 * time.Hour

// Token status constants.
const (
	TokenStatusActive   = "active"
	TokenStatusRotating = "rotating"
	TokenStatusRevoked  = "revoked"
	TokenStatusExpired  = "expired"
)

// RunnerTokenService manages runner token lifecycle.
type RunnerTokenService struct {
	store          store.RunnerTokenStore
	idGen          func() string
	rotationWindow time.Duration
}

// NewRunnerTokenService creates a new runner token service.
//
// Parameters:
//   - store: The runner token store for persistence
//   - idGen: Function to generate token IDs (e.g., id.RunnerToken)
func NewRunnerTokenService(store store.RunnerTokenStore, idGen func() string) *RunnerTokenService {
	return &RunnerTokenService{
		store:          store,
		idGen:          idGen,
		rotationWindow: DefaultRotationWindow,
	}
}

// WithRotationWindow sets the rotation window duration.
func (s *RunnerTokenService) WithRotationWindow(d time.Duration) *RunnerTokenService {
	s.rotationWindow = d
	return s
}

// CreateRunnerTokenOptions defines options for creating a runner token.
type CreateRunnerTokenOptions struct {
	PoolName  string            // Pool this token belongs to (required)
	TenantID  *string           // Tenant isolation
	Labels    map[string]string // Key-value labels
	ExpiresAt *time.Time        // Optional expiration time
	CreatedBy *string           // Who created this token
}

// Create generates a new runner token for a pool.
// Returns the created token record AND the plaintext token (shown once).
func (s *RunnerTokenService) Create(ctx context.Context, opts CreateRunnerTokenOptions) (*store.RunnerToken, string, error) {
	// Validate input
	if strings.TrimSpace(opts.PoolName) == "" {
		return nil, "", ErrInvalidPoolName
	}

	// Generate token
	token, displayPrefix, hash, version, err := cryptoutil.GenerateRunnerToken()
	if err != nil {
		return nil, "", err
	}

	// Generate ID
	id := ""
	if s.idGen != nil {
		id = s.idGen()
	}

	// Marshal labels to JSON
	var labelsJSON json.RawMessage
	if opts.Labels != nil {
		labelsJSON, _ = json.Marshal(opts.Labels)
	} else {
		labelsJSON = json.RawMessage("{}")
	}

	// Create token record
	rt := &store.RunnerToken{
		ID:          id,
		TokenHash:   hash,
		TokenPrefix: displayPrefix,
		HashVersion: version,
		PoolName:    strings.TrimSpace(opts.PoolName),
		Status:      TokenStatusActive,
		TenantID:    opts.TenantID,
		Labels:      labelsJSON,
		CreatedAt:   time.Now(),
		CreatedBy:   opts.CreatedBy,
		ExpiresAt:   opts.ExpiresAt,
	}

	// Store the token
	if err := s.store.CreateRunnerToken(ctx, rt); err != nil {
		return nil, "", err
	}

	return rt, token, nil
}

// Validate checks a token and returns the runner token info if valid.
// Supports both current and previous hash during rotation window.
func (s *RunnerTokenService) Validate(ctx context.Context, token string) (*store.RunnerToken, error) {
	// First check prefix to provide specific error for wrong token type
	prefix := cryptoutil.ExtractPrefix(token)
	if prefix != cryptoutil.PrefixRunnerToken {
		// Check if it's a valid prefix but wrong type
		if prefix == cryptoutil.PrefixAPIKey || prefix == cryptoutil.PrefixTunnelToken {
			return nil, ErrInvalidPrefix
		}
		return nil, ErrInvalidToken
	}

	// Validate token format (length, characters)
	if !cryptoutil.ValidateTokenFormat(token, cryptoutil.PrefixRunnerToken) {
		return nil, ErrInvalidToken
	}

	// Hash the token
	hash := cryptoutil.HashToken(token)

	// Look up by hash (store handles both current and previous hash)
	rt, err := s.store.GetRunnerTokenByHash(ctx, hash)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}

	// Check if revoked
	if rt.Status == TokenStatusRevoked || rt.RevokedAt != nil {
		return nil, ErrTokenRevoked
	}

	// Check if expired
	if rt.Status == TokenStatusExpired {
		return nil, ErrTokenExpired
	}
	if rt.ExpiresAt != nil && time.Now().After(*rt.ExpiresAt) {
		return nil, ErrTokenExpired
	}

	return rt, nil
}

// Rotate starts token rotation, returns new plaintext token.
// Old token remains valid until rotation_deadline.
func (s *RunnerTokenService) Rotate(ctx context.Context, id string) (string, error) {
	// Get existing token
	existing, err := s.store.GetRunnerToken(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrTokenNotFound
		}
		return "", err
	}

	// Check if already rotating
	if existing.Status == TokenStatusRotating && existing.RotationDeadline != nil {
		if time.Now().Before(*existing.RotationDeadline) {
			return "", errors.New("rotation already in progress")
		}
	}

	// Check if revoked
	if existing.Status == TokenStatusRevoked || existing.RevokedAt != nil {
		return "", ErrTokenRevoked
	}

	// Generate new token
	newToken, newDisplayPrefix, newHash, _, err := cryptoutil.GenerateRunnerToken()
	if err != nil {
		return "", err
	}

	// Calculate rotation deadline
	deadline := time.Now().Add(s.rotationWindow)
	status := TokenStatusRotating

	// Update token with rotation info
	updates := store.RunnerTokenUpdates{
		PreviousTokenHash: &existing.TokenHash,
		TokenHash:         &newHash,
		TokenPrefix:       &newDisplayPrefix,
		Status:            &status,
		RotationDeadline:  &deadline,
	}

	if err := s.store.UpdateRunnerToken(ctx, id, updates); err != nil {
		return "", err
	}

	return newToken, nil
}

// CompleteRotation clears previous token after rotation window.
// This should be called after rotation_deadline has passed.
func (s *RunnerTokenService) CompleteRotation(ctx context.Context, id string) error {
	token, err := s.store.GetRunnerToken(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTokenNotFound
		}
		return err
	}

	// Only complete if in rotating status
	if token.Status != TokenStatusRotating {
		return nil // Already completed or not rotating
	}

	// Check if deadline has passed
	if token.RotationDeadline != nil && time.Now().Before(*token.RotationDeadline) {
		return errors.New("rotation window has not expired")
	}

	// Clear rotation state
	status := TokenStatusActive
	emptyHash := "" // Use empty string to clear
	updates := store.RunnerTokenUpdates{
		PreviousTokenHash: &emptyHash,
		RotationDeadline:  nil, // Setting to nil won't update, need special handling
		Status:            &status,
	}

	return s.store.UpdateRunnerToken(ctx, id, updates)
}

// BindRunner associates a token with a runner after first use.
func (s *RunnerTokenService) BindRunner(ctx context.Context, tokenID, runnerID string) error {
	// Verify token exists
	token, err := s.store.GetRunnerToken(ctx, tokenID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTokenNotFound
		}
		return err
	}

	// Check if already bound
	if token.RunnerID != nil && *token.RunnerID != runnerID {
		return errors.New("token already bound to different runner")
	}

	return s.store.UpdateRunnerToken(ctx, tokenID, store.RunnerTokenUpdates{
		RunnerID: &runnerID,
	})
}

// UnbindRunner removes the runner association from a token.
func (s *RunnerTokenService) UnbindRunner(ctx context.Context, tokenID string) error {
	_, err := s.store.GetRunnerToken(ctx, tokenID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTokenNotFound
		}
		return err
	}

	// Use empty string to clear runner association
	emptyRunnerID := ""
	return s.store.UpdateRunnerToken(ctx, tokenID, store.RunnerTokenUpdates{
		RunnerID: &emptyRunnerID,
	})
}

// Get retrieves a runner token by ID.
func (s *RunnerTokenService) Get(ctx context.Context, id string) (*store.RunnerToken, error) {
	token, err := s.store.GetRunnerToken(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrTokenNotFound
		}
		return nil, err
	}
	return token, nil
}

// List returns runner tokens matching options.
func (s *RunnerTokenService) List(ctx context.Context, opts store.ListRunnerTokensOptions) ([]*store.RunnerToken, error) {
	result, err := s.store.ListRunnerTokens(ctx, opts)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// Revoke revokes a runner token.
func (s *RunnerTokenService) Revoke(ctx context.Context, id, reason string) error {
	// Verify token exists
	_, err := s.store.GetRunnerToken(ctx, id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrTokenNotFound
		}
		return err
	}

	now := time.Now()
	status := TokenStatusRevoked
	updates := store.RunnerTokenUpdates{
		Status:    &status,
		RevokedAt: &now,
	}
	if reason != "" {
		updates.RevokeReason = &reason
	}

	return s.store.UpdateRunnerToken(ctx, id, updates)
}

// UpdateLastUsed updates the last_used_at timestamp.
func (s *RunnerTokenService) UpdateLastUsed(ctx context.Context, id string) error {
	now := time.Now()
	return s.store.UpdateRunnerToken(ctx, id, store.RunnerTokenUpdates{
		LastUsedAt: &now,
	})
}
