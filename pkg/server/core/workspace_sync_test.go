package core

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/config"
	"github.com/chunlea/marionette/pkg/store"
)

// syncWorkspaceStore is a WorkspaceStore that records the updates it is asked
// to apply, so the bookkeeping can be asserted without a database.
type syncWorkspaceStore struct {
	workspaces map[string]*store.Workspace
	applied    []store.WorkspaceUpdates
	updateErr  error
}

func newSyncWorkspaceStore(workspaces ...*store.Workspace) *syncWorkspaceStore {
	s := &syncWorkspaceStore{workspaces: make(map[string]*store.Workspace, len(workspaces))}
	for _, ws := range workspaces {
		s.workspaces[ws.ID] = ws
	}
	return s
}

func (s *syncWorkspaceStore) CreateWorkspace(context.Context, *store.Workspace) error { return nil }

func (s *syncWorkspaceStore) GetWorkspace(_ context.Context, workspaceID string) (*store.Workspace, error) {
	ws, ok := s.workspaces[workspaceID]
	if !ok {
		return nil, &store.NotFoundError{Resource: "workspace", ID: workspaceID}
	}
	return ws, nil
}

func (s *syncWorkspaceStore) ListWorkspaces(context.Context, store.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	return &store.ListResult[store.Workspace]{}, nil
}

func (s *syncWorkspaceStore) UpdateWorkspace(_ context.Context, workspaceID string, updates store.WorkspaceUpdates) error {
	if s.updateErr != nil {
		return s.updateErr
	}

	s.applied = append(s.applied, updates)

	ws, ok := s.workspaces[workspaceID]
	if !ok {
		return &store.NotFoundError{Resource: "workspace", ID: workspaceID}
	}
	if updates.StorageKey != nil {
		ws.StorageKey = updates.StorageKey
	}
	if updates.StorageSizeBytes != nil {
		ws.StorageSizeBytes = updates.StorageSizeBytes
	}
	if updates.LastSyncedAt != nil {
		ws.LastSyncedAt = updates.LastSyncedAt
	}
	return nil
}

func (s *syncWorkspaceStore) DeleteWorkspace(context.Context, string) error { return nil }

func (s *syncWorkspaceStore) ListSessions(context.Context, store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	return &store.ListResult[store.Session]{}, nil
}

func newSyncManager(t *testing.T, s WorkspaceStore) *WorkspaceManager {
	t.Helper()
	return NewWorkspaceManager(s, config.WorkspaceStorageConfig{BaseDir: t.TempDir()}, zap.NewNop())
}

func TestRecordSyncWritesTheBookkeeping(t *testing.T) {
	ctx := context.Background()

	ws := &store.Workspace{ID: "ws_1", Name: "project"}
	s := newSyncWorkspaceStore(ws)
	m := newSyncManager(t, s)

	manifest := &store.Manifest{ID: "mfst_1", WorkspaceID: "ws_1", TotalSize: 4096}

	got, err := m.RecordSync(ctx, "ws_1", manifest)
	if err != nil {
		t.Fatalf("RecordSync() error = %v", err)
	}

	if got.StorageKey == nil || *got.StorageKey != "mfst_1" {
		t.Errorf("StorageKey = %v, want the manifest id — a resume has no other way to find it", got.StorageKey)
	}
	if got.StorageSizeBytes == nil || *got.StorageSizeBytes != 4096 {
		t.Errorf("StorageSizeBytes = %v, want 4096", got.StorageSizeBytes)
	}
	if got.LastSyncedAt == nil {
		t.Error("LastSyncedAt was not recorded")
	}

	if len(s.applied) != 1 {
		t.Fatalf("applied %d updates, want 1", len(s.applied))
	}
}

// TestRecordSyncRejectsAForeignManifest guards the failure that would be
// silent: pointing a workspace at another workspace's manifest restores the
// wrong contents on the next resume.
func TestRecordSyncRejectsAForeignManifest(t *testing.T) {
	ctx := context.Background()

	s := newSyncWorkspaceStore(&store.Workspace{ID: "ws_1"}, &store.Workspace{ID: "ws_2"})
	m := newSyncManager(t, s)

	manifest := &store.Manifest{ID: "mfst_other", WorkspaceID: "ws_2", TotalSize: 10}

	_, err := m.RecordSync(ctx, "ws_1", manifest)
	if !errors.Is(err, store.ErrInvalidInput) {
		t.Fatalf("RecordSync() error = %v, want ErrInvalidInput", err)
	}
	if len(s.applied) != 0 {
		t.Error("a foreign manifest was recorded against the workspace anyway")
	}
}

func TestRecordSyncRejectsBadInput(t *testing.T) {
	ctx := context.Background()
	m := newSyncManager(t, newSyncWorkspaceStore(&store.Workspace{ID: "ws_1"}))

	t.Run("nil manifest", func(t *testing.T) {
		if _, err := m.RecordSync(ctx, "ws_1", nil); !errors.Is(err, store.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("manifest with no id", func(t *testing.T) {
		_, err := m.RecordSync(ctx, "ws_1", &store.Manifest{WorkspaceID: "ws_1"})
		if !errors.Is(err, store.ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	})

	t.Run("unknown workspace", func(t *testing.T) {
		// Get translates the store's not-found into this package's sentinel,
		// which is the convention every other manager method follows.
		_, err := m.RecordSync(ctx, "ws_missing", &store.Manifest{ID: "mfst_1", WorkspaceID: "ws_missing"})
		if !errors.Is(err, ErrWorkspaceNotFound) {
			t.Fatalf("error = %v, want ErrWorkspaceNotFound", err)
		}
	})
}

func TestRecordSyncPropagatesStoreFailures(t *testing.T) {
	ctx := context.Background()

	boom := errors.New("database down")
	s := newSyncWorkspaceStore(&store.Workspace{ID: "ws_1"})
	s.updateErr = boom
	m := newSyncManager(t, s)

	_, err := m.RecordSync(ctx, "ws_1", &store.Manifest{ID: "mfst_1", WorkspaceID: "ws_1"})
	if !errors.Is(err, boom) {
		t.Fatalf("RecordSync() error = %v, want %v", err, boom)
	}
}
