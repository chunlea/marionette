package network

import (
	"net"
)

// DefaultBlockedCIDRs contains network ranges that should always be blocked
// for security reasons, regardless of the allow list configuration.
var DefaultBlockedCIDRs = []string{
	// Cloud metadata endpoints (security critical)
	"169.254.169.254/32", // AWS, GCP, Azure metadata service

	// Link-local addresses
	"169.254.0.0/16", // IPv4 link-local

	// Loopback addresses
	"127.0.0.0/8", // IPv4 loopback

	// Private networks (prevent lateral movement)
	"10.0.0.0/8",     // Class A private
	"172.16.0.0/12",  // Class B private
	"192.168.0.0/16", // Class C private

	// IPv6 blocked ranges
	"::1/128",    // IPv6 loopback
	"fe80::/10",  // IPv6 link-local
	"fc00::/7",   // IPv6 unique local (private)
	"fd00::/8",   // IPv6 unique local (private)
	// Note: We don't block ::ffff:0:0/96 here because Go's net.IP stores
	// all IPv4 addresses as IPv4-mapped IPv6 internally. Instead, IPv4
	// addresses are checked against the IPv4 CIDRs above.
}

// ParsedBlockedCIDRs contains the parsed net.IPNet representations
// of DefaultBlockedCIDRs. Computed once at package initialization.
var ParsedBlockedCIDRs []*net.IPNet

func init() {
	ParsedBlockedCIDRs = make([]*net.IPNet, 0, len(DefaultBlockedCIDRs))
	for _, cidr := range DefaultBlockedCIDRs {
		_, network, err := net.ParseCIDR(cidr)
		if err != nil {
			// This should never happen with our hardcoded CIDRs
			panic("invalid blocked CIDR: " + cidr)
		}
		ParsedBlockedCIDRs = append(ParsedBlockedCIDRs, network)
	}
}

// IsBlockedIP checks if an IP address falls within any of the blocked CIDRs.
func IsBlockedIP(ip net.IP) bool {
	for _, network := range ParsedBlockedCIDRs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// IsBlockedIPString parses an IP string and checks if it's blocked.
// Returns true if the IP is invalid or blocked.
func IsBlockedIPString(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		// Invalid IPs are considered blocked for safety
		return true
	}
	return IsBlockedIP(ip)
}

// FilterBlockedIPs removes blocked IPs from the input slice.
// Returns a new slice containing only allowed IPs.
func FilterBlockedIPs(ips []net.IP) []net.IP {
	result := make([]net.IP, 0, len(ips))
	for _, ip := range ips {
		if !IsBlockedIP(ip) {
			result = append(result, ip)
		}
	}
	return result
}

// blockedReasonEntry maps a CIDR to its human-readable reason.
type blockedReasonEntry struct {
	cidr   string
	reason string
}

// blockedReasons is an ordered list of CIDRs and their reasons.
// More specific CIDRs should come before broader ones.
var blockedReasons = []blockedReasonEntry{
	// Most specific first
	{"169.254.169.254/32", "cloud metadata endpoint"},
	// Then broader ranges
	{"169.254.0.0/16", "link-local address"},
	{"127.0.0.0/8", "loopback address"},
	{"10.0.0.0/8", "private network (Class A)"},
	{"172.16.0.0/12", "private network (Class B)"},
	{"192.168.0.0/16", "private network (Class C)"},
	{"::1/128", "IPv6 loopback"},
	{"fe80::/10", "IPv6 link-local"},
	{"fc00::/7", "IPv6 unique local (private)"},
	{"fd00::/8", "IPv6 unique local (private)"},
}

// BlockedReason returns a human-readable reason why an IP is blocked,
// or an empty string if the IP is not blocked.
func BlockedReason(ip net.IP) string {
	if ip == nil {
		return "invalid IP address"
	}

	// Check in order (most specific CIDRs first)
	for _, entry := range blockedReasons {
		_, network, _ := net.ParseCIDR(entry.cidr)
		if network.Contains(ip) {
			return entry.reason
		}
	}

	return ""
}
