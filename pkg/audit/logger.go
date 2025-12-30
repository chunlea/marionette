package audit

import (
	"context"
	"time"

	"github.com/chunlea/marionette/pkg/id"
)

// DefaultLogger implements the Logger interface using a Store backend.
type DefaultLogger struct {
	store Store
}

// NewLogger creates a new audit logger with the given store.
func NewLogger(store Store) *DefaultLogger {
	return &DefaultLogger{store: store}
}

// Log records an audit event.
func (l *DefaultLogger) Log(ctx context.Context, event Event) error {
	stored := &StoredEvent{
		ID:    id.ActionLog(),
		Event: event,
	}

	// Set timestamp if not provided.
	if stored.Timestamp.IsZero() {
		stored.Timestamp = time.Now().UTC()
	}

	return l.store.CreateActionLog(ctx, stored)
}

// Query retrieves audit events matching the filter.
func (l *DefaultLogger) Query(ctx context.Context, filter Filter) (*QueryResult, error) {
	return l.store.ListActionLogs(ctx, filter)
}

// Ensure DefaultLogger implements Logger.
var _ Logger = (*DefaultLogger)(nil)
