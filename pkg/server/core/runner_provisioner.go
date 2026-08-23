package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/auth"
	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

// Provisioner errors.
var (
	// ErrProviderRequired is returned when no provider was named and the
	// registry has no default.
	ErrProviderRequired = errors.New("core: provider is required")

	// ErrProviderNotManaged is returned when the named provider does not spawn
	// runners (pool and external providers).
	ErrProviderNotManaged = errors.New("core: provider does not spawn runners")
)

// runnerProvisionPool is the pool name recorded on provisioned runners and
// their tokens. Runner tokens require a pool, and a managed runner belongs to
// no operator-defined pool, so they all share this one.
const runnerProvisionPool = "managed"

// RunnerProvisioner spawns and destroys runners on a managed provider.
//
// It exists because a provider's SpawnResult was previously thrown away. Three
// things have to be written for a spawned runner to be reachable again, and
// nothing wrote them:
//
//   - the runner row, so the server can name the runner at all;
//   - a runner token bound to that row, so the agent inside the instance can
//     authenticate (a spawn with no token produces an instance that can never
//     connect, and simply burns money until it is reaped);
//   - runners.provider_instance_id, the provider-side handle. It is the only
//     one that survives a server restart, and for E2B it is the only handle
//     that exists at all once a sandbox is paused: a paused sandbox is absent
//     from GET /sandboxes, so a restarted server with an empty cache can
//     neither resume nor kill it.
type RunnerProvisioner struct {
	store     store.Store
	registry  ProviderRegistryInterface
	tokens    *auth.RunnerTokenService
	serverURL string
	logger    *zap.Logger
}

// NewRunnerProvisioner creates a RunnerProvisioner.
//
// serverURL is the gRPC address spawned agents dial back on. An empty value
// still spawns, but the agent has nowhere to connect, so callers should pass
// one.
func NewRunnerProvisioner(
	s store.Store,
	registry ProviderRegistryInterface,
	tokens *auth.RunnerTokenService,
	serverURL string,
	logger *zap.Logger,
) *RunnerProvisioner {
	return &RunnerProvisioner{
		store:     s,
		registry:  registry,
		tokens:    tokens,
		serverURL: serverURL,
		logger:    logger,
	}
}

// ProvisionOptions describes a runner to spawn.
type ProvisionOptions struct {
	// Name is the human-readable runner name. Generated when empty.
	Name string

	// ProviderConfigID names the provider config to spawn on. Either this or
	// ProviderName must be set; this one wins.
	ProviderConfigID string

	// ProviderName names the provider directly. Falls back to the registry
	// default when both are empty.
	ProviderName string

	// ProfileID is the profile recorded on the runner row.
	ProfileID string

	// Labels are recorded on the runner row and passed to the provider.
	Labels map[string]string

	// WorkspaceMount is the host path to mount as /workspace.
	WorkspaceMount string
}

// Spawn creates a runner on a managed provider and returns its row.
//
// The row and the token are written before the provider is called, because the
// spawned agent needs the token to exist by the time it boots. If the spawn
// fails they are rolled back, so a failed spawn does not leave a runner row
// nothing will ever connect to.
func (p *RunnerProvisioner) Spawn(ctx context.Context, opts ProvisionOptions) (*store.Runner, error) {
	prov, provConfigID, err := p.resolveProvider(ctx, opts)
	if err != nil {
		return nil, err
	}
	if prov.Type() != provider.ProviderTypeManaged {
		return nil, fmt.Errorf("%w: %s is %s", ErrProviderNotManaged, prov.Name(), prov.Type())
	}

	runner, token, err := p.prepare(ctx, provConfigID, opts)
	if err != nil {
		return nil, err
	}

	spawnOpts := provider.SpawnOptions{
		RunnerID:       runner.ID,
		Name:           runner.Name,
		ServerURL:      p.serverURL,
		RunnerToken:    token,
		Labels:         opts.Labels,
		SandboxMode:    runner.SandboxMode,
		WorkspaceMount: opts.WorkspaceMount,
	}
	if runner.TenantID != nil {
		spawnOpts.TenantID = *runner.TenantID
	}

	instance, err := prov.Spawn(ctx, spawnOpts)
	if err != nil {
		p.rollback(ctx, runner.ID)
		return nil, err
	}

	p.recordInstance(ctx, runner.ID, instance.ProviderID)
	runner.ProviderInstanceID = &instance.ProviderID

	p.logger.Info("runner spawned",
		zap.String("runner_id", runner.ID),
		zap.String("provider", prov.Name()),
		zap.String("provider_instance_id", instance.ProviderID),
	)

	return runner, nil
}

