package network

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffIPs(t *testing.T) {
	ip := func(s string) net.IP { return net.ParseIP(s) }

	tests := []struct {
		name        string
		prev, next  []net.IP
		wantAdded   []string
		wantRemoved []string
	}{
		{
			name:      "from nothing",
			next:      []net.IP{ip("1.1.1.1")},
			wantAdded: []string{"1.1.1.1"},
		},
		{
			name:        "to nothing",
			prev:        []net.IP{ip("1.1.1.1")},
			wantRemoved: []string{"1.1.1.1"},
		},
		{
			name: "identical",
			prev: []net.IP{ip("1.1.1.1"), ip("2.2.2.2")},
			next: []net.IP{ip("2.2.2.2"), ip("1.1.1.1")},
		},
		{
			name:        "rotation",
			prev:        []net.IP{ip("1.1.1.1"), ip("2.2.2.2")},
			next:        []net.IP{ip("2.2.2.2"), ip("3.3.3.3")},
			wantAdded:   []string{"3.3.3.3"},
			wantRemoved: []string{"1.1.1.1"},
		},
		{
			name:      "ipv4 and ipv6 are distinct",
			prev:      []net.IP{ip("1.1.1.1")},
			next:      []net.IP{ip("1.1.1.1"), ip("2001:db8::1")},
			wantAdded: []string{"2001:db8::1"},
		},
		{
			name: "ipv4 in v6 form is the same address",
			prev: []net.IP{ip("1.1.1.1")},
			next: []net.IP{ip("::ffff:1.1.1.1")},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, removed := DiffIPs(tt.prev, tt.next)
			assert.Equal(t, tt.wantAdded, ipStrings(added))
			assert.Equal(t, tt.wantRemoved, ipStrings(removed))
		})
	}
}

func TestDiffIPs_IsSorted(t *testing.T) {
	next := []net.IP{net.ParseIP("9.9.9.9"), net.ParseIP("1.1.1.1"), net.ParseIP("5.5.5.5")}
	added, _ := DiffIPs(nil, next)
	assert.Equal(t, []string{"1.1.1.1", "5.5.5.5", "9.9.9.9"}, ipStrings(added),
		"rule order must be reproducible across resolutions")
}

func TestResolvedPolicy_AllIPsFilteredDedupes(t *testing.T) {
	policy, err := ParsePolicy("allow_list", []string{"a.example.com", "b.example.com"})
	require.NoError(t, err)

	resolved := NewResolvedPolicy(policy, []HostResolution{
		{Pattern: "a.example.com", IPs: []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("10.0.0.1")}},
		{Pattern: "b.example.com", IPs: []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("2.2.2.2")}},
	}, time.Minute)

	// 10.0.0.1 is a blocked private range; 1.1.1.1 appears under two patterns.
	assert.Equal(t, []string{"1.1.1.1", "2.2.2.2"}, ipStrings(resolved.AllIPsFiltered()))
}

func TestResolvedPolicy_ControlPlanePinning(t *testing.T) {
	mock := NewMockResolver()
	mock.SetResult("marionette.internal", parseIPsForTest(t, []string{"10.5.0.7"}))
	resolver := NewDNSResolver(WithResolver(mock))

	ep, err := ParseEndpoint("marionette.internal:9090", DefaultControlPlanePort)
	require.NoError(t, err)

	policy, err := ParsePolicy("air_gapped", nil, WithControlPlane(ep))
	require.NoError(t, err)

	resolved, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)

	assert.Equal(t, []string{"10.5.0.7"}, ipStrings(resolved.ControlPlaneIPs()))

	// The control plane is normally on a private network, which is a blocked
	// CIDR. The operator's pin has to win over the blanket block.
	assert.True(t, resolved.IsIPAllowed(net.ParseIP("10.5.0.7")))
	assert.True(t, resolved.IsConnectionAllowed(net.ParseIP("10.5.0.7"), 9090))

	// Air-gapped means air-gapped: nothing else, not even on the same port.
	assert.False(t, resolved.IsConnectionAllowed(net.ParseIP("8.8.8.8"), 9090))
	assert.False(t, resolved.IsConnectionAllowed(net.ParseIP("10.5.0.7"), 443))

	// No external DNS is permitted in air-gapped mode.
	assert.Empty(t, resolved.DNSServers)
}

func TestResolvedPolicy_IPLiteralControlPlaneSkipsLookup(t *testing.T) {
	mock := NewMockResolver()
	resolver := NewDNSResolver(WithResolver(mock))

	er := resolver.ResolveEndpoint(context.Background(), Endpoint{Host: "10.5.0.7", Port: 9090})
	require.NoError(t, er.Error)
	assert.Equal(t, []string{"10.5.0.7"}, ipStrings(er.IPs))
	assert.Empty(t, mock.GetCalls(), "IP literals must not hit DNS")
}

