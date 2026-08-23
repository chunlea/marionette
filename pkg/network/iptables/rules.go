package iptables

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// RuleAction defines what happens when a rule matches. It is also the jump
// target, so a custom chain name is a valid action.
type RuleAction string

const (
	// ActionAccept allows the packet.
	ActionAccept RuleAction = "ACCEPT"

	// ActionDrop silently drops the packet.
	ActionDrop RuleAction = "DROP"

	// ActionReject rejects the packet with an ICMP error.
	ActionReject RuleAction = "REJECT"

	// ActionLog logs the packet (usually combined with other rules).
	ActionLog RuleAction = "LOG"
)

// RuleProtocol defines the network protocol for a rule.
type RuleProtocol string

const (
	// ProtocolTCP matches TCP packets.
	ProtocolTCP RuleProtocol = "tcp"

	// ProtocolUDP matches UDP packets.
	ProtocolUDP RuleProtocol = "udp"

	// ProtocolICMP matches ICMP packets.
	ProtocolICMP RuleProtocol = "icmp"

	// ProtocolAll matches all protocols.
	ProtocolAll RuleProtocol = "all"
)

// ConntrackEstablished is the state match for replies to traffic the sandbox
// itself opened.
//
// This uses -m conntrack rather than the legacy -m state: xt_state is a compat
// shim that minimal images and nftables-backed iptables do not always carry.
var ConntrackEstablished = []string{"ESTABLISHED", "RELATED"}

// Rule represents an iptables rule.
type Rule struct {
	// Chain is the chain to add the rule to.
	Chain string

	// Action is what to do when the rule matches, or the chain to jump to.
	Action RuleAction

	// Protocol is the network protocol to match (tcp, udp, icmp, all).
	Protocol RuleProtocol

	// DestIP is the destination IP address to match.
	DestIP net.IP

	// DestCIDR is the destination network to match (alternative to DestIP).
	DestCIDR *net.IPNet

	// DestPort is the destination port to match (for TCP/UDP).
	DestPort int

	// SourceIP is the source IP address to match.
	SourceIP net.IP

	// SourceCIDR is the source network to match (alternative to SourceIP).
	SourceCIDR *net.IPNet

	// SourcePort is the source port to match (for TCP/UDP).
	SourcePort int

	// OutInterface matches the outgoing interface, e.g. "lo".
	OutInterface string

	// State matches connection tracking states (ESTABLISHED, RELATED, ...).
	State []string

	// Comment is an optional comment for the rule.
	Comment string

	// LogPrefix is the prefix for LOG action.
	LogPrefix string

	// IsIPv6 indicates this rule is for ip6tables.
	IsIPv6 bool
}

// Args renders the rule with the given verb: -A to append, -I to insert,
// -D to delete, -C to test for existence.
//
// Delete and check must reproduce the append form exactly, comment included,
// or iptables will not find the rule. That is why every caller goes through
// this one renderer.
func (r *Rule) Args(verb string) []string {
	args := []string{verb, r.Chain}

	if r.Protocol != "" && r.Protocol != ProtocolAll {
		args = append(args, "-p", string(r.Protocol))
	}

	if r.SourceIP != nil {
		args = append(args, "-s", r.SourceIP.String())
	} else if r.SourceCIDR != nil {
		args = append(args, "-s", r.SourceCIDR.String())
	}

	if r.SourcePort > 0 && (r.Protocol == ProtocolTCP || r.Protocol == ProtocolUDP) {
		args = append(args, "--sport", strconv.Itoa(r.SourcePort))
	}

	if r.DestIP != nil {
		args = append(args, "-d", r.DestIP.String())
	} else if r.DestCIDR != nil {
		args = append(args, "-d", r.DestCIDR.String())
	}

	if r.DestPort > 0 && (r.Protocol == ProtocolTCP || r.Protocol == ProtocolUDP) {
		args = append(args, "--dport", strconv.Itoa(r.DestPort))
	}

	if r.OutInterface != "" {
		args = append(args, "-o", r.OutInterface)
	}

	if len(r.State) > 0 {
		args = append(args, "-m", "conntrack", "--ctstate", strings.Join(r.State, ","))
	}

	if r.Comment != "" {
		args = append(args, "-m", "comment", "--comment", r.Comment)
	}

	args = append(args, "-j", string(r.Action))

	if r.Action == ActionLog && r.LogPrefix != "" {
		args = append(args, "--log-prefix", r.LogPrefix)
	}

	return args
}

// ToArgs converts the rule to append (-A) arguments.
func (r *Rule) ToArgs() []string {
	return r.Args("-A")
}

// String returns a human-readable representation of the rule.
func (r *Rule) String() string {
	var sb strings.Builder
	sb.WriteString(r.Chain)
	sb.WriteString(": ")

	if r.Protocol != "" && r.Protocol != ProtocolAll {
		sb.WriteString(string(r.Protocol))
		sb.WriteString(" ")
	}

	if r.OutInterface != "" {
		sb.WriteString("out ")
		sb.WriteString(r.OutInterface)
		sb.WriteString(" ")
	}

	if r.SourceIP != nil {
		sb.WriteString("from ")
		sb.WriteString(r.SourceIP.String())
		sb.WriteString(" ")
	} else if r.SourceCIDR != nil {
		sb.WriteString("from ")
		sb.WriteString(r.SourceCIDR.String())
		sb.WriteString(" ")
	}

	if r.DestIP != nil {
		sb.WriteString("to ")
		sb.WriteString(r.DestIP.String())
	} else if r.DestCIDR != nil {
		sb.WriteString("to ")
		sb.WriteString(r.DestCIDR.String())
	}

	if r.DestPort > 0 {
		sb.WriteString(":")
		sb.WriteString(strconv.Itoa(r.DestPort))
	}

	sb.WriteString(" -> ")
	sb.WriteString(string(r.Action))

	return sb.String()
}

