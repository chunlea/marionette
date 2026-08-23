package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"go.uber.org/zap"

	pb "github.com/chunlea/marionette/gen/proto/v1"
	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	pgstore "github.com/chunlea/marionette/pkg/store/postgres"
)

// Two servers, one database.
//
// Runner allocation reads the idle runners and then asks whether a live session
// already owns one. Those are two statements, and between them the runner still
// looks free: two processes both pass and both take it. The in-process
// reservation that used to close this window could not see the other process at
// all, so the whole guarantee evaporated the moment a second replica existed.
//
// These tests run the real allocation path from two independently Wire()'d apps
// against one Postgres, which is the only way to demonstrate that the arbiter
// is the database and not a map in one process's memory.

// dockerAvailable reports whether a Docker daemon is reachable. testcontainers
// panics rather than erroring when there is no Docker host at all, so the probe
// has to recover.
func dockerAvailable(ctx context.Context) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()

	prov, err := testcontainers.NewDockerProvider()
	if err != nil {
		return false
	}
	defer func() { _ = prov.Close() }()

	return prov.Health(ctx) == nil
}

// One container serves every DB-backed test in this package: starting a
// Postgres per test costs more than the tests themselves.
var (
	sharedDBOnce sync.Once
	sharedDSN    string
	sharedDBStop func()
)

// TestMain exists only to tear the shared container down. It is started lazily,
// so a run with no DB-backed tests selected never pays for it.
func TestMain(m *testing.M) {
	code := m.Run()
	if sharedDBStop != nil {
		sharedDBStop()
	}
	os.Exit(code)
}

// startPostgres brings up a database with every migration applied, or skips.
//
// Skipping rather than failing is deliberate here: unlike pkg/store/postgres,
// this package's other tests are pure unit tests and must keep running on a
// machine with no Docker.
func startPostgres(t *testing.T) string {
	t.Helper()

	sharedDBOnce.Do(func() { sharedDSN = bootPostgres(t) })
	if sharedDSN == "" {
		t.Skip("no Docker daemon reachable: skipping the two-process claim test")
	}
	return sharedDSN
}

// bootPostgres starts the container and applies the migrations. It returns ""
// when there is no Docker to run it on.
func bootPostgres(t *testing.T) string {
	t.Helper()

	ctx := context.Background()
	if !dockerAvailable(ctx) {
		return ""
	}

	container, err := tcpostgres.Run(ctx,
		"postgres:15-alpine",
		tcpostgres.WithDatabase("marionette_claim_test"),
		tcpostgres.WithUsername("test"),
		tcpostgres.WithPassword("test"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(90*time.Second),
		),
	)
	require.NoError(t, err)
	sharedDBStop = func() { _ = container.Terminate(context.Background()) }

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	migrator, err := pgstore.New(ctx, pgstore.Config{URL: dsn}, zap.NewNop())
	require.NoError(t, err)
	defer func() { _ = migrator.Close() }()

	// Glob rather than a fixed list: a migration this test does not know about
	// must not be silently skipped, or it would validate a stale schema.
	files, err := filepath.Glob("../../../migrations/*.up.sql")
	require.NoError(t, err)
	require.NotEmpty(t, files, "no migrations found")
	sort.Strings(files)

	for _, file := range files {
		sql, err := os.ReadFile(file)
		require.NoError(t, err)
		require.NoError(t, migrator.ExecRaw(ctx, string(sql)), "applying %s", file)
	}

	return dsn
}

// allConnected reports every runner as connected: these tests are about who
// gets a runner, not about connectivity.
type allConnected struct{}

func (allConnected) IsConnected(string) bool                     { return true }
func (allConnected) UpdateLastSeen(string) error                 { return nil }
func (allConnected) SendCommand(string, *pb.ServerCommand) error { return nil }

// testApp is one "process": its own store handle, its own managers.
type testApp struct {
	app   *App
	store *pgstore.Store
}

