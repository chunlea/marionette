package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// =============================================================================
// API Key Tests
// =============================================================================

func TestAPIKeyCRUD(t *testing.T) {
	ctx := context.Background()

	// Create
	apiKey := &store.APIKey{
		Name:        "test-key-" + time.Now().Format("150405"),
		KeyHash:     "sha256-test-hash-" + time.Now().Format("150405"),
		KeyPrefix:   "mk_test1234",
		HashVersion: 1,
		Scopes:      []string{"read", "write"},
	}

	err := testStore.CreateAPIKey(ctx, apiKey)
	require.NoError(t, err)
	assert.NotEmpty(t, apiKey.ID)

	// Get
	got, err := testStore.GetAPIKey(ctx, apiKey.ID)
	require.NoError(t, err)
	assert.Equal(t, apiKey.Name, got.Name)
	assert.Equal(t, apiKey.KeyHash, got.KeyHash)

	// Get by hash
	got, err = testStore.GetAPIKeyByHash(ctx, apiKey.KeyHash)
	require.NoError(t, err)
	assert.Equal(t, apiKey.ID, got.ID)

	// Update
	newName := "updated-key"
	err = testStore.UpdateAPIKey(ctx, apiKey.ID, store.APIKeyUpdates{
		Name: &newName,
	})
	require.NoError(t, err)

	got, err = testStore.GetAPIKey(ctx, apiKey.ID)
	require.NoError(t, err)
	assert.Equal(t, "updated-key", got.Name)

	// Delete
	err = testStore.DeleteAPIKey(ctx, apiKey.ID)
	require.NoError(t, err)

	_, err = testStore.GetAPIKey(ctx, apiKey.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// =============================================================================
// RunnerToken Tests
// =============================================================================

func TestRunnerTokenCRUD(t *testing.T) {
	ctx := context.Background()

	// Create
	token := &store.RunnerToken{
		TokenHash:   "sha256-test-token-hash-" + time.Now().Format("150405"),
		TokenPrefix: "rtok_test123",
		HashVersion: 1,
		PoolName:    "test-pool",
		Status:      "active",
	}

	err := testStore.CreateRunnerToken(ctx, token)
	require.NoError(t, err)
	assert.NotEmpty(t, token.ID)
	assert.NotZero(t, token.CreatedAt)

	// Get
	got, err := testStore.GetRunnerToken(ctx, token.ID)
	require.NoError(t, err)
	assert.Equal(t, token.TokenHash, got.TokenHash)
	assert.Equal(t, token.PoolName, got.PoolName)
	assert.Equal(t, "active", got.Status)

	// Get by hash
	got, err = testStore.GetRunnerTokenByHash(ctx, token.TokenHash)
	require.NoError(t, err)
	assert.Equal(t, token.ID, got.ID)

	// Update
	newStatus := "rotating"
	err = testStore.UpdateRunnerToken(ctx, token.ID, store.RunnerTokenUpdates{
		Status: &newStatus,
	})
	require.NoError(t, err)

	got, err = testStore.GetRunnerToken(ctx, token.ID)
	require.NoError(t, err)
	assert.Equal(t, "rotating", got.Status)

	// Delete
	err = testStore.DeleteRunnerToken(ctx, token.ID)
	require.NoError(t, err)

	_, err = testStore.GetRunnerToken(ctx, token.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestListAPIKeys(t *testing.T) {
	ctx := context.Background()

	// Create 3 API keys with unique names
	keys := make([]*store.APIKey, 3)
	for i := 0; i < 3; i++ {
		keys[i] = &store.APIKey{
			Name:        "test-list-key-" + time.Now().Format("150405") + "-" + string(rune('A'+i)),
			KeyHash:     "sha256-list-test-hash-" + time.Now().Format("150405") + "-" + string(rune('A'+i)),
			KeyPrefix:   "mk_list" + string(rune('A'+i)),
			HashVersion: 1,
			Scopes:      []string{"read"},
		}
		err := testStore.CreateAPIKey(ctx, keys[i])
		require.NoError(t, err)
	}

	// Cleanup
	defer func() {
		for _, key := range keys {
			_ = testStore.DeleteAPIKey(ctx, key.ID)
		}
	}()

	// Test listing all keys
	result, err := testStore.ListAPIKeys(ctx, store.ListAPIKeysOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Items), 3, "should have at least 3 keys")

	// Test with limit
	result, err = testStore.ListAPIKeys(ctx, store.ListAPIKeysOptions{
		BaseListOptions: store.BaseListOptions{
			Limit: 2,
		},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, len(result.Items), "should return exactly 2 keys")
	assert.True(t, result.HasMore, "HasMore should be true when limited")
}

func TestListRunnerTokens(t *testing.T) {
	ctx := context.Background()

	poolName := "test-list-pool-" + time.Now().Format("150405")

	// Create 3 runner tokens with unique hashes
	tokens := make([]*store.RunnerToken, 3)
	for i := 0; i < 3; i++ {
		tokens[i] = &store.RunnerToken{
			TokenHash:   "sha256-list-token-hash-" + time.Now().Format("150405") + "-" + string(rune('A'+i)),
			TokenPrefix: "rtok_list" + string(rune('A'+i)),
			HashVersion: 1,
			PoolName:    poolName,
			Status:      "active",
		}
		err := testStore.CreateRunnerToken(ctx, tokens[i])
		require.NoError(t, err)
	}

	// Cleanup
	defer func() {
		for _, token := range tokens {
			_ = testStore.DeleteRunnerToken(ctx, token.ID)
		}
	}()

	// Test listing all tokens
	result, err := testStore.ListRunnerTokens(ctx, store.ListRunnerTokensOptions{})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Items), 3, "should have at least 3 tokens")

	// Test filtering by pool
	result, err = testStore.ListRunnerTokens(ctx, store.ListRunnerTokensOptions{
		PoolName: &poolName,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(result.Items), 3, "should have at least 3 tokens in pool")
	// Verify all returned tokens belong to the specified pool
	for _, token := range result.Items {
		assert.Equal(t, poolName, token.PoolName, "all tokens should belong to the specified pool")
	}
}
