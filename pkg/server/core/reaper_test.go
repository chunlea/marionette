package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// reaperTestStore adds runner and provider-config knowledge to the shared
// in-memory test store.
type reaperTestStore struct {
	*testStore

	runners        map[string]*store.Runner
	providerConfig *store.ProviderConfig
	listRunnersErr error
	listSessionErr error
}

func newReaperTestStore() *reaperTestStore {
	return &reaperTestStore{
		testStore: newTestStore(),
		runners:   make(map[string]*store.Runner),
		providerConfig: &store.ProviderConfig{
			ID:       "pcfg_1",
			Name:     "docker-default",
			Provider: "docker",
		},
	}
}

func (s *reaperTestStore) ListRunners(_ context.Context, _ store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	if s.listRunnersErr != nil {
		return nil, s.listRunnersErr
	}
	items := make([]*store.Runner, 0, len(s.runners))
	for _, r := range s.runners {
		cp := *r
		items = append(items, &cp)
	}
	return &store.ListResult[store.Runner]{Items: items}, nil
}

func (s *reaperTestStore) GetRunner(_ context.Context, id string) (*store.Runner, error) {
	r, ok := s.runners[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *r
	return &cp, nil
}

func (s *reaperTestStore) UpdateRunner(_ context.Context, id string, updates store.RunnerUpdates) error {
	r, ok := s.runners[id]
	if !ok {
		return store.ErrNotFound
	}
	if updates.Status != nil {
		r.Status = *updates.Status
	}
	return nil
}

func (s *reaperTestStore) GetProviderConfig(_ context.Context, _ string) (*store.ProviderConfig, error) {
	if s.providerConfig == nil {
		return nil, store.ErrNotFound
	}
	return s.providerConfig, nil
}

func (s *reaperTestStore) ListSessions(ctx context.Context, opts store.ListSessionsOptions) (*store.ListResult[store.Session], error) {
	if s.listSessionErr != nil {
		return nil, s.listSessionErr
	}
	all, err := s.testStore.ListSessions(ctx, opts)
	if err != nil {
		return nil, err
	}

	items := make([]*store.Session, 0, len(all.Items))
	for _, sess := range all.Items {
		if len(opts.Status) > 0 && !containsString(opts.Status, sess.Status) {
			continue
		}
		if opts.RunnerID != nil {
			if sess.RunnerID == nil || *sess.RunnerID != *opts.RunnerID {
				continue
			}
		}
		items = append(items, sess)
	}
	return &store.ListResult[store.Session]{Items: items}, nil
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// fakeProvider is a managed provider that records what was destroyed.
type fakeProvider struct {
	name       string
	kind       provider.ProviderType
	destroyed  []string
	destroyErr error
}

func (p *fakeProvider) Name() string                { return p.name }
func (p *fakeProvider) Type() provider.ProviderType { return p.kind }
func (p *fakeProvider) Spawn(context.Context, provider.SpawnOptions) (*provider.RunnerInstance, error) {
	return nil, errors.New("not implemented")
}

func (p *fakeProvider) Destroy(_ context.Context, runnerID string) error {
	if p.destroyErr != nil {
		return p.destroyErr
	}
	p.destroyed = append(p.destroyed, runnerID)
	return nil
}

func (p *fakeProvider) Status(context.Context, string) (*provider.RunnerStatus, error) {
	return nil, errors.New("not implemented")
}
func (p *fakeProvider) List(context.Context) ([]*provider.RunnerInstance, error) { return nil, nil }
func (p *fakeProvider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{}
}

// fakeProviderRegistry serves one provider for every lookup.
type fakeProviderRegistry struct {
	prov provider.Provider
	err  error
}

func (r *fakeProviderRegistry) GetDefault(context.Context) (provider.Provider, error) {
	return r.prov, r.err
}

func (r *fakeProviderRegistry) Get(context.Context, string) (provider.Provider, error) {
	return r.prov, r.err
}

func newReaperFixture(t *testing.T) (*Reaper, *reaperTestStore, *fakeProvider) {
	t.Helper()
	s := newReaperTestStore()
	prov := &fakeProvider{name: "docker-default", kind: provider.ProviderTypeManaged}
	r := NewReaper(s, &fakeProviderRegistry{prov: prov}, nil, zap.NewNop(), WithReapMinAge(0))
	return r, s, prov
}

func managedRunner(id string) *store.Runner {
	cfgID := "pcfg_1"
	return &store.Runner{
		ID:               id,
		Name:             id,
		Status:           StatusIdle,
		ProviderConfigID: &cfgID,
		CreatedAt:        time.Now().Add(-time.Hour),
	}
}

// TestReaper_DestroysOrphanedRunner is the whole point of the reaper:
// Provider.Destroy previously had no caller anywhere, so a terminated session's
// container kept running and billing forever.
func TestReaper_DestroysOrphanedRunner(t *testing.T) {
	r, s, prov := newReaperFixture(t)
	s.runners["run_1"] = managedRunner("run_1")
	s.sessions["sess_1"] = &store.Session{
		ID:     "sess_1",
		Status: SessionStatusTerminated,
	}

	require.NoError(t, r.Sweep(context.Background()))

	assert.Equal(t, []string{"run_1"}, prov.destroyed)
	assert.Equal(t, StatusOffline, s.runners["run_1"].Status)
}

func TestReaper_KeepsRunnerClaimedByLiveSession(t *testing.T) {
	for _, status := range liveSessionStatuses {
		t.Run(status, func(t *testing.T) {
			r, s, prov := newReaperFixture(t)
			s.runners["run_1"] = managedRunner("run_1")
			runnerID := "run_1"
			s.sessions["sess_1"] = &store.Session{
				ID:       "sess_1",
				Status:   status,
				RunnerID: &runnerID,
			}

			require.NoError(t, r.Sweep(context.Background()))
			assert.Empty(t, prov.destroyed)
		})
	}
}

// TestReaper_KeepsResumeCandidate guards the suspend/resume path: a suspended
// session releases runner_id but remembers the instance it wants back.
func TestReaper_KeepsResumeCandidate(t *testing.T) {
	r, s, prov := newReaperFixture(t)
	s.runners["run_1"] = managedRunner("run_1")
	previous := "run_1"
	s.sessions["sess_1"] = &store.Session{
		ID:               "sess_1",
		Status:           SessionStatusSuspended,
		PreviousRunnerID: &previous,
	}

	require.NoError(t, r.Sweep(context.Background()))
	assert.Empty(t, prov.destroyed)
}

func TestReaper_SkipsPoolAndExternalRunners(t *testing.T) {
	t.Run("pool runner", func(t *testing.T) {
		r, s, prov := newReaperFixture(t)
		runner := managedRunner("run_pool")
		poolName := "macos-pool"
		runner.PoolName = &poolName
		s.runners["run_pool"] = runner

		require.NoError(t, r.Sweep(context.Background()))
		assert.Empty(t, prov.destroyed)
	})

	t.Run("external runner", func(t *testing.T) {
		r, s, prov := newReaperFixture(t)
		runner := managedRunner("run_ext")
		runner.ProviderConfigID = nil
		s.runners["run_ext"] = runner

		require.NoError(t, r.Sweep(context.Background()))
		assert.Empty(t, prov.destroyed)
	})

	t.Run("non-managed provider", func(t *testing.T) {
		s := newReaperTestStore()
		prov := &fakeProvider{name: "external", kind: provider.ProviderTypeExternal}
		r := NewReaper(s, &fakeProviderRegistry{prov: prov}, nil, zap.NewNop(), WithReapMinAge(0))
		s.runners["run_1"] = managedRunner("run_1")

		require.NoError(t, r.Sweep(context.Background()))
		assert.Empty(t, prov.destroyed)
	})
}

// TestReaper_RespectsMinAge stops the reaper from racing a session that has
// spawned a runner but not yet attached it.
func TestReaper_RespectsMinAge(t *testing.T) {
	s := newReaperTestStore()
	prov := &fakeProvider{name: "docker-default", kind: provider.ProviderTypeManaged}
	r := NewReaper(s, &fakeProviderRegistry{prov: prov}, nil, zap.NewNop(), WithReapMinAge(10*time.Minute))

	runner := managedRunner("run_new")
	runner.CreatedAt = time.Now()
	s.runners["run_new"] = runner

	require.NoError(t, r.Sweep(context.Background()))
	assert.Empty(t, prov.destroyed)
}

// TestReaper_NeverReapsOnIncompleteInformation: if the claim lookup fails we
// cannot know whether a session wants the runner, and destroying is not
// reversible.
func TestReaper_NeverReapsOnIncompleteInformation(t *testing.T) {
	r, s, prov := newReaperFixture(t)
	s.runners["run_1"] = managedRunner("run_1")
	s.listSessionErr = errors.New("database unavailable")

	require.NoError(t, r.Sweep(context.Background()))
	assert.Empty(t, prov.destroyed)
}

// TestReaper_GivesUpAfterMaxAttempts keeps a permanently broken instance from
// being retried on every sweep forever.
func TestReaper_GivesUpAfterMaxAttempts(t *testing.T) {
	s := newReaperTestStore()
	prov := &fakeProvider{
		name:       "docker-default",
		kind:       provider.ProviderTypeManaged,
		destroyErr: errors.New("docker daemon unreachable"),
	}
	r := NewReaper(s, &fakeProviderRegistry{prov: prov}, nil, zap.NewNop(),
		WithReapMinAge(0), WithReapMaxAttempts(2))
	s.runners["run_1"] = managedRunner("run_1")

	for i := 0; i < 5; i++ {
		require.NoError(t, r.Sweep(context.Background()))
	}

	r.mu.Lock()
	attempts := r.attempts["run_1"]
	r.mu.Unlock()
	assert.Equal(t, 2, attempts, "the reaper must stop retrying after maxAttempts")
}

func TestReaper_StartStop(t *testing.T) {
	r, _, _ := newReaperFixture(t)

	r.Start(context.Background())
	r.Start(context.Background()) // second Start is a no-op
	r.Stop()
	r.Stop() // second Stop is a no-op
}
