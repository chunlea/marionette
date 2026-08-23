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

	// DynChainSuffix marks the chain holding refreshable allow-list rules.
	DynChainSuffix = "_D"
)

// Manager manages iptables rules for session network isolation.
//
// Rules live in two chains per runner. The main chain holds everything that
// never changes while the runner lives: loopback, connection state, the
// operator's pinned endpoints, the blocked ranges, and the default drop. The
// dynamic chain holds only the allow-list addresses, which the DNS refresher
// adds to and removes from as records rotate.
//
// The split is what makes refreshing safe. Appending to the dynamic chain can
// never land an allow rule ahead of the metadata-endpoint block, because the
// blocks are in the parent chain and are evaluated before the jump.
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

// ChainName generates the main chain name for a runner key.
//
// The name is truncated with room for DynChainSuffix so both chains fit inside
// the iptables limit.
func (m *Manager) ChainName(key string) string {
	id := strings.TrimPrefix(key, "sess_")

	var sb strings.Builder
	sb.WriteString(m.chainPrefix)
	for _, r := range id {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_':
			sb.WriteRune(r)
		default:
			sb.WriteRune('_')
		}
	}

	name := sb.String()
	if max := MaxChainNameLength - len(DynChainSuffix); len(name) > max {
		name = name[:max]
	}
	return name
}

// DynChainName returns the chain holding refreshable allow-list rules.
func (m *Manager) DynChainName(key string) string {
	return m.ChainName(key) + DynChainSuffix
}

// BuildRuleSets renders a resolved policy into the main and dynamic chains.
func (m *Manager) BuildRuleSets(key string, policy *network.ResolvedPolicy) (main, dyn *RuleSet) {
	mainChain := m.ChainName(key)
	dynChain := m.DynChainName(key)

	main = NewRuleSet(mainChain)
	dyn = NewRuleSet(dynChain)

	// 1. Traffic that never leaves the sandbox.
	main.AddAllowLoopback()

	// 2. Replies to connections the sandbox already opened.
	main.AddAllowEstablished()

	// 3. Operator pins, ahead of the blanket blocks. The control plane, the
	//    proxy and the resolvers normally sit on a private network, which is
	//    itself a blocked range; ordering them after the blocks would cut the
	//    runner off from the server it is supposed to report to.
	for _, er := range policy.ControlPlane {
		for _, ip := range er.IPs {
			main.AddAllowIP(ip, er.Endpoint.Port, "Allow control plane "+er.Endpoint.String())
		}
	}

	if policy.Proxy != nil {
		for _, ip := range policy.Proxy.IPs {
			main.AddAllowIP(ip, policy.Proxy.Endpoint.Port, "Allow proxy "+policy.Proxy.Endpoint.String())
		}
	}

	for _, ip := range policy.DNSServers {
		main.AddAllowDNSServer(ip, "Allow DNS resolver "+ip.String())
	}

	// 4. Always-blocked ranges: cloud metadata, loopback by address, and the
	//    private networks an agent could use for lateral movement.
	for _, cidr := range policy.BlockedCIDRs {
		main.AddBlockCIDR(cidr, "Block "+cidr.String())
	}

	// 5. If no resolver could be discovered, allow-list and proxy modes still
	//    need name resolution to function at all. Air-gapped never does.
	if len(policy.DNSServers) == 0 && policy.OriginalPolicy != nil && policy.OriginalPolicy.AllowsExternalDNS() {
		main.AddAllowDNSAny("Allow DNS (no resolver pinned)")
	}

	// 6. The refreshable allow list. Empty for proxy and air_gapped, which
	//    reach the outside world only through the pins above.
	main.AddJump(dynChain, "Allow-list")

	// 7. Everything else.
	main.AddDefaultDrop()

	for _, rule := range m.dynamicRules(dynChain, policy) {
		if rule.IsIPv6 {
			dyn.IPv6Rules = append(dyn.IPv6Rules, rule)
		} else {
			dyn.Rules = append(dyn.Rules, rule)
		}
	}

	return main, dyn
}

