package core

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

func TestTenantFor(t *testing.T) {
	tenantA := "tenant_a"
	tenantB := "tenant_b"
	empty := ""

	t.Run("no tenant in context keeps the explicit value", func(t *testing.T) {
		got, err := tenantFor(context.Background(), &tenantA)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, tenantA, *got,
			"single-tenant and internal callers must keep working")
	})

	t.Run("no tenant anywhere stays nil", func(t *testing.T) {
		got, err := tenantFor(context.Background(), nil)
		require.NoError(t, err)
		assert.Nil(t, got)
	})

	t.Run("context wins over an absent explicit value", func(t *testing.T) {
		ctx := store.WithTenant(context.Background(), tenantA)
		got, err := tenantFor(ctx, nil)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, tenantA, *got)
	})

	t.Run("a matching explicit value is accepted", func(t *testing.T) {
		ctx := store.WithTenant(context.Background(), tenantA)
		got, err := tenantFor(ctx, &tenantA)
		require.NoError(t, err)
		require.Equal(t, tenantA, *got)
	})

	t.Run("an empty explicit value defers to the context", func(t *testing.T) {
		ctx := store.WithTenant(context.Background(), tenantA)
		got, err := tenantFor(ctx, &empty)
		require.NoError(t, err)
		require.Equal(t, tenantA, *got)
	})

	// The refusal, rather than a silent correction: a caller that asked for the
	// wrong tenant is a caller whose next request is also wrong.
	t.Run("a mismatched explicit value is refused", func(t *testing.T) {
		ctx := store.WithTenant(context.Background(), tenantA)
		_, err := tenantFor(ctx, &tenantB)
		assert.ErrorIs(t, err, ErrTenantMismatch)
	})
}

func TestRequireSameTenant(t *testing.T) {
	tenantA := "tenant_a"
	tenantB := "tenant_b"
	empty := ""

	assert.NoError(t, requireSameTenant("workspace", "ws_1", nil, nil))
	assert.NoError(t, requireSameTenant("workspace", "ws_1", &tenantA, &tenantA))

	// NULL and "" are the same absence of a tenant, so a row written before
	// tenancy existed compares equal to a single-tenant one.
	assert.NoError(t, requireSameTenant("workspace", "ws_1", nil, &empty))

	assert.ErrorIs(t, requireSameTenant("workspace", "ws_1", &tenantA, &tenantB), ErrTenantMismatch)
	assert.ErrorIs(t, requireSameTenant("workspace", "ws_1", &tenantA, nil), ErrTenantMismatch)
	assert.ErrorIs(t, requireSameTenant("runner", "run_1", nil, &tenantB), ErrTenantMismatch)
}

// TestSessionManager_Create_RefusesAnotherTenantsWorkspace covers the check row
// level security cannot make: to the database a workspace_id is just a foreign
// key, so knowing the id would otherwise be enough to bind a session to it.
func TestSessionManager_Create_RefusesAnotherTenantsWorkspace(t *testing.T) {
	manager, s := setupSessionManagerTest()

	other := "tenant_b"
	s.SetWorkspace(&store.Workspace{ID: "ws_other", Name: "theirs", TenantID: &other})

	ctx := store.WithTenant(context.Background(), "tenant_a")
	_, err := manager.Create(ctx, CreateSessionOptions{
		WorkspaceID: "ws_other",
		Agent:       "claude",
	})

	assert.ErrorIs(t, err, ErrTenantMismatch)
}

func TestSessionManager_Create_StampsTheContextTenant(t *testing.T) {
	manager, s := setupSessionManagerTest()

	tenant := "tenant_a"
	s.SetWorkspace(&store.Workspace{ID: "ws_mine", Name: "mine", TenantID: &tenant})

	ctx := store.WithTenant(context.Background(), tenant)
	sess, err := manager.Create(ctx, CreateSessionOptions{
		WorkspaceID: "ws_mine",
		Agent:       "claude",
	})
	require.NoError(t, err)
	require.NotNil(t, sess.TenantID)
	assert.Equal(t, tenant, *sess.TenantID)
}

// TestSessionManager_Activate_RefusesAnotherTenantsRunner: the runner mounts
// the session's workspace and holds its credentials, so it must be in the same
// tenant.
func TestSessionManager_Activate_RefusesAnotherTenantsRunner(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)

	tenantA := "tenant_a"
	tenantB := "tenant_b"
	s.SetRunner(&store.Runner{ID: "run_theirs", Name: "theirs", Status: StatusIdle, TenantID: &tenantB})
	s.SetSession(&store.Session{ID: "sess_mine", Status: SessionStatusPending, TenantID: &tenantA})

	err := manager.Activate(context.Background(), "sess_mine", "run_theirs")
	assert.ErrorIs(t, err, ErrTenantMismatch)
}

func TestSessionManager_Activate_AllowsSameTenantRunner(t *testing.T) {
	cmdSender := &mockCommandSenderForSession{}
	manager, s, _ := setupSessionManagerTestFull(cmdSender)
	manager.setTaskManager(&mockTaskManagerForSession{})

	tenant := "tenant_a"
	s.SetRunner(&store.Runner{ID: "run_mine", Name: "mine", Status: StatusIdle, TenantID: &tenant})
	s.SetSession(&store.Session{ID: "sess_mine", Status: SessionStatusPending, TenantID: &tenant})

	require.NoError(t, manager.Activate(context.Background(), "sess_mine", "run_mine"))
}

// TestTaskManager_Create_InheritsTheSessionTenant: a task belongs wherever its
// session does, and a caller cannot place it somewhere else.
func TestTaskManager_Create_InheritsTheSessionTenant(t *testing.T) {
	s := newTestTaskStore()
	manager := NewTaskManager(s, &mockCommandSender{}, &mockSessionMgrForTask{}, nil, zap.NewNop())

	tenant := "tenant_a"
	runnerID := "run_1"
	s.sessions["sess_1"] = &store.Session{
		ID:       "sess_1",
		Status:   SessionStatusActive,
		RunnerID: &runnerID,
		TenantID: &tenant,
	}

	ctx := store.WithTenant(context.Background(), tenant)
	task, err := manager.Create(ctx, CreateTaskOptions{SessionID: "sess_1", Prompt: "go"})
	require.NoError(t, err)
	require.NotNil(t, task.TenantID)
	assert.Equal(t, tenant, *task.TenantID)

	t.Run("and refuses a mismatched explicit tenant", func(t *testing.T) {
		other := "tenant_b"
		_, err := manager.Create(ctx, CreateTaskOptions{
			SessionID: "sess_1",
			Prompt:    "go",
			TenantID:  &other,
		})
		assert.ErrorIs(t, err, ErrTenantMismatch)
	})
}
