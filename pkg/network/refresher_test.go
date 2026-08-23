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

// recordingApplier captures the order and content of every enforcement call.
type recordingApplier struct {
	mu sync.Mutex

	calls       []string
	allowed     [][]net.IP
	denied      [][]net.IP
	reinstalled []*ResolvedPolicy

	installed    bool
	installedErr error
	allowErr     error
	denyErr      error
	reinstallErr error
}

func newRecordingApplier() *recordingApplier {
	return &recordingApplier{installed: true}
}

func (a *recordingApplier) Allow(_ context.Context, ips []net.IP) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "allow")
	a.allowed = append(a.allowed, ips)
	return a.allowErr
}

func (a *recordingApplier) Deny(_ context.Context, ips []net.IP) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "deny")
	a.denied = append(a.denied, ips)
	return a.denyErr
}

func (a *recordingApplier) Reinstall(_ context.Context, p *ResolvedPolicy) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "reinstall")
	a.reinstalled = append(a.reinstalled, p)
	return a.reinstallErr
}

func (a *recordingApplier) Installed(_ context.Context) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls = append(a.calls, "installed")
	return a.installed, a.installedErr
}

func (a *recordingApplier) snapshot() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.calls...)
}

// ipStrings renders addresses for readable assertions.
func ipStrings(ips []net.IP) []string {
	if len(ips) == 0 {
		return nil
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return out
}

// refreshFixture wires a mock resolver to a policy and an applier.
type refreshFixture struct {
	resolver *DNSResolver
	mock     *MockResolver
	applier  *recordingApplier
	policy   *NetworkPolicy
}

func newRefreshFixture(t *testing.T, hosts []string, initial map[string][]string) *refreshFixture {
	t.Helper()

	mock := NewMockResolver()
	for host, ips := range initial {
		mock.SetResult(host, parseIPsForTest(t, ips))
	}

	policy, err := ParsePolicy("allow_list", hosts)
	require.NoError(t, err)

	return &refreshFixture{
		resolver: NewDNSResolver(WithResolver(mock), WithCacheTTL(time.Minute)),
		mock:     mock,
		applier:  newRecordingApplier(),
		policy:   policy,
	}
}

func (f *refreshFixture) resolve(t *testing.T) *ResolvedPolicy {
	t.Helper()
	resolved, err := f.resolver.ResolvePolicyFresh(context.Background(), f.policy)
	require.NoError(t, err)
	return resolved
}

func (f *refreshFixture) newRefresher(t *testing.T, initial *ResolvedPolicy, opts ...RefresherOption) *Refresher {
	t.Helper()
	r, err := NewRefresher(f.resolver, f.applier, f.policy, initial, opts...)
	require.NoError(t, err)
	return r
}

func parseIPsForTest(t *testing.T, raw []string) []net.IP {
	t.Helper()
	out := make([]net.IP, 0, len(raw))
	for _, s := range raw {
		ip := net.ParseIP(s)
		require.NotNil(t, ip, "bad test IP %q", s)
		out = append(out, ip)
	}
	return out
}

func TestNewRefresher_Validation(t *testing.T) {
	resolver := NewDNSResolver()
	applier := newRecordingApplier()
	policy, err := ParsePolicy("allow_list", []string{"github.com"})
	require.NoError(t, err)

	_, err = NewRefresher(nil, applier, policy, nil)
	assert.ErrorContains(t, err, "resolver is required")

	_, err = NewRefresher(resolver, nil, policy, nil)
	assert.ErrorContains(t, err, "applier is required")

	_, err = NewRefresher(resolver, applier, nil, nil)
	assert.ErrorContains(t, err, "policy is required")
}

func TestRefresher_GrowAddsBeforeRemoving(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1"},
	})
	initial := f.resolve(t)
	r := f.newRefresher(t, initial)

	// The record rotates: one address is added and the old one is withdrawn.
	f.mock.SetResult("cdn.example.com", parseIPsForTest(t, []string{"2.2.2.2"}))

	res := r.RefreshOnce(context.Background())
	require.NoError(t, res.Err)

	assert.Equal(t, []string{"2.2.2.2"}, ipStrings(res.Added))
	assert.Equal(t, []string{"1.1.1.1"}, ipStrings(res.Removed))
	assert.True(t, res.Changed())

	// Ordering is the whole point: the new address must be open before the old
	// one closes, so a rotating record never has a window with neither.
	assert.Equal(t, []string{"installed", "allow", "deny"}, f.applier.snapshot())
}

