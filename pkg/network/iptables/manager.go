package iptables

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/chunlea/marionette/pkg/network"
)

const (
	// DefaultChainPrefix is the prefix for Marionette-created chains.
	DefaultChainPrefix = "MARIONETTE_"

	// MaxChainNameLength is the maximum length for iptables chain names.
	MaxChainNameLength = 28
)

// Manager manages iptables rules for session network isolation.
type Manager struct {
	executor    Executor
	chainPrefix string
	mu          sync.Mutex

	// activeChains tracks chains created by this manager.
	activeChains map[string]bool
}

// NewManager creates a new iptables manager.
func NewManager(executor Executor) *Manager {
	return &Manager{
		executor:     executor,
		chainPrefix:  DefaultChainPrefix,
		activeChains: make(map[string]bool),
	}
}

// ChainName generates a chain name for a session.
// The name is prefixed and truncated to fit iptables limits.
func (m *Manager) ChainName(sessionID string) string {
	// Remove any common prefix from session ID
	id := strings.TrimPrefix(sessionID, "sess_")

	// Combine prefix and ID, truncate if needed
	name := m.chainPrefix + id
	if len(name) > MaxChainNameLength {
		name = name[:MaxChainNameLength]
	}

	return name
}

// CreateChain creates a new iptables chain for a session.
func (m *Manager) CreateChain(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	chainName := m.ChainName(sessionID)

	// Create IPv4 chain
	if err := m.executor.Run(ctx, "-N", chainName); err != nil {
		// Ignore "Chain already exists" error
		if !strings.Contains(err.Error(), "Chain already exists") {
			return fmt.Errorf("create chain %s: %w", chainName, err)
		}
	}

	// Create IPv6 chain
	if err := m.executor.RunIPv6(ctx, "-N", chainName); err != nil {
		if !strings.Contains(err.Error(), "Chain already exists") {
			return fmt.Errorf("create IPv6 chain %s: %w", chainName, err)
		}
	}

	m.activeChains[chainName] = true
	return nil
}

// DeleteChain removes an iptables chain for a session.
func (m *Manager) DeleteChain(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	chainName := m.ChainName(sessionID)

	// Flush and delete IPv4 chain
	_ = m.executor.Run(ctx, "-F", chainName)
	if err := m.executor.Run(ctx, "-X", chainName); err != nil {
		// Ignore "No chain" error
		if !strings.Contains(err.Error(), "No chain") {
			return fmt.Errorf("delete chain %s: %w", chainName, err)
		}
	}

	// Flush and delete IPv6 chain
	_ = m.executor.RunIPv6(ctx, "-F", chainName)
	if err := m.executor.RunIPv6(ctx, "-X", chainName); err != nil {
		if !strings.Contains(err.Error(), "No chain") {
			return fmt.Errorf("delete IPv6 chain %s: %w", chainName, err)
		}
	}

	delete(m.activeChains, chainName)
	return nil
}

// ApplyPolicy applies a resolved network policy to a session's chain.
func (m *Manager) ApplyPolicy(ctx context.Context, sessionID string, policy *network.ResolvedPolicy) error {
	if policy == nil {
		return fmt.Errorf("policy is nil")
	}

	chainName := m.ChainName(sessionID)
	ruleSet := m.GenerateRuleSet(chainName, policy)

	if err := ruleSet.Validate(); err != nil {
		return fmt.Errorf("invalid rule set: %w", err)
	}

	// Apply IPv4 rules
	for _, rule := range ruleSet.Rules {
		args := rule.ToArgs()
		if err := m.executor.Run(ctx, args...); err != nil {
			return fmt.Errorf("apply rule %s: %w", rule.String(), err)
		}
	}

	// Apply IPv6 rules
	for _, rule := range ruleSet.IPv6Rules {
		args := rule.ToArgs()
		if err := m.executor.RunIPv6(ctx, args...); err != nil {
			return fmt.Errorf("apply IPv6 rule %s: %w", rule.String(), err)
		}
	}

	return nil
}

// GenerateRuleSet generates iptables rules for a resolved policy.
func (m *Manager) GenerateRuleSet(chainName string, policy *network.ResolvedPolicy) *RuleSet {
	rs := NewRuleSet(chainName)

	// 1. Allow established connections
	rs.AddAllowEstablished()

	// 2. Block dangerous CIDRs first (metadata service, private networks)
	for _, cidr := range policy.BlockedCIDRs {
		comment := "Block " + cidr.String()
		rs.AddBlockCIDR(cidr, comment)
	}

	// 3. Allow specific IPs and ports
	for _, hr := range policy.AllowedIPs {
		for _, ip := range hr.IPs {
			// Skip blocked IPs
			if network.IsBlockedIP(ip) {
				continue
			}

			// Add allow rule for each allowed port
			for _, port := range policy.AllowedPorts {
				comment := fmt.Sprintf("Allow %s:%d", hr.Pattern, port)
				rs.AddAllowIP(ip, port, comment)
			}
		}
	}

	// 4. Default drop all other traffic
	rs.AddDefaultDrop()

	return rs
}