func newTestApp(t *testing.T, dsn string) *testApp {
	t.Helper()

	s, err := pgstore.New(context.Background(), pgstore.Config{URL: dsn}, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	conn := allConnected{}
	app, err := Wire(WireDeps{
		Store:              s,
		ConnManager:        conn,
		CmdSender:          conn,
		RunnerTokenService: auth.NewRunnerTokenService(s, id.RunnerToken),
		ProviderRegistry:   &fakeProviderRegistry{err: errors.New("no provider")},
		Logger:             zap.NewNop(),
		Jobs: JobsConfig{
			DisableStaleDetector:       true,
			DisableTaskTimeout:         true,
			DisablePermissionTimeout:   true,
			DisableScheduledTasks:      true,
			DisableScheduledSessions:   true,
			DisableReaper:              true,
			DisablePartitionMaintainer: true,
			DisableChunkGC:             true,
			DisableRedispatch:          true,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	return &testApp{app: app, store: s}
}

// resetState clears everything the previous test left behind. The container is
// shared, so without this a test would allocate a runner some earlier test
// created and its assertions would be about the wrong row.
func resetState(t *testing.T, a *testApp) {
	t.Helper()
	require.NoError(t, a.store.ExecRaw(context.Background(),
		`TRUNCATE sessions, runners, workspaces, tasks, task_runs, runner_tokens CASCADE`))
}

// seedRunners creates idle runners both apps can see.
func seedRunners(t *testing.T, a *testApp, n int) []string {
	t.Helper()

	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		runner := &store.Runner{
			ID:           id.Runner(),
			Name:         "claim-runner-" + id.Runner(),
			Status:       StatusIdle,
			SandboxMode:  "runner-is-sandbox",
			SandboxTypes: []string{},
			Capabilities: []string{},
			Labels:       json.RawMessage("{}"),
			Annotations:  json.RawMessage("{}"),
		}
		require.NoError(t, a.store.CreateRunner(context.Background(), runner))
		ids = append(ids, runner.ID)
	}
	return ids
}

// seedSession creates a pending session through the real manager, so the row
// is exactly the one production writes - constraints, defaults and all.
func seedSession(t *testing.T, a *testApp, name string) string {
	t.Helper()

	ctx := context.Background()
	ws, err := a.app.Workspaces.Create(ctx, CreateWorkspaceOptions{Name: name})
	require.NoError(t, err)

	sess, err := a.app.Sessions.Create(ctx, CreateSessionOptions{
		Name:        &name,
		WorkspaceID: ws.ID,
		Agent:       "claude",
		IsBYOK:      true,
	})
	require.NoError(t, err)
	return sess.ID
}

// TestTwoProcesses_OneRunnerGoesToOneSession is the regression: one idle runner,
// two servers racing for it. Exactly one session may end up holding it, and the
// loser must fail cleanly rather than take it away from the winner.
func TestTwoProcesses_OneRunnerGoesToOneSession(t *testing.T) {
	dsn := startPostgres(t)
	first := newTestApp(t, dsn)
	second := newTestApp(t, dsn)

	for round := 0; round < 5; round++ {
		// Each round starts from nothing: a runner left over from the previous
		// round would give both sessions somewhere to go and the race would
		// stop being a race.
		resetState(t, first)
		runners := seedRunners(t, first, 1)
		sessionA := seedSession(t, first, fmt.Sprintf("a-%d", round))
		sessionB := seedSession(t, second, fmt.Sprintf("b-%d", round))

		start := make(chan struct{})
		var wg sync.WaitGroup
		errs := make([]error, 2)

		for i, attempt := range []struct {
			app     *testApp
			session string
		}{{first, sessionA}, {second, sessionB}} {
			wg.Add(1)
			go func(i int, app *testApp, sessionID string) {
				defer wg.Done()
				<-start
				_, errs[i] = app.app.Sessions.EnsureRunner(context.Background(), sessionID)
			}(i, attempt.app, attempt.session)
		}
		close(start)
		wg.Wait()

		won := 0
		for _, err := range errs {
			if err == nil {
				won++
				continue
			}
			// The only acceptable failure is "there was no runner for me",
			// which is what a clean lost race looks like from the loser's side.
			require.ErrorIs(t, err, ErrNoRunnerAvailable, "a lost race must not surface as an unexpected error")
		}
		assert.Equal(t, 1, won, "exactly one session must get the runner, round %d", round)

		// And the winner must still hold it: the loser detaching the winner is
		// the failure mode the runner claim exists to stop.
		holders := sessionsHolding(t, first, runners[0])
		assert.Len(t, holders, 1, "exactly one session may hold the runner, round %d", round)

		terminateAll(t, first, sessionA, sessionB)
	}
}

// TestTwoProcesses_RunnersAreNotDoubleAllocated: N runners, 2N sessions split
// across two servers. Every runner may serve at most one session, and no
// session may be left holding a runner another session also holds.
func TestTwoProcesses_RunnersAreNotDoubleAllocated(t *testing.T) {
	dsn := startPostgres(t)
	first := newTestApp(t, dsn)
	second := newTestApp(t, dsn)
	resetState(t, first)

	const runnerCount = 6
	runners := seedRunners(t, first, runnerCount)

	var sessions []string
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := 0; i < runnerCount*2; i++ {
		app := first
		if i%2 == 1 {
			app = second
		}
		sessionID := seedSession(t, app, fmt.Sprintf("session-%d", i))
		sessions = append(sessions, sessionID)

		wg.Add(1)
		go func(app *testApp, sessionID string) {
			defer wg.Done()
			<-start
			_, _ = app.app.Sessions.EnsureRunner(context.Background(), sessionID)
		}(app, sessionID)
	}

	close(start)
	wg.Wait()

	for _, runnerID := range runners {
		holders := sessionsHolding(t, first, runnerID)
		assert.LessOrEqual(t, len(holders), 1,
			"runner %s is held by more than one session: %v", runnerID, holders)
	}

	terminateAll(t, first, sessions...)
}

// TestTwoProcesses_ClaimIsReleasedForTheNextAllocation: a claim taken and
// released must not leave the runner unusable. Without the release a runner
// would be stuck until its lease expired, which is a slow-motion outage.
func TestTwoProcesses_ClaimIsReleasedForTheNextAllocation(t *testing.T) {
	dsn := startPostgres(t)
	first := newTestApp(t, dsn)
	second := newTestApp(t, dsn)
	resetState(t, first)

	runners := seedRunners(t, first, 1)
	ctx := context.Background()

	sessionA := seedSession(t, first, "first-holder")
	_, err := first.app.Sessions.EnsureRunner(ctx, sessionA)
	require.NoError(t, err)

	// Free the runner the way a real suspend does.
	require.NoError(t, first.app.Sessions.Terminate(ctx, sessionA))
	require.NoError(t, first.store.UpdateRunner(ctx, runners[0], store.RunnerUpdates{
		Status: stringPtr(StatusIdle),
	}))

	// The other process must be able to take it immediately.
	sessionB := seedSession(t, second, "second-holder")
	_, err = second.app.Sessions.EnsureRunner(ctx, sessionB)
	require.NoError(t, err, "a released claim must not block the next allocation")

	holders := sessionsHolding(t, second, runners[0])
	require.Len(t, holders, 1)
	assert.Equal(t, sessionB, holders[0])

	terminateAll(t, second, sessionB)
}

// sessionsHolding returns the live sessions that name a runner.
func sessionsHolding(t *testing.T, a *testApp, runnerID string) []string {
	t.Helper()

	result, err := a.store.ListSessions(context.Background(), store.ListSessionsOptions{
		BaseListOptions: store.BaseListOptions{Limit: 100},
		RunnerID:        &runnerID,
		Status:          liveSessionStatuses,
	})
	require.NoError(t, err)

	ids := make([]string, 0, len(result.Items))
	for _, sess := range result.Items {
		ids = append(ids, sess.ID)
	}
	return ids
}

// terminateAll frees the runners a round held so the next round starts clean.
func terminateAll(t *testing.T, a *testApp, sessionIDs ...string) {
	t.Helper()
	for _, sessionID := range sessionIDs {
		if err := a.app.Sessions.Terminate(context.Background(), sessionID); err != nil &&
			!errors.Is(err, ErrSessionAlreadyTerminated) {
			t.Logf("cleanup: terminating %s: %v", sessionID, err)
		}
	}
}
