package docker

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mnet "github.com/chunlea/marionette/pkg/network"
	"github.com/chunlea/marionette/pkg/network/iptables"
	"github.com/chunlea/marionette/pkg/provider"
)

// stubResolver answers DNS from a fixed table.
type stubResolver struct {
	mu      sync.Mutex
	results map[string][]net.IP
	errs    map[string]error
	calls   int
}

func newStubResolver() *stubResolver {
	return &stubResolver{results: map[string][]net.IP{}, errs: map[string]error{}}
}

func (s *stubResolver) LookupIP(_ context.Context, _, host string) ([]net.IP, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if err, ok := s.errs[host]; ok {
		return nil, err
	}
	if ips, ok := s.results[host]; ok {
		return ips, nil
	}
	return nil, errors.New("no such host: " + host)
}

func (s *stubResolver) set(host string, ips ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	parsed := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		parsed = append(parsed, net.ParseIP(ip))
	}
	s.results[host] = parsed
}

// isolationFixture wires a NetworkIsolation to a recording executor.
type isolationFixture struct {
	isolation *NetworkIsolation
	executor  *iptables.MockExecutor
	resolver  *stubResolver
}

func newIsolationFixture(t *testing.T, opts ...NetworkIsolationOption) *isolationFixture {
	t.Helper()

	exec := iptables.NewMockExecutor()
	stub := newStubResolver()

	all := append([]NetworkIsolationOption{WithIPTablesExecutor(exec)}, opts...)
	ni := NewNetworkIsolation(nil, all...)
	ni.resolver = mnet.NewDNSResolver(mnet.WithResolver(stub))

	return &isolationFixture{isolation: ni, executor: exec, resolver: stub}
}

func (f *isolationFixture) commands() []string {
	out := make([]string, 0)
	for _, c := range f.executor.GetCommands() {
		out = append(out, strings.Join(c, " "))
	}
	for _, c := range f.executor.GetIPv6Commands() {
		out = append(out, "v6 "+strings.Join(c, " "))
	}
	return out
}

