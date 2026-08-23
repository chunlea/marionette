package docker

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"

	mnet "github.com/chunlea/marionette/pkg/network"
	"github.com/chunlea/marionette/pkg/network/iptables"
	"github.com/chunlea/marionette/pkg/provider"
)

// NetworkIsolation enforces a session's network policy on a container.
//
// The enforcement point is iptables inside the container's own network
// namespace, driven from the host with nsenter. Rules are installed while the
// namespace still has nothing but loopback in it, and only then is an
// interface attached: see Provider.Spawn. A policy applied after the container
// is on the network leaves a window of unrestricted egress hundreds of
// milliseconds wide, which is long enough for a process that starts by phoning
// home.
type NetworkIsolation struct {
	resolver *mnet.DNSResolver
	client   DockerClient
	logger   *zap.Logger

	// refreshInterval overrides the DNS refresh cadence when non-zero.
	refreshInterval time.Duration

	// newExecutor builds the iptables executor for a container's namespace.
	// Tests replace it with a recording executor.
	newExecutor func(resolveNS func(context.Context) (string, error)) iptables.Executor

	mu     sync.Mutex
	guards map[string]*runnerGuard
}

// runnerGuard is the live enforcement state for one runner.
type runnerGuard struct {
	manager   *iptables.Manager
	refresher *mnet.Refresher
	resolved  *mnet.ResolvedPolicy
}

// NetworkIsolationOption configures a NetworkIsolation.
type NetworkIsolationOption func(*NetworkIsolation)

// WithNetworkLogger sets the logger.
func WithNetworkLogger(l *zap.Logger) NetworkIsolationOption {
	return func(n *NetworkIsolation) {
		if l != nil {
			n.logger = l
		}
	}
}

// WithNetworkRefreshInterval overrides the DNS refresh cadence.
func WithNetworkRefreshInterval(d time.Duration) NetworkIsolationOption {
	return func(n *NetworkIsolation) {
		n.refreshInterval = d
	}
}

// WithIPTablesExecutor forces a specific iptables executor instead of entering
// the container's namespace. For tests only.
func WithIPTablesExecutor(executor iptables.Executor) NetworkIsolationOption {
	return func(n *NetworkIsolation) {
		n.newExecutor = func(func(context.Context) (string, error)) iptables.Executor {
			return executor
		}
	}
}

// NewNetworkIsolation creates a network isolation handler.
func NewNetworkIsolation(client DockerClient, opts ...NetworkIsolationOption) *NetworkIsolation {
	n := &NetworkIsolation{
		resolver: mnet.NewDNSResolver(),
		client:   client,
		logger:   zap.NewNop(),
		guards:   make(map[string]*runnerGuard),
	}
	n.newExecutor = func(resolveNS func(context.Context) (string, error)) iptables.Executor {
		return iptables.NewNamespaceExecutor(resolveNS)
	}

	for _, opt := range opts {
		opt(n)
	}
	return n
}

// NewNetworkIsolationWithExecutor creates a handler with a fixed iptables
// executor. This is useful for testing.
func NewNetworkIsolationWithExecutor(executor iptables.Executor, client DockerClient) *NetworkIsolation {
	return NewNetworkIsolation(client, WithIPTablesExecutor(executor))
}

// SetLogger attaches a logger after construction.
func (n *NetworkIsolation) SetLogger(logger *zap.Logger) {
	if logger != nil {
		n.logger = logger
	}
}

