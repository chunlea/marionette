package public

import (
	"context"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// MockPermissionService is a mock implementation of PermissionService for testing.
type MockPermissionService struct {
	mu          sync.RWMutex
	permissions map[string]*store.PermissionRequest

	// Function stubs for custom behavior
	GetFunc     func(ctx context.Context, id string) (*store.PermissionRequest, error)
	ListFunc    func(ctx context.Context, opts ListPermissionsOptions) (*store.ListResult[store.PermissionRequest], error)
	ApproveFunc func(ctx context.Context, id string, opts ApproveOptions) error
	DenyFunc    func(ctx context.Context, id string, opts DenyOptions) error
}

// NewMockPermissionService creates a new MockPermissionService with an empty store.
func NewMockPermissionService() *MockPermissionService {
	return &MockPermissionService{
		permissions: make(map[string]*store.PermissionRequest),
	}
}

// Get retrieves a permission request by ID.
func (m *MockPermissionService) Get(ctx context.Context, permID string) (*store.PermissionRequest, error) {
	if m.GetFunc != nil {
		return m.GetFunc(ctx, permID)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	perm, ok := m.permissions[permID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return perm, nil
}

// List returns permission requests matching the filter options.
func (m *MockPermissionService) List(ctx context.Context, opts ListPermissionsOptions) (*store.ListResult[store.PermissionRequest], error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, opts)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	items := make([]*store.PermissionRequest, 0, len(m.permissions))
	for _, perm := range m.permissions {
		// Apply filters
		if opts.SessionID != "" && perm.SessionID != opts.SessionID {
			continue
		}
		if opts.TaskID != "" && perm.TaskID != opts.TaskID {
			continue
		}
		if len(opts.Status) > 0 && !contains(opts.Status, perm.Status) {
			continue
		}
		if len(opts.RiskLevel) > 0 && !contains(opts.RiskLevel, perm.RiskLevel) {
			continue
		}
		items = append(items, perm)
	}

	// Apply limit
	limit := opts.Limit
	if limit <= 0 {
		limit = 50
	}
	if len(items) > limit {
		items = items[:limit]
	}

	return &store.ListResult[store.PermissionRequest]{
		Items:      items,
		TotalCount: int64(len(items)),
		HasMore:    false,
	}, nil
}

// Approve approves a pending permission request.
func (m *MockPermissionService) Approve(ctx context.Context, permID string, opts ApproveOptions) error {
	if m.ApproveFunc != nil {
		return m.ApproveFunc(ctx, permID, opts)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	perm, ok := m.permissions[permID]
	if !ok {
		return store.ErrNotFound
	}

	if perm.Status != "pending" {
		return &InvalidStateError{
			Resource: "permission_request",
			ID:       permID,
			Current:  perm.Status,
			Expected: "pending",
		}
	}

	now := time.Now()
	perm.Status = "approved"
	perm.RespondedAt = &now
	perm.UpdatedAt = now

	if opts.Reason != "" {
		perm.ResponseReason = &opts.Reason
	}

	return nil
}

// Deny denies a pending permission request.
func (m *MockPermissionService) Deny(ctx context.Context, permID string, opts DenyOptions) error {
	if m.DenyFunc != nil {
		return m.DenyFunc(ctx, permID, opts)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	perm, ok := m.permissions[permID]
	if !ok {
		return store.ErrNotFound
	}

	if perm.Status != "pending" {
		return &InvalidStateError{
			Resource: "permission_request",
			ID:       permID,
			Current:  perm.Status,
			Expected: "pending",
		}
	}

	now := time.Now()
	perm.Status = "denied"
	perm.RespondedAt = &now
	perm.UpdatedAt = now

	if opts.Reason != "" {
		perm.ResponseReason = &opts.Reason
	}

	return nil
}

// AddPermission adds a permission request directly to the mock store (for testing).
func (m *MockPermissionService) AddPermission(perm *store.PermissionRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.permissions[perm.ID] = perm
}

// GetAllPermissions returns all permission requests in the mock store (for testing).
func (m *MockPermissionService) GetAllPermissions() []*store.PermissionRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	perms := make([]*store.PermissionRequest, 0, len(m.permissions))
	for _, p := range m.permissions {
		perms = append(perms, p)
	}
	return perms
}

// Reset clears all permission requests from the mock store.
func (m *MockPermissionService) Reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.permissions = make(map[string]*store.PermissionRequest)
}

// Verify MockPermissionService implements PermissionService.
var _ PermissionService = (*MockPermissionService)(nil)
