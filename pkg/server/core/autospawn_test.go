package core

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

// autoSpawnStore is a testStore that actually keeps runners, runner tokens and
// provider configs. The base fake stubs them out, and auto-spawn is entirely
// about what those rows say: the budget counts them, the pending-spawn guard
// finds them, and the claim is written on one.
type autoSpawnStore struct {
	*testStore

	mu      sync.Mutex
	runners map[string]*store.Runner
	tokens  map[string]*store.RunnerToken
	configs map[string]*store.ProviderConfig
	// labelListErr fails only the label-filtered queries, which are the ones
	// auto-spawn makes: failing every ListRunners would fail allocation itself
	// and never reach the code under test.
	labelListErr error
}

func newAutoSpawnStore() *autoSpawnStore {
	return &autoSpawnStore{
		testStore: newTestStore(),
		runners:   map[string]*store.Runner{},
		tokens:    map[string]*store.RunnerToken{},
		configs:   map[string]*store.ProviderConfig{},
	}
}

func (s *autoSpawnStore) CreateRunner(_ context.Context, runner *store.Runner) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if runner.CreatedAt.IsZero() {
		runner.CreatedAt = time.Now()
	}
	copied := *runner
	s.runners[runner.ID] = &copied
	return nil
}

func (s *autoSpawnStore) GetRunner(_ context.Context, runnerID string) (*store.Runner, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	runner, ok := s.runners[runnerID]
	if !ok {
		return nil, store.ErrNotFound
	}
	copied := *runner
	return &copied, nil
}

func (s *autoSpawnStore) ListRunners(
	_ context.Context,
	opts store.ListRunnersOptions,
) (*store.ListResult[store.Runner], error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.labelListErr != nil && len(opts.Labels) > 0 {
		return nil, s.labelListErr
	}

	items := make([]*store.Runner, 0, len(s.runners))
	for _, runner := range s.runners {
		if len(opts.Status) > 0 && !containsString(opts.Status, runner.Status) {
			continue
		}
		if !runnerHasLabels(runner, opts.Labels) {
			continue
		}
		copied := *runner
		items = append(items, &copied)
	}
	return &store.ListResult[store.Runner]{Items: items, TotalCount: int64(len(items))}, nil
}

func (s *autoSpawnStore) UpdateRunner(_ context.Context, runnerID string, updates store.RunnerUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	runner, ok := s.runners[runnerID]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		runner.Status = *updates.Status
	}
	if updates.ProviderInstanceID != nil {
		runner.ProviderInstanceID = updates.ProviderInstanceID
	}
	return nil
}

func (s *autoSpawnStore) DeleteRunner(_ context.Context, runnerID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runners, runnerID)
	return nil
}

func (s *autoSpawnStore) CreateRunnerToken(_ context.Context, token *store.RunnerToken) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	copied := *token
	s.tokens[token.ID] = &copied
	return nil
}

func (s *autoSpawnStore) GetRunnerToken(_ context.Context, tokenID string) (*store.RunnerToken, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.tokens[tokenID]
	if !ok {
		return nil, store.ErrNotFound
	}
	copied := *token
	return &copied, nil
}

func (s *autoSpawnStore) UpdateRunnerToken(_ context.Context, tokenID string, updates store.RunnerTokenUpdates) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	token, ok := s.tokens[tokenID]
	if !ok {
		return store.ErrNotFound
	}
	if updates.RunnerID != nil {
		token.RunnerID = updates.RunnerID
	}
	return nil
}

func (s *autoSpawnStore) GetProviderConfigByName(_ context.Context, name string) (*store.ProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, cfg := range s.configs {
		if cfg.Name == name {
			copied := *cfg
			return &copied, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *autoSpawnStore) GetProviderConfig(_ context.Context, configID string) (*store.ProviderConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cfg, ok := s.configs[configID]
	if !ok {
		return nil, store.ErrNotFound
	}
	copied := *cfg
	return &copied, nil
}

// seedRunner puts a runner row in place without going through a spawn.
func (s *autoSpawnStore) seedRunner(runner *store.Runner) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if runner.CreatedAt.IsZero() {
		runner.CreatedAt = time.Now()
	}
	s.runners[runner.ID] = runner
}

func (s *autoSpawnStore) runnerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.runners)
}

func (s *autoSpawnStore) onlyRunner(t *testing.T) *store.Runner {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	require.Len(t, s.runners, 1)
	for _, runner := range s.runners {
		copied := *runner
		return &copied
	}
	return nil
}

