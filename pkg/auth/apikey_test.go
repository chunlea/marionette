package auth

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/store/mock"
)

func newTestAPIKeyService() (*APIKeyService, *mock.APIKeyStore) {
	mockStore := mock.NewAPIKeyStore()
	idGen := func() string { return "key_test123" }
	svc := NewAPIKeyService(mockStore, idGen)
	return svc, mockStore
}

func TestAPIKeyService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("creates valid key", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		key, token, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name:   "test-key",
			Scopes: []string{"tasks:read", "sessions:*"},
		})
		require.NoError(t, err)

		assert.Equal(t, "key_test123", key.ID)
		assert.Equal(t, "test-key", key.Name)
		assert.NotEmpty(t, key.KeyHash)
		assert.NotEmpty(t, key.KeyPrefix)
		assert.Equal(t, []string{"tasks:read", "sessions:*"}, key.Scopes)

		// Token should start with mk_
		assert.True(t, len(token) > 10)
		assert.Equal(t, "mk_", token[:3])
	})

	t.Run("creates key with all options", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		tenantID := "tenant1"
		createdBy := "admin"
		expiresAt := time.Now().Add(24 * time.Hour)

		key, _, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name:        "full-options-key",
			Scopes:      []string{"*"},
			TenantID:    &tenantID,
			Labels:      map[string]string{"env": "test"},
			Annotations: map[string]string{"note": "test key"},
			ExpiresAt:   &expiresAt,
			CreatedBy:   &createdBy,
		})
		require.NoError(t, err)

		assert.Equal(t, &tenantID, key.TenantID)
		// Check labels
		var labels map[string]string
		require.NoError(t, json.Unmarshal(key.Labels, &labels))
		assert.Equal(t, "test", labels["env"])
		// Check annotations
		var annotations map[string]string
		require.NoError(t, json.Unmarshal(key.Annotations, &annotations))
		assert.Equal(t, "test key", annotations["note"])
		assert.NotNil(t, key.ExpiresAt)
		assert.Equal(t, &createdBy, key.CreatedBy)
	})

	t.Run("rejects empty name", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		_, _, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name: "",
		})
		assert.ErrorIs(t, err, ErrInvalidName)

		_, _, err = svc.Create(ctx, CreateAPIKeyOptions{
			Name: "   ",
		})
		assert.ErrorIs(t, err, ErrInvalidName)
	})

	t.Run("trims whitespace from name", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		key, _, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name: "  trimmed  ",
		})
		require.NoError(t, err)
		assert.Equal(t, "trimmed", key.Name)
	})
}

func TestAPIKeyService_Validate(t *testing.T) {
	ctx := context.Background()

	t.Run("validates correct token", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		created, token, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name: "valid-key",
		})
		require.NoError(t, err)

		validated, err := svc.Validate(ctx, token)
		require.NoError(t, err)
		assert.Equal(t, created.ID, validated.ID)
		assert.Equal(t, created.Name, validated.Name)
	})

	t.Run("rejects invalid token format", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		_, err := svc.Validate(ctx, "invalid")
		assert.ErrorIs(t, err, ErrInvalidToken)

		_, err = svc.Validate(ctx, "mk_short")
		assert.ErrorIs(t, err, ErrInvalidToken)
	})

	t.Run("rejects wrong prefix", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		// Create a token-like string with wrong prefix
		_, err := svc.Validate(ctx, "rtok_abcdefghijklmnopqrstuvwxyz0123456789ABCDEF")
		assert.ErrorIs(t, err, ErrInvalidPrefix)
	})

	t.Run("rejects non-existent token", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		_, err := svc.Validate(ctx, "mk_abcdefghijklmnopqrstuvwxyz0123456789ABCDEF")
		assert.ErrorIs(t, err, ErrTokenNotFound)
	})

	t.Run("rejects revoked token", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		_, token, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name: "to-revoke",
		})
		require.NoError(t, err)

		err = svc.Revoke(ctx, "key_test123", "testing")
		require.NoError(t, err)

		_, err = svc.Validate(ctx, token)
		assert.ErrorIs(t, err, ErrTokenRevoked)
	})

	t.Run("rejects expired token", func(t *testing.T) {
		svc, mockStore := newTestAPIKeyService()

		expiredTime := time.Now().Add(-1 * time.Hour)
		_, token, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name:      "expired",
			ExpiresAt: &expiredTime,
		})
		require.NoError(t, err)

		// The mock store doesn't filter by expiration, so the key exists
		// but validation should reject it
		_ = mockStore // just to use it

		_, err = svc.Validate(ctx, token)
		assert.ErrorIs(t, err, ErrTokenExpired)
	})
}

func TestAPIKeyService_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("gets existing key", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		created, _, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name: "get-test",
		})
		require.NoError(t, err)

		retrieved, err := svc.Get(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, created.ID, retrieved.ID)
		assert.Equal(t, created.Name, retrieved.Name)
	})

	t.Run("returns error for non-existent key", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		_, err := svc.Get(ctx, "key_nonexistent")
		assert.ErrorIs(t, err, ErrTokenNotFound)
	})
}

