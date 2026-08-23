package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/config"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// workspaceTestStore is a minimal store for WorkspaceManager testing.
type workspaceTestStore struct {
	workspaces map[string]*store.Workspace
	sessions   map[string]*store.Session
}

func newWorkspaceTestStore() *workspaceTestStore {
	return &workspaceTestStore{
		workspaces: make(map[string]*store.Workspace),
		sessions:   make(map[string]*store.Session),
	}
}

func (m *workspaceTestStore) CreateWorkspace(_ context.Context, ws *store.Workspace) error {
	if _, exists := m.workspaces[ws.ID]; exists {
		return ErrWorkspaceAlreadyExists
	}
	ws.CreatedAt = time.Now()
	ws.UpdatedAt = time.Now()
	m.workspaces[ws.ID] = ws
	return nil
}

func (m *workspaceTestStore) GetWorkspace(_ context.Context, id string) (*store.Workspace, error) {
	ws, ok := m.workspaces[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return ws, nil
}

func (m *workspaceTestStore) ListWorkspaces(_ context.Context, opts store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	var items []*store.Workspace
	for _, ws := range m.workspaces {
		if !opts.IncludeDeleted && ws.DeletedAt != nil {
			continue
		}
		if opts.TenantID != nil && (ws.TenantID == nil || *ws.TenantID != *opts.TenantID) {
			continue
		}
		items = append(items, ws)
	}

	totalCount := int64(len(items))
	hasMore := false

	// Apply limit
	if opts.Limit > 0 && len(items) > opts.Limit {
		hasMore = true
		items = items[:opts.Limit]
	}

	return &store.ListResult[store.Workspace]{
		Items:      items,
		TotalCount: totalCount,
		HasMore:    hasMore,
	}, nil
}

func (m *workspaceTestStore) UpdateWorkspace(_ context.Context, id string, updates store.WorkspaceUpdates) error {
	ws, ok := m.workspaces[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Name != nil {
		ws.Name = *updates.Name
	}
	if updates.Persist != nil {
		ws.Persist = *updates.Persist
	}
	if updates.DiskQuotaMB != nil {
		ws.DiskQuotaMB = updates.DiskQuotaMB
	}
	if updates.DeletedAt != nil {
		ws.DeletedAt = updates.DeletedAt
	}
	if updates.Labels != nil {
		ws.Labels = updates.Labels
	}
	ws.UpdatedAt = time.Now()
	return nil
}

func (m *workspaceTestStore) DeleteWorkspace(_ context.Context, id string) error {
	ws, ok := m.workspaces[id]
	if !ok {
		return store.ErrNotFound
	}
	now := time.Now()
	ws.DeletedAt = &now
	return nil
}

func (m *workspaceTestStore) ListSessions(_ context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	var items []*store.Session
	for _, sess := range m.sessions {
		if opts.WorkspaceID != nil && sess.WorkspaceID != *opts.WorkspaceID {
			continue
		}
		// Honour the status filter the way the real store does. Ignoring it
		// here let IsInUse look correct against a fake that returned rows the
		// query would never have produced.
		if len(opts.Status) > 0 && !matchesAnyStatus(opts.Status, sess.Status) {
			continue
		}
		items = append(items, sess)
	}
	return &store.ListResult[store.Session]{
		Items:      items,
		TotalCount: int64(len(items)),
	}, nil
}

func matchesAnyStatus(statuses []string, status string) bool {
	for _, s := range statuses {
		if s == status {
			return true
		}
	}
	return false
}

// AddSession adds a session to the test store.
func (m *workspaceTestStore) AddSession(sess *store.Session) {
	m.sessions[sess.ID] = sess
}

// testWorkspaceManager creates a WorkspaceManager with a test store.
type testWorkspaceManager struct {
	*WorkspaceManager
	store *workspaceTestStore
}

func newTestWorkspaceManager(t *testing.T, cfg config.WorkspaceStorageConfig) *testWorkspaceManager {
	testStore := newWorkspaceTestStore()
	logger := zap.NewNop()

	if cfg.BaseDir == "" {
		cfg.BaseDir = t.TempDir()
	}
	mgr := NewWorkspaceManager(testStore, cfg, logger)

	return &testWorkspaceManager{
		WorkspaceManager: mgr,
		store:            testStore,
	}
}

// Tests

func TestWorkspaceManager_Create(t *testing.T) {
	cfg := config.WorkspaceStorageConfig{
		BaseDir:        t.TempDir(),
		DefaultQuotaMB: 1024,
	}
	mgr := newTestWorkspaceManager(t, cfg)
	ctx := context.Background()

	t.Run("creates workspace with defaults", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{})
		require.NoError(t, err)
		assert.NotEmpty(t, ws.ID)
		assert.True(t, ws.Persist)
		assert.Equal(t, "volume", ws.StorageType)
		assert.Equal(t, "local", ws.Mobility)
		assert.NotNil(t, ws.DiskQuotaMB)
		assert.Equal(t, 1024, *ws.DiskQuotaMB)
	})

	t.Run("creates workspace with custom name", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{
			Name: "my-workspace",
		})
		require.NoError(t, err)
		assert.Equal(t, "my-workspace", ws.Name)
	})

	t.Run("creates workspace with custom quota", func(t *testing.T) {
		quota := 2048
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{
			DiskQuotaMB: &quota,
		})
		require.NoError(t, err)
		assert.Equal(t, 2048, *ws.DiskQuotaMB)
	})

	t.Run("creates workspace with labels", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{
			Labels: map[string]string{"env": "test"},
		})
		require.NoError(t, err)

		var labels map[string]string
		err = json.Unmarshal(ws.Labels, &labels)
		require.NoError(t, err)
		assert.Equal(t, "test", labels["env"])
	})
}

