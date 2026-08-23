package network

import (
	"bytes"
	"net"
	"sort"
	"time"
)

// ResolvedPolicy represents a network policy with DNS resolution completed.
// IP addresses are pinned at resolution time to prevent DNS rebinding attacks.
//
// A pinned set goes stale as soon as a CDN rotates a record, which is why a
// Refresher re-resolves it on a cadence and applies the difference to the live
// firewall instead of leaving the original pins in place forever.
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

	// ControlPlane holds the pinned Marionette server endpoints. These stay
	// reachable at every restricted level, including air_gapped.
	ControlPlane []EndpointResolution

	// Proxy holds the pinned egress proxy endpoint (proxy mode only).
	Proxy *EndpointResolution

	// DNSServers are the resolver addresses the sandbox may reach on port 53.
	// Empty in air_gapped mode: see NetworkPolicy.AllowsExternalDNS.
	DNSServers []net.IP

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

	// CIDRs holds the network block when the pattern was one. A block needs no
	// resolution and never changes, so it is not part of the refresh diff.
	CIDRs []*net.IPNet

	// ResolvedAt is when this specific host was resolved
	ResolvedAt time.Time

	// Error contains any error that occurred during resolution
	Error error
}

// EndpointResolution is a host:port endpoint pinned to concrete IPs.
type EndpointResolution struct {
	// Endpoint is the original host:port.
	Endpoint Endpoint

	// IPs are the addresses the host resolved to. An IP literal resolves to
	// itself without a lookup.
	IPs []net.IP

	// ResolvedAt is when the lookup happened.
	ResolvedAt time.Time

	// Error contains any error that occurred during resolution.
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

// AllIPsFiltered returns the allow-list IPs with blocked and duplicate IPs
// removed, in a stable order. This is the exact set the firewall opens, so it
// is also the set the refresher diffs against.
func (r *ResolvedPolicy) AllIPsFiltered() []net.IP {
	return dedupeIPs(FilterBlockedIPs(r.AllIPs()))
}

// AllowedCIDRs returns the allow-list network blocks that do not overlap a
// blocked range.
//
// A block that straddles one of the blocked ranges is dropped whole rather
// than trimmed: "allow 169.254.0.0/16" must not become a way to reach the
// cloud metadata endpoint, and silently narrowing an operator's block would
// hide that their policy said something they did not mean.
func (r *ResolvedPolicy) AllowedCIDRs() []*net.IPNet {
	var out []*net.IPNet
	for _, hr := range r.AllowedIPs {
		for _, cidr := range hr.CIDRs {
			if cidrOverlapsBlocked(cidr) {
				continue
			}
			out = append(out, cidr)
		}
	}
	return out
}

// RejectedCIDRs returns allow-list blocks dropped for overlapping a blocked
// range, so callers can report them instead of silently ignoring them.
func (r *ResolvedPolicy) RejectedCIDRs() []*net.IPNet {
	var out []*net.IPNet
	for _, hr := range r.AllowedIPs {
		for _, cidr := range hr.CIDRs {
			if cidrOverlapsBlocked(cidr) {
				out = append(out, cidr)
			}
		}
	}
	return out
}

// cidrOverlapsBlocked reports whether a block intersects any blocked range.
func cidrOverlapsBlocked(cidr *net.IPNet) bool {
	for _, blocked := range ParsedBlockedCIDRs {
		if blocked.Contains(cidr.IP) || cidr.Contains(blocked.IP) {
			return true
		}
	}
	return false
}

// ControlPlaneIPs returns every pinned control-plane address.
func (r *ResolvedPolicy) ControlPlaneIPs() []net.IP {
	var result []net.IP
	for _, er := range r.ControlPlane {
		result = append(result, er.IPs...)
	}
	return dedupeIPs(result)
}

// ProxyIPs returns the pinned proxy addresses, or nil when not in proxy mode.
func (r *ResolvedPolicy) ProxyIPs() []net.IP {
	if r.Proxy == nil {
		return nil
	}
	return dedupeIPs(r.Proxy.IPs)
}

// UnenforceableHostPatterns returns allow-list patterns that a packet filter
// cannot enforce.
//
// Wildcards such as *.github.com have no fixed address set to pin, so an
// iptables allow-list silently permits nothing for them. Callers must surface
// this rather than let an operator believe a wildcard is being honoured;
// proxy mode is the level that can enforce wildcards, at the CONNECT host.
func (r *ResolvedPolicy) UnenforceableHostPatterns() []string {
	var out []string
	for _, hr := range r.AllowedIPs {
		if len(hr.IPs) == 0 && len(hr.CIDRs) == 0 && hr.Error == nil && isWildcardPattern(hr.Pattern) {
			out = append(out, hr.Pattern)
		}
	}
	return out
}

// Level returns the policy level, or PolicyNone if the policy is missing.
func (r *ResolvedPolicy) Level() PolicyLevel {
	if r == nil || r.OriginalPolicy == nil {
		return PolicyNone
	}
	return r.OriginalPolicy.Level
}

// TTL returns how long this resolution is considered fresh.
func (r *ResolvedPolicy) TTL() time.Duration {
	return r.ExpiresAt.Sub(r.PinnedAt)
}

// IsExpired returns true if the resolution has expired and should be refreshed.
func (r *ResolvedPolicy) IsExpired() bool {
	return time.Now().After(r.ExpiresAt)
}

// HasErrors returns true if any resolution failed.
func (r *ResolvedPolicy) HasErrors() bool {
	return len(r.Errors()) > 0
}

// Errors returns all resolution errors, including control-plane and proxy ones.
func (r *ResolvedPolicy) Errors() []error {
	var errs []error
	for _, hr := range r.AllowedIPs {
		if hr.Error != nil {
			errs = append(errs, hr.Error)
		}
	}
	for _, er := range r.ControlPlane {
		if er.Error != nil {
			errs = append(errs, er.Error)
		}
	}
	if r.Proxy != nil && r.Proxy.Error != nil {
		errs = append(errs, r.Proxy.Error)
	}
	return errs
}

// IsIPAllowed checks if an IP address is allowed by this policy.
// Returns true if the IP matches any resolved host and is not blocked.
//
// Control-plane and proxy addresses are allowed even when they fall inside a
// blocked CIDR: a server on a private network is the normal deployment, and
// those pins come from the operator, not from the sandbox.
func (r *ResolvedPolicy) IsIPAllowed(ip net.IP) bool {
	for _, allowed := range r.ControlPlaneIPs() {
		if ip.Equal(allowed) {
			return true
		}
	}
	for _, allowed := range r.ProxyIPs() {
		if ip.Equal(allowed) {
			return true
		}
	}

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
	for _, er := range r.ControlPlane {
		if er.Endpoint.Port != port {
			continue
		}
		for _, allowed := range er.IPs {
			if ip.Equal(allowed) {
				return true
			}
		}
	}

	if r.Proxy != nil && r.Proxy.Endpoint.Port == port {
		for _, allowed := range r.Proxy.IPs {
			if ip.Equal(allowed) {
				return true
			}
		}
	}

	// Proxy mode drops direct egress: only the proxy and the control plane
	// above are reachable.
	if r.Level() == PolicyProxy || r.Level() == PolicyAirGapped {
		return false
	}

	return r.IsIPAllowed(ip) && r.IsPortAllowed(port)
}

// DiffAllowed compares this resolution's allow-list IPs against a previous one.
//
// added must be applied before removed: opening the new address first means a
// connection to a rotated record never sees a window with neither the old nor
// the new IP permitted.
func (r *ResolvedPolicy) DiffAllowed(prev *ResolvedPolicy) (added, removed []net.IP) {
	var prevIPs []net.IP
	if prev != nil {
		prevIPs = prev.AllIPsFiltered()
	}
	return DiffIPs(prevIPs, r.AllIPsFiltered())
}

// DiffIPs returns the addresses present only in next (added) and only in
// prev (removed). Both results are sorted for deterministic rule ordering.
func DiffIPs(prev, next []net.IP) (added, removed []net.IP) {
	prevSet := make(map[string]net.IP, len(prev))
	for _, ip := range prev {
		prevSet[ip.String()] = ip
	}
	nextSet := make(map[string]net.IP, len(next))
	for _, ip := range next {
		nextSet[ip.String()] = ip
	}

	for key, ip := range nextSet {
		if _, ok := prevSet[key]; !ok {
			added = append(added, ip)
		}
	}
	for key, ip := range prevSet {
		if _, ok := nextSet[key]; !ok {
			removed = append(removed, ip)
		}
	}

	sortIPs(added)
	sortIPs(removed)
	return added, removed
}

// Summary returns a summary of the resolved policy for logging.
func (r *ResolvedPolicy) Summary() map[string]interface{} {
	hostCount := len(r.AllowedIPs)
	ipCount := len(r.AllIPs())
	filteredCount := len(r.AllIPsFiltered())

	summary := map[string]interface{}{
		"level":             r.Level(),
		"host_patterns":     hostCount,
		"resolved_ips":      ipCount,
		"allowed_ips":       filteredCount,
		"blocked_ips":       ipCount - filteredCount,
		"allowed_ports":     r.AllowedPorts,
		"control_plane_ips": len(r.ControlPlaneIPs()),
		"dns_servers":       len(r.DNSServers),
		"pinned_at":         r.PinnedAt,
		"expires_at":        r.ExpiresAt,
		"has_errors":        r.HasErrors(),
	}

	if r.Proxy != nil {
		summary["proxy"] = r.Proxy.Endpoint.String()
	}

	return summary
}

// dedupeIPs removes duplicates and returns a stably ordered slice.
func dedupeIPs(ips []net.IP) []net.IP {
	seen := make(map[string]bool, len(ips))
	out := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if ip == nil {
			continue
		}
		key := ip.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ip)
	}
	sortIPs(out)
	return out
}

// sortIPs orders addresses by their byte representation so rule generation is
// reproducible across resolutions.
func sortIPs(ips []net.IP) {
	sort.Slice(ips, func(i, j int) bool {
		a, b := ips[i].To16(), ips[j].To16()
		switch {
		case a == nil:
			return false
		case b == nil:
			return true
		}
		return bytes.Compare(a, b) < 0
	})
}
