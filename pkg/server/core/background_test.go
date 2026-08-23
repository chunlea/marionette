package core

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBackgroundTasks_RunsAndDrains(t *testing.T) {
	b := newBackgroundTasks(context.Background(), 4, zap.NewNop())

	var ran atomic.Int32
	for i := 0; i < 4; i++ {
		require.True(t, b.Go("unit", func(context.Context) { ran.Add(1) }))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, b.Wait(ctx))
	assert.Equal(t, int32(4), ran.Load())
}

// TestBackgroundTasks_RejectsWhenSaturated is the cap that used to be missing:
// a burst of failing runs spawned one unbounded goroutine each.
func TestBackgroundTasks_RejectsWhenSaturated(t *testing.T) {
	b := newBackgroundTasks(context.Background(), 1, zap.NewNop())

	release := make(chan struct{})
	var once sync.Once
	defer once.Do(func() { close(release) })

	started := make(chan struct{})
	require.True(t, b.Go("blocker", func(context.Context) {
		close(started)
		<-release
	}))
	<-started

	assert.False(t, b.Go("rejected", func(context.Context) {
		t.Error("saturated pool must not accept work")
	}))

	once.Do(func() { close(release) })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, b.Wait(ctx))
}

func TestBackgroundTasks_RejectsAfterWait(t *testing.T) {
	b := newBackgroundTasks(context.Background(), 2, zap.NewNop())
	require.NoError(t, b.Wait(context.Background()))

	assert.False(t, b.Go("after-shutdown", func(context.Context) {
		t.Error("a drained pool must not accept work")
	}))
}

// TestBackgroundTasks_RecoversPanic: these goroutines had no recover, so a
// panic anywhere in the retry path killed the whole server process.
func TestBackgroundTasks_RecoversPanic(t *testing.T) {
	b := newBackgroundTasks(context.Background(), 2, zap.NewNop())

	require.True(t, b.Go("panicker", func(context.Context) {
		panic("boom")
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, b.Wait(ctx))

	// The slot must have been returned despite the panic.
	b2 := newBackgroundTasks(context.Background(), 1, zap.NewNop())
	require.True(t, b2.Go("first", func(context.Context) { panic("boom") }))
	require.NoError(t, b2.Wait(context.Background()))
}

// TestBackgroundTasks_WaitRespectsDeadline stops a wedged job from holding
// shutdown open forever.
func TestBackgroundTasks_WaitRespectsDeadline(t *testing.T) {
	b := newBackgroundTasks(context.Background(), 1, zap.NewNop())

	release := make(chan struct{})
	defer close(release)
	started := make(chan struct{})
	require.True(t, b.Go("wedged", func(context.Context) {
		close(started)
		<-release
	}))
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	assert.Error(t, b.Wait(ctx))
}

// TestBackgroundTasks_UsesPoolContextNotCallerContext is the whole point:
// work scheduled from a request must not die when that request returns.
func TestBackgroundTasks_UsesPoolContextNotCallerContext(t *testing.T) {
	poolCtx, cancelPool := context.WithCancel(context.Background())
	defer cancelPool()
	b := newBackgroundTasks(poolCtx, 2, zap.NewNop())

	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest() // the request is already gone

	observed := make(chan error, 1)
	require.True(t, b.Go("unit", func(ctx context.Context) {
		observed <- ctx.Err()
	}))
	_ = requestCtx

	select {
	case err := <-observed:
		assert.NoError(t, err, "background work must not inherit the request's cancellation")
	case <-time.After(5 * time.Second):
		t.Fatal("background work never ran")
	}
}

func TestSleepCtx(t *testing.T) {
	assert.True(t, sleepCtx(context.Background(), time.Millisecond))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.False(t, sleepCtx(ctx, time.Hour))
	assert.False(t, sleepCtx(ctx, 0))
}

// TestRetryDelay_GrowsAndCaps documents the backoff the retry path had none of.
func TestRetryDelay_GrowsAndCaps(t *testing.T) {
	// Jitter is +/-25%, so compare against the jittered bounds.
	first := retryDelay(0)
	assert.GreaterOrEqual(t, first, retryBaseDelay*3/4)
	assert.LessOrEqual(t, first, retryBaseDelay*5/4)

	for attempt := 0; attempt < 20; attempt++ {
		d := retryDelay(attempt)
		assert.Positive(t, d)
		assert.LessOrEqual(t, d, retryMaxDelay*5/4, "backoff must stay capped")
	}

	assert.GreaterOrEqual(t, retryDelay(-1), retryBaseDelay*3/4)
}
