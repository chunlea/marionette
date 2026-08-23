package postgres_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// partitionExists reports whether a relation with that name is a partition of logs.
func partitionExists(ctx context.Context, t *testing.T, name string) bool {
	t.Helper()

	var exists bool
	err := testStore.Pool().QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_inherits i ON c.oid = i.inhrelid
			JOIN pg_class p ON i.inhparent = p.oid
			WHERE p.relname = 'logs' AND c.relname = $1
		)`, name).Scan(&exists)
	require.NoError(t, err)
	return exists
}

func TestLogDefaultPartitionExists(t *testing.T) {
	ctx := context.Background()

	// Migration 007's safety net: without it, inserts fail outright once the
	// pre-created daily partitions run out.
	assert.True(t, partitionExists(ctx, t, "logs_default"),
		"logs_default partition is missing — migration 007 did not run")
}

func TestMaintainLogPartitions(t *testing.T) {
	ctx := context.Background()

	require.NoError(t, testStore.MaintainLogPartitions(ctx, 7))

	// Idempotent: a second run must not fail on existing partitions.
	require.NoError(t, testStore.MaintainLogPartitions(ctx, 7))

	today := time.Now().UTC()
	for _, offset := range []int{0, 1, 7} {
		name := "logs_" + today.AddDate(0, 0, offset).Format("20060102")
		assert.True(t, partitionExists(ctx, t, name), "missing partition %s", name)
	}

	t.Run("rejects negative days ahead", func(t *testing.T) {
		err := testStore.MaintainLogPartitions(ctx, -1)
		assert.ErrorIs(t, err, store.ErrInvalidInput)
	})

	t.Run("rejects negative retention", func(t *testing.T) {
		err := testStore.DropOldLogPartitions(ctx, -1)
		assert.ErrorIs(t, err, store.ErrInvalidInput)
	})
}

// TestCreateLogPartitionDrainsDefault covers the trap the default partition
// introduces: once rows land in logs_default, plain "CREATE TABLE ... PARTITION
// OF" for that day fails, which would wedge partition maintenance forever.
func TestCreateLogPartitionDrainsDefault(t *testing.T) {
	ctx := context.Background()
	pool := testStore.Pool()

	// Far enough ahead that maintain_log_partitions has not created the day.
	day := time.Now().UTC().AddDate(0, 0, 60).Truncate(24 * time.Hour)
	partitionName := "logs_" + day.Format("20060102")
	require.False(t, partitionExists(ctx, t, partitionName), "test day already has a partition")

	logID := "log_drain" + time.Now().Format("150405.000000")
	_, err := pool.Exec(ctx, `
		INSERT INTO logs (id, session_id, task_id, run_id, runner_id, stream, level, content, sequence, created_at)
		VALUES ($1, 'sess_x', 'task_x', 'trun_x', 'run_x', 'stdout', 'info', 'stranded', 1, $2)`,
		logID, day.Add(6*time.Hour))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), fmt.Sprintf("DROP TABLE IF EXISTS %s", partitionName))
	})

	// With no daily partition the row must have landed in the default one.
	var inDefault int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM logs_default WHERE id = $1", logID).Scan(&inDefault))
	require.Equal(t, 1, inDefault, "row did not land in logs_default")

	// Creating the day's partition must drain the row instead of failing.
	_, err = pool.Exec(ctx, "SELECT create_log_partition($1::date)", day)
	require.NoError(t, err, "create_log_partition must handle rows already in logs_default")

	assert.True(t, partitionExists(ctx, t, partitionName))

	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM logs_default WHERE id = $1", logID).Scan(&inDefault))
	assert.Zero(t, inDefault, "row was not drained out of logs_default")

	var inPartition int
	require.NoError(t, pool.QueryRow(ctx,
		fmt.Sprintf("SELECT count(*) FROM %s WHERE id = $1", partitionName), logID).Scan(&inPartition))
	assert.Equal(t, 1, inPartition, "row was not moved into the new partition")

	// And it is still readable through the parent table.
	var throughParent int
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT count(*) FROM logs WHERE id = $1", logID).Scan(&throughParent))
	assert.Equal(t, 1, throughParent)
}

func TestDropOldLogPartitionsKeepsDefault(t *testing.T) {
	ctx := context.Background()
	pool := testStore.Pool()

	// Create a partition well past any retention window.
	old := time.Now().UTC().AddDate(0, 0, -400).Truncate(24 * time.Hour)
	oldName := "logs_" + old.Format("20060102")
	_, err := pool.Exec(ctx, "SELECT create_log_partition($1::date)", old)
	require.NoError(t, err)
	require.True(t, partitionExists(ctx, t, oldName))

	require.NoError(t, testStore.DropOldLogPartitions(ctx, 7))

	assert.False(t, partitionExists(ctx, t, oldName), "stale partition was not dropped")
	assert.True(t, partitionExists(ctx, t, "logs_default"), "logs_default must never be dropped")
}

// TestOneActiveSessionPerRunner covers the partial unique index from migration 007.
func TestOneActiveSessionPerRunner(t *testing.T) {
	ctx := context.Background()

	newRunner := func(suffix string) *store.Runner {
		r := &store.Runner{
			Name:         "active-session-" + suffix + "-" + time.Now().Format("150405.000000"),
			Hostname:     "localhost",
			Status:       "idle",
			SandboxMode:  "runner-is-sandbox",
			Capabilities: []string{},
			SandboxTypes: []string{},
		}
		require.NoError(t, testStore.CreateRunner(ctx, r))
		t.Cleanup(func() { _ = testStore.DeleteRunner(context.Background(), r.ID) })
		return r
	}

	// One shared workspace for the whole test: sessions do not need distinct
	// ones, and every extra row here pushes older workspaces off the first page
	// of ListWorkspaces for other tests.
	workspace := &store.Workspace{
		Name:        "active-session-ws-" + time.Now().Format("150405.000000"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	require.NoError(t, testStore.CreateWorkspace(ctx, workspace))
	t.Cleanup(func() { _ = testStore.DeleteWorkspace(context.Background(), workspace.ID) })

	newSession := func(runnerID *string, status string) *store.Session {
		return &store.Session{
			WorkspaceID:   workspace.ID,
			Agent:         "claude",
			Status:        status,
			RunnerID:      runnerID,
			NetworkPolicy: "allow_list",
			LifecycleMode: "on_demand",
			AllowedHosts:  []string{},
		}
	}

	t.Run("rejects a second active session on the same runner", func(t *testing.T) {
		runner := newRunner("dup")

		first := newSession(&runner.ID, "active")
		require.NoError(t, testStore.CreateSession(ctx, first))
		t.Cleanup(func() { _ = testStore.DeleteSession(context.Background(), first.ID) })

		second := newSession(&runner.ID, "active")
		err := testStore.CreateSession(ctx, second)
		require.Error(t, err, "a runner must host at most one active session")
		assert.ErrorIs(t, err, store.ErrAlreadyExists)
	})

	t.Run("allows a non-active session on a busy runner", func(t *testing.T) {
		runner := newRunner("suspended")

		active := newSession(&runner.ID, "active")
		require.NoError(t, testStore.CreateSession(ctx, active))
		t.Cleanup(func() { _ = testStore.DeleteSession(context.Background(), active.ID) })

		suspended := newSession(&runner.ID, "suspended")
		require.NoError(t, testStore.CreateSession(ctx, suspended))
		t.Cleanup(func() { _ = testStore.DeleteSession(context.Background(), suspended.ID) })
	})

	t.Run("allows many active sessions without a runner", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			s := newSession(nil, "active")
			require.NoError(t, testStore.CreateSession(ctx, s))
			t.Cleanup(func() { _ = testStore.DeleteSession(context.Background(), s.ID) })
		}
	})

	t.Run("frees the slot when the session leaves active", func(t *testing.T) {
		runner := newRunner("handoff")

		first := newSession(&runner.ID, "active")
		require.NoError(t, testStore.CreateSession(ctx, first))
		t.Cleanup(func() { _ = testStore.DeleteSession(context.Background(), first.ID) })

		suspended := "suspended"
		require.NoError(t, testStore.UpdateSession(ctx, first.ID, store.SessionUpdates{Status: &suspended}))

		second := newSession(&runner.ID, "active")
		require.NoError(t, testStore.CreateSession(ctx, second))
		t.Cleanup(func() { _ = testStore.DeleteSession(context.Background(), second.ID) })
	})
}
