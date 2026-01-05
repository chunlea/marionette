// Package network provides network policy types and enforcement for session isolation.
package network

import (
	"fmt"
	"strings"
)

// PolicyLevel defines the network isolation level for a session.
type PolicyLevel string

const (
	// PolicyNone allows full internet access without restrictions.
	PolicyNone PolicyLevel = "none"

	// PolicyAllowList restricts network access to a whitelist of domains.
	// DNS resolution is pinned at task start to prevent DNS rebinding attacks.
	PolicyAllowList PolicyLevel = "allow_list"

	// PolicyProxy routes all traffic through a transparent proxy for
	// inspection and logging. Enables TLS termination (MITM) for visibility.
	PolicyProxy PolicyLevel = "proxy"

	// PolicyAirGapped blocks all internet access. Only Marionette server
	// gRPC and inbound streaming tunnels are permitted.
	PolicyAirGapped PolicyLevel = "air_gapped"
)

// DefaultAllowedPorts are the ports allowed when no explicit ports are specified.
var DefaultAllowedPorts = []int{80, 443}

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
}

// ParsePolicy creates a NetworkPolicy from configuration values.
// Returns an error if the level is invalid.
func ParsePolicy(level string, allowedHosts []string) (*NetworkPolicy, error) {
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

	// Validate host patterns
	for _, host := range p.AllowedHosts {
		if err := validateHostPattern(host); err != nil {
			return fmt.Errorf("invalid host pattern %q: %w", host, err)
		}
	}

	return nil
}

// IsRestricted returns true if network access is restricted (not "none").
func (p *NetworkPolicy) IsRestricted() bool {
	return p.Level != PolicyNone
}

// IsAirGapped returns true if the policy completely blocks internet access.
func (p *NetworkPolicy) IsAirGapped() bool {
	return p.Level == PolicyAirGapped
}

// RequiresDNSPinning returns true if the policy requires DNS pinning.
func (p *NetworkPolicy) RequiresDNSPinning() bool {
	return p.Level == PolicyAllowList
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
func validateHostPattern(pattern string) error {
	if pattern == "" {
		return fmt.Errorf("empty pattern")
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