// autoSpawnProvider is a managed provider that records what it was asked for.
// (runner_provisioner_test.go has a similar one; this one is configurable per
// test and counts failed attempts, which the backoff assertions need.)
type autoSpawnProvider struct {
	mu       sync.Mutex
	name     string
	kind     provider.ProviderType
	spawnErr error
	spawns   []provider.SpawnOptions
	attempts int
}

func (p *autoSpawnProvider) Name() string { return p.name }
func (p *autoSpawnProvider) Type() provider.ProviderType {
	if p.kind == "" {
		return provider.ProviderTypeManaged
	}
	return p.kind
}

func (p *autoSpawnProvider) Spawn(_ context.Context, opts provider.SpawnOptions) (*provider.RunnerInstance, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.attempts++
	if p.spawnErr != nil {
		return nil, p.spawnErr
	}
	p.spawns = append(p.spawns, opts)
	return &provider.RunnerInstance{
		ID:         opts.RunnerID,
		ProviderID: "inst_" + opts.RunnerID,
		Status:     provider.InstanceStatusPending,
	}, nil
}

func (p *autoSpawnProvider) Destroy(context.Context, string, provider.DestroyOptions) error {
	return nil
}
func (p *autoSpawnProvider) Status(context.Context, string) (*provider.RunnerStatus, error) {
	return nil, errors.New("not implemented")
}
func (p *autoSpawnProvider) List(context.Context) ([]*provider.RunnerInstance, error) {
	return nil, nil
}
func (p *autoSpawnProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{}
}

func (p *autoSpawnProvider) spawnCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.spawns)
}

// attemptCount counts every call, failures included: a backoff that stopped
// counting failures would not be a backoff.
func (p *autoSpawnProvider) attemptCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.attempts
}

func (p *autoSpawnProvider) lastSpawn(t *testing.T) provider.SpawnOptions {
	t.Helper()

	p.mu.Lock()
	defer p.mu.Unlock()
	require.NotEmpty(t, p.spawns)
	return p.spawns[len(p.spawns)-1]
}

// autoSpawnFixture is a session manager whose provider spawns.
type autoSpawnFixture struct {
	manager  *SessionManager
	store    *autoSpawnStore
	provider *autoSpawnProvider
	session  *store.Session
}

func newAutoSpawnFixture(t *testing.T, policy AutoSpawnPolicy) *autoSpawnFixture {
	t.Helper()

	s := newAutoSpawnStore()
	prov := &autoSpawnProvider{name: "docker"}
	registry := &fakeProviderRegistry{prov: prov}

	manager := NewSessionManagerWithConfig(SessionManagerConfig{
		Store:            s,
		ConnManager:      &mockConnManagerForSession{},
		CmdSender:        &mockCommandSender{},
		ProviderRegistry: registry,
		AutoSpawn:        policy,
		Logger:           zap.NewNop(),
	})
	manager.setProvisioner(NewRunnerProvisioner(
		s, registry, auth.NewRunnerTokenService(s, id.RunnerToken),
		"grpc://server:9090", zap.NewNop(),
	))

	session := &store.Session{
		ID:            id.Session(),
		Status:        SessionStatusPending,
		WorkspaceID:   "ws_1",
		Agent:         "claude",
		NetworkPolicy: "air_gapped",
		AllowedHosts:  []string{"api.anthropic.com"},
		Annotations:   json.RawMessage("{}"),
	}
	require.NoError(t, s.CreateSession(context.Background(), session))

	return &autoSpawnFixture{manager: manager, store: s, provider: prov, session: session}
}

// The dogfood catch: a fresh Docker deployment had zero idle runners, so
// allocation found nothing and never asked the provider whose whole job is to
// make one.
func TestAutoSpawnAsksTheProviderWhenNothingIsIdle(t *testing.T) {
	fixture := newAutoSpawnFixture(t, AutoSpawnPolicy{Enabled: true})

	_, err := fixture.manager.allocateRunner(context.Background(), fixture.session)
	require.ErrorIs(t, err, ErrRunnerSpawning)
	require.ErrorIs(t, err, ErrNoRunnerAvailable,
		"callers must keep treating this as 'no runner': the task stays pending either way")

	require.Equal(t, 1, fixture.provider.spawnCount())
	runner := fixture.store.onlyRunner(t)

	labels := map[string]string{}
	require.NoError(t, json.Unmarshal(runner.Labels, &labels))
	assert.Equal(t, "true", labels[AutoSpawnLabel])
	assert.Equal(t, fixture.session.ID, labels[AutoSpawnSessionLabel])
	assert.Equal(t, StatusOffline, runner.Status,
		"a spawned runner is offline until its agent connects")
}

