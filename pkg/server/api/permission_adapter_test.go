package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/store"
)

// mockPermissionManager implements PermissionManagerInterface for testing.
type mockPermissionManager struct {
	permissions map[string]*store.PermissionRequest
	getErr      error
	listErr     error
	respondErr  error

	// Track calls for verification
	lastListOpts *store.ListPermissionRequestsOptions
}

func newMockPermissionManager() *mockPermissionManager {
	return &mockPermissionManager{
		permissions: make(map[string]*store.PermissionRequest),
	}
}

func (m *mockPermissionManager) Get(_ context.Context, id string) (*store.PermissionRequest, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	perm, ok := m.permissions[id]
	if !ok {
		return nil, errors.New("permission request not found")
	}
	return perm, nil
}

func (m *mockPermissionManager) List(_ context.Context, opts store.ListPermissionRequestsOptions) (*store.ListResult[store.PermissionRequest], error) {
	m.lastListOpts = &opts

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

func (m *mockPermissionManager) Respond(_ context.Context, permID string, approved bool, reason, respondedBy string) error {
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

// Verify mockPermissionManager implements PermissionManagerInterface.
var _ PermissionManagerInterface = (*mockPermissionManager)(nil)

func TestNewPermissionAdapter(t *testing.T) {
	mock := newMockPermissionManager()
	adapter := NewPermissionAdapter(mock)
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}
	if adapter.manager != mock {
		t.Error("expected manager to be set")
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

	// Use the actual PermissionAdapter
	adapter := NewPermissionAdapter(mock)

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
	adapter := NewPermissionAdapter(mock)

	ctx := context.Background()

	// Test Get non-existent permission
	_, err := adapter.Get(ctx, "perm_nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent permission")
	}
}

func TestPermissionAdapter_Get_Error(t *testing.T) {
	mock := newMockPermissionManager()
	mock.getErr = errors.New("database error")
	adapter := NewPermissionAdapter(mock)

	ctx := context.Background()

	_, err := adapter.Get(ctx, "perm_123")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "database error" {
		t.Errorf("expected 'database error', got %q", err.Error())
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

	adapter := NewPermissionAdapter(mock)

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

func TestPermissionAdapter_List_WithFilters(t *testing.T) {
	mock := newMockPermissionManager()
	mock.permissions["perm_1"] = &store.PermissionRequest{
		ID:        "perm_1",
		SessionID: "sess_456",
		TaskID:    "task_789",
		Status:    "pending",
	}

	adapter := NewPermissionAdapter(mock)

	ctx := context.Background()

	// Test List with SessionID and TaskID filters
	_, err := adapter.List(ctx, ListPermissionsOptions{
		SessionID: "sess_456",
		TaskID:    "task_789",
		Limit:     10,
		Cursor:    "cursor123",
		RiskLevel: []string{"high"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify options were passed correctly
	if mock.lastListOpts == nil {
		t.Fatal("expected lastListOpts to be set")
	}
	if mock.lastListOpts.SessionID == nil || *mock.lastListOpts.SessionID != "sess_456" {
		t.Error("expected SessionID filter to be set")
	}
	if mock.lastListOpts.TaskID == nil || *mock.lastListOpts.TaskID != "task_789" {
		t.Error("expected TaskID filter to be set")
	}
	if mock.lastListOpts.Limit != 10 {
		t.Errorf("expected Limit 10, got %d", mock.lastListOpts.Limit)
	}
	if mock.lastListOpts.Cursor != "cursor123" {
		t.Errorf("expected Cursor 'cursor123', got %q", mock.lastListOpts.Cursor)
	}
	if len(mock.lastListOpts.RiskLevel) != 1 || mock.lastListOpts.RiskLevel[0] != "high" {
		t.Error("expected RiskLevel filter to be set")
	}
}

func TestPermissionAdapter_List_Error(t *testing.T) {
	mock := newMockPermissionManager()
	mock.listErr = errors.New("database error")
	adapter := NewPermissionAdapter(mock)

	ctx := context.Background()

	_, err := adapter.List(ctx, ListPermissionsOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "database error" {
		t.Errorf("expected 'database error', got %q", err.Error())
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

	adapter := NewPermissionAdapter(mock)

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
	if perm.RespondedBy == nil || *perm.RespondedBy != "api" {
		t.Error("expected respondedBy to be 'api'")
	}
}

func TestPermissionAdapter_Approve_Error(t *testing.T) {
	mock := newMockPermissionManager()
	mock.respondErr = errors.New("respond error")
	adapter := NewPermissionAdapter(mock)

	ctx := context.Background()

	err := adapter.Approve(ctx, "perm_123", ApproveOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "respond error" {
		t.Errorf("expected 'respond error', got %q", err.Error())
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

	adapter := NewPermissionAdapter(mock)

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
	if perm.RespondedBy == nil || *perm.RespondedBy != "api" {
		t.Error("expected respondedBy to be 'api'")
	}
}

func TestPermissionAdapter_Deny_Error(t *testing.T) {
	mock := newMockPermissionManager()
	mock.respondErr = errors.New("respond error")
	adapter := NewPermissionAdapter(mock)

	ctx := context.Background()

	err := adapter.Deny(ctx, "perm_123", DenyOptions{})
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "respond error" {
		t.Errorf("expected 'respond error', got %q", err.Error())
	}
}

// Verify PermissionAdapter implements PermissionService interface.
var _ PermissionService = (*PermissionAdapter)(nil)
