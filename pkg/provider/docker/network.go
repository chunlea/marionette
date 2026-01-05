package docker

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/chunlea/marionette/pkg/network"
	"github.com/chunlea/marionette/pkg/network/iptables"
	"github.com/chunlea/marionette/pkg/provider"
)

// NetworkIsolation handles network policy enforcement for containers.
type NetworkIsolation struct {
	resolver *network.DNSResolver
	iptables *iptables.Manager
}

// NewNetworkIsolation creates a new network isolation handler.
func NewNetworkIsolation() *NetworkIsolation {
	return &NetworkIsolation{
		resolver: network.NewDNSResolver(),
		iptables: iptables.NewManager(iptables.NewRealExecutor()),
	}
}

// NewNetworkIsolationWithExecutor creates a network isolation handler with a custom iptables executor.
// This is useful for testing.
func NewNetworkIsolationWithExecutor(executor iptables.Executor) *NetworkIsolation {
	return &NetworkIsolation{
		resolver: network.NewDNSResolver(),
		iptables: iptables.NewManager(executor),
	}
}

// ApplyPolicy applies network isolation to a container.
// This should be called after the container is started and we can get its PID.
func (n *NetworkIsolation) ApplyPolicy(ctx context.Context, containerID string, opts provider.SpawnOptions) error {
	// Skip if no network policy or policy is "none"
	if opts.NetworkPolicy == "" || opts.NetworkPolicy == string(network.PolicyNone) {
		return nil
	}

	// Parse the network policy
	policy, err := network.ParsePolicy(opts.NetworkPolicy, opts.AllowedHosts)
	if err != nil {
		return fmt.Errorf("parsing network policy: %w", err)
	}

	// Resolve DNS for allowed hosts
	resolved, err := n.resolver.ResolvePolicy(ctx, policy)
	if err != nil {
		return fmt.Errorf("resolving network policy: %w", err)
	}

	// Get container PID
	pid, err := n.getContainerPID(ctx, containerID)
	if err != nil {
		return fmt.Errorf("getting container PID: %w", err)
	}

	// Apply iptables rules in container's network namespace
	sessionID := containerIDToSessionID(containerID)
	if err := n.applyIPTablesInNamespace(ctx, pid, sessionID, resolved); err != nil {
		return fmt.Errorf("applying iptables rules: %w", err)
	}

	return nil
}

// CleanupPolicy removes network isolation from a container.
func (n *NetworkIsolation) CleanupPolicy(ctx context.Context, containerID string) error {
	sessionID := containerIDToSessionID(containerID)
	return n.iptables.DeleteChain(ctx, sessionID)
}

// containerIDToSessionID generates a session ID from a container ID.
func containerIDToSessionID(containerID string) string {
	// Use first 12 chars of container ID, or full ID if shorter
	id := containerID
	if len(id) > 12 {
		id = id[:12]
	}
	return "container_" + id
}

// getContainerPID gets the PID of the main process in a container.
func (n *NetworkIsolation) getContainerPID(ctx context.Context, containerID string) (int, error) {
	// Use docker inspect to get the container PID
	cmd := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.Pid}}", containerID)
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("docker inspect failed: %w", err)
	}

	pidStr := strings.TrimSpace(string(output))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("invalid PID %q: %w", pidStr, err)
	}

	if pid == 0 {
		return 0, fmt.Errorf("container not running (PID is 0)")
	}

	return pid, nil
}

// applyIPTablesInNamespace applies iptables rules in a container's network namespace.
func (n *NetworkIsolation) applyIPTablesInNamespace(ctx context.Context, pid int, sessionID string, policy *network.ResolvedPolicy) error {
	// Get the network namespace path
	nsPath := fmt.Sprintf("/proc/%d/ns/net", pid)

	// Create the iptables chain
	if err := n.runInNamespace(ctx, nsPath, "iptables", "-N", n.iptables.ChainName(sessionID)); err != nil {
		// Ignore "Chain already exists" error
		if !strings.Contains(err.Error(), "Chain already exists") {
			return fmt.Errorf("create chain: %w", err)
		}
	}

	// Same for IPv6
	if err := n.runInNamespace(ctx, nsPath, "ip6tables", "-N", n.iptables.ChainName(sessionID)); err != nil {
		if !strings.Contains(err.Error(), "Chain already exists") {
			return fmt.Errorf("create IPv6 chain: %w", err)
		}
	}

	// Generate and apply rules
	ipv4Rules, ipv6Rules := n.iptables.GenerateRules(sessionID, policy)

	// Apply IPv4 rules
	for _, args := range ipv4Rules {
		if err := n.runInNamespace(ctx, nsPath, "iptables", args...); err != nil {
			return fmt.Errorf("apply rule %v: %w", args, err)
		}
	}

	// Apply IPv6 rules
	for _, args := range ipv6Rules {
		if err := n.runInNamespace(ctx, nsPath, "ip6tables", args...); err != nil {
			return fmt.Errorf("apply IPv6 rule %v: %w", args, err)
		}
	}

	// Link chain to OUTPUT
	if err := n.runInNamespace(ctx, nsPath, "iptables", "-I", "OUTPUT", "-j", n.iptables.ChainName(sessionID)); err != nil {
		return fmt.Errorf("link to OUTPUT: %w", err)
	}

	if err := n.runInNamespace(ctx, nsPath, "ip6tables", "-I", "OUTPUT", "-j", n.iptables.ChainName(sessionID)); err != nil {
		return fmt.Errorf("link IPv6 to OUTPUT: %w", err)
	}

	return nil
}

// runInNamespace runs a command in a network namespace using nsenter.
func (n *NetworkIsolation) runInNamespace(ctx context.Context, nsPath string, command string, args ...string) error {
	fullArgs := append([]string{"--net=" + nsPath, command}, args...)
	cmd := exec.CommandContext(ctx, "nsenter", fullArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w (output: %s)", command, err, string(output))
	}
	return nil
}

// PolicySummary returns a summary of the applied policy for logging.
func (n *NetworkIsolation) PolicySummary(policy *network.ResolvedPolicy) map[string]interface{} {
	if policy == nil {
		return nil
	}
	return policy.Summary()
}