// dynamicRules renders the allow-list addresses for the dynamic chain.
func (m *Manager) dynamicRules(dynChain string, policy *network.ResolvedPolicy) []Rule {
	if policy.Level() != network.PolicyAllowList {
		return nil
	}
	return allowRules(dynChain, policy.AllIPsFiltered(), policy.AllowedPorts)
}

// allowRules renders one accept rule per address and port.
func allowRules(chain string, ips []net.IP, ports []int) []Rule {
	rules := make([]Rule, 0, len(ips)*len(ports))
	for _, ip := range ips {
		if network.IsBlockedIP(ip) {
			continue
		}
		for _, port := range ports {
			rules = append(rules, Rule{
				Chain:    chain,
				Action:   ActionAccept,
				Protocol: ProtocolTCP,
				DestIP:   ip,
				DestPort: port,
				Comment:  fmt.Sprintf("Allow %s:%d", ip.String(), port),
				IsIPv6:   ip.To4() == nil,
			})
		}
	}
	return rules
}

// Install writes the complete rule set for a runner and links it into OUTPUT.
//
// It is idempotent: both chains are flushed first, so re-installing over a
// partially configured namespace converges instead of stacking duplicates.
func (m *Manager) Install(ctx context.Context, key string, policy *network.ResolvedPolicy) error {
	if policy == nil {
		return fmt.Errorf("policy is nil")
	}

	main, dyn := m.BuildRuleSets(key, policy)
	if err := main.Validate(); err != nil {
		return fmt.Errorf("invalid rule set: %w", err)
	}
	if err := dyn.Validate(); err != nil {
		return fmt.Errorf("invalid dynamic rule set: %w", err)
	}

	// Both chains must exist before the main chain jumps to the dynamic one.
	if err := m.createChain(ctx, main.ChainName); err != nil {
		return err
	}
	if err := m.createChain(ctx, dyn.ChainName); err != nil {
		return err
	}

	if err := m.flushChain(ctx, main.ChainName); err != nil {
		return err
	}
	if err := m.flushChain(ctx, dyn.ChainName); err != nil {
		return err
	}

	if err := m.applyRules(ctx, dyn); err != nil {
		return err
	}
	if err := m.applyRules(ctx, main); err != nil {
		return err
	}

	if err := m.LinkChainToOutput(ctx, key); err != nil {
		return err
	}

	m.mu.Lock()
	m.activeChains[main.ChainName] = true
	m.mu.Unlock()

	return nil
}

// Uninstall removes a runner's chains and the OUTPUT jump.
func (m *Manager) Uninstall(ctx context.Context, key string) error {
	var firstErr error
	record := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	record(m.UnlinkChainFromOutput(ctx, key))
	record(m.deleteChain(ctx, m.ChainName(key)))
	record(m.deleteChain(ctx, m.DynChainName(key)))

	m.mu.Lock()
	delete(m.activeChains, m.ChainName(key))
	m.mu.Unlock()

	return firstErr
}

// Installed reports whether the runner's rules are still in place.
//
// A container restart inside a session hands the runner a brand new network
// namespace with none of our rules in it. Without this check the refresher
// would diff against its own memory, conclude nothing changed, and leave the
// sandbox with unrestricted egress.
func (m *Manager) Installed(ctx context.Context, key string) (bool, error) {
	chain := m.ChainName(key)

	if _, err := m.executor.Output(ctx, "-S", chain); err != nil {
		if isMissingChain(err) {
			return false, nil
		}
		return false, fmt.Errorf("listing chain %s: %w", chain, err)
	}

	jump := m.outputJumpRule(chain)
	if err := m.executor.Run(ctx, jump.Args("-C")...); err != nil {
		if isMissingRule(err) || isMissingChain(err) {
			return false, nil
		}
		return false, fmt.Errorf("checking OUTPUT jump for %s: %w", chain, err)
	}

	return true, nil
}

