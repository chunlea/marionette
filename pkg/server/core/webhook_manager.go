package core

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
	"github.com/chunlea/marionette/pkg/webhook"
	"go.uber.org/zap"
)

// Webhook-related errors.
var (
	ErrWebhookNotFound      = errors.New("webhook not found")
	ErrWebhookNameExists    = errors.New("webhook name already exists")
	ErrWebhookEventNotFound = errors.New("webhook event not found")
	ErrInvalidEventPattern  = errors.New("invalid event pattern")
)

// WebhookManager handles webhook configuration and event dispatch.
type WebhookManager struct {
	store      store.Store
	dispatcher *webhook.Dispatcher
	matcher    *webhook.Matcher
	config     webhook.Config
	logger     *zap.Logger
}

// WebhookManagerInterface defines the interface for webhook management.
type WebhookManagerInterface interface {
	// Webhook CRUD
	Create(ctx context.Context, input *CreateWebhookInput) (*store.Webhook, string, error)
	Get(ctx context.Context, webhookID string) (*store.Webhook, error)
	GetByName(ctx context.Context, name string, tenantID *string) (*store.Webhook, error)
	List(ctx context.Context, opts store.ListWebhooksOptions) (*store.ListResult[store.Webhook], error)
	Update(ctx context.Context, webhookID string, input *UpdateWebhookInput) error
	Delete(ctx context.Context, webhookID string) error
	RotateSecret(ctx context.Context, webhookID string) (string, error)

	// Event dispatch
	Dispatch(ctx context.Context, eventType string, resource webhook.ResourceInfo, data any, tenantID *string) error

	// Webhook events
	GetEvent(ctx context.Context, eventID string) (*store.WebhookEvent, error)
	ListEvents(ctx context.Context, opts store.ListWebhookEventsOptions) (*store.ListResult[store.WebhookEvent], error)
	RetryEvent(ctx context.Context, eventID string) error
}

// CreateWebhookInput contains input for creating a webhook.
type CreateWebhookInput struct {
	Name              string
	URL               string
	Events            []string
	MaxRetries        *int
	RetryDelaySeconds *int
	TimeoutSeconds    *int
	Headers           map[string]string
	TenantID          *string
	Labels            map[string]string
	Annotations       map[string]string
}

// UpdateWebhookInput contains input for updating a webhook.
type UpdateWebhookInput struct {
	Name              *string
	URL               *string
	Events            []string
	IsActive          *bool
	MaxRetries        *int
	RetryDelaySeconds *int
	TimeoutSeconds    *int
	Headers           map[string]string
	Labels            map[string]string
	Annotations       map[string]string
}

// NewWebhookManager creates a new WebhookManager.
func NewWebhookManager(
	store store.Store,
	config webhook.Config,
	logger *zap.Logger,
) *WebhookManager {
	if logger == nil {
		logger = zap.NewNop()
	}

	return &WebhookManager{
		store:      store,
		dispatcher: webhook.NewDispatcher(config, logger.Named("dispatcher")),
		matcher:    webhook.NewMatcher(),
		config:     config,
		logger:     logger,
	}
}

// Stop gracefully stops the webhook manager.
func (m *WebhookManager) Stop() {
	if m.dispatcher != nil {
		m.dispatcher.Stop()
	}
}

