package admin

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// MockAPIKeyService is a mock implementation of APIKeyService for testing.
type MockAPIKeyService struct {
	mu            sync.RWMutex
	keys          map[string]*store.APIKey
	nextID        int
	internalError error
}

// NewMockAPIKeyService creates a new mock API key service.
func NewMockAPIKeyService() *MockAPIKeyService {
	return &MockAPIKeyService{
		keys: make(map[string]*store.APIKey),
	}
}

// SetInternalError sets an internal error to be returned on next operation.
func (m *MockAPIKeyService) SetInternalError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = err
}

// ClearInternalError clears the internal error.
func (m *MockAPIKeyService) ClearInternalError() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.internalError = nil
}

// Create creates a new API key.
func (m *MockAPIKeyService) Create(_ context.Context, opts CreateAPIKeyOptions) (*store.APIKey, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, "", err
	}

	if opts.Name == "" {
		return nil, "", &ValidationError{Field: "name", Message: "name is required"}
	}

	m.nextID++
	id := "key_mock" + string(rune('0'+m.nextID))

	labelsJSON := json.RawMessage("{}")
	annotationsJSON := json.RawMessage("{}")
	if opts.Labels != nil {
		labelsJSON, _ = json.Marshal(opts.Labels)
	}
	if opts.Annotations != nil {
		annotationsJSON, _ = json.Marshal(opts.Annotations)
	}

	key := &store.APIKey{
		ID:          id,
		Name:        opts.Name,
		KeyPrefix:   "mk_mock1234",
		KeyHash:     "mockhash1234567890",
		HashVersion: 1,
		Scopes:      opts.Scopes,
		Labels:      labelsJSON,
		Annotations: annotationsJSON,
		CreatedAt:   time.Now(),
		ExpiresAt:   opts.ExpiresAt,
	}

	if key.Scopes == nil {
		key.Scopes = []string{}
	}

	m.keys[id] = key

	return key, "mk_plaintext_token_mock123456789012345678", nil
}

// Get retrieves an API key by ID.
func (m *MockAPIKeyService) Get(_ context.Context, id string) (*store.APIKey, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	key, ok := m.keys[id]
	if !ok {
		return nil, store.ErrNotFound
	}

	return key, nil
}

// List returns API keys matching the given options.
func (m *MockAPIKeyService) List(_ context.Context, opts ListAPIKeysOptions) (*ListResult[store.APIKey], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return nil, err
	}

	items := make([]*store.APIKey, 0, len(m.keys))
	for _, key := range m.keys {
		if len(opts.Labels) > 0 {
			if !matchLabelsJSON(key.Labels, opts.Labels) {
				continue
			}
		}
		items = append(items, key)
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(items) > limit {
		items = items[:limit]
	}

	return &ListResult[store.APIKey]{
		Items:      items,
		TotalCount: int64(len(m.keys)),
	}, nil
}

// Revoke revokes an API key.
func (m *MockAPIKeyService) Revoke(_ context.Context, id, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.internalError != nil {
		err := m.internalError
		m.internalError = nil
		return err
	}

	key, ok := m.keys[id]
	if !ok {
		return store.ErrNotFound
	}

	now := time.Now()
	key.RevokedAt = &now
	if reason != "" {
		key.RevokeReason = &reason
	}

	return nil
}

// AddKey adds a key to the mock for testing.
func (m *MockAPIKeyService) AddKey(key *store.APIKey) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[key.ID] = key
}

// matchLabelsJSON checks if a JSON labels field contains all the required labels.
func matchLabelsJSON(labelsJSON json.RawMessage, required map[string]string) bool {
	if len(labelsJSON) == 0 || string(labelsJSON) == "{}" {
		return len(required) == 0
	}

	var labels map[string]string
	if err := json.Unmarshal(labelsJSON, &labels); err != nil {
		return false
	}

	for k, v := range required {
		if labels[k] != v {
			return false
		}
	}
	return true
}
