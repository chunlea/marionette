package kubernetes

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mnet "github.com/chunlea/marionette/pkg/network"
	"github.com/chunlea/marionette/pkg/provider"
)

// stubLookup answers DNS from a fixed table so tests never touch the network.
type stubLookup struct {
	results map[string][]net.IP
}

func (s *stubLookup) LookupIP(_ context.Context, _, host string) ([]net.IP, error) {
	if ips, ok := s.results[host]; ok {
		return ips, nil
	}
	return nil, errors.New("no such host: " + host)
}

// stubbedResolver builds a resolver backed by a fixed answer table.
func stubbedResolver(table map[string][]string) *mnet.DNSResolver {
	results := map[string][]net.IP{}
	for host, ips := range table {
		parsed := make([]net.IP, 0, len(ips))
		for _, ip := range ips {
			parsed = append(parsed, net.ParseIP(ip))
		}
		results[host] = parsed
	}
	return mnet.NewDNSResolver(mnet.WithResolver(&stubLookup{results: results}))
}

// networkFixture is a provider with a stubbed resolver and mock API.
type networkFixture struct {
	provider *Provider
	client   *MockKubeClient
}

func newNetworkFixture(t *testing.T, isolation IsolationConfig) *networkFixture {
	t.Helper()

	client := NewMockKubeClient()
	client.AddNamespace("test-ns")

	p := NewWithClient("k8s-test", &Config{
		Namespace:     "test-ns",
		Image:         "marionette/agent:latest",
		LabelPrefix:   "marionette.dev",
		RestartPolicy: "Never",
		Resources:     ResourceConfig{Memory: "2Gi", CPUs: "2"},
		Storage:       StorageConfig{Size: "10Gi", AccessMode: "ReadWriteOnce"},
		Isolation:     isolation,
	}, nil, client)

	p.resolver = stubbedResolver(map[string][]string{
		"github.com":          {"140.82.121.4"},
		"marionette.internal": {"10.5.0.7"},
		"proxy.internal":      {"203.0.113.9"},
		"evil.example.com":    {"169.254.169.254", "8.8.8.8"},
	})

	return &networkFixture{provider: p, client: client}
}

// spawn runs Spawn and unblocks waitForPodReady by flipping the mock pod to
// Running once it exists.
func (f *networkFixture) spawn(t *testing.T, opts provider.SpawnOptions) (*provider.RunnerInstance, error) {
	t.Helper()

	name := f.provider.podName(opts.Name, opts.RunnerID)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			f.client.SetPodPhase("test-ns", name, corev1.PodRunning)
			if pod := f.client.GetStoredPod("test-ns", name); pod != nil && pod.Status.Phase == corev1.PodRunning {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	defer func() { <-done }()

	return f.provider.Spawn(context.Background(), opts)
}

// build prepares, resolves and renders a NetworkPolicy in one step.
func (f *networkFixture) build(t *testing.T, opts provider.SpawnOptions) *networkingv1.NetworkPolicy {
	t.Helper()

	policy, err := f.provider.prepareNetworkPolicy(opts)
	require.NoError(t, err)
	if !policy.IsRestricted() {
		np, err := f.provider.buildNetworkPolicy(opts.RunnerID, opts, nil)
		require.NoError(t, err)
		return np
	}

	resolved, err := f.provider.resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)

	np, err := f.provider.buildNetworkPolicy(opts.RunnerID, opts, resolved)
	require.NoError(t, err)
	return np
}

// cidrs collects every ipBlock CIDR in an egress rule set.
func cidrs(rules []networkingv1.NetworkPolicyEgressRule) []string {
	var out []string
	for _, r := range rules {
		for _, peer := range r.To {
			if peer.IPBlock != nil {
				out = append(out, peer.IPBlock.CIDR)
			}
		}
	}
	return out
}

