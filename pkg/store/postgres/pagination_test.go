package postgres_test

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// pageResult is one page of any listing, flattened to what pagination cares
// about.
type pageResult struct {
	ids        []string
	totalCount int64
	hasMore    bool
	nextCursor string
}

// pagedList reads one page of a listing that has been narrowed to the rows a
// test created.
type pagedList func(ctx context.Context, opts store.BaseListOptions) (pageResult, error)

// toPage flattens a ListResult, given how to read an item's ID.
func toPage[T any](res *store.ListResult[T], err error, id func(*T) string) (pageResult, error) {
	if err != nil {
		return pageResult{}, err
	}

	ids := make([]string, 0, len(res.Items))
	for _, item := range res.Items {
		ids = append(ids, id(item))
	}
	return pageResult{
		ids:        ids,
		totalCount: res.TotalCount,
		hasMore:    res.HasMore,
		nextCursor: res.NextCursor,
	}, nil
}

// walk pages through a listing pageSize rows at a time and returns the IDs in
// the order they were seen.
func walk(ctx context.Context, t *testing.T, list pagedList, pageSize int, desc bool) []string {
	t.Helper()

	var seen []string
	var cursor string

	// Bounded: a cursor that fails to advance would otherwise spin forever.
	for i := 0; i < 50; i++ {
		page, err := list(ctx, store.BaseListOptions{
			Limit:     pageSize,
			Cursor:    cursor,
			OrderDesc: desc,
		})
		require.NoError(t, err)

		seen = append(seen, page.ids...)

		if !page.hasMore {
			assert.Empty(t, page.nextCursor, "no cursor should be offered on the last page")
			return seen
		}
		require.NotEmpty(t, page.nextCursor, "hasMore was set but no cursor was returned")
		require.NotEqual(t, cursor, page.nextCursor, "cursor did not advance")
		cursor = page.nextCursor
	}

	t.Fatal("pagination did not terminate")
	return nil
}

func assertNoDuplicates(t *testing.T, ids []string) {
	t.Helper()

	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		assert.Falsef(t, seen[id], "id %s was returned on more than one page", id)
		seen[id] = true
	}
}

func reversed(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[len(ids)-1-i] = id
	}
	return out
}

// paginationCase seeds n rows and hands back a listing narrowed to exactly
// those rows, so a shared test database cannot make the assertions ambiguous.
type paginationCase struct {
	name  string
	setup func(ctx context.Context, t *testing.T, n int) (created []string, list pagedList)
}