// A session that asked to be air-gapped must not get an unrestricted container
// merely because the server, rather than an operator, asked for it.
func TestAutoSpawnCarriesTheSessionsNetworkPolicy(t *testing.T) {
	fixture := newAutoSpawnFixture(t, AutoSpawnPolicy{Enabled: true})

	_, err := fixture.manager.allocateRunner(context.Background(), fixture.session)
	require.ErrorIs(t, err, ErrRunnerSpawning)

	spawn := fixture.provider.lastSpawn(t)
	assert.Equal(t, "air_gapped", spawn.NetworkPolicy)
	assert.Equal(t, []string{"api.anthropic.com"}, spawn.AllowedHosts)
	assert.NotEmpty(t, spawn.RunnerToken, "an instance with no token can never connect")
	assert.Equal(t, "grpc://server:9090", spawn.ServerURL)
}

// The claim is taken between writing the runner row and asking the provider for
// an instance, so a rival cannot take the spawn the moment it connects.
func TestAutoSpawnClaimsTheRunnerBeforeItCanConnect(t *testing.T) {
	fixture := newAutoSpawnFixture(t, AutoSpawnPolicy{Enabled: true})

	_, err := fixture.manager.allocateRunner(context.Background(), fixture.session)
	require.ErrorIs(t, err, ErrRunnerSpawning)

	runner := fixture.store.onlyRunner(t)
	won, err := fixture.store.ClaimRunner(context.Background(), runner.ID, "sess_rival", store.DefaultRunnerClaimLease)
	require.NoError(t, err)
	assert.False(t, won, "another session must not be able to claim a runner spawned for this one")

	// And the session that spawned it still can: its own claim is not in its
	// own way when allocation eventually runs.
	won, err = fixture.store.ClaimRunner(context.Background(), runner.ID, fixture.session.ID, store.DefaultRunnerClaimLease)
	require.NoError(t, err)
	assert.True(t, won)
}

// The sweeper fires every 60 seconds and every runner-joined edge fires too. A
// boot takes longer than that, so without a guard one session would spawn a
// fleet.
func TestAutoSpawnDoesNotSpawnTwiceForOneSession(t *testing.T) {
	fixture := newAutoSpawnFixture(t, AutoSpawnPolicy{Enabled: true})
	ctx := context.Background()

	_, err := fixture.manager.allocateRunner(ctx, fixture.session)
	require.ErrorIs(t, err, ErrRunnerSpawning)

	_, err = fixture.manager.allocateRunner(ctx, fixture.session)
	require.ErrorIs(t, err, ErrNoRunnerAvailable)
	require.NotErrorIs(t, err, ErrRunnerSpawning)

	assert.Equal(t, 1, fixture.provider.spawnCount(),
		"a runner that is still booting is a reason not to spawn another")
}

func TestAutoSpawnRespectsTheBudget(t *testing.T) {
	fixture := newAutoSpawnFixture(t, AutoSpawnPolicy{Enabled: true, MaxRunners: 2})
	ctx := context.Background()

	// Two live auto-spawned runners on the same (empty) provider config, busy
	// with other sessions' work - they count against the budget and are not
	// candidates for this session.
	for i := 0; i < 2; i++ {
		fixture.store.seedRunner(&store.Runner{
			ID:     id.Runner(),
			Name:   "existing",
			Status: StatusBusy,
			Labels: json.RawMessage(`{"` + AutoSpawnLabel + `":"true","` + AutoSpawnSessionLabel + `":"sess_other"}`),
		})
	}

	_, err := fixture.manager.allocateRunner(ctx, fixture.session)
	require.ErrorIs(t, err, ErrNoRunnerAvailable)
	require.NotErrorIs(t, err, ErrRunnerSpawning)
	assert.Equal(t, 0, fixture.provider.spawnCount())

	// And the refusal is visible on the session rather than only in a log.
	session, err := fixture.store.GetSession(ctx, fixture.session.ID)
	require.NoError(t, err)
	annotations := decodeAnnotations(session.Annotations)
	assert.Contains(t, annotations[autoSpawnErrorAnnotation], "budget spent")
	assert.NotEmpty(t, annotations[autoSpawnRetryAnnotation])
}