// GenerateRules returns the list of iptables command arguments for a policy.
// This is useful for debugging or generating scripts.
func (m *Manager) GenerateRules(sessionID string, policy *network.ResolvedPolicy) ([][]string, [][]string) {
	chainName := m.ChainName(sessionID)
	ruleSet := m.GenerateRuleSet(chainName, policy)

	ipv4Rules := make([][]string, 0, len(ruleSet.Rules))
	for _, rule := range ruleSet.Rules {
		ipv4Rules = append(ipv4Rules, rule.ToArgs())
	}

	ipv6Rules := make([][]string, 0, len(ruleSet.IPv6Rules))
	for _, rule := range ruleSet.IPv6Rules {
		ipv6Rules = append(ipv6Rules, rule.ToArgs())
	}

	return ipv4Rules, ipv6Rules
}

// FlushChain removes all rules from a chain.
func (m *Manager) FlushChain(ctx context.Context, sessionID string) error {
	chainName := m.ChainName(sessionID)

	if err := m.executor.Run(ctx, "-F", chainName); err != nil {
		return fmt.Errorf("flush chain %s: %w", chainName, err)
	}

	if err := m.executor.RunIPv6(ctx, "-F", chainName); err != nil {
		return fmt.Errorf("flush IPv6 chain %s: %w", chainName, err)
	}

	return nil
}

// ChainExists checks if a chain exists.
func (m *Manager) ChainExists(ctx context.Context, sessionID string) (bool, error) {
	chainName := m.ChainName(sessionID)

	_, err := m.executor.Output(ctx, "-L", chainName, "-n")
	if err != nil {
		if strings.Contains(err.Error(), "No chain") {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// ListActiveChains returns the list of chains created by this manager.
func (m *Manager) ListActiveChains() []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	chains := make([]string, 0, len(m.activeChains))
	for chain := range m.activeChains {
		chains = append(chains, chain)
	}
	return chains
}

// CleanupAllChains removes all chains created by this manager.
func (m *Manager) CleanupAllChains(ctx context.Context) error {
	chains := m.ListActiveChains()

	var lastErr error
	for _, chain := range chains {
		// Extract session ID from chain name
		sessionID := strings.TrimPrefix(chain, m.chainPrefix)
		if err := m.DeleteChain(ctx, sessionID); err != nil {
			lastErr = err
		}
	}

	return lastErr
}

// LinkChainToOutput adds a jump rule from OUTPUT chain to the session's chain.
// This is needed to actually filter outgoing traffic.
func (m *Manager) LinkChainToOutput(ctx context.Context, sessionID string) error {
	chainName := m.ChainName(sessionID)

	// Add jump rule to OUTPUT chain
	if err := m.executor.Run(ctx, "-I", "OUTPUT", "-j", chainName); err != nil {
		return fmt.Errorf("link chain to OUTPUT: %w", err)
	}

	if err := m.executor.RunIPv6(ctx, "-I", "OUTPUT", "-j", chainName); err != nil {
		return fmt.Errorf("link IPv6 chain to OUTPUT: %w", err)
	}

	return nil
}

// UnlinkChainFromOutput removes the jump rule from OUTPUT chain.
func (m *Manager) UnlinkChainFromOutput(ctx context.Context, sessionID string) error {
	chainName := m.ChainName(sessionID)

	// Remove jump rule from OUTPUT chain
	if err := m.executor.Run(ctx, "-D", "OUTPUT", "-j", chainName); err != nil {
		if !strings.Contains(err.Error(), "Bad rule") {
			return fmt.Errorf("unlink chain from OUTPUT: %w", err)
		}
	}

	if err := m.executor.RunIPv6(ctx, "-D", "OUTPUT", "-j", chainName); err != nil {
		if !strings.Contains(err.Error(), "Bad rule") {
			return fmt.Errorf("unlink IPv6 chain from OUTPUT: %w", err)
		}
	}

	return nil
}

// ApplyInNamespace applies iptables rules in a specific network namespace.
// This is used for container network isolation.
func (m *Manager) ApplyInNamespace(ctx context.Context, nsPath string, sessionID string, policy *network.ResolvedPolicy) error {
	// For namespace-aware execution, we would use nsenter or similar.
	// This is a placeholder for the actual implementation.
	// In practice, this would be called from the Docker provider.

	// Create chain
	if err := m.CreateChain(ctx, sessionID); err != nil {
		return err
	}

	// Apply policy
	if err := m.ApplyPolicy(ctx, sessionID, policy); err != nil {
		// Cleanup on failure
		_ = m.DeleteChain(ctx, sessionID)
		return err
	}

	// Link to OUTPUT
	if err := m.LinkChainToOutput(ctx, sessionID); err != nil {
		_ = m.DeleteChain(ctx, sessionID)
		return err
	}

	return nil
}

// CreateBlockedCIDRs creates Rule structs for all blocked CIDRs.
func CreateBlockedCIDRs() []*net.IPNet {
	return network.ParsedBlockedCIDRs
}