func TestWorkspaceManager_Get(t *testing.T) {
	cfg := config.WorkspaceStorageConfig{
		BaseDir: t.TempDir(),
	}
	mgr := newTestWorkspaceManager(t, cfg)
	ctx := context.Background()

	// Create a workspace first
	ws, err := mgr.Create(ctx, CreateWorkspaceOptions{Name: "test-ws"})
	require.NoError(t, err)

	t.Run("gets existing workspace", func(t *testing.T) {
		got, err := mgr.Get(ctx, ws.ID)
		require.NoError(t, err)
		assert.Equal(t, ws.ID, got.ID)
		assert.Equal(t, "test-ws", got.Name)
	})

	t.Run("returns error for non-existent workspace", func(t *testing.T) {
		_, err := mgr.Get(ctx, "ws_nonexistent")
		assert.ErrorIs(t, err, ErrWorkspaceNotFound)
	})

	t.Run("returns error for deleted workspace", func(t *testing.T) {
		// Create and delete a workspace
		ws2, err := mgr.Create(ctx, CreateWorkspaceOptions{Name: "to-delete"})
		require.NoError(t, err)

		err = mgr.Delete(ctx, ws2.ID)
		require.NoError(t, err)

		_, err = mgr.Get(ctx, ws2.ID)
		assert.ErrorIs(t, err, ErrWorkspaceDeleted)
	})
}

func TestWorkspaceManager_List(t *testing.T) {
	cfg := config.WorkspaceStorageConfig{
		BaseDir: t.TempDir(),
	}
	mgr := newTestWorkspaceManager(t, cfg)
	ctx := context.Background()

	// Create several workspaces
	for i := 0; i < 3; i++ {
		_, err := mgr.Create(ctx, CreateWorkspaceOptions{})
		require.NoError(t, err)
	}

	t.Run("lists all workspaces", func(t *testing.T) {
		result, err := mgr.List(ctx, ListWorkspacesOptions{})
		require.NoError(t, err)
		assert.Len(t, result.Items, 3)
	})

	t.Run("respects limit", func(t *testing.T) {
		result, err := mgr.List(ctx, ListWorkspacesOptions{Limit: 2})
		require.NoError(t, err)
		assert.LessOrEqual(t, len(result.Items), 2)
	})
}

func TestWorkspaceManager_Update(t *testing.T) {
	cfg := config.WorkspaceStorageConfig{
		BaseDir: t.TempDir(),
	}
	mgr := newTestWorkspaceManager(t, cfg)
	ctx := context.Background()

	// Create a workspace
	ws, err := mgr.Create(ctx, CreateWorkspaceOptions{Name: "original"})
	require.NoError(t, err)

	t.Run("updates workspace name", func(t *testing.T) {
		newName := "updated"
		updated, err := mgr.Update(ctx, ws.ID, store.WorkspaceUpdates{
			Name: &newName,
		})
		require.NoError(t, err)
		assert.Equal(t, "updated", updated.Name)
	})

	t.Run("returns error for non-existent workspace", func(t *testing.T) {
		newName := "test"
		_, err := mgr.Update(ctx, "ws_nonexistent", store.WorkspaceUpdates{
			Name: &newName,
		})
		assert.ErrorIs(t, err, ErrWorkspaceNotFound)
	})
}

