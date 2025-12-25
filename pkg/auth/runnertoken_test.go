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

func newTestRunnerTokenService() (*RunnerTokenService, *mock.RunnerTokenStore) {
	mockStore := mock.NewRunnerTokenStore()
	idGen := func() string { return "rtok_test123" }
	svc := NewRunnerTokenService(mockStore, idGen)
	return svc, mockStore
}

func TestRunnerTokenService_Create(t *testing.T) {
	ctx := context.Background()

	t.Run("creates valid token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		token, plaintext, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "default-pool",
		})
		require.NoError(t, err)

		assert.Equal(t, "rtok_test123", token.ID)
		assert.Equal(t, "default-pool", token.PoolName)
		assert.Equal(t, TokenStatusActive, token.Status)
		assert.NotEmpty(t, token.TokenHash)
		assert.NotEmpty(t, token.TokenPrefix)

		// Token should start with rtok_
		assert.True(t, len(plaintext) > 10)
		assert.Equal(t, "rtok_", plaintext[:5])
	})

	t.Run("creates token with all options", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		tenantID := "tenant1"
		createdBy := "admin"
		expiresAt := time.Now().Add(24 * time.Hour)

		token, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName:  "gpu-pool",
			TenantID:  &tenantID,
			Labels:    map[string]string{"gpu": "true"},
			ExpiresAt: &expiresAt,
			CreatedBy: &createdBy,
		})
		require.NoError(t, err)

		assert.Equal(t, "gpu-pool", token.PoolName)
		assert.Equal(t, &tenantID, token.TenantID)
		// Check labels
		var labels map[string]string
		require.NoError(t, json.Unmarshal(token.Labels, &labels))
		assert.Equal(t, "true", labels["gpu"])
		assert.NotNil(t, token.ExpiresAt)
		assert.Equal(t, &createdBy, token.CreatedBy)
	})

	t.Run("rejects empty pool name", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "",
		})
		assert.ErrorIs(t, err, ErrInvalidPoolName)

		_, _, err = svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "   ",
		})
		assert.ErrorIs(t, err, ErrInvalidPoolName)
	})

	t.Run("trims whitespace from pool name", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		token, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "  trimmed  ",
		})
		require.NoError(t, err)
		assert.Equal(t, "trimmed", token.PoolName)
	})
}

func TestRunnerTokenService_Validate(t *testing.T) {
	ctx := context.Background()

	t.Run("validates correct token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		created, plaintext, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "test-pool",
		})
		require.NoError(t, err)

		validated, err := svc.Validate(ctx, plaintext)
		require.NoError(t, err)
		assert.Equal(t, created.ID, validated.ID)
		assert.Equal(t, created.PoolName, validated.PoolName)
	})

	t.Run("rejects invalid token format", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, err := svc.Validate(ctx, "invalid")
		assert.ErrorIs(t, err, ErrInvalidToken)

		_, err = svc.Validate(ctx, "rtok_short")
		assert.ErrorIs(t, err, ErrInvalidToken)
	})

	t.Run("rejects wrong prefix", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, err := svc.Validate(ctx, "mk_abcdefghijklmnopqrstuvwxyz0123456789ABCDEF")
		assert.ErrorIs(t, err, ErrInvalidPrefix)
	})

	t.Run("rejects non-existent token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, err := svc.Validate(ctx, "rtok_abcdefghijklmnopqrstuvwxyz0123456789ABCDE")
		assert.ErrorIs(t, err, ErrTokenNotFound)
	})

	t.Run("rejects revoked token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, plaintext, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "revoke-pool",
		})
		require.NoError(t, err)

		err = svc.Revoke(ctx, "rtok_test123", "testing")
		require.NoError(t, err)

		_, err = svc.Validate(ctx, plaintext)
		assert.ErrorIs(t, err, ErrTokenRevoked)
	})

	t.Run("rejects expired token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		expiredTime := time.Now().Add(-1 * time.Hour)
		_, plaintext, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName:  "expired-pool",
			ExpiresAt: &expiredTime,
		})
		require.NoError(t, err)

		_, err = svc.Validate(ctx, plaintext)
		assert.ErrorIs(t, err, ErrTokenExpired)
	})
}

