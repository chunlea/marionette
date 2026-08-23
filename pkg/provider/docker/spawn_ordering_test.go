package docker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mnet "github.com/chunlea/marionette/pkg/network"
	"github.com/chunlea/marionette/pkg/network/iptables"
	"github.com/chunlea/marionette/pkg/provider"
)

// eventLog is a single ordered record of everything a spawn did, across both
// the Docker API and iptables. Two separate mocks cannot answer "did any rule
// exist before the container got an interface"; one shared log can.
type eventLog struct {
	mu     sync.Mutex
	events []string
}

func (l *eventLog) record(format string, args ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, fmt.Sprintf(format, args...))
}

func (l *eventLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

// indexOfEvent returns the position of the first event with the given prefix,
// or -1.
func (l *eventLog) indexOfEvent(prefix string) int {
	for i, e := range l.snapshot() {
		if strings.HasPrefix(e, prefix) {
			return i
		}
	}
	return -1
}

// firstRuleIndex returns the position of the first iptables command that
// installs or modifies a rule (as opposed to creating an empty chain).
func (l *eventLog) firstRuleIndex() int {
	for i, e := range l.snapshot() {
		if !strings.HasPrefix(e, "iptables ") {
			continue
		}
		if strings.Contains(e, " -A ") || strings.Contains(e, " -I ") {
			return i
		}
	}
	return -1
}

// loggingClient is a DockerClient that appends to an eventLog.
type loggingClient struct {
	log *eventLog

	createErr  error
	startErr   error
	connectErr error

	createdHostConfig *container.HostConfig
	createdNetConfig  *network.NetworkingConfig
	createdConfig     *container.Config
}

func newLoggingClient(log *eventLog) *loggingClient {
	return &loggingClient{log: log}
}

func (c *loggingClient) ContainerCreate(_ context.Context, cfg *container.Config, hostCfg *container.HostConfig,
	netCfg *network.NetworkingConfig, _ *ocispec.Platform, name string) (container.CreateResponse, error) {
	c.createdConfig = cfg
	c.createdHostConfig = hostCfg
	c.createdNetConfig = netCfg

	mode := ""
	if hostCfg != nil {
		mode = string(hostCfg.NetworkMode)
	}
	c.log.record("container-create name=%s network-mode=%q", name, mode)

	if c.createErr != nil {
		return container.CreateResponse{}, c.createErr
	}
	return container.CreateResponse{ID: "container1"}, nil
}

func (c *loggingClient) ContainerStart(_ context.Context, id string, _ container.StartOptions) error {
	c.log.record("container-start %s", id)
	return c.startErr
}

func (c *loggingClient) ContainerStop(_ context.Context, id string, _ container.StopOptions) error {
	c.log.record("container-stop %s", id)
	return nil
}

func (c *loggingClient) ContainerRemove(_ context.Context, id string, _ container.RemoveOptions) error {
	c.log.record("container-remove %s", id)
	return nil
}

func (c *loggingClient) ContainerInspect(_ context.Context, id string) (types.ContainerJSON, error) {
	return types.ContainerJSON{
		ContainerJSONBase: &types.ContainerJSONBase{
			ID:    id,
			State: &container.State{Running: true, Pid: 4242},
		},
	}, nil
}

func (c *loggingClient) ContainerList(_ context.Context, _ container.ListOptions) ([]types.Container, error) {
	return []types.Container{{ID: "container1"}}, nil
}

func (c *loggingClient) ContainerPause(context.Context, string) error   { return nil }
func (c *loggingClient) ContainerUnpause(context.Context, string) error { return nil }

func (c *loggingClient) ImagePull(context.Context, string, image.PullOptions) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("")), nil
}

func (c *loggingClient) NetworkList(context.Context, network.ListOptions) ([]network.Summary, error) {
	return []network.Summary{{Name: "marionette-network"}}, nil
}

func (c *loggingClient) NetworkCreate(context.Context, string, network.CreateOptions) (network.CreateResponse, error) {
	return network.CreateResponse{}, nil
}