func TestResolvedPolicy_ProxyPinning(t *testing.T) {
	mock := NewMockResolver()
	mock.SetResult("proxy.internal", parseIPsForTest(t, []string{"203.0.113.9"}))
	mock.SetResult("marionette.internal", parseIPsForTest(t, []string{"10.5.0.7"}))
	resolver := NewDNSResolver(WithResolver(mock))

	proxy, err := ParseProxyConfig("http://proxy.internal:3128", nil, "")
	require.NoError(t, err)
	ep, err := ParseEndpoint("marionette.internal:9090", DefaultControlPlanePort)
	require.NoError(t, err)

	policy, err := ParsePolicy("proxy", nil, WithProxy(proxy), WithControlPlane(ep), WithDNSServers("10.5.0.53"))
	require.NoError(t, err)

	resolved, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)

	require.NotNil(t, resolved.Proxy)
	assert.Equal(t, []string{"203.0.113.9"}, ipStrings(resolved.ProxyIPs()))
	assert.Equal(t, 3128, resolved.Proxy.Endpoint.Port)

	assert.True(t, resolved.IsConnectionAllowed(net.ParseIP("203.0.113.9"), 3128))
	assert.True(t, resolved.IsConnectionAllowed(net.ParseIP("10.5.0.7"), 9090))

	// Direct egress is denied even on ordinary web ports: that is what makes a
	// tool which ignores HTTPS_PROXY fail closed instead of escaping.
	assert.False(t, resolved.IsConnectionAllowed(net.ParseIP("93.184.216.34"), 443))
	assert.False(t, resolved.IsConnectionAllowed(net.ParseIP("203.0.113.9"), 443))

	assert.Equal(t, []string{"10.5.0.53"}, ipStrings(resolved.DNSServers))
}

func TestResolvedPolicy_ProxyResolutionErrorIsReported(t *testing.T) {
	mock := NewMockResolver()
	mock.SetError("proxy.internal", errors.New("NXDOMAIN"))
	resolver := NewDNSResolver(WithResolver(mock))

	proxy, err := ParseProxyConfig("http://proxy.internal:3128", nil, "")
	require.NoError(t, err)
	policy, err := ParsePolicy("proxy", nil, WithProxy(proxy))
	require.NoError(t, err)

	resolved, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)

	assert.True(t, resolved.HasErrors())
	require.Len(t, resolved.Errors(), 1)
	assert.Contains(t, resolved.Errors()[0].Error(), "NXDOMAIN")
}

func TestResolvedPolicy_ControlPlaneResolutionErrorIsReported(t *testing.T) {
	mock := NewMockResolver()
	mock.SetError("marionette.internal", errors.New("SERVFAIL"))
	resolver := NewDNSResolver(WithResolver(mock))

	ep, err := ParseEndpoint("marionette.internal:9090", DefaultControlPlanePort)
	require.NoError(t, err)
	policy, err := ParsePolicy("air_gapped", nil, WithControlPlane(ep))
	require.NoError(t, err)

	resolved, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)
	assert.True(t, resolved.HasErrors())
}

func TestResolvedPolicy_UnenforceableHostPatterns(t *testing.T) {
	mock := NewMockResolver()
	mock.SetResult("github.com", parseIPsForTest(t, []string{"140.82.121.4"}))
	resolver := NewDNSResolver(WithResolver(mock))

	policy, err := ParsePolicy("allow_list", []string{"github.com", "*.githubusercontent.com"})
	require.NoError(t, err)

	resolved, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)

	// A packet filter has no address set to pin for a wildcard, so it enforces
	// nothing for it. Callers must surface that rather than imply coverage.
	assert.Equal(t, []string{"*.githubusercontent.com"}, resolved.UnenforceableHostPatterns())
	assert.Equal(t, []string{"140.82.121.4"}, ipStrings(resolved.AllIPsFiltered()))
}

func TestResolvePolicyFresh_BypassesTheCache(t *testing.T) {
	mock := NewMockResolver()
	mock.SetResult("cdn.example.com", parseIPsForTest(t, []string{"1.1.1.1"}))

	// A long cache TTL would otherwise serve the refresher the very entry it
	// is trying to replace.
	resolver := NewDNSResolver(WithResolver(mock), WithCacheTTL(time.Hour))
	policy, err := ParsePolicy("allow_list", []string{"cdn.example.com"})
	require.NoError(t, err)

	first, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, []string{"1.1.1.1"}, ipStrings(first.AllIPsFiltered()))

	mock.SetResult("cdn.example.com", parseIPsForTest(t, []string{"2.2.2.2"}))

	cached, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, []string{"1.1.1.1"}, ipStrings(cached.AllIPsFiltered()), "cache should still serve the old answer")

	fresh, err := resolver.ResolvePolicyFresh(context.Background(), policy)
	require.NoError(t, err)
	assert.Equal(t, []string{"2.2.2.2"}, ipStrings(fresh.AllIPsFiltered()))
}

