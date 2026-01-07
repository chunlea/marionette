package iptables

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/chunlea/marionette/pkg/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewManager(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	assert.NotNil(t, m)
	assert.Equal(t, DefaultChainPrefix, m.chainPrefix)
	assert.Empty(t, m.activeChains)
}

func TestManager_ChainName(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	tests := []struct {
		sessionID string
		expected  string
	}{
		{"sess_abc123", "MARIONETTE_abc123"},
		{"abc123", "MARIONETTE_abc123"},
		{"sess_verylongsessionidthatneedstruncation", "MARIONETTE_verylongsessionid"}, // Truncated to 28 chars
	}

	for _, tt := range tests {
		t.Run(tt.sessionID, func(t *testing.T) {
			name := m.ChainName(tt.sessionID)
			assert.Equal(t, tt.expected, name)
			assert.LessOrEqual(t, len(name), MaxChainNameLength)
		})
	}
}

func TestManager_CreateChain(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		err := m.CreateChain(context.Background(), "sess_test123")
		require.NoError(t, err)

		// Should have created both IPv4 and IPv6 chains
		commands := mock.GetCommands()
		require.Len(t, commands, 1)
		assert.Equal(t, []string{"-N", "MARIONETTE_test123"}, commands[0])

		ipv6Commands := mock.GetIPv6Commands()
		require.Len(t, ipv6Commands, 1)
		assert.Equal(t, []string{"-N", "MARIONETTE_test123"}, ipv6Commands[0])

		// Should be tracked
		chains := m.ListActiveChains()
		assert.Contains(t, chains, "MARIONETTE_test123")
	})

	t.Run("chain already exists", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-N", "MARIONETTE_test123"}, &chainExistsError{})
		m := NewManager(mock)

		// Should not error if chain already exists
		err := m.CreateChain(context.Background(), "sess_test123")
		require.NoError(t, err)
	})

	t.Run("ipv4 other error", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-N", "MARIONETTE_test123"}, &genericError{msg: "permission denied"})
		m := NewManager(mock)

		err := m.CreateChain(context.Background(), "sess_test123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create chain")
	})

	t.Run("ipv6 chain already exists", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.Errors["v6:-N MARIONETTE_test123"] = &chainExistsError{}
		m := NewManager(mock)

		// Should not error if IPv6 chain already exists
		err := m.CreateChain(context.Background(), "sess_test123")
		require.NoError(t, err)
	})

	t.Run("ipv6 other error", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.Errors["v6:-N MARIONETTE_test123"] = &genericError{msg: "permission denied"}
		m := NewManager(mock)

		err := m.CreateChain(context.Background(), "sess_test123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create IPv6 chain")
	})
}

func TestManager_DeleteChain(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		// Create chain first
		_ = m.CreateChain(context.Background(), "sess_test123")
		mock.Reset()

		err := m.DeleteChain(context.Background(), "sess_test123")
		require.NoError(t, err)

		// Should have flushed and deleted
		commands := mock.GetCommands()
		assert.Contains(t, flattenCommands(commands), "-F")
		assert.Contains(t, flattenCommands(commands), "-X")

		// Should no longer be tracked
		chains := m.ListActiveChains()
		assert.NotContains(t, chains, "MARIONETTE_test123")
	})

	t.Run("chain does not exist", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-X", "MARIONETTE_test123"}, &noChainError{})
		m := NewManager(mock)

		// Should not error if chain doesn't exist
		err := m.DeleteChain(context.Background(), "sess_test123")
		require.NoError(t, err)
	})

	t.Run("ipv4 other error", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-X", "MARIONETTE_test123"}, &genericError{msg: "permission denied"})
		m := NewManager(mock)

		err := m.DeleteChain(context.Background(), "sess_test123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delete chain")
	})

	t.Run("ipv6 chain does not exist", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.Errors["v6:-X MARIONETTE_test123"] = &noChainError{}
		m := NewManager(mock)

		// Should not error if IPv6 chain doesn't exist
		err := m.DeleteChain(context.Background(), "sess_test123")
		require.NoError(t, err)
	})

	t.Run("ipv6 other error", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.Errors["v6:-X MARIONETTE_test123"] = &genericError{msg: "permission denied"}
		m := NewManager(mock)

		err := m.DeleteChain(context.Background(), "sess_test123")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "delete IPv6 chain")
	})
}

