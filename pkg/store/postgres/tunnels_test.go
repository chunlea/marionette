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
// Tunnel Tests
// =============================================================================

func createTestSession(ctx context.Context, t *testing.T) *store.Session {
	// Create a workspace first
	workspace := &store.Workspace{
		Name:        "test-workspace-" + time.Now().Format("150405.000000"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	// Create a session
	session := &store.Session{
		Name:          ptrStr("test-session-" + time.Now().Format("150405.000000")),
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)
	return session
}

func createTestRunner(ctx context.Context, t *testing.T) *store.Runner {
	runner := &store.Runner{
		Name:         "test-runner-" + time.Now().Format("150405.000000"),
		Hostname:     "localhost",
		Status:       "idle",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	err := testStore.CreateRunner(ctx, runner)
	require.NoError(t, err)
	return runner
}

func ptrStr(s string) *string {
	return &s
}

// Note: TestTunnelCRUD and TestTunnelNotFound are in storage_test.go
// This file contains tests for tunnel extension methods

func TestGetTunnelByHash(t *testing.T) {
	ctx := context.Background()

	session := createTestSession(ctx, t)
	runner := createTestRunner(ctx, t)

	// Create tunnel with unique hash
	hash := "unique_hash_" + time.Now().Format("150405.000000")
	tunnel := &store.Tunnel{
		SessionID:   session.ID,
		RunnerID:    &runner.ID,
		Type:        "http",
		Direction:   "outbound",
		LocalPort:   3001,
		TokenHash:   hash,
		TokenPrefix: "ttok_test",
		HashVersion: 1,
		ExpiresAt:   time.Now().Add(time.Hour),
	}

	err := testStore.CreateTunnel(ctx, tunnel)
	require.NoError(t, err)

	// Get by hash
	got, err := testStore.GetTunnelByTokenHash(ctx, hash)
	require.NoError(t, err)
	assert.Equal(t, tunnel.ID, got.ID)
	assert.Equal(t, hash, got.TokenHash)

	// Non-existent hash
	_, err = testStore.GetTunnelByTokenHash(ctx, "non_existent_hash")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestListTunnelsFiltering(t *testing.T) {
	ctx := context.Background()

	session1 := createTestSession(ctx, t)
	session2 := createTestSession(ctx, t)
	runner := createTestRunner(ctx, t)

	// Create tunnels for session1
	for i := 0; i < 3; i++ {
		tunnel := &store.Tunnel{
			SessionID:   session1.ID,
			RunnerID:    &runner.ID,
			Type:        "http",
			Direction:   "outbound",
			LocalPort:   4000 + i,
			TokenHash:   "hash_s1_" + time.Now().Format("150405.000000") + string(rune('a'+i)),
			TokenPrefix: "ttok_s1",
			HashVersion: 1,
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		err := testStore.CreateTunnel(ctx, tunnel)
		require.NoError(t, err)
	}

	// Create tunnels for session2
	for i := 0; i < 2; i++ {
		tunnel := &store.Tunnel{
			SessionID:   session2.ID,
			RunnerID:    &runner.ID,
			Type:        "tcp",
			Direction:   "outbound",
			LocalPort:   5000 + i,
			TokenHash:   "hash_s2_" + time.Now().Format("150405.000000") + string(rune('a'+i)),
			TokenPrefix: "ttok_s2",
			HashVersion: 1,
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		err := testStore.CreateTunnel(ctx, tunnel)
		require.NoError(t, err)
	}

	t.Run("filter by session", func(t *testing.T) {
		list, err := testStore.ListTunnels(ctx, store.ListTunnelsOptions{
			SessionID: &session1.ID,
		})
		require.NoError(t, err)
		assert.Len(t, list.Items, 3)
	})

	t.Run("filter by type", func(t *testing.T) {
		list, err := testStore.ListTunnels(ctx, store.ListTunnelsOptions{
			Type: []string{"tcp"},
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(list.Items), 2)
		for _, tunnel := range list.Items {
			assert.Equal(t, "tcp", tunnel.Type)
		}
	})

	t.Run("filter by runner", func(t *testing.T) {
		list, err := testStore.ListTunnels(ctx, store.ListTunnelsOptions{
			RunnerID: &runner.ID,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(list.Items), 5)
	})

	t.Run("filter by direction", func(t *testing.T) {
		direction := "outbound"
		list, err := testStore.ListTunnels(ctx, store.ListTunnelsOptions{
			Direction: &direction,
		})
		require.NoError(t, err)
		assert.GreaterOrEqual(t, len(list.Items), 5)
	})
}

func TestListTunnelsIncludeClosed(t *testing.T) {
	ctx := context.Background()

	session := createTestSession(ctx, t)
	runner := createTestRunner(ctx, t)

	// Create an open tunnel
	openTunnel := &store.Tunnel{
		SessionID:   session.ID,
		RunnerID:    &runner.ID,
		Type:        "http",
		Direction:   "outbound",
		LocalPort:   6000,
		TokenHash:   "open_hash_" + time.Now().Format("150405.000000"),
		TokenPrefix: "ttok_open",
		HashVersion: 1,
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	err := testStore.CreateTunnel(ctx, openTunnel)
	require.NoError(t, err)

	// Create a closed tunnel
	closedAt := time.Now()
	closedTunnel := &store.Tunnel{
		SessionID:   session.ID,
		RunnerID:    &runner.ID,
		Type:        "http",
		Direction:   "outbound",
		LocalPort:   6001,
		TokenHash:   "closed_hash_" + time.Now().Format("150405.000000"),
		TokenPrefix: "ttok_closed",
		HashVersion: 1,
		ExpiresAt:   time.Now().Add(time.Hour),
		ClosedAt:    &closedAt,
	}
	err = testStore.CreateTunnel(ctx, closedTunnel)
	require.NoError(t, err)

	t.Run("exclude closed by default", func(t *testing.T) {
		list, err := testStore.ListTunnels(ctx, store.ListTunnelsOptions{
			SessionID: &session.ID,
		})
		require.NoError(t, err)
		for _, tunnel := range list.Items {
			assert.Nil(t, tunnel.ClosedAt)
		}
	})

	t.Run("include closed", func(t *testing.T) {
		list, err := testStore.ListTunnels(ctx, store.ListTunnelsOptions{
			SessionID:     &session.ID,
			IncludeClosed: true,
		})
		require.NoError(t, err)
		// Should find both open and closed tunnels
		hasOpen := false
		hasClosed := false
		for _, tunnel := range list.Items {
			if tunnel.ID == openTunnel.ID {
				hasOpen = true
			}
			if tunnel.ID == closedTunnel.ID {
				hasClosed = true
			}
		}
		assert.True(t, hasOpen)
		assert.True(t, hasClosed)
	})
}

func TestCloseTunnel(t *testing.T) {
	ctx := context.Background()

	session := createTestSession(ctx, t)
	runner := createTestRunner(ctx, t)

	tunnel := &store.Tunnel{
		SessionID:   session.ID,
		RunnerID:    &runner.ID,
		Type:        "http",
		Direction:   "outbound",
		LocalPort:   7000,
		TokenHash:   "close_hash_" + time.Now().Format("150405.000000"),
		TokenPrefix: "ttok_close",
		HashVersion: 1,
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	err := testStore.CreateTunnel(ctx, tunnel)
	require.NoError(t, err)
	assert.Nil(t, tunnel.ClosedAt)

	// Close the tunnel
	closedAt := time.Now()
	err = testStore.UpdateTunnel(ctx, tunnel.ID, store.TunnelUpdates{
		ClosedAt: &closedAt,
	})
	require.NoError(t, err)

	// Verify closed
	got, err := testStore.GetTunnel(ctx, tunnel.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.ClosedAt)
}

func TestCloseSessionTunnels(t *testing.T) {
	ctx := context.Background()

	session := createTestSession(ctx, t)
	runner := createTestRunner(ctx, t)

	// Create multiple tunnels
	for i := 0; i < 3; i++ {
		tunnel := &store.Tunnel{
			SessionID:   session.ID,
			RunnerID:    &runner.ID,
			Type:        "http",
			Direction:   "outbound",
			LocalPort:   8000 + i,
			TokenHash:   "session_hash_" + time.Now().Format("150405.000000") + string(rune('a'+i)),
			TokenPrefix: "ttok_sess",
			HashVersion: 1,
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		err := testStore.CreateTunnel(ctx, tunnel)
		require.NoError(t, err)
	}

	// Verify tunnels are open
	list, err := testStore.ListTunnels(ctx, store.ListTunnelsOptions{
		SessionID: &session.ID,
	})
	require.NoError(t, err)
	assert.Len(t, list.Items, 3)

	// Close all tunnels for session
	closed, err := testStore.CloseSessionTunnels(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(3), closed)

	// Verify all are closed
	list, err = testStore.ListTunnels(ctx, store.ListTunnelsOptions{
		SessionID: &session.ID,
	})
	require.NoError(t, err)
	assert.Len(t, list.Items, 0)

	// But they should exist if we include closed
	list, err = testStore.ListTunnels(ctx, store.ListTunnelsOptions{
		SessionID:     &session.ID,
		IncludeClosed: true,
	})
	require.NoError(t, err)
	assert.Len(t, list.Items, 3)
}

func TestDeleteExpiredTunnels(t *testing.T) {
	ctx := context.Background()

	session := createTestSession(ctx, t)
	runner := createTestRunner(ctx, t)

	// Create expired tunnels
	for i := 0; i < 2; i++ {
		tunnel := &store.Tunnel{
			SessionID:   session.ID,
			RunnerID:    &runner.ID,
			Type:        "http",
			Direction:   "outbound",
			LocalPort:   9000 + i,
			TokenHash:   "expired_hash_" + time.Now().Format("150405.000000") + string(rune('a'+i)),
			TokenPrefix: "ttok_exp",
			HashVersion: 1,
			ExpiresAt:   time.Now().Add(-time.Hour), // Already expired
		}
		err := testStore.CreateTunnel(ctx, tunnel)
		require.NoError(t, err)
	}

	// Create valid tunnel
	validTunnel := &store.Tunnel{
		SessionID:   session.ID,
		RunnerID:    &runner.ID,
		Type:        "http",
		Direction:   "outbound",
		LocalPort:   9002,
		TokenHash:   "valid_hash_" + time.Now().Format("150405.000000"),
		TokenPrefix: "ttok_valid",
		HashVersion: 1,
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	err := testStore.CreateTunnel(ctx, validTunnel)
	require.NoError(t, err)

	// Delete expired
	deleted, err := testStore.DeleteExpiredTunnels(ctx)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, deleted, int64(2))

	// Valid tunnel should still exist
	_, err = testStore.GetTunnel(ctx, validTunnel.ID)
	require.NoError(t, err)
}

func TestGetTunnelsByRunner(t *testing.T) {
	ctx := context.Background()

	session := createTestSession(ctx, t)
	runner1 := createTestRunner(ctx, t)
	runner2 := createTestRunner(ctx, t)

	// Create tunnels for runner1
	for i := 0; i < 3; i++ {
		tunnel := &store.Tunnel{
			SessionID:   session.ID,
			RunnerID:    &runner1.ID,
			Type:        "http",
			Direction:   "outbound",
			LocalPort:   10000 + i,
			TokenHash:   "r1_hash_" + time.Now().Format("150405.000000") + string(rune('a'+i)),
			TokenPrefix: "ttok_r1",
			HashVersion: 1,
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		err := testStore.CreateTunnel(ctx, tunnel)
		require.NoError(t, err)
	}

	// Create tunnels for runner2
	for i := 0; i < 2; i++ {
		tunnel := &store.Tunnel{
			SessionID:   session.ID,
			RunnerID:    &runner2.ID,
			Type:        "tcp",
			Direction:   "outbound",
			LocalPort:   11000 + i,
			TokenHash:   "r2_hash_" + time.Now().Format("150405.000000") + string(rune('a'+i)),
			TokenPrefix: "ttok_r2",
			HashVersion: 1,
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		err := testStore.CreateTunnel(ctx, tunnel)
		require.NoError(t, err)
	}

	// Get tunnels for runner1
	tunnels, err := testStore.GetTunnelsByRunner(ctx, runner1.ID)
	require.NoError(t, err)
	assert.Len(t, tunnels, 3)
	for _, tunnel := range tunnels {
		assert.Equal(t, runner1.ID, *tunnel.RunnerID)
	}

	// Get tunnels for runner2
	tunnels, err = testStore.GetTunnelsByRunner(ctx, runner2.ID)
	require.NoError(t, err)
	assert.Len(t, tunnels, 2)
	for _, tunnel := range tunnels {
		assert.Equal(t, runner2.ID, *tunnel.RunnerID)
	}
}

func TestGetActiveTunnelCount(t *testing.T) {
	ctx := context.Background()

	// Get initial count
	initialCount, err := testStore.GetActiveTunnelCount(ctx)
	require.NoError(t, err)

	session := createTestSession(ctx, t)
	runner := createTestRunner(ctx, t)

	// Create active tunnels
	for i := 0; i < 3; i++ {
		tunnel := &store.Tunnel{
			SessionID:   session.ID,
			RunnerID:    &runner.ID,
			Type:        "http",
			Direction:   "outbound",
			LocalPort:   12000 + i,
			TokenHash:   "count_hash_" + time.Now().Format("150405.000000") + string(rune('a'+i)),
			TokenPrefix: "ttok_cnt",
			HashVersion: 1,
			ExpiresAt:   time.Now().Add(time.Hour),
		}
		err := testStore.CreateTunnel(ctx, tunnel)
		require.NoError(t, err)
	}

	// Verify count increased
	newCount, err := testStore.GetActiveTunnelCount(ctx)
	require.NoError(t, err)
	assert.Equal(t, initialCount+3, newCount)
}

func TestTunnelTypes(t *testing.T) {
	ctx := context.Background()

	session := createTestSession(ctx, t)
	runner := createTestRunner(ctx, t)

	// Test all valid tunnel types
	types := []struct {
		tunnelType string
		direction  string
	}{
		{"http", "outbound"},
		{"tcp", "outbound"},
		{"desktop", "inbound"},
		{"browser", "inbound"},
		{"ios", "inbound"},
		{"android", "inbound"},
	}

	for _, tc := range types {
		t.Run(tc.tunnelType, func(t *testing.T) {
			tunnel := &store.Tunnel{
				SessionID:   session.ID,
				RunnerID:    &runner.ID,
				Type:        tc.tunnelType,
				Direction:   tc.direction,
				LocalPort:   13000,
				TokenHash:   "type_hash_" + tc.tunnelType + "_" + time.Now().Format("150405.000000"),
				TokenPrefix: "ttok_type",
				HashVersion: 1,
				ExpiresAt:   time.Now().Add(time.Hour),
			}
			err := testStore.CreateTunnel(ctx, tunnel)
			require.NoError(t, err)

			got, err := testStore.GetTunnel(ctx, tunnel.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.tunnelType, got.Type)
			assert.Equal(t, tc.direction, got.Direction)
		})
	}
}

func TestUpdateTunnelNoChanges(t *testing.T) {
	ctx := context.Background()

	session := createTestSession(ctx, t)
	runner := createTestRunner(ctx, t)

	tunnel := &store.Tunnel{
		SessionID:   session.ID,
		RunnerID:    &runner.ID,
		Type:        "http",
		Direction:   "outbound",
		LocalPort:   14000,
		TokenHash:   "noop_hash_" + time.Now().Format("150405.000000"),
		TokenPrefix: "ttok_noop",
		HashVersion: 1,
		ExpiresAt:   time.Now().Add(time.Hour),
	}
	err := testStore.CreateTunnel(ctx, tunnel)
	require.NoError(t, err)

	// Update with no changes should be no-op
	err = testStore.UpdateTunnel(ctx, tunnel.ID, store.TunnelUpdates{})
	require.NoError(t, err)
}

func TestUpdateTunnelNotFound(t *testing.T) {
	ctx := context.Background()

	newURL := "http://test.com"
	err := testStore.UpdateTunnel(ctx, "tun_nonexistent", store.TunnelUpdates{
		PublicURL: &newURL,
	})
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestDeleteTunnelNotFound(t *testing.T) {
	ctx := context.Background()

	err := testStore.DeleteTunnel(ctx, "tun_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}