// Prepare turns spawn options plus operator configuration into a policy.
//
// It returns (nil, nil) when the session asks for no isolation. Parsing before
// anything is created means a malformed policy fails the spawn instead of
// leaving a running container with no rules on it.
func (n *NetworkIsolation) Prepare(opts provider.SpawnOptions, cfg *Config) (*mnet.NetworkPolicy, error) {
	level := opts.NetworkPolicy
	if level == "" || level == string(mnet.PolicyNone) {
		return nil, nil
	}

	var policyOpts []mnet.PolicyOption

	isolation := IsolationConfig{}
	if cfg != nil {
		isolation = cfg.Isolation
	}

	// The runner must keep reaching the server at every level, air-gapped
	// included. SpawnOptions wins; the provider config is the fallback for
	// deployments that do not populate it.
	serverAddr := opts.ServerURL
	if serverAddr == "" {
		serverAddr = isolation.ServerURL
	}
	if serverAddr != "" {
		ep, err := mnet.ParseEndpoint(serverAddr, mnet.DefaultControlPlanePort)
		if err != nil {
			return nil, fmt.Errorf("parsing control plane address: %w", err)
		}
		policyOpts = append(policyOpts, mnet.WithControlPlane(ep))
	}

	if len(isolation.DNSServers) > 0 {
		policyOpts = append(policyOpts, mnet.WithDNSServers(isolation.DNSServers...))
	}

	if level == string(mnet.PolicyProxy) {
		proxy, err := mnet.ParseProxyConfig(isolation.ProxyURL, isolation.ProxyNoProxy, isolation.ProxyCACert)
		if err != nil {
			return nil, fmt.Errorf("proxy policy requires a configured proxy: %w", err)
		}
		policyOpts = append(policyOpts, mnet.WithProxy(proxy))
	}

	policy, err := mnet.ParsePolicy(level, opts.AllowedHosts, policyOpts...)
	if err != nil {
		return nil, fmt.Errorf("parsing network policy: %w", err)
	}

	if len(policy.ControlPlane) == 0 {
		// Not fatal: a runner with no server address was never going to work,
		// but that is a spawn-plumbing problem, not a firewall one. Say so
		// loudly rather than silently producing a sandbox that cannot report.
		n.logger.Warn("network policy has no control-plane address to pin; "+
			"the runner will not be able to reach the server",
			zap.String("level", level),
			zap.String("runner_id", opts.RunnerID),
		)
	}

	return policy, nil
}

// Resolve pins every hostname in the policy to concrete addresses.
//
// This runs on the host before the container exists, because the container's
// /etc/hosts and proxy environment are derived from the result.
func (n *NetworkIsolation) Resolve(ctx context.Context, policy *mnet.NetworkPolicy) (*mnet.ResolvedPolicy, error) {
	resolved, err := n.resolver.ResolvePolicy(ctx, policy)
	if err != nil {
		return nil, fmt.Errorf("resolving network policy: %w", err)
	}

	if patterns := resolved.UnenforceableHostPatterns(); len(patterns) > 0 {
		// A packet filter has no address set to pin for a wildcard. Saying so
		// is the difference between a documented limit and a silent one.
		n.logger.Warn("wildcard host patterns are not enforced by the packet filter; "+
			"use proxy mode to filter by hostname",
			zap.Strings("patterns", patterns),
		)
	}

	if resolved.HasErrors() {
		n.logger.Warn("some hosts in the network policy did not resolve",
			zap.Errors("errors", resolved.Errors()),
		)
	}

	return resolved, nil
}

// Install writes the policy into the container's network namespace.
//
// The caller must not have attached an interface yet.
func (n *NetworkIsolation) Install(ctx context.Context, key, containerID string, resolved *mnet.ResolvedPolicy) error {
	manager := iptables.NewManager(n.newExecutor(n.namespaceResolver(containerID)))

	if err := manager.Install(ctx, key, resolved); err != nil {
		return fmt.Errorf("installing iptables rules: %w", err)
	}

	n.mu.Lock()
	n.guards[key] = &runnerGuard{manager: manager, resolved: resolved}
	n.mu.Unlock()

	n.logger.Info("network policy installed",
		zap.String("runner_key", key),
		zap.Any("policy", resolved.Summary()),
	)

	return nil
}

// StartRefresh runs the DNS refresh loop for a runner.
//
// ctx must outlive the spawn request: the loop stops when the runner is
// cleaned up, not when the API call that created it returns.
func (n *NetworkIsolation) StartRefresh(ctx context.Context, key string, policy *mnet.NetworkPolicy, resolved *mnet.ResolvedPolicy) error {
	if !policy.RequiresDNSPinning() {
		// Nothing in air-gapped mode can rotate: the control plane is pinned
		// once and there is no allow list to follow.
		return nil
	}

	n.mu.Lock()
	guard, ok := n.guards[key]
	n.mu.Unlock()
	if !ok {
		return fmt.Errorf("no installed policy for runner %s", key)
	}

	applier := &containerApplier{
		manager: guard.manager,
		key:     key,
		ports:   resolved.AllowedPorts,
	}

	var opts []mnet.RefresherOption
	if n.refreshInterval > 0 {
		opts = append(opts, mnet.WithRefreshInterval(n.refreshInterval))
	}
	opts = append(opts, mnet.WithRefreshLogger(n.logger.With(zap.String("runner_key", key))))

	refresher, err := mnet.NewRefresher(n.resolver, applier, policy, resolved, opts...)
	if err != nil {
		return fmt.Errorf("creating DNS refresher: %w", err)
	}

	n.mu.Lock()
	guard.refresher = refresher
	n.mu.Unlock()

	refresher.Start(ctx)

	n.logger.Info("network policy refresher started",
		zap.String("runner_key", key),
		zap.Duration("interval", refresher.Interval()),
	)

	return nil
}