func TestRefresher_GrowOnly(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1"},
	})
	r := f.newRefresher(t, f.resolve(t))

	f.mock.SetResult("cdn.example.com", parseIPsForTest(t, []string{"1.1.1.1", "3.3.3.3"}))

	res := r.RefreshOnce(context.Background())
	require.NoError(t, res.Err)
	assert.Equal(t, []string{"3.3.3.3"}, ipStrings(res.Added))
	assert.Empty(t, res.Removed)
	assert.Equal(t, []string{"installed", "allow"}, f.applier.snapshot())
}

func TestRefresher_ShrinkOnly(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1", "3.3.3.3"},
	})
	r := f.newRefresher(t, f.resolve(t))

	f.mock.SetResult("cdn.example.com", parseIPsForTest(t, []string{"1.1.1.1"}))

	res := r.RefreshOnce(context.Background())
	require.NoError(t, res.Err)
	assert.Empty(t, res.Added)
	assert.Equal(t, []string{"3.3.3.3"}, ipStrings(res.Removed))
	assert.Equal(t, []string{"installed", "deny"}, f.applier.snapshot())
}

func TestRefresher_NoChangeTouchesNothing(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1"},
	})
	r := f.newRefresher(t, f.resolve(t))

	res := r.RefreshOnce(context.Background())
	require.NoError(t, res.Err)
	assert.False(t, res.Changed())
	assert.Equal(t, []string{"installed"}, f.applier.snapshot())
}

func TestRefresher_ReinstallsWhenRulesAreGone(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1"},
	})
	initial := f.resolve(t)
	r := f.newRefresher(t, initial)

	// A container restart replaces the network namespace and takes the rules
	// with it. Diffing would compute "no change" and leave egress wide open.
	f.applier.installed = false
	f.mock.SetResult("cdn.example.com", parseIPsForTest(t, []string{"9.9.9.9"}))

	res := r.RefreshOnce(context.Background())
	require.NoError(t, res.Err)
	assert.True(t, res.Reinstalled)
	assert.Equal(t, []string{"installed", "reinstall"}, f.applier.snapshot())

	require.Len(t, f.applier.reinstalled, 1)
	assert.Equal(t, []string{"9.9.9.9"}, ipStrings(f.applier.reinstalled[0].AllIPsFiltered()))
	assert.Equal(t, []string{"9.9.9.9"}, ipStrings(r.Current().AllIPsFiltered()))
}

func TestRefresher_CarriesForwardResolutionFailures(t *testing.T) {
	f := newRefreshFixture(t, []string{"a.example.com", "b.example.com"}, map[string][]string{
		"a.example.com": {"1.1.1.1"},
		"b.example.com": {"2.2.2.2"},
	})
	r := f.newRefresher(t, f.resolve(t))

	// A transient SERVFAIL must not revoke a live allow-list entry.
	f.mock.SetError("a.example.com", errors.New("SERVFAIL"))

	res := r.RefreshOnce(context.Background())
	require.NoError(t, res.Err)
	assert.False(t, res.Changed(), "a transient DNS failure must not change rules")
	assert.Equal(t, []string{"installed"}, f.applier.snapshot())
	assert.ElementsMatch(t, []string{"1.1.1.1", "2.2.2.2"}, ipStrings(r.Current().AllIPsFiltered()))
}