func containsFragment(cmds []string, fragments ...string) bool {
	for _, c := range cmds {
		matched := true
		for _, f := range fragments {
			if !strings.Contains(c, f) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func TestNewNetworkIsolation(t *testing.T) {
	ni := NewNetworkIsolation(nil)
	assert.NotNil(t, ni.resolver)
	assert.NotNil(t, ni.newExecutor)
	assert.NotNil(t, ni.logger)
}

func TestNewNetworkIsolationWithExecutor(t *testing.T) {
	mock := iptables.NewMockExecutor()
	ni := NewNetworkIsolationWithExecutor(mock, nil)
	assert.NotNil(t, ni.resolver)
	assert.Same(t, mock, ni.newExecutor(nil))
}

func TestNetworkIsolation_Prepare_NoPolicy(t *testing.T) {
	f := newIsolationFixture(t)

	for _, level := range []string{"", "none"} {
		policy, err := f.isolation.Prepare(provider.SpawnOptions{NetworkPolicy: level}, &Config{})
		require.NoError(t, err)
		assert.Nil(t, policy, "level %q must not produce a policy", level)
		assert.False(t, policy.IsRestricted())
	}
}

func TestNetworkIsolation_Prepare_InvalidLevel(t *testing.T) {
	f := newIsolationFixture(t)

	_, err := f.isolation.Prepare(provider.SpawnOptions{NetworkPolicy: "invalid_policy"}, &Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing network policy")
}

func TestNetworkIsolation_Prepare_AllowListNeedsHosts(t *testing.T) {
	f := newIsolationFixture(t)

	_, err := f.isolation.Prepare(provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{},
	}, &Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one allowed host")
}

func TestNetworkIsolation_Prepare_ControlPlaneFromSpawnOptions(t *testing.T) {
	f := newIsolationFixture(t)

	policy, err := f.isolation.Prepare(provider.SpawnOptions{
		NetworkPolicy: "air_gapped",
		ServerURL:     "marionette.internal:9090",
	}, &Config{Isolation: IsolationConfig{ServerURL: "fallback.internal:1234"}})
	require.NoError(t, err)

	// The caller's address wins over the operator fallback.
	require.Len(t, policy.ControlPlane, 1)
	assert.Equal(t, "marionette.internal", policy.ControlPlane[0].Host)
	assert.Equal(t, 9090, policy.ControlPlane[0].Port)
}

func TestNetworkIsolation_Prepare_ControlPlaneFallsBackToConfig(t *testing.T) {
	f := newIsolationFixture(t)

	// The session manager does not populate SpawnOptions.ServerURL today, so
	// without this fallback every restricted runner would be cut off from the
	// server it is supposed to report to.
	policy, err := f.isolation.Prepare(provider.SpawnOptions{
		NetworkPolicy: "air_gapped",
	}, &Config{Isolation: IsolationConfig{ServerURL: "marionette.internal"}})
	require.NoError(t, err)

	require.Len(t, policy.ControlPlane, 1)
	assert.Equal(t, mnet.DefaultControlPlanePort, policy.ControlPlane[0].Port)
}

func TestNetworkIsolation_Prepare_NoControlPlaneIsNotFatal(t *testing.T) {
	f := newIsolationFixture(t)

	policy, err := f.isolation.Prepare(provider.SpawnOptions{NetworkPolicy: "air_gapped"}, &Config{})
	require.NoError(t, err)
	assert.Empty(t, policy.ControlPlane)
}

func TestNetworkIsolation_Prepare_ProxyRequiresConfiguration(t *testing.T) {
	f := newIsolationFixture(t)

	_, err := f.isolation.Prepare(provider.SpawnOptions{NetworkPolicy: "proxy"}, &Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "proxy policy requires a configured proxy")

	policy, err := f.isolation.Prepare(provider.SpawnOptions{NetworkPolicy: "proxy"}, &Config{
		Isolation: IsolationConfig{ProxyURL: "http://proxy.internal:3128"},
	})
	require.NoError(t, err)
	require.NotNil(t, policy.Proxy)
}

func TestNetworkIsolation_Prepare_InvalidServerAddress(t *testing.T) {
	f := newIsolationFixture(t)

	_, err := f.isolation.Prepare(provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
		ServerURL:     "://nonsense",
	}, &Config{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing control plane address")
}

func TestNetworkIsolation_Resolve_PinsEverything(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.set("github.com", "140.82.121.4")
	f.resolver.set("marionette.internal", "10.5.0.7")

	policy, err := f.isolation.Prepare(provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
		ServerURL:     "marionette.internal:9090",
	}, &Config{Isolation: IsolationConfig{DNSServers: []string{"10.5.0.53"}}})
	require.NoError(t, err)

	resolved, err := f.isolation.Resolve(context.Background(), policy)
	require.NoError(t, err)

	assert.Equal(t, "140.82.121.4", resolved.AllIPsFiltered()[0].String())
	assert.Equal(t, "10.5.0.7", resolved.ControlPlaneIPs()[0].String())
	assert.Equal(t, "10.5.0.53", resolved.DNSServers[0].String())
}

func TestNetworkIsolation_Resolve_SurvivesFailedLookups(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.errs["broken.example.com"] = errors.New("SERVFAIL")

	policy, err := f.isolation.Prepare(provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"broken.example.com"},
	}, &Config{})
	require.NoError(t, err)

	// A host that will not resolve is not a reason to refuse the spawn: it just
	// gets no rule, which fails closed.
	resolved, err := f.isolation.Resolve(context.Background(), policy)
	require.NoError(t, err)
	assert.True(t, resolved.HasErrors())
	assert.Empty(t, resolved.AllIPsFiltered())
}

// installedPolicy prepares, resolves and installs a policy in one step.
func (f *isolationFixture) installedPolicy(t *testing.T, opts provider.SpawnOptions, cfg *Config) (*mnet.NetworkPolicy, *mnet.ResolvedPolicy) {
	t.Helper()

	policy, err := f.isolation.Prepare(opts, cfg)
	require.NoError(t, err)

	resolved, err := f.isolation.Resolve(context.Background(), policy)
	require.NoError(t, err)

	require.NoError(t, f.isolation.Install(context.Background(), "run_1", "container1", resolved))
	return policy, resolved
}

func TestNetworkIsolation_Install_AllowList(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.set("github.com", "140.82.121.4")
	f.resolver.set("marionette.internal", "10.5.0.7")

	f.installedPolicy(t, provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
		ServerURL:     "marionette.internal:9090",
	}, &Config{})

	cmds := f.commands()
	assert.True(t, containsFragment(cmds, "-d 140.82.121.4", "--dport 443"), "allow-list address must be opened")
	assert.True(t, containsFragment(cmds, "-d 10.5.0.7", "--dport 9090"), "control plane must stay reachable")
	assert.True(t, containsFragment(cmds, "-d 169.254.169.254/32", "DROP"), "metadata endpoint must be blocked")
	assert.True(t, containsFragment(cmds, "-I OUTPUT"), "the chain has to be linked into OUTPUT to do anything")

	assert.NotNil(t, f.isolation.ResolvedPolicy("run_1"))
}

func TestNetworkIsolation_Install_AirGappedOpensOnlyTheControlPlane(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.set("marionette.internal", "10.5.0.7")

	f.installedPolicy(t, provider.SpawnOptions{
		NetworkPolicy: "air_gapped",
		ServerURL:     "marionette.internal:9090",
	}, &Config{})

	cmds := f.commands()
	assert.True(t, containsFragment(cmds, "-d 10.5.0.7", "--dport 9090"))

	// No DNS, no allow list, and a default drop. That is the whole promise.
	for _, c := range cmds {
		assert.NotContains(t, c, "--dport 53")
		assert.NotContains(t, c, "--dport 80")
	}
	assert.True(t, containsFragment(cmds, "-j DROP"))
}

func TestNetworkIsolation_Install_ProxyOpensOnlyTheProxy(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.set("proxy.internal", "203.0.113.9")
	f.resolver.set("marionette.internal", "10.5.0.7")

	f.installedPolicy(t, provider.SpawnOptions{
		NetworkPolicy: "proxy",
		ServerURL:     "marionette.internal:9090",
	}, &Config{Isolation: IsolationConfig{ProxyURL: "http://proxy.internal:3128"}})

	cmds := f.commands()
	assert.True(t, containsFragment(cmds, "-d 203.0.113.9", "--dport 3128"))
	assert.True(t, containsFragment(cmds, "-d 10.5.0.7", "--dport 9090"))

	// Direct egress on 443 is not opened, so a tool that ignores HTTPS_PROXY
	// fails instead of bypassing the proxy.
	for _, c := range cmds {
		assert.NotContains(t, c, "--dport 443")
	}
}

func TestNetworkIsolation_Install_Failure(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.set("github.com", "140.82.121.4")

	f.executor.SetError([]string{"-N", "MARIONETTE_run_1"}, errors.New("permission denied"))

	policy, err := f.isolation.Prepare(provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	}, &Config{})
	require.NoError(t, err)

	resolved, err := f.isolation.Resolve(context.Background(), policy)
	require.NoError(t, err)

	err = f.isolation.Install(context.Background(), "run_1", "container1", resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "installing iptables rules")
	assert.Nil(t, f.isolation.ResolvedPolicy("run_1"), "a failed install must not be recorded as active")
}