func TestWorkspaceManager_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.WorkspaceStorageConfig{
		BaseDir:            tmpDir,
		CleanupOnTerminate: true,
	}
	mgr := newTestWorkspaceManager(t, cfg)
	ctx := context.Background()

	t.Run("deletes workspace", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{})
		require.NoError(t, err)

		// Create host directory
		hostPath, err := mgr.EnsureHostDirectory(ctx, ws.ID)
		require.NoError(t, err)
		require.DirExists(t, hostPath)

		// Delete the workspace
		err = mgr.Delete(ctx, ws.ID)
		require.NoError(t, err)

		// Verify host directory is cleaned up
		_, err = os.Stat(hostPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("returns error for non-existent workspace", func(t *testing.T) {
		err := mgr.Delete(ctx, "ws_nonexistent")
		assert.ErrorIs(t, err, ErrWorkspaceNotFound)
	})

	t.Run("returns error for workspace in use", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{})
		require.NoError(t, err)

		// Add an active session using this workspace
		mgr.store.AddSession(&store.Session{
			ID:          "sess_test",
			WorkspaceID: ws.ID,
			Status:      SessionStatusActive,
		})

		err = mgr.Delete(ctx, ws.ID)
		assert.ErrorIs(t, err, ErrWorkspaceInUse)
	})
}

func TestWorkspaceManager_HostDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.WorkspaceStorageConfig{
		BaseDir: tmpDir,
	}
	mgr := newTestWorkspaceManager(t, cfg)
	ctx := context.Background()

	t.Run("GetHostPath returns correct path", func(t *testing.T) {
		path, err := mgr.GetHostPath(ctx, "ws_test123")
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(tmpDir, "ws_test123"), path)
	})

	t.Run("EnsureHostDirectory creates directory", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{})
		require.NoError(t, err)

		path, err := mgr.EnsureHostDirectory(ctx, ws.ID)
		require.NoError(t, err)
		assert.DirExists(t, path)
	})

	t.Run("EnsureHostDirectory is idempotent", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{})
		require.NoError(t, err)

		path1, err := mgr.EnsureHostDirectory(ctx, ws.ID)
		require.NoError(t, err)

		path2, err := mgr.EnsureHostDirectory(ctx, ws.ID)
		require.NoError(t, err)

		assert.Equal(t, path1, path2)
		assert.DirExists(t, path1)
	})

	t.Run("CleanupHostDirectory removes directory", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{})
		require.NoError(t, err)

		path, err := mgr.EnsureHostDirectory(ctx, ws.ID)
		require.NoError(t, err)
		assert.DirExists(t, path)

		err = mgr.CleanupHostDirectory(ctx, ws.ID)
		require.NoError(t, err)

		_, err = os.Stat(path)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("CleanupHostDirectory handles non-existent directory", func(t *testing.T) {
		err := mgr.CleanupHostDirectory(ctx, "ws_nonexistent")
		assert.NoError(t, err)
	})
}

func TestWorkspaceManager_IsInUse(t *testing.T) {
	cfg := config.WorkspaceStorageConfig{
		BaseDir: t.TempDir(),
	}
	mgr := newTestWorkspaceManager(t, cfg)
	ctx := context.Background()

	t.Run("returns false for unused workspace", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{})
		require.NoError(t, err)

		inUse, err := mgr.IsInUse(ctx, ws.ID)
		require.NoError(t, err)
		assert.False(t, inUse)
	})

	t.Run("returns true for workspace with active session", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{})
		require.NoError(t, err)

		mgr.store.AddSession(&store.Session{
			ID:          "sess_active",
			WorkspaceID: ws.ID,
			Status:      SessionStatusActive,
		})

		inUse, err := mgr.IsInUse(ctx, ws.ID)
		require.NoError(t, err)
		assert.True(t, inUse)
	})

	t.Run("returns false for workspace with terminated session", func(t *testing.T) {
		ws, err := mgr.Create(ctx, CreateWorkspaceOptions{})
		require.NoError(t, err)

		mgr.store.AddSession(&store.Session{
			ID:          "sess_terminated",
			WorkspaceID: ws.ID,
			Status:      SessionStatusTerminated,
		})

		inUse, err := mgr.IsInUse(ctx, ws.ID)
		require.NoError(t, err)
		assert.False(t, inUse)
	})
}

func TestWorkspaceManager_GetBaseDir(t *testing.T) {
	t.Run("returns configured base dir", func(t *testing.T) {
		cfg := config.WorkspaceStorageConfig{
			BaseDir: "/custom/path",
		}
		mgr := newTestWorkspaceManager(t, cfg)
		assert.Equal(t, "/custom/path", mgr.GetBaseDir())
	})

	t.Run("returns default when not configured", func(t *testing.T) {
		testStore := newWorkspaceTestStore()
		logger := zap.NewNop()
		cfg := config.WorkspaceStorageConfig{}

		// NewWorkspaceManager fills in the default base dir.
		mgr := NewWorkspaceManager(testStore, cfg, logger)
		assert.Equal(t, DefaultWorkspaceBaseDir, mgr.GetBaseDir())
	})
}
