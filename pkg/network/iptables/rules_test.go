package iptables

import (
	"net"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRule_ToArgs(t *testing.T) {
	tests := []struct {
		name     string
		rule     Rule
		expected []string
	}{
		{
			name: "simple drop",
			rule: Rule{
				Chain:  "OUTPUT",
				Action: ActionDrop,
			},
			expected: []string{"-A", "OUTPUT", "-j", "DROP"},
		},
		{
			name: "tcp with port",
			rule: Rule{
				Chain:    "OUTPUT",
				Action:   ActionAccept,
				Protocol: ProtocolTCP,
				DestPort: 443,
			},
			expected: []string{"-A", "OUTPUT", "-p", "tcp", "--dport", "443", "-j", "ACCEPT"},
		},
		{
			name: "destination IP",
			rule: Rule{
				Chain:    "OUTPUT",
				Action:   ActionAccept,
				Protocol: ProtocolTCP,
				DestIP:   net.ParseIP("8.8.8.8"),
				DestPort: 443,
			},
			expected: []string{"-A", "OUTPUT", "-p", "tcp", "-d", "8.8.8.8", "--dport", "443", "-j", "ACCEPT"},
		},
		{
			name: "destination CIDR",
			rule: Rule{
				Chain:    "OUTPUT",
				Action:   ActionDrop,
				DestCIDR: mustParseCIDR("10.0.0.0/8"),
			},
			expected: []string{"-A", "OUTPUT", "-d", "10.0.0.0/8", "-j", "DROP"},
		},
		{
			name: "connection state",
			rule: Rule{
				Chain:  "OUTPUT",
				Action: ActionAccept,
				State:  []string{"ESTABLISHED", "RELATED"},
			},
			expected: []string{"-A", "OUTPUT", "-m", "conntrack", "--ctstate", "ESTABLISHED,RELATED", "-j", "ACCEPT"},
		},
		{
			name: "with comment",
			rule: Rule{
				Chain:   "OUTPUT",
				Action:  ActionAccept,
				Comment: "Allow HTTPS",
			},
			expected: []string{"-A", "OUTPUT", "-m", "comment", "--comment", "Allow HTTPS", "-j", "ACCEPT"},
		},
		{
			name: "source IP and port",
			rule: Rule{
				Chain:      "INPUT",
				Action:     ActionAccept,
				Protocol:   ProtocolUDP,
				SourceIP:   net.ParseIP("192.168.1.1"),
				SourcePort: 53,
			},
			expected: []string{"-A", "INPUT", "-p", "udp", "-s", "192.168.1.1", "--sport", "53", "-j", "ACCEPT"},
		},
		{
			name: "log with prefix",
			rule: Rule{
				Chain:     "OUTPUT",
				Action:    ActionLog,
				LogPrefix: "[MARIONETTE] ",
			},
			expected: []string{"-A", "OUTPUT", "-j", "LOG", "--log-prefix", "[MARIONETTE] "},
		},
		{
			name: "source CIDR",
			rule: Rule{
				Chain:      "INPUT",
				Action:     ActionAccept,
				Protocol:   ProtocolTCP,
				SourceCIDR: mustParseCIDR("192.168.0.0/16"),
			},
			expected: []string{"-A", "INPUT", "-p", "tcp", "-s", "192.168.0.0/16", "-j", "ACCEPT"},
		},
		{
			name: "port without tcp/udp is ignored",
			rule: Rule{
				Chain:    "OUTPUT",
				Action:   ActionAccept,
				Protocol: ProtocolICMP,
				DestPort: 443,
			},
			expected: []string{"-A", "OUTPUT", "-p", "icmp", "-j", "ACCEPT"},
		},
		{
			name: "protocol all is not included",
			rule: Rule{
				Chain:    "OUTPUT",
				Action:   ActionDrop,
				Protocol: ProtocolAll,
			},
			expected: []string{"-A", "OUTPUT", "-j", "DROP"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.rule.ToArgs()
			assert.Equal(t, tt.expected, args)
		})
	}
}

func TestRule_String(t *testing.T) {
	tests := []struct {
		name     string
		rule     Rule
		contains []string
	}{
		{
			name: "simple rule",
			rule: Rule{
				Chain:  "OUTPUT",
				Action: ActionDrop,
			},
			contains: []string{"OUTPUT", "DROP"},
		},
		{
			name: "with destination",
			rule: Rule{
				Chain:    "OUTPUT",
				Action:   ActionAccept,
				Protocol: ProtocolTCP,
				DestIP:   net.ParseIP("8.8.8.8"),
				DestPort: 443,
			},
			contains: []string{"OUTPUT", "tcp", "8.8.8.8", "443", "ACCEPT"},
		},
		{
			name: "with source IP",
			rule: Rule{
				Chain:    "INPUT",
				Action:   ActionAccept,
				SourceIP: net.ParseIP("192.168.1.1"),
			},
			contains: []string{"INPUT", "from", "192.168.1.1", "ACCEPT"},
		},
		{
			name: "with source CIDR",
			rule: Rule{
				Chain:      "INPUT",
				Action:     ActionAccept,
				SourceCIDR: mustParseCIDR("192.168.0.0/16"),
			},
			contains: []string{"INPUT", "from", "192.168.0.0/16", "ACCEPT"},
		},
		{
			name: "with destination CIDR",
			rule: Rule{
				Chain:    "OUTPUT",
				Action:   ActionDrop,
				DestCIDR: mustParseCIDR("10.0.0.0/8"),
			},
			contains: []string{"OUTPUT", "to", "10.0.0.0/8", "DROP"},
		},
		{
			name: "with protocol all (should not display)",
			rule: Rule{
				Chain:    "OUTPUT",
				Action:   ActionDrop,
				Protocol: ProtocolAll,
			},
			contains: []string{"OUTPUT", "DROP"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			str := tt.rule.String()
			for _, s := range tt.contains {
				assert.Contains(t, str, s)
			}
		})
	}
}

func TestRuleSet_NewRuleSet(t *testing.T) {
	rs := NewRuleSet("TEST_CHAIN")
	assert.Equal(t, "TEST_CHAIN", rs.ChainName)
	assert.Empty(t, rs.Rules)
	assert.Empty(t, rs.IPv6Rules)
	assert.True(t, rs.FlushFirst)
}

func TestRuleSet_AddAllowEstablished(t *testing.T) {
	rs := NewRuleSet("TEST_CHAIN")
	rs.AddAllowEstablished()

	require.Len(t, rs.Rules, 1)
	require.Len(t, rs.IPv6Rules, 1, "state matching must exist in both families")

	rule := rs.Rules[0]
	assert.Equal(t, "TEST_CHAIN", rule.Chain)
	assert.Equal(t, ActionAccept, rule.Action)
	assert.Contains(t, rule.State, "ESTABLISHED")
	assert.Contains(t, rule.State, "RELATED")
	assert.True(t, rs.IPv6Rules[0].IsIPv6)
}

func TestRuleSet_AddAllowIP(t *testing.T) {
	t.Run("IPv4", func(t *testing.T) {
		rs := NewRuleSet("TEST_CHAIN")
		ip := net.ParseIP("8.8.8.8")
		rs.AddAllowIP(ip, 443, "Allow DNS")

		require.Len(t, rs.Rules, 1)
		require.Empty(t, rs.IPv6Rules)

		rule := rs.Rules[0]
		assert.Equal(t, ip, rule.DestIP)
		assert.Equal(t, 443, rule.DestPort)
		assert.Equal(t, "Allow DNS", rule.Comment)
		assert.False(t, rule.IsIPv6)
	})

	t.Run("IPv6", func(t *testing.T) {
		rs := NewRuleSet("TEST_CHAIN")
		ip := net.ParseIP("2001:4860:4860::8888")
		rs.AddAllowIP(ip, 443, "Allow IPv6 DNS")

		require.Empty(t, rs.Rules)
		require.Len(t, rs.IPv6Rules, 1)

		rule := rs.IPv6Rules[0]
		assert.Equal(t, ip, rule.DestIP)
		assert.True(t, rule.IsIPv6)
	})
}

func TestRuleSet_AddBlockCIDR(t *testing.T) {
	t.Run("IPv4", func(t *testing.T) {
		rs := NewRuleSet("TEST_CHAIN")
		cidr := mustParseCIDR("10.0.0.0/8")
		rs.AddBlockCIDR(cidr, "Block private")

		require.Len(t, rs.Rules, 1)
		assert.Equal(t, ActionDrop, rs.Rules[0].Action)
		assert.Equal(t, cidr, rs.Rules[0].DestCIDR)
	})

	t.Run("IPv6", func(t *testing.T) {
		rs := NewRuleSet("TEST_CHAIN")
		cidr := mustParseCIDR("fc00::/7")
		rs.AddBlockCIDR(cidr, "Block IPv6 ULA")

		require.Empty(t, rs.Rules)
		require.Len(t, rs.IPv6Rules, 1)
		assert.True(t, rs.IPv6Rules[0].IsIPv6)
	})
}

func TestRuleSet_AddDefaultDrop(t *testing.T) {
	rs := NewRuleSet("TEST_CHAIN")
	rs.AddDefaultDrop()

	require.Len(t, rs.Rules, 1)
	require.Len(t, rs.IPv6Rules, 1)

	assert.Equal(t, ActionDrop, rs.Rules[0].Action)
	assert.Equal(t, ActionDrop, rs.IPv6Rules[0].Action)
}

func TestRuleSet_Validate(t *testing.T) {
	tests := []struct {
		name      string
		chainName string
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "valid chain name",
			chainName: "MARIONETTE_sess_123",
			wantErr:   false,
		},
		{
			name:      "empty chain name",
			chainName: "",
			wantErr:   true,
			errMsg:    "chain name is required",
		},
		{
			name:      "invalid character",
			chainName: "TEST-CHAIN",
			wantErr:   true,
			errMsg:    "invalid chain name character",
		},
		{
			name:      "too long",
			chainName: "THIS_CHAIN_NAME_IS_WAY_TOO_LONG_FOR_IPTABLES",
			wantErr:   true,
			errMsg:    "exceeds 28 characters",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rs := NewRuleSet(tt.chainName)
			err := rs.Validate()

			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func mustParseCIDR(s string) *net.IPNet {
	_, cidr, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return cidr
}

func TestRuleSet_AddAllowLoopback(t *testing.T) {
	rs := NewRuleSet("TEST_CHAIN")
	rs.AddAllowLoopback()

	require.Len(t, rs.Rules, 1)
	require.Len(t, rs.IPv6Rules, 1)
	assert.Equal(t, "lo", rs.Rules[0].OutInterface)
	assert.Contains(t, rs.Rules[0].ToArgs(), "-o")
}

func TestRuleSet_AddAllowDNSServer(t *testing.T) {
	rs := NewRuleSet("TEST_CHAIN")
	rs.AddAllowDNSServer(net.ParseIP("10.0.0.53"), "resolver")

	// UDP is the normal path; TCP is needed for answers over 512 bytes and for
	// DNSSEC, so a UDP-only rule breaks resolution unpredictably.
	require.Len(t, rs.Rules, 2)
	assert.Equal(t, ProtocolUDP, rs.Rules[0].Protocol)
	assert.Equal(t, ProtocolTCP, rs.Rules[1].Protocol)
	for _, r := range rs.Rules {
		assert.Equal(t, 53, r.DestPort)
	}
	assert.Empty(t, rs.IPv6Rules)
}

func TestRuleSet_AddAllowDNSAny(t *testing.T) {
	rs := NewRuleSet("TEST_CHAIN")
	rs.AddAllowDNSAny("fallback")

	require.Len(t, rs.Rules, 2)
	require.Len(t, rs.IPv6Rules, 2)
	for _, r := range rs.Rules {
		assert.Nil(t, r.DestIP, "the fallback deliberately has no destination")
		assert.Equal(t, 53, r.DestPort)
	}
}

func TestRuleSet_AddJump(t *testing.T) {
	rs := NewRuleSet("TEST_CHAIN")
	rs.AddJump("TEST_CHAIN_D", "allow list")

	require.Len(t, rs.Rules, 1)
	require.Len(t, rs.IPv6Rules, 1)
	assert.Equal(t, RuleAction("TEST_CHAIN_D"), rs.Rules[0].Action)
	assert.Contains(t, rs.Rules[0].ToArgs(), "TEST_CHAIN_D")
}

func TestRule_ArgsVerbs(t *testing.T) {
	rule := Rule{
		Chain:    "CHAIN",
		Action:   ActionAccept,
		Protocol: ProtocolTCP,
		DestIP:   net.ParseIP("1.1.1.1"),
		DestPort: 443,
		Comment:  "Allow 1.1.1.1:443",
	}

	for _, verb := range []string{"-A", "-I", "-D", "-C"} {
		args := rule.Args(verb)
		assert.Equal(t, verb, args[0])
		assert.Equal(t, "CHAIN", args[1])
		// Everything after the verb must be byte-identical, or iptables cannot
		// match a delete against the rule an append created.
		assert.Equal(t, rule.Args("-A")[2:], args[2:])
	}
}

func TestValidateChainName(t *testing.T) {
	require.NoError(t, ValidateChainName("MARIONETTE_abc_D"))
	assert.ErrorContains(t, ValidateChainName("MARIONETTE-abc"), "invalid chain name character")
	assert.ErrorContains(t, ValidateChainName(strings.Repeat("a", 29)), "exceeds 28 characters")
}

func TestRuleSet_ValidateRejectsActionlessRules(t *testing.T) {
	rs := NewRuleSet("TEST_CHAIN")
	rs.Rules = append(rs.Rules, Rule{Chain: "TEST_CHAIN"})
	assert.ErrorContains(t, rs.Validate(), "no action")
}
