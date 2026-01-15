package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"go.uber.org/zap"
)

// Dispatcher handles webhook delivery with retry support.
type Dispatcher struct {
	client  *http.Client
	config  Config
	matcher *Matcher
	logger  *zap.Logger

	// Worker pool
	workCh   chan *deliveryJob
	wg       sync.WaitGroup
	stopOnce sync.Once
	stopCh   chan struct{}
}

// deliveryJob represents a single webhook delivery task.
type deliveryJob struct {
	webhook  *WebhookInfo
	payload  *Payload
	eventID  string
	secret   string
	callback DeliveryCallback
}

// WebhookInfo contains webhook delivery configuration.
type WebhookInfo struct {
	ID             string
	URL            string
	Headers        map[string]string
	TimeoutSeconds int
}

// DeliveryCallback is called after delivery attempt.
type DeliveryCallback func(result DeliveryResult)

// NewDispatcher creates a new webhook dispatcher.
func NewDispatcher(config Config, logger *zap.Logger) *Dispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}

	d := &Dispatcher{
		client: &http.Client{
			Timeout: time.Duration(config.DefaultTimeoutSeconds) * time.Second,
		},
		config:  config,
		matcher: NewMatcher(),
		logger:  logger,
		workCh:  make(chan *deliveryJob, config.BatchSize),
		stopCh:  make(chan struct{}),
	}

	// Start worker pool
	for i := 0; i < config.WorkerCount; i++ {
		d.wg.Add(1)
		go d.worker(i)
	}

	return d
}

// Dispatch queues a webhook for delivery.
func (d *Dispatcher) Dispatch(ctx context.Context, webhook *WebhookInfo, payload *Payload, eventID, secret string, callback DeliveryCallback) error {
	job := &deliveryJob{
		webhook:  webhook,
		payload:  payload,
		eventID:  eventID,
		secret:   secret,
		callback: callback,
	}

	select {
	case d.workCh <- job:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-d.stopCh:
		return fmt.Errorf("dispatcher stopped")
	}
}

// DispatchSync delivers a webhook synchronously and returns the result.
func (d *Dispatcher) DispatchSync(ctx context.Context, webhook *WebhookInfo, payload *Payload, eventID, secret string) DeliveryResult {
	return d.deliver(ctx, webhook, payload, eventID, secret)
}

// Stop gracefully stops the dispatcher.
func (d *Dispatcher) Stop() {
	d.stopOnce.Do(func() {
		close(d.stopCh)
		close(d.workCh)
		d.wg.Wait()
	})
}

// worker processes delivery jobs from the work channel.
func (d *Dispatcher) worker(_ int) {
	defer d.wg.Done()

	for job := range d.workCh {
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(d.config.DefaultTimeoutSeconds)*time.Second)
		result := d.deliver(ctx, job.webhook, job.payload, job.eventID, job.secret)
		cancel()

		if job.callback != nil {
			job.callback(result)
		}
	}
}

// deliver performs the actual HTTP request.
func (d *Dispatcher) deliver(ctx context.Context, webhook *WebhookInfo, payload *Payload, eventID, secret string) DeliveryResult {
	start := time.Now()

	// Marshal payload
	body, err := json.Marshal(payload)
	if err != nil {
		return DeliveryResult{
			Success:  false,
			Error:    fmt.Errorf("failed to marshal payload: %w", err),
			Duration: time.Since(start),
		}
	}

	// Check payload size
	if len(body) > d.config.MaxPayloadSize {
		return DeliveryResult{
			Success:  false,
			Error:    fmt.Errorf("payload size %d exceeds max %d", len(body), d.config.MaxPayloadSize),
			Duration: time.Since(start),
		}
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, bytes.NewReader(body))
	if err != nil {
		return DeliveryResult{
			Success:  false,
			Error:    fmt.Errorf("failed to create request: %w", err),
			Duration: time.Since(start),
		}
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", d.config.UserAgent)
	req.Header.Set(IDHeader, eventID)

	// Add custom headers
	for k, v := range webhook.Headers {
		req.Header.Set(k, v)
	}

	// Sign the request
	timestamp := time.Now()
	signature := Sign(body, secret, timestamp)
	req.Header.Set(SignatureHeader, signature)
	req.Header.Set(TimestampHeader, fmt.Sprintf("%d", timestamp.Unix()))

	// Send request
	resp, err := d.client.Do(req)
	if err != nil {
		return DeliveryResult{
			Success:  false,
			Error:    fmt.Errorf("request failed: %w", err),
			Duration: time.Since(start),
		}
	}
	defer func() { _ = resp.Body.Close() }()

	// Read response body (limited to prevent memory issues)
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024*1024)) // 1MB limit

	// Check status code
	success := resp.StatusCode >= 200 && resp.StatusCode < 300
	result := DeliveryResult{
		Success:    success,
		StatusCode: resp.StatusCode,
		Duration:   time.Since(start),
	}

	if !success {
		result.Error = fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}

	return result
}

// BuildPayload creates a webhook payload from event data.
func BuildPayload(eventType string, resource ResourceInfo, data any) (*Payload, error) {
	dataBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal event data: %w", err)
	}

	return &Payload{
		Event:     eventType,
		Timestamp: time.Now().UTC(),
		Resource:  resource,
		Data:      dataBytes,
	}, nil
}

// CalculateNextRetry calculates the next retry time using exponential backoff.
func CalculateNextRetry(attempt int, baseDelay time.Duration) time.Time {
	// Exponential backoff: baseDelay * 2^attempt
	// Cap at 24 hours
	delay := baseDelay * time.Duration(1<<uint(attempt))
	maxDelay := 24 * time.Hour
	if delay > maxDelay {
		delay = maxDelay
	}
	return time.Now().Add(delay)
}

// ShouldRetry determines if a delivery should be retried based on status code.
func ShouldRetry(statusCode int) bool {
	// Retry on server errors (5xx) and specific client errors
	switch {
	case statusCode >= 500:
		return true // Server errors
	case statusCode == 408:
		return true // Request timeout
	case statusCode == 429:
		return true // Too many requests
	default:
		return false // Don't retry other errors (4xx)
	}
}