// Allow appends accept rules for the given addresses to the dynamic chain.
// Existing rules are left alone, so repeated calls converge.
func (m *Manager) Allow(ctx context.Context, key string, ips []net.IP, ports []int) error {
	for _, rule := range allowRules(m.DynChainName(key), ips, ports) {
		rule := rule
		if err := m.ensureRule(ctx, &rule); err != nil {
			return err
		}
	}
	return nil
}

// Deny removes accept rules for the given addresses from the dynamic chain.
// Rules that are already gone are not an error.
func (m *Manager) Deny(ctx context.Context, key string, ips []net.IP, ports []int) error {
	for _, rule := range allowRules(m.DynChainName(key), ips, ports) {
		rule := rule
		if err := m.removeRule(ctx, &rule); err != nil {
			return err
		}
	}
	return nil
}

// LinkChainToOutput adds a jump rule from OUTPUT to the runner's chain.
// The jump is inserted at the head so it runs before anything else.
func (m *Manager) LinkChainToOutput(ctx context.Context, key string) error {
	rule := m.outputJumpRule(m.ChainName(key))

	if err := m.executor.Run(ctx, rule.Args("-C")...); err == nil {
		return nil // already linked
	} else if !isMissingRule(err) && !isMissingChain(err) {
		return fmt.Errorf("checking OUTPUT jump: %w", err)
	}

	if err := m.executor.Run(ctx, rule.Args("-I")...); err != nil {
		return fmt.Errorf("link chain to OUTPUT: %w", err)
	}

	v6 := rule
	v6.IsIPv6 = true
	if err := m.executor.RunIPv6(ctx, v6.Args("-C")...); err == nil {
		return nil
	} else if !isMissingRule(err) && !isMissingChain(err) {
		return fmt.Errorf("checking IPv6 OUTPUT jump: %w", err)
	}

	if err := m.executor.RunIPv6(ctx, v6.Args("-I")...); err != nil {
		return fmt.Errorf("link IPv6 chain to OUTPUT: %w", err)
	}

	return nil
}

// UnlinkChainFromOutput removes the jump rule from OUTPUT.
func (m *Manager) UnlinkChainFromOutput(ctx context.Context, key string) error {
	rule := m.outputJumpRule(m.ChainName(key))

	if err := m.executor.Run(ctx, rule.Args("-D")...); err != nil {
		if !isMissingRule(err) && !isMissingChain(err) {
			return fmt.Errorf("unlink chain from OUTPUT: %w", err)
		}
	}

	v6 := rule
	v6.IsIPv6 = true
	if err := m.executor.RunIPv6(ctx, v6.Args("-D")...); err != nil {
		if !isMissingRule(err) && !isMissingChain(err) {
			return fmt.Errorf("unlink IPv6 chain from OUTPUT: %w", err)
		}
	}

	return nil
}

