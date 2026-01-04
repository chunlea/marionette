package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testWorkspaceManager is a mock implementation of core.WorkspaceManagerInterface for adapter tests.
type testWorkspaceManager struct {
	CreateFunc             func(ctx context.Context, opts core.CreateWorkspaceOptions) (*store.Workspace, error)
	GetFunc                func(ctx context.Context, workspaceID string) (*store.Workspace, error)
	ListFunc               func(ctx context.Context, opts core.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error)
	UpdateFunc             func(ctx context.Context, workspaceID string, updates store.WorkspaceUpdates) (*store.Workspace, error)
	DeleteFunc             func(ctx context.Context, workspaceID string) error
	GetHostPathFunc        func(ctx context.Context, workspaceID string) (string, error)
	EnsureHostDirectoryFunc func(ctx context.Context, workspaceID string) (string, error)
	CleanupHostDirectoryFunc func(ctx context.Context, workspaceID string) error
	IsInUseFunc            func(ctx context.Context, workspaceID string) (bool, error)
}

func (m *testWorkspaceManager) Create(ctx context.Context, opts core.CreateWorkspaceOptions) (*store.Workspace, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, opts)
	}
	return nil, errors.New("not implemented")
}

func (m *testWorkspaceManager) Get(ctx context.Context, workspaceID string) (*store.Workspace, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, workspaceID)
	}
	return nil, errors.New("not implemented")
}

func (m *testWorkspaceManager) List(ctx context.Context, opts core.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, opts)
	}
	return nil, errors.New("not implemented")
}

func (m *testWorkspaceManager) Update(ctx context.Context, workspaceID string, updates store.WorkspaceUpdates) (*store.Workspace, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, workspaceID, updates)
	}
	return nil, errors.New("not implemented")
}

func (m *testWorkspaceManager) Delete(ctx context.Context, workspaceID string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, workspaceID)
	}
	return errors.New("not implemented")
}

func (m *testWorkspaceManager) GetHostPath(ctx context.Context, workspaceID string) (string, error) {
	if m.GetHostPathFunc != nil {
		return m.GetHostPathFunc(ctx, workspaceID)
	}
	return "", errors.New("not implemented")
}

func (m *testWorkspaceManager) EnsureHostDirectory(ctx context.Context, workspaceID string) (string, error) {
	if m.EnsureHostDirectoryFunc != nil {
		return m.EnsureHostDirectoryFunc(ctx, workspaceID)
	}
	return "", errors.New("not implemented")
}

func (m *testWorkspaceManager) CleanupHostDirectory(ctx context.Context, workspaceID string) error {
	if m.CleanupHostDirectoryFunc != nil {
		return m.CleanupHostDirectoryFunc(ctx, workspaceID)
	}
	return errors.New("not implemented")
}

func (m *testWorkspaceManager) IsInUse(ctx context.Context, workspaceID string) (bool, error) {
	if m.IsInUseFunc != nil {
		return m.IsInUseFunc(ctx, workspaceID)
	}
	return false, errors.New("not implemented")
}

func TestNewWorkspaceAdapter(t *testing.T) {
	mock := &testWorkspaceManager{}
	adapter := NewWorkspaceAdapter(mock)
	assert.NotNil(t, adapter)
	assert.Equal(t, mock, adapter.manager)
}

func boolPtr(b bool) *bool { return &b }