func (c *loggingClient) NetworkInspect(context.Context, string, network.InspectOptions) (network.Inspect, error) {
	return network.Inspect{}, nil
}

func (c *loggingClient) NetworkConnect(_ context.Context, networkID, containerID string, _ *network.EndpointSettings) error {
	c.log.record("network-connect %s -> %s", containerID, networkID)
	return c.connectErr
}

func (c *loggingClient) NetworkDisconnect(_ context.Context, networkID, containerID string, _ bool) error {
	c.log.record("network-disconnect %s -> %s", containerID, networkID)
	return nil
}

func (c *loggingClient) Ping(context.Context) (types.Ping, error) { return types.Ping{}, nil }
func (c *loggingClient) Close() error                             { return nil }

var _ DockerClient = (*loggingClient)(nil)

// loggingExecutor records every iptables command into the shared event log.
type loggingExecutor struct {
	log *eventLog

	mu       sync.Mutex
	failOn   string
	checkErr error
}

func newLoggingExecutor(log *eventLog) *loggingExecutor {
	return &loggingExecutor{log: log, checkErr: iptables.ErrMockRuleMissing}
}

func (e *loggingExecutor) exec(binary string, args []string) error {
	e.log.record("%s %s", binary, strings.Join(args, " "))

	e.mu.Lock()
	failOn := e.failOn
	e.mu.Unlock()

	if failOn != "" && strings.Contains(strings.Join(args, " "), failOn) {
		return errors.New("simulated iptables failure")
	}
	if len(args) > 0 && args[0] == "-C" {
		return e.checkErr
	}
	return nil
}

func (e *loggingExecutor) Run(_ context.Context, args ...string) error {
	return e.exec("iptables", args)
}
func (e *loggingExecutor) Output(_ context.Context, args ...string) ([]byte, error) {
	return nil, e.exec("iptables", args)
}
func (e *loggingExecutor) RunIPv6(_ context.Context, args ...string) error {
	return e.exec("ip6tables", args)
}
func (e *loggingExecutor) OutputIPv6(_ context.Context, args ...string) ([]byte, error) {
	return nil, e.exec("ip6tables", args)
}

var _ iptables.Executor = (*loggingExecutor)(nil)

// orderingFixture wires a provider whose Docker calls and iptables commands
// land in one ordered log.
type orderingFixture struct {
	log      *eventLog
	client   *loggingClient
	executor *loggingExecutor
	provider *Provider
	resolver *stubResolver
}

func newOrderingFixture(t *testing.T, cfg *Config) *orderingFixture {
	t.Helper()

	log := &eventLog{}
	client := newLoggingClient(log)
	executor := newLoggingExecutor(log)

	stub := newStubResolver()
	stub.set("github.com", "140.82.121.4")
	stub.set("marionette.internal", "10.5.0.7")
	stub.set("proxy.internal", "203.0.113.9")

	ni := NewNetworkIsolation(client, WithIPTablesExecutor(executor))
	ni.resolver = mnet.NewDNSResolver(mnet.WithResolver(stub))

	if cfg == nil {
		cfg = &Config{Image: "marionette/agent:latest", Network: "marionette-network"}
	}

	p := NewWithClientAndNetworkIsolation("docker-test", cfg, nil, client, ni)
	t.Cleanup(func() { ni.StopAll() })

	return &orderingFixture{log: log, client: client, executor: executor, provider: p, resolver: stub}
}

