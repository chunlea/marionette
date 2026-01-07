package network

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHostMatcher(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		wantErr  bool
	}{
		{
			name:     "empty patterns",
			patterns: []string{},
			wantErr:  false,
		},
		{
			name:     "valid patterns",
			patterns: []string{"github.com", "*.anthropic.com"},
			wantErr:  false,
		},
		{
			name:     "invalid pattern",
			patterns: []string{"foo..bar"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewHostMatcher(tt.patterns)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, len(tt.patterns), m.Len())
		})
	}
}

func TestHostMatcher_Match(t *testing.T) {
	tests := []struct {
		name     string
		patterns []string
		host     string
		expected bool
	}{
		// Exact matches
		{
			name:     "exact match",
			patterns: []string{"github.com"},
			host:     "github.com",
			expected: true,
		},
		{
			name:     "exact match case insensitive",
			patterns: []string{"GitHub.COM"},
			host:     "github.com",
			expected: true,
		},
		{
			name:     "exact match host case insensitive",
			patterns: []string{"github.com"},
			host:     "GitHub.COM",
			expected: true,
		},
		{
			name:     "no match different domain",
			patterns: []string{"github.com"},
			host:     "gitlab.com",
			expected: false,
		},
		{
			name:     "no match subdomain without wildcard",
			patterns: []string{"github.com"},
			host:     "api.github.com",
			expected: false,
		},

		// Leading wildcard (*.example.com)
		{
			name:     "wildcard matches subdomain",
			patterns: []string{"*.github.com"},
			host:     "api.github.com",
			expected: true,
		},
		{
			name:     "wildcard matches any subdomain",
			patterns: []string{"*.github.com"},
			host:     "foo.github.com",
			expected: true,
		},
		{
			name:     "wildcard matches deep subdomain",
			patterns: []string{"*.github.com"},
			host:     "foo.bar.github.com",
			expected: true,
		},
		{
			name:     "wildcard does not match base domain",
			patterns: []string{"*.github.com"},
			host:     "github.com",
			expected: false,
		},
		{
			name:     "wildcard does not match different domain",
			patterns: []string{"*.github.com"},
			host:     "api.gitlab.com",
			expected: false,
		},

		// Embedded wildcard (api.*.example.com)
		{
			name:     "embedded wildcard matches",
			patterns: []string{"api.*.example.com"},
			host:     "api.us.example.com",
			expected: true,
		},
		{
			name:     "embedded wildcard matches any label",
			patterns: []string{"api.*.example.com"},
			host:     "api.eu.example.com",
			expected: true,
		},
		{
			name:     "embedded wildcard requires exact structure",
			patterns: []string{"api.*.example.com"},
			host:     "api.us.west.example.com",
			expected: false, // Different number of labels
		},
		{
			name:     "embedded wildcard first label must match",
			patterns: []string{"api.*.example.com"},
			host:     "web.us.example.com",
			expected: false,
		},

		// Multiple patterns
		{
			name:     "multiple patterns first match",
			patterns: []string{"github.com", "*.anthropic.com"},
			host:     "github.com",
			expected: true,
		},
		{
			name:     "multiple patterns second match",
			patterns: []string{"github.com", "*.anthropic.com"},
			host:     "api.anthropic.com",
			expected: true,
		},
		{
			name:     "multiple patterns no match",
			patterns: []string{"github.com", "*.anthropic.com"},
			host:     "google.com",
			expected: false,
		},

		// Edge cases
		{
			name:     "empty host",
			patterns: []string{"github.com"},
			host:     "",
			expected: false,
		},
		{
			name:     "empty patterns",
			patterns: []string{},
			host:     "github.com",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, err := NewHostMatcher(tt.patterns)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, m.Match(tt.host))
		})
	}
}

