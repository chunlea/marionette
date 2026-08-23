package postgres_test

import (
	"context"
	"fmt"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/chunlea/marionette/pkg/store"
	pgstore "github.com/chunlea/marionette/pkg/store/postgres"
)

// These tests are the proof that tenant isolation is enforced by Postgres and
// not merely by the diligence of the queries above it. They run against the
// real schema, with the real policies from migration 008.
//
// They connect as an UNPRIVILEGED role, not the harness's superuser. This is
// not a detail: superusers - and any role with BYPASSRLS - ignore row level
// security entirely, and FORCE ROW LEVEL SECURITY does not change that (it
// subjects the table owner, not a superuser). Run these as the harness role and
// every policy passes vacuously, which is exactly how a deployment connecting
// as postgres would look healthy while isolating nothing.

// appStore is a store on the test database connected as an unprivileged role.
var (
	appStoreOnce sync.Once
	appStore     *pgstore.Store
	appStoreErr  error
)

// unprivilegedStore returns a store whose role is subject to the tenant
// policies, creating the role on first use.
func unprivilegedStore(t *testing.T) *pgstore.Store {
	t.Helper()

	appStoreOnce.Do(func() {
		ctx := context.Background()
		pool := testStore.Pool()

		// A plain LOGIN role: it owns nothing, so FORCE is not even needed for
		// the policies to bind, and it is not exempt from them.
		stmts := []string{
			`DROP ROLE IF EXISTS marionette_app`,
			`CREATE ROLE marionette_app LOGIN PASSWORD 'app'`,
			`GRANT USAGE ON SCHEMA public TO marionette_app`,
			`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO marionette_app`,
			`GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public TO marionette_app`,
		}
		for _, stmt := range stmts {
			if _, err := pool.Exec(ctx, stmt); err != nil {
				appStoreErr = fmt.Errorf("%s: %w", stmt, err)
				return
			}
		}

		dsn, err := url.Parse(testDSN)
		if err != nil {
			appStoreErr = err
			return
		}
		dsn.User = url.UserPassword("marionette_app", "app")

		appStore, appStoreErr = pgstore.New(ctx, pgstore.Config{URL: dsn.String()}, zap.NewNop())
	})

	require.NoError(t, appStoreErr, "could not build an unprivileged store")
	require.NotNil(t, appStore)
	return appStore
}

func tenantCtx(id string) context.Context {
	return store.WithTenant(context.Background(), id)
}

// newWorkspace builds a workspace that satisfies the schema's check
// constraints, so a failure in these tests is about tenancy and nothing else.
func newWorkspace(name string, tenantID *string) *store.Workspace {
	return &store.Workspace{
		Name:        name,
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
		TenantID:    tenantID,
	}
}

// newSession builds a session that satisfies the schema's check constraints.
func newSession(workspaceID string, tenantID *string) *store.Session {
	return &store.Session{
		WorkspaceID:   workspaceID,
		Agent:         "claude",
		Status:        "pending",
		NetworkPolicy: "allow_list",
		LifecycleMode: "on_demand",
		AllowedHosts:  []string{},
		TenantID:      tenantID,
	}
}

// seedSessionFor creates a workspace and a session owned by a tenant, using
// that tenant's context so the row lands on the right side of the policy.
func seedSessionFor(t *testing.T, ctx context.Context, name string) (*store.Workspace, *store.Session) {
	t.Helper()
	s := unprivilegedStore(t)

	tenantID := store.TenantPtr(ctx)
	ws := newWorkspace(name+"-ws", tenantID)
	require.NoError(t, s.CreateWorkspace(ctx, ws))

	sess := newSession(ws.ID, tenantID)
	require.NoError(t, s.CreateSession(ctx, sess))
	return ws, sess
}

