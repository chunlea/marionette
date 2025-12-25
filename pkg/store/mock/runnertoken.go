package mock

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// RunnerTokenStore is an in-memory mock implementation of store.RunnerTokenStore.
type RunnerTokenStore struct {
	tokens map[string]*store.RunnerToken // by ID
	byHash map[string]*store.RunnerToken // by current hash
	mu     sync.RWMutex
}

// NewRunnerTokenStore creates a new mock runner token store.
func NewRunnerTokenStore() *RunnerTokenStore {
	return &RunnerTokenStore{
		tokens: make(map[string]*store.RunnerToken),
		byHash: make(map[string]*store.RunnerToken),
	}
}

// CreateRunnerToken stores a new runner token.
func (s *RunnerTokenStore) CreateRunnerToken(ctx context.Context, token *store.RunnerToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.tokens[token.ID]; exists {
		return store.ErrAlreadyExists
	}
	if _, exists := s.byHash[token.TokenHash]; exists {
		return store.ErrAlreadyExists
	}

	// Make a copy to avoid external mutations
	tokenCopy := *token
	if token.Labels != nil {
		tokenCopy.Labels = make(json.RawMessage, len(token.Labels))
		copy(tokenCopy.Labels, token.Labels)
	}

	s.tokens[token.ID] = &tokenCopy
	s.byHash[token.TokenHash] = &tokenCopy
	return nil
}

// GetRunnerTokenByHash retrieves a runner token by its hash.
// Also checks the previous hash if the token is in rotation.
func (s *RunnerTokenStore) GetRunnerTokenByHash(ctx context.Context, hash string) (*store.RunnerToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Check current hash first
	if token, ok := s.byHash[hash]; ok {
		return token, nil
	}

	// Check previous hash for tokens in rotation
	for _, token := range s.tokens {
		if token.PreviousTokenHash != nil && *token.PreviousTokenHash == hash {
			// Check if rotation is still valid
			if token.RotationDeadline != nil && time.Now().Before(*token.RotationDeadline) {
				return token, nil
			}
		}
	}

	return nil, store.ErrNotFound
}

// GetRunnerToken retrieves a runner token by its ID.
func (s *RunnerTokenStore) GetRunnerToken(ctx context.Context, id string) (*store.RunnerToken, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	token, ok := s.tokens[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return token, nil
}

// ListRunnerTokens returns runner tokens matching the given options.
func (s *RunnerTokenStore) ListRunnerTokens(ctx context.Context, opts store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*store.RunnerToken
	for _, token := range s.tokens {
		// Apply filters
		if opts.PoolName != nil && token.PoolName != *opts.PoolName {
			continue
		}
		if opts.RunnerID != nil && (token.RunnerID == nil || *token.RunnerID != *opts.RunnerID) {
			continue
		}
		if !opts.IncludeRevoked && token.RevokedAt != nil {
			continue
		}
		if len(opts.Status) > 0 && !containsStatus(opts.Status, token.Status) {
			continue
		}
		result = append(result, token)
	}

	totalCount := int64(len(result))

	// Apply pagination
	limit := opts.Limit
	if limit <= 0 {
		limit = 50 // Default limit
	}
	hasMore := len(result) > limit
	if limit > 0 && limit < len(result) {
		result = result[:limit]
	}

	return &store.ListResult[store.RunnerToken]{
		Items:      result,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// containsStatus checks if status is in the list.
func containsStatus(list []string, status string) bool {
	for _, s := range list {
		if s == status {
			return true
		}
	}
	return false
}

// UpdateRunnerToken updates specific fields of a runner token.
func (s *RunnerTokenStore) UpdateRunnerToken(ctx context.Context, id string, updates store.RunnerTokenUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.tokens[id]
	if !ok {
		return store.ErrNotFound
	}

	// Track if hash changes for index update
	oldHash := token.TokenHash

	// Apply updates - note: we need to copy values, not pointers, to avoid
	// aliasing issues where the same object is modified multiple times
	if updates.PreviousTokenHash != nil {
		if *updates.PreviousTokenHash == "" {
			token.PreviousTokenHash = nil
		} else {
			v := *updates.PreviousTokenHash // Copy the value
			token.PreviousTokenHash = &v
		}
	}
	if updates.TokenHash != nil {
		token.TokenHash = *updates.TokenHash
	}
	if updates.TokenPrefix != nil {
		token.TokenPrefix = *updates.TokenPrefix
	}
	if updates.RunnerID != nil {
		if *updates.RunnerID == "" {
			token.RunnerID = nil
		} else {
			v := *updates.RunnerID // Copy the value
			token.RunnerID = &v
		}
	}
	if updates.Status != nil {
		token.Status = *updates.Status
	}
	if updates.RotationDeadline != nil {
		token.RotationDeadline = updates.RotationDeadline
	}
	if updates.Labels != nil {
		token.Labels = updates.Labels
	}
	if updates.LastUsedAt != nil {
		token.LastUsedAt = updates.LastUsedAt
	}
	if updates.RevokedAt != nil {
		token.RevokedAt = updates.RevokedAt
	}
	if updates.RevokeReason != nil {
		token.RevokeReason = updates.RevokeReason
	}

	// Update hash index if changed
	if token.TokenHash != oldHash {
		delete(s.byHash, oldHash)
		s.byHash[token.TokenHash] = token
	}

	return nil
}

// DeleteRunnerToken permanently deletes a runner token.
func (s *RunnerTokenStore) DeleteRunnerToken(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.tokens[id]
	if !ok {
		return store.ErrNotFound
	}

	delete(s.byHash, token.TokenHash)
	delete(s.tokens, id)
	return nil
}

// Ensure RunnerTokenStore implements store.RunnerTokenStore.
var _ store.RunnerTokenStore = (*RunnerTokenStore)(nil)
