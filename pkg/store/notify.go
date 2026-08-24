package store

import (
	"context"
	"errors"
)

// MaxNotifyPayload is the largest payload a notification may carry.
//
// Postgres caps a whole notification at 8000 bytes, channel name included, and
// exceeding it is an error at NOTIFY time rather than a truncation. The margin
// is deliberate: payloads here are pointers into tables, so nothing legitimate
// comes close, and a payload that does is a design mistake worth failing on.
const MaxNotifyPayload = 7000

// ErrPayloadTooLarge is returned when a notification payload exceeds
// MaxNotifyPayload.
var ErrPayloadTooLarge = errors.New("store: notification payload is too large")

// Notification is one message delivered on a LISTEN channel.
type Notification struct {
	Channel string
	Payload string
}

// NotificationStream delivers notifications from one dedicated connection.
//
// It is not a queue: a stream that breaks, or a listener that was not
// connected when the notification was sent, misses it silently. Callers must
// only carry information whose loss costs a gap, never information whose loss
// costs a fact.
type NotificationStream interface {
	// Next blocks until a notification arrives, the connection breaks, or ctx
	// is done. A non-nil error means the stream is finished; open a new one.
	Next(ctx context.Context) (Notification, error)
	// Close releases the connection. Safe to call more than once.
	Close(ctx context.Context) error
}

// Notifier is the Postgres LISTEN/NOTIFY capability, as a capability rather
// than part of Store.
//
// It is optional on purpose. A store that does not implement it leaves the
// live fan-out relay unwired, and everything keeps working: the database rows
// are still written, history still reads back, and only the live tail on a
// replica that does not hold the runner's stream is missing.
type Notifier interface {
	// Notify publishes payload on channel. Delivery is best effort: replicas
	// that are not listening at this moment never see it.
	Notify(ctx context.Context, channel, payload string) error
	// Listen opens a dedicated connection subscribed to channels.
	Listen(ctx context.Context, channels ...string) (NotificationStream, error)
}
