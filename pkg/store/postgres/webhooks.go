package postgres

import (
	"context"
	"time"

	"github.com/chunlea/marionette/pkg/store"
	"github.com/lib/pq"
)

// Webhook operations

func (s *Store) CreateWebhook(ctx context.Context, webhook *store.Webhook) error {
	query := `
		INSERT INTO webhooks (
			id, name, url, events, secret_hash, secret_prefix,
			is_active, max_retries, retry_delay_seconds, timeout_seconds,
			headers, tenant_id, labels, annotations, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13, $14, $15, $16
		)`

	now := time.Now()
	webhook.CreatedAt = now
	webhook.UpdatedAt = now

	headers := nullableJSON(webhook.Headers)
	labels := nullableJSON(webhook.Labels)
	annotations := nullableJSON(webhook.Annotations)

	_, err := s.db(ctx).ExecContext(ctx, query,
		webhook.ID,
		webhook.Name,
		webhook.URL,
		pq.Array(webhook.Events),
		webhook.SecretHash,
		webhook.SecretPrefix,
		webhook.IsActive,
		webhook.MaxRetries,
		webhook.RetryDelaySeconds,
		webhook.TimeoutSeconds,
		headers,
		webhook.TenantID,
		labels,
		annotations,
		webhook.CreatedAt,
		webhook.UpdatedAt,
	)
	return err
}

func (s *Store) GetWebhook(ctx context.Context, id string) (*store.Webhook, error) {
	query := `
		SELECT
			id, name, url, events, secret_hash, secret_prefix,
			is_active, max_retries, retry_delay_seconds, timeout_seconds,
			headers, tenant_id, labels, annotations, created_at, updated_at
		FROM webhooks
		WHERE id = $1`

	var webhook store.Webhook
	err := s.db(ctx).QueryRowContext(ctx, query, id).Scan(
		&webhook.ID,
		&webhook.Name,
		&webhook.URL,
		pq.Array(&webhook.Events),
		&webhook.SecretHash,
		&webhook.SecretPrefix,
		&webhook.IsActive,
		&webhook.MaxRetries,
		&webhook.RetryDelaySeconds,
		&webhook.TimeoutSeconds,
		&webhook.Headers,
		&webhook.TenantID,
		&webhook.Labels,
		&webhook.Annotations,
		&webhook.CreatedAt,
		&webhook.UpdatedAt,
	)
	if err != nil {
		return nil, wrapNotFound(err, "webhook", id)
	}
	return &webhook, nil
}

func (s *Store) GetWebhookByName(ctx context.Context, name string, tenantID *string) (*store.Webhook, error) {
	query := `
		SELECT
			id, name, url, events, secret_hash, secret_prefix,
			is_active, max_retries, retry_delay_seconds, timeout_seconds,
			headers, tenant_id, labels, annotations, created_at, updated_at
		FROM webhooks
		WHERE name = $1 AND COALESCE(tenant_id, '') = COALESCE($2, '')`

	var webhook store.Webhook
	err := s.db(ctx).QueryRowContext(ctx, query, name, tenantID).Scan(
		&webhook.ID,
		&webhook.Name,
		&webhook.URL,
		pq.Array(&webhook.Events),
		&webhook.SecretHash,
		&webhook.SecretPrefix,
		&webhook.IsActive,
		&webhook.MaxRetries,
		&webhook.RetryDelaySeconds,
		&webhook.TimeoutSeconds,
		&webhook.Headers,
		&webhook.TenantID,
		&webhook.Labels,
		&webhook.Annotations,
		&webhook.CreatedAt,
		&webhook.UpdatedAt,
	)
	if err != nil {
		return nil, wrapNotFound(err, "webhook", name)
	}
	return &webhook, nil
}

