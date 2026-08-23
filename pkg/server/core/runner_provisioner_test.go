package core

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

// provisionerStore is a behavioural in-memory store: it reads back what was
// written, which is the whole point of these tests - the bug was a write that
// never happened.
type provisionerStore struct {
	store.Store

	runners map[string]*store.Runner
	tokens  map[string]*store.RunnerToken
	configs map[string]*store.ProviderConfig

	updateRunnerErr error
}

func newProvisionerStore() *provisionerStore {
	return &provisionerStore{
		runners: map[string]*store.Runner{},
		tokens:  map[string]*store.RunnerToken{},
		configs: map[string]*store.ProviderConfig{},
	}
}

func (s *provisionerStore) CreateRunner(_ context.Context, runner *store.Runner) error {
	if runner.ID == "" {
		runner.ID = id.Runner()
	}
	clone := *runner
	s.runners[runner.ID] = &clone
	return nil
}

func (s *provisionerStore) GetRunner(_ context.Context, runnerID string) (*store.Runner, error) {
	runner, ok := s.runners[runnerID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return runner, nil
}

func (s *provisionerStore) UpdateRunner(_ context.Context, runnerID string, updates store.RunnerUpdates) error {
	if s.updateRunnerErr != nil {
		return s.updateRunnerErr
	}
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

func (s *provisionerStore) DeleteRunner(_ context.Context, runnerID string) error {
	delete(s.runners, runnerID)
	return nil
}

func (s *provisionerStore) ListRunners(_ context.Context, _ store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	result := &store.ListResult[store.Runner]{}
	for _, runner := range s.runners {
		result.Items = append(result.Items, runner)
	}
	result.TotalCount = int64(len(result.Items))
	return result, nil
}

func (s *provisionerStore) GetProviderConfig(_ context.Context, configID string) (*store.ProviderConfig, error) {
	cfg, ok := s.configs[configID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return cfg, nil
}

func (s *provisionerStore) GetProviderConfigByName(_ context.Context, name string) (*store.ProviderConfig, error) {
	for _, cfg := range s.configs {
		if cfg.Name == name {
			return cfg, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *provisionerStore) CreateRunnerToken(_ context.Context, token *store.RunnerToken) error {
	clone := *token
	s.tokens[token.ID] = &clone
	return nil
}

func (s *provisionerStore) GetRunnerToken(_ context.Context, tokenID string) (*store.RunnerToken, error) {
	token, ok := s.tokens[tokenID]
	if !ok {
		return nil, store.ErrNotFound
	}
	return token, nil
}

func (s *provisionerStore) UpdateRunnerToken(_ context.Context, tokenID string, updates store.RunnerTokenUpdates) error {
	token, ok := s.tokens[tokenID]
	if !ok {
		return store.ErrNotFound
	}
	if updates.RunnerID != nil {
		token.RunnerID = updates.RunnerID
	}
	if updates.Status != nil {
		token.Status = *updates.Status
	}
	return nil
}

func (s *provisionerStore) ListRunnerTokens(_ context.Context, opts store.ListRunnerTokensOptions) (*store.ListResult[store.RunnerToken], error) {
	result := &store.ListResult[store.RunnerToken]{}
	for _, token := range s.tokens {
		if opts.RunnerID != nil {
			if token.RunnerID == nil || *token.RunnerID != *opts.RunnerID {
				continue
			}
		}
		result.Items = append(result.Items, token)
	}
	return result, nil
}

// spawningProvider records what it was asked to spawn and hands back an
// instance id, the way a real managed provider does.
type spawningProvider struct {
	fakeProvider

	spawned   []provider.SpawnOptions
	spawnErr  error
	instanceI string
}

func newSpawningProvider() *spawningProvider {
	return &spawningProvider{
		fakeProvider: fakeProvider{name: "docker-default", kind: provider.ProviderTypeManaged},
		instanceI:    "container-abc",
	}
}

func (p *spawningProvider) Spawn(_ context.Context, opts provider.SpawnOptions) (*provider.RunnerInstance, error) {
	if p.spawnErr != nil {
		return nil, p.spawnErr
	}
	p.spawned = append(p.spawned, opts)
	return &provider.RunnerInstance{ID: opts.RunnerID, ProviderID: p.instanceI}, nil
}

func newProvisionerFixture(t *testing.T) (*RunnerProvisioner, *provisionerStore, *spawningProvider) {
	t.Helper()
	s := newProvisionerStore()
	s.configs["pcfg_1"] = &store.ProviderConfig{ID: "pcfg_1", Name: "docker-default", Provider: "docker"}
	prov := newSpawningProvider()
	tokens := auth.NewRunnerTokenService(s, id.RunnerToken)
	p := NewRunnerProvisioner(s, &fakeProviderRegistry{prov: prov}, tokens, "grpc.example:9090", zap.NewNop())
	return p, s, prov
}

// TestProvisioner_SpawnRecordsInstanceID is the reason this type exists: the
// provider's instance id used to be logged and dropped, which is what made a
// paused E2B sandbox unreachable after a restart.
func TestProvisioner_SpawnRecordsInstanceID(t *testing.T) {
	p, s, prov := newProvisionerFixture(t)

	runner, err := p.Spawn(context.Background(), ProvisionOptions{
		Name:             "runner-1",
		ProviderConfigID: "pcfg_1",
	})
	require.NoError(t, err)

	require.NotNil(t, runner.ProviderInstanceID)
	assert.Equal(t, prov.instanceI, *runner.ProviderInstanceID)

	stored, err := s.GetRunner(context.Background(), runner.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.ProviderInstanceID, "the instance id must be on the row, not just in the return value")
	assert.Equal(t, prov.instanceI, *stored.ProviderInstanceID)
	require.NotNil(t, stored.ProviderConfigID)
	assert.Equal(t, "pcfg_1", *stored.ProviderConfigID)
}

// TestProvisioner_SpawnIssuesABoundToken: a spawned instance with no token can
// never authenticate, so it comes up, fails to connect, and bills until the
// reaper finds it.
func TestProvisioner_SpawnIssuesABoundToken(t *testing.T) {
	p, s, prov := newProvisionerFixture(t)

	runner, err := p.Spawn(context.Background(), ProvisionOptions{ProviderConfigID: "pcfg_1"})
	require.NoError(t, err)

	require.Len(t, prov.spawned, 1)
	assert.NotEmpty(t, prov.spawned[0].RunnerToken, "the instance must be given a token")
	assert.Equal(t, runner.ID, prov.spawned[0].RunnerID)
	assert.Equal(t, "grpc.example:9090", prov.spawned[0].ServerURL)

	require.Len(t, s.tokens, 1)
	for _, token := range s.tokens {
		require.NotNil(t, token.RunnerID, "the token must be bound to the runner it was minted for")
		assert.Equal(t, runner.ID, *token.RunnerID)
	}
}

// TestProvisioner_FailedSpawnLeavesNoRunnerRow: a row for an instance that does
// not exist is a runner nothing will ever connect to, and the allocator would
// keep offering it.
func TestProvisioner_FailedSpawnLeavesNoRunnerRow(t *testing.T) {
	p, s, prov := newProvisionerFixture(t)
	prov.spawnErr = errors.New("provider is down")

	_, err := p.Spawn(context.Background(), ProvisionOptions{ProviderConfigID: "pcfg_1"})
	require.Error(t, err)

	assert.Empty(t, s.runners, "a failed spawn must not leave a runner row behind")
}

// TestProvisioner_RefusesNonManagedProvider: pool runners join by token, they
// are not spawned. Trying anyway would write a row and a token for an instance
// nobody creates.
func TestProvisioner_RefusesNonManagedProvider(t *testing.T) {
	p, s, prov := newProvisionerFixture(t)
	prov.kind = provider.ProviderTypePool

	_, err := p.Spawn(context.Background(), ProvisionOptions{ProviderConfigID: "pcfg_1"})
	require.ErrorIs(t, err, ErrProviderNotManaged)
	assert.Empty(t, s.runners)
}

// TestProvisioner_DestroyPassesThePersistedID is the read side of the fix.
func TestProvisioner_DestroyPassesThePersistedID(t *testing.T) {
	p, s, prov := newProvisionerFixture(t)

	runner, err := p.Spawn(context.Background(), ProvisionOptions{ProviderConfigID: "pcfg_1"})
	require.NoError(t, err)

	require.NoError(t, p.Destroy(context.Background(), runner.ID))

	assert.Equal(t, []string{runner.ID}, prov.destroyed)
	assert.Equal(t, []string{prov.instanceI}, prov.destroyedInstances,
		"destroy must address the instance by the id that was persisted")

	stored, err := s.GetRunner(context.Background(), runner.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusOffline, stored.Status)
}

// TestProvisioner_PrepareSpawnDiscard covers the resume path's bookkeeping: the
// server prepares a runner in case the provider has to spawn, and has to put it
// back when the provider reused the existing instance instead.
func TestProvisioner_PrepareSpawnDiscard(t *testing.T) {
	p, s, _ := newProvisionerFixture(t)

	spawnOpts, err := p.PrepareSpawn(context.Background(), ProvisionOptions{ProviderConfigID: "pcfg_1"})
	require.NoError(t, err)
	require.NotEmpty(t, spawnOpts.RunnerID)
	require.NotEmpty(t, spawnOpts.RunnerToken)
	require.Len(t, s.runners, 1)

	p.DiscardPrepared(context.Background(), spawnOpts.RunnerID)

	assert.Empty(t, s.runners, "an unused prepared runner must not linger")
	for _, token := range s.tokens {
		assert.Equal(t, auth.TokenStatusRevoked, token.Status,
			"and its token must not stay usable")
	}
}

// TestProvisioner_SpawnSucceedsWhenRecordingTheIDFails: losing the id is bad,
// but the instance is up and killing it because a write failed would be worse.
// The row simply carries no id, which is the pre-fix behaviour for that runner.
func TestProvisioner_SpawnSucceedsWhenRecordingTheIDFails(t *testing.T) {
	p, s, _ := newProvisionerFixture(t)

	runner, err := p.Spawn(context.Background(), ProvisionOptions{ProviderConfigID: "pcfg_1"})
	require.NoError(t, err)
	s.updateRunnerErr = errors.New("database is down")

	second, err := p.Spawn(context.Background(), ProvisionOptions{ProviderConfigID: "pcfg_1"})
	require.NoError(t, err)
	require.NotEqual(t, runner.ID, second.ID)

	stored, err := s.GetRunner(context.Background(), second.ID)
	require.NoError(t, err)
	assert.Nil(t, stored.ProviderInstanceID)
}
