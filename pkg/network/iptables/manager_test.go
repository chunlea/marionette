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
}

func TestManager_GenerateRuleSet(t *testing.T) {
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
	mock := NewMockExecutor()
	m := NewManager(mock)

	err := m.FlushChain(context.Background(), "sess_test")
	require.NoError(t, err)

	commands := mock.GetCommands()
	require.Len(t, commands, 1)
	assert.Equal(t, []string{"-F", "MARIONETTE_test"}, commands[0])
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
}

func TestManager_LinkChainToOutput(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	err := m.LinkChainToOutput(context.Background(), "sess_test")
	require.NoError(t, err)

	commands := mock.GetCommands()
	require.Len(t, commands, 1)
	assert.Equal(t, []string{"-I", "OUTPUT", "-j", "MARIONETTE_test"}, commands[0])
}

func TestManager_UnlinkChainFromOutput(t *testing.T) {
	mock := NewMockExecutor()
	m := NewManager(mock)

	err := m.UnlinkChainFromOutput(context.Background(), "sess_test")
	require.NoError(t, err)

	commands := mock.GetCommands()
	require.Len(t, commands, 1)
	assert.Equal(t, []string{"-D", "OUTPUT", "-j", "MARIONETTE_test"}, commands[0])
}

func TestManager_CleanupAllChains(t *testing.T) {
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
}

func TestManager_ApplyInNamespace(t *testing.T) {
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