// ChainExists checks if the runner's main chain exists.
func (m *Manager) ChainExists(ctx context.Context, key string) (bool, error) {
	chain := m.ChainName(key)
	if _, err := m.executor.Output(ctx, "-S", chain); err != nil {
		if isMissingChain(err) {
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

// CleanupAllChains removes every chain this manager created.
func (m *Manager) CleanupAllChains(ctx context.Context) error {
	var lastErr error
	for _, chain := range m.ListActiveChains() {
		key := strings.TrimPrefix(chain, m.chainPrefix)
		if err := m.Uninstall(ctx, key); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// outputJumpRule renders the OUTPUT -> runner chain jump.
func (m *Manager) outputJumpRule(chain string) Rule {
	return Rule{
		Chain:  "OUTPUT",
		Action: RuleAction(chain),
	}
}

func (m *Manager) createChain(ctx context.Context, chain string) error {
	if err := m.executor.Run(ctx, "-N", chain); err != nil && !isChainExists(err) {
		return fmt.Errorf("create chain %s: %w", chain, err)
	}
	if err := m.executor.RunIPv6(ctx, "-N", chain); err != nil && !isChainExists(err) {
		return fmt.Errorf("create IPv6 chain %s: %w", chain, err)
	}
	return nil
}

func (m *Manager) deleteChain(ctx context.Context, chain string) error {
	_ = m.executor.Run(ctx, "-F", chain)
	if err := m.executor.Run(ctx, "-X", chain); err != nil && !isMissingChain(err) {
		return fmt.Errorf("delete chain %s: %w", chain, err)
	}

	_ = m.executor.RunIPv6(ctx, "-F", chain)
	if err := m.executor.RunIPv6(ctx, "-X", chain); err != nil && !isMissingChain(err) {
		return fmt.Errorf("delete IPv6 chain %s: %w", chain, err)
	}
	return nil
}

func (m *Manager) flushChain(ctx context.Context, chain string) error {
	if err := m.executor.Run(ctx, "-F", chain); err != nil {
		return fmt.Errorf("flush chain %s: %w", chain, err)
	}
	if err := m.executor.RunIPv6(ctx, "-F", chain); err != nil {
		return fmt.Errorf("flush IPv6 chain %s: %w", chain, err)
	}
	return nil
}

func (m *Manager) applyRules(ctx context.Context, rs *RuleSet) error {
	for i := range rs.Rules {
		if err := m.executor.Run(ctx, rs.Rules[i].ToArgs()...); err != nil {
			return fmt.Errorf("apply rule %s: %w", rs.Rules[i].String(), err)
		}
	}
	for i := range rs.IPv6Rules {
		if err := m.executor.RunIPv6(ctx, rs.IPv6Rules[i].ToArgs()...); err != nil {
			return fmt.Errorf("apply IPv6 rule %s: %w", rs.IPv6Rules[i].String(), err)
		}
	}
	return nil
}

// ensureRule appends a rule unless an identical one already exists.
func (m *Manager) ensureRule(ctx context.Context, rule *Rule) error {
	run, family := m.executor.Run, "IPv4"
	if rule.IsIPv6 {
		run, family = m.executor.RunIPv6, "IPv6"
	}

	if err := run(ctx, rule.Args("-C")...); err == nil {
		return nil
	} else if !isMissingRule(err) && !isMissingChain(err) {
		return fmt.Errorf("checking %s rule %s: %w", family, rule.String(), err)
	}

	if err := run(ctx, rule.Args("-A")...); err != nil {
		return fmt.Errorf("adding %s rule %s: %w", family, rule.String(), err)
	}
	return nil
}

// removeRule deletes a rule, tolerating one that is already gone.
func (m *Manager) removeRule(ctx context.Context, rule *Rule) error {
	run, family := m.executor.Run, "IPv4"
	if rule.IsIPv6 {
		run, family = m.executor.RunIPv6, "IPv6"
	}

	if err := run(ctx, rule.Args("-D")...); err != nil {
		if isMissingRule(err) || isMissingChain(err) {
			return nil
		}
		return fmt.Errorf("removing %s rule %s: %w", family, rule.String(), err)
	}
	return nil
}

// isChainExists matches iptables' "chain already exists" complaint.
func isChainExists(err error) bool {
	return err != nil && strings.Contains(err.Error(), "Chain already exists")
}

// isMissingChain matches the several ways iptables reports an absent chain.
func isMissingChain(err error) bool {
	if err == nil {
		return false
	}
	// "iptables: No chain/target/match by that name."
	return strings.Contains(err.Error(), "No chain")
}

// isMissingRule matches iptables' response to -C or -D for an absent rule.
func isMissingRule(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Bad rule") ||
		strings.Contains(msg, "does a matching rule exist") ||
		strings.Contains(msg, "No chain/target/match by that name")
}

// CreateBlockedCIDRs returns the always-blocked ranges.
func CreateBlockedCIDRs() []*net.IPNet {
	return network.ParsedBlockedCIDRs
}