func TestAPIKeyService_List(t *testing.T) {
	ctx := context.Background()

	t.Run("lists all keys", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		// Create multiple keys with different IDs
		idCounter := 0
		svc.idGen = func() string {
			idCounter++
			return "key_test" + string(rune('0'+idCounter))
		}

		_, _, _ = svc.Create(ctx, CreateAPIKeyOptions{Name: "key1"})
		_, _, _ = svc.Create(ctx, CreateAPIKeyOptions{Name: "key2"})
		_, _, _ = svc.Create(ctx, CreateAPIKeyOptions{Name: "key3"})

		keys, err := svc.List(ctx, store.ListAPIKeysOptions{})
		require.NoError(t, err)
		assert.Len(t, keys, 3)
	})

	t.Run("paginates results", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		idCounter := 0
		svc.idGen = func() string {
			idCounter++
			return "key_page" + string(rune('0'+idCounter))
		}

		for i := 0; i < 5; i++ {
			_, _, _ = svc.Create(ctx, CreateAPIKeyOptions{Name: "key"})
		}

		keys, err := svc.List(ctx, store.ListAPIKeysOptions{BaseListOptions: store.BaseListOptions{Limit: 2}})
		require.NoError(t, err)
		assert.Len(t, keys, 2)
	})
}

func TestAPIKeyService_Revoke(t *testing.T) {
	ctx := context.Background()

	t.Run("revokes existing key", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		key, _, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name: "to-revoke",
		})
		require.NoError(t, err)

		err = svc.Revoke(ctx, key.ID, "no longer needed")
		require.NoError(t, err)

		retrieved, err := svc.Get(ctx, key.ID)
		require.NoError(t, err)
		assert.NotNil(t, retrieved.RevokedAt)
		assert.Equal(t, "no longer needed", *retrieved.RevokeReason)
	})

	t.Run("revoke without reason", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		key, _, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name: "revoke-no-reason",
		})
		require.NoError(t, err)

		err = svc.Revoke(ctx, key.ID, "")
		require.NoError(t, err)

		retrieved, err := svc.Get(ctx, key.ID)
		require.NoError(t, err)
		assert.NotNil(t, retrieved.RevokedAt)
	})

	t.Run("returns error for non-existent key", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		err := svc.Revoke(ctx, "key_nonexistent", "reason")
		assert.ErrorIs(t, err, ErrTokenNotFound)
	})
}

func TestAPIKeyService_UpdateLastUsed(t *testing.T) {
	ctx := context.Background()

	t.Run("updates last used timestamp", func(t *testing.T) {
		svc, _ := newTestAPIKeyService()

		key, _, err := svc.Create(ctx, CreateAPIKeyOptions{
			Name: "last-used-test",
		})
		require.NoError(t, err)
		assert.Nil(t, key.LastUsedAt)

		err = svc.UpdateLastUsed(ctx, key.ID)
		require.NoError(t, err)

		retrieved, err := svc.Get(ctx, key.ID)
		require.NoError(t, err)
		assert.NotNil(t, retrieved.LastUsedAt)
	})
}

func TestHasScope(t *testing.T) {
	tests := []struct {
		name     string
		scopes   []string
		required string
		expected bool
	}{
		{
			name:     "exact match",
			scopes:   []string{"tasks:read"},
			required: "tasks:read",
			expected: true,
		},
		{
			name:     "no match",
			scopes:   []string{"tasks:read"},
			required: "tasks:write",
			expected: false,
		},
		{
			name:     "wildcard match",
			scopes:   []string{"tasks:*"},
			required: "tasks:read",
			expected: true,
		},
		{
			name:     "wildcard match write",
			scopes:   []string{"tasks:*"},
			required: "tasks:write",
			expected: true,
		},
		{
			name:     "wildcard no cross-resource",
			scopes:   []string{"tasks:*"},
			required: "sessions:read",
			expected: false,
		},
		{
			name:     "full wildcard",
			scopes:   []string{"*"},
			required: "anything:anything",
			expected: true,
		},
		{
			name:     "multiple scopes match",
			scopes:   []string{"tasks:read", "sessions:*"},
			required: "sessions:write",
			expected: true,
		},
		{
			name:     "empty scopes",
			scopes:   []string{},
			required: "tasks:read",
			expected: false,
		},
		{
			name:     "nil key",
			scopes:   nil,
			required: "tasks:read",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var key *store.APIKey
			if tt.scopes != nil {
				key = &store.APIKey{Scopes: tt.scopes}
			}
			result := HasScope(key, tt.required)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHasAnyScope(t *testing.T) {
	key := &store.APIKey{
		Scopes: []string{"tasks:read", "sessions:read"},
	}

	assert.True(t, HasAnyScope(key, "tasks:read"))
	assert.True(t, HasAnyScope(key, "tasks:write", "tasks:read"))
	assert.False(t, HasAnyScope(key, "tasks:write", "sessions:write"))
}

func TestHasAllScopes(t *testing.T) {
	key := &store.APIKey{
		Scopes: []string{"tasks:read", "sessions:read"},
	}

	assert.True(t, HasAllScopes(key, "tasks:read"))
	assert.True(t, HasAllScopes(key, "tasks:read", "sessions:read"))
	assert.False(t, HasAllScopes(key, "tasks:read", "tasks:write"))
}
