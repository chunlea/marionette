package iptables

import (
	"net"
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
			expected: []string{"-A", "OUTPUT", "-m", "state", "--state", "ESTABLISHED,RELATED", "-j", "ACCEPT"},
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
	rule := rs.Rules[0]
	assert.Equal(t, "TEST_CHAIN", rule.Chain)
	assert.Equal(t, ActionAccept, rule.Action)
	assert.Contains(t, rule.State, "ESTABLISHED")
	assert.Contains(t, rule.State, "RELATED")
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
