// Package mock provides in-memory mock implementations of store interfaces for testing.
package mock

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/chunlea/marionette/pkg/store"
)

// APIKeyStore is an in-memory mock implementation of store.APIKeyStore.
type APIKeyStore struct {
	keys   map[string]*store.APIKey // by ID
	byHash map[string]*store.APIKey // by hash
	mu     sync.RWMutex
}

// NewAPIKeyStore creates a new mock API key store.
func NewAPIKeyStore() *APIKeyStore {
	return &APIKeyStore{
		keys:   make(map[string]*store.APIKey),
		byHash: make(map[string]*store.APIKey),
	}
}

// CreateAPIKey stores a new API key.
func (s *APIKeyStore) CreateAPIKey(ctx context.Context, key *store.APIKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.keys[key.ID]; exists {
		return store.ErrAlreadyExists
	}
	if _, exists := s.byHash[key.KeyHash]; exists {
		return store.ErrAlreadyExists
	}

	// Make a copy to avoid external mutations
	keyCopy := *key
	if key.Labels != nil {
		keyCopy.Labels = make(json.RawMessage, len(key.Labels))
		copy(keyCopy.Labels, key.Labels)
	}
	if key.Annotations != nil {
		keyCopy.Annotations = make(json.RawMessage, len(key.Annotations))
		copy(keyCopy.Annotations, key.Annotations)
	}
	if key.Scopes != nil {
		keyCopy.Scopes = make([]string, len(key.Scopes))
		copy(keyCopy.Scopes, key.Scopes)
	}

	s.keys[key.ID] = &keyCopy
	s.byHash[key.KeyHash] = &keyCopy
	return nil
}

// GetAPIKeyByHash retrieves an API key by its hash.
func (s *APIKeyStore) GetAPIKeyByHash(ctx context.Context, hash string) (*store.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.byHash[hash]
	if !ok {
		return nil, store.ErrNotFound
	}
	return key, nil
}

// GetAPIKey retrieves an API key by its ID.
func (s *APIKeyStore) GetAPIKey(ctx context.Context, id string) (*store.APIKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key, ok := s.keys[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return key, nil
}

// ListAPIKeys returns API keys matching the given options.
func (s *APIKeyStore) ListAPIKeys(ctx context.Context, opts store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var result []*store.APIKey
	for _, key := range s.keys {
		// Apply filters
		if !opts.IncludeRevoked && key.RevokedAt != nil {
			continue
		}
		if !matchLabelsRaw(key.Labels, opts.Labels) {
			continue
		}
		result = append(result, key)
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

	return &store.ListResult[store.APIKey]{
		Items:      result,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

// UpdateAPIKey updates specific fields of an API key.
func (s *APIKeyStore) UpdateAPIKey(ctx context.Context, id string, updates store.APIKeyUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, ok := s.keys[id]
	if !ok {
		return store.ErrNotFound
	}

	// Apply updates
	if updates.Name != nil {
		key.Name = *updates.Name
	}
	if updates.Scopes != nil {
		key.Scopes = updates.Scopes
	}
	if updates.Labels != nil {
		key.Labels = updates.Labels
	}
	if updates.Annotations != nil {
		key.Annotations = updates.Annotations
	}
	if updates.LastUsedAt != nil {
		key.LastUsedAt = updates.LastUsedAt
	}
	if updates.RevokedAt != nil {
		key.RevokedAt = updates.RevokedAt
	}
	if updates.RevokeReason != nil {
		key.RevokeReason = updates.RevokeReason
	}

	return nil
}

// DeleteAPIKey permanently deletes an API key.
func (s *APIKeyStore) DeleteAPIKey(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key, ok := s.keys[id]
	if !ok {
		return store.ErrNotFound
	}

	delete(s.byHash, key.KeyHash)
	delete(s.keys, id)
	return nil
}

// matchLabelsRaw checks if all selector labels are present in the target labels.
func matchLabelsRaw(target json.RawMessage, selector map[string]string) bool {
	if len(selector) == 0 {
		return true
	}
	if len(target) == 0 {
		return false
	}
	var targetMap map[string]string
	if err := json.Unmarshal(target, &targetMap); err != nil {
		return false
	}
	for k, v := range selector {
		if targetMap[k] != v {
			return false
		}
	}
	return true
}

// Ensure APIKeyStore implements store.APIKeyStore.
var _ store.APIKeyStore = (*APIKeyStore)(nil)
