// Package network provides network policy types and enforcement for session isolation.
package network

import (
	"fmt"
	"net"
	"strings"
)

// PolicyLevel defines the network isolation level for a session.
type PolicyLevel string

const (
	// PolicyNone allows full internet access without restrictions.
	PolicyNone PolicyLevel = "none"

	// PolicyAllowList restricts network access to a whitelist of domains.
	// DNS resolution is pinned at runner start and refreshed in place to
	// prevent DNS rebinding attacks.
	PolicyAllowList PolicyLevel = "allow_list"

	// PolicyProxy forces all HTTP(S) egress through a configured proxy.
	// Direct egress is dropped, so a tool that ignores the proxy environment
	// fails closed rather than escaping the policy.
	PolicyProxy PolicyLevel = "proxy"

	// PolicyAirGapped blocks all egress. Only the runner to server control
	// plane is permitted; external DNS is not.
	PolicyAirGapped PolicyLevel = "air_gapped"
)

// DefaultAllowedPorts are the ports allowed when no explicit ports are specified.
var DefaultAllowedPorts = []int{80, 443}

// DefaultControlPlanePort is assumed when the server address carries no port.
const DefaultControlPlanePort = 9090

// NetworkPolicy defines the network isolation configuration for a session.
type NetworkPolicy struct {
	// Level is the isolation level (none, allow_list, proxy, air_gapped).
	Level PolicyLevel

	// AllowedHosts contains host patterns for allow_list mode.
	// Supports wildcards: *.github.com, api.*.example.com
	AllowedHosts []string

	// AllowedPorts restricts which ports can be accessed.
	// If empty, defaults to [80, 443].
	AllowedPorts []int

	// BlockMetadata controls whether cloud metadata endpoints are blocked.
	// Always true for security; cannot be disabled.
	BlockMetadata bool

	// ControlPlane lists endpoints that stay reachable at every restricted
	// level: the Marionette server's gRPC address. Without it a restricted
	// runner cannot report back and the session is dead on arrival.
	ControlPlane []Endpoint

	// Proxy configures the egress proxy. Required for PolicyProxy, ignored
	// otherwise.
	Proxy *ProxyConfig

	// DNSServers are the resolver addresses the sandbox may reach on port 53.
	// Empty means "no explicit resolver known": see AllowsExternalDNS.
	DNSServers []string
}

// PolicyOption configures a NetworkPolicy during ParsePolicy.
type PolicyOption func(*NetworkPolicy)

// WithControlPlane sets the endpoints that must stay reachable.
func WithControlPlane(endpoints ...Endpoint) PolicyOption {
	return func(p *NetworkPolicy) {
		p.ControlPlane = append(p.ControlPlane, endpoints...)
	}
}

// WithProxy sets the egress proxy configuration.
func WithProxy(proxy *ProxyConfig) PolicyOption {
	return func(p *NetworkPolicy) {
		p.Proxy = proxy
	}
}

// WithDNSServers sets the resolver addresses the sandbox may reach.
func WithDNSServers(servers ...string) PolicyOption {
	return func(p *NetworkPolicy) {
		p.DNSServers = append(p.DNSServers, servers...)
	}
}

// WithAllowedPorts overrides the default allowed ports.
func WithAllowedPorts(ports ...int) PolicyOption {
	return func(p *NetworkPolicy) {
		if len(ports) > 0 {
			p.AllowedPorts = ports
		}
	}
}

// ParsePolicy creates a NetworkPolicy from configuration values.
// Returns an error if the level is invalid.
func ParsePolicy(level string, allowedHosts []string, opts ...PolicyOption) (*NetworkPolicy, error) {
	policyLevel := PolicyLevel(level)
	if err := validatePolicyLevel(policyLevel); err != nil {
		return nil, err
	}

	policy := &NetworkPolicy{
		Level:         policyLevel,
		AllowedHosts:  allowedHosts,
		AllowedPorts:  DefaultAllowedPorts,
		BlockMetadata: true, // Always block metadata endpoints
	}

	for _, opt := range opts {
		opt(policy)
	}

	if err := policy.Validate(); err != nil {
		return nil, err
	}

	return policy, nil
}

// Validate checks that the policy configuration is valid.
func (p *NetworkPolicy) Validate() error {
	if err := validatePolicyLevel(p.Level); err != nil {
		return err
	}

	// allow_list requires at least one allowed host
	if p.Level == PolicyAllowList && len(p.AllowedHosts) == 0 {
		return fmt.Errorf("allow_list policy requires at least one allowed host")
	}

	// proxy mode without a proxy would silently degrade to "drop everything".
	if p.Level == PolicyProxy {
		if p.Proxy == nil {
			return fmt.Errorf("proxy policy requires a proxy configuration")
		}
		if err := p.Proxy.Validate(); err != nil {
			return fmt.Errorf("invalid proxy configuration: %w", err)
		}
	}

	// Validate host patterns
	for _, host := range p.AllowedHosts {
		if err := validateHostPattern(host); err != nil {
			return fmt.Errorf("invalid host pattern %q: %w", host, err)
		}
	}

	for _, ep := range p.ControlPlane {
		if err := ep.Validate(); err != nil {
			return fmt.Errorf("invalid control plane endpoint: %w", err)
		}
	}

	for _, port := range p.AllowedPorts {
		if port <= 0 || port > 65535 {
			return fmt.Errorf("invalid allowed port %d", port)
		}
	}

	return nil
}

