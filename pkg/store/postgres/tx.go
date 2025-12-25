package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"

	"github.com/chunlea/marionette/pkg/store"
)

// Tx wraps a pgx transaction and implements store.Tx.
type Tx struct {
	tx     pgx.Tx
	logger *zap.Logger
	closed bool
}

// NOTE: Compile-time interface check is deferred until all CRUD methods are implemented.
// var _ store.Tx = (*Tx)(nil)

// Commit commits the transaction.
func (t *Tx) Commit(ctx context.Context) error {
	if t.closed {
		return store.ErrTxClosed
	}
	t.closed = true
	if err := t.tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}
	return nil
}

// Rollback aborts the transaction.
// Rollback is safe to call multiple times and after Commit.
func (t *Tx) Rollback(ctx context.Context) error {
	if t.closed {
		return nil // Already closed, no-op
	}
	t.closed = true
	if err := t.tx.Rollback(ctx); err != nil {
		return fmt.Errorf("rolling back transaction: %w", err)
	}
	return nil
}

// isClosed returns true if the transaction has been committed or rolled back.
//
//nolint:unused // Used by CRUD operations
func (t *Tx) isClosed() bool {
	return t.closed
}
