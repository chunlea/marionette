package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

func createTestWebhook(ctx context.Context, t *testing.T, events []string) *store.Webhook {
	t.Helper()

	wh := &store.Webhook{
		Name:              "test-webhook-" + time.Now().Format("150405.000000"),
		URL:               "https://example.invalid/hook",
		Events:            events,
		SecretEncrypted:   "encrypted",
		SecretHash:        "hash-" + time.Now().Format("150405.000000"),
		SecretPrefix:      "whsec_test",
		IsActive:          true,
		MaxRetries:        3,
		RetryDelaySeconds: 60,
		TimeoutSeconds:    30,
	}
	require.NoError(t, testStore.CreateWebhook(ctx, wh))
	t.Cleanup(func() { _ = testStore.DeleteWebhook(context.Background(), wh.ID) })
	return wh
}

func createTestWebhookEvent(ctx context.Context, t *testing.T, webhookID, eventType string) *store.WebhookEvent {
	t.Helper()

	payload, err := json.Marshal(map[string]string{"event": eventType})
	require.NoError(t, err)

	event := &store.WebhookEvent{
		WebhookID: webhookID,
		EventType: eventType,
		Payload:   payload,
		Status:    store.WebhookEventStatusPending,
	}
	require.NoError(t, testStore.CreateWebhookEvent(ctx, event))
	return event
}