// IsRestricted returns true if network access is restricted (not "none").
//
// A nil policy is the "no isolation" case, so the predicates below tolerate
// one: callers get a policy or nil from a single Prepare step and should not
// have to guard every use.
func (p *NetworkPolicy) IsRestricted() bool {
	return p != nil && p.Level != PolicyNone
}

// IsAirGapped returns true if the policy completely blocks internet access.
func (p *NetworkPolicy) IsAirGapped() bool {
	return p != nil && p.Level == PolicyAirGapped
}

// RequiresDNSPinning returns true if the policy resolves names to IP rules and
// therefore needs a refresh loop to follow rotating records.
//
// Proxy mode is included: the proxy endpoint itself is pinned to an IP.
func (p *NetworkPolicy) RequiresDNSPinning() bool {
	return p != nil && (p.Level == PolicyAllowList || p.Level == PolicyProxy)
}

// AllowsExternalDNS reports whether the sandbox may send DNS queries off-box.
//
// Air-gapped runners may not: DNS is a general-purpose exfiltration channel,
// so the control-plane address is pinned into the container's /etc/hosts
// instead and port 53 stays closed.
func (p *NetworkPolicy) AllowsExternalDNS() bool {
	return p == nil || p.Level != PolicyAirGapped
}

// ControlPlaneHosts returns the host part of every control-plane endpoint.
func (p *NetworkPolicy) ControlPlaneHosts() []string {
	hosts := make([]string, 0, len(p.ControlPlane))
	for _, ep := range p.ControlPlane {
		hosts = append(hosts, ep.Host)
	}
	return hosts
}

// EffectivePorts returns the ports that should be allowed.
// Returns DefaultAllowedPorts if AllowedPorts is empty.
func (p *NetworkPolicy) EffectivePorts() []int {
	if len(p.AllowedPorts) == 0 {
		return DefaultAllowedPorts
	}
	return p.AllowedPorts
}

// validatePolicyLevel checks if the level is one of the known values.
func validatePolicyLevel(level PolicyLevel) error {
	switch level {
	case PolicyNone, PolicyAllowList, PolicyProxy, PolicyAirGapped:
		return nil
	case "":
		return fmt.Errorf("policy level is required")
	default:
		return fmt.Errorf("unknown policy level: %s", level)
	}
}

// validateHostPattern checks if a host pattern is valid.
//
// An entry is a hostname (optionally with a leading or embedded wildcard), an
// IP literal, or a CIDR block.
func validateHostPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("empty pattern")
	}

	// A network block, e.g. 10.20.0.0/16 or 2001:db8::/32.
	if strings.Contains(pattern, "/") {
		if _, _, err := net.ParseCIDR(pattern); err != nil {
			return fmt.Errorf("not a valid CIDR: %w", err)
		}
		return nil
	}

	// An IP literal, including IPv6 which is full of colons.
	if net.ParseIP(pattern) != nil {
		return nil
	}

	// A trailing port used to be accepted and then silently ignored, which
	// quietly widened the rule to every allowed port. Say so instead.
	if strings.Contains(pattern, ":") {
		return fmt.Errorf("a port is not allowed here; set allowed_ports on the policy instead")
	}

	// Check for invalid characters
	for _, r := range pattern {
		if !isValidHostChar(r) {
			return fmt.Errorf("invalid character %q", r)
		}
	}

	// Wildcard validation
	parts := strings.Split(pattern, ".")
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("empty label")
		}

		// Wildcard must be a complete label
		if strings.Contains(part, "*") {
			if part != "*" {
				return fmt.Errorf("wildcard must be a complete label, got %q", part)
			}
			// Wildcard cannot be the only label (just "*")
			if len(parts) == 1 {
				return fmt.Errorf("wildcard-only pattern not allowed")
			}
		}

		// First and last character of non-wildcard labels cannot be hyphen
		if part != "*" {
			if strings.HasPrefix(part, "-") || strings.HasSuffix(part, "-") {
				return fmt.Errorf("label %q cannot start or end with hyphen", part)
			}
		}

		// Check label length (max 63 characters per DNS spec)
		if len(part) > 63 {
			return fmt.Errorf("label %q exceeds 63 characters", part[:20]+"...")
		}
	}

	return nil
}

// isValidHostChar returns true if the rune is valid in a host pattern.
func isValidHostChar(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '-' || r == '.' || r == '*'
}