func TestNetworkIsolation_StartRefresh_SkippedForAirGapped(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.set("marionette.internal", "10.5.0.7")

	policy, resolved := f.installedPolicy(t, provider.SpawnOptions{
		NetworkPolicy: "air_gapped",
		ServerURL:     "marionette.internal:9090",
	}, &Config{})

	// Nothing rotates in air-gapped mode: there is no allow list, and the
	// control plane is pinned once into /etc/hosts.
	require.NoError(t, f.isolation.StartRefresh(context.Background(), "run_1", policy, resolved))

	f.isolation.mu.Lock()
	guard := f.isolation.guards["run_1"]
	f.isolation.mu.Unlock()
	assert.Nil(t, guard.refresher)
}

func TestNetworkIsolation_StartRefresh_RunsForAllowList(t *testing.T) {
	f := newIsolationFixture(t, WithNetworkRefreshInterval(time.Minute))
	f.resolver.set("github.com", "140.82.121.4")

	policy, resolved := f.installedPolicy(t, provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	}, &Config{})

	require.NoError(t, f.isolation.StartRefresh(context.Background(), "run_1", policy, resolved))
	defer f.isolation.StopAll()

	f.isolation.mu.Lock()
	guard := f.isolation.guards["run_1"]
	f.isolation.mu.Unlock()
	require.NotNil(t, guard.refresher)
	assert.Equal(t, time.Minute, guard.refresher.Interval())
}

func TestNetworkIsolation_StartRefresh_WithoutInstall(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.set("github.com", "140.82.121.4")

	policy, err := f.isolation.Prepare(provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	}, &Config{})
	require.NoError(t, err)
	resolved, err := f.isolation.Resolve(context.Background(), policy)
	require.NoError(t, err)

	err = f.isolation.StartRefresh(context.Background(), "run_1", policy, resolved)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no installed policy")
}

func TestNetworkIsolation_RefresherFollowsARotatedRecord(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.set("cdn.example.com", "1.1.1.1")

	policy, resolved := f.installedPolicy(t, provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"cdn.example.com"},
	}, &Config{})
	require.NoError(t, f.isolation.StartRefresh(context.Background(), "run_1", policy, resolved))
	defer f.isolation.StopAll()

	f.isolation.mu.Lock()
	refresher := f.isolation.guards["run_1"].refresher
	f.isolation.mu.Unlock()
	require.NotNil(t, refresher)

	// The chain is in place, so the refresher diffs rather than reinstalls.
	f.executor.SetCheckOK([]string{"-C", "OUTPUT", "-j", "MARIONETTE_run_1"})
	f.executor.Reset()
	f.executor.SetCheckOK([]string{"-C", "OUTPUT", "-j", "MARIONETTE_run_1"})

	f.resolver.set("cdn.example.com", "2.2.2.2")
	res := refresher.RefreshOnce(context.Background())
	require.NoError(t, res.Err)

	cmds := f.commands()
	assert.True(t, containsFragment(cmds, "-A MARIONETTE_run_1_D", "-d 2.2.2.2"), "the new address must be opened")
	assert.True(t, containsFragment(cmds, "-D MARIONETTE_run_1_D", "-d 1.1.1.1"), "the old address must be withdrawn")
}