// hasDNSRule reports whether any rule opens port 53.
func hasDNSRule(rules []networkingv1.NetworkPolicyEgressRule) bool {
	for _, r := range rules {
		for _, port := range r.Ports {
			if port.Port != nil && port.Port.IntValue() == 53 {
				return true
			}
		}
	}
	return false
}

func TestBuildNetworkPolicy_NoneNeedsNoPolicy(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	for _, level := range []string{"", "none"} {
		np := f.build(t, provider.SpawnOptions{RunnerID: "run_none", NetworkPolicy: level})
		assert.Nil(t, np, "level %q must not create a NetworkPolicy", level)
	}
}

func TestBuildNetworkPolicy_UnknownLevelIsAnError(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	_, err := f.provider.prepareNetworkPolicy(provider.SpawnOptions{
		RunnerID:      "run_bogus",
		NetworkPolicy: "not_a_real_policy",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown policy level")
}

func TestBuildNetworkPolicy_AllowListPinsResolvedAddresses(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	np := f.build(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	})
	require.NotNil(t, np)

	// NetworkPolicy cannot express a hostname, so the name is resolved on the
	// server and written as a single-host ipBlock. That is also what makes DNS
	// rebinding ineffective: the rule never consults DNS again.
	assert.Contains(t, cidrs(np.Spec.Egress), "140.82.121.4/32")
	assert.Contains(t, cidrs(np.Spec.Egress), "10.5.0.7/32")
	assert.True(t, hasDNSRule(np.Spec.Egress))

	assert.Equal(t, []networkingv1.PolicyType{networkingv1.PolicyTypeEgress}, np.Spec.PolicyTypes)
	assert.Equal(t, map[string]string{"marionette.dev/runner-id": "run_1"}, np.Spec.PodSelector.MatchLabels)
}

func TestBuildNetworkPolicy_AllowListNeverOpensBlockedRanges(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	// A rebinding answer that points at the cloud metadata endpoint.
	np := f.build(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"evil.example.com"},
	})
	require.NotNil(t, np)

	blocks := cidrs(np.Spec.Egress)
	assert.NotContains(t, blocks, "169.254.169.254/32")
	assert.Contains(t, blocks, "8.8.8.8/32")
}

func TestBuildNetworkPolicy_AllowListUsesPolicyPorts(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	np := f.build(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		NetworkPolicy: "allow_list",
		AllowedHosts:  []string{"github.com"},
	})
	require.NotNil(t, np)

	// This used to hand out 22 and 9418 unconditionally, which is a shell and
	// a git daemon nobody asked for.
	var ports []int
	for _, r := range np.Spec.Egress {
		for _, peer := range r.To {
			if peer.IPBlock == nil || peer.IPBlock.CIDR != "140.82.121.4/32" {
				continue
			}
			for _, port := range r.Ports {
				ports = append(ports, port.Port.IntValue())
			}
		}
	}
	assert.ElementsMatch(t, mnet.DefaultAllowedPorts, ports)
}

func TestBuildNetworkPolicy_AllowListWithNothingResolvedDeniesEverything(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	np := f.build(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		NetworkPolicy: "allow_list",
		// Wildcards cannot be expressed as an ipBlock at all.
		AllowedHosts: []string{"*.githubusercontent.com"},
	})
	require.NotNil(t, np)

	// An allow list that matched nothing allows nothing. Only DNS is open.
	assert.Empty(t, cidrs(np.Spec.Egress))
	assert.True(t, hasDNSRule(np.Spec.Egress))
}

func TestBuildNetworkPolicy_AirGappedOpensOnlyTheControlPlane(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	np := f.build(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "air_gapped",
	})
	require.NotNil(t, np)

	require.Len(t, np.Spec.Egress, 1)
	assert.Equal(t, []string{"10.5.0.7/32"}, cidrs(np.Spec.Egress))
	assert.Equal(t, 9090, np.Spec.Egress[0].Ports[0].Port.IntValue())

	// No DNS. The old code appended a DNS rule to an "air-gapped" policy,
	// leaving the pod a working exfiltration channel.
	assert.False(t, hasDNSRule(np.Spec.Egress))
}

