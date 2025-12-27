package api

import (
	"context"
	"testing"

	"github.com/chunlea/marionette/pkg/store"
)

// mockSessionManager implements a minimal interface for testing SessionAdapter.
type mockSessionManager struct {
	createFunc    func(ctx context.Context, opts interface{}) (*store.Session, error)
	getFunc       func(ctx context.Context, id string) (*store.Session, error)
	listFunc      func(ctx context.Context, opts interface{}) (*store.ListResult[store.Session], error)
	suspendFunc   func(ctx context.Context, id, strategy string) error
	resumeFunc    func(ctx context.Context, id string) error
	terminateFunc func(ctx context.Context, id string) error
}

// mockWorkspaceStore implements workspace creation for testing.
type mockWorkspaceStore struct {
	workspaces map[string]*store.Workspace
}

func newMockWorkspaceStore() *mockWorkspaceStore {
	return &mockWorkspaceStore{
		workspaces: make(map[string]*store.Workspace),
	}
}

func (s *mockWorkspaceStore) CreateWorkspace(ctx context.Context, ws *store.Workspace) error {
	s.workspaces[ws.ID] = ws
	return nil
}

func TestNewSessionAdapter(t *testing.T) {
	adapter := NewSessionAdapter(nil, nil)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}

func TestSessionAdapter_ensureWorkspace(t *testing.T) {
	st := newMockWorkspaceStore()
	adapter := &SessionAdapter{store: st}

	ctx := context.Background()

	// Test with name
	wsID, err := adapter.ensureWorkspace(ctx, "my-workspace")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wsID == "" {
		t.Fatal("expected non-empty workspace ID")
	}

	// Verify workspace was created with correct properties
	ws := st.workspaces[wsID]
	if ws == nil {
		t.Fatal("workspace not created")
	}
	if ws.Name != "my-workspace" {
		t.Errorf("expected name 'my-workspace', got %q", ws.Name)
	}
	if ws.StorageType != "volume" {
		t.Errorf("expected storage_type 'volume', got %q", ws.StorageType)
	}
	if ws.Mobility != "local" {
		t.Errorf("expected mobility 'local', got %q", ws.Mobility)
	}
	if !ws.Persist {
		t.Error("expected persist to be true")
	}
}

func TestSessionAdapter_ensureWorkspace_EmptyName(t *testing.T) {
	st := newMockWorkspaceStore()
	adapter := &SessionAdapter{store: st}

	ctx := context.Background()

	// Test with empty name - should generate a name
	wsID, err := adapter.ensureWorkspace(ctx, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ws := st.workspaces[wsID]
	if ws == nil {
		t.Fatal("workspace not created")
	}
	// Name should start with "workspace-"
	if len(ws.Name) < 10 || ws.Name[:10] != "workspace-" {
		t.Errorf("expected name starting with 'workspace-', got %q", ws.Name)
	}
}

// Verify SessionAdapter implements SessionService interface
var _ SessionService = (*SessionAdapter)(nil)
