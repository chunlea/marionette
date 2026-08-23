package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParsePolicy(t *testing.T) {
	tests := []struct {
		name         string
		level        string
		allowedHosts []string
		wantErr      bool
		errContains  string
	}{
		{
			name:         "none policy",
			level:        "none",
			allowedHosts: nil,
			wantErr:      false,
		},
		{
			name:         "allow_list with hosts",
			level:        "allow_list",
			allowedHosts: []string{"github.com", "*.anthropic.com"},
			wantErr:      false,
		},
		{
			name:         "proxy policy without a proxy is rejected",
			level:        "proxy",
			allowedHosts: nil,
			wantErr:      true,
			errContains:  "proxy policy requires a proxy configuration",
		},
		{
			name:         "air_gapped policy",
			level:        "air_gapped",
			allowedHosts: nil,
			wantErr:      false,
		},
		{
			name:         "allow_list without hosts",
			level:        "allow_list",
			allowedHosts: nil,
			wantErr:      true,
			errContains:  "requires at least one allowed host",
		},
		{
			name:         "unknown policy level",
			level:        "unknown",
			allowedHosts: nil,
			wantErr:      true,
			errContains:  "unknown policy level",
		},
		{
			name:         "empty policy level",
			level:        "",
			allowedHosts: nil,
			wantErr:      true,
			errContains:  "policy level is required",
		},
		{
			name:         "invalid host pattern",
			level:        "allow_list",
			allowedHosts: []string{"invalid..host"},
			wantErr:      true,
			errContains:  "invalid host pattern",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy, err := ParsePolicy(tt.level, tt.allowedHosts)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, PolicyLevel(tt.level), policy.Level)
			assert.Equal(t, tt.allowedHosts, policy.AllowedHosts)
			assert.True(t, policy.BlockMetadata, "BlockMetadata should always be true")
		})
	}
}

func TestNetworkPolicy_Validate(t *testing.T) {
	tests := []struct {
		name        string
		policy      NetworkPolicy
		wantErr     bool
		errContains string
	}{
		{
			name: "valid none policy",
			policy: NetworkPolicy{
				Level: PolicyNone,
			},
			wantErr: false,
		},
		{
			name: "valid allow_list",
			policy: NetworkPolicy{
				Level:        PolicyAllowList,
				AllowedHosts: []string{"*.github.com"},
			},
			wantErr: false,
		},
		{
			name: "allow_list empty hosts",
			policy: NetworkPolicy{
				Level:        PolicyAllowList,
				AllowedHosts: []string{},
			},
			wantErr:     true,
			errContains: "requires at least one allowed host",
		},
		{
			name: "invalid host pattern - empty label",
			policy: NetworkPolicy{
				Level:        PolicyAllowList,
				AllowedHosts: []string{"foo..bar"},
			},
			wantErr:     true,
			errContains: "empty label",
		},
		{
			name: "invalid host pattern - wildcard only",
			policy: NetworkPolicy{
				Level:        PolicyAllowList,
				AllowedHosts: []string{"*"},
			},
			wantErr:     true,
			errContains: "wildcard-only pattern not allowed",
		},
		{
			name: "invalid host pattern - partial wildcard",
			policy: NetworkPolicy{
				Level:        PolicyAllowList,
				AllowedHosts: []string{"foo*.bar.com"},
			},
			wantErr:     true,
			errContains: "wildcard must be a complete label",
		},
		{
			name: "invalid host pattern - hyphen at start",
			policy: NetworkPolicy{
				Level:        PolicyAllowList,
				AllowedHosts: []string{"-foo.bar.com"},
			},
			wantErr:     true,
			errContains: "cannot start or end with hyphen",
		},
		{
			name: "invalid level",
			policy: NetworkPolicy{
				Level: PolicyLevel("invalid"),
			},
			wantErr:     true,
			errContains: "unknown policy level",
		},
		{
			name: "empty level",
			policy: NetworkPolicy{
				Level: PolicyLevel(""),
			},
			wantErr:     true,
			errContains: "policy level is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.policy.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestNetworkPolicy_IsRestricted(t *testing.T) {
	tests := []struct {
		level    PolicyLevel
		expected bool
	}{
		{PolicyNone, false},
		{PolicyAllowList, true},
		{PolicyProxy, true},
		{PolicyAirGapped, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			p := &NetworkPolicy{Level: tt.level}
			assert.Equal(t, tt.expected, p.IsRestricted())
		})
	}
}

func TestNetworkPolicy_IsAirGapped(t *testing.T) {
	tests := []struct {
		level    PolicyLevel
		expected bool
	}{
		{PolicyNone, false},
		{PolicyAllowList, false},
		{PolicyProxy, false},
		{PolicyAirGapped, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			p := &NetworkPolicy{Level: tt.level}
			assert.Equal(t, tt.expected, p.IsAirGapped())
		})
	}
}

