package admin

import (
	"context"
	"errors"

	"github.com/chunlea/marionette/pkg/server/core"
	"github.com/chunlea/marionette/pkg/store"
)

// WebhookAdapter adapts core.WebhookManager to the admin.WebhookService interface.
type WebhookAdapter struct {
	manager *core.WebhookManager
}

// NewWebhookAdapter creates a new WebhookAdapter.
func NewWebhookAdapter(manager *core.WebhookManager) *WebhookAdapter {
	return &WebhookAdapter{manager: manager}
}

// Create creates a new webhook and returns the webhook and plaintext secret.
func (a *WebhookAdapter) Create(ctx context.Context, input *CreateWebhookInput) (*store.Webhook, string, error) {
	coreInput := &core.CreateWebhookInput{
		Name:              input.Name,
		URL:               input.URL,
		Events:            input.Events,
		MaxRetries:        input.MaxRetries,
		RetryDelaySeconds: input.RetryDelaySeconds,
		TimeoutSeconds:    input.TimeoutSeconds,
		Headers:           input.Headers,
		TenantID:          input.TenantID,
		Labels:            input.Labels,
		Annotations:       input.Annotations,
	}

	wh, secret, err := a.manager.Create(ctx, coreInput)
	if err != nil {
		return nil, "", translateCoreError(err)
	}
	return wh, secret, nil
}

// Get retrieves a webhook by ID.
func (a *WebhookAdapter) Get(ctx context.Context, webhookID string) (*store.Webhook, error) {
	wh, err := a.manager.Get(ctx, webhookID)
	if err != nil {
		return nil, translateCoreError(err)
	}
	return wh, nil
}

// List returns webhooks matching the given options.
func (a *WebhookAdapter) List(ctx context.Context, opts store.ListWebhooksOptions) (*store.ListResult[store.Webhook], error) {
	return a.manager.List(ctx, opts)
}

// Update updates a webhook.
func (a *WebhookAdapter) Update(ctx context.Context, webhookID string, input *UpdateWebhookInput) error {
	coreInput := &core.UpdateWebhookInput{
		Name:              input.Name,
		URL:               input.URL,
		Events:            input.Events,
		IsActive:          input.IsActive,
		MaxRetries:        input.MaxRetries,
		RetryDelaySeconds: input.RetryDelaySeconds,
		TimeoutSeconds:    input.TimeoutSeconds,
		Headers:           input.Headers,
		Labels:            input.Labels,
		Annotations:       input.Annotations,
	}

	if err := a.manager.Update(ctx, webhookID, coreInput); err != nil {
		return translateCoreError(err)
	}
	return nil
}

// Delete deletes a webhook.
func (a *WebhookAdapter) Delete(ctx context.Context, webhookID string) error {
	if err := a.manager.Delete(ctx, webhookID); err != nil {
		return translateCoreError(err)
	}
	return nil
}

// RotateSecret generates a new secret for a webhook.
func (a *WebhookAdapter) RotateSecret(ctx context.Context, webhookID string) (string, error) {
	secret, err := a.manager.RotateSecret(ctx, webhookID)
	if err != nil {
		return "", translateCoreError(err)
	}
	return secret, nil
}

// GetEvent retrieves a webhook event by ID.
func (a *WebhookAdapter) GetEvent(ctx context.Context, eventID string) (*store.WebhookEvent, error) {
	event, err := a.manager.GetEvent(ctx, eventID)
	if err != nil {
		return nil, translateCoreError(err)
	}
	return event, nil
}

// ListEvents returns webhook events matching the given options.
func (a *WebhookAdapter) ListEvents(ctx context.Context, opts store.ListWebhookEventsOptions) (*store.ListResult[store.WebhookEvent], error) {
	return a.manager.ListEvents(ctx, opts)
}

// RetryEvent manually retries a failed webhook event.
func (a *WebhookAdapter) RetryEvent(ctx context.Context, eventID string) error {
	if err := a.manager.RetryEvent(ctx, eventID); err != nil {
		return translateCoreError(err)
	}
	return nil
}

// translateCoreError translates core package errors to admin package errors.
func translateCoreError(err error) error {
	if err == nil {
		return nil
	}

	switch {
	case errors.Is(err, core.ErrWebhookNotFound):
		return ErrWebhookNotFound
	case errors.Is(err, core.ErrWebhookNameExists):
		return ErrWebhookNameExists
	case errors.Is(err, core.ErrInvalidEventPattern):
		return ErrInvalidEventPattern
	case errors.Is(err, core.ErrWebhookEventNotFound):
		return ErrWebhookEventNotFound
	default:
		return err
	}
}

// Ensure WebhookAdapter implements WebhookService.
var _ WebhookService = (*WebhookAdapter)(nil)
