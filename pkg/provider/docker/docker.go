// Package docker implements a Docker container provider for Marionette.
// It manages runner lifecycle using the Docker API, supporting container
// creation, destruction, pause/unpause, and status monitoring.
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"go.uber.org/zap"

	network2 "github.com/chunlea/marionette/pkg/network"
	"github.com/chunlea/marionette/pkg/provider"
	"github.com/chunlea/marionette/pkg/store"
)

const (
	// Label keys used for container identification.
	labelManagedBy = "managed-by"
	labelRunnerID  = "runner-id"
	labelSessionID = "session-id"
	labelTenantID  = "tenant-id"

	// Default stop timeout in seconds.
	defaultStopTimeout = 30

	// noNetworkMode is Docker's built-in network that provides a namespace
	// with nothing but loopback in it.
	noNetworkMode = "none"
)

// Provider implements the Docker container provider.
type Provider struct {
	name          string
	config        *Config
	suspendConfig *provider.SuspendConfig
	client        DockerClient

	// networkOnce ensures network is only created once.
	networkOnce sync.Once
	networkErr  error

	// networkIsolation handles network policy enforcement.
	networkIsolation *NetworkIsolation

	logger *zap.Logger
}

// SetLogger attaches a logger to the provider and its network isolation.
// Without it both stay silent, which is the right default for tests but hides
// policy decisions in production.
func (p *Provider) SetLogger(logger *zap.Logger) {
	if logger == nil {
		return
	}
	p.logger = logger
	p.networkIsolation.SetLogger(logger.Named("network"))
}

// Compile-time interface checks.
var (
	_ provider.Provider         = (*Provider)(nil)
	_ provider.PausableProvider = (*Provider)(nil)
)

// New creates a Docker provider from a store.ProviderConfig.
func New(cfg *store.ProviderConfig) (*Provider, error) {
	dockerCfg, err := ParseConfig(cfg.Config)
	if err != nil {
		return nil, err
	}

	suspendCfg, err := provider.ParseSuspendConfig(cfg.SuspendConfig, defaultSuspendConfig())
	if err != nil {
		return nil, err
	}

	client, err := NewDockerClient(dockerCfg)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	return &Provider{
		name:             cfg.Name,
		config:           dockerCfg,
		suspendConfig:    suspendCfg,
		client:           client,
		networkIsolation: NewNetworkIsolation(client, WithProcRoot(dockerCfg.Isolation.ProcRoot)),
		logger:           zap.NewNop(),
	}, nil
}

// NewWithClient creates a provider with an injected client (for testing).
func NewWithClient(name string, cfg *Config, suspendCfg *provider.SuspendConfig, client DockerClient) *Provider {
	if suspendCfg == nil {
		suspendCfg = &provider.SuspendConfig{}
		suspendCfg.ApplyDefaults(defaultSuspendConfig())
	}
	return &Provider{
		name:             name,
		config:           cfg,
		suspendConfig:    suspendCfg,
		client:           client,
		networkIsolation: NewNetworkIsolation(client, WithProcRoot(cfg.Isolation.ProcRoot)),
		logger:           zap.NewNop(),
	}
}

// NewWithClientAndNetworkIsolation creates a provider with injected client and network isolation (for testing).
func NewWithClientAndNetworkIsolation(name string, cfg *Config, suspendCfg *provider.SuspendConfig, client DockerClient, ni *NetworkIsolation) *Provider {
	if suspendCfg == nil {
		suspendCfg = &provider.SuspendConfig{}
		suspendCfg.ApplyDefaults(defaultSuspendConfig())
	}
	return &Provider{
		name:             name,
		config:           cfg,
		suspendConfig:    suspendCfg,
		client:           client,
		networkIsolation: ni,
		logger:           zap.NewNop(),
	}
}

// Name returns the provider config name.
func (p *Provider) Name() string {
	return p.name
}

// Type returns the provider type (managed).
func (p *Provider) Type() provider.ProviderType {
	return provider.ProviderTypeManaged
}

// Capabilities returns the provider's capabilities.
func (p *Provider) Capabilities() provider.ProviderCapabilities {
	return provider.ProviderCapabilities{
		Pause:    true,
		Snapshot: false, // Docker CRIU not enabled by default
		Suspend: provider.SuspendCapability{
			// Derived from the dispatcher so capabilities cannot claim a
			// strategy the provider does not implement.
			Strategies: p.suspendDispatcher().Strategies(),
			Default:    provider.SuspendStrategyPause,
		},
	}
}

