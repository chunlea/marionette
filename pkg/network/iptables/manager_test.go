package iptables

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/network"
)

// joined renders recorded commands as strings for readable assertions.
func joined(cmds [][]string) []string {
	out := make([]string, 0, len(cmds))
	for _, c := range cmds {
		out = append(out, strings.Join(c, " "))
	}
	return out
}

// indexOf returns the position of the first command containing every fragment.
func indexOf(t *testing.T, cmds []string, fragments ...string) int {
	t.Helper()
	for i, c := range cmds {
		matched := true
		for _, f := range fragments {
			if !strings.Contains(c, f) {
				matched = false
				break
			}
		}
		if matched {
			return i
		}
	}
	t.Fatalf("no command matching %v in:\n  %s", fragments, strings.Join(cmds, "\n  "))
	return -1
}

func mustIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	require.NotNil(t, ip, "bad IP %q", s)
	return ip
}

// allowListPolicy builds a resolved allow_list policy with a control plane.
func allowListPolicy(t *testing.T, ips ...string) *network.ResolvedPolicy {
	t.Helper()

	ep, err := network.ParseEndpoint("10.5.0.7:9090", network.DefaultControlPlanePort)
	require.NoError(t, err)

	policy, err := network.ParsePolicy("allow_list", []string{"github.com"},
		network.WithControlPlane(ep),
		network.WithDNSServers("10.5.0.53"),
	)
	require.NoError(t, err)

	parsed := make([]net.IP, 0, len(ips))
	for _, s := range ips {
		parsed = append(parsed, mustIP(t, s))
	}

	resolved := network.NewResolvedPolicy(policy, []network.HostResolution{
		{Pattern: "github.com", IPs: parsed},
	}, time.Minute)
	resolved.ControlPlane = []network.EndpointResolution{
		{Endpoint: ep, IPs: []net.IP{mustIP(t, "10.5.0.7")}},
	}
	resolved.DNSServers = []net.IP{mustIP(t, "10.5.0.53")}

	return resolved
}

func TestNewManager(t *testing.T) {
	m := NewManager(NewMockExecutor())
	assert.NotNil(t, m)
	assert.Equal(t, DefaultChainPrefix, m.chainPrefix)
	assert.Empty(t, m.ListActiveChains())
}

func TestManager_ChainName(t *testing.T) {
	m := NewManager(NewMockExecutor())

	tests := []struct {
		key      string
		expected string
	}{
		{"sess_abc123", "MARIONETTE_abc123"},
		{"abc123", "MARIONETTE_abc123"},
		// Truncated so the dynamic chain's suffix still fits in 28 characters.
		{"sess_verylongsessionidthatneedstruncation", "MARIONETTE_verylongsession"},
		// iptables chain names are alphanumeric plus underscore.
		{"run-1.2:3", "MARIONETTE_run_1_2_3"},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			name := m.ChainName(tt.key)
			assert.Equal(t, tt.expected, name)
			assert.LessOrEqual(t, len(m.DynChainName(tt.key)), MaxChainNameLength,
				"the dynamic chain must fit too")
			require.NoError(t, ValidateChainName(m.DynChainName(tt.key)))
		})
	}
}

func TestManager_BuildRuleSets_Ordering(t *testing.T) {
	m := NewManager(NewMockExecutor())
	resolved := allowListPolicy(t, "140.82.121.4")

	main, dyn := m.BuildRuleSets("run_1", resolved)
	rules := make([]string, 0, len(main.Rules))
	for i := range main.Rules {
		rules = append(rules, strings.Join(main.Rules[i].ToArgs(), " "))
	}

	loopback := indexOf(t, rules, "-o lo")
	established := indexOf(t, rules, "--ctstate ESTABLISHED,RELATED")
	controlPlane := indexOf(t, rules, "-d 10.5.0.7", "--dport 9090")
	resolver := indexOf(t, rules, "-d 10.5.0.53", "--dport 53", "-p udp")
	blocked := indexOf(t, rules, "-d 169.254.169.254/32", "DROP")
	privateBlock := indexOf(t, rules, "-d 10.0.0.0/8", "DROP")
	jump := indexOf(t, rules, "-j "+m.DynChainName("run_1"))
	drop := len(rules) - 1

	assert.Less(t, loopback, established)

	// The pins have to precede the blanket blocks: a server and a resolver on
	// 10.0.0.0/8 are the normal deployment, and 10.0.0.0/8 is blocked.
	assert.Less(t, controlPlane, blocked, "control plane pin must beat the private-range block")
	assert.Less(t, resolver, privateBlock, "resolver pin must beat the private-range block")

	// The allow list is only consulted after the blocks, so a rebinding answer
	// pointing at metadata cannot be opened by a refresh.
	assert.Less(t, blocked, jump)
	assert.Less(t, jump, drop)
	assert.Contains(t, rules[drop], "-j DROP")
	assert.NotContains(t, rules[drop], "-d ")

	// The refreshable rules live in the dynamic chain, not the main one.
	require.Len(t, dyn.Rules, 2) // one IP x two default ports
	assert.Contains(t, strings.Join(dyn.Rules[0].ToArgs(), " "), "-d 140.82.121.4")
	assert.Contains(t, strings.Join(dyn.Rules[0].ToArgs(), " "), dyn.ChainName)
}