// Destroy leaves the runner row behind with status offline. Counting every
// offline row would let a deployment's history spend a budget that is meant to
// describe what is running now.
func TestAutoSpawnBudgetIgnoresLongDeadRunners(t *testing.T) {
	fixture := newAutoSpawnFixture(t, AutoSpawnPolicy{Enabled: true, MaxRunners: 1})

	fixture.store.seedRunner(&store.Runner{
		ID:        id.Runner(),
		Name:      "destroyed-last-week",
		Status:    StatusOffline,
		CreatedAt: time.Now().Add(-7 * 24 * time.Hour),
		Labels:    json.RawMessage(`{"` + AutoSpawnLabel + `":"true"}`),
	})

	_, err := fixture.manager.allocateRunner(context.Background(), fixture.session)
	require.ErrorIs(t, err, ErrRunnerSpawning)
	assert.Equal(t, 1, fixture.provider.spawnCount())
}

// A spawn that fails because the daemon is down fails the same way for every
// session. Retrying it on every sweep would be a hot loop against something
// already broken.
func TestAutoSpawnFailureIsVisibleAndBacksOff(t *testing.T) {
	fixture := newAutoSpawnFixture(t, AutoSpawnPolicy{Enabled: true})
	fixture.provider.spawnErr = errors.New("cannot connect to the docker daemon")
	ctx := context.Background()

	_, err := fixture.manager.allocateRunner(ctx, fixture.session)
	require.ErrorIs(t, err, ErrNoRunnerAvailable)
	require.NotErrorIs(t, err, ErrRunnerSpawning)

	session, err := fixture.store.GetSession(ctx, fixture.session.ID)
	require.NoError(t, err)
	annotations := decodeAnnotations(session.Annotations)
	assert.Contains(t, annotations[autoSpawnErrorAnnotation], "docker daemon")
	assert.Equal(t, "1", annotations[autoSpawnAttemptsAnnotation])

	retryAt, err := time.Parse(time.RFC3339, annotations[autoSpawnRetryAnnotation])
	require.NoError(t, err)
	assert.True(t, retryAt.After(time.Now()), "the next attempt must be scheduled, not immediate")

	// A failed spawn rolls its row back, so the pending guard cannot be what
	// stops the second attempt - the backoff has to.
	assert.Zero(t, fixture.store.runnerCount())

	fresh, err := fixture.store.GetSession(ctx, fixture.session.ID)
	require.NoError(t, err)
	_, err = fixture.manager.allocateRunner(ctx, fresh)
	require.ErrorIs(t, err, ErrNoRunnerAvailable)
	assert.Equal(t, 1, fixture.provider.attemptCount(),
		"the provider must not be asked again before the backoff expires")
}

func TestAutoSpawnIsSkippedForPoolProviders(t *testing.T) {
	fixture := newAutoSpawnFixture(t, AutoSpawnPolicy{Enabled: true})
	fixture.provider.kind = provider.ProviderTypePool

	_, err := fixture.manager.allocateRunner(context.Background(), fixture.session)
	require.ErrorIs(t, err, ErrNoRunnerAvailable)
	require.NotErrorIs(t, err, ErrRunnerSpawning)
	assert.Equal(t, 0, fixture.provider.spawnCount(),
		"a pool's runners arrive by themselves; there is nothing to spawn")
}

func TestAutoSpawnDisabledDoesNothing(t *testing.T) {
	fixture := newAutoSpawnFixture(t, AutoSpawnPolicy{Enabled: false})

	_, err := fixture.manager.allocateRunner(context.Background(), fixture.session)
	require.ErrorIs(t, err, ErrNoRunnerAvailable)
	require.NotErrorIs(t, err, ErrRunnerSpawning)
	assert.Equal(t, 0, fixture.provider.spawnCount())
}

// A read failure is not a licence to spawn: a missed spawn leaves a task
// pending, a spurious one bills until the reaper finds it.
func TestAutoSpawnDoesNotSpawnWhenItCannotSeeTheFleet(t *testing.T) {
	fixture := newAutoSpawnFixture(t, AutoSpawnPolicy{Enabled: true})
	fixture.store.mu.Lock()
	fixture.store.labelListErr = errors.New("database is down")
	fixture.store.mu.Unlock()

	_, err := fixture.manager.allocateRunner(context.Background(), fixture.session)
	require.ErrorIs(t, err, ErrNoRunnerAvailable)
	assert.Equal(t, 0, fixture.provider.spawnCount())
}