func TestResolvePolicyFresh_NilPolicy(t *testing.T) {
	_, err := NewDNSResolver().ResolvePolicyFresh(context.Background(), nil)
	assert.ErrorContains(t, err, "policy is nil")
}

func TestResolvedPolicy_TTLAndSummary(t *testing.T) {
	policy, err := ParsePolicy("allow_list", []string{"a.example.com"})
	require.NoError(t, err)

	resolved := NewResolvedPolicy(policy, []HostResolution{
		{Pattern: "a.example.com", IPs: []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("10.0.0.1")}},
	}, 2*time.Minute)
	resolved.DNSServers = []net.IP{net.ParseIP("10.0.0.53")}

	assert.Equal(t, 2*time.Minute, resolved.TTL())
	assert.False(t, resolved.IsExpired())

	summary := resolved.Summary()
	assert.Equal(t, PolicyAllowList, summary["level"])
	assert.Equal(t, 2, summary["resolved_ips"])
	assert.Equal(t, 1, summary["allowed_ips"])
	assert.Equal(t, 1, summary["blocked_ips"])
	assert.Equal(t, 1, summary["dns_servers"])
	assert.Equal(t, false, summary["has_errors"])
	assert.NotContains(t, summary, "proxy")
}

func TestResolvedPolicy_LevelOnNilIsNone(t *testing.T) {
	var resolved *ResolvedPolicy
	assert.Equal(t, PolicyNone, resolved.Level())
	assert.Equal(t, PolicyNone, (&ResolvedPolicy{}).Level())
}

func TestParseIPList(t *testing.T) {
	got := parseIPList([]string{"10.0.0.53", "10.0.0.54:53", "not-an-ip", "", "2001:db8::53"})
	assert.Equal(t, []string{"10.0.0.53", "10.0.0.54", "2001:db8::53"}, ipStrings(got))
}

func TestResolvedPolicy_CIDRPatterns(t *testing.T) {
	mock := NewMockResolver()
	mock.SetResult("github.com", parseIPsForTest(t, []string{"140.82.121.4"}))
	resolver := NewDNSResolver(WithResolver(mock))

	policy, err := ParsePolicy("allow_list", []string{
		"203.0.113.0/24",
		"2001:db8::/32",
		"github.com",
	})
	require.NoError(t, err)

	resolved, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)

	var blocks []string
	for _, c := range resolved.AllowedCIDRs() {
		blocks = append(blocks, c.String())
	}
	assert.Equal(t, []string{"203.0.113.0/24", "2001:db8::/32"}, blocks)

	// A block is not a lookup: nothing was resolved for it.
	assert.Equal(t, []string{"140.82.121.4"}, ipStrings(resolved.AllIPsFiltered()))
	assert.Empty(t, resolved.UnenforceableHostPatterns())
	assert.False(t, resolved.HasErrors())
	assert.Empty(t, mock.GetCalls()[1:], "only the hostname should have been looked up")
}

func TestResolvedPolicy_CIDRsOverlappingBlockedRangesAreDropped(t *testing.T) {
	resolver := NewDNSResolver(WithResolver(NewMockResolver()))

	policy, err := ParsePolicy("allow_list", []string{
		"169.254.0.0/16", // contains the cloud metadata endpoint
		"10.0.0.0/8",     // a blocked private range verbatim
		"192.168.1.0/24", // inside a blocked private range
		"203.0.113.0/24", // fine
	})
	require.NoError(t, err)

	resolved, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)

	var allowed []string
	for _, c := range resolved.AllowedCIDRs() {
		allowed = append(allowed, c.String())
	}
	assert.Equal(t, []string{"203.0.113.0/24"}, allowed)

	// The dropped blocks are reported rather than silently ignored: an
	// operator who wrote 169.254.0.0/16 needs to know it did not take effect.
	var rejected []string
	for _, c := range resolved.RejectedCIDRs() {
		rejected = append(rejected, c.String())
	}
	assert.Equal(t, []string{"169.254.0.0/16", "10.0.0.0/8", "192.168.1.0/24"}, rejected)
}

func TestResolvedPolicy_IPLiteralPatternNeedsNoCIDR(t *testing.T) {
	mock := NewMockResolver()
	mock.SetResult("10.0.0.1", parseIPsForTest(t, []string{"10.0.0.1"}))
	mock.SetResult("203.0.113.5", parseIPsForTest(t, []string{"203.0.113.5"}))
	resolver := NewDNSResolver(WithResolver(mock))

	policy, err := ParsePolicy("allow_list", []string{"10.0.0.1", "203.0.113.5"})
	require.NoError(t, err)

	resolved, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)

	// 10.0.0.1 is inside a blocked private range and is filtered out.
	assert.Equal(t, []string{"203.0.113.5"}, ipStrings(resolved.AllIPsFiltered()))
	assert.Empty(t, resolved.AllowedCIDRs())
}