func TestNetworkPolicy_RequiresDNSPinning(t *testing.T) {
	tests := []struct {
		level    PolicyLevel
		expected bool
	}{
		{PolicyNone, false},
		{PolicyAllowList, true},
		{PolicyProxy, true},
		{PolicyAirGapped, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			p := &NetworkPolicy{Level: tt.level}
			assert.Equal(t, tt.expected, p.RequiresDNSPinning())
		})
	}
}

func TestNetworkPolicy_EffectivePorts(t *testing.T) {
	tests := []struct {
		name     string
		ports    []int
		expected []int
	}{
		{
			name:     "nil ports uses defaults",
			ports:    nil,
			expected: DefaultAllowedPorts,
		},
		{
			name:     "empty ports uses defaults",
			ports:    []int{},
			expected: DefaultAllowedPorts,
		},
		{
			name:     "custom ports",
			ports:    []int{8080, 8443},
			expected: []int{8080, 8443},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := &NetworkPolicy{AllowedPorts: tt.ports}
			assert.Equal(t, tt.expected, p.EffectivePorts())
		})
	}
}

func TestValidateHostPattern(t *testing.T) {
	validPatterns := []string{
		"github.com",
		"api.github.com",
		"*.github.com",
		"api.*.example.com",
		"a.b.c.d.example.com",
		"foo-bar.example.com",
		"123.example.com",
		"A.B.COM", // uppercase is valid
	}

	for _, p := range validPatterns {
		t.Run("valid: "+p, func(t *testing.T) {
			err := validateHostPattern(p)
			assert.NoError(t, err)
		})
	}

	invalidPatterns := []struct {
		pattern     string
		errContains string
	}{
		{"", "empty pattern"},
		{"*", "wildcard-only pattern not allowed"},
		{"foo..bar", "empty label"},
		{"foo*.com", "wildcard must be a complete label"},
		{"*foo.com", "wildcard must be a complete label"},
		{"-foo.com", "cannot start or end with hyphen"},
		{"foo-.com", "cannot start or end with hyphen"},
		{"foo@bar.com", "invalid character"},
		// A slash now means "CIDR", so this fails as a malformed one.
		{"foo/bar.com", "not a valid CIDR"},
		{"10.0.0.0/64", "not a valid CIDR"},
		{"github.com:443", "a port is not allowed here"},
		// Label exceeds 63 characters (DNS spec limit)
		{"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmn.com", "exceeds 63 characters"},
	}

	for _, tc := range invalidPatterns {
		t.Run("invalid: "+tc.pattern, func(t *testing.T) {
			err := validateHostPattern(tc.pattern)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errContains)
		})
	}
}

func TestIsValidHostChar(t *testing.T) {
	validChars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-.*"
	for _, c := range validChars {
		assert.True(t, isValidHostChar(c), "expected %q to be valid", c)
	}

	invalidChars := "@#$%^&()_=+[]{}|;:'\",<>/?\\ \t\n"
	for _, c := range invalidChars {
		assert.False(t, isValidHostChar(c), "expected %q to be invalid", c)
	}
}