func TestRunnerTokenService_Rotate(t *testing.T) {
	ctx := context.Background()

	t.Run("rotates token successfully", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()
		svc.rotationWindow = 100 * time.Millisecond // Short window for testing

		created, oldPlaintext, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "rotate-pool",
		})
		require.NoError(t, err)
		oldHash := created.TokenHash

		// Rotate
		newPlaintext, err := svc.Rotate(ctx, created.ID)
		require.NoError(t, err)
		assert.NotEqual(t, oldPlaintext, newPlaintext)
		assert.True(t, len(newPlaintext) > 10)
		assert.Equal(t, "rtok_", newPlaintext[:5])

		// Check token state
		token, err := svc.Get(ctx, created.ID)
		require.NoError(t, err)
		assert.Equal(t, TokenStatusRotating, token.Status)
		assert.NotNil(t, token.PreviousTokenHash)
		assert.Equal(t, oldHash, *token.PreviousTokenHash)
		assert.NotNil(t, token.RotationDeadline)
	})

	t.Run("both tokens valid during rotation window", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()
		svc.rotationWindow = 1 * time.Hour // Long enough for test

		_, oldPlaintext, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "dual-valid-pool",
		})
		require.NoError(t, err)

		newPlaintext, err := svc.Rotate(ctx, "rtok_test123")
		require.NoError(t, err)

		// Both should validate
		_, err = svc.Validate(ctx, oldPlaintext)
		require.NoError(t, err)

		_, err = svc.Validate(ctx, newPlaintext)
		require.NoError(t, err)
	})

	t.Run("old token invalid after rotation window", func(t *testing.T) {
		svc, mockStore := newTestRunnerTokenService()
		svc.rotationWindow = 1 * time.Millisecond // Very short

		_, oldPlaintext, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "expire-pool",
		})
		require.NoError(t, err)

		newPlaintext, err := svc.Rotate(ctx, "rtok_test123")
		require.NoError(t, err)

		// Wait for rotation to expire
		time.Sleep(10 * time.Millisecond)

		// Complete rotation
		err = svc.CompleteRotation(ctx, "rtok_test123")
		require.NoError(t, err)

		// Check state
		token, _ := mockStore.GetRunnerToken(ctx, "rtok_test123")
		assert.Equal(t, TokenStatusActive, token.Status)
		assert.Nil(t, token.PreviousTokenHash)

		// New token still valid
		_, err = svc.Validate(ctx, newPlaintext)
		require.NoError(t, err)

		// Old token should not be found (previous hash cleared)
		_, err = svc.Validate(ctx, oldPlaintext)
		assert.ErrorIs(t, err, ErrTokenNotFound)
	})

	t.Run("cannot rotate revoked token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "revoked-pool",
		})
		require.NoError(t, err)

		err = svc.Revoke(ctx, "rtok_test123", "test")
		require.NoError(t, err)

		_, err = svc.Rotate(ctx, "rtok_test123")
		assert.ErrorIs(t, err, ErrTokenRevoked)
	})

	t.Run("cannot rotate non-existent token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, err := svc.Rotate(ctx, "rtok_nonexistent")
		assert.ErrorIs(t, err, ErrTokenNotFound)
	})
}

func TestRunnerTokenService_BindRunner(t *testing.T) {
	ctx := context.Background()

	t.Run("binds runner to token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "bind-pool",
		})
		require.NoError(t, err)

		err = svc.BindRunner(ctx, "rtok_test123", "run_abc123")
		require.NoError(t, err)

		token, err := svc.Get(ctx, "rtok_test123")
		require.NoError(t, err)
		assert.NotNil(t, token.RunnerID)
		assert.Equal(t, "run_abc123", *token.RunnerID)
	})

	t.Run("allows rebinding same runner", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "rebind-pool",
		})
		require.NoError(t, err)

		err = svc.BindRunner(ctx, "rtok_test123", "run_abc123")
		require.NoError(t, err)

		err = svc.BindRunner(ctx, "rtok_test123", "run_abc123")
		require.NoError(t, err)
	})

	t.Run("rejects binding different runner", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "conflict-pool",
		})
		require.NoError(t, err)

		err = svc.BindRunner(ctx, "rtok_test123", "run_abc123")
		require.NoError(t, err)

		err = svc.BindRunner(ctx, "rtok_test123", "run_xyz789")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already bound")
	})

	t.Run("unbinds runner", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "unbind-pool",
		})
		require.NoError(t, err)

		err = svc.BindRunner(ctx, "rtok_test123", "run_abc123")
		require.NoError(t, err)

		err = svc.UnbindRunner(ctx, "rtok_test123")
		require.NoError(t, err)

		token, err := svc.Get(ctx, "rtok_test123")
		require.NoError(t, err)
		assert.Nil(t, token.RunnerID)
	})
}