// Spawn creates and starts a new container.
//
// A runner under a restricted network policy is created with no network at
// all, started, fitted with its firewall rules while its namespace holds
// nothing but loopback, and only then connected to a network. The container
// therefore never has an interface it could send a packet through before the
// rules exist. Applying the policy after connecting, which is what this used
// to do, leaves a window of unrestricted egress between start and enforcement.
func (p *Provider) Spawn(ctx context.Context, opts provider.SpawnOptions) (*provider.RunnerInstance, error) {
	// Ensure network exists (auto-create if configured).
	if err := p.ensureNetwork(ctx); err != nil {
		return nil, &provider.ErrSpawnFailed{Reason: "network setup failed", Cause: err}
	}

	// Parse the policy before creating anything: a malformed policy must fail
	// the spawn, not leave a running container with nothing enforced on it.
	policy, err := p.networkIsolation.Prepare(opts, p.config)
	if err != nil {
		return nil, &provider.ErrSpawnFailed{Reason: "invalid network policy", Cause: err}
	}

	// Resolve on the host, before the container exists: /etc/hosts entries and
	// the proxy environment are derived from the pinned addresses.
	var resolved *network2.ResolvedPolicy
	if policy.IsRestricted() {
		resolved, err = p.networkIsolation.Resolve(ctx, policy)
		if err != nil {
			return nil, &provider.ErrSpawnFailed{Reason: "network policy resolution failed", Cause: err}
		}
	}

	// Build container name.
	containerName := p.containerName(opts.Name, opts.RunnerID)

	// Build container configuration.
	containerConfig := p.buildContainerConfig(opts, resolved)
	hostConfig := p.buildHostConfig(opts, resolved)
	networkConfig := p.buildNetworkConfig(resolved)

	// Create container.
	resp, err := p.client.ContainerCreate(ctx, containerConfig, hostConfig, networkConfig, nil, containerName)
	if err != nil {
		return nil, &provider.ErrSpawnFailed{Reason: "container create failed", Cause: err}
	}

	// Start container.
	if err := p.client.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Cleanup on failure.
		_ = p.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return nil, &provider.ErrSpawnFailed{Reason: "container start failed", Cause: err}
	}

	if resolved != nil {
		if err := p.applyIsolation(ctx, opts, resp.ID, policy, resolved); err != nil {
			// The container is running but unprotected, and it is still
			// detached from every network. Destroy it rather than connect it.
			_ = p.client.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
			return nil, &provider.ErrSpawnFailed{Reason: "network policy failed", Cause: err}
		}
	}

	return &provider.RunnerInstance{
		ID:          opts.RunnerID,
		ProviderID:  resp.ID,
		Name:        opts.Name,
		Status:      provider.InstanceStatusRunning,
		SandboxMode: opts.SandboxMode,
		CreatedAt:   time.Now(),
		Labels:      opts.Labels,
		Annotations: opts.Annotations,
		Metadata: map[string]string{
			"container_id":   resp.ID,
			"container_name": containerName,
			"image":          p.config.Image,
		},
	}, nil
}

// applyIsolation installs the firewall and only then gives the container an
// interface.
//
// Order is the whole point. Between ContainerStart and NetworkConnect the
// container's namespace has no non-loopback interface, so there is no path a
// packet could take; the rules land in that gap.
func (p *Provider) applyIsolation(ctx context.Context, opts provider.SpawnOptions, containerID string, policy *network2.NetworkPolicy, resolved *network2.ResolvedPolicy) error {
	key := runnerChainKey(opts.RunnerID, containerID)

	if err := p.networkIsolation.Install(ctx, key, containerID, resolved); err != nil {
		return err
	}

	// Docker refuses to attach a network to a container that is still in
	// "none" mode ("cannot be connected to multiple networks with one of the
	// networks in private (none) mode"), so the placeholder has to be dropped
	// first. Neither call creates an interface on its own: the namespace stays
	// loopback-only until the connect below returns.
	if err := p.client.NetworkDisconnect(ctx, noNetworkMode, containerID, false); err != nil {
		_ = p.networkIsolation.Cleanup(ctx, key)
		return fmt.Errorf("detaching container from the none network: %w", err)
	}

	if err := p.client.NetworkConnect(ctx, p.attachNetwork(), containerID, nil); err != nil {
		_ = p.networkIsolation.Cleanup(ctx, key)
		return fmt.Errorf("connecting container to network: %w", err)
	}

	// The refresh loop outlives this request: it must keep the pinned
	// addresses current for as long as the runner exists.
	if err := p.networkIsolation.StartRefresh(context.WithoutCancel(ctx), key, policy, resolved); err != nil {
		_ = p.networkIsolation.Cleanup(ctx, key)
		return err
	}

	return nil
}

