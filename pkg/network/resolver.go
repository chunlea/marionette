package network

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"
)

// DefaultCacheTTL is the default time-to-live for cached DNS resolutions.
const DefaultCacheTTL = 5 * time.Minute

// DefaultRefreshInterval is how often to proactively refresh cached entries.
const DefaultRefreshInterval = 4 * time.Minute

// DNSResolver resolves hostnames to IP addresses with caching.
// It supports DNS pinning to prevent DNS rebinding attacks.
type DNSResolver struct {
	cache    map[string]*cacheEntry
	cacheTTL time.Duration
	mu       sync.RWMutex

	// resolver is the underlying DNS resolver (can be mocked for testing)
	resolver Resolver

	// onResolve is called after each resolution (for testing/logging)
	onResolve func(host string, ips []net.IP, err error)
}

// cacheEntry represents a cached DNS resolution.
type cacheEntry struct {
	IPs        []net.IP
	ResolvedAt time.Time
	ExpiresAt  time.Time
	Error      error
}

// Resolver is an interface for DNS resolution.
// This allows mocking the resolver for testing.
type Resolver interface {
	LookupIP(ctx context.Context, network, host string) ([]net.IP, error)
}

// netResolver wraps net.Resolver to implement the Resolver interface.
type netResolver struct {
	resolver *net.Resolver
}

func (r *netResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	return r.resolver.LookupIP(ctx, network, host)
}

// ResolverOption configures a DNSResolver.
type ResolverOption func(*DNSResolver)

// WithCacheTTL sets the cache TTL for DNS resolutions.
func WithCacheTTL(ttl time.Duration) ResolverOption {
	return func(r *DNSResolver) {
		r.cacheTTL = ttl
	}
}

// WithResolver sets a custom resolver (for testing).
func WithResolver(resolver Resolver) ResolverOption {
	return func(r *DNSResolver) {
		r.resolver = resolver
	}
}

// WithOnResolve sets a callback for resolution events.
func WithOnResolve(fn func(host string, ips []net.IP, err error)) ResolverOption {
	return func(r *DNSResolver) {
		r.onResolve = fn
	}
}

// NewDNSResolver creates a new DNS resolver with caching.
func NewDNSResolver(opts ...ResolverOption) *DNSResolver {
	r := &DNSResolver{
		cache:    make(map[string]*cacheEntry),
		cacheTTL: DefaultCacheTTL,
		resolver: &netResolver{resolver: net.DefaultResolver},
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// Resolve looks up IP addresses for a hostname.
// Results are cached for the configured TTL.
func (r *DNSResolver) Resolve(ctx context.Context, host string) ([]net.IP, error) {
	// Check cache first
	if entry := r.getCached(host); entry != nil {
		if entry.Error != nil {
			return nil, entry.Error
		}
		return entry.IPs, nil
	}

	// Perform DNS lookup
	ips, err := r.resolver.LookupIP(ctx, "ip", host)

	// Cache the result (including errors)
	r.setCached(host, ips, err)

	// Call the onResolve callback if set
	if r.onResolve != nil {
		r.onResolve(host, ips, err)
	}

	return ips, err
}

// ResolvePolicy resolves all allowed hosts in a network policy.
// Returns a ResolvedPolicy with pinned IP addresses.
func (r *DNSResolver) ResolvePolicy(ctx context.Context, policy *NetworkPolicy) (*ResolvedPolicy, error) {
	if policy == nil {
		return nil, fmt.Errorf("policy is nil")
	}

	// For non-allow_list policies, return a minimal resolved policy
	if policy.Level != PolicyAllowList {
		return NewResolvedPolicy(policy, nil, r.cacheTTL), nil
	}

	resolutions := make([]HostResolution, 0, len(policy.AllowedHosts))

	for _, pattern := range policy.AllowedHosts {
		hr := r.resolvePattern(ctx, pattern)
		resolutions = append(resolutions, hr)
	}

	return NewResolvedPolicy(policy, resolutions, r.cacheTTL), nil
}

// resolvePattern resolves a single host pattern to IP addresses.
func (r *DNSResolver) resolvePattern(ctx context.Context, pattern string) HostResolution {
	now := time.Now()

	// Wildcards cannot be directly resolved
	if isWildcardPattern(pattern) {
		return HostResolution{
			Pattern:    pattern,
			Hosts:      nil, // No concrete hosts for wildcards
			IPs:        nil, // Wildcards are matched at connection time
			ResolvedAt: now,
			Error:      nil, // Not an error, just deferred resolution
		}
	}

	// Resolve concrete hostname
	ips, err := r.Resolve(ctx, pattern)
	return HostResolution{
		Pattern:    pattern,
		Hosts:      []string{pattern},
		IPs:        ips,
		ResolvedAt: now,
		Error:      err,
	}
}

// getCached returns a cached entry if it exists and hasn't expired.
func (r *DNSResolver) getCached(host string) *cacheEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.cache[host]
	if !ok {
		return nil
	}

	if time.Now().After(entry.ExpiresAt) {
		return nil
	}

	return entry
}

// setCached stores a resolution result in the cache.
func (r *DNSResolver) setCached(host string, ips []net.IP, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	r.cache[host] = &cacheEntry{
		IPs:        ips,
		ResolvedAt: now,
		ExpiresAt:  now.Add(r.cacheTTL),
		Error:      err,
	}
}

// InvalidateCache removes a host from the cache.
func (r *DNSResolver) InvalidateCache(host string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.cache, host)
}

// InvalidateAll clears the entire cache.
func (r *DNSResolver) InvalidateAll() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cache = make(map[string]*cacheEntry)
}

// CacheSize returns the number of entries in the cache.
func (r *DNSResolver) CacheSize() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cache)
}

// CacheStats returns statistics about the cache.
func (r *DNSResolver) CacheStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	now := time.Now()
	expired := 0
	for _, entry := range r.cache {
		if now.After(entry.ExpiresAt) {
			expired++
		}
	}

	return map[string]interface{}{
		"size":    len(r.cache),
		"expired": expired,
		"ttl":     r.cacheTTL.String(),
	}
}

// isWildcardPattern returns true if the pattern contains wildcards.
func isWildcardPattern(pattern string) bool {
	for _, c := range pattern {
		if c == '*' {
			return true
		}
	}
	return false
}

// MockResolver is a resolver for testing that returns predefined results.
type MockResolver struct {
	mu      sync.Mutex
	Results map[string][]net.IP
	Errors  map[string]error
	Calls   []string
}

// NewMockResolver creates a new mock resolver.
func NewMockResolver() *MockResolver {
	return &MockResolver{
		Results: make(map[string][]net.IP),
		Errors:  make(map[string]error),
		Calls:   make([]string, 0),
	}
}

// LookupIP returns mocked results for DNS lookups.
func (m *MockResolver) LookupIP(ctx context.Context, network, host string) ([]net.IP, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.Calls = append(m.Calls, host)

	if err, ok := m.Errors[host]; ok {
		return nil, err
	}

	if ips, ok := m.Results[host]; ok {
		return ips, nil
	}

	return nil, fmt.Errorf("no such host: %s", host)
}

// SetResult sets the mock result for a host.
func (m *MockResolver) SetResult(host string, ips []net.IP) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Results[host] = ips
}

// SetError sets the mock error for a host.
func (m *MockResolver) SetError(host string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Errors[host] = err
}

// GetCalls returns all hosts that were looked up.
func (m *MockResolver) GetCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.Calls))
	copy(result, m.Calls)
	return result
}