func TestBuildNetworkPolicy_AirGappedWithNoControlPlaneDeniesEverything(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	np := f.build(t, provider.SpawnOptions{RunnerID: "run_1", NetworkPolicy: "air_gapped"})
	require.NotNil(t, np)

	// An empty, non-nil egress list is deny-all. A nil one would read as "no
	// egress rules declared", which Kubernetes treats as no restriction.
	require.NotNil(t, np.Spec.Egress)
	assert.Empty(t, np.Spec.Egress)
}

func TestBuildNetworkPolicy_ProxyOpensOnlyTheProxy(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{ProxyURL: "http://proxy.internal:3128"})

	np := f.build(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "proxy",
	})
	require.NotNil(t, np)

	blocks := cidrs(np.Spec.Egress)
	assert.Contains(t, blocks, "203.0.113.9/32")
	assert.Contains(t, blocks, "10.5.0.7/32")

	// Proxy mode used to be a copy of allow_list, which meant it ignored the
	// proxy entirely and opened the allow list instead.
	for _, r := range np.Spec.Egress {
		for _, port := range r.Ports {
			assert.NotEqual(t, 443, port.Port.IntValue(), "direct egress must stay closed")
		}
	}
}

func TestPrepareNetworkPolicy_ProxyRequiresConfiguration(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	_, err := f.provider.prepareNetworkPolicy(provider.SpawnOptions{
		RunnerID:      "run_1",
		NetworkPolicy: "proxy",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "proxy policy requires a configured proxy")
}

func TestPrepareNetworkPolicy_ControlPlaneFallsBackToConfig(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{ServerURL: "marionette.internal"})

	policy, err := f.provider.prepareNetworkPolicy(provider.SpawnOptions{
		RunnerID:      "run_1",
		NetworkPolicy: "air_gapped",
	})
	require.NoError(t, err)
	require.Len(t, policy.ControlPlane, 1)
	assert.Equal(t, mnet.DefaultControlPlanePort, policy.ControlPlane[0].Port)
}

func TestDNSEgressRule_HonoursConfiguredNamespace(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{DNSNamespace: "dns-system"})

	rule := f.provider.dnsEgressRule()
	require.Len(t, rule.To, 1)
	require.NotNil(t, rule.To[0].NamespaceSelector)
	assert.Equal(t, "dns-system", rule.To[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])

	f = newNetworkFixture(t, IsolationConfig{})
	rule = f.provider.dnsEgressRule()
	assert.Equal(t, DefaultDNSNamespace, rule.To[0].NamespaceSelector.MatchLabels["kubernetes.io/metadata.name"])
}

// TestSpawn_NetworkPolicyExistsBeforeThePod is the Kubernetes half of the
// startup-window fix.
//
// A NetworkPolicy selects pods by label, so creating it first means it is
// already in the API server when the pod carrying that label appears. Creating
// it after waitForPodReady, which is what this used to do, left the pod
// running unfiltered for up to two minutes.
func TestSpawn_NetworkPolicyExistsBeforeThePod(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	_, err := f.spawn(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		Name:          "runner-1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "air_gapped",
	})
	require.NoError(t, err)

	require.Len(t, f.client.CreateNetworkPolicyCalls, 1)
	require.Len(t, f.client.CreatePodCalls, 1)
	assert.Less(t, f.client.CreateNetworkPolicyCalls[0].Seq, f.client.CreatePodCalls[0].Seq,
		"the policy must reach the API server before the pod it constrains")
}

func TestSpawn_PodFailureRemovesTheNetworkPolicy(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})
	f.client.CreatePodErr = errors.New("quota exceeded")

	_, err := f.spawn(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "air_gapped",
	})
	require.Error(t, err)

	// Otherwise every failed spawn leaves an orphan policy in the namespace.
	assert.NotEmpty(t, f.client.DeleteNetworkPolicyCalls)
}

