package network

import (
	"strings"
)

// HostMatcher matches hostnames against a set of patterns.
// Supports wildcard patterns like *.github.com, api.*.example.com.
type HostMatcher struct {
	patterns []*hostPattern
}

// hostPattern represents a parsed host pattern.
type hostPattern struct {
	original string   // Original pattern string
	labels   []string // Pattern split by "."
}

// NewHostMatcher creates a HostMatcher from a list of patterns.
// Returns an error if any pattern is invalid.
func NewHostMatcher(patterns []string) (*HostMatcher, error) {
	m := &HostMatcher{
		patterns: make([]*hostPattern, 0, len(patterns)),
	}

	for _, p := range patterns {
		if err := validateHostPattern(p); err != nil {
			return nil, err
		}

		m.patterns = append(m.patterns, &hostPattern{
			original: p,
			labels:   strings.Split(strings.ToLower(p), "."),
		})
	}

	return m, nil
}

// Match returns true if the host matches any of the patterns.
// Host comparison is case-insensitive.
func (m *HostMatcher) Match(host string) bool {
	if host == "" {
		return false
	}

	hostLower := strings.ToLower(host)
	hostLabels := strings.Split(hostLower, ".")

	for _, p := range m.patterns {
		if matchLabels(p.labels, hostLabels) {
			return true
		}
	}

	return false
}

// Patterns returns the list of pattern strings.
func (m *HostMatcher) Patterns() []string {
	result := make([]string, len(m.patterns))
	for i, p := range m.patterns {
		result[i] = p.original
	}
	return result
}

// Len returns the number of patterns.
func (m *HostMatcher) Len() int {
	return len(m.patterns)
}

// matchLabels checks if the host labels match the pattern labels.
// Supports:
// - Exact match: github.com matches github.com
// - Leading wildcard: *.github.com matches api.github.com, foo.github.com
// - Embedded wildcard: api.*.example.com matches api.us.example.com
func matchLabels(pattern, host []string) bool {
	// Handle leading wildcard (*.example.com)
	if len(pattern) > 0 && pattern[0] == "*" {
		// Pattern: *.example.com (2 parts after wildcard)
		// Host must have at least as many parts as pattern
		if len(host) < len(pattern) {
			return false
		}

		// For *.github.com, we need host to have at least 3 labels
		// and the last 2 labels must match
		suffixLen := len(pattern) - 1 // Number of non-wildcard labels
		hostSuffix := host[len(host)-suffixLen:]
		patternSuffix := pattern[1:]

		return labelsEqual(patternSuffix, hostSuffix)
	}

	// Patterns with embedded wildcards or exact match
	if len(pattern) != len(host) {
		return false
	}

	for i := range pattern {
		if pattern[i] == "*" {
			// Wildcard matches any single label
			continue
		}
		if pattern[i] != host[i] {
			return false
		}
	}

	return true
}

// labelsEqual checks if two label slices are equal.
func labelsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// MatchAny returns true if the host matches any pattern in the given list.
// This is a convenience function that creates a temporary HostMatcher.
func MatchAny(host string, patterns []string) bool {
	m, err := NewHostMatcher(patterns)
	if err != nil {
		return false
	}
	return m.Match(host)
}

// NormalizeHost removes the port from a host:port string and lowercases it.
func NormalizeHost(hostPort string) string {
	host := hostPort

	// Remove port if present
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		// Check if this is actually a port (not IPv6)
		possiblePort := host[idx+1:]
		isPort := true
		for _, r := range possiblePort {
			if r < '0' || r > '9' {
				isPort = false
				break
			}
		}
		if isPort {
			host = host[:idx]
		}
	}

	// Handle IPv6 with brackets
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = host[1 : len(host)-1]
	}

	return strings.ToLower(host)
}