// PrepareSpawn writes the runner row and token for a runner some other caller
// is about to spawn, and returns spawn options already carrying them.
//
// Session resume spawns through the provider's own Resume, which builds its
// instance from the SpawnOptions the server hands it. Without this the server
// would hand over a runner id no row exists for and an empty token, and the
// instance would come up unable to authenticate.
func (p *RunnerProvisioner) PrepareSpawn(ctx context.Context, opts ProvisionOptions) (*provider.SpawnOptions, error) {
	_, provConfigID, err := p.resolveProvider(ctx, opts)
	if err != nil {
		return nil, err
	}

	runner, token, err := p.prepare(ctx, provConfigID, opts)
	if err != nil {
		return nil, err
	}

	spawnOpts := &provider.SpawnOptions{
		RunnerID:       runner.ID,
		Name:           runner.Name,
		ServerURL:      p.serverURL,
		RunnerToken:    token,
		Labels:         opts.Labels,
		SandboxMode:    runner.SandboxMode,
		WorkspaceMount: opts.WorkspaceMount,
	}
	if runner.TenantID != nil {
		spawnOpts.TenantID = *runner.TenantID
	}
	return spawnOpts, nil
}

// DiscardPrepared removes a runner row and token written by PrepareSpawn that
// were never used.
//
// Resume asks the provider to reuse the existing instance when it can, and
// only spawns when it cannot; the server cannot know which happened until the
// call returns. Preparing up front and discarding the unused half is what
// keeps a resume that reused an instance from leaving a runner row nothing
// will ever connect to, plus a live token for it.
func (p *RunnerProvisioner) DiscardPrepared(ctx context.Context, runnerID string) {
	if p.tokens != nil {
		tokens, err := p.tokens.List(ctx, store.ListRunnerTokensOptions{RunnerID: &runnerID})
		if err != nil {
			p.logger.Warn("failed to list tokens for a discarded runner",
				zap.String("runner_id", runnerID),
				zap.Error(err),
			)
		}
		for _, token := range tokens {
			if err := p.tokens.Revoke(ctx, token.ID, "prepared runner was not spawned"); err != nil {
				p.logger.Warn("failed to revoke token for a discarded runner",
					zap.String("runner_id", runnerID),
					zap.String("token_id", token.ID),
					zap.Error(err),
				)
			}
		}
	}
	p.rollback(ctx, runnerID)
}

// Destroy terminates the runner's instance and marks the row offline.
//
// The row is kept rather than deleted: task runs and audit entries reference
// the runner, and the reaper needs to be able to tell a destroyed runner from
// one that never existed.
func (p *RunnerProvisioner) Destroy(ctx context.Context, runnerID string) error {
	runner, err := p.store.GetRunner(ctx, runnerID)
	if err != nil {
		return err
	}

	if runner.ProviderConfigID != nil && *runner.ProviderConfigID != "" {
		provConfig, err := p.store.GetProviderConfig(ctx, *runner.ProviderConfigID)
		if err != nil {
			return err
		}
		prov, err := p.registry.Get(ctx, provConfig.Name)
		if err != nil {
			return err
		}
		if err := prov.Destroy(ctx, runnerID, provider.DestroyOptions{
			ProviderInstanceID: runnerInstanceID(runner),
		}); err != nil {
			return err
		}
	}

	status := StatusOffline
	if err := p.store.UpdateRunner(ctx, runnerID, store.RunnerUpdates{Status: &status}); err != nil {
		return err
	}

	p.logger.Info("runner destroyed",
		zap.String("runner_id", runnerID),
		zap.String("provider_instance_id", runnerInstanceID(runner)),
	)
	return nil
}

// Get retrieves a runner by ID.
func (p *RunnerProvisioner) Get(ctx context.Context, runnerID string) (*store.Runner, error) {
	return p.store.GetRunner(ctx, runnerID)
}

// List returns runners matching opts.
func (p *RunnerProvisioner) List(ctx context.Context, opts store.ListRunnersOptions) (*store.ListResult[store.Runner], error) {
	return p.store.ListRunners(ctx, opts)
}