// Create creates a new webhook and returns its secret (only shown once).
func (m *WebhookManager) Create(ctx context.Context, input *CreateWebhookInput) (*store.Webhook, string, error) {
	// Validate event patterns
	for _, pattern := range input.Events {
		if !webhook.ValidateEventPattern(pattern) {
			m.logger.Warn("invalid event pattern", zap.String("pattern", pattern))
			return nil, "", ErrInvalidEventPattern
		}
	}

	// Check for existing webhook with same name
	existing, err := m.store.GetWebhookByName(ctx, input.Name, input.TenantID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, "", err
	}
	if existing != nil {
		return nil, "", ErrWebhookNameExists
	}

	// Generate secret
	secret, secretHash, secretPrefix, err := webhook.GenerateSecret()
	if err != nil {
		return nil, "", err
	}

	now := time.Now()
	wh := &store.Webhook{
		ID:                id.Webhook(),
		Name:              input.Name,
		URL:               input.URL,
		Events:            input.Events,
		SecretHash:        secretHash,
		SecretPrefix:      secretPrefix,
		IsActive:          true,
		MaxRetries:        m.config.DefaultMaxRetries,
		RetryDelaySeconds: m.config.DefaultRetryDelaySeconds,
		TimeoutSeconds:    m.config.DefaultTimeoutSeconds,
		TenantID:          input.TenantID,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// Set labels if provided
	if input.Labels != nil {
		labelsJSON, err := json.Marshal(input.Labels)
		if err != nil {
			return nil, "", err
		}
		wh.Labels = labelsJSON
	}

	// Set annotations if provided
	if input.Annotations != nil {
		annotationsJSON, err := json.Marshal(input.Annotations)
		if err != nil {
			return nil, "", err
		}
		wh.Annotations = annotationsJSON
	}

	// Apply optional overrides
	if input.MaxRetries != nil {
		wh.MaxRetries = *input.MaxRetries
	}
	if input.RetryDelaySeconds != nil {
		wh.RetryDelaySeconds = *input.RetryDelaySeconds
	}
	if input.TimeoutSeconds != nil {
		wh.TimeoutSeconds = *input.TimeoutSeconds
	}
	if input.Headers != nil {
		headersJSON, err := json.Marshal(input.Headers)
		if err != nil {
			return nil, "", err
		}
		wh.Headers = headersJSON
	}

	if err := m.store.CreateWebhook(ctx, wh); err != nil {
		return nil, "", err
	}

	m.logger.Info("webhook created",
		zap.String("webhook_id", wh.ID),
		zap.String("name", wh.Name),
		zap.Strings("events", wh.Events),
	)

	return wh, secret, nil
}

// Get retrieves a webhook by ID.
func (m *WebhookManager) Get(ctx context.Context, webhookID string) (*store.Webhook, error) {
	wh, err := m.store.GetWebhook(ctx, webhookID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrWebhookNotFound
		}
		return nil, err
	}
	return wh, nil
}

// GetByName retrieves a webhook by name.
func (m *WebhookManager) GetByName(ctx context.Context, name string, tenantID *string) (*store.Webhook, error) {
	wh, err := m.store.GetWebhookByName(ctx, name, tenantID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrWebhookNotFound
		}
		return nil, err
	}
	return wh, nil
}

// List retrieves webhooks with filters.
func (m *WebhookManager) List(ctx context.Context, opts store.ListWebhooksOptions) (*store.ListResult[store.Webhook], error) {
	return m.store.ListWebhooks(ctx, opts)
}

// Update updates a webhook.
func (m *WebhookManager) Update(ctx context.Context, webhookID string, input *UpdateWebhookInput) error {
	// Validate event patterns if provided
	if input.Events != nil {
		for _, pattern := range input.Events {
			if !webhook.ValidateEventPattern(pattern) {
				m.logger.Warn("invalid event pattern", zap.String("pattern", pattern))
				return ErrInvalidEventPattern
			}
		}
	}

	// Check webhook exists
	_, err := m.store.GetWebhook(ctx, webhookID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrWebhookNotFound
		}
		return err
	}

	updates := store.WebhookUpdates{
		Name:              input.Name,
		URL:               input.URL,
		Events:            input.Events,
		IsActive:          input.IsActive,
		MaxRetries:        input.MaxRetries,
		RetryDelaySeconds: input.RetryDelaySeconds,
		TimeoutSeconds:    input.TimeoutSeconds,
	}

	if input.Headers != nil {
		headersJSON, err := json.Marshal(input.Headers)
		if err != nil {
			return err
		}
		updates.Headers = headersJSON
	}

	if input.Labels != nil {
		labelsJSON, err := json.Marshal(input.Labels)
		if err != nil {
			return err
		}
		updates.Labels = labelsJSON
	}

	if input.Annotations != nil {
		annotationsJSON, err := json.Marshal(input.Annotations)
		if err != nil {
			return err
		}
		updates.Annotations = annotationsJSON
	}

	if err := m.store.UpdateWebhook(ctx, webhookID, updates); err != nil {
		return err
	}

	m.logger.Info("webhook updated",
		zap.String("webhook_id", webhookID),
	)

	return nil
}

