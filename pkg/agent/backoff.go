package agent

import (
	"crypto/rand"
	"encoding/binary"
	"math"
	"sync"
	"time"
)

// BackoffConfig configures exponential backoff behavior.
type BackoffConfig struct {
	// InitialDelay is the first delay after a failure. Default: 1s.
	InitialDelay time.Duration

	// MaxDelay is the maximum delay between retries. Default: 60s.
	MaxDelay time.Duration

	// Multiplier is the factor by which delay increases. Default: 2.0.
	Multiplier float64

	// Jitter adds randomness to the delay. 0.2 means +/- 20%. Default: 0.2.
	Jitter float64

	// MaxRetries is the maximum number of retry attempts. -1 for unlimited. Default: -1.
	MaxRetries int
}

// DefaultBackoffConfig returns sensible defaults for backoff.
func DefaultBackoffConfig() BackoffConfig {
	return BackoffConfig{
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.2,
		MaxRetries:   -1,
	}
}

// ExponentialBackoff provides exponential backoff with jitter.
type ExponentialBackoff struct {
	cfg     BackoffConfig
	attempt int
	mu      sync.Mutex
}

// NewExponentialBackoff creates a new backoff calculator.
// If you want default values including jitter, use DefaultBackoffConfig().
func NewExponentialBackoff(cfg BackoffConfig) *ExponentialBackoff {
	// Apply defaults for zero values (except Jitter, which 0 means "no jitter")
	if cfg.InitialDelay == 0 {
		cfg.InitialDelay = 1 * time.Second
	}
	if cfg.MaxDelay == 0 {
		cfg.MaxDelay = 60 * time.Second
	}
	if cfg.Multiplier == 0 {
		cfg.Multiplier = 2.0
	}
	// Note: Jitter = 0 means no jitter, which is a valid value.
	// Use DefaultBackoffConfig() if you want the default jitter of 0.2.

	return &ExponentialBackoff{
		cfg: cfg,
	}
}

// Next returns the next backoff delay, or 0 if max retries exceeded.
func (b *ExponentialBackoff) Next() time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Check if we've exceeded max retries
	if b.cfg.MaxRetries >= 0 && b.attempt >= b.cfg.MaxRetries {
		return 0
	}

	b.attempt++

	// Calculate base delay: initial * multiplier^(attempt-1)
	delay := float64(b.cfg.InitialDelay) * math.Pow(b.cfg.Multiplier, float64(b.attempt-1))

	// Cap at max delay
	if delay > float64(b.cfg.MaxDelay) {
		delay = float64(b.cfg.MaxDelay)
	}

	// Add jitter: delay * (1 + random(-jitter, +jitter))
	jitterRange := delay * b.cfg.Jitter
	jitter := (cryptoRandFloat64()*2 - 1) * jitterRange
	delay += jitter

	// Ensure delay is not negative (shouldn't happen, but safety check)
	if delay < 0 {
		delay = float64(b.cfg.InitialDelay)
	}

	return time.Duration(delay)
}

// Reset resets the backoff counter to zero.
func (b *ExponentialBackoff) Reset() {
	b.mu.Lock()
	b.attempt = 0
	b.mu.Unlock()
}

// Attempt returns the current attempt number (1-indexed after first Next call).
func (b *ExponentialBackoff) Attempt() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.attempt
}

// cryptoRandFloat64 returns a cryptographically random float64 in [0, 1).
func cryptoRandFloat64() float64 {
	var buf [8]byte
	_, err := rand.Read(buf[:])
	if err != nil {
		// Fallback to 0.5 if crypto/rand fails (extremely unlikely)
		return 0.5
	}
	// Convert to uint64 and then to float64 in [0, 1)
	n := binary.BigEndian.Uint64(buf[:])
	return float64(n) / float64(1<<64)
}