func TestRefresher_AllowFailureLeavesOldRulesIntact(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1"},
	})
	initial := f.resolve(t)
	r := f.newRefresher(t, initial)

	f.applier.allowErr = errors.New("iptables busy")
	f.mock.SetResult("cdn.example.com", parseIPsForTest(t, []string{"2.2.2.2"}))

	res := r.RefreshOnce(context.Background())
	require.Error(t, res.Err)
	assert.Contains(t, res.Err.Error(), "allowing 1 new address")

	// Nothing was denied, and the believed state is unchanged so the next
	// cycle retries the whole diff.
	assert.Equal(t, []string{"installed", "allow"}, f.applier.snapshot())
	assert.Same(t, initial, r.Current())
}

func TestRefresher_DenyFailureRetriesNextCycle(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1"},
	})
	initial := f.resolve(t)
	r := f.newRefresher(t, initial)

	f.applier.denyErr = errors.New("iptables busy")
	f.mock.SetResult("cdn.example.com", parseIPsForTest(t, []string{"2.2.2.2"}))

	res := r.RefreshOnce(context.Background())
	require.Error(t, res.Err)
	assert.Contains(t, res.Err.Error(), "denying 1 stale address")
	assert.Same(t, initial, r.Current(), "believed state must not advance past a failed removal")

	// Second cycle: the removal is retried, and re-allowing the already-open
	// address is why Allow has to be idempotent.
	f.applier.denyErr = nil
	res = r.RefreshOnce(context.Background())
	require.NoError(t, res.Err)
	assert.Equal(t, []string{"2.2.2.2"}, ipStrings(res.Added))
	assert.Equal(t, []string{"1.1.1.1"}, ipStrings(res.Removed))
	assert.Equal(t, []string{"2.2.2.2"}, ipStrings(r.Current().AllIPsFiltered()))
}

func TestRefresher_InstalledCheckFailureIsFailSafe(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1"},
	})
	initial := f.resolve(t)
	r := f.newRefresher(t, initial)

	f.applier.installedErr = errors.New("nsenter: no such process")

	res := r.RefreshOnce(context.Background())
	require.Error(t, res.Err)
	assert.Contains(t, res.Err.Error(), "checking installed rules")
	assert.Equal(t, []string{"installed"}, f.applier.snapshot())
	assert.Same(t, initial, r.Current())
}

func TestRefresher_BlockedIPsNeverEnterTheAllowSet(t *testing.T) {
	f := newRefreshFixture(t, []string{"evil.example.com"}, map[string][]string{
		// A rebinding answer pointing at the cloud metadata endpoint and at
		// loopback. Neither may ever reach the firewall.
		"evil.example.com": {"169.254.169.254", "127.0.0.1", "8.8.8.8"},
	})
	r := f.newRefresher(t, nil)

	res := r.RefreshOnce(context.Background())
	require.NoError(t, res.Err)
	assert.Equal(t, []string{"8.8.8.8"}, ipStrings(res.Added))
	assert.Equal(t, []string{"8.8.8.8"}, ipStrings(r.Current().AllIPsFiltered()))
}

func TestRefresher_IntervalClamping(t *testing.T) {
	f := newRefreshFixture(t, []string{"a.example.com"}, map[string][]string{
		"a.example.com": {"1.1.1.1"},
	})

	tests := []struct {
		name string
		opt  RefresherOption
		want time.Duration
	}{
		{"below floor", WithRefreshInterval(time.Second), MinRefreshInterval},
		{"above ceiling", WithRefreshInterval(time.Hour), MaxRefreshInterval},
		{"inside range", WithRefreshInterval(90 * time.Second), 90 * time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := f.newRefresher(t, nil, tt.opt)
			assert.Equal(t, tt.want, r.Interval())
		})
	}
}