func TestNetworkIsolation_Cleanup(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.set("github.com", "140.82.121.4")

	policy, resolved := f.installedPolicy(t, provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	}, &Config{})
	require.NoError(t, f.isolation.StartRefresh(context.Background(), "run_1", policy, resolved))

	f.executor.Reset()
	require.NoError(t, f.isolation.Cleanup(context.Background(), "run_1"))

	cmds := f.commands()
	assert.True(t, containsFragment(cmds, "-D OUTPUT", "-j MARIONETTE_run_1"))
	assert.True(t, containsFragment(cmds, "-X MARIONETTE_run_1"))
	assert.True(t, containsFragment(cmds, "-X MARIONETTE_run_1_D"))
	assert.Nil(t, f.isolation.ResolvedPolicy("run_1"))
}

func TestNetworkIsolation_CleanupUnknownRunner(t *testing.T) {
	f := newIsolationFixture(t)
	require.NoError(t, f.isolation.Cleanup(context.Background(), "run_missing"))
	assert.Empty(t, f.commands())
}

func TestNetworkIsolation_StopAllIsSafeToRepeat(t *testing.T) {
	f := newIsolationFixture(t)
	f.resolver.set("github.com", "140.82.121.4")

	policy, resolved := f.installedPolicy(t, provider.SpawnOptions{
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	}, &Config{})
	require.NoError(t, f.isolation.StartRefresh(context.Background(), "run_1", policy, resolved))

	f.isolation.StopAll()
	f.isolation.StopAll()
	assert.Nil(t, f.isolation.ResolvedPolicy("run_1"))
}

func TestNetworkIsolation_PolicySummary(t *testing.T) {
	ni := NewNetworkIsolation(nil)
	assert.Nil(t, ni.PolicySummary(nil))

	policy, err := mnet.ParsePolicy("allow_list", []string{"github.com"})
	require.NoError(t, err)
	resolved := mnet.NewResolvedPolicy(policy, nil, time.Minute)
	assert.Equal(t, mnet.PolicyAllowList, ni.PolicySummary(resolved)["level"])
}

func TestRunnerChainKey(t *testing.T) {
	// The runner ID survives a container being replaced inside a session, so
	// the chain name stays stable for the refresher.
	assert.Equal(t, "run_abc", runnerChainKey("run_abc", "container1"))
	assert.Equal(t, "container_abcdef012345", runnerChainKey("", "abcdef0123456789"))
	assert.Equal(t, "container_short", runnerChainKey("", "short"))
}

func TestControlPlaneHostEntries(t *testing.T) {
	resolved := &mnet.ResolvedPolicy{
		ControlPlane: []mnet.EndpointResolution{
			{Endpoint: mnet.Endpoint{Host: "marionette.internal", Port: 9090}, IPs: []net.IP{net.ParseIP("10.5.0.7")}},
			// An IP literal needs no hosts entry.
			{Endpoint: mnet.Endpoint{Host: "10.5.0.8", Port: 9090}, IPs: []net.IP{net.ParseIP("10.5.0.8")}},
			// A name that did not resolve cannot be pinned.
			{Endpoint: mnet.Endpoint{Host: "unresolved.internal", Port: 9090}},
		},
	}

	assert.Equal(t, []string{"marionette.internal:10.5.0.7"}, controlPlaneHostEntries(resolved))
}

func TestProxyEnv(t *testing.T) {
	assert.Nil(t, proxyEnv(nil))
	assert.Nil(t, proxyEnv(&mnet.ResolvedPolicy{}))

	proxy, err := mnet.ParseProxyConfig("http://proxy.internal:3128", nil, "")
	require.NoError(t, err)
	policy, err := mnet.ParsePolicy("proxy", nil, mnet.WithProxy(proxy))
	require.NoError(t, err)

	resolved := mnet.NewResolvedPolicy(policy, nil, time.Minute)
	resolved.ControlPlane = []mnet.EndpointResolution{
		{Endpoint: mnet.Endpoint{Host: "marionette.internal", Port: 9090}},
	}

	env := proxyEnv(resolved)
	assert.Equal(t, "http://proxy.internal:3128", env["HTTPS_PROXY"])
	// The agent's own gRPC connection must never be proxied.
	assert.Contains(t, env["NO_PROXY"], "marionette.internal")
}