// attachNetwork returns the network a restricted container is connected to
// after its rules are in place.
func (p *Provider) attachNetwork() string {
	if p.config.Network != "" {
		return p.config.Network
	}
	// Matching Docker's own default, so a provider with no configured network
	// still ends up with the connectivity an unrestricted runner would have.
	return "bridge"
}

// Destroy stops and removes a container.
func (p *Provider) Destroy(ctx context.Context, runnerID string) error {
	containerID, err := p.findContainerByRunnerID(ctx, runnerID)
	if err != nil {
		return err
	}

	// Stop the refresher and drop the rules. Errors are not fatal: the rules
	// live in the container's namespace and die with it.
	if err := p.networkIsolation.Cleanup(ctx, runnerChainKey(runnerID, containerID)); err != nil {
		p.logger.Warn("network policy cleanup failed",
			zap.String("runner_id", runnerID),
			zap.Error(err),
		)
	}

	// Stop with timeout.
	timeout := defaultStopTimeout
	if err := p.client.ContainerStop(ctx, containerID, container.StopOptions{Timeout: &timeout}); err != nil {
		// Ignore "not running" errors.
		if !isNotRunningError(err) {
			return &provider.ErrDestroyFailed{RunnerID: runnerID, Cause: err}
		}
	}

	// Remove container (keep volumes for workspace persistence).
	if err := p.client.ContainerRemove(ctx, containerID, container.RemoveOptions{
		RemoveVolumes: false,
		Force:         true,
	}); err != nil {
		return &provider.ErrDestroyFailed{RunnerID: runnerID, Cause: err}
	}

	return nil
}

// Status returns the current status of a runner.
func (p *Provider) Status(ctx context.Context, runnerID string) (*provider.RunnerStatus, error) {
	containerID, err := p.findContainerByRunnerID(ctx, runnerID)
	if err != nil {
		return nil, err
	}

	info, err := p.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return nil, fmt.Errorf("inspecting container: %w", err)
	}

	return &provider.RunnerStatus{
		Status:    mapContainerState(info.State),
		UpdatedAt: time.Now(),
	}, nil
}

// List returns all runners managed by this provider.
func (p *Provider) List(ctx context.Context) ([]*provider.RunnerInstance, error) {
	containers, err := p.client.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", p.labelKey(labelManagedBy)+"=marionette"),
		),
	})
	if err != nil {
		return nil, fmt.Errorf("listing containers: %w", err)
	}

	instances := make([]*provider.RunnerInstance, 0, len(containers))
	for _, c := range containers {
		instances = append(instances, p.containerToInstance(c))
	}

	return instances, nil
}

// Pause suspends a running container, preserving memory state.
func (p *Provider) Pause(ctx context.Context, runnerID string) error {
	containerID, err := p.findContainerByRunnerID(ctx, runnerID)
	if err != nil {
		return err
	}

	if err := p.client.ContainerPause(ctx, containerID); err != nil {
		return &provider.ErrPauseFailed{RunnerID: runnerID, Cause: err}
	}

	return nil
}

// Unpause resumes a paused container.
func (p *Provider) Unpause(ctx context.Context, runnerID string) error {
	containerID, err := p.findContainerByRunnerID(ctx, runnerID)
	if err != nil {
		return err
	}

	if err := p.client.ContainerUnpause(ctx, containerID); err != nil {
		return &provider.ErrUnpauseFailed{RunnerID: runnerID, Cause: err}
	}

	return nil
}

// EnsureNetwork creates the configured network if it doesn't exist.
func (p *Provider) ensureNetwork(ctx context.Context) error {
	if p.config.Network == "" {
		return nil
	}

	p.networkOnce.Do(func() {
		p.networkErr = p.doEnsureNetwork(ctx)
	})

	return p.networkErr
}

func (p *Provider) doEnsureNetwork(ctx context.Context) error {
	// Check if network exists.
	networks, err := p.client.NetworkList(ctx, network.ListOptions{
		Filters: filters.NewArgs(filters.Arg("name", p.config.Network)),
	})
	if err != nil {
		return fmt.Errorf("listing networks: %w", err)
	}

	// Network already exists.
	for _, n := range networks {
		if n.Name == p.config.Network {
			return nil
		}
	}

	// Create network.
	_, err = p.client.NetworkCreate(ctx, p.config.Network, network.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			p.labelKey(labelManagedBy): "marionette",
		},
	})
	if err != nil {
		return fmt.Errorf("creating network %s: %w", p.config.Network, err)
	}

	return nil
}

// Helper methods

func (p *Provider) containerName(name, runnerID string) string {
	if name != "" {
		return fmt.Sprintf("marionette-%s", name)
	}
	return fmt.Sprintf("marionette-%s", runnerID)
}

