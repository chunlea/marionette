package admin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/store"
)

// mockRunnerTokenStore implements store.RunnerTokenStore for testing.
type mockRunnerTokenStore struct {
	tokens map[string]*store.RunnerToken
}

func newMockRunnerTokenStore() *mockRunnerTokenStore {
	return &mockRunnerTokenStore{
		tokens: make(map[string]*store.RunnerToken),
	}
}

func (s *mockRunnerTokenStore) CreateRunnerToken(ctx context.Context, token *store.RunnerToken) error {
	s.tokens[token.ID] = token
	return nil
}

func (s *mockRunnerTokenStore) GetRunnerToken(ctx context.Context, id string) (*store.RunnerToken, error) {
	token, ok := s.tokens[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return token, nil
}

func (s *mockRunnerTokenStore) GetRunnerTokenByHash(ctx context.Context, hash string) (*store.RunnerToken, error) {
	for _, token := range s.tokens {
		if token.TokenHash == hash {
			return token, nil
		}
		if token.PreviousTokenHash != nil && *token.PreviousTokenHash == hash {
			return token, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *mockRunnerTokenStore) ListRunnerTokens(ctx context.Context, opts store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	var items []*store.RunnerToken
	for _, token := range s.tokens {
		// Filter by status if specified
		if !opts.IncludeRevoked && token.Status == "revoked" {
			continue
		}
		// Filter by pool_name if specified
		if opts.PoolName != nil && token.PoolName != *opts.PoolName {
			continue
		}
		items = append(items, token)
	}
	return &store.ListResult[store.RunnerToken]{Items: items}, nil
}

func (s *mockRunnerTokenStore) UpdateRunnerToken(ctx context.Context, id string, updates store.RunnerTokenUpdates) error {
	token, ok := s.tokens[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		token.Status = *updates.Status
	}
	if updates.RevokedAt != nil {
		token.RevokedAt = updates.RevokedAt
	}
	if updates.RevokeReason != nil {
		token.RevokeReason = updates.RevokeReason
	}
	if updates.TokenHash != nil {
		token.TokenHash = *updates.TokenHash
	}
	if updates.TokenPrefix != nil {
		token.TokenPrefix = *updates.TokenPrefix
	}
	if updates.PreviousTokenHash != nil {
		token.PreviousTokenHash = updates.PreviousTokenHash
	}
	if updates.RotationDeadline != nil {
		token.RotationDeadline = updates.RotationDeadline
	}
	if updates.LastUsedAt != nil {
		token.LastUsedAt = updates.LastUsedAt
	}
	return nil
}

func (s *mockRunnerTokenStore) DeleteRunnerToken(ctx context.Context, id string) error {
	delete(s.tokens, id)
	return nil
}

func testRunnerTokenIDGen() string {
	return "rtok_test123"
}

func TestNewRunnerTokenAdapter(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)

	adapter := NewRunnerTokenAdapter(svc)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.service != svc {
		t.Error("service not set correctly")
	}
}

func TestRunnerTokenAdapter_Create(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()
	token, rawToken, err := adapter.Create(ctx, CreateRunnerTokenOptions{
		PoolName: "default",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == nil {
		t.Fatal("expected non-nil token")
	}
	if rawToken == "" {
		t.Fatal("expected non-empty raw token")
	}
	if token.PoolName != "default" {
		t.Errorf("expected pool name 'default', got %q", token.PoolName)
	}
	if token.Status != "active" {
		t.Errorf("expected status 'active', got %q", token.Status)
	}
}

func TestRunnerTokenAdapter_Create_WithLabels(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()
	token, _, err := adapter.Create(ctx, CreateRunnerTokenOptions{
		PoolName: "labeled-pool",
		Labels:   map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token == nil {
		t.Fatal("expected non-nil token")
	}

	// Verify labels are stored
	var labels map[string]string
	if err := json.Unmarshal(token.Labels, &labels); err != nil {
		t.Fatalf("failed to unmarshal labels: %v", err)
	}
	if labels["env"] != "test" {
		t.Errorf("expected label env=test, got %v", labels)
	}
}

func TestRunnerTokenAdapter_Create_WithExpiry(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()
	expiresAt := time.Now().Add(24 * time.Hour)
	token, _, err := adapter.Create(ctx, CreateRunnerTokenOptions{
		PoolName:  "expiring-pool",
		ExpiresAt: &expiresAt,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if token.ExpiresAt == nil {
		t.Fatal("expected non-nil ExpiresAt")
	}
}

func TestRunnerTokenAdapter_Create_EmptyPoolName(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()
	_, _, err := adapter.Create(ctx, CreateRunnerTokenOptions{
		PoolName: "",
	})
	if err == nil {
		t.Fatal("expected error for empty pool name")
	}
}

func TestRunnerTokenAdapter_Get(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()

	// Create a token first
	created, _, _ := adapter.Create(ctx, CreateRunnerTokenOptions{PoolName: "test-pool"})

	// Get the token
	got, err := adapter.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("expected ID %q, got %q", created.ID, got.ID)
	}
}

func TestRunnerTokenAdapter_Get_NotFound(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()
	_, err := adapter.Get(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token")
	}
}

func TestRunnerTokenAdapter_List(t *testing.T) {
	st := newMockRunnerTokenStore()
	// Use a counter for unique IDs
	counter := 0
	idGen := func() string {
		counter++
		return "rtok_" + string(rune('a'+counter))
	}
	svc := auth.NewRunnerTokenService(st, idGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()

	// Create some tokens
	_, _, _ = adapter.Create(ctx, CreateRunnerTokenOptions{PoolName: "pool-1"})
	_, _, _ = adapter.Create(ctx, CreateRunnerTokenOptions{PoolName: "pool-2"})

	// List tokens
	result, err := adapter.List(ctx, ListRunnerTokensOptions{Limit: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(result.Items))
	}
}

func TestRunnerTokenAdapter_List_ByPoolName(t *testing.T) {
	st := newMockRunnerTokenStore()
	counter := 0
	idGen := func() string {
		counter++
		return "rtok_" + string(rune('a'+counter))
	}
	svc := auth.NewRunnerTokenService(st, idGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()

	// Create tokens in different pools
	_, _, _ = adapter.Create(ctx, CreateRunnerTokenOptions{PoolName: "pool-a"})
	_, _, _ = adapter.Create(ctx, CreateRunnerTokenOptions{PoolName: "pool-b"})
	_, _, _ = adapter.Create(ctx, CreateRunnerTokenOptions{PoolName: "pool-a"})

	// List tokens filtered by pool
	result, err := adapter.List(ctx, ListRunnerTokensOptions{PoolName: "pool-a"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 tokens in pool-a, got %d", len(result.Items))
	}
}

func TestRunnerTokenAdapter_Revoke(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()

	// Create a token
	token, _, _ := adapter.Create(ctx, CreateRunnerTokenOptions{PoolName: "test-pool"})

	// Revoke it
	err := adapter.Revoke(ctx, token.ID, "no longer needed")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify it's revoked
	got, _ := adapter.Get(ctx, token.ID)
	if got.Status != "revoked" {
		t.Errorf("expected status 'revoked', got %q", got.Status)
	}
	if got.RevokedAt == nil {
		t.Error("expected token to have revoked_at set")
	}
}

func TestRunnerTokenAdapter_Revoke_NotFound(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()
	err := adapter.Revoke(ctx, "nonexistent", "reason")
	if err == nil {
		t.Fatal("expected error for nonexistent token")
	}
}

func TestRunnerTokenAdapter_Rotate(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()

	// Create a token
	original, originalRawToken, _ := adapter.Create(ctx, CreateRunnerTokenOptions{PoolName: "test-pool"})

	// Rotate it
	token, newRawToken, err := adapter.Rotate(ctx, original.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if token == nil {
		t.Fatal("expected non-nil token")
	}
	if newRawToken == "" {
		t.Fatal("expected non-empty new raw token")
	}
	if newRawToken == originalRawToken {
		t.Error("expected new token to be different from original")
	}
	if token.Status != "rotating" {
		t.Errorf("expected status 'rotating', got %q", token.Status)
	}
	if token.RotationDeadline == nil {
		t.Error("expected rotation deadline to be set")
	}
}

func TestRunnerTokenAdapter_Rotate_NotFound(t *testing.T) {
	st := newMockRunnerTokenStore()
	svc := auth.NewRunnerTokenService(st, testRunnerTokenIDGen)
	adapter := NewRunnerTokenAdapter(svc)

	ctx := context.Background()
	_, _, err := adapter.Rotate(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token")
	}
}

// Verify RunnerTokenAdapter implements RunnerTokenAdminService interface
var _ RunnerTokenAdminService = (*RunnerTokenAdapter)(nil)
