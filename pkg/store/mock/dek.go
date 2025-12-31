package mock

import (
	"context"
	"sync"

	"github.com/chunlea/marionette/pkg/cryptoutil"
)

// DEKStore is an in-memory mock implementation of cryptoutil.DEKStore.
type DEKStore struct {
	keys map[string]*cryptoutil.DataKey // keyed by "resourceType:resourceID"
	mu   sync.RWMutex
}

// NewDEKStore creates a new mock DEK store.
func NewDEKStore() *DEKStore {
	return &DEKStore{
		keys: make(map[string]*cryptoutil.DataKey),
	}
}

// makeKey creates the composite key for storage.
func makeKey(resourceType, resourceID string) string {
	return resourceType + ":" + resourceID
}

// GetDEK retrieves a DEK by resource type and ID.
func (s *DEKStore) GetDEK(ctx context.Context, resourceType, resourceID string) (*cryptoutil.DataKey, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	key := makeKey(resourceType, resourceID)
	dk, ok := s.keys[key]
	if !ok {
		return nil, cryptoutil.ErrDEKNotFound
	}
	return dk, nil
}

// CreateDEK stores a new encrypted DEK.
func (s *DEKStore) CreateDEK(ctx context.Context, dk *cryptoutil.DataKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := makeKey(dk.ResourceType, dk.ResourceID)

	// Make a copy
	dkCopy := *dk
	s.keys[key] = &dkCopy
	return nil
}

// UpdateDEK updates an existing DEK (for rotation).
func (s *DEKStore) UpdateDEK(ctx context.Context, dk *cryptoutil.DataKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := makeKey(dk.ResourceType, dk.ResourceID)
	if _, exists := s.keys[key]; !exists {
		return cryptoutil.ErrDEKNotFound
	}

	// Make a copy
	dkCopy := *dk
	s.keys[key] = &dkCopy
	return nil
}

// Ensure DEKStore implements cryptoutil.DEKStore.
var _ cryptoutil.DEKStore = (*DEKStore)(nil)