func TestWorkspaceAdapter_Create(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("success", func(t *testing.T) {
		mock := &testWorkspaceManager{
			CreateFunc: func(_ context.Context, opts core.CreateWorkspaceOptions) (*store.Workspace, error) {
				assert.Equal(t, "test-workspace", opts.Name)
				assert.NotNil(t, opts.Persist)
				assert.True(t, *opts.Persist)
				assert.Equal(t, "volume", opts.StorageType)
				return &store.Workspace{
					ID:        "ws_test123",
					Name:      opts.Name,
					Persist:   true,
					CreatedAt: now,
				}, nil
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		ws, err := adapter.Create(ctx, CreateWorkspaceOptions{
			Name:        "test-workspace",
			Persist:     boolPtr(true),
			StorageType: "volume",
		})

		require.NoError(t, err)
		assert.Equal(t, "ws_test123", ws.ID)
		assert.Equal(t, "test-workspace", ws.Name)
	})

	t.Run("error", func(t *testing.T) {
		mock := &testWorkspaceManager{
			CreateFunc: func(_ context.Context, _ core.CreateWorkspaceOptions) (*store.Workspace, error) {
				return nil, errors.New("create failed")
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		_, err := adapter.Create(ctx, CreateWorkspaceOptions{Name: "test"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create failed")
	})
}

func TestWorkspaceAdapter_Get(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := &testWorkspaceManager{
			GetFunc: func(_ context.Context, id string) (*store.Workspace, error) {
				assert.Equal(t, "ws_test123", id)
				return &store.Workspace{
					ID:   id,
					Name: "test-workspace",
				}, nil
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		ws, err := adapter.Get(ctx, "ws_test123")
		require.NoError(t, err)
		assert.Equal(t, "ws_test123", ws.ID)
	})

	t.Run("not found", func(t *testing.T) {
		mock := &testWorkspaceManager{
			GetFunc: func(_ context.Context, _ string) (*store.Workspace, error) {
				return nil, store.ErrNotFound
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		_, err := adapter.Get(ctx, "ws_notfound")
		require.Error(t, err)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

func TestWorkspaceAdapter_List(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := &testWorkspaceManager{
			ListFunc: func(_ context.Context, opts core.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
				assert.Equal(t, 10, opts.Limit)
				return &store.ListResult[store.Workspace]{
					Items: []*store.Workspace{
						{ID: "ws_1", Name: "workspace-1"},
						{ID: "ws_2", Name: "workspace-2"},
					},
					TotalCount: 2,
				}, nil
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		result, err := adapter.List(ctx, ListWorkspacesOptions{Limit: 10})
		require.NoError(t, err)
		assert.Len(t, result.Items, 2)
		assert.Equal(t, int64(2), result.TotalCount)
	})
}

func TestWorkspaceAdapter_Update(t *testing.T) {
	ctx := context.Background()

	t.Run("success with name", func(t *testing.T) {
		newName := "updated-name"
		mock := &testWorkspaceManager{
			UpdateFunc: func(_ context.Context, id string, updates store.WorkspaceUpdates) (*store.Workspace, error) {
				assert.Equal(t, "ws_test123", id)
				assert.Equal(t, &newName, updates.Name)
				return &store.Workspace{
					ID:   id,
					Name: newName,
				}, nil
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		ws, err := adapter.Update(ctx, "ws_test123", UpdateWorkspaceOptions{
			Name: &newName,
		})
		require.NoError(t, err)
		assert.Equal(t, "updated-name", ws.Name)
	})

	t.Run("success with labels", func(t *testing.T) {
		labels := map[string]string{"env": "test"}
		mock := &testWorkspaceManager{
			UpdateFunc: func(_ context.Context, id string, updates store.WorkspaceUpdates) (*store.Workspace, error) {
				assert.NotNil(t, updates.Labels)
				var parsedLabels map[string]string
				err := json.Unmarshal(updates.Labels, &parsedLabels)
				require.NoError(t, err)
				assert.Equal(t, "test", parsedLabels["env"])
				return &store.Workspace{ID: id}, nil
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		_, err := adapter.Update(ctx, "ws_test123", UpdateWorkspaceOptions{
			Labels: labels,
		})
		require.NoError(t, err)
	})

	t.Run("success with annotations", func(t *testing.T) {
		annotations := map[string]string{"description": "test workspace"}
		mock := &testWorkspaceManager{
			UpdateFunc: func(_ context.Context, id string, updates store.WorkspaceUpdates) (*store.Workspace, error) {
				assert.NotNil(t, updates.Annotations)
				var parsedAnnotations map[string]string
				err := json.Unmarshal(updates.Annotations, &parsedAnnotations)
				require.NoError(t, err)
				assert.Equal(t, "test workspace", parsedAnnotations["description"])
				return &store.Workspace{ID: id}, nil
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		_, err := adapter.Update(ctx, "ws_test123", UpdateWorkspaceOptions{
			Annotations: annotations,
		})
		require.NoError(t, err)
	})

	t.Run("not found", func(t *testing.T) {
		mock := &testWorkspaceManager{
			UpdateFunc: func(_ context.Context, _ string, _ store.WorkspaceUpdates) (*store.Workspace, error) {
				return nil, store.ErrNotFound
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		_, err := adapter.Update(ctx, "ws_notfound", UpdateWorkspaceOptions{})
		require.Error(t, err)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}

func TestWorkspaceAdapter_Delete(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mock := &testWorkspaceManager{
			DeleteFunc: func(_ context.Context, id string) error {
				assert.Equal(t, "ws_test123", id)
				return nil
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		err := adapter.Delete(ctx, "ws_test123")
		require.NoError(t, err)
	})

	t.Run("not found", func(t *testing.T) {
		mock := &testWorkspaceManager{
			DeleteFunc: func(_ context.Context, _ string) error {
				return store.ErrNotFound
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		err := adapter.Delete(ctx, "ws_notfound")
		require.Error(t, err)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("in use error", func(t *testing.T) {
		mock := &testWorkspaceManager{
			DeleteFunc: func(_ context.Context, _ string) error {
				return core.ErrWorkspaceInUse
			},
		}
		adapter := NewWorkspaceAdapter(mock)

		err := adapter.Delete(ctx, "ws_inuse")
		require.Error(t, err)
		assert.ErrorIs(t, err, core.ErrWorkspaceInUse)
	})
}
