package api

import (
	"context"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
)

// mockWorkspaceManager implements core.WorkspaceManagerInterface for testing.
type mockWorkspaceManager struct {
	workspaces map[string]*store.Workspace
}

func newMockWorkspaceManager() *mockWorkspaceManager {
	return &mockWorkspaceManager{
		workspaces: make(map[string]*store.Workspace),
	}
}

func (m *mockWorkspaceManager) Create(_ context.Context, opts core.CreateWorkspaceOptions) (*store.Workspace, error) {
	wsID := id.Workspace()
	name := opts.Name
	if name == "" {
		name = "workspace-" + wsID
	}

	ws := &store.Workspace{
		ID:          wsID,
		Name:        name,
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	m.workspaces[ws.ID] = ws
	return ws, nil
}

func (m *mockWorkspaceManager) Get(_ context.Context, workspaceID string) (*store.Workspace, error) {
	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return nil, core.ErrWorkspaceNotFound
	}
	return ws, nil
}

func (m *mockWorkspaceManager) List(_ context.Context, _ core.ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	var items []*store.Workspace
	for _, ws := range m.workspaces {
		items = append(items, ws)
	}
	return &store.ListResult[store.Workspace]{
		Items:      items,
		TotalCount: int64(len(items)),
	}, nil
}

func (m *mockWorkspaceManager) Update(_ context.Context, workspaceID string, updates store.WorkspaceUpdates) (*store.Workspace, error) {
	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return nil, core.ErrWorkspaceNotFound
	}
	if updates.Name != nil {
		ws.Name = *updates.Name
	}
	ws.UpdatedAt = time.Now()
	return ws, nil
}

func (m *mockWorkspaceManager) Delete(_ context.Context, workspaceID string) error {
	if _, ok := m.workspaces[workspaceID]; !ok {
		return core.ErrWorkspaceNotFound
	}
	delete(m.workspaces, workspaceID)
	return nil
}

func (m *mockWorkspaceManager) GetHostPath(_ context.Context, workspaceID string) (string, error) {
	return "/var/marionette/workspaces/" + workspaceID, nil
}

func (m *mockWorkspaceManager) EnsureHostDirectory(_ context.Context, workspaceID string) (string, error) {
	return "/var/marionette/workspaces/" + workspaceID, nil
}

func (m *mockWorkspaceManager) CleanupHostDirectory(_ context.Context, _ string) error {
	return nil
}

func (m *mockWorkspaceManager) IsInUse(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func TestNewSessionAdapter(t *testing.T) {
	adapter := NewSessionAdapter(nil, nil)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}

func TestSessionAdapter_ensureWorkspace(t *testing.T) {
	wsMgr := newMockWorkspaceManager()
	adapter := &SessionAdapter{workspaceManager: wsMgr}

	ctx := context.Background()

	// Test with name
	opts := CreateSessionOptions{
		Name:  "my-workspace",
		Agent: "claude",
	}
	wsID, err := adapter.ensureWorkspace(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wsID == "" {
		t.Fatal("expected non-empty workspace ID")
	}

	// Verify workspace was created with correct properties
	ws := wsMgr.workspaces[wsID]
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
	wsMgr := newMockWorkspaceManager()
	adapter := &SessionAdapter{workspaceManager: wsMgr}

	ctx := context.Background()

	// Test with empty name - should generate a name
	opts := CreateSessionOptions{
		Agent: "claude",
	}
	wsID, err := adapter.ensureWorkspace(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ws := wsMgr.workspaces[wsID]
	if ws == nil {
		t.Fatal("workspace not created")
	}
	// Name should start with "workspace-"
	if len(ws.Name) < 10 || ws.Name[:10] != "workspace-" {
		t.Errorf("expected name starting with 'workspace-', got %q", ws.Name)
	}
}

func TestSessionAdapter_ensureWorkspace_ExistingWorkspace(t *testing.T) {
	wsMgr := newMockWorkspaceManager()
	adapter := &SessionAdapter{workspaceManager: wsMgr}

	ctx := context.Background()

	// Create a workspace first
	existingWs, err := wsMgr.Create(ctx, core.CreateWorkspaceOptions{
		Name: "existing-workspace",
	})
	if err != nil {
		t.Fatalf("failed to create workspace: %v", err)
	}

	// Test with existing workspace ID
	opts := CreateSessionOptions{
		Agent:       "claude",
		WorkspaceID: existingWs.ID,
	}
	wsID, err := adapter.ensureWorkspace(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wsID != existingWs.ID {
		t.Errorf("expected workspace ID %q, got %q", existingWs.ID, wsID)
	}
}

func TestSessionAdapter_ensureWorkspace_NonExistingWorkspace(t *testing.T) {
	wsMgr := newMockWorkspaceManager()
	adapter := &SessionAdapter{workspaceManager: wsMgr}

	ctx := context.Background()

	// Test with non-existing workspace ID
	opts := CreateSessionOptions{
		Agent:       "claude",
		WorkspaceID: "ws_nonexistent",
	}
	_, err := adapter.ensureWorkspace(ctx, opts)
	if err == nil {
		t.Fatal("expected error for non-existing workspace")
	}
	if err != core.ErrWorkspaceNotFound {
		t.Errorf("expected ErrWorkspaceNotFound, got %v", err)
	}
}

// Verify SessionAdapter implements SessionService interface
var _ SessionService = (*SessionAdapter)(nil)