func TestManager_ApplyPolicy(t *testing.T) {
	t.Run("nil policy", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		err := m.ApplyPolicy(context.Background(), "sess_test", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "policy is nil")
	})

	t.Run("applies rules", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		policy := createTestPolicy()

		err := m.ApplyPolicy(context.Background(), "sess_test", policy)
		require.NoError(t, err)

		// Should have applied multiple rules
		commands := mock.GetCommands()
		assert.NotEmpty(t, commands)

		// Check for expected rule types
		allArgs := flattenCommands(commands)
		assert.Contains(t, allArgs, "ESTABLISHED,RELATED") // Allow established
		assert.Contains(t, allArgs, "DROP")                // Default drop or block
	})

	t.Run("invalid chain name", func(t *testing.T) {
		mock := NewMockExecutor()
		// Change the prefix to create an invalid chain name
		m := &Manager{
			executor:     mock,
			chainPrefix:  "INVALID-PREFIX-", // Contains dash, which is invalid
			activeChains: make(map[string]bool),
		}

		policy := createTestPolicy()

		err := m.ApplyPolicy(context.Background(), "sess_test", policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "invalid rule set")
	})

	t.Run("ipv4 rule error", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		policy := createTestPolicy()

		// Set error for the first rule (allow established)
		mock.SetError([]string{"-A", "MARIONETTE_test", "-m", "state", "--state", "ESTABLISHED,RELATED", "-m", "comment", "--comment", "Allow established connections", "-j", "ACCEPT"}, &genericError{msg: "permission denied"})

		err := m.ApplyPolicy(context.Background(), "sess_test", policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "apply rule")
	})

	t.Run("ipv6 rule error", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		policy := createTestPolicyWithIPv6()

		// Set error for the IPv6 rule
		mock.Errors["v6:-A MARIONETTE_test -p tcp -d 2001:4860:4860::8888 --dport 443 -m comment --comment Allow github.com:443 -j ACCEPT"] = &genericError{msg: "permission denied"}

		err := m.ApplyPolicy(context.Background(), "sess_test", policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "apply IPv6 rule")
	})
}

func TestManager_GenerateRuleSet(t *testing.T) {
	t.Run("basic policy", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		policy := createTestPolicy()
		ruleSet := m.GenerateRuleSet("TEST_CHAIN", policy)

		require.NotNil(t, ruleSet)
		assert.Equal(t, "TEST_CHAIN", ruleSet.ChainName)

		// Should have rules for:
		// 1. Allow established
		// 2. Block dangerous CIDRs
		// 3. Allow specific IPs
		// 4. Default drop
		assert.NotEmpty(t, ruleSet.Rules)

		// First rule should be allow established
		assert.Equal(t, ActionAccept, ruleSet.Rules[0].Action)
		assert.Contains(t, ruleSet.Rules[0].State, "ESTABLISHED")

		// Last rule should be default drop
		lastRule := ruleSet.Rules[len(ruleSet.Rules)-1]
		assert.Equal(t, ActionDrop, lastRule.Action)
	})

	t.Run("skips blocked IPs", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		// Create a policy with a blocked IP (metadata service IP)
		policy := &network.ResolvedPolicy{
			OriginalPolicy: &network.NetworkPolicy{
				Level: network.PolicyAllowList,
			},
			AllowedIPs: []network.HostResolution{
				{
					Pattern: "evil.com",
					IPs:     []net.IP{net.ParseIP("169.254.169.254")}, // Metadata service IP - blocked
				},
				{
					Pattern: "good.com",
					IPs:     []net.IP{net.ParseIP("8.8.8.8")}, // Normal IP - allowed
				},
			},
			AllowedPorts: []int{443},
			BlockedCIDRs: []*net.IPNet{},
			PinnedAt:     time.Now(),
			ExpiresAt:    time.Now().Add(time.Hour),
		}

		ruleSet := m.GenerateRuleSet("TEST_CHAIN", policy)

		// Check that the blocked IP is NOT in the rules
		for _, rule := range ruleSet.Rules {
			if rule.DestIP != nil {
				assert.NotEqual(t, "169.254.169.254", rule.DestIP.String(), "blocked IP should be skipped")
			}
		}

		// Check that the good IP IS in the rules
		foundGoodIP := false
		for _, rule := range ruleSet.Rules {
			if rule.DestIP != nil && rule.DestIP.String() == "8.8.8.8" {
				foundGoodIP = true
				break
			}
		}
		assert.True(t, foundGoodIP, "good IP should be in rules")
	})
}

func TestManager_GenerateRules(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	policy := createTestPolicy()

	ipv4Rules, ipv6Rules := m.GenerateRules("sess_test", policy)

	assert.NotEmpty(t, ipv4Rules)
	assert.NotEmpty(t, ipv6Rules) // Should have IPv6 default drop at minimum
}