func TestWebhookCRUD(t *testing.T) {
	ctx := context.Background()

	wh := createTestWebhook(ctx, t, []string{"task.completed"})
	assert.NotEmpty(t, wh.ID)

	got, err := testStore.GetWebhook(ctx, wh.ID)
	require.NoError(t, err)
	assert.Equal(t, wh.Name, got.Name)
	assert.Equal(t, []string{"task.completed"}, got.Events)

	byName, err := testStore.GetWebhookByName(ctx, wh.Name, nil)
	require.NoError(t, err)
	assert.Equal(t, wh.ID, byName.ID)

	inactive := false
	require.NoError(t, testStore.UpdateWebhook(ctx, wh.ID, store.WebhookUpdates{IsActive: &inactive}))
	got, err = testStore.GetWebhook(ctx, wh.ID)
	require.NoError(t, err)
	assert.False(t, got.IsActive)

	_, err = testStore.GetWebhook(ctx, "whk_missing")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

// GetActiveWebhooksForEvent returns every active webhook for the tenant and
// leaves wildcard subscription matching ("task.*") to the WebhookManager, so
// the only filter asserted here is is_active.
func TestGetActiveWebhooksForEvent(t *testing.T) {
	ctx := context.Background()

	active := createTestWebhook(ctx, t, []string{"session.created"})

	disabled := createTestWebhook(ctx, t, []string{"session.created"})
	inactive := false
	require.NoError(t, testStore.UpdateWebhook(ctx, disabled.ID, store.WebhookUpdates{IsActive: &inactive}))

	matched, err := testStore.GetActiveWebhooksForEvent(ctx, "session.created", nil)
	require.NoError(t, err)

	ids := make(map[string]bool, len(matched))
	for _, w := range matched {
		ids[w.ID] = true
	}
	assert.True(t, ids[active.ID], "active webhook should be returned")
	assert.False(t, ids[disabled.ID], "inactive webhook should not be returned")

	t.Run("honours context cancellation", func(t *testing.T) {
		canceled, cancel := context.WithCancel(ctx)
		cancel()

		_, err := testStore.GetActiveWebhooksForEvent(canceled, "session.created", nil)
		assert.Error(t, err, "a canceled context must not be ignored")
	})
}

// TestGetPendingWebhookEventsClaims covers the duplicate-delivery bug: a plain
// SELECT hands the same rows to every replica on every tick.
func TestGetPendingWebhookEventsClaims(t *testing.T) {
	ctx := context.Background()

	wh := createTestWebhook(ctx, t, []string{"task.completed"})
	first := createTestWebhookEvent(ctx, t, wh.ID, "task.completed")
	second := createTestWebhookEvent(ctx, t, wh.ID, "task.completed")

	claimed, err := testStore.GetPendingWebhookEvents(ctx, 100)
	require.NoError(t, err)

	claimedIDs := make(map[string]bool, len(claimed))
	for _, e := range claimed {
		claimedIDs[e.ID] = true
	}
	require.True(t, claimedIDs[first.ID], "first event was not claimed")
	require.True(t, claimedIDs[second.ID], "second event was not claimed")

	// A second worker must not see the events the first one just claimed.
	again, err := testStore.GetPendingWebhookEvents(ctx, 100)
	require.NoError(t, err)
	for _, e := range again {
		assert.NotEqual(t, first.ID, e.ID, "event was handed out twice")
		assert.NotEqual(t, second.ID, e.ID, "event was handed out twice")
	}

	// The lease is visible on the row and in the future.
	leased, err := testStore.GetWebhookEvent(ctx, first.ID)
	require.NoError(t, err)
	require.NotNil(t, leased.NextRetryAt)
	assert.True(t, leased.NextRetryAt.After(time.Now()), "claim did not lease the event")
	assert.Equal(t, store.WebhookEventStatusPending, leased.Status,
		"claiming must not change the event status")
}

func TestGetPendingWebhookEventsRespectsRetrySchedule(t *testing.T) {
	ctx := context.Background()

	wh := createTestWebhook(ctx, t, []string{"task.failed"})
	event := createTestWebhookEvent(ctx, t, wh.ID, "task.failed")

	// Schedule it well into the future: it must not be claimed yet.
	future := time.Now().Add(time.Hour)
	require.NoError(t, testStore.UpdateWebhookEvent(ctx, event.ID, store.WebhookEventUpdates{
		NextRetryAt: &future,
	}))

	claimed, err := testStore.GetPendingWebhookEvents(ctx, 100)
	require.NoError(t, err)
	for _, e := range claimed {
		assert.NotEqual(t, event.ID, e.ID, "event scheduled for the future was claimed")
	}

	// Once due, it is claimable again.
	past := time.Now().Add(-time.Minute)
	require.NoError(t, testStore.UpdateWebhookEvent(ctx, event.ID, store.WebhookEventUpdates{
		NextRetryAt: &past,
	}))

	claimed, err = testStore.GetPendingWebhookEvents(ctx, 100)
	require.NoError(t, err)
	found := false
	for _, e := range claimed {
		if e.ID == event.ID {
			found = true
		}
	}
	assert.True(t, found, "due event was not claimed")
}

func TestGetPendingWebhookEventsSkipsTerminalStates(t *testing.T) {
	ctx := context.Background()

	wh := createTestWebhook(ctx, t, []string{"task.completed"})

	for _, status := range []store.WebhookEventStatus{
		store.WebhookEventStatusDelivered,
		store.WebhookEventStatusExhausted,
		store.WebhookEventStatusCanceled,
	} {
		event := createTestWebhookEvent(ctx, t, wh.ID, "task.completed")
		s := status
		require.NoError(t, testStore.UpdateWebhookEvent(ctx, event.ID, store.WebhookEventUpdates{Status: &s}))

		claimed, err := testStore.GetPendingWebhookEvents(ctx, 100)
		require.NoError(t, err)
		for _, e := range claimed {
			assert.NotEqualf(t, event.ID, e.ID, "%s event was claimed", status)
		}
	}
}

func TestCancelWebhookEventsByWebhook(t *testing.T) {
	ctx := context.Background()

	wh := createTestWebhook(ctx, t, []string{"session.terminated"})
	event := createTestWebhookEvent(ctx, t, wh.ID, "session.terminated")

	require.NoError(t, testStore.CancelWebhookEventsByWebhook(ctx, wh.ID))

	got, err := testStore.GetWebhookEvent(ctx, event.ID)
	require.NoError(t, err)
	assert.Equal(t, store.WebhookEventStatusCanceled, got.Status)
}

func TestListWebhookEvents(t *testing.T) {
	ctx := context.Background()

	wh := createTestWebhook(ctx, t, []string{"task.completed"})
	event := createTestWebhookEvent(ctx, t, wh.ID, "task.completed")

	list, err := testStore.ListWebhookEvents(ctx, store.ListWebhookEventsOptions{
		WebhookID: wh.ID,
	})
	require.NoError(t, err)
	require.Len(t, list.Items, 1)
	assert.Equal(t, event.ID, list.Items[0].ID)
}
