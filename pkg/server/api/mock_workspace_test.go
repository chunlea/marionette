package api

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
)

// MockWorkspaceService implements WorkspaceService for testing.
type MockWorkspaceService struct {
	mu         sync.RWMutex
	workspaces map[string]*store.Workspace
}

// NewMockWorkspaceService creates a new MockWorkspaceService.
func NewMockWorkspaceService() *MockWorkspaceService {
	return &MockWorkspaceService{
		workspaces: make(map[string]*store.Workspace),
	}
}

// AddWorkspace adds a workspace to the mock store for testing.
func (m *MockWorkspaceService) AddWorkspace(ws *store.Workspace) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.workspaces[ws.ID] = ws
}

// Create creates a new workspace.
func (m *MockWorkspaceService) Create(_ context.Context, opts CreateWorkspaceOptions) (*store.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	wsID := id.Workspace()
	name := opts.Name
	if name == "" {
		name = "workspace-" + wsID
	}

	persist := true
	if opts.Persist != nil {
		persist = *opts.Persist
	}

	storageType := "volume"
	if opts.StorageType != "" {
		storageType = opts.StorageType
	}

	mobility := "local"
	if opts.Mobility != "" {
		mobility = opts.Mobility
	}

	labels, _ := json.Marshal(opts.Labels)
	if len(labels) == 0 {
		labels = []byte("{}")
	}
	annotations, _ := json.Marshal(opts.Annotations)
	if len(annotations) == 0 {
		annotations = []byte("{}")
	}

	ws := &store.Workspace{
		ID:          wsID,
		Name:        name,
		Persist:     persist,
		StorageType: storageType,
		Mobility:    mobility,
		DiskQuotaMB: opts.DiskQuotaMB,
		Labels:      labels,
		Annotations: annotations,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	m.workspaces[ws.ID] = ws
	return ws, nil
}

// Get retrieves a workspace by ID.
func (m *MockWorkspaceService) Get(_ context.Context, workspaceID string) (*store.Workspace, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return nil, core.ErrWorkspaceNotFound
	}

	if ws.DeletedAt != nil {
		return nil, core.ErrWorkspaceDeleted
	}

	return ws, nil
}

// List returns workspaces matching the filter options.
func (m *MockWorkspaceService) List(_ context.Context, opts ListWorkspacesOptions) (*store.ListResult[store.Workspace], error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var items []*store.Workspace
	for _, ws := range m.workspaces {
		if ws.DeletedAt != nil {
			continue
		}
		items = append(items, ws)
	}

	totalCount := int64(len(items))
	hasMore := false

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

// Update updates a workspace.
func (m *MockWorkspaceService) Update(_ context.Context, workspaceID string, opts UpdateWorkspaceOptions) (*store.Workspace, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return nil, core.ErrWorkspaceNotFound
	}

	if ws.DeletedAt != nil {
		return nil, core.ErrWorkspaceDeleted
	}

	if opts.Name != nil {
		ws.Name = *opts.Name
	}
	if opts.Persist != nil {
		ws.Persist = *opts.Persist
	}
	if opts.DiskQuotaMB != nil {
		ws.DiskQuotaMB = opts.DiskQuotaMB
	}
	if opts.Labels != nil {
		labels, _ := json.Marshal(opts.Labels)
		ws.Labels = labels
	}

	ws.UpdatedAt = time.Now()
	return ws, nil
}

// Delete deletes a workspace.
func (m *MockWorkspaceService) Delete(_ context.Context, workspaceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, ok := m.workspaces[workspaceID]
	if !ok {
		return core.ErrWorkspaceNotFound
	}

	now := time.Now()
	ws.DeletedAt = &now
	return nil
}

// Ensure MockWorkspaceService implements WorkspaceService.
var _ WorkspaceService = (*MockWorkspaceService)(nil)
