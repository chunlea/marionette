package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Duration is a time.Duration that unmarshals from either a duration string
// ("60s", "24h") or a plain number of seconds.
//
// Providers each carried their own copy of this type; this is the one.
type Duration time.Duration

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		// Not a string: try a number of seconds.
		var n int64
		if err := json.Unmarshal(b, &n); err != nil {
			return fmt.Errorf("invalid duration: %s", string(b))
		}
		*d = Duration(time.Duration(n) * time.Second)
		return nil
	}

	dur, err := time.ParseDuration(s)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", s, err)
	}
	*d = Duration(dur)
	return nil
}

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

// Duration returns the underlying time.Duration.
func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// rawSuspendConfig mirrors SuspendConfig with JSON-friendly durations.
type rawSuspendConfig struct {
	Strategy      SuspendStrategy `json:"strategy"`
	MinDuration   Duration        `json:"min_duration"`
	MaxDuration   Duration        `json:"max_duration"`
	Fallback      SuspendStrategy `json:"fallback"`
	SaveSnapshot  bool            `json:"save_snapshot"`
	SyncWorkspace bool            `json:"sync_workspace"`
}

// ParseSuspendConfig parses a provider's suspend configuration, applying the
// given per-provider defaults for anything the caller left unset. Durations
// may be given as strings ("60s") or as a number of seconds.
//
// Defaults differ per provider: Kubernetes cannot pause, E2B holds a pause for
// 30 days, pools release back to the pool.
func ParseSuspendConfig(data json.RawMessage, defaults SuspendConfig) (*SuspendConfig, error) {
	var raw rawSuspendConfig
	if len(data) > 0 {
		if err := json.Unmarshal(data, &raw); err != nil {
			return nil, fmt.Errorf("parsing suspend config: %w", err)
		}
	}

	cfg := &SuspendConfig{
		Strategy:      raw.Strategy,
		MinDuration:   raw.MinDuration.Duration(),
		MaxDuration:   raw.MaxDuration.Duration(),
		Fallback:      raw.Fallback,
		SaveSnapshot:  raw.SaveSnapshot,
		SyncWorkspace: raw.SyncWorkspace,
	}
	cfg.ApplyDefaults(defaults)

	return cfg, nil
}

// ApplyDefaults fills in any unset suspend setting from defaults.
func (c *SuspendConfig) ApplyDefaults(defaults SuspendConfig) {
	if c.Strategy == "" {
		c.Strategy = defaults.Strategy
	}
	if c.MinDuration == 0 {
		c.MinDuration = defaults.MinDuration
	}
	if c.MaxDuration == 0 {
		c.MaxDuration = defaults.MaxDuration
	}
	if c.Fallback == "" {
		c.Fallback = defaults.Fallback
	}
}

// SuspendFunc performs one suspend strategy against a runner.
//
// opts is handed through unchanged so a handler can reach the provider
// instance id the server persisted; a provider that can only find its own
// instances by enumeration needs it.
type SuspendFunc func(ctx context.Context, runnerID string, opts SuspendOptions) error

// SuspendDispatcher runs a provider's suspend strategies with shared
// validation, fallback and error handling.
//
// Handlers is the single source of truth for what a provider can actually do:
// a strategy with no handler is not supported, so capabilities and dispatch
// cannot drift apart the way they did when each provider kept its own switch
// statement alongside a separately maintained Capabilities list.
type SuspendDispatcher struct {
	// Provider is the provider name, used in errors.
	Provider string

	// Config is the provider's suspend configuration.
	Config SuspendConfig

	// Handlers maps each supported strategy to its implementation.
	Handlers map[SuspendStrategy]SuspendFunc
}

// Strategies returns the supported strategies in the canonical order.
// Use this to build ProviderCapabilities so it cannot claim more than the
// provider implements.
func (d SuspendDispatcher) Strategies() []SuspendStrategy {
	ordered := []SuspendStrategy{
		SuspendStrategyPause,
		SuspendStrategySnapshot,
		SuspendStrategyTerminatePreserveStorage,
		SuspendStrategyReleaseToPool,
		SuspendStrategyTerminate,
	}

	out := make([]SuspendStrategy, 0, len(d.Handlers))
	for _, s := range ordered {
		if _, ok := d.Handlers[s]; ok {
			out = append(out, s)
		}
	}
	return out
}

// Supports reports whether the provider implements a strategy.
func (d SuspendDispatcher) Supports(strategy SuspendStrategy) bool {
	_, ok := d.Handlers[strategy]
	return ok
}

// Suspend runs the requested strategy, falling back once to Config.Fallback
// if it fails.
//
// A strategy the provider does not implement is rejected outright rather than
// quietly replaced by the fallback: substituting a different behaviour for the
// one the caller explicitly asked for hides the mistake.
func (d SuspendDispatcher) Suspend(ctx context.Context, runnerID string, opts SuspendOptions) (*SuspendResult, error) {
	strategy := opts.Strategy
	if strategy == "" {
		strategy = d.Config.Strategy
	}

	if !d.Supports(strategy) {
		return nil, &ErrStrategyNotSupported{Strategy: strategy, Provider: d.Provider}
	}

	result, err := d.run(ctx, runnerID, strategy, opts)
	if err == nil {
		return result, nil
	}

	fallback := d.Config.Fallback
	if fallback == "" || fallback == strategy || !d.Supports(fallback) {
		return nil, err
	}

	result, fallbackErr := d.run(ctx, runnerID, fallback, opts)
	if fallbackErr != nil {
		// Report both. The per-provider copies of this logic recursed into
		// the fallback and dropped the original error entirely, which made
		// the actual cause of a suspend failure invisible.
		return nil, fmt.Errorf("%w (fallback %s also failed: %w)", err, fallback, fallbackErr)
	}

	return result, nil
}

// run executes a single strategy without fallback.
func (d SuspendDispatcher) run(ctx context.Context, runnerID string, strategy SuspendStrategy, opts SuspendOptions) (*SuspendResult, error) {
	fn, ok := d.Handlers[strategy]
	if !ok {
		return nil, &ErrStrategyNotSupported{Strategy: strategy, Provider: d.Provider}
	}

	if err := fn(ctx, runnerID, opts); err != nil {
		return nil, &ErrSuspendFailed{RunnerID: runnerID, Strategy: strategy, Cause: err}
	}

	return &SuspendResult{
		Strategy: strategy,
		// Always false: no workspace sync exists yet. Providers used to echo
		// back opts.SyncWorkspace here, reporting a sync that never happened.
		// Set this for real when CAS sync lands (decision D3).
		WorkspaceSynced: false,
		SuspendedAt:     time.Now(),
	}, nil
}