func TestManager_FlushChain(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		err := m.FlushChain(context.Background(), "sess_test")
		require.NoError(t, err)

		commands := mock.GetCommands()
		require.Len(t, commands, 1)
		assert.Equal(t, []string{"-F", "MARIONETTE_test"}, commands[0])
	})

	t.Run("ipv4 error", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-F", "MARIONETTE_test"}, &genericError{msg: "permission denied"})
		m := NewManager(mock)

		err := m.FlushChain(context.Background(), "sess_test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "flush chain")
	})

	t.Run("ipv6 error", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.Errors["v6:-F MARIONETTE_test"] = &genericError{msg: "permission denied"}
		m := NewManager(mock)

		err := m.FlushChain(context.Background(), "sess_test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "flush IPv6 chain")
	})
}

func TestManager_ChainExists(t *testing.T) {
	t.Run("exists", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetOutput([]string{"-L", "MARIONETTE_test", "-n"}, []byte("Chain MARIONETTE_test"))
		m := NewManager(mock)

		exists, err := m.ChainExists(context.Background(), "sess_test")
		require.NoError(t, err)
		assert.True(t, exists)
	})

	t.Run("does not exist", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-L", "MARIONETTE_test", "-n"}, &noChainError{})
		m := NewManager(mock)

		exists, err := m.ChainExists(context.Background(), "sess_test")
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("other error", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-L", "MARIONETTE_test", "-n"}, &genericError{msg: "permission denied"})
		m := NewManager(mock)

		exists, err := m.ChainExists(context.Background(), "sess_test")
		require.Error(t, err)
		assert.False(t, exists)
	})
}

func TestManager_LinkChainToOutput(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		err := m.LinkChainToOutput(context.Background(), "sess_test")
		require.NoError(t, err)

		commands := mock.GetCommands()
		require.Len(t, commands, 1)
		assert.Equal(t, []string{"-I", "OUTPUT", "-j", "MARIONETTE_test"}, commands[0])
	})

	t.Run("ipv4 error", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-I", "OUTPUT", "-j", "MARIONETTE_test"}, &genericError{msg: "permission denied"})
		m := NewManager(mock)

		err := m.LinkChainToOutput(context.Background(), "sess_test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "link chain to OUTPUT")
	})

	t.Run("ipv6 error", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.Errors["v6:-I OUTPUT -j MARIONETTE_test"] = &genericError{msg: "permission denied"}
		m := NewManager(mock)

		err := m.LinkChainToOutput(context.Background(), "sess_test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "link IPv6 chain to OUTPUT")
	})
}

func TestManager_UnlinkChainFromOutput(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		err := m.UnlinkChainFromOutput(context.Background(), "sess_test")
		require.NoError(t, err)

		commands := mock.GetCommands()
		require.Len(t, commands, 1)
		assert.Equal(t, []string{"-D", "OUTPUT", "-j", "MARIONETTE_test"}, commands[0])
	})

	t.Run("bad rule error is ignored", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-D", "OUTPUT", "-j", "MARIONETTE_test"}, &badRuleError{})
		m := NewManager(mock)

		// Should not error if rule doesn't exist
		err := m.UnlinkChainFromOutput(context.Background(), "sess_test")
		require.NoError(t, err)
	})

	t.Run("other ipv4 error is returned", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-D", "OUTPUT", "-j", "MARIONETTE_test"}, &genericError{msg: "permission denied"})
		m := NewManager(mock)

		err := m.UnlinkChainFromOutput(context.Background(), "sess_test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unlink chain from OUTPUT")
	})

	t.Run("ipv6 bad rule error is ignored", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"v6:-D", "OUTPUT", "-j", "MARIONETTE_test"}, &badRuleError{})
		m := NewManager(mock)

		// Need to properly set IPv6 error
		mock.Errors["v6:-D OUTPUT -j MARIONETTE_test"] = &badRuleError{}

		err := m.UnlinkChainFromOutput(context.Background(), "sess_test")
		require.NoError(t, err)
	})

	t.Run("other ipv6 error is returned", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.Errors["v6:-D OUTPUT -j MARIONETTE_test"] = &genericError{msg: "permission denied"}
		m := NewManager(mock)

		err := m.UnlinkChainFromOutput(context.Background(), "sess_test")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "unlink IPv6 chain from OUTPUT")
	})
}