func TestRunnerTokenService_Revoke(t *testing.T) {
	ctx := context.Background()

	t.Run("revokes existing token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "revoke-test-pool",
		})
		require.NoError(t, err)

		err = svc.Revoke(ctx, "rtok_test123", "no longer needed")
		require.NoError(t, err)

		token, err := svc.Get(ctx, "rtok_test123")
		require.NoError(t, err)
		assert.Equal(t, TokenStatusRevoked, token.Status)
		assert.NotNil(t, token.RevokedAt)
		assert.Equal(t, "no longer needed", *token.RevokeReason)
	})

	t.Run("returns error for non-existent token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		err := svc.Revoke(ctx, "rtok_nonexistent", "reason")
		assert.ErrorIs(t, err, ErrTokenNotFound)
	})
}

func TestRunnerTokenService_UpdateLastUsed(t *testing.T) {
	ctx := context.Background()

	t.Run("updates last used timestamp", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		token, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "last-used-pool",
		})
		require.NoError(t, err)
		assert.Nil(t, token.LastUsedAt)

		err = svc.UpdateLastUsed(ctx, "rtok_test123")
		require.NoError(t, err)

		retrieved, err := svc.Get(ctx, "rtok_test123")
		require.NoError(t, err)
		assert.NotNil(t, retrieved.LastUsedAt)
	})
}

func TestRunnerTokenService_List(t *testing.T) {
	ctx := context.Background()

	t.Run("lists all tokens", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		idCounter := 0
		svc.idGen = func() string {
			idCounter++
			return "rtok_test" + string(rune('0'+idCounter))
		}

		_, _, _ = svc.Create(ctx, CreateRunnerTokenOptions{PoolName: "pool1"})
		_, _, _ = svc.Create(ctx, CreateRunnerTokenOptions{PoolName: "pool2"})
		_, _, _ = svc.Create(ctx, CreateRunnerTokenOptions{PoolName: "pool3"})

		tokens, err := svc.List(ctx, store.ListRunnerTokensOptions{})
		require.NoError(t, err)
		assert.Len(t, tokens, 3)
	})
}

func TestRunnerTokenService_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("gets existing token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "test-pool",
		})
		require.NoError(t, err)

		token, err := svc.Get(ctx, "rtok_test123")
		require.NoError(t, err)
		assert.Equal(t, "rtok_test123", token.ID)
		assert.Equal(t, "test-pool", token.PoolName)
	})

	t.Run("returns error for non-existent token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, err := svc.Get(ctx, "rtok_nonexistent")
		assert.ErrorIs(t, err, ErrTokenNotFound)
	})
}

func TestRunnerTokenService_UnbindRunner_NotFound(t *testing.T) {
	ctx := context.Background()
	svc, _ := newTestRunnerTokenService()

	err := svc.UnbindRunner(ctx, "rtok_nonexistent")
	assert.ErrorIs(t, err, ErrTokenNotFound)
}

func TestRunnerTokenService_CompleteRotation_EdgeCases(t *testing.T) {
	ctx := context.Background()

	t.Run("no-op for non-rotating token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		_, _, err := svc.Create(ctx, CreateRunnerTokenOptions{
			PoolName: "test-pool",
		})
		require.NoError(t, err)

		// CompleteRotation on active token should be a no-op
		err = svc.CompleteRotation(ctx, "rtok_test123")
		require.NoError(t, err)

		// Token should still be active
		token, err := svc.Get(ctx, "rtok_test123")
		require.NoError(t, err)
		assert.Equal(t, TokenStatusActive, token.Status)
	})

	t.Run("returns error for non-existent token", func(t *testing.T) {
		svc, _ := newTestRunnerTokenService()

		err := svc.CompleteRotation(ctx, "rtok_nonexistent")
		assert.ErrorIs(t, err, ErrTokenNotFound)
	})
}

func TestRunnerTokenService_WithRotationWindow(t *testing.T) {
	mockStore := mock.NewRunnerTokenStore()
	idGen := func() string { return "rtok_test123" }
	svc := NewRunnerTokenService(mockStore, idGen).WithRotationWindow(2 * time.Hour)

	assert.Equal(t, 2*time.Hour, svc.rotationWindow)
}
