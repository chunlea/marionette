package api

import (
	"context"
	"encoding/json"
	"errors"
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

// mockSessionManager implements SessionManagerInterface for testing.
type mockSessionManager struct {
	sessions            map[string]*store.Session
	createErr           error
	getErr              error
	listErr             error
	suspendErr          error
	resumeErr           error
	terminateErr        error
	lastCreateOpts      *core.CreateSessionOptions
	lastListOpts        *store.ListSessionsOptions
	lastSuspendStrategy string
}

func newMockSessionManager() *mockSessionManager {
	return &mockSessionManager{
		sessions: make(map[string]*store.Session),
	}
}

func (m *mockSessionManager) Create(_ context.Context, opts core.CreateSessionOptions) (*store.Session, error) {
	m.lastCreateOpts = &opts
	if m.createErr != nil {
		return nil, m.createErr
	}
	sessID := id.Session()

	// Convert labels map to json.RawMessage
	var labelsJSON json.RawMessage
	if opts.Labels != nil {
		labelsJSON, _ = json.Marshal(opts.Labels)
	} else {
		labelsJSON = json.RawMessage("{}")
	}

	sess := &store.Session{
		ID:            sessID,
		Agent:         opts.Agent,
		Status:        "pending",
		WorkspaceID:   opts.WorkspaceID,
		LifecycleMode: opts.LifecycleMode,
		NetworkPolicy: opts.NetworkPolicy,
		AllowedHosts:  opts.AllowedHosts,
		Labels:        labelsJSON,
		IsBYOK:        opts.IsBYOK,
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	if opts.Name != nil {
		sess.Name = opts.Name
	}
	m.sessions[sessID] = sess
	return sess, nil
}

func (m *mockSessionManager) Get(_ context.Context, sessionID string) (*store.Session, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	sess, ok := m.sessions[sessionID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return sess, nil
}

func (m *mockSessionManager) List(_ context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	m.lastListOpts = &opts
	if m.listErr != nil {
		return nil, m.listErr
	}
	var items []*store.Session
	for _, sess := range m.sessions {
		if len(opts.Status) > 0 {
			found := false
			for _, s := range opts.Status {
				if sess.Status == s {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		items = append(items, sess)
	}
	return &store.ListResult[store.Session]{Items: items, TotalCount: int64(len(items))}, nil
}

func (m *mockSessionManager) Suspend(_ context.Context, sessionID string, strategy string) error {
	m.lastSuspendStrategy = strategy
	if m.suspendErr != nil {
		return m.suspendErr
	}
	sess, ok := m.sessions[sessionID]
	if !ok {
		return store.ErrNotFound
	}
	sess.Status = "suspended"
	return nil
}

func (m *mockSessionManager) Resume(_ context.Context, sessionID string) error {
	if m.resumeErr != nil {
		return m.resumeErr
	}
	sess, ok := m.sessions[sessionID]
	if !ok {
		return store.ErrNotFound
	}
	sess.Status = "resuming"
	return nil
}

func (m *mockSessionManager) Terminate(_ context.Context, sessionID string) error {
	if m.terminateErr != nil {
		return m.terminateErr
	}
	sess, ok := m.sessions[sessionID]
	if !ok {
		return store.ErrNotFound
	}
	sess.Status = "terminated"
	return nil
}

// Verify mockSessionManager implements SessionManagerInterface.
var _ SessionManagerInterface = (*mockSessionManager)(nil)

func TestNewSessionAdapter(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.manager != sessMgr {
		t.Error("expected session manager to be set")
	}
	if adapter.workspaceManager != wsMgr {
		t.Error("expected workspace manager to be set")
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

func TestSessionAdapter_Create(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	opts := CreateSessionOptions{
		Name:               "test-session",
		Agent:              "claude",
		LifecycleMode:      "on_demand",
		IdleTimeoutSeconds: 3600,
		NetworkPolicy:      "allow_list",
		AllowedHosts:       []string{"github.com"},
		Labels:             map[string]string{"env": "test"},
	}

	sess, err := adapter.Create(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sess == nil {
		t.Fatal("expected non-nil session")
	}
	if sess.Agent != "claude" {
		t.Errorf("expected agent 'claude', got %q", sess.Agent)
	}

	// Verify core options were set correctly
	if sessMgr.lastCreateOpts == nil {
		t.Fatal("expected lastCreateOpts to be set")
	}
	if sessMgr.lastCreateOpts.Name == nil || *sessMgr.lastCreateOpts.Name != "test-session" {
		t.Error("expected Name to be set")
	}
	if sessMgr.lastCreateOpts.IdleTimeout == nil || *sessMgr.lastCreateOpts.IdleTimeout != 3600 {
		t.Error("expected IdleTimeout to be set")
	}
}

func TestSessionAdapter_Create_WithBYOK(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	opts := CreateSessionOptions{
		Agent:  "claude",
		APIKey: "sk-secret-key",
	}

	sess, err := adapter.Create(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !sess.IsBYOK {
		t.Error("expected IsBYOK to be true")
	}
}

func TestSessionAdapter_Create_WithAgentConfigID(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	opts := CreateSessionOptions{
		Agent:         "claude",
		AgentConfigID: "acfg_123",
	}

	_, err := adapter.Create(ctx, opts)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if sessMgr.lastCreateOpts.AgentConfigID == nil || *sessMgr.lastCreateOpts.AgentConfigID != "acfg_123" {
		t.Error("expected AgentConfigID to be set")
	}
}

func TestSessionAdapter_Create_Error(t *testing.T) {
	sessMgr := newMockSessionManager()
	sessMgr.createErr = errors.New("create error")
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	_, err := adapter.Create(ctx, CreateSessionOptions{Agent: "claude"})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "create error" {
		t.Errorf("expected 'create error', got %q", err.Error())
	}
}

func TestSessionAdapter_Get(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	// Create a session first
	sess, _ := adapter.Create(ctx, CreateSessionOptions{Agent: "claude", Name: "test"})

	// Get it
	got, err := adapter.Get(ctx, sess.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != sess.ID {
		t.Errorf("expected ID %q, got %q", sess.ID, got.ID)
	}
}

func TestSessionAdapter_Get_NotFound(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	_, err := adapter.Get(ctx, "sess_nonexistent")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestSessionAdapter_Get_Error(t *testing.T) {
	sessMgr := newMockSessionManager()
	sessMgr.getErr = errors.New("get error")
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	_, err := adapter.Get(ctx, "sess_123")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "get error" {
		t.Errorf("expected 'get error', got %q", err.Error())
	}
}

func TestSessionAdapter_List(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	// Create sessions
	_, _ = adapter.Create(ctx, CreateSessionOptions{Agent: "claude", Name: "session1"})
	_, _ = adapter.Create(ctx, CreateSessionOptions{Agent: "claude", Name: "session2"})

	// List all
	result, err := adapter.List(ctx, ListSessionsOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(result.Items))
	}
}

func TestSessionAdapter_List_WithFilters(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	// List with filters
	_, err := adapter.List(ctx, ListSessionsOptions{
		Limit:  10,
		Cursor: "cursor123",
		Status: []string{"active"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify options were passed
	if sessMgr.lastListOpts == nil {
		t.Fatal("expected lastListOpts to be set")
	}
	if sessMgr.lastListOpts.Limit != 10 {
		t.Errorf("expected Limit 10, got %d", sessMgr.lastListOpts.Limit)
	}
	if sessMgr.lastListOpts.Cursor != "cursor123" {
		t.Errorf("expected Cursor 'cursor123', got %q", sessMgr.lastListOpts.Cursor)
	}
}

func TestSessionAdapter_List_Error(t *testing.T) {
	sessMgr := newMockSessionManager()
	sessMgr.listErr = errors.New("list error")
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	_, err := adapter.List(ctx, ListSessionsOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "list error" {
		t.Errorf("expected 'list error', got %q", err.Error())
	}
}

func TestSessionAdapter_Suspend(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	// Create a session
	sess, _ := adapter.Create(ctx, CreateSessionOptions{Agent: "claude"})

	// Suspend it
	err := adapter.Suspend(ctx, sess.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify strategy was "terminate"
	if sessMgr.lastSuspendStrategy != "terminate" {
		t.Errorf("expected strategy 'terminate', got %q", sessMgr.lastSuspendStrategy)
	}

	// Verify status changed
	if sessMgr.sessions[sess.ID].Status != "suspended" {
		t.Errorf("expected status 'suspended', got %q", sessMgr.sessions[sess.ID].Status)
	}
}

func TestSessionAdapter_Suspend_Error(t *testing.T) {
	sessMgr := newMockSessionManager()
	sessMgr.suspendErr = errors.New("suspend error")
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	err := adapter.Suspend(ctx, "sess_123")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "suspend error" {
		t.Errorf("expected 'suspend error', got %q", err.Error())
	}
}

func TestSessionAdapter_Resume(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	// Create and suspend a session
	sess, _ := adapter.Create(ctx, CreateSessionOptions{Agent: "claude"})
	_ = adapter.Suspend(ctx, sess.ID)

	// Resume it
	err := adapter.Resume(ctx, sess.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify status changed
	if sessMgr.sessions[sess.ID].Status != "resuming" {
		t.Errorf("expected status 'resuming', got %q", sessMgr.sessions[sess.ID].Status)
	}
}

func TestSessionAdapter_Resume_Error(t *testing.T) {
	sessMgr := newMockSessionManager()
	sessMgr.resumeErr = errors.New("resume error")
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	err := adapter.Resume(ctx, "sess_123")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "resume error" {
		t.Errorf("expected 'resume error', got %q", err.Error())
	}
}

func TestSessionAdapter_Terminate(t *testing.T) {
	sessMgr := newMockSessionManager()
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	// Create a session
	sess, _ := adapter.Create(ctx, CreateSessionOptions{Agent: "claude"})

	// Terminate it
	err := adapter.Terminate(ctx, sess.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify status changed
	if sessMgr.sessions[sess.ID].Status != "terminated" {
		t.Errorf("expected status 'terminated', got %q", sessMgr.sessions[sess.ID].Status)
	}
}

func TestSessionAdapter_Terminate_Error(t *testing.T) {
	sessMgr := newMockSessionManager()
	sessMgr.terminateErr = errors.New("terminate error")
	wsMgr := newMockWorkspaceManager()
	adapter := NewSessionAdapter(sessMgr, wsMgr)

	ctx := context.Background()

	err := adapter.Terminate(ctx, "sess_123")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "terminate error" {
		t.Errorf("expected 'terminate error', got %q", err.Error())
	}
}

// Verify SessionAdapter implements SessionService interface.
var _ SessionService = (*SessionAdapter)(nil)
