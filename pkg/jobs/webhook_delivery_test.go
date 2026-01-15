package jobs

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// mockWebhookDeliverer is a mock implementation of WebhookDeliverer for testing.
type mockWebhookDeliverer struct {
	mu             sync.Mutex
	deliverCalls   int
	deliveredCount int
	err            error
}

func (m *mockWebhookDeliverer) DeliverPendingEvents(_ context.Context, limit int) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deliverCalls++
	if m.err != nil {
		return 0, m.err
	}
	// Simulate delivering some events (up to limit)
	delivered := min(m.deliveredCount, limit)
	return delivered, nil
}

func (m *mockWebhookDeliverer) setError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.err = err
}

func (m *mockWebhookDeliverer) getDeliverCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.deliverCalls
}

func TestWebhookDeliveryJob_StartStop(t *testing.T) {
	deliverer := &mockWebhookDeliverer{}
	logger := zap.NewNop()

	job := NewWebhookDeliveryJob(deliverer, WebhookDeliveryJobConfig{
		Interval:  100 * time.Millisecond,
		BatchSize: 50,
		Logger:    logger,
	})

	ctx := context.Background()

	// Start the job
	err := job.Start(ctx)
	require.NoError(t, err)
	assert.True(t, job.IsRunning())

	// Starting again should fail
	err = job.Start(ctx)
	assert.Error(t, err)

	// Wait for at least one delivery run
	time.Sleep(150 * time.Millisecond)
	assert.GreaterOrEqual(t, deliverer.getDeliverCalls(), 1)

	// Stop the job
	stopCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	err = job.Stop(stopCtx)
	require.NoError(t, err)
	assert.False(t, job.IsRunning())
}

func TestWebhookDeliveryJob_RunNow(t *testing.T) {
	deliverer := &mockWebhookDeliverer{deliveredCount: 5}
	logger := zap.NewNop()

	job := NewWebhookDeliveryJob(deliverer, WebhookDeliveryJobConfig{
		Interval:  time.Hour, // Long interval so only RunNow triggers
		BatchSize: 100,
		Logger:    logger,
	})

	ctx := context.Background()

	// RunNow without starting should still work
	result, err := job.RunNow(ctx)
	require.NoError(t, err)
	assert.Equal(t, 5, result.Delivered)
	assert.Equal(t, 1, deliverer.getDeliverCalls())
}

func TestWebhookDeliveryJob_Stats(t *testing.T) {
	deliverer := &mockWebhookDeliverer{deliveredCount: 10}
	logger := zap.NewNop()

	job := NewWebhookDeliveryJob(deliverer, WebhookDeliveryJobConfig{
		Interval:  time.Hour,
		BatchSize: 100,
		Logger:    logger,
	})

	ctx := context.Background()

	// Run twice
	_, err := job.RunNow(ctx)
	require.NoError(t, err)
	_, err = job.RunNow(ctx)
	require.NoError(t, err)

	lastDelivered, totalDelivered := job.Stats()
	assert.Equal(t, 10, lastDelivered)
	assert.Equal(t, int64(20), totalDelivered)
}

func TestWebhookDeliveryJob_ErrorHandling(t *testing.T) {
	deliverer := &mockWebhookDeliverer{}
	deliverer.setError(errors.New("delivery failed"))
	logger := zap.NewNop()

	job := NewWebhookDeliveryJob(deliverer, WebhookDeliveryJobConfig{
		Interval:  time.Hour,
		BatchSize: 100,
		Logger:    logger,
	})

	ctx := context.Background()

	// Should return error
	_, err := job.RunNow(ctx)
	assert.Error(t, err)
	assert.Equal(t, "delivery failed", err.Error())
}

func TestWebhookDeliveryJob_DefaultConfig(t *testing.T) {
	deliverer := &mockWebhookDeliverer{}

	// Test with zero/nil values
	job := NewWebhookDeliveryJob(deliverer, WebhookDeliveryJobConfig{})

	// Should have defaults applied
	assert.NotNil(t, job)
	// Internal state should be initialized
	assert.False(t, job.IsRunning())
}