func (s *Store) ListWebhooks(ctx context.Context, opts store.ListWebhooksOptions) (*store.ListResult[store.Webhook], error) {
	query := `
		SELECT
			id, name, url, events, secret_hash, secret_prefix,
			is_active, max_retries, retry_delay_seconds, timeout_seconds,
			headers, tenant_id, labels, annotations, created_at, updated_at
		FROM webhooks
		WHERE 1=1`

	countQuery := `SELECT COUNT(*) FROM webhooks WHERE 1=1`
	args := []any{}
	argIndex := 1

	if opts.TenantID != nil {
		query += ` AND tenant_id = $` + itoa(argIndex)
		countQuery += ` AND tenant_id = $` + itoa(argIndex)
		args = append(args, *opts.TenantID)
		argIndex++
	}

	if opts.ActiveOnly {
		query += ` AND is_active = TRUE`
		countQuery += ` AND is_active = TRUE`
	}

	if len(opts.Labels) > 0 {
		for k, v := range opts.Labels {
			query += ` AND labels->>$` + itoa(argIndex) + ` = $` + itoa(argIndex+1)
			countQuery += ` AND labels->>$` + itoa(argIndex) + ` = $` + itoa(argIndex+1)
			args = append(args, k, v)
			argIndex += 2
		}
	}

	// Get total count
	var total int
	if err := s.db(ctx).QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	// Add ordering and pagination
	query += ` ORDER BY created_at DESC`

	if opts.Limit > 0 {
		query += ` LIMIT $` + itoa(argIndex)
		args = append(args, opts.Limit)
		argIndex++
	}

	if opts.Offset > 0 {
		query += ` OFFSET $` + itoa(argIndex)
		args = append(args, opts.Offset)
	}

	rows, err := s.db(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []store.Webhook
	for rows.Next() {
		var webhook store.Webhook
		if err := rows.Scan(
			&webhook.ID,
			&webhook.Name,
			&webhook.URL,
			pq.Array(&webhook.Events),
			&webhook.SecretHash,
			&webhook.SecretPrefix,
			&webhook.IsActive,
			&webhook.MaxRetries,
			&webhook.RetryDelaySeconds,
			&webhook.TimeoutSeconds,
			&webhook.Headers,
			&webhook.TenantID,
			&webhook.Labels,
			&webhook.Annotations,
			&webhook.CreatedAt,
			&webhook.UpdatedAt,
		); err != nil {
			return nil, err
		}
		webhooks = append(webhooks, webhook)
	}

	return &store.ListResult[store.Webhook]{
		Items: webhooks,
		Total: total,
	}, rows.Err()
}

func (s *Store) UpdateWebhook(ctx context.Context, id string, updates store.WebhookUpdates) error {
	query := `UPDATE webhooks SET updated_at = $2`
	args := []any{id, time.Now()}
	argIndex := 3

	if updates.Name != nil {
		query += `, name = $` + itoa(argIndex)
		args = append(args, *updates.Name)
		argIndex++
	}

	if updates.URL != nil {
		query += `, url = $` + itoa(argIndex)
		args = append(args, *updates.URL)
		argIndex++
	}

	if updates.Events != nil {
		query += `, events = $` + itoa(argIndex)
		args = append(args, pq.Array(updates.Events))
		argIndex++
	}

	if updates.SecretHash != nil {
		query += `, secret_hash = $` + itoa(argIndex)
		args = append(args, *updates.SecretHash)
		argIndex++
	}

	if updates.SecretPrefix != nil {
		query += `, secret_prefix = $` + itoa(argIndex)
		args = append(args, *updates.SecretPrefix)
		argIndex++
	}

	if updates.IsActive != nil {
		query += `, is_active = $` + itoa(argIndex)
		args = append(args, *updates.IsActive)
		argIndex++
	}

	if updates.MaxRetries != nil {
		query += `, max_retries = $` + itoa(argIndex)
		args = append(args, *updates.MaxRetries)
		argIndex++
	}

	if updates.RetryDelaySeconds != nil {
		query += `, retry_delay_seconds = $` + itoa(argIndex)
		args = append(args, *updates.RetryDelaySeconds)
		argIndex++
	}

	if updates.TimeoutSeconds != nil {
		query += `, timeout_seconds = $` + itoa(argIndex)
		args = append(args, *updates.TimeoutSeconds)
		argIndex++
	}

	if updates.Headers != nil {
		query += `, headers = $` + itoa(argIndex)
		args = append(args, updates.Headers)
		argIndex++
	}

	if updates.Labels != nil {
		query += `, labels = $` + itoa(argIndex)
		args = append(args, updates.Labels)
		argIndex++
	}

	if updates.Annotations != nil {
		query += `, annotations = $` + itoa(argIndex)
		args = append(args, updates.Annotations)
		argIndex++
	}

	query += ` WHERE id = $1`

	result, err := s.db(ctx).ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	return checkRowsAffected(result, "webhook", id)
}

func (s *Store) DeleteWebhook(ctx context.Context, id string) error {
	query := `DELETE FROM webhooks WHERE id = $1`
	result, err := s.db(ctx).ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	return checkRowsAffected(result, "webhook", id)
}

func (s *Store) GetActiveWebhooksForEvent(ctx context.Context, eventType string, tenantID *string) ([]*store.Webhook, error) {
	// Match webhooks where:
	// 1. Exact match: events contains eventType
	// 2. Wildcard match: events contains pattern like "task.*" that matches eventType
	query := `
		SELECT
			id, name, url, events, secret_hash, secret_prefix,
			is_active, max_retries, retry_delay_seconds, timeout_seconds,
			headers, tenant_id, labels, annotations, created_at, updated_at
		FROM webhooks
		WHERE is_active = TRUE
		AND COALESCE(tenant_id, '') = COALESCE($2, '')
		AND (
			$1 = ANY(events)
			OR EXISTS (
				SELECT 1 FROM unnest(events) AS pattern
				WHERE $1 LIKE REPLACE(REPLACE(pattern, '.', '\.'), '*', '%')
			)
		)`

	rows, err := s.db(ctx).QueryContext(ctx, query, eventType, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []*store.Webhook
	for rows.Next() {
		var webhook store.Webhook
		if err := rows.Scan(
			&webhook.ID,
			&webhook.Name,
			&webhook.URL,
			pq.Array(&webhook.Events),
			&webhook.SecretHash,
			&webhook.SecretPrefix,
			&webhook.IsActive,
			&webhook.MaxRetries,
			&webhook.RetryDelaySeconds,
			&webhook.TimeoutSeconds,
			&webhook.Headers,
			&webhook.TenantID,
			&webhook.Labels,
			&webhook.Annotations,
			&webhook.CreatedAt,
			&webhook.UpdatedAt,
		); err != nil {
			return nil, err
		}
		webhooks = append(webhooks, &webhook)
	}

	return webhooks, rows.Err()
}

// WebhookEvent operations

func (s *Store) CreateWebhookEvent(ctx context.Context, event *store.WebhookEvent) error {
	query := `
		INSERT INTO webhook_events (
			id, webhook_id, event_type, payload, status, attempts,
			last_error, last_status_code, next_retry_at, delivered_at,
			tenant_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10,
			$11, $12, $13
		)`

	now := time.Now()
	event.CreatedAt = now
	event.UpdatedAt = now

	_, err := s.db(ctx).ExecContext(ctx, query,
		event.ID,
		event.WebhookID,
		event.EventType,
		event.Payload,
		event.Status,
		event.Attempts,
		event.LastError,
		event.LastStatusCode,
		event.NextRetryAt,
		event.DeliveredAt,
		event.TenantID,
		event.CreatedAt,
		event.UpdatedAt,
	)
	return err
}

func (s *Store) GetWebhookEvent(ctx context.Context, id string) (*store.WebhookEvent, error) {
	query := `
		SELECT
			id, webhook_id, event_type, payload, status, attempts,
			last_error, last_status_code, next_retry_at, delivered_at,
			tenant_id, created_at, updated_at
		FROM webhook_events
		WHERE id = $1`

	var event store.WebhookEvent
	err := s.db(ctx).QueryRowContext(ctx, query, id).Scan(
		&event.ID,
		&event.WebhookID,
		&event.EventType,
		&event.Payload,
		&event.Status,
		&event.Attempts,
		&event.LastError,
		&event.LastStatusCode,
		&event.NextRetryAt,
		&event.DeliveredAt,
		&event.TenantID,
		&event.CreatedAt,
		&event.UpdatedAt,
	)
	if err != nil {
		return nil, wrapNotFound(err, "webhook_event", id)
	}
	return &event, nil
}

func (s *Store) ListWebhookEvents(ctx context.Context, opts store.ListWebhookEventsOptions) (*store.ListResult[store.WebhookEvent], error) {
	query := `
		SELECT
			id, webhook_id, event_type, payload, status, attempts,
			last_error, last_status_code, next_retry_at, delivered_at,
			tenant_id, created_at, updated_at
		FROM webhook_events
		WHERE webhook_id = $1`

	countQuery := `SELECT COUNT(*) FROM webhook_events WHERE webhook_id = $1`
	args := []any{opts.WebhookID}
	argIndex := 2

	if opts.TenantID != nil {
		query += ` AND tenant_id = $` + itoa(argIndex)
		countQuery += ` AND tenant_id = $` + itoa(argIndex)
		args = append(args, *opts.TenantID)
		argIndex++
	}

	if opts.Status != nil {
		query += ` AND status = $` + itoa(argIndex)
		countQuery += ` AND status = $` + itoa(argIndex)
		args = append(args, *opts.Status)
		argIndex++
	}

	if opts.EventType != nil {
		query += ` AND event_type = $` + itoa(argIndex)
		countQuery += ` AND event_type = $` + itoa(argIndex)
		args = append(args, *opts.EventType)
		argIndex++
	}

	// Get total count
	var total int
	if err := s.db(ctx).QueryRowContext(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	// Add ordering and pagination
	query += ` ORDER BY created_at DESC`

	if opts.Limit > 0 {
		query += ` LIMIT $` + itoa(argIndex)
		args = append(args, opts.Limit)
		argIndex++
	}

	if opts.Offset > 0 {
		query += ` OFFSET $` + itoa(argIndex)
		args = append(args, opts.Offset)
	}

	rows, err := s.db(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []store.WebhookEvent
	for rows.Next() {
		var event store.WebhookEvent
		if err := rows.Scan(
			&event.ID,
			&event.WebhookID,
			&event.EventType,
			&event.Payload,
			&event.Status,
			&event.Attempts,
			&event.LastError,
			&event.LastStatusCode,
			&event.NextRetryAt,
			&event.DeliveredAt,
			&event.TenantID,
			&event.CreatedAt,
			&event.UpdatedAt,
		); err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return &store.ListResult[store.WebhookEvent]{
		Items: events,
		Total: total,
	}, rows.Err()
}

func (s *Store) UpdateWebhookEvent(ctx context.Context, id string, updates store.WebhookEventUpdates) error {
	query := `UPDATE webhook_events SET updated_at = $2`
	args := []any{id, time.Now()}
	argIndex := 3

	if updates.Status != nil {
		query += `, status = $` + itoa(argIndex)
		args = append(args, *updates.Status)
		argIndex++
	}

	if updates.Attempts != nil {
		query += `, attempts = $` + itoa(argIndex)
		args = append(args, *updates.Attempts)
		argIndex++
	}

	if updates.LastError != nil {
		query += `, last_error = $` + itoa(argIndex)
		args = append(args, *updates.LastError)
		argIndex++
	}

	if updates.LastStatusCode != nil {
		query += `, last_status_code = $` + itoa(argIndex)
		args = append(args, *updates.LastStatusCode)
		argIndex++
	}

	if updates.NextRetryAt != nil {
		query += `, next_retry_at = $` + itoa(argIndex)
		args = append(args, *updates.NextRetryAt)
		argIndex++
	}

	if updates.DeliveredAt != nil {
		query += `, delivered_at = $` + itoa(argIndex)
		args = append(args, *updates.DeliveredAt)
		argIndex++
	}

	query += ` WHERE id = $1`

	result, err := s.db(ctx).ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}

	return checkRowsAffected(result, "webhook_event", id)
}

func (s *Store) GetPendingWebhookEvents(ctx context.Context, limit int) ([]*store.WebhookEvent, error) {
	query := `
		SELECT
			id, webhook_id, event_type, payload, status, attempts,
			last_error, last_status_code, next_retry_at, delivered_at,
			tenant_id, created_at, updated_at
		FROM webhook_events
		WHERE status IN ('pending', 'failed')
		AND (next_retry_at IS NULL OR next_retry_at <= NOW())
		ORDER BY created_at ASC
		LIMIT $1`

	rows, err := s.db(ctx).QueryContext(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*store.WebhookEvent
	for rows.Next() {
		var event store.WebhookEvent
		if err := rows.Scan(
			&event.ID,
			&event.WebhookID,
			&event.EventType,
			&event.Payload,
			&event.Status,
			&event.Attempts,
			&event.LastError,
			&event.LastStatusCode,
			&event.NextRetryAt,
			&event.DeliveredAt,
			&event.TenantID,
			&event.CreatedAt,
			&event.UpdatedAt,
		); err != nil {
			return nil, err
		}
		events = append(events, &event)
	}

	return events, rows.Err()
}

func (s *Store) CancelWebhookEventsByWebhook(ctx context.Context, webhookID string) error {
	query := `
		UPDATE webhook_events
		SET status = 'canceled', updated_at = $2
		WHERE webhook_id = $1
		AND status IN ('pending', 'failed')`

	_, err := s.db(ctx).ExecContext(ctx, query, webhookID, time.Now())
	return err
}