func (p *Provider) labelKey(key string) string {
	return fmt.Sprintf("%s/%s", p.config.LabelPrefix, key)
}

func (p *Provider) buildContainerConfig(opts provider.SpawnOptions, resolved *network2.ResolvedPolicy) *container.Config {
	env := p.buildEnv(opts, resolved)
	labels := p.buildLabels(opts)

	cfg := &container.Config{
		Image:  p.config.Image,
		Env:    env,
		Labels: labels,
	}

	// Add command if specified in config.
	if len(p.config.Cmd) > 0 {
		cfg.Cmd = p.config.Cmd
	}

	return cfg
}

func (p *Provider) buildHostConfig(opts provider.SpawnOptions, resolved *network2.ResolvedPolicy) *container.HostConfig {
	cfg := &container.HostConfig{
		Resources: p.buildResources(opts),
		Mounts:    p.buildMounts(opts),
	}

	if resolved == nil {
		return cfg
	}

	// No network at creation time. The container starts with a namespace that
	// contains only loopback, which is what makes "rules before packets"
	// achievable rather than a race we hope to win.
	cfg.NetworkMode = noNetworkMode

	// Never let Docker restart this container behind our back: a restart
	// re-creates the namespace with an interface already attached, and the
	// rules would only come back at the next refresh tick.
	cfg.RestartPolicy = container.RestartPolicy{Name: container.RestartPolicyDisabled}

	cfg.ExtraHosts = controlPlaneHostEntries(resolved)

	return cfg
}

// controlPlaneHostEntries pins the server's name to its address in the
// container's /etc/hosts.
//
// Air-gapped runners have no DNS at all, so this is the only way the agent can
// turn a hostname into an address. It also removes a lookup from the path for
// every other level: the name cannot be re-pointed mid-session.
func controlPlaneHostEntries(resolved *network2.ResolvedPolicy) []string {
	var entries []string
	for _, er := range resolved.ControlPlane {
		if er.Endpoint.IsIP() || len(er.IPs) == 0 {
			continue
		}
		entries = append(entries, fmt.Sprintf("%s:%s", er.Endpoint.Host, er.IPs[0].String()))
	}
	return entries
}

func (p *Provider) buildNetworkConfig(resolved *network2.ResolvedPolicy) *network.NetworkingConfig {
	// A restricted container is attached only after its rules exist.
	if resolved != nil {
		return nil
	}

	if p.config.Network == "" {
		return nil
	}

	return &network.NetworkingConfig{
		EndpointsConfig: map[string]*network.EndpointSettings{
			p.config.Network: {},
		},
	}
}

func (p *Provider) buildEnv(opts provider.SpawnOptions, resolved *network2.ResolvedPolicy) []string {
	env := []string{
		fmt.Sprintf("MARIONETTE_SERVER=%s", opts.ServerURL),
		fmt.Sprintf("MARIONETTE_RUNNER_TOKEN=%s", opts.RunnerToken),
	}

	if opts.SandboxMode != "" {
		env = append(env, fmt.Sprintf("MARIONETTE_SANDBOX_MODE=%s", opts.SandboxMode))
	}

	// Proxy mode is enforced by the firewall, but the tools have to be told
	// where the proxy is or every one of them just fails.
	for k, v := range proxyEnv(resolved) {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	// Session environment last so an operator override still wins.
	for k, v := range opts.Environment {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}

	return env
}

// proxyEnv renders the proxy environment for a resolved policy, in a stable
// order so two spawns of the same session produce the same container config.
func proxyEnv(resolved *network2.ResolvedPolicy) map[string]string {
	if resolved == nil || resolved.OriginalPolicy == nil || resolved.OriginalPolicy.Proxy == nil {
		return nil
	}

	noProxy := make([]string, 0, len(resolved.ControlPlane))
	for _, er := range resolved.ControlPlane {
		noProxy = append(noProxy, er.Endpoint.Host)
	}

	return resolved.OriginalPolicy.Proxy.Env(noProxy...)
}

func (p *Provider) buildLabels(opts provider.SpawnOptions) map[string]string {
	labels := map[string]string{
		p.labelKey(labelManagedBy): "marionette",
		p.labelKey(labelRunnerID):  opts.RunnerID,
	}

	if opts.TenantID != "" {
		labels[p.labelKey(labelTenantID)] = opts.TenantID
	}

	for k, v := range opts.Labels {
		labels[k] = v
	}

	return labels
}

func (p *Provider) buildResources(opts provider.SpawnOptions) container.Resources {
	var resources container.Resources

	// Memory
	if opts.MemoryMB > 0 {
		resources.Memory = int64(opts.MemoryMB) * 1024 * 1024
	} else if mem, err := ParseMemory(p.config.Resources.Memory); err == nil {
		resources.Memory = mem
	}

	// CPUs
	if opts.CPUs > 0 {
		resources.NanoCPUs = int64(opts.CPUs * 1e9)
	} else if cpus, err := ParseCPUs(p.config.Resources.CPUs); err == nil {
		resources.NanoCPUs = int64(cpus * 1e9)
	}

	return resources
}

func (p *Provider) buildMounts(opts provider.SpawnOptions) []mount.Mount {
	var mounts []mount.Mount

	if opts.WorkspaceMount != "" {
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeBind,
			Source: opts.WorkspaceMount,
			Target: "/workspace",
		})
	}

	return mounts
}