func paginationCases() []paginationCase {
	return []paginationCase{
		{
			name: "sessions",
			setup: func(ctx context.Context, t *testing.T, n int) ([]string, pagedList) {
				workspace := &store.Workspace{
					Name:        "page-ws-" + time.Now().Format("150405.000000"),
					Persist:     true,
					StorageType: "volume",
					Mobility:    "local",
				}
				require.NoError(t, testStore.CreateWorkspace(ctx, workspace))
				t.Cleanup(func() { _ = testStore.DeleteWorkspace(context.Background(), workspace.ID) })

				ids := make([]string, 0, n)
				for i := 0; i < n; i++ {
					name := fmt.Sprintf("page-session-%02d", i)
					s := &store.Session{
						Name:          &name,
						WorkspaceID:   workspace.ID,
						Agent:         "claude",
						Status:        "pending",
						NetworkPolicy: "allow_list",
						LifecycleMode: "on_demand",
						AllowedHosts:  []string{},
					}
					require.NoError(t, testStore.CreateSession(ctx, s))
					t.Cleanup(func() { _ = testStore.DeleteSession(context.Background(), s.ID) })
					ids = append(ids, s.ID)
				}

				workspaceID := workspace.ID
				return ids, func(ctx context.Context, opts store.BaseListOptions) (pageResult, error) {
					res, err := testStore.ListSessions(ctx, store.ListSessionsOptions{
						BaseListOptions: opts,
						WorkspaceID:     &workspaceID,
					})
					return toPage(res, err, func(s *store.Session) string { return s.ID })
				}
			},
		},
		{
			name: "runners",
			setup: func(ctx context.Context, t *testing.T, n int) ([]string, pagedList) {
				pool := "page-pool-" + time.Now().Format("150405.000000")

				ids := make([]string, 0, n)
				for i := 0; i < n; i++ {
					r := &store.Runner{
						Name:         fmt.Sprintf("page-runner-%02d-%s", i, pool),
						Hostname:     "localhost",
						Status:       "offline",
						SandboxMode:  "runner-is-sandbox",
						PoolName:     &pool,
						Capabilities: []string{},
						SandboxTypes: []string{},
					}
					require.NoError(t, testStore.CreateRunner(ctx, r))
					t.Cleanup(func() { _ = testStore.DeleteRunner(context.Background(), r.ID) })
					ids = append(ids, r.ID)
				}

				return ids, func(ctx context.Context, opts store.BaseListOptions) (pageResult, error) {
					res, err := testStore.ListRunners(ctx, store.ListRunnersOptions{
						BaseListOptions: opts,
						PoolName:        &pool,
					})
					return toPage(res, err, func(r *store.Runner) string { return r.ID })
				}
			},
		},
		{
			name: "tasks",
			setup: func(ctx context.Context, t *testing.T, n int) ([]string, pagedList) {
				session := createTestSession(ctx, t)

				ids := make([]string, 0, n)
				for i := 0; i < n; i++ {
					task := &store.Task{
						SessionID:      session.ID,
						Prompt:         fmt.Sprintf("page task %02d", i),
						Status:         "pending",
						TimeoutSeconds: 3600,
					}
					require.NoError(t, testStore.CreateTask(ctx, task))
					t.Cleanup(func() { _ = testStore.DeleteTask(context.Background(), task.ID) })
					ids = append(ids, task.ID)
				}

				sessionID := session.ID
				return ids, func(ctx context.Context, opts store.BaseListOptions) (pageResult, error) {
					res, err := testStore.ListTasks(ctx, store.ListTasksOptions{
						BaseListOptions: opts,
						SessionID:       &sessionID,
					})
					return toPage(res, err, func(t *store.Task) string { return t.ID })
				}
			},
		},
		{
			name: "permission requests",
			setup: func(ctx context.Context, t *testing.T, n int) ([]string, pagedList) {
				session, task, run, _ := createTestRunChain(ctx, t)

				ids := make([]string, 0, n)
				for i := 0; i < n; i++ {
					req := &store.PermissionRequest{
						OriginalRequestID:   fmt.Sprintf("tool_use_%02d", i),
						SessionID:           session,
						TaskID:              task,
						RunID:               run,
						Tool:                "bash",
						Action:              fmt.Sprintf("echo %d", i),
						RiskLevel:           "medium",
						Status:              "pending",
						SuspendAfterSeconds: 1800,
					}
					require.NoError(t, testStore.CreatePermissionRequest(ctx, req))
					ids = append(ids, req.ID)
				}

				return ids, func(ctx context.Context, opts store.BaseListOptions) (pageResult, error) {
					res, err := testStore.ListPermissionRequests(ctx, store.ListPermissionRequestsOptions{
						BaseListOptions: opts,
						RunID:           &run,
					})
					return toPage(res, err, func(p *store.PermissionRequest) string { return p.ID })
				}
			},
		},
	}
}

// TestListCursorPagination walks each listing a page at a time and checks the
// rows come back exactly once, in the order a single large page gives.
func TestListCursorPagination(t *testing.T) {
	ctx := context.Background()

	for _, tc := range paginationCases() {
		t.Run(tc.name, func(t *testing.T) {
			const total = 7
			created, list := tc.setup(ctx, t, total)
			require.Len(t, created, total)

			t.Run("ascending", func(t *testing.T) {
				seen := walk(ctx, t, list, 2, false)
				assertNoDuplicates(t, seen)
				assert.Equal(t, created, seen)
			})

			t.Run("descending", func(t *testing.T) {
				seen := walk(ctx, t, list, 2, true)
				assertNoDuplicates(t, seen)
				assert.Equal(t, reversed(created), seen)
			})

			t.Run("page size of one", func(t *testing.T) {
				seen := walk(ctx, t, list, 1, false)
				assertNoDuplicates(t, seen)
				assert.Equal(t, created, seen)
			})

			t.Run("page size larger than the result set", func(t *testing.T) {
				page, err := list(ctx, store.BaseListOptions{Limit: total + 50})
				require.NoError(t, err)
				assert.False(t, page.hasMore, "everything fit on one page")
				assert.Empty(t, page.nextCursor, "a complete page must not offer a cursor")
				assert.Equal(t, created, page.ids)
			})

			t.Run("page size exactly the result set", func(t *testing.T) {
				page, err := list(ctx, store.BaseListOptions{Limit: total})
				require.NoError(t, err)
				assert.False(t, page.hasMore, "an exactly-full page is still the last one")
				assert.Empty(t, page.nextCursor)
			})

			t.Run("total count does not shrink while paging", func(t *testing.T) {
				first, err := list(ctx, store.BaseListOptions{Limit: 2})
				require.NoError(t, err)
				require.NotEmpty(t, first.nextCursor)

				second, err := list(ctx, store.BaseListOptions{Limit: 2, Cursor: first.nextCursor})
				require.NoError(t, err)

				assert.Equal(t, int64(total), first.totalCount)
				assert.Equal(t, first.totalCount, second.totalCount,
					"TotalCount must count rows matching the filters, not rows left to read")
			})
		})
	}
}

