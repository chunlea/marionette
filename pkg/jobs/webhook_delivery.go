package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// WebhookDeliverer is the interface for delivering webhook events.
type WebhookDeliverer interface {
	// DeliverPendingEvents processes pending webhook events.
	// Returns the number of events delivered and any error.
	DeliverPendingEvents(ctx context.Context, limit int) (int, error)
}

// WebhookDeliveryJob manages periodic delivery of pending webhook events.
type WebhookDeliveryJob struct {
	deliverer WebhookDeliverer
	interval  time.Duration
	batchSize int
	logger    *zap.Logger

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	// Metrics
	lastRun         time.Time
	lastDelivered   int
	totalDelivered  int64
	consecutiveErrs int
}

// WebhookDeliveryJobConfig contains configuration for the webhook delivery job.
type WebhookDeliveryJobConfig struct {
	// Interval is how often to check for pending events. Default: 10 seconds
	Interval time.Duration

	// BatchSize is the number of events to process per run. Default: 100
	BatchSize int

	// Logger is the structured logger to use.
	Logger *zap.Logger
}

// WebhookDeliveryResult contains the result of a delivery run.
type WebhookDeliveryResult struct {
	Delivered int
	Duration  time.Duration
}

// NewWebhookDeliveryJob creates a new webhook delivery job.
func NewWebhookDeliveryJob(deliverer WebhookDeliverer, config WebhookDeliveryJobConfig) *WebhookDeliveryJob {
	if config.Interval <= 0 {
		config.Interval = 10 * time.Second
	}
	if config.BatchSize <= 0 {
		config.BatchSize = 100
	}
	if config.Logger == nil {
		config.Logger = zap.NewNop()
	}

	return &WebhookDeliveryJob{
		deliverer: deliverer,
		interval:  config.Interval,
		batchSize: config.BatchSize,
		logger:    config.Logger,
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Start begins the periodic delivery job.
func (j *WebhookDeliveryJob) Start(ctx context.Context) error {
	j.mu.Lock()
	if j.running {
		j.mu.Unlock()
		return fmt.Errorf("webhook delivery job already running")
	}
	j.running = true
	j.stopCh = make(chan struct{})
	j.doneCh = make(chan struct{})
	j.mu.Unlock()

	go j.run(ctx)
	j.logger.Info("webhook delivery job started",
		zap.Duration("interval", j.interval),
		zap.Int("batch_size", j.batchSize),
	)
	return nil
}

// Stop stops the delivery job gracefully.
func (j *WebhookDeliveryJob) Stop(ctx context.Context) error {
	j.mu.Lock()
	if !j.running {
		j.mu.Unlock()
		return nil
	}
	close(j.stopCh)
	doneCh := j.doneCh
	j.mu.Unlock()

	select {
	case <-doneCh:
		j.logger.Info("webhook delivery job stopped")
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// IsRunning returns whether the job is currently running.
func (j *WebhookDeliveryJob) IsRunning() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.running
}

// LastRun returns the time of the last delivery run.
func (j *WebhookDeliveryJob) LastRun() time.Time {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastRun
}

// Stats returns delivery statistics.
func (j *WebhookDeliveryJob) Stats() (lastDelivered int, totalDelivered int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.lastDelivered, j.totalDelivered
}

// RunNow triggers an immediate delivery run.
func (j *WebhookDeliveryJob) RunNow(ctx context.Context) (*WebhookDeliveryResult, error) {
	return j.runDelivery(ctx)
}

func (j *WebhookDeliveryJob) run(ctx context.Context) {
	defer func() {
		j.mu.Lock()
		j.running = false
		close(j.doneCh)
		j.mu.Unlock()
	}()

	ticker := time.NewTicker(j.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-j.stopCh:
			return
		case <-ticker.C:
			if _, err := j.runDelivery(ctx); err != nil {
				j.mu.Lock()
				j.consecutiveErrs++
				j.mu.Unlock()

				// Backoff on consecutive errors
				if j.consecutiveErrs > 5 {
					j.logger.Warn("multiple consecutive delivery errors, backing off",
						zap.Int("consecutive_errors", j.consecutiveErrs),
						zap.Error(err),
					)
				} else {
					j.logger.Error("webhook delivery failed", zap.Error(err))
				}
			} else {
				j.mu.Lock()
				j.consecutiveErrs = 0
				j.mu.Unlock()
			}
		}
	}
}

func (j *WebhookDeliveryJob) runDelivery(ctx context.Context) (*WebhookDeliveryResult, error) {
	start := time.Now()

	delivered, err := j.deliverer.DeliverPendingEvents(ctx, j.batchSize)
	if err != nil {
		return nil, err
	}

	duration := time.Since(start)

	j.mu.Lock()
	j.lastRun = time.Now()
	j.lastDelivered = delivered
	j.totalDelivered += int64(delivered)
	j.mu.Unlock()

	if delivered > 0 {
		j.logger.Debug("webhook delivery run completed",
			zap.Int("delivered", delivered),
			zap.Duration("duration", duration),
		)
	}

	return &WebhookDeliveryResult{
		Delivered: delivered,
		Duration:  duration,
	}, nil
}
