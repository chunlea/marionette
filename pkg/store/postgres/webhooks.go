package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/chunlea/marionette/pkg/id"
	"github.com/chunlea/marionette/pkg/store"
)

// Webhook column list for SELECT queries.
const webhookColumns = `id, name, url, events, secret_hash, secret_prefix,
	is_active, max_retries, retry_delay_seconds, timeout_seconds,
	headers, tenant_id, labels, annotations, created_at, updated_at`

// WebhookEvent column list for SELECT queries.
const webhookEventColumns = `id, webhook_id, event_type, payload, status,
	attempts, last_error, last_status_code, next_retry_at, delivered_at,
	tenant_id, created_at, updated_at`

// Webhook operations

func (s *Store) CreateWebhook(ctx context.Context, webhook *store.Webhook) error {
	return createWebhook(ctx, s.pool, webhook)
}

func (t *Tx) CreateWebhook(ctx context.Context, webhook *store.Webhook) error {
	return createWebhook(ctx, t.tx, webhook)
}

func createWebhook(ctx context.Context, q querier, webhook *store.Webhook) error {
	if webhook.ID == "" {
		webhook.ID = id.Webhook()
	}

	query := `
		INSERT INTO webhooks (
			id, name, url, events, secret_hash, secret_prefix,
			is_active, max_retries, retry_delay_seconds, timeout_seconds,
			headers, tenant_id, labels, annotations, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		webhook.ID, webhook.Name, webhook.URL, webhook.Events,
		webhook.SecretHash, webhook.SecretPrefix, webhook.IsActive,
		webhook.MaxRetries, webhook.RetryDelaySeconds, webhook.TimeoutSeconds,
		emptyJSONObject(webhook.Headers), webhook.TenantID,
		emptyJSONObject(webhook.Labels), emptyJSONObject(webhook.Annotations),
	).Scan(&webhook.CreatedAt, &webhook.UpdatedAt)

	if err != nil {
		return handlePgError(err, "webhook", webhook.ID)
	}
	return nil
}

func (s *Store) GetWebhook(ctx context.Context, webhookID string) (*store.Webhook, error) {
	return getWebhook(ctx, s.pool, webhookID)
}

func (t *Tx) GetWebhook(ctx context.Context, webhookID string) (*store.Webhook, error) {
	return getWebhook(ctx, t.tx, webhookID)
}

func getWebhook(ctx context.Context, q querier, webhookID string) (*store.Webhook, error) {
	query := fmt.Sprintf(`SELECT %s FROM webhooks WHERE id = $1`, webhookColumns)
	row := q.QueryRow(ctx, query, webhookID)
	return scanWebhook(row, webhookID)
}

func (s *Store) GetWebhookByName(ctx context.Context, name string, tenantID *string) (*store.Webhook, error) {
	return getWebhookByName(ctx, s.pool, name, tenantID)
}

func (t *Tx) GetWebhookByName(ctx context.Context, name string, tenantID *string) (*store.Webhook, error) {
	return getWebhookByName(ctx, t.tx, name, tenantID)
}

func getWebhookByName(ctx context.Context, q querier, name string, tenantID *string) (*store.Webhook, error) {
	query := fmt.Sprintf(`SELECT %s FROM webhooks WHERE name = $1 AND COALESCE(tenant_id, '') = COALESCE($2, '')`, webhookColumns)
	row := q.QueryRow(ctx, query, name, tenantID)
	return scanWebhook(row, name)
}

func (s *Store) ListWebhooks(ctx context.Context, opts store.ListWebhooksOptions) (*store.ListResult[store.Webhook], error) {
	return listWebhooks(ctx, s.pool, opts)
}

func (t *Tx) ListWebhooks(ctx context.Context, opts store.ListWebhooksOptions) (*store.ListResult[store.Webhook], error) {
	return listWebhooks(ctx, t.tx, opts)
}

func listWebhooks(ctx context.Context, q querier, opts store.ListWebhooksOptions) (*store.ListResult[store.Webhook], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argNum))
		args = append(args, *opts.TenantID)
		argNum++
	}

	if opts.ActiveOnly {
		conditions = append(conditions, "is_active = true")
	}

	if len(opts.Labels) > 0 {
		for key, value := range opts.Labels {
			conditions = append(conditions, fmt.Sprintf("labels->>'%s' = $%d", key, argNum))
			args = append(args, value)
			argNum++
		}
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM webhooks %s", where)
	var total int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	// Get results
	limit := 100
	if opts.Limit > 0 && opts.Limit < limit {
		limit = opts.Limit
	}
	offset := opts.Offset

	query := fmt.Sprintf(`SELECT %s FROM webhooks %s ORDER BY created_at DESC LIMIT %d OFFSET %d`,
		webhookColumns, where, limit, offset)

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.Webhook
	for rows.Next() {
		webhook, err := scanWebhookRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, webhook)
	}

	return &store.ListResult[store.Webhook]{
		Items:      items,
		TotalCount: total,
	}, nil
}

func (s *Store) UpdateWebhook(ctx context.Context, webhookID string, updates store.WebhookUpdates) error {
	return updateWebhook(ctx, s.pool, webhookID, updates)
}

func (t *Tx) UpdateWebhook(ctx context.Context, webhookID string, updates store.WebhookUpdates) error {
	return updateWebhook(ctx, t.tx, webhookID, updates)
}

func updateWebhook(ctx context.Context, q querier, webhookID string, updates store.WebhookUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("name = $%d", argNum))
		args = append(args, *updates.Name)
		argNum++
	}
	if updates.URL != nil {
		setClauses = append(setClauses, fmt.Sprintf("url = $%d", argNum))
		args = append(args, *updates.URL)
		argNum++
	}
	if updates.Events != nil {
		setClauses = append(setClauses, fmt.Sprintf("events = $%d", argNum))
		args = append(args, updates.Events)
		argNum++
	}
	if updates.SecretHash != nil {
		setClauses = append(setClauses, fmt.Sprintf("secret_hash = $%d", argNum))
		args = append(args, *updates.SecretHash)
		argNum++
	}
	if updates.SecretPrefix != nil {
		setClauses = append(setClauses, fmt.Sprintf("secret_prefix = $%d", argNum))
		args = append(args, *updates.SecretPrefix)
		argNum++
	}
	if updates.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("is_active = $%d", argNum))
		args = append(args, *updates.IsActive)
		argNum++
	}
	if updates.MaxRetries != nil {
		setClauses = append(setClauses, fmt.Sprintf("max_retries = $%d", argNum))
		args = append(args, *updates.MaxRetries)
		argNum++
	}
	if updates.RetryDelaySeconds != nil {
		setClauses = append(setClauses, fmt.Sprintf("retry_delay_seconds = $%d", argNum))
		args = append(args, *updates.RetryDelaySeconds)
		argNum++
	}
	if updates.TimeoutSeconds != nil {
		setClauses = append(setClauses, fmt.Sprintf("timeout_seconds = $%d", argNum))
		args = append(args, *updates.TimeoutSeconds)
		argNum++
	}
	if updates.Headers != nil {
		setClauses = append(setClauses, fmt.Sprintf("headers = $%d", argNum))
		args = append(args, updates.Headers)
		argNum++
	}
	if updates.Labels != nil {
		setClauses = append(setClauses, fmt.Sprintf("labels = $%d", argNum))
		args = append(args, updates.Labels)
		argNum++
	}
	if updates.Annotations != nil {
		setClauses = append(setClauses, fmt.Sprintf("annotations = $%d", argNum))
		args = append(args, updates.Annotations)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, webhookID)

	query := fmt.Sprintf("UPDATE webhooks SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), argNum)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "webhook", webhookID)
	}

	if result.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) DeleteWebhook(ctx context.Context, webhookID string) error {
	return deleteWebhook(ctx, s.pool, webhookID)
}

func (t *Tx) DeleteWebhook(ctx context.Context, webhookID string) error {
	return deleteWebhook(ctx, t.tx, webhookID)
}

func deleteWebhook(ctx context.Context, q querier, webhookID string) error {
	query := `DELETE FROM webhooks WHERE id = $1`
	result, err := q.Exec(ctx, query, webhookID)
	if err != nil {
		return handlePgError(err, "webhook", webhookID)
	}
	if result.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetActiveWebhooksForEvent(ctx context.Context, eventType string, tenantID *string) ([]*store.Webhook, error) {
	return getActiveWebhooksForEvent(ctx, s.pool, eventType, tenantID)
}

func (t *Tx) GetActiveWebhooksForEvent(ctx context.Context, eventType string, tenantID *string) ([]*store.Webhook, error) {
	return getActiveWebhooksForEvent(ctx, t.tx, eventType, tenantID)
}

func getActiveWebhooksForEvent(_ context.Context, q querier, eventType string, tenantID *string) ([]*store.Webhook, error) {
	// This query finds webhooks that:
	// 1. Are active
	// 2. Match the tenant (or are global if tenantID is nil)
	// 3. Have an event pattern that matches the event type
	// Pattern matching is done at application layer since SQL wildcards are complex
	query := fmt.Sprintf(`SELECT %s FROM webhooks WHERE is_active = true AND COALESCE(tenant_id, '') = COALESCE($1, '')`, webhookColumns)

	// Note: Event pattern matching (wildcards like task.*) should be done in application layer
	// Here we just get all active webhooks for the tenant and let the WebhookManager filter

	ctx := context.Background()
	rows, err := q.Query(ctx, query, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var webhooks []*store.Webhook
	for rows.Next() {
		webhook, err := scanWebhookRows(rows)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, webhook)
	}

	return webhooks, nil
}

// WebhookEvent operations

func (s *Store) CreateWebhookEvent(ctx context.Context, event *store.WebhookEvent) error {
	return createWebhookEvent(ctx, s.pool, event)
}

func (t *Tx) CreateWebhookEvent(ctx context.Context, event *store.WebhookEvent) error {
	return createWebhookEvent(ctx, t.tx, event)
}

func createWebhookEvent(ctx context.Context, q querier, event *store.WebhookEvent) error {
	if event.ID == "" {
		event.ID = id.WebhookEvent()
	}

	query := `
		INSERT INTO webhook_events (
			id, webhook_id, event_type, payload, status,
			attempts, last_error, last_status_code, next_retry_at, delivered_at,
			tenant_id, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW()
		)
		RETURNING created_at, updated_at`

	err := q.QueryRow(ctx, query,
		event.ID, event.WebhookID, event.EventType, event.Payload, event.Status,
		event.Attempts, event.LastError, event.LastStatusCode, event.NextRetryAt, event.DeliveredAt,
		event.TenantID,
	).Scan(&event.CreatedAt, &event.UpdatedAt)

	if err != nil {
		return handlePgError(err, "webhook_event", event.ID)
	}
	return nil
}

func (s *Store) GetWebhookEvent(ctx context.Context, eventID string) (*store.WebhookEvent, error) {
	return getWebhookEvent(ctx, s.pool, eventID)
}

func (t *Tx) GetWebhookEvent(ctx context.Context, eventID string) (*store.WebhookEvent, error) {
	return getWebhookEvent(ctx, t.tx, eventID)
}

func getWebhookEvent(ctx context.Context, q querier, eventID string) (*store.WebhookEvent, error) {
	query := fmt.Sprintf(`SELECT %s FROM webhook_events WHERE id = $1`, webhookEventColumns)
	row := q.QueryRow(ctx, query, eventID)
	return scanWebhookEvent(row, eventID)
}

func (s *Store) ListWebhookEvents(ctx context.Context, opts store.ListWebhookEventsOptions) (*store.ListResult[store.WebhookEvent], error) {
	return listWebhookEvents(ctx, s.pool, opts)
}

func (t *Tx) ListWebhookEvents(ctx context.Context, opts store.ListWebhookEventsOptions) (*store.ListResult[store.WebhookEvent], error) {
	return listWebhookEvents(ctx, t.tx, opts)
}

func listWebhookEvents(ctx context.Context, q querier, opts store.ListWebhookEventsOptions) (*store.ListResult[store.WebhookEvent], error) {
	var conditions []string
	var args []any
	argNum := 1

	if opts.WebhookID != "" {
		conditions = append(conditions, fmt.Sprintf("webhook_id = $%d", argNum))
		args = append(args, opts.WebhookID)
		argNum++
	}
	if opts.TenantID != nil {
		conditions = append(conditions, fmt.Sprintf("tenant_id = $%d", argNum))
		args = append(args, *opts.TenantID)
		argNum++
	}
	if opts.Status != nil {
		conditions = append(conditions, fmt.Sprintf("status = $%d", argNum))
		args = append(args, *opts.Status)
		argNum++
	}
	if opts.EventType != nil {
		conditions = append(conditions, fmt.Sprintf("event_type = $%d", argNum))
		args = append(args, *opts.EventType)
	}

	where := ""
	if len(conditions) > 0 {
		where = "WHERE " + strings.Join(conditions, " AND ")
	}

	// Get total count
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM webhook_events %s", where)
	var total int64
	if err := q.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, err
	}

	// Get results
	limit := 100
	if opts.Limit > 0 && opts.Limit < limit {
		limit = opts.Limit
	}
	offset := opts.Offset

	query := fmt.Sprintf(`SELECT %s FROM webhook_events %s ORDER BY created_at DESC LIMIT %d OFFSET %d`,
		webhookEventColumns, where, limit, offset)

	rows, err := q.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*store.WebhookEvent
	for rows.Next() {
		event, err := scanWebhookEventRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, event)
	}

	return &store.ListResult[store.WebhookEvent]{
		Items:      items,
		TotalCount: total,
	}, nil
}

func (s *Store) UpdateWebhookEvent(ctx context.Context, eventID string, updates store.WebhookEventUpdates) error {
	return updateWebhookEvent(ctx, s.pool, eventID, updates)
}

func (t *Tx) UpdateWebhookEvent(ctx context.Context, eventID string, updates store.WebhookEventUpdates) error {
	return updateWebhookEvent(ctx, t.tx, eventID, updates)
}

func updateWebhookEvent(ctx context.Context, q querier, eventID string, updates store.WebhookEventUpdates) error {
	var setClauses []string
	var args []any
	argNum := 1

	if updates.Status != nil {
		setClauses = append(setClauses, fmt.Sprintf("status = $%d", argNum))
		args = append(args, *updates.Status)
		argNum++
	}
	if updates.Attempts != nil {
		setClauses = append(setClauses, fmt.Sprintf("attempts = $%d", argNum))
		args = append(args, *updates.Attempts)
		argNum++
	}
	if updates.LastError != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_error = $%d", argNum))
		args = append(args, *updates.LastError)
		argNum++
	}
	if updates.LastStatusCode != nil {
		setClauses = append(setClauses, fmt.Sprintf("last_status_code = $%d", argNum))
		args = append(args, *updates.LastStatusCode)
		argNum++
	}
	if updates.NextRetryAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("next_retry_at = $%d", argNum))
		args = append(args, *updates.NextRetryAt)
		argNum++
	}
	if updates.DeliveredAt != nil {
		setClauses = append(setClauses, fmt.Sprintf("delivered_at = $%d", argNum))
		args = append(args, *updates.DeliveredAt)
		argNum++
	}

	if len(setClauses) == 0 {
		return nil
	}

	setClauses = append(setClauses, "updated_at = NOW()")
	args = append(args, eventID)

	query := fmt.Sprintf("UPDATE webhook_events SET %s WHERE id = $%d",
		strings.Join(setClauses, ", "), argNum)

	result, err := q.Exec(ctx, query, args...)
	if err != nil {
		return handlePgError(err, "webhook_event", eventID)
	}

	if result.RowsAffected() == 0 {
		return store.ErrNotFound
	}
	return nil
}

func (s *Store) GetPendingWebhookEvents(ctx context.Context, limit int) ([]*store.WebhookEvent, error) {
	return getPendingWebhookEvents(ctx, s.pool, limit)
}

func (t *Tx) GetPendingWebhookEvents(ctx context.Context, limit int) ([]*store.WebhookEvent, error) {
	return getPendingWebhookEvents(ctx, t.tx, limit)
}

func getPendingWebhookEvents(ctx context.Context, q querier, limit int) ([]*store.WebhookEvent, error) {
	query := fmt.Sprintf(`
		SELECT %s FROM webhook_events
		WHERE status = 'pending' AND (next_retry_at IS NULL OR next_retry_at <= NOW())
		ORDER BY created_at ASC
		LIMIT $1`, webhookEventColumns)

	rows, err := q.Query(ctx, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*store.WebhookEvent
	for rows.Next() {
		event, err := scanWebhookEventRows(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}

	return events, nil
}

func (s *Store) CancelWebhookEventsByWebhook(ctx context.Context, webhookID string) error {
	return cancelWebhookEventsByWebhook(ctx, s.pool, webhookID)
}

func (t *Tx) CancelWebhookEventsByWebhook(ctx context.Context, webhookID string) error {
	return cancelWebhookEventsByWebhook(ctx, t.tx, webhookID)
}

func cancelWebhookEventsByWebhook(ctx context.Context, q querier, webhookID string) error {
	query := `UPDATE webhook_events SET status = 'canceled', updated_at = NOW() WHERE webhook_id = $1 AND status = 'pending'`
	_, err := q.Exec(ctx, query, webhookID)
	return err
}

// Scan helpers

func scanWebhook(row pgx.Row, identifier string) (*store.Webhook, error) {
	var webhook store.Webhook
	err := row.Scan(
		&webhook.ID, &webhook.Name, &webhook.URL, &webhook.Events,
		&webhook.SecretHash, &webhook.SecretPrefix, &webhook.IsActive,
		&webhook.MaxRetries, &webhook.RetryDelaySeconds, &webhook.TimeoutSeconds,
		&webhook.Headers, &webhook.TenantID, &webhook.Labels, &webhook.Annotations,
		&webhook.CreatedAt, &webhook.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, handlePgError(err, "webhook", identifier)
	}
	return &webhook, nil
}

func scanWebhookRows(rows pgx.Rows) (*store.Webhook, error) {
	var webhook store.Webhook
	err := rows.Scan(
		&webhook.ID, &webhook.Name, &webhook.URL, &webhook.Events,
		&webhook.SecretHash, &webhook.SecretPrefix, &webhook.IsActive,
		&webhook.MaxRetries, &webhook.RetryDelaySeconds, &webhook.TimeoutSeconds,
		&webhook.Headers, &webhook.TenantID, &webhook.Labels, &webhook.Annotations,
		&webhook.CreatedAt, &webhook.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &webhook, nil
}

func scanWebhookEvent(row pgx.Row, identifier string) (*store.WebhookEvent, error) {
	var event store.WebhookEvent
	err := row.Scan(
		&event.ID, &event.WebhookID, &event.EventType, &event.Payload, &event.Status,
		&event.Attempts, &event.LastError, &event.LastStatusCode, &event.NextRetryAt, &event.DeliveredAt,
		&event.TenantID, &event.CreatedAt, &event.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, store.ErrNotFound
		}
		return nil, handlePgError(err, "webhook_event", identifier)
	}
	return &event, nil
}

func scanWebhookEventRows(rows pgx.Rows) (*store.WebhookEvent, error) {
	var event store.WebhookEvent
	err := rows.Scan(
		&event.ID, &event.WebhookID, &event.EventType, &event.Payload, &event.Status,
		&event.Attempts, &event.LastError, &event.LastStatusCode, &event.NextRetryAt, &event.DeliveredAt,
		&event.TenantID, &event.CreatedAt, &event.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &event, nil
}