// Delete deletes a webhook and cancels its pending events.
func (m *WebhookManager) Delete(ctx context.Context, webhookID string) error {
	// Check webhook exists
	_, err := m.store.GetWebhook(ctx, webhookID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrWebhookNotFound
		}
		return err
	}

	// Cancel pending events for this webhook
	if err := m.store.CancelWebhookEventsByWebhook(ctx, webhookID); err != nil {
		m.logger.Warn("failed to cancel webhook events",
			zap.String("webhook_id", webhookID),
			zap.Error(err),
		)
	}

	if err := m.store.DeleteWebhook(ctx, webhookID); err != nil {
		return err
	}

	m.logger.Info("webhook deleted",
		zap.String("webhook_id", webhookID),
	)

	return nil
}

// RotateSecret generates a new secret for a webhook.
// Returns the new secret (only shown once).
func (m *WebhookManager) RotateSecret(ctx context.Context, webhookID string) (string, error) {
	// Check webhook exists
	_, err := m.store.GetWebhook(ctx, webhookID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", ErrWebhookNotFound
		}
		return "", err
	}

	// Generate new secret
	secret, secretHash, secretPrefix, err := webhook.GenerateSecret()
	if err != nil {
		return "", err
	}

	updates := store.WebhookUpdates{
		SecretHash:   &secretHash,
		SecretPrefix: &secretPrefix,
	}

	if err := m.store.UpdateWebhook(ctx, webhookID, updates); err != nil {
		return "", err
	}

	m.logger.Info("webhook secret rotated",
		zap.String("webhook_id", webhookID),
		zap.String("secret_prefix", secretPrefix),
	)

	return secret, nil
}

// Dispatch sends an event to all matching webhooks.
func (m *WebhookManager) Dispatch(ctx context.Context, eventType string, resource webhook.ResourceInfo, data any, tenantID *string) error {
	// Get all active webhooks that subscribe to this event type
	webhooks, err := m.store.GetActiveWebhooksForEvent(ctx, eventType, tenantID)
	if err != nil {
		return err
	}

	if len(webhooks) == 0 {
		m.logger.Debug("no webhooks subscribed to event",
			zap.String("event_type", eventType),
		)
		return nil
	}

	// Build payload
	payload, err := webhook.BuildPayload(eventType, resource, data)
	if err != nil {
		return err
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	// Create webhook events for each matching webhook
	now := time.Now()
	for _, wh := range webhooks {
		// Check if this webhook's patterns match
		if !m.matcher.Matches(eventType, wh.Events) {
			continue
		}

		event := &store.WebhookEvent{
			ID:        id.WebhookEvent(),
			WebhookID: wh.ID,
			EventType: eventType,
			Payload:   payloadBytes,
			Status:    store.WebhookEventStatusPending,
			Attempts:  0,
			TenantID:  tenantID,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := m.store.CreateWebhookEvent(ctx, event); err != nil {
			m.logger.Error("failed to create webhook event",
				zap.String("webhook_id", wh.ID),
				zap.String("event_type", eventType),
				zap.Error(err),
			)
			continue
		}

		m.logger.Debug("webhook event created",
			zap.String("event_id", event.ID),
			zap.String("webhook_id", wh.ID),
			zap.String("event_type", eventType),
		)
	}

	return nil
}

// GetEvent retrieves a webhook event by ID.
func (m *WebhookManager) GetEvent(ctx context.Context, eventID string) (*store.WebhookEvent, error) {
	event, err := m.store.GetWebhookEvent(ctx, eventID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, ErrWebhookEventNotFound
		}
		return nil, err
	}
	return event, nil
}

// ListEvents retrieves webhook events with filters.
func (m *WebhookManager) ListEvents(ctx context.Context, opts store.ListWebhookEventsOptions) (*store.ListResult[store.WebhookEvent], error) {
	return m.store.ListWebhookEvents(ctx, opts)
}

// RetryEvent manually retries a failed webhook event.
func (m *WebhookManager) RetryEvent(ctx context.Context, eventID string) error {
	event, err := m.store.GetWebhookEvent(ctx, eventID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return ErrWebhookEventNotFound
		}
		return err
	}

	// Only allow retry for failed or exhausted events
	if event.Status != store.WebhookEventStatusFailed && event.Status != store.WebhookEventStatusExhausted {
		return errors.New("can only retry failed or exhausted events")
	}

	// Reset to pending with retry scheduled for now
	now := time.Now()
	status := store.WebhookEventStatusPending
	updates := store.WebhookEventUpdates{
		Status:      &status,
		NextRetryAt: &now,
	}

	if err := m.store.UpdateWebhookEvent(ctx, eventID, updates); err != nil {
		return err
	}

	m.logger.Info("webhook event queued for retry",
		zap.String("event_id", eventID),
	)

	return nil
}

