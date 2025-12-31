package admin

import (
	"context"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/store"
)

// mockAPIKeyStore implements store.APIKeyStore for testing.
type mockAPIKeyStore struct {
	keys map[string]*store.APIKey
}

func newMockAPIKeyStore() *mockAPIKeyStore {
	return &mockAPIKeyStore{
		keys: make(map[string]*store.APIKey),
	}
}

func (s *mockAPIKeyStore) CreateAPIKey(ctx context.Context, key *store.APIKey) error {
	s.keys[key.ID] = key
	return nil
}

func (s *mockAPIKeyStore) GetAPIKey(ctx context.Context, id string) (*store.APIKey, error) {
	key, ok := s.keys[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return key, nil
}

func (s *mockAPIKeyStore) GetAPIKeyByHash(ctx context.Context, hash string) (*store.APIKey, error) {
	for _, key := range s.keys {
		if key.KeyHash == hash {
			return key, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *mockAPIKeyStore) ListAPIKeys(ctx context.Context, opts store.ListAPIKeysOptions) (*store.ListResult[store.APIKey], error) {
	var items []*store.APIKey
	for _, key := range s.keys {
		items = append(items, key)
	}
	return &store.ListResult[store.APIKey]{Items: items}, nil
}

func (s *mockAPIKeyStore) UpdateAPIKey(ctx context.Context, id string, updates store.APIKeyUpdates) error {
	key, ok := s.keys[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.RevokedAt != nil {
		key.RevokedAt = updates.RevokedAt
	}
	if updates.RevokeReason != nil {
		key.RevokeReason = updates.RevokeReason
	}
	if updates.LastUsedAt != nil {
		key.LastUsedAt = updates.LastUsedAt
	}
	return nil
}

func (s *mockAPIKeyStore) DeleteAPIKey(ctx context.Context, id string) error {
	delete(s.keys, id)
	return nil
}

func testIDGen() string {
	return "key_test123"
}

func TestNewAPIKeyAdapter(t *testing.T) {
	st := newMockAPIKeyStore()
	svc := auth.NewAPIKeyService(st, testIDGen)

	adapter := NewAPIKeyAdapter(svc)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.service != svc {
		t.Error("service not set correctly")
	}
}

func TestAPIKeyAdapter_Create(t *testing.T) {
	st := newMockAPIKeyStore()
	svc := auth.NewAPIKeyService(st, testIDGen)
	adapter := NewAPIKeyAdapter(svc)

	ctx := context.Background()
	key, token, err := adapter.Create(ctx, CreateAPIKeyOptions{
		Name:   "test-key",
		Scopes: []string{"sessions:*"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}
	if key.Name != "test-key" {
		t.Errorf("expected name 'test-key', got %q", key.Name)
	}
	if len(key.Scopes) != 1 || key.Scopes[0] != "sessions:*" {
		t.Errorf("expected scopes ['sessions:*'], got %v", key.Scopes)
	}
}

func TestAPIKeyAdapter_Create_WithLabels(t *testing.T) {
	st := newMockAPIKeyStore()
	svc := auth.NewAPIKeyService(st, testIDGen)
	adapter := NewAPIKeyAdapter(svc)

	ctx := context.Background()
	key, _, err := adapter.Create(ctx, CreateAPIKeyOptions{
		Name:   "labeled-key",
		Labels: map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key == nil {
		t.Fatal("expected non-nil key")
	}
}

func TestAPIKeyAdapter_Create_WithExpiry(t *testing.T) {
	st := newMockAPIKeyStore()
	svc := auth.NewAPIKeyService(st, testIDGen)
	adapter := NewAPIKeyAdapter(svc)

	ctx := context.Background()
	expiresAt := time.Now().Add(24 * time.Hour)
	key, _, err := adapter.Create(ctx, CreateAPIKeyOptions{
		Name:      "expiring-key",
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key.ExpiresAt == nil {
		t.Fatal("expected non-nil ExpiresAt")
	}
}

func TestAPIKeyAdapter_Get(t *testing.T) {
	st := newMockAPIKeyStore()
	svc := auth.NewAPIKeyService(st, testIDGen)
	adapter := NewAPIKeyAdapter(svc)

	ctx := context.Background()

	// Create a key first
	created, _, _ := adapter.Create(ctx, CreateAPIKeyOptions{Name: "test-key"})

	// Get the key
	got, err := adapter.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, got.ID)
	}
}

func TestAPIKeyAdapter_Get_NotFound(t *testing.T) {
	st := newMockAPIKeyStore()
	svc := auth.NewAPIKeyService(st, testIDGen)
	adapter := NewAPIKeyAdapter(svc)

	ctx := context.Background()
	_, err := adapter.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}

func TestAPIKeyAdapter_List(t *testing.T) {
	st := newMockAPIKeyStore()
	// Use a counter for unique IDs
	counter := 0
	idGen := func() string {
		counter++
		return "key_" + string(rune('a'+counter))
	}
	svc := auth.NewAPIKeyService(st, idGen)
	adapter := NewAPIKeyAdapter(svc)

	ctx := context.Background()

	// Create some keys
	_, _ = adapter.Create(ctx, CreateAPIKeyOptions{Name: "key-1"})
	_, _ = adapter.Create(ctx, CreateAPIKeyOptions{Name: "key-2"})

	// List keys
	result, err := adapter.List(ctx, ListAPIKeysOptions{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 keys, got %d", len(result.Items))
	}
}

func TestAPIKeyAdapter_Revoke(t *testing.T) {
	st := newMockAPIKeyStore()
	svc := auth.NewAPIKeyService(st, testIDGen)
	adapter := NewAPIKeyAdapter(svc)

	ctx := context.Background()

	// Create a key
	key, _, _ := adapter.Create(ctx, CreateAPIKeyOptions{Name: "test-key"})

	// Revoke it
	err := adapter.Revoke(ctx, key.ID, "no longer needed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's revoked
	got, _ := adapter.Get(ctx, key.ID)
	if got.RevokedAt == nil {
		t.Error("expected key to be revoked")
	}
}

func TestAPIKeyAdapter_Revoke_NotFound(t *testing.T) {
	st := newMockAPIKeyStore()
	svc := auth.NewAPIKeyService(st, testIDGen)
	adapter := NewAPIKeyAdapter(svc)

	ctx := context.Background()
	err := adapter.Revoke(ctx, "nonexistent", "reason")
	if err == nil {
		t.Fatal("expected error for nonexistent key")
	}
}

// Verify APIKeyAdapter implements APIKeyService interface
var _ APIKeyService = (*APIKeyAdapter)(nil)