func TestManager_BuildRuleSets_StructuralRulesExistInBothFamilies(t *testing.T) {
	m := NewManager(NewMockExecutor())
	main, _ := m.BuildRuleSets("run_1", allowListPolicy(t, "140.82.121.4"))

	v6 := make([]string, 0, len(main.IPv6Rules))
	for i := range main.IPv6Rules {
		v6 = append(v6, strings.Join(main.IPv6Rules[i].ToArgs(), " "))
	}

	// An IPv6-capable container behind an IPv4-only chain has no policy at all.
	indexOf(t, v6, "-o lo")
	indexOf(t, v6, "--ctstate ESTABLISHED,RELATED")
	indexOf(t, v6, "-j "+m.DynChainName("run_1"))
	assert.Contains(t, v6[len(v6)-1], "-j DROP")
}

func TestManager_BuildRuleSets_AirGappedHasNoAllowListAndNoDNS(t *testing.T) {
	ep, err := network.ParseEndpoint("10.5.0.7:9090", network.DefaultControlPlanePort)
	require.NoError(t, err)
	policy, err := network.ParsePolicy("air_gapped", nil, network.WithControlPlane(ep))
	require.NoError(t, err)

	resolved := network.NewResolvedPolicy(policy, nil, time.Minute)
	resolved.ControlPlane = []network.EndpointResolution{
		{Endpoint: ep, IPs: []net.IP{mustIP(t, "10.5.0.7")}},
	}

	m := NewManager(NewMockExecutor())
	main, dyn := m.BuildRuleSets("run_1", resolved)

	rules := joined(rulesToArgs(main.Rules))
	indexOf(t, rules, "-d 10.5.0.7", "--dport 9090")

	// No DNS at any address: it is the exfiltration channel air-gapped closes.
	for _, r := range append(rules, joined(rulesToArgs(main.IPv6Rules))...) {
		assert.NotContains(t, r, "--dport 53", "air-gapped must not open DNS")
	}

	assert.Empty(t, dyn.Rules)
	assert.Empty(t, dyn.IPv6Rules)
}

func TestManager_BuildRuleSets_ProxyOpensOnlyTheProxy(t *testing.T) {
	proxy, err := network.ParseProxyConfig("http://proxy.internal:3128", nil, "")
	require.NoError(t, err)
	policy, err := network.ParsePolicy("proxy", nil, network.WithProxy(proxy))
	require.NoError(t, err)

	resolved := network.NewResolvedPolicy(policy, nil, time.Minute)
	resolved.Proxy = &network.EndpointResolution{
		Endpoint: network.Endpoint{Host: "proxy.internal", Port: 3128},
		IPs:      []net.IP{mustIP(t, "203.0.113.9")},
	}

	m := NewManager(NewMockExecutor())
	main, dyn := m.BuildRuleSets("run_1", resolved)

	rules := joined(rulesToArgs(main.Rules))
	indexOf(t, rules, "-d 203.0.113.9", "--dport 3128")

	// Nothing opens 443 directly: a tool that ignores HTTPS_PROXY must fail,
	// not slip past the policy.
	for _, r := range rules {
		assert.NotContains(t, r, "--dport 443")
	}
	assert.Empty(t, dyn.Rules)
}

