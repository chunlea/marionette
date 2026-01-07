package network

import (
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewResolvedPolicy(t *testing.T) {
	policy := &NetworkPolicy{
		Level:        PolicyAllowList,
		AllowedHosts: []string{"github.com"},
		AllowedPorts: []int{443},
	}

	resolutions := []HostResolution{
		{
			Pattern:    "github.com",
			Hosts:      []string{"github.com"},
			IPs:        []net.IP{net.ParseIP("140.82.112.4")},
			ResolvedAt: time.Now(),
		},
	}

	ttl := 5 * time.Minute
	resolved := NewResolvedPolicy(policy, resolutions, ttl)

	assert.Equal(t, policy, resolved.OriginalPolicy)
	assert.Equal(t, resolutions, resolved.AllowedIPs)
	assert.Equal(t, []int{443}, resolved.AllowedPorts)
	assert.NotNil(t, resolved.BlockedCIDRs)
	assert.False(t, resolved.IsExpired())
}

func TestResolvedPolicy_AllIPs(t *testing.T) {
	resolutions := []HostResolution{
		{
			Pattern: "a.com",
			IPs:     []net.IP{net.ParseIP("1.1.1.1")},
		},
		{
			Pattern: "b.com",
			IPs:     []net.IP{net.ParseIP("2.2.2.2"), net.ParseIP("3.3.3.3")},
		},
	}

	resolved := &ResolvedPolicy{AllowedIPs: resolutions}

	allIPs := resolved.AllIPs()
	assert.Len(t, allIPs, 3)
}

func TestResolvedPolicy_AllIPsFiltered(t *testing.T) {
	resolutions := []HostResolution{
		{
			Pattern: "mixed.com",
			IPs: []net.IP{
				net.ParseIP("8.8.8.8"),     // Public
				net.ParseIP("192.168.1.1"), // Private (blocked)
				net.ParseIP("1.1.1.1"),     // Public
				net.ParseIP("127.0.0.1"),   // Loopback (blocked)
			},
		},
	}

	resolved := &ResolvedPolicy{AllowedIPs: resolutions}

	filtered := resolved.AllIPsFiltered()
	assert.Len(t, filtered, 2)

	// Should only contain public IPs
	for _, ip := range filtered {
		assert.False(t, IsBlockedIP(ip))
	}
}

func TestResolvedPolicy_IsExpired(t *testing.T) {
	t.Run("not expired", func(t *testing.T) {
		resolved := &ResolvedPolicy{
			ExpiresAt: time.Now().Add(time.Hour),
		}
		assert.False(t, resolved.IsExpired())
	})

	t.Run("expired", func(t *testing.T) {
		resolved := &ResolvedPolicy{
			ExpiresAt: time.Now().Add(-time.Hour),
		}
		assert.True(t, resolved.IsExpired())
	})
}

func TestResolvedPolicy_HasErrors(t *testing.T) {
	t.Run("no errors", func(t *testing.T) {
		resolved := &ResolvedPolicy{
			AllowedIPs: []HostResolution{
				{Pattern: "a.com", Error: nil},
				{Pattern: "b.com", Error: nil},
			},
		}
		assert.False(t, resolved.HasErrors())
		assert.Empty(t, resolved.Errors())
	})

	t.Run("with errors", func(t *testing.T) {
		resolved := &ResolvedPolicy{
			AllowedIPs: []HostResolution{
				{Pattern: "a.com", Error: nil},
				{Pattern: "b.com", Error: assert.AnError},
			},
		}
		assert.True(t, resolved.HasErrors())
		assert.Len(t, resolved.Errors(), 1)
	})
}

func TestResolvedPolicy_IsIPAllowed(t *testing.T) {
	resolved := &ResolvedPolicy{
		AllowedIPs: []HostResolution{
			{
				Pattern: "allowed.com",
				IPs:     []net.IP{net.ParseIP("1.2.3.4"), net.ParseIP("5.6.7.8")},
			},
		},
	}

	tests := []struct {
		ip      string
		allowed bool
	}{
		{"1.2.3.4", true},      // In allowed list
		{"5.6.7.8", true},      // In allowed list
		{"9.10.11.12", false},  // Not in allowed list
		{"192.168.1.1", false}, // Blocked (private network)
		{"127.0.0.1", false},   // Blocked (loopback)
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			require.NotNil(t, ip)
			assert.Equal(t, tt.allowed, resolved.IsIPAllowed(ip))
		})
	}
}

func TestResolvedPolicy_IsPortAllowed(t *testing.T) {
	resolved := &ResolvedPolicy{
		AllowedPorts: []int{80, 443, 8080},
	}

	tests := []struct {
		port    int
		allowed bool
	}{
		{80, true},
		{443, true},
		{8080, true},
		{22, false},
		{3306, false},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.port)), func(t *testing.T) {
			assert.Equal(t, tt.allowed, resolved.IsPortAllowed(tt.port))
		})
	}
}

func TestResolvedPolicy_IsConnectionAllowed(t *testing.T) {
	resolved := &ResolvedPolicy{
		AllowedIPs: []HostResolution{
			{
				Pattern: "allowed.com",
				IPs:     []net.IP{net.ParseIP("1.2.3.4")},
			},
		},
		AllowedPorts: []int{443},
	}

	tests := []struct {
		ip      string
		port    int
		allowed bool
	}{
		{"1.2.3.4", 443, true},      // Both allowed
		{"1.2.3.4", 80, false},      // IP allowed, port not
		{"5.6.7.8", 443, false},     // IP not allowed
		{"192.168.1.1", 443, false}, // Blocked IP
	}

	for _, tt := range tests {
		name := tt.ip + ":" + string(rune(tt.port))
		t.Run(name, func(t *testing.T) {
			ip := net.ParseIP(tt.ip)
			assert.Equal(t, tt.allowed, resolved.IsConnectionAllowed(ip, tt.port))
		})
	}
}

func TestResolvedPolicy_Summary(t *testing.T) {
	policy := &NetworkPolicy{
		Level:        PolicyAllowList,
		AllowedPorts: []int{443},
	}

	resolutions := []HostResolution{
		{
			Pattern: "example.com",
			IPs: []net.IP{
				net.ParseIP("1.2.3.4"),
				net.ParseIP("192.168.1.1"), // Will be filtered
			},
		},
	}

	resolved := NewResolvedPolicy(policy, resolutions, time.Hour)
	summary := resolved.Summary()

	assert.Equal(t, PolicyAllowList, summary["level"])
	assert.Equal(t, 1, summary["host_patterns"])
	assert.Equal(t, 2, summary["resolved_ips"])
	assert.Equal(t, 1, summary["allowed_ips"]) // After filtering
	assert.Equal(t, 1, summary["blocked_ips"]) // The private IP
	assert.False(t, summary["has_errors"].(bool))
}

func TestHostResolution(t *testing.T) {
	now := time.Now()

	hr := HostResolution{
		Pattern:    "*.example.com",
		Hosts:      []string{"api.example.com", "www.example.com"},
		IPs:        []net.IP{net.ParseIP("1.2.3.4")},
		ResolvedAt: now,
		Error:      nil,
	}

	assert.Equal(t, "*.example.com", hr.Pattern)
	assert.Len(t, hr.Hosts, 2)
	assert.Len(t, hr.IPs, 1)
	assert.Nil(t, hr.Error)
}
