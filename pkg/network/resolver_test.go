package network

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewDNSResolver(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		r := NewDNSResolver()
		assert.NotNil(t, r)
		assert.Equal(t, DefaultCacheTTL, r.cacheTTL)
		assert.NotNil(t, r.cache)
		assert.NotNil(t, r.resolver)
	})

	t.Run("with custom TTL", func(t *testing.T) {
		ttl := 10 * time.Minute
		r := NewDNSResolver(WithCacheTTL(ttl))
		assert.Equal(t, ttl, r.cacheTTL)
	})

	t.Run("with mock resolver", func(t *testing.T) {
		mock := NewMockResolver()
		r := NewDNSResolver(WithResolver(mock))
		assert.Equal(t, mock, r.resolver)
	})
}

func TestDNSResolver_Resolve(t *testing.T) {
	t.Run("successful resolution", func(t *testing.T) {
		mock := NewMockResolver()
		mock.SetResult("example.com", []net.IP{
			net.ParseIP("93.184.216.34"),
		})

		r := NewDNSResolver(WithResolver(mock))

		ips, err := r.Resolve(context.Background(), "example.com")
		require.NoError(t, err)
		require.Len(t, ips, 1)
		assert.Equal(t, "93.184.216.34", ips[0].String())
	})

	t.Run("resolution error", func(t *testing.T) {
		mock := NewMockResolver()
		mock.SetError("unknown.example.com", errors.New("no such host"))

		r := NewDNSResolver(WithResolver(mock))

		ips, err := r.Resolve(context.Background(), "unknown.example.com")
		require.Error(t, err)
		assert.Nil(t, ips)
		assert.Contains(t, err.Error(), "no such host")
	})

	t.Run("caching", func(t *testing.T) {
		mock := NewMockResolver()
		mock.SetResult("cached.example.com", []net.IP{
			net.ParseIP("1.2.3.4"),
		})

		r := NewDNSResolver(WithResolver(mock))

		// First call
		ips1, err := r.Resolve(context.Background(), "cached.example.com")
		require.NoError(t, err)
		assert.Len(t, ips1, 1)

		// Second call should use cache
		ips2, err := r.Resolve(context.Background(), "cached.example.com")
		require.NoError(t, err)
		assert.Len(t, ips2, 1)

		// Only one actual DNS lookup should have been made
		calls := mock.GetCalls()
		assert.Len(t, calls, 1)
	})

	t.Run("cache expiration", func(t *testing.T) {
		mock := NewMockResolver()
		mock.SetResult("expiring.example.com", []net.IP{
			net.ParseIP("1.2.3.4"),
		})

		r := NewDNSResolver(
			WithResolver(mock),
			WithCacheTTL(1*time.Millisecond),
		)

		// First call
		_, err := r.Resolve(context.Background(), "expiring.example.com")
		require.NoError(t, err)

		// Wait for cache to expire
		time.Sleep(5 * time.Millisecond)

		// Second call should make a new lookup
		_, err = r.Resolve(context.Background(), "expiring.example.com")
		require.NoError(t, err)

		calls := mock.GetCalls()
		assert.Len(t, calls, 2)
	})

	t.Run("error caching", func(t *testing.T) {
		mock := NewMockResolver()
		mock.SetError("error.example.com", errors.New("dns error"))

		r := NewDNSResolver(WithResolver(mock))

		// First call - should fail
		_, err1 := r.Resolve(context.Background(), "error.example.com")
		require.Error(t, err1)

		// Second call - should use cached error
		_, err2 := r.Resolve(context.Background(), "error.example.com")
		require.Error(t, err2)

		// Only one lookup should have been made
		calls := mock.GetCalls()
		assert.Len(t, calls, 1)
	})
}

