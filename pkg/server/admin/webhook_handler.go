package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/chunlea/marionette/pkg/server/admin/admintypes"
	"github.com/chunlea/marionette/pkg/store"
)

// Webhook-related sentinel errors for admin package.
var (
	ErrWebhookNameExists    = errors.New("webhook name already exists")
	ErrInvalidEventPattern  = errors.New("invalid event pattern")
	ErrWebhookNotFound      = errors.New("webhook not found")
	ErrWebhookEventNotFound = errors.New("webhook event not found")
)

// CreateWebhookRequest is the request body for creating a webhook.
type CreateWebhookRequest struct {
	Name              string            `json:"name"`
	URL               string            `json:"url"`
	Events            []string          `json:"events"`
	MaxRetries        *int              `json:"max_retries,omitempty"`
	RetryDelaySeconds *int              `json:"retry_delay_seconds,omitempty"`
	TimeoutSeconds    *int              `json:"timeout_seconds,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
}

// UpdateWebhookRequest is the request body for updating a webhook.
type UpdateWebhookRequest struct {
	Name              *string           `json:"name,omitempty"`
	URL               *string           `json:"url,omitempty"`
	Events            []string          `json:"events,omitempty"`
	IsActive          *bool             `json:"is_active,omitempty"`
	MaxRetries        *int              `json:"max_retries,omitempty"`
	RetryDelaySeconds *int              `json:"retry_delay_seconds,omitempty"`
	TimeoutSeconds    *int              `json:"timeout_seconds,omitempty"`
	Headers           map[string]string `json:"headers,omitempty"`
	Labels            map[string]string `json:"labels,omitempty"`
	Annotations       map[string]string `json:"annotations,omitempty"`
}

func (s *Server) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "Webhook service not configured")
		return
	}

	var req CreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	if req.Name == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "name is required")
		return
	}
	if req.URL == "" {
		WriteError(w, http.StatusBadRequest, "validation_error", "url is required")
		return
	}
	if len(req.Events) == 0 {
		WriteError(w, http.StatusBadRequest, "validation_error", "at least one event is required")
		return
	}

	input := &CreateWebhookInput{
		Name:              req.Name,
		URL:               req.URL,
		Events:            req.Events,
		MaxRetries:        req.MaxRetries,
		RetryDelaySeconds: req.RetryDelaySeconds,
		TimeoutSeconds:    req.TimeoutSeconds,
		Headers:           req.Headers,
		Labels:            req.Labels,
		Annotations:       req.Annotations,
	}

	webhook, secret, err := s.webhooks.Create(r.Context(), input)
	if err != nil {
		if errors.Is(err, ErrWebhookNameExists) {
			WriteError(w, http.StatusConflict, "name_exists", "webhook with this name already exists")
			return
		}
		if errors.Is(err, ErrInvalidEventPattern) {
			WriteError(w, http.StatusBadRequest, "validation_error", "invalid event pattern")
			return
		}
		s.logger.Error("failed to create webhook", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to create webhook")
		return
	}

	WriteJSON(w, http.StatusCreated, admintypes.CreatedWebhook{
		Webhook: toWebhookResponse(webhook),
		Secret:  secret,
	})
}

func (s *Server) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "Webhook service not configured")
		return
	}

	opts := store.ListWebhooksOptions{
		Labels: parseLabels(r.URL.Query().Get("labels")),
		Limit:  parseLimit(r.URL.Query().Get("limit")),
	}

	// Parse is_active filter
	if active := r.URL.Query().Get("is_active"); active == "true" {
		opts.ActiveOnly = true
	}

	result, err := s.webhooks.List(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list webhooks", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list webhooks")
		return
	}

	WriteJSON(w, http.StatusOK, toStoreListResponse(result, toWebhookResponse))
}

func (s *Server) handleGetWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "Webhook service not configured")
		return
	}

	webhookID := chi.URLParam(r, "webhookID")
	if webhookID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "webhook ID is required")
		return
	}

	webhook, err := s.webhooks.Get(r.Context(), webhookID)
	if err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			WriteError(w, http.StatusNotFound, "not_found", "webhook not found")
			return
		}
		s.logger.Error("failed to get webhook", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get webhook")
		return
	}

	WriteJSON(w, http.StatusOK, toWebhookResponse(webhook))
}

func (s *Server) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "Webhook service not configured")
		return
	}

	webhookID := chi.URLParam(r, "webhookID")
	if webhookID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "webhook ID is required")
		return
	}

	var req UpdateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return
	}

	input := &UpdateWebhookInput{
		Name:              req.Name,
		URL:               req.URL,
		Events:            req.Events,
		IsActive:          req.IsActive,
		MaxRetries:        req.MaxRetries,
		RetryDelaySeconds: req.RetryDelaySeconds,
		TimeoutSeconds:    req.TimeoutSeconds,
		Headers:           req.Headers,
		Labels:            req.Labels,
		Annotations:       req.Annotations,
	}

	if err := s.webhooks.Update(r.Context(), webhookID, input); err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			WriteError(w, http.StatusNotFound, "not_found", "webhook not found")
			return
		}
		if errors.Is(err, ErrInvalidEventPattern) {
			WriteError(w, http.StatusBadRequest, "validation_error", "invalid event pattern")
			return
		}
		s.logger.Error("failed to update webhook", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to update webhook")
		return
	}

	// Get updated webhook to return
	webhook, err := s.webhooks.Get(r.Context(), webhookID)
	if err != nil {
		s.logger.Error("failed to get updated webhook", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "webhook updated but failed to retrieve")
		return
	}

	WriteJSON(w, http.StatusOK, toWebhookResponse(webhook))
}

func (s *Server) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "Webhook service not configured")
		return
	}

	webhookID := chi.URLParam(r, "webhookID")
	if webhookID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "webhook ID is required")
		return
	}

	if err := s.webhooks.Delete(r.Context(), webhookID); err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			WriteError(w, http.StatusNotFound, "not_found", "webhook not found")
			return
		}
		s.logger.Error("failed to delete webhook", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to delete webhook")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleRotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "Webhook service not configured")
		return
	}

	webhookID := chi.URLParam(r, "webhookID")
	if webhookID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "webhook ID is required")
		return
	}

	secret, err := s.webhooks.RotateSecret(r.Context(), webhookID)
	if err != nil {
		if errors.Is(err, ErrWebhookNotFound) {
			WriteError(w, http.StatusNotFound, "not_found", "webhook not found")
			return
		}
		s.logger.Error("failed to rotate webhook secret", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to rotate webhook secret")
		return
	}

	// Get updated webhook to get the new prefix
	webhook, err := s.webhooks.Get(r.Context(), webhookID)
	if err != nil {
		s.logger.Error("failed to get webhook after rotation", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "secret rotated but failed to retrieve prefix")
		return
	}

	WriteJSON(w, http.StatusOK, admintypes.RotatedWebhookSecret{
		Secret:       secret,
		SecretPrefix: webhook.SecretPrefix,
	})
}

// Webhook Events handlers

func (s *Server) handleListWebhookEvents(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "Webhook service not configured")
		return
	}

	opts := store.ListWebhookEventsOptions{
		Limit: parseLimit(r.URL.Query().Get("limit")),
	}

	// Parse filters
	if webhookID := r.URL.Query().Get("webhook_id"); webhookID != "" {
		opts.WebhookID = webhookID
	}
	if eventType := r.URL.Query().Get("event_type"); eventType != "" {
		opts.EventType = &eventType
	}
	if status := r.URL.Query().Get("status"); status != "" {
		s := store.WebhookEventStatus(status)
		opts.Status = &s
	}

	result, err := s.webhooks.ListEvents(r.Context(), opts)
	if err != nil {
		s.logger.Error("failed to list webhook events", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to list webhook events")
		return
	}

	WriteJSON(w, http.StatusOK, toStoreListResponse(result, toWebhookEventResponse))
}

func (s *Server) handleGetWebhookEvent(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "Webhook service not configured")
		return
	}

	eventID := chi.URLParam(r, "eventID")
	if eventID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "event ID is required")
		return
	}

	event, err := s.webhooks.GetEvent(r.Context(), eventID)
	if err != nil {
		if errors.Is(err, ErrWebhookEventNotFound) {
			WriteError(w, http.StatusNotFound, "not_found", "webhook event not found")
			return
		}
		s.logger.Error("failed to get webhook event", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", "failed to get webhook event")
		return
	}

	WriteJSON(w, http.StatusOK, toWebhookEventResponse(event))
}

func (s *Server) handleRetryWebhookEvent(w http.ResponseWriter, r *http.Request) {
	if s.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "not_implemented", "Webhook service not configured")
		return
	}

	eventID := chi.URLParam(r, "eventID")
	if eventID == "" {
		WriteError(w, http.StatusBadRequest, "invalid_request", "event ID is required")
		return
	}

	if err := s.webhooks.RetryEvent(r.Context(), eventID); err != nil {
		if errors.Is(err, ErrWebhookEventNotFound) {
			WriteError(w, http.StatusNotFound, "not_found", "webhook event not found")
			return
		}
		s.logger.Error("failed to retry webhook event", logError(err))
		WriteError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, admintypes.Accepted{Status: "queued"})
}