// DeliverPendingEvents processes pending webhook events.
// This is called by the retry job.
func (m *WebhookManager) DeliverPendingEvents(ctx context.Context, limit int) (int, error) {
	events, err := m.store.GetPendingWebhookEvents(ctx, limit)
	if err != nil {
		return 0, err
	}

	delivered := 0
	for _, event := range events {
		if err := m.deliverEvent(ctx, event); err != nil {
			m.logger.Warn("failed to deliver webhook event",
				zap.String("event_id", event.ID),
				zap.Error(err),
			)
		} else {
			delivered++
		}
	}

	return delivered, nil
}

// deliverEvent attempts to deliver a single webhook event.
func (m *WebhookManager) deliverEvent(ctx context.Context, event *store.WebhookEvent) error {
	// Get webhook configuration
	wh, err := m.store.GetWebhook(ctx, event.WebhookID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Webhook was deleted, cancel the event
			status := store.WebhookEventStatusCanceled
			updates := store.WebhookEventUpdates{Status: &status}
			return m.store.UpdateWebhookEvent(ctx, event.ID, updates)
		}
		return err
	}

	// Check if webhook is active
	if !wh.IsActive {
		status := store.WebhookEventStatusCanceled
		updates := store.WebhookEventUpdates{Status: &status}
		return m.store.UpdateWebhookEvent(ctx, event.ID, updates)
	}

	// Parse headers
	var headers map[string]string
	if wh.Headers != nil {
		if err := json.Unmarshal(wh.Headers, &headers); err != nil {
			headers = nil
		}
	}

	// Build webhook info
	webhookInfo := &webhook.WebhookInfo{
		ID:             wh.ID,
		URL:            wh.URL,
		Headers:        headers,
		TimeoutSeconds: wh.TimeoutSeconds,
	}

	// Parse payload
	var payload webhook.Payload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}

	// Get the secret for signing (need to look it up since we only store hash)
	// Note: In production, you'd need a way to retrieve the actual secret
	// For now, we'll use the hash as the secret (recipients would need to handle this)
	// This is a simplification - in real implementation, you'd store an encrypted secret

	// Deliver synchronously
	result := m.dispatcher.DispatchSync(ctx, webhookInfo, &payload, event.ID, wh.SecretHash)

	// Update event status
	now := time.Now()
	attempts := event.Attempts + 1
	var status store.WebhookEventStatus
	var nextRetry *time.Time
	var lastError *string
	var lastStatusCode *int

	if result.Success {
		status = store.WebhookEventStatusDelivered
	} else {
		errorStr := ""
		if result.Error != nil {
			errorStr = result.Error.Error()
			lastError = &errorStr
		}
		if result.StatusCode > 0 {
			lastStatusCode = &result.StatusCode
		}

		if attempts >= wh.MaxRetries {
			status = store.WebhookEventStatusExhausted
		} else if webhook.ShouldRetry(result.StatusCode) || result.StatusCode == 0 {
			status = store.WebhookEventStatusPending
			next := webhook.CalculateNextRetry(attempts, time.Duration(wh.RetryDelaySeconds)*time.Second)
			nextRetry = &next
		} else {
			status = store.WebhookEventStatusFailed
		}
	}

	updates := store.WebhookEventUpdates{
		Status:         &status,
		Attempts:       &attempts,
		LastError:      lastError,
		LastStatusCode: lastStatusCode,
		NextRetryAt:    nextRetry,
		DeliveredAt:    nil,
	}

	if result.Success {
		updates.DeliveredAt = &now
	}

	if err := m.store.UpdateWebhookEvent(ctx, event.ID, updates); err != nil {
		return err
	}

	m.logger.Debug("webhook event delivery attempted",
		zap.String("event_id", event.ID),
		zap.String("webhook_id", wh.ID),
		zap.Bool("success", result.Success),
		zap.Int("status_code", result.StatusCode),
		zap.Int("attempts", attempts),
	)

	return nil
}