func TestDNSResolver_ResolvePolicy(t *testing.T) {
	t.Run("nil policy", func(t *testing.T) {
		r := NewDNSResolver()
		_, err := r.ResolvePolicy(context.Background(), nil)
		require.Error(t, err)
	})

	t.Run("none policy", func(t *testing.T) {
		r := NewDNSResolver()
		policy := &NetworkPolicy{Level: PolicyNone}

		resolved, err := r.ResolvePolicy(context.Background(), policy)
		require.NoError(t, err)
		assert.Equal(t, policy, resolved.OriginalPolicy)
		assert.Empty(t, resolved.AllowedIPs)
	})

	t.Run("allow_list with concrete hosts", func(t *testing.T) {
		mock := NewMockResolver()
		mock.SetResult("github.com", []net.IP{
			net.ParseIP("140.82.112.4"),
		})
		mock.SetResult("api.anthropic.com", []net.IP{
			net.ParseIP("104.18.1.1"),
			net.ParseIP("104.18.2.2"),
		})

		r := NewDNSResolver(WithResolver(mock))
		policy := &NetworkPolicy{
			Level: PolicyAllowList,
			AllowedHosts: []string{
				"github.com",
				"api.anthropic.com",
			},
		}

		resolved, err := r.ResolvePolicy(context.Background(), policy)
		require.NoError(t, err)
		assert.Len(t, resolved.AllowedIPs, 2)

		allIPs := resolved.AllIPs()
		assert.Len(t, allIPs, 3) // 1 + 2 IPs
	})

	t.Run("allow_list with wildcard patterns", func(t *testing.T) {
		mock := NewMockResolver()
		r := NewDNSResolver(WithResolver(mock))

		policy := &NetworkPolicy{
			Level: PolicyAllowList,
			AllowedHosts: []string{
				"*.github.com",
			},
		}

		resolved, err := r.ResolvePolicy(context.Background(), policy)
		require.NoError(t, err)
		assert.Len(t, resolved.AllowedIPs, 1)

		// Wildcard patterns should not have resolved IPs
		assert.Empty(t, resolved.AllowedIPs[0].IPs)
		assert.Nil(t, resolved.AllowedIPs[0].Error) // No error for wildcards

		// No DNS lookups should have been made for wildcards
		assert.Empty(t, mock.GetCalls())
	})

	t.Run("allow_list with resolution errors", func(t *testing.T) {
		mock := NewMockResolver()
		mock.SetError("unknown.example.com", errors.New("dns error"))

		r := NewDNSResolver(WithResolver(mock))
		policy := &NetworkPolicy{
			Level:        PolicyAllowList,
			AllowedHosts: []string{"unknown.example.com"},
		}

		resolved, err := r.ResolvePolicy(context.Background(), policy)
		require.NoError(t, err) // Policy resolution succeeds even with DNS errors
		assert.True(t, resolved.HasErrors())
		assert.Len(t, resolved.Errors(), 1)
	})
}

func TestDNSResolver_CacheOperations(t *testing.T) {
	t.Run("invalidate single host", func(t *testing.T) {
		mock := NewMockResolver()
		mock.SetResult("test.example.com", []net.IP{net.ParseIP("1.2.3.4")})

		r := NewDNSResolver(WithResolver(mock))

		// Populate cache
		_, _ = r.Resolve(context.Background(), "test.example.com")
		assert.Equal(t, 1, r.CacheSize())

		// Invalidate
		r.InvalidateCache("test.example.com")
		assert.Equal(t, 0, r.CacheSize())
	})

	t.Run("invalidate all", func(t *testing.T) {
		mock := NewMockResolver()
		mock.SetResult("a.example.com", []net.IP{net.ParseIP("1.2.3.4")})
		mock.SetResult("b.example.com", []net.IP{net.ParseIP("5.6.7.8")})

		r := NewDNSResolver(WithResolver(mock))

		// Populate cache
		_, _ = r.Resolve(context.Background(), "a.example.com")
		_, _ = r.Resolve(context.Background(), "b.example.com")
		assert.Equal(t, 2, r.CacheSize())

		// Invalidate all
		r.InvalidateAll()
		assert.Equal(t, 0, r.CacheSize())
	})

	t.Run("cache stats", func(t *testing.T) {
		mock := NewMockResolver()
		mock.SetResult("test.example.com", []net.IP{net.ParseIP("1.2.3.4")})

		r := NewDNSResolver(WithResolver(mock), WithCacheTTL(time.Hour))

		_, _ = r.Resolve(context.Background(), "test.example.com")

		stats := r.CacheStats()
		assert.Equal(t, 1, stats["size"])
		assert.Equal(t, 0, stats["expired"])
		assert.Equal(t, time.Hour.String(), stats["ttl"])
	})
}