// TestListCursorWithIdenticalKeys is the case the id tiebreaker exists for:
// rows sharing a created_at. Keyed on the timestamp alone, a page boundary
// landing inside such a run either skips rows or hands them out twice.
func TestListCursorWithIdenticalKeys(t *testing.T) {
	ctx := context.Background()

	workspace := &store.Workspace{
		Name:        "tie-ws-" + time.Now().Format("150405.000000"),
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	require.NoError(t, testStore.CreateWorkspace(ctx, workspace))
	t.Cleanup(func() { _ = testStore.DeleteWorkspace(context.Background(), workspace.ID) })

	const total = 7
	ids := make([]string, 0, total)
	for i := 0; i < total; i++ {
		name := fmt.Sprintf("tie-session-%02d", i)
		session := &store.Session{
			Name:          &name,
			WorkspaceID:   workspace.ID,
			Agent:         "claude",
			Status:        "pending",
			NetworkPolicy: "allow_list",
			LifecycleMode: "on_demand",
			AllowedHosts:  []string{},
		}
		require.NoError(t, testStore.CreateSession(ctx, session))
		t.Cleanup(func() { _ = testStore.DeleteSession(context.Background(), session.ID) })
		ids = append(ids, session.ID)
	}

	// Collapse every row onto one instant.
	shared := time.Now().UTC().Truncate(time.Second)
	_, err := testStore.Pool().Exec(ctx,
		"UPDATE sessions SET created_at = $1 WHERE workspace_id = $2", shared, workspace.ID)
	require.NoError(t, err)

	// With the keys tied, the id tiebreaker decides the order.
	expected := append([]string(nil), ids...)
	sort.Strings(expected)

	list := func(ctx context.Context, opts store.BaseListOptions) (pageResult, error) {
		res, err := testStore.ListSessions(ctx, store.ListSessionsOptions{
			BaseListOptions: opts,
			WorkspaceID:     &workspace.ID,
		})
		return toPage(res, err, func(s *store.Session) string { return s.ID })
	}

	t.Run("ascending", func(t *testing.T) {
		seen := walk(ctx, t, list, 2, false)
		assertNoDuplicates(t, seen)
		assert.Equal(t, expected, seen, "rows sharing a key must still page exactly once, in id order")
	})

	t.Run("descending", func(t *testing.T) {
		seen := walk(ctx, t, list, 2, true)
		assertNoDuplicates(t, seen)
		assert.Equal(t, reversed(expected), seen)
	})

	t.Run("page size of one", func(t *testing.T) {
		seen := walk(ctx, t, list, 1, false)
		assertNoDuplicates(t, seen)
		assert.Equal(t, expected, seen)
	})
}

func TestListCursorRejectsBadInput(t *testing.T) {
	ctx := context.Background()

	t.Run("malformed cursor", func(t *testing.T) {
		_, err := testStore.ListSessions(ctx, store.ListSessionsOptions{
			BaseListOptions: store.BaseListOptions{Cursor: "not a cursor!!"},
		})
		require.Error(t, err, "a malformed cursor must not silently restart the listing")
		assert.ErrorIs(t, err, store.ErrInvalidInput)
	})

	t.Run("cursor carrying a value of the wrong shape", func(t *testing.T) {
		// Well-formed encoding, but the key is not a timestamp.
		_, err := testStore.ListSessions(ctx, store.ListSessionsOptions{
			BaseListOptions: store.BaseListOptions{Cursor: encodeTestCursor("12345", "sess_x")},
		})
		require.Error(t, err)
		assert.ErrorIs(t, err, store.ErrInvalidInput)
	})

	t.Run("cursor combined with an ordering it does not describe", func(t *testing.T) {
		first, err := testStore.ListSessions(ctx, store.ListSessionsOptions{
			BaseListOptions: store.BaseListOptions{Limit: 1},
		})
		require.NoError(t, err)
		if first.NextCursor == "" {
			t.Skip("not enough sessions to produce a cursor")
		}

		_, err = testStore.ListSessions(ctx, store.ListSessionsOptions{
			BaseListOptions: store.BaseListOptions{
				Limit:   1,
				Cursor:  first.NextCursor,
				OrderBy: "name",
			},
		})
		require.Error(t, err, "a cursor is only valid for the ordering that produced it")
		assert.ErrorIs(t, err, store.ErrInvalidInput)
	})

	t.Run("no cursor offered for a non-default ordering", func(t *testing.T) {
		res, err := testStore.ListSessions(ctx, store.ListSessionsOptions{
			BaseListOptions: store.BaseListOptions{Limit: 1, OrderBy: "name"},
		})
		require.NoError(t, err)
		assert.Empty(t, res.NextCursor,
			"offering a cursor the next call would reject is worse than offering none")
	})
}

// TestLogCursorPaginationBySequence covers the one listing keyed on an integer
// sequence rather than a timestamp.
func TestLogCursorPaginationBySequence(t *testing.T) {
	ctx := context.Background()

	session, task, run, runner := createTestRunChain(ctx, t)

	const total = 6
	created := make([]string, 0, total)
	for i := 0; i < total; i++ {
		entry := &store.Log{
			SessionID: session,
			TaskID:    task,
			RunID:     run,
			RunnerID:  runner,
			Stream:    "stdout",
			Level:     "info",
			Content:   fmt.Sprintf("line %d", i),
			Sequence:  int64(i + 1),
		}
		require.NoError(t, testStore.CreateLog(ctx, entry))
		created = append(created, entry.ID)
	}

	list := func(ctx context.Context, opts store.BaseListOptions) (pageResult, error) {
		res, err := testStore.ListLogs(ctx, store.ListLogsOptions{
			BaseListOptions: opts,
			RunID:           &run,
		})
		return toPage(res, err, func(l *store.Log) string { return l.ID })
	}

	t.Run("ascending by sequence", func(t *testing.T) {
		seen := walk(ctx, t, list, 2, false)
		assertNoDuplicates(t, seen)
		assert.Equal(t, created, seen)
	})

	t.Run("descending by sequence", func(t *testing.T) {
		seen := walk(ctx, t, list, 2, true)
		assertNoDuplicates(t, seen)
		assert.Equal(t, reversed(created), seen)
	})

	t.Run("rejects a timestamp cursor", func(t *testing.T) {
		_, err := list(ctx, store.BaseListOptions{
			Limit:  2,
			Cursor: encodeTestCursor(time.Now().Format(time.RFC3339Nano), "log_x"),
		})
		require.Error(t, err, "a sequence-keyed listing must not accept a timestamp cursor")
		assert.ErrorIs(t, err, store.ErrInvalidInput)
	})
}

// Helpers -------------------------------------------------------------------

// encodeTestCursor mirrors the store's cursor encoding so tests can hand-craft
// one. Kept deliberately independent of the implementation: a test that reused
// the production encoder could not catch a change of format.
func encodeTestCursor(value, id string) string {
	return base64.URLEncoding.EncodeToString([]byte(value + "|" + id))
}

// createTestRunChain creates the workspace -> session -> task -> run chain that
// logs and permission requests hang off, and returns the IDs.
func createTestRunChain(ctx context.Context, t *testing.T) (sessionID, taskID, runID, runnerID string) {
	t.Helper()

	stamp := time.Now().Format("150405.000000")

	workspace := &store.Workspace{
		Name:        "chain-ws-" + stamp,
		Persist:     true,
		StorageType: "volume",
		Mobility:    "local",
	}
	require.NoError(t, testStore.CreateWorkspace(ctx, workspace))
	t.Cleanup(func() { _ = testStore.DeleteWorkspace(context.Background(), workspace.ID) })

	session := &store.Session{
		Status:        "pending",
		WorkspaceID:   workspace.ID,
		Agent:         "claude",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
		LifecycleMode: "on_demand",
	}
	require.NoError(t, testStore.CreateSession(ctx, session))
	t.Cleanup(func() { _ = testStore.DeleteSession(context.Background(), session.ID) })

	task := &store.Task{
		SessionID:      session.ID,
		Prompt:         "chain prompt",
		Status:         "running",
		TimeoutSeconds: 3600,
	}
	require.NoError(t, testStore.CreateTask(ctx, task))

	runner := &store.Runner{
		Name:         "chain-runner-" + stamp,
		Hostname:     "localhost",
		Status:       "busy",
		SandboxMode:  "runner-is-sandbox",
		Capabilities: []string{},
		SandboxTypes: []string{},
	}
	require.NoError(t, testStore.CreateRunner(ctx, runner))
	t.Cleanup(func() { _ = testStore.DeleteRunner(context.Background(), runner.ID) })

	run := &store.TaskRun{
		TaskID:   task.ID,
		Attempt:  1,
		Status:   "running",
		RunnerID: &runner.ID,
	}
	require.NoError(t, testStore.CreateTaskRun(ctx, run))

	return session.ID, task.ID, run.ID, runner.ID
}