// TestRLS_TenantsCannotSeeEachOther is the headline guarantee: a WHERE-less
// list is answered with the caller's rows, not everyone's, because the database
// says so.
func TestRLS_TenantsCannotSeeEachOther(t *testing.T) {
	s := unprivilegedStore(t)
	ctxA := tenantCtx("tenant_rls_a")
	ctxB := tenantCtx("tenant_rls_b")

	wsA, sessA := seedSessionFor(t, ctxA, "rls-a")
	wsB, sessB := seedSessionFor(t, ctxB, "rls-b")

	t.Run("list returns only the caller's sessions", func(t *testing.T) {
		got, err := s.ListSessions(ctxA, store.ListSessionsOptions{})
		require.NoError(t, err)

		ids := map[string]bool{}
		for _, s := range got.Items {
			ids[s.ID] = true
		}
		assert.True(t, ids[sessA.ID], "tenant A must see its own session")
		assert.False(t, ids[sessB.ID], "tenant A must not see tenant B's session")
	})

	t.Run("a direct get by id finds nothing across tenants", func(t *testing.T) {
		_, err := s.GetSession(ctxA, sessB.ID)
		assert.ErrorIs(t, err, store.ErrNotFound,
			"knowing another tenant's session id must not be enough to read it")

		_, err = s.GetWorkspace(ctxB, wsA.ID)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})

	t.Run("an update cannot reach across tenants", func(t *testing.T) {
		name := "stolen"
		err := s.UpdateSession(ctxA, sessB.ID, store.SessionUpdates{Name: &name})
		assert.Error(t, err, "updating another tenant's session must not succeed")

		// And the row is untouched.
		after, err := s.GetSession(ctxB, sessB.ID)
		require.NoError(t, err)
		assert.NotEqual(t, "stolen", derefString(after.Name))
	})

	t.Run("a delete cannot reach across tenants", func(t *testing.T) {
		err := s.DeleteSession(ctxA, sessB.ID)
		assert.Error(t, err)

		_, err = s.GetSession(ctxB, sessB.ID)
		assert.NoError(t, err, "tenant B's session must survive tenant A's delete")
	})

	t.Run("writes are stamped into the caller's tenant", func(t *testing.T) {
		got, err := s.GetWorkspace(ctxB, wsB.ID)
		require.NoError(t, err)
		require.NotNil(t, got.TenantID)
		assert.Equal(t, "tenant_rls_b", *got.TenantID)
	})
}

// TestRLS_WriteIntoAnotherTenantIsRejected covers the WITH CHECK half: the
// policy stops a row being written into a tenant the caller is not acting for,
// which is what makes a compromised or buggy caller contained.
func TestRLS_WriteIntoAnotherTenantIsRejected(t *testing.T) {
	s := unprivilegedStore(t)
	ctxA := tenantCtx("tenant_rls_write_a")
	other := "tenant_rls_write_b"

	ws := newWorkspace("smuggled", &other)
	err := s.CreateWorkspace(ctxA, ws)
	assert.Error(t, err, "a workspace must not be created into another tenant")
}

// TestRLS_SingleTenantSeesEverythingItDidBefore is the compatibility half. A
// deployment that never sets a tenant writes NULL tenant rows and must keep
// seeing exactly those.
func TestRLS_SingleTenantSeesEverythingItDidBefore(t *testing.T) {
	s := unprivilegedStore(t)
	ctx := context.Background()

	ws := newWorkspace("single-tenant-ws", nil)
	require.NoError(t, s.CreateWorkspace(ctx, ws))
	assert.Nil(t, ws.TenantID)

	sess := newSession(ws.ID, nil)
	require.NoError(t, s.CreateSession(ctx, sess))

	got, err := s.GetSession(ctx, sess.ID)
	require.NoError(t, err, "a single-tenant deployment must read back what it wrote")
	assert.Equal(t, sess.ID, got.ID)

	list, err := s.ListSessions(ctx, store.ListSessionsOptions{})
	require.NoError(t, err)
	found := false
	for _, s := range list.Items {
		if s.ID == sess.ID {
			found = true
		}
		assert.Nil(t, s.TenantID,
			"a tenantless caller must never be shown a tenant-owned row")
	}
	assert.True(t, found)
}

// TestRLS_PooledConnectionDoesNotLeakTenant is the trap this design exists to
// avoid. SET LOCAL dies with its transaction; a connection-level SET would
// survive being handed to the next request, which is a cross-tenant read.
func TestRLS_PooledConnectionDoesNotLeakTenant(t *testing.T) {
	s := unprivilegedStore(t)
	ctxA := tenantCtx("tenant_leak_a")
	_, sessA := seedSessionFor(t, ctxA, "leak-a")

	// Churn the pool so the tenantless read below is very likely to land on a
	// connection that just served tenant A.
	for i := 0; i < 20; i++ {
		_, err := s.ListSessions(ctxA, store.ListSessionsOptions{})
		require.NoError(t, err)
	}

	for i := 0; i < 20; i++ {
		list, err := s.ListSessions(context.Background(), store.ListSessionsOptions{})
		require.NoError(t, err)
		for _, s := range list.Items {
			require.NotEqual(t, sessA.ID, s.ID,
				"a tenantless request saw a tenant's row: the binding leaked across the pool")
			require.Nil(t, s.TenantID)
		}
	}
}