func TestManager_BuildRuleSets_DNSFallbackOnlyWithoutAPinnedResolver(t *testing.T) {
	policy, err := network.ParsePolicy("allow_list", []string{"github.com"})
	require.NoError(t, err)
	resolved := network.NewResolvedPolicy(policy, nil, time.Minute)

	m := NewManager(NewMockExecutor())
	main, _ := m.BuildRuleSets("run_1", resolved)
	rules := joined(rulesToArgs(main.Rules))

	// Without a resolver address, name resolution has to be allowed broadly or
	// nothing in the sandbox works at all. It is a documented widening.
	i := indexOf(t, rules, "--dport 53", "-p udp")
	assert.NotContains(t, rules[i], "-d ")

	// With a resolver pinned, the broad rule disappears.
	main, _ = m.BuildRuleSets("run_1", allowListPolicy(t, "140.82.121.4"))
	for _, r := range joined(rulesToArgs(main.Rules)) {
		if strings.Contains(r, "--dport 53") {
			assert.Contains(t, r, "-d 10.5.0.53")
		}
	}
}

func TestManager_BuildRuleSets_BlockedIPsNeverReachTheAllowChain(t *testing.T) {
	m := NewManager(NewMockExecutor())
	// A rebinding answer that points at the cloud metadata endpoint.
	_, dyn := m.BuildRuleSets("run_1", allowListPolicy(t, "169.254.169.254", "140.82.121.4"))

	for _, r := range joined(rulesToArgs(dyn.Rules)) {
		assert.NotContains(t, r, "169.254.169.254")
	}
	assert.Len(t, dyn.Rules, 2)
}

func rulesToArgs(rules []Rule) [][]string {
	out := make([][]string, 0, len(rules))
	for i := range rules {
		out = append(out, rules[i].ToArgs())
	}
	return out
}

func TestManager_Install(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	require.NoError(t, m.Install(context.Background(), "run_1", allowListPolicy(t, "140.82.121.4")))

	cmds := joined(mock.GetCommands())
	chain := m.ChainName("run_1")
	dyn := m.DynChainName("run_1")

	// Both chains are created before the main chain jumps to the dynamic one.
	createMain := indexOf(t, cmds, "-N "+chain)
	createDyn := indexOf(t, cmds, "-N "+dyn)
	jump := indexOf(t, cmds, "-A "+chain, "-j "+dyn)
	assert.Less(t, createDyn, jump)
	assert.Less(t, createMain, jump)

	// Flushing first is what makes a re-install converge instead of stacking.
	assert.Less(t, indexOf(t, cmds, "-F "+chain), indexOf(t, cmds, "-A "+chain))

	// The chain is useless until OUTPUT jumps to it, and that has to be last.
	link := indexOf(t, cmds, "-I OUTPUT", "-j "+chain)
	assert.Greater(t, link, indexOf(t, cmds, "-A "+chain, "-j DROP"))

	assert.Equal(t, []string{chain}, m.ListActiveChains())
}

func TestManager_Install_IsIdempotent(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)
	policy := allowListPolicy(t, "140.82.121.4")

	require.NoError(t, m.Install(context.Background(), "run_1", policy))

	// Second install: the OUTPUT jump already exists, so it must not be added
	// a second time. Duplicated jumps leak on every session resume.
	chain := m.ChainName("run_1")
	mock.SetCheckOK([]string{"-C", "OUTPUT", "-j", chain})
	mock.SetIPv6CheckOK([]string{"-C", "OUTPUT", "-j", chain})
	mock.Reset()
	mock.SetCheckOK([]string{"-C", "OUTPUT", "-j", chain})
	mock.SetIPv6CheckOK([]string{"-C", "OUTPUT", "-j", chain})

	require.NoError(t, m.Install(context.Background(), "run_1", policy))

	for _, c := range joined(mock.GetCommands()) {
		assert.NotContains(t, c, "-I OUTPUT")
	}
}

func TestManager_Install_NilPolicy(t *testing.T) {
	m := NewManager(NewMockExecutor())
	assert.ErrorContains(t, m.Install(context.Background(), "run_1", nil), "policy is nil")
}

func TestManager_Install_PropagatesFailures(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)
	mock.SetError([]string{"-N", m.ChainName("run_1")}, errors.New("permission denied"))

	err := m.Install(context.Background(), "run_1", allowListPolicy(t, "140.82.121.4"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create chain")
}

func TestManager_Install_ToleratesExistingChain(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)
	mock.SetError([]string{"-N", m.ChainName("run_1")}, errors.New("iptables: Chain already exists."))

	require.NoError(t, m.Install(context.Background(), "run_1", allowListPolicy(t, "140.82.121.4")))
}