func TestHostMatcher_Patterns(t *testing.T) {
	patterns := []string{"github.com", "*.anthropic.com", "api.*.example.com"}
	m, err := NewHostMatcher(patterns)
	require.NoError(t, err)

	result := m.Patterns()
	assert.Equal(t, patterns, result)
}

func TestMatchAny(t *testing.T) {
	patterns := []string{"github.com", "*.anthropic.com"}

	assert.True(t, MatchAny("github.com", patterns))
	assert.True(t, MatchAny("api.anthropic.com", patterns))
	assert.False(t, MatchAny("google.com", patterns))

	// Invalid patterns return false
	assert.False(t, MatchAny("github.com", []string{"foo..bar"}))
}

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple host",
			input:    "github.com",
			expected: "github.com",
		},
		{
			name:     "host with port",
			input:    "github.com:443",
			expected: "github.com",
		},
		{
			name:     "host with non-standard port",
			input:    "localhost:8080",
			expected: "localhost",
		},
		{
			name:     "uppercase host",
			input:    "GitHub.COM",
			expected: "github.com",
		},
		{
			name:     "uppercase host with port",
			input:    "GitHub.COM:443",
			expected: "github.com",
		},
		{
			name:     "IPv6 with brackets",
			input:    "[::1]",
			expected: "::1",
		},
		{
			name:     "IPv6 with brackets and port",
			input:    "[::1]:8080",
			expected: "::1",
		},
		{
			name:     "IPv4 address",
			input:    "192.168.1.1",
			expected: "192.168.1.1",
		},
		{
			name:     "IPv4 address with port",
			input:    "192.168.1.1:8080",
			expected: "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := NormalizeHost(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchLabels(t *testing.T) {
	tests := []struct {
		name     string
		pattern  []string
		host     []string
		expected bool
	}{
		{
			name:     "exact match",
			pattern:  []string{"github", "com"},
			host:     []string{"github", "com"},
			expected: true,
		},
		{
			name:     "leading wildcard",
			pattern:  []string{"*", "github", "com"},
			host:     []string{"api", "github", "com"},
			expected: true,
		},
		{
			name:     "leading wildcard deep",
			pattern:  []string{"*", "github", "com"},
			host:     []string{"foo", "bar", "github", "com"},
			expected: true,
		},
		{
			name:     "embedded wildcard",
			pattern:  []string{"api", "*", "com"},
			host:     []string{"api", "example", "com"},
			expected: true,
		},
		{
			name:     "length mismatch no wildcard",
			pattern:  []string{"github", "com"},
			host:     []string{"api", "github", "com"},
			expected: false,
		},
		{
			name:     "label mismatch",
			pattern:  []string{"github", "com"},
			host:     []string{"gitlab", "com"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchLabels(tt.pattern, tt.host)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLabelsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        []string
		b        []string
		expected bool
	}{
		{
			name:     "equal slices",
			a:        []string{"github", "com"},
			b:        []string{"github", "com"},
			expected: true,
		},
		{
			name:     "different lengths",
			a:        []string{"github", "com"},
			b:        []string{"api", "github", "com"},
			expected: false,
		},
		{
			name:     "same length different content",
			a:        []string{"github", "com"},
			b:        []string{"gitlab", "com"},
			expected: false,
		},
		{
			name:     "empty slices",
			a:        []string{},
			b:        []string{},
			expected: true,
		},
		{
			name:     "one empty one not",
			a:        []string{},
			b:        []string{"com"},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := labelsEqual(tt.a, tt.b)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func BenchmarkHostMatcher_Match(b *testing.B) {
	patterns := []string{
		"github.com",
		"*.anthropic.com",
		"api.*.example.com",
		"*.google.com",
		"*.amazonaws.com",
	}

	m, _ := NewHostMatcher(patterns)
	hosts := []string{
		"github.com",
		"api.anthropic.com",
		"api.us.example.com",
		"search.google.com",
		"s3.amazonaws.com",
		"unknown.example.org",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, h := range hosts {
			m.Match(h)
		}
	}
}
