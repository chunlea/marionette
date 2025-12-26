package agent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExponentialBackoff_Sequence(t *testing.T) {
	cfg := BackoffConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		Jitter:       0, // No jitter for predictable testing
		MaxRetries:   -1,
	}

	b := NewExponentialBackoff(cfg)

	// Expected sequence without jitter: 100ms, 200ms, 400ms, 800ms, 1000ms (capped)
	expected := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		1000 * time.Millisecond, // Capped at MaxDelay
		1000 * time.Millisecond,
	}

	for i, exp := range expected {
		got := b.Next()
		assert.Equal(t, exp, got, "attempt %d", i+1)
	}
}

func TestExponentialBackoff_MaxRetries(t *testing.T) {
	cfg := BackoffConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		Jitter:       0,
		MaxRetries:   3,
	}

	b := NewExponentialBackoff(cfg)

	// Should get 3 delays, then 0
	assert.NotZero(t, b.Next())
	assert.Equal(t, 1, b.Attempt())
	assert.NotZero(t, b.Next())
	assert.Equal(t, 2, b.Attempt())
	assert.NotZero(t, b.Next())
	assert.Equal(t, 3, b.Attempt())

	// Fourth call should return 0 (max retries exceeded)
	assert.Zero(t, b.Next())
	assert.Equal(t, 3, b.Attempt()) // Attempt should not increase
}

func TestExponentialBackoff_Jitter(t *testing.T) {
	cfg := BackoffConfig{
		InitialDelay: 1 * time.Second,
		MaxDelay:     60 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.2, // +/- 20%
		MaxRetries:   -1,
	}

	b := NewExponentialBackoff(cfg)

	// First delay should be around 1s +/- 20%
	for i := 0; i < 100; i++ {
		b.Reset()
		delay := b.Next()
		// 1s with 20% jitter means 800ms to 1200ms
		assert.GreaterOrEqual(t, delay, 800*time.Millisecond, "delay too small")
		assert.LessOrEqual(t, delay, 1200*time.Millisecond, "delay too large")
	}
}

func TestExponentialBackoff_Reset(t *testing.T) {
	cfg := BackoffConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		Jitter:       0,
		MaxRetries:   -1,
	}

	b := NewExponentialBackoff(cfg)

	// Advance a few times
	b.Next() // 100ms
	b.Next() // 200ms
	b.Next() // 400ms
	assert.Equal(t, 3, b.Attempt())

	// Reset
	b.Reset()
	assert.Equal(t, 0, b.Attempt())

	// Should start over
	delay := b.Next()
	assert.Equal(t, 100*time.Millisecond, delay)
	assert.Equal(t, 1, b.Attempt())
}

func TestExponentialBackoff_Defaults(t *testing.T) {
	// DefaultBackoffConfig should use defaults including jitter
	b := NewExponentialBackoff(DefaultBackoffConfig())

	// First delay should be around 1s (default)
	delay := b.Next()
	require.NotZero(t, delay)

	// Should be within jitter range of 1s (default 20% jitter)
	assert.GreaterOrEqual(t, delay, 800*time.Millisecond)
	assert.LessOrEqual(t, delay, 1200*time.Millisecond)
}

func TestDefaultBackoffConfig(t *testing.T) {
	cfg := DefaultBackoffConfig()

	assert.Equal(t, 1*time.Second, cfg.InitialDelay)
	assert.Equal(t, 60*time.Second, cfg.MaxDelay)
	assert.Equal(t, 2.0, cfg.Multiplier)
	assert.Equal(t, 0.2, cfg.Jitter)
	assert.Equal(t, -1, cfg.MaxRetries)
}

func TestExponentialBackoff_Concurrent(t *testing.T) {
	cfg := BackoffConfig{
		InitialDelay: 100 * time.Millisecond,
		MaxDelay:     1 * time.Second,
		Multiplier:   2.0,
		Jitter:       0.1,
		MaxRetries:   -1,
	}

	b := NewExponentialBackoff(cfg)

	// Run concurrent Next() calls
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func() {
			for j := 0; j < 100; j++ {
				_ = b.Next()
			}
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Should have made 1000 attempts total
	assert.Equal(t, 1000, b.Attempt())
}