func (p *Provider) findContainerByRunnerID(ctx context.Context, runnerID string) (string, error) {
	containers, err := p.client.ContainerList(ctx, container.ListOptions{
		All: true,
		Filters: filters.NewArgs(
			filters.Arg("label", p.labelKey(labelRunnerID)+"="+runnerID),
		),
	})
	if err != nil {
		return "", fmt.Errorf("listing containers: %w", err)
	}

	if len(containers) == 0 {
		return "", &provider.ErrRunnerNotFound{RunnerID: runnerID}
	}

	return containers[0].ID, nil
}

func (p *Provider) containerToInstance(c types.Container) *provider.RunnerInstance {
	runnerID := c.Labels[p.labelKey(labelRunnerID)]

	// Extract name (remove leading slash).
	name := ""
	if len(c.Names) > 0 {
		name = strings.TrimPrefix(c.Names[0], "/")
	}

	return &provider.RunnerInstance{
		ID:         runnerID,
		ProviderID: c.ID,
		Name:       name,
		Status:     mapContainerStateString(c.State),
		CreatedAt:  time.Unix(c.Created, 0),
		Labels:     c.Labels,
		Metadata: map[string]string{
			"container_id": c.ID,
			"image":        c.Image,
		},
	}
}

// mapContainerState maps Docker container state to InstanceStatus.
func mapContainerState(state *container.State) provider.InstanceStatus {
	if state == nil {
		return provider.InstanceStatusFailed
	}

	// Check Paused before Running - when paused, both Paused AND Running are true
	switch {
	case state.Paused:
		return provider.InstanceStatusPaused
	case state.Running:
		return provider.InstanceStatusRunning
	case state.Dead, state.OOMKilled:
		return provider.InstanceStatusFailed
	case state.Status == "created":
		return provider.InstanceStatusPending
	default:
		return provider.InstanceStatusStopped
	}
}

// mapContainerStateString maps Docker container state string to InstanceStatus.
func mapContainerStateString(state string) provider.InstanceStatus {
	switch state {
	case "running":
		return provider.InstanceStatusRunning
	case "paused":
		return provider.InstanceStatusPaused
	case "created":
		return provider.InstanceStatusPending
	case "exited", "dead":
		return provider.InstanceStatusStopped
	default:
		return provider.InstanceStatusFailed
	}
}

// isNotRunningError checks if the error is because the container is not running.
func isNotRunningError(err error) bool {
	if err == nil {
		return false
	}
	// Docker returns specific error messages for stopped containers.
	errStr := err.Error()
	return strings.Contains(errStr, "is not running") ||
		strings.Contains(errStr, "No such container")
}

// NewFromJSON creates a Docker provider from raw JSON configuration.
func NewFromJSON(name string, configJSON, suspendConfigJSON json.RawMessage) (*Provider, error) {
	cfg, err := ParseConfig(configJSON)
	if err != nil {
		return nil, err
	}

	suspendCfg, err := provider.ParseSuspendConfig(suspendConfigJSON, defaultSuspendConfig())
	if err != nil {
		return nil, err
	}

	client, err := NewDockerClient(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating docker client: %w", err)
	}

	return &Provider{
		name:             name,
		config:           cfg,
		suspendConfig:    suspendCfg,
		client:           client,
		networkIsolation: NewNetworkIsolation(client, WithProcRoot(cfg.Isolation.ProcRoot)),
		logger:           zap.NewNop(),
	}, nil
}

// Close stops the policy refreshers and closes the Docker client connection.
func (p *Provider) Close() error {
	p.networkIsolation.StopAll()
	return p.client.Close()
}

// SuspendConfig returns the provider's suspend configuration.
func (p *Provider) SuspendConfig() provider.SuspendConfig {
	return *p.suspendConfig
}
