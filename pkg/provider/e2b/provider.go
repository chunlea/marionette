package e2b

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

// Compile-time interface checks.
var (
	_ provider.Provider            = (*Provider)(nil)
	_ provider.PausableProvider    = (*Provider)(nil)
	_ provider.SuspendableProvider = (*Provider)(nil)
)

// Provider implements the E2B cloud sandbox provider.
type Provider struct {
	name          string
	config        *Config
	suspendConfig *provider.SuspendConfig
	client        *Client

	// sandboxCache maps runnerID -> sandboxID.
	//
	// It is a cache, not the record: a paused E2B sandbox is absent from GET
	// /sandboxes (verified against the live API), so a process that only has
	// this map loses every paused sandbox when it restarts - and a lost paused
	// sandbox keeps billing. The record is runners.provider_instance_id, which
	// the server passes back in through DestroyOptions/SuspendOptions/
	// ResumeOptions; this map only saves a round trip when it happens to be
	// warm.
	sandboxCache sync.Map
}

// New creates a new E2B provider from a ProviderConfig.
func New(cfg *store.ProviderConfig) (*Provider, error) {
	config, err := ParseConfig(cfg.Config)
	if err != nil {
		return nil, err
	}

	suspendCfg, err := provider.ParseSuspendConfig(cfg.SuspendConfig, defaultSuspendConfig())
	if err != nil {
		return nil, err
	}

	// Get API key from config or environment
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("MARIONETTE_E2B_API_KEY")
	}
	if apiKey == "" {
		return nil, &provider.ErrInvalidConfig{
			Field:  "api_key",
			Reason: "required (set in config or MARIONETTE_E2B_API_KEY env var)",
		}
	}

	client := NewClient(config.APIURL, apiKey)

	return &Provider{
		name:          cfg.Name,
		config:        config,
		suspendConfig: suspendCfg,
		client:        client,
	}, nil
}

// Name returns the provider config name.
func (p *Provider) Name() string {
	return p.name
}

// Type returns the provider mode (managed for E2B).
func (p *Provider) Type() provider.ProviderType {
	return provider.ProviderTypeManaged
}

// Spawn creates a new E2B sandbox.
func (p *Provider) Spawn(ctx context.Context, opts provider.SpawnOptions) (*provider.RunnerInstance, error) {
	// Build metadata from labels and annotations
	metadata := make(map[string]string)
	for k, v := range opts.Labels {
		metadata[p.config.LabelPrefix+"/"+k] = v
	}
	// Add runner ID to metadata for lookup
	metadata[p.config.LabelPrefix+"/runner-id"] = opts.RunnerID
	if opts.TenantID != "" {
		metadata[p.config.LabelPrefix+"/tenant-id"] = opts.TenantID
	}

	// Build environment variables
	envVars := make(map[string]string)
	for k, v := range opts.Environment {
		envVars[k] = v
	}
	// Add marionette-specific environment variables
	if opts.ServerURL != "" {
		envVars["MARIONETTE_SERVER"] = opts.ServerURL
	}
	if opts.RunnerToken != "" {
		envVars["MARIONETTE_RUNNER_TOKEN"] = opts.RunnerToken
	}
	if opts.RunnerID != "" {
		envVars["MARIONETTE_RUNNER_ID"] = opts.RunnerID
	}
	if opts.SandboxMode != "" {
		envVars["MARIONETTE_SANDBOX_MODE"] = opts.SandboxMode
	}

	req := &CreateSandboxRequest{
		TemplateID: p.config.Template,
		Metadata:   metadata,
		Timeout:    p.config.TimeoutSeconds,
		EnvVars:    envVars,
	}

	resp, err := p.client.CreateSandbox(ctx, req)
	if err != nil {
		return nil, &provider.ErrSpawnFailed{
			Cause: err,
		}
	}

	// Cache the runnerID -> sandboxID mapping for pause/unpause operations.
	// E2B paused sandboxes are not in the regular list, so we need this cache.
	p.sandboxCache.Store(opts.RunnerID, resp.SandboxID)

	return &provider.RunnerInstance{
		ID:          opts.RunnerID,
		ProviderID:  resp.SandboxID,
		Name:        opts.Name,
		Status:      provider.InstanceStatusRunning,
		SandboxMode: opts.SandboxMode,
		CreatedAt:   time.Now(),
		Labels:      opts.Labels,
		Annotations: opts.Annotations,
		Metadata: map[string]string{
			"sandbox_id":  resp.SandboxID,
			"template_id": resp.TemplateID,
			"client_id":   resp.ClientID,
		},
	}, nil
}