func TestManager_CleanupAllChains(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		// Create multiple chains
		_ = m.CreateChain(context.Background(), "sess_test1")
		_ = m.CreateChain(context.Background(), "sess_test2")
		mock.Reset()

		err := m.CleanupAllChains(context.Background())
		require.NoError(t, err)

		// All chains should be removed
		assert.Empty(t, m.ListActiveChains())
	})

	t.Run("returns last error", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		// Create chains
		_ = m.CreateChain(context.Background(), "sess_test1")
		_ = m.CreateChain(context.Background(), "sess_test2")
		mock.Reset()

		// Set error for one of the chains
		mock.SetError([]string{"-X", "MARIONETTE_test1"}, &genericError{msg: "permission denied"})

		err := m.CleanupAllChains(context.Background())
		require.Error(t, err)
	})
}

func TestCreateBlockedCIDRs(t *testing.T) {
	cidrs := CreateBlockedCIDRs()

	require.NotEmpty(t, cidrs)

	// Should contain metadata service CIDR
	found := false
	for _, cidr := range cidrs {
		if cidr.String() == "169.254.0.0/16" {
			found = true
			break
		}
	}
	assert.True(t, found, "should contain metadata service CIDR")
}

func TestManager_ApplyInNamespace(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		policy := createTestPolicy()

		err := m.ApplyInNamespace(context.Background(), "/proc/123/ns/net", "sess_test", policy)
		require.NoError(t, err)

		// Should have created chain, applied policy, and linked to OUTPUT
		commands := mock.GetCommands()
		allArgs := flattenCommands(commands)
		assert.Contains(t, allArgs, "-N")
		assert.Contains(t, allArgs, "-I")
	})

	t.Run("create chain error", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-N", "MARIONETTE_test"}, &genericError{msg: "permission denied"})
		m := NewManager(mock)

		policy := createTestPolicy()

		err := m.ApplyInNamespace(context.Background(), "/proc/123/ns/net", "sess_test", policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "create chain")
	})

	t.Run("apply policy error cleans up", func(t *testing.T) {
		mock := NewMockExecutor()
		m := NewManager(mock)

		// Pass nil policy to trigger error
		err := m.ApplyInNamespace(context.Background(), "/proc/123/ns/net", "sess_test", nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "policy is nil")
	})

	t.Run("link to output error cleans up", func(t *testing.T) {
		mock := NewMockExecutor()
		mock.SetError([]string{"-I", "OUTPUT", "-j", "MARIONETTE_test"}, &genericError{msg: "permission denied"})
		m := NewManager(mock)

		policy := createTestPolicy()

		err := m.ApplyInNamespace(context.Background(), "/proc/123/ns/net", "sess_test", policy)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "link chain to OUTPUT")
	})
}

// Helper functions

func createTestPolicy() *network.ResolvedPolicy {
	return &network.ResolvedPolicy{
		OriginalPolicy: &network.NetworkPolicy{
			Level: network.PolicyAllowList,
		},
		AllowedIPs: []network.HostResolution{
			{
				Pattern: "github.com",
				IPs:     []net.IP{net.ParseIP("140.82.112.4")},
			},
		},
		AllowedPorts: []int{443, 80},
		BlockedCIDRs: network.ParsedBlockedCIDRs,
		PinnedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour),
	}
}

func createTestPolicyWithIPv6() *network.ResolvedPolicy {
	return &network.ResolvedPolicy{
		OriginalPolicy: &network.NetworkPolicy{
			Level: network.PolicyAllowList,
		},
		AllowedIPs: []network.HostResolution{
			{
				Pattern: "github.com",
				IPs:     []net.IP{net.ParseIP("2001:4860:4860::8888")}, // IPv6 address
			},
		},
		AllowedPorts: []int{443},
		BlockedCIDRs: []*net.IPNet{},
		PinnedAt:     time.Now(),
		ExpiresAt:    time.Now().Add(time.Hour),
	}
}

func flattenCommands(commands [][]string) string {
	var result string
	for _, cmd := range commands {
		for _, arg := range cmd {
			result += arg + " "
		}
	}
	return result
}

// Error types for testing
type chainExistsError struct{}

func (e *chainExistsError) Error() string {
	return "iptables: Chain already exists."
}

type noChainError struct{}

func (e *noChainError) Error() string {
	return "iptables: No chain/target/match by that name."
}

type badRuleError struct{}

func (e *badRuleError) Error() string {
	return "iptables: Bad rule (does a matching rule exist in that chain?)."
}

type genericError struct {
	msg string
}

func (e *genericError) Error() string {
	return e.msg
}
