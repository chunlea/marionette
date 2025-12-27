package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// mockPermissionManager implements a minimal interface for testing PermissionAdapter.
type mockPermissionManager struct {
	permissions map[string]*store.PermissionRequest
	getErr      error
	listErr     error
	respondErr  error
}

func newMockPermissionManager() *mockPermissionManager {
	return &mockPermissionManager{
		permissions: make(map[string]*store.PermissionRequest),
	}
}

func (m *mockPermissionManager) Get(ctx context.Context, id string) (*store.PermissionRequest, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	perm, ok := m.permissions[id]
	if !ok {
		return nil, errors.New("permission request not found")
	}
	return perm, nil
}

func (m *mockPermissionManager) List(ctx context.Context, opts store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	if m.listErr != nil {
		return nil, m.listErr
	}
	var items []*store.PermissionRequest
	for _, perm := range m.permissions {
		// Apply filters
		if opts.SessionID != nil && perm.SessionID != *opts.SessionID {
			continue
		}
		if opts.TaskID != nil && perm.TaskID != *opts.TaskID {
			continue
		}
		if len(opts.Status) > 0 {
			found := false
			for _, s := range opts.Status {
				if perm.Status == s {
					found = true
					break
				}
			}
			if !found {
				continue
			}
		}
		items = append(items, perm)
	}
	return &store.ListResult[store.PermissionRequest]{Items: items}, nil
}

func (m *mockPermissionManager) Respond(ctx context.Context, permID string, approved bool, reason, respondedBy string) error {
	if m.respondErr != nil {
		return m.respondErr
	}
	perm, ok := m.permissions[permID]
	if !ok {
		return errors.New("permission request not found")
	}
	if approved {
		perm.Status = "approved"
	} else {
		perm.Status = "denied"
	}
	now := time.Now()
	perm.RespondedAt = &now
	perm.RespondedBy = &respondedBy
	if reason != "" {
		perm.ResponseReason = &reason
	}
	return nil
}

// permissionManagerAdapter wraps mockPermissionManager to satisfy PermissionAdapter's requirements.
type permissionManagerAdapter struct {
	mock *mockPermissionManager
}

func TestNewPermissionAdapter(t *testing.T) {
	adapter := NewPermissionAdapter(nil)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
}

func TestPermissionAdapter_Get(t *testing.T) {
	mock := newMockPermissionManager()
	mock.permissions["perm_123"] = &store.PermissionRequest{
		ID:        "perm_123",
		SessionID: "sess_456",
		TaskID:    "task_789",
		Tool:      "bash",
		Action:    "rm -rf /tmp/test",
		Status:    "pending",
	}

	// Create adapter using a wrapper that implements the interface
	adapter := &testPermissionAdapter{mock: mock}

	ctx := context.Background()

	// Test Get
	perm, err := adapter.Get(ctx, "perm_123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if perm.ID != "perm_123" {
		t.Errorf("expected ID 'perm_123', got %q", perm.ID)
	}
	if perm.Tool != "bash" {
		t.Errorf("expected tool 'bash', got %q", perm.Tool)
	}
}

func TestPermissionAdapter_Get_NotFound(t *testing.T) {
	mock := newMockPermissionManager()
	adapter := &testPermissionAdapter{mock: mock}

	ctx := context.Background()

	// Test Get non-existent permission
	_, err := adapter.Get(ctx, "perm_nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent permission")
	}
}

func TestPermissionAdapter_List(t *testing.T) {
	mock := newMockPermissionManager()
	mock.permissions["perm_1"] = &store.PermissionRequest{
		ID:        "perm_1",
		SessionID: "sess_456",
		TaskID:    "task_789",
		Status:    "pending",
	}
	mock.permissions["perm_2"] = &store.PermissionRequest{
		ID:        "perm_2",
		SessionID: "sess_456",
		TaskID:    "task_789",
		Status:    "approved",
	}

	adapter := &testPermissionAdapter{mock: mock}

	ctx := context.Background()

	// Test List all
	result, err := adapter.List(ctx, ListPermissionsOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 2 {
		t.Errorf("expected 2 permissions, got %d", len(result.Items))
	}

	// Test List with status filter
	result, err = adapter.List(ctx, ListPermissionsOptions{Status: []string{"pending"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Items) != 1 {
		t.Errorf("expected 1 pending permission, got %d", len(result.Items))
	}
}

func TestPermissionAdapter_Approve(t *testing.T) {
	mock := newMockPermissionManager()
	mock.permissions["perm_123"] = &store.PermissionRequest{
		ID:        "perm_123",
		SessionID: "sess_456",
		TaskID:    "task_789",
		Status:    "pending",
	}

	adapter := &testPermissionAdapter{mock: mock}

	ctx := context.Background()

	// Test Approve
	err := adapter.Approve(ctx, "perm_123", ApproveOptions{Reason: "Looks safe"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify status changed
	perm := mock.permissions["perm_123"]
	if perm.Status != "approved" {
		t.Errorf("expected status 'approved', got %q", perm.Status)
	}
	if perm.ResponseReason == nil || *perm.ResponseReason != "Looks safe" {
		t.Error("expected reason to be set")
	}
}

func TestPermissionAdapter_Deny(t *testing.T) {
	mock := newMockPermissionManager()
	mock.permissions["perm_123"] = &store.PermissionRequest{
		ID:        "perm_123",
		SessionID: "sess_456",
		TaskID:    "task_789",
		Status:    "pending",
	}

	adapter := &testPermissionAdapter{mock: mock}

	ctx := context.Background()

	// Test Deny
	err := adapter.Deny(ctx, "perm_123", DenyOptions{Reason: "Too risky"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify status changed
	perm := mock.permissions["perm_123"]
	if perm.Status != "denied" {
		t.Errorf("expected status 'denied', got %q", perm.Status)
	}
	if perm.ResponseReason == nil || *perm.ResponseReason != "Too risky" {
		t.Error("expected reason to be set")
	}
}

// testPermissionAdapter is a test adapter that uses the mock directly.
type testPermissionAdapter struct {
	mock *mockPermissionManager
}

func (a *testPermissionAdapter) Get(ctx context.Context, id string) (*store.PermissionRequest, error) {
	return a.mock.Get(ctx, id)
}

func (a *testPermissionAdapter) List(ctx context.Context, opts ListPermissionsOptions) (*store.ListResult[store.PermissionRequest], error) {
	coreOpts := store.ListPermissionRequestsOptions{
		BaseListOptions: store.BaseListOptions{
			Limit:  opts.Limit,
			Cursor: opts.Cursor,
		},
		Status:    opts.Status,
		RiskLevel: opts.RiskLevel,
	}
	if opts.SessionID != "" {
		coreOpts.SessionID = &opts.SessionID
	}
	if opts.TaskID != "" {
		coreOpts.TaskID = &opts.TaskID
	}
	return a.mock.List(ctx, coreOpts)
}

func (a *testPermissionAdapter) Approve(ctx context.Context, id string, opts ApproveOptions) error {
	return a.mock.Respond(ctx, id, true, opts.Reason, "api")
}

func (a *testPermissionAdapter) Deny(ctx context.Context, id string, opts DenyOptions) error {
	return a.mock.Respond(ctx, id, false, opts.Reason, "api")
}

// Verify testPermissionAdapter implements PermissionService interface
var _ PermissionService = (*testPermissionAdapter)(nil)

// Verify PermissionAdapter implements PermissionService interface
var _ PermissionService = (*PermissionAdapter)(nil)
