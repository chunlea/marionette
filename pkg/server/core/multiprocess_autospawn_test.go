package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
	pgstore "github.com/chunlea/marionette/pkg/store/postgres"
)

// Two servers, one database, two sessions with nowhere to run.
//
// R4's claim made allocation safe across processes; auto-spawn adds a runner
// that appears mid-race. The question this answers is whether the runner one
// replica spawned can be taken by the other replica's session before the
// session that paid for it ever sees it - which is exactly what would happen if
// the claim were taken after the spawn, or in memory.

// newAutoSpawnApp is one "process" whose default provider spawns.
func newAutoSpawnApp(t *testing.T, dsn string, prov provider.Provider) *testApp {
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
		ProviderRegistry:   &fakeProviderRegistry{prov: prov},
		RunnerServerURL:    "grpc://server:9090",
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
			DisableLiveFanout:          true,
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = app.Stop(context.Background()) })

	return &testApp{app: app, store: s}
}

// countingProvider spawns for anyone, and is safe to share between two apps.
type countingProvider struct {
	mu     sync.Mutex
	spawns []provider.SpawnOptions
}

func (p *countingProvider) Name() string                { return "docker" }
func (p *countingProvider) Type() provider.ProviderType { return provider.ProviderTypeManaged }

func (p *countingProvider) Spawn(_ context.Context, opts provider.SpawnOptions) (*provider.RunnerInstance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.spawns = append(p.spawns, opts)
	return &provider.RunnerInstance{
		ID:         opts.RunnerID,
		ProviderID: "inst_" + opts.RunnerID,
		Status:     provider.InstanceStatusPending,
	}, nil
}

func (p *countingProvider) Destroy(context.Context, string, provider.DestroyOptions) error {
	return nil
}

func (p *countingProvider) Status(context.Context, string) (*provider.RunnerStatus, error) {
	return nil, fmt.Errorf("not implemented")
}
func (p *countingProvider) List(context.Context) ([]*provider.RunnerInstance, error) { return nil, nil }
func (p *countingProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{}
}

func (p *countingProvider) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.spawns)
}

// TestTwoProcesses_AutoSpawnedRunnersAreNotStolen: both replicas spawn at the
// same moment, and each session must end up on the runner spawned for it.
func TestTwoProcesses_AutoSpawnedRunnersAreNotStolen(t *testing.T) {
	dsn := startPostgres(t)
	prov := &countingProvider{}
	first := newAutoSpawnApp(t, dsn, prov)
	second := newAutoSpawnApp(t, dsn, prov)
	resetState(t, first)

	sessionA := seedSession(t, first, "spawn-race-a")
	sessionB := seedSession(t, second, "spawn-race-b")

	// Both allocate at once, with nothing idle: each must ask the provider.
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

	for i, err := range errs {
		require.ErrorIs(t, err, ErrNoRunnerAvailable,
			"a session whose runner is still booting stays pending (replica %d)", i)
		require.ErrorIs(t, err, ErrRunnerSpawning,
			"and the reason must say a spawn is under way (replica %d)", i)
	}
	require.Equal(t, 2, prov.count(), "each session needs its own runner")

	// The claim is what stops one session taking the other's spawn. Nothing has
	// connected yet, so this is the only thing standing between them.
	spawned := autoSpawnedRunners(t, first)
	require.Len(t, spawned, 2)

	claimedFor := map[string]string{}
	for _, runner := range spawned {
		labels := map[string]string{}
		require.NoError(t, json.Unmarshal(runner.Labels, &labels))
		claimedFor[labels[AutoSpawnSessionLabel]] = runner.ID
	}
	require.Contains(t, claimedFor, sessionA)
	require.Contains(t, claimedFor, sessionB)

	// A rival cannot take a claimed spawn...
	won, err := first.store.ClaimRunner(context.Background(),
		claimedFor[sessionB], sessionA, store.DefaultRunnerClaimLease)
	require.NoError(t, err)
	assert.False(t, won, "session A must not be able to claim the runner spawned for session B")

	// ...and when the runners connect, each session gets its own.
	for sessionID, runnerID := range claimedFor {
		require.NoError(t, first.store.UpdateRunner(context.Background(), runnerID,
			store.RunnerUpdates{Status: stringPtr(StatusIdle)}), "session %s", sessionID)
	}

	updatedA, err := first.app.Sessions.EnsureRunner(context.Background(), sessionA)
	require.NoError(t, err)
	require.NotNil(t, updatedA.RunnerID)
	assert.Equal(t, claimedFor[sessionA], *updatedA.RunnerID,
		"a session must land on the runner that was spawned for it")

	updatedB, err := second.app.Sessions.EnsureRunner(context.Background(), sessionB)
	require.NoError(t, err)
	require.NotNil(t, updatedB.RunnerID)
	assert.Equal(t, claimedFor[sessionB], *updatedB.RunnerID)

	terminateAll(t, first, sessionA, sessionB)
}

// TestTwoProcesses_AutoSpawnBudgetIsShared: the budget lives in the database,
// so two replicas cannot each spend the whole of it.
func TestTwoProcesses_AutoSpawnBudgetIsShared(t *testing.T) {
	dsn := startPostgres(t)
	prov := &countingProvider{}
	first := newAutoSpawnAppWithBudget(t, dsn, prov, 1)
	second := newAutoSpawnAppWithBudget(t, dsn, prov, 1)
	resetState(t, first)

	sessionA := seedSession(t, first, "budget-a")
	sessionB := seedSession(t, second, "budget-b")

	_, err := first.app.Sessions.EnsureRunner(context.Background(), sessionA)
	require.ErrorIs(t, err, ErrRunnerSpawning)

	_, err = second.app.Sessions.EnsureRunner(context.Background(), sessionB)
	require.ErrorIs(t, err, ErrNoRunnerAvailable)
	require.NotErrorIs(t, err, ErrRunnerSpawning,
		"the other replica's spawn has already spent the budget")

	assert.Equal(t, 1, prov.count())

	terminateAll(t, first, sessionA, sessionB)
}

func newAutoSpawnAppWithBudget(t *testing.T, dsn string, prov provider.Provider, max int) *testApp {
	t.Helper()

	app := newAutoSpawnApp(t, dsn, prov)
	app.app.Sessions.autoSpawn = AutoSpawnPolicy{Enabled: true, MaxRunners: max}
	return app
}

// autoSpawnedRunners returns the runners this server spawned on its own.
func autoSpawnedRunners(t *testing.T, a *testApp) []*store.Runner {
	t.Helper()

	result, err := a.store.ListRunners(context.Background(), store.ListRunnersOptions{
		BaseListOptions: store.BaseListOptions{Limit: 100},
		Labels:          map[string]string{AutoSpawnLabel: "true"},
	})
	require.NoError(t, err)
	return result.Items
}
