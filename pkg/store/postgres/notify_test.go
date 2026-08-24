package postgres_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/chunlea/marionette/pkg/store"
)

// LISTEN/NOTIFY is the transport under the live fan-out relay. These tests pin
// the three properties the relay is built on: a notification reaches a listener
// on another connection, an oversized payload is refused rather than truncated,
// and a listener hears its own process's notifications too - which is why the
// relay suppresses by replica id rather than assuming it will not see them.

func TestNotifyReachesAListener(t *testing.T) {
	ctx := context.Background()

	stream, err := testStore.Listen(ctx, "fanout_test_a")
	require.NoError(t, err)
	defer func() { _ = stream.Close(context.Background()) }()

	require.NoError(t, testStore.Notify(ctx, "fanout_test_a", `{"hello":"world"}`))

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	notification, err := stream.Next(waitCtx)
	require.NoError(t, err)
	assert.Equal(t, "fanout_test_a", notification.Channel)
	assert.Equal(t, `{"hello":"world"}`, notification.Payload)
}

func TestNotifyIsHeardOnEveryChannelListenedTo(t *testing.T) {
	ctx := context.Background()

	stream, err := testStore.Listen(ctx, "fanout_test_b", "fanout_test_c")
	require.NoError(t, err)
	defer func() { _ = stream.Close(context.Background()) }()

	require.NoError(t, testStore.Notify(ctx, "fanout_test_c", "second"))
	require.NoError(t, testStore.Notify(ctx, "fanout_test_b", "first"))

	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	seen := map[string]string{}
	for i := 0; i < 2; i++ {
		notification, err := stream.Next(waitCtx)
		require.NoError(t, err)
		seen[notification.Channel] = notification.Payload
	}
	assert.Equal(t, map[string]string{"fanout_test_b": "first", "fanout_test_c": "second"}, seen)
}

func TestNotifyRefusesAnOversizedPayload(t *testing.T) {
	ctx := context.Background()

	err := testStore.Notify(ctx, "fanout_test_d", strings.Repeat("x", store.MaxNotifyPayload+1))
	require.ErrorIs(t, err, store.ErrPayloadTooLarge)
}

func TestNotifyRequiresAChannel(t *testing.T) {
	require.Error(t, testStore.Notify(context.Background(), "", "payload"))
}

func TestListenRequiresAChannel(t *testing.T) {
	_, err := testStore.Listen(context.Background())
	require.Error(t, err)
}

// A channel name is an identifier interpolated into LISTEN, so it is quoted
// rather than concatenated. A name that would break out of the statement has to
// come back as an error, not as executed SQL.
func TestListenQuotesTheChannelName(t *testing.T) {
	ctx := context.Background()

	stream, err := testStore.Listen(ctx, `weird"; DROP TABLE runners; --`)
	if err == nil {
		// Quoting made it a (useless but harmless) channel name rather than a
		// second statement. What matters is that the table is still there.
		defer func() { _ = stream.Close(context.Background()) }()
	}

	_, listErr := testStore.ListRunners(ctx, store.ListRunnersOptions{})
	require.NoError(t, listErr, "the runners table must have survived")
}

func TestClosedStreamStopsDelivering(t *testing.T) {
	ctx := context.Background()

	stream, err := testStore.Listen(ctx, "fanout_test_e")
	require.NoError(t, err)

	require.NoError(t, stream.Close(ctx))
	// Idempotent: the relay closes a broken stream on its way to reconnecting
	// and again on shutdown.
	require.NoError(t, stream.Close(ctx))

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err = stream.Next(waitCtx)
	require.Error(t, err, "a closed stream must report the break, not block")
}