// Destroy terminates an E2B sandbox.
func (p *Provider) Destroy(ctx context.Context, runnerID string, opts provider.DestroyOptions) error {
	sandboxID, err := p.findSandboxByRunnerID(ctx, runnerID, opts.ProviderInstanceID)
	if err != nil {
		if IsNotFoundError(err) {
			// Sandbox already terminated, clean up cache
			p.sandboxCache.Delete(runnerID)
			return nil
		}
		return &provider.ErrDestroyFailed{
			RunnerID: runnerID,
			Cause:    err,
		}
	}

	if err := p.client.KillSandbox(ctx, sandboxID); err != nil {
		if IsNotFoundError(err) {
			// Sandbox already terminated, clean up cache
			p.sandboxCache.Delete(runnerID)
			return nil
		}
		return &provider.ErrDestroyFailed{
			RunnerID: runnerID,
			Cause:    err,
		}
	}

	// Clean up cache after successful destruction
	p.sandboxCache.Delete(runnerID)

	return nil
}

// Status returns the current status of an E2B sandbox.
func (p *Provider) Status(ctx context.Context, runnerID string) (*provider.RunnerStatus, error) {
	sandboxID, err := p.findSandboxByRunnerID(ctx, runnerID, "")
	if err != nil {
		if IsNotFoundError(err) {
			return nil, &provider.ErrRunnerNotFound{RunnerID: runnerID}
		}
		return nil, err
	}

	sandbox, err := p.client.GetSandbox(ctx, sandboxID)
	if err != nil {
		if IsNotFoundError(err) {
			return nil, &provider.ErrRunnerNotFound{RunnerID: runnerID}
		}
		if IsPausedError(err) {
			return &provider.RunnerStatus{
				Status:    provider.InstanceStatusPaused,
				UpdatedAt: time.Now(),
			}, nil
		}
		return nil, err
	}

	status := provider.InstanceStatusRunning
	if sandbox.EndedAt != nil {
		status = provider.InstanceStatusStopped
	}

	return &provider.RunnerStatus{
		Status:    status,
		UpdatedAt: time.Now(),
	}, nil
}

// List returns all E2B sandboxes managed by this provider.
func (p *Provider) List(ctx context.Context) ([]*provider.RunnerInstance, error) {
	sandboxes, err := p.client.ListSandboxes(ctx)
	if err != nil {
		return nil, err
	}

	var instances []*provider.RunnerInstance
	for _, sandbox := range sandboxes {
		// Check if this sandbox was created by marionette
		runnerID, ok := sandbox.Metadata[p.config.LabelPrefix+"/runner-id"]
		if !ok {
			continue // Not a marionette sandbox
		}

		status := provider.InstanceStatusRunning
		if sandbox.EndedAt != nil {
			status = provider.InstanceStatusStopped
		}

		// Extract labels from metadata
		labels := make(map[string]string)
		for k, v := range sandbox.Metadata {
			if len(k) > len(p.config.LabelPrefix)+1 {
				key := k[len(p.config.LabelPrefix)+1:]
				if key != "runner-id" && key != "tenant-id" {
					labels[key] = v
				}
			}
		}

		instances = append(instances, &provider.RunnerInstance{
			ID:         runnerID,
			ProviderID: sandbox.SandboxID,
			Status:     status,
			CreatedAt:  sandbox.StartedAt,
			Labels:     labels,
			Metadata: map[string]string{
				"sandbox_id":  sandbox.SandboxID,
				"template_id": sandbox.TemplateID,
				"client_id":   sandbox.ClientID,
			},
		})
	}

	return instances, nil
}