// resolveProvider resolves the provider to spawn on and the provider config id
// to record on the runner row.
func (p *RunnerProvisioner) resolveProvider(ctx context.Context, opts ProvisionOptions) (provider.Provider, string, error) {
	if p.registry == nil {
		return nil, "", ErrProviderRequired
	}

	if opts.ProviderConfigID != "" {
		provConfig, err := p.store.GetProviderConfig(ctx, opts.ProviderConfigID)
		if err != nil {
			return nil, "", err
		}
		prov, err := p.registry.Get(ctx, provConfig.Name)
		if err != nil {
			return nil, "", err
		}
		return prov, provConfig.ID, nil
	}

	name := opts.ProviderName
	var (
		prov provider.Provider
		err  error
	)
	if name == "" {
		prov, err = p.registry.GetDefault(ctx)
	} else {
		prov, err = p.registry.Get(ctx, name)
	}
	if err != nil {
		return nil, "", err
	}

	// The config id is what links the runner row back to a provider; look it
	// up by name so a runner spawned via the default provider is still
	// resolvable later.
	provConfigID := ""
	if cfg, cfgErr := p.store.GetProviderConfigByName(ctx, prov.Name()); cfgErr == nil {
		provConfigID = cfg.ID
	}
	return prov, provConfigID, nil
}

// prepare writes the runner row and a token bound to it.
func (p *RunnerProvisioner) prepare(
	ctx context.Context,
	provConfigID string,
	opts ProvisionOptions,
) (*store.Runner, string, error) {
	if p.tokens == nil {
		return nil, "", ErrRunnerTokenSvcRequired
	}

	name := opts.Name
	if name == "" {
		name = "runner-" + time.Now().UTC().Format("20060102150405")
	}

	labels := json.RawMessage("{}")
	if len(opts.Labels) > 0 {
		if encoded, err := json.Marshal(opts.Labels); err == nil {
			labels = encoded
		}
	}

	pool := runnerProvisionPool
	runner := &store.Runner{
		ID:           id.Runner(),
		Name:         name,
		Status:       StatusOffline,
		SandboxMode:  "runner-is-sandbox",
		SandboxTypes: []string{},
		Capabilities: []string{},
		PoolName:     &pool,
		Labels:       labels,
		Annotations:  json.RawMessage("{}"),
	}
	if tenant, ok := store.TenantFromContext(ctx); ok && tenant != "" {
		runner.TenantID = &tenant
	}
	if provConfigID != "" {
		runner.ProviderConfigID = &provConfigID
	}
	if opts.ProfileID != "" {
		runner.ProfileID = &opts.ProfileID
	}

	if err := p.store.CreateRunner(ctx, runner); err != nil {
		return nil, "", err
	}

	record, token, err := p.tokens.Create(ctx, auth.CreateRunnerTokenOptions{
		PoolName: pool,
		TenantID: runner.TenantID,
		Labels:   opts.Labels,
	})
	if err != nil {
		p.rollback(ctx, runner.ID)
		return nil, "", err
	}

	// Binding is not optional here, unlike self-registration: the token exists
	// for exactly this runner, and an unbound one would let any agent that
	// obtained it register as something else.
	if err := p.tokens.BindRunner(ctx, record.ID, runner.ID); err != nil {
		_ = p.tokens.Revoke(ctx, record.ID, "runner provisioning failed")
		p.rollback(ctx, runner.ID)
		return nil, "", err
	}

	return runner, token, nil
}

// recordInstance writes the provider-side instance id onto the runner row.
func (p *RunnerProvisioner) recordInstance(ctx context.Context, runnerID, instanceID string) {
	if instanceID == "" {
		return
	}
	if err := p.store.UpdateRunner(ctx, runnerID, store.RunnerUpdates{
		ProviderInstanceID: &instanceID,
	}); err != nil {
		// Not fatal for the spawn, but it is the failure that orphans
		// instances, so it is logged at error.
		p.logger.Error("spawned runner but failed to record its provider instance id",
			zap.String("runner_id", runnerID),
			zap.String("provider_instance_id", instanceID),
			zap.Error(err),
		)
	}
}

// rollback removes a runner row written for a spawn that did not happen.
func (p *RunnerProvisioner) rollback(ctx context.Context, runnerID string) {
	if err := p.store.DeleteRunner(ctx, runnerID); err != nil {
		p.logger.Warn("failed to remove runner row after a failed spawn",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
	}
}
