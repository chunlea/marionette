package core

import (
	"context"
	"math/rand/v2"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Background work defaults.
const (
	// defaultBackgroundWorkers bounds how much fire-and-forget work can be in
	// flight at once. Retries used to be spawned as bare goroutines with no
	// limit: a burst of failing runs spawned a goroutine per run, all racing to
	// talk to a store that was being closed.
	//
	// A retry holds its slot for the whole backoff, so a fleet-wide failure can
	// saturate the pool. That is deliberate: the rejected retries are logged
	// and the task timeout enforcer picks those tasks up, which is slower than
	// an immediate retry but bounded, whereas the old behaviour was not.
	defaultBackgroundWorkers = 32

	// retryBaseDelay is the first retry backoff.
	retryBaseDelay = 2 * time.Second

	// retryMaxDelay caps the exponential backoff.
	retryMaxDelay = 2 * time.Minute
)

// backgroundTasks runs fire-and-forget work with a bounded worker count, a
// lifetime independent of any request, and a drain on shutdown.
//
// Every previous background spawn in this package took the caller's request
// context, so work that outlives the request was cancelled the moment the
// handler returned - or, worse, took context.Background() and became invisible
// to shutdown entirely.
type backgroundTasks struct {
	ctx    context.Context
	slots  chan struct{}
	wg     sync.WaitGroup
	logger *zap.Logger

	mu     sync.Mutex
	closed bool
}

// newBackgroundTasks creates a pool bound to ctx, which must be an application
// lifetime context and never a request context.
func newBackgroundTasks(ctx context.Context, limit int, logger *zap.Logger) *backgroundTasks {
	if limit <= 0 {
		limit = defaultBackgroundWorkers
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	return &backgroundTasks{
		ctx:    ctx,
		slots:  make(chan struct{}, limit),
		logger: logger,
	}
}

// Go runs fn on a background goroutine under the pool's context. It never
// blocks the caller and reports whether the work was accepted: a saturated pool
// or a shutting-down App rejects it, loudly, instead of growing without bound.
//
// A panic in fn is recovered and logged. These goroutines used to have no
// recover at all, so a panic anywhere in the retry path killed the process.
func (b *backgroundTasks) Go(name string, fn func(ctx context.Context)) bool {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		b.logger.Debug("background work rejected: shutting down", zap.String("job", name))
		return false
	}

	select {
	case b.slots <- struct{}{}:
	default:
		b.mu.Unlock()
		b.logger.Warn("background work rejected: worker pool saturated",
			zap.String("job", name),
			zap.Int("limit", cap(b.slots)),
		)
		return false
	}

	b.wg.Add(1)
	b.mu.Unlock()

	go func() {
		defer b.wg.Done()
		defer func() { <-b.slots }()
		defer func() {
			if r := recover(); r != nil {
				b.logger.Error("background work panicked",
					zap.String("job", name),
					zap.Any("panic", r),
				)
			}
		}()
		fn(b.ctx)
	}()
	return true
}

// Wait stops accepting new work and drains what is in flight, giving up when
// ctx expires so a wedged job cannot hold shutdown open forever.
func (b *backgroundTasks) Wait(ctx context.Context) error {
	b.mu.Lock()
	b.closed = true
	b.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		b.wg.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// sleepCtx waits for d, or until ctx is done. It reports whether the full wait
// elapsed.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

// retryDelay is the backoff before retry attempt n (0-based): exponential up to
// a cap, with jitter so a fleet-wide failure does not resend in lockstep.
func retryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}

	delay := retryBaseDelay
	for i := 0; i < attempt && delay < retryMaxDelay; i++ {
		delay *= 2
	}
	if delay > retryMaxDelay {
		delay = retryMaxDelay
	}

	// Up to +/-25% jitter.
	jitter := time.Duration(rand.Int64N(int64(delay/2)+1)) - delay/4
	return delay + jitter
}
