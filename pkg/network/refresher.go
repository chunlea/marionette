package network

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Refresh cadence bounds.
//
// Go's standard resolver does not expose record TTLs (net.Resolver.LookupIP
// throws them away), so Marionette cannot follow a record's own TTL. Instead
// the pin lifetime is the policy TTL, clamped here. MinRefreshInterval is the
// short-TTL floor: a hostile record with a one-second TTL cannot make a runner
// hammer DNS, and MaxRefreshInterval bounds how long a stale pin can survive
// even if the policy asks for a very long TTL.
const (
	MinRefreshInterval = 30 * time.Second
	MaxRefreshInterval = 15 * time.Minute

	// DefaultRefreshJitter spreads refreshes across runners so a hundred
	// sandboxes do not re-resolve the same names in the same millisecond.
	DefaultRefreshJitter = 0.2
)

// RuleApplier is the enforcement point a Refresher drives. Implementations
// live next to the thing they configure: iptables inside a container network
// namespace for Docker, and so on.
//
// Allow must be idempotent. The refresher re-issues an Allow for addresses it
// believes are already open whenever a previous Deny failed.
type RuleApplier interface {
	// Allow opens egress to the given addresses. Always called before Deny.
	Allow(ctx context.Context, ips []net.IP) error

	// Deny closes egress to the given addresses.
	Deny(ctx context.Context, ips []net.IP) error

	// Reinstall rewrites the complete rule set. Used when the enforcement
	// point lost the rules, e.g. after a container restart replaced the
	// network namespace.
	Reinstall(ctx context.Context, resolved *ResolvedPolicy) error

	// Installed reports whether the enforcement point still holds our rules.
	Installed(ctx context.Context) (bool, error)
}

// RefreshResult describes one refresh cycle.
type RefreshResult struct {
	At          time.Time
	Added       []net.IP
	Removed     []net.IP
	Reinstalled bool
	Err         error
}

// Changed reports whether the cycle altered any rule.
func (r RefreshResult) Changed() bool {
	return r.Reinstalled || len(r.Added) > 0 || len(r.Removed) > 0
}

// Refresher keeps a runner's pinned allow-list in step with DNS.
//
// It owns exactly one runner's rules and stops when that runner goes away.
type Refresher struct {
	resolver *DNSResolver
	applier  RuleApplier
	policy   *NetworkPolicy
	interval time.Duration
	jitter   float64
	logger   *zap.Logger

	mu      sync.Mutex
	current *ResolvedPolicy

	startOnce sync.Once
	stopOnce  sync.Once
	cancel    context.CancelFunc
	done      chan struct{}

	// onResult is a test hook fired after every cycle.
	onResult func(RefreshResult)
}

// RefresherOption configures a Refresher.
type RefresherOption func(*Refresher)

// WithRefreshInterval overrides the refresh cadence. The value is clamped to
// [MinRefreshInterval, MaxRefreshInterval].
func WithRefreshInterval(d time.Duration) RefresherOption {
	return func(r *Refresher) {
		r.interval = d
	}
}

// WithRefreshJitter sets the fractional jitter applied to the interval.
func WithRefreshJitter(f float64) RefresherOption {
	return func(r *Refresher) {
		r.jitter = f
	}
}

// WithRefreshLogger sets the logger.
func WithRefreshLogger(l *zap.Logger) RefresherOption {
	return func(r *Refresher) {
		if l != nil {
			r.logger = l
		}
	}
}

// WithRefreshResultHook registers a callback fired after every cycle.
func WithRefreshResultHook(fn func(RefreshResult)) RefresherOption {
	return func(r *Refresher) {
		r.onResult = fn
	}
}

// NewRefresher creates a refresher for one runner's policy.
//
// initial is the resolution that is already installed at the enforcement
// point; the first cycle diffs against it.
func NewRefresher(resolver *DNSResolver, applier RuleApplier, policy *NetworkPolicy, initial *ResolvedPolicy, opts ...RefresherOption) (*Refresher, error) {
	if resolver == nil {
		return nil, fmt.Errorf("resolver is required")
	}
	if applier == nil {
		return nil, fmt.Errorf("applier is required")
	}
	if policy == nil {
		return nil, fmt.Errorf("policy is required")
	}

	r := &Refresher{
		resolver: resolver,
		applier:  applier,
		policy:   policy,
		jitter:   DefaultRefreshJitter,
		logger:   zap.NewNop(),
		current:  initial,
		done:     make(chan struct{}),
	}

	for _, opt := range opts {
		opt(r)
	}

	if r.interval <= 0 {
		r.interval = defaultIntervalFor(initial)
	}
	r.interval = clampInterval(r.interval)

	return r, nil
}

// Interval returns the clamped base refresh interval.
func (r *Refresher) Interval() time.Duration {
	return r.interval
}

// Current returns the resolution the enforcement point is believed to hold.
func (r *Refresher) Current() *ResolvedPolicy {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.current
}

// Start launches the refresh loop. It returns immediately; the loop stops when
// ctx is cancelled or Stop is called.
func (r *Refresher) Start(ctx context.Context) {
	r.startOnce.Do(func() {
		loopCtx, cancel := context.WithCancel(ctx)
		r.cancel = cancel
		go r.loop(loopCtx)
	})
}