func TestManager_Uninstall(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)
	require.NoError(t, m.Install(context.Background(), "run_1", allowListPolicy(t, "140.82.121.4")))

	mock.Reset()
	require.NoError(t, m.Uninstall(context.Background(), "run_1"))

	cmds := joined(mock.GetCommands())
	chain := m.ChainName("run_1")
	dyn := m.DynChainName("run_1")

	// Unlink before delete: iptables refuses to delete a referenced chain.
	assert.Less(t, indexOf(t, cmds, "-D OUTPUT", "-j "+chain), indexOf(t, cmds, "-X "+chain))
	indexOf(t, cmds, "-X "+dyn)
	assert.Empty(t, m.ListActiveChains())
}

func TestManager_Uninstall_ToleratesMissingRules(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)
	chain := m.ChainName("run_1")

	mock.SetError([]string{"-D", "OUTPUT", "-j", chain}, ErrMockRuleMissing)
	mock.SetError([]string{"-X", chain}, errors.New("iptables: No chain/target/match by that name."))

	require.NoError(t, m.Uninstall(context.Background(), "run_1"))
}

func TestManager_Uninstall_ReportsRealFailures(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)
	mock.SetError([]string{"-X", m.ChainName("run_1")}, errors.New("resource busy"))

	err := m.Uninstall(context.Background(), "run_1")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete chain")
}