// RuleSet is a collection of rules to apply to a chain.
type RuleSet struct {
	// ChainName is the custom chain name.
	ChainName string

	// Rules are the IPv4 rules to add to the chain.
	Rules []Rule

	// FlushFirst indicates whether to flush the chain before adding rules.
	FlushFirst bool

	// IPv6Rules are the ip6tables rules.
	IPv6Rules []Rule
}

// NewRuleSet creates a new rule set for a chain.
func NewRuleSet(chainName string) *RuleSet {
	return &RuleSet{
		ChainName:  chainName,
		Rules:      make([]Rule, 0),
		IPv6Rules:  make([]Rule, 0),
		FlushFirst: true,
	}
}

// addBoth appends a rule to both the IPv4 and IPv6 sets. Structural rules
// (loopback, state, jumps, the default drop) must exist in both families:
// an IPv6-capable container with an IPv4-only chain has no policy at all.
func (rs *RuleSet) addBoth(r Rule) {
	v4 := r
	v4.Chain = rs.ChainName
	rs.Rules = append(rs.Rules, v4)

	v6 := r
	v6.Chain = rs.ChainName
	v6.IsIPv6 = true
	rs.IPv6Rules = append(rs.IPv6Rules, v6)
}

// addByFamily appends a rule to the set matching the address family.
func (rs *RuleSet) addByFamily(r Rule, isIPv6 bool) {
	r.Chain = rs.ChainName
	r.IsIPv6 = isIPv6
	if isIPv6 {
		rs.IPv6Rules = append(rs.IPv6Rules, r)
	} else {
		rs.Rules = append(rs.Rules, r)
	}
}

// AddAllowLoopback permits traffic that never leaves the sandbox.
//
// The container's own resolver stub (Docker publishes 127.0.0.11) and any
// local helper the agent runs live here. Dropping loopback breaks name
// resolution and the agent's own sandbox plumbing without isolating anything.
func (rs *RuleSet) AddAllowLoopback() {
	rs.addBoth(Rule{
		Action:       ActionAccept,
		OutInterface: "lo",
		Comment:      "Allow loopback",
	})
}

// AddAllowEstablished adds a rule to allow established and related connections.
func (rs *RuleSet) AddAllowEstablished() {
	rs.addBoth(Rule{
		Action:  ActionAccept,
		State:   ConntrackEstablished,
		Comment: "Allow established connections",
	})
}

// AddAllowIP adds a rule to allow TCP traffic to a specific IP and port.
func (rs *RuleSet) AddAllowIP(ip net.IP, port int, comment string) {
	rs.addByFamily(Rule{
		Action:   ActionAccept,
		Protocol: ProtocolTCP,
		DestIP:   ip,
		DestPort: port,
		Comment:  comment,
	}, ip.To4() == nil)
}

// AddAllowDNSServer permits UDP and TCP queries to one resolver.
func (rs *RuleSet) AddAllowDNSServer(ip net.IP, comment string) {
	isIPv6 := ip.To4() == nil
	for _, proto := range []RuleProtocol{ProtocolUDP, ProtocolTCP} {
		rs.addByFamily(Rule{
			Action:   ActionAccept,
			Protocol: proto,
			DestIP:   ip,
			DestPort: 53,
			Comment:  comment,
		}, isIPv6)
	}
}

// AddAllowDNSAny permits DNS to any destination.
//
// This is the fallback when no resolver address could be discovered. It is a
// real widening of the policy: DNS is a usable exfiltration channel, and this
// rule is the reason allow_list cannot claim to stop a determined agent from
// leaking data. Air-gapped mode never uses it.
func (rs *RuleSet) AddAllowDNSAny(comment string) {
	for _, proto := range []RuleProtocol{ProtocolUDP, ProtocolTCP} {
		rs.addBoth(Rule{
			Action:   ActionAccept,
			Protocol: proto,
			DestPort: 53,
			Comment:  comment,
		})
	}
}

// AddBlockCIDR adds a rule to block traffic to a CIDR.
func (rs *RuleSet) AddBlockCIDR(cidr *net.IPNet, comment string) {
	rs.addByFamily(Rule{
		Action:   ActionDrop,
		DestCIDR: cidr,
		Comment:  comment,
	}, cidr.IP.To4() == nil)
}

// AddJump sends matching traffic to another chain.
func (rs *RuleSet) AddJump(target, comment string) {
	rs.addBoth(Rule{
		Action:  RuleAction(target),
		Comment: comment,
	})
}

// AddDefaultDrop adds a rule to drop all traffic that doesn't match previous rules.
func (rs *RuleSet) AddDefaultDrop() {
	rs.addBoth(Rule{
		Action:  ActionDrop,
		Comment: "Default drop",
	})
}

// Validate checks if the rule set is valid.
func (rs *RuleSet) Validate() error {
	if rs.ChainName == "" {
		return fmt.Errorf("chain name is required")
	}

	if err := ValidateChainName(rs.ChainName); err != nil {
		return err
	}

	for _, r := range append(append([]Rule{}, rs.Rules...), rs.IPv6Rules...) {
		if r.Action == "" {
			return fmt.Errorf("rule in chain %s has no action", rs.ChainName)
		}
	}

	return nil
}

// ValidateChainName checks a chain name against iptables' constraints.
func ValidateChainName(name string) error {
	for _, c := range name {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return fmt.Errorf("invalid chain name character: %c", c)
		}
	}

	if len(name) > MaxChainNameLength {
		return fmt.Errorf("chain name exceeds %d characters", MaxChainNameLength)
	}

	return nil
}