// Stop halts the loop and waits for the current cycle to finish.
func (r *Refresher) Stop() {
	r.stopOnce.Do(func() {
		if r.cancel != nil {
			r.cancel()
			<-r.done
			return
		}
		// Never started: unblock anyone waiting on done.
		close(r.done)
	})
}

func (r *Refresher) loop(ctx context.Context) {
	defer close(r.done)

	timer := time.NewTimer(r.nextInterval())
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}

		res := r.RefreshOnce(ctx)
		switch {
		case res.Err != nil:
			// A failed refresh leaves the previous pins in place. Losing DNS
			// must not tear down a working session's egress.
			r.logger.Warn("network policy refresh failed",
				zap.Error(res.Err),
				zap.String("level", string(r.policy.Level)),
			)
		case res.Changed():
			r.logger.Info("network policy refreshed",
				zap.Int("added", len(res.Added)),
				zap.Int("removed", len(res.Removed)),
				zap.Bool("reinstalled", res.Reinstalled),
			)
		}

		if r.onResult != nil {
			r.onResult(res)
		}

		timer.Reset(r.nextInterval())
	}
}

// RefreshOnce runs a single refresh cycle.
//
// Ordering is add-before-remove: a rotated record is reachable through both
// the old and the new address for the width of one cycle rather than through
// neither.
func (r *Refresher) RefreshOnce(ctx context.Context) RefreshResult {
	res := RefreshResult{At: time.Now()}

	r.mu.Lock()
	prev := r.current
	r.mu.Unlock()

	// A container restart within the session replaces the network namespace,
	// taking the rules with it. Detect that first: diffing against a namespace
	// that holds no rules at all would "add" nothing and leave it wide open.
	installed, err := r.applier.Installed(ctx)
	if err != nil {
		res.Err = fmt.Errorf("checking installed rules: %w", err)
		return res
	}

	next, err := r.resolver.ResolvePolicyFresh(ctx, r.policy)
	if err != nil {
		res.Err = fmt.Errorf("resolving policy: %w", err)
		return res
	}
	carryForwardFailures(prev, next)

	if !installed {
		if err := r.applier.Reinstall(ctx, next); err != nil {
			res.Err = fmt.Errorf("reinstalling rules: %w", err)
			return res
		}
		r.setCurrent(next)
		res.Reinstalled = true
		return res
	}

	added, removed := next.DiffAllowed(prev)

	if len(added) > 0 {
		if err := r.applier.Allow(ctx, added); err != nil {
			// Nothing was removed yet, so the old set is still intact.
			res.Err = fmt.Errorf("allowing %d new address(es): %w", len(added), err)
			return res
		}
		res.Added = added
	}

	if len(removed) > 0 {
		if err := r.applier.Deny(ctx, removed); err != nil {
			// Keep the previous resolution as the believed state so the next
			// cycle retries the removal. Allow is idempotent, so re-adding the
			// same addresses is harmless.
			res.Added = added
			res.Err = fmt.Errorf("denying %d stale address(es): %w", len(removed), err)
			return res
		}
		res.Removed = removed
	}

	r.setCurrent(next)
	return res
}

func (r *Refresher) setCurrent(p *ResolvedPolicy) {
	r.mu.Lock()
	r.current = p
	r.mu.Unlock()
}

// nextInterval applies symmetric jitter around the base interval.
func (r *Refresher) nextInterval() time.Duration {
	if r.jitter <= 0 {
		return r.interval
	}
	spread := float64(r.interval) * r.jitter
	delta := (rand.Float64()*2 - 1) * spread //nolint:gosec // jitter, not a secret
	d := time.Duration(float64(r.interval) + delta)
	if d < time.Second {
		d = time.Second
	}
	return d
}

// carryForwardFailures keeps the previous pins for hosts that failed to
// resolve this cycle.
//
// Without this, a transient SERVFAIL would revoke a live allow-list entry and
// break a running task. A host is only closed when DNS positively says it now
// points somewhere else.
func carryForwardFailures(prev, next *ResolvedPolicy) {
	if prev == nil || next == nil {
		return
	}

	previous := make(map[string]HostResolution, len(prev.AllowedIPs))
	for _, hr := range prev.AllowedIPs {
		previous[hr.Pattern] = hr
	}

	for i, hr := range next.AllowedIPs {
		if hr.Error == nil && len(hr.IPs) > 0 {
			continue
		}
		old, ok := previous[hr.Pattern]
		if !ok || len(old.IPs) == 0 {
			continue
		}
		next.AllowedIPs[i].IPs = old.IPs
		next.AllowedIPs[i].ResolvedAt = old.ResolvedAt
	}
}

// defaultIntervalFor derives a cadence from the resolution's own TTL.
func defaultIntervalFor(resolved *ResolvedPolicy) time.Duration {
	interval := DefaultRefreshInterval
	if resolved != nil {
		if ttl := resolved.TTL(); ttl > 0 && ttl < interval {
			interval = ttl
		}
	}
	return interval
}

// clampInterval keeps the cadence inside the supported bounds.
func clampInterval(d time.Duration) time.Duration {
	switch {
	case d < MinRefreshInterval:
		return MinRefreshInterval
	case d > MaxRefreshInterval:
		return MaxRefreshInterval
	default:
		return d
	}
}