func TestRefresher_IntervalFollowsPolicyTTL(t *testing.T) {
	mock := NewMockResolver()
	mock.SetResult("a.example.com", parseIPsForTest(t, []string{"1.1.1.1"}))

	policy, err := ParsePolicy("allow_list", []string{"a.example.com"})
	require.NoError(t, err)

	// A short cache TTL must pull the refresh cadence in with it.
	resolver := NewDNSResolver(WithResolver(mock), WithCacheTTL(45*time.Second))
	resolved, err := resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)

	r, err := NewRefresher(resolver, newRecordingApplier(), policy, resolved)
	require.NoError(t, err)
	assert.Equal(t, 45*time.Second, r.Interval())

	// A long TTL is capped by the default cadence, not by the TTL.
	resolver = NewDNSResolver(WithResolver(mock), WithCacheTTL(time.Hour))
	resolved, err = resolver.ResolvePolicy(context.Background(), policy)
	require.NoError(t, err)

	r, err = NewRefresher(resolver, newRecordingApplier(), policy, resolved)
	require.NoError(t, err)
	assert.Equal(t, DefaultRefreshInterval, r.Interval())
}

func TestRefresher_JitterStaysInsideBounds(t *testing.T) {
	f := newRefreshFixture(t, []string{"a.example.com"}, map[string][]string{
		"a.example.com": {"1.1.1.1"},
	})
	r := f.newRefresher(t, nil, WithRefreshInterval(time.Minute), WithRefreshJitter(0.2))

	sawDifferent := false
	first := r.nextInterval()
	for i := 0; i < 200; i++ {
		d := r.nextInterval()
		assert.GreaterOrEqual(t, d, 48*time.Second)
		assert.LessOrEqual(t, d, 72*time.Second)
		if d != first {
			sawDifferent = true
		}
	}
	assert.True(t, sawDifferent, "jitter must actually vary the interval")

	// Jitter is opt-out for deterministic deployments.
	r = f.newRefresher(t, nil, WithRefreshInterval(time.Minute), WithRefreshJitter(0))
	assert.Equal(t, time.Minute, r.nextInterval())
}

func TestRefresher_StartStop(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1"},
	})

	cycles := make(chan RefreshResult, 8)
	r := f.newRefresher(t, f.resolve(t),
		WithRefreshInterval(time.Millisecond), // clamped to the floor below
		WithRefreshJitter(0),
		WithRefreshResultHook(func(res RefreshResult) {
			select {
			case cycles <- res:
			default:
			}
		}),
	)
	// The floor would make this test take 30s, so drive the loop directly and
	// only assert that Start/Stop is clean.
	assert.Equal(t, MinRefreshInterval, r.Interval())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	r.Start(ctx)
	r.Start(ctx) // idempotent
	r.Stop()
	r.Stop() // idempotent
}

func TestRefresher_StopWithoutStart(t *testing.T) {
	f := newRefreshFixture(t, []string{"a.example.com"}, map[string][]string{
		"a.example.com": {"1.1.1.1"},
	})
	r := f.newRefresher(t, nil)
	r.Stop()
	r.Stop()
}

func TestRefresher_LoopRunsAndStops(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1"},
	})

	done := make(chan struct{})
	var once sync.Once
	r := f.newRefresher(t, f.resolve(t),
		WithRefreshJitter(0),
		WithRefreshResultHook(func(RefreshResult) { once.Do(func() { close(done) }) }),
	)
	// Bypass the public clamp: the loop itself must fire, and waiting 30s for
	// the floor would make the suite unusable.
	r.interval = 5 * time.Millisecond

	r.Start(context.Background())
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh loop never fired")
	}
	r.Stop()
}

func TestRefresher_LoopStopsWithContext(t *testing.T) {
	f := newRefreshFixture(t, []string{"cdn.example.com"}, map[string][]string{
		"cdn.example.com": {"1.1.1.1"},
	})
	r := f.newRefresher(t, f.resolve(t), WithRefreshJitter(0))
	r.interval = 5 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	r.Start(ctx)
	cancel()

	select {
	case <-r.done:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh loop ignored context cancellation")
	}
}