// TestRLS_TransactionsCarryTheTenant proves BeginTx binds the tenant for every
// statement inside it, not just the first.
func TestRLS_TransactionsCarryTheTenant(t *testing.T) {
	s := unprivilegedStore(t)
	ctxA := tenantCtx("tenant_tx_a")
	ctxB := tenantCtx("tenant_tx_b")
	_, sessB := seedSessionFor(t, ctxB, "tx-b")

	err := store.WithTx(ctxA, s, func(tx store.Tx) error {
		_, err := tx.GetSession(ctxA, sessB.ID)
		return err
	})
	assert.ErrorIs(t, err, store.ErrNotFound,
		"a transaction opened for tenant A must not read tenant B's rows")

	// The same transaction shape works for the tenant that owns the row.
	require.NoError(t, store.WithTx(ctxB, s, func(tx store.Tx) error {
		_, err := tx.GetSession(ctxB, sessB.ID)
		return err
	}))
}

// TestRLS_LogsAreCoveredThroughThePartitionedParent checks the partitioned
// table specifically: a policy on the parent has to cover rows that live in
// partitions, or the busiest table in the schema would be the one hole.
func TestRLS_LogsAreCoveredThroughThePartitionedParent(t *testing.T) {
	s := unprivilegedStore(t)
	ctxA := tenantCtx("tenant_logs_a")
	ctxB := tenantCtx("tenant_logs_b")

	_, sessA := seedSessionFor(t, ctxA, "logs-a")

	logA := &store.Log{
		SessionID: sessA.ID,
		TaskID:    "task_logs_a",
		RunID:     "trun_logs_a",
		RunnerID:  "run_logs_a",
		Stream:    "stdout",
		Level:     "info",
		Content:   "tenant a only",
		Sequence:  1,
		TenantID:  store.TenantPtr(ctxA),
	}
	require.NoError(t, s.CreateLog(ctxA, logA))

	mine, err := s.ListLogs(ctxA, store.ListLogsOptions{SessionID: &sessA.ID})
	require.NoError(t, err)
	assert.NotEmpty(t, mine.Items, "tenant A must see its own logs")

	theirs, err := s.ListLogs(ctxB, store.ListLogsOptions{SessionID: &sessA.ID})
	require.NoError(t, err)
	assert.Empty(t, theirs.Items, "tenant B must not see tenant A's logs")
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// strictStore returns an unprivileged store in multi-tenant mode, where a
// missing tenant is an error rather than a single-tenant deployment.
func strictStore(t *testing.T) *pgstore.Store {
	t.Helper()

	// Ensure the unprivileged role exists before connecting as it.
	unprivilegedStore(t)

	dsn, err := url.Parse(testDSN)
	require.NoError(t, err)
	dsn.User = url.UserPassword("marionette_app", "app")

	s, err := pgstore.New(context.Background(), pgstore.Config{
		URL:         dsn.String(),
		MultiTenant: true,
		MaxConns:    4,
	}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestRLS_StrictModeRejectsTenantlessWrites is the third mode. With
// multi_tenant on, a statement that reaches the database without a tenant is a
// bug in the caller, and it fails closed instead of quietly writing a row
// nobody owns.
func TestRLS_StrictModeRejectsTenantlessWrites(t *testing.T) {
	s := strictStore(t)
	ctx := context.Background()

	err := s.CreateWorkspace(ctx, newWorkspace("strict-no-tenant", nil))
	require.Error(t, err, "a tenantless write must be refused in multi-tenant mode")

	t.Run("and reads return nothing", func(t *testing.T) {
		got, err := s.ListSessions(ctx, store.ListSessionsOptions{})
		require.NoError(t, err)
		assert.Empty(t, got.Items,
			"a tenantless read must not be answered with another tenant's rows")
	})

	t.Run("while a tenant-bound write still works", func(t *testing.T) {
		tctx := store.WithTenant(ctx, "tenant_strict_ok")
		ws := newWorkspace("strict-with-tenant", store.TenantPtr(tctx))
		require.NoError(t, s.CreateWorkspace(tctx, ws))

		got, err := s.GetWorkspace(tctx, ws.ID)
		require.NoError(t, err)
		assert.Equal(t, ws.ID, got.ID)
	})
}

// TestRLS_StrictModeRefusesAnExemptRole guards the assumption the whole design
// rests on. A superuser ignores row level security, so multi-tenant mode on
// such a role would isolate nothing while looking perfectly healthy.
func TestRLS_StrictModeRefusesAnExemptRole(t *testing.T) {
	_, err := pgstore.New(context.Background(), pgstore.Config{
		URL:         testDSN, // the harness role: a superuser
		MultiTenant: true,
		MaxConns:    2,
	}, zap.NewNop())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "multi_tenant is on")
	assert.Contains(t, err.Error(), "BYPASSRLS")
}

// TestRLS_SingleTenantWarnsButStarts is the other half: the same conditions are
// harmless while there is only one tenant, so they must not stop the server -
// which is what keeps the smoke walk and every existing deployment working.
func TestRLS_SingleTenantWarnsButStarts(t *testing.T) {
	logs, observed := observer.New(zap.WarnLevel)

	s, err := pgstore.New(context.Background(), pgstore.Config{
		URL:      testDSN, // the harness role: a superuser
		MaxConns: 2,
	}, zap.New(logs))
	require.NoError(t, err, "single-tenant mode must start on an exempt role")
	t.Cleanup(func() { _ = s.Close() })

	require.Positive(t, observed.Len(), "an unenforced policy must be said out loud")
	assert.Contains(t, observed.All()[0].Message, "row level security is not enforced")
}

// TestRLS_UnprivilegedRoleIsAccepted proves the check passes for the shape a
// multi-tenant deployment is supposed to use.
func TestRLS_UnprivilegedRoleIsAccepted(t *testing.T) {
	s := strictStore(t)
	require.NotNil(t, s, "an unprivileged role with the policies applied must be accepted")
}

// TestRLS_SingleTenantCRUDSliceUnchanged runs a representative slice of the
// ordinary CRUD paths as an unprivileged role with no tenant bound: exactly a
// single-tenant deployment after this migration. Everything it writes it must
// still be able to read, list, update and delete.
func TestRLS_SingleTenantCRUDSliceUnchanged(t *testing.T) {
	s := unprivilegedStore(t)
	ctx := context.Background()

	ws := newWorkspace("crud-slice-ws", nil)
	require.NoError(t, s.CreateWorkspace(ctx, ws))

	sess := newSession(ws.ID, nil)
	require.NoError(t, s.CreateSession(ctx, sess))

	task := &store.Task{SessionID: sess.ID, Prompt: "do the thing", Status: "pending"}
	require.NoError(t, s.CreateTask(ctx, task))

	runner := &store.Runner{
		Name:         "crud-slice-runner",
		Hostname:     "localhost",
		Status:       "offline",
		SandboxMode:  "runner-is-sandbox",
		SandboxTypes: []string{},
		Capabilities: []string{},
	}
	require.NoError(t, s.CreateRunner(ctx, runner))

	t.Run("read back", func(t *testing.T) {
		gotWS, err := s.GetWorkspace(ctx, ws.ID)
		require.NoError(t, err)
		assert.Equal(t, ws.ID, gotWS.ID)

		gotSess, err := s.GetSession(ctx, sess.ID)
		require.NoError(t, err)
		assert.Equal(t, sess.ID, gotSess.ID)

		gotTask, err := s.GetTask(ctx, task.ID)
		require.NoError(t, err)
		assert.Equal(t, task.ID, gotTask.ID)

		gotRunner, err := s.GetRunner(ctx, runner.ID)
		require.NoError(t, err)
		assert.Equal(t, runner.ID, gotRunner.ID)
	})

	t.Run("list", func(t *testing.T) {
		sessions, err := s.ListSessions(ctx, store.ListSessionsOptions{})
		require.NoError(t, err)
		assert.NotEmpty(t, sessions.Items)

		tasks, err := s.ListTasks(ctx, store.ListTasksOptions{SessionID: &sess.ID})
		require.NoError(t, err)
		assert.Len(t, tasks.Items, 1)

		runners, err := s.ListRunners(ctx, store.ListRunnersOptions{})
		require.NoError(t, err)
		assert.NotEmpty(t, runners.Items)
	})

	t.Run("update", func(t *testing.T) {
		status := "active"
		require.NoError(t, s.UpdateSession(ctx, sess.ID, store.SessionUpdates{Status: &status}))

		got, err := s.GetSession(ctx, sess.ID)
		require.NoError(t, err)
		assert.Equal(t, "active", got.Status)
	})

	t.Run("transaction", func(t *testing.T) {
		require.NoError(t, store.WithTx(ctx, s, func(tx store.Tx) error {
			_, err := tx.GetSession(ctx, sess.ID)
			return err
		}))
	})

	t.Run("delete", func(t *testing.T) {
		require.NoError(t, s.DeleteTask(ctx, task.ID))
		_, err := s.GetTask(ctx, task.ID)
		assert.ErrorIs(t, err, store.ErrNotFound)
	})
}
