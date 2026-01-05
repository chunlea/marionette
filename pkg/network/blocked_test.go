package network

import (
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsedBlockedCIDRs(t *testing.T) {
	// Ensure all default CIDRs are parsed correctly at init
	assert.Equal(t, len(DefaultBlockedCIDRs), len(ParsedBlockedCIDRs))

	for i, cidr := range DefaultBlockedCIDRs {
		assert.NotNil(t, ParsedBlockedCIDRs[i], "CIDR %s should be parsed", cidr)
	}
}

func TestIsBlockedIP(t *testing.T) {
	tests := []struct {
		name    string
		ip      string
		blocked bool
		reason  string
	}{
		// Metadata endpoints (always blocked)
		{"metadata endpoint", "169.254.169.254", true, "cloud metadata endpoint"},
		{"metadata alternate", "169.254.169.253", true, "link-local address"},

		// Loopback addresses
		{"loopback", "127.0.0.1", true, "loopback address"},
		{"loopback other", "127.0.0.2", true, "loopback address"},
		{"loopback high", "127.255.255.255", true, "loopback address"},

		// Private networks
		{"private class A", "10.0.0.1", true, "private network (Class A)"},
		{"private class A high", "10.255.255.255", true, "private network (Class A)"},
		{"private class B", "172.16.0.1", true, "private network (Class B)"},
		{"private class B high", "172.31.255.255", true, "private network (Class B)"},
		{"private class C", "192.168.1.1", true, "private network (Class C)"},
		{"private class C high", "192.168.255.255", true, "private network (Class C)"},

		// Public IPs (allowed)
		{"public google", "8.8.8.8", false, ""},
		{"public cloudflare", "1.1.1.1", false, ""},
		{"public github", "140.82.112.4", false, ""},

		// Edge cases at boundaries
		{"just before class A", "9.255.255.255", false, ""},
		{"just after class A", "11.0.0.0", false, ""},
		{"just before class B", "172.15.255.255", false, ""},
		{"just after class B", "172.32.0.0", false, ""},
		{"just before class C", "192.167.255.255", false, ""},
		{"just after class C", "192.169.0.0", false, ""},

		// IPv6 addresses
		{"ipv6 loopback", "::1", true, "IPv6 loopback"},
		{"ipv6 link-local", "fe80::1", true, "IPv6 link-local"},
		{"ipv6 unique local fc", "fc00::1", true, "IPv6 unique local (private)"},
		{"ipv6 unique local fd", "fd00::1", true, "IPv6 unique local (private)"},
		{"ipv6 public", "2607:f8b0:4004:800::200e", false, ""}, // Google

		// Note: IPv4-mapped IPv6 addresses like ::ffff:192.168.1.1 are handled
		// as IPv4 addresses by Go's net.IP, so they're checked against IPv4 CIDRs.
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip, "failed to parse IP: %s", tt.ip)

			assert.Equal(t, tt.blocked, IsBlockedIP(ip), "IP %s blocked status", tt.ip)

			if tt.reason != "" {
				assert.Contains(t, BlockedReason(ip), tt.reason)
			}
		})
	}
}

func TestIsBlockedIPString(t *testing.T) {
	tests := []struct {
		ip      string
		blocked bool
	}{
		{"127.0.0.1", true},
		{"8.8.8.8", false},
		{"invalid-ip", true}, // Invalid IPs are blocked for safety
		{"", true},           // Empty string is blocked
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			assert.Equal(t, tt.blocked, IsBlockedIPString(tt.ip))
		})
	}
}

func TestFilterBlockedIPs(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "all public",
			input:    []string{"8.8.8.8", "1.1.1.1"},
			expected: []string{"8.8.8.8", "1.1.1.1"},
		},
		{
			name:     "all private",
			input:    []string{"192.168.1.1", "10.0.0.1"},
			expected: nil, // Empty result is nil slice
		},
		{
			name:     "mixed",
			input:    []string{"8.8.8.8", "192.168.1.1", "1.1.1.1", "127.0.0.1"},
			expected: []string{"8.8.8.8", "1.1.1.1"},
		},
		{
			name:     "empty input",
			input:    []string{},
			expected: nil, // Empty result is nil slice
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var input []net.IP
			for _, s := range tt.input {
				input = append(input, net.ParseIP(s))
			}

			result := FilterBlockedIPs(input)

			var resultStrings []string
			for _, ip := range result {
				resultStrings = append(resultStrings, ip.String())
			}

			assert.Equal(t, tt.expected, resultStrings)
		})
	}
}

func TestBlockedReason(t *testing.T) {
	tests := []struct {
		ip       string
		contains string
	}{
		{"169.254.169.254", "cloud metadata"},
		{"127.0.0.1", "loopback"},
		{"10.0.0.1", "private"},
		{"172.16.0.1", "private"},
		{"192.168.1.1", "private"},
		{"::1", "loopback"},
		{"fe80::1", "link-local"},
		{"8.8.8.8", ""}, // Not blocked, empty reason
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			reason := BlockedReason(ip)

			if tt.contains == "" {
				assert.Empty(t, reason)
			} else {
				assert.Contains(t, reason, tt.contains)
			}
		})
	}
}

func TestBlockedReason_NilIP(t *testing.T) {
	reason := BlockedReason(nil)
	assert.Equal(t, "invalid IP address", reason)
}