// TestSpawn_NoEgressIsPossibleBeforeRulesExist is the proof for the startup
// window.
//
// The mechanism being asserted, precisely:
//
//  1. The container is created with NetworkMode "none". Docker gives it a
//     private network namespace containing a loopback interface and nothing
//     else. There is no veth, no route off-box, and no address other than
//     127.0.0.1: a packet has nowhere to go, whatever the process inside does.
//  2. The container is started. Its entrypoint runs, and any traffic it
//     attempts fails immediately for want of a route.
//  3. iptables rules are installed into that namespace from the host, via
//     nsenter, while it is still interface-less.
//  4. Only then is the container connected to a network, which is the moment
//     an interface, an address and a default route appear.
//
// So the ordering assertion below is the security property: every rule exists
// before the container has any means of sending a packet at all. Applying the
// policy after start-with-network, which is what this used to do, left a
// window between the two hundreds of milliseconds wide.
func TestSpawn_NoEgressIsPossibleBeforeRulesExist(t *testing.T) {
	f := newOrderingFixture(t, nil)

	_, err := f.provider.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_1",
		Name:          "runner-1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	})
	require.NoError(t, err)

	events := f.log.snapshot()
	create := f.log.indexOfEvent("container-create")
	start := f.log.indexOfEvent("container-start")
	firstRule := f.log.firstRuleIndex()
	connect := f.log.indexOfEvent("network-connect")

	require.NotEqual(t, -1, create, "events: %v", events)
	require.NotEqual(t, -1, start)
	require.NotEqual(t, -1, firstRule)
	require.NotEqual(t, -1, connect, "a restricted container must be connected explicitly")

	assert.Less(t, create, start)
	assert.Less(t, start, firstRule)
	assert.Less(t, firstRule, connect,
		"every rule must be installed before the container has an interface to send through")

	// Step 1: the namespace really is interface-less at creation.
	assert.Contains(t, events[create], `network-mode="none"`)
	assert.Nil(t, f.client.createdNetConfig,
		"a restricted container must not be attached at creation time")

	// The default drop is what the allow-list rules sit in front of; without it
	// the chain would fall through to ACCEPT.
	assert.True(t, containsFragment(events, "-A MARIONETTE_run_1 ", "-j DROP"))

	// Docker must never restart this container behind our back: a restart
	// re-creates the namespace with an interface already attached.
	require.NotNil(t, f.client.createdHostConfig)
	assert.Equal(t, container.RestartPolicyDisabled, f.client.createdHostConfig.RestartPolicy.Name)
}

func TestSpawn_RuleFailureNeverConnectsTheContainer(t *testing.T) {
	f := newOrderingFixture(t, nil)
	// Fail while writing the default drop, i.e. midway through installation.
	f.executor.failOn = "-j DROP"

	_, err := f.provider.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "network policy failed")

	// A half-configured container is destroyed, never connected. Connecting it
	// would hand a sandbox with incomplete rules a working interface.
	assert.Equal(t, -1, f.log.indexOfEvent("network-connect"))
	assert.NotEqual(t, -1, f.log.indexOfEvent("container-remove"))
}

func TestSpawn_ConnectFailureCleansUp(t *testing.T) {
	f := newOrderingFixture(t, nil)
	f.client.connectErr = errors.New("network not found")

	_, err := f.provider.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "air_gapped",
	})
	require.Error(t, err)
	assert.NotEqual(t, -1, f.log.indexOfEvent("container-remove"))
	assert.Nil(t, f.provider.networkIsolation.ResolvedPolicy("run_1"))
}

func TestSpawn_InvalidPolicyCreatesNothing(t *testing.T) {
	f := newOrderingFixture(t, nil)

	_, err := f.provider.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_1",
		NetworkPolicy: "nonsense",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid network policy")

	// Nothing was created, so there is nothing to leak.
	assert.Empty(t, f.log.snapshot())
}

// TestSpawn_UnrestrictedPathIsUnchanged guards the no-isolation path. The
// smoke runner and every default session run with policy "none", and none of
// the isolation machinery may touch them.
func TestSpawn_UnrestrictedPathIsUnchanged(t *testing.T) {
	for _, level := range []string{"", "none"} {
		t.Run("level="+level, func(t *testing.T) {
			f := newOrderingFixture(t, nil)

			_, err := f.provider.Spawn(context.Background(), provider.SpawnOptions{
				RunnerID:      "run_1",
				NetworkPolicy: level,
			})
			require.NoError(t, err)

			events := f.log.snapshot()
			for _, e := range events {
				assert.False(t, strings.HasPrefix(e, "iptables"), "unexpected rule: %s", e)
				assert.False(t, strings.HasPrefix(e, "ip6tables"), "unexpected rule: %s", e)
			}

			// Attached at creation, as before, and never connected separately.
			assert.Equal(t, -1, f.log.indexOfEvent("network-connect"))
			require.NotNil(t, f.client.createdNetConfig)
			assert.Contains(t, f.client.createdNetConfig.EndpointsConfig, "marionette-network")
			assert.Empty(t, string(f.client.createdHostConfig.NetworkMode))
			assert.Empty(t, f.client.createdHostConfig.ExtraHosts)
		})
	}
}