func TestSpawn_UnrestrictedPathCreatesNoPolicy(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	_, err := f.spawn(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		NetworkPolicy: "none",
	})
	require.NoError(t, err)
	assert.Empty(t, f.client.CreateNetworkPolicyCalls)
}

func TestSpawn_AirGappedPinsTheServerIntoHostAliases(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{})

	_, err := f.spawn(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "air_gapped",
	})
	require.NoError(t, err)

	require.Len(t, f.client.CreatePodCalls, 1)
	aliases := f.client.CreatePodCalls[0].Pod.Spec.HostAliases
	require.Len(t, aliases, 1)
	assert.Equal(t, "10.5.0.7", aliases[0].IP)
	assert.Equal(t, []string{"marionette.internal"}, aliases[0].Hostnames)
}

func TestSpawn_ProxyModeInjectsTheProxyEnvironment(t *testing.T) {
	f := newNetworkFixture(t, IsolationConfig{
		ProxyURL:    "http://proxy.internal:3128",
		ProxyCACert: "/etc/marionette/proxy-ca.crt",
	})

	_, err := f.spawn(t, provider.SpawnOptions{
		RunnerID:      "run_1",
		ServerURL:     "marionette.internal:9090",
		NetworkPolicy: "proxy",
	})
	require.NoError(t, err)

	require.Len(t, f.client.CreatePodCalls, 1)
	env := map[string]string{}
	for _, e := range f.client.CreatePodCalls[0].Pod.Spec.Containers[0].Env {
		env[e.Name] = e.Value
	}

	assert.Equal(t, "http://proxy.internal:3128", env["HTTPS_PROXY"])
	assert.Equal(t, "/etc/marionette/proxy-ca.crt", env["NODE_EXTRA_CA_CERTS"])
	assert.Contains(t, env["NO_PROXY"], "marionette.internal")
}

func TestControlPlaneHostAliases(t *testing.T) {
	assert.Nil(t, controlPlaneHostAliases(nil))

	resolved := &mnet.ResolvedPolicy{
		ControlPlane: []mnet.EndpointResolution{
			{Endpoint: mnet.Endpoint{Host: "marionette.internal", Port: 9090}, IPs: []net.IP{net.ParseIP("10.5.0.7")}},
			// An IP literal needs no alias, and a name that did not resolve
			// cannot be pinned.
			{Endpoint: mnet.Endpoint{Host: "10.5.0.8", Port: 9090}, IPs: []net.IP{net.ParseIP("10.5.0.8")}},
			{Endpoint: mnet.Endpoint{Host: "unresolved.internal", Port: 9090}},
		},
	}

	aliases := controlPlaneHostAliases(resolved)
	require.Len(t, aliases, 1)
	assert.Equal(t, []string{"marionette.internal"}, aliases[0].Hostnames)
}

func TestProxyEnvVars_IsDeterministic(t *testing.T) {
	assert.Nil(t, proxyEnvVars(nil))
	assert.Nil(t, proxyEnvVars(&mnet.ResolvedPolicy{}))

	proxy, err := mnet.ParseProxyConfig("http://proxy.internal:3128", nil, "")
	require.NoError(t, err)
	policy, err := mnet.ParsePolicy("proxy", nil, mnet.WithProxy(proxy))
	require.NoError(t, err)

	resolved := mnet.NewResolvedPolicy(policy, nil, 0)
	first := proxyEnvVars(resolved)
	second := proxyEnvVars(resolved)

	// Two spawns of the same session must produce identical pod specs.
	assert.Equal(t, first, second)
	assert.IsIncreasing(t, envNames(first))
}

func envNames(vars []corev1.EnvVar) []string {
	out := make([]string, 0, len(vars))
	for _, v := range vars {
		out = append(out, v.Name)
	}
	return out
}