func TestManager_Installed(t *testing.T) {
	chain := NewManager(NewMockExecutor()).ChainName("run_1")

	tests := []struct {
		name    string
		setup   func(*MockExecutor)
		want    bool
		wantErr string
	}{
		{
			name: "chain and jump present",
			setup: func(mock *MockExecutor) {
				mock.SetCheckOK([]string{"-C", "OUTPUT", "-j", chain})
			},
			want: true,
		},
		{
			name: "chain gone after a container restart",
			setup: func(mock *MockExecutor) {
				mock.SetError([]string{"-S", chain}, errors.New("iptables: No chain/target/match by that name."))
			},
			want: false,
		},
		{
			name:  "chain present but nothing jumps to it",
			setup: func(*MockExecutor) {},
			want:  false,
		},
		{
			name: "namespace unreachable",
			setup: func(mock *MockExecutor) {
				mock.SetError([]string{"-S", chain}, errors.New("nsenter: cannot open /proc/1/ns/net"))
			},
			wantErr: "listing chain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := NewMockExecutor()
			m := NewManager(mock)
			tt.setup(mock)

			got, err := m.Installed(context.Background(), "run_1")
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestManager_AllowAppendsToTheDynamicChain(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	ips := []net.IP{mustIP(t, "1.1.1.1"), mustIP(t, "2001:db8::1")}
	require.NoError(t, m.Allow(context.Background(), "run_1", ips, []int{443}))

	dyn := m.DynChainName("run_1")
	v4 := joined(mock.GetCommands())
	v6 := joined(mock.GetIPv6Commands())

	// Appends, never inserts: an insert at position 1 of the main chain would
	// land ahead of the metadata block. The dynamic chain has no such hazard.
	indexOf(t, v4, "-A "+dyn, "-d 1.1.1.1", "--dport 443")
	indexOf(t, v6, "-A "+dyn, "-d 2001:db8::1", "--dport 443")

	for _, c := range append(v4, v6...) {
		assert.NotContains(t, c, "-I ")
	}
}

func TestManager_AllowIsIdempotent(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	ip := mustIP(t, "1.1.1.1")
	rule := allowRules(m.DynChainName("run_1"), []net.IP{ip}, []int{443})[0]
	mock.SetCheckOK(rule.Args("-C"))

	// The refresher re-issues Allow for addresses it believes are already open
	// whenever a previous Deny failed. That must not stack duplicates.
	require.NoError(t, m.Allow(context.Background(), "run_1", []net.IP{ip}, []int{443}))

	for _, c := range joined(mock.GetCommands()) {
		assert.NotContains(t, c, "-A ")
	}
}

func TestManager_AllowSkipsBlockedIPs(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	ips := []net.IP{mustIP(t, "169.254.169.254"), mustIP(t, "127.0.0.1"), mustIP(t, "1.1.1.1")}
	require.NoError(t, m.Allow(context.Background(), "run_1", ips, []int{443}))

	cmds := joined(mock.GetCommands())
	for _, c := range cmds {
		assert.NotContains(t, c, "169.254.169.254")
		assert.NotContains(t, c, "127.0.0.1")
	}
	indexOf(t, cmds, "-d 1.1.1.1")
}

func TestManager_AllowPropagatesFailure(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	ip := mustIP(t, "1.1.1.1")
	rule := allowRules(m.DynChainName("run_1"), []net.IP{ip}, []int{443})[0]
	mock.SetError(rule.Args("-A"), errors.New("table is locked"))

	err := m.Allow(context.Background(), "run_1", []net.IP{ip}, []int{443})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "adding IPv4 rule")
}

func TestManager_Deny(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	require.NoError(t, m.Deny(context.Background(), "run_1", []net.IP{mustIP(t, "1.1.1.1")}, []int{443}))
	indexOf(t, joined(mock.GetCommands()), "-D "+m.DynChainName("run_1"), "-d 1.1.1.1")
}

func TestManager_DenyToleratesAlreadyGoneRules(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	ip := mustIP(t, "1.1.1.1")
	rule := allowRules(m.DynChainName("run_1"), []net.IP{ip}, []int{443})[0]
	mock.SetError(rule.Args("-D"), ErrMockRuleMissing)

	require.NoError(t, m.Deny(context.Background(), "run_1", []net.IP{ip}, []int{443}))
}

func TestManager_DenyReportsRealFailures(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	ip := mustIP(t, "1.1.1.1")
	rule := allowRules(m.DynChainName("run_1"), []net.IP{ip}, []int{443})[0]
	mock.SetError(rule.Args("-D"), errors.New("table is locked"))

	err := m.Deny(context.Background(), "run_1", []net.IP{ip}, []int{443})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "removing IPv4 rule")
}

func TestManager_AllowAndDenyRenderIdenticalRules(t *testing.T) {
	m := NewManager(NewMockExecutor())
	rules := allowRules(m.DynChainName("run_1"), []net.IP{mustIP(t, "1.1.1.1")}, []int{443})
	require.Len(t, rules, 1)

	// iptables matches -D against the exact append form, comment included.
	add := rules[0].Args("-A")
	del := rules[0].Args("-D")
	assert.Equal(t, add[1:], del[1:])
}

func TestManager_ChainExists(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	exists, err := m.ChainExists(context.Background(), "run_1")
	require.NoError(t, err)
	assert.True(t, exists)

	mock.SetError([]string{"-S", m.ChainName("run_1")}, errors.New("iptables: No chain/target/match by that name."))
	exists, err = m.ChainExists(context.Background(), "run_1")
	require.NoError(t, err)
	assert.False(t, exists)

	mock.SetError([]string{"-S", m.ChainName("run_1")}, errors.New("kernel module missing"))
	_, err = m.ChainExists(context.Background(), "run_1")
	assert.Error(t, err)
}

func TestManager_LinkChainToOutput(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	require.NoError(t, m.LinkChainToOutput(context.Background(), "run_1"))

	chain := m.ChainName("run_1")
	// Inserted at the head so the policy runs before any pre-existing rule.
	indexOf(t, joined(mock.GetCommands()), "-I OUTPUT", "-j "+chain)
	indexOf(t, joined(mock.GetIPv6Commands()), "-I OUTPUT", "-j "+chain)
}

func TestManager_UnlinkChainFromOutput(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	require.NoError(t, m.UnlinkChainFromOutput(context.Background(), "run_1"))
	indexOf(t, joined(mock.GetCommands()), "-D OUTPUT", "-j "+m.ChainName("run_1"))
}

func TestManager_CleanupAllChains(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	policy := allowListPolicy(t, "140.82.121.4")
	require.NoError(t, m.Install(context.Background(), "run_1", policy))
	require.NoError(t, m.Install(context.Background(), "run_2", policy))
	require.Len(t, m.ListActiveChains(), 2)

	require.NoError(t, m.CleanupAllChains(context.Background()))
	assert.Empty(t, m.ListActiveChains())
}

func TestCreateBlockedCIDRs(t *testing.T) {
	cidrs := CreateBlockedCIDRs()
	require.NotEmpty(t, cidrs)

	found := false
	for _, c := range cidrs {
		if c.String() == "169.254.169.254/32" {
			found = true
		}
	}
	assert.True(t, found, "the cloud metadata endpoint must always be blocked")
}