func TestSpawn_AirGappedPinsTheServerIntoEtcHosts(t *testing.T) {
	f := newOrderingFixture(t, nil)

	_, err := f.provider.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "air_gapped",
	})
	require.NoError(t, err)

	// Air-gapped runners have no DNS at all, so the only way the agent can turn
	// the server's name into an address is a hosts entry pinned at spawn time.
	require.NotNil(t, f.client.createdHostConfig)
	assert.Equal(t, []string{"marionette.internal:10.5.0.7"}, f.client.createdHostConfig.ExtraHosts)

	for _, e := range f.log.snapshot() {
		if strings.HasPrefix(e, "iptables") || strings.HasPrefix(e, "ip6tables") {
			assert.NotContains(t, e, "--dport 53", "air-gapped must not open DNS")
		}
	}
}

func TestSpawn_ProxyModeInjectsTheProxyEnvironment(t *testing.T) {
	f := newOrderingFixture(t, &Config{
		Image:   "marionette/agent:latest",
		Network: "marionette-network",
		Isolation: IsolationConfig{
			ProxyURL:    "http://proxy.internal:3128",
			ProxyCACert: "/etc/marionette/proxy-ca.crt",
		},
	})

	_, err := f.provider.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "proxy",
	})
	require.NoError(t, err)

	env := strings.Join(f.client.createdConfig.Env, "\n")
	assert.Contains(t, env, "HTTPS_PROXY=http://proxy.internal:3128")
	assert.Contains(t, env, "NODE_EXTRA_CA_CERTS=/etc/marionette/proxy-ca.crt")
	// The agent's gRPC connection must not be proxied.
	assert.Contains(t, env, "NO_PROXY=")
	assert.Contains(t, env, "marionette.internal")

	// And the firewall is what actually enforces it: only the proxy is open.
	assert.True(t, containsFragment(f.log.snapshot(), "-d 203.0.113.9", "--dport 3128"))
}

func TestSpawn_SessionEnvironmentOverridesInjectedProxy(t *testing.T) {
	f := newOrderingFixture(t, &Config{
		Image:     "marionette/agent:latest",
		Network:   "marionette-network",
		Isolation: IsolationConfig{ProxyURL: "http://proxy.internal:3128"},
	})

	_, err := f.provider.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "proxy",
		Environment:   map[string]string{"HTTPS_PROXY": "http://override:8080"},
	})
	require.NoError(t, err)

	// Docker takes the last value for a repeated key, so an operator override
	// has to come after the injected default.
	env := f.client.createdConfig.Env
	last := ""
	for _, e := range env {
		if strings.HasPrefix(e, "HTTPS_PROXY=") {
			last = e
		}
	}
	assert.Equal(t, "HTTPS_PROXY=http://override:8080", last)
}

func TestDestroy_StopsTheRefresherAndRemovesRules(t *testing.T) {
	f := newOrderingFixture(t, nil)

	_, err := f.provider.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	})
	require.NoError(t, err)
	require.NotNil(t, f.provider.networkIsolation.ResolvedPolicy("run_1"))

	require.NoError(t, f.provider.Destroy(context.Background(), "run_1"))

	assert.True(t, containsFragment(f.log.snapshot(), "-X MARIONETTE_run_1"))
	assert.Nil(t, f.provider.networkIsolation.ResolvedPolicy("run_1"))
}

func TestProviderClose_StopsRefreshers(t *testing.T) {
	f := newOrderingFixture(t, nil)

	_, err := f.provider.Spawn(context.Background(), provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	})
	require.NoError(t, err)

	require.NoError(t, f.provider.Close())
	assert.Nil(t, f.provider.networkIsolation.ResolvedPolicy("run_1"))
}
