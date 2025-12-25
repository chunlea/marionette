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
// Snapshot Tests
// =============================================================================

func TestSnapshotCRUD(t *testing.T) {
	ctx := context.Background()

	// Create runner first (snapshot requires a runner)
	runner := &store.Runner{
		Name:         "snapshot-test-runner-" + time.Now().Format("150405"),
		Hostname:     "localhost",
		Status:       "offline",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	err := testStore.CreateRunner(ctx, runner)
	require.NoError(t, err)

	// Create snapshot
	snapshot := &store.Snapshot{
		RunnerID:           runner.ID,
		Name:               "test-snapshot-" + time.Now().Format("150405"),
		ProviderSnapshotID: "provider-snap-123",
	}

	err = testStore.CreateSnapshot(ctx, snapshot)
	require.NoError(t, err)
	assert.NotEmpty(t, snapshot.ID)
	assert.NotZero(t, snapshot.CreatedAt)

	// Get
	got, err := testStore.GetSnapshot(ctx, snapshot.ID)
	require.NoError(t, err)
	assert.Equal(t, snapshot.Name, got.Name)
	assert.Equal(t, snapshot.RunnerID, got.RunnerID)
	assert.Equal(t, snapshot.ProviderSnapshotID, got.ProviderSnapshotID)

	// Get by runner and name
	got, err = testStore.GetSnapshotByRunnerAndName(ctx, runner.ID, snapshot.Name)
	require.NoError(t, err)
	assert.Equal(t, snapshot.ID, got.ID)

	// Update
	newStorageKey := "s3://bucket/key"
	newSizeBytes := int64(1024000)
	err = testStore.UpdateSnapshot(ctx, snapshot.ID, store.SnapshotUpdates{
		StorageKey: &newStorageKey,
		SizeBytes:  &newSizeBytes,
	})
	require.NoError(t, err)

	got, err = testStore.GetSnapshot(ctx, snapshot.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.StorageKey)
	assert.Equal(t, "s3://bucket/key", *got.StorageKey)
	assert.NotNil(t, got.SizeBytes)
	assert.Equal(t, int64(1024000), *got.SizeBytes)

	// List by runner
	list, err := testStore.ListSnapshots(ctx, store.ListSnapshotsOptions{
		RunnerID: &runner.ID,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)
	found := false
	for _, s := range list.Items {
		if s.ID == snapshot.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "snapshot should appear in list by runner")

	// Delete
	err = testStore.DeleteSnapshot(ctx, snapshot.ID)
	require.NoError(t, err)

	_, err = testStore.GetSnapshot(ctx, snapshot.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup runner
	_ = testStore.DeleteRunner(ctx, runner.ID)
}

func TestSnapshotNotFound(t *testing.T) {
	ctx := context.Background()

	_, err := testStore.GetSnapshot(ctx, "snap_nonexistent12345")
	assert.ErrorIs(t, err, store.ErrNotFound)

	var notFoundErr *store.NotFoundError
	assert.ErrorAs(t, err, &notFoundErr)
	assert.Equal(t, "snapshot", notFoundErr.Resource)
}

// =============================================================================
// Tunnel Tests
// =============================================================================

func TestTunnelCRUD(t *testing.T) {
	ctx := context.Background()

	// Create workspace and session first (tunnel requires a session)
	workspace := &store.Workspace{
		Name:        "tunnel-test-ws-" + time.Now().Format("150405"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	err := testStore.CreateWorkspace(ctx, workspace)
	require.NoError(t, err)

	session := &store.Session{
		Status:        "active",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	err = testStore.CreateSession(ctx, session)
	require.NoError(t, err)

	// Create tunnel
	expiresAt := time.Now().Add(1 * time.Hour)
	tunnel := &store.Tunnel{
		SessionID:   session.ID,
		Type:        "http",
		Direction:   "outbound",
		LocalPort:   8080,
		TokenHash:   "sha256-tunnel-hash-" + time.Now().Format("150405"),
		TokenPrefix: "ttok_test1234",
		HashVersion: 1,
		ExpiresAt:   expiresAt,
	}

	err = testStore.CreateTunnel(ctx, tunnel)
	require.NoError(t, err)
	assert.NotEmpty(t, tunnel.ID)
	assert.NotZero(t, tunnel.CreatedAt)

	// Get
	got, err := testStore.GetTunnel(ctx, tunnel.ID)
	require.NoError(t, err)
	assert.Equal(t, tunnel.SessionID, got.SessionID)
	assert.Equal(t, tunnel.Type, got.Type)
	assert.Equal(t, tunnel.Direction, got.Direction)
	assert.Equal(t, tunnel.LocalPort, got.LocalPort)
	assert.Equal(t, tunnel.TokenHash, got.TokenHash)

	// Get by token hash
	got, err = testStore.GetTunnelByTokenHash(ctx, tunnel.TokenHash)
	require.NoError(t, err)
	assert.Equal(t, tunnel.ID, got.ID)

	// Update
	publicURL := "https://tunnel.example.com"
	err = testStore.UpdateTunnel(ctx, tunnel.ID, store.TunnelUpdates{
		PublicURL: &publicURL,
	})
	require.NoError(t, err)

	got, err = testStore.GetTunnel(ctx, tunnel.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.PublicURL)
	assert.Equal(t, "https://tunnel.example.com", *got.PublicURL)

	// List by session
	list, err := testStore.ListTunnels(ctx, store.ListTunnelsOptions{
		SessionID: &session.ID,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)
	found := false
	for _, tun := range list.Items {
		if tun.ID == tunnel.ID {
			found = true
			break
		}
	}
	assert.True(t, found, "tunnel should appear in list by session")

	// List by type
	types := []string{"http"}
	list, err = testStore.ListTunnels(ctx, store.ListTunnelsOptions{
		Type: types,
	})
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list.Items), 1)

	// Delete
	err = testStore.DeleteTunnel(ctx, tunnel.ID)
	require.NoError(t, err)

	_, err = testStore.GetTunnel(ctx, tunnel.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Cleanup
	_ = testStore.DeleteSession(ctx, session.ID)
	_ = testStore.DeleteWorkspace(ctx, workspace.ID)
}

func TestTunnelNotFound(t *testing.T) {
	ctx := context.Background()

	_, err := testStore.GetTunnel(ctx, "tun_nonexistent12345")
	assert.ErrorIs(t, err, store.ErrNotFound)

	var notFoundErr *store.NotFoundError
	assert.ErrorAs(t, err, &notFoundErr)
	assert.Equal(t, "tunnel", notFoundErr.Resource)
}

// =============================================================================
// DataKey Tests
// =============================================================================

func TestDataKeyCRUD(t *testing.T) {
	ctx := context.Background()

	// Create data key
	dataKey := &store.DataKey{
		ResourceType: "workspace",
		ResourceID:   "ws_test123456789",
		DEKEncrypted: "encrypted-dek-" + time.Now().Format("150405"),
		Algorithm:    "AES-256-GCM",
	}

	err := testStore.CreateDataKey(ctx, dataKey)
	require.NoError(t, err)
	assert.NotEmpty(t, dataKey.ID)
	assert.NotZero(t, dataKey.CreatedAt)

	// Get
	got, err := testStore.GetDataKey(ctx, dataKey.ID)
	require.NoError(t, err)
	assert.Equal(t, dataKey.ResourceType, got.ResourceType)
	assert.Equal(t, dataKey.ResourceID, got.ResourceID)
	assert.Equal(t, dataKey.DEKEncrypted, got.DEKEncrypted)
	assert.Equal(t, dataKey.Algorithm, got.Algorithm)

	// Get by resource
	got, err = testStore.GetDataKeyByResource(ctx, dataKey.ResourceType, dataKey.ResourceID)
	require.NoError(t, err)
	assert.Equal(t, dataKey.ID, got.ID)

	// Update
	newDEK := "new-encrypted-dek-" + time.Now().Format("150405")
	rotatedAt := time.Now()
	err = testStore.UpdateDataKey(ctx, dataKey.ID, store.DataKeyUpdates{
		DEKEncrypted: &newDEK,
		RotatedAt:    &rotatedAt,
	})
	require.NoError(t, err)

	got, err = testStore.GetDataKey(ctx, dataKey.ID)
	require.NoError(t, err)
	assert.Equal(t, newDEK, got.DEKEncrypted)
	assert.NotNil(t, got.RotatedAt)

	// Delete
	err = testStore.DeleteDataKey(ctx, dataKey.ID)
	require.NoError(t, err)

	_, err = testStore.GetDataKey(ctx, dataKey.ID)
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestDataKeyNotFound(t *testing.T) {
	ctx := context.Background()

	_, err := testStore.GetDataKey(ctx, "dek_nonexistent12345")
	assert.ErrorIs(t, err, store.ErrNotFound)

	var notFoundErr *store.NotFoundError
	assert.ErrorAs(t, err, &notFoundErr)
	assert.Equal(t, "data_key", notFoundErr.Resource)
}

func TestDataKeyByResourceNotFound(t *testing.T) {
	ctx := context.Background()

	_, err := testStore.GetDataKeyByResource(ctx, "workspace", "ws_nonexistent")
	assert.ErrorIs(t, err, store.ErrNotFound)
}