// Capabilities returns what this provider supports.
func (p *Provider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{
		Pause:    true, // E2B supports native pause (beta)
		Snapshot: false,
		Suspend: provider.SuspendCapability{
			// Derived from the dispatcher so capabilities cannot claim a
			// strategy the provider does not implement.
			Strategies: p.suspendDispatcher().Strategies(),
			Default:    provider.SuspendStrategyPause,
		},
	}
}

// Pause pauses an E2B sandbox (beta feature).
func (p *Provider) Pause(ctx context.Context, runnerID string) error {
	return p.pause(ctx, runnerID, "")
}

// pause pauses the sandbox, preferring the caller's persisted instance id.
func (p *Provider) pause(ctx context.Context, runnerID, instanceID string) error {
	sandboxID, err := p.findSandboxByRunnerID(ctx, runnerID, instanceID)
	if err != nil {
		return &provider.ErrPauseFailed{
			RunnerID: runnerID,
			Cause:    err,
		}
	}

	if err := p.client.PauseSandbox(ctx, sandboxID); err != nil {
		return &provider.ErrPauseFailed{
			RunnerID: runnerID,
			Cause:    err,
		}
	}

	return nil
}

// Unpause resumes a paused E2B sandbox (beta feature).
func (p *Provider) Unpause(ctx context.Context, runnerID string) error {
	return p.unpause(ctx, runnerID, "")
}

// unpause resumes the sandbox, preferring the caller's persisted instance id.
func (p *Provider) unpause(ctx context.Context, runnerID, instanceID string) error {
	sandboxID, err := p.findSandboxByRunnerID(ctx, runnerID, instanceID)
	if err != nil {
		return &provider.ErrResumeFailed{
			SessionID: runnerID,
			Cause:     err,
		}
	}

	if _, err := p.client.ResumeSandbox(ctx, sandboxID, p.config.TimeoutSeconds); err != nil {
		return &provider.ErrResumeFailed{
			SessionID: runnerID,
			Cause:     err,
		}
	}

	return nil
}

// findSandboxByRunnerID resolves the E2B sandbox id for a runner, in order of
// how much the answer can be trusted:
//
//  1. instanceID, the id the server persisted at spawn. It survives a restart,
//     so it is the only source that can name a paused sandbox on a cold
//     process.
//  2. the in-memory cache, warm only for sandboxes this process spawned or
//     looked up.
//  3. GET /sandboxes, which is blind to paused sandboxes and so can only ever
//     be a last resort.
func (p *Provider) findSandboxByRunnerID(ctx context.Context, runnerID, instanceID string) (string, error) {
	if instanceID != "" {
		// Warm the cache so the rest of this process's calls skip the lookup.
		p.sandboxCache.Store(runnerID, instanceID)
		return instanceID, nil
	}

	if sandboxID, ok := p.sandboxCache.Load(runnerID); ok {
		return sandboxID.(string), nil
	}

	// Last resort: enumerate. A paused sandbox will not be in this list.
	sandboxes, err := p.client.ListSandboxes(ctx)
	if err != nil {
		return "", err
	}

	for _, sandbox := range sandboxes {
		if id, ok := sandbox.Metadata[p.config.LabelPrefix+"/runner-id"]; ok && id == runnerID {
			// Update cache for future lookups
			p.sandboxCache.Store(runnerID, sandbox.SandboxID)
			return sandbox.SandboxID, nil
		}
	}

	return "", &APIError{Code: 404, Message: "sandbox not found for runner: " + runnerID}
}