func TestDNSResolver_Concurrency(t *testing.T) {
	mock := NewMockResolver()
	for i := 0; i < 10; i++ {
		host := "host" + string(rune('0'+i)) + ".example.com"
		mock.SetResult(host, []net.IP{net.ParseIP("1.2.3." + string(rune('0'+i)))})
	}

	r := NewDNSResolver(WithResolver(mock))

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			host := "host" + string(rune('0'+(n%10))) + ".example.com"
			_, _ = r.Resolve(context.Background(), host)
		}(i)
	}
	wg.Wait()

	// Should have resolved all 10 unique hosts
	assert.LessOrEqual(t, r.CacheSize(), 10)
}

func TestDNSResolver_OnResolve(t *testing.T) {
	mock := NewMockResolver()
	mock.SetResult("callback.example.com", []net.IP{net.ParseIP("1.2.3.4")})

	var callbackHost string
	var callbackIPs []net.IP

	r := NewDNSResolver(
		WithResolver(mock),
		WithOnResolve(func(host string, ips []net.IP, err error) {
			callbackHost = host
			callbackIPs = ips
		}),
	)

	_, _ = r.Resolve(context.Background(), "callback.example.com")

	assert.Equal(t, "callback.example.com", callbackHost)
	assert.Len(t, callbackIPs, 1)

	// Cached calls should not trigger callback
	callbackHost = ""
	_, _ = r.Resolve(context.Background(), "callback.example.com")
	assert.Empty(t, callbackHost) // No callback for cached result
}

func TestMockResolver(t *testing.T) {
	t.Run("set and get result", func(t *testing.T) {
		m := NewMockResolver()
		ips := []net.IP{net.ParseIP("1.2.3.4")}
		m.SetResult("test.com", ips)

		result, err := m.LookupIP(context.Background(), "ip", "test.com")
		require.NoError(t, err)
		assert.Equal(t, ips, result)
	})

	t.Run("set and get error", func(t *testing.T) {
		m := NewMockResolver()
		m.SetError("error.com", errors.New("test error"))

		_, err := m.LookupIP(context.Background(), "ip", "error.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "test error")
	})

	t.Run("unknown host", func(t *testing.T) {
		m := NewMockResolver()

		_, err := m.LookupIP(context.Background(), "ip", "unknown.com")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no such host")
	})

	t.Run("tracks calls", func(t *testing.T) {
		m := NewMockResolver()
		m.SetResult("a.com", []net.IP{})
		m.SetResult("b.com", []net.IP{})

		_, _ = m.LookupIP(context.Background(), "ip", "a.com")
		_, _ = m.LookupIP(context.Background(), "ip", "b.com")
		_, _ = m.LookupIP(context.Background(), "ip", "a.com")

		calls := m.GetCalls()
		assert.Equal(t, []string{"a.com", "b.com", "a.com"}, calls)
	})
}

func TestIsWildcardPattern(t *testing.T) {
	tests := []struct {
		pattern  string
		expected bool
	}{
		{"github.com", false},
		{"*.github.com", true},
		{"api.*.example.com", true},
		{"*", true},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			assert.Equal(t, tt.expected, isWildcardPattern(tt.pattern))
		})
	}
}
