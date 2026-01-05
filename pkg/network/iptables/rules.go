package iptables

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// RuleAction defines what happens when a rule matches.
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

// Rule represents an iptables rule.
type Rule struct {
	// Chain is the chain to add the rule to.
	Chain string

	// Action is what to do when the rule matches.
	Action RuleAction

	// Protocol is the network protocol to match (tcp, udp, icmp, all).
	Protocol RuleProtocol

	// DestIP is the destination IP address or CIDR to match.
	DestIP net.IP

	// DestCIDR is the destination network to match (alternative to DestIP).
	DestCIDR *net.IPNet

	// DestPort is the destination port to match (for TCP/UDP).
	DestPort int

	// SourceIP is the source IP address or CIDR to match.
	SourceIP net.IP

	// SourceCIDR is the source network to match (alternative to SourceIP).
	SourceCIDR *net.IPNet

	// SourcePort is the source port to match (for TCP/UDP).
	SourcePort int

	// State matches connection tracking states (ESTABLISHED, RELATED, NEW, etc.).
	State []string

	// Comment is an optional comment for the rule.
	Comment string

	// LogPrefix is the prefix for LOG action.
	LogPrefix string

	// IsIPv6 indicates this rule is for ip6tables.
	IsIPv6 bool
}

// ToArgs converts the rule to iptables command-line arguments.
// Returns arguments for appending to a chain with -A.
func (r *Rule) ToArgs() []string {
	args := []string{"-A", r.Chain}

	// Protocol
	if r.Protocol != "" && r.Protocol != ProtocolAll {
		args = append(args, "-p", string(r.Protocol))
	}

	// Source
	if r.SourceIP != nil {
		args = append(args, "-s", r.SourceIP.String())
	} else if r.SourceCIDR != nil {
		args = append(args, "-s", r.SourceCIDR.String())
	}

	// Source port
	if r.SourcePort > 0 && (r.Protocol == ProtocolTCP || r.Protocol == ProtocolUDP) {
		args = append(args, "--sport", strconv.Itoa(r.SourcePort))
	}

	// Destination
	if r.DestIP != nil {
		args = append(args, "-d", r.DestIP.String())
	} else if r.DestCIDR != nil {
		args = append(args, "-d", r.DestCIDR.String())
	}

	// Destination port
	if r.DestPort > 0 && (r.Protocol == ProtocolTCP || r.Protocol == ProtocolUDP) {
		args = append(args, "--dport", strconv.Itoa(r.DestPort))
	}

	// Connection state
	if len(r.State) > 0 {
		args = append(args, "-m", "state", "--state", strings.Join(r.State, ","))
	}

	// Comment
	if r.Comment != "" {
		args = append(args, "-m", "comment", "--comment", r.Comment)
	}

	// Action
	args = append(args, "-j", string(r.Action))

	// Log prefix (for LOG action)
	if r.Action == ActionLog && r.LogPrefix != "" {
		args = append(args, "--log-prefix", r.LogPrefix)
	}

	return args
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

	// Rules are the rules to add to the chain.
	Rules []Rule

	// FlushFirst indicates whether to flush the chain before adding rules.
	FlushFirst bool

	// IPv6Rules are additional IPv6-specific rules.
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

// AddAllowEstablished adds a rule to allow established and related connections.
func (rs *RuleSet) AddAllowEstablished() {
	rs.Rules = append(rs.Rules, Rule{
		Chain:   rs.ChainName,
		Action:  ActionAccept,
		State:   []string{"ESTABLISHED", "RELATED"},
		Comment: "Allow established connections",
	})
}

// AddAllowIP adds a rule to allow traffic to a specific IP and port.
func (rs *RuleSet) AddAllowIP(ip net.IP, port int, comment string) {
	rule := Rule{
		Chain:    rs.ChainName,
		Action:   ActionAccept,
		Protocol: ProtocolTCP,
		DestIP:   ip,
		DestPort: port,
		Comment:  comment,
		IsIPv6:   ip.To4() == nil,
	}

	if rule.IsIPv6 {
		rs.IPv6Rules = append(rs.IPv6Rules, rule)
	} else {
		rs.Rules = append(rs.Rules, rule)
	}
}

// AddBlockCIDR adds a rule to block traffic to a CIDR.
func (rs *RuleSet) AddBlockCIDR(cidr *net.IPNet, comment string) {
	rule := Rule{
		Chain:    rs.ChainName,
		Action:   ActionDrop,
		DestCIDR: cidr,
		Comment:  comment,
		IsIPv6:   cidr.IP.To4() == nil,
	}

	if rule.IsIPv6 {
		rs.IPv6Rules = append(rs.IPv6Rules, rule)
	} else {
		rs.Rules = append(rs.Rules, rule)
	}
}

// AddDefaultDrop adds a rule to drop all traffic that doesn't match previous rules.
func (rs *RuleSet) AddDefaultDrop() {
	rs.Rules = append(rs.Rules, Rule{
		Chain:   rs.ChainName,
		Action:  ActionDrop,
		Comment: "Default drop",
	})

	rs.IPv6Rules = append(rs.IPv6Rules, Rule{
		Chain:   rs.ChainName,
		Action:  ActionDrop,
		Comment: "Default drop IPv6",
		IsIPv6:  true,
	})
}

// Validate checks if the rule set is valid.
func (rs *RuleSet) Validate() error {
	if rs.ChainName == "" {
		return fmt.Errorf("chain name is required")
	}

	// Chain name must be alphanumeric with underscores
	for _, c := range rs.ChainName {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return fmt.Errorf("invalid chain name character: %c", c)
		}
	}

	if len(rs.ChainName) > 28 {
		return fmt.Errorf("chain name exceeds 28 characters")
	}

	return nil
}