// Cleanup stops the refresher and removes the runner's rules.
//
// The rules die with the network namespace anyway, so a failure here is not
// fatal for isolation; it is reported so a leak on a shared namespace is
// visible.
func (n *NetworkIsolation) Cleanup(ctx context.Context, key string) error {
	n.mu.Lock()
	guard, ok := n.guards[key]
	delete(n.guards, key)
	n.mu.Unlock()

	if !ok {
		return nil
	}

	if guard.refresher != nil {
		guard.refresher.Stop()
	}

	if err := guard.manager.Uninstall(ctx, key); err != nil {
		return fmt.Errorf("removing iptables rules: %w", err)
	}
	return nil
}

// StopAll stops every refresher without touching the rules. Used on shutdown.
func (n *NetworkIsolation) StopAll() {
	n.mu.Lock()
	guards := make([]*runnerGuard, 0, len(n.guards))
	for _, g := range n.guards {
		guards = append(guards, g)
	}
	n.guards = make(map[string]*runnerGuard)
	n.mu.Unlock()

	for _, g := range guards {
		if g.refresher != nil {
			g.refresher.Stop()
		}
	}
}

// ResolvedPolicy returns the policy currently believed to be installed.
func (n *NetworkIsolation) ResolvedPolicy(key string) *mnet.ResolvedPolicy {
	n.mu.Lock()
	defer n.mu.Unlock()

	guard, ok := n.guards[key]
	if !ok {
		return nil
	}
	if guard.refresher != nil {
		return guard.refresher.Current()
	}
	return guard.resolved
}

// namespaceResolver returns the container's current network namespace path.
//
// It re-inspects on every call rather than caching a PID: a container that
// restarts inside a session comes back under a new PID with a new namespace,
// and a cached path would silently target a namespace that no longer exists.
func (n *NetworkIsolation) namespaceResolver(containerID string) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		pid, err := n.containerPID(ctx, containerID)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("/proc/%d/ns/net", pid), nil
	}
}

// containerPID gets the PID of the main process in a container.
func (n *NetworkIsolation) containerPID(ctx context.Context, containerID string) (int, error) {
	if n.client == nil {
		return 0, fmt.Errorf("docker client not configured")
	}

	info, err := n.client.ContainerInspect(ctx, containerID)
	if err != nil {
		return 0, fmt.Errorf("inspecting container %s: %w", containerID, err)
	}
	if info.State == nil {
		return 0, fmt.Errorf("container %s has no state", containerID)
	}
	if info.State.Pid == 0 {
		return 0, fmt.Errorf("container %s is not running (PID is 0)", containerID)
	}

	return info.State.Pid, nil
}

// PolicySummary returns a summary of the applied policy for logging.
func (n *NetworkIsolation) PolicySummary(policy *mnet.ResolvedPolicy) map[string]interface{} {
	if policy == nil {
		return nil
	}
	return policy.Summary()
}

// containerApplier drives one container's chains on behalf of the refresher.
type containerApplier struct {
	manager *iptables.Manager
	key     string
	ports   []int
}

var _ mnet.RuleApplier = (*containerApplier)(nil)

func (a *containerApplier) Allow(ctx context.Context, ips []net.IP) error {
	return a.manager.Allow(ctx, a.key, ips, a.ports)
}

func (a *containerApplier) Deny(ctx context.Context, ips []net.IP) error {
	return a.manager.Deny(ctx, a.key, ips, a.ports)
}

func (a *containerApplier) Reinstall(ctx context.Context, resolved *mnet.ResolvedPolicy) error {
	return a.manager.Install(ctx, a.key, resolved)
}

func (a *containerApplier) Installed(ctx context.Context) (bool, error) {
	return a.manager.Installed(ctx, a.key)
}

// runnerChainKey names the iptables chains for a runner.
//
// The runner ID is preferred over the container ID: it survives a container
// being replaced inside a session, and it keeps the chain name stable for the
// refresher.
func runnerChainKey(runnerID, containerID string) string {
	if runnerID != "" {
		return runnerID
	}
	if len(containerID) > 12 {
		return "container_" + containerID[:12]
	}
	return "container_" + containerID
}
