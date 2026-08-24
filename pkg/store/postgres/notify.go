package postgres

import (
	"context"
	"fmt"
	"sync"

	"github.com/jackc/pgx/v5"

	"github.com/chunlea/marionette/pkg/store"
)

// Compile-time check: the fan-out relay type-asserts the store to this, so a
// signature drift must fail here rather than silently leave the relay unwired.
var _ store.Notifier = (*Store)(nil)

// Notify publishes a payload on a channel.
//
// It goes through the pool rather than through tenantDB, for two reasons.
// pg_notify reads no table, so row level security has nothing to apply and the
// tenant binding would buy nothing but two extra round trips. And a statement
// sent inside a transaction only delivers at COMMIT, which would put the
// notification behind whatever else that transaction was doing.
func (s *Store) Notify(ctx context.Context, channel, payload string) error {
	if channel == "" {
		return fmt.Errorf("postgres: notify channel is required")
	}
	if len(payload) > store.MaxNotifyPayload {
		return fmt.Errorf("%w: %d bytes on channel %q", store.ErrPayloadTooLarge, len(payload), channel)
	}

	if _, err := s.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, channel, payload); err != nil {
		return fmt.Errorf("publishing on %q: %w", channel, err)
	}
	return nil
}

// Listen opens a dedicated connection subscribed to channels.
//
// The connection is dialled outside the pool, not acquired from it. A listener
// holds its connection for the life of the process, and pgxpool has no concept
// of a connection that is busy without running a statement: an acquired one
// would count against MaxConns forever, and a hijacked one would be a
// connection the pool's health checks and lifetime limits no longer see.
func (s *Store) Listen(ctx context.Context, channels ...string) (store.NotificationStream, error) {
	if len(channels) == 0 {
		return nil, fmt.Errorf("postgres: at least one channel is required")
	}

	conn, err := pgx.ConnectConfig(ctx, s.pool.Config().ConnConfig.Copy())
	if err != nil {
		return nil, fmt.Errorf("dialling a listener connection: %w", err)
	}

	for _, channel := range channels {
		// The channel is an identifier, not a parameter: LISTEN takes no bind
		// parameters at all, so it has to be interpolated, and it has to be
		// quoted properly to be safe when it is.
		stmt := "LISTEN " + pgx.Identifier{channel}.Sanitize()
		if _, err := conn.Exec(ctx, stmt); err != nil {
			_ = conn.Close(ctx)
			return nil, fmt.Errorf("listening on %q: %w", channel, err)
		}
	}

	return &notificationStream{conn: conn}, nil
}

// notificationStream is one listener connection.
type notificationStream struct {
	conn *pgx.Conn

	mu     sync.Mutex
	closed bool
}

// Next waits for the next notification.
//
// pgx delivers notifications only while something is reading the connection,
// so this blocks in WaitForNotification and the caller's loop is what keeps
// the socket drained.
func (s *notificationStream) Next(ctx context.Context) (store.Notification, error) {
	notification, err := s.conn.WaitForNotification(ctx)
	if err != nil {
		return store.Notification{}, err
	}
	return store.Notification{
		Channel: notification.Channel,
		Payload: notification.Payload,
	}, nil
}

// Close releases the connection. It is safe to call more than once, which
// matters because the relay closes a broken stream on the way to reconnecting
// and again when it shuts down.
func (s *notificationStream) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true
	return s.conn.Close(ctx)
}
