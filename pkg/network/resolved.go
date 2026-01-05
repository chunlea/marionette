package network

import (
	"net"
	"time"
)

// ResolvedPolicy represents a network policy with DNS resolution completed.
// IP addresses are pinned at resolution time to prevent DNS rebinding attacks.
type ResolvedPolicy struct {
	// OriginalPolicy is the policy that was resolved.
	OriginalPolicy *NetworkPolicy

	// AllowedIPs contains the resolved IP addresses for allowed hosts.
	// Each HostResolution maps a hostname pattern to its resolved IPs.
	AllowedIPs []HostResolution

	// AllowedPorts contains the ports that are allowed for connections.
	AllowedPorts []int

	// BlockedCIDRs contains network ranges that are always blocked.
	BlockedCIDRs []*net.IPNet

	// PinnedAt is when the DNS resolution was performed.
	PinnedAt time.Time

	// ExpiresAt is when the resolution should be refreshed.
	ExpiresAt time.Time
}

// HostResolution maps a hostname pattern to its resolved IP addresses.
type HostResolution struct {
	// Pattern is the original hostname pattern (e.g., "github.com" or "*.anthropic.com")
	Pattern string

	// Hosts are the actual hostnames that were resolved (for wildcards, this may be expanded)
	Hosts []string

	// IPs are the resolved IP addresses for all hosts
	IPs []net.IP

	// ResolvedAt is when this specific host was resolved
	ResolvedAt time.Time

	// Error contains any error that occurred during resolution
	Error error
}

// NewResolvedPolicy creates a ResolvedPolicy from the given policy and resolutions.
func NewResolvedPolicy(policy *NetworkPolicy, resolutions []HostResolution, ttl time.Duration) *ResolvedPolicy {
	now := time.Now()
	return &ResolvedPolicy{
		OriginalPolicy: policy,
		AllowedIPs:     resolutions,
		AllowedPorts:   policy.EffectivePorts(),
		BlockedCIDRs:   ParsedBlockedCIDRs,
		PinnedAt:       now,
		ExpiresAt:      now.Add(ttl),
	}
}

// AllIPs returns all resolved IP addresses across all host resolutions.
func (r *ResolvedPolicy) AllIPs() []net.IP {
	var result []net.IP
	for _, hr := range r.AllowedIPs {
		result = append(result, hr.IPs...)
	}
	return result
}

// AllIPsFiltered returns all resolved IP addresses with blocked IPs removed.
func (r *ResolvedPolicy) AllIPsFiltered() []net.IP {
	return FilterBlockedIPs(r.AllIPs())
}

// IsExpired returns true if the resolution has expired and should be refreshed.
func (r *ResolvedPolicy) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// HasErrors returns true if any host resolution failed.
func (r *ResolvedPolicy) HasErrors() bool {
	for _, hr := range r.AllowedIPs {
		if hr.Error != nil {
			return true
		}
	}
	return false
}

// Errors returns all resolution errors.
func (r *ResolvedPolicy) Errors() []error {
	var errs []error
	for _, hr := range r.AllowedIPs {
		if hr.Error != nil {
			errs = append(errs, hr.Error)
		}
	}
	return errs
}

// IsIPAllowed checks if an IP address is allowed by this policy.
// Returns true if the IP matches any resolved host and is not blocked.
func (r *ResolvedPolicy) IsIPAllowed(ip net.IP) bool {
	// First check if the IP is blocked
	if IsBlockedIP(ip) {
		return false
	}

	// Check if the IP is in any of the allowed resolutions
	for _, hr := range r.AllowedIPs {
		for _, allowedIP := range hr.IPs {
			if ip.Equal(allowedIP) {
				return true
			}
		}
	}

	return false
}

// IsPortAllowed checks if a port is allowed by this policy.
func (r *ResolvedPolicy) IsPortAllowed(port int) bool {
	for _, p := range r.AllowedPorts {
		if p == port {
			return true
		}
	}
	return false
}

// IsConnectionAllowed checks if a connection to the given IP and port is allowed.
func (r *ResolvedPolicy) IsConnectionAllowed(ip net.IP, port int) bool {
	return r.IsIPAllowed(ip) && r.IsPortAllowed(port)
}

// Summary returns a summary of the resolved policy for logging.
func (r *ResolvedPolicy) Summary() map[string]interface{} {
	hostCount := len(r.AllowedIPs)
	ipCount := len(r.AllIPs())
	filteredCount := len(r.AllIPsFiltered())

	return map[string]interface{}{
		"level":           r.OriginalPolicy.Level,
		"host_patterns":   hostCount,
		"resolved_ips":    ipCount,
		"allowed_ips":     filteredCount,
		"blocked_ips":     ipCount - filteredCount,
		"allowed_ports":   r.AllowedPorts,
		"pinned_at":       r.PinnedAt,
		"expires_at":      r.ExpiresAt,
		"has_errors":      r.HasErrors(),
	}
}
